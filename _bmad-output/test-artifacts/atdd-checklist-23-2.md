---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-12'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/23-2-config-driven-daemon-registration.md'
---

# ATDD Checklist - Epic 23, Story 2: 配置驱动的 Daemon 启动注册流程

**Date:** 2026-03-12
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

Story 23.2 实现配置驱动的 daemon 启动注册流程。基于 Story 23-1 的 `ProvidersConfig` 解析结果，通过工厂函数 (`CreateDriver`) 和编排函数 (`RegisterProviders`) 动态实例化 LLM 驱动，注册到 `DriverRegistry` 和 VFS `DeviceRegistry`，替换 `main.go` 中的硬编码注册。

**As a** 系统
**I want** daemon 启动时根据配置文件动态实例化并注册 LLM 驱动
**So that** 所有已配置的 provider 自动可用，无需硬编码

---

## Acceptance Criteria

1. **AC1 - 驱动工厂与注册:** Given `rnix-providers.yaml` 已解析, When daemon 启动注册阶段, Then 遍历 providers 配置，根据 `driver` 字段选择构造函数（`claude-cli` → `NewClaudeCliDriver()`、`cursor-cli` → `NewCursorCliDriver()`、`openai-compat` → `NewOpenAICompatDriver(name, baseURL, opts...)`）And 每个驱动通过 `DriverRegistry.Register(name, driver)` 注册 And 每个驱动通过 `DeviceRegistry.Register("/dev/llm/"+name, FileFactory(driver))` 挂载到 VFS
2. **AC2 - VFS 路由:** Given 注册完成, When 进程通过 VFS `Open("/dev/llm/ollama")` 访问, Then 正确路由到 `OpenAICompatDriver` 实例
3. **AC3 - 去硬编码:** Given 移除 `main.go` 中的硬编码注册, When daemon 启动, Then 所有 LLM 驱动仅通过配置文件注册，`main.go` 无 `NewClaudeCliDriver()` 等硬编码调用
4. **AC4 - Registry 统一管理:** Given 已有 `DriverRegistry` 实现, When 重构后, Then `DriverRegistry` 作为驱动实例的统一管理入口，所有驱动通过它注册和获取

---

## Test Strategy

| AC | 测试层级 | 测试文件 | 验证方式 |
|----|---------|---------|---------|
| AC1 驱动工厂 | Unit | `drivers/llm/factory_test.go` | `CreateDriver` 对每种 driver 类型返回正确驱动实例，验证 `Info()` 元数据 |
| AC1 注册编排 | Unit | `drivers/llm/factory_test.go` | `RegisterProviders` 将驱动注册到 `DriverRegistry` + mock `DeviceRegisterer` |
| AC2 VFS 路由 | Integration | `drivers/llm/factory_test.go` | 使用真实 `vfs.DeviceRegistry` + `vfs.VFS`，`Open` 后验证路由和 `Stat` |
| AC3 去硬编码 | Code Review | 人工审查 | 确认 `main.go` 无直接 `NewClaudeCliDriver()` 等调用 |
| AC4 Registry 增强 | Unit | `drivers/llm/registry_test.go` | `Names()`、`Len()` 方法的正确性 |

---

## Failing Tests Created (RED Phase)

### Unit Tests — 驱动工厂 `CreateDriver` (6 tests)

**File:** `drivers/llm/factory_test.go`

- RED **Test:** `TestCreateDriver_ClaudeCLI`
  - **Status:** RED — `CreateDriver` 函数未定义
  - **Verifies:** AC#1 — `DriverClaudeCLI` 配置返回 `*ClaudeCliDriver`，`Info().Name == "claude-cli"`，`Info().DefaultModel == "haiku"`
  - **Setup:** `ProviderConfig{Name: "claude", Driver: DriverClaudeCLI, DefaultModel: "haiku"}`

- RED **Test:** `TestCreateDriver_CursorCLI`
  - **Status:** RED — `CreateDriver` 函数未定义
  - **Verifies:** AC#1 — `DriverCursorCLI` 配置返回 `*CursorCliDriver`，`Info().Name == "cursor-cli"`
  - **Setup:** `ProviderConfig{Name: "cursor", Driver: DriverCursorCLI}`

