# Story 23.4: HTTP API Provider 的 API Key 管理

Status: done

## Story

As a 用户,
I want HTTP API 类型的 provider 通过环境变量引用 API Key,
So that 密钥不明文存储在配置文件中，符合安全最佳实践。

## Acceptance Criteria

1. **Given** `rnix-providers.yaml` 中 provider 配置了 `api_key_env: GROQ_API_KEY`
   **When** 创建 `OpenAICompatDriver` 实例
   **Then** 从环境变量 `GROQ_API_KEY` 读取 API Key
   **And** Key 值通过 `WithAPIKey()` 选项传入驱动

2. **Given** `api_key_env` 指定的环境变量不存在
   **When** daemon 启动
   **Then** 日志输出 warning：`provider "groq": API key env var GROQ_API_KEY not set`
   **And** provider 仍然注册（用户可能稍后设置环境变量）
   **But** 首次调用时返回 `ErrAuth` 错误

3. **Given** 本地 provider（如 Ollama）不需要 API Key
   **When** `api_key_env` 字段为空或未设置
   **Then** 驱动正常创建，不附带认证头

4. **Given** 安全审计
   **When** 检查配置文件和日志
   **Then** API Key 明文不出现在任何配置文件、日志输出或错误消息中

## Tasks / Subtasks

### Task 1: 修改 `CreateDriver` 工厂函数，读取环境变量并传入 `WithAPIKey()`（AC: #1, #3）

- [x] 1.1 修改 `drivers/llm/factory.go` 中的 `CreateDriver` 函数，在 `DriverOpenAICompat` 分支中处理 `api_key_env` 字段：

  ```go
  case DriverOpenAICompat:
      var opts []CompatOption
      if cfg.DefaultModel != "" {
          opts = append(opts, WithCompatModel(cfg.DefaultModel))
      }
      if cfg.APIKeyEnv != "" {
          if key := os.Getenv(cfg.APIKeyEnv); key != "" {
              opts = append(opts, WithAPIKey(key))
          } else {
              log.Printf("[llm] warning: provider %q: API key env var %s not set", cfg.Name, cfg.APIKeyEnv)
          }
      }
      return NewOpenAICompatDriver(cfg.Name, cfg.BaseURL, opts...), nil
  ```

  **关键点：**
  - `cfg.APIKeyEnv` 已在 `ProviderConfig` 结构体中定义（`config.go:39`），字段 `api_key_env`
  - `WithAPIKey()` 已在 `openai_compat.go:52-54` 实现
  - `os.Getenv` 返回空字符串时不传入 `WithAPIKey`，保留 driver 无认证状态
  - 需要新增 `"os"` 和 `"log"` import 到 `factory.go`

- [x] 1.2 删除 `factory.go:19` 的注释 `// API key handling is deferred to Story 23-4.`

### Task 2: 确保无 API Key 时首次调用返回 `ErrAuth`（AC: #2）

- [x] 2.1 验证 `OpenAICompatDriver.doHTTP()` 现有行为：

  当 `d.apiKey == ""` 时（`openai_compat.go:257-259`），不设置 `Authorization` header。这意味着请求发送到需要认证的 API 端点时，服务端返回 HTTP 401，被 `classifyHTTPError` 映射为 `ErrAuth`（`openai_compat.go:275`）。

  **结论：现有代码已满足 AC #2，无需修改 `OpenAICompatDriver`。** 缺少 API Key 时的行为链：
  1. `doHTTP` 不设置 Authorization header
  2. 远程 API 返回 401 Unauthorized
  3. `classifyHTTPError` 将 401 映射为 `ErrAuth`
  4. 调用方收到 `*LLMError{Provider: "groq", StatusCode: 401, Err: ErrAuth}`

  此行为链完全满足 AC #2 的 "首次调用时返回 `ErrAuth` 错误" 要求。

- [x] 2.2 添加测试验证此行为（见 Task 4）。

### Task 3: 确保 API Key 不泄露到日志和错误消息中（AC: #4）

- [x] 3.1 审计 `factory.go` 中的日志输出：

  warning 日志仅输出环境变量名（`GROQ_API_KEY`），不输出 key 值。格式：
  ```
  [llm] warning: provider "groq": API key env var GROQ_API_KEY not set
  ```

  确认：
  - `log.Printf` 中不引用 `key` 变量
  - `RegisterProviders` 的日志行（`factory.go:73`）仅输出 provider name 和路径，不含 key
  - `LLMError.Error()` 不包含 key（`errors.go:27-30`）
  - `classifyHTTPError` 不输出 Authorization header 值

