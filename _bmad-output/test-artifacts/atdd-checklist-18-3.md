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
  - _bmad-output/planning-artifacts/epics/epic-18-agentshell完整脚本语言-agentshell-complete-scripting.md
  - shell/script.go
  - shell/env.go
  - shell/parser.go
  - shell/pipe.go
  - shell/script_test.go
  - shell/env_test.go
  - shell/pipe_test.go
---

# ATDD Checklist - Epic 18, Story 3: 数据结构与字符串插值

**Date:** 2026-03-09
**Author:** Decker
**Primary Test Level:** Unit (Go `testing` package)

---

## Story Summary

AgentShell 脚本支持数组字面量、映射字面量和字符串插值，用户可以处理结构化数据并动态构建智能体意图。未定义变量引用时报告错误并指出行号。

**As a** 应用开发者
**I want** 在 AgentShell 中使用数组、映射和字符串插值
**So that** 我可以处理结构化数据并动态构建智能体意图

---

## Acceptance Criteria

1. **AC1 数组字面量与索引**: `files = ["a.go", "b.go", "c.go"]` → `files[0]` 返回 "a.go"
2. **AC2 映射字面量与属性访问**: `config = {model: "sonnet", budget: 5000}` → `config.model` 返回 "sonnet"
3. **AC3 字符串插值**: `spawn "分析 ${file_path} 的代码质量"` 当 `file_path = "main.go"` → intent = "分析 main.go 的代码质量"
4. **AC4 未定义变量错误**: 字符串插值中引用未定义变量 → 错误含行号和变量名
5. **NFR38 解析性能**: 脚本解析时间 <= 50ms

---

## Failing Tests Created (RED Phase)

### Unit Tests (35 tests)

**File:** `shell/data_test.go` (约 450 行)

#### Parsing Tests (12 tests)

- ✅ **Test:** `TestParseScript_ArrayLitAssignment`
  - **Status:** RED - StmtArrayLit undefined
  - **Verifies:** AC1 数组字面量解析（双引号元素）

- ✅ **Test:** `TestParseScript_ArrayLitAssignment_Unquoted`
  - **Status:** RED - StmtArrayLit undefined
  - **Verifies:** AC1 数组字面量解析（无引号元素）

- ✅ **Test:** `TestParseScript_MapLitAssignment`
  - **Status:** RED - StmtMapLit undefined
  - **Verifies:** AC2 映射字面量解析（基本）

- ✅ **Test:** `TestParseScript_MapLitAssignment_QuotedValues`
  - **Status:** RED - StmtMapLit undefined
  - **Verifies:** AC2 映射字面量解析（带引号值）

- ✅ **Test:** `TestParseScript_ArrayLitSingleElement`
  - **Status:** RED - StmtArrayLit undefined
  - **Verifies:** AC1 单元素数组（边界）

- ✅ **Test:** `TestParseScript_MapLitSingleEntry`
  - **Status:** RED - StmtMapLit undefined
  - **Verifies:** AC2 单键映射（边界）

- ✅ **Test:** `TestParseScript_ArrayLitWithOtherStatements`
  - **Status:** RED - StmtArrayLit undefined
  - **Verifies:** AC1 数组与其他语句共存

- ✅ **Test:** `TestParseScript_MapLitWithOtherStatements`
  - **Status:** RED - StmtMapLit undefined
  - **Verifies:** AC2 映射与其他语句共存

- ✅ **Test:** `TestParseScript_Error_UnclosedArrayBracket`
  - **Status:** RED - 解析应报错但当前不识别语法
  - **Verifies:** AC1 未闭合方括号错误

- ✅ **Test:** `TestParseScript_Error_UnclosedMapBrace`
  - **Status:** RED - 解析应报错但当前不识别语法
  - **Verifies:** AC2 未闭合花括号错误

- ✅ **Test:** `TestParseScript_Error_MapMissingColon`
  - **Status:** RED - 映射解析器不存在
  - **Verifies:** AC2 映射缺少冒号错误

- ✅ **Test:** `TestParseScript_Error_DataLitReservedKeyword`
  - **Status:** RED - 数据字面量赋值解析不存在
  - **Verifies:** 保留关键字不能用作数据变量名

#### Execution Tests (10 tests)

- ✅ **Test:** `TestScriptExecutor_ArrayLit_IndexAccess`
  - **Status:** RED - 数组执行和 ${files[0]} 展开不存在
  - **Verifies:** AC1 数组索引访问

