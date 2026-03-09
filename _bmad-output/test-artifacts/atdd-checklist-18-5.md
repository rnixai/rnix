---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-test-generation']
lastStep: 'step-02-test-generation'
lastSaved: '2026-03-09'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/18-5-modularization-and-script-execution.md'
  - 'shell/script.go'
  - 'shell/script_test.go'
  - 'shell/parallel_test.go'
  - 'shell/pipe_test.go'
  - 'cmd/rnix/main_test.go'
  - 'ipc/protocol.go'
---

# ATDD Checklist - Epic 18, Story 5: 模块化与脚本执行

**Date:** 2026-03-09
**Author:** Decker
**Primary Test Level:** Unit (Go standard testing)

---

## Story Summary

Story 18.5 实现 AgentShell 脚本的模块化组织和文件执行能力。

**As a** 应用开发者
**I want** 通过 `source` 导入其他脚本并通过 `rnix run` 执行脚本文件
**So that** 我可以模块化组织脚本并直接运行。

---

## Acceptance Criteria

1. `source ./lib/helpers.ash` 导入的函数和变量在当前脚本中可用
2. `rnix run deploy.ash` 按顺序执行脚本，实时输出进度
3. Shebang `#!/usr/bin/env rnix run` 支持直接执行
4. 语法错误报告包含脚本名、行号和具体问题
5. `source` 目标文件不存在时报告文件不存在信息
6. 循环引用检测（A source B, B source A → 报错）
7. sourced 脚本的函数可调用，变量可在 `${var}` 中引用
8. `rnix run deploy.ash --env staging` 参数通过环境变量传递
9. 脚本解析时间 <= 50ms（NFR38，≤ 1000 行含 source 展开）

---

## Failing Tests Created (RED Phase)

### Unit Tests — shell/source_test.go (29 tests)

**File:** `shell/source_test.go` (约 530 行)

**解析测试 (9 tests):**

- ✅ **Test:** `TestParseScript_Source_Basic`
  - **Status:** RED — `StmtSource` undefined
  - **Verifies:** AC1 — source 基本解析

- ✅ **Test:** `TestParseScript_Source_QuotedPath`
  - **Status:** RED — `StmtSource` undefined
  - **Verifies:** AC1 — 带双引号路径解析

- ✅ **Test:** `TestParseScript_Source_SingleQuotedPath`
  - **Status:** RED — `StmtSource` undefined
  - **Verifies:** AC1 — 带单引号路径解析

- ✅ **Test:** `TestParseScript_Source_NoPath`
  - **Status:** RED — parse error expected
  - **Verifies:** AC4 — 无参数报错

- ✅ **Test:** `TestParseScript_Source_InFunction`
  - **Status:** RED — `StmtSource` undefined
  - **Verifies:** 函数体内 source

- ✅ **Test:** `TestParseScript_Source_Shebang`
  - **Status:** RED — `stripShebang` undefined
  - **Verifies:** AC3 — shebang 行跳过

- ✅ **Test:** `TestParseScript_Source_CaseInsensitive`
  - **Status:** RED — `StmtSource` undefined
  - **Verifies:** 大小写不敏感

- ✅ **Test:** `TestParseScript_Source_LineNumber`
  - **Status:** RED — `StmtSource` undefined
  - **Verifies:** 行号记录

- ✅ **Test:** `TestParseScript_Source_Performance_NFR38`
  - **Status:** RED — compile failure
  - **Verifies:** AC9 — NFR38 性能要求

**stripShebang 测试 (4 tests):**

- ✅ **Test:** `TestStripShebang_Present`
  - **Status:** RED — `stripShebang` undefined
  - **Verifies:** AC3 — shebang 去除

- ✅ **Test:** `TestStripShebang_Absent`
  - **Status:** RED — `stripShebang` undefined
  - **Verifies:** 无 shebang 时原样返回

- ✅ **Test:** `TestStripShebang_OnlyShebang`
  - **Status:** RED — `stripShebang` undefined
  - **Verifies:** 仅含 shebang 返回空

- ✅ **Test:** `TestStripShebang_EmptyInput`
  - **Status:** RED — `stripShebang` undefined
  - **Verifies:** 空输入返回空

