package sdk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
)

type State int

const (
	StateInit State = iota
	StateActive
	StateGrace
	StateLocked
	StateBanned
)

func (s State) String() string {
	switch s {
	case StateInit:
		return "INIT"
	case StateActive:
		return "ACTIVE"
	case StateGrace:
		return "GRACE"
	case StateLocked:
		return "LOCKED"
	case StateBanned:
		return "BANNED"
	default:
		return "UNKNOWN"
	}
}

type persistedState struct {
	Lease          *lease          `json:"lease,omitempty"`
	LeaseCanonical json.RawMessage `json:"lease_canonical,omitempty"`
	LeaseSignature string          `json:"lease_signature,omitempty"`
	Watermark      string          `json:"watermark,omitempty"`
	LockFlag       bool            `json:"lock_flag"`
	BanFlag        bool            `json:"ban_flag"`
	UpdatedAt      string          `json:"updated_at"`
}

type persistedEnvelope struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

type persistentStateStore struct {
	mu          sync.RWMutex
	cfg         Config
	fingerprint *Fingerprint
	current     *persistedState
}

func newPersistentStateStore(cfg Config, fingerprint *Fingerprint) *persistentStateStore {
	return &persistentStateStore{cfg: cfg, fingerprint: fingerprint}
}

func (ps *persistentStateStore) Snapshot() *persistedState {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.current == nil {
		return nil
	}
	cloned := *ps.current
	if ps.current.Lease != nil {
		leaseCopy := *ps.current.Lease
		cloned.Lease = &leaseCopy
	}
	cloned.LeaseCanonical = append(json.RawMessage(nil), ps.current.LeaseCanonical...)
	return &cloned
}

func (ps *persistentStateStore) Load() (*persistedState, error) {
	path := filepath.Join(ps.cacheDir(), "state.bin")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var envelope persistedEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, ErrStateTampered
	}

	valid, err := ps.verifySignature(envelope.Payload, envelope.Signature)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrStateTampered
	}

	var state persistedState
	if err := json.Unmarshal(envelope.Payload, &state); err != nil {
		return nil, ErrStateTampered
	}

	ps.mu.Lock()
	ps.current = &state
	ps.mu.Unlock()
	return ps.Snapshot(), nil
}

func (ps *persistentStateStore) Save(state *persistedState) error {
	if state == nil {
		return nil
	}
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	signature, err := ps.signPayload(payload)
	if err != nil {
		return err
	}
	envelope := persistedEnvelope{
		Payload:   payload,
		Signature: signature,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}

	dir := ps.cacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "state.bin"), data, 0o600); err != nil {
		return err
	}

	ps.mu.Lock()
	copyState := *state
	if state.Lease != nil {
		leaseCopy := *state.Lease
		copyState.Lease = &leaseCopy
	}
	copyState.LeaseCanonical = append(json.RawMessage(nil), state.LeaseCanonical...)
	ps.current = &copyState
	ps.mu.Unlock()
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameAtomic(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func renameAtomic(tmpName, path string) error {
	if err := os.Rename(tmpName, path); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return err
		}
		return os.Rename(tmpName, path)
	}
	return nil
}

func (ps *persistentStateStore) signPayload(payload []byte) (string, error) {
	key, err := ps.deriveStateKey()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (ps *persistentStateStore) verifySignature(payload []byte, signature string) (bool, error) {
	expectedSignature, err := ps.signPayload(payload)
	if err != nil {
		return false, err
	}
	expected, err1 := base64.StdEncoding.DecodeString(expectedSignature)
	actual, err2 := base64.StdEncoding.DecodeString(signature)
	if err1 != nil || err2 != nil {
		return false, nil
	}
	return hmac.Equal(expected, actual), nil
}

func (ps *persistentStateStore) deriveStateKey() ([]byte, error) {
	reader := hkdf.New(sha256.New, []byte(ps.fingerprint.MachineID()), []byte(ps.cfg.ProjectSlug), []byte(ps.cfg.ComponentSlug+"|state"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("derive state key: %w", err)
	}
	return key, nil
}

func (ps *persistentStateStore) cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deploy-guard", ps.cfg.ProjectSlug, ps.cfg.ComponentSlug)
}

type stateMachine struct {
	mu    sync.RWMutex
	state State
}

func newStateMachine() *stateMachine {
	return &stateMachine{state: StateInit}
}

func (sm *stateMachine) restore(state *persistedState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	switch {
	case state == nil:
		sm.state = StateInit
	case state.BanFlag:
		sm.state = StateBanned
	case state.LockFlag:
		sm.state = StateLocked
	case state.Lease != nil:
		sm.state = StateActive
	default:
		sm.state = StateInit
	}
}

func (sm *stateMachine) Current() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *stateMachine) set(state State) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.state = state
}

func (sm *stateMachine) OnVerifySuccess() {
	sm.set(StateActive)
}

func (sm *stateMachine) OnHeartbeatOK() {
	current := sm.Current()
	if current == StateGrace || current == StateActive {
		sm.set(StateActive)
	}
}

func (sm *stateMachine) OnHeartbeatFail() {
	if sm.Current() == StateActive {
		sm.set(StateGrace)
	}
}

func (sm *stateMachine) OnKill() {
	sm.set(StateBanned)
}

func (sm *stateMachine) OnGracePeriodExpired() {
	if sm.Current() == StateGrace || sm.Current() == StateActive {
		sm.set(StateLocked)
	}
}
