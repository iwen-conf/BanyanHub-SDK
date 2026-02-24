# BanyanHub SDK

[中文文档](./README_zh.md)

Go SDK for [BanyanHub](https://github.com/iwen-conf/BanyanHub-SDK) — an enterprise centralized release system providing license verification, machine fingerprinting, heartbeat monitoring, OTA updates, and CDK activation.

## Installation

```bash
go get github.com/iwen-conf/BanyanHub-SDK
```

Requires Go 1.24+.

## Quick Start

```go
package main

import (
    "context"
    "log"
    _ "embed"

    sdk "github.com/iwen-conf/BanyanHub-SDK"
)

//go:embed public_key.pem
var publicKeyPEM []byte

func main() {
    guard, err := sdk.New(sdk.Config{
        ServerURL:     "https://your-api.example.com",
        LicenseKey:    "XXXXX-XXXXX-XXXXX-XXXXX",
        PublicKeyPEM:  publicKeyPEM,
        ProjectSlug:   "my-project",
        ComponentSlug: "backend",
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := guard.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer guard.Stop()

    if err := guard.Check(); err != nil {
        log.Fatal(err)
    }

    log.Println("License verified, app running")
    // your business logic...
}
```

## CDK Activation

Exchange an activation code for a license key:

```go
result, err := sdk.Activate(
    "https://your-api.example.com",
    "CDK-A1B2-C3D4-E5F6-G7H8",
    "Acme Corp",
    "admin@acme.com",
)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("License Key: %s\n", result.LicenseKey)
```

## Configuration

```go
sdk.Config{
    // Required
    ServerURL:     "https://your-api.example.com",
    LicenseKey:    "XXXXX-XXXXX-XXXXX-XXXXX",
    PublicKeyPEM:  publicKeyPEM,
    ProjectSlug:   "my-project",
    ComponentSlug: "backend",

    // Optional
    HeartbeatInterval: 30 * time.Minute,  // default: 1h
    GracePolicy: sdk.GracePolicy{
        MaxOfflineDuration: 48 * time.Hour,  // default: 72h
    },
    OTA: sdk.OTAConfig{
        Enabled:    true,
        AutoUpdate: true,
        Platform:   "linux-amd64",
    },
    ManagedComponents: []sdk.ManagedComponent{
        {
            Slug:     "admin-frontend",
            Dir:      "/opt/app/frontend",
            Strategy: sdk.UpdateFrontend,
        },
    },
}
```

## Plugin Discovery and Manual Updates

If you want end users to confirm updates manually, set `OTA.AutoUpdate = false` and use:

```go
catalog, err := guard.GetPluginCatalog(context.Background(), true)
if err != nil {
    log.Fatal(err)
}

updates, err := guard.CheckPluginUpdates(context.Background())
if err != nil {
    log.Fatal(err)
}

for _, p := range updates {
    // Let users confirm via your UI/CLI first
    if p.Slug == "admin-frontend" {
        if err := guard.UpdatePlugin(context.Background(), p.Slug); err != nil {
            log.Printf("update failed: %v", err)
        }
    }
}
```

## State Machine

```
INIT ──verify ok──→ ACTIVE ──heartbeat fail──→ GRACE ──timeout──→ LOCKED
                      ↑                          │
                      └──heartbeat ok────────────┘

Any state ──server ban──→ BANNED
```

| State | `Check()` | Description |
|-------|-----------|-------------|
| ACTIVE | `nil` | Normal operation |
| GRACE | `nil` | Heartbeat failed, within grace period |
| LOCKED | `ErrLocked` | Offline timeout, app should stop |
| BANNED | `ErrBanned` | Banned by admin |

## Features

- **License Verification** — Ed25519 signature verification with local caching
- **Heartbeat** — Periodic heartbeat with jitter to avoid thundering herd
- **State Machine** — INIT → ACTIVE → GRACE → LOCKED, with BANNED override
- **OTA Updates** — Backend binary replacement + frontend tar.gz atomic swap
- **CDK Activation** — Exchange activation codes for license keys
- **Machine Fingerprint** — Hardware-based device identification

## License

Private
