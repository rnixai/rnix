---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-09'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/18-2-function-definition-and-invocation.md
  - shell/script.go
  - shell/script_test.go
  - _bmad/tea/config.yaml
  - _bmad/tea/testarch/knowledge/test-quality.md
---

# ATDD Checklist - Epic 18, Story 2: 函数定义与调用

**Date:** 2026-03-09
**Author:** Decker
**Primary Test Level:** Unit (Go backend)

---

## Story Summary

在 AgentShell 脚本中实现函数定义和调用功能，允许开发者定义可复用的脚本逻辑，支持参数传递和返回值。

**As a** 应用开发者
**I want** 在 AgentShell 脚本中定义和调用函数
**So that** 我可以复用脚本逻辑

---

## Acceptance Criteria

1. fn 定义 + 调用，参数正确传递，返回值可用
2. return result 返回值可用
3. 参数数量不匹配 → 解析错误（含行号和期望参数数量）
4. 无参数函数 fn setup() 正常执行
5. 函数体内 spawn/if/for/while 正确执行
6. 嵌套函数调用（A 调 B），参数独立
7. 调用未定义函数 → 运行时错误
8. 参数作用域隔离（函数返回后外部变量恢复）
9. return 不带值 → 返回空字符串
10. 函数名为保留关键字 → 解析错误

---

## Failing Tests Created (RED Phase)

### Unit Tests (33 tests)

**File:** `shell/script_test.go` (追加到文件末尾)

#### 解析测试 (ParseScript)

- ✅ **Test:** TestParseScript_FnDef_WithParams (18.2-UNIT-001)
  - **Status:** RED - `StmtFnDef` undefined, `Statement.FnDef` undefined
  - **Verifies:** AC1 — fn 基本定义解析（带参数）

- ✅ **Test:** TestParseScript_FnDef_NoParams (18.2-UNIT-002)
  - **Status:** RED - `StmtFnDef` undefined
  - **Verifies:** AC4 — 无参数函数定义

- ✅ **Test:** TestParseScript_FnDef_MultipleParams (18.2-UNIT-003)
  - **Status:** RED - `Statement.FnDef` undefined
  - **Verifies:** AC1 — 多参数定义

- ✅ **Test:** TestParseScript_FnDef_NestedStatements (18.2-UNIT-004)
  - **Status:** RED - `Statement.FnDef` undefined
  - **Verifies:** AC5 — 函数体内嵌套 spawn/if/for/while

- ✅ **Test:** TestParseScript_Error_FnNameReservedKeyword (18.2-UNIT-005)
  - **Status:** RED - compile error
  - **Verifies:** AC10 — 函数名是保留关键字

- ✅ **Test:** TestParseScript_Error_FnDuplicateParam (18.2-UNIT-006)
  - **Status:** RED - compile error
  - **Verifies:** 参数名重复

- ✅ **Test:** TestParseScript_Error_FnUnclosed (18.2-UNIT-007)
  - **Status:** RED - compile error
  - **Verifies:** 未闭合 fn 块

- ✅ **Test:** TestParseScript_Error_FnNestedDefinition (18.2-UNIT-008)
  - **Status:** RED - compile error
  - **Verifies:** 块内定义 fn 不允许

- ✅ **Test:** TestParseScript_Error_FnDuplicateName (18.2-UNIT-009)
  - **Status:** RED - compile error
  - **Verifies:** 重复定义同名函数

- ✅ **Test:** TestParseScript_FnCall_WithArgs (18.2-UNIT-010)
  - **Status:** RED - `StmtFnCall` undefined, `Statement.FnCall` undefined
  - **Verifies:** AC1 — 函数调用解析（带参数）

- ✅ **Test:** TestParseScript_FnCall_NoArgs (18.2-UNIT-011)
  - **Status:** RED - `StmtFnCall` undefined
  - **Verifies:** AC4 — 无参数调用

