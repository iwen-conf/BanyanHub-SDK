[根目录](../CLAUDE.md) > **sdk**

# SDK 模块

## 模块职责

Go 客户端 SDK，嵌入到客户的 Go 应用中，提供许可证验证、心跳保活、状态机管理、OTA 自动更新（后端二进制 + 前端静态资源）、机器指纹采集、中央发版版本自动识别、CDK 激活码激活等能力。模块名 `github.com/iwen-conf/BanyanHub-SDK`。

## 入口与启动

- 入口: `guard.go` - `New(cfg Config)` 构造 Guard 实例
- 版本识别: `Guard.AutoResolveVersion()` 通过 binary hash 自动解析版本
- 启动: `Guard.Start(ctx)` 执行许可证验证并启动心跳协程
- 运行时检查: `Guard.Check()` 返回当前状态是否允许运行
- 停止: `Guard.Stop()` 取消心跳协程
- CDK 激活: `Activate(serverURL, code, org, email)` 独立函数，激活码换许可证

## 对外接口

### 核心 API

| 方法 | 说明 |
|------|------|
| `New(cfg Config) (*Guard, error)` | 创建 Guard 实例，解析公钥、采集指纹 |
| `Start(ctx context.Context) error` | 验证许可证 + 启动心跳 |
| `Stop()` | 停止心跳 |
| `Check() error` | 检查当前状态，Active/Grace 返回 nil |
| `State() State` | 返回当前状态枚举 |
| `AutoResolveVersion() error` | 计算 binary hash 自动解析版本（中央发版系统） |
| `SetVersion(v string)` | 手动设置当前组件版本号 |
| `SetManagedVersion(slug, version string)` | 设置托管组件版本号 |
| `SetLogger(logger *slog.Logger)` | 设置日志器（默认静默） |
| `Activate(serverURL, code, org, email string) (*ActivationResult, error)` | CDK 激活码激活（独立函数） |

### 状态机 (state.go)

```
INIT -> ACTIVE (验证成功)
ACTIVE -> GRACE (心跳失败)
GRACE -> ACTIVE (心跳恢复)
GRACE -> LOCKED (离线超时，默认 72h)
ANY -> BANNED (服务端 kill)
```

状态机实现: `stateMachine` 结构 + `sync.RWMutex`，提供 `OnVerifySuccess/OnHeartbeatOK/OnHeartbeatFail/OnKill/OnGracePeriodExpired` 方法。

### 调用的服务端端点

- `POST /api/v1/verify` - 许可证验证 + 机器注册
- `POST /api/v1/heartbeat` - 心跳上报 + 更新检查
- `POST /api/v1/version/resolve` - 中央发版版本解析
- `POST /api/v1/update/download` - 请求制品下载 URL
- `GET /api/v1/update/fetch/:token` - 下载制品（流式）
- `POST /api/v1/activate` - CDK 激活码激活

### 错误定义 (errors.go)

| 错误 | 说明 |
|------|------|
| `ErrLicenseInvalid` | 许可证无效 |
| `ErrLicenseExpired` | 许可证过期 |
| `ErrLicenseSuspended` | 许可证已暂停 |
| `ErrMachineBanned` | 机器被封禁 |
| `ErrMaxMachinesExceeded` | 超出最大机器数 |
| `ErrProjectNotAuthorized` | 项目未授权 |
| `ErrUpdateFrozen` | 更新通道被冻结 |
| `ErrNetworkError` | 网络错误 |
| `ErrNotActivated` / `ErrLocked` / `ErrBanned` | 状态错误 |
| `ErrCDKNotFound` / `ErrCDKAlreadyUsed` / `ErrCDKRevoked` | CDK 激活码错误 |
| `ErrUpdateDownload` / `ErrUpdateVerify` / `ErrUpdateApply` / `ErrUpdateRollback` / `ErrUpdateConcurrent` | OTA 更新分阶段错误 |

## 关键依赖与配置

### go.mod 直接依赖

- `github.com/denisbrodbeck/machineid` v1.0.1 - 机器唯一 ID
- `github.com/shirou/gopsutil/v4` v4.25.1 - CPU/内存等系统信息

### go.mod 间接依赖（OTA 相关）

- `github.com/creativeprojects/go-selfupdate` v1.5.2 - 二进制自更新（update.Apply）
- `github.com/Masterminds/semver/v3` - 语义版本
- `github.com/ulikunitz/xz` - xz 压缩

