---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-10'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/20-1-ooda-loop-core-implementation.md'
  - 'kernel/kernel.go'
  - 'kernel/kernel_test.go'
  - 'kernel/process.go'
  - 'internal/types/types.go'
---

# ATDD Checklist - Epic 20, Story 20-1: OODA 循环核心实现

**Date:** 2026-03-10
**Author:** Decker
**Primary Test Level:** Unit / Integration (Go backend)

---

## Story Summary

实现 OODA（感知-判断-决策-行动）循环作为智能体的新推理模式，与现有的线性 reasonStep 并行存在。

**As a** 平台构建者
**I want** 智能体可以在推理循环中执行 OODA 四阶段（感知-判断-决策-行动）
**So that** 智能体能够自主感知环境变化并做出适应性决策

---

## Acceptance Criteria

1. **AC #1** - Observe 阶段：智能体通过 VFS 读取环境信息（/proc/ 状态、其他进程输出、文件变更）
2. **AC #2** - Orient 阶段：智能体评估感知数据与目标的偏差
3. **AC #3** - Decide 阶段：智能体自主选择下一步行动（调用工具、spawn 子进程、请求协作或调整计划）
4. **AC #4** - Act 阶段：执行决策并将结果反馈到下一轮 Observe 形成闭环，单轮 OODA 循环框架开销 <= 200ms（NFR41）

---

## Test Strategy

**Stack Type:** backend (Go)
**Generation Mode:** AI Generation（纯后端项目，无 UI 测试需求）

### Test Level Mapping

| AC | Test Level | Rationale |
|----|-----------|-----------|
| #1-#4 Types | Unit | 纯类型定义、常量正确性验证 |
| #1-#4 Process OODA State | Unit | Process 结构体字段和方法的线程安全测试 |
| #1-#4 OODA Cycle | Integration | 完整的 Spawn + OODA reasonStep 循环，需要 kernel + VFS + context 协作 |
| #4 NFR41 | Performance | 框架开销基准测试（使用 mock LLM 即时返回隔离框架代码） |
| Compatibility | Regression | 确保默认 SpawnOpts 不影响现有线性推理 |
| Events/Logs | Integration | 验证 OODA 阶段产生正确的 SyscallEvent 和 LogEntry |

### Priority Assignment

| Test | Priority | Risk |
|------|----------|------|
| TestOODAPhase_Constants | P1 | 低 - 基础类型定义 |
| TestOODADecision_Types | P1 | 低 - 基础类型定义 |
| TestOODAState_Struct | P1 | 低 - 结构体字段验证 |
| TestProcess_OODAState | P0 | 高 - 核心 Process 状态扩展 |
| TestProcess_OODAState_SetPhase | P0 | 高 - OODA 状态管理 |
| TestProcess_OODAState_ConcurrentAccess | P0 | 高 - 并发安全是核心要求 |
| TestOODAReasonStep_SingleCycle | P0 | 高 - OODA 核心路径 |
| TestOODAReasonStep_MultipleCycles | P0 | 高 - 循环反馈机制 |
| TestOODAReasonStep_SpawnAction | P1 | 中 - 子进程创建分支 |
| TestOODAReasonStep_ReplanAction | P1 | 中 - Replan 分支 |
| TestOODAReasonStep_MaxCyclesExceeded | P0 | 高 - 安全退出机制 |
| TestOODAReasonStep_ContextCancellation | P0 | 高 - 优雅退出机制 |
| TestOODAReasonStep_ObserveError | P1 | 中 - 错误恢复 |
| TestOODAReasonStep_FrameworkOverhead | P0 | 高 - NFR41 性能约束 |
| TestSpawn_DefaultReasoningMode | P0 | 高 - 回归保护 |
| TestSpawn_OODAReasoningMode | P0 | 高 - OODA 入口验证 |
| TestOODAReasonStep_SyscallEvents | P1 | 中 - 可观测性 |
| TestOODAReasonStep_LogEntries | P1 | 中 - 可观测性 |
| TestLogOODA_Category | P1 | 低 - 类型常量 |

