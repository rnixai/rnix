# Story 23.3: Provider 动态解析与白名单移除

Status: done

## Story

As a 系统,
I want `resolveLLMDevice()` 从配置文件动态解析 provider，不再使用硬编码白名单,
So that 用户添加新 provider 时内核代码无需任何修改。

## Acceptance Criteria

1. **Given** `kernel/kernel.go` 中的 `allowedLLMProviders` 硬编码 map
   **When** 重构后
   **Then** 移除硬编码白名单 map，改为查询 `DriverRegistry` 判断 provider 是否已注册

2. **Given** Agent `agent.yaml` 指定 `models.provider: ollama`
   **When** Spawn 时调用 `resolveLLMDevice()`
   **Then** 查询 `DriverRegistry`，确认 `ollama` 已注册，返回 `/dev/llm/ollama`

3. **Given** Agent 指定不存在的 provider（如 `models.provider: nonexist`）
   **When** Spawn 时
   **Then** 返回清晰错误：`unsupported LLM provider: "nonexist" (available: claude, cursor, ollama)`，列出所有已注册的 provider

4. **Given** Agent 未指定 provider（空字符串）
   **When** Spawn 时
   **Then** 使用系统默认 provider（`claude`）

5. **Given** CLI `--provider=groq` 参数
   **When** Spawn 时
   **Then** CLI 参数覆盖 agent.yaml 中的 provider 配置

## Tasks / Subtasks

### Task 1: 修改 `resolveLLMDevice()` 签名，接收 `DriverRegistry` 参数（AC: #1, #2, #3）

- [x] 1.1 修改 `resolveLLMDevice` 函数签名，新增 `*llm.DriverRegistry` 参数：

  ```go
  func resolveLLMDevice(agent *agents.AgentInfo, providerOverride string, driverReg *llm.DriverRegistry) (string, error)
  ```

  **注意：** `kernel` 包需新增 `drivers/llm` 包的导入。但 `kernel` 当前不导入 `drivers/llm`。需要评估依赖方向——architecture 规定 `cmd/` → `kernel/` → `vfs/` → `drivers/`，而 `kernel` 不应直接导入 `drivers/llm`。

  **解决方案：** 定义一个接口（`ProviderChecker`）在 `kernel` 包内，由 `DriverRegistry` 满足。这遵循"接口定义在使用方"的 Go 惯例，且不引入 `drivers/llm` 的直接依赖。

- [x] 1.2 在 `kernel/kernel.go` 中定义 `ProviderChecker` 接口：

  ```go
  // ProviderChecker checks whether a named LLM provider is registered.
  type ProviderChecker interface {
      Get(name string) (any, bool)  // returns driver if found
      Names() []string              // returns sorted list of registered provider names
  }
  ```

  **设计理由：** `DriverRegistry` 已有 `Get(string) (LLMDriver, bool)` 和 `Names() []string` 方法。但 `kernel` 包不应引用 `LLMDriver` 类型。使用一个更通用的接口（或使用两个独立方法签名），让 `DriverRegistry` 自然满足。

  **更精简的方案：** 由于 `resolveLLMDevice` 只需要知道 provider 是否存在和所有已注册名称，不需要获取 driver 实例本身，可以定义更小的接口：

  ```go
  // ProviderResolver checks whether a named LLM provider is registered
  // and lists all available providers for error messages.
  type ProviderResolver interface {
      HasProvider(name string) bool
      ProviderNames() []string
  }
  ```

  但这需要在 `DriverRegistry` 上新增方法。为避免不必要的方法膨胀，使用适配器模式：在 `kernel` 包定义接口，在 `cmd/rnix/main.go` 构造适配器传入。

  **最终方案（最简洁）：** 使用闭包注入——将 provider 验证能力作为回调函数存储在 `KernelImpl` 中，类似已有的 `agentLoader func(name string) (*agents.AgentInfo, error)` 模式。

