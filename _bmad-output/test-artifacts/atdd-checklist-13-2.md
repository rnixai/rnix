---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-07'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/13-2-breakpoint-system.md'
  - 'kernel/process.go'
  - 'kernel/signal_test.go'
  - 'kernel/kernel.go'
  - 'ipc/protocol.go'
  - 'ipc/protocol_test.go'
  - 'ipc/server_test.go'
  - 'ipc/integration_test.go'
  - 'ipc/client.go'
  - 'ipc/server.go'
  - 'cmd/rnix/gdb.go'
---

# ATDD Checklist - Epic 13, Story 2: 断点系统

**Date:** 2026-03-07
**Author:** Decker
**Primary Test Level:** Unit + Integration (Kernel + IPC)

---

## Story Summary

实现 gdb 调试器的断点系统，支持四种断点类型（syscall/推理/质量/预算），在 kernel 的 reasonStep 循环和 emitEvent 中植入检查钩子，通过 IPC 协议传输断点命令，在 gdb CLI 中提供交互式断点管理。

**As a** 平台构建者
**I want** 在 gdb 中设置四种断点（syscall/推理/质量/预算），精确控制智能体暂停的时机
**So that** 我可以在关键执行节点检查智能体状态，定位问题根因

---

## Acceptance Criteria

1. **AC1**: Given 用户处于 gdb 调试会话中, When 用户执行 `break syscall Read`, Then 智能体在下次调用 Read syscall 前暂停，显示 syscall 参数
2. **AC2**: Given 用户处于 gdb 调试会话中, When 用户执行 `break reasoning`, Then 智能体在每次 LLM 调用前暂停，显示即将发送的 prompt 摘要
3. **AC3**: Given 用户设置质量断点 `break quality --pattern "安全漏洞"`, When 智能体输出包含匹配关键词, Then 智能体暂停，高亮显示匹配内容
4. **AC4**: Given 用户设置质量断点 `break quality --eval "输出必须包含代码示例"`, When 智能体输出经评估不满足标准, Then 智能体暂停，显示评估结果和不满足原因
5. **AC5**: Given 用户设置预算断点 `break budget 5000`, When 智能体 token 消耗达到 5000, Then 智能体暂停，显示当前 token 消耗明细, And 断点触发到暂停延迟 <= 100ms (NFR31)

---

## Failing Tests Created (RED Phase)

### Unit Tests — Breakpoint 数据模型 (26 tests)

**File:** `kernel/breakpoint_test.go` (新增约 460 行)

- **Test:** `TestBreakpointType_Constants` (13.2-UNIT-001)
  - **Status:** RED - `BreakpointType`/`BPSyscall`/`BPReasoning`/`BPQuality`/`BPBudget` 未定义
  - **Verifies:** AC1-5 - 四种断点类型枚举存在且互不相同
  - **Priority:** P0

- **Test:** `TestBreakpoint_Fields` (13.2-UNIT-002)
  - **Status:** RED - `Breakpoint` 结构体未定义
  - **Verifies:** AC1-5 - Breakpoint 包含 ID、Type、Enabled、HitCount 字段
  - **Priority:** P0

- **Test:** `TestBreakpoint_SyscallCondition` (13.2-UNIT-003a)
  - **Status:** RED - `SyscallCondition` 未定义
  - **Verifies:** AC1 - SyscallCondition 实现 BreakpointCondition 接口
  - **Priority:** P0

- **Test:** `TestBreakpoint_ReasoningCondition` (13.2-UNIT-003b)
  - **Status:** RED - `ReasoningCondition` 未定义
  - **Verifies:** AC2 - ReasoningCondition 实现 BreakpointCondition 接口
  - **Priority:** P0

- **Test:** `TestBreakpoint_QualityPatternCondition` (13.2-UNIT-003c)
  - **Status:** RED - `QualityCondition`/`QualityModePattern` 未定义
  - **Verifies:** AC3 - QualityCondition --pattern 模式
  - **Priority:** P0

- **Test:** `TestBreakpoint_QualityEvalCondition` (13.2-UNIT-003d)
  - **Status:** RED - `QualityCondition`/`QualityModeEval` 未定义
  - **Verifies:** AC4 - QualityCondition --eval 模式
  - **Priority:** P0

