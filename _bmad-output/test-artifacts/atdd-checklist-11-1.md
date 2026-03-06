---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-implementation-checklist
  - step-06-deliverables
lastStep: step-06-deliverables
lastSaved: '2026-03-03'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/11-1-pipe-syntax.md
  - _bmad/tea/testarch/knowledge/test-quality.md
  - _bmad/tea/testarch/knowledge/test-levels-framework.md
  - _bmad/tea/testarch/knowledge/test-priorities-matrix.md
  - _bmad/tea/testarch/knowledge/data-factories.md
---

# ATDD Checklist - Epic 11, Story 1: 管道语法 (Pipe Syntax)

**Date:** 2026-03-03
**Author:** Decker
**Primary Test Level:** Unit (Go `testing` package)
**Stack:** Backend (Go)

---

## Story Summary

用户可以在 AgentShell 中通过管道语法组合智能体执行链，前一个智能体的输出自动成为后一个的输入。

**As a** 用户
**I want** 在 AgentShell 中通过管道语法组合智能体执行链
**So that** 前一个智能体的输出自动成为后一个的输入

---

## Acceptance Criteria

1. **AC1: 双智能体管道** — 执行 `spawn "分析代码" | spawn "写文档"`，系统解析管道语法，Spawn 第一个智能体并将其输出通过 `[PIPE_INPUT]` 注入第二个智能体上下文
2. **AC2: 多级管道链** — 执行 `spawn "A" | spawn "B" | spawn "C"`，按顺序链式传递 A→B→C
3. **AC3: 管道错误中断** — 某个智能体退出非零码时，下游智能体不启动，管道中断并报告错误位置

---

## Failing Tests Created (RED Phase)

### Unit Tests — Parser (6 个测试 + 5 个错误测试)

**File:** `shell/parser_test.go` (~170 行)

- ✅ **Test:** `TestParsePipeline_SingleSpawn`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 单 spawn 命令解析为 1-Command Pipeline (11.1-UNIT-001, P0)

- ✅ **Test:** `TestParsePipeline_TwoStages`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 双管道 `spawn "A" | spawn "B"` 解析为 2-Command Pipeline (11.1-UNIT-002, P0, AC1)

- ✅ **Test:** `TestParsePipeline_ThreeStages`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 三管道解析为 3-Command Pipeline (11.1-UNIT-003, P0, AC2)

- ✅ **Test:** `TestParsePipeline_WithAgentAndModel`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** `--agent=X --model=Y` 参数正确解析 (11.1-UNIT-004, P1)

- ✅ **Test:** `TestParsePipeline_SingleQuotedIntent`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 单引号 intent 解析 (11.1-UNIT-004, P1)

- ✅ **Test:** `TestParsePipeline_CaseInsensitiveSpawn`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** `Spawn`/`SPAWN` 大小写不敏感 (11.1-UNIT-004b, P1)

- ✅ **Test:** `TestParsePipeline_EmptyInput`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 空输入返回错误 (11.1-UNIT-005, P0)

- ✅ **Test:** `TestParsePipeline_NonSpawnCommand`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 非 spawn 命令返回错误 (11.1-UNIT-005, P0)

- ✅ **Test:** `TestParsePipeline_UnclosedQuote`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 未闭合引号返回错误 (11.1-UNIT-005, P0)

- ✅ **Test:** `TestParsePipeline_EmptySegment`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 空段 `| |` 返回错误 (11.1-UNIT-005, P0)

- ✅ **Test:** `TestParsePipeline_MissingIntent`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** `spawn` 无 intent 返回错误 (11.1-UNIT-005, P0)

- ✅ **Test:** `TestParsePipeline_PipeInsideQuotes`
  - **Status:** RED — `undefined: ParsePipeline`
  - **Verifies:** 引号内 `|` 不分割 (11.1-REG-002, P2)

### Unit Tests — Executor (9 个测试)

**File:** `shell/pipe_test.go` (~280 行)

- ✅ **Test:** `TestPipelineExecutor_TwoStages_PipeInput`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** 双阶段执行 + `[PIPE_INPUT]` 注入 + Token 累加 (11.1-UNIT-006, P0, AC1)

- ✅ **Test:** `TestPipelineExecutor_ThreeStages_ChainTransfer`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** 三阶段 A→B→C 链式传递 (11.1-UNIT-007, P0, AC2)

- ✅ **Test:** `TestPipelineExecutor_FirstStageFails`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** 首阶段 ExitCode!=0 → 第二阶段不执行 (11.1-UNIT-008, P0, AC3)