- [x] 1.3 **采用回调函数注入模式**（与 `agentLoader`、`skillLoader` 模式一致）：

  在 `KernelImpl` 中新增字段：

  ```go
  // Provider resolution (Story 23.3)
  providerNames func() []string             // returns sorted registered provider names
  hasProvider   func(name string) bool       // checks if provider is registered
  ```

  新增 setter 方法：

  ```go
  func (k *KernelImpl) SetProviderResolver(names func() []string, has func(name string) bool) {
      k.providerNames = names
      k.hasProvider = has
  }
  ```

  在 `cmd/rnix/main.go` 中调用：

  ```go
  k.SetProviderResolver(
      driverReg.Names,
      func(name string) bool { _, ok := driverReg.Get(name); return ok },
  )
  ```

### Task 2: 移除 `allowedLLMProviders` 白名单并重写 `resolveLLMDevice()`（AC: #1, #2, #3, #4）

- [x] 2.1 删除 `kernel/kernel.go` 第 169-174 行的 `allowedLLMProviders` 变量：

  **删除：**
  ```go
  // allowedLLMProviders is the whitelist of valid LLM provider values.
  var allowedLLMProviders = map[string]bool{
      "":       true,
      "claude": true,
      "cursor": true,
  }
  ```

- [x] 2.2 重写 `resolveLLMDevice` 函数（第 176-191 行），改为使用 `KernelImpl` 的 `hasProvider` 和 `providerNames` 回调：

  **新签名**（改为 `KernelImpl` 方法，便于访问回调字段）：

  ```go
  func (k *KernelImpl) resolveLLMDevice(agent *agents.AgentInfo, providerOverride string) (string, error) {
      provider := providerOverride
      if provider == "" && agent != nil {
          provider = agent.Manifest.Models.Provider
      }
      if provider == "" {
          provider = "claude"  // system default
      }

      if k.hasProvider != nil && !k.hasProvider(provider) {
          available := "none"
          if k.providerNames != nil {
              names := k.providerNames()
              if len(names) > 0 {
                  available = strings.Join(names, ", ")
              }
          }
          return "", fmt.Errorf("unsupported LLM provider: %q (available: %s)", provider, available)
      }

      return "/dev/llm/" + provider, nil
  }
  ```

  **关键变化：**
  - 从包级函数变为 `KernelImpl` 方法
  - `allowedLLMProviders` map 查询 → `k.hasProvider(provider)` 回调
  - 错误消息包含可用 provider 列表
  - 空 provider 统一处理为 `"claude"` default（与之前行为一致）
  - 当 `hasProvider` 为 nil 时（向后兼容/测试场景），跳过检查——这确保没有注入 resolver 的测试不会意外失败

- [x] 2.3 更新 `Spawn` 方法中对 `resolveLLMDevice` 的调用（第 393 行）：

  **当前代码：**
  ```go
  llmDevice, resolveErr := resolveLLMDevice(agent, opts.Provider)
  ```

  **替换为：**
  ```go
  llmDevice, resolveErr := k.resolveLLMDevice(agent, opts.Provider)
  ```

### Task 3: `cmd/rnix/main.go` 注入 DriverRegistry 回调（AC: #1）

- [x] 3.1 在 `runDaemon` 函数中，创建 `KernelImpl` 之后（第 1102 行附近），注入 provider resolver：

  ```go
  k.SetProviderResolver(
      driverReg.Names,
      func(name string) bool { _, ok := driverReg.Get(name); return ok },
  )
  ```

  **位置：** 在 `k := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())` 之后、`k.SetMountManager(mountMgr)` 之前。

- [x] 3.2 更新 CLI `--provider` flag 的帮助文本（第 218 行），移除硬编码的 provider 列表：

  **当前：**
  ```go
  rootCmd.Flags().StringVar(&flagProvider, "provider", "", "LLM provider override (claude, cursor)")
  ```

  **替换为：**
  ```go
  rootCmd.Flags().StringVar(&flagProvider, "provider", "", "LLM provider override (see rnix-providers.yaml)")
  ```

### Task 4: 更新 `SpawnOpts.Provider` 文档注释（AC: #5）