- RED **Test:** `TestCreateDriver_OpenAICompat`
  - **Status:** RED — `CreateDriver` 函数未定义
  - **Verifies:** AC#1 — `DriverOpenAICompat` 配置返回 `*OpenAICompatDriver`，`Info().Name == "ollama"`，`Info().Provider == "ollama"`
  - **Setup:** `ProviderConfig{Name: "ollama", Driver: DriverOpenAICompat, BaseURL: "http://localhost:11434/v1"}`

- RED **Test:** `TestCreateDriver_OpenAICompat_WithModel`
  - **Status:** RED — `CreateDriver` 函数未定义
  - **Verifies:** AC#1 — `default_model` 字段正确传递到驱动，`Info().DefaultModel == "llama3"`
  - **Setup:** `ProviderConfig{Name: "ollama", Driver: DriverOpenAICompat, BaseURL: "http://localhost:11434/v1", DefaultModel: "llama3"}`

- RED **Test:** `TestCreateDriver_UnknownDriver`
  - **Status:** RED — `CreateDriver` 函数未定义
  - **Verifies:** AC#1 — 未知 driver 类型返回 error，error 消息包含 driver 类型名
  - **Setup:** `ProviderConfig{Name: "bad", Driver: "unknown-driver"}`

- RED **Test:** `TestCreateDriver_OpenAICompat_MissingBaseURL`
  - **Status:** RED — `CreateDriver` 函数未定义
  - **Verifies:** AC#1 — `openai-compat` 缺少 `base_url` 时返回 error（defense-in-depth，`Validate()` 已拦截但工厂函数额外防御）
  - **Setup:** `ProviderConfig{Name: "bad", Driver: DriverOpenAICompat, BaseURL: ""}`

### Unit Tests — Registry 增强 (3 tests)

**File:** `drivers/llm/registry_test.go`

- RED **Test:** `TestDriverRegistry_Names`
  - **Status:** RED — `Names()` 方法未定义
  - **Verifies:** AC#4 — 注册多个驱动后返回排序的名称列表
  - **Setup:** 注册 "cursor"、"claude"、"ollama" 三个驱动，断言 `Names()` 返回 `["claude", "cursor", "ollama"]`

- RED **Test:** `TestDriverRegistry_Names_Empty`
  - **Status:** RED — `Names()` 方法未定义
  - **Verifies:** AC#4 — 空注册表返回空 slice（非 nil）
  - **Setup:** 新建空 `DriverRegistry`

- RED **Test:** `TestDriverRegistry_Len`
  - **Status:** RED — `Len()` 方法未定义
  - **Verifies:** AC#4 — 注册 N 个驱动后 `Len()` 返回 N
  - **Setup:** 注册 3 个驱动，断言 `Len() == 3`

### Unit Tests — 注册编排 `RegisterProviders` (6 tests)

**File:** `drivers/llm/factory_test.go`

- RED **Test:** `TestRegisterProviders_Default`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#1 — `DefaultProvidersConfig()` 注册 2 个 provider（claude + cursor），`DriverRegistry.Len() == 2`
  - **Setup:** `DefaultProvidersConfig()` + 真实 `DriverRegistry` + mock `DeviceRegisterer`

- RED **Test:** `TestRegisterProviders_WithOpenAICompat`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#1 — 包含 `openai-compat` 类型的 provider 正确注册
  - **Setup:** 含 ollama provider 的 `ProvidersConfig` + 真实 `DriverRegistry` + mock `DeviceRegisterer`

- RED **Test:** `TestRegisterProviders_MultipleProviders`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#1 — 三种驱动类型（claude-cli、cursor-cli、openai-compat）全部注册成功，`DriverRegistry.Len() == 3`
  - **Setup:** 包含三种类型的完整配置

- RED **Test:** `TestRegisterProviders_DriverRegistryPopulated`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#4 — 所有 provider 通过 `DriverRegistry.Get(name)` 可获取，且 `Names()` 返回排序列表
  - **Setup:** 完整配置 + 真实 `DriverRegistry`

