---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-checklist
  - step-06-verify-red
lastStep: step-06-verify-red
lastSaved: '2026-03-09'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/18-4-spawn-return-capture-and-parallel-execution.md
  - _bmad-output/planning-artifacts/epics/epic-18-agentshell完整脚本语言-agentshell-complete-scripting.md
  - shell/script.go
  - shell/env.go
  - shell/pipe.go
  - shell/pipe_test.go
  - shell/data_test.go
---

# ATDD Checklist - Epic 18, Story 4: Spawn 返回值捕获与并行执行

**Date:** 2026-03-09
**Author:** Decker
**Primary Test Level:** Unit (Go `testing` package)

---

## Story Summary

AgentShell 脚本支持 `parallel ... end` 块，块内 spawn/pipeline 并行执行，块结束等待全部完成。每个 spawn 可赋值捕获结果，失败不影响其他 spawn，on-error handler 在同一 goroutine 中执行。

**As a** 应用开发者
**I want** 捕获 spawn 的返回值到变量，并使用 parallel 块并行执行多个 spawn
**So that** 我可以组合智能体结果并加速并行任务

---

## Acceptance Criteria

1. **AC1**: `result = spawn "分析代码" --agent=analyst` → 智能体完成后输出绑定到 `result`
2. **AC2**: parallel 块内三个 spawn → 三个 spawn 并行启动，块结束时等待全部完成
3. **AC3**: parallel 块一个 spawn 失败 → 不影响其他 spawn，块结束汇总所有结果
4. **AC4**: 循环/函数调用运行时开销 <= 1ms/次 (NFR39)
5. **AC5**: parallel 块中多个赋值 spawn → 每个变量正确绑定对应 spawn 结果，`$r.exitcode`/`$r.result` 可用
6. **AC6**: parallel 块中 spawn 带 on-error → on-error 在同一并行任务中执行，结果覆盖原值
7. **AC7**: parallel 块中包含 pipeline → pipeline 与其他 spawn 并行执行
8. **AC8**: parallel 块中 intent 引用未定义变量 → 报告错误并指出行号和变量名
9. **AC9**: parallel 块内含非 spawn/pipeline 语句 → 解析错误
10. **AC10**: 空 parallel 块 → no-op，脚本继续执行

---

## Failing Tests Created (RED Phase)

### Unit Tests (30 tests)

**File:** `shell/parallel_test.go` (约 550 行)

#### Parsing Tests (11 tests)

- ✅ **Test:** `TestParseScript_Parallel_BasicBlock`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC2 基本 parallel 块解析（3 个 spawn）

- ✅ **Test:** `TestParseScript_Parallel_WithAssignment`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC5 带赋值的 spawn 在 parallel 块中

- ✅ **Test:** `TestParseScript_Parallel_WithOnError`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC6 spawn 带 on-error 在 parallel 块中

- ✅ **Test:** `TestParseScript_Parallel_WithPipeline`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC7 pipeline 在 parallel 块中

- ✅ **Test:** `TestParseScript_Parallel_Empty`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC10 空 parallel 块（合法 no-op）

- ✅ **Test:** `TestParseScript_Error_ParallelUnclosed`
  - **Status:** RED - parallel 关键字未识别
  - **Verifies:** AC9 无 end → 错误

- ✅ **Test:** `TestParseScript_Error_ParallelInvalidContent_Export`
  - **Status:** RED - parallel 关键字未识别
  - **Verifies:** AC9 export 在 parallel 内 → 错误

- ✅ **Test:** `TestParseScript_Error_ParallelInvalidContent_If`
  - **Status:** RED - parallel 关键字未识别
  - **Verifies:** AC9 if 在 parallel 内 → 错误

- ✅ **Test:** `TestParseScript_Error_ParallelInvalidContent_For`
  - **Status:** RED - parallel 关键字未识别
  - **Verifies:** AC9 for 在 parallel 内 → 错误

- ✅ **Test:** `TestParseScript_Error_ParallelInvalidContent_FnCall`
  - **Status:** RED - parallel 关键字未识别
  - **Verifies:** AC9 fn call 在 parallel 内 → 错误

