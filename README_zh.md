# DeployGuard Go SDK

[English](./README.md)

DeployGuard 的 Go 客户端 SDK，为你的 Go 应用提供许可证验证、机器指纹绑定、心跳保活、OTA 自动更新和激活码 (CDK) 激活能力。

## 安装

```bash
go get github.com/iwen-conf/go-deploy-guard-sdk
```

要求 Go 1.24+。

## 快速开始

```go
package main

import (
    "context"
    "log"
    _ "embed"

    sdk "github.com/iwen-conf/go-deploy-guard-sdk"
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

    log.Println("授权验证通过，应用正常运行")
    // 你的业务代码...
}
```

## 激活码 (CDK) 激活

用激活码兑换许可证密钥：

```go
result, err := sdk.Activate(
    "https://your-api.example.com",
    "CDK-A1B2-C3D4-E5F6-G7H8",  // 激活码
    "客户公司名称",                 // 组织名
    "admin@customer.com",         // 邮箱（可选）
)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("许可证密钥: %s\n", result.LicenseKey)
```

## 完整配置

```go
sdk.Config{
    // 必填
    ServerURL:     "https://your-api.example.com",
    LicenseKey:    "XXXXX-XXXXX-XXXXX-XXXXX",
    PublicKeyPEM:  publicKeyPEM,
    ProjectSlug:   "my-project",
    ComponentSlug: "backend",

    // 可选
    HeartbeatInterval: 30 * time.Minute,  // 默认 1 小时
    GracePolicy: sdk.GracePolicy{
        MaxOfflineDuration: 48 * time.Hour,  // 默认 72 小时
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

## 状态机

```
INIT ──验证成功──→ ACTIVE ──心跳失败──→ GRACE ──超时──→ LOCKED
                    ↑                    │
                    └──心跳恢复──────────┘

任意状态 ──服务端封禁──→ BANNED
```

| 状态 | `Check()` 返回 | 说明 |
|------|----------------|------|
| ACTIVE | `nil` | 正常运行 |
| GRACE | `nil` | 心跳失败，宽限期内仍可运行 |
| LOCKED | `ErrLocked` | 离线超时，应用应停止 |
| BANNED | `ErrBanned` | 被管理员封禁 |

## 功能特性

- **许可证验证** — Ed25519 签名验证 + 本地缓存
- **心跳保活** — 定时心跳上报，带随机抖动避免流量突发
- **状态机** — INIT → ACTIVE → GRACE → LOCKED，支持 BANNED 覆盖
- **OTA 更新** — 后端二进制替换 + 前端 tar.gz 原子交换
- **激活码** — 用 CDK 激活码兑换许可证密钥
- **机器指纹** — 基于硬件 ID 的设备唯一标识

## 许可证

Private
