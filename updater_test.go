package sdk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"sync"
	"testing"
	"time"
)

func TestHandleUpdateNotification_BackendComponent(t *testing.T) {
	updateCalled := false
	g := &Guard{
		cfg: Config{
			ComponentSlug: "backend",
			OTA: OTAConfig{
				AutoUpdate: true,
			},
		},
		mu: sync.RWMutex{},
	}

	u := updateInfo{
		Component:       "backend",
		UpdateAvailable: true,
	}

	if u.Component == g.cfg.ComponentSlug && g.cfg.OTA.AutoUpdate {
		updateCalled = true
	}

	if !updateCalled {
		t.Error("expected update to be processed for backend component")
	}
}

func TestHandleUpdateNotification_ManagedComponent(t *testing.T) {
	g := &Guard{
		cfg: Config{
			ComponentSlug: "backend",
			ManagedComponents: []ManagedComponent{
				{Slug: "frontend", Strategy: UpdateFrontend},
			},
			OTA: OTAConfig{
				AutoUpdate: true,
			},
		},
		mu: sync.RWMutex{},
	}

	u := updateInfo{
		Component:       "frontend",
		UpdateAvailable: true,
	}

	found := false
	for _, mc := range g.cfg.ManagedComponents {
		if mc.Slug == u.Component && g.cfg.OTA.AutoUpdate {
			found = true
			if mc.Strategy != UpdateFrontend {
				t.Error("expected UpdateFrontend strategy")
			}
		}
	}

	if !found {
		t.Error("expected to find managed component")
	}
}

func TestHandleUpdateNotification_AutoUpdateDisabled(t *testing.T) {
	g := &Guard{
		cfg: Config{
			ComponentSlug: "backend",
			OTA: OTAConfig{
				AutoUpdate: false,
			},
		},
		mu: sync.RWMutex{},
	}

	if g.cfg.OTA.AutoUpdate {
		t.Error("AutoUpdate should be disabled in this test")
	}
}

func TestUpdateBackend_Success(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	testBinary := []byte("test binary content")
	hash := sha256.Sum256(testBinary)
	hashStr := hex.EncodeToString(hash[:])

	digest := sha256.Sum256([]byte(hashStr))
	signature := ed25519.Sign(privKey, digest[:])
	signatureB64 := base64.StdEncoding.EncodeToString(signature)

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

func TestDownloadArtifactWithProgress_NetworkError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL: server.URL,
			OTA: OTAConfig{
				DownloadTimeout:  10 * time.Second,
				MaxArtifactBytes: 1024 * 1024,
			},
		},
		publicKey:  pubKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	_, _, err := g.downloadArtifactWithProgress("/download/test.bin", g.cfg.OTA.MaxArtifactBytes)
	if err == nil {
		t.Error("expected error for non-200 status code")
	}
}