- ✅ **Test:** `TestParseScript_Error_ParallelNested`
  - **Status:** RED - parallel 关键字未识别
  - **Verifies:** 嵌套 parallel → 错误

#### Execution Tests (14 tests)

- ✅ **Test:** `TestScriptExecutor_Parallel_AllSucceed`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC2 三个 spawn 全成功，tokens 汇总

- ✅ **Test:** `TestScriptExecutor_Parallel_Assignment`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC1, AC5 赋值 spawn 在 parallel 中，env 和 captures 正确

- ✅ **Test:** `TestScriptExecutor_Parallel_OneFails_OthersContinue`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC3 一个失败不影响其他

- ✅ **Test:** `TestScriptExecutor_Parallel_OnError`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC6 失败 spawn 的 on-error handler 执行

- ✅ **Test:** `TestScriptExecutor_Parallel_AllFail`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** 全部失败，结果全部捕获

- ✅ **Test:** `TestScriptExecutor_Parallel_LastResult_DeclarationOrder`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** LastResult 是声明顺序的最后一个

- ✅ **Test:** `TestScriptExecutor_Parallel_TokenAggregation`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** 所有 tokens 汇总到 TotalTokens

- ✅ **Test:** `TestScriptExecutor_Parallel_CapturedResult_Condition`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC5 parallel 后 `if $r.exitcode == 0` 条件

- ✅ **Test:** `TestScriptExecutor_Parallel_IntentExpansion`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** intent 中 `${var}` 正确展开

- ✅ **Test:** `TestScriptExecutor_Parallel_UndefinedVar_Error`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC8 ExpandStrict 未定义变量 → 含行号错误

- ✅ **Test:** `TestScriptExecutor_Parallel_Empty`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC10 空 parallel 块 no-op

- ✅ **Test:** `TestScriptExecutor_Parallel_ContextCancel`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** context 取消传播到所有并行任务

- ✅ **Test:** `TestScriptExecutor_Parallel_Pipeline`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** AC7 pipeline 在 parallel 块中并行执行

- ✅ **Test:** `TestScriptExecutor_Parallel_StageCount`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** countStages 正确统计 parallel 内 spawn

#### Combination Tests (4 tests)

- ✅ **Test:** `TestScriptExecutor_Parallel_InForLoop`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** for 循环内使用 parallel 块

- ✅ **Test:** `TestScriptExecutor_Parallel_InFunction`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** 函数内使用 parallel 块

- ✅ **Test:** `TestScriptExecutor_Parallel_AfterDataStructures`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** parallel 前数组/映射赋值 + intent 中 `${arr[0]}` 展开

- ✅ **Test:** `TestScriptExecutor_Parallel_ResultInWhileCondition`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** parallel 结果用于 if 条件

#### Race & Performance Tests (1 + 1 tests)

- ✅ **Test:** `TestScriptExecutor_Parallel_NoRace`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** 5 个并行 spawn 无数据竞争（配合 `go test -race`）

- ✅ **Test:** `TestParseScript_Parallel_Performance_NFR38`
  - **Status:** RED - StmtParallel undefined
  - **Verifies:** NFR38 parallel 块解析性能 <= 50ms

---

## Data Factories Created

不适用 — Go 测试使用 `concurrentMockSpawner` 和 `mockSpawner` 测试桩，不需要额外的数据工厂。

---

## Fixtures Created

不适用 — Go 测试使用标准 `testing.T` 和内联 mock，不需要 Playwright 风格的 fixtures。

---

## Mock Requirements

### concurrentMockSpawner (新增)

**File:** `shell/parallel_test.go`

线程安全的 mock spawner，用于 parallel 执行测试。使用 `sync.Mutex` 保护 `calls` 切片，`results` map 预设后只读。支持按 intent 匹配结果和按调用顺序匹配结果两种模式。

**Key Methods:**
- `SpawnAndWait(ctx, intent, agent, model)` — 线程安全记录调用 + 返回预设结果
- `getCalls()` — 返回调用记录的快照副本

