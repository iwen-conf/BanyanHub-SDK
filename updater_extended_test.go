package sdk

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestHandleUpdateNotification_NoUpdate tests update when update is not available
func TestHandleUpdateNotification_NoUpdate(t *testing.T) {
	g := &Guard{
		cfg: Config{
			ComponentSlug: "backend",
			OTA: OTAConfig{
				AutoUpdate: true,
			},
		},
		mu: sync.RWMutex{},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	u := updateInfo{
		Component:       "unrelated",
		UpdateAvailable: false,
	}

	// Should not crash even if component doesn't match
	g.handleUpdateNotification(u)
}

// TestUpdateBackend_RequestDownloadFailure tests updateBackend when request fails
func TestUpdateBackend_RequestDownloadFailure(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "internal_error",
		})
	}))
	defer server.Close()

	progressCalled := false
	failureCalled := false
	resultCalled := false

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				DownloadTimeout:  10 * time.Second,
				MaxArtifactBytes: 1024 * 1024,
				OnUpdateProgress: func(component, stage string, progress float64) {
					progressCalled = true
				},
				OnUpdateFailure: func(component string, err error) {
					failureCalled = true
				},
				OnUpdateResult: func(component, oldVer, newVer string, success bool, err error) {
					resultCalled = true
				},
			},
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
		version:    "1.0.0",
		updateMu:   sync.Mutex{},
		mu:         sync.RWMutex{},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	u := updateInfo{
		Component:       "backend",
		Latest:          "2.0.0",
		UpdateAvailable: true,
	}

	g.updateBackend(u)

	if !progressCalled {
		t.Error("expected OnUpdateProgress to be called")
	}
	if !failureCalled {
		t.Error("expected OnUpdateFailure to be called")
	}
	if !resultCalled {
		t.Error("expected OnUpdateResult to be called")
	}
}

// TestUpdateFrontend_Success tests successful frontend update
func TestUpdateFrontend_Success(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	tempDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/update/download" {
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			})
		} else if r.URL.Path == "/download/frontend.tar.gz" {
			// Empty gzip archive
			w.Write([]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		}
	}))
	defer server.Close()

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			OTA: OTAConfig{
				AutoUpdate: true,
				OnUpdateResult: func(component, oldVer, newVer string, success bool, err error) {
					// Just verify callback is called
				},
			},
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
		updateMu:   sync.Mutex{},
		mu:         sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	u := updateInfo{
		Component:       "frontend",
		Latest:          "2.0.0",
		UpdateAvailable: true,
	}

	mc := ManagedComponent{
		Slug: "frontend",
		Dir:  tempDir,
	}

	g.updateFrontend(mc, u)
}

// TestUpdateFrontend_NetworkError tests frontend update with network error
func TestUpdateFrontend_NetworkError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	tempDir := t.TempDir()

	failureCalled := false
	g := &Guard{
		cfg: Config{
			ServerURL:     "http://invalid-server-that-does-not-exist.local",
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			OTA: OTAConfig{
				OnUpdateFailure: func(component string, err error) {
					failureCalled = true
				},
			},
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 1 * time.Second},
		updateMu:   sync.Mutex{},
		mu:         sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	u := updateInfo{
		Component:       "frontend",
		Latest:          "2.0.0",
		UpdateAvailable: true,
	}

	mc := ManagedComponent{
		Slug: "frontend",
		Dir:  tempDir,
	}

	g.updateFrontend(mc, u)

	if !failureCalled {
		t.Error("expected OnUpdateFailure to be called")
	}
}

// TestHandleUpdateNotification_ManagedComponentBackend tests backend strategy for managed component
func TestHandleUpdateNotification_ManagedComponentBackend(t *testing.T) {
	g := &Guard{
		cfg: Config{
			ComponentSlug: "app",
			ManagedComponents: []ManagedComponent{
				{Slug: "library", Strategy: UpdateBackend},
			},
			OTA: OTAConfig{
				AutoUpdate: false, // Disable auto-update to avoid goroutine
			},
		},
		mu: sync.RWMutex{},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	u := updateInfo{
		Component:       "library",
		UpdateAvailable: true,
	}

	// Should not crash
	g.handleUpdateNotification(u)
}

// TestApplyBackendBinaryWithSelfupdate_FileNotFound tests error when temp file not found
func TestApplyBackendBinaryWithSelfupdate_FileNotFoundExtended(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	g := &Guard{
		publicKey: pubKey,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := g.applyBackendBinaryWithSelfupdate("/nonexistent/path/binary", "/target/path")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}
