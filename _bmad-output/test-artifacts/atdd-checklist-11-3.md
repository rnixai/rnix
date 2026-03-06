---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-checklist
lastStep: step-05-checklist
lastSaved: '2026-03-03'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/11-3-minimal-control-structures.md
  - shell/script.go
  - shell/script_test.go
  - shell/pipe_test.go
  - shell/env.go
  - cmd/rnix/main.go
  - cmd/rnix/main_test.go
---

# ATDD Checklist - Epic 11, Story 3: 最小控制结构 (Minimal Control Structures)

**Date:** 2026-03-03
**Author:** Decker
**Primary Test Level:** Unit (Go `testing`)

---

## Story Summary

为 AgentShell 添加最小控制结构支持，包括 if/else/end 条件分支、on-error 内联错误处理和嵌套控制结构。

**As a** 用户
**I want** 在 AgentShell 中使用 if-else 和 on-error 编排执行流程
**So that** 智能体工作流可以有条件分支和错误处理

---

## Acceptance Criteria

1. **AC1**: if/else/end 条件分支 — 按条件分支正确执行（FR68）
2. **AC2**: on-error 内联错误处理 — 主命令失败时自动执行回滚
3. **AC3**: 嵌套控制结构 — 超过 1 层嵌套正确执行

---

## Failing Tests Created (RED Phase)

### Unit Tests (26 tests)

**File:** `shell/script_test.go` — 25 新测试

| 测试 ID | 函数名 | 优先级 | AC | 失败原因 |
|---------|--------|--------|-----|---------|
| 11.3-UNIT-001 | `TestParseScript_IfElseEnd_Basic` | P0 | AC1 | `StmtIf` 未定义，`Statement.If`/`.Assign` 字段不存在 |
| 11.3-UNIT-002 | `TestParseScript_IfNoElse` | P0 | AC1 | `StmtIf` 未定义，`Statement.If` 不存在 |
| 11.3-UNIT-003 | `TestParseScript_NestedIf` | P0 | AC3 | `StmtIf` 未定义，`Statement.If` 不存在 |
| 11.3-UNIT-004 | `TestParseScript_Condition_VarPropEquals` | P0 | AC1 | `Statement.If.Condition` 不存在 |
| 11.3-UNIT-005 | `TestParseScript_Condition_PlainVar` | P1 | AC1 | `Statement.If.Condition` 不存在 |
| 11.3-UNIT-006 | `TestParseScript_AssignmentSpawn` | P0 | AC1 | `Statement.Assign` 不存在 |
| 11.3-UNIT-007 | `TestParseScript_OnError` | P0 | AC2 | `Statement.OnError` 不存在 |
| 11.3-UNIT-008 | `TestParseScript_AssignmentPlusOnError` | P0 | AC1,AC2 | `Statement.Assign`/`.OnError` 不存在 |
| 11.3-UNIT-009 | `TestParseScript_Error_UnclosedIf` | P0 | — | `ParseScript` 不识别 `if` 语法 |
| 11.3-UNIT-010 | `TestParseScript_Error_ElseOutsideIf` | P1 | — | `ParseScript` 不识别 `else`/`end` |
| 11.3-UNIT-011 | `TestParseScript_Error_InvalidCondition` | P1 | — | `ParseScript` 不识别 `if` 语法 |
| 11.3-UNIT-012 | `TestScriptExecutor_IfThenBranch` | P0 | AC1 | 编译失败（依赖 `Statement.Assign`） |
| 11.3-UNIT-013 | `TestScriptExecutor_IfElseBranch` | P0 | AC1 | 编译失败 |
| 11.3-UNIT-014 | `TestScriptExecutor_IfNoElse_SkipWhenFalse` | P0 | AC1 | 编译失败 |
| 11.3-UNIT-015 | `TestScriptExecutor_NestedIf` | P0 | AC3 | 编译失败 |
| 11.3-UNIT-016 | `TestScriptExecutor_AssignSpawn_NoBreak` | P0 | AC1 | 编译失败 |
| 11.3-UNIT-017 | `TestScriptExecutor_AssignSpawn_VarExpansion` | P0 | AC1 | 编译失败 |
| 11.3-UNIT-018 | `TestScriptExecutor_OnError_Triggered` | P0 | AC2 | 编译失败（依赖 `Statement.OnError`） |
| 11.3-UNIT-019 | `TestScriptExecutor_OnError_NotTriggered` | P0 | AC2 | 编译失败 |
| 11.3-UNIT-020 | `TestScriptExecutor_OnError_HandlerSuccess_Continues` | P0 | AC2 | 编译失败 |
| 11.3-UNIT-021 | `TestScriptExecutor_OnError_HandlerFail_Breaks` | P0 | AC2 | 编译失败 |
| 11.3-UNIT-022 | `TestScriptExecutor_Condition_EnvVar` | P1 | AC1 | 编译失败 |
| 11.3-UNIT-023 | `TestScriptExecutor_Condition_NotEquals` | P1 | AC1 | 编译失败 |
| EXTRA-001 | `TestParseScript_OnError_InsideQuotes` | P1 | AC2 | `Statement.OnError` 不存在 |
| EXTRA-002 | `TestParseScript_IfCaseInsensitive` | P1 | AC1 | `StmtIf` 未定义 |
| EXTRA-003 | `TestParseScript_EmptyThenBody` | P1 | AC1 | `Statement.If` 不存在 |

