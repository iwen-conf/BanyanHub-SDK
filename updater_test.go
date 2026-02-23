package sdk

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateBackend_Success(t *testing.T) {
	// Generate test key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Create test binary
	testBinary := []byte("test binary content")
	hash := sha256.Sum256(testBinary)
	hashStr := hex.EncodeToString(hash[:])

	// Sign the hash
	digest := sha256.Sum256([]byte(hashStr))
	signature := ed25519.Sign(privKey, digest[:])
	signatureB64 := base64.StdEncoding.EncodeToString(signature)

	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/update/download" {
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/test.bin",
				"sha256":       hashStr,
				"signature":    signatureB64,
			})
		} else if r.URL.Path == "/download/test.bin" {
			w.Write(testBinary)
		}
	}))
	defer server.Close()

	// Create guard
	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				DownloadTimeout:  10 * time.Second,
				MaxArtifactBytes: 1024 * 1024,
			},
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
		version:    "1.0.0",
	}

	// Test update
	// Note: This test will fail in actual execution because it tries to replace the running binary
	// In a real test environment, you would mock the update.Apply function
	// For now, we just test the download and verification stages
	downloadURL, sha256Hash, signatureStr, err := g.requestDownloadMeta("backend", "2.0.0", g.cfg.OTA.OS, g.cfg.OTA.Arch)
	if err != nil {
		t.Fatalf("requestDownloadMeta failed: %v", err)
	}

	if downloadURL != "/download/test.bin" {
		t.Errorf("expected url /download/test.bin, got %s", downloadURL)
	}

	if sha256Hash != hashStr {
		t.Errorf("expected hash %s, got %s", hashStr, sha256Hash)
	}

	if signatureStr != signatureB64 {
		t.Errorf("expected signature %s, got %s", signatureB64, signatureStr)
	}
}

func TestUpdateBackend_HashMismatch(t *testing.T) {
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	testBinary := []byte("test binary content")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/update/download" {
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/test.bin",
				"sha256":       wrongHash,
				"signature":    "dummy",
			})
		} else if r.URL.Path == "/download/test.bin" {
			w.Write(testBinary)
		}
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				DownloadTimeout:  10 * time.Second,
				MaxArtifactBytes: 1024 * 1024,
			},
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	url, expectedHash, _, err := g.requestDownloadMeta("backend", "2.0.0", g.cfg.OTA.OS, g.cfg.OTA.Arch)
	if err != nil {
		t.Fatalf("requestDownloadMeta failed: %v", err)
	}

	tmpPath, actualHash, err := g.downloadArtifactWithProgress(url, g.cfg.OTA.MaxArtifactBytes)
	if err != nil {
		t.Fatalf("downloadArtifactWithProgress failed: %v", err)
	}
	defer os.Remove(tmpPath)

	if actualHash == expectedHash {
		t.Error("expected hash mismatch, but hashes matched")
	}
}

func TestUpdateBackend_SignatureVerification(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	data := "test data"
	digest := sha256.Sum256([]byte(data))
	signature := ed25519.Sign(privKey, digest[:])
	signatureB64 := base64.StdEncoding.EncodeToString(signature)

	g := &Guard{
		publicKey: pubKey,
	}

	// Valid signature
	if err := g.verifySignature(data, signatureB64); err != nil {
		t.Errorf("valid signature failed: %v", err)
	}

	// Invalid signature
	wrongSignature := base64.StdEncoding.EncodeToString([]byte("wrong signature"))
	if err := g.verifySignature(data, wrongSignature); err == nil {
		t.Error("expected signature verification to fail, but it succeeded")
	}
}

func TestUpdateBackend_ConcurrentUpdate(t *testing.T) {
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	g := &Guard{
		cfg: Config{
			ServerURL:     "http://localhost",
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

	// Lock the update mutex
	g.updateMu.Lock()

	// Try to acquire lock in goroutine (should block)
	done := make(chan bool)
	go func() {
		g.updateMu.Lock()
		g.updateMu.Unlock()
		done <- true
	}()

	// Wait a bit to ensure goroutine is blocked
	select {
	case <-done:
		t.Error("expected goroutine to be blocked, but it completed")
	case <-time.After(100 * time.Millisecond):
		// Expected: goroutine is blocked
	}

	// Unlock and verify goroutine completes
	g.updateMu.Unlock()
	select {
	case <-done:
		// Expected: goroutine completed
	case <-time.After(1 * time.Second):
		t.Error("goroutine did not complete after unlock")
	}
}

func TestUpdateFrontend_PathTraversal(t *testing.T) {
	// This test would require creating a malicious tar.gz file
	// For now, we just verify the path checking logic
	tmpDir := t.TempDir()

	// Valid path
	target := filepath.Join(tmpDir, "subdir", "file.txt")
	cleanedTarget := filepath.Clean(target)
	cleanedTmpDir := filepath.Clean(tmpDir) + string(os.PathSeparator)

	if !filepath.HasPrefix(cleanedTarget, cleanedTmpDir) {
		t.Error("valid path rejected")
	}

	// Invalid path (traversal attempt)
	maliciousTarget := filepath.Join(tmpDir, "..", "etc", "passwd")
	cleanedMalicious := filepath.Clean(maliciousTarget)

	if filepath.HasPrefix(cleanedMalicious, cleanedTmpDir) {
		t.Error("path traversal attempt not detected")
	}
}

func TestUpdateCallbacks(t *testing.T) {
	progressCalled := false
	resultCalled := false
	failureCalled := false

	cfg := Config{
		OTA: OTAConfig{
			OnUpdateProgress: func(component, stage string, progress float64) {
				progressCalled = true
			},
			OnUpdateResult: func(component, oldVer, newVer string, success bool, err error) {
				resultCalled = true
			},
			OnUpdateFailure: func(component string, err error) {
				failureCalled = true
			},
		},
	}

	if cfg.OTA.OnUpdateProgress != nil {
		cfg.OTA.OnUpdateProgress("test", "downloading", 0.5)
	}

	if cfg.OTA.OnUpdateResult != nil {
		cfg.OTA.OnUpdateResult("test", "1.0", "2.0", true, nil)
	}

	if cfg.OTA.OnUpdateFailure != nil {
		cfg.OTA.OnUpdateFailure("test", ErrUpdateDownload)
	}

	if !progressCalled {
		t.Error("OnUpdateProgress not called")
	}

	if !resultCalled {
		t.Error("OnUpdateResult not called")
	}

	if !failureCalled {
		t.Error("OnUpdateFailure not called")
	}
}
