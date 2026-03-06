---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-06'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/13-1-gdb-attach-detach.md'
  - 'ipc/protocol.go'
  - 'ipc/client.go'
  - 'ipc/server.go'
  - 'ipc/protocol_test.go'
  - 'ipc/integration_test.go'
  - 'ipc/server_test.go'
  - 'kernel/process.go'
  - 'internal/types/types.go'
  - 'cmd/rnix/main.go'
---

# ATDD Checklist - Epic 13, Story 1: gdb 调试会话管理（Attach/Detach）

**Date:** 2026-03-06
**Author:** Decker
**Primary Test Level:** Integration (IPC)

---

## Story Summary

实现 gdb 调试器的核心会话管理功能，包括通过 IPC 附着到运行中的智能体进程进入交互式调试 TUI，以及随时 Detach 断开调试会话而不影响智能体执行。

**As a** 平台构建者
**I want** 通过 `rnix gdb <pid>` 附着到运行中的智能体进入交互式调试会话，并可随时 Detach 断开
**So that** 我可以在不中断智能体执行的前提下进入和退出调试模式

---

## Acceptance Criteria

1. **AC1**: Given 一个 Running 状态的智能体进程 PID=N, When 用户执行 `rnix gdb N`, Then 系统通过 IPC 发送 `attach_gdb` 请求，成功后进入交互式调试 TUI, And Attach 延迟 <= 200ms
2. **AC2**: Given 用户处于 gdb 调试会话中, When 用户执行 `detach` 命令, Then 调试会话断开，智能体继续正常执行，不受影响
3. **AC3**: Given 目标进程不存在或已处于 Dead 状态, When 用户执行 `rnix gdb N`, Then 系统返回结构化错误信息：进程不存在/已终止

---

## Failing Tests Created (RED Phase)

### Unit Tests (8 tests)

**File:** `ipc/protocol_test.go` (新增约 180 行)

- **Test:** `TestMethodAttachGdb_Exists` (13.1-UNIT-001)
  - **Status:** RED - `MethodAttachGdb` 未定义
  - **Verifies:** AC1 - MethodAttachGdb 常量存在且与 MethodAttachDebug/MethodAttachLog 不同
  - **Priority:** P0

- **Test:** `TestAttachGdbRequest_MarshalRoundTrip` (13.1-UNIT-002)
  - **Status:** RED - `AttachGdbRequest` 结构体未定义
  - **Verifies:** AC1 - AttachGdbRequest 包含 PID 字段且正确序列化
  - **Priority:** P0

- **Test:** `TestAttachGdbResponse_MarshalRoundTrip` (13.1-UNIT-003)
  - **Status:** RED - `AttachGdbResponse` 结构体未定义
  - **Verifies:** AC1 - AttachGdbResponse 包含进程元信息（PID、State、Intent、Skills、TokensUsed）
  - **Priority:** P0

- **Test:** `TestAttachGdbResponse_NilSkills` (13.1-UNIT-004)
  - **Status:** RED - `AttachGdbResponse` 结构体未定义
  - **Verifies:** AC1 - Skills 为 nil 时序列化为 [] 而非 null
  - **Priority:** P1

- **Test:** `TestDetachGdbRequest_MarshalRoundTrip` (13.1-UNIT-005)
  - **Status:** RED - `DetachGdbRequest` 结构体未定义
  - **Verifies:** AC2 - DetachGdbRequest 包含 PID 字段且正确序列化
  - **Priority:** P0

- **Test:** `TestGdbEventType_Constants` (13.1-UNIT-006)
  - **Status:** RED - `StreamGdbSyscall`/`StreamGdbLog`/`StreamGdbStateChange` 未定义
  - **Verifies:** AC1 - gdb 事件类型常量存在且与现有类型不冲突
  - **Priority:** P0

- **Test:** `TestAttachGdbRequest_IPCEnvelope` (13.1-UNIT-007)
  - **Status:** RED - `MethodAttachGdb`/`AttachGdbRequest` 未定义
  - **Verifies:** AC1 - AttachGdbRequest 能正确包装在 IPC Request envelope 中
  - **Priority:** P1

- **Test:** `TestAttachGdbResponse_FullMetadata` (13.1-UNIT-008)
  - **Status:** RED - `AttachGdbResponse` 未定义
  - **Verifies:** AC1 - 响应 JSON 包含所有必要字段
  - **Priority:** P1