**File:** `cmd/rnix/main_test.go` — 1 新测试

| 测试 ID | 函数名 | 优先级 | AC | 失败原因 |
|---------|--------|--------|-----|---------|
| 11.3-REG-003 | `TestIsScriptSyntax_OnError` | P1 | AC2 | `isScriptSyntax` 未检测 `on-error`，返回 `false` |

---

## Data Factories Created

N/A — Go 后端项目使用 `mockSpawner`（已在 `pipe_test.go` 中定义），无需额外工厂。

### mockSpawner（复用）

**File:** `shell/pipe_test.go`

**Exports:**
- `mockSpawner` — 模拟 `KernelSpawner` 接口，按序返回预定义结果
- `mockResult` — 单次调用的返回值（result, exitCode, tokens, err）
- `mockCall` — 记录调用参数（intent, agent, model）

---

## Fixtures Created

N/A — Go 测试使用内联 setup，无需独立 fixture 文件。

---

## Mock Requirements

### KernelSpawner Mock

**接口:** `KernelSpawner.SpawnAndWait(ctx, intent, agent, model) (string, int, int, error)`

**已有实现:** `shell/pipe_test.go` 中的 `mockSpawner`

**新增测试场景需要的 mock 配置：**

- 赋值 spawn（exitCode=0 和 exitCode≠0 各一组）
- on-error 触发（主命令 exitCode=1 + handler exitCode=0）
- on-error handler 失败（主命令 exitCode=1 + handler exitCode=2）
- 嵌套 if 分支（多次 spawn 调用）

---

## Required data-testid Attributes

N/A — 后端 Go 项目，无 UI 组件。

---

## Implementation Checklist

### Test: ParseScript if/else/end 系列 (UNIT-001 ~ UNIT-005, EXTRA-002, EXTRA-003)

**File:** `shell/script_test.go`

**Tasks to make these tests pass:**

- [ ] `shell/script.go`：新增 `StmtIf StatementKind = "if"`
- [ ] `shell/script.go`：定义 `Condition` 结构体（VarName, Property, Operator, Value）
- [ ] `shell/script.go`：定义 `IfBlock` 结构体（Condition, Then []Statement, Else []Statement）
- [ ] `shell/script.go`：扩展 `Statement` 新增 `If *IfBlock` 字段
- [ ] `shell/script.go`：实现 `parseCondition(s string) (*Condition, error)`
- [ ] `shell/script.go`：实现 `parseBlock(lines, startIdx, insideIf) ([]Statement, int, error)`
- [ ] `shell/script.go`：实现 `parseIfBlock(lines, ifLineIdx) (*IfBlock, int, error)`
- [ ] `shell/script.go`：重构 `ParseScript` 调用 `parseBlock`
- [ ] Run test: `go test ./shell/... -run "TestParseScript_If|TestParseScript_Nested|TestParseScript_Condition|TestParseScript_Error|TestParseScript_EmptyThenBody" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 2 hours

---

### Test: ParseScript 赋值 spawn (UNIT-006)

**File:** `shell/script_test.go`

**Tasks to make this test pass:**

- [ ] `shell/script.go`：扩展 `Statement` 新增 `Assign string` 字段
- [ ] `shell/script.go`：实现 `isAssignment(line string) (varName, rest string, ok bool)`
- [ ] `shell/script.go`：更新 `parseStatement` 在 export 之后检测赋值语法
- [ ] Run test: `go test ./shell/... -run TestParseScript_AssignmentSpawn -v`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: ParseScript on-error (UNIT-007, UNIT-008, EXTRA-001)

**File:** `shell/script_test.go`

**Tasks to make these tests pass:**

- [ ] `shell/script.go`：扩展 `Statement` 新增 `OnError *Command` 字段
- [ ] `shell/script.go`：实现 `splitOnError(line string) (main, handler string, found bool)`
- [ ] `shell/script.go`：更新 `parseStatement` 分派顺序——赋值 → on-error split → pipeline → spawn
- [ ] Run test: `go test ./shell/... -run "TestParseScript_OnError|TestParseScript_AssignmentPlusOnError" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: ScriptExecutor if 分支执行 (UNIT-012 ~ UNIT-015)

