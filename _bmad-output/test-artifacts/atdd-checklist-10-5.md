---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-03'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/10-5-init-bootstrap-sequence.md'
  - '_bmad/tea/testarch/knowledge/test-quality.md'
  - '_bmad/tea/testarch/knowledge/test-levels-framework.md'
  - '_bmad/tea/testarch/knowledge/data-factories.md'
  - 'kernel/supervisor_test.go'
  - 'kernel/kernel_test.go'
---

# ATDD Checklist - Epic 10, Story 5: init 引导序列

**Date:** 2026-03-03
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

Story 10.5 实现 daemon 启动时的配置驱动 init 引导序列，按配置文件初始化系统级服务和 Supervisor 树。

**As a** 系统
**I want** daemon 启动时按配置初始化系统级服务和 Supervisor 树
**So that** 系统启动后所有基础设施就位

---

## Acceptance Criteria

1. **AC1: 配置驱动的 init 引导序列** — daemon 启动时按配置文件初始化系统级服务（日志聚合、Skill 注册表、MCP 管理器），并构建 Supervisor 树
2. **AC2: 必须服务启动失败** — required 服务失败时 daemon 启动失败，输出具体错误信息和恢复建议
3. **AC3: 可选服务启动失败** — optional 服务失败时记录警告，继续启动其余服务

---

## 生成模式

**AI 生成模式**（backend Go 项目，无需浏览器录制）

---

## 测试策略

### AC → 测试场景映射

| 测试 ID | AC | 级别 | 优先级 | 场景描述 |
|---------|-----|------|--------|---------|
| 10.5-UNIT-001 | AC1 | Unit | P0 | 默认配置（无服务无 Supervisor）→ Bootstrap 成功，InitResult 为空 |
| 10.5-UNIT-002 | AC2 | Unit | P0 | required 服务失败 → Bootstrap 返回 error，含服务名和恢复建议 |
| 10.5-UNIT-003 | AC3 | Unit | P0 | optional 服务失败 → Bootstrap 成功，InitResult.Warnings 含警告 |
| 10.5-UNIT-004 | AC1 | Unit | P0 | Supervisor 树从 config 构建 → SpawnSupervisor 成功 |
| 10.5-UNIT-005 | AC2 | Unit | P0 | required Supervisor 构建失败 → Bootstrap 返回 error |
| 10.5-INT-001 | AC1,2,3 | Integration | P1 | 混合场景：2 required + 1 optional 服务 + 1 Supervisor → 全部成功 |
| 10.5-INT-002 | AC1 | Integration | P1 | AgentLoaderFunc 加载 agent → ChildSpec.Agent 正确设置 |
| 10.5-REG-001 | ALL | Regression | P2 | 编译时类型检查 — 确保公共 API 类型存在 |

### 测试级别选择理由

- **Unit 测试**（P0，5 个）：Bootstrap 函数是纯逻辑函数，接收注入的 mock ServiceInitializer 和 AgentLoaderFunc，无外部依赖，适合单元测试覆盖核心路径
- **Integration 测试**（P1，2 个）：验证多服务 + Supervisor 混合场景和 AgentLoader 集成，需要真实 KernelImpl 和 VFS 环境
- **Regression 测试**（P2，1 个）：确保 init_test.go 不引入编译或行为回归

---

## Failing Tests Created (RED Phase)

### Unit Tests (5 tests)

**File:** `kernel/init_test.go` (324 lines)

- **Test:** `TestBootstrap_DefaultConfig_EmptyResult`
  - **Status:** RED — `DefaultInitConfig` 未定义（编译失败）
  - **Verifies:** AC1 — 空配置 Bootstrap 成功

- **Test:** `TestBootstrap_RequiredServiceFailure_ReturnsError`
  - **Status:** RED — `InitConfig`, `ServiceConfig`, `serviceRegistry` 未定义
  - **Verifies:** AC2 — required 服务失败时返回带服务名的 error

- **Test:** `TestBootstrap_OptionalServiceFailure_SucceedsWithWarnings`
  - **Status:** RED — `InitConfig`, `ServiceConfig`, `serviceRegistry` 未定义
  - **Verifies:** AC3 — optional 服务失败时 Bootstrap 成功 + warnings