### Server Tests (2 tests)

**File:** `ipc/server_test.go` (新增约 45 行)

- **Test:** `TestServer_AttachGdb_NotFound` (13.1-SRV-001)
  - **Status:** RED - `MethodAttachGdb`/`AttachGdbRequest` 未定义
  - **Verifies:** AC3 - Server 对不存在 PID 的 attach_gdb 请求返回 NOT_FOUND
  - **Priority:** P0

- **Test:** `TestServer_AttachGdb_InvalidPayload` (13.1-SRV-002)
  - **Status:** RED - `MethodAttachGdb` 未定义
  - **Verifies:** AC3 - Server 对无效 payload 的 attach_gdb 请求优雅处理
  - **Priority:** P1

### Integration Tests (7 tests)

**File:** `ipc/integration_test.go` (新增约 230 行)

- **Test:** `TestIntegration_AttachGdb_ReceivesDualChannelEvents` (13.1-INT-001)
  - **Status:** RED - `client.AttachGdb()`/`GdbEvent` 未定义
  - **Verifies:** AC1 - Attach 成功后接收 DebugChan + LogChan 双通道事件流
  - **Priority:** P0

- **Test:** `TestIntegration_AttachGdb_NotFound` (13.1-INT-002)
  - **Status:** RED - `client.AttachGdb()` 未定义
  - **Verifies:** AC3 - 进程不存在时返回 NOT_FOUND 错误
  - **Priority:** P0

- **Test:** `TestIntegration_AttachGdb_DeadProcess` (13.1-INT-003)
  - **Status:** RED - `client.AttachGdb()` 未定义
  - **Verifies:** AC3 - 进程已 Dead/Zombie 时返回错误
  - **Priority:** P0

- **Test:** `TestIntegration_AttachGdb_DetachDoesNotAffectProcess` (13.1-INT-004)
  - **Status:** RED - `client.AttachGdb()`/`client.SendDetach()` 未定义
  - **Verifies:** AC2 - Detach 后智能体继续正常执行
  - **Priority:** P0

- **Test:** `TestIntegration_AttachGdb_ProcessExitDuringSession` (13.1-INT-005)
  - **Status:** RED - `client.AttachGdb()`/`StreamGdbStateChange` 未定义
  - **Verifies:** AC1 - 进程在 attach 后退出时发送 state_change 事件
  - **Priority:** P1

- **Test:** `TestIntegration_AttachGdb_InitialMetadata` (13.1-INT-006)
  - **Status:** RED - `client.AttachGdb()` 未定义
  - **Verifies:** AC1 - Attach 返回进程元信息快照（PID、Intent、Skills）
  - **Priority:** P1

- **Test:** `TestIntegration_AttachGdb_ClientDisconnectCleanup` (13.1-INT-007)
  - **Status:** RED - `client.AttachGdb()` 未定义
  - **Verifies:** AC2 - 客户端断开连接后 server 自动清理 attach 状态
  - **Priority:** P2

### CLI Tests (6 tests)

**File:** `cmd/rnix/main_test.go` (新增约 75 行)

- **Test:** `TestHelp_ContainsGdbSubcommand` (13.1-CLI-001)
  - **Status:** RED - `gdbCmd` 未注册到 rootCmd
  - **Verifies:** AC1 - gdb 子命令在 help 中可见
  - **Priority:** P0

- **Test:** `TestGdbCmd_RequiresExactlyOnePID` (13.1-CLI-002)
  - **Status:** RED - `gdbCmd` 未定义
  - **Verifies:** AC1 - gdb 命令要求恰好 1 个 PID 参数
  - **Priority:** P0

- **Test:** `TestRunGdb_InvalidPID` (13.1-CLI-003)
  - **Status:** RED - `runGdb` 未定义
  - **Verifies:** AC3 - 无效 PID 格式返回错误
  - **Priority:** P0

- **Test:** `TestRunGdb_PIDNotFound_ViaIPC` (13.1-CLI-004)
  - **Status:** RED - `runGdb` 未定义
  - **Verifies:** AC3 - PID 不存在时通过 IPC 返回错误
  - **Priority:** P0