**执行测试 (14 tests):**

- ✅ **Test:** `TestScriptExecutor_Source_FunctionsAvailable`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC1, AC7 — source 后函数可调用

- ✅ **Test:** `TestScriptExecutor_Source_VariablesAvailable`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC7 — source 后变量可引用

- ✅ **Test:** `TestScriptExecutor_Source_FileNotFound`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC5 — 文件不存在报错含行号

- ✅ **Test:** `TestScriptExecutor_Source_CircularDetection`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC6 — A→B→A 循环引用

- ✅ **Test:** `TestScriptExecutor_Source_SelfReference`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC6 — A→A 自引用

- ✅ **Test:** `TestScriptExecutor_Source_RelativePath`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC1 — 相对路径基于 scriptDir

- ✅ **Test:** `TestScriptExecutor_Source_VariableInPath`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** 路径变量展开

- ✅ **Test:** `TestScriptExecutor_Source_ParseError`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC4 — sourced 文件语法错误

- ✅ **Test:** `TestScriptExecutor_Source_WithSpawn`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** sourced 脚本含 spawn

- ✅ **Test:** `TestScriptExecutor_Source_ChainedSource`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** A→B→C 链式 source

- ✅ **Test:** `TestScriptExecutor_Source_OverrideFunction`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** 后 source 覆盖同名函数

- ✅ **Test:** `TestScriptExecutor_Source_EmptyFile`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** 空文件 no-op

- ✅ **Test:** `TestScriptExecutor_Source_FileWithShebang`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** AC3 — sourced 文件 shebang 跳过

- ✅ **Test:** `TestScriptExecutor_Source_AbsolutePath`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** 绝对路径直接使用

**组合测试 (6 tests):**

- ✅ **Test:** `TestScriptExecutor_Source_InForLoop`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** for 循环内 source

- ✅ **Test:** `TestScriptExecutor_Source_InIfBlock`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** if 块内条件 source

- ✅ **Test:** `TestScriptExecutor_Source_BeforeParallel`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** source 函数后 parallel 使用

- ✅ **Test:** `TestScriptExecutor_Source_WithDataStructures`
  - **Status:** RED — `NewScriptExecutorWithReader` undefined
  - **Verifies:** source 后数组/映射使用

- ✅ **Test:** `TestCountStages_SourceZero`
  - **Status:** RED — compile failure
  - **Verifies:** countStagesInBlock source = 0

- ✅ **Test:** `TestValidateFnCalls_SourceSkipped`
  - **Status:** RED — compile failure
  - **Verifies:** validateFnCalls 跳过 source

### CLI Tests — cmd/rnix/run_test.go (5 tests)

**File:** `cmd/rnix/run_test.go` (约 90 行)

- ✅ **Test:** `TestRunCmd_Registered`
  - **Status:** RED — `runCmd` undefined
  - **Verifies:** AC2 — run 子命令注册

- ✅ **Test:** `TestRunCmd_NoArgs`
  - **Status:** RED — `runCmd` undefined
  - **Verifies:** AC2 — 无参数报错

- ✅ **Test:** `TestRunCmd_FileNotFound`
  - **Status:** RED — `runRunCmd` undefined
  - **Verifies:** AC4 — 文件不存在报错

- ✅ **Test:** `TestRunCmd_UsageAndDescription`
  - **Status:** RED — `runCmd` undefined
  - **Verifies:** 命令描述

- ✅ **Test:** `TestRunCmd_SupportsJSONFlag`
  - **Status:** RED — `runCmd` undefined
  - **Verifies:** JSON flag 继承

---

## Data Factories Created

不适用 — Go 项目使用内联测试数据和 mock 实现。

### mockFileReader

**File:** `shell/source_test.go`（内联定义）

**Exports:**
- `mockFileReader{files: map[string]string{...}}` — 内存文件系统 mock
- 满足 `FileReader` 接口

**Example Usage:**

```go
reader := &mockFileReader{
    files: map[string]string{
        "/project/lib/helpers.ash": "fn greet()\n  spawn \"hello\"\nend",
    },
}
```

---

## Fixtures Created