- **Test:** `TestBreakpoint_BudgetCondition` (13.2-UNIT-003e)
  - **Status:** RED - `BudgetCondition` 未定义
  - **Verifies:** AC5 - BudgetCondition 包含 Threshold 字段
  - **Priority:** P0

- **Test:** `TestProcess_AddBreakpoint` (13.2-UNIT-004)
  - **Status:** RED - `Process.AddBreakpoint` 未定义
  - **Verifies:** AC1-5 - AddBreakpoint 返回正的断点 ID
  - **Priority:** P0

- **Test:** `TestProcess_AddBreakpoint_IncrementingIDs` (13.2-UNIT-005)
  - **Status:** RED - `Process.AddBreakpoint` 未定义
  - **Verifies:** AC1-5 - 连续添加断点 ID 递增
  - **Priority:** P0

- **Test:** `TestProcess_RemoveBreakpoint` (13.2-UNIT-006)
  - **Status:** RED - `Process.RemoveBreakpoint` 未定义
  - **Verifies:** AC1-5 - 删除存在的断点返回 true
  - **Priority:** P0

- **Test:** `TestProcess_RemoveBreakpoint_NotFound` (13.2-UNIT-007)
  - **Status:** RED - `Process.RemoveBreakpoint` 未定义
  - **Verifies:** AC1-5 - 删除不存在的断点返回 false
  - **Priority:** P0

- **Test:** `TestProcess_ListBreakpoints` (13.2-UNIT-008)
  - **Status:** RED - `Process.ListBreakpoints` 未定义
  - **Verifies:** AC1-5 - 列出所有断点
  - **Priority:** P0

- **Test:** `TestProcess_ListBreakpoints_ReturnsCopy` (13.2-UNIT-009)
  - **Status:** RED - `Process.ListBreakpoints` 未定义
  - **Verifies:** AC1-5 - 返回独立副本
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_Syscall` (13.2-UNIT-010)
  - **Status:** RED - `Process.CheckBreakpoint`/`BreakpointContext` 未定义
  - **Verifies:** AC1 - Syscall 断点名称匹配
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_Reasoning` (13.2-UNIT-011)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC2 - Reasoning 断点匹配
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_QualityPattern` (13.2-UNIT-012)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC3 - Quality --pattern 正则匹配
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_QualityEval` (13.2-UNIT-013)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC4 - Quality --eval 内容包含检查
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_Budget` (13.2-UNIT-014)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC5 - Budget 阈值匹配
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_DisabledSkipped` (13.2-UNIT-015)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC1-5 - 禁用断点不触发
  - **Priority:** P1

- **Test:** `TestProcess_CheckBreakpoint_IncrementHitCount` (13.2-UNIT-016)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC1-5 - 命中时 HitCount 递增
  - **Priority:** P1

- **Test:** `TestProcess_GdbPause_GdbResume` (13.2-UNIT-017)
  - **Status:** RED - `Process.GdbPause`/`Process.GdbResume`/`Process.IsGdbPaused`/`Process.GdbPauseCh` 未定义
  - **Verifies:** AC1-5 - GdbPause 阻塞，GdbResume 解除阻塞
  - **Priority:** P0

- **Test:** `TestProcess_GdbResume_NotPaused` (13.2-UNIT-018)
  - **Status:** RED - `Process.GdbResume` 未定义
  - **Verifies:** AC1-5 - 未暂停时 GdbResume 为 noop
  - **Priority:** P1

- **Test:** `TestProcess_GdbPauseCh_NilWhenNotPaused` (13.2-UNIT-019)
  - **Status:** RED - `Process.GdbPauseCh` 未定义
  - **Verifies:** AC1-5 - 未暂停时 GdbPauseCh 返回 nil
  - **Priority:** P1

- **Test:** `TestProcess_GdbPause_IndependentOfSignalPause` (13.2-UNIT-020)
  - **Status:** RED - `Process.GdbPause`/`Process.IsGdbPaused` 未定义
  - **Verifies:** AC1-5 - gdb 暂停与 Signal 暂停互不干扰
  - **Priority:** P0