- **Test:** `TestGdbCmd_UsageAndDescription` (13.1-CLI-005)
  - **Status:** RED - `gdbCmd` 未定义
  - **Verifies:** AC1 - gdb 命令有正确的 Use 和 Short 描述
  - **Priority:** P1

- **Test:** `TestGdbCmd_SupportsJSONFlag` (13.1-CLI-006)
  - **Status:** RED - `gdbCmd` 未定义
  - **Verifies:** AC1 - gdb 命令支持 --json 全局 flag
  - **Priority:** P1

---

## Mock Requirements

N/A -- 本 story 使用真实的 kernel + IPC server 进行集成测试（复用 `setupIntegrationServer` 和 `setupTestServer` helper 函数），不需要外部服务 mock。

---

## Implementation Checklist

### Test: 13.1-UNIT-001~008 (Protocol 序列化)

**File:** `ipc/protocol_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `ipc/protocol.go` 新增 `MethodAttachGdb Method = "attach_gdb"`
- [ ] 新增 `AttachGdbRequest` 结构体 (PID 字段)
- [ ] 新增 `AttachGdbResponse` 结构体 (PID, State, Intent, Skills, TokensUsed)
- [ ] 新增 `DetachGdbRequest` 结构体 (PID 字段)
- [ ] 新增 `StreamGdbSyscall`, `StreamGdbLog`, `StreamGdbStateChange` 事件类型常量
- [ ] Run: `go test ./ipc/ -run 'Test(MethodAttachGdb|AttachGdb|DetachGdb|GdbEventType)' -v`

### Test: 13.1-SRV-001~002 (Server Handler)

**File:** `ipc/server_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `ipc/server.go` 的 `handleConn` switch 中增加 `MethodAttachGdb` 分支
- [ ] 实现 `handleAttachGdb` 方法：验证 PID 存在且 Running，否则返回 NOT_FOUND
- [ ] Run: `go test ./ipc/ -run 'TestServer_AttachGdb' -v`

### Test: 13.1-INT-001~007 (集成测试)

**File:** `ipc/integration_test.go`

**Tasks to make these tests pass:**

- [ ] 新增 `GdbEvent` 类型 (Type + Payload)
- [ ] 在 `ipc/client.go` 新增 `AttachGdb` 方法：发送请求、读取初始响应、启动事件流读取循环
- [ ] 在 `ipc/client.go` 新增 `SendDetach` 方法
- [ ] 在 `ipc/server.go` 的 `handleAttachGdb` 中：
  - 获取 DebugChan + LogChan 双通道
  - 发送初始响应含进程元信息
  - 启动双 goroutine 转发事件
  - 监听 detach 或连接断开
- [ ] 处理进程 attach 后退出的 state_change 事件
- [ ] 实现客户端断开时 server 端自动清理
- [ ] Run: `go test ./ipc/ -run 'TestIntegration_AttachGdb' -v`

### Test: 13.1-CLI-001~006 (CLI 命令)

**File:** `cmd/rnix/main_test.go`

**Tasks to make these tests pass:**

- [ ] 新建 `cmd/rnix/gdb.go`，定义 `gdbCmd` 和 `runGdb`
- [ ] 在 `cmd/rnix/main.go` 的 `init()` 中注册 `gdbCmd` 到 `rootCmd`
- [ ] 实现 PID 参数解析和错误处理
- [ ] 实现 IPC 连接和 AttachGdb 调用
- [ ] Run: `go test ./cmd/rnix/ -run 'Test(Help_ContainsGdb|GdbCmd|RunGdb)' -v`

---

## Running Tests

```bash
# Run all failing tests for this story (will fail to compile until implementation)
go test ./ipc/ ./cmd/rnix/ -run '13\.1|AttachGdb|GdbCmd|RunGdb|GdbEvent' -v

# Run protocol unit tests only
go test ./ipc/ -run 'Test(MethodAttachGdb|AttachGdb|DetachGdb|GdbEventType)' -v

# Run integration tests only
go test ./ipc/ -run 'TestIntegration_AttachGdb' -v

# Run server tests only
go test ./ipc/ -run 'TestServer_AttachGdb' -v

# Run CLI tests only
go test ./cmd/rnix/ -run 'Test(Help_ContainsGdb|GdbCmd|RunGdb)' -v

# Run all ipc tests (includes existing + new)
go test ./ipc/ -v -race

# Run all cmd tests (includes existing + new)
go test ./cmd/rnix/ -v -race
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 23 tests written and failing (compile errors)
- Tests cover all 3 acceptance criteria
- Tests follow existing project patterns (protocol_test.go, integration_test.go, server_test.go, main_test.go)
- No test dependencies (each test creates its own process and server)
- Tests are deterministic (no race conditions, proper channel lifecycle)

**Verification:**

```
go test ./ipc/ 2>&1 | head -20
# Expected: compile failure referencing MethodAttachGdb, AttachGdbRequest, etc.