---

## Failing Tests Created (RED Phase)

### Unit / Integration Tests (19 tests)

**File:** `kernel/ooda_test.go` (约 550 行)

- **Test:** `TestOODAPhase_Constants`
  - **Status:** RED - undefined: PhaseObserve, OODAPhase
  - **Verifies:** AC #1-#4 - OODA 阶段常量定义正确性

- **Test:** `TestOODADecision_Types`
  - **Status:** RED - undefined: OODAToolCall, OODAActionType
  - **Verifies:** AC #1-#4 - 决策类型常量完整性

- **Test:** `TestOODAState_Struct`
  - **Status:** RED - undefined: OODAState, OODADecision
  - **Verifies:** AC #1-#4 - OODAState 结构体字段完整性

- **Test:** `TestProcess_OODAState`
  - **Status:** RED - proc.IsOODA undefined
  - **Verifies:** AC #1-#4 - Process 的 OODA 状态默认值

- **Test:** `TestProcess_OODAState_SetPhase`
  - **Status:** RED - proc.SetOODAPhase undefined
  - **Verifies:** AC #1-#4 - OODA 状态设置和读取

- **Test:** `TestProcess_OODAState_ConcurrentAccess`
  - **Status:** RED - proc.SetOODAPhase/IsOODA/GetOODAState undefined
  - **Verifies:** AC #1-#4 - Process 的 OODA 状态并发读写安全

- **Test:** `TestOODAReasonStep_SingleCycle`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #1-#4 - 单轮 OODA 循环正常完成

- **Test:** `TestOODAReasonStep_MultipleCycles`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #1-#4 - 多轮 OODA 循环（tool_call -> complete）反馈机制

- **Test:** `TestOODAReasonStep_SpawnAction`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #3 - Decide 选择 spawn 时创建子进程

- **Test:** `TestOODAReasonStep_ReplanAction`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #3 - Decide 选择 replan 时不执行外部操作

- **Test:** `TestOODAReasonStep_MaxCyclesExceeded`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #4 - 超过最大循环数时正确终止（exit code != 0）

- **Test:** `TestOODAReasonStep_ContextCancellation`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #4 - OODA 循环中 context 取消时优雅退出

- **Test:** `TestOODAReasonStep_ObserveError`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #1 - Observe 阶段 VFS 读取失败时的错误处理

- **Test:** `TestOODAReasonStep_FrameworkOverhead`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** NFR41 - 单轮 OODA 框架开销 <= 200ms

- **Test:** `TestSpawn_DefaultReasoningMode`
  - **Status:** RED - proc.IsOODA undefined
  - **Verifies:** 回归 - 默认 SpawnOpts 使用线性推理，不启用 OODA

- **Test:** `TestSpawn_OODAReasoningMode`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** 入口 - ReasoningMode: "ooda" 启用 OODA 循环

- **Test:** `TestOODAReasonStep_SyscallEvents`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #1-#4 - OODA 各阶段产生正确的 SyscallEvent

- **Test:** `TestOODAReasonStep_LogEntries`
  - **Status:** RED - SpawnOpts.ReasoningMode undefined
  - **Verifies:** AC #1-#4 - OODA 各阶段产生正确的 LogEntry（LogOODA 类别）

- **Test:** `TestLogOODA_Category`
  - **Status:** RED - types.LogOODA undefined
  - **Verifies:** types 包中 LogOODA 常量定义

---

## Mock Infrastructure

### mockDynamicLLMFile

**File:** `kernel/ooda_test.go`

**Purpose:** 动态响应 LLM mock，根据 Write 数据决定 Read 返回值。允许模拟多轮 OODA 循环中不同阶段的 LLM 响应。

**Exports:**
- `newOODATestKernel(t, responseFunc)` - 创建带有动态 LLM mock 的测试 kernel