- [x] 3.2 确认 `OpenAICompatDriver` 的 `apiKey` 字段为非导出 (`apiKey string`)，外部无法通过反射以外方式访问。已满足。

### Task 4: 单元测试（AC: #1, #2, #3, #4）

- [x] 4.1 在 `drivers/llm/factory_test.go` 新增测试：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestCreateDriver_OpenAICompat_WithAPIKeyEnv` | 设置 `GROQ_API_KEY` 环境变量 + `api_key_env: GROQ_API_KEY` | driver 创建成功，HTTP 请求包含 `Authorization: Bearer <key>` |
  | `TestCreateDriver_OpenAICompat_APIKeyEnvNotSet` | `api_key_env: GROQ_API_KEY` 但环境变量未设置 | driver 创建成功（warning 日志），无 Authorization header |
  | `TestCreateDriver_OpenAICompat_NoAPIKeyEnv` | `api_key_env` 为空 | driver 创建成功，无 Authorization header（本地 provider 场景） |
  | `TestCreateDriver_OpenAICompat_APIKeyNotLeaked` | 设置环境变量 | API Key 不出现在 driver.Info() 输出中 |

- [x] 4.2 API Key 注入验证测试策略：

  由于 `apiKey` 是非导出字段，无法直接断言其值。使用 `httptest.NewServer` 创建 mock HTTP 服务端，验证：
  1. 有 API Key 时：请求头包含 `Authorization: Bearer <key>`
  2. 无 API Key 时：请求头不包含 `Authorization`

  ```go
  func TestCreateDriver_OpenAICompat_WithAPIKeyEnv(t *testing.T) {
      t.Setenv("TEST_API_KEY", "sk-test-12345")

      // Mock HTTP server to capture Authorization header
      var gotAuth string
      srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
          gotAuth = r.Header.Get("Authorization")
          // Return minimal valid response
          json.NewEncoder(w).Encode(oaiResponse{
              Choices: []oaiChoice{{Message: oaiMessage{Content: "ok"}}},
          })
      }))
      defer srv.Close()

      d, err := CreateDriver(ProviderConfig{
          Name:      "test-provider",
          Driver:    DriverOpenAICompat,
          BaseURL:   srv.URL,
          APIKeyEnv: "TEST_API_KEY",
      })
      if err != nil {
          t.Fatalf("unexpected error: %v", err)
      }

      _, _ = d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})

      if gotAuth != "Bearer sk-test-12345" {
          t.Errorf("expected Authorization header %q, got %q", "Bearer sk-test-12345", gotAuth)
      }
  }
  ```

  **注意：** `t.Setenv` 自动在测试结束后恢复环境变量。使用 `t.Setenv` 的测试不能标记 `t.Parallel()`（Go 限制）。

- [x] 4.3 无 API Key 时返回 ErrAuth 的集成测试：

  ```go
  func TestCreateDriver_OpenAICompat_APIKeyEnvNotSet_ErrAuth(t *testing.T) {
      // Ensure env var is not set
      t.Setenv("NONEXISTENT_KEY", "")
      os.Unsetenv("NONEXISTENT_KEY")  // t.Setenv sets it to "", need to actually unset

      srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
          if r.Header.Get("Authorization") == "" {
              w.WriteHeader(401)
              json.NewEncoder(w).Encode(oaiErrorResponse{...})
              return
          }
      }))
      defer srv.Close()

      d, err := CreateDriver(ProviderConfig{
          Name:      "test-nokey",
          Driver:    DriverOpenAICompat,
          BaseURL:   srv.URL,
          APIKeyEnv: "NONEXISTENT_KEY",
      })
      if err != nil {
          t.Fatalf("unexpected error: %v", err)
      }

      _, callErr := d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})
      if callErr == nil {
          t.Fatal("expected ErrAuth error, got nil")
      }
      if !errors.Is(callErr, ErrAuth) {
          t.Errorf("expected ErrAuth, got: %v", callErr)
      }
  }
  ```

### Task 5: RegisterProviders 集成测试更新（AC: #1, #2）

- [x] 5.1 在 `factory_test.go` 新增 `RegisterProviders` 测试验证 API Key 传递：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestRegisterProviders_WithAPIKey` | 完整配置含 `api_key_env` + 环境变量设置 | provider 注册成功，HTTP 请求带 Authorization header |
  | `TestRegisterProviders_APIKeyEnvMissing` | `api_key_env` 设置但环境变量缺失 | provider 仍注册成功（仅 warning） |