### mockSpawner (已存在)

**File:** `shell/pipe_test.go`

复用现有的 `mockSpawner`，但**不用于** parallel 执行测试（非线程安全）。仅在解析测试中间接使用（通过同包共享）。

---

## Required New Types

### Statement Kinds

- `StmtParallel StatementKind = "parallel"` — parallel 块语句

### Data Types

- `ParallelBlock { Body []Statement }` — parallel 块结构体

### Statement Extensions

- `Statement.Parallel *ParallelBlock` — 新字段

### parseBlock Extensions

- `parseParallelBlock(lines []string, parallelLineIdx int) (*ParallelBlock, int, error)` — 新解析函数
- 在 `parseBlock` 中 `for` 检测之前新增 `parallel` 关键字检测

### executeBlock Extensions

- `case StmtParallel:` — 三阶段执行（顺序展开 → 并行执行 → 顺序收集）
- `import "sync"` — 首次在 shell 包中使用

### validateFnCalls Extensions

- `case StmtParallel:` — 递归校验 parallel 块内的函数调用

### countStagesInBlock Extensions

- `case StmtParallel:` — 递归计数 parallel 块内的 stage

---

## Implementation Checklist

### Test: TestParseScript_Parallel_BasicBlock + TestParseScript_Parallel_WithAssignment + TestParseScript_Parallel_WithOnError + TestParseScript_Parallel_WithPipeline + TestParseScript_Parallel_Empty

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 定义 `StmtParallel StatementKind = "parallel"`
- [ ] 定义 `ParallelBlock { Body []Statement }` 结构体
- [ ] 在 `Statement` 添加 `Parallel *ParallelBlock` 字段
- [ ] 在 `parseBlock` 中 `for` 检测之前新增 `parallel` 关键字检测
- [ ] 实现 `parseParallelBlock(lines, parallelLineIdx)` 函数：
  - 调用 `parseBlock(lines, startIdx+1, true)` 解析 body
  - 检查 nextIdx 处为 `end`，否则报错 `unclosed parallel block`
  - 遍历 body 校验 Kind 为 `StmtSpawn` 或 `StmtPipeline`
  - 空 body 合法（no-op）
- [ ] Run test: `go test ./shell/ -run "TestParseScript_Parallel_" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestParseScript_Error_ParallelUnclosed + TestParseScript_Error_ParallelInvalidContent_* + TestParseScript_Error_ParallelNested

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] `parseParallelBlock` 中检测未闭合 parallel（nextIdx >= len(lines)）
- [ ] body 语句 Kind 校验：非 StmtSpawn/StmtPipeline → 报错含行号
- [ ] 嵌套 parallel 检测（body 中 Kind == StmtParallel → 报错）
- [ ] 但注意：嵌套 parallel 实际上会在 parseBlock 中被识别为 parallel 关键字然后递归调用 parseParallelBlock，最终 body 校验会拒绝它（因为 StmtParallel 不在允许列表中）
- [ ] Run test: `go test ./shell/ -run "TestParseScript_Error_Parallel" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestScriptExecutor_Parallel_AllSucceed + TestScriptExecutor_Parallel_TokenAggregation + TestScriptExecutor_Parallel_LastResult_DeclarationOrder

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 添加 `"sync"` 到 `script.go` 的 import
- [ ] 在 `executeBlock` 中新增 `case StmtParallel:` 分支
- [ ] 实现阶段 A：顺序展开所有 intent（主 goroutine）
- [ ] 实现阶段 B：goroutine per task + sync.WaitGroup
- [ ] 实现阶段 C：顺序收集结果（tokens, LastResult, LastExitCode）
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_(AllSucceed|TokenAgg|LastResult)" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 2 hours

---

