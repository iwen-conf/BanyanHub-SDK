package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"strings"

	sdk "github.com/iwen-conf/BanyanHub-SDK"
)

type runtimeConfig struct {
	DSN        string `json:"dsn"`
	APIBaseURL string `json:"api_base_url"`
}

func main() {
	publicKeyPEM, err := os.ReadFile("public_key.pem")
	if err != nil {
		log.Fatalf("read public_key.pem: %v", err)
	}

	guard, err := sdk.New(sdk.Config{
		ServerURL:        os.Getenv("GUARD_SERVER_URL"),
		LicenseKey:       os.Getenv("GUARD_LICENSE_KEY"),
		PublicKeyPEM:     publicKeyPEM,
		ProjectSlug:      os.Getenv("GUARD_PROJECT_SLUG"),
		ComponentSlug:    os.Getenv("GUARD_COMPONENT_SLUG"),
		PinnedSPKIHashes: splitPins(os.Getenv("GUARD_PINNED_SPKI")),
		AllowSystemTrust: os.Getenv("GUARD_ALLOW_SYSTEM_TRUST") == "1",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := guard.Start(context.Background()); err != nil {
		log.Fatalf("guard start failed: %v", err)
	}
	defer guard.Stop()

	boxB64, err := os.ReadFile("config.sealed")
	if err != nil {
		log.Fatalf("read config.sealed: %v", err)
	}
	box, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(boxB64)))
	if err != nil {
		log.Fatalf("decode config.sealed: %v", err)
	}

	plaintext, err := guard.Unseal(box)
	if err != nil {
		log.Fatalf("unseal runtime config: %v", err)
	}

	var cfg runtimeConfig
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		log.Fatalf("parse runtime config: %v", err)
	}

	featureToken, err := guard.FeatureToken("runtime-config")
	if err != nil {
		log.Fatalf("derive feature token: %v", err)
	}

	log.Printf("runtime config unlocked for %s, feature token prefix=%s", cfg.APIBaseURL, featureToken[:12])
	log.Printf("dsn=%s", cfg.DSN)
}

func splitPins(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