- **Test:** `TestProcess_Breakpoint_Concurrent` (13.2-UNIT-021)
  - **Status:** RED - 所有断点方法未定义
  - **Verifies:** AC1-5 - 100 goroutine 并发安全
  - **Priority:** P1

- **Test:** `TestProcess_GdbPause_EmitsDebugEvent` (13.2-UNIT-022)
  - **Status:** RED - `Process.GdbPause` 未定义
  - **Verifies:** AC1-5 - GdbPause 发送 gdb_prompt 事件到 DebugChan
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_NoBreakpoints` (13.2-UNIT-023)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC1-5 - 无断点时 O(1) 跳过
  - **Priority:** P0

- **Test:** `TestProcess_CheckBreakpoint_AfterRemove` (13.2-UNIT-024)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC1-5 - 删除后不再匹配
  - **Priority:** P1

- **Test:** `TestBreakpointContext_Fields` (13.2-UNIT-025)
  - **Status:** RED - `BreakpointContext` 未定义
  - **Verifies:** AC1-5 - BreakpointContext 包含所有必要字段
  - **Priority:** P1

- **Test:** `TestQualityMode_Constants` (13.2-UNIT-026)
  - **Status:** RED - `QualityModePattern`/`QualityModeEval` 未定义
  - **Verifies:** AC3-4 - QualityMode 枚举值互不相同
  - **Priority:** P2

- **Test:** `TestProcess_CheckBreakpoint_PerformanceSub100ms` (13.2-PERF-001)
  - **Status:** RED - `Process.CheckBreakpoint` 未定义
  - **Verifies:** AC5 NFR31 - 10 个断点下检查延迟 <= 100ms
  - **Priority:** P1

### IPC Protocol Tests (8 tests)

**File:** `ipc/protocol_test.go` (新增约 160 行)

- **Test:** `TestMethodGdbCommand_Exists` (13.2-IPC-001)
  - **Status:** RED - `MethodGdbCommand` 未定义
  - **Verifies:** AC1-5 - MethodGdbCommand 常量存在且唯一
  - **Priority:** P0

- **Test:** `TestGdbCommandRequest_MarshalRoundTrip` (13.2-IPC-002)
  - **Status:** RED - `GdbCommandRequest` 未定义
  - **Verifies:** AC1-5 - GdbCommandRequest 包含 PID、Command、Args 字段
  - **Priority:** P0

- **Test:** `TestGdbCommandResponse_MarshalRoundTrip` (13.2-IPC-003)
  - **Status:** RED - `GdbCommandResponse` 未定义
  - **Verifies:** AC1-5 - GdbCommandResponse 包含 OK、Message、Data 字段
  - **Priority:** P0

- **Test:** `TestGdbCommandResponse_ErrorCase` (13.2-IPC-004)
  - **Status:** RED - `GdbCommandResponse` 未定义
  - **Verifies:** AC1-5 - GdbCommandResponse 错误场景
  - **Priority:** P1

- **Test:** `TestGdbCommandRequest_IPCEnvelope` (13.2-IPC-005)
  - **Status:** RED - `MethodGdbCommand`/`GdbCommandRequest` 未定义
  - **Verifies:** AC1-5 - GdbCommandRequest 能包装在 IPC Request envelope
  - **Priority:** P0

- **Test:** `TestStreamGdbPrompt_Exists` (13.2-IPC-006)
  - **Status:** RED - `StreamGdbPrompt` 未定义
  - **Verifies:** AC1-5 - StreamGdbPrompt 事件类型存在且唯一
  - **Priority:** P0

- **Test:** `TestGdbCommandRequest_ArgsOmitEmpty` (13.2-IPC-007)
  - **Status:** RED - `GdbCommandRequest` 未定义
  - **Verifies:** AC1-5 - Args 为空时 omitempty
  - **Priority:** P1

- **Test:** `TestGdbCommandResponse_DataOmitEmpty` (13.2-IPC-008)
  - **Status:** RED - `GdbCommandResponse` 未定义
  - **Verifies:** AC1-5 - Data 为空时 omitempty
  - **Priority:** P1

### Server Tests (6 tests)

**File:** `ipc/server_test.go` (新增约 180 行)

- **Test:** `TestServer_GdbCommand_NotFound` (13.2-SRV-001)
  - **Status:** RED - `MethodGdbCommand`/`GdbCommandRequest` 未定义
  - **Verifies:** AC1-5 - 不存在 PID 返回 NOT_FOUND
  - **Priority:** P0

- **Test:** `TestServer_GdbCommand_BreakSyscall` (13.2-SRV-002)
  - **Status:** RED - `MethodGdbCommand`/`GdbCommandRequest`/`GdbCommandResponse` 未定义
  - **Verifies:** AC1 - Server 处理 break syscall 命令
  - **Priority:** P0

- **Test:** `TestServer_GdbCommand_Continue` (13.2-SRV-003)
  - **Status:** RED - `MethodGdbCommand`/`GdbCommandRequest` 未定义
  - **Verifies:** AC1-5 - Server 处理 continue 命令
  - **Priority:** P0

- **Test:** `TestServer_GdbCommand_Info` (13.2-SRV-004)
  - **Status:** RED - `MethodGdbCommand`/`GdbCommandRequest` 未定义
  - **Verifies:** AC1-5 - Server 处理 info 命令
  - **Priority:** P1

- **Test:** `TestServer_GdbCommand_Delete` (13.2-SRV-005)
  - **Status:** RED - `MethodGdbCommand`/`GdbCommandRequest` 未定义
  - **Verifies:** AC1-5 - Server 处理 delete 命令
  - **Priority:** P1

- **Test:** `TestServer_GdbCommand_InvalidPayload` (13.2-SRV-006)
  - **Status:** RED - `MethodGdbCommand` 未定义
  - **Verifies:** AC1-5 - 无效 payload 优雅处理
  - **Priority:** P1

### Integration Tests (9 tests)

**File:** `ipc/integration_test.go` (新增约 210 行)

- **Test:** `TestIntegration_GdbCommand_BreakSyscall` (13.2-INT-001)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC1 - 通过 IPC 设置 syscall 断点
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_BreakReasoning` (13.2-INT-002)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC2 - 通过 IPC 设置 reasoning 断点
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_BreakBudget` (13.2-INT-003)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC5 - 通过 IPC 设置 budget 断点
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_BreakQualityPattern` (13.2-INT-004)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC3 - 通过 IPC 设置 quality --pattern 断点
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_Delete` (13.2-INT-005)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC1-5 - 通过 IPC 删除断点
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_Continue` (13.2-INT-006)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC1-5 - 通过 IPC 发送 continue 恢复暂停
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_InfoBreakpoints` (13.2-INT-007)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC1-5 - 通过 IPC 查询断点列表
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_IndependentConnection` (13.2-INT-008)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC1-5 - GdbCommand 走独立连接不阻塞 attach 事件流
  - **Priority:** P0