- ✅ **Test:** `TestPipelineExecutor_MiddleStageFails`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** 中间阶段失败 → 后续不执行，前置结果保留 (11.1-UNIT-009, P0, AC3)

- ✅ **Test:** `TestPipelineExecutor_ContextCancelled`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** context 取消 → 返回错误 (11.1-UNIT-010, P1)

- ✅ **Test:** `TestPipelineExecutor_RecordsElapsed`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** PipelineResult.Elapsed > 0 (11.1-UNIT-011, P1)

- ✅ **Test:** `TestPipelineExecutor_StageElapsed`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** StageResult.Elapsed 独立计时 (11.1-UNIT-012, P1)

- ✅ **Test:** `TestPipelineExecutor_PassesAgentAndModel`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** Agent/Model 参数传递到 spawner (11.1-UNIT-013, P1)

- ✅ **Test:** `TestPipelineExecutor_SpawnerError`
  - **Status:** RED — `undefined: NewPipelineExecutor`
  - **Verifies:** Spawner 返回 error → 管道中断 (11.1-UNIT-014, P2)

### Integration Tests — IPC Protocol (6 个测试)

**File:** `ipc/protocol_test.go` (新增 ~150 行)

- ✅ **Test:** `TestMethodSpawnPipeline_Exists`
  - **Status:** RED — `undefined: MethodSpawnPipeline`
  - **Verifies:** 新 Method 常量存在且唯一 (11.1-INT-001a, P1)

- ✅ **Test:** `TestSpawnPipelineRequest_MarshalRoundTrip`
  - **Status:** RED — `undefined: SpawnPipelineRequest`
  - **Verifies:** 请求序列化 roundtrip (11.1-INT-001b, P1)

- ✅ **Test:** `TestSpawnPipelineResponse_MarshalRoundTrip`
  - **Status:** RED — `undefined: SpawnPipelineResponse`
  - **Verifies:** 响应序列化 roundtrip (11.1-INT-001c, P1)

- ✅ **Test:** `TestPipelineStageWire_ZeroExitCode`
  - **Status:** RED — `undefined: PipelineStageWire`
  - **Verifies:** ExitCode=0 不被 omitempty 吞掉 (11.1-INT-001d, P1)

- ✅ **Test:** `TestSpawnPipelineCommand_OmitEmpty`
  - **Status:** RED — `undefined: SpawnPipelineCommand`
  - **Verifies:** Agent/Model 空值 omitempty (11.1-INT-001e, P1)

- ✅ **Test:** `TestSpawnPipelineRequest_IPCEnvelope`
  - **Status:** RED — `undefined: MethodSpawnPipeline`
  - **Verifies:** IPC Request 包装正确 (11.1-INT-001f, P2)

### Regression Tests — CLI (2 个测试)

**File:** `cmd/crux/main_test.go` (新增 ~40 行)

- ✅ **Test:** `TestIsPipelineSyntax_BasicPipe`
  - **Status:** RED — `undefined: isPipelineSyntax`
  - **Verifies:** 正确识别 spawn 管道语法 (11.1-REG-001, P2)

- ✅ **Test:** `TestIsPipelineSyntax_NonPipe`
  - **Status:** RED — `undefined: isPipelineSyntax`
  - **Verifies:** 非管道 intent（含 `|`）不误判 (11.1-REG-002, P2)

---

## Implementation Checklist

### Task 1: Shell 解析器 — 管道语法词法分析与 AST

**Files to create:** `shell/parser.go`

**Tasks to make parser tests pass:**

- [ ] 定义 `Command` 类型（Type, Intent, Agent, Model string）
- [ ] 定义 `Pipeline` 类型（Commands []Command）
- [ ] 实现 `ParsePipeline(input string) (*Pipeline, error)`
- [ ] 实现按 `|` 分割（忽略引号内的 `|`）
- [ ] 实现 `parseSpawnCommand` — 提取 spawn 关键字、引号 intent、命名参数
- [ ] 支持双引号和单引号 intent
- [ ] 支持 `--agent=X` 和 `--model=Y` 可选参数
- [ ] spawn 关键字大小写不敏感
- [ ] 错误处理：空输入、非 spawn 命令、未闭合引号、空段、缺少 intent
- [ ] Run tests: `go test ./shell/ -run TestParsePipeline -v`
- [ ] ✅ All parser tests pass (green phase)