- ✅ **Test:** `TestScriptExecutor_ArrayLit_DifferentIndices`
  - **Status:** RED - 数组执行不存在
  - **Verifies:** AC1 多个不同索引访问

- ✅ **Test:** `TestScriptExecutor_MapLit_PropertyAccess`
  - **Status:** RED - 映射执行和 ${config.model} 展开不存在
  - **Verifies:** AC2 映射属性访问

- ✅ **Test:** `TestScriptExecutor_MapLit_DifferentProperties`
  - **Status:** RED - 映射执行不存在
  - **Verifies:** AC2 多个不同属性访问

- ✅ **Test:** `TestScriptExecutor_StringInterpolation_DefinedVar`
  - **Status:** RED - 编译失败（同包内其他编译错误）
  - **Verifies:** AC3 已定义变量字符串插值

- ✅ **Test:** `TestScriptExecutor_UndefinedVar_ErrorWithLineAndName`
  - **Status:** RED - ExpandStrict 不存在
  - **Verifies:** AC4 未定义变量错误含行号和变量名

- ✅ **Test:** `TestScriptExecutor_ArrayOutOfBounds_Error`
  - **Status:** RED - 数组越界错误处理不存在
  - **Verifies:** AC1 数组越界边界

- ✅ **Test:** `TestScriptExecutor_MapUndefinedKey_Error`
  - **Status:** RED - 映射未定义键错误处理不存在
  - **Verifies:** AC2 映射不存在键边界

- ✅ **Test:** `TestScriptExecutor_ArrayLit_ForLoopIteration`
  - **Status:** RED - for-in $array 语法不存在
  - **Verifies:** AC1 数组与 for 循环迭代

- ✅ **Test:** `TestScriptExecutor_MapLit_PropertyInCondition`
  - **Status:** RED - ${config.model} 在条件中不支持
  - **Verifies:** AC2 映射属性在条件判断中

#### Combination Tests (8 tests)

- ✅ **Test:** `TestScriptExecutor_ArrayAndMapCombined`
  - **Status:** RED - 数组+映射混合使用
  - **Verifies:** AC1+AC2+AC3

- ✅ **Test:** `TestScriptExecutor_ArrayForLoopWithInterpolation`
  - **Status:** RED - 数组+for+插值
  - **Verifies:** AC1+AC3

- ✅ **Test:** `TestScriptExecutor_MapLit_InFunction`
  - **Status:** RED - 映射属性作为函数参数
  - **Verifies:** AC2+函数

- ✅ **Test:** `TestScriptExecutor_ArrayLit_SpawnResultInteraction`
  - **Status:** RED - 数组+赋值 spawn
  - **Verifies:** AC1+赋值

- ✅ **Test:** `TestScriptExecutor_UndefinedVar_CorrectLineNumber`
  - **Status:** RED - 多行中定位正确行号
  - **Verifies:** AC4

- ✅ **Test:** `TestScriptExecutor_MapLit_ConditionElse`
  - **Status:** RED - 映射+条件+else
  - **Verifies:** AC2

- ✅ **Test:** `TestScriptExecutor_ArrayLit_Overwrite`
  - **Status:** RED - 数组覆盖赋值
  - **Verifies:** AC1

- ✅ **Test:** `TestScriptExecutor_MapLit_Overwrite`
  - **Status:** RED - 映射覆盖赋值
  - **Verifies:** AC2

#### Performance Test (1 test)

- ✅ **Test:** `TestParseScript_Performance_NFR38`
  - **Status:** RED - 编译失败
  - **Verifies:** NFR38 解析 <= 50ms

#### Environment Tests (12 tests)

- ✅ **Test:** `TestEnvironment_SetArray_GetArray`
  - **Status:** RED - SetArray/GetArray undefined
  - **Verifies:** AC1 Environment 数组存储

- ✅ **Test:** `TestEnvironment_SetMap_GetMap`
  - **Status:** RED - SetMap/GetMap undefined
  - **Verifies:** AC2 Environment 映射存储

- ✅ **Test:** `TestEnvironment_Expand_ArrayIndex`
  - **Status:** RED - SetArray undefined
  - **Verifies:** AC1 ${VAR[N]} 展开

- ✅ **Test:** `TestEnvironment_Expand_MapField`
  - **Status:** RED - SetMap undefined
  - **Verifies:** AC2 ${VAR.KEY} 展开