- **Test:** `TestIntegration_GdbCommand_NotFound` (13.2-INT-009)
  - **Status:** RED - `client.SendGdbCommand` 未定义
  - **Verifies:** AC1-5 - 不存在的 PID 返回错误
  - **Priority:** P1

### CLI Tests (15 tests)

**File:** `cmd/rnix/gdb_test.go` (新增约 185 行)

- **Test:** `TestParseBreakCommand_Syscall` (13.2-CLI-001)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC1 - 解析 "break syscall Read"
  - **Priority:** P0

- **Test:** `TestParseBreakCommand_Reasoning` (13.2-CLI-002)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC2 - 解析 "break reasoning"
  - **Priority:** P0

- **Test:** `TestParseBreakCommand_QualityPattern` (13.2-CLI-003)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC3 - 解析 "break quality --pattern ..."
  - **Priority:** P0

- **Test:** `TestParseBreakCommand_QualityEval` (13.2-CLI-004)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC4 - 解析 "break quality --eval ..."
  - **Priority:** P0

- **Test:** `TestParseBreakCommand_Budget` (13.2-CLI-005)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC5 - 解析 "break budget 5000"
  - **Priority:** P0

- **Test:** `TestParseBreakCommand_NoArgs` (13.2-CLI-006)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC1-5 - 无参数返回错误
  - **Priority:** P1

