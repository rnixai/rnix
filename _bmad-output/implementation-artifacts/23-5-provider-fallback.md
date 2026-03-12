# Story 23.5: Provider Fallback 降级机制

Status: done

## Story

As a 用户,
I want 当首选 provider 调用失败时自动切换到备选 provider,
So that 智能体任务不会因单个 provider 故障而中断。

## Acceptance Criteria

1. **Given** Agent `agent.yaml` 配置了 `models.preferred: sonnet` + `models.fallback: haiku`（同 provider 内模型降级）
   **When** preferred 模型调用返回 `ErrModelNotFound`
   **Then** 自动使用 fallback 模型重试

2. **Given** Agent 配置了跨 provider fallback（如 `provider: ollama` + fallback 对应 claude）
   **When** Ollama 调用失败（HTTP 5xx、连接超时、连接拒绝、认证失败）
   **Then** 自动切换到 claude provider 的 fallback 模型
   **And** 切换延迟（从检测失败到发起 fallback 调用）<= 1 秒（NFR33）

3. **Given** Fallback 也失败
   **When** 所有配置的 provider 均不可用
   **Then** 进程转为 Zombie 状态，错误信息包含所有尝试过的 provider 列表和各自失败原因

4. **Given** Fallback 成功
   **When** 任务完成
   **Then** strace 输出中可见 provider 切换事件：`[fallback] /dev/llm/ollama failed (connection refused) -> /dev/llm/claude`

5. **Given** Agent 未配置 fallback（`models.fallback` 为空）
   **When** 首选 provider 调用失败
   **Then** 直接报错，不尝试 fallback

## Tasks / Subtasks

### Task 1: 扩展 `AgentModels` 支持跨 provider fallback（AC: #2）

- [x] 1.1 在 `agents/types.go` 的 `AgentModels` 结构体中新增 `FallbackProvider` 字段：
  ```go
  type AgentModels struct {
      Provider         string `yaml:"provider"`
      Preferred        string `yaml:"preferred"`
      Fallback         string `yaml:"fallback"`
      FallbackProvider string `yaml:"fallback_provider"` // 跨 provider fallback；空 = 使用同 provider
  }
  ```
  **关键设计：**
  - `FallbackProvider` 为空时，fallback 模型使用与 `Provider` 相同的 provider（同 provider 内降级）
  - `FallbackProvider` 非空时，fallback 使用指定的 provider（跨 provider 降级）
  - 这保持了与现有 `Fallback` 字段的向后兼容性

- [x] 1.2 在 `agents/testdata/mock-agent/agent.yaml` 验证 YAML 解析正确性（已有 `fallback: haiku`，无需修改）

- [x] 1.3 新增 testdata fixture `agents/testdata/cross-provider-agent/agent.yaml` 用于跨 provider fallback 测试：
  ```yaml
  name: cross-provider-agent
  description: "Agent with cross-provider fallback"
  models:
    provider: ollama
    preferred: llama3
    fallback: haiku
    fallback_provider: claude
  skills: []
  ```

### Task 2: 在 `reasonStep` 中实现 fallback 逻辑（AC: #1, #2, #3, #5）

- [x] 2.1 修改 `kernel/kernel.go` 中的 `reasonStep` 函数，在 LLM Write 失败时检查 fallback 配置。核心逻辑：

  当 `k.vfs.Write(proc.ctx, proc.PID, llmFD, reqJSON)` 返回错误时：
  1. 检查进程是否有 fallback 配置（需要在 `Process` 中增加 fallback 信息）
  2. 如果有 fallback，打开 fallback provider 的 VFS 设备
  3. 修改请求中的 model 字段为 fallback model
  4. 通过 fallback 设备重试调用
  5. 如果 fallback 也失败，进入 Zombie 状态并报告完整错误链

- [x] 2.2 在 `kernel/process.go` 的 `Process` 结构体中新增 fallback 相关字段：
  ```go
  // Fallback configuration (Story 23.5)
  FallbackModel    string // fallback model name
  FallbackProvider string // fallback provider name; "" = same as primary
  FallbackDevice   string // resolved fallback VFS device path; "" = no fallback
  ```