- ✅ **Test:** `TestEnvironment_ExpandStrict_Defined`
  - **Status:** RED - ExpandStrict undefined
  - **Verifies:** AC4 ExpandStrict 已定义变量

- ✅ **Test:** `TestEnvironment_ExpandStrict_Undefined_Error`
  - **Status:** RED - ExpandStrict undefined
  - **Verifies:** AC4 ExpandStrict 未定义变量

- ✅ **Test:** `TestEnvironment_ExpandStrict_ArrayOutOfBounds_Error`
  - **Status:** RED - ExpandStrict undefined
  - **Verifies:** AC1 数组越界 strict 错误

- ✅ **Test:** `TestEnvironment_ExpandStrict_MapKeyMissing_Error`
  - **Status:** RED - ExpandStrict undefined
  - **Verifies:** AC2 映射缺键 strict 错误

- ✅ **Test:** `TestEnvironment_GetArray_NotExists`
  - **Status:** RED - GetArray undefined
  - **Verifies:** 不存在的数组返回 false

- ✅ **Test:** `TestEnvironment_GetMap_NotExists`
  - **Status:** RED - GetMap undefined
  - **Verifies:** 不存在的映射返回 false

- ✅ **Test:** `TestEnvironment_SetArray_SnapshotIsolation`
  - **Status:** RED - SetArray undefined
  - **Verifies:** 数组快照隔离

- ✅ **Test:** `TestEnvironment_SetMap_SnapshotIsolation`
  - **Status:** RED - SetMap undefined
  - **Verifies:** 映射快照隔离

---

## Data Factories Created

不适用 — Go 测试使用 `mockSpawner` 测试桩（来自 `pipe_test.go`），不需要额外的数据工厂。

---

## Fixtures Created

不适用 — Go 测试使用标准 `testing.T` 和内联 mock，不需要 Playwright 风格的 fixtures。

---

## Mock Requirements

### mockSpawner (已存在)

**File:** `shell/pipe_test.go`

复用现有的 `mockSpawner` 结构体，支持预定义 `results` 和记录 `calls`。

---

## Required New Types

### Statement Kinds

- `StmtArrayLit StatementKind = "array-lit"` — 数组字面量赋值
- `StmtMapLit StatementKind = "map-lit"` — 映射字面量赋值

### Data Types

- `ArrayLitStmt { Items []string }` — 数组字面量语句数据
- `MapLitStmt { Entries []MapEntry }` — 映射字面量语句数据
- `MapEntry { Key, Value string }` — 映射键值对

### Statement Extensions

- `Statement.ArrayLit *ArrayLitStmt` — 新字段
- `Statement.MapLit *MapLitStmt` — 新字段

### Environment Extensions

- `SetArray(key string, items []string)` — 存储数组
- `GetArray(key string) ([]string, bool)` — 获取数组
- `SetMap(key string, m map[string]string)` — 存储映射
- `GetMap(key string) (map[string]string, bool)` — 获取映射
- `ExpandStrict(input string) (string, error)` — 严格展开（未定义变量报错）

### Expand Extensions

- `${VAR[N]}` — 数组索引访问（花括号内含方括号）
- `${VAR.KEY}` — 映射属性访问（花括号内含点号）

---

## Implementation Checklist