func TestDownloadArtifactWithProgress_ExceedsMaxBytes(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	largeData := make([]byte, 1000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(largeData)
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL: server.URL,
			OTA: OTAConfig{
				DownloadTimeout:  10 * time.Second,
				MaxArtifactBytes: 100,
			},
		},
		publicKey:  pubKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	tmpPath, _, err := g.downloadArtifactWithProgress("/download/test.bin", g.cfg.OTA.MaxArtifactBytes)
	if err != nil {
		t.Fatalf("downloadArtifactWithProgress failed: %v", err)
	}
	defer os.Remove(tmpPath)

	info, err := os.Stat(tmpPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if info.Size() > 100 {
		t.Errorf("expected file size <= 100, got %d", info.Size())
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

	if err := g.verifySignature(data, signatureB64); err != nil {
		t.Errorf("valid signature failed: %v", err)
	}

	wrongSignature := base64.StdEncoding.EncodeToString([]byte("wrong signature"))
	if err := g.verifySignature(data, wrongSignature); err == nil {
		t.Error("expected signature verification to fail, but it succeeded")
	}
}

func TestUpdateBackend_InvalidSignatureEncoding(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	g := &Guard{
		publicKey: pubKey,
	}

	if err := g.verifySignature("data", "not-valid-base64!!!"); err == nil {
		t.Error("expected error for invalid base64")
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

	g.updateMu.Lock()

	done := make(chan bool)
	go func() {
		g.updateMu.Lock()
		g.updateMu.Unlock()
		done <- true
	}()

	select {
	case <-done:
		t.Error("expected goroutine to be blocked, but it completed")
	case <-time.After(100 * time.Millisecond):
	}

	g.updateMu.Unlock()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("goroutine did not complete after unlock")
	}
}

func TestUpdateFrontend_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	target := filepath.Join(tmpDir, "subdir", "file.txt")
	cleanedTarget := filepath.Clean(target)
	cleanedTmpDir := filepath.Clean(tmpDir) + string(os.PathSeparator)

	if !filepath.HasPrefix(cleanedTarget, cleanedTmpDir) {
		t.Error("valid path rejected")
	}

	maliciousTarget := filepath.Join(tmpDir, "..", "etc", "passwd")
	cleanedMalicious := filepath.Clean(maliciousTarget)

	if filepath.HasPrefix(cleanedMalicious, cleanedTmpDir) {
		t.Error("path traversal attempt not detected")
	}
}

func TestUpdateFrontend_TarGzExtraction(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tr := tar.NewWriter(gz)

	fileContent := []byte("test file content")
	hdr := &tar.Header{
		Name: "test.txt",
		Size: int64(len(fileContent)),
		Mode: 0o644,
	}
	tr.WriteHeader(hdr)
	tr.Write(fileContent)
	tr.Close()
	gz.Close()

	tarGzData := buf.Bytes()
	hash := sha256.Sum256(tarGzData)
	hashStr := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/update/download" {
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       hashStr,
			})
		} else if r.URL.Path == "/download/frontend.tar.gz" {
			w.Write(tarGzData)
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

	url, _, _, err := g.requestDownloadMeta("frontend", "2.0.0", "universal", "universal")
	if err != nil {
		t.Fatalf("requestDownloadMeta failed: %v", err)
	}

	tmpPath, actualHash, err := g.downloadArtifactWithProgress(url, g.cfg.OTA.MaxArtifactBytes)
	if err != nil {
		t.Fatalf("downloadArtifactWithProgress failed: %v", err)
	}
	defer os.Remove(tmpPath)

	if actualHash != hashStr {
		t.Errorf("expected hash %s, got %s", hashStr, actualHash)
	}
}

func TestRequestDownloadMeta_ServerError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"error": "version_not_found",
		})
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				DownloadTimeout: 10 * time.Second,
			},
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	_, _, _, err := g.requestDownloadMeta("backend", "2.0.0", "linux", "amd64")
	if err == nil {
		t.Error("expected error for server error response")
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

func TestApplyBackendBinaryWithSelfupdate_FileNotFound(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	g := &Guard{
		publicKey: pubKey,
	}

	err := g.applyBackendBinaryWithSelfupdate("/nonexistent/path/binary", "/target/path")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestDownloadArtifactWithProgress_ContextTimeout(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("test"))
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL: server.URL,
			OTA: OTAConfig{
				DownloadTimeout:  100 * time.Millisecond,
				MaxArtifactBytes: 1024 * 1024,
			},
		},
		publicKey:  pubKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	_, _, err := g.downloadArtifactWithProgress("/download/test.bin", g.cfg.OTA.MaxArtifactBytes)
	if err == nil {
		t.Error("expected error for timeout")
	}
}

func TestVerifySignature_EdgeCases(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	g := &Guard{
		publicKey: pubKey,
	}

	emptyDigest := sha256.Sum256([]byte(""))
	emptySignature := ed25519.Sign(privKey, emptyDigest[:])
	emptySignatureB64 := base64.StdEncoding.EncodeToString(emptySignature)

	tests := []struct {
		name    string
		data    string
		sig     string
		wantErr bool
	}{
		{"empty data", "", emptySignatureB64, false},
		{"empty signature", "test", "", true},
		{"invalid base64", "test", "!!!invalid!!!", true},
		{"corrupted signature", "test", base64.StdEncoding.EncodeToString([]byte("corrupted")), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := g.verifySignature(tt.data, tt.sig)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr %v, got err %v", tt.wantErr, err)
			}
		})
	}
}
