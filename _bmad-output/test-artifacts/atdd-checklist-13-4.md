---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-07'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/13-4-runtime-parameter-hot-modification.md'
  - '_bmad/tea/config.yaml'
  - 'kernel/process.go'
  - 'kernel/breakpoint.go'
  - 'kernel/breakpoint_test.go'
  - 'ipc/server.go'
  - 'ipc/server_test.go'
  - 'ipc/protocol_test.go'
  - 'cmd/rnix/gdb.go'
  - 'cmd/rnix/gdb_test.go'
  - 'context/context.go'
---

# ATDD Checklist - Epic 13, Story 4: 运行时参数热修改

**Date:** 2026-03-07
**Author:** Decker
**Primary Test Level:** Unit + Integration (Backend Go)

---

## Step 1: Preflight & Context Loading

### Stack Detection
- **Detected Stack:** `backend` (Go 1.26, go.mod detected, no frontend indicators)
- **Test Framework:** Go standard `testing` package with `go test -race`
- **Test Stack Type:** auto -> resolved to `backend`

### Prerequisites Verified
- Story 13-4 approved with 4 clear acceptance criteria (AC #1-4)
- Test framework configured: Go `testing` + existing `*_test.go` patterns
- Development environment available

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/13-4-runtime-parameter-hot-modification.md`
- **Acceptance Criteria:** 4 ACs covering set model, set context append, set skills add, set env
- **Affected Components:** `kernel/process.go`, `kernel/kernel.go`, `ipc/server.go`, `cmd/rnix/gdb.go`
- **Dependencies:** Builds on Story 13-2 breakpoint system + Story 13-3 step/inspect system

### Framework & Existing Patterns
- Existing test patterns in `kernel/breakpoint_test.go` (13-2 + 13-3 tests)
- Existing IPC tests in `ipc/server_test.go` (gdb_command handler tests)
- Existing protocol tests in `ipc/protocol_test.go` (serialization roundtrips)
- Existing gdb CLI tests in `cmd/rnix/gdb_test.go` (command parsing tests)
- Helper: `newBreakpointTestProcess()` creates Running test processes
- Helper: `setupTestServer()` creates full IPC test server with context manager

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (kernel, IPC, CLI), standard CRUD-like operations on Process struct fields

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | `set model sonnet` -> 模型偏好切换 | Unit (kernel) + IPC (server) + CLI (parser) | P0 |
| AC#2 | `set context append "text"` -> 追加上下文 | Unit (kernel) + IPC (server) + CLI (parser) | P0 |
| AC#3 | `set skills add name` -> 技能列表添加 | Unit (kernel) + IPC (server) + CLI (parser) | P0 |
| AC#4 | `set env KEY=VALUE` -> 环境变量设置 | Unit (kernel) + IPC (server) + CLI (parser) | P0 |

### Test Levels

- **Unit Tests** (`kernel/breakpoint_test.go`): Process field operations (SetGdbModelOverride/GetGdbModelOverride, SetGdbEnv/GetGdbEnvVars, AddGdbSkill/GetGdbExtraSkills), copy isolation, concurrency safety
- **Integration Tests** (`ipc/server_test.go`): IPC server routing for `set` command, end-to-end set model/context/skills/env through IPC
- **CLI Parse Tests** (`cmd/rnix/gdb_test.go`): parseSetCommand for all four subcommands, error handling, struct validation

### Red Phase Verification
- All tests reference functions/types that do NOT exist yet (SetGdbModelOverride, GetGdbModelOverride, SetGdbEnv, GetGdbEnvVars, AddGdbSkill, GetGdbExtraSkills, SetCommandResult, parseSetCommand, handleGdbSet)
- Compilation will fail -> RED phase confirmed

---

## Failing Tests Created (RED Phase)

### Unit Tests (12 tests)

**File:** `kernel/breakpoint_test.go` (appended)

- **13.4-UNIT-001** [P0] SetGdbModelOverride + GetGdbModelOverride 基本设置/获取
  - **Status:** RED - `proc.GetGdbModelOverride()` 不存在，编译失败
  - **Verifies:** AC#1 模型覆盖的基本存储和读取

- **13.4-UNIT-002** [P0] SetGdbModelOverride 覆盖后再次设置新值
  - **Status:** RED - `proc.SetGdbModelOverride()` 不存在，编译失败
  - **Verifies:** AC#1 模型覆盖值可以被更新

- **13.4-UNIT-003** [P1] SetGdbModelOverride 设置空字符串清除覆盖
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 清除模型覆盖回到默认

- **13.4-UNIT-004** [P0] SetGdbEnv + GetGdbEnvVars 基本设置/获取
  - **Status:** RED - `proc.SetGdbEnv()` / `proc.GetGdbEnvVars()` 不存在
  - **Verifies:** AC#4 环境变量的存储和读取

- **13.4-UNIT-005** [P0] SetGdbEnv 设置多个变量
  - **Status:** RED - 编译失败
  - **Verifies:** AC#4 多个环境变量并存

- **13.4-UNIT-006** [P0] SetGdbEnv 覆盖已有变量
  - **Status:** RED - 编译失败
  - **Verifies:** AC#4 环境变量可覆盖

- **13.4-UNIT-007** [P0] GetGdbEnvVars 返回副本，不暴露内部 map
  - **Status:** RED - 编译失败
  - **Verifies:** AC#4 线程安全，返回独立副本

- **13.4-UNIT-008** [P0] AddGdbSkill + GetGdbExtraSkills 基本添加/获取
  - **Status:** RED - `proc.AddGdbSkill()` / `proc.GetGdbExtraSkills()` 不存在
  - **Verifies:** AC#3 技能列表的添加和读取

- **13.4-UNIT-009** [P0] AddGdbSkill 幂等性 -- 重复添加不会重复
  - **Status:** RED - 编译失败
  - **Verifies:** AC#3 技能添加的幂等性

- **13.4-UNIT-010** [P0] AddGdbSkill 添加多个不同 skill
  - **Status:** RED - 编译失败
  - **Verifies:** AC#3 多个技能并存

- **13.4-UNIT-011** [P0] GetGdbExtraSkills 返回副本，不暴露内部 slice
  - **Status:** RED - 编译失败
  - **Verifies:** AC#3 线程安全，返回独立副本

- **13.4-UNIT-012** [P1] 并发安全测试 -- model override / env vars / skills
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1-4 所有新字段的并发安全

### CLI Parse Tests (14 tests)

**File:** `cmd/rnix/gdb_test.go` (appended)

- **13.4-CLI-001** [P0] parseSetCommand 解析 "set model sonnet"
  - **Status:** RED - `parseSetCommand` 不存在，编译失败
  - **Verifies:** AC#1 CLI 解析 set model 命令

- **13.4-CLI-002** [P0] parseSetCommand 解析 "set context append text"
  - **Status:** RED - 编译失败
  - **Verifies:** AC#2 CLI 解析 set context append 命令

- **13.4-CLI-003** [P0] parseSetCommand 解析 "set skills add name"
  - **Status:** RED - 编译失败
  - **Verifies:** AC#3 CLI 解析 set skills add 命令

- **13.4-CLI-004** [P0] parseSetCommand 解析 "set env KEY=VALUE"
  - **Status:** RED - 编译失败
  - **Verifies:** AC#4 CLI 解析 set env 命令

- **13.4-CLI-005** [P1] parseSetCommand 无参数返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** 错误处理

- **13.4-CLI-006** [P1] parseSetCommand 未知子命令返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** 错误处理

- **13.4-CLI-007** [P1] parseSetCommand "set model" 无值返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#1 错误处理

- **13.4-CLI-008** [P1] parseSetCommand "set context" 无 action 返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#2 错误处理

- **13.4-CLI-009** [P1] parseSetCommand "set context append" 无文本返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#2 错误处理

- **13.4-CLI-010** [P1] parseSetCommand "set skills" 无 action 返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#3 错误处理

- **13.4-CLI-011** [P1] parseSetCommand "set skills add" 无名称返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#3 错误处理

- **13.4-CLI-012** [P1] parseSetCommand "set env" 无 KEY=VALUE 返回错误
  - **Status:** RED - 编译失败
  - **Verifies:** AC#4 错误处理

- **13.4-CLI-013** [P0] SetCommandResult 结构体字段完整
  - **Status:** RED - `SetCommandResult` 不存在，编译失败
  - **Verifies:** CLI 数据结构定义

- **13.4-CLI-014** [P0] parseSetCommand "set context append" 多词文本拼接
  - **Status:** RED - 编译失败
  - **Verifies:** AC#2 多词文本 Join 为空格分隔

### IPC Server Tests (7 tests)

**File:** `ipc/server_test.go` (appended)

- **13.4-IPC-001** [P0] Server handles gdb_command "set model" routing
  - **Status:** RED - handleGdbSet 不存在，IPC server 无 "set" 路由
  - **Verifies:** AC#1 IPC 端到端 set model

- **13.4-IPC-002** [P0] Server handles gdb_command "set context append"
  - **Status:** RED - handleGdbSet 不存在
  - **Verifies:** AC#2 IPC 端到端 set context append

- **13.4-IPC-003** [P0] Server handles gdb_command "set skills add"
  - **Status:** RED - handleGdbSet 不存在
  - **Verifies:** AC#3 IPC 端到端 set skills add

- **13.4-IPC-004** [P0] Server handles gdb_command "set env KEY=VALUE"
  - **Status:** RED - handleGdbSet 不存在
  - **Verifies:** AC#4 IPC 端到端 set env

- **13.4-IPC-005** [P1] Server handles "set" with no args returns error
  - **Status:** RED - handleGdbSet 不存在
  - **Verifies:** 错误处理

- **13.4-IPC-006** [P1] Server handles "set" with unknown subcommand returns error
  - **Status:** RED - handleGdbSet 不存在
  - **Verifies:** 错误处理

- **13.4-IPC-007** [P1] Server handles "set env" with invalid format returns error
  - **Status:** RED - handleGdbSet 不存在
  - **Verifies:** AC#4 无效 KEY=VALUE 格式错误处理

---

## Story Summary

**As a** 平台构建者
**I want** 在 gdb 中检查和修改智能体的运行时参数，修改立即生效于下一个推理步骤
**So that** 我可以在调试过程中快速测试不同配置对智能体行为的影响

---

## Acceptance Criteria

1. `set model sonnet` -> 智能体的模型偏好切换为 sonnet，下一次 LLM 调用使用新模型
2. `set context append "额外分析指令"` -> 指定内容被追加到上下文，下一次推理步骤包含该内容
3. `set skills add code-review` -> code-review Skill 被加载并加入智能体的能力列表
4. `set env DEBUG=true` -> 环境变量被设置，智能体后续执行可引用该变量

---

## Implementation Checklist

### Test: 13.4-UNIT-001 to 13.4-UNIT-012 (Kernel Unit Tests)

**File:** `kernel/breakpoint_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `kernel/process.go` 的 `Process` 结构体中新增 `gdbModelOverride string` 字段（mu 保护）
- [ ] 实现 `Process.SetGdbModelOverride(model string)` 方法
- [ ] 实现 `Process.GetGdbModelOverride() string` 方法
- [ ] 在 `Process` 结构体中新增 `gdbEnvVars map[string]string` 字段（mu 保护）
- [ ] 实现 `Process.SetGdbEnv(key, value string)` 方法
- [ ] 实现 `Process.GetGdbEnvVars() map[string]string` 方法（返回副本）
- [ ] 在 `Process` 结构体中新增 `gdbExtraSkills []string` 字段（mu 保护）
- [ ] 实现 `Process.AddGdbSkill(name string)` 方法（幂等，避免重复）
- [ ] 实现 `Process.GetGdbExtraSkills() []string` 方法（返回副本）
- [ ] Run test: `go test -race -run 'TestProcess_(SetGdb|GetGdb|AddGdb)' ./kernel/`

---

### Test: 13.4-CLI-001 to 13.4-CLI-014 (CLI Parse Tests)

**File:** `cmd/rnix/gdb_test.go`

**Tasks to make these tests pass:**

- [ ] 定义 `SetCommandResult` 结构体（SubCommand, Action, Value 字段）
- [ ] 实现 `parseSetCommand(args []string) (*SetCommandResult, error)` 函数
- [ ] 处理四种子命令：model, context, skills, env
- [ ] 对 "context append" 的多词文本用空格拼接
- [ ] 对缺少参数的情况返回适当错误
- [ ] Run test: `go test -race -run 'TestParseSetCommand|TestSetCommandResult' ./cmd/rnix/`

---

### Test: 13.4-IPC-001 to 13.4-IPC-007 (IPC Server Tests)

**File:** `ipc/server_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `ipc/server.go` 的 `handleGdbCommand` switch 中新增 `"set"` 命令分支
- [ ] 实现 `handleGdbSet(proc *kernel.Process, args []string) GdbCommandResponse`
- [ ] `set model` -> 调用 `proc.SetGdbModelOverride(args[1])`
- [ ] `set context append` -> 调用 `s.ctxMgr.AppendMessage(proc.CtxID, context.RoleUser, content)`
- [ ] `set skills add` -> 调用 `proc.AddGdbSkill(args[2])`
- [ ] `set env` -> 解析 KEY=VALUE，调用 `proc.SetGdbEnv(key, value)`
- [ ] Run test: `go test -race -run 'TestServer_GdbCommand_Set' ./ipc/`

---

### Additional Implementation Tasks (Not directly tested by ATDD but required by story)

- [ ] 在 `kernel/kernel.go` 的 `reasonStep` 循环内，构建 `llmRequest` 之前，检查 `proc.GetGdbModelOverride()` 并使用覆盖值
- [ ] 在 `cmd/rnix/gdb.go` 的命令循环 switch 中新增 `"set"` 分支和 `gdbSet()` 函数
- [ ] 更新 `printGdbHelp` 增加 set 命令说明

---

## Running Tests

```bash
# Run all failing tests for this story (kernel unit tests)
go test -race -run 'TestProcess_(SetGdb|GetGdb|AddGdb|GdbRuntimeParams)' ./kernel/

# Run CLI parse tests
go test -race -run 'TestParseSetCommand|TestSetCommandResult' ./cmd/rnix/

# Run IPC server tests
go test -race -run 'TestServer_GdbCommand_Set' ./ipc/

# Run all story 13-4 tests across packages
go test -race -run '13.4|SetGdb|GdbRuntimeParams|ParseSetCommand|SetCommandResult|GdbCommand_Set' ./kernel/ ./cmd/rnix/ ./ipc/

# Run with verbose output for debugging
go test -race -v -run 'TestProcess_SetGdbModelOverride_Basic' ./kernel/
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 33 tests written and failing (compilation errors due to missing types/methods)
- Tests organized across 3 files matching existing patterns
- Implementation checklist created

**Verification:**

- All tests fail due to missing implementation (undefined symbols: SetGdbModelOverride, GetGdbModelOverride, SetGdbEnv, GetGdbEnvVars, AddGdbSkill, GetGdbExtraSkills, SetCommandResult, parseSetCommand, handleGdbSet)
- Failure messages are clear: "undefined: proc.SetGdbModelOverride" etc.
- Tests follow existing project patterns (Go standard testing, table-driven where appropriate)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with kernel unit tests** (13.4-UNIT-001 to 13.4-UNIT-012) -- implement Process fields and methods
2. **Then CLI parse tests** (13.4-CLI-001 to 13.4-CLI-014) -- implement SetCommandResult and parseSetCommand
3. **Then IPC server tests** (13.4-IPC-001 to 13.4-IPC-007) -- implement handleGdbSet
4. **Finally** implement reasonStep model override hook and gdb CLI set command routing

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all 33 tests pass with `go test -race`
2. Review code for consistency with existing gdb commands (step, inspect patterns)
3. Ensure no duplication between handleGdbSet and existing handlers

---

## Test Summary

| Category | Test Count | Priority Breakdown |
|---|---|---|
| Kernel Unit Tests | 12 | 9 P0, 3 P1 |
| CLI Parse Tests | 14 | 6 P0, 8 P1 |
| IPC Server Tests | 7 | 4 P0, 3 P1 |
| **Total** | **33** | **19 P0, 14 P1** |

---

## Next Steps

1. **Share this checklist** with the dev workflow
2. **Run failing tests** to confirm RED phase: `go test -race ./kernel/ ./cmd/rnix/ ./ipc/`
3. **Begin implementation** using implementation checklist as guide
4. **Work one test group at a time** (kernel -> CLI -> IPC)
5. **When all tests pass**, refactor code for quality

---

**Generated by BMad TEA Agent** - 2026-03-07
