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
  - _bmad-output/implementation-artifacts/11-2-variables-and-environment-passing.md
  - shell/parser.go
  - shell/pipe.go
  - shell/parser_test.go
  - shell/pipe_test.go
  - ipc/protocol.go
  - ipc/server.go
  - ipc/client.go
  - cmd/rnix/main.go
  - cmd/rnix/main_test.go
---

# ATDD 检查清单 - Epic 11, Story 2: 变量与环境传递

**日期:** 2026-03-03
**作者:** Decker
**主要测试级别:** Unit (Go `testing` 包)

---

## Story 概要

在 AgentShell 中实现变量定义和环境传递机制，支持 `export` 命令设置变量、`$VAR`/`${VAR}` 引用语法、以及变量值在 spawn/pipeline intent 中的自动展开。

**As a** 用户
**I want** 在 AgentShell 中定义变量和传递环境给智能体
**So that** 智能体可以引用动态参数

---

## 验收标准

1. **AC1: export 命令设置变量** — 执行 `export TARGET=./src/auth.go` 后变量 `TARGET` 存储在 shell 环境中
2. **AC2: 变量替换注入 intent** — Spawn 的智能体 intent 中引用 `$TARGET` 时变量值被替换后注入
3. **AC3: 标准变量引用语法** — 支持 `$VAR` 和 `${VAR}` 引用语法

---

## 失败测试已创建 (RED Phase)

### Unit Tests — Environment (22 tests)

**文件:** `shell/env_test.go` (~230 行)

- ✅ **Test:** TestEnvironment_SetGet
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC1 — 基本 Set/Get 操作
- ✅ **Test:** TestEnvironment_SetOverwrite
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** Set 覆盖已有变量
- ✅ **Test:** TestEnvironment_SetEmptyValue
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** 空值 export 合法
- ✅ **Test:** TestEnvironment_Delete
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** Delete 移除变量
- ✅ **Test:** TestEnvironment_DeleteNonExistent
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** 删除不存在的 key 不 panic
- ✅ **Test:** TestExpand_SingleVar
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC2, AC3 — `$VAR` 单变量展开
- ✅ **Test:** TestExpand_MultipleVars
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC3 — 多变量展开
- ✅ **Test:** TestExpand_AdjacentVars
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC3 — 相邻变量 `$X$Y`
- ✅ **Test:** TestExpand_BracesSyntax
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC3 — `${VAR}` 花括号语法
- ✅ **Test:** TestExpand_BracesSuffix
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC3 — `${VAR}/suffix`
- ✅ **Test:** TestExpand_BracesInMiddle
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC3 — `prefix${VAR}suffix`
- ✅ **Test:** TestExpand_EscapedDollar
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC3 — `\$` 转义
- ✅ **Test:** TestExpand_EscapedDollarBeforeVar
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** AC3 — `\$X is $X`
- ✅ **Test:** TestExpand_UndefinedVar
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** 未定义变量展开为空字符串
- ✅ **Test:** TestExpand_UndefinedBracesVar
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** `${MISSING}` 展开为空
- ✅ **Test:** TestExpand_DollarAtEnd
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** `$` 在末尾保持原样
- ✅ **Test:** TestExpand_DollarFollowedByNonVar
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** `$100` 中 `$` 保持原样
- ✅ **Test:** TestExpand_UnclosedBraces
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** `${UNCLOSED` 保持原样
- ✅ **Test:** TestExpand_NoVariables / TestExpand_EmptyInput
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** 无变量文本和空输入
- ✅ **Test:** TestNewEnvironmentFromOS_ContainsPath / ContainsHome
  - **状态:** RED — `undefined: NewEnvironmentFromOS`
  - **验证:** 从 OS 环境初始化
- ✅ **Test:** TestEnvironment_All_Snapshot
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** All() 返回快照副本
- ✅ **Test:** TestExpand_CaseSensitive / TestExpand_CircularReference
  - **状态:** RED — `undefined: NewEnvironment`
  - **验证:** 大小写敏感、循环引用边界

### Unit Tests — Script Parser & Executor (20 tests)

**文件:** `shell/script_test.go` (~290 行)