- ✅ **Test:** TestParseScript_FnCall_Assignment (18.2-UNIT-012)
  - **Status:** RED - `StmtFnCall` undefined
  - **Verifies:** AC1, AC2 — 赋值形式函数调用

- ✅ **Test:** TestParseScript_Error_FnCallArgCountMismatch (18.2-UNIT-013)
  - **Status:** RED - compile error
  - **Verifies:** AC3 — 参数数量不匹配

- ✅ **Test:** TestParseScript_Error_FnCallUndefined (18.2-UNIT-014)
  - **Status:** RED - compile error
  - **Verifies:** AC7 — 调用未定义函数

- ✅ **Test:** TestParseScript_Return_WithValue (18.2-UNIT-015)
  - **Status:** RED - `StmtReturn` undefined, `Statement.Return` undefined
  - **Verifies:** AC2 — return 带值

- ✅ **Test:** TestParseScript_Return_NoValue (18.2-UNIT-016)
  - **Status:** RED - `StmtReturn` undefined
  - **Verifies:** AC9 — return 不带值

- ✅ **Test:** TestParseScript_Return_LiteralValue (18.2-UNIT-017)
  - **Status:** RED - `StmtReturn` undefined
  - **Verifies:** AC2 — return 字面量

- ✅ **Test:** TestParseScript_Error_ReturnAtTopLevel (18.2-UNIT-018)
  - **Status:** RED - compile error
  - **Verifies:** 顶层 return 报错

- ✅ **Test:** TestParseScript_FnCaseInsensitive (18.2-UNIT-019)
  - **Status:** RED - `StmtFnDef` undefined
  - **Verifies:** fn 大小写不敏感

- ✅ **Test:** TestParseScript_Return_CaptureProperty (18.2-UNIT-020)
  - **Status:** RED - `Statement.FnDef` undefined
  - **Verifies:** AC2 — return captures 属性

#### 执行测试 (ScriptExecutor)

- ✅ **Test:** TestScriptExecutor_FnCallBasic (18.2-UNIT-021)
  - **Status:** RED - compile error
  - **Verifies:** AC1 — 函数定义 + 调用，参数正确传递

- ✅ **Test:** TestScriptExecutor_FnReturnValueCapture (18.2-UNIT-022)
  - **Status:** RED - compile error
  - **Verifies:** AC2 — return 值捕获

- ✅ **Test:** TestScriptExecutor_FnNoReturn_EmptyString (18.2-UNIT-023)
  - **Status:** RED - compile error
  - **Verifies:** AC9 — 无 return 返回空字符串

- ✅ **Test:** TestScriptExecutor_FnParamScopeIsolation (18.2-UNIT-024)
  - **Status:** RED - compile error
  - **Verifies:** AC8 — 参数作用域隔离

- ✅ **Test:** TestScriptExecutor_FnNestedCalls (18.2-UNIT-025)
  - **Status:** RED - compile error
  - **Verifies:** AC6 — 嵌套函数调用

- ✅ **Test:** TestScriptExecutor_FnNestedCalls_ParamIndependent (18.2-UNIT-026)
  - **Status:** RED - compile error
  - **Verifies:** AC6 — 嵌套调用参数独立

- ✅ **Test:** TestScriptExecutor_FnCallUndefined_RuntimeError (18.2-UNIT-027)
  - **Status:** RED - compile error
  - **Verifies:** AC7 — 运行时未定义函数错误

- ✅ **Test:** TestScriptExecutor_FnCallZeroArgs (18.2-UNIT-028)
  - **Status:** RED - compile error
  - **Verifies:** AC4 — 零参数调用

- ✅ **Test:** TestScriptExecutor_FnBodyWithForAndIf (18.2-UNIT-029)
  - **Status:** RED - compile error
  - **Verifies:** AC5 — 函数体内 for/if 组合

