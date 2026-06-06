package sdk

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type heartbeatResponse struct {
	Status            string          `json:"status"`
	Lease             json.RawMessage `json:"lease"`
	LeaseSignature    string          `json:"lease_signature"`
	ResponseSignature string          `json:"response_signature"`
	Nonce             string          `json:"nonce"`
	ServerTime        string          `json:"server_time"`
	Updates           []updateInfo    `json:"updates"`
	Reason            string          `json:"reason"`
	Message           string          `json:"message"`
}

type updateInfo struct {
	Component       string `json:"component"`
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	Mandatory       bool   `json:"mandatory"`
	ReleaseNotes    string `json:"release_notes"`
}

type heartbeatComponent struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
}

type heartbeatRequestBody struct {
	LicenseKey    string               `json:"license_key"`
	MachineID     string               `json:"machine_id"`
	ProjectSlug   string               `json:"project_slug"`
	ComponentSlug string               `json:"component_slug"`
	Components    []heartbeatComponent `json:"components"`
	Nonce         string               `json:"nonce"`
	Timestamp     int64                `json:"timestamp"`
	BinaryHash    string               `json:"binary_hash"`
}

type heartbeatSignaturePayload struct {
	Lease          json.RawMessage `json:"lease"`
	LeaseSignature string          `json:"lease_signature"`
	Nonce          string          `json:"nonce"`
	ServerTime     string          `json:"server_time"`
	Status         string          `json:"status"`
	UpdatesDigest  string          `json:"updates_digest"`
}

func (g *Guard) startHeartbeat(ctx context.Context, done chan struct{}) {
	interval := g.cfg.HeartbeatInterval
	graceStart := time.Time{}

	go func() {
		defer g.finishHeartbeat(done)

		for {
			jitter := heartbeatJitter(interval)
			select {
			case <-ctx.Done():
				return
			case <-time.After(jitter):
			}

			err := g.sendHeartbeat(ctx)
			if err == nil {
				g.sm.OnHeartbeatOK()
				graceStart = time.Time{}
				continue
			}
			if errors.Is(err, context.Canceled) {
				return
			}

			if isFatalError(err) {
				g.sm.OnKill()
				_ = g.persistBan()
				return
			}

			g.sm.OnHeartbeatFail()
			_ = g.persistGrace()
			if graceStart.IsZero() {
				graceStart = time.Now()
			}
			if time.Since(graceStart) > g.cfg.GracePolicy.MaxOfflineDuration {
				g.sm.OnGracePeriodExpired()
				_ = g.persistLock()
				return
			}
		}
	}()
}

func heartbeatJitter(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	delta := interval / 10
	if delta <= 0 {
		return interval
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if interval > maxDuration-delta {
		return interval
	}
	maxOffset := delta * 2
	offset, err := rand.Int(rand.Reader, big.NewInt(int64(maxOffset)+1))
	if err != nil {
		return interval
	}
	return interval - delta + time.Duration(offset.Int64())
}

func (g *Guard) sendHeartbeat(parent context.Context) error {
	g.mu.RLock()
	currentVersion := g.version
	managedVersionsSnapshot := make(map[string]string, len(g.managedVersions))
	for k, v := range g.managedVersions {
		managedVersionsSnapshot[k] = v
	}
	g.mu.RUnlock()

	components := []heartbeatComponent{
		{
			Slug:    g.cfg.ComponentSlug,
			Version: currentVersion,
		},
	}
	for _, mc := range g.cfg.ManagedComponents {
		components = append(components, heartbeatComponent{
			Slug:    mc.Slug,
			Version: managedVersionsSnapshot[mc.Slug],
		})
	}

	binaryHash, err := GetBinaryHash()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	nonce, err := randomNonce()
	if err != nil {
		return err
	}
	reqBody := heartbeatRequestBody{
		LicenseKey:    g.cfg.LicenseKey,
		MachineID:     g.fingerprint.MachineID(),
		ProjectSlug:   g.cfg.ProjectSlug,
		ComponentSlug: g.cfg.ComponentSlug,
		Components:    components,
		Nonce:         nonce,
		Timestamp:     nowUnix(),
		BinaryHash:    binaryHash,
	}

	var resp heartbeatResponse
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	raw, err := g.postJSON(ctx, "/api/v1/heartbeat", reqBodyJSON)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}

	if err := g.verifyHeartbeatResponse(resp, nonce); err != nil {
		return err
	}
	if resp.Status == "kill" {
		g.sm.OnKill()
		_ = g.persistBan()
		return ErrBanned
	}

	leaseValue, err := parseAndVerifyLease(resp.Lease, resp.LeaseSignature, g.verificationKeys(), g.fingerprint.MachineID(), time.Now(), g.currentWatermark())
	if err != nil {
		return err
	}
	if err := g.acceptLease(leaseValue, resp.LeaseSignature, false); err != nil {
		return err
	}

	for _, u := range resp.Updates {
		if g.cfg.OTA.Enabled && u.UpdateAvailable {
			g.handleUpdateNotification(u)
		}
	}

	return nil
}

func (g *Guard) verifyHeartbeatResponse(resp heartbeatResponse, requestNonce string) error {
	if resp.ResponseSignature == "" {
		return ErrHeartbeatInvalid
	}
	if resp.Nonce != requestNonce {
		return ErrHeartbeatNonceMismatch
	}

	payload := heartbeatSignaturePayload{
		Lease:          normalizedJSONObject(resp.Lease),
		LeaseSignature: resp.LeaseSignature,
		Nonce:          resp.Nonce,
		ServerTime:     resp.ServerTime,
		Status:         resp.Status,
		UpdatesDigest:  updatesDigest(resp.Updates),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrHeartbeatInvalid
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return ErrHeartbeatInvalid
	}
	if err := verifyEd25519Digest(canonical, resp.ResponseSignature, g.verificationKeys()); err != nil {
		return ErrHeartbeatInvalid
	}
	return nil
}

func normalizedJSONObject(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] != '{' {
		return json.RawMessage("{}")
	}
	return trimmed
}

func updatesDigest(updates []updateInfo) string {
	if len(updates) == 0 {
		updates = []updateInfo{}
	}
	raw, _ := json.Marshal(updates)
	canonical, err := canonicalJSON(raw)
	if err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func (g *Guard) persistBan() error {
	state := g.currentLeaseState()
	if state == nil {
		state = &persistedState{}
	}
	state.BanFlag = true
	state.LockFlag = false
	return g.store.Save(state)
}

func (g *Guard) persistLock() error {
	state := g.currentLeaseState()
	if state == nil {
		state = &persistedState{}
	}
	state.LockFlag = true
	return g.store.Save(state)
}

func (g *Guard) persistGrace() error {
	state := g.currentLeaseState()
	if state == nil {
		return nil
	}
	return g.store.Save(state)
}

func isFatalError(err error) bool {
	return errors.Is(err, ErrBanned) || errors.Is(err, ErrLicenseSuspended) || errors.Is(err, ErrMachineBanned)
}
