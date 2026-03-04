package sdk

import (
	"context"
	"testing"
	"time"
)

func TestStartHeartbeat_InitialState(t *testing.T) {
	cfg := Config{
		HeartbeatInterval: 100 * time.Millisecond,
		GracePolicy: GracePolicy{
			MaxOfflineDuration: 1 * time.Second,
		},
	}

	g := &Guard{
		cfg:         cfg,
		sm:          newStateMachine(),
		fingerprint: &Fingerprint{machineID: "test-machine"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g.startHeartbeat(ctx)

	time.Sleep(50 * time.Millisecond)

	if g.State() != StateInit {
		t.Errorf("expected Init state after heartbeat started, got %v", g.State())
	}
}

func TestStartHeartbeat_ContextCancellation(t *testing.T) {
	cfg := Config{
		HeartbeatInterval: 100 * time.Millisecond,
		GracePolicy: GracePolicy{
			MaxOfflineDuration: 10 * time.Second,
		},
	}

	g := &Guard{
		cfg:         cfg,
		sm:          newStateMachine(),
		fingerprint: &Fingerprint{machineID: "test-machine"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	g.startHeartbeat(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()

	time.Sleep(200 * time.Millisecond)

	select {
	case <-ctx.Done():
	default:
		t.Error("context should be cancelled")
	}
}