- RED **Test:** `TestRegisterProviders_DeviceRegistryPopulated`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#1 — mock `DeviceRegisterer` 记录的注册路径为 `/dev/llm/<name>` 格式
  - **Setup:** 完整配置 + mock `DeviceRegisterer`（记录 `Register` 调用路径）

- RED **Test:** `TestRegisterProviders_VFSRouting`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#2 — 使用真实 `vfs.DeviceRegistry`，注册后 `Open("/dev/llm/<name>")` 路由到正确驱动
  - **Setup:** 完整配置 + 真实 `vfs.DeviceRegistry`

### Integration Tests — VFS 端到端 (3 tests)

**File:** `drivers/llm/factory_test.go`

- RED **Test:** `TestRegisterProviders_VFSOpenRouting`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#2 — 使用真实 `vfs.DeviceRegistry` + `vfs.VFS`，`RegisterProviders` 后 `vfsInst.Open(pid, "/dev/llm/ollama", O_RDWR)` 返回有效 FD，`Stat` 的 `DevicePath` 为 `/dev/llm/ollama`
  - **Setup:** 含 ollama 的完整配置 + 真实 VFS 栈

- RED **Test:** `TestRegisterProviders_DefaultConfig_VFSCompat`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#2 + AC#3 — 使用 `DefaultProvidersConfig()` 注册后 `/dev/llm/claude` 和 `/dev/llm/cursor` 均可通过 VFS Open 访问，行为与旧硬编码注册一致
  - **Setup:** `DefaultProvidersConfig()` + 真实 VFS 栈

- RED **Test:** `TestRegisterProviders_VFSOpenNotFound`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#2 — 注册后 `Open("/dev/llm/nonexistent")` 返回 device not found 错误
  - **Setup:** `DefaultProvidersConfig()` + 真实 VFS 栈

### Error Handling Tests (2 tests)

**File:** `drivers/llm/factory_test.go`

- RED **Test:** `TestRegisterProviders_CreateDriverError`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#1 — 配置包含未知 driver 类型时 `RegisterProviders` fail-fast 返回 error
  - **Setup:** 配置含 `driver: "bad-driver"` 的 provider

- RED **Test:** `TestRegisterProviders_DuplicateProvider`
  - **Status:** RED — `RegisterProviders` 函数未定义
  - **Verifies:** AC#4 — 配置包含同名 provider 时 `DriverRegistry.Register` 返回重复错误，`RegisterProviders` fail-fast
  - **Setup:** 两个 `name: "claude"` 的 provider 配置

---

## AC ↔ Test 覆盖矩阵

| AC | Test(s) | 覆盖方式 |
|----|---------|----------|
| AC1 驱动工厂 | TestCreateDriver_ClaudeCLI, TestCreateDriver_CursorCLI, TestCreateDriver_OpenAICompat, TestCreateDriver_OpenAICompat_WithModel, TestCreateDriver_UnknownDriver, TestCreateDriver_OpenAICompat_MissingBaseURL | 每种 driver 类型的工厂函数分发 + Info() 元数据验证 |
| AC1 注册编排 | TestRegisterProviders_Default, TestRegisterProviders_WithOpenAICompat, TestRegisterProviders_MultipleProviders, TestRegisterProviders_DeviceRegistryPopulated, TestRegisterProviders_CreateDriverError | 编排函数注册流程 + VFS 路径格式 + fail-fast 错误处理 |
| AC2 VFS 路由 | TestRegisterProviders_VFSRouting, TestRegisterProviders_VFSOpenRouting, TestRegisterProviders_DefaultConfig_VFSCompat, TestRegisterProviders_VFSOpenNotFound | 真实 VFS 栈的 Open/Stat 路由验证 |
| AC3 去硬编码 | TestRegisterProviders_DefaultConfig_VFSCompat | 默认配置注册后行为与旧硬编码一致（回归测试）+ 代码审查 |
| AC4 Registry 增强 | TestDriverRegistry_Names, TestDriverRegistry_Names_Empty, TestDriverRegistry_Len, TestRegisterProviders_DriverRegistryPopulated, TestRegisterProviders_DuplicateProvider | Names/Len 方法 + 编排后 Registry 状态验证 |

---

## Test Implementation Plan

### `drivers/llm/factory_test.go` — 测试函数签名

