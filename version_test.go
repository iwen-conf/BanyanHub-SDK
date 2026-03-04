package sdk

import (
	"testing"
)

func TestVersionInfo(t *testing.T) {
	info := VersionInfo()

	if info == "" {
		t.Error("VersionInfo should not be empty")
	}

	expected := Version + " (" + GitCommit + ", built at " + BuildTime + ")"
	if info != expected {
		t.Errorf("VersionInfo mismatch: got %q, expected %q", info, expected)
	}
}

func TestVersionInfo_DefaultValues(t *testing.T) {
	info := VersionInfo()

	if info != "dev (unknown, built at unknown)" {
		t.Logf("Note: Version info shows injected values: %s", info)
	}
}
