package sdk

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

type heartbeatVectorCase struct {
	Updates       []updateInfo           `json:"updates"`
	UpdatesDigest string                 `json:"updates_digest"`
	Payload       map[string]interface{} `json:"payload"`
	Canonical     string                 `json:"canonical"`
	Signature     string                 `json:"signature"`
}

type vector struct {
	Keys struct {
		PublicSPKI   string `json:"public_spki"`
		PrivatePKCS8 string `json:"private_pkcs8"`
	} `json:"keys"`
	Lease struct {
		Payload   map[string]interface{} `json:"payload"`
		Canonical string                 `json:"canonical"`
		Signature string                 `json:"signature"`
	} `json:"lease"`
	Heartbeat struct {
		EmptyUpdates    heartbeatVectorCase `json:"empty_updates"`
		NonEmptyUpdates heartbeatVectorCase `json:"non_empty_updates"`
	} `json:"heartbeat"`
}

func TestV3ContractParity(t *testing.T) {
	data, err := os.ReadFile("contract/v3_vectors.json")
	if err != nil {
		t.Fatalf("failed to read vectors: %v", err)
	}

	var v vector
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("failed to unmarshal vectors: %v", err)
	}

	t.Run("Lease Canonicalization", func(t *testing.T) {
		raw, _ := json.Marshal(v.Lease.Payload)
		canonical, err := canonicalJSON(raw)
		if err != nil {
			t.Fatalf("canonicalJSON failed: %v", err)
		}
		if string(canonical) != v.Lease.Canonical {
			t.Errorf("canonical mismatch\nexpected: %s\ngot: %s", v.Lease.Canonical, string(canonical))
		}
	})

	t.Run("Empty Updates Digest Uses []", func(t *testing.T) {
		got := updatesDigest(nil)
		if got != v.Heartbeat.EmptyUpdates.UpdatesDigest {
			t.Fatalf("nil updates digest mismatch\nexpected: %s\ngot: %s", v.Heartbeat.EmptyUpdates.UpdatesDigest, got)
		}
		got = updatesDigest([]updateInfo{})
		if got != v.Heartbeat.EmptyUpdates.UpdatesDigest {
			t.Fatalf("empty updates digest mismatch\nexpected: %s\ngot: %s", v.Heartbeat.EmptyUpdates.UpdatesDigest, got)
		}
	})

	t.Run("Non-empty Updates Digest Matches Vector", func(t *testing.T) {
		got := updatesDigest(v.Heartbeat.NonEmptyUpdates.Updates)
		if got != v.Heartbeat.NonEmptyUpdates.UpdatesDigest {
			t.Fatalf("non-empty updates digest mismatch\nexpected: %s\ngot: %s", v.Heartbeat.NonEmptyUpdates.UpdatesDigest, got)
		}
	})

	t.Run("Heartbeat Canonicalization Empty Updates", func(t *testing.T) {
		raw, _ := json.Marshal(v.Heartbeat.EmptyUpdates.Payload)
		canonical, err := canonicalJSON(raw)
		if err != nil {
			t.Fatalf("canonicalJSON failed: %v", err)
		}
		if string(canonical) != v.Heartbeat.EmptyUpdates.Canonical {
			t.Errorf("canonical mismatch\nexpected: %s\ngot: %s", v.Heartbeat.EmptyUpdates.Canonical, string(canonical))
		}
	})

	t.Run("Heartbeat Canonicalization Non-empty Updates", func(t *testing.T) {
		raw, _ := json.Marshal(v.Heartbeat.NonEmptyUpdates.Payload)
		canonical, err := canonicalJSON(raw)
		if err != nil {
			t.Fatalf("canonicalJSON failed: %v", err)
		}
		if string(canonical) != v.Heartbeat.NonEmptyUpdates.Canonical {
			t.Errorf("canonical mismatch\nexpected: %s\ngot: %s", v.Heartbeat.NonEmptyUpdates.Canonical, string(canonical))
		}
	})

	t.Run("Heartbeat Signature Non-empty Updates", func(t *testing.T) {
		raw, _ := json.Marshal(v.Heartbeat.NonEmptyUpdates.Payload)
		canonical, err := canonicalJSON(raw)
		if err != nil {
			t.Fatalf("canonicalJSON failed: %v", err)
		}

		keyDER, err := base64.StdEncoding.DecodeString(v.Keys.PrivatePKCS8)
		if err != nil {
			t.Fatalf("decode private key failed: %v", err)
		}
		privateKeyAny, err := x509.ParsePKCS8PrivateKey(keyDER)
		if err != nil {
			t.Fatalf("parse private key failed: %v", err)
		}
		privateKey, ok := privateKeyAny.(ed25519.PrivateKey)
		if !ok {
			t.Fatal("private key is not Ed25519")
		}

		digest := sha256.Sum256(canonical)
		signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:]))
		if signature != v.Heartbeat.NonEmptyUpdates.Signature {
			t.Fatalf("non-empty heartbeat signature mismatch\nexpected: %s\ngot: %s", v.Heartbeat.NonEmptyUpdates.Signature, signature)
		}
	})
}