```go
package llm

import (
    "testing"

    "github.com/rnixai/rnix/vfs"
    "github.com/rnixai/rnix/internal/types"
)

// --- Mock DeviceRegisterer ---

type mockDeviceRegisterer struct {
    paths []string
}

func (m *mockDeviceRegisterer) Register(path string, factory vfs.VFSFileFactory) error {
    m.paths = append(m.paths, path)
    return nil
}

// --- AC1: CreateDriver 工厂函数 ---

func TestCreateDriver_ClaudeCLI(t *testing.T)
func TestCreateDriver_CursorCLI(t *testing.T)
func TestCreateDriver_OpenAICompat(t *testing.T)
func TestCreateDriver_OpenAICompat_WithModel(t *testing.T)
func TestCreateDriver_UnknownDriver(t *testing.T)
func TestCreateDriver_OpenAICompat_MissingBaseURL(t *testing.T)

// --- AC1+AC4: RegisterProviders 注册编排 ---

func TestRegisterProviders_Default(t *testing.T)
func TestRegisterProviders_WithOpenAICompat(t *testing.T)
func TestRegisterProviders_MultipleProviders(t *testing.T)
func TestRegisterProviders_DriverRegistryPopulated(t *testing.T)
func TestRegisterProviders_DeviceRegistryPopulated(t *testing.T)
func TestRegisterProviders_VFSRouting(t *testing.T)

// --- AC2: VFS Integration ---

func TestRegisterProviders_VFSOpenRouting(t *testing.T)
func TestRegisterProviders_DefaultConfig_VFSCompat(t *testing.T)
func TestRegisterProviders_VFSOpenNotFound(t *testing.T)

// --- Error handling ---

func TestRegisterProviders_CreateDriverError(t *testing.T)
func TestRegisterProviders_DuplicateProvider(t *testing.T)
```

### `drivers/llm/registry_test.go` — 新增测试函数签名

```go
func TestDriverRegistry_Names(t *testing.T)
func TestDriverRegistry_Names_Empty(t *testing.T)
func TestDriverRegistry_Len(t *testing.T)
```

---

## 测试隔离策略

- **Mock DeviceRegisterer:** `factory_test.go` 定义 `mockDeviceRegisterer` 实现 `DeviceRegisterer` 接口，记录注册路径用于断言
- **真实 VFS 栈:** 集成测试使用 `vfs.NewDeviceRegistry()` + `vfs.NewVFS(devReg)` 验证端到端路由
- **并行安全:** 所有测试标记 `t.Parallel()`（本 Story 不涉及 `t.Setenv` 或 CWD 修改）
- **Race 检测:** 通过 `go test -race` 运行
- **无外部依赖:** 工厂函数创建的驱动实例不调用外部进程或网络，仅验证类型和元数据

---

## Running Tests

```bash
# 运行 Story 23-2 相关的所有测试
go test -race -v -run "TestCreateDriver|TestRegisterProviders|TestDriverRegistry_Names|TestDriverRegistry_Len" ./drivers/llm/...

# 仅运行工厂函数测试
go test -race -v -run "TestCreateDriver" ./drivers/llm/...

# 仅运行注册编排测试
go test -race -v -run "TestRegisterProviders" ./drivers/llm/...

# 仅运行 Registry 增强测试
go test -race -v -run "TestDriverRegistry_Names|TestDriverRegistry_Len" ./drivers/llm/...

# 运行全部 drivers/llm 测试（包括 Story 23-1 回归）
go test -race -v ./drivers/llm/...
```

---

## Red-Green-Refactor Workflow

### Phase 1: RED — 编写失败测试

1. 在 `drivers/llm/factory_test.go` 编写 17 个测试（6 工厂 + 6 编排 + 3 VFS 集成 + 2 错误处理）
2. 在 `drivers/llm/registry_test.go` 追加 3 个测试（Names + Names_Empty + Len）
3. 运行 `go test ./drivers/llm/...` 验证全部编译失败（`CreateDriver`、`RegisterProviders`、`DeviceRegisterer`、`Names`、`Len` 未定义）

### Phase 2: GREEN — 最小实现

按 Task 顺序逐步实现：