## Dev Notes

### 核心设计决策

**在 `CreateDriver` 工厂函数中读取环境变量（而非在 `OpenAICompatDriver` 构造函数中）。** 这是因为：
1. `ProviderConfig.APIKeyEnv` 是配置层概念，属于工厂函数职责
2. `NewOpenAICompatDriver` 已通过 `WithAPIKey(key)` 接受已解析的 key 值，不应知道 key 来源
3. 与 Story 23-2 建立的模式一致：`CreateDriver` 负责从 `ProviderConfig` 提取所有参数并传入构造函数
4. 未来扩展 `api_key_file` 时只需修改 `CreateDriver`，不影响 driver 层

**环境变量缺失时 provider 仍注册。** Epic 定义明确要求此行为——用户可能先配置再设置环境变量。Warning 日志提醒用户，首次调用时 HTTP 401 触发 `ErrAuth` 提供运行时错误反馈。

**不修改 `OpenAICompatDriver`。** driver 层代码已完整支持 API Key 功能：`WithAPIKey()` 设置 `apiKey` 字段，`doHTTP()` 根据 `apiKey` 是否为空决定是否设置 Authorization header，`classifyHTTPError` 将 401 映射为 `ErrAuth`。本 Story 仅需在工厂层接线。

### 变更范围（极小）

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `drivers/llm/factory.go` | **修改** | `CreateDriver` openai-compat 分支新增 ~8 行环境变量读取 + `WithAPIKey()` 传入；新增 `"os"` import；删除 defer 注释 |
| `drivers/llm/factory_test.go` | **修改** | 新增 ~5 个测试函数验证 API Key 注入、缺失场景、不泄露 |

**不修改的文件（已具备能力）：**
- `drivers/llm/config.go` — `ProviderConfig.APIKeyEnv` 字段已定义
- `drivers/llm/openai_compat.go` — `WithAPIKey()`、`doHTTP()` Authorization header、`classifyHTTPError` 401→ErrAuth 全部已实现
- `drivers/llm/errors.go` — `ErrAuth` sentinel 已定义
- `drivers/llm/registry.go` — 无需修改
- `kernel/kernel.go` — 无需修改
- `cmd/rnix/main.go` — `RegisterProviders` 调用不变

### 安全审计清单

1. `factory.go` 日志仅输出环境变量名称，不输出值
2. `LLMError.Error()` 不包含 API Key
3. `classifyHTTPError` 输出 HTTP 状态码和错误消息，不输出请求头
4. `DriverInfo` 结构体（`Info()` 返回值）不包含 API Key 字段
5. `apiKey` 字段为非导出（`openai_compat.go:25`），外部不可直接访问
6. `ProviderConfig.APIKeyEnv` 仅存储环境变量名，不存储值

### 架构合规

- **依赖方向**：`factory.go` 仅使用标准库 `os.Getenv` 和 `log.Printf`，不引入新外部依赖
- **包内聚**：变更集中在 `drivers/llm` 包内的工厂函数，不跨包
- **线程安全**：`os.Getenv` 在 daemon 启动时单线程调用（`runDaemon` → `RegisterProviders` → `CreateDriver`），无并发问题
- **命名规范**：日志前缀 `[llm]` 与 `RegisterProviders` 已有日志一致
- **错误处理**：环境变量缺失为 warning（不阻断启动），运行时 401 通过已有 `ErrAuth` 错误链传播

### 前置 Story 智能

**来自 Story 23-1（config.go）：**
- `ProviderConfig.APIKeyEnv string` 字段已定义（`config.go:39`）
- 使用 YAML tag `api_key_env`
- 配置校验 `Validate()` 中无 `APIKeyEnv` 相关校验（非必填字段）

**来自 Story 23-2（factory.go）：**
- `CreateDriver` 工厂函数分三个 driver 分支处理
- `RegisterProviders` 调用 `CreateDriver` 后注册到 `DriverRegistry` + `DeviceRegistry`
- 日志格式 `[llm] registered %d providers: ...`
- 现有注释 `// API key handling is deferred to Story 23-4.` 标记了本 Story 的接入点

