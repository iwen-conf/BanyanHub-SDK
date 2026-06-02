package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/hkdf"
)

type lease struct {
	ProjectSlug string `json:"project_slug"`
	Tier        string `json:"tier"`
	LeaseID     string `json:"lease_id"`
	LicenseKey  string `json:"license_key"`
	MachineID   string `json:"machine_id"`
}

func main() {
	var (
		leasePath      = flag.String("lease", "", "path to canonical lease JSON")
		signatureB64   = flag.String("signature", "", "base64 lease signature")
		configPath     = flag.String("config", "", "path to plaintext JSON config")
		outPath        = flag.String("out", "config.sealed", "output file path")
	)
	flag.Parse()

	if *leasePath == "" || *signatureB64 == "" || *configPath == "" {
		fmt.Fprintln(os.Stderr, "usage: sealbox -lease lease.json -signature <base64> -config config.json [-out config.sealed]")
		os.Exit(2)
	}

	leaseBytes, err := os.ReadFile(*leasePath)
	if err != nil {
		panic(err)
	}
	var value lease
	if err := json.Unmarshal(leaseBytes, &value); err != nil {
		panic(err)
	}

	plain, err := os.ReadFile(*configPath)
	if err != nil {
		panic(err)
	}

	box, err := seal(value, *signatureB64, plain)
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(*outPath, []byte(base64.StdEncoding.EncodeToString(box)), 0o600); err != nil {
		panic(err)
	}
}

func seal(leaseValue lease, signatureB64 string, plaintext []byte) ([]byte, error) {
	signature, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return nil, err
	}
	reader := hkdf.New(
		sha256.New,
		signature,
		[]byte(leaseValue.MachineID),
		[]byte(leaseValue.LicenseKey+"|"+leaseValue.LeaseID),
	)
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := []byte(leaseValue.ProjectSlug + "|" + leaseValue.Tier + "|" + leaseValue.LeaseID)
	box := aead.Seal(nil, nonce, plaintext, aad)
	return append(nonce, box...), nil
}