- [x] 4.1 更新 `kernel/kernel.go` 第 47 行的 `Provider` 字段注释：

  **当前：**
  ```go
  Provider      string        // LLM provider override; "" = use agent manifest or default "claude"
  ```

  **替换为：**
  ```go
  Provider      string        // LLM provider override (from CLI --provider); "" = use agent manifest or default "claude"
  ```

### Task 5: 更新现有测试（AC: #1, #2, #3, #4, #5）

- [x] 5.1 **重构所有 `resolveLLMDevice` 测试**（`kernel/kernel_test.go` 第 2466-2597 行）：

  由于 `resolveLLMDevice` 从包级函数变为 `KernelImpl` 方法，所有测试需要创建 `KernelImpl` 实例并注入 mock resolver。

  **测试辅助函数：**
  ```go
  func newKernelWithProviders(providers ...string) *KernelImpl {
      k := NewKernel(/* minimal VFS/CtxMgr mock */)
      providerSet := make(map[string]bool)
      for _, p := range providers {
          providerSet[p] = true
      }
      k.SetProviderResolver(
          func() []string {
              names := make([]string, 0, len(providerSet))
              for n := range providerSet { names = append(names, n) }
              sort.Strings(names)
              return names
          },
          func(name string) bool { return providerSet[name] },
      )
      return k
  }
  ```

  **注意：** `NewKernel` 需要 `*vfs.VFS` 和 `*rnixctx.Manager` 参数。在测试中可传入 nil（`resolveLLMDevice` 不访问这些字段），但 `NewKernel` 内部启动了 reaper goroutine，需要确保测试结束时调用 `Shutdown()`。或者直接构造 `&KernelImpl{}` 并注入回调，避免 reaper 开销。