**来自 Story 23-3（kernel.go）：**
- `resolveLLMDevice` 已改为 `KernelImpl` 方法
- Provider 解析通过 `SetProviderResolver` 回调注入
- 与本 Story 无直接代码交互（本 Story 在 driver 层）

**已有测试模式（factory_test.go）：**
- 使用 `t.Parallel()` 标记可并行测试
- Mock VFS 通过 `mockDeviceRegisterer` 实现
- VFS 集成测试使用真实 `vfs.NewDeviceRegistry()`
- 测试命名遵循 `Test<Function>_<Scenario>` 格式

### 测试策略

- **API Key 注入测试**：使用 `httptest.NewServer` 创建 mock 服务端，捕获 Authorization header 验证 key 传入
- **环境变量管理**：使用 `t.Setenv`（Go 1.17+），自动在测试结束后恢复
- **并行性**：使用 `t.Setenv` 的测试不能 `t.Parallel()`（修改进程全局状态）；不需要环境变量的测试保持 `t.Parallel()`
- **ErrAuth 测试**：mock 服务端在无 Authorization header 时返回 401，验证错误链
- **安全审计测试**：验证 `Info()` 输出不含 key
- **所有测试启用 `-race`**

### Git 智能

最近提交：
- `e4c0324 feat: implement Story 23.3 - Dynamic Provider Resolution and Whitelist Removal`
- `d3124c2 feat: implement Story 23.2 - Config-driven Daemon Registration`
- `4cc651e feat : story 23-1`

提交建议：`feat: implement Story 23.4 - API Key Management for HTTP Providers`

### 组合矩阵（Cross-Module Interaction Matrix）

| 调用方 | 被调用方 | 交互方式 | 本 Story 涉及 |
|--------|---------|---------|:---:|
| `cmd/rnix/main.go` (`runDaemon`) | `llm.RegisterProviders(cfg, driverReg, devReg)` | 函数调用 | No (不修改) |
| `llm.RegisterProviders` | `llm.CreateDriver(pc)` | 函数调用 | Yes (间接) |
| `llm.CreateDriver` | `os.Getenv(cfg.APIKeyEnv)` | 环境变量读取 | Yes |
| `llm.CreateDriver` | `llm.WithAPIKey(key)` | Option 传入 | Yes |
| `llm.CreateDriver` | `llm.NewOpenAICompatDriver(name, url, opts...)` | 构造函数 | No (不修改) |
| `OpenAICompatDriver.doHTTP` | `req.Header.Set("Authorization", ...)` | HTTP header 设置 | No (已实现) |
| `OpenAICompatDriver.classifyHTTPError` | `ErrAuth` | 错误映射 | No (已实现) |
| `factory_test.go` | `httptest.NewServer` | 测试 mock | Yes (新增) |
| `factory_test.go` | `t.Setenv` | 环境变量设置 | Yes (新增) |

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 Story | 说明 |
|------|-----------|------|
| `api_key_file` 文件引用 | 未规划 | Epic 提及 "考虑支持"，本 Story 不实现 |
| Fallback 降级 | 23-5 | `reasonStep` 中 LLM 调用失败时尝试 fallback provider |
| 健康检查 | 23-6 | HTTP provider 启动时可达性检测 |
| Compose/Init provider 配置 | 23-7 | `rnix-compose.yaml` / `rnix-init.yaml` 新增 `provider` 字段 |

### Project Structure Notes

- 变更严格限制在 `drivers/llm/` 包内
- 不引入新文件、不新增外部依赖
- `factory.go` 新增 `"os"` import（标准库）
- 与统一项目结构完全对齐，无冲突

### References