**File:** `shell/script_test.go`

**Tasks to make these tests pass:**

- [ ] `shell/script.go`：定义 `SpawnResult` 结构体（ExitCode, Result, Tokens）
- [ ] `shell/script.go`：`ScriptExecutor` 新增 `captures map[string]*SpawnResult` 字段
- [ ] `shell/script.go`：`NewScriptExecutor` 初始化 captures map
- [ ] `shell/script.go`：实现 `evalCondition(cond *Condition) (bool, error)`
- [ ] `shell/script.go`：实现 `executeBlock(ctx, stmts, result, stageNum, totalStages) error`
- [ ] `shell/script.go`：重构 `Execute` 调用 `executeBlock`
- [ ] `shell/script.go`：`StmtIf` case 在 `executeBlock` 中调用 `evalCondition` + 递归执行分支
- [ ] Run test: `go test ./shell/... -run "TestScriptExecutor_If|TestScriptExecutor_Nested" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 2 hours

---

### Test: ScriptExecutor 赋值 spawn (UNIT-016, UNIT-017)

**File:** `shell/script_test.go`

**Tasks to make these tests pass:**

- [ ] `shell/script.go`：赋值 spawn 执行——结果存入 captures + `env.Set`（文本输出）
- [ ] `shell/script.go`：赋值 spawn 非零 ExitCode 不中断脚本
- [ ] Run test: `go test ./shell/... -run "TestScriptExecutor_AssignSpawn" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: ScriptExecutor on-error (UNIT-018 ~ UNIT-021)

**File:** `shell/script_test.go`

**Tasks to make these tests pass:**

- [ ] `shell/script.go`：on-error 执行——主命令失败时执行 handler
- [ ] `shell/script.go`：on-error handler 成功→脚本继续
- [ ] `shell/script.go`：on-error handler 失败→脚本中断
- [ ] `shell/script.go`：更新 `countExecutableStages` 递归计算（含 on-error handler）
- [ ] Run test: `go test ./shell/... -run "TestScriptExecutor_OnError" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: ScriptExecutor 条件 env 变量和 != (UNIT-022, UNIT-023)

**File:** `shell/script_test.go`

**Tasks to make these tests pass:**

- [ ] `shell/script.go`：`evalCondition` 支持从 env 查找普通变量
- [ ] `shell/script.go`：`evalCondition` 支持 `!=` 操作符
- [ ] Run test: `go test ./shell/... -run "TestScriptExecutor_Condition" -v`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: isScriptSyntax on-error 检测 (REG-003)

**File:** `cmd/rnix/main_test.go`

**Tasks to make this test pass:**

- [ ] `cmd/rnix/main.go`：`isScriptSyntax` 新增 `on-error` 关键字检测
- [ ] Run test: `go test ./cmd/rnix/... -run TestIsScriptSyntax_OnError -v`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

### 回归验证 (REG-001, REG-002)

**Tasks:**

- [ ] Run: `go test ./shell/...` — 所有 11.2 测试仍通过
- [ ] Run: `go test ./cmd/rnix/...` — 所有 11.2 CLI 测试仍通过
- [ ] ✅ Regression verified

**Estimated Effort:** 0.25 hours

---

## Running Tests

```bash
# Run all failing tests for this story (shell package)
go test ./shell/... -v

# Run specific test file
go test ./shell/... -run "11\.3" -v

# Run only parser tests
go test ./shell/... -run "TestParseScript_(If|Nested|Condition|Assignment|OnError|Error)" -v

# Run only executor tests
go test ./shell/... -run "TestScriptExecutor_(If|Nested|Assign|OnError|Condition)" -v