- [x] 5.2 更新测试用例列表（对应新行为）：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestResolveLLMDevice_NilAgent` | agent=nil, override="", providers=[claude,cursor] | `/dev/llm/claude` (default) |
  | `TestResolveLLMDevice_EmptyProvider` | agent.provider="", override="", providers=[claude,cursor] | `/dev/llm/claude` (default) |
  | `TestResolveLLMDevice_Claude` | agent.provider="claude", providers=[claude,cursor] | `/dev/llm/claude` |
  | `TestResolveLLMDevice_Cursor` | agent.provider="cursor", providers=[claude,cursor] | `/dev/llm/cursor` |
  | `TestResolveLLMDevice_DynamicProvider` | agent.provider="ollama", providers=[claude,cursor,ollama] | `/dev/llm/ollama` — **新增** |
  | `TestResolveLLMDevice_Unsupported` | agent.provider="nonexist", providers=[claude,cursor] | error 含 "unsupported LLM provider" 和 "(available: claude, cursor)" |
  | `TestResolveLLMDevice_UnsupportedListsAvailable` | agent.provider="nonexist", providers=[claude,cursor,ollama] | error 含 "(available: claude, cursor, ollama)" — **新增** |
  | `TestResolveLLMDevice_PathTraversal` | agent.provider="../fs", providers=[claude] | error（注册表不会包含路径遍历名称） |
  | `TestResolveLLMDevice_OverrideAgent` | agent.provider="claude", override="cursor", providers=[claude,cursor] | `/dev/llm/cursor` |
  | `TestResolveLLMDevice_OverrideNoAgent` | agent=nil, override="cursor", providers=[claude,cursor] | `/dev/llm/cursor` |
  | `TestResolveLLMDevice_OverrideUnsupported` | override="nonexistent", providers=[claude,cursor] | error 含 "unsupported LLM provider" 和 "(available: claude, cursor)" |
  | `TestResolveLLMDevice_NilResolver` | hasProvider=nil（未注入）, agent.provider="anything" | `/dev/llm/anything`（向后兼容） — **新增** |
  | `TestResolveLLMDevice_OverrideDynamic` | agent.provider="claude", override="groq", providers=[claude,groq] | `/dev/llm/groq` — **新增** |

### Task 6: `SetProviderResolver` 单元测试

- [x] 6.1 `kernel/kernel_test.go` — SetProviderResolver 方法测试：
  - `TestSetProviderResolver` — 注入后 `providerNames` 和 `hasProvider` 非 nil
  - `TestSetProviderResolver_NamesCallable` — 调用 `providerNames()` 返回预期列表
  - `TestSetProviderResolver_HasProviderCallable` — 调用 `hasProvider("claude")` 返回 true，`hasProvider("nonexist")` 返回 false

## Dev Notes

### 核心设计决策

**使用回调函数注入（非接口注入）避免循环依赖。** `kernel` 包不能导入 `drivers/llm` 包（架构规则：`kernel/` 不导入 `drivers/`）。已有三个先例使用同一模式：`agentLoader func(name string) (*agents.AgentInfo, error)`、`skillLoader func(string) (*skills.SkillInfo, error)`、`stemMatcher *StemMatcher`。回调注入是最轻量的解决方案——不引入新接口文件、不修改 `NewKernel` 签名、不破坏现有调用方。

**`resolveLLMDevice` 从包级函数变为 `KernelImpl` 方法。** 回调字段存储在 `KernelImpl` 上，函数需要访问 `k.hasProvider` 和 `k.providerNames`。改为方法调用是最自然的访问方式。代价是现有 8 个测试需要调整调用方式（从 `resolveLLMDevice(agent, override)` 变为 `k.resolveLLMDevice(agent, override)`）——但逻辑不变。

**`hasProvider` 为 nil 时跳过检查（向后兼容）。** 如果 `SetProviderResolver` 未被调用（如某些测试场景中），`resolveLLMDevice` 默认允许所有 provider。这确保不注入 resolver 的现有 Spawn 测试不会意外失败。在生产代码中 `runDaemon` 始终调用 `SetProviderResolver`。

**错误消息包含可用 provider 列表。** Epic 23.3 AC #3 明确要求：`unsupported LLM provider: "nonexist" (available: claude, cursor, ollama)`。使用 `k.providerNames()` 获取排序列表（`DriverRegistry.Names()` 已保证排序），`strings.Join` 拼接。

**默认 provider 仍为 `"claude"`。** 当 `provider == ""` 时统一填充 `"claude"`，然后走注册表检查。这与之前的行为一致（`DefaultProvidersConfig()` 始终包含 claude provider）。

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 Story | 说明 |
|------|-----------|------|
| API Key 环境变量读取 | 23-4 | `CreateDriver` 扩展 `WithAPIKey()` |
| Fallback 降级 | 23-5 | `reasonStep` 中 LLM 调用失败时尝试 fallback provider |
| 健康检查 | 23-6 | HTTP provider 启动时可达性检测 |
| Compose/Init provider 配置 | 23-7 | `rnix-compose.yaml` / `rnix-init.yaml` 新增 `provider` 字段 |

### 架构合规

- **依赖方向**：`kernel/kernel.go` 不新增任何 import（不导入 `drivers/llm`）。回调函数类型使用原始 Go 类型（`func() []string`、`func(string) bool`），无跨包类型引用。`cmd/rnix/main.go` 注入时引用 `driverReg.Names` 和 `driverReg.Get` — `cmd/` → `drivers/llm` 方向正确。
- **包内聚**：`resolveLLMDevice` 留在 `kernel/kernel.go`，不引入新文件。
- **线程安全**：`providerNames` 和 `hasProvider` 回调底层访问 `DriverRegistry`（`xsync.Registry`，RWMutex 保护）。`SetProviderResolver` 在 daemon 启动时单线程调用。`resolveLLMDevice` 在 `Spawn` 内调用，多 Spawn 并发时回调是只读操作，安全。
- **命名规范**：`SetProviderResolver`（PascalCase 导出），`providerNames`/`hasProvider`（camelCase 非导出字段），与 `agentLoader`、`skillLoader` 命名风格一致。
- **错误处理**：错误消息格式 `unsupported LLM provider: %q (available: %s)` 与 Story 23-2 的 `factory.go:44` 错误格式 `unsupported driver type: %q` 风格一致。
- **VFS 路径**：统一 `/dev/llm/<name>` 格式不变。

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/kernel.go` | **修改** | 删除 `allowedLLMProviders` map（L169-174）；重写 `resolveLLMDevice` 为 `KernelImpl` 方法（L176-191）；`KernelImpl` 新增 `providerNames`/`hasProvider` 字段；新增 `SetProviderResolver` 方法；更新 `Spawn` 中 `resolveLLMDevice` 调用；更新 `Provider` 字段注释 |
| `kernel/kernel_test.go` | **修改** | 重构 8 个 `resolveLLMDevice` 测试为方法调用模式；新增 4 个测试（动态 provider、available 列表、nil resolver、override 动态）；新增 `SetProviderResolver` 测试 |
| `cmd/rnix/main.go` | **修改** | `runDaemon` 中新增 `k.SetProviderResolver(...)` 调用（~L1103）；更新 `--provider` flag 帮助文本 |

