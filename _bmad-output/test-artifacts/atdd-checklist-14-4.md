---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/14-4-fork-continue-branch-exploration.md'
  - '_bmad/tea/testarch/knowledge/data-factories.md'
  - '_bmad/tea/testarch/knowledge/test-quality.md'
  - '_bmad/tea/testarch/knowledge/test-levels-framework.md'
  - '_bmad/tea/testarch/knowledge/test-healing-patterns.md'
---

# ATDD Checklist - Epic 14, Story 4: Fork-Continue 分支探索

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit + Integration (Backend Go)

---

## Story Summary

在回放的任意时间点创建新分支，修改上下文后重新执行（产生真实 LLM 调用），以验证"如果当时做了不同决定会怎样"。

**As a** 平台构建者
**I want** 在回放的任意时间点创建新分支，修改上下文后重新执行
**So that** 我可以验证"如果当时做了不同决定会怎样"

---

## Acceptance Criteria

1. **AC#1** - 用户在回放界面执行 `fork`，系统从该时间点恢复上下文快照
2. **AC#2** - 用户修改 fork 上下文并执行 `continue`，系统 Spawn 新进程产生真实 LLM 调用
3. **AC#3** - fork 产生的新进程可通过 `rnix ps` 和 `rnix strace` 正常查看

---

## Failing Tests Created (RED Phase)

### Unit Tests - debug/fork_test.go (18 tests)

**File:** `debug/fork_test.go` (~350 lines)

- **Test:** TestNewSnapshotRestorer (14.4-FORK-001)
  - **Status:** RED - SnapshotRestorer 类型不存在
  - **Verifies:** AC#1 - 创建 SnapshotRestorer

- **Test:** TestSnapshotRestorer_RestoreContext_ExtractsIntent (14.4-FORK-002)
  - **Status:** RED - RestoreContext 方法不存在
  - **Verifies:** AC#1 - 从 Spawn 事件提取 intent

- **Test:** TestSnapshotRestorer_RestoreContext_UsesSnapshot (14.4-FORK-003)
  - **Status:** RED - RestoreContext 方法不存在
  - **Verifies:** AC#1 - 使用 context_snapshot 恢复消息历史

- **Test:** TestSnapshotRestorer_RestoreContext_TruncatesAtSeqNum (14.4-FORK-004)
  - **Status:** RED - RestoreContext 方法不存在
  - **Verifies:** AC#1 - 在指定 SeqNum 处截断

- **Test:** TestSnapshotRestorer_RestoreContext_NoSnapshotBeforeSeqNum (14.4-FORK-005)
  - **Status:** RED - RestoreContext 方法不存在
  - **Verifies:** AC#1 - 无有效快照时返回错误

- **Test:** TestSnapshotRestorer_RestoreContext_SetsOriginalPID (14.4-FORK-006)
  - **Status:** RED - RestoreContext 方法不存在
  - **Verifies:** AC#1 - 记录原始 PID

- **Test:** TestSnapshotRestorer_RestoreContext_SetsSeqNum (14.4-FORK-007)
  - **Status:** RED - RestoreContext 方法不存在
  - **Verifies:** AC#1 - 记录 SeqNum

- **Test:** TestSnapshotRestorer_RestoreContext_SystemPrompt (14.4-FORK-008)
  - **Status:** RED - RestoreContext 方法不存在
  - **Verifies:** AC#1 - 提取 system prompt

- **Test:** TestForkContext_SetSystemPrompt (14.4-FORK-009)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - 修改 system prompt

- **Test:** TestForkContext_AppendMessage (14.4-FORK-010)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - 追加消息

- **Test:** TestForkContext_RemoveLastMessages (14.4-FORK-011)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - 移除消息

- **Test:** TestForkContext_RemoveLastMessages_MoreThanAvailable (14.4-FORK-012)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - 边界处理

- **Test:** TestForkContext_ReplaceLastMessage (14.4-FORK-013)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - 替换消息

- **Test:** TestForkContext_ReplaceLastMessage_EmptyMessages (14.4-FORK-014)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - 空消息边界

- **Test:** TestForkContext_Summary (14.4-FORK-015)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - 摘要输出

- **Test:** TestForkMessage_Fields (14.4-FORK-016)
  - **Status:** RED - ForkMessage 类型不存在
  - **Verifies:** AC#2 - 消息结构体

