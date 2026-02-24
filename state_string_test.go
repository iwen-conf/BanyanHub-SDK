package sdk

import (
	"testing"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateInit, "INIT"},
		{StateActive, "ACTIVE"},
		{StateGrace, "GRACE"},
		{StateLocked, "LOCKED"},
		{StateBanned, "BANNED"},
		{State(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.state.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