### 测试策略

- **`resolveLLMDevice` 测试**：创建 `&KernelImpl{}` 最小实例（不调用 `NewKernel` 以避免 reaper goroutine），注入 mock `providerNames`/`hasProvider` 回调，验证各 provider 解析场景。
- **向后兼容测试**：`hasProvider` 为 nil 时不 panic，返回 `/dev/llm/<name>`。
- **错误消息测试**：断言 error 同时包含 `"unsupported LLM provider"` 和 `"(available:"` 子串。
- **所有测试启用 `-race`**。
- **Go 1.26 注意**：本 Story 不涉及 `t.Setenv()`，所有测试可安全使用 `t.Parallel()`。
- **测试命名**：遵循 `Test<Function>_<Scenario>` 格式。

### 组合矩阵（Cross-Module Interaction Matrix）

| 调用方 | 被调用方 | 交互方式 | 本 Story 涉及 |
|--------|---------|---------|:---:|
| `cmd/rnix/main.go` (`runDaemon`) | `KernelImpl.SetProviderResolver(...)` | 回调注入 | Yes |
| `KernelImpl.Spawn()` | `k.resolveLLMDevice(agent, override)` | 方法调用 | Yes |
| `k.resolveLLMDevice()` | `k.hasProvider(name)` | 回调 | Yes |
| `k.resolveLLMDevice()` | `k.providerNames()` | 回调 | Yes |
| `k.hasProvider` 闭包 | `driverReg.Get(name)` | 注册表查询 | Yes (间接) |
| `k.providerNames` 闭包 | `driverReg.Names()` | 注册表查询 | Yes (间接) |
| `kernel_test.go` | `&KernelImpl{}` + mock 回调 | 测试构造 | Yes |
| CLI `--provider` flag | `SpawnOpts.Provider` | 参数传递 | Yes (帮助文本更新) |
| `IPC Server` spawn handler | `kernel.Spawn(intent, agent, opts)` | opts.Provider 传递 | No (现有流程不变) |
| `Compose engine` | `kernel.Spawn(intent, agent, opts)` | opts.Provider 传递 | No (Story 23-7) |
| `kernel.reasonStep` | `k.vfs.Write/Read` LLM device | VFS 路由 | No (不变) |

### 前置 Story 智能

**来自 Story 23-1：**
- `drivers/llm/config.go`：`ProvidersConfig`、`ProviderConfig` 类型、三种 driver 常量
- 使用 `github.com/goccy/go-yaml v1.19.2`
- `DefaultProvidersConfig()` 返回 claude + cursor

**来自 Story 23-2：**
- `drivers/llm/factory.go`：`CreateDriver` 工厂函数、`RegisterProviders` 编排函数、`DeviceRegisterer` 接口
- `drivers/llm/registry.go`：`DriverRegistry` 含 `Register`/`Get`/`Names`/`Len` 方法
- `cmd/rnix/main.go`：`runDaemon` 已使用 `LoadOrDefaultProvidersConfig` + `RegisterProviders` + `driverReg`（第 1061-1069 行）
- `driverReg` 变量在 `runDaemon` 中已声明（第 1065 行），但未传入 Kernel — 本 Story 将通过 `SetProviderResolver` 完成注入
- 提交风格：`feat: story 23-2 config-driven daemon registration`