- **Test:** TestForkMessage_ToolCallID_Optional (14.4-FORK-017)
  - **Status:** RED - ForkMessage 类型不存在
  - **Verifies:** AC#2 - ToolCallID 可选

- **Test:** TestForkContext_JSONRoundTrip (14.4-FORK-018)
  - **Status:** RED - ForkContext 类型不存在
  - **Verifies:** AC#2 - JSON 序列化

### Unit Tests - debug/replay_test.go (7 tests appended)

**File:** `debug/replay_test.go` (tests appended)

- **Test:** TestReplaySession_Fork (14.4-REPLAY-001)
  - **Status:** RED - Fork() 方法不存在
  - **Verifies:** AC#1 - 在有效 cursor 位置 fork

- **Test:** TestReplaySession_Fork_NotStarted (14.4-REPLAY-002)
  - **Status:** RED - Fork() 方法不存在
  - **Verifies:** AC#1 - cursor=-1 时返回错误

- **Test:** TestReplaySession_Fork_DoesNotMoveCursor (14.4-REPLAY-003)
  - **Status:** RED - Fork() 方法不存在
  - **Verifies:** AC#1 - fork 不改变 cursor

- **Test:** TestReplaySession_ForkAt (14.4-REPLAY-004)
  - **Status:** RED - ForkAt() 方法不存在
  - **Verifies:** AC#1 - 在指定 SeqNum fork

- **Test:** TestReplaySession_ForkAt_DoesNotMoveCursor (14.4-REPLAY-005)
  - **Status:** RED - ForkAt() 方法不存在
  - **Verifies:** AC#1 - ForkAt 不改变 cursor

- **Test:** TestReplaySession_ForkAt_InvalidSeqNum (14.4-REPLAY-006)
  - **Status:** RED - ForkAt() 方法不存在
  - **Verifies:** AC#1 - 无效 SeqNum 报错

- **Test:** TestReplaySession_Fork_DifferentPositions (14.4-REPLAY-007)
  - **Status:** RED - Fork() 方法不存在
  - **Verifies:** AC#1 - 不同位置产生不同上下文

### Integration Tests - ipc/server_test.go (5 tests appended)

**File:** `ipc/server_test.go` (tests appended)

- **Test:** TestServer_ForkContinue (14.4-IPC-001)
  - **Status:** RED - MethodForkContinue/ForkContinueRequest 不存在
  - **Verifies:** AC#2 - fork_continue 创建新进程

- **Test:** TestServer_ForkContinue_MessagesReplayed (14.4-IPC-002)
  - **Status:** RED - MethodForkContinue 不存在
  - **Verifies:** AC#2 - 消息历史回放到新上下文

- **Test:** TestServer_ForkContinue_PPID (14.4-IPC-003)
  - **Status:** RED - ForkContinueResponse 不存在
  - **Verifies:** AC#3 - 新进程 PPID 指向原进程

- **Test:** TestServer_ForkContinue_OriginalPIDNotFound (14.4-IPC-004)
  - **Status:** RED - MethodForkContinue 不存在
  - **Verifies:** AC#3 - 原 PID 不存在时 PPID=0

- **Test:** TestServer_ForkContinue_EmptyMessages (14.4-IPC-005)
  - **Status:** RED - MethodForkContinue 不存在
  - **Verifies:** AC#2 - 空消息处理

### CLI Tests - cmd/rnix/replay_test.go (3 tests appended)

**File:** `cmd/rnix/replay_test.go` (tests appended)

- **Test:** TestReplayCommand_ForkInHelp (14.4-CLI-001)
  - **Status:** RED - printReplayHelp 未包含 fork
  - **Verifies:** AC#1 - fork 命令在 help 中

- **Test:** TestReplayCommand_ContinueInHelp (14.4-CLI-002)
  - **Status:** RED - printReplayHelp 未包含 continue
  - **Verifies:** AC#2 - continue 命令在 help 中

- **Test:** TestReplayCommand_ForkSubcommands (14.4-CLI-003)
  - **Status:** RED - printReplayHelp 未包含子命令
  - **Verifies:** AC#1,#2 - fork 子模式命令

---

## Data Factories Created

### Go Test Helpers (内联)

本项目使用 Go 标准测试模式。测试数据通过内联 helper 函数生成：

