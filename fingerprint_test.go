package sdk

import (
	"testing"
)

func TestCollectFingerprint(t *testing.T) {
	fp, err := collectFingerprint()
	if err != nil {
		t.Fatalf("collectFingerprint failed: %v", err)
	}

	if fp.MachineID() == "" {
		t.Error("expected non-empty machine ID")
	}

	auxSignals := fp.AuxSignals()
	if len(auxSignals) == 0 {
		t.Error("expected non-empty aux signals")
	}

	// Check for expected keys
	expectedKeys := []string{"os", "arch", "cpu_cores"}
	for _, key := range expectedKeys {
		if _, ok := auxSignals[key]; !ok {
			t.Errorf("expected aux signal %s not found", key)
		}
	}
}

func TestFingerprint_MachineID(t *testing.T) {
	fp := &Fingerprint{
		machineID: "test-machine-id",
	}

	if fp.MachineID() != "test-machine-id" {
		t.Errorf("expected machine ID test-machine-id, got %s", fp.MachineID())
	}
}

func TestFingerprint_AuxSignals(t *testing.T) {
	fp := &Fingerprint{
		auxSignals: map[string]string{
			"os":   "linux",
			"arch": "amd64",
		},
	}

	signals := fp.AuxSignals()
	if signals["os"] != "linux" {
		t.Errorf("expected os linux, got %s", signals["os"])
	}

	if signals["arch"] != "amd64" {
		t.Errorf("expected arch amd64, got %s", signals["arch"])
	}
}
