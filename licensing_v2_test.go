package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyLicense_MachineBindingMismatch(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	originalMachineID := guard.fingerprint.MachineID()
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(originalMachineID))

	err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false)
	if err != nil {
		t.Fatalf("acceptLease: %v", err)
	}

	guard.fingerprint = &Fingerprint{machineID: originalMachineID + "-other", auxSignals: guard.fingerprint.AuxSignals()}
	if err := guard.validatePersistedLease(time.Now()); err != ErrLeaseBindingMismatch {
		t.Fatalf("expected ErrLeaseBindingMismatch, got %v", err)
	}
}

func TestHeartbeat_UnsignedResponseForcesFailure(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	if err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false); err != nil {
		t.Fatal(err)
	}
	guard.sm.OnVerifySuccess()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(heartbeatResponse{
			Status:         "ok",
			Lease:          json.RawMessage(leaseJSON),
			LeaseSignature: sig,
			Nonce:          "wrong",
			ServerTime:     time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	guard.cfg.ServerURL = server.URL
	guard.httpClient = insecureClientFromServer(server)
	err := guard.sendHeartbeat(context.Background())
	if err == nil {
		t.Fatal("expected invalid heartbeat error")
	}
	if err != ErrHeartbeatInvalid && err != ErrHeartbeatNonceMismatch {
		t.Fatalf("expected signature or nonce failure, got %v", err)
	}
}

func TestHeartbeatJitterBounds(t *testing.T) {
	interval := time.Second
	min := 900 * time.Millisecond
	max := 1100 * time.Millisecond

	for i := 0; i < 100; i++ {
		got := heartbeatJitter(interval)
		if got < min || got > max {
			t.Fatalf("heartbeatJitter(%v) = %v, want between %v and %v", interval, got, min, max)
		}
	}
	if got := heartbeatJitter(0); got != 0 {
		t.Fatalf("heartbeatJitter(0) = %v, want 0", got)
	}
	if got := heartbeatJitter(time.Nanosecond); got != time.Nanosecond {
		t.Fatalf("heartbeatJitter(1ns) = %v, want 1ns", got)
	}
}

func TestPersistentBannedStateSurvivesRestart(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	pins := []string{"test-pin"}
	guard, privKey := newTestGuard(t, &pins)
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	if err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false); err != nil {
		t.Fatal(err)
	}
	if err := guard.persistBan(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(Config{
		ServerURL:        "https://example.invalid",
		LicenseKey:       "test-license",
		PublicKeyPEM:     pemEncodePublicKey(guard.publicKey),
		ProjectSlug:      "test-project",
		ComponentSlug:    "backend",
		PinnedSPKIHashes: pins,
	})
	if err != nil {
		t.Fatal(err)
	}
	reloaded.fingerprint = guard.fingerprint
	if reloaded.State() != StateBanned {
		t.Fatalf("expected banned state after restart, got %v", reloaded.State())
	}
}

func TestClockRollbackBelowWatermarkForcesRecheck(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	leaseValue := mustParseLease(t, leaseJSON)
	if err := guard.acceptLease(leaseValue, sig, false); err != nil {
		t.Fatal(err)
	}
	state := guard.currentLeaseState()
	state.Watermark = time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	if err := guard.store.Save(state); err != nil {
		t.Fatal(err)
	}

	if err := guard.validatePersistedLease(time.Now()); err != ErrClockRollback {
		t.Fatalf("expected ErrClockRollback, got %v", err)
	}
}

func TestHardBindingAPIsRequireLeaseAndThenSucceed(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	if _, err := guard.Unseal([]byte("bad")); err != ErrLeaseUnavailable {
		t.Fatalf("expected ErrLeaseUnavailable, got %v", err)
	}
	if _, err := guard.FeatureToken("reports"); err != ErrLeaseUnavailable {
		t.Fatalf("expected ErrLeaseUnavailable, got %v", err)
	}

	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	if err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false); err != nil {
		t.Fatal(err)
	}
	guard.sm.OnVerifySuccess()

	box := sealForTest(t, sig, mustParseLease(t, leaseJSON), []byte(`{"dsn":"real"}`))
	plain, err := guard.Unseal(box)
	if err != nil {
		t.Fatalf("Unseal failed: %v", err)
	}
	if string(plain) != `{"dsn":"real"}` {
		t.Fatalf("unexpected plaintext: %s", string(plain))
	}
	token, err := guard.FeatureToken("reports")
	if err != nil || token == "" {
		t.Fatalf("FeatureToken failed: %v token=%q", err, token)
	}
}

func TestTLSPinMismatchRefusesConnectionUnlessAllowSystemTrust(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := Config{
		ServerURL:        server.URL,
		LicenseKey:       "test-license",
		PublicKeyPEM:     pemEncodePublicKey(pubKeyFromRandom(t)),
		ProjectSlug:      "test-project",
		ComponentSlug:    "backend",
		PinnedSPKIHashes: []string{"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	}
	guard, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	verifyConnection := guard.httpClient.Transport.(*pinEnforcingTransport).base.(*http.Transport).TLSClientConfig.VerifyConnection
	guard.httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				VerifyConnection:   verifyConnection,
			},
		},
	}
	_, err = guard.httpClient.Get(server.URL)
	if err == nil {
		t.Fatal("expected tls pin mismatch")
	}

	cfg.AllowSystemTrust = true
	cfg.PinnedSPKIHashes = nil
	guard, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	guard.httpClient = server.Client()
	resp, err := guard.httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("expected request success with AllowSystemTrust, got %v", err)
	}
	resp.Body.Close()
}

