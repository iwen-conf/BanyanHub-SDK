package sdk

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func signUpdateHash(t *testing.T, privKey ed25519.PrivateKey, hash string) string {
	t.Helper()

	digest := sha256.Sum256([]byte(hash))
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privKey, digest[:]))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