- [Source: drivers/llm/factory.go#17-46] — `CreateDriver` 工厂函数（修改目标，openai-compat 分支 L36-41）
- [Source: drivers/llm/factory.go#19] — Story 23-4 defer 注释（删除目标）
- [Source: drivers/llm/config.go#34-40] — `ProviderConfig` 结构体（`APIKeyEnv` 字段已定义 L39）
- [Source: drivers/llm/openai_compat.go#23-31] — `OpenAICompatDriver` 结构体（`apiKey` 非导出字段 L25）
- [Source: drivers/llm/openai_compat.go#52-54] — `WithAPIKey()` CompatOption（已实现）
- [Source: drivers/llm/openai_compat.go#257-259] — `doHTTP` Authorization header 设置逻辑（已实现）
- [Source: drivers/llm/openai_compat.go#275] — `classifyHTTPError` 401→ErrAuth 映射（已实现）
- [Source: drivers/llm/errors.go#11] — `ErrAuth` sentinel 错误定义
- [Source: drivers/llm/factory_test.go] — 现有测试模式参考
- [Source: cmd/rnix/main.go#1065-1068] — `runDaemon` 中 `RegisterProviders` 调用（不修改）
- [Source: _bmad-output/planning-artifacts/epics/epic-23...#125-156] — Story 23.4 Epic 定义
- FRs covered: FR146

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A — 零 bug，首次运行全部通过

### Completion Notes List

- ✅ Task 1: `factory.go:41-47` CreateDriver 读取 `api_key_env` 环境变量并通过 `WithAPIKey()` 注入。`os` 和 `log` import 已存在。defer 注释已删除。
- ✅ Task 2: `openai_compat.go:257-259` 已有行为满足 AC#2 — 无 API Key 时不设置 Authorization header，远程 401 通过 `classifyHTTPError` 映射为 `ErrAuth`。
- ✅ Task 3: 安全审计通过 — 日志仅输出环境变量名，`DriverInfo` 不含 key，`LLMError.Error()` 不含 key，`apiKey` 字段非导出。
- ✅ Task 4: 11 个 ATDD 测试全部 PASS（`atdd_23_4_api_key_management_test.go`）。测试使用 `httptest.NewServer` 验证 Authorization header 和 `t.Setenv` 管理环境变量。
- ✅ Task 5: RegisterProviders 集成测试通过 — API Key 正确通过完整注册链路传递。
- ✅ 全量测试: 20/20 包通过，零回归。golangci-lint 零 issue。

### File List

| 文件 | 变更类型 |
|------|---------|
| `drivers/llm/factory.go` | 修改（API key env 读取 + WithAPIKey 注入，~8 行） |
| `drivers/llm/atdd_23_4_api_key_management_test.go` | 新增（11 个测试，394 行） |
| `_bmad-output/implementation-artifacts/23-4-api-key-management.md` | 修改（任务完成标记 + Dev Agent Record） |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | 修改（story 状态更新） |
| `_bmad-output/test-artifacts/atdd-checklist-23-4.md` | 新增（ATDD checklist） |

## Senior Developer Review (AI)

**Reviewer:** Amelia (Dev Agent) | **Date:** 2026-03-12 | **Model:** Claude Opus 4.6

### Outcome: APPROVED

### Review Summary

- **4 ACs**: 全部 IMPLEMENTED，代码证据充分
- **8 Tasks**: 全部 [x] 标记真实，无虚标
- **Git vs Story**: 文件清单完全一致，0 差异
- **测试**: 11/11 ATDD PASS + 20/20 全量包 PASS，零回归
- **Lint**: golangci-lint 0 issues

### Issues Found & Fixed

| # | 严重度 | 描述 | 状态 |
|---|--------|------|------|
| L2 | LOW | 测试文件头 "TDD RED PHASE" 注释已过时 | FIXED - 删除过时注释 |
| L3 | LOW | `TestATDD_23_4_AC4_APIKeyNotInErrorMessage` 用 `t.Skip` 应为 `t.Fatal` | FIXED - 改为 `t.Fatal` |

### Issues Noted (Not Fixed)

| # | 严重度 | 描述 | 原因 |
|---|--------|------|------|
| M1 | MEDIUM | Story 文档中 Task 4.3 示例代码与实际实现有差异 | 文档问题，实际代码正确 |
| M2 | MEDIUM | `log.Printf` 非结构化日志 | 与项目现有风格一致，属全局改进 |
| M3 | MEDIUM | Tasks 节引用 `factory_test.go`，实际文件名 `atdd_23_4_*` | 文档问题，Dev Record 已修正 |
| L1 | LOW | `ProviderConfigStoresEnvVarNameNotValue` 测试断言价值低 | 不影响覆盖率，保留 |

### Change Log

- 2026-03-12: Code Review APPROVED, 2 LOW issues fixed in test file
