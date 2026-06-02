package sdk

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	defaultLeaseClockSkew = 5 * time.Minute
	verifyTimeout         = 30 * time.Second
)

type lease struct {
	ExpiresAt   string   `json:"expires_at"`
	Features    []string `json:"features,omitempty"`
	GraceUntil  string   `json:"grace_until"`
	IssuedAt    string   `json:"issued_at"`
	LeaseID     string   `json:"lease_id"`
	LicenseKey  string   `json:"license_key"`
	MachineID   string   `json:"machine_id"`
	MaxMachines int      `json:"max_machines"`
	ProjectSlug string   `json:"project_slug"`
	ServerTime  string   `json:"server_time"`
	Tier        string   `json:"tier"`
}

type verifyResponse struct {
	Lease          json.RawMessage `json:"lease"`
	LeaseSignature string          `json:"lease_signature"`
	ServerTime     string          `json:"server_time"`
	Error          string          `json:"error"`
	Message        string          `json:"message"`
}

func (g *Guard) verifyLicense(ctx context.Context) error {
	now := time.Now()
	if err := g.validatePersistedLease(now); err == nil {
		g.sm.OnVerifySuccess()
		return nil
	}

	verifiedLease, leaseSignature, err := g.verifyOnline(ctx, now)
	if err != nil {
		return err
	}
	if err := g.acceptLease(verifiedLease, leaseSignature, false); err != nil {
		return err
	}
	g.sm.OnVerifySuccess()
	return nil
}

func (g *Guard) verifyOnline(parent context.Context, now time.Time) (*lease, string, error) {
	binaryHash, err := GetBinaryHash()
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrNetworkError, err)
	}

	reqBody := map[string]any{
		"license_key":    g.cfg.LicenseKey,
		"machine_id":     g.fingerprint.MachineID(),
		"aux_signals":    g.fingerprint.AuxSignals(),
		"project_slug":   g.cfg.ProjectSlug,
		"component_slug": g.cfg.ComponentSlug,
		"hostname":       hostname(),
		"os":             g.fingerprint.auxSignals["os"],
		"arch":           g.fingerprint.auxSignals["arch"],
		"nonce":          randomNonce(),
		"timestamp":      now.Unix(),
		"binary_hash":    binaryHash,
	}

	var resp verifyResponse
	ctx, cancel := context.WithTimeout(parent, verifyTimeout)
	defer cancel()

	if err := g.postJSON(ctx, "/api/v1/verify", reqBody, &resp); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	if resp.Error != "" {
		return nil, "", mapVerifyError(resp.Error)
	}
	if len(resp.Lease) == 0 || resp.LeaseSignature == "" {
		return nil, "", ErrInvalidServerResponse
	}

	leaseValue, err := parseAndVerifyLease(resp.Lease, resp.LeaseSignature, g.verificationKeys(), g.fingerprint.MachineID(), now, g.currentWatermark())
	if err != nil {
		return nil, "", err
	}

	return leaseValue, resp.LeaseSignature, nil
}

func (g *Guard) validatePersistedLease(now time.Time) error {
	state := g.currentLeaseState()
	if state == nil || state.Lease == nil || state.LeaseSignature == "" {
		return ErrLeaseUnavailable
	}
	if state.LockFlag {
		g.sm.OnGracePeriodExpired()
		return ErrLocked
	}
	if state.BanFlag {
		g.sm.OnKill()
		return ErrBanned
	}
	if _, err := parseAndVerifyLease(state.LeaseCanonical, state.LeaseSignature, g.verificationKeys(), g.fingerprint.MachineID(), now, state.Watermark); err != nil {
		return err
	}
	if watermarkTime, err := parseRFC3339(state.Watermark); err == nil {
		if now.Before(watermarkTime.Add(-defaultLeaseClockSkew)) {
			return ErrClockRollback
		}
	}
	return nil
}

func (g *Guard) acceptLease(leaseValue *lease, leaseSignature string, keepCurrentState bool) error {
	canonical, err := canonicalJSONFromLease(leaseValue)
	if err != nil {
		return err
	}
	state := g.currentLeaseState()
	if state == nil {
		state = &persistedState{}
	}
	state.Lease = leaseValue
	state.LeaseCanonical = canonical
	state.LeaseSignature = leaseSignature
	state.Watermark = maxTimestamp(state.Watermark, leaseValue.ServerTime)
	if !keepCurrentState {
		state.LockFlag = false
		state.BanFlag = false
	}
	if err := g.store.Save(state); err != nil {
		return err
	}
	return nil
}

func (g *Guard) currentWatermark() string {
	if state := g.currentLeaseState(); state != nil {
		return state.Watermark
	}
	return ""
}

