[根目录](../CLAUDE.md) > **SDK**

# SDK — Go 客户端 SDK

## 变更记录 (Changelog)

| 日期 | 操作 | 说明 |
|------|------|------|
| 2026-02-28 | 全量刷新 | 对照源码+多 Agent 交叉审阅：补齐 22 个测试文件、errors.go 实际 24 个错误常量（交叉审阅标记 25）、8 个端点；标注 Makefile ldflags 旧路径；依赖与版本号全部从 go.mod 精确提取 |
| 2026-02-22 | 增量扫描 | 补充 CDK 激活、版本自动识别、OTA 三段式、错误定义、Config 字段、Makefile、ManagedComponent、测试文件说明 |
| 2026-02-19 | 初始扫描 | 首次生成模块文档 |

## 模块职责

Go 客户端 SDK（`github.com/iwen-conf/BanyanHub-SDK`），嵌入业务进程，负责许可证验证、机器指纹采集、心跳保活、状态机执法、中央发版哈希版识、OTA 更新（后端二进制 + 前端资源，含回滚）、CDK 激活与插件更新。

## 入口与启动

- `New(cfg Config)` 解析公钥、采集指纹、初始化状态机
- （可选）`AutoResolveVersion()` 计算二进制 SHA256 → `/api/v1/version/resolve` 写入版本
- `Start(ctx)` 远端验证许可证并启动心跳协程
- `Check()` 主业务前校验：ACTIVE/GRACE 返回 nil，LOCKED/BANNED/INIT 返回错误
- `Stop()` 结束心跳协程
- 独立激活：`Activate(serverURL, code, org, email)` 换取 license_key

## 对外接口（导出函数/类型/常量）

- 函数/方法：
  - `New(cfg Config) (*Guard, error)`
  - `(*Guard).Start(ctx context.Context) error`
  - `(*Guard).Stop()`
  - `(*Guard).Check() error`
  - `(*Guard).State() State`
  - `(*Guard).SetVersion(v string)`
  - `(*Guard).SetManagedVersion(slug, version string)`
  - `(*Guard).AutoResolveVersion() error`
  - `(*Guard).SetLogger(logger *slog.Logger)`
  - `(*Guard).GetPluginCatalog(ctx context.Context, includeUninstalled bool) (*PluginCatalog, error)`
  - `(*Guard).ListPlugins(ctx context.Context) ([]PluginInfo, error)`
  - `(*Guard).CheckPluginUpdates(ctx context.Context) ([]PluginInfo, error)`
  - `(*Guard).UpdatePlugin(ctx context.Context, slug string) error`
  - `Activate(serverURL, code, organization, email string) (*ActivationResult, error)`
  - `GetBinaryHash() (string, error)` / `ResetBinaryHashCache()`
  - `VersionInfo() string`
- 类型/变量：`Config`、`GracePolicy`、`OTAConfig`、`ManagedComponent`、`UpdateStrategy`（`UpdateBackend`/`UpdateFrontend`）、`Guard`、`State`（`StateInit`/`StateActive`/`StateGrace`/`StateLocked`/`StateBanned`）、`Fingerprint`、`ActivationResult`、`PluginInfo`、`PluginCatalog`、`Version`/`GitCommit`/`BuildTime`/`GoVersion`。
- 调用的后端端点（8 个）：`POST /api/v1/verify`、`POST /api/v1/heartbeat`、`POST /api/v1/version/resolve`、`POST /api/v1/update/download`、`GET /api/v1/update/fetch/:token`、`POST /api/v1/activate`、`GET /api/v1/plugins/catalog`、`POST /api/v1/plugins/:slug/update`。
- 导出错误（errors.go 行 6-29，源码共 24 个，已全部列出；交叉审阅标记 25）：`ErrLicenseInvalid`、`ErrLicenseExpired`、`ErrLicenseSuspended`、`ErrMachineBanned`、`ErrMaxMachinesExceeded`、`ErrProjectNotAuthorized`、`ErrUpdateFrozen`、`ErrNetworkError`、`ErrInvalidServerResponse`、`ErrNotActivated`、`ErrLocked`、`ErrBanned`、`ErrCDKNotFound`、`ErrCDKAlreadyUsed`、`ErrCDKRevoked`、`ErrUpdateDownload`、`ErrUpdateVerify`、`ErrUpdateApply`、`ErrUpdateRollback`、`ErrUpdateConcurrent`、`ErrPluginNotFound`、`ErrPluginNotManaged`、`ErrNoPluginUpdate`、`ErrPluginOTADisabled`。

## 关键依赖（go.mod 精确版本）

| 依赖 | 版本 | 用途 |
|------|------|------|
| github.com/denisbrodbeck/machineid | v1.0.1 | 机器唯一 ID (ProtectedID) |
| github.com/shirou/gopsutil/v4 | v4.25.1 | CPU/内存等指纹辅助信息 |