- **Test:** `TestParseBreakCommand_UnknownSubtype` (13.2-CLI-007)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC1-5 - 未知子类型返回错误
  - **Priority:** P1

- **Test:** `TestParseBreakCommand_SyscallNoName` (13.2-CLI-008)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC1 - "break syscall" 无名称返回错误
  - **Priority:** P1

- **Test:** `TestParseBreakCommand_BudgetNoValue` (13.2-CLI-009)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC5 - "break budget" 无值返回错误
  - **Priority:** P1

- **Test:** `TestParseBreakCommand_BudgetInvalidNumber` (13.2-CLI-010)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC5 - "break budget abc" 无效数字返回错误
  - **Priority:** P1

- **Test:** `TestParseBreakCommand_QualityNoFlag` (13.2-CLI-011)
  - **Status:** RED - `parseBreakCommand` 未定义
  - **Verifies:** AC3-4 - "break quality" 无 flag 返回错误
  - **Priority:** P1

- **Test:** `TestParseDeleteCommand` (13.2-CLI-012)
  - **Status:** RED - `parseDeleteCommand` 未定义
  - **Verifies:** AC1-5 - 解析 "delete 1"
  - **Priority:** P0

- **Test:** `TestParseDeleteCommand_NoArgs` (13.2-CLI-013)
  - **Status:** RED - `parseDeleteCommand` 未定义
  - **Verifies:** AC1-5 - 无参数返回错误
  - **Priority:** P1

- **Test:** `TestParseDeleteCommand_InvalidID` (13.2-CLI-014)
  - **Status:** RED - `parseDeleteCommand` 未定义
  - **Verifies:** AC1-5 - 非数字返回错误
  - **Priority:** P1

- **Test:** `TestBreakCommandResult_Fields` (13.2-CLI-015)
  - **Status:** RED - `BreakCommandResult` 未定义
  - **Verifies:** AC1-5 - BreakCommandResult 结构体字段完整
  - **Priority:** P0

---

## Mock Requirements

N/A -- 本 story 使用真实的 kernel + IPC server 进行集成测试（复用 `setupIntegrationServer` 和 `setupTestServer` helper 函数），不需要外部服务 mock。

---

## Implementation Checklist

### Task 1: 断点数据模型与注册表 (13.2-UNIT-001~016, 023~026)

**File:** `kernel/breakpoint.go` (新建)、`kernel/process.go` (修改)

**Tasks to make these tests pass:**

- [ ] 定义 `BreakpointType` 枚举: `BPSyscall`、`BPReasoning`、`BPQuality`、`BPBudget`
- [ ] 定义 `BreakpointCondition` 接口
- [ ] 定义条件结构体: `SyscallCondition`、`ReasoningCondition`、`QualityCondition`、`BudgetCondition`
- [ ] 定义 `QualityMode` 枚举: `QualityModePattern`、`QualityModeEval`
- [ ] 定义 `Breakpoint` 结构体 (ID, Type, Condition, Enabled, HitCount)
- [ ] 定义 `BreakpointContext` 结构体 (BPType, SyscallName, SyscallArgs, LLMResponse, TokensUsed, StepNumber)
- [ ] 在 `Process` 中新增 `breakpoints []*Breakpoint` 和 `bpIDCounter int` 字段
- [ ] 实现 `Process.AddBreakpoint(bp *Breakpoint) int`
- [ ] 实现 `Process.RemoveBreakpoint(id int) bool`
- [ ] 实现 `Process.ListBreakpoints() []*Breakpoint`
- [ ] 实现 `Process.CheckBreakpoint(ctx BreakpointContext) *Breakpoint`
- [ ] Run: `go test ./kernel/ -run 'TestBreakpoint|TestProcess_AddBreakpoint|TestProcess_RemoveBreakpoint|TestProcess_ListBreakpoints|TestProcess_CheckBreakpoint|TestQualityMode' -v`

### Task 2: GdbPause/GdbResume 暂停机制 (13.2-UNIT-017~022)

**File:** `kernel/process.go` (修改)

**Tasks to make these tests pass:**

