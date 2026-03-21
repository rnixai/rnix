---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-21'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-3-watch-command-level1-realtime-stream.md
  - ipc/protocol.go
  - ipc/server.go (callbackMux L1373-1423)
  - ipc/client.go (SpawnAndWatch L108-154)
  - kernel/kernel.go (KernelCallbacks L164-172)
  - ipc/atdd_27_2_getstepdetail_test.go (pattern reference)
  - ipc/atdd_3_6_step_output_streaming_test.go (pattern reference)
  - ipc/server_test.go (test helpers)
---

# ATDD Checklist — Story 27.3: watch 命令基础 — Level 1 实时流

## Preflight

- **Stack**: `backend` (Go 1.26, go.mod detected)
- **Generation mode**: AI Generation (no browser recording)
- **Test framework**: Go standard `testing` package with `go test -race`
- **Prerequisites**: Story approved (ready-for-dev), test infra ready, 27.1/27.2 completed

## Test Strategy

| AC | Test Function | Level | Priority | Red Phase Failure |
|---|---|---|---|---|
| AC-2 | `TestATDD_27_3_AC2_MethodWatch_Constant` | Unit | P0 | `undefined: MethodWatch` |
| AC-2 | `TestATDD_27_3_AC2_WatchRequest_Serialization` | Unit | P0 | `undefined: WatchRequest` |
| AC-3 | `TestATDD_27_3_AC3_ProgressPayload_HasError_Serialization` | Unit | P0 | `unknown field HasError` |
| AC-3 | `TestATDD_27_3_AC3_ProgressPayload_DurationMs_Serialization` | Unit | P0 | `unknown field DurationMs` |
| AC-3 | `TestATDD_27_3_AC3_ProgressPayload_NewFields_OmitEmpty` | Unit | P1 | `unknown field HasError` |
| AC-5 | `TestATDD_27_3_AC5_CallbackMux_MultiSubscriber_AllReceive` | Unit | P0 | compile (multi-subscriber not impl) |
| AC-5 | `TestATDD_27_3_AC5_CallbackMux_UnregisterOne_OthersUnaffected` | Unit | P0 | `too many arguments in call to mux.unregister` |
| AC-5 | `TestATDD_27_3_AC5_CallbackMux_UnregisterAll_Cleanup` | Unit | P1 | `too many arguments in call to mux.unregister` |
| AC-11 | `TestATDD_27_3_AC11_OnStepComplete_FillsDurationMs` | Unit | P0 | `too many arguments in call to mux.OnStepComplete` |
| AC-11 | `TestATDD_27_3_AC11_OnStepComplete_FillsHasError` | Unit | P0 | `too many arguments in call to mux.OnStepComplete` |
| AC-11 | `TestATDD_27_3_AC11_ProgressPayload_FullStepComplete` | Unit | P0 | `unknown field HasError/DurationMs` |
| AC-4 | `TestATDD_27_3_AC4_CallbackMux_ImplementsUpdatedKernelCallbacks` | Unit | P0 | interface mismatch after sig change |
| AC-10 | `TestATDD_27_3_AC10_HandleWatch_PIDNotFound` | Integration | P0 | `undefined: MethodWatch` |
| AC-6 | `TestATDD_27_3_AC6_HandleWatch_StreamEvents` | Integration | P0 | `undefined: MethodWatch` |
| AC-6 | `TestATDD_27_3_AC6_HandleWatch_HistoryReplay` | Integration | P0 | `undefined: MethodWatch` |
| AC-6 | `TestATDD_27_3_AC6_Client_WatchProcess_Roundtrip` | Integration | P0 | `undefined: MethodWatch` + `client.WatchProcess` |
| AC-5+6 | `TestATDD_27_3_AC5_MultipleWatchers_SamePID` | Integration | P0 | `undefined: MethodWatch` |

## Red Phase Compilation Errors (17 tests, all expected)

```
ipc/atdd_27_3_watch_command_test.go:38:  undefined: MethodWatch
ipc/atdd_27_3_watch_command_test.go:44:  undefined: WatchRequest
ipc/atdd_27_3_watch_command_test.go:78:  unknown field HasError in struct literal of type ProgressPayload
ipc/atdd_27_3_watch_command_test.go:99:  unknown field DurationMs in struct literal of type ProgressPayload
ipc/atdd_27_3_watch_command_test.go:186: too many arguments in call to mux.unregister
ipc/atdd_27_3_watch_command_test.go:256: too many arguments in call to mux.OnStepComplete
```

## Test File

- `ipc/atdd_27_3_watch_command_test.go` — 17 test functions covering AC-2,3,4,5,6,10,11

## AC Coverage Not Tested (with rationale)

| AC | Reason |
|---|---|
| AC-1 | Cobra 命令注册 — 需集成测试 `cmd/rnix` 包，由 dev story 阶段验证 |
| AC-7 | 渲染函数输出格式 — 需 `cmd/rnix` 包内测试，属 UI 逻辑 |
| AC-8 | spawn --watch flag — 属 CLI 集成层，由 dev story 阶段验证 |
| AC-9 | q 键退出 — 终端 raw mode 需手工/集成测试 |

## Implementation Impact on Existing Tests

当实现 27-3 变更后，以下现有测试将因 `OnStepComplete` 签名变更而编译失败，需同步更新：

1. `ipc/atdd_3_6_step_output_streaming_test.go` — `OnStepComplete` 4-arg → 6-arg
2. `kernel/atdd_3_6_step_output_streaming_test.go` — 同上
3. `cmd/rnix/main_test.go` — mock callbacks
4. `kernel/stem_integration_test.go` — mock callbacks