- [x] 2.3 在 `kernel/kernel.go` 的 `Spawn` 函数中解析 fallback 配置并存入 Process：
  - 在 agent 信息加载部分（约 L295-325）添加 fallback 解析：
  ```go
  // Fallback configuration (Story 23.5)
  if agent != nil && agent.Manifest.Models.Fallback != "" {
      proc.FallbackModel = agent.Manifest.Models.Fallback
      fbProvider := agent.Manifest.Models.FallbackProvider
      if fbProvider == "" {
          fbProvider = provider // 同 provider 内降级，使用主 provider
      }
      proc.FallbackProvider = fbProvider
      fbDevice, fbErr := k.resolveLLMDevice(nil, fbProvider)
      if fbErr == nil {
          proc.FallbackDevice = fbDevice
      }
      // fallback 解析失败不阻断 spawn，仅意味着 fallback 不可用
  }
  ```
  这里 `provider` 是 Spawn 时已解析的主 provider 名称。

- [x] 2.4 实现 fallback 调用辅助函数 `attemptFallback`：
  ```go
  // attemptFallback tries the fallback provider when primary LLM call fails.
  // Returns the response data and nil error on success, or nil and error if fallback also fails.
  func (k *KernelImpl) attemptFallback(proc *Process, req llmRequest, primaryErr error) ([]byte, error) {
      if proc.FallbackDevice == "" {
          return nil, primaryErr // no fallback configured
      }
      // ... open fallback device, modify req.Model, write+read, close, return
  }
  ```

- [x] 2.5 在 `reasonStep` 的 LLM write 和 read 错误处理路径中调用 `attemptFallback`。
  需要修改两个位置：
  - **Write 失败**（L795-807）：LLM 设备 Write 返回错误时尝试 fallback
  - **Read 失败**（L821-828）：理论上 Write 成功后 Read 不会因 provider 故障失败，但为健壮性考虑也加 fallback

  修改模式（Write 失败路径）：
  ```go
  if err := k.vfs.Write(proc.ctx, proc.PID, llmFD, reqJSON); err != nil {
      // Attempt fallback (Story 23.5)
      fbData, fbErr := k.attemptFallback(proc, req, err)
      if fbErr != nil {
          // Fallback also failed or not configured
          k.emitEvent(proc, "Write", ...)
          k.emitEvent(proc, "ReasonStep", ...)
          k.finishProcess(proc, ExitStatus{
              Code:   1,
              Reason: "llm write failed (all providers exhausted)",
              Err:    fbErr,
          })
          return
      }
      // Fallback succeeded — use fbData as response
      respData = fbData
      goto parseResponse // skip normal read path
  }
  ```
  **注意：** 避免使用 `goto`，应重构为更清晰的控制流。建议提取 `doLLMCall` 辅助函数封装 write+read 流程。

### Task 3: Strace fallback 事件输出（AC: #4）

- [x] 3.1 在 `attemptFallback` 函数中通过 `k.emitEvent` 发出 fallback 切换事件：
  ```go
  k.emitEvent(proc, "ReasonStep", map[string]any{
      "step":            step,
      "action":          "fallback",
      "primary_device":  primaryDevice,
      "primary_error":   primaryErr.Error(),
      "fallback_device": proc.FallbackDevice,
      "fallback_model":  proc.FallbackModel,
  }, nil, nil, time.Since(fallbackStart))
  ```
  strace 格式器（`debug/strace.go`）已有通用 JSON 格式化，fallback 事件会自动显示。

- [x] 3.2 当 fallback 也失败时，emitEvent 包含完整错误链：
  ```go
  k.emitEvent(proc, "ReasonStep", map[string]any{
      "step":            step,
      "action":          "fallback_exhausted",
      "primary_device":  primaryDevice,
      "primary_error":   primaryErr.Error(),
      "fallback_device": proc.FallbackDevice,
      "fallback_error":  fbErr.Error(),
  }, nil, fbErr, time.Since(fallbackStart))
  ```

### Task 4: 错误报告 — Zombie 状态包含完整 provider 列表（AC: #3）

- [x] 4.1 当所有 provider（primary + fallback）都失败时，`finishProcess` 的 `ExitStatus` 需包含详细信息：
  ```go
  k.finishProcess(proc, ExitStatus{
      Code:   1,
      Reason: "all providers exhausted",
      Err:    fmt.Errorf("primary %s: %v; fallback %s: %v", primaryDevice, primaryErr, proc.FallbackDevice, fbErr),
  })
  ```

### Task 5: 单元测试与集成测试（AC: #1-#5）