- [ ] 在 `Process` 中新增 `gdbPauseCh chan struct{}` 字段
- [ ] 实现 `Process.GdbPause(reason string, hitBP *Breakpoint)`: 发送 GdbPause 事件到 DebugChan，设置 gdbPauseCh 阻塞
- [ ] 实现 `Process.GdbResume()`: close(gdbPauseCh) 解除阻塞
- [ ] 实现 `Process.IsGdbPaused() bool`
- [ ] 实现 `Process.GdbPauseCh() <-chan struct{}`
- [ ] 确保 gdb 暂停与 Signal 暂停 (resumeCh) 互不干扰
- [ ] Run: `go test ./kernel/ -run 'TestProcess_GdbPause|TestProcess_GdbResume' -v`

### Task 3: IPC 协议扩展 (13.2-IPC-001~008)

**File:** `ipc/protocol.go` (修改)

**Tasks to make these tests pass:**

- [ ] 新增 `MethodGdbCommand Method = "gdb_command"`
- [ ] 定义 `GdbCommandRequest` 结构体: `{PID, Command, Args}`
- [ ] 定义 `GdbCommandResponse` 结构体: `{OK, Message, Data}`
- [ ] 新增 `StreamGdbPrompt StreamEventType = "gdb_prompt"`
- [ ] Run: `go test ./ipc/ -run 'TestMethodGdbCommand|TestGdbCommandR|TestStreamGdbPrompt' -v`

### Task 4: Server GdbCommand Handler (13.2-SRV-001~006)

**File:** `ipc/server.go` (修改)

**Tasks to make these tests pass:**

- [ ] 在 `handleConn` switch 中增加 `MethodGdbCommand` 分支
- [ ] 实现 `handleGdbCommand` 方法:
  - "break" → `proc.AddBreakpoint(parsedBP)` → 返回 bp_id
  - "delete" → `proc.RemoveBreakpoint(id)` → 返回 ok/not_found
  - "continue" → `proc.GdbResume()` → 返回 ok
  - "info" → `proc.ListBreakpoints()` → 返回 bp 列表
- [ ] GdbCommand 走独立连接（与 SendDetach 相同模式）
- [ ] Run: `go test ./ipc/ -run 'TestServer_GdbCommand' -v`

### Task 5: Client SendGdbCommand 方法 (13.2-INT-001~009)

**File:** `ipc/client.go` (修改)

**Tasks to make these tests pass:**

- [ ] 实现 `Client.SendGdbCommand(pid types.PID, cmd string, args []string) (*GdbCommandResponse, error)`
- [ ] 使用独立连接模式（参考 `SendDetach`）
- [ ] Run: `go test ./ipc/ -run 'TestIntegration_GdbCommand' -v`

### Task 6: gdb CLI 命令扩展 (13.2-CLI-001~015)

**File:** `cmd/rnix/gdb.go` (修改)

**Tasks to make these tests pass:**

- [ ] 定义 `BreakCommandResult` 结构体
- [ ] 实现 `parseBreakCommand(args []string) (BreakCommandResult, error)`:
  - "syscall <name>" → 设置 SubType/SyscallName
  - "reasoning" → 设置 SubType
  - "quality --pattern <pat>" → 设置 SubType/QualityMode/Pattern
  - "quality --eval <expr>" → 设置 SubType/QualityMode/EvalExpr
  - "budget <tokens>" → 设置 SubType/BudgetTokens
- [ ] 实现 `parseDeleteCommand(args []string) (int, error)`
- [ ] 在 gdb 命令循环中新增 break/delete/info breakpoints/continue 分支
- [ ] Run: `go test ./cmd/rnix/ -run 'TestParseBreak|TestParseDelete|TestBreakCommandResult' -v`

### Performance (13.2-PERF-001)

- [ ] Run: `go test ./kernel/ -run 'TestProcess_CheckBreakpoint_PerformanceSub100ms' -v`
- [ ] 验证 10 个断点下 CheckBreakpoint 延迟 <= 100ms

---

## Running Tests