- ✅ **Test:** TestScriptExecutor_FnBodyWithOnError (18.2-UNIT-030)
  - **Status:** RED - compile error
  - **Verifies:** AC5 — 函数体内 spawn on-error

- ✅ **Test:** TestScriptExecutor_FnReturnEarlyExit (18.2-UNIT-031)
  - **Status:** RED - compile error
  - **Verifies:** AC2 — return 中途退出

- ✅ **Test:** TestScriptExecutor_FnReturnEmpty (18.2-UNIT-032)
  - **Status:** RED - compile error
  - **Verifies:** AC9 — return 不带值

#### 组合矩阵测试 (Cross-Feature)

- ✅ **Test:** TestScriptExecutor_FnReturnInForLoop (18.2-CR-001)
  - **Status:** RED - compile error
  - **Verifies:** return 在 for 循环内退出函数

- ✅ **Test:** TestScriptExecutor_FnReturnInIfBranch (18.2-CR-002)
  - **Status:** RED - compile error
  - **Verifies:** return 在 if/else 内退出函数

- ✅ **Test:** TestScriptExecutor_ExitInFnBody_TerminatesScript (18.2-CR-003)
  - **Status:** RED - compile error
  - **Verifies:** exit 在函数体内终止整个脚本

- ✅ **Test:** TestScriptExecutor_FnParamVsForLoopVar (18.2-CR-004)
  - **Status:** RED - compile error
  - **Verifies:** fn 参数与 for 循环变量互不干扰

- ✅ **Test:** TestScriptExecutor_FnMaxCallDepth (18.2-CR-005)
  - **Status:** RED - `MaxCallDepth` undefined
  - **Verifies:** 递归深度保护

- ✅ **Test:** TestCountStages_FnDefZero (18.2-CR-006)
  - **Status:** RED - compile error
  - **Verifies:** fn 定义不计入执行阶段数

- ✅ **Test:** TestCountStages_FnCallOne (18.2-CR-007)
  - **Status:** RED - compile error
  - **Verifies:** fn 调用计为一个执行阶段

- ✅ **Test:** TestParseScript_Error_FnParamReservedKeyword (18.2-CR-008)
  - **Status:** RED - compile error
  - **Verifies:** 参数名是保留关键字

- ✅ **Test:** TestScriptExecutor_FnCallAndSpawnAssignment (18.2-CR-009)
  - **Status:** RED - compile error
  - **Verifies:** fn 调用和 spawn 赋值共存

- ✅ **Test:** TestScriptExecutor_FnReturnCaptureResult (18.2-CR-010)
  - **Status:** RED - compile error
  - **Verifies:** return captures.result 属性

- ✅ **Test:** TestScriptExecutor_FnReturnDoesNotLeak (18.2-CR-011)
  - **Status:** RED - compile error
  - **Verifies:** ErrFnReturn 不泄漏到函数外部

- ✅ **Test:** TestScriptExecutor_FnReturnInWhileLoop (18.2-CR-012)
  - **Status:** RED - compile error
  - **Verifies:** return 在 while 循环内退出函数

- ✅ **Test:** TestScriptExecutor_FnCallWithVarArgs (18.2-CR-013)
  - **Status:** RED - compile error
  - **Verifies:** 函数调用参数使用变量引用

---

## Data Factories Created

N/A — Go 项目使用 `mockSpawner` 模式，复用 `pipe_test.go` 中已有的 mock 基础设施。

---

## Fixtures Created

N/A — Go 内置 testing 包，使用 `mockSpawner` 和 `mockWaitableSpawner` 作为测试基础设施（已在 `pipe_test.go` 中定义）。

---

## Mock Requirements

### mockSpawner (已存在)

已在 `pipe_test.go` 中定义，提供：
- `SpawnAndWait(ctx, intent, agent, model)` — 模拟进程 spawn
- `Wait(ctx, pid)` — 模拟等待进程

### mockWaitableSpawner (已存在)