### Test: TestEnvironment_SetArray_GetArray + TestEnvironment_GetArray_NotExists + TestEnvironment_SetArray_SnapshotIsolation

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `Environment` 结构体添加 `arrays map[string][]string` 字段
- [ ] 在 `NewEnvironment()` 初始化 `arrays` map
- [ ] 实现 `SetArray(key string, items []string)` — 复制切片存储
- [ ] 实现 `GetArray(key string) ([]string, bool)` — 返回副本
- [ ] Run test: `go test ./shell/ -run TestEnvironment_SetArray`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestEnvironment_SetMap_GetMap + TestEnvironment_GetMap_NotExists + TestEnvironment_SetMap_SnapshotIsolation

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `Environment` 结构体添加 `maps map[string]map[string]string` 字段
- [ ] 在 `NewEnvironment()` 初始化 `maps` map
- [ ] 实现 `SetMap(key string, m map[string]string)` — 复制 map 存储
- [ ] 实现 `GetMap(key string) (map[string]string, bool)` — 返回副本
- [ ] Run test: `go test ./shell/ -run TestEnvironment_SetMap`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestEnvironment_Expand_ArrayIndex + TestEnvironment_Expand_MapField

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 修改 `Expand` 方法：在 `${...}` 解析中检测 `VAR[N]` 模式
- [ ] 检测到 `[N]` 后缀时，查找 `arrays[VAR]` 并返回 `items[N]`
- [ ] 修改 `Expand` 方法：在 `${...}` 解析中检测 `VAR.KEY` 模式
- [ ] 检测到 `.KEY` 后缀时，查找 `maps[VAR]` 并返回 `m[KEY]`
- [ ] 确保未找到时保持现有行为（展开为空字符串）
- [ ] Run test: `go test ./shell/ -run "TestEnvironment_Expand_(ArrayIndex|MapField)"`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestEnvironment_ExpandStrict_* (4 tests)

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `ExpandStrict(input string) (string, error)`
- [ ] 与 `Expand` 类似的展开逻辑，但遇到未定义变量时返回 `error`
- [ ] 错误信息包含未定义的变量名
- [ ] 数组越界返回错误
- [ ] 映射缺键返回错误
- [ ] Run test: `go test ./shell/ -run TestEnvironment_ExpandStrict`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestParseScript_ArrayLit* (5 parse tests)

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 定义 `StmtArrayLit StatementKind = "array-lit"`
- [ ] 定义 `ArrayLitStmt { Items []string }` 结构体
- [ ] 在 `Statement` 添加 `ArrayLit *ArrayLitStmt` 字段
- [ ] 在 `parseStatement` 中添加数组字面量赋值检测: `VAR = [...]`
- [ ] 实现 `parseArrayLit(line string)` 函数解析方括号内元素
- [ ] 支持双引号元素和无引号元素
- [ ] 验证变量名不是保留关键字
- [ ] Run test: `go test ./shell/ -run "TestParseScript_ArrayLit"`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestParseScript_MapLit* (5 parse tests)

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 定义 `StmtMapLit StatementKind = "map-lit"`
- [ ] 定义 `MapLitStmt { Entries []MapEntry }` 和 `MapEntry { Key, Value string }` 结构体
- [ ] 在 `Statement` 添加 `MapLit *MapLitStmt` 字段
- [ ] 在 `parseStatement` 中添加映射字面量赋值检测: `VAR = {...}`
- [ ] 实现 `parseMapLit(line string)` 函数解析 `key: value` 对
- [ ] 支持带引号值和无引号值
- [ ] 验证变量名不是保留关键字
- [ ] Run test: `go test ./shell/ -run "TestParseScript_MapLit"`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestParseScript_Error_* (3 error tests)

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 未闭合方括号检测和错误报告
- [ ] 未闭合花括号检测和错误报告
- [ ] 映射缺少冒号分隔符检测
- [ ] 保留关键字变量名检测
- [ ] Run test: `go test ./shell/ -run "TestParseScript_Error_"`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestScriptExecutor_ArrayLit_* + TestScriptExecutor_MapLit_* (10 execution tests)

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `ScriptExecutor.executeBlock` 添加 `StmtArrayLit` case
- [ ] 解析数组字面量元素，调用 `env.SetArray()`
- [ ] 在 `ScriptExecutor.executeBlock` 添加 `StmtMapLit` case
- [ ] 解析映射条目，调用 `env.SetMap()`
- [ ] 确保 `Expand` 支持 `${files[0]}` 和 `${config.model}`
- [ ] 在 spawn 执行时使用 `ExpandStrict` 替代 `Expand`
- [ ] 确保错误包含行号信息（从 `Statement.Line` 获取）
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_(Array|Map)"`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 2 hours

---