func TestUpdateRejectsNonStrictlyGreaterVersion(t *testing.T) {
	if isStrictlyNewerVersion("1.2.3", "1.2.3") {
		t.Fatal("equal version should not be newer")
	}
	if isStrictlyNewerVersion("1.2.3", "1.2.2") {
		t.Fatal("downgrade should not be newer")
	}
	if !isStrictlyNewerVersion("1.2.3", "1.2.4") {
		t.Fatal("greater version should be newer")
	}
}

func newTestGuard(t *testing.T, pins *[]string) (*Guard, ed25519.PrivateKey) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pinSet := []string{"test-pin"}
	if pins != nil {
		pinSet = *pins
	}
	cfg := Config{
		ServerURL:        "https://example.invalid",
		LicenseKey:       "test-license",
		PublicKeyPEM:     pemEncodePublicKey(pubKey),
		ProjectSlug:      "test-project",
		ComponentSlug:    "backend",
		PinnedSPKIHashes: pinSet,
	}
	guard, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	guard.httpClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	guard.store = newPersistentStateStore(cfg, guard.fingerprint)
	return guard, privKey
}

func testLease(machineID string) *lease {
	now := time.Now().UTC()
	return &lease{
		ExpiresAt:   now.Add(24 * time.Hour).Format(time.RFC3339),
		Features:    []string{"reports"},
		GraceUntil:  now.Add(72 * time.Hour).Format(time.RFC3339),
		IssuedAt:    now.Format(time.RFC3339),
		LeaseID:     "lease-123",
		LicenseKey:  "test-license",
		MachineID:   machineID,
		MaxMachines: 1,
		ProjectSlug: "test-project",
		ServerTime:  now.Format(time.RFC3339),
		Tier:        "commercial",
	}
}

func signedLeaseJSON(t *testing.T, privKey ed25519.PrivateKey, leaseValue *lease) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(leaseValue)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	sig := ed25519.Sign(privKey, digest[:])
	return canonical, base64.StdEncoding.EncodeToString(sig)
}

func mustParseLease(t *testing.T, raw []byte) *lease {
	t.Helper()
	var leaseValue lease
	if err := json.Unmarshal(raw, &leaseValue); err != nil {
		t.Fatal(err)
	}
	return &leaseValue
}

func sealForTest(t *testing.T, sig string, leaseValue *lease, plaintext []byte) []byte {
	t.Helper()
	aead, err := newLeaseAEAD(sig, leaseValue)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	box := aead.Seal(nil, nonce, plaintext, leaseAAD(leaseValue))
	return append(nonce, box...)
}

func pubKeyFromRandom(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pubKey
}

func insecureClientFromServer(server *httptest.Server) *http.Client {
	client := server.Client()
	return client
}

func TestStateFileWritten(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	if err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(guard.store.cacheDir(), "state.bin")
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("state.bin missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state.bin mode = %v, want 0600", info.Mode().Perm())
	}
	if loaded, err := guard.store.Load(); err != nil || loaded == nil || loaded.Lease == nil {
		t.Fatalf("saved state should reload, state=%#v err=%v", loaded, err)
	}
	matches, err := filepath.Glob(filepath.Join(guard.store.cacheDir(), ".state.bin.*.tmp"))
	if err != nil {
		t.Fatalf("glob temp state files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("state save should not leave temp files: %v", matches)
	}
}

func TestStateSaveCleansTempFileOnRenameFailure(t *testing.T) {
	guard, _ := newTestGuard(t, nil)
	dir := guard.store.cacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	statePath := filepath.Join(dir, "state.bin")
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatalf("mkdir state.bin blocker: %v", err)
	}

	err := guard.store.Save(&persistedState{BanFlag: true})
	if err == nil {
		t.Fatal("expected state save to fail when target is a directory")
	}
	if info, statErr := os.Stat(statePath); statErr != nil || !info.IsDir() {
		t.Fatalf("state.bin blocker should remain a directory, info=%#v err=%v", info, statErr)
	}
	matches, globErr := filepath.Glob(filepath.Join(dir, ".state.bin.*.tmp"))
	if globErr != nil {
		t.Fatalf("glob temp state files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("failed state save should clean temp files: %v", matches)
	}
}

func TestStartUsesPersistedLease(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	if err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false); err != nil {
		t.Fatal(err)
	}
	if err := guard.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	guard.Stop()
}

func TestStartIsIdempotent(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	if err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false); err != nil {
		t.Fatal(err)
	}

	if err := guard.Start(context.Background()); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	firstDone := guard.heartbeatDone

	if err := guard.Start(context.Background()); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	if guard.heartbeatDone != firstDone {
		t.Fatal("expected second Start to reuse existing heartbeat loop")
	}

	guard.Stop()
}

func TestStopCancelsInFlightHeartbeat(t *testing.T) {
	guard, privKey := newTestGuard(t, nil)
	leaseJSON, sig := signedLeaseJSON(t, privKey, testLease(guard.fingerprint.MachineID()))
	if err := guard.acceptLease(mustParseLease(t, leaseJSON), sig, false); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	guard.cfg.ServerURL = "https://example.invalid"
	guard.cfg.HeartbeatInterval = time.Millisecond
	guard.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/v1/heartbeat" {
				return nil, http.ErrUseLastResponse
			}
			select {
			case started <- struct{}{}:
			default:
			}

			<-req.Context().Done()
			select {
			case canceled <- struct{}{}:
			default:
			}
			return nil, req.Context().Err()
		}),
	}

	if err := guard.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat request did not start")
	}

	stopped := make(chan struct{})
	go func() {
		guard.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after canceling heartbeat")
	}

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected in-flight heartbeat request to be canceled")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
