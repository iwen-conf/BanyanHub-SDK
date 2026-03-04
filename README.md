# BanyanHub SDK

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Private-red)](LICENSE)

[中文文档](./README_zh.md)

Go SDK for [BanyanHub](https://github.com/iwen-conf/BanyanHub) — an enterprise centralized release system. Embed this SDK into your Go application to gain license verification, machine fingerprinting, heartbeat monitoring, OTA auto-updates, plugin management, user feedback, and CDK activation.

## Installation

```bash
go get github.com/iwen-conf/BanyanHub-SDK
```

Requires **Go 1.24+**.

## Quick Start

```go
package main

import (
    "context"
    _ "embed"
    "log"

    sdk "github.com/iwen-conf/BanyanHub-SDK"
)

//go:embed public_key.pem
var publicKeyPEM []byte

func main() {
    guard, err := sdk.New(sdk.Config{
        ServerURL:     "https://guard.example.com",
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

## Features

| Feature | Description |
|---------|-------------|
| **License Verification** | Ed25519 signature verification with local caching and offline grace period |
| **Machine Fingerprint** | Hardware-based device binding (Machine ID + CPU/RAM/MAC signals) |
| **Heartbeat** | Periodic status reporting with jitter to prevent thundering herd |
| **State Machine** | Graceful degradation: INIT → ACTIVE → GRACE → LOCKED, with BANNED override |
| **OTA Updates** | Binary atomic replacement + frontend tar.gz directory swap with auto-rollback |
| **Plugin System** | Plugin discovery, version management, and individual update control |
| **CDK Activation** | Exchange activation codes for license keys (self-service provisioning) |
| **User Feedback** | Submit bug reports/suggestions with file attachments, track resolution |
| **Central Versioning** | Auto-resolve app version via binary SHA256 hash — no manual config needed |

## CDK Activation

Exchange an activation code for a license key:

```go
result, err := sdk.Activate(
    "https://guard.example.com",
    "CDK-A1B2-C3D4-E5F6-G7H8",
    "Acme Corp",
    "admin@acme.com",
)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("License Key: %s (expires: %s)\n", result.LicenseKey, result.ExpiresAt)
```

## Configuration

```go
sdk.Config{
    // Required
    ServerURL:     "https://guard.example.com",
    LicenseKey:    "XXXXX-XXXXX-XXXXX-XXXXX",
    PublicKeyPEM:  publicKeyPEM,          // Ed25519 public key in PEM format
    ProjectSlug:   "my-project",
    ComponentSlug: "backend",

    // Optional: heartbeat interval (default: 1h)
    HeartbeatInterval: 30 * time.Minute,

    // Optional: offline grace policy
    GracePolicy: sdk.GracePolicy{
        MaxOfflineDuration: 48 * time.Hour,   // default: 72h
        WarningInterval:    2 * time.Hour,    // default: 4h
    },

    // Optional: OTA auto-update
    OTA: sdk.OTAConfig{
        Enabled:       true,
        AutoUpdate:    true,
        CheckInterval: 6 * time.Hour,        // default: 6h
        OnUpdateProgress: func(component, stage string, progress float64) {
            log.Printf("[%s] %s: %.0f%%", component, stage, progress*100)
        },
    },

    // Optional: managed frontend components
    ManagedComponents: []sdk.ManagedComponent{
        {
            Slug:     "admin-frontend",
            Dir:      "/opt/app/frontend",
            Strategy: sdk.UpdateFrontend,
            PostUpdate: func() error {
                return exec.Command("systemctl", "reload", "nginx").Run()
            },
        },
    },
}
```

## State Machine

```
INIT ──verify ok──→ ACTIVE ──heartbeat fail──→ GRACE ──timeout──→ LOCKED
                      ↑                          │
                      └──heartbeat ok────────────┘

Any state ──server ban──→ BANNED
```

| State | `Check()` returns | Description |
|-------|-------------------|-------------|
| INIT | `ErrNotActivated` | Not yet verified |
| ACTIVE | `nil` | Normal operation |
| GRACE | `nil` | Heartbeat failed, within grace period |
| LOCKED | `ErrLocked` | Offline timeout exceeded, app should stop |
| BANNED | `ErrBanned` | Banned by server admin |

## Plugin Management

```go
// Browse all available plugins (including uninstalled)
catalog, _ := guard.GetPluginCatalog(ctx, true)

// Check for updates
updates, _ := guard.CheckPluginUpdates(ctx)
for _, p := range updates {
    fmt.Printf("%s: %s → %s\n", p.Name, *p.InstalledVersion, *p.LatestVersion)
    guard.UpdatePlugin(ctx, p.Slug)
}
```

## User Feedback

```go
// Submit a bug report
feedback, _ := guard.SubmitFeedback(ctx, sdk.SubmitFeedbackRequest{
    UserID:   "user-123",
    UserName: "Alice",
    Category: sdk.FeedbackBug,
    Title:    "Slow loading on Windows",
    Content:  "First launch takes over 5 seconds on Windows 11",
})

// Upload attachment first, then reference in submission
upload, _ := guard.UploadFeedbackFile(ctx, "screenshot.png", "image/png", file)
// use upload.FileKey in SubmitFeedbackRequest.Attachments

// View release notes with resolved feedback
notes, _ := guard.FetchReleaseNotes(ctx)
```

## Version Injection

Use `ldflags` to inject build-time version info:

```bash
go build -ldflags " \
  -X 'github.com/iwen-conf/BanyanHub-SDK.Version=1.2.3' \
  -X 'github.com/iwen-conf/BanyanHub-SDK.GitCommit=$(git rev-parse --short HEAD)' \
  -X 'github.com/iwen-conf/BanyanHub-SDK.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)' \
  -X 'github.com/iwen-conf/BanyanHub-SDK.GoVersion=$(go version | cut -d\" \" -f3)' \
"
```

Or let the SDK auto-detect via central versioning:

```go
guard.AutoResolveVersion() // calculates binary SHA256, resolves version from server
```

## Error Handling

```go
if err := guard.Check(); err != nil {
    switch {
    case errors.Is(err, sdk.ErrLocked):
        log.Fatal("Offline timeout — check network connectivity")
    case errors.Is(err, sdk.ErrBanned):
        log.Fatal("Machine banned — contact administrator")
    case errors.Is(err, sdk.ErrLicenseExpired):
        log.Fatal("License expired — please renew")
    case errors.Is(err, sdk.ErrMaxMachinesExceeded):
        log.Fatal("Device limit reached — deactivate unused machines")
    default:
        log.Fatalf("Authorization error: %v", err)
    }
}
```

<details>
<summary>All exported errors (24)</summary>

| Error | Description |
|-------|-------------|
| `ErrLicenseInvalid` | License key is invalid |
| `ErrLicenseExpired` | License has expired |
| `ErrLicenseSuspended` | License is suspended |
| `ErrMachineBanned` | Machine is banned |
| `ErrMaxMachinesExceeded` | Maximum machine count exceeded |
| `ErrProjectNotAuthorized` | Project not authorized for this license |
| `ErrNetworkError` | Network communication error |
| `ErrInvalidServerResponse` | Unexpected server response |
| `ErrNotActivated` | Guard not yet activated (state: INIT) |
| `ErrLocked` | System locked (state: LOCKED) |
| `ErrBanned` | System banned (state: BANNED) |
| `ErrCDKNotFound` | Activation code not found |
| `ErrCDKAlreadyUsed` | Activation code already redeemed |
| `ErrCDKRevoked` | Activation code revoked |
| `ErrUpdateFrozen` | Update channel is frozen |
| `ErrUpdateDownload` | Update download failed |
| `ErrUpdateVerify` | Update verification failed (hash/signature) |
| `ErrUpdateApply` | Failed to apply update |
| `ErrUpdateRollback` | Rollback failed |
| `ErrUpdateConcurrent` | Another update is in progress |
| `ErrPluginNotFound` | Plugin not found |
| `ErrPluginNotManaged` | Plugin not locally managed |
| `ErrNoPluginUpdate` | No update available |
| `ErrPluginOTADisabled` | Plugin OTA is disabled |

</details>

## Testing

```bash
make all         # vet + lint + test
make test        # go test -race -covermode=atomic
make coverage    # generate HTML coverage report
```

## License

Private — **Banyan Information Technology Studio**
