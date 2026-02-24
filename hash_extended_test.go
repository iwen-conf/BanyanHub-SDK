package sdk

import (
	"os"
	"testing"
)

// TestGetBinaryHash_CachingBehavior tests that binary hash is cached
func TestGetBinaryHash_CachingBehavior(t *testing.T) {
	// Reset cache for test
	ResetBinaryHashCache()

	// First call should compute
	hash1, err1 := GetBinaryHash()
	if err1 != nil {
		t.Fatalf("first GetBinaryHash failed: %v", err1)
	}

	if hash1 == "" {
		t.Error("expected non-empty hash")
	}

	// Second call should return cached value
	hash2, err2 := GetBinaryHash()
	if err2 != nil {
		t.Fatalf("second GetBinaryHash failed: %v", err2)
	}

	if hash1 != hash2 {
		t.Errorf("expected same hash from cache, got %s then %s", hash1, hash2)
	}
}

// TestGetBinaryHash_ConsistentValue tests that binary hash is consistent
func TestGetBinaryHash_ConsistentValue(t *testing.T) {
	ResetBinaryHashCache()

	hash1, _ := GetBinaryHash()

	ResetBinaryHashCache()

	hash2, _ := GetBinaryHash()

	if hash1 != hash2 {
		t.Errorf("expected consistent hash, got %s then %s", hash1, hash2)
	}
}

// TestGetBinaryHash_ValidSHA256 tests that returned hash is valid SHA256 format
func TestGetBinaryHash_ValidSHA256(t *testing.T) {
	ResetBinaryHashCache()

	hash, err := GetBinaryHash()
	if err != nil {
		t.Fatalf("GetBinaryHash failed: %v", err)
	}

	// SHA256 hex string should be 64 characters
	if len(hash) != 64 {
		t.Errorf("expected SHA256 hex length 64, got %d", len(hash))
	}

	// Verify it's valid hex
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex character in hash: %c", c)
			break
		}
	}
}

// TestResetBinaryHashCache_ClearsCache tests that ResetBinaryHashCache clears cache
func TestResetBinaryHashCache_ClearsCache(t *testing.T) {
	hash1, _ := GetBinaryHash()
	ResetBinaryHashCache()
	hash2, _ := GetBinaryHash()

	// Both should be valid (should be same value from same binary)
	if hash1 != hash2 {
		t.Errorf("expected same hash after reset, got %s then %s", hash1, hash2)
	}

	// Just verify it doesn't panic
	ResetBinaryHashCache()
}

// TestGetBinaryHash_CurrentExecutable tests with actual binary
func TestGetBinaryHash_CurrentExecutable(t *testing.T) {
	ResetBinaryHashCache()

	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot get executable path: %v", err)
	}

	hash, err := GetBinaryHash()
	if err != nil {
		t.Fatalf("GetBinaryHash failed: %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty hash for current executable")
	}

	// Verify the file is readable
	if _, err := os.Stat(exe); err != nil {
		t.Skipf("executable not accessible: %v", err)
	}
}