### Config 结构 (config.go)

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| ServerURL | string | (必填) | 云端 API 地址 |
| LicenseKey | string | (必填) | 许可证密钥 |
| PublicKeyPEM | []byte | (必填) | Ed25519 公钥 PEM |
| ProjectSlug | string | (必填) | 项目标识 |
| ComponentSlug | string | (必填) | 组件标识 |
| HeartbeatInterval | Duration | 1h | 心跳间隔 |
| GracePolicy.MaxOfflineDuration | Duration | 72h | 最大离线容忍时间 |
| GracePolicy.WarningInterval | Duration | 4h | 警告间隔 |
| OTA.Enabled | bool | false | 是否启用 OTA |
| OTA.AutoUpdate | bool | false | 是否自动更新 |
| OTA.CheckInterval | Duration | 6h | 更新检查间隔 |
| OTA.OS | string | runtime.GOOS | 目标操作系统 |
| OTA.Arch | string | runtime.GOARCH | 目标架构 |
| OTA.DownloadTimeout | Duration | 10min | 下载超时 |
| OTA.MaxArtifactBytes | int64 | 500MB | 最大制品大小 |
| OTA.OnUpdateProgress | func | nil | 更新进度回调 |
| OTA.OnUpdateResult | func | nil | 更新结果回调 |
| OTA.OnUpdateFailure | func | nil | 更新失败回调 |
| ManagedComponents | []ManagedComponent | [] | 托管组件列表（前端资源等） |

### ManagedComponent 结构

| 字段 | 说明 |
|------|------|
| Slug | 组件标识 |
| Dir | 目标目录 |
| Strategy | UpdateBackend (二进制替换) / UpdateFrontend (tar.gz 解压) |
| PostUpdate | 更新后钩子函数 |

## 数据模型

- `cachedLicense` (license.go) - 本地缓存的许可证信息（license_key, public_data, signature, verified_at）
- `heartbeatResponse` / `updateInfo` (heartbeat.go) - 心跳响应与更新通知
- `Fingerprint` (fingerprint.go) - 机器指纹 (sha256(machineID) + auxSignals: os, arch, cpu, ram, mac)
- `ActivationResult` (activate.go) - CDK 激活结果（license_key, project_slug, expires_at）
- 缓存路径: `~/.deploy-guard/{project_slug}/{component_slug}/license.cache`

### 版本自动识别 (hash.go)

- `GetBinaryHash()` 计算当前可执行文件的 SHA256 hash，结果被 `sync.Once` 缓存
- `ResetBinaryHashCache()` 重置缓存（测试或运行时替换后使用）
- hash 发送到 `/api/v1/version/resolve` 端点，服务端查 `artifact_versions` 表返回版本信息

### 版本信息注入 (version.go)

- `Version`, `GitCommit`, `BuildTime`, `GoVersion` 通过 ldflags 在构建时注入
- `VersionInfo()` 返回格式化版本字符串

## 测试与质量

- 测试文件: 7 个（`guard_test.go`, `license_test.go`, `state_test.go`, `activate_test.go`, `fingerprint_test.go`, `heartbeat_test.go`, `updater_test.go`）
- 运行: `go test -v -race ./...`
- Makefile: `make test` (race + coverage), `make vet`, `make lint` (staticcheck), `make coverage`
- 技术债治理计划: `sdk/.claude/team-plan/refactor-ota-and-eliminate-tech-debt.md`（17 个子任务）

## 常见问题 (FAQ)

- OTA 后端更新: 使用 `go-selfupdate/update.Apply` 原子替换 + .bak 回滚
- OTA 前端更新: tar.gz 流式解压 + SHA256 校验 + 目录原子交换 + .bak 回滚
- 心跳间隔带 +/-10% 随机抖动（`0.9 + rand*0.2`），避免服务端突发流量
- 并发更新保护: `updateMu sync.Mutex` 防止同时执行多个更新
- 签名校验: Ed25519 签名验证（SHA256(data) -> ed25519.Verify），确保制品完整性

## 相关文件清单

| 文件 | 说明 |
|------|------|
| `guard.go` | Guard 核心结构、New/Start/Stop/Check/AutoResolveVersion/SetLogger |
| `config.go` | Config/GracePolicy/OTAConfig/ManagedComponent/UpdateStrategy |
| `state.go` | 状态机 (State enum + stateMachine + 5 个状态转换方法) |
| `license.go` | 许可证验证（云端+本地缓存）与 Ed25519 签名校验 |
| `heartbeat.go` | 心跳协程、heartbeatResponse/updateInfo 结构、grace period |
| `updater.go` | OTA 更新 (updateBackend 三段式 + updateFrontend tar.gz 解压) |
| `fingerprint.go` | 机器指纹采集 (machineID + CPU/RAM/MAC/OS/Arch) |
| `hash.go` | 二进制 SHA256 哈希计算（中央发版系统使用） |
| `version.go` | 版本信息（ldflags 注入） |
| `activate.go` | CDK 激活码激活函数 |
| `errors.go` | 错误定义（18 个错误常量） |
| `Makefile` | 质量门禁 (test/vet/lint/coverage) |
| `go.mod` | 模块依赖 |
| `*_test.go` (7 个) | 单元测试 |

## 变更记录 (Changelog)

| 日期 | 操作 | 说明 |
|------|------|------|
| 2026-02-22 | 增量扫描 | 完善文档：补充 CDK 激活、版本自动识别、OTA 三段式、错误定义（18 个）、Config 完整字段、Makefile、ManagedComponent、测试文件（7 个）；新增 hash.go/version.go/activate.go 说明 |
| 2026-02-19 | 初始扫描 | 首次生成模块文档 |