- ✅ **Test:** TestParseScript_SingleExport
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** AC1 — 单行 export 解析
- ✅ **Test:** TestParseScript_ExportEmptyValue
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** `export KEY=` 空值
- ✅ **Test:** TestParseScript_ExportValueContainsEquals
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** `export CONFIG=a=b=c`
- ✅ **Test:** TestParseScript_ExportCaseInsensitive
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** export/Export/EXPORT 均可识别
- ✅ **Test:** TestParseScript_ExportDoubleQuotedValue
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** `export KEY="value with spaces"`
- ✅ **Test:** TestParseScript_ExportSingleQuotedValue
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** `export KEY='value with spaces'`
- ✅ **Test:** TestParseScript_MultiLine_ExportAndSpawn
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** AC1, AC2 — 多行 export + spawn
- ✅ **Test:** TestParseScript_MultiLine_ExportAndPipeline
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** AC1, AC2 — 多行 export + pipeline
- ✅ **Test:** TestParseScript_SkipEmptyAndComments
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** 跳过空行和 `#` 注释
- ✅ **Test:** TestParseScript_InvalidExport_NoEquals / NoKey / SpacesAroundEquals
  - **状态:** RED — `undefined: ParseScript`
  - **验证:** 无效 export 格式返回错误
- ✅ **Test:** TestScriptExecutor_ExportThenSpawn
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** AC1, AC2 — export 后 spawn 变量展开
- ✅ **Test:** TestScriptExecutor_ExportValueExpansion
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** export 值中引用其他变量
- ✅ **Test:** TestScriptExecutor_PipelineVarExpansion
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** AC2 — pipeline 命令变量展开
- ✅ **Test:** TestScriptExecutor_ExportOverwrite
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** export 覆盖同名变量
- ✅ **Test:** TestScriptExecutor_NonZeroExitBreaks
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** 非零 ExitCode 中断脚本
- ✅ **Test:** TestScriptExecutor_ContextCancelled
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** context 取消中断执行
- ✅ **Test:** TestScriptExecutor_SpawnWithAgentAndModel
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** --agent/--model 参数传递
- ✅ **Test:** TestScriptExecutor_RecordsElapsed
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** ScriptResult.Elapsed 记录
- ✅ **Test:** TestScriptExecutor_OnStageStartCallback
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** OnStageStart 回调触发
- ✅ **Test:** TestScriptExecutor_ExportOnly
  - **状态:** RED — `undefined: NewScriptExecutor`
  - **验证:** 纯 export 脚本（无 spawn）

### Regression Tests — CLI (4 tests)

**文件:** `cmd/rnix/main_test.go` (追加 ~80 行)

- ✅ **Test:** TestIsScriptSyntax_Positive
  - **状态:** RED — `undefined: isScriptSyntax`
  - **验证:** 多行脚本、单行 export 检测
- ✅ **Test:** TestIsScriptSyntax_Negative
  - **状态:** RED — `undefined: isScriptSyntax`
  - **验证:** 单 spawn、pipeline 不误判
- ✅ **Test:** TestScriptDetection_PriorityOverPipeline
  - **状态:** RED — `undefined: isScriptSyntax`
  - **验证:** 脚本检测优先级高于管道
- ✅ **Test:** TestExistingPaths_Unchanged
  - **状态:** RED — `undefined: isScriptSyntax`
  - **验证:** 现有单 spawn/管道路径不受影响

---

## 数据工厂

**不适用** — Go 后端项目使用结构体字面量和 `mockSpawner`（复用 Story 11.1 的 mock 模式）。

### mockSpawner（复用 pipe_test.go）

**文件:** `shell/pipe_test.go`（已存在）

**导出:**
- `mockSpawner` — 模拟 `KernelSpawner` 接口，记录调用并返回预设结果

---

## Fixtures

**不适用** — Go 后端项目使用标准 `testing` 包，无需 Playwright fixtures。

---

## Mock 需求

### KernelSpawner Mock

**接口:** `shell.KernelSpawner`
**实现:** `mockSpawner`（`pipe_test.go` 中已定义）

用于 ScriptExecutor 测试，预设每次 SpawnAndWait 的返回值（result, exitCode, tokens, error）。

---

## 实现清单

### Test Group 1: Environment 模型 (shell/env.go)

**文件:** `shell/env_test.go`

**让以下测试通过的实现任务:**

