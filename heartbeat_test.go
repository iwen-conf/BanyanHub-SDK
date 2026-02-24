package sdk

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSendHeartbeat_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/heartbeat" {
			json.NewEncoder(w).Encode(heartbeatResponse{
				Status:     "ok",
				ServerTime: time.Now().Format(time.RFC3339),
			})
		}
	}))
	defer server.Close()

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

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
		sm:         newStateMachine(),
		version:    "1.0.0",
		managedVersions: map[string]string{},
	}

	g.sm.OnVerifySuccess()

	if err := g.sendHeartbeat(); err != nil {
		t.Errorf("sendHeartbeat failed: %v", err)
	}

	if g.sm.Current() != StateActive {
		t.Errorf("expected state Active, got %v", g.sm.Current())
	}
}

func TestSendHeartbeat_NetworkError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	g := &Guard{
		cfg: Config{
			ServerURL:     "http://invalid-server-that-does-not-exist",
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
		},
		publicKey: pubKey,
		fingerprint: &Fingerprint{
			machineID: "test-machine",
		},
		httpClient: &http.Client{Timeout: 1 * time.Second},
		sm:         newStateMachine(),
		version:    "1.0.0",
		managedVersions: map[string]string{},
	}

	g.sm.OnVerifySuccess()

	if err := g.sendHeartbeat(); err == nil {
		t.Error("expected sendHeartbeat to fail, but it succeeded")
	}
}

func TestSendHeartbeat_KillCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(heartbeatResponse{
			Status: "kill",
			Reason: "banned by admin",
		})
	}))
	defer server.Close()

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

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
		sm:         newStateMachine(),
		version:    "1.0.0",
		managedVersions: map[string]string{},
	}

	g.sm.OnVerifySuccess()

	err := g.sendHeartbeat()
	if err != ErrBanned {
		t.Errorf("expected ErrBanned, got %v", err)
	}

	if g.sm.Current() != StateBanned {
		t.Errorf("expected state Banned, got %v", g.sm.Current())
	}
}

func TestHeartbeat_VersionSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(heartbeatResponse{
			Status: "ok",
		})
	}))
	defer server.Close()

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

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
		sm:         newStateMachine(),
		version:    "1.0.0",
		managedVersions: map[string]string{
			"frontend": "2.0.0",
		},
	}

	g.sm.OnVerifySuccess()

	// Concurrent version update while heartbeat is running
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			g.SetVersion("1.0.1")
			g.SetManagedVersion("frontend", "2.0.1")
		}
		done <- true
	}()

	// Send heartbeat concurrently
	for i := 0; i < 10; i++ {
		g.sendHeartbeat()
	}

	<-done
	// If no race condition, test passes
}



func TestSendHeartbeat_WithUpdateNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/heartbeat" {
			json.NewEncoder(w).Encode(heartbeatResponse{
				Status:     "ok",
				ServerTime: time.Now().Format(time.RFC3339),
				Updates: []updateInfo{
					{
						Component: "backend",
						Latest:    "2.0.0",
					},
				},
			})
		}
	}))
	defer server.Close()

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

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
		sm:         newStateMachine(),
		version:    "1.0.0",
		managedVersions: map[string]string{},
	}

	g.sm.OnVerifySuccess()

	if err := g.sendHeartbeat(); err != nil {
		t.Errorf("sendHeartbeat failed: %v", err)
	}
}