**Reuses:**
- `mockLLMFile` (from `kernel_test.go`) - 静态 LLM mock，用于兼容性测试
- `newTestKernel(t, llmFile)` (from `kernel_test.go`) - 标准测试 kernel 工厂
- `makeLLMResponse(content, tokens)` (from `kernel_test.go`) - LLM 响应 JSON 构建器

---

## Implementation Checklist

### Test: TestOODAPhase_Constants / TestOODADecision_Types / TestOODAState_Struct

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `kernel/ooda.go` 中定义 `OODAPhase` 类型和四个阶段常量
- [ ] 在 `kernel/ooda.go` 中定义 `OODAActionType` 类型和四个动作常量
- [ ] 在 `kernel/ooda.go` 中定义 `OODAState` 和 `OODADecision` 结构体
- [ ] Run test: `go test -race -run "TestOODAPhase_Constants|TestOODADecision_Types|TestOODAState_Struct" ./kernel/`

---

### Test: TestProcess_OODAState / TestProcess_OODAState_SetPhase / TestProcess_OODAState_ConcurrentAccess

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `kernel/process.go` 的 `Process` 结构体新增 `oodaEnabled bool` 和 `oodaState *OODAState` 字段（mu 保护）
- [ ] 实现 `IsOODA() bool` 方法（mu.Lock 读取 oodaEnabled）
- [ ] 实现 `GetOODAState() *OODAState` 方法（mu.Lock 读取 oodaState）
- [ ] 实现 `SetOODAPhase(phase OODAPhase)` 方法（mu.Lock 设置 oodaEnabled=true，初始化/更新 oodaState）
- [ ] Run test: `go test -race -run "TestProcess_OODAState" ./kernel/`

---

### Test: TestOODAReasonStep_SingleCycle

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `kernel/kernel.go` 的 `SpawnOpts` 新增 `ReasoningMode string` 字段
- [ ] 在 `Spawn` 方法中根据 `opts.ReasoningMode == "ooda"` 设置 `proc.oodaEnabled = true` 并初始化 `proc.oodaState`
- [ ] 在 `Spawn` 的 goroutine 启动处添加 OODA 模式分支：`if proc.oodaEnabled { k.oodaReasonStep(...) } else { k.reasonStep(...) }`
- [ ] 在 `kernel/ooda.go` 实现 `oodaReasonStep` 方法骨架
- [ ] 实现 `oodaObserve` 方法（VFS 读取环境信息）
- [ ] 实现 `oodaOrient` 方法（通过 LLM 评估偏差）
- [ ] 实现 `oodaDecide` 方法（通过 LLM 输出结构化决策）
- [ ] 实现 `oodaAct` 方法（根据决策类型执行，OODAComplete 路径）
- [ ] 实现 `finishProcess` 调用确保 Zombie 状态转换
- [ ] Run test: `go test -race -run "TestOODAReasonStep_SingleCycle" ./kernel/`

---

### Test: TestOODAReasonStep_MultipleCycles

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `oodaAct` 中实现 `OODAToolCall` 分支（VFS Open/Write/Read/Close 工具调用）
- [ ] 确保 Act 结果写入上下文供下一轮 Observe 使用
- [ ] 验证循环递增和状态传递
- [ ] Run test: `go test -race -run "TestOODAReasonStep_MultipleCycles" ./kernel/`

---

### Test: TestOODAReasonStep_SpawnAction

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `oodaAct` 中实现 `OODASpawn` 分支（通过 `k.Spawn()` 创建子进程）
- [ ] Run test: `go test -race -run "TestOODAReasonStep_SpawnAction" ./kernel/`

---

### Test: TestOODAReasonStep_ReplanAction

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `oodaAct` 中实现 `OODAReplan` 分支（仅写入上下文，不执行外部操作）
- [ ] Run test: `go test -race -run "TestOODAReasonStep_ReplanAction" ./kernel/`

---

