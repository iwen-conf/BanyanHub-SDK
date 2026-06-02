package sdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
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

func (g *Guard) startHeartbeat(ctx context.Context, done chan struct{}) {
	interval := g.cfg.HeartbeatInterval
	graceStart := time.Time{}

	go func() {
		defer g.finishHeartbeat(done)

		for {
			jitter := time.Duration(float64(interval) * (0.9 + rand.Float64()*0.2))
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

func (g *Guard) sendHeartbeat(parent context.Context) error {
	g.mu.RLock()
	currentVersion := g.version
	managedVersionsSnapshot := make(map[string]string, len(g.managedVersions))
	for k, v := range g.managedVersions {
		managedVersionsSnapshot[k] = v
	}
	g.mu.RUnlock()

	components := []map[string]string{
		{
			"slug":    g.cfg.ComponentSlug,
			"version": currentVersion,
		},
	}
	for _, mc := range g.cfg.ManagedComponents {
		components = append(components, map[string]string{
			"slug":    mc.Slug,
			"version": managedVersionsSnapshot[mc.Slug],
		})
	}

	binaryHash, err := GetBinaryHash()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	nonce := randomNonce()
	reqBody := map[string]any{
		"license_key":    g.cfg.LicenseKey,
		"machine_id":     g.fingerprint.MachineID(),
		"project_slug":   g.cfg.ProjectSlug,
		"component_slug": g.cfg.ComponentSlug,
		"components":     components,
		"nonce":          nonce,
		"timestamp":      nowUnix(),
		"binary_hash":    binaryHash,
	}

	var resp heartbeatResponse
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := g.postJSON(ctx, "/api/v1/heartbeat", reqBody, &resp); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrNetworkError, err)
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

	payload := map[string]any{
		"lease":           mustJSONObject(resp.Lease),
		"lease_signature": resp.LeaseSignature,
		"nonce":           resp.Nonce,
		"server_time":     resp.ServerTime,
		"status":          resp.Status,
		"updates_digest":  updatesDigest(resp.Updates),
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

func mustJSONObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{}
	}
	return value
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
	return err == ErrBanned || err == ErrLicenseSuspended || err == ErrMachineBanned
}