go test ./cmd/rnix/ 2>&1 | head -10
# Expected: compile failure referencing gdbCmd, runGdb
```

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with protocol types** (Task 1): MethodAttachGdb, AttachGdbRequest, AttachGdbResponse, DetachGdbRequest, GdbEventType
2. **Run unit tests** to verify serialization passes
3. **Implement server handler** (Task 2): handleAttachGdb in server.go
4. **Implement client method** (Task 3): AttachGdb and SendDetach in client.go
5. **Run integration tests** to verify IPC flow
6. **Implement CLI command** (Task 4): gdb.go with gdbCmd and runGdb
7. **Run CLI tests** to verify command registration and PID handling

---

### REFACTOR Phase (After All Tests Pass)

1. Verify all 23 tests pass with `go test ./ipc/ ./cmd/rnix/ -v -race`
2. Review code for quality (consistent error handling, proper goroutine cleanup)
3. Ensure attach state cleanup on disconnection
4. Run full test suite: `make test`

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./ipc/ ./cmd/rnix/ 2>&1`

**Results:**

```
# github.com/rnixai/rnix/ipc [github.com/rnixai/rnix/ipc.test]
ipc/protocol_test.go: MethodAttachGdb undefined
ipc/protocol_test.go: AttachGdbRequest undefined
ipc/protocol_test.go: AttachGdbResponse undefined
ipc/protocol_test.go: DetachGdbRequest undefined
ipc/protocol_test.go: StreamGdbSyscall undefined
ipc/protocol_test.go: StreamGdbLog undefined
ipc/protocol_test.go: StreamGdbStateChange undefined
ipc/integration_test.go: client.AttachGdb undefined
ipc/integration_test.go: GdbEvent undefined
ipc/integration_test.go: client.SendDetach undefined
FAIL github.com/rnixai/rnix/ipc [build failed]

# github.com/rnixai/rnix/cmd/rnix [github.com/rnixai/rnix/cmd/rnix.test]
cmd/rnix/main_test.go: gdbCmd undefined
cmd/rnix/main_test.go: runGdb undefined
FAIL github.com/rnixai/rnix/cmd/rnix [build failed]
```

**Summary:**

- Total tests: 23
- Passing: 0 (expected)
- Failing: 23 (compile errors, expected)
- Status: RED phase verified

---

## Acceptance Criteria Coverage

| AC | Description | Tests | Priority |
|----|-------------|-------|----------|
| AC1 | Attach 成功 + 双通道事件流 + TUI | UNIT-001~003,006~008, INT-001,005,006, CLI-001~002,005~006 | P0-P1 |
| AC2 | Detach 不影响智能体 | UNIT-005, INT-004,007 | P0-P2 |
| AC3 | 错误处理 (不存在/已 Dead) | SRV-001~002, INT-002,003, CLI-003~004 | P0-P1 |

---

## Next Steps

1. **DEV workflow 实现** Story 13-1 的代码
2. **按 Implementation Checklist** 顺序逐个让测试通过
3. **每实现一个 Task 后运行对应测试** 验证 GREEN
4. **所有 23 个测试通过后** 运行 `make test` 全量回归
5. **REFACTOR** 清理代码后确认测试仍通过

---

## Notes

- gdb 的 attach 本质是同时订阅 DebugChan + LogChan 的双通道合并模式，与 strace（只订阅 DebugChan）不同
- 进程在 attach 后退出需要发送 `gdb_state_change` 事件通知客户端
- Server 需要追踪每个进程的 attach 状态，防止多客户端冲突
- 客户端断开连接时 server 必须自动清理 attach 状态
- CLI 的交互式 TUI 部分（bufio.Scanner 读取用户输入）在本 ATDD 中不直接测试，由手动验证覆盖

---

**Generated by BMad TEA Agent** - 2026-03-06