- `buildForkTestEvents()` - 构建包含 Spawn、CtxAlloc、ContextSnapshot、LLMResponse 事件的完整录制序列
- `buildForkTestEventsNoSpawn()` - 构建无 Spawn 事件的录制序列（边界测试）
- `createForkTestRecording(t)` - 创建完整的 fork 测试录制目录
- 复用现有 helpers: `writeTestMetadata()`, `writeTestEvents()`, `createTestRecording()`

---

## Fixtures Created

Go 标准 `testing.T` 模式，使用 `t.TempDir()` 自动清理。无需额外 fixture 文件。

---

## Mock Requirements

### IPC Server Mock

无需外部 mock。测试使用 `setupTestServer()` 创建真实的内存 kernel + IPC server 实例。

---

## Required data-testid Attributes

不适用 — 纯后端 Go 项目，无 UI 组件。

---

## Implementation Checklist

### Test: TestNewSnapshotRestorer + RestoreContext tests (14.4-FORK-001 to 008)

**File:** `debug/fork_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `debug/fork.go` 文件
- [ ] 定义 `ForkMessage` 结构体 (Role, Content, ToolCallID)
- [ ] 定义 `ForkContext` 结构体 (OriginalPID, SeqNum, Intent, SystemPrompt, Messages)
- [ ] 定义 `SnapshotRestorer` 结构体 (reader *RecordReader)
- [ ] 实现 `NewSnapshotRestorer(reader *RecordReader) *SnapshotRestorer`
- [ ] 实现 `RestoreContext(seqNum uint64) (*ForkContext, error)`
  - 使用 SnapshotFinder 找到 seqNum 之前最近的 context_snapshot
  - 从 Spawn 事件提取 intent
  - 从 snapshot 恢复消息列表和 system prompt
  - 记录 OriginalPID 和 SeqNum
- [ ] 运行测试: `go test ./debug/ -run TestSnapshotRestorer -v`
- [ ] 所有 8 个测试通过 (green phase)

### Test: ForkContext modification tests (14.4-FORK-009 to 018)

**File:** `debug/fork_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `ForkContext.SetSystemPrompt(prompt string)`
- [ ] 实现 `ForkContext.AppendMessage(role, content string)`
- [ ] 实现 `ForkContext.RemoveLastMessages(n int)`
- [ ] 实现 `ForkContext.ReplaceLastMessage(content string)`
- [ ] 实现 `ForkContext.Summary() string`
- [ ] 添加 JSON tags 到 ForkContext 和 ForkMessage
- [ ] 运行测试: `go test ./debug/ -run TestForkContext -v`
- [ ] 所有 10 个测试通过 (green phase)

### Test: ReplaySession Fork/ForkAt tests (14.4-REPLAY-001 to 007)

**File:** `debug/replay_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `debug/replay.go` 中新增 `Fork() (*ForkContext, error)` 方法
  - 验证 cursor >= 0
  - 使用 SnapshotRestorer 从当前 cursor 位置恢复上下文
  - 不改变 cursor 位置
- [ ] 新增 `ForkAt(seqNum uint64) (*ForkContext, error)` 方法
  - 临时 goto seqNum 恢复上下文
  - 不改变 cursor 位置
- [ ] 运行测试: `go test ./debug/ -run TestReplaySession_Fork -v`
- [ ] 所有 7 个测试通过 (green phase)

### Test: IPC fork_continue tests (14.4-IPC-001 to 005)

**File:** `ipc/server_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `ipc/protocol.go` 新增 `MethodForkContinue Method = "fork_continue"`
- [ ] 定义 `ForkContinueRequest` 结构体 (Intent, SystemPrompt, Messages, OriginalPID)
- [ ] 定义 `ForkMessageWire` 结构体 (Role, Content, ToolCallID)
- [ ] 定义 `ForkContinueResponse` 结构体 (PID, PPID)
- [ ] 在 `ipc/server.go` 实现 `handleForkContinue` 处理函数
  - CtxAlloc → SetSystemPrompt → AppendMessage 序列
  - Spawn 新进程 (ParentPID 指向 OriginalPID)
  - 处理 OriginalPID 不存在的情况
- [ ] 在 `ipc/client.go` 新增 `ForkContinue(ctx ForkContext) (types.PID, error)`
- [ ] 运行测试: `go test ./ipc/ -run TestServer_ForkContinue -v`
- [ ] 所有 5 个测试通过 (green phase)

### Test: CLI fork command tests (14.4-CLI-001 to 003)

