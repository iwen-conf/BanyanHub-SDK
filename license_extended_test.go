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

// TestLoadCachedLicense_Success tests loading valid cached license
func TestLoadCachedLicense_Success(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://localhost",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	// Create cached license
	cacheData := cachedLicense{
		LicenseKey: "test-key",
		PublicData: "test-data",
		Signature:  encodeSignatureB64(privKey, "test-data"),
		VerifiedAt: time.Now().Format(time.RFC3339),
	}

	cacheDir := g.cacheDir()
	os.MkdirAll(cacheDir, 0o700)
	cacheJson, _ := json.Marshal(cacheData)
	os.WriteFile(filepath.Join(cacheDir, "license.cache"), cacheJson, 0o600)

	// Load cache
	cached, err := g.loadCachedLicense()
	if err != nil {
		t.Fatalf("loadCachedLicense failed: %v", err)
	}

	if cached.LicenseKey != "test-key" {
		t.Errorf("expected license key test-key, got %s", cached.LicenseKey)
	}
	if cached.PublicData != "test-data" {
		t.Errorf("expected public data test-data, got %s", cached.PublicData)
	}

	// Cleanup
	os.RemoveAll(cacheDir)
}

// TestLoadCachedLicense_FileNotFound tests loading when cache file doesn't exist
func TestLoadCachedLicense_FileNotFound(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://localhost",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "nonexistent-project-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	cached, err := g.loadCachedLicense()
	if err == nil {
		t.Error("expected error, got nil")
	}
	if cached != nil {
		t.Error("expected nil result on error")
	}
}

// TestLoadCachedLicense_InvalidJSON tests loading with malformed JSON
func TestLoadCachedLicense_InvalidJSON(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://localhost",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "invalid-json-project-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	cacheDir := g.cacheDir()
	os.MkdirAll(cacheDir, 0o700)
	os.WriteFile(filepath.Join(cacheDir, "license.cache"), []byte("invalid json"), 0o600)

	cached, err := g.loadCachedLicense()
	if err == nil {
		t.Error("expected error, got nil")
	}
	if cached != nil {
		t.Error("expected nil result on error")
	}

	// Cleanup
	os.RemoveAll(cacheDir)
}

// TestCacheLicense_Success tests saving license to cache
func TestCacheLicense_Success(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://localhost",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "cache-test-project-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	g.cacheLicense("test-data", "test-signature")

	// Verify file exists
	cacheDir := g.cacheDir()
	cacheFile := filepath.Join(cacheDir, "license.cache")
	if _, err := os.Stat(cacheFile); err != nil {
		t.Errorf("cache file not created: %v", err)
	}

	// Verify content
	data, _ := os.ReadFile(cacheFile)
	var cached cachedLicense
	json.Unmarshal(data, &cached)

	if cached.PublicData != "test-data" {
		t.Errorf("expected public data test-data, got %s", cached.PublicData)
	}
	if cached.Signature != "test-signature" {
		t.Errorf("expected signature test-signature, got %s", cached.Signature)
	}

	// Cleanup
	os.RemoveAll(cacheDir)
}

// TestVerifyLicense_CloudVerificationSuccess tests cloud verification success
func TestVerifyLicense_CloudVerificationSuccess(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/verify" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"public_data": "test-data",
				"signature":   encodeSignatureB64(privKey, "test-data"),
			})
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "verify-test-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.verifyLicense()
	if err != nil {
		t.Fatalf("verifyLicense failed: %v", err)
	}

	// Cleanup
	os.RemoveAll(g.cacheDir())
	server.Close()
}

// TestVerifyLicense_CachedLicenseValid tests verification using cached license
func TestVerifyLicense_CachedLicenseValid(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://localhost:9999", // This shouldn't be called
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "cached-verify-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	// Pre-create cache
	cacheData := cachedLicense{
		LicenseKey: "test-key",
		PublicData: "cached-data",
		Signature:  encodeSignatureB64(privKey, "cached-data"),
		VerifiedAt: time.Now().Format(time.RFC3339),
	}

	cacheDir := g.cacheDir()
	cacheJson, _ := json.Marshal(cacheData)
	os.MkdirAll(cacheDir, 0o700)
	os.WriteFile(filepath.Join(cacheDir, "license.cache"), cacheJson, 0o600)

	err := g.verifyLicense()
	if err != nil {
		t.Fatalf("verifyLicense with cache failed: %v", err)
	}

	// Cleanup
	os.RemoveAll(cacheDir)
}

// TestVerifyLicense_InvalidLicense tests verification with invalid license error
func TestVerifyLicense_InvalidLicense(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "license_not_found",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "invalid-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "invalid-lic-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.verifyLicense()
	if err != ErrLicenseInvalid {
		t.Errorf("expected ErrLicenseInvalid, got %v", err)
	}

	server.Close()
}

// TestVerifyLicense_ExpiredLicense tests with expired license error
func TestVerifyLicense_ExpiredLicense(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "license_expired",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "expired-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "expired-lic-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.verifyLicense()
	if err != ErrLicenseExpired {
		t.Errorf("expected ErrLicenseExpired, got %v", err)
	}

	server.Close()
}

// TestVerifyLicense_ProjectNotAuthorized tests with project not authorized error
func TestVerifyLicense_ProjectNotAuthorized(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "project_not_authorized",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "unauth-proj-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.verifyLicense()
	if err != ErrProjectNotAuthorized {
		t.Errorf("expected ErrProjectNotAuthorized, got %v", err)
	}

	server.Close()
}

// TestVerifyLicense_MaxMachinesExceeded tests with max machines exceeded error
func TestVerifyLicense_MaxMachinesExceeded(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "max_machines_exceeded",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "max-machines-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.verifyLicense()
	if err != ErrMaxMachinesExceeded {
		t.Errorf("expected ErrMaxMachinesExceeded, got %v", err)
	}

	server.Close()
}

// TestVerifyLicense_MachineBanned tests with machine banned error
func TestVerifyLicense_MachineBanned(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "machine_banned",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "banned-machine-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.verifyLicense()
	if err != ErrMachineBanned {
		t.Errorf("expected ErrMachineBanned, got %v", err)
	}

	server.Close()
}

// TestVerifyLicense_NetworkError tests with network failure
func TestVerifyLicense_NetworkError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://invalid-server.local",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "network-error-" + time.Now().Format("20060102150405"),
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.verifyLicense()
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// Helper function
func encodeSignatureB64(privKey ed25519.PrivateKey, data string) string {
	digest := sha256.Sum256([]byte(data))
	sig := ed25519.Sign(privKey, digest[:])
	return base64.StdEncoding.EncodeToString(sig)
}
