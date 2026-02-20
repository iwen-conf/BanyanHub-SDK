# Team Plan: Refactor OTA Update & Eliminate Tech Debt

## 概述

用 go-selfupdate 重构 updateBackend() 函数，替换手动二进制替换逻辑，同时全面消除项目技术债（测试、日志、错误处理、并发安全、安全漏洞），确保代码达到生产级别标准。

## Codex 分析摘要

**技术可行性**：✅ 可行且建议落地
- 推荐使用 `update.Apply`（低层 API）而非 `selfupdate.UpdateTo`（高层 API）
- 原因：保留现有下载/校验流程，提供原子替换与回滚能力
- 当前痛点：手动 rename/copy 跨平台鲁棒性不足，错误无观测

**关键架构改进**：
1. updateBackend 重构为三段：requestDownloadMeta → downloadArtifactWithProgress → applyBackendBinaryWithSelfupdate
2. Guard 增加更新互斥锁和日志器字段，消除版本号竞态读写
3. OTAConfig 补齐生产配置：DownloadTimeout、MaxArtifactBytes、回调函数
4. errors.go 新增更新分阶段错误（下载、校验、应用、回滚）
5. 全面测试覆盖（当前 0.0%）

**已识别技术债**：
- license 缓存逻辑不可用（写缓存未写签名，读缓存却校验签名）
- OTA.CheckInterval 和 GracePolicy.WarningInterval 未被使用
- postJSON 未携带 context，超时控制不够细粒度
- updateFrontend 路径穿越校验不严格
- 后端更新未使用 signature 字段校验

## Gemini 分析摘要

（纯后端 Go SDK 项目，无 UI/UX 组件，Gemini 分析不适用）

**SDK 使用者体验考虑**：
- 需要更新进度回调（下载进度、应用状态）
- 需要更新结果通知（成功/失败/回滚）
- 需要可注入的日志器（默认 noop，可选 slog.Logger）
- 需要更新失败的详细错误信息（阶段、原因、是否可重试）

## 技术方案

### 核心重构策略

1. **updateBackend 三段式重构**
   - 第一段：requestDownloadMeta - 请求下载元数据（URL、SHA256、Signature）
   - 第二段：downloadArtifactWithProgress - 下载制品并校验 SHA256
   - 第三段：applyBackendBinaryWithSelfupdate - 使用 update.Apply 原子替换

2. **并发安全与状态管理**
   - Guard 增加 updateMu sync.Mutex（防并发更新）
   - Guard 增加 logger *slog.Logger（可注入日志器）
   - 版本号读写统一走 mu 保护

3. **错误处理与可观测性**
   - 新增分阶段错误类型（ErrUpdateDownload、ErrUpdateVerify、ErrUpdateApply、ErrUpdateRollback）
   - 所有错误包装包含 component/version/stage 信息
   - 更新事件回调（OnUpdateProgress、OnUpdateResult、OnUpdateFailure）

4. **安全加固**
   - 后端更新补签名校验（使用 Ed25519 验证 signature 字段）
   - 前端更新严格路径校验（防路径穿越）
   - 下载响应校验 HTTP 状态码和大小上限

5. **测试体系建设**
   - 新增 7 个测试文件，覆盖关键场景
   - 质量门禁：go test -race -covermode=atomic、go vet、staticcheck

## 子任务列表

### Task 1: 错误类型与配置扩展
- **类型**: 后端
- **文件范围**:
  - `errors.go` - 新增更新分阶段错误
  - `config.go` - 扩展 OTAConfig 结构
- **依赖**: 无
- **实施步骤**:
  1. 在 errors.go 新增：ErrUpdateDownload、ErrUpdateVerify、ErrUpdateApply、ErrUpdateRollback、ErrUpdateConcurrent
  2. 在 config.go 的 OTAConfig 新增字段：
     - DownloadTimeout time.Duration
     - MaxArtifactBytes int64
     - OnUpdateProgress func(component, stage string, progress float64)
     - OnUpdateResult func(component, oldVer, newVer string, success bool, err error)
     - OnUpdateFailure func(component string, err error)
  3. 在 setDefaults() 中设置默认值（DownloadTimeout: 10min, MaxArtifactBytes: 500MB）
- **验收标准**:
  - 新增 5 个错误常量
  - OTAConfig 新增 5 个字段
  - setDefaults() 设置默认值

### Task 2: Guard 结构扩展与并发安全
- **类型**: 后端
- **文件范围**:
  - `guard.go` - 扩展 Guard 结构，修改 New/SetVersion/SetManagedVersion