func canonicalJSONFromLease(value *lease) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(raw)
}

func parseAndVerifyLease(raw json.RawMessage, signature string, publicKeys []ed25519.PublicKey, machineID string, now time.Time, watermark string) (*lease, error) {
	if !json.Valid(raw) {
		return nil, ErrInvalidServerResponse
	}

	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, ErrInvalidServerResponse
	}

	if err := verifyEd25519Digest(canonical, signature, publicKeys); err != nil {
		return nil, err
	}

	var value lease
	if err := json.Unmarshal(canonical, &value); err != nil {
		return nil, ErrInvalidServerResponse
	}

	if value.MachineID != machineID {
		return nil, ErrLeaseBindingMismatch
	}
	expiresAt, err := parseRFC3339(value.ExpiresAt)
	if err != nil {
		return nil, ErrInvalidServerResponse
	}
	graceUntil, err := parseRFC3339(value.GraceUntil)
	if err != nil {
		return nil, ErrInvalidServerResponse
	}
	serverTime, err := parseRFC3339(value.ServerTime)
	if err != nil {
		return nil, ErrInvalidServerResponse
	}
	if now.After(expiresAt) || now.After(graceUntil) {
		return nil, ErrLicenseExpired
	}
	if now.Before(serverTime.Add(-defaultLeaseClockSkew)) {
		return nil, ErrClockRollback
	}
	if watermark != "" {
		watermarkTime, err := parseRFC3339(watermark)
		if err != nil {
			return nil, ErrStateTampered
		}
		if now.Before(watermarkTime.Add(-defaultLeaseClockSkew)) {
			return nil, ErrClockRollback
		}
		if serverTime.Before(watermarkTime) {
			return nil, ErrClockRollback
		}
	}

	return &value, nil
}

func verifyEd25519Digest(canonical []byte, signature string, publicKeys []ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	digest := sha256.Sum256(canonical)
	for _, publicKey := range publicKeys {
		if ed25519.Verify(publicKey, digest[:], sig) {
			return nil
		}
	}
	return ErrLicenseInvalid
}

func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	normalized, err := marshalCanonical(value)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func marshalCanonical(v any) ([]byte, error) {
	switch value := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		buf := make([]byte, 0, 256)
		buf = append(buf, '{')
		for i, key := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			keyJSON, _ := json.Marshal(key)
			buf = append(buf, keyJSON...)
			buf = append(buf, ':')
			child, err := marshalCanonical(value[key])
			if err != nil {
				return nil, err
			}
			buf = append(buf, child...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []any:
		buf := make([]byte, 0, 128)
		buf = append(buf, '[')
		for i, item := range value {
			if i > 0 {
				buf = append(buf, ',')
			}
			child, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, child...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		return json.Marshal(value)
	}
}

func parseRFC3339(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}

func mapVerifyError(code string) error {
	switch code {
	case "license_not_found", "license_inactive":
		return ErrLicenseInvalid
	case "license_expired":
		return ErrLicenseExpired
	case "project_not_authorized":
		return ErrProjectNotAuthorized
	case "max_machines_exceeded":
		return ErrMaxMachinesExceeded
	case "machine_banned":
		return ErrMachineBanned
	case "binary_not_recognized":
		return ErrBinaryNotRecognized
	default:
		return fmt.Errorf("%w: %s", ErrLicenseInvalid, code)
	}
}

func maxTimestamp(current, candidate string) string {
	if current == "" {
		return candidate
	}
	currentTime, currentErr := parseRFC3339(current)
	candidateTime, candidateErr := parseRFC3339(candidate)
	switch {
	case currentErr != nil:
		return candidate
	case candidateErr != nil:
		return current
	case candidateTime.After(currentTime):
		return candidate
	default:
		return current
	}
}

func deriveLeaseSecret(signature string, leaseValue *lease) ([]byte, error) {
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return nil, err
	}
	reader := hkdf.New(
		sha256.New,
		sigBytes,
		[]byte(leaseValue.MachineID),
		[]byte(leaseValue.LicenseKey+"|"+leaseValue.LeaseID),
	)
	secret := make([]byte, 32)
	if _, err := io.ReadFull(reader, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func leaseAAD(leaseValue *lease) []byte {
	return []byte(leaseValue.ProjectSlug + "|" + leaseValue.ComponentScope())
}

func (l *lease) ComponentScope() string {
	return l.Tier + "|" + l.LeaseID
}

func newLeaseAEAD(signature string, leaseValue *lease) (cipher.AEAD, error) {
	secret, err := deriveLeaseSecret(signature, leaseValue)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func deriveFeatureToken(signature string, leaseValue *lease, name string) (string, error) {
	secret, err := deriveLeaseSecret(signature, leaseValue)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(name))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