扩展 `mockSpawner`，新增 `Wait` 方法实现。

---

## Implementation Checklist

### Task 1: AST 节点扩展

**Tests:** 18.2-UNIT-001 ~ 003, 010 ~ 012, 015 ~ 017

- [ ] 新增 `StmtFnDef`, `StmtFnCall`, `StmtReturn` StatementKind 常量
- [ ] 新增 `FnDef` 结构体 (Name, Params, Body)
- [ ] 新增 `FnCallStmt` 结构体 (Name, Args)
- [ ] 新增 `ReturnStmt` 结构体 (Value)
- [ ] 扩展 `Statement` 结构体 (FnDef, FnCall, Return 字段)
- [ ] 新增 `Script.Functions map[string]*FnDef` 字段
- [ ] Run test: `go test ./shell/ -run TestParseScript_FnDef`
- [ ] ✅ 编译通过

### Task 2: 解析器 — 函数定义

**Tests:** 18.2-UNIT-001 ~ 009, 019

- [ ] `parseBlock` 识别 `fn` 关键字（仅顶层允许）
- [ ] 实现 `parseFnDef` (语法: `fn NAME(PARAMS) ... end`)
- [ ] 函数名校验（非保留关键字）
- [ ] 参数名校验（非重复、非保留关键字）
- [ ] 收集 FnDef 到 Script.Functions
- [ ] 重复函数名检测
- [ ] Run test: `go test ./shell/ -run "TestParseScript_FnDef|TestParseScript_Error_Fn"`
- [ ] ✅ Tests pass (green phase)

### Task 3: 解析器 — 函数调用

**Tests:** 18.2-UNIT-010 ~ 014

- [ ] `parseStatement` 识别 `IDENTIFIER(` 模式
- [ ] 实现 `parseFnCall` (参数列表解析)
- [ ] 赋值形式: `VAR = IDENTIFIER(ARGS)`
- [ ] 解析后全局校验（存在性 + 参数数量）
- [ ] Run test: `go test ./shell/ -run "TestParseScript_FnCall"`
- [ ] ✅ Tests pass (green phase)

### Task 4: 解析器 — return 语句

**Tests:** 18.2-UNIT-015 ~ 018, 020

- [ ] `parseBlock` 识别 `return` 关键字（仅块内）
- [ ] 实现 `parseReturnStatement` (支持: return / return $VAR / return "literal" / return $VAR.result)
- [ ] 顶层 return 报错
- [ ] Run test: `go test ./shell/ -run "TestParseScript_Return"`
- [ ] ✅ Tests pass (green phase)

### Task 5: 解释器 — 函数注册

**Tests:** 18.2-UNIT-021, 18.2-CR-006 ~ 007

- [ ] ScriptExecutor 新增 `functions map[string]*FnDef`
- [ ] Execute 入口从 script.Functions 加载
- [ ] StmtFnDef 分支：跳过（不执行）
- [ ] countStagesInBlock 处理新类型
- [ ] Run test: `go test ./shell/ -run "TestCountStages_Fn"`
- [ ] ✅ Tests pass (green phase)

### Task 6: 解释器 — 函数调用

**Tests:** 18.2-UNIT-021 ~ 030, 18.2-CR-001 ~ 004, 009, 013

- [ ] executeBlock 处理 StmtFnCall
- [ ] 查找函数定义，未找到返回错误
- [ ] 参数展开 + save/restore 模式
- [ ] 赋值形式捕获返回值
- [ ] 新增 MaxCallDepth 常量 + callDepth 递归保护
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_Fn"`
- [ ] ✅ Tests pass (green phase)

### Task 7: 解释器 — return 语句

**Tests:** 18.2-UNIT-031 ~ 032, 18.2-CR-001 ~ 003, 010 ~ 012

