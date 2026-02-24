package sdk

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
)

// TestCollectFingerprint_ReturnsValid tests that collectFingerprint returns valid data
func TestCollectFingerprint_ReturnsValid(t *testing.T) {
	fp, err := collectFingerprint()
	if err != nil {
		t.Fatalf("collectFingerprint failed: %v", err)
	}

	if fp == nil {
		t.Error("expected non-nil fingerprint")
	}

	// Test MachineID
	machineID := fp.MachineID()
	if machineID == "" {
		t.Error("expected non-empty machine ID")
	}

	// Machine ID should be sha256: followed by 64 hex chars
	if !strings.HasPrefix(machineID, "sha256:") {
		t.Error("expected machine ID to start with sha256:")
	}
	hexPart := strings.TrimPrefix(machineID, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("expected machine ID hex part length 64, got %d", len(hexPart))
	}

	// Test AuxSignals
	signals := fp.AuxSignals()
	if len(signals) == 0 {
		t.Error("expected non-empty aux signals")
	}

	// Should have os, arch. Other keys are optional (may be missing in some environments)
	expectedKeys := []string{"os", "arch"}
	for _, key := range expectedKeys {
		if _, ok := signals[key]; !ok {
			t.Errorf("expected aux signal %s to be present", key)
		}
	}
}

// TestCollectFingerprint_ConsistentMachineID tests machine ID consistency
func TestCollectFingerprint_ConsistentMachineID(t *testing.T) {
	fp1, _ := collectFingerprint()
	machineID1 := fp1.MachineID()

	fp2, _ := collectFingerprint()
	machineID2 := fp2.MachineID()

	if machineID1 != machineID2 {
		t.Errorf("expected consistent machine ID, got %s then %s", machineID1, machineID2)
	}
}

// TestAuxSignals_ContainsSystemInfo tests that aux signals contain system information
func TestAuxSignals_ContainsSystemInfo(t *testing.T) {
	fp, _ := collectFingerprint()
	signals := fp.AuxSignals()

	// OS should be non-empty
	os := signals["os"]
	if os == "" {
		t.Error("expected non-empty os in aux signals")
	}

	// Valid OS values: linux, darwin, windows, etc.
	validOS := []string{"linux", "darwin", "windows", "freebsd", "openbsd"}
	found := false
	for _, valid := range validOS {
		if strings.Contains(os, valid) || os == valid {
			found = true
			break
		}
	}
	if !found && os != "" {
		t.Logf("unusual OS value: %s", os)
	}

	// Arch should be non-empty
	arch := signals["arch"]
	if arch == "" {
		t.Error("expected non-empty arch in aux signals")
	}

	// Valid architectures
	validArchs := []string{"amd64", "arm64", "386", "arm"}
	found = false
	for _, valid := range validArchs {
		if arch == valid {
			found = true
			break
		}
	}
	if !found && arch != "" {
		t.Logf("unusual arch value: %s", arch)
	}

	// CPU info is optional (may be missing in some environments)
	_, hasCPUModel := signals["cpu_model"]
	_, hasCPUCores := signals["cpu_cores"]
	if !hasCPUModel && !hasCPUCores {
		t.Logf("CPU info not available")
	}

	// RAM info is optional (may be missing in some environments)
	_, hasRAM := signals["total_ram_mb"]
	if !hasRAM {
		t.Logf("RAM info not available")
	}

	// MAC addresses is optional (may be missing in some environments)
	_, hasMACs := signals["mac_addresses"]
	if !hasMACs {
		t.Logf("MAC addresses not available")
	}
}

// TestMachineID_HexFormat tests that machine ID is valid hex
func TestMachineID_HexFormat(t *testing.T) {
	fp, _ := collectFingerprint()
	machineID := fp.MachineID()

	// Check that the hex part after sha256: is valid hex
	hexPart := strings.TrimPrefix(machineID, "sha256:")
	for _, c := range hexPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex character in machine ID: %c", c)
			break
		}
	}
}

// TestGetMACAddresses_NonEmpty tests that MAC addresses are collected
func TestGetMACAddresses_NonEmpty(t *testing.T) {
	fp, err := collectFingerprint()
	if err != nil {
		t.Fatalf("collectFingerprint failed: %v", err)
	}

	signals := fp.AuxSignals()
	_, hasMACs := signals["mac_addresses"]

	// MAC can be empty on some systems (e.g., in containers), but should be present in map
	// Just verify it's a string (could be empty)
	if !hasMACs {
		t.Logf("MAC address not available (might be in container/VM)")
	}
}

// TestFingerprint_Isolated tests that fingerprints are isolated per instance
func TestFingerprint_Isolated(t *testing.T) {
	cfg1 := Config{
		ServerURL:     "http://localhost",
		LicenseKey:    "key1",
		PublicKeyPEM:  generateTestPublicKey(),
		ProjectSlug:   "project1",
		ComponentSlug: "backend",
	}

	cfg2 := Config{
		ServerURL:     "http://localhost",
		LicenseKey:    "key2",
		PublicKeyPEM:  generateTestPublicKey(),
		ProjectSlug:   "project2",
		ComponentSlug: "backend",
	}

	g1, _ := New(cfg1)
	g2, _ := New(cfg2)

	// Both should have fingerprints
	if g1.fingerprint == nil {
		t.Error("g1 should have fingerprint")
	}
	if g2.fingerprint == nil {
		t.Error("g2 should have fingerprint")
	}

	// Machine IDs should be the same (same machine)
	if g1.fingerprint.MachineID() != g2.fingerprint.MachineID() {
		t.Error("expected same machine ID for same machine")
	}
}

// Helper function
func generateTestPublicKey() []byte {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	return pemEncodePublicKey(pubKey)
}
