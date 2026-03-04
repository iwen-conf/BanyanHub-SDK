package sdk

import "sync"

type State int

const (
	StateInit   State = iota
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

type stateMachine struct {
	mu    sync.RWMutex
	state State
}

func newStateMachine() *stateMachine {
	return &stateMachine{state: StateInit}
}

func (sm *stateMachine) Current() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *stateMachine) OnVerifySuccess() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.state == StateInit || sm.state == StateGrace {
		sm.state = StateActive
	}
}

func (sm *stateMachine) OnHeartbeatOK() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.state == StateGrace || sm.state == StateActive {
		sm.state = StateActive
	}
}

func (sm *stateMachine) OnHeartbeatFail() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.state == StateActive {
		sm.state = StateGrace
	}
}

func (sm *stateMachine) OnKill() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.state = StateBanned
}

func (sm *stateMachine) OnGracePeriodExpired() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.state == StateGrace {
		sm.state = StateLocked
	}
}