- **Test:** `TestBootstrap_SupervisorTreeConstruction_Succeeds`
  - **Status:** RED — `SupervisorConfig`, `ChildConfig` 未定义
  - **Verifies:** AC1 — Supervisor 树配置驱动构建

- **Test:** `TestBootstrap_RequiredSupervisorFailure_ReturnsError`
  - **Status:** RED — `SupervisorConfig`, `ChildConfig` 未定义
  - **Verifies:** AC2 — required Supervisor 失败时 Bootstrap 返回 error

### Integration Tests (2 tests)

**File:** `kernel/init_test.go` (same file)

- **Test:** `TestBootstrap_MixedScenario_AllSucceed`
  - **Status:** RED — 全部 init 类型未定义
  - **Verifies:** AC1+AC2+AC3 — 混合场景端到端成功

- **Test:** `TestBootstrap_AgentLoaderFunc_SetsChildAgent`
  - **Status:** RED — `AgentLoaderFunc` 未定义
  - **Verifies:** AC1 — AgentLoader 集成到 Supervisor 子进程

### Regression Tests (1 test)

**File:** `kernel/init_test.go` (same file)

- **Test:** `TestInit_TypesExist`
  - **Status:** RED — 所有公共类型和函数未定义
  - **Verifies:** 编译时类型检查，确保公共 API 存在

---

## Test Infrastructure Created

### Mock Infrastructure

**File:** `kernel/init_test.go`（内置在测试文件中）

**Mock Types:**

- `mockServiceInitializer` — 可控的 `ServiceInitializer` mock，支持注入错误
- `mockAgentLoader` — 返回 `AgentLoaderFunc`，从 map 中解析已知 agent 名
- `newInitTestKernel` — 复用 `newSimpleTestKernel` + `normalFile`，为 init 测试创建内核

**复用现有 Mock:**

- `normalFile` — 返回 "done" 的 LLM 设备 mock（来自 `supervisor_test.go`）
- `newSimpleTestKernel` — 创建带单一 LLM 文件的测试内核（来自 `supervisor_test.go`）

---

## Mock Requirements

### ServiceInitializer Mock

**接口:** `ServiceInitializer`

**成功响应:**
```go
Init(cfg map[string]any) error → nil
```

**失败响应:**
```go
Init(cfg map[string]any) error → fmt.Errorf("scan_path does not exist")
```

**注意:** 通过 `serviceRegistry` 覆盖注入 mock，测试后恢复原始注册表。

### AgentLoaderFunc Mock

**类型:** `func(name string) (*agents.AgentInfo, error)`

**成功响应:**
```go
mockAgentLoader(map[string]*agents.AgentInfo{"test-agent": testAgent})
```

**失败响应:**
```go
mockAgentLoader(nil) // 任何 agent 名都返回 "agent not found" error
```

---

## Implementation Checklist

### Test: TestBootstrap_DefaultConfig_EmptyResult

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 创建 `kernel/init.go` 文件
- [ ] 定义 `InitConfig` 结构体
- [ ] 定义 `DefaultInitConfig()` 函数（返回空配置）
- [ ] 定义 `InitResult` 结构体（Started, Warnings, Failed 字段）
- [ ] 定义 `AgentLoaderFunc` 类型
- [ ] 实现 `Bootstrap()` 函数基本框架（空配置路径）
- [ ] Run test: `go test ./kernel/ -run TestBootstrap_DefaultConfig_EmptyResult -race`
- [ ] Test passes (green phase)

---

### Test: TestBootstrap_RequiredServiceFailure_ReturnsError

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 定义 `ServiceConfig` 结构体（Name, Type, Required, Config）
- [ ] 定义 `ServiceInitializer` 接口（Name(), Init()）
- [ ] 定义 `ServiceError` 结构体
- [ ] 定义 `serviceRegistry` 变量（map[string]func() ServiceInitializer）
- [ ] 实现 Bootstrap Phase 1：遍历 services，调用 Init()
- [ ] 实现 required 服务失败 → 返回 error（含服务名）
- [ ] Run test: `go test ./kernel/ -run TestBootstrap_RequiredServiceFailure -race`
- [ ] Test passes (green phase)

