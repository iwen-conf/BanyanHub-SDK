package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Guard struct {
	cfg         Config
	publicKey   ed25519.PublicKey
	publicKeys  []ed25519.PublicKey
	fingerprint *Fingerprint
	sm          *stateMachine
	httpClient  *http.Client
	store       *persistentStateStore

	version         string
	managedVersions map[string]string

	cancel        context.CancelFunc
	heartbeatDone chan struct{}
	mu            sync.RWMutex
	updateMu      sync.Mutex
	lifecycleMu   sync.Mutex
	running       bool
	logger        *slog.Logger
}

func New(cfg Config) (*Guard, error) {
	cfg.setDefaults()

	// After setDefaults(), ServerURL is guaranteed to have a value
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
	pubKeys, err := decodePublicKeys(cfg.PublicKeyPEM, cfg.LegacyPublicKeysPEM)
	if err != nil {
		return nil, err
	}

	fp, err := collectFingerprint()
	if err != nil {
		return nil, fmt.Errorf("collect fingerprint: %w", err)
	}

	httpClient, err := newPinnedHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	store := newPersistentStateStore(cfg, fp)
	loadedState, err := store.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		loadedState = &persistedState{
			LockFlag:  true,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		_ = store.Save(loadedState)
		err = nil
	}

	managedVersions := make(map[string]string, len(cfg.ManagedComponents))
	for _, mc := range cfg.ManagedComponents {
		managedVersions[mc.Slug] = "unknown"
	}

	sm := newStateMachine()
	if loadedState != nil {
		sm.restore(loadedState)
	}

	return &Guard{
		cfg:             cfg,
		publicKey:       pubKeys[0],
		publicKeys:      pubKeys,
		fingerprint:     fp,
		sm:              sm,
		httpClient:      httpClient,
		store:           store,
		version:         "unknown",
		managedVersions: managedVersions,
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, nil
}

func (g *Guard) Start(ctx context.Context) error {
	g.lifecycleMu.Lock()
	defer g.lifecycleMu.Unlock()

	if g.running {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)

	if err := g.verifyLicense(ctx); err != nil {
		cancel()
		return fmt.Errorf("license verification failed: %w", err)
	}

	done := make(chan struct{})
	g.cancel = cancel
	g.heartbeatDone = done
	g.running = true
	g.startHeartbeat(ctx, done)

	return nil
}

func (g *Guard) Stop() {
	g.lifecycleMu.Lock()
	if !g.running {
		g.lifecycleMu.Unlock()
		return
	}

	cancel := g.cancel
	done := g.heartbeatDone
	g.running = false
	g.cancel = nil
	g.heartbeatDone = nil
	g.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (g *Guard) finishHeartbeat(done chan struct{}) {
	close(done)

	g.lifecycleMu.Lock()
	defer g.lifecycleMu.Unlock()
	if g.heartbeatDone == done {
		g.running = false
		g.cancel = nil
		g.heartbeatDone = nil
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

func (g *Guard) Unseal(box []byte) ([]byte, error) {
	leaseState, err := g.currentActiveLease()
	if err != nil {
		return nil, err
	}

	aead, err := newLeaseAEAD(leaseState.LeaseSignature, leaseState.Lease)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHardBindingUnavailable, err)
	}

	nonceSize := aead.NonceSize()
	if len(box) < nonceSize {
		return nil, ErrHardBindingUnavailable
	}

	nonce := box[:nonceSize]
	ciphertext := box[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, leaseAAD(leaseState.Lease))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHardBindingUnavailable, err)
	}
	return plaintext, nil
}

func (g *Guard) FeatureToken(name string) (string, error) {
	leaseState, err := g.currentActiveLease()
	if err != nil {
		return "", err
	}

	token, err := deriveFeatureToken(leaseState.LeaseSignature, leaseState.Lease, name)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrHardBindingUnavailable, err)
	}
	return token, nil
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

func (g *Guard) currentLeaseState() *persistedState {
	if g.store == nil {
		return nil
	}
	return g.store.Snapshot()
}

func (g *Guard) currentActiveLease() (*persistedState, error) {
	if state := g.currentLeaseState(); state != nil {
		switch g.sm.Current() {
		case StateActive, StateGrace:
			if state.Lease != nil && state.LeaseSignature != "" {
				return state, nil
			}
		}
	}
	return nil, ErrLeaseUnavailable
}

func (g *Guard) verificationKeys() []ed25519.PublicKey {
	if len(g.publicKeys) > 0 {
		return g.publicKeys
	}
	if len(g.publicKey) > 0 {
		return []ed25519.PublicKey{g.publicKey}
	}
	return nil
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
	req.Header.Set("User-Agent", "BanyanHub-SDK/"+Version)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
	req.Header.Set("User-Agent", "BanyanHub-SDK/"+Version)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func decodePublicKeys(primary []byte, legacy [][]byte) ([]ed25519.PublicKey, error) {
	keys := make([]ed25519.PublicKey, 0, 1+len(legacy))
	candidates := append([][]byte{primary}, legacy...)
	for _, pemBytes := range candidates {
		if len(bytes.TrimSpace(pemBytes)) == 0 {
			continue
		}
		block, _ := pem.Decode(pemBytes)
		if block == nil {
			return nil, fmt.Errorf("failed to decode public key PEM")
		}
		if len(block.Bytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid ed25519 public key size: got %d, want %d", len(block.Bytes), ed25519.PublicKeySize)
		}
		key := ed25519.PublicKey(append([]byte(nil), block.Bytes...))
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no valid ed25519 public keys configured")
	}
	return keys, nil
}

func newPinnedHTTPClient(cfg Config) (*http.Client, error) {
	pins := cfg.PinnedSPKIHashes
	if cfg.AllowSystemTrust {
		return &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			},
		}, nil
	}
	normalizedPins := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		normalized := strings.TrimSpace(pin)
		if normalized == "" {
			continue
		}
		normalizedPins[normalized] = struct{}{}
	}
	if strings.HasPrefix(strings.TrimSpace(cfg.ServerURL), "https://") && len(normalizedPins) == 0 {
		return nil, ErrTLSPinNotConfigured
	}
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(normalizedPins) == 0 {
				return ErrTLSPinNotConfigured
			}
			if len(cs.PeerCertificates) == 0 {
				return ErrTLSPinMismatch
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].RawSubjectPublicKeyInfo)
			actual := base64.StdEncoding.EncodeToString(sum[:])
			if _, ok := normalizedPins[actual]; ok {
				return nil
			}
			return fmt.Errorf("%w: got %s", ErrTLSPinMismatch, actual)
		},
	}

	if pool, err := x509.SystemCertPool(); err == nil && pool != nil {
		tlsCfg.RootCAs = pool
	}

	return &http.Client{
		Transport: &pinEnforcingTransport{
			base: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		},
	}, nil
}

type pinEnforcingTransport struct {
	base http.RoundTripper
}

func (p *pinEnforcingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL != nil && req.URL.Scheme == "https" && p.base == nil {
		return nil, ErrTLSPinNotConfigured
	}
	return p.base.RoundTrip(req)
}