- **依赖**: 无
- **实施步骤**:
  1. Guard 结构新增字段：
     - updateMu sync.Mutex（更新互斥锁）
     - logger *slog.Logger（日志器，默认 slog.New(slog.NewTextHandler(io.Discard, nil))）
  2. 修改 SetVersion 和 SetManagedVersion，确保已加锁
  3. 在 New() 中初始化 logger（默认 noop logger）
  4. 新增 SetLogger(logger *slog.Logger) 方法
- **验收标准**:
  - Guard 新增 updateMu 和 logger 字段
  - SetVersion/SetManagedVersion 使用 mu 保护
  - 新增 SetLogger 方法

### Task 3: 重构 updateBackend 函数
- **类型**: 后端
- **文件范围**:
  - `updater.go` - 重构 updateBackend 函数（第 34-106 行）
- **依赖**: Task 1, Task 2
- **实施步骤**:
  1. 将 updateBackend 拆分为三个私有函数：
     - requestDownloadMeta(component, version, platform string) (url, sha256, signature string, err error)
     - downloadArtifactWithProgress(url string, maxBytes int64) (tmpPath, actualSHA256 string, err error)
     - applyBackendBinaryWithSelfupdate(tmpPath, targetPath string) error
  2. 在 updateBackend 中：
     - 加 updateMu 锁（defer unlock）
     - 调用三个函数
     - 每阶段触发 OnUpdateProgress 回调
     - 成功后触发 OnUpdateResult 回调
     - 失败后触发 OnUpdateFailure 回调
     - 所有错误包装包含 component/version/stage 信息
  3. applyBackendBinaryWithSelfupdate 使用 update.Apply：
     - 设置 OldSavePath 为 exe + ".bak"
     - 调用 update.Apply(tmpPath, update.Options{...})
     - 处理 update.RollbackError
  4. 补签名校验（使用 g.publicKey 验证 signature）
  5. 使用 g.httpClient 而非 http.Get
  6. 所有阶段输出结构化日志（g.logger）
- **验收标准**:
  - updateBackend 拆分为三个函数
  - 使用 update.Apply 替换手动 rename/copy
  - 补签名校验
  - 触发所有回调
  - 输出结构化日志

### Task 4: 修复 handleUpdateNotification 策略分发
- **类型**: 后端
- **文件范围**:
  - `updater.go` - 修改 handleUpdateNotification 函数（第 15-32 行）
- **依赖**: Task 3
- **实施步骤**:
  1. 检查 ManagedComponent.Strategy 字段
  2. 根据 UpdateBackend/UpdateFrontend 路由到对应函数
  3. 当前逻辑忽略了 Strategy，需要修复
- **验收标准**:
  - 根据 Strategy 正确路由
  - 支持 UpdateBackend 和 UpdateFrontend 两种策略

### Task 5: 治理 updateFrontend 函数
- **类型**: 后端
- **文件范围**:
  - `updater.go` - 治理 updateFrontend 函数（第 108-214 行）
- **依赖**: Task 1, Task 2
- **实施步骤**:
  1. 补 HTTP 状态码校验（httpResp.StatusCode != 200 返回错误）
  2. 补 io.Copy 错误处理（第 179 行当前忽略错误）
  3. 强化路径穿越校验（改为 strings.HasPrefix(filepath.Clean(target), filepath.Clean(tmpDir)+string(os.PathSeparator))）
  4. 补大小限制检查（使用 io.LimitReader）
  5. 加 updateMu 锁
  6. 触发回调和日志
- **验收标准**:
  - HTTP 状态码校验
  - io.Copy 错误处理
  - 严格路径穿越校验
  - 大小限制检查
  - 加锁和回调

### Task 6: 修复 license 缓存逻辑
- **类型**: 后端
- **文件范围**:
  - `license.go` - 修复缓存读写逻辑
- **依赖**: 无
- **实施步骤**:
  1. 在 cachedLicense 结构中新增 Signature 字段
  2. 写缓存时保存 signature（第 83 行）
  3. 读缓存时使用保存的 signature 校验（第 23 行）
- **验收标准**:
  - cachedLicense 新增 Signature 字段
  - 写缓存保存 signature
  - 读缓存使用 signature 校验

### Task 7: 修复 heartbeat 版本号竞态读写
- **类型**: 后端
- **文件范围**:
  - `heartbeat.go` - 修改 sendHeartbeat 函数（第 49-91 行）
- **依赖**: Task 2
- **实施步骤**:
  1. 在 sendHeartbeat 开始时，加锁读取 g.version 和 g.managedVersions 到局部变量
  2. 使用局部变量构建 components 数组
  3. 避免在构建请求时直接读取 g.version 和 g.managedVersions
- **验收标准**:
  - 版本号读取加锁
  - 使用局部变量快照

### Task 8: 补齐 postJSON context 支持
- **类型**: 后端
- **文件范围**:
  - `guard.go` - 修改 postJSON 函数（第 134-162 行）