### Test: TestScriptExecutor_Parallel_Assignment + TestScriptExecutor_Parallel_CapturedResult_Condition

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 阶段 C 中处理 `stmt.Assign != ""` → 写入 captures 和 env
- [ ] 确保 parallel 块后 `$r.exitcode` 和 `$r.result` 在 if 条件中可用
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_(Assignment|CapturedResult)" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestScriptExecutor_Parallel_OneFails_OthersContinue + TestScriptExecutor_Parallel_AllFail

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 阶段 B 中每个 goroutine 独立处理错误，写入 `results[idx]`
- [ ] 阶段 C 中收集错误但不立即返回（容错模式）
- [ ] parallel 块不因非零 exitCode 终止脚本
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_(OneFails|AllFail)" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestScriptExecutor_Parallel_OnError

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 阶段 A 中预展开 on-error intent
- [ ] 阶段 B goroutine 内：exitCode != 0 且有 OnError → 执行 on-error spawn
- [ ] on-error 结果覆盖原始 spawn 结果
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_OnError" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestScriptExecutor_Parallel_IntentExpansion + TestScriptExecutor_Parallel_UndefinedVar_Error

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 阶段 A 使用 `ExpandStrict` 展开 intent（非 Expand）
- [ ] 展开错误立即返回，包含行号信息
- [ ] 正常变量正确展开后传递给 goroutine
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_(IntentExpansion|UndefinedVar)" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestScriptExecutor_Parallel_Empty + TestScriptExecutor_Parallel_ContextCancel

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 空 body → 直接 break（no-op，不创建 goroutine）
- [ ] 阶段 B 开始前检查 `ctx.Err()`
- [ ] 各 goroutine 将 ctx 传递给 SpawnAndWait
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_(Empty$|ContextCancel)" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestScriptExecutor_Parallel_Pipeline

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 阶段 A 中对 StmtPipeline 调用 `expandPipelineIntentsStrict`
- [ ] 阶段 B 中 StmtPipeline → `NewPipelineExecutor(e.spawner).Execute(ctx, expandedPipeline)`
- [ ] pipeline 结果提取 last stage result/exitCode/tokens
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_Pipeline" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestScriptExecutor_Parallel_StageCount

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `countStagesInBlock` 中新增 `case StmtParallel:` → 递归 `countStagesInBlock(stmt.Parallel.Body)`
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_StageCount" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestScriptExecutor_Parallel_InForLoop + TestScriptExecutor_Parallel_InFunction + TestScriptExecutor_Parallel_AfterDataStructures + TestScriptExecutor_Parallel_ResultInWhileCondition

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 确保 parallel 块在 for 循环 body 中正常工作（parseBlock insideBlock=true）
- [ ] 确保 parallel 块在函数 body 中正常工作
- [ ] 确保数组/映射变量在 parallel intent 预展开中正确解析
- [ ] 确保 parallel 结果（captures）在后续 if 条件中可用
- [ ] 在 `validateFnCalls` 中新增 `case StmtParallel:` 递归校验
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Parallel_(InFor|InFunction|AfterData|ResultIn)" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestScriptExecutor_Parallel_NoRace

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 确保阶段 B 中 goroutine 仅写入 `results[idx]`（索引访问，无竞争）
- [ ] 确保不在 goroutine 中直接操作 env/captures
- [ ] Run test: `go test -race ./shell/ -run "TestScriptExecutor_Parallel_NoRace" -v`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestParseScript_Parallel_Performance_NFR38

**File:** `shell/parallel_test.go`

**Tasks to make these tests pass:**

- [ ] 确保 parallel 解析不引入性能退化
- [ ] 10 个 parallel 块 × 5 个 spawn = 50 个 spawn 语句解析 <= 50ms
- [ ] Run test: `go test ./shell/ -run "TestParseScript_Parallel_Performance" -v`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

## Running Tests