### Test: TestScriptExecutor_UndefinedVar_* (2 tests)

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 在 spawn 执行路径中使用 `ExpandStrict` 替代 `Expand`
- [ ] 捕获 `ExpandStrict` 返回的错误，包装为 `line N: undefined variable "X"`
- [ ] 确保行号从 `Statement.Line` 正确传递
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_UndefinedVar"`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestScriptExecutor_*Combined + CR tests (8 combination tests)

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 确保数组+映射可以在同一脚本中使用
- [ ] 确保 `for f in $files` 支持数组变量迭代
- [ ] 确保映射属性可作为函数参数
- [ ] 确保数组/映射覆盖赋值正确工作
- [ ] 确保映射属性在条件判断中正确展开
- [ ] Run test: `go test ./shell/ -run "TestScriptExecutor_(Array|Map|Combined)"`
- [ ] ✅ Tests pass (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestParseScript_Performance_NFR38

**File:** `shell/data_test.go`

**Tasks to make these tests pass:**

- [ ] 确保解析器扩展不引入性能退化
- [ ] 新的数组/映射解析使用高效的字符串操作
- [ ] 测试包含 20+ spawn + for + fn + 数组/映射的复杂脚本
- [ ] Run test: `go test ./shell/ -run TestParseScript_Performance_NFR38`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

## Running Tests

```bash
# Run all failing tests for this story
go test ./shell/ -run "18.3|ArrayLit|MapLit|ExpandStrict|DataLit" -v

# Run specific test file (all tests in data_test.go)
go test ./shell/ -v

# Run only parsing tests
go test ./shell/ -run "TestParseScript_(Array|Map)" -v

# Run only execution tests
go test ./shell/ -run "TestScriptExecutor_(Array|Map|UndefinedVar|Combined)" -v

# Run only environment tests
go test ./shell/ -run "TestEnvironment_(SetArray|GetArray|SetMap|GetMap|Expand)" -v

# Run performance test
go test ./shell/ -run TestParseScript_Performance_NFR38 -v

# Debug specific test
go test ./shell/ -run TestScriptExecutor_ArrayLit_IndexAccess -v -count=1

# Run tests with coverage
go test ./shell/ -coverprofile=coverage.out && go tool cover -html=coverage.out
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 35 tests written and failing (compile errors)
- ✅ mockSpawner reused from existing test infrastructure
- ✅ New type requirements documented
- ✅ Implementation checklist created

**Verification:**

- All tests fail to compile due to undefined types
- Failure is due to missing implementation, not test bugs
- Error output confirms: `StmtArrayLit undefined`, `ArrayLit undefined`, `SetArray undefined`, etc.

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with Environment layer** (env.go):
   - Add `arrays` and `maps` fields to `Environment`
   - Implement `SetArray`, `GetArray`, `SetMap`, `GetMap`
   - Extend `Expand` for `${VAR[N]}` and `${VAR.KEY}` syntax
   - Implement `ExpandStrict`

2. **Then Parser layer** (script.go):
   - Add `StmtArrayLit`, `StmtMapLit` statement kinds
   - Define `ArrayLitStmt`, `MapLitStmt`, `MapEntry` types
   - Add `ArrayLit`, `MapLit` fields to `Statement`
   - Implement `parseArrayLit()` and `parseMapLit()` functions
   - Hook into `parseStatement()` for `VAR = [...]` and `VAR = {...}` detection

3. **Then Executor layer** (script.go):
   - Add `StmtArrayLit` and `StmtMapLit` cases to `executeBlock`
   - Switch spawn intent expansion from `Expand` to `ExpandStrict`
   - Wrap errors with line number from `Statement.Line`

4. **Run tests after each layer** to verify progress

**Key Principles:**

- One test at a time (don't try to fix all at once)
- Minimal implementation (don't over-engineer)
- Run tests frequently (immediate feedback)
- Use implementation checklist as roadmap

**Suggested Order:**

```
env.go: SetArray/GetArray → data_test.go:TestEnvironment_SetArray_* (3 tests)
env.go: SetMap/GetMap     → data_test.go:TestEnvironment_SetMap_* (3 tests)
env.go: Expand array/map  → data_test.go:TestEnvironment_Expand_* (2 tests)
env.go: ExpandStrict       → data_test.go:TestEnvironment_ExpandStrict_* (4 tests)
script.go: parse array     → data_test.go:TestParseScript_ArrayLit* (5 tests)
script.go: parse map       → data_test.go:TestParseScript_MapLit* (5 tests)
script.go: parse errors    → data_test.go:TestParseScript_Error_* (3 tests)
script.go: execute array   → data_test.go:TestScriptExecutor_ArrayLit_* (4 tests)
script.go: execute map     → data_test.go:TestScriptExecutor_MapLit_* (4 tests)
script.go: strict expand   → data_test.go:TestScriptExecutor_UndefinedVar_* (2 tests)
integration: combinations  → data_test.go:TestScriptExecutor_*Combined/CR_* (8 tests)
performance: NFR38          → data_test.go:TestParseScript_Performance_NFR38 (1 test)
```

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

**DEV Agent Responsibilities:**

1. **Verify all 35 tests pass** (green phase complete)
2. **Review code for quality**: DRY between `Expand` and `ExpandStrict`, clean parser structure
3. **Extract duplications**: array/map parsing helper functions
4. **Optimize performance**: ensure parse performance stays under 50ms (NFR38)
5. **Ensure existing tests still pass** (regression): `go test ./shell/...`
6. **Update `countExecutableStages`** if needed for new statement kinds

---

## Next Steps

1. **Share this checklist and failing tests** with the dev workflow (manual handoff)
2. **Review this checklist** with team
3. **Run failing tests** to confirm RED phase: `go test ./shell/ 2>&1 | head -20`
4. **Begin implementation** following the suggested order above
5. **Work one test group at a time** (red → green for each)
6. **When all tests pass**, refactor code for quality
7. **When refactoring complete**, manually update story status to 'done' in sprint-status.yaml

---

## Knowledge Base References Applied

This ATDD workflow consulted the following project artifacts:

- **epic-18-agentshell完整脚本语言** - Story 18.3 acceptance criteria and NFR38
- **shell/script.go** - 现有 ParseScript, ScriptExecutor, Statement 类型
- **shell/env.go** - 现有 Environment, Expand, Set/Get/Delete/All 方法
- **shell/parser.go** - 现有 ParsePipeline, tokenize, Command 类型
- **shell/pipe.go** - KernelSpawner interface, PipelineExecutor
- **shell/script_test.go** - 现有 Story 11.2, 11.3, 18.1, 18.2 测试模式
- **shell/env_test.go** - 现有 Environment 单元测试模式
- **shell/pipe_test.go** - mockSpawner 测试桩定义

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./shell/ 2>&1 | head -20`

**Results:**

```
# github.com/rnixai/rnix/shell [github.com/rnixai/rnix/shell.test]
shell/data_test.go:36:18: undefined: StmtArrayLit
shell/data_test.go:37:45: undefined: StmtArrayLit
shell/data_test.go:42:10: stmt.ArrayLit undefined (type Statement has no field or method ArrayLit)
shell/data_test.go:45:14: stmt.ArrayLit undefined (type Statement has no field or method ArrayLit)
shell/data_test.go:46:47: stmt.ArrayLit undefined (type Statement has no field or method ArrayLit)
shell/data_test.go:50:11: stmt.ArrayLit undefined (type Statement has no field or method ArrayLit)
shell/data_test.go:51:47: stmt.ArrayLit undefined (type Statement has no field or method ArrayLit)
shell/data_test.go:69:18: undefined: StmtArrayLit
shell/data_test.go:70:45: undefined: StmtArrayLit
shell/data_test.go:72:10: stmt.ArrayLit undefined (type Statement has no field or method ArrayLit)
shell/data_test.go:72:10: too many errors
FAIL    github.com/rnixai/rnix/shell [build failed]
```

**Summary:**

- Total tests: 35
- Passing: 0 (expected)
- Failing: 35 (compile errors, expected)
- Status: ✅ RED phase verified

**Expected Failure Messages:**

- `undefined: StmtArrayLit` — 数组字面量语句类型未定义
- `undefined: StmtMapLit` — 映射字面量语句类型未定义
- `Statement has no field or method ArrayLit` — Statement 缺少 ArrayLit 字段
- `Statement has no field or method MapLit` — Statement 缺少 MapLit 字段
- `env.SetArray undefined` — Environment 缺少 SetArray 方法
- `env.GetArray undefined` — Environment 缺少 GetArray 方法
- `env.SetMap undefined` — Environment 缺少 SetMap 方法
- `env.GetMap undefined` — Environment 缺少 GetMap 方法
- `env.ExpandStrict undefined` — Environment 缺少 ExpandStrict 方法

---

## Notes

- Go 编译器在同一包内发现未定义类型时，整个包都无法编译。因此 RED phase 表现为编译失败而非测试运行失败。这是 Go TDD 的标准行为。
- Story 18.3 的字符串插值 (AC3) 部分功能已由 `env.Expand` 支持（`$VAR` 和 `${VAR}` 语法）。新增需求是：数组索引 `${VAR[N]}`、映射属性 `${VAR.KEY}` 和未定义变量严格模式。
- `for f in $files` 语法需要扩展现有 `parseForBlock` 以支持数组变量引用（当前只支持字面量列表 `[a, b, c]` 和空格分隔 `a b c`）。
- `ExpandStrict` 的引入是一个行为变更：spawn intent 中引用未定义变量将从静默展开为空字符串变为报错。需要确保只在 spawn intent 中使用 strict 模式，export 等场景保持现有行为。

---

**Generated by BMad TEA Agent** - 2026-03-09