**来自现有 Kernel 注入模式：**
- `SetAgentLoader(loader func(name string) (*agents.AgentInfo, error))` — Story 20.2
- `SetSkillLoader(fn func(string) (*skills.SkillInfo, error))` — Story 20.3
- `SetStemMatcher(m *StemMatcher)` — Story 20.3
- `SetDiffMemory(m *DiffMemory)` — Story 20.4
- 所有 setter 在 `runDaemon` 中 `NewKernel` 之后调用

### Git 智能

最近提交：
- `d3124c2 feat: implement Story 23.2 - Config-driven Daemon Registration`
- `4cc651e feat : story 23-1`
- `6c3e287 feat: prd / arch/ epics for Multi-LLM Provider Management`

提交建议：`feat: story 23-3 dynamic provider resolution`

### 当前 `resolveLLMDevice` 精确代码（重构目标）

```go
// kernel/kernel.go L169-191 (current)

// allowedLLMProviders is the whitelist of valid LLM provider values.
var allowedLLMProviders = map[string]bool{
    "":       true,
    "claude": true,
    "cursor": true,
}

// resolveLLMDevice returns the VFS device path for the LLM provider.
// providerOverride takes precedence over the agent manifest's provider field.
// Returns "/dev/llm/claude" by default when both are empty.
func resolveLLMDevice(agent *agents.AgentInfo, providerOverride string) (string, error) {
    provider := providerOverride
    if provider == "" && agent != nil {
        provider = agent.Manifest.Models.Provider
    }
    if !allowedLLMProviders[provider] {
        return "", fmt.Errorf("unsupported LLM provider: %q", provider)
    }
    if provider == "" || provider == "claude" {
        return "/dev/llm/claude", nil
    }
    return "/dev/llm/" + provider, nil
}
```

### Spawn 中的调用点

```go
// kernel/kernel.go L392-394 (current)
llmDevice, resolveErr := resolveLLMDevice(agent, opts.Provider)
```

### 现有 `resolveLLMDevice` 测试（全部 8 个需重构）

| 测试函数 | 行号 | 变更 |
|----------|------|------|
| `TestResolveLLMDevice_NilAgent` | L2468-2477 | 改为 `k.resolveLLMDevice(nil, "")` |
| `TestResolveLLMDevice_EmptyProvider` | L2479-2493 | 改为 `k.resolveLLMDevice(agent, "")` |
| `TestResolveLLMDevice_Claude` | L2495-2509 | 改为 `k.resolveLLMDevice(agent, "")` |
| `TestResolveLLMDevice_Cursor` | L2511-2525 | 改为 `k.resolveLLMDevice(agent, "")` |
| `TestResolveLLMDevice_Unsupported` | L2527-2541 | 改调用 + 断言 `"(available:"` |
| `TestResolveLLMDevice_PathTraversal` | L2543-2557 | 改调用方式 |
| `TestResolveLLMDevice_OverrideAgent` | L2559-2574 | 改调用方式 |
| `TestResolveLLMDevice_OverrideNoAgent` | L2576-2586 | 改调用方式 |
| `TestResolveLLMDevice_OverrideUnsupported` | L2588-2597 | 改调用 + 断言 `"(available:"` |

### 参考模式

**已有回调注入模式** (`kernel/kernel.go`):
```go
// L141-145: 字段定义
agentLoader func(name string) (*agents.AgentInfo, error)
stemMatcher *StemMatcher
skillLoader func(string) (*skills.SkillInfo, error)
diffMemory  *DiffMemory

// L1428-1430: Setter 方法
func (k *KernelImpl) SetAgentLoader(loader func(name string) (*agents.AgentInfo, error)) {
    k.agentLoader = loader
}
```

**`cmd/rnix/main.go` 注入模式** (L1100-1114):
```go
k := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
k.SetMountManager(mountMgr)
k.SetAgentLoader(agentLoader.Load)
// ... more setters ...
```

## Project Structure Notes

- 核心变更集中在 `kernel/kernel.go`（业务逻辑）和 `kernel/kernel_test.go`（测试）
- `cmd/rnix/main.go` 仅新增一行 setter 调用和帮助文本更新
- 不新增文件、不新增外部依赖
- `drivers/llm/registry.go` 不需要任何修改（`Names()` 和 `Get()` 已存在）