**Estimated Effort:** 2-3 hours

---

### Task 2: 管道执行引擎

**Files to create:** `shell/pipe.go`

**Tasks to make executor tests pass:**

- [ ] 定义 `KernelSpawner` 接口（`SpawnAndWait(ctx, intent, agent, model) (result, exitCode, tokens, error)`）
- [ ] 定义 `PipelineExecutor` 结构体
- [ ] 定义 `PipelineResult`（Stages []StageResult, TotalTokens int, Elapsed time.Duration）
- [ ] 定义 `StageResult`（PID types.PID, Intent, Result string, ExitCode int, TokensUsed int, Elapsed time.Duration）
- [ ] 实现 `NewPipelineExecutor(spawner KernelSpawner) *PipelineExecutor`
- [ ] 实现 `Execute(ctx, pipeline) (*PipelineResult, error)` — 顺序执行各阶段
- [ ] 实现 `[PIPE_INPUT]` 注入格式 — 前一阶段 Result 作为标记注入下一阶段 intent 前面
- [ ] 实现错误语义 — ExitCode != 0 时不启动后续阶段
- [ ] 实现 context 取消检查 — 每阶段前检查 `ctx.Err()`
- [ ] 实现 Spawner error 处理 — 返回 error 时中断
- [ ] 记录 PipelineResult.Elapsed 总耗时
- [ ] 记录 StageResult.Elapsed 每阶段独立计时
- [ ] Run tests: `go test ./shell/ -run TestPipelineExecutor -v`
- [ ] ✅ All executor tests pass (green phase)

**Estimated Effort:** 3-4 hours

---

### Task 3: IPC 协议扩展

**Files to modify:** `ipc/protocol.go`

**Tasks to make IPC tests pass:**

- [ ] 新增 `MethodSpawnPipeline Method = "spawn_pipeline"` 常量
- [ ] 定义 `SpawnPipelineCommand` 类型（Intent, Agent, Model string 带 omitempty）
- [ ] 定义 `SpawnPipelineRequest` 类型（Commands []SpawnPipelineCommand）
- [ ] 定义 `PipelineStageWire` 类型（PID, Intent, Result, ExitCode, TokensUsed, ElapsedMs — ExitCode 不用 omitempty）
- [ ] 定义 `SpawnPipelineResponse` 类型（Stages []PipelineStageWire）
- [ ] Run tests: `go test ./ipc/ -run TestSpawnPipeline -v && go test ./ipc/ -run TestMethodSpawnPipeline -v && go test ./ipc/ -run TestPipelineStageWire -v`
- [ ] ✅ All IPC protocol tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Task 4: CLI 集成 — 管道检测

**Files to modify:** `cmd/crux/main.go`

**Tasks to make CLI tests pass:**

- [ ] 实现 `isPipelineSyntax(intent string) bool` — 检测 intent 是否是 `spawn "X" | spawn "Y"` 管道语法
- [ ] 内部调用 `containsSpawnKeyword` — 检查 `|` 两侧是否有 `spawn` 关键字
- [ ] 避免误判：普通 intent 中含 `|` 但无 spawn 关键字时返回 false
- [ ] Run tests: `go test ./cmd/crux/ -run TestIsPipelineSyntax -v`
- [ ] ✅ All CLI detection tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Task 5: IPC Server + Client 管道支持

**Files to modify:** `ipc/server.go`, `ipc/client.go`

**Tasks (DEV 阶段 2):**

- [ ] `ipc/server.go`: 实现 `handleSpawnPipeline` — 接收请求，调用 PipelineExecutor.Execute()
- [ ] `ipc/server.go`: 实现 `ipcKernelSpawner` 适配器 — 桥接 kernel.Spawn
- [ ] `ipc/client.go`: 实现 `SpawnPipelineAndWatch(req, onEvent)` 客户端方法
- [ ] `cmd/crux/main.go`: `runRoot` 中 `isPipelineSyntax` 为 true 时走管道执行路径

**Estimated Effort:** 4-5 hours

---

## Running Tests