---

### Test: TestBootstrap_OptionalServiceFailure_SucceedsWithWarnings

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 实现 optional 服务失败 → 记录 warning 继续
- [ ] Warning 信息包含服务名
- [ ] Run test: `go test ./kernel/ -run TestBootstrap_OptionalServiceFailure -race`
- [ ] Test passes (green phase)

---

### Test: TestBootstrap_SupervisorTreeConstruction_Succeeds

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 定义 `SupervisorConfig` 结构体
- [ ] 定义 `ChildConfig` 结构体
- [ ] 实现 `SupervisorConfig.toSupervisorSpec()` 转换
- [ ] 实现 Bootstrap Phase 2：遍历 supervisors，调用 SpawnSupervisor()
- [ ] 成功的 Supervisor 记录到 InitResult.Started
- [ ] Run test: `go test ./kernel/ -run TestBootstrap_SupervisorTreeConstruction -race`
- [ ] Test passes (green phase)

---

### Test: TestBootstrap_RequiredSupervisorFailure_ReturnsError

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 实现 required Supervisor 失败 → 回滚已启动的 Supervisor → 返回 error
- [ ] Error 信息包含 Supervisor 名
- [ ] Run test: `go test ./kernel/ -run TestBootstrap_RequiredSupervisorFailure -race`
- [ ] Test passes (green phase)

---

### Test: TestBootstrap_MixedScenario_AllSucceed

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 实现内置 `skillRegistryService`（扫描 lib/skills/）
- [ ] 实现内置 `mcpManagerService`（验证 mcp.yaml）
- [ ] 实现内置 `logAggregatorService`（no-op 占位符）
- [ ] 注册到 `serviceRegistry`
- [ ] Run test: `go test ./kernel/ -run TestBootstrap_MixedScenario -race`
- [ ] Test passes (green phase)

---

### Test: TestBootstrap_AgentLoaderFunc_SetsChildAgent

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `toSupervisorSpec()` 中 agentLoader 调用
- [ ] ChildConfig.Agent 非空时调用 agentLoader 加载 AgentInfo
- [ ] 设置 ChildSpec.Agent 字段
- [ ] Run test: `go test ./kernel/ -run TestBootstrap_AgentLoaderFunc -race`
- [ ] Test passes (green phase)

---

### Test: TestInit_TypesExist

**File:** `kernel/init_test.go`

**Tasks to make this test pass:**

- [ ] 所有上述类型和函数定义完成
- [ ] Run test: `go test ./kernel/ -run TestInit_TypesExist -race`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all init bootstrap tests
go test ./kernel/ -run "TestBootstrap_|TestInit_" -race -v

# Run specific test
go test ./kernel/ -run TestBootstrap_DefaultConfig_EmptyResult -race -v

# Run all kernel tests (including regression check)
go test ./kernel/... -race

# Run with verbose output
go test ./kernel/ -run "TestBootstrap_|TestInit_" -race -v -count=1

# Run with coverage
go test ./kernel/ -run "TestBootstrap_|TestInit_" -race -coverprofile=coverage.out
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 8 tests written and failing (compilation errors)
- Mock infrastructure created (mockServiceInitializer, mockAgentLoader, newInitTestKernel)
- 复用现有 mock（normalFile, newSimpleTestKernel）
- Implementation checklist created

**Verification:**

```
$ go test ./kernel/... 2>&1 | head -5
# github.com/usecrux/crux/kernel [github.com/usecrux/crux/kernel.test]
kernel/init_test.go:32:58: undefined: AgentLoaderFunc
kernel/init_test.go:46:9: undefined: DefaultInitConfig
kernel/init_test.go:48:17: undefined: Bootstrap
kernel/init_test.go:66:10: undefined: InitConfig
```

