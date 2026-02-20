[根目录](../CLAUDE.md) > **sdk**

# SDK 模块

## 模块职责

Go 客户端 SDK，嵌入到客户的 Go 应用中，提供许可证验证、心跳保活、状态机管理、OTA 自动更新（后端二进制 + 前端静态资源）、机器指纹采集等能力。

## 入口与启动

- 入口: `guard.go` - `New(cfg Config)` 构造 Guard 实例
- 启动: `Guard.Start(ctx)` 执行许可证验证并启动心跳协程
- 运行时检查: `Guard.Check()` 返回当前状态是否允许运行
- 停止: `Guard.Stop()` 取消心跳协程

## 对外接口

### 核心 API

| 方法 | 说明 |
|------|------|
| `New(cfg Config) (*Guard, error)` | 创建 Guard 实例，解析公钥、采集指纹 |
| `Start(ctx context.Context) error` | 验证许可证 + 启动心跳 |
| `Stop()` | 停止心跳 |
| `Check() error` | 检查当前状态，Active/Grace 返回 nil |
| `State() State` | 返回当前状态枚举 |
| `SetVersion(v string)` | 设置当前组件版本号 |
| `SetManagedVersion(slug, version string)` | 设置托管组件版本号 |

### 状态机 (state.go)

```
INIT -> ACTIVE (验证成功)
ACTIVE -> GRACE (心跳失败)
GRACE -> ACTIVE (心跳恢复)
GRACE -> LOCKED (离线超时)
ANY -> BANNED (服务端 kill)
```

### 调用的服务端端点

- `POST /api/v1/verify` - 许可证验证 + 机器注册
- `POST /api/v1/heartbeat` - 心跳上报 + 更新检查
- `POST /api/v1/update/download` - 请求制品下载 URL

## 关键依赖与配置

### go.mod

- `github.com/denisbrodbeck/machineid` - 机器唯一 ID
- `github.com/shirou/gopsutil/v4` - CPU/内存等系统信息

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
| OTA.Enabled | bool | false | 是否启用 OTA |
| OTA.AutoUpdate | bool | false | 是否自动更新 |

## 数据模型

- `cachedLicense` (license.go) - 本地缓存的许可证信息
- `heartbeatResponse` / `updateInfo` (heartbeat.go) - 心跳响应与更新通知
- `Fingerprint` (fingerprint.go) - 机器指纹 (machineID + auxSignals)
- 缓存路径: `~/.deploy-guard/{project_slug}/{component_slug}/license.cache`

## 测试与质量

- 当前无测试文件 (缺口)
- 建议: 添加 `guard_test.go`, `license_test.go`, `state_test.go` 等单元测试

## 常见问题 (FAQ)

- OTA 后端更新采用二进制替换 + .bak 回滚策略
- OTA 前端更新采用 tar.gz 解压 + 目录原子交换策略
- 心跳间隔带 +/-10% 随机抖动，避免服务端突发流量

## 相关文件清单

| 文件 | 说明 |
|------|------|
| `guard.go` | Guard 核心结构、New/Start/Stop/Check |
| `config.go` | Config/GracePolicy/OTAConfig/ManagedComponent |
| `state.go` | 状态机 (State enum + stateMachine) |
| `license.go` | 许可证验证与本地缓存 |
| `heartbeat.go` | 心跳协程与响应处理 |
| `updater.go` | OTA 更新 (后端二进制 + 前端静态资源) |
| `fingerprint.go` | 机器指纹采集 |
| `errors.go` | 错误定义 |
| `go.mod` | 模块依赖 |

## 变更记录 (Changelog)

| 日期 | 操作 | 说明 |
|------|------|------|
| 2026-02-19 | 初始扫描 | 首次生成模块文档 |
