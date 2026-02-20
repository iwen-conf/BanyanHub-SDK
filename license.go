package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type cachedLicense struct {
	LicenseKey string `json:"license_key"`
	PublicData string `json:"public_data"`
	Signature  string `json:"signature"`
	VerifiedAt string `json:"verified_at"`
}

func (g *Guard) verifyLicense() error {
	// 1. Try local cache first
	if cached, err := g.loadCachedLicense(); err == nil {
		sig, err := base64.StdEncoding.DecodeString(cached.Signature)
		if err == nil {
			digest := sha256.Sum256([]byte(cached.PublicData))
			if ed25519.Verify(g.publicKey, digest[:], sig) {
				return nil
			}
		}
	}

	// 2. Cloud verification
	reqBody := map[string]any{
		"license_key":  g.cfg.LicenseKey,
		"machine_id":   g.fingerprint.MachineID(),
		"aux_signals":  g.fingerprint.AuxSignals(),
		"project_slug": g.cfg.ProjectSlug,
		"hostname":     hostname(),
		"os":           g.fingerprint.auxSignals["os"],
		"arch":         g.fingerprint.auxSignals["arch"],
		"nonce":        randomNonce(),
		"timestamp":    nowUnix(),
	}

	var resp struct {
		Status       string `json:"status"`
		Error        string `json:"error"`
		Message      string `json:"message"`
		UpdateFrozen bool   `json:"update_frozen"`
		PublicData   string `json:"public_data"`
		Signature    string `json:"signature"`
	}

	if err := g.postJSON(context.Background(), "/api/v1/verify", reqBody, &resp); err != nil {
		return fmt.Errorf("%w: %v", ErrNetworkError, err)
	}

	if resp.Error != "" {
		switch resp.Error {
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
		default:
			return fmt.Errorf("%w: %s", ErrLicenseInvalid, resp.Error)
		}
	}

	// 3. Cache locally
	g.cacheLicense(resp.PublicData, resp.Signature)

	return nil
}

func (g *Guard) cacheLicense(publicData, signature string) {
	dir := g.cacheDir()
	os.MkdirAll(dir, 0o700)

	data := cachedLicense{
		LicenseKey: g.cfg.LicenseKey,
		PublicData: publicData,
		Signature:  signature,
		VerifiedAt: nowRFC3339(),
	}

	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(dir, "license.cache"), b, 0o600)
}

func (g *Guard) loadCachedLicense() (*cachedLicense, error) {
	path := filepath.Join(g.cacheDir(), "license.cache")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cached cachedLicense
	if err := json.Unmarshal(b, &cached); err != nil {
		return nil, err
	}
	return &cached, nil
}

func (g *Guard) cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deploy-guard", g.cfg.ProjectSlug, g.cfg.ComponentSlug)
}