- **依赖**: 无
- **实施步骤**:
  1. postJSON 签名改为 postJSON(ctx context.Context, path string, body any, result any) error
  2. 使用 http.NewRequestWithContext(ctx, ...)
  3. 更新所有调用点（verifyLicense、sendHeartbeat、updateBackend、updateFrontend、Activate）
- **验收标准**:
  - postJSON 支持 context
  - 所有调用点更新

### Task 9: 测试文件 - updater_test.go
- **类型**: 后端
- **文件范围**:
  - `updater_test.go` - 新建测试文件
- **依赖**: Task 3, Task 4, Task 5
- **实施步骤**:
  1. 测试 updateBackend 成功场景
  2. 测试 updateBackend 下载失败
  3. 测试 updateBackend SHA256 不匹配
  4. 测试 updateBackend 签名校验失败
  5. 测试 updateBackend Apply 失败
  6. 测试 updateBackend 并发更新（应被互斥锁阻止）
  7. 测试 updateFrontend 成功场景
  8. 测试 updateFrontend 路径穿越攻击
  9. 测试 updateFrontend 大小超限
  10. 测试回调函数被正确触发
- **验收标准**:
  - 至少 10 个测试用例
  - 覆盖成功和失败场景
  - 使用 httptest 模拟服务端

### Task 10: 测试文件 - heartbeat_test.go
- **类型**: 后端
- **文件范围**:
  - `heartbeat_test.go` - 新建测试文件
- **依赖**: Task 7
- **实施步骤**:
  1. 测试 sendHeartbeat 成功
  2. 测试 sendHeartbeat 网络错误（进入 GRACE）
  3. 测试 sendHeartbeat kill 指令（进入 BANNED）
  4. 测试 grace period 超时（进入 LOCKED）
  5. 测试 heartbeat 恢复（GRACE → ACTIVE）
  6. 测试更新通知处理
- **验收标准**:
  - 至少 6 个测试用例
  - 覆盖状态机转换

### Task 11: 测试文件 - license_test.go
- **类型**: 后端
- **文件范围**:
  - `license_test.go` - 新建测试文件
- **依赖**: Task 6
- **实施步骤**:
  1. 测试 verifyLicense 成功
  2. 测试 verifyLicense 签名校验失败
  3. 测试 verifyLicense 过期
  4. 测试 verifyLicense 项目未授权
  5. 测试缓存读写
  6. 测试缓存签名校验
- **验收标准**:
  - 至少 6 个测试用例
  - 覆盖缓存逻辑

### Task 12: 测试文件 - state_test.go
- **类型**: 后端
- **文件范围**:
  - `state_test.go` - 新建测试文件
- **依赖**: 无
- **实施步骤**:
  1. 测试状态机初始状态
  2. 测试 INIT → ACTIVE
  3. 测试 ACTIVE → GRACE
  4. 测试 GRACE → ACTIVE
  5. 测试 GRACE → LOCKED
  6. 测试 ANY → BANNED
- **验收标准**:
  - 至少 6 个测试用例
  - 覆盖所有状态转换

### Task 13: 测试文件 - activate_test.go
- **类型**: 后端
- **文件范围**:
  - `activate_test.go` - 新建测试文件
- **依赖**: 无
- **实施步骤**:
  1. 测试 Activate 成功
  2. 测试 Activate CDK 不存在
  3. 测试 Activate CDK 已使用
  4. 测试 Activate CDK 已撤销
  5. 测试 Activate 网络错误
- **验收标准**:
  - 至少 5 个测试用例
  - 使用 httptest 模拟服务端

### Task 14: 测试文件 - guard_test.go
- **类型**: 后端
- **文件范围**:
  - `guard_test.go` - 新建测试文件
- **依赖**: Task 2
- **实施步骤**:
  1. 测试 New 成功
  2. 测试 New 参数校验
  3. 测试 Start 成功
  4. 测试 Start 许可证验证失败
  5. 测试 Check 各状态返回值
  6. 测试 Stop 幂等性
  7. 测试 SetLogger
- **验收标准**:
  - 至少 7 个测试用例
  - 覆盖核心 API

### Task 15: 测试文件 - fingerprint_test.go
- **类型**: 后端
- **文件范围**:
  - `fingerprint_test.go` - 新建测试文件
- **依赖**: 无
- **实施步骤**:
  1. 测试 collectFingerprint 成功
  2. 测试 MachineID 返回值
  3. 测试 AuxSignals 包含 CPU/内存信息
- **验收标准**:
  - 至少 3 个测试用例