- All tests fail due to missing implementation (compilation errors), not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **创建 `kernel/init.go`** — 定义所有类型和接口
2. **实现 Bootstrap 函数** — Phase 1 (services) + Phase 2 (supervisors)
3. **实现内置服务** — skillRegistryService, mcpManagerService, logAggregatorService
4. **逐个运行测试** — 每实现一个功能运行对应测试
5. **修改 `cmd/crux/main.go`** — runDaemon 集成 Bootstrap 调用

**Key Principles:**

- 一次一个测试（从 UNIT-001 开始）
- 最小实现（不要过度工程化）
- 频繁运行测试（即时反馈）

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 验证所有 8 个测试通过
2. 审查代码质量（可读性、可维护性）
3. 提取重复代码
4. 确保测试仍然通过
5. 运行 `make all` 完整检查

---

## Next Steps

1. **运行失败测试确认 RED 阶段**: `go test ./kernel/ -run "TestBootstrap_|TestInit_" -race 2>&1 | head -5`
2. **开始实现** — 使用 `bmad-bmm-dev-story` workflow 执行 Story 10.5
3. **逐个让测试变绿** — 按 Implementation Checklist 顺序
4. **全部通过后重构** — 运行 `make all` 确认无回归
5. **更新 story 状态** — 手动更新 sprint-status.yaml

---

## Knowledge Base References Applied

- **test-quality.md** — 测试质量标准：确定性、隔离性、显式断言
- **test-levels-framework.md** — 测试级别选择：Unit 用于纯逻辑，Integration 用于组件交互
- **data-factories.md** — mock factory 模式应用于 mockServiceInitializer 和 mockAgentLoader
- **supervisor_test.go** — 复用现有 mock 基础设施（normalFile, newSimpleTestKernel, waitSupervisor）

See `tea-index.csv` for complete knowledge fragment mapping.

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./kernel/... 2>&1 | head -15`

**Results:**

```
# github.com/usecrux/crux/kernel [github.com/usecrux/crux/kernel.test]
kernel/init_test.go:32:58: undefined: AgentLoaderFunc
kernel/init_test.go:46:9: undefined: DefaultInitConfig
kernel/init_test.go:48:17: undefined: Bootstrap
kernel/init_test.go:66:10: undefined: InitConfig
kernel/init_test.go:67:15: undefined: ServiceConfig
kernel/init_test.go:78:18: undefined: serviceRegistry
kernel/init_test.go:79:2: undefined: serviceRegistry
kernel/init_test.go:79:38: undefined: ServiceInitializer
kernel/init_test.go:80:28: undefined: ServiceInitializer
kernel/init_test.go:87:17: undefined: serviceRegistry
kernel/init_test.go:87:17: too many errors
FAIL    github.com/usecrux/crux/kernel [build failed]
```

**Summary:**

- Total tests: 8
- Passing: 0 (expected)
- Failing: 8 (expected — compilation errors)
- Status: RED phase verified

---

## Validation Checklist

- [x] Story 有明确的 acceptance criteria（3 个 AC）
- [x] 测试框架已配置（Go testing + race detector）
- [x] 所有 AC 映射到测试场景
- [x] 测试级别适当选择（Unit P0, Integration P1, Regression P2）
- [x] 避免重复覆盖（每个 AC 不在多个级别重复测试）
- [x] 测试在实现前失败（编译错误 = 最强 RED 信号）
- [x] Mock 基础设施完整（mockServiceInitializer, mockAgentLoader）
- [x] 复用现有 mock（normalFile, newSimpleTestKernel）
- [x] Implementation checklist 完整（每个测试映射到具体任务）
- [x] 测试命令可用
- [x] ATDD checklist 文档生成到正确位置

---

## Notes

- Go 后端项目的 RED 阶段 = 编译失败（比 `test.skip()` 更强的信号）
- 所有测试在同一个文件 `kernel/init_test.go` 中，与 `kernel/init.go` 对应
- Mock 通过 `serviceRegistry` 变量覆盖注入，测试后 defer 恢复
- Integration 测试使用真实 KernelImpl + VFS，但 service 实现为内置 no-op

---

**Generated by BMad TEA Agent** - 2026-03-03