```bash
# Run all failing tests for this story (will fail to compile until implementation)
go test ./kernel/ ./ipc/ ./cmd/rnix/ -run '13\.2|Breakpoint|GdbPause|GdbResume|GdbCommand|ParseBreak|ParseDelete|BreakCommandResult|QualityMode|GdbPrompt' -v

# Run kernel unit tests only
go test ./kernel/ -run 'TestBreakpoint|TestProcess_AddBreakpoint|TestProcess_RemoveBreakpoint|TestProcess_ListBreakpoints|TestProcess_CheckBreakpoint|TestProcess_GdbPause|TestProcess_GdbResume|TestQualityMode' -v

# Run IPC protocol tests only
go test ./ipc/ -run 'TestMethodGdbCommand|TestGdbCommandR|TestStreamGdbPrompt' -v

# Run server tests only
go test ./ipc/ -run 'TestServer_GdbCommand' -v

# Run integration tests only
go test ./ipc/ -run 'TestIntegration_GdbCommand' -v

# Run CLI tests only
go test ./cmd/rnix/ -run 'TestParseBreak|TestParseDelete|TestBreakCommandResult' -v

# Run all tests with race detection
go test ./kernel/ ./ipc/ ./cmd/rnix/ -v -race

# Run performance test
go test ./kernel/ -run 'TestProcess_CheckBreakpoint_PerformanceSub100ms' -v
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 64 tests written and failing (compile errors)
- Tests cover all 5 acceptance criteria + NFR31 性能要求
- Tests follow existing project patterns (signal_test.go, protocol_test.go, integration_test.go, server_test.go)
- No test dependencies (each test creates its own process and server)
- Tests are deterministic (no race conditions, proper channel lifecycle)
- gdb 暂停机制与 Signal 暂停独立性已测试 (UNIT-020)

**Verification:**

```
go test ./kernel/ 2>&1 | head -15
# Expected: compile failure referencing BreakpointType, Breakpoint, etc.

go test ./ipc/ 2>&1 | head -15
# Expected: compile failure referencing MethodGdbCommand, GdbCommandRequest, etc.

go test ./cmd/rnix/ 2>&1 | head -15
# Expected: compile failure referencing parseBreakCommand, etc.
```

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with Task 1** (数据模型): BreakpointType, Breakpoint, conditions, Process methods
2. **Run kernel unit tests** → 验证数据模型和条件匹配通过
3. **Implement Task 2** (暂停机制): GdbPause, GdbResume, gdbPauseCh
4. **Run kernel pause tests** → 验证暂停/恢复通过
5. **Implement Task 3** (IPC 协议): MethodGdbCommand, Request/Response types
6. **Run protocol tests** → 验证序列化通过
7. **Implement Task 4** (Server): handleGdbCommand
8. **Run server tests** → 验证 server 处理通过
9. **Implement Task 5** (Client): SendGdbCommand
10. **Run integration tests** → 验证端到端 IPC 通过
11. **Implement Task 6** (CLI): parseBreakCommand, parseDeleteCommand, 命令循环扩展
12. **Run CLI tests** → 验证命令解析通过

---

### REFACTOR Phase (After All Tests Pass)

1. Verify all 64 tests pass with `go test ./kernel/ ./ipc/ ./cmd/rnix/ -v -race`
2. Review code for quality (consistent error handling, proper goroutine cleanup)
3. Ensure breakpoint state cleanup during process reap
4. Run full test suite: `make test`

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./kernel/ ./ipc/ ./cmd/rnix/ 2>&1`

**Results:**

