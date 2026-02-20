package sdk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	// Cached binary hash to avoid recalculating
	binaryHashOnce sync.Once
	binaryHashValue string
	binaryHashError error
)

// GetBinaryHash calculates the SHA256 hash of the current executable binary.
// The result is cached after the first call.
//
// This hash is used by the Centralized Release System (中央发版系统) to
// automatically identify the version of the running binary without manual
// configuration.
//
// The central server maintains a mapping table of hash → version for all
// released artifacts, allowing clients to query their version information
// at startup.
func GetBinaryHash() (string, error) {
	binaryHashOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			binaryHashError = fmt.Errorf("get executable path: %w", err)
			return
		}

		file, err := os.Open(exe)
		if err != nil {
			binaryHashError = fmt.Errorf("open executable: %w", err)
			return
		}
		defer file.Close()

		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			binaryHashError = fmt.Errorf("calculate hash: %w", err)
			return
		}

		binaryHashValue = hex.EncodeToString(hasher.Sum(nil))
	})

	return binaryHashValue, binaryHashError
}

// ResetBinaryHashCache resets the cached binary hash.
// This is useful for testing or when the binary is replaced at runtime.
func ResetBinaryHashCache() {
	binaryHashOnce = sync.Once{}
	binaryHashValue = ""
	binaryHashError = nil
}