## References

- [Source: kernel/kernel.go#169-191] — `allowedLLMProviders` 硬编码白名单 + `resolveLLMDevice` 当前实现（重构目标）
- [Source: kernel/kernel.go#38-52] — `SpawnOpts` 结构体（`Provider` 字段注释更新）
- [Source: kernel/kernel.go#110-149] — `KernelImpl` 结构体（新增 `providerNames`/`hasProvider` 字段）
- [Source: kernel/kernel.go#151-167] — `NewKernel` 构造函数（不修改）
- [Source: kernel/kernel.go#392-394] — `Spawn` 中 `resolveLLMDevice` 调用点
- [Source: kernel/kernel.go#1428-1445] — 已有 `SetAgentLoader`/`SetStemMatcher`/`SetSkillLoader`/`SetDiffMemory` setter 模式
- [Source: kernel/kernel_test.go#2466-2597] — 现有 8 个 `resolveLLMDevice` 测试（全部需重构）
- [Source: drivers/llm/registry.go#27-29] — `DriverRegistry.Get(path string) (LLMDriver, bool)`
- [Source: drivers/llm/registry.go#32-39] — `DriverRegistry.Names() []string`（排序返回）
- [Source: drivers/llm/factory.go#51-76] — `RegisterProviders` 编排函数（Story 23-2 交付）
- [Source: cmd/rnix/main.go#1061-1069] — `runDaemon` 中 `driverReg` 变量（已声明，本 Story 注入到 Kernel）
- [Source: cmd/rnix/main.go#1100-1114] — 已有 setter 注入模式
- [Source: cmd/rnix/main.go#218] — `--provider` flag 帮助文本
- [Source: agents/types.go#12-16] — `AgentModels` 结构体（`Provider` 字段）
- [Source: _bmad-output/planning-artifacts/epics/epic-23-多llm-provider动态配置-multi-llm-provider-management.md#91-123] — Epic 23 Story 23.3 定义
- FRs covered: FR143, FR145

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6

### Debug Log References
N/A - no debugging issues encountered.

### Completion Notes List
- Removed `allowedLLMProviders` hardcoded whitelist map from `kernel/kernel.go`
- Converted `resolveLLMDevice` from package-level function to `KernelImpl` method
- Added `providerNames`/`hasProvider` callback fields to `KernelImpl` struct
- Added `SetProviderResolver` setter method following existing injection patterns (`agentLoader`, `skillLoader`)
- Injected `SetProviderResolver` in `cmd/rnix/main.go` `runDaemon` using `driverReg.Names` and `driverReg.Get`
- Updated `--provider` CLI flag help text to reference `rnix-providers.yaml`
- Updated `SpawnOpts.Provider` comment to note CLI origin
- Refactored all 9 existing `resolveLLMDevice` tests to use method calls with mock resolver
- Added 4 new unit tests: DynamicProvider, UnsupportedListsAvailable, NilResolver (backward compat), OverrideDynamic
- Added 3 SetProviderResolver tests
- Updated all 12 ATDD tests: removed `t.Skip()`, injected resolvers, converted to method calls
- Error messages now include `(available: ...)` sorted list of registered providers
- Nil resolver backward compatibility: allows any provider when `SetProviderResolver` not called
- All 20 packages pass tests with race detection, 0 lint issues

### File List
- `kernel/kernel.go` (modified) - Removed allowedLLMProviders, resolveLLMDevice as method, SetProviderResolver, provider callback fields
- `kernel/kernel_test.go` (modified) - Refactored 9 tests + added 7 new tests (4 unit + 3 setter)
- `kernel/atdd_23_3_dynamic_provider_resolution_test.go` (modified) - GREEN phase: 12 ATDD tests updated
- `cmd/rnix/main.go` (modified) - SetProviderResolver injection + --provider help text

## Change Log

- 2026-03-12: Story 创建，状态 ready-for-dev
- 2026-03-12: Story 实现完成，所有 6 个 Task 完成，28 个测试通过，状态 review