**File:** `cmd/rnix/replay_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/rnix/replay.go` 的交互式命令循环中新增 `fork` 命令分支
- [ ] 实现 fork 子模式循环 (set prompt, append, remove, replace, show, continue, cancel)
- [ ] 在 `printReplayHelp` 中添加 fork/continue 命令说明
- [ ] 运行测试: `go test ./cmd/rnix/ -run TestReplayCommand_Fork -v`
- [ ] 所有 3 个测试通过 (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story
go test ./debug/ ./ipc/ ./cmd/rnix/ -run "14.4|Fork|ForkContinue" -v

# Run specific test file
go test ./debug/ -run TestSnapshotRestorer -v
go test ./debug/ -run TestForkContext -v
go test ./debug/ -run TestReplaySession_Fork -v
go test ./ipc/ -run TestServer_ForkContinue -v
go test ./cmd/rnix/ -run TestReplayCommand_Fork -v

# Run all debug package tests
go test ./debug/ -v

# Run with race detector
go test ./debug/ ./ipc/ -race -v

# Run tests with coverage
go test ./debug/ ./ipc/ ./cmd/rnix/ -coverprofile=coverage.out -v
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All tests written and failing (compile errors expected)
- Test helpers and data builders created
- Test follows existing project patterns (Go standard testing.T)
- Implementation checklist created

**Verification:**

- All tests fail due to missing types/methods (compile errors)
- Failure messages are clear: undefined types (SnapshotRestorer, ForkContext, etc.)
- Tests fail due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Pick one failing test group** from implementation checklist
2. **Read the tests** to understand expected behavior
3. **Implement minimal code** to make tests pass
4. **Run tests** to verify green
5. **Move to next group** and repeat

**Suggested Order:**

1. ForkMessage + ForkContext 数据结构 (debug/fork.go)
2. SnapshotRestorer (debug/fork.go)
3. ReplaySession.Fork/ForkAt (debug/replay.go)
4. IPC 协议 + Server handler (ipc/protocol.go, ipc/server.go, ipc/client.go)
5. CLI fork 子模式 (cmd/rnix/replay.go)

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Review code for consistency with existing patterns
2. Ensure error messages are clear and actionable
3. Verify all tests pass with `-race` flag
4. No unnecessary abstractions

---

## Next Steps

1. **Review this checklist** with team
2. **Run failing tests** to confirm RED phase: `go test ./debug/ ./ipc/ ./cmd/rnix/ -run "Fork" -v`
3. **Begin implementation** using implementation checklist as guide
4. **Work one test group at a time** (red -> green)
5. **When all tests pass**, refactor code for quality
6. **When refactoring complete**, update story status to 'done'

---

## Knowledge Base References Applied

- **data-factories.md** - Go test helper patterns (factory functions with overrides)
- **test-quality.md** - Deterministic, isolated, explicit test design
- **test-levels-framework.md** - Unit vs Integration test level selection
- **test-healing-patterns.md** - Error message patterns for clear failure diagnosis

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./debug/ ./ipc/ ./cmd/rnix/ -run "Fork" -v`

**Expected Results:**

- Total tests: 33
- Passing: 0 (expected - compile errors)
- Failing: 33 (expected - types/methods not defined)
- Status: RED phase verified

**Expected Failure Messages:**

- `undefined: NewSnapshotRestorer` — SnapshotRestorer 未实现
- `undefined: ForkContext` — ForkContext 结构体未定义
- `undefined: ForkMessage` — ForkMessage 结构体未定义
- `undefined: MethodForkContinue` — IPC 方法未注册
- `undefined: ForkContinueRequest` — IPC 请求类型未定义
- `undefined: ForkContinueResponse` — IPC 响应类型未定义
- `undefined: ForkMessageWire` — IPC 消息 wire 类型未定义

---

## Notes

- 本项目是纯后端 Go 项目（detected_stack=backend），无需 E2E、Playwright 或浏览器测试
- 测试遵循项目现有模式：`testing.T`、`t.TempDir()`、`t.Helper()`
- IPC 测试使用 `setupTestServer()` 创建真实内存服务器实例
- fork 操作是纯本地操作（无 IPC），continue 需要 daemon（通过 IPC）
- 上下文重建使用 ContextSnapshotData，而非 syscall 事件中的 Args（Args 不含消息内容）

---

**Generated by BMad TEA Agent** - 2026-03-08
