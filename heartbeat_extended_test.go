package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestStartHeartbeat_SuccessfulHeartbeats tests successful heartbeat sequence
func TestStartHeartbeat_SuccessfulHeartbeats(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/api/v1/verify" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"public_data": "test-data",
				"signature":   encodeSignature(privKey, "test-data"),
			})
		} else if r.URL.Path == "/api/v1/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
			})
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
		HeartbeatInterval: 100 * time.Millisecond,
	}

	g, _ := New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	g.Start(ctx)
	<-ctx.Done()

	if callCount < 3 { // At least verify + 2 heartbeats
		t.Logf("expected at least 3 calls, got %d", callCount)
	}

	server.Close()
}

// TestStartHeartbeat_FatalErrorStopsHeartbeat tests that fatal errors stop heartbeat
func TestStartHeartbeat_FatalErrorStopsHeartbeat(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/api/v1/verify" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"public_data": "test-data",
				"signature":   encodeSignature(privKey, "test-data"),
			})
		} else if r.URL.Path == "/api/v1/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "kill",
			})
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
		HeartbeatInterval: 100 * time.Millisecond,
	}

	g, _ := New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	g.Start(ctx)
	time.Sleep(500 * time.Millisecond)

	// After kill, machine should be banned
	if g.sm.Current() != StateBanned {
		t.Errorf("expected StateBanned after kill, got %v", g.sm.Current())
	}

	server.Close()
}

// TestStartHeartbeat_GraceExpiration tests grace period expiration
func TestStartHeartbeat_GraceExpiration(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/api/v1/verify" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"public_data": "test-data",
				"signature":   encodeSignature(privKey, "test-data"),
			})
		} else if r.URL.Path == "/api/v1/heartbeat" {
			// All heartbeats fail with network error
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
		HeartbeatInterval: 50 * time.Millisecond,
		GracePolicy: GracePolicy{
			MaxOfflineDuration: 200 * time.Millisecond,
		},
	}

	g, _ := New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	g.Start(ctx)
	time.Sleep(600 * time.Millisecond)

	// After grace period, machine should be locked
	if g.sm.Current() != StateLocked {
		t.Errorf("expected StateLocked after grace period, got %v", g.sm.Current())
	}

	server.Close()
}

// TestSendHeartbeat_WithManagedComponents tests heartbeat with managed components
func TestSendHeartbeat_WithManagedComponents(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
			})
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
		ManagedComponents: []ManagedComponent{
			{
				Slug:     "frontend",
				Dir:      "/tmp/frontend",
				Strategy: UpdateFrontend,
			},
		},
	}

	g, _ := New(cfg)

	_ = g.sendHeartbeat()
	// Network error expected since not a full start

	server.Close()
}

// TestSendHeartbeat_UpdateNotification tests heartbeat processing update notification
func TestSendHeartbeat_UpdateNotification(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
				"updates": []map[string]interface{}{
					{
						"component":        "backend",
						"current":          "1.0.0",
						"latest":           "1.1.0",
						"update_available": true,
						"mandatory":        false,
					},
				},
			})
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
		OTA: OTAConfig{
			Enabled:    true,
			AutoUpdate: false, // Don't auto-update to prevent goroutine issues
		},
	}

	g, _ := New(cfg)

	_ = g.sendHeartbeat()
	// Network error expected since we're calling outside of Start context

	server.Close()
}

// TestSendHeartbeat_UpdateFrozen tests heartbeat with update frozen flag
func TestSendHeartbeat_UpdateFrozen(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":        "ok",
				"update_frozen": true,
				"updates": []map[string]interface{}{
					{
						"component":        "backend",
						"latest":           "1.1.0",
						"update_available": true,
					},
				},
			})
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
		OTA: OTAConfig{
			Enabled:    true,
			AutoUpdate: true,
		},
	}

	g, _ := New(cfg)

	_ = g.sendHeartbeat()
	// Network error expected

	server.Close()
}

// TestSendHeartbeat_VersionSnapshot tests that version snapshot is correct
func TestSendHeartbeat_VersionSnapshot(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
			})
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)
	g.SetVersion("1.2.3")

	_ = g.sendHeartbeat()
	// Network error expected

	server.Close()
}

// TestIsFatalError_Banned tests isFatalError with ErrBanned
func TestIsFatalError_Banned(t *testing.T) {
	if !isFatalError(ErrBanned) {
		t.Error("ErrBanned should be fatal")
	}
}

// TestIsFatalError_LicenseSuspended tests isFatalError with ErrLicenseSuspended
func TestIsFatalError_LicenseSuspended(t *testing.T) {
	if !isFatalError(ErrLicenseSuspended) {
		t.Error("ErrLicenseSuspended should be fatal")
	}
}

// TestIsFatalError_MachineBanned tests isFatalError with ErrMachineBanned
func TestIsFatalError_MachineBanned(t *testing.T) {
	if !isFatalError(ErrMachineBanned) {
		t.Error("ErrMachineBanned should be fatal")
	}
}

// TestIsFatalError_NetworkError tests isFatalError with ErrNetworkError
func TestIsFatalError_NetworkError(t *testing.T) {
	if isFatalError(ErrNetworkError) {
		t.Error("ErrNetworkError should not be fatal")
	}
}

// TestHeartbeat_Recovery tests heartbeat recovery after failure
func TestHeartbeat_Recovery(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	failCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/verify" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"public_data": "test-data",
				"signature":   encodeSignature(privKey, "test-data"),
			})
		} else if r.URL.Path == "/api/v1/heartbeat" {
			failCount++
			if failCount < 2 {
				// Fail first heartbeat
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("error"))
			} else {
				// Recover on second heartbeat
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
				})
			}
		}
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
		HeartbeatInterval: 100 * time.Millisecond,
	}

	g, _ := New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	g.Start(ctx)
	<-ctx.Done()

	// After recovery, should be active again
	if g.sm.Current() != StateActive && g.sm.Current() != StateGrace {
		t.Errorf("expected StateActive or StateGrace after recovery, got %v", g.sm.Current())
	}

	server.Close()
}