- [ ] 实现 `Environment` 结构体（`vars map[string]string`）
- [ ] 实现 `NewEnvironment() *Environment`
- [ ] 实现 `NewEnvironmentFromOS() *Environment`
- [ ] 实现 `Set(key, value string)` / `Get(key string) (string, bool)` / `Delete(key string)`
- [ ] 实现 `All() map[string]string` 返回快照副本
- [ ] 实现 `Expand(input string) string` 变量展开引擎
  - [ ] `$VAR` 语法
  - [ ] `${VAR}` 语法
  - [ ] `\$` 转义
  - [ ] 未定义变量 → 空字符串
  - [ ] `$` 在末尾或后跟非变量名字符 → 保持原样
- [ ] 运行: `go test ./shell/ -run "TestEnvironment|TestExpand|TestNewEnvironment"`
- [ ] ✅ 全部通过 (green phase)

**预计工作量:** 1-2 小时

---

### Test Group 2: 脚本解析器 (shell/script.go)

**文件:** `shell/script_test.go`

**让以下测试通过的实现任务:**

- [ ] 定义 `StatementKind` 类型 (`StmtExport`/`StmtSpawn`/`StmtPipeline`)
- [ ] 定义 `ExportStmt` 结构体 (`Key string`, `Value string`)
- [ ] 定义 `Statement` 结构体 (`Kind`, `Export`, `Spawn`, `Pipeline`, `Raw`)
- [ ] 定义 `Script` 结构体 (`Statements []Statement`)
- [ ] 实现 `ParseScript(input string) (*Script, error)`
  - [ ] 按行分割
  - [ ] 跳过空行和 `#` 注释
  - [ ] `parseExport(line)` 解析 export 语法（引号值、无引号值、值含 `=`）
  - [ ] 分派: export / pipeline（含 `|`）/ spawn（默认）
- [ ] 运行: `go test ./shell/ -run "TestParseScript"`
- [ ] ✅ 全部通过 (green phase)

**预计工作量:** 2-3 小时

---

### Test Group 3: 脚本执行器 (shell/script.go)

**文件:** `shell/script_test.go`

**让以下测试通过的实现任务:**

- [ ] 定义 `ScriptResult` (`LastResult`, `LastExitCode`, `TotalTokens`, `Elapsed`)
- [ ] 定义 `ScriptExecutor` (`spawner`, `env`, `OnStageStart`)
- [ ] 实现 `NewScriptExecutor(spawner KernelSpawner, env *Environment) *ScriptExecutor`
- [ ] 实现 `Execute(ctx context.Context, script *Script) (*ScriptResult, error)`
  - [ ] export: 展开 Value 中变量后写入 env
  - [ ] spawn: 展开 Intent 后调用 spawner.SpawnAndWait
  - [ ] pipeline: 展开每个 Command 的 Intent 后调用 PipelineExecutor
  - [ ] 非零 ExitCode 中断
  - [ ] context 取消检查
- [ ] 运行: `go test ./shell/ -run "TestScriptExecutor"`
- [ ] ✅ 全部通过 (green phase)

**预计工作量:** 2-3 小时

---

### Test Group 4: CLI 集成 (cmd/rnix/main.go)

**文件:** `cmd/rnix/main_test.go`

**让以下测试通过的实现任务:**

- [ ] 实现 `isScriptSyntax(intent string) bool` — 检测多行（含 `\n`）或以 `export` 开头
- [ ] 在 `runRoot` 中插入 `isScriptSyntax` 检测（在 `isPipelineSyntax` 之前）
- [ ] 运行: `go test ./cmd/rnix/ -run "TestIsScriptSyntax|TestScriptDetection|TestExistingPaths"`
- [ ] ✅ 全部通过 (green phase)

**预计工作量:** 1 小时

---

### Test Group 5: IPC 扩展 (未在 ATDD 中覆盖 — DEV 阶段补充)

**说明:** IPC 层的 `MethodExecScript`、`ExecScriptRequest/Response`、`handleExecScript`、`ExecScriptAndWatch` 测试将在 DEV 实现阶段通过集成测试覆盖，复用现有 `ipc/pipeline_test.go` 的 `newTestServer` 模式。

---

## 运行测试