### Test: TestOODAReasonStep_MaxCyclesExceeded

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `oodaReasonStep` 的 for 循环中检查 `cycle <= maxCycles`
- [ ] 超过 maxCycles 时调用 `k.finishProcess(proc, ExitStatus{Code: 1, Reason: "max ooda cycles exceeded"})`
- [ ] Run test: `go test -race -run "TestOODAReasonStep_MaxCyclesExceeded" ./kernel/`

---

### Test: TestOODAReasonStep_ContextCancellation

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `oodaReasonStep` 循环内检查 `proc.ctx.Err()`，取消时退出
- [ ] 确保 Kill 触发 context 取消传播到 oodaReasonStep
- [ ] Run test: `go test -race -run "TestOODAReasonStep_ContextCancellation" ./kernel/`

---

### Test: TestOODAReasonStep_FrameworkOverhead (NFR41)

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 确保 Observe 阶段是纯框架代码（不调用 LLM），开销极小
- [ ] 确保 Orient/Decide/Act 的框架开销（排除 LLM 调用）在合理范围内
- [ ] Run test: `go test -race -run "TestOODAReasonStep_FrameworkOverhead" ./kernel/`

---

### Test: TestSpawn_DefaultReasoningMode / TestSpawn_OODAReasoningMode

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 确保 `SpawnOpts{ReasoningMode: ""}` 走默认 reasonStep 路径
- [ ] 确保 `SpawnOpts{ReasoningMode: "ooda"}` 走 oodaReasonStep 路径
- [ ] Run test: `go test -race -run "TestSpawn_DefaultReasoningMode|TestSpawn_OODAReasoningMode" ./kernel/`

---

### Test: TestOODAReasonStep_SyscallEvents / TestOODAReasonStep_LogEntries

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `oodaObserve`/`oodaOrient`/`oodaDecide`/`oodaAct` 中调用 `k.emitEvent()` 产生 OODAObserve/OODAOrient/OODADecide/OODAAct 事件
- [ ] 在 OODA 循环完成后调用 `k.emitEvent()` 产生 OODACycle 事件
- [ ] 在各阶段调用 `k.emitLog()` 使用 `types.LogOODA` 类别
- [ ] Run test: `go test -race -run "TestOODAReasonStep_SyscallEvents|TestOODAReasonStep_LogEntries" ./kernel/`

---

### Test: TestLogOODA_Category

**File:** `kernel/ooda_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `internal/types/types.go` 的 LogCategory 常量块新增 `LogOODA LogCategory = "ooda"`
- [ ] Run test: `go test -race -run "TestLogOODA_Category" ./kernel/`

---

## Running Tests

```bash
# Run all failing tests for Story 20-1
go test -race -run "TestOODA|TestSpawn_DefaultReasoningMode|TestSpawn_OODAReasoningMode|TestLogOODA" ./kernel/

# Run specific test
go test -race -run TestOODAReasonStep_SingleCycle ./kernel/

# Run with verbose output
go test -race -v -run "TestOODA" ./kernel/

# Run performance test
go test -race -run TestOODAReasonStep_FrameworkOverhead ./kernel/

# Run all kernel tests (regression check)
go test -race ./kernel/
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 19 tests written and failing (compilation errors due to undefined types/methods)
- Mock infrastructure created (mockDynamicLLMFile + newOODATestKernel)
- Implementation checklist created mapping each test to concrete tasks
- Test file: `kernel/ooda_test.go`

**Verification:**

```
# github.com/rnixai/rnix/kernel [github.com/rnixai/rnix/kernel.test]
kernel/ooda_test.go:23:5: undefined: PhaseObserve
kernel/ooda_test.go:23:21: undefined: OODAPhase
...
FAIL github.com/rnixai/rnix/kernel [build failed]
```