- [ ] 定义 ErrFnReturn 类型
- [ ] executeBlock 处理 StmtReturn
- [ ] 函数调用处捕获 ErrFnReturn
- [ ] 无 return 返回空字符串
- [ ] ErrFnReturn 不泄漏到函数外
- [ ] 穿透 for/while/if 块
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_FnReturn"`
- [ ] ✅ Tests pass (green phase)

### Task 8: 竞态测试

- [ ] `go test -race ./shell/...`
- [ ] ✅ 无竞态

---

## Running Tests

```bash
# Run all failing tests for this story
go test ./shell/ -run "18.2|FnDef|FnCall|FnReturn|FnParam|FnNested|FnBody|FnMax|CountStages_Fn"

# Run specific test
go test ./shell/ -run TestParseScript_FnDef_WithParams -v

# Run all shell tests
go test ./shell/ -v

# Run with race detector
go test -race ./shell/...

# Run with coverage
go test ./shell/ -cover -coverprofile=coverage.out
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 33 tests written and failing (compile error)
- ✅ Tests reference not-yet-implemented types: StmtFnDef, StmtFnCall, StmtReturn, FnDef, FnCallStmt, ReturnStmt, ErrFnReturn, MaxCallDepth, Script.Functions
- ✅ Mock infrastructure reused (mockSpawner from pipe_test.go)
- ✅ Implementation checklist created (7 tasks)

**Verification:**

- All tests fail at compile time (expected)
- Failure is due to missing types/fields, not test bugs
- Error: `undefined: StmtFnDef` (and other not-yet-defined symbols)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Pick Task 1 (AST)** — add types to make tests compile
2. **Pick Task 2 (Parser: fn def)** — implement parsing
3. **Pick Task 3 (Parser: fn call)** — implement call parsing
4. **Pick Task 4 (Parser: return)** — implement return parsing
5. **Pick Task 5 (Executor: register)** — skip fn defs
6. **Pick Task 6 (Executor: call)** — execute function calls
7. **Pick Task 7 (Executor: return)** — handle return values
8. **Run all tests** to verify green

**Key Principles:**

- One task at a time
- Run tests after each task
- Minimal implementation first

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all 33 tests pass
2. Review code quality
3. Extract duplications
4. Run race detector: `go test -race ./shell/...`
5. Ensure existing Story 11.2/11.3/18.1 tests still pass

---

## Next Steps

1. **Share this checklist** with the dev workflow
2. **Run failing tests** to confirm RED phase: `go test ./shell/`
3. **Begin implementation** using Task 1-7 as roadmap
4. **Work one task at a time** (red → green)
5. **When all tests pass**, refactor code
6. **Update sprint-status.yaml** when complete

---

## Knowledge Base References Applied

- **test-quality.md** — 确定性测试、隔离性、显式断言
- **test-levels-framework.md** — 单元测试级别选择（Go 后端纯逻辑层）

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./shell/`

**Results:**

```
# github.com/rnixai/rnix/shell [github.com/rnixai/rnix/shell.test]
shell/script_test.go:2241:18: undefined: StmtFnDef
shell/script_test.go:2242:45: undefined: StmtFnDef
shell/script_test.go:2244:10: stmt.FnDef undefined (type Statement has no field or method FnDef)
... (10+ more compile errors)
FAIL    github.com/rnixai/rnix/shell [build failed]
```

**Summary:**

- Total tests: 33 (new for Story 18.2)
- Passing: 0 (expected)
- Failing: 33 (compile error — expected)
- Status: ✅ RED phase verified

---

## Notes

- 所有改动限制在 `shell/` 包内（script.go + script_test.go）
- 无需修改 KernelSpawner 接口或 IPC 层
- 复用 Story 18.1 已建立的 mockWaitableSpawner
- MaxCallDepth 建议值为 100（防止递归深度溢出）
- ErrFnReturn 遵循 ErrScriptExit 模式（特殊错误类型实现流控制）

---

**Generated by BMad TEA Agent** - 2026-03-09