# Run CLI test
go test ./cmd/rnix/... -run TestIsScriptSyntax_OnError -v

# Run full regression
go test ./shell/... ./cmd/rnix/... -v

# Run with race detector
go test -race ./shell/... ./cmd/rnix/... -v
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ 26 个测试已写入并确认失败
- ✅ shell/ 编译失败（引用 StmtIf, IfBlock, Condition, Statement.If/Assign/OnError）
- ✅ cmd/rnix/ 运行时失败（isScriptSyntax 不检测 on-error）
- ✅ 复用现有 mockSpawner（无需新 fixture）
- ✅ Implementation checklist 已创建

**Verification:**

- `go test ./shell/...` → 编译失败（预期）
- `go test ./cmd/rnix/... -run TestIsScriptSyntax_OnError` → FAIL（预期）

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **从 Task 1 开始**：新增类型与扩展 Statement
2. **逐步实现**：每完成一组类型/函数后运行对应测试
3. **优先级**：先实现 P0 测试所需代码，再处理 P1
4. **一次一个测试组**（不要一次性实现所有）
5. **运行回归**：每步完成后确认 11.2 测试不受影响

**Recommended Implementation Order:**

1. 新增类型定义（StmtIf, Condition, IfBlock, SpawnResult, Statement 扩展）→ 编译通过
2. 解析器重构（parseBlock, parseIfBlock, parseCondition）→ Parser tests GREEN
3. 赋值和 on-error 解析（isAssignment, splitOnError, parseStatement 更新）→ More parser tests GREEN
4. 执行器重构（captures, evalCondition, executeBlock）→ Executor tests GREEN
5. CLI 更新（isScriptSyntax on-error 检测）→ CLI test GREEN
6. 回归验证 → All tests GREEN

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 确认所有 26 个新测试 + 所有 11.2 测试通过
2. 审查递归解析器代码质量
3. 检查 splitOnError 引号处理边界
4. 确认 countExecutableStages 递归计算正确
5. 运行 `go vet ./...` 和 `golangci-lint run`

---

## Next Steps

1. **运行失败测试** 确认 RED 阶段: `go test ./shell/... && go test ./cmd/rnix/... -run TestIsScriptSyntax_OnError`
2. **开始实现** 按 Implementation Checklist 顺序
3. **一次一个测试组** (red → green for each)
4. **完成后回归验证**: `go test ./shell/... ./cmd/rnix/... -v`
5. **重构完成后** 更新 sprint-status.yaml

---

## Knowledge Base References Applied

- **test-quality.md** — 测试设计原则（Given-When-Then, 确定性, 隔离性）
- **data-factories.md** — mockSpawner 模式（复用 11.1 factory 模式）
- **test-levels-framework.md** — 测试级别选择（后端→Unit 为主）

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./shell/...`

**Results:**

```
# github.com/rnixai/rnix/shell [github.com/rnixai/rnix/shell.test]
shell/script_test.go:568:12: assign.Assign undefined (type Statement has no field or method Assign)
shell/script_test.go:573:20: undefined: StmtIf
shell/script_test.go:576:12: ifStmt.If undefined (type Statement has no field or method If)
...
shell/script_test.go:585:12: too many errors
FAIL    github.com/rnixai/rnix/shell [build failed]
```

**Command:** `go test ./cmd/rnix/... -run TestIsScriptSyntax_OnError -v`

**Results:**

```
=== RUN   TestIsScriptSyntax_OnError
    main_test.go:1127: isScriptSyntax("spawn \"A\" on-error spawn \"B\"") = false, want true
--- FAIL: TestIsScriptSyntax_OnError (0.00s)
FAIL
```

**Summary:**

- Total new tests: 26
- Passing: 0 (expected)
- Failing: 26 (expected — compile failure + runtime failure)
- Status: ✅ RED phase verified

---

## Notes

- 所有控制结构逻辑在 `shell/script.go` 内，不引入新文件
- `shell/` 不依赖 `kernel/`/`vfs/`/`drivers/` — 通过 `KernelSpawner` 接口解耦
- IPC 层零改动 — `exec_script` 协议已足够通用
- 新增 `strconv` 导入（`strconv.Itoa` 用于 exitcode 转字符串比较）
- `mockSpawner` 来自 `pipe_test.go`，同包内直接使用

---

**Generated by BMad TEA Agent** - 2026-03-03