Tests fail due to missing implementation, not test bugs.

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with types** (Task 1): Define OODAPhase, OODAActionType, OODAState, OODADecision in `kernel/ooda.go`
2. **Add LogOODA** (Task 5.2): Add `LogOODA` constant to `internal/types/types.go`
3. **Extend Process** (Task 1.3): Add oodaEnabled/oodaState fields and accessor methods
4. **Extend SpawnOpts** (Task 3.2): Add ReasoningMode field and Spawn branching
5. **Implement oodaReasonStep** (Task 2): Core OODA loop with four phases
6. **Add events/logs** (Task 5): emitEvent/emitLog calls in each phase

**Key Principles:**

- One test at a time (start with type constants, work towards integration)
- Minimal implementation (don't over-engineer)
- Run tests frequently with `-race`

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all 19 tests pass
2. Run full `go test -race ./kernel/` for regression
3. Review OODA code for consistency with existing reasonStep patterns
4. Ensure no duplicate logic between oodaReasonStep and reasonStep

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run "TestOODA|TestSpawn_DefaultReasoningMode|TestSpawn_OODAReasoningMode|TestLogOODA" ./kernel/`

**Results:**

```
# github.com/rnixai/rnix/kernel [github.com/rnixai/rnix/kernel.test]
kernel/ooda_test.go:23:5: undefined: PhaseObserve
kernel/ooda_test.go:23:21: undefined: OODAPhase
kernel/ooda_test.go:26:5: undefined: PhaseOrient
kernel/ooda_test.go:29:5: undefined: PhaseDecide
kernel/ooda_test.go:32:5: undefined: PhaseAct
...
FAIL	github.com/rnixai/rnix/kernel [build failed]
```

**Summary:**

- Total tests: 19
- Passing: 0 (expected)
- Failing: 19 (expected - all fail at compilation)
- Status: RED phase verified

**Expected Failure Messages:**
- `undefined: PhaseObserve` / `undefined: OODAPhase` - types not yet defined
- `undefined: OODAToolCall` / `undefined: OODAActionType` - action types not yet defined
- `undefined: OODAState` / `undefined: OODADecision` - structs not yet defined
- `proc.IsOODA undefined` - Process methods not yet added
- `proc.SetOODAPhase undefined` - Process methods not yet added
- `proc.GetOODAState undefined` - Process methods not yet added
- `SpawnOpts.ReasoningMode undefined` - SpawnOpts field not yet added
- `types.LogOODA undefined` - LogCategory constant not yet added

---

## Notes

- Go TDD 特性：测试文件引用未定义类型导致整个 kernel 包编译失败，这是 RED 阶段的正常行为。DEV 实现类型后包恢复编译。
- OODA 测试复用了 `kernel_test.go` 中已有的 mock 基础设施（mockLLMFile, newTestKernel, makeLLMResponse）
- 新增 `mockDynamicLLMFile` 支持多轮对话场景中根据 Write 内容动态选择响应
- 所有测试启用 `-race` 检测，确保并发安全
- 性能测试（NFR41）使用即时响应的 mock LLM，仅测量纯框架代码开销
- 兼容性测试（TestSpawn_DefaultReasoningMode）确保现有线性推理不受影响

---

## Key Risks and Assumptions

1. **Mock 精确度**: mockDynamicLLMFile 的 Write 接口签名需要与实际 VFSFile.Write 签名匹配（当前使用 `interface{ Deadline() (time.Time, bool) }`）。如果 VFSFile.Write 签名变化，mock 需同步更新。
2. **OODA 循环中的工具调用**: TestOODAReasonStep_MultipleCycles 假设 oodaAct 的 OODAToolCall 分支能正确复用 reasonStep 的 VFS 工具调用模式。如果实际实现使用不同模式，测试 mock 可能需要调整。
3. **OODASpawn 子进程**: TestOODAReasonStep_SpawnAction 需要 oodaAct 内部调用 `k.Spawn()`。如果子进程也需要 LLM mock，当前单一 mock 可能不足。

---

**Generated by BMad TEA Agent** - 2026-03-10
