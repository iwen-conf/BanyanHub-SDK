package sdk

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"log/slog"
	"testing"
)

func TestNew_Success(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	cfg := Config{
		ServerURL:     "https://api.example.com",
		LicenseKey:    "test-key",
		PublicKeyPEM:  pubKeyPEM,
		ProjectSlug:   "test-project",
		ComponentSlug: "backend",
	}

	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if g.cfg.ServerURL != cfg.ServerURL {
		t.Errorf("expected ServerURL %s, got %s", cfg.ServerURL, g.cfg.ServerURL)
	}

	if g.logger == nil {
		t.Error("expected logger to be initialized")
	}
}

func TestNew_MissingParameters(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyPEM := pemEncodePublicKey(pubKey)

	tests := []struct {
		name        string
		cfg         Config
		expectedErr string
	}{
		{
			"missing server URL",
			Config{LicenseKey: "key", PublicKeyPEM: pubKeyPEM, ProjectSlug: "proj", ComponentSlug: "comp"},
			"server_url is required",
		},
		{
			"missing license key",
			Config{ServerURL: "url", PublicKeyPEM: pubKeyPEM, ProjectSlug: "proj", ComponentSlug: "comp"},
			"license_key is required",
		},
		{
			"missing public key",
			Config{ServerURL: "url", LicenseKey: "key", ProjectSlug: "proj", ComponentSlug: "comp"},
			"public_key_pem is required",
		},
		{
			"missing project slug",
			Config{ServerURL: "url", LicenseKey: "key", PublicKeyPEM: pubKeyPEM, ComponentSlug: "comp"},
			"project_slug is required",
		},
		{
			"missing component slug",
			Config{ServerURL: "url", LicenseKey: "key", PublicKeyPEM: pubKeyPEM, ProjectSlug: "proj"},
			"component_slug is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil {
				t.Error("expected error, got nil")
			}
			if err.Error() != tt.expectedErr {
				t.Errorf("expected error %q, got %q", tt.expectedErr, err.Error())
			}
		})
	}
}

func TestCheck_States(t *testing.T) {
	g := &Guard{
		sm: newStateMachine(),
	}

	// Init state
	if err := g.Check(); err != ErrNotActivated {
		t.Errorf("expected ErrNotActivated in Init state, got %v", err)
	}

	// Active state
	g.sm.OnVerifySuccess()
	if err := g.Check(); err != nil {
		t.Errorf("expected nil in Active state, got %v", err)
	}

	// Grace state
	g.sm.OnHeartbeatFail()
	if err := g.Check(); err != nil {
		t.Errorf("expected nil in Grace state, got %v", err)
	}

	// Locked state
	g.sm.OnGracePeriodExpired()
	if err := g.Check(); err != ErrLocked {
		t.Errorf("expected ErrLocked in Locked state, got %v", err)
	}

	// Banned state
	g.sm.OnKill()
	if err := g.Check(); err != ErrBanned {
		t.Errorf("expected ErrBanned in Banned state, got %v", err)
	}
}

func TestSetLogger(t *testing.T) {
	g := &Guard{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	customLogger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	g.SetLogger(customLogger)

	if g.logger != customLogger {
		t.Error("logger not updated")
	}

	// Test nil logger (should not update)
	oldLogger := g.logger
	g.SetLogger(nil)
	if g.logger != oldLogger {
		t.Error("logger should not be updated with nil")
	}
}

func TestSetVersion(t *testing.T) {
	g := &Guard{
		version: "1.0.0",
	}

	g.SetVersion("2.0.0")

	if g.version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", g.version)
	}
}

func TestSetManagedVersion(t *testing.T) {
	g := &Guard{
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
	}

	g.SetManagedVersion("frontend", "2.0.0")

	if g.managedVersions["frontend"] != "2.0.0" {
		t.Errorf("expected frontend version 2.0.0, got %s", g.managedVersions["frontend"])
	}
}

// Helper function to encode public key to PEM
func pemEncodePublicKey(pubKey ed25519.PublicKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKey,
	})
}