```bash
# Run all failing tests for this story
go test ./shell/ -run "Parallel" -v

# Run only parsing tests
go test ./shell/ -run "TestParseScript_Parallel" -v

# Run only error parsing tests
go test ./shell/ -run "TestParseScript_Error_Parallel" -v

# Run only execution tests
go test ./shell/ -run "TestScriptExecutor_Parallel" -v

# Run only combination tests
go test ./shell/ -run "TestScriptExecutor_Parallel_(InFor|InFunction|AfterData|ResultIn)" -v

# Run race detection
go test -race ./shell/ -run "Parallel" -v

# Debug specific test
go test ./shell/ -run TestScriptExecutor_Parallel_AllSucceed -v -count=1

# Run tests with coverage
go test ./shell/ -coverprofile=coverage.out -run "Parallel" && go tool cover -html=coverage.out

# Run all shell tests (including existing stories)
go test ./shell/ -v
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 30 tests written and failing (compile errors)
- ✅ concurrentMockSpawner created for thread-safe parallel testing
- ✅ New type requirements documented
- ✅ Implementation checklist created

**Verification:**

- All tests fail to compile due to undefined types
- Failure is due to missing implementation, not test bugs
- Error output confirms: `StmtParallel undefined`, `Parallel undefined`

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with AST types** (script.go):
   - Add `StmtParallel` constant
   - Define `ParallelBlock` struct
   - Add `Parallel` field to `Statement`

2. **Then Parser** (script.go):
   - Implement `parseParallelBlock()`
   - Hook into `parseBlock()` before `for` detection
   - Add body content validation (only spawn/pipeline allowed)

3. **Then Validator** (script.go):
   - Add `case StmtParallel:` to `validateFnCalls`
   - Add `case StmtParallel:` to `countStagesInBlock`

4. **Then Executor** (script.go):
   - Add `"sync"` import
   - Implement three-phase parallel execution in `executeBlock`
   - Phase A: sequential intent expansion (main goroutine)
   - Phase B: parallel spawn (goroutine per task + WaitGroup)
   - Phase C: sequential result collection (main goroutine)

5. **Run tests after each layer** to verify progress

**Suggested Order:**

```
script.go: AST types           → parallel_test.go:TestParseScript_Parallel_BasicBlock (1 test)
script.go: parseParallelBlock  → parallel_test.go:TestParseScript_Parallel_* (5 tests)
script.go: parse errors        → parallel_test.go:TestParseScript_Error_Parallel* (6 tests)
script.go: validator           → (enables fn call in parallel error test)
script.go: stage counting      → parallel_test.go:TestScriptExecutor_Parallel_StageCount (1 test)
script.go: executor basics     → parallel_test.go:TestScriptExecutor_Parallel_AllSucceed (1 test)
script.go: assignment          → parallel_test.go:TestScriptExecutor_Parallel_Assignment (1 test)
script.go: fault tolerance     → parallel_test.go:TestScriptExecutor_Parallel_OneFails_* (2 tests)
script.go: on-error            → parallel_test.go:TestScriptExecutor_Parallel_OnError (1 test)
script.go: intent expansion    → parallel_test.go:TestScriptExecutor_Parallel_IntentExpansion (1 test)
script.go: strict expansion    → parallel_test.go:TestScriptExecutor_Parallel_UndefinedVar (1 test)
script.go: empty + cancel      → parallel_test.go:TestScriptExecutor_Parallel_Empty/ContextCancel (2 tests)
script.go: pipeline support    → parallel_test.go:TestScriptExecutor_Parallel_Pipeline (1 test)
script.go: token + lastresult  → parallel_test.go:*TokenAgg/*LastResult (2 tests)
script.go: captured result     → parallel_test.go:*CapturedResult_Condition (1 test)
integration: combinations      → parallel_test.go:TestScriptExecutor_Parallel_InFor/InFunc/AfterDS/ResultInWhile (4 tests)
race: go test -race             → parallel_test.go:TestScriptExecutor_Parallel_NoRace (1 test)
performance: NFR38              → parallel_test.go:TestParseScript_Parallel_Performance_NFR38 (1 test)
```

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

**DEV Agent Responsibilities:**

1. **Verify all 30 tests pass** (green phase complete)
2. **Run `go test -race ./shell/...`** 确认无数据竞争
3. **Review code for quality**: 三阶段执行逻辑清晰，错误处理完备
4. **Ensure existing tests still pass**: `go test ./shell/...` 无回归
5. **Check for dead code**: 确保没有未使用的函数或变量
6. **Performance check**: `go test -bench . ./shell/...` 确认无性能退化

---

## Next Steps

1. **Share this checklist and failing tests** with the dev workflow (manual handoff)
2. **Review this checklist** with team
3. **Run failing tests** to confirm RED phase: `go test ./shell/ 2>&1 | head -20`
4. **Begin implementation** following the suggested order above
5. **Work one test group at a time** (red → green for each)
6. **When all tests pass**, run `go test -race ./shell/...` 确认无数据竞争
7. **When refactoring complete**, manually update story status to 'done' in sprint-status.yaml

---

## Knowledge Base References Applied

This ATDD workflow consulted the following project artifacts:

- **18-4-spawn-return-capture-and-parallel-execution.md** - Story spec with detailed tasks, dev notes, architecture constraints
- **epic-18-agentshell完整脚本语言** - Story 18.4 acceptance criteria, FR100/FR101, NFR38/NFR39
- **shell/script.go** - 现有 ParseScript, parseBlock, executeBlock, ScriptExecutor, Statement 类型
- **shell/env.go** - Environment, ExpandStrict（线程安全注意事项）
- **shell/pipe.go** - KernelSpawner interface, PipelineExecutor
- **shell/pipe_test.go** - mockSpawner 测试桩定义（非线程安全）
- **shell/data_test.go** - Story 18.3 测试模式参考（同包 mock 复用）

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./shell/ 2>&1 | head -25`

**Results:**

```
# github.com/rnixai/rnix/shell [github.com/rnixai/rnix/shell.test]
shell/parallel_test.go:82:18: undefined: StmtParallel
shell/parallel_test.go:83:45: undefined: StmtParallel
shell/parallel_test.go:85:10: stmt.Parallel undefined (type Statement has no field or method Parallel)
shell/parallel_test.go:88:14: stmt.Parallel undefined (type Statement has no field or method Parallel)
shell/parallel_test.go:89:55: stmt.Parallel undefined (type Statement has no field or method Parallel)
shell/parallel_test.go:91:25: stmt.Parallel undefined (type Statement has no field or method Parallel)
shell/parallel_test.go:108:18: undefined: StmtParallel
shell/parallel_test.go:109:45: undefined: StmtParallel
shell/parallel_test.go:111:14: stmt.Parallel undefined (type Statement has no field or method Parallel)
shell/parallel_test.go:112:46: stmt.Parallel undefined (type Statement has no field or method Parallel)
shell/parallel_test.go:112:46: too many errors
FAIL    github.com/rnixai/rnix/shell [build failed]
```

**Summary:**

- Total tests: 30
- Passing: 0 (expected)
- Failing: 30 (compile errors, expected)
- Status: ✅ RED phase verified

**Expected Failure Messages:**

- `undefined: StmtParallel` — parallel 语句类型未定义
- `stmt.Parallel undefined (type Statement has no field or method Parallel)` — Statement 缺少 Parallel 字段

---

## Notes

- Go 编译器在同一包内发现未定义类型时，整个包都无法编译。因此 RED phase 表现为编译失败而非测试运行失败。这是 Go TDD 的标准行为。
- `concurrentMockSpawner` 使用 `sync.Mutex` 保护 `calls` 切片和 `atomic.Int32` 计数有序结果索引，确保并行测试的线程安全。
- `mockSpawner`（来自 `pipe_test.go`）**不用于**执行测试——仅因同包可见而间接编译。所有 parallel 执行测试使用 `concurrentMockSpawner`。
- Story 18.4 的核心设计是三阶段执行模型：阶段 A 顺序展开（env 读取安全）、阶段 B 并行执行（仅调用 SpawnAndWait，写入预分配 results slice）、阶段 C 顺序收集（env 写入安全）。
- `parallel` 关键字已在 `reservedKeywords` 中预注册（Phase 3 预留），本 Story 将其从预留变为已实现。
- parallel 块是容错的——即使某个 spawn 返回非零 exitCode，脚本也不会停止。与顺序执行中未赋值 spawn 失败则停止的行为不同。

---

**Generated by BMad TEA Agent** - 2026-03-09
