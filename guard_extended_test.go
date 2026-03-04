package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestStart_Success tests successful license verification and heartbeat startup
func TestStart_Success(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/verify" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":     "ok",
				"public_data": "test-data",
				"signature":  encodeSignature(privKey, "test-data"),
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := g.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if g.sm.Current() != StateActive {
		t.Errorf("expected StateActive, got %v", g.sm.Current())
	}

	server.Close()
}

// TestStart_LicenseVerificationFailed tests Start with license verification failure
func TestStart_LicenseVerificationFailed(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	// Mock server returning error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "license_not_found",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "invalid-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := g.Start(ctx)
	if err == nil {
		t.Error("expected error, got nil")
	}

	server.Close()
}

// TestStart_NetworkError tests Start with network failure
func TestStart_NetworkError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://invalid-server-that-does-not-exist.local",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := g.Start(ctx)
	if err == nil {
		t.Error("expected error, got nil")
	}

	if g.sm.Current() != StateInit {
		t.Errorf("expected StateInit after failed verification, got %v", g.sm.Current())
	}
}

// TestStop_CancelsContext tests Stop cancels the heartbeat context
func TestStop_CancelsContext(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "ok",
			"public_data": "test-data",
			"signature":   encodeSignature(privKey, "test-data"),
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)
	ctx := context.Background()

	g.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	g.Stop()
	time.Sleep(100 * time.Millisecond)

	if g.cancel == nil {
		t.Error("expected cancel function to be set")
	}

	server.Close()
}

// TestStop_NilCancel tests Stop with nil cancel function
func TestStop_NilCancel(t *testing.T) {
	g := &Guard{cancel: nil}
	g.Stop() // Should not panic
}

// TestAutoResolveVersion_Success tests successful version resolution
func TestAutoResolveVersion_Success(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version/resolve" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"version":    "1.2.3",
				"git_commit": "abc123",
				"build_time": "2026-02-23T10:00:00Z",
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

	err := g.AutoResolveVersion()
	if err != nil {
		t.Fatalf("AutoResolveVersion failed: %v", err)
	}

	if g.version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", g.version)
	}

	server.Close()
}

// TestAutoResolveVersion_ServerError tests version resolution with server error
func TestAutoResolveVersion_ServerError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "binary_hash_not_found",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.AutoResolveVersion()
	if err == nil {
		t.Error("expected error, got nil")
	}

	server.Close()
}

// TestAutoResolveVersion_NetworkError tests version resolution with network failure
func TestAutoResolveVersion_NetworkError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://invalid-server.local",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	err := g.AutoResolveVersion()
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestPostJSON_Success tests successful JSON POST request
func TestPostJSON_Success(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"message": "success",
		})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	var result map[string]interface{}
	err := g.postJSON(context.Background(), "/api/v1/test", map[string]string{"key": "value"}, &result)
	if err != nil {
		t.Fatalf("postJSON failed: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}

	server.Close()
}

// TestPostJSON_InvalidStatusCode tests postJSON with non-200 status
func TestPostJSON_InvalidStatusCode(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	var result map[string]interface{}
	err := g.postJSON(context.Background(), "/api/v1/test", map[string]string{}, &result)
	if err == nil {
		t.Error("expected error, got nil")
	}

	server.Close()
}

// TestPostJSON_InvalidJSON tests postJSON with invalid JSON response
func TestPostJSON_InvalidJSON(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	var result map[string]interface{}
	err := g.postJSON(context.Background(), "/api/v1/test", map[string]string{}, &result)
	if err == nil {
		t.Error("expected error, got nil")
	}

	server.Close()
}

// TestPostJSON_MarshalError tests postJSON with unmarshalable request body
func TestPostJSON_MarshalError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "http://localhost:9999",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	// Use a channel which cannot be marshaled to JSON
	unmarshalable := make(chan int)
	var result map[string]interface{}
	err := g.postJSON(context.Background(), "/api/v1/test", unmarshalable, &result)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestPostJSON_ContextTimeout tests postJSON with context timeout
func TestPostJSON_ContextTimeout(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}))

	cfg := Config{
		ServerURL:     server.URL,
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, _ := New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var result map[string]interface{}
	err := g.postJSON(ctx, "/api/v1/test", map[string]string{}, &result)
	if err == nil {
		t.Error("expected error, got nil")
	}

	server.Close()
}

// Helper function to encode signature
func encodeSignature(privKey ed25519.PrivateKey, data string) string {
	sig := ed25519.Sign(privKey, []byte(data))
	return base64.StdEncoding.EncodeToString(sig)
}
