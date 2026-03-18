---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-18'
storyId: '3-6'
storyTitle: 'Step Output Streaming'
detectedStack: backend
generationMode: ai-generation
executionMode: sequential
tddPhase: RED
inputDocuments:
  - _bmad-output/implementation-artifacts/3-6-step-output-streaming.md
  - kernel/kernel.go (KernelCallbacks interface, reasonStep action branches)
  - ipc/protocol.go (ProgressPayload struct)
  - ipc/server.go (callbackMux implementation)
  - internal/ui/progress.go (AgentStep rendering)
  - cmd/rnix/main.go (SpawnAndWatch callback)
  - kernel/atdd_3_5_config_resolve_strace_test.go (prior ATDD pattern)
  - internal/ui/atdd_3_5_config_resolve_traceline_test.go (prior UI ATDD pattern)
---

# ATDD Checklist — Story 3.6: Step Output Streaming

## Step 1: Preflight & Context

- **Stack**: `backend` (Go 1.26, `go.mod`)
- **Test Framework**: Go `testing` + `go test -race`
- **Prerequisites**: Story approved, AC clear, test framework configured
- **Existing Patterns**: ATDD files follow `atdd_{story}_{description}_test.go` naming

## Step 2: Generation Mode

- **Mode**: AI Generation (backend project, no browser recording needed)
- **Rationale**: AC are clear, scenarios are standard callback/rendering patterns

## Step 3: Test Strategy

### AC → Test Level Mapping

| AC | Description | Test Level | Priority | Test File |
|----|------------|-----------|----------|-----------|
| AC1 | tool_call 步骤摘要 | Unit (kernel callback + UI render) | P0 | `kernel/atdd_3_6_*`, `internal/ui/atdd_3_6_*` |
| AC2 | plan 步骤摘要 | Unit (kernel callback + UI render) | P0 | `kernel/atdd_3_6_*`, `internal/ui/atdd_3_6_*` |
| AC3 | spawn 步骤摘要 | Unit (kernel callback + UI render) | P0 | `kernel/atdd_3_6_*`, `internal/ui/atdd_3_6_*` |
| AC4 | quiet 模式静默 | Unit (UI render) | P1 | `internal/ui/atdd_3_6_*` |
| AC5 | JSON 模式结构化 | Unit (IPC protocol + UI render) | P1 | `ipc/atdd_3_6_*`, `internal/ui/atdd_3_6_*` |
| AC6 | 所有测试通过 | Meta — covered by all tests | P0 | — |

### Test Layer Architecture

```
kernel/atdd_3_6_step_output_streaming_test.go     ← Kernel 层：OnStepComplete 回调触发
internal/ui/atdd_3_6_step_output_streaming_test.go ← UI 层：AgentStepComplete 渲染 + 模式静默
ipc/atdd_3_6_step_output_streaming_test.go         ← IPC 层：ProgressPayload 扩展 + callbackMux
```

## Step 4: Generated Tests (RED Phase)

### File 1: `kernel/atdd_3_6_step_output_streaming_test.go`

| Test | AC | Status | Description |
|------|-----|--------|-------------|
| `TestATDD_3_6_AC1_OnStepComplete_ToolCall` | AC1 | RED (timeout) | 验证 tool_call 后 OnStepComplete 被调用，summary 包含 toolPath |
| `TestATDD_3_6_AC2_OnStepComplete_Plan` | AC2 | RED (timeout) | 验证 plan 后 OnStepComplete 被调用，summary = "plan (3 steps)" |
| `TestATDD_3_6_AC3_OnStepComplete_Spawn` | AC3 | RED (timeout) | 验证 spawn 后 OnStepComplete 被调用，summary 包含 child PID |
| `TestATDD_3_6_AC1_OnStepComplete_Complete_EmptySummary` | AC1 | RED (timeout) | 验证 complete/text action 的空 summary |
| `TestATDD_3_6_AC1_OnStepComplete_ToolCall_SummaryTruncation` | AC1 | RED (timeout) | 验证 >60 字符的 briefResult 被截断并追加 "..." |

**RED 原因**: kernel 尚未在 reasonStep 中调用 `OnStepComplete`，`waitForAction()` 超时。

### File 2: `internal/ui/atdd_3_6_step_output_streaming_test.go`