- [x] 5.1 新增 `kernel/atdd_23_5_provider_fallback_test.go`，包含以下测试：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestATDD_23_5_AC1_SameProviderFallback` | preferred 模型 ErrModelNotFound，fallback 成功 | 进程正常完成，使用 fallback 模型 |
  | `TestATDD_23_5_AC2_CrossProviderFallback` | 主 provider 调用 5xx/连接拒绝，fallback provider 成功 | 进程正常完成，切换到 fallback provider |
  | `TestATDD_23_5_AC2_FallbackLatency` | 主 provider 失败，测量 fallback 发起时间 | 切换延迟 <= 1 秒 |
  | `TestATDD_23_5_AC3_AllProvidersExhausted` | primary + fallback 均失败 | 进程 Zombie，错误含两个 provider 信息 |
  | `TestATDD_23_5_AC4_StraceShowsFallback` | 带 strace 的 fallback 切换 | DebugChan 收到 fallback 事件 |
  | `TestATDD_23_5_AC5_NoFallbackConfigured` | `models.fallback` 为空，primary 失败 | 直接报错，无 fallback 尝试 |
  | `TestATDD_23_5_FallbackProviderNotRegistered` | fallback_provider 指向未注册 provider | fallback 不可用，primary 失败直接报错 |
  | `TestATDD_23_5_SameProviderModelDowngrade` | provider: claude, preferred: sonnet, fallback: haiku | 同一 `/dev/llm/claude` 设备，model 切换 |

- [x] 5.2 测试 mock 策略：
  - 使用现有的 mock VFS 设备机制：注册自定义 `VFSFileFactory` 返回可控的 LLM 响应
  - Primary 设备：Write 时返回 error（模拟 provider 故障）
  - Fallback 设备：Write 时返回成功响应
  - 使用 `proc.DebugChan` 捕获 strace 事件验证 fallback 日志

- [x] 5.3 确保所有测试启用 `-race` 检测，并通过 `go test -race ./kernel/...`

## Dev Notes

### 核心设计决策

**1. Fallback 逻辑在 `reasonStep` 中实现（而非 VFS 层或 driver 层）。**
- `reasonStep` 是 LLM 调用的发起方，拥有完整的进程上下文（fallback 配置、strace 通道）
- VFS 层不应包含 provider 切换逻辑——VFS 只负责设备路由
- Driver 层不应知道其他 driver 的存在——每个 driver 是独立的
- Epic 定义明确指出："Fallback 逻辑实现在 `kernel/kernel.go` 的 `reasonStep` 中"

**2. 使用 `FallbackProvider` 新字段（而非重载 `Fallback` 字段格式）。**
- `Fallback` 字段已有大量使用（`agent.yaml` 中 `models.fallback: haiku`），含义为"备选模型名"
- 如果改为 `provider:model` 格式会破坏向后兼容
- 新增 `FallbackProvider` 字段语义清晰，且 `fallback_provider` 为空时自动使用主 provider

**3. Fallback 仅在 `vfs.Write` 失败时触发。**
- `vfs.Write` 调用 `LLMFile.Write`，后者调用 `driver.Call`，这是 LLM 实际调用点
- Write 返回的错误直接来自 driver（`LLMError` with `Err: ErrAuth/ErrModelNotFound/...`）
- Read 失败通常是内部 bug（response buffer 问题），不应触发 fallback
- 但出于健壮性，也可在 Read 失败时检查是否为 driver 相关错误

**4. Fallback 设备在 `attemptFallback` 中临时打开和关闭。**
- 不在 Spawn 时预开 fallback FD（避免浪费资源，大多数调用不会触发 fallback）
- `attemptFallback` 内部：Open fallback device -> Write request -> Read response -> Close
- 保持与正常 LLM 调用相同的 VFS 语义

### 变更范围

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `agents/types.go` | **修改** | `AgentModels` 新增 `FallbackProvider string` 字段 |
| `kernel/process.go` | **修改** | `Process` 新增 `FallbackModel`, `FallbackProvider`, `FallbackDevice` 字段 |
| `kernel/kernel.go` | **修改** | Spawn 中解析 fallback 配置；新增 `attemptFallback` 函数；reasonStep 中调用 fallback |
| `kernel/atdd_23_5_provider_fallback_test.go` | **新增** | 8+ 个 ATDD 测试 |
| `agents/testdata/cross-provider-agent/agent.yaml` | **新增** | 跨 provider fallback 测试 fixture |
| `agents/testdata/cross-provider-agent/instructions.md` | **新增** | 测试 fixture instructions |

**不修改的文件：**
- `drivers/llm/` — driver 层不感知 fallback，每个 driver 独立处理单次调用
- `drivers/llm/errors.go` — `LLMError`/`ErrAuth`/`ErrModelNotFound` 等已有定义，够用
- `ipc/server.go` — IPC 层不参与 fallback（fallback 在 kernel 内部处理）
- `debug/strace.go` — 通用格式器已有，fallback 事件通过 `emitEvent` 自动输出

### 架构合规

- **依赖方向**：`kernel` 依赖 `agents`（读取 `AgentModels`）和 `vfs`（Open/Write/Read/Close），不引入新依赖
- **包边界**：fallback 逻辑封装在 `kernel` 包内，不跨包暴露
- **线程安全**：`Process` 的 fallback 字段在 Spawn 时单线程设置，在 `reasonStep` goroutine 中只读——无并发问题
- **命名规范**：`attemptFallback` 遵循 kernel 包内 camelCase 私有方法命名
- **错误处理**：fallback 失败使用 `ExitStatus.Err` 传播完整错误链，不丢失原始 primary 错误
- **VFS 语义**：fallback 调用遵循标准 Open/Write/Read/Close 流程，不绕过 VFS 直接调用 driver

### 前置 Story 智能

**来自 Story 23-1（config.go）：**
- `ProviderConfig` 结构体不需要修改——fallback 配置在 `agent.yaml` 的 `AgentModels` 中，不在 `rnix-providers.yaml` 中

**来自 Story 23-2（factory.go）：**
- `RegisterProviders` 将所有 provider 注册到 `DriverRegistry` 和 `DeviceRegistry`
- Fallback 逻辑需要 VFS 中已注册 fallback provider 的设备才能 `Open`
- 如果 fallback provider 未在 `rnix-providers.yaml` 中配置（未注册到 VFS），则 `vfs.Open` 会失败——此时 fallback 不可用，行为等同于 AC #5

**来自 Story 23-3（kernel.go）：**
- `resolveLLMDevice` 用于将 provider 名称解析为 VFS 路径
- Spawn 中已有 `resolveLLMDevice(agent, opts.Provider)` 调用
- Fallback 的 VFS 路径也通过 `resolveLLMDevice` 解析（传入 `FallbackProvider`）

**来自 Story 23-4（factory.go）：**
- API Key 在工厂函数中注入——如果 fallback provider 是 HTTP API 类型且 API Key 缺失，调用时会返回 `ErrAuth`
- 这种情况下 fallback 也失败，进入 AC #3 路径

**已有测试模式（kernel 测试）：**
- `kernel_test.go` 大量使用 mock VFS 设备进行测试
- 通过 `vfs.NewDeviceRegistry()` + `Register` 注册 mock 设备
- Mock LLM 设备通过自定义 `VFSFileFactory` 返回可控行为
- `e2e_test.go:191` 已有包含 `Fallback: "haiku"` 的 agent 测试 fixture
- 参考 `atdd_23_3_dynamic_provider_resolution_test.go` 的测试模式

### 测试策略

- **Mock 双设备**：注册两个 VFS 设备（`/dev/llm/primary` 和 `/dev/llm/fallback`），primary 返回错误，fallback 返回成功
- **错误类型覆盖**：测试各种错误类型（ErrModelNotFound、HTTP 5xx/连接拒绝/ErrAuth）都能触发 fallback
- **Strace 验证**：通过 `proc.DebugChan` 读取事件，验证 fallback 事件包含正确的 primary/fallback 设备信息
- **延迟测量**：在测试中记录 primary 失败时间和 fallback 发起时间，断言差值 <= 1 秒
- **竞态检测**：所有测试运行 `-race`

### Git 智能

最近提交：
- `c9318b4 feat: implement Story 23.4 - API Key Management for HTTP API Provider`
- `e4c0324 feat: implement Story 23.3 - Dynamic Provider Resolution and Whitelist Removal`
- `d3124c2 feat: implement Story 23.2 - Config-driven Daemon Registration`
- `4cc651e feat : story 23-1`

提交建议：`feat: implement Story 23.5 - Provider Fallback Mechanism`

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 Story | 说明 |
|------|-----------|------|
| 健康检查 | 23-6 | HTTP provider 启动时可达性检测，标记 unhealthy |
| Compose/Init provider 配置 | 23-7 | `rnix-compose.yaml` / `rnix-init.yaml` 新增 `provider` 字段 |
| OODA reasonStep fallback | 未规划 | `oodaReasonStep` 有独立的推理循环，本 Story 仅修改线性 `reasonStep`；OODA 可在后续 Story 扩展 |
| Fallback 策略可配置（如重试次数、延迟） | 未规划 | 本 Story 固定策略：失败 1 次即 fallback，无重试、无退避 |
| `api_key_file` | 未规划 | Epic 提及 "考虑支持"，不在本 Epic 范围 |

### Project Structure Notes

- 变更主要在 `kernel/` 包（`kernel.go`, `process.go`）和 `agents/` 包（`types.go`）
- 新增测试文件遵循现有命名模式：`atdd_23_5_provider_fallback_test.go`
- 新增 testdata fixture 在 `agents/testdata/cross-provider-agent/`
- 不引入新外部依赖
- 与统一项目结构完全对齐

### References

- [Source: kernel/kernel.go#652-838] — `reasonStep` 函数（LLM 调用循环，fallback 注入目标）
- [Source: kernel/kernel.go#793-807] — `reasonStep` 中 LLM Write 失败处理（主要修改点）
- [Source: kernel/kernel.go#815-828] — `reasonStep` 中 LLM Read 失败处理（次要修改点）
- [Source: kernel/kernel.go#178-199] — `resolveLLMDevice` 函数（用于解析 fallback provider VFS 路径）
- [Source: kernel/kernel.go#202-526] — `Spawn` 函数（fallback 配置解析注入点，约 L295-325）
- [Source: kernel/process.go#32-80] — `Process` 结构体（新增 fallback 字段）
- [Source: agents/types.go#11-16] — `AgentModels` 结构体（新增 `FallbackProvider` 字段）
- [Source: drivers/llm/vfsfile.go#21-43] — `LLMFile.Write`（LLM 调用通过 VFS Write 触发）
- [Source: drivers/llm/errors.go#9-15] — `ErrRateLimit/ErrAuth/ErrModelNotFound` 等 sentinel 错误
- [Source: drivers/llm/errors.go#17-41] — `LLMError` 结构体（包含 Provider 字段用于识别失败来源）
- [Source: lib/agents/code-analyst/agent.yaml#1-9] — 已有 fallback 配置示例
- [Source: kernel/kernel_test.go#132-135] — 测试中已有 Fallback 配置的 AgentModels
- [Source: kernel/e2e_test.go#191] — E2E 测试中已有 Fallback 配置
- [Source: kernel/atdd_23_3_dynamic_provider_resolution_test.go] — Story 23-3 ATDD 测试模式参考
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#131] — FR144 定义
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#61] — NFR33 定义
- [Source: _bmad-output/planning-artifacts/epics/epic-23...#158-192] — Story 23.5 Epic 定义
- FRs covered: FR144
- NFRs covered: NFR33

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

No debug issues encountered during implementation.

### Completion Notes List

- Implemented `FallbackProvider` field on `AgentModels` in `agents/types.go` with YAML tag `fallback_provider`
- Added `FallbackModel`, `FallbackProvider`, `FallbackDevice`, and `PrimaryDevice` fields to `Process` struct in `kernel/process.go`
- Implemented fallback config parsing in `Spawn` function: resolves fallback provider and device path using `resolveLLMDevice`; resolution failure is non-blocking
- Implemented `attemptFallback` method on `KernelImpl`: opens fallback device, modifies request model, writes/reads via VFS, emits strace events for both success and exhaustion scenarios
- Modified `reasonStep` LLM write failure path to call `attemptFallback` before entering zombie state; uses if/else control flow (no goto) to handle fallback response data
- Strace events emitted: `"action": "fallback"` for successful fallback, `"action": "fallback_exhausted"` when all providers fail; events include `primary_device`, `primary_error`, `fallback_device`, `fallback_model`/`fallback_error`
- Error chain on exhaustion: `fmt.Errorf("primary %s: %v; fallback %s: %v", ...)` preserves both provider names and their respective errors
- Created cross-provider test fixture at `agents/testdata/cross-provider-agent/`
- Added `newSameProviderFallbackKernel` test helper using `atomic.Int32` counter-based factory for same-provider model downgrade tests
- Fixed race conditions in strace event verification tests using `sync/atomic.Bool`
- All 15 ATDD tests pass with `-race` detection
- Full regression suite: 20/20 packages pass
- Lint: 0 issues

### Change Log

- 2026-03-12: Implemented Story 23.5 - Provider Fallback mechanism with same-provider model downgrade and cross-provider fallback. Added 15 ATDD tests covering all 5 ACs plus edge cases.

### File List

- `agents/types.go` — modified: added `FallbackProvider string` field to `AgentModels`
- `kernel/process.go` — modified: added `FallbackModel`, `FallbackProvider`, `FallbackDevice`, `PrimaryDevice` fields to `Process`
- `kernel/kernel.go` — modified: fallback config parsing in `Spawn`, new `attemptFallback` method, `reasonStep` fallback path on LLM write failure
- `kernel/atdd_23_5_provider_fallback_test.go` — modified: updated from RED to GREEN phase, 15 passing tests
- `agents/testdata/cross-provider-agent/agent.yaml` — new: cross-provider fallback test fixture
- `agents/testdata/cross-provider-agent/instructions.md` — new: test fixture instructions