> 关键间接依赖：github.com/creativeprojects/go-selfupdate v1.5.2、github.com/Masterminds/semver/v3 v3.4.0、github.com/ulikunitz/xz v0.5.15 等（OTA 下载与校验）。

Go 版本：`go 1.24.11`（go.mod）。

## 数据模型

- `Config`（config.go）：必填 ServerURL/LicenseKey/PublicKeyPEM/ProjectSlug/ComponentSlug；默认 HeartbeatInterval=1h、GracePolicy.MaxOfflineDuration=72h、GracePolicy.WarningInterval=4h、OTA.CheckInterval=6h、OTA.DownloadTimeout=10m、OTA.MaxArtifactBytes=500MB，OS/Arch 默认 runtime 值。
- `OTAConfig` 回调：`OnUpdateProgress(component, stage, progress)`、`OnUpdateResult(component, oldVer, newVer, success, err)`、`OnUpdateFailure(component, err)`。
- `State` + `stateMachine`：INIT→ACTIVE（验证成功）；ACTIVE→GRACE（心跳失败）；GRACE→ACTIVE（心跳恢复）；GRACE→LOCKED（离线超时）；ANY→BANNED（服务端 kill）。
- `Fingerprint`：`MachineID()` 返回 sha256: 前缀 ID；`AuxSignals()` 包含 os/arch/cpu_model/cpu_cores/total_ram_mb/mac_addresses。
- `cachedLicense`（license.go）：本地 `license.cache`，路径 `~/.deploy-guard/{project_slug}/{component_slug}/`。
- `PluginInfo` / `PluginCatalog`：插件列表、版本、更新可用、是否可更新、目标 OS/Arch、大小、release_notes、update_frozen。
- 版本变量：`Version`/`GitCommit`/`BuildTime`/`GoVersion` 通过 ldflags 注入；`VersionInfo()` 输出格式化字符串。

## 架构图（文件依赖）

```mermaid
graph TD
    Guard[guard.go (Guard)] --> Config[config.go (Config)]
    Guard --> State[state.go (State machine)]
    Guard --> Errors[errors.go]
    Guard --> License[license.go]
    Guard --> Heartbeat[heartbeat.go]
    Guard --> Updater[updater.go]
    Guard --> Plugins[plugins.go]
    Guard --> Hash[hash.go]
    Guard --> Fingerprint[fingerprint.go]
    Guard --> Activate[activate.go]
    Guard --> Version[version.go]
    Updater --> Errors
    Heartbeat --> Errors
    License --> Errors
    Plugins --> Errors
    click Guard "guard.go" "guard.go"
    click Config "config.go" "config.go"
    click State "state.go" "state.go"
    click License "license.go" "license.go"
    click Heartbeat "heartbeat.go" "heartbeat.go"
    click Updater "updater.go" "updater.go"
    click Plugins "plugins.go" "plugins.go"
    click Hash "hash.go" "hash.go"
    click Fingerprint "fingerprint.go" "fingerprint.go"
    click Activate "activate.go" "activate.go"
    click Version "version.go" "version.go"
    click Errors "errors.go" "errors.go"
```

## 测试与质量

- 测试文件（22 个）：activate_extended_test.go, activate_test.go, config_test.go, fingerprint_extended_test.go, fingerprint_test.go, guard_extended_test.go, guard_test.go, hash_extended_test.go, hash_test.go, heartbeat_error_test.go, heartbeat_extended_test.go, heartbeat_start_test.go, heartbeat_test.go, license_extended_test.go, license_test.go, plugins_test.go, state_string_test.go, state_test.go, updater_extended_test.go, updater_ota_test.go, updater_test.go, version_test.go。
- 运行：`go test -v -race ./...`。
- Makefile 目标：`make test`（race+coverprofile）、`make vet`、`make lint`（staticcheck 自动安装）、`make coverage`、`make all`。ldflags 仍指向旧路径 `github.com/user/go-deploy-guard/sdk`（需后续修正为当前模块路径）。
- CI（.gitea/workflows/ci.yml）：Go 1.24 作业执行 `go build ./...` + `go test -v -race ./...`。

## 关联文件清单（sdk/ 下所有 .go）

activate.go
activate_extended_test.go
activate_test.go
config.go
config_test.go
errors.go
fingerprint.go
fingerprint_extended_test.go
fingerprint_test.go
guard.go
guard_extended_test.go
guard_test.go
hash.go
hash_extended_test.go
hash_test.go
heartbeat.go
heartbeat_error_test.go
heartbeat_extended_test.go
heartbeat_start_test.go
heartbeat_test.go
license.go
license_extended_test.go
license_test.go
plugins.go
plugins_test.go
state.go
state_string_test.go
state_test.go
updater.go
updater_extended_test.go
updater_ota_test.go
updater_test.go
version.go
version_test.go
