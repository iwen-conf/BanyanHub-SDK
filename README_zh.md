# BanyanHub SDK

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Private-red)](LICENSE)

[English](./README.md)

BanyanHub 的 Go 客户端 SDK —— 企业级中央发版系统。将此 SDK 嵌入你的 Go 应用，即可获得许可证验证、机器指纹绑定、心跳保活、OTA 自动更新、插件管理、用户反馈和激活码 (CDK) 激活能力。

## 安装

```bash
go get github.com/iwen-conf/BanyanHub-SDK
```

要求 **Go 1.24+**。

## 快速开始

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

    log.Println("授权验证通过，应用正常运行")
    // 你的业务代码...
}
```

## 功能特性

| 功能 | 说明 |
|------|------|
| **许可证验证** | Ed25519 签名验证 + 本地缓存 + 离线宽限期 |
| **机器指纹** | 基于硬件的设备绑定（Machine ID + CPU/内存/MAC 辅助信号） |
| **心跳保活** | 定时状态上报，带随机抖动避免流量突发 |
| **状态机** | 优雅降级：INIT → ACTIVE → GRACE → LOCKED，支持 BANNED 覆盖 |
| **OTA 更新** | 后端二进制原子替换 + 前端 tar.gz 目录交换 + 自动回滚 |
| **插件系统** | 插件发现、版本管理、独立更新控制 |
| **激活码 (CDK)** | 用激活码自助兑换许可证密钥 |
| **用户反馈** | 提交 Bug/建议/问题，附件上传，追踪解决状态 |
| **中央发版** | 通过二进制 SHA256 哈希自动识别版本，无需手动配置 |

## 激活码 (CDK) 激活

用激活码兑换许可证密钥：

```go
result, err := sdk.Activate(
    "https://guard.example.com",
    "CDK-A1B2-C3D4-E5F6-G7H8",  // 激活码
    "客户公司名称",                 // 组织名
    "admin@customer.com",         // 邮箱（可选）
)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("许可证密钥: %s（有效期至: %s）\n", result.LicenseKey, result.ExpiresAt)
```

## 完整配置

```go
sdk.Config{
    // 必填
    ServerURL:     "https://guard.example.com",
    LicenseKey:    "XXXXX-XXXXX-XXXXX-XXXXX",
    PublicKeyPEM:  publicKeyPEM,          // Ed25519 公钥（PEM 格式）
    ProjectSlug:   "my-project",
    ComponentSlug: "backend",

    // 可选：心跳间隔（默认 1 小时）
    HeartbeatInterval: 30 * time.Minute,

    // 可选：离线宽限策略
    GracePolicy: sdk.GracePolicy{
        MaxOfflineDuration: 48 * time.Hour,   // 默认 72 小时
        WarningInterval:    2 * time.Hour,    // 默认 4 小时
    },

    // 可选：OTA 自动更新
    OTA: sdk.OTAConfig{
        Enabled:       true,
        AutoUpdate:    true,
        CheckInterval: 6 * time.Hour,        // 默认 6 小时
        OnUpdateProgress: func(component, stage string, progress float64) {
            log.Printf("[%s] %s: %.0f%%", component, stage, progress*100)
        },
    },

    // 可选：托管前端组件
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

## 状态机

```
INIT ──验证成功──→ ACTIVE ──心跳失败──→ GRACE ──超时──→ LOCKED
                    ↑                    │
                    └──心跳恢复──────────┘

任意状态 ──服务端封禁──→ BANNED
```

| 状态 | `Check()` 返回 | 说明 |
|------|----------------|------|
| INIT | `ErrNotActivated` | 未验证 |
| ACTIVE | `nil` | 正常运行 |
| GRACE | `nil` | 心跳失败，宽限期内仍可运行 |
| LOCKED | `ErrLocked` | 离线超时，应用应停止 |
| BANNED | `ErrBanned` | 被管理员封禁 |

## 插件管理

```go
// 浏览所有可用插件（含未安装的）
catalog, _ := guard.GetPluginCatalog(ctx, true)

// 检查可用更新
updates, _ := guard.CheckPluginUpdates(ctx)
for _, p := range updates {
    fmt.Printf("%s: %s → %s\n", p.Name, *p.InstalledVersion, *p.LatestVersion)
    guard.UpdatePlugin(ctx, p.Slug)
}
```

## 用户反馈

```go
// 提交 Bug 报告
feedback, _ := guard.SubmitFeedback(ctx, sdk.SubmitFeedbackRequest{
    UserID:   "user-123",
    UserName: "张三",
    Category: sdk.FeedbackBug,
    Title:    "Windows 上加载缓慢",
    Content:  "首次启动应用，界面加载需要 5 秒以上",
})

// 先上传附件，再在提交时引用
upload, _ := guard.UploadFeedbackFile(ctx, "screenshot.png", "image/png", file)
// 在 SubmitFeedbackRequest.Attachments 中使用 upload.FileKey

// 查看发版说明（含已解决的反馈）
notes, _ := guard.FetchReleaseNotes(ctx)
```

## 版本注入

通过 `ldflags` 注入构建时版本信息：

```bash
go build -ldflags " \
  -X 'github.com/iwen-conf/BanyanHub-SDK.Version=1.2.3' \
  -X 'github.com/iwen-conf/BanyanHub-SDK.GitCommit=$(git rev-parse --short HEAD)' \
  -X 'github.com/iwen-conf/BanyanHub-SDK.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)' \
  -X 'github.com/iwen-conf/BanyanHub-SDK.GoVersion=$(go version | cut -d\" \" -f3)' \
"
```

或者让 SDK 通过中央发版系统自动识别：

```go
guard.AutoResolveVersion() // 计算二进制 SHA256，从服务端解析版本号
```

## 错误处理

```go
if err := guard.Check(); err != nil {
    switch {
    case errors.Is(err, sdk.ErrLocked):
        log.Fatal("离线超时 — 请检查网络连接")
    case errors.Is(err, sdk.ErrBanned):
        log.Fatal("设备已封禁 — 请联系管理员")
    case errors.Is(err, sdk.ErrLicenseExpired):
        log.Fatal("许可证已过期 — 请续期")
    case errors.Is(err, sdk.ErrMaxMachinesExceeded):
        log.Fatal("设备数超限 — 请停用闲置设备")
    default:
        log.Fatalf("授权异常: %v", err)
    }
}
```

<details>
<summary>全部导出错误（24 个）</summary>

| 错误 | 说明 |
|------|------|
| `ErrLicenseInvalid` | 许可证无效 |
| `ErrLicenseExpired` | 许可证已过期 |
| `ErrLicenseSuspended` | 许可证已暂停 |
| `ErrMachineBanned` | 机器已封禁 |
| `ErrMaxMachinesExceeded` | 超过最大机器数限制 |
| `ErrProjectNotAuthorized` | 项目未授权 |
| `ErrNetworkError` | 网络错误 |
| `ErrInvalidServerResponse` | 无效的服务器响应 |
| `ErrNotActivated` | Guard 未激活（状态: INIT） |
| `ErrLocked` | 系统已锁定（状态: LOCKED） |
| `ErrBanned` | 系统已封禁（状态: BANNED） |
| `ErrCDKNotFound` | 激活码不存在 |
| `ErrCDKAlreadyUsed` | 激活码已使用 |
| `ErrCDKRevoked` | 激活码已撤销 |
| `ErrUpdateFrozen` | 更新通道已冻结 |
| `ErrUpdateDownload` | 下载失败 |
| `ErrUpdateVerify` | 验证失败（哈希或签名） |
| `ErrUpdateApply` | 应用更新失败 |
| `ErrUpdateRollback` | 回滚失败 |
| `ErrUpdateConcurrent` | 并发更新（正在执行更新） |
| `ErrPluginNotFound` | 插件不存在 |
| `ErrPluginNotManaged` | 插件不在本地管理 |
| `ErrNoPluginUpdate` | 没有可用更新 |
| `ErrPluginOTADisabled` | 插件 OTA 已禁用 |

</details>

## 测试

```bash
make all         # vet + lint + test
make test        # go test -race -covermode=atomic
make coverage    # 生成 HTML 覆盖率报告
```

## 许可证

Private — **小榕树信息技术工作室**
