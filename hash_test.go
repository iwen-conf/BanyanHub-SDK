package sdk

import (
	"testing"
)

func TestGetBinaryHash_Success(t *testing.T) {
	// Reset cache before test
	ResetBinaryHashCache()

	hash, err := GetBinaryHash()
	if err != nil {
		t.Fatalf("GetBinaryHash failed: %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty hash")
	}

	// Hash should be 64 characters (SHA256 hex)
	if len(hash) != 64 {
		t.Errorf("expected hash length 64, got %d", len(hash))
	}

	// Should only contain hex characters
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("hash contains invalid character: %c", c)
		}
	}
}

func TestGetBinaryHash_Cached(t *testing.T) {
	// Reset cache
	ResetBinaryHashCache()

	// First call
	hash1, err1 := GetBinaryHash()
	if err1 != nil {
		t.Fatalf("first call failed: %v", err1)
	}

	// Second call (should be cached)
	hash2, err2 := GetBinaryHash()
	if err2 != nil {
		t.Fatalf("second call failed: %v", err2)
	}

	// Should return same value
	if hash1 != hash2 {
		t.Errorf("hashes don't match: %s vs %s", hash1, hash2)
	}
}

func TestResetBinaryHashCache(t *testing.T) {
	// Get initial hash
	ResetBinaryHashCache()
	hash1, _ := GetBinaryHash()

	// Reset cache
	ResetBinaryHashCache()

	// Get hash again
	hash2, _ := GetBinaryHash()

	// Should be the same (same executable)
	if hash1 != hash2 {
		t.Errorf("cache reset broken: %s vs %s", hash1, hash2)
	}
}



func TestGetBinaryHash_MultipleCalls(t *testing.T) {
	ResetBinaryHashCache()

	for i := 0; i < 5; i++ {
		hash, err := GetBinaryHash()
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		if hash == "" {
			t.Errorf("call %d returned empty hash", i)
		}
	}
}