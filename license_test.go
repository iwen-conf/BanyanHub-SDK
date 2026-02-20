package sdk

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyLicense_Success(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	publicData := "test-project"
	digest := sha256.Sum256([]byte(publicData))
	signature := ed25519.Sign(privKey, digest[:])
	signatureB64 := base64.StdEncoding.EncodeToString(signature)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status":      "ok",
			"public_data": publicData,
			"signature":   signatureB64,
		})
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	if err := g.verifyLicense(); err != nil {
		t.Errorf("verifyLicense failed: %v", err)
	}
}

func TestVerifyLicense_Expired(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"error": "license_expired",
		})
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	err := g.verifyLicense()
	if err != ErrLicenseExpired {
		t.Errorf("expected ErrLicenseExpired, got %v", err)
	}
}

func TestLicenseCache_ReadWrite(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")

	publicData := "test-project"
	digest := sha256.Sum256([]byte(publicData))
	signature := ed25519.Sign(privKey, digest[:])
	signatureB64 := base64.StdEncoding.EncodeToString(signature)

	g := &Guard{
		cfg: Config{
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
		},
		publicKey: pubKey,
	}

	// Write cache
	g.cacheLicense(publicData, signatureB64)

	// Read cache
	cached, err := g.loadCachedLicense()
	if err != nil {
		t.Fatalf("loadCachedLicense failed: %v", err)
	}

	if cached.PublicData != publicData {
		t.Errorf("expected public_data %s, got %s", publicData, cached.PublicData)
	}

	if cached.Signature != signatureB64 {
		t.Errorf("expected signature %s, got %s", signatureB64, cached.Signature)
	}

	// Verify signature
	sig, err := base64.StdEncoding.DecodeString(cached.Signature)
	if err != nil {
		t.Fatalf("decode signature failed: %v", err)
	}

	digest2 := sha256.Sum256([]byte(cached.PublicData))
	if !ed25519.Verify(g.publicKey, digest2[:], sig) {
		t.Error("signature verification failed")
	}
}

func TestLicenseCache_InvalidSignature(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".deploy-guard", "test-project", "backend")
	os.MkdirAll(cacheDir, 0o700)

	// Write cache with invalid signature
	cache := cachedLicense{
		LicenseKey: "test-key",
		PublicData: "test-data",
		Signature:  base64.StdEncoding.EncodeToString([]byte("invalid signature")),
		VerifiedAt: time.Now().Format(time.RFC3339),
	}

	data, _ := json.Marshal(cache)
	os.WriteFile(filepath.Join(cacheDir, "license.cache"), data, 0o600)

	g := &Guard{
		cfg: Config{
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
		},
		publicKey: pubKey,
	}

	// Override cacheDir to use tmpDir
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	cached, err := g.loadCachedLicense()
	if err != nil {
		t.Fatalf("loadCachedLicense failed: %v", err)
	}

	// Verify signature should fail
	sig, _ := base64.StdEncoding.DecodeString(cached.Signature)
	digest := sha256.Sum256([]byte(cached.PublicData))
	if ed25519.Verify(g.publicKey, digest[:], sig) {
		t.Error("expected signature verification to fail, but it succeeded")
	}
}