```
# github.com/rnixai/rnix/kernel [github.com/rnixai/rnix/kernel.test]
kernel/breakpoint_test.go: undefined: BreakpointType
kernel/breakpoint_test.go: undefined: BPSyscall
kernel/breakpoint_test.go: undefined: BPReasoning
kernel/breakpoint_test.go: undefined: BPQuality
kernel/breakpoint_test.go: undefined: BPBudget
kernel/breakpoint_test.go: undefined: Breakpoint
kernel/breakpoint_test.go: undefined: SyscallCondition
kernel/breakpoint_test.go: undefined: ReasoningCondition
kernel/breakpoint_test.go: undefined: QualityCondition
kernel/breakpoint_test.go: undefined: BudgetCondition
kernel/breakpoint_test.go: undefined: BreakpointContext
kernel/breakpoint_test.go: undefined: Process.AddBreakpoint
kernel/breakpoint_test.go: undefined: Process.RemoveBreakpoint
kernel/breakpoint_test.go: undefined: Process.ListBreakpoints
kernel/breakpoint_test.go: undefined: Process.CheckBreakpoint
kernel/breakpoint_test.go: undefined: Process.GdbPause
kernel/breakpoint_test.go: undefined: Process.GdbResume
kernel/breakpoint_test.go: undefined: Process.IsGdbPaused
kernel/breakpoint_test.go: undefined: Process.GdbPauseCh
FAIL github.com/rnixai/rnix/kernel [build failed]

# github.com/rnixai/rnix/ipc [github.com/rnixai/rnix/ipc.test]
ipc/protocol_test.go: undefined: MethodGdbCommand
ipc/protocol_test.go: undefined: GdbCommandRequest
ipc/protocol_test.go: undefined: GdbCommandResponse
ipc/protocol_test.go: undefined: StreamGdbPrompt
ipc/server_test.go: undefined: MethodGdbCommand
ipc/server_test.go: undefined: GdbCommandRequest
ipc/server_test.go: undefined: GdbCommandResponse
ipc/integration_test.go: client.SendGdbCommand undefined
FAIL github.com/rnixai/rnix/ipc [build failed]

# github.com/rnixai/rnix/cmd/rnix [github.com/rnixai/rnix/cmd/rnix.test]
cmd/rnix/gdb_test.go: undefined: parseBreakCommand
cmd/rnix/gdb_test.go: undefined: parseDeleteCommand
cmd/rnix/gdb_test.go: undefined: BreakCommandResult
FAIL github.com/rnixai/rnix/cmd/rnix [build failed]
```

**Summary:**

- Total tests: 64
- Passing: 0 (expected)
- Failing: 64 (compile errors, expected)
- Status: RED phase verified

---

## Acceptance Criteria Coverage

| AC | Description | Tests | Priority |
|----|-------------|-------|----------|
| AC1 | syscall 断点 (`break syscall Read`) | UNIT-003a,010,022, IPC-001~005, SRV-002, INT-001,008, CLI-001,008 | P0-P1 |
| AC2 | reasoning 断点 (`break reasoning`) | UNIT-003b,011,016, INT-002, CLI-002 | P0-P1 |
| AC3 | quality --pattern 断点 | UNIT-003c,012,026, INT-004, CLI-003,011 | P0-P2 |
| AC4 | quality --eval 断点 | UNIT-003d,013, CLI-004 | P0 |
| AC5 | budget 断点 + NFR31 性能 | UNIT-003e,014,PERF-001, INT-003, CLI-005,009,010 | P0-P1 |
| ALL | 断点管理 (增删查/暂停恢复) | UNIT-004~009,015~021,023~025, SRV-001~006, INT-005~009, CLI-006~007,012~015 | P0-P2 |

---

## Next Steps

1. **DEV workflow 实现** Story 13-2 的代码
2. **按 Implementation Checklist** 顺序逐个让测试通过
3. **每实现一个 Task 后运行对应测试** 验证 GREEN
4. **所有 64 个测试通过后** 运行 `make test` 全量回归
5. **REFACTOR** 清理代码后确认测试仍通过

---

## Notes

- 断点数据模型完全在 Process 内部管理 (mutex 保护)，不引入额外全局状态
- GdbPause/GdbResume 使用与 Signal Pause 完全独立的 channel 机制 (`gdbPauseCh` vs `resumeCh`)
- GdbPause 向 DebugChan 发送 "GdbPause" 事件，IPC 流将其转为 StreamGdbPrompt 通知客户端
- Budget 断点是软限制（暂停等待用户决策），与内核 `budget_exceeded` 硬限制（终止进程）区分
- Quality --eval 在 MVP 阶段使用简单字符串包含检查，不调用外部 LLM
- CLI 的 parseBreakCommand 纯函数设计便于单元测试，与 IPC 调用分离
- SendGdbCommand 走独立连接（同 SendDetach 模式），避免阻塞 attach 事件流

---

**Generated by BMad TEA Agent** - 2026-03-07