| Test | AC | Status | Description |
|------|-----|--------|-------------|
| `TestATDD_3_6_AC1_AgentStepComplete_ToolCall_DefaultMode` | AC1 | RED (compile) | Default 模式输出 "[agent/1] step 2: ..." |
| `TestATDD_3_6_AC2_AgentStepComplete_Plan_DefaultMode` | AC2 | RED (compile) | Default 模式输出 "plan (3 steps)" |
| `TestATDD_3_6_AC3_AgentStepComplete_Spawn_DefaultMode` | AC3 | RED (compile) | Default 模式输出 spawn 摘要 |
| `TestATDD_3_6_AC1_AgentStepComplete_EmptySummary` | AC1 | RED (compile) | 空 summary 时不输出 " → " |
| `TestATDD_3_6_AC4_AgentStepComplete_QuietMode` | AC4 | RED (compile) | Quiet 模式无输出 |
| `TestATDD_3_6_AC5_AgentStepComplete_JSONMode` | AC5 | RED (compile) | JSON 模式无输出（由 cmd 层处理） |
| `TestATDD_3_6_AC1_AgentStepComplete_VerboseMode` | AC1 | RED (compile) | Verbose 模式正常输出 |
| `TestATDD_3_6_AgentStep_DefaultMode_Silent` | Task7 | RED (runtime) | AgentStep 默认模式改为静默 |
| `TestATDD_3_6_AgentStep_VerboseMode_StillOutputs` | Task7 | GREEN (existing) | AgentStep verbose 模式保留输出 |

**RED 原因**: `ProgressReporter.AgentStepComplete` 方法不存在，编译失败。

### File 3: `ipc/atdd_3_6_step_output_streaming_test.go`

| Test | AC | Status | Description |
|------|-----|--------|-------------|
| `TestATDD_3_6_AC1_CallbackMux_OnStepComplete_SendsEvent` | AC1 | RED (compile) | callbackMux 发送包含 action/summary 的 StreamEvent |
| `TestATDD_3_6_AC5_ProgressPayload_JSONSerialization` | AC5 | RED (compile) | ProgressPayload 序列化包含 action/summary |
| `TestATDD_3_6_AC5_ProgressPayload_OmitEmptySummary` | AC5 | RED (compile) | 空 summary 被 omitempty 省略 |
| `TestATDD_3_6_CallbackMux_ImplementsKernelCallbacks` | — | RED (compile) | 编译时接口满足检查 |

**RED 原因**: `callbackMux.OnStepComplete` 不存在、`ProgressPayload.Action/Summary` 字段不存在。

## RED Phase Verification

```
$ go vet ./kernel/...        → OK (compiles, tests fail at runtime: timeout)
$ go vet ./internal/ui/...   → FAIL: AgentStepComplete undefined
$ go vet ./ipc/...           → FAIL: OnStepComplete undefined, Action/Summary undefined
$ go test -run TestATDD_3_6_AC1_OnStepComplete_Complete_EmptySummary ./kernel/...
  → FAIL: "timed out — OnStepComplete never called"
```

## Implementation Checklist (for dev agent)

When implementing Story 3.6, making these tests pass requires:

- [ ] `kernel/kernel.go`: Add `OnStepComplete(pid, step, action, summary)` to `KernelCallbacks` interface
- [ ] `kernel/kernel.go`: Call `OnStepComplete` in each action branch of `reasonStep`
- [ ] `kernel/kernel.go`: Generate summary strings per action type (truncation for tool_call)
- [ ] `ipc/protocol.go`: Add `Action` and `Summary` fields to `ProgressPayload`
- [ ] `ipc/server.go`: Implement `callbackMux.OnStepComplete`
- [ ] `internal/ui/progress.go`: Add `AgentStepComplete` method to `ProgressReporter`
- [ ] `internal/ui/progress.go`: Change `AgentStep` to only output in verbose mode
- [ ] `cmd/rnix/main.go`: Add `OnStepComplete` to `cliCallbacks`
- [ ] `cmd/rnix/main.go`: Handle `"step_complete"` event in `SpawnAndWatch` callback
- [ ] All `KernelCallbacks` implementors: Add `OnStepComplete` method (mocks, compose, etc.)