```bash
# Run all failing tests for this story
go test ./shell/... ./ipc/... ./cmd/crux/... -v 2>&1 | head -50

# Run parser tests only
go test ./shell/ -run TestParsePipeline -v

# Run executor tests only
go test ./shell/ -run TestPipelineExecutor -v

# Run IPC protocol tests only
go test ./ipc/ -run "TestSpawnPipeline|TestMethodSpawnPipeline|TestPipelineStageWire" -v

# Run CLI detection tests only
go test ./cmd/crux/ -run TestIsPipelineSyntax -v

# Run all tests with race detector
go test -race ./shell/... ./ipc/... ./cmd/crux/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ 所有测试已编写并失败（编译错误 = RED）
- ✅ Mock spawner 已创建用于执行引擎测试
- ✅ IPC 协议类型测试已覆盖
- ✅ CLI 管道检测测试已覆盖
- ✅ Implementation checklist 已创建

**Verification:**

- `shell/parser_test.go`: `undefined: ParsePipeline` — 12 个测试编译失败
- `shell/pipe_test.go`: `undefined: NewPipelineExecutor` — 9 个测试编译失败
- `ipc/protocol_test.go`: `undefined: MethodSpawnPipeline` — 6 个测试编译失败
- `cmd/crux/main_test.go`: `undefined: isPipelineSyntax` — 2 个测试编译失败

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Pick one task** from implementation checklist (建议顺序: Task 1 → Task 2 → Task 3 → Task 4 → Task 5)
2. **Read the tests** to understand expected behavior
3. **Implement minimal code** to make tests pass
4. **Run the tests** to verify green
5. **Check off the task** in implementation checklist
6. **Move to next task** and repeat

**Key Principles:**

- Task 1 (parser) 和 Task 2 (executor) 可以独立进行（零外部依赖）
- Task 3 (IPC protocol) 可以与 Task 1/2 并行
- Task 4 (CLI detection) 依赖 Task 1
- Task 5 (IPC server/client) 依赖 Task 1 + 2 + 3

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 验证所有测试通过
2. 检查代码质量和一致性
3. 运行 `go vet ./shell/... ./ipc/... ./cmd/crux/...`
4. 运行 `go test -race ./shell/... ./ipc/... ./cmd/crux/...`
5. 确保测试仍通过

---

## Next Steps

1. **Share this checklist and failing tests** with dev workflow
2. **Run failing tests** to confirm RED phase: `go test ./shell/... ./ipc/... ./cmd/crux/... 2>&1`
3. **Begin implementation** — 建议从 Task 1 (parser) 开始
4. **Work one task at a time** (red → green for each)
5. **When all tests pass**, refactor code for quality
6. **When refactoring complete**, 更新 sprint-status.yaml story 状态为 'done'

---

## Knowledge Base References Applied

- **test-quality.md** — 确定性测试设计原则（无硬等待、无条件控制、隔离性）
- **test-levels-framework.md** — Go 后端：Unit 测试优先，Integration 测试服务交互
- **test-priorities-matrix.md** — P0-P3 优先级分配（AC 直接覆盖 = P0，辅助功能 = P1/P2）
- **data-factories.md** — Mock spawner 模式（factory with overrides 思想应用于 Go mock）

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./shell/... ./ipc/... ./cmd/crux/... 2>&1`

**Results:**

```
# github.com/usecrux/crux/shell [github.com/usecrux/crux/shell.test]
shell/parser_test.go:17:19: undefined: ParsePipeline
... (10+ more errors)
FAIL    github.com/usecrux/crux/shell [build failed]

# github.com/usecrux/crux/ipc [github.com/usecrux/crux/ipc.test]
ipc/protocol_test.go:501:5: undefined: MethodSpawnPipeline
... (10+ more errors)
FAIL    github.com/usecrux/crux/ipc [build failed]

# github.com/usecrux/crux/cmd/crux [github.com/usecrux/crux/cmd/crux.test]
cmd/crux/main_test.go:992:13: undefined: isPipelineSyntax
cmd/crux/main_test.go:1013:13: undefined: isPipelineSyntax
FAIL    github.com/usecrux/crux/cmd/crux [build failed]
```

**Summary:**

- Total tests: 29 (12 parser + 9 executor + 6 IPC + 2 CLI)
- Passing: 0 (expected)
- Failing: 29 — compile errors (expected)
- Status: ✅ RED phase verified

---

## Test ID Reference Table

