package sdk

import "testing"

func TestStateMachine_InitialState(t *testing.T) {
	sm := newStateMachine()
	if sm.Current() != StateInit {
		t.Errorf("expected initial state Init, got %v", sm.Current())
	}
}

func TestStateMachine_VerifySuccess(t *testing.T) {
	sm := newStateMachine()
	sm.OnVerifySuccess()
	if sm.Current() != StateActive {
		t.Errorf("expected state Active after verify success, got %v", sm.Current())
	}
}

func TestStateMachine_HeartbeatFail(t *testing.T) {
	sm := newStateMachine()
	sm.OnVerifySuccess()
	sm.OnHeartbeatFail()
	if sm.Current() != StateGrace {
		t.Errorf("expected state Grace after heartbeat fail, got %v", sm.Current())
	}
}

func TestStateMachine_HeartbeatRecover(t *testing.T) {
	sm := newStateMachine()
	sm.OnVerifySuccess()
	sm.OnHeartbeatFail()
	sm.OnHeartbeatOK()
	if sm.Current() != StateActive {
		t.Errorf("expected state Active after heartbeat recover, got %v", sm.Current())
	}
}

func TestStateMachine_GracePeriodExpired(t *testing.T) {
	sm := newStateMachine()
	sm.OnVerifySuccess()
	sm.OnHeartbeatFail()
	sm.OnGracePeriodExpired()
	if sm.Current() != StateLocked {
		t.Errorf("expected state Locked after grace period expired, got %v", sm.Current())
	}
}

func TestStateMachine_Kill(t *testing.T) {
	sm := newStateMachine()
	sm.OnVerifySuccess()
	sm.OnKill()
	if sm.Current() != StateBanned {
		t.Errorf("expected state Banned after kill, got %v", sm.Current())
	}
}

func TestStateMachine_KillFromAnyState(t *testing.T) {
	states := []State{StateInit, StateActive, StateGrace, StateLocked}
	for _, initialState := range states {
		sm := newStateMachine()
		sm.state = initialState
		sm.OnKill()
		if sm.Current() != StateBanned {
			t.Errorf("expected state Banned after kill from %v, got %v", initialState, sm.Current())
		}
	}
}