```bash
# 运行所有 11.2 失败测试（当前 RED phase — 编译失败）
go test ./shell/... ./cmd/rnix/...

# 运行 env 相关测试
go test ./shell/ -run "TestEnvironment|TestExpand|TestNewEnvironment"

# 运行 script 解析测试
go test ./shell/ -run "TestParseScript"

# 运行 script 执行测试
go test ./shell/ -run "TestScriptExecutor"

# 运行 CLI 回归测试
go test ./cmd/rnix/ -run "TestIsScriptSyntax|TestScriptDetection|TestExistingPaths"

# 运行全部 11.2 测试（verbose）
go test -v ./shell/... ./cmd/rnix/... -run "11.2|Script|Env|Export|Expand"
```

---

## Red-Green-Refactor 工作流

### RED Phase (完成) ✅

**TEA Agent 职责:**

- ✅ 所有测试已编写并失败（编译错误 = RED）
- ✅ 复用 mockSpawner（Story 11.1 模式）
- ✅ 实现清单已创建

**验证:**

- 所有测试运行后编译失败（`undefined: NewEnvironment`, `undefined: ParseScript`, `undefined: isScriptSyntax`）
- 失败原因是缺少实现代码，而非测试 bug
- 测试设计覆盖 3 个 AC 的所有场景

---

### GREEN Phase (DEV Team — 下一步)

**DEV Agent 职责:**

1. **从 Test Group 1 开始**（Environment 模型）— 最基础，无依赖
2. **阅读测试**理解预期行为
3. **实现最小代码**使测试通过
4. **运行测试**验证 green
5. **勾选实现清单**中的任务
6. **依次完成** Test Group 2 → 3 → 4

**关键原则:**

- 一次实现一个 Test Group
- 最小实现（不过度工程化）
- 频繁运行测试
- 使用实现清单作为路线图

**建议实现顺序:**

1. `shell/env.go` (Environment + Expand) → 运行 env_test.go
2. `shell/script.go` (ParseScript) → 运行 script_test.go 解析部分
3. `shell/script.go` (ScriptExecutor) → 运行 script_test.go 执行部分
4. `cmd/rnix/main.go` (isScriptSyntax) → 运行 main_test.go
5. `ipc/protocol.go` + `ipc/server.go` + `ipc/client.go` → 集成测试

---

### REFACTOR Phase (DEV Team — 全部测试通过后)

1. **验证所有测试通过**
2. **审查代码质量**
3. **提取重复代码**（DRY）
4. **确保测试仍然通过**
5. **更新文档**

---

## 下一步

1. **将此清单和失败测试交给 DEV 工作流**
2. **运行失败测试**确认 RED phase: `go test ./shell/... ./cmd/rnix/...`
3. **从 Test Group 1 (env.go) 开始实现**
4. **每完成一个 Group 运行一次测试**
5. **全部通过后进入 REFACTOR 阶段**
6. **完成后更新 sprint-status.yaml**

---

## 测试执行证据

### 初始测试运行 (RED Phase 验证)

**命令:** `go test ./shell/... ./cmd/rnix/...`

**结果:**

```
# github.com/rnixai/rnix/shell [github.com/rnixai/rnix/shell.test]
shell/env_test.go:19:9: undefined: NewEnvironment
shell/env_test.go:38:9: undefined: NewEnvironment
...
shell/env_test.go:133:9: too many errors
FAIL    github.com/rnixai/rnix/shell [build failed]

# github.com/rnixai/rnix/cmd/rnix [github.com/rnixai/rnix/cmd/rnix.test]
cmd/rnix/main_test.go:1048:13: undefined: isScriptSyntax
cmd/rnix/main_test.go:1071:13: undefined: isScriptSyntax
...
FAIL    github.com/rnixai/rnix/cmd/rnix [build failed]
```

**摘要:**

- 总测试数: ~46 (22 env + 20 script + 4 CLI)
- 通过: 0 (预期)
- 失败: 46 (编译失败 — 预期)
- 状态: ✅ RED phase 已验证

---

## 备注

- Go 后端项目使用标准 `testing` 包，无需 Playwright/Cypress
- mockSpawner 复用 Story 11.1 的 `pipe_test.go` 定义（同包可见）
- IPC 层测试（exec_script）将在 DEV 阶段通过集成测试补充
- `shell/script_test.go` 中的 `contains` 辅助函数用于检查 PIPE_INPUT 注入

---

**Generated by BMad TEA Agent** — 2026-03-03