| Test ID | Priority | Level | AC | File | Test Function |
|---------|----------|-------|-----|------|---------------|
| 11.1-UNIT-001 | P0 | Unit | AC1,AC2 | shell/parser_test.go | TestParsePipeline_SingleSpawn |
| 11.1-UNIT-002 | P0 | Unit | AC1 | shell/parser_test.go | TestParsePipeline_TwoStages |
| 11.1-UNIT-003 | P0 | Unit | AC2 | shell/parser_test.go | TestParsePipeline_ThreeStages |
| 11.1-UNIT-004 | P1 | Unit | AC1 | shell/parser_test.go | TestParsePipeline_WithAgentAndModel |
| 11.1-UNIT-004 | P1 | Unit | AC1 | shell/parser_test.go | TestParsePipeline_SingleQuotedIntent |
| 11.1-UNIT-004b | P1 | Unit | AC1 | shell/parser_test.go | TestParsePipeline_CaseInsensitiveSpawn |
| 11.1-UNIT-005 | P0 | Unit | AC3 | shell/parser_test.go | TestParsePipeline_EmptyInput |
| 11.1-UNIT-005 | P0 | Unit | AC3 | shell/parser_test.go | TestParsePipeline_NonSpawnCommand |
| 11.1-UNIT-005 | P0 | Unit | AC3 | shell/parser_test.go | TestParsePipeline_UnclosedQuote |
| 11.1-UNIT-005 | P0 | Unit | AC3 | shell/parser_test.go | TestParsePipeline_EmptySegment |
| 11.1-UNIT-005 | P0 | Unit | AC3 | shell/parser_test.go | TestParsePipeline_MissingIntent |
| 11.1-UNIT-006 | P0 | Unit | AC1 | shell/pipe_test.go | TestPipelineExecutor_TwoStages_PipeInput |
| 11.1-UNIT-007 | P0 | Unit | AC2 | shell/pipe_test.go | TestPipelineExecutor_ThreeStages_ChainTransfer |
| 11.1-UNIT-008 | P0 | Unit | AC3 | shell/pipe_test.go | TestPipelineExecutor_FirstStageFails |
| 11.1-UNIT-009 | P0 | Unit | AC3 | shell/pipe_test.go | TestPipelineExecutor_MiddleStageFails |
| 11.1-UNIT-010 | P1 | Unit | AC3 | shell/pipe_test.go | TestPipelineExecutor_ContextCancelled |
| 11.1-UNIT-011 | P1 | Unit | - | shell/pipe_test.go | TestPipelineExecutor_RecordsElapsed |
| 11.1-UNIT-012 | P1 | Unit | - | shell/pipe_test.go | TestPipelineExecutor_StageElapsed |
| 11.1-UNIT-013 | P1 | Unit | AC1 | shell/pipe_test.go | TestPipelineExecutor_PassesAgentAndModel |
| 11.1-UNIT-014 | P2 | Unit | AC3 | shell/pipe_test.go | TestPipelineExecutor_SpawnerError |
| 11.1-INT-001a | P1 | Integration | - | ipc/protocol_test.go | TestMethodSpawnPipeline_Exists |
| 11.1-INT-001b | P1 | Integration | AC1 | ipc/protocol_test.go | TestSpawnPipelineRequest_MarshalRoundTrip |
| 11.1-INT-001c | P1 | Integration | AC1 | ipc/protocol_test.go | TestSpawnPipelineResponse_MarshalRoundTrip |
| 11.1-INT-001d | P1 | Integration | - | ipc/protocol_test.go | TestPipelineStageWire_ZeroExitCode |
| 11.1-INT-001e | P1 | Integration | - | ipc/protocol_test.go | TestSpawnPipelineCommand_OmitEmpty |
| 11.1-INT-001f | P2 | Integration | - | ipc/protocol_test.go | TestSpawnPipelineRequest_IPCEnvelope |
| 11.1-REG-001 | P2 | Regression | - | cmd/crux/main_test.go | TestIsPipelineSyntax_BasicPipe |
| 11.1-REG-002 | P2 | Regression | - | cmd/crux/main_test.go | TestIsPipelineSyntax_NonPipe |
| - | P2 | Unit | - | shell/parser_test.go | TestParsePipeline_PipeInsideQuotes |

---

## Notes

- Go 后端项目的 RED phase = 编译失败（引用不存在的类型/函数），而非 `test.skip()`
- `shell/` 包是新建包（AgentShell DSL 层），与 `drivers/shell/`（宿主 Shell 驱动）完全不同
- `shell/` 包零外部依赖（仅标准库），通过 `KernelSpawner` 接口与 kernel 解耦
- Mock spawner (`mockSpawner`) 用于测试执行引擎，避免真实 LLM 调用
- 管道执行采用顺序模型（非 Unix 并发管道），因为 LLM 需要完整输入

---

**Generated by BMad TEA Agent** — 2026-03-03