不适用 — Go 项目使用 `mockSpawner`（来自 `pipe_test.go`）和 `concurrentMockSpawner`（来自 `parallel_test.go`），无需额外 fixture。

---

## Mock Requirements

### FileReader Mock

**Interface:** `FileReader`（`shell/file_reader.go` 中定义，尚未实现）

```go
type FileReader interface {
    ReadFile(path string) (string, error)
}
```

**测试实现:** `mockFileReader`（`shell/source_test.go` 中定义）

- 成功响应: 返回 `files[path]` 内容
- 失败响应: 返回 `&os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}`

**Notes:** 所有 source 执行测试通过 `NewScriptExecutorWithReader` 注入 mock

---

## Required data-testid Attributes

不适用 — 纯 Go 后端项目，无 UI 组件。

---

## Implementation Checklist

### Test: ParseScript source 基本解析 (9 parsing tests)

**File:** `shell/source_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `shell/script.go` 新增 `StmtSource StatementKind = "source"`
- [ ] 新增 `SourceStmt` 结构体 `type SourceStmt struct { Path string }`
- [ ] 扩展 `Statement` 结构体新增 `Source *SourceStmt` 字段
- [ ] 在 `parseBlock` 中新增 `source` 关键字检测（在 builtin 之前）
- [ ] 实现 `parseSourceStatement(line string, lineIdx int) (Statement, error)`
- [ ] 在 `ParseScript` 入口调用 `stripShebang`
- [ ] Run: `go test ./shell/ -run TestParseScript_Source`
- [ ] ✅ All 9 parsing tests pass (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: stripShebang 基本功能 (4 tests)

**File:** `shell/source_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `stripShebang(content string) string` 函数
- [ ] Shebang 检测: `strings.HasPrefix(content, "#!")`
- [ ] 找到第一个 `\n` 位置，返回其后内容
- [ ] 只有 shebang 行（无 `\n`）时返回空字符串
- [ ] Run: `go test ./shell/ -run TestStripShebang`
- [ ] ✅ All 4 stripShebang tests pass (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: ScriptExecutor source 执行 (14 execution tests)

**File:** `shell/source_test.go`

**Tasks to make these tests pass:**

- [ ] 新增 `shell/file_reader.go` — `FileReader` 接口 + `OSFileReader` 实现
- [ ] 在 `ScriptExecutor` 新增字段: `fileReader FileReader`, `sourceStack map[string]bool`, `scriptDir string`
- [ ] 实现 `NewScriptExecutorWithReader(spawner, env, reader)` 构造函数
- [ ] 实现 `SetScriptDir(dir string)` 方法
- [ ] 在 `executeBlock` 中新增 `case StmtSource` 分支:
  - [ ] `env.ExpandStrict(stmt.Source.Path)` 展开路径变量
  - [ ] 相对路径: 基于 `e.scriptDir` 解析（`filepath.Abs`）
  - [ ] 循环引用检测: 绝对路径在 `sourceStack` 中检查
  - [ ] `fileReader.ReadFile(absPath)` 读取文件
  - [ ] `stripShebang(content)` 去除 shebang
  - [ ] `ParseScript(content)` 解析 sourced 脚本
  - [ ] 注册 sourced 函数到 `e.functions`
  - [ ] `sourceStack[absPath] = true` + 保存/恢复 `scriptDir`
  - [ ] `executeBlock(ctx, script.Statements, ...)` 执行
  - [ ] 执行完毕恢复 `scriptDir`，移除 `sourceStack` 条目
- [ ] `NewScriptExecutor` 保持向后兼容（使用 `&OSFileReader{}`）
- [ ] Run: `go test ./shell/ -run TestScriptExecutor_Source`
- [ ] ✅ All 14 execution tests pass (green phase)

**Estimated Effort:** 3 hours

---

### Test: 组合测试 (6 tests)

**File:** `shell/source_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `validateFnCalls` 新增 `StmtSource` 分支 — skip（运行时函数注册）
- [ ] 在 `countStagesInBlock` 新增 `StmtSource` 分支 — return 0
- [ ] 验证 for/if/parallel 与 source 组合执行
- [ ] Run: `go test ./shell/ -run "TestScriptExecutor_Source_(InForLoop|InIfBlock|BeforeParallel|WithDataStructures)|TestCountStages_SourceZero|TestValidateFnCalls_SourceSkipped"`
- [ ] ✅ All 6 combination tests pass (green phase)

**Estimated Effort:** 1 hour

---

### Test: rnix run CLI (5 tests)

**File:** `cmd/rnix/run_test.go`

**Tasks to make these tests pass:**

- [ ] 新增 `cmd/rnix/run.go`:
  - [ ] `var runCmd = &cobra.Command{Use: "run <script.ash>", ...}`
  - [ ] `func runRunCmd(cmd *cobra.Command, args []string) error`
  - [ ] `func init() { rootCmd.AddCommand(runCmd) }`
- [ ] `runRunCmd` 实现:
  - [ ] `os.ReadFile(args[0])` 读取脚本
  - [ ] `stripShebang(content)` 去除 shebang
  - [ ] 参数传递: `RNIX_ARG_0` ~ `RNIX_ARG_N`, `RNIX_ARGS`
  - [ ] 注入 `RNIX_SCRIPT_FILE`, `RNIX_SCRIPT_DIR`
  - [ ] 复用 `runScript(renderer, mode, progress, client, content, start)` 流程
- [ ] Run: `go test ./cmd/rnix/ -run TestRunCmd`
- [ ] ✅ All 5 CLI tests pass (green phase)

**Estimated Effort:** 2 hours

---

### IPC 扩展 (需额外测试)

**File:** `ipc/protocol.go`, `ipc/server.go`

**Tasks:**

- [ ] `ExecScriptRequest` 新增 `ScriptDir string` 字段 (`json:"script_dir,omitempty"`)
- [ ] `handleExecScript` 中传递 `ScriptDir` 到 `ScriptExecutor`
- [ ] `ScriptExecutor` 新增 `SetFileReader(r FileReader)` 方法
- [ ] 验证现有 IPC 测试不受影响

**Estimated Effort:** 0.5 hours

---

## Running Tests

```bash
# Run all failing tests for this story (shell package)
go test ./shell/ -run "TestParseScript_Source|TestStripShebang|TestScriptExecutor_Source|TestCountStages_Source|TestValidateFnCalls_Source" -v

# Run all failing tests for this story (cmd package)
go test ./cmd/rnix/ -run "TestRunCmd" -v

# Run specific test file
go test ./shell/ -run TestParseScript_Source_Basic -v

# Run tests with race detector
go test -race ./shell/... ./cmd/rnix/...

# Run all story tests (both packages)
go test ./shell/... ./cmd/rnix/... -run "18.5|Source|StripShebang|RunCmd" -v

# Run tests with coverage
go test -coverprofile=coverage.out ./shell/... ./cmd/rnix/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 34 tests written and failing (compile errors)
- ✅ mockFileReader created with FileReader interface assertion
- ✅ Mock requirements documented
- ✅ Implementation checklist created

**Verification:**

- `shell/source_test.go` — 29 tests, compile failure: `StmtSource`, `FileReader`, `Statement.Source`, `NewScriptExecutorWithReader`, `stripShebang` undefined
- `cmd/rnix/run_test.go` — 5 tests, compile failure: `runCmd`, `runRunCmd` undefined
- Failures are due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Pick one failing test group** from implementation checklist (start with stripShebang → parsing → execution → CLI)
2. **Read the tests** to understand expected behavior
3. **Implement minimal code** to make tests pass
4. **Run tests** to verify green
5. **Check off tasks** in implementation checklist
6. **Move to next group** and repeat

**Recommended Order:**
1. `stripShebang` (4 tests) — 独立函数，无依赖
2. Parsing tests (9 tests) — 需要 `StmtSource`, `SourceStmt`, `Statement.Source`
3. Execution tests (14 tests) — 需要 `FileReader`, `NewScriptExecutorWithReader`, `executeBlock` source 分支
4. Combination tests (6 tests) — 需要 `validateFnCalls` 和 `countStagesInBlock` 扩展
5. CLI tests (5 tests) — 需要 `cmd/rnix/run.go`
6. IPC extension — 需要 `ExecScriptRequest.ScriptDir`

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

**DEV Agent Responsibilities:**

1. **Verify all tests pass** (green phase complete)
2. **Run race detector:** `go test -race ./shell/... ./cmd/rnix/...`
3. **Review code for quality** (no dead code, consistent patterns)
4. **Ensure** `OSFileReader` 在独立文件 `shell/file_reader.go` 中（不在 `script.go`）
5. **Ensure** 错误消息包含文件路径和行号
6. **Run full test suite:** `go test ./...` 确认无回归

---

## Next Steps

1. **Review this checklist** — 确认测试覆盖所有 AC
2. **Run failing tests** to confirm RED phase: `go test ./shell/ ./cmd/rnix/ 2>&1 | head -20`
3. **Begin implementation** using implementation checklist as guide (recommended order above)
4. **Work one test group at a time** (red → green for each)
5. **When all tests pass**, run `go test -race ./shell/... ./cmd/rnix/...`
6. **When refactoring complete**, update story status to 'done' in sprint-status.yaml

---

## Knowledge Base References Applied

此 ATDD 工作流基于以下项目知识：

- **shell/script_test.go** — 现有测试模式：`TestParseScript_*` / `TestScriptExecutor_*` 命名、mockSpawner 使用
- **shell/parallel_test.go** — concurrentMockSpawner 线程安全 mock 模式
- **shell/pipe_test.go** — mockSpawner / mockResult / mockCall 基础类型定义
- **Story 18.5 Implementation Artifact** — 完整的任务分解、测试清单、代码模式引用
- **shell/script.go** — 现有 AST 类型、StatementKind、parseBlock 关键字检测顺序
- **ipc/protocol.go** — ExecScriptRequest 结构体

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./shell/ ./cmd/rnix/ 2>&1`

**Results:**

```
# github.com/rnixai/rnix/shell [github.com/rnixai/rnix/shell.test]
shell/source_test.go:37:7: undefined: FileReader
shell/source_test.go:54:18: undefined: StmtSource
shell/source_test.go:55:45: undefined: StmtSource
shell/source_test.go:57:10: stmt.Source undefined (type Statement has no field or method Source)
...
FAIL	github.com/rnixai/rnix/shell [build failed]

# github.com/rnixai/rnix/cmd/rnix [github.com/rnixai/rnix/cmd/rnix.test]
cmd/rnix/run_test.go:38:5: undefined: runCmd
cmd/rnix/run_test.go:41:12: undefined: runCmd
cmd/rnix/run_test.go:56:9: undefined: runRunCmd
...
FAIL	github.com/rnixai/rnix/cmd/rnix [build failed]
```

**Summary:**

- Total tests: 34
- Passing: 0 (expected)
- Failing: 34 (expected — compile errors)
- Status: ✅ RED phase verified

**Expected Failure Messages:**
- `undefined: FileReader` — FileReader 接口尚未定义
- `undefined: StmtSource` — StatementKind 常量尚未定义
- `stmt.Source undefined` — Statement 结构体缺少 Source 字段
- `undefined: NewScriptExecutorWithReader` — 构造函数尚未实现
- `undefined: stripShebang` — 函数尚未实现
- `undefined: runCmd` — cobra.Command 尚未定义
- `undefined: runRunCmd` — 命令处理函数尚未实现

---

## Notes

- **Go 项目适配**: 本 ATDD 工作流针对 Go 后端项目，使用 Go 标准 `testing` 包而非 Playwright/Cypress
- **RED phase = 编译失败**: Go 的 RED phase 表现为编译错误（引用未定义的类型和函数），而非运行时断言失败
- **mockFileReader 设计**: 使用内存 map 模拟文件系统，满足 `FileReader` 接口，避免测试依赖真实文件系统
- **向后兼容**: `NewScriptExecutor` 保持不变（使用 `OSFileReader`），新增 `NewScriptExecutorWithReader` 用于测试注入
- **循环引用检测**: 使用绝对路径（`filepath.Abs`）作为 key，避免 `./a.ash` 和 `a.ash` 被视为不同文件

---

## Contact

**Questions or Issues?**

- Tag @Decker
- Refer to `_bmad-output/implementation-artifacts/18-5-modularization-and-script-execution.md` for full story specification

---

**Generated by BMad TEA Agent** - 2026-03-09
