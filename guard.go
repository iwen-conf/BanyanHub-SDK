package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

type Guard struct {
	cfg         Config
	publicKey   ed25519.PublicKey
	fingerprint *Fingerprint
	sm          *stateMachine
	httpClient  *http.Client

	version         string
	managedVersions map[string]string

	cancel   context.CancelFunc
	mu       sync.RWMutex
	updateMu sync.Mutex
	logger   *slog.Logger
}

func New(cfg Config) (*Guard, error) {
	cfg.setDefaults()

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("server_url is required")
	}
	if cfg.LicenseKey == "" {
		return nil, fmt.Errorf("license_key is required")
	}
	if cfg.PublicKeyPEM == nil {
		return nil, fmt.Errorf("public_key_pem is required")
	}
	if cfg.ProjectSlug == "" {
		return nil, fmt.Errorf("project_slug is required")
	}
	if cfg.ComponentSlug == "" {
		return nil, fmt.Errorf("component_slug is required")
	}

	block, _ := pem.Decode(cfg.PublicKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode public key PEM")
	}
	if len(block.Bytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key size: got %d, want %d", len(block.Bytes), ed25519.PublicKeySize)
	}
	pubKey := ed25519.PublicKey(block.Bytes)

	fp, err := collectFingerprint()
	if err != nil {
		return nil, fmt.Errorf("collect fingerprint: %w", err)
	}

	managedVersions := make(map[string]string, len(cfg.ManagedComponents))
	for _, mc := range cfg.ManagedComponents {
		managedVersions[mc.Slug] = "unknown"
	}

	return &Guard{
		cfg:             cfg,
		publicKey:       pubKey,
		fingerprint:     fp,
		sm:              newStateMachine(),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		version:         "unknown",
		managedVersions: managedVersions,
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, nil
}

func (g *Guard) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	g.cancel = cancel

	if err := g.verifyLicense(); err != nil {
		cancel()
		return fmt.Errorf("license verification failed: %w", err)
	}
	g.sm.OnVerifySuccess()

	g.startHeartbeat(ctx)

	return nil
}

func (g *Guard) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
}

func (g *Guard) Check() error {
	switch g.sm.Current() {
	case StateActive, StateGrace:
		return nil
	case StateLocked:
		return ErrLocked
	case StateBanned:
		return ErrBanned
	case StateInit:
		return ErrNotActivated
	default:
		return ErrNotActivated
	}
}

func (g *Guard) State() State {
	return g.sm.Current()
}

func (g *Guard) SetVersion(v string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.version = v
}

// AutoResolveVersion automatically resolves the version by querying the
// Centralized Release System (中央发版系统).
//
// It calculates the binary hash of the current executable and sends it to
// the central server, which returns the corresponding version information.
// This eliminates the need to manually set version numbers.
//
// The central server maintains a hash-to-version mapping table for all
// released artifacts across all projects, components, and platforms.
//
// Usage:
//
//	guard, _ := sdk.New(cfg)
//	if err := guard.AutoResolveVersion(); err != nil {
//	    // Fallback to default version
//	    guard.SetVersion("unknown")
//	}
//	guard.Start(context.Background())
func (g *Guard) AutoResolveVersion() error {
	// Calculate binary hash
	binaryHash, err := GetBinaryHash()
	if err != nil {
		return fmt.Errorf("calculate binary hash: %w", err)
	}

	// Request version from server
	reqBody := map[string]any{
		"license_key":  g.cfg.LicenseKey,
		"machine_id":   g.fingerprint.MachineID(),
		"project_slug": g.cfg.ProjectSlug,
		"component":    g.cfg.ComponentSlug,
		"binary_hash":  binaryHash,
	}

	var resp struct {
		Version   string `json:"version"`
		GitCommit string `json:"git_commit"`
		BuildTime string `json:"build_time"`
		Error     string `json:"error"`
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := g.postJSON(ctx, "/api/v1/version/resolve", reqBody, &resp); err != nil {
		return fmt.Errorf("request version resolution: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("server error: %s", resp.Error)
	}

	// Update version
	g.mu.Lock()
	g.version = resp.Version
	g.mu.Unlock()

	g.logger.Info("version resolved automatically",
		"version", resp.Version,
		"git_commit", resp.GitCommit,
		"build_time", resp.BuildTime,
		"binary_hash", binaryHash)

	return nil
}

func (g *Guard) SetManagedVersion(slug, version string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.managedVersions[slug] = version
}

func (g *Guard) SetLogger(logger *slog.Logger) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if logger != nil {
		g.logger = logger
	}
}

// postJSON sends a JSON POST request to the server and decodes the response.
func (g *Guard) postJSON(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := g.cfg.ServerURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrInvalidServerResponse, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}

	return nil
}

// getJSON sends a JSON GET request to the server and decodes the response.
func (g *Guard) getJSON(ctx context.Context, path string, query url.Values, result any) error {
	fullURL := g.cfg.ServerURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrInvalidServerResponse, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}

	return nil
}

func randomNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func nowUnix() int64 {
	return time.Now().Unix()
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