| 实现步骤 | 文件 | 内容 | 解锁测试 |
|---------|------|------|---------|
| 2.1 | `drivers/llm/registry.go` | 添加 `Names() []string` 和 `Len() int` | TestDriverRegistry_Names, TestDriverRegistry_Names_Empty, TestDriverRegistry_Len |
| 2.2 | `drivers/llm/factory.go` | `CreateDriver(cfg ProviderConfig) (LLMDriver, error)` 工厂函数 | TestCreateDriver_* (6 tests) |
| 2.3 | `drivers/llm/factory.go` | `DeviceRegisterer` 接口 | 编译依赖 |
| 2.4 | `drivers/llm/factory.go` | `RegisterProviders(cfg, driverReg, devReg) error` 编排函数 | TestRegisterProviders_* (11 tests) |

每步后运行 `go test -race ./drivers/llm/...` 验证新增测试从 RED 转 GREEN。

### Phase 3: REFACTOR

1. 运行 `go vet ./drivers/llm/...` 确保无告警
2. 运行 `golangci-lint run ./drivers/llm/...` 确保风格一致
3. 审查 `main.go` 重构（Task 4），确认无硬编码残留

---

## Implementation Checklist

| Task | 描述 | 对应测试 | 状态 |
|------|------|---------|------|
| Task 1: `CreateDriver` 工厂函数 | `drivers/llm/factory.go` — 根据 `cfg.Driver` 分发到构造函数 | TestCreateDriver_ClaudeCLI, TestCreateDriver_CursorCLI, TestCreateDriver_OpenAICompat, TestCreateDriver_OpenAICompat_WithModel, TestCreateDriver_UnknownDriver, TestCreateDriver_OpenAICompat_MissingBaseURL | ☐ |
| Task 2: `DriverRegistry.Names()` / `Len()` | `drivers/llm/registry.go` — 使用 `Range()` 收集 key | TestDriverRegistry_Names, TestDriverRegistry_Names_Empty, TestDriverRegistry_Len | ☐ |
| Task 3: `RegisterProviders` 编排函数 | `drivers/llm/factory.go` — 遍历配置，调用 CreateDriver + Register + VFS 挂载 | TestRegisterProviders_Default, TestRegisterProviders_WithOpenAICompat, TestRegisterProviders_MultipleProviders, TestRegisterProviders_DriverRegistryPopulated, TestRegisterProviders_DeviceRegistryPopulated, TestRegisterProviders_VFSRouting, TestRegisterProviders_VFSOpenRouting, TestRegisterProviders_DefaultConfig_VFSCompat, TestRegisterProviders_VFSOpenNotFound, TestRegisterProviders_CreateDriverError, TestRegisterProviders_DuplicateProvider | ☐ |
| Task 4: 重构 `main.go` | 替换硬编码注册为 `LoadOrDefaultProvidersConfig` + `RegisterProviders` | TestRegisterProviders_DefaultConfig_VFSCompat (回归) | ☐ |

---

## 待实现目标文件

| 文件 | 状态 | 说明 |
|------|------|------|
| `drivers/llm/factory.go` | 待创建 | `CreateDriver` 工厂、`DeviceRegisterer` 接口、`RegisterProviders` 编排函数 |
| `drivers/llm/factory_test.go` | 待创建 (RED) | 17 个测试 — 工厂 + 编排 + VFS 集成 + 错误处理 |
| `drivers/llm/registry.go` | 待修改 | 添加 `Names() []string` 和 `Len() int` 方法 |
| `drivers/llm/registry_test.go` | 待修改 (RED) | 追加 3 个测试 — Names + Names_Empty + Len |
| `cmd/rnix/main.go` | 待修改 | `runDaemon` 第 1061-1066 行替换硬编码注册 |

---

## 下一步

1. 在 `drivers/llm/factory_test.go` 和 `drivers/llm/registry_test.go` 编写全部 RED 测试
2. 运行 `go test ./drivers/llm/...` 确认全部编译失败
3. 按 Task 1 → 2 → 3 → 4 顺序实现，每步验证测试转 GREEN
4. 重构 `cmd/rnix/main.go`，运行全量测试确认回归
5. 运行 `make all` 确保 lint + vet + test + build 全部通过