### Task 16: 质量门禁与 CI 配置
- **类型**: 后端
- **文件范围**:
  - `.github/workflows/test.yml` - 新建 CI 配置（如果使用 GitHub Actions）
  - `Makefile` - 新建或更新 Makefile
- **依赖**: Task 9-15
- **实施步骤**:
  1. 创建 Makefile，包含：
     - test: go test ./... -race -covermode=atomic -coverprofile=coverage.out
     - vet: go vet ./...
     - lint: staticcheck ./...
     - coverage: go tool cover -html=coverage.out
  2. 如果使用 GitHub Actions，创建 .github/workflows/test.yml
  3. 配置质量门禁：覆盖率 >= 80%
- **验收标准**:
  - Makefile 包含 test/vet/lint/coverage 目标
  - 所有测试通过
  - 覆盖率 >= 80%

### Task 17: 文档更新
- **类型**: 文档
- **文件范围**:
  - `README.md` - 更新示例代码
  - `README_zh.md` - 更新中文文档
  - `CLAUDE.md` - 更新模块文档
- **依赖**: Task 1-16
- **实施步骤**:
  1. 更新 README.md 示例代码，展示新的回调配置
  2. 更新 README_zh.md 对应内容
  3. 更新 CLAUDE.md，记录本次重构内容
  4. 新增 CHANGELOG.md，记录 v0.3.0 变更
- **验收标准**:
  - README 示例代码包含回调配置
  - CLAUDE.md 更新变更记录
  - CHANGELOG.md 新增 v0.3.0 条目

## 文件冲突检查

✅ 无冲突 - 所有子任务的文件范围已通过依赖关系隔离

**潜在冲突点**：
- updater.go 被 Task 3, 4, 5 修改 → 通过依赖关系串行化（Task 4 依赖 Task 3，Task 5 独立）
- guard.go 被 Task 2, 8 修改 → 文件不同部分，可并行
- errors.go 和 config.go 被 Task 1 修改 → 单一任务内完成

## 并行分组

- **Layer 1 (并行)**: Task 1, Task 2, Task 6, Task 8
  - 4 个任务，无依赖，可并行执行

- **Layer 2 (依赖 Layer 1)**: Task 3, Task 5, Task 7
  - Task 3 依赖 Task 1, 2
  - Task 5 依赖 Task 1, 2
  - Task 7 依赖 Task 2

- **Layer 3 (依赖 Layer 2)**: Task 4
  - Task 4 依赖 Task 3

- **Layer 4 (依赖 Layer 1-3)**: Task 9, Task 10, Task 11, Task 12, Task 13, Task 14, Task 15
  - 7 个测试任务，可并行执行
  - Task 9 依赖 Task 3, 4, 5
  - Task 10 依赖 Task 7
  - Task 11 依赖 Task 6
  - Task 12, 13, 14, 15 无特殊依赖

- **Layer 5 (依赖 Layer 4)**: Task 16
  - Task 16 依赖所有测试任务

- **Layer 6 (依赖 Layer 5)**: Task 17
  - Task 17 依赖所有代码和测试完成

**预估并行度**：
- Layer 1: 4 个 Builder
- Layer 2: 3 个 Builder
- Layer 3: 1 个 Builder
- Layer 4: 7 个 Builder
- Layer 5: 1 个 Builder
- Layer 6: 1 个 Builder

**总计**: 17 个子任务，最多需要 7 个并行 Builder

## 风险评估

1. **更新损坏或替换失败**
   - 缓解：update.Apply + OldSavePath + update.RollbackError 分级告警

2. **并发更新导致状态错乱**
   - 缓解：组件级互斥、同版本幂等保护

3. **制品安全问题（篡改/路径穿越）**
   - 缓解：SHA256+签名双校验、前端解包路径白名单、大小限制

4. **可观测性不足导致问题不可诊断**
   - 缓解：结构化日志 + 更新事件回调 + 失败分类错误码

5. **测试覆盖率不足**
   - 缓解：17 个子任务中 7 个是测试任务，质量门禁要求 >= 80% 覆盖率

## 实施建议

1. **优先级排序**：
   - P0（必须）：Task 1-8（核心重构和安全修复）
   - P1（重要）：Task 9-16（测试和质量门禁）
   - P2（建议）：Task 17（文档更新）

2. **并行策略**：
   - 使用 ccg:team-exec 启动多个 Builder teammates
   - Layer 1 启动 4 个 Builder 并行执行
   - 每层完成后启动下一层

3. **验收标准**：
   - 所有测试通过（go test ./... -race）
   - 覆盖率 >= 80%
   - go vet 无警告
   - staticcheck 无问题
   - 代码审查通过（ccg:team-review）

4. **发布策略**：
   - 完成后打 tag v0.3.0
   - 更新 CHANGELOG.md
   - 推送到远程仓库
