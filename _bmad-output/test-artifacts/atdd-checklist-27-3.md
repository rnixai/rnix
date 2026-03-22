---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-22'
storyId: '27-3'
storyTitle: 'Dashboard 时间线三级详细度'
detectedStack: 'backend'
testFramework: 'go test -race'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-3-dashboard-timeline-three-level-detail.md
  - ipc/protocol.go
  - ipc/server.go
  - ipc/client.go
  - ipc/server_test.go
  - ipc/atdd_27_2_getstepdetail_test.go
  - kernel/kernel.go
  - cmd/rnix/dashboard.go
  - cmd/rnix/dashboard_test.go
  - cmd/rnix/main.go
  - internal/types/step_record.go
---

# ATDD Checklist — Story 27.3: Dashboard 时间线三级详细度

## Test Files

- `ipc/atdd_27_3_liststeps_test.go` — AC-6 (ProgressPayload 扩展) + AC-8 (ListSteps IPC)
- `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` — AC-1~AC-5, AC-7 (Dashboard 渲染 + 键盘交互 + spawn --dashboard)

## Red Phase Status

**编译状态: FAIL (预期)**

### ipc 包编译错误

| 符号 | 类型 | 预期定义位置 |
|------|------|-------------|
| `ProgressPayload.HasError` | 字段 | `ipc/protocol.go` |
| `ProgressPayload.DurationMs` | 字段 | `ipc/protocol.go` |
| `OnStepComplete(pid, step, action, summary, hasError, durationMs)` | 签名扩展 | `kernel/kernel.go` + `ipc/server.go` |
| `MethodListSteps` | 常量 | `ipc/protocol.go` |
| `ListStepsRequest` | 结构体 | `ipc/protocol.go` |
| `ListStepsResponse` | 结构体 | `ipc/protocol.go` |
| `StepSummaryWire` | 结构体 | `ipc/protocol.go` |
| `Client.ListSteps()` | 方法 | `ipc/client.go` |
| `Server.handleListSteps()` | handler | `ipc/server.go` |

### cmd/rnix 包编译错误

| 符号 | 类型 | 预期定义位置 |
|------|------|-------------|
| `stepDetailLevel` | 类型 | `cmd/rnix/dashboard.go` |
| `levelSummary` / `levelExpanded` / `levelDebug` | 常量 | `cmd/rnix/dashboard.go` |
| `stepEntry` | 结构体 | `cmd/rnix/dashboard.go` |
| `dashboardModel.stepEntries` | 字段 | `cmd/rnix/dashboard.go` |
| `dashboardModel.stepCursor` | 字段 | `cmd/rnix/dashboard.go` |
| `dashboardModel.stepDetailCache` | 字段 | `cmd/rnix/dashboard.go` |
| `dashboardModel.stepTimelineMode` | 字段 | `cmd/rnix/dashboard.go` |
| `dashboardModel.applyNewSteps()` | 方法 | `cmd/rnix/dashboard.go` |
| `rootCmd --dashboard` flag | Flag | `cmd/rnix/main.go` |

## Test Coverage Map

### IPC 层 (ipc/atdd_27_3_liststeps_test.go)

| AC | 测试函数 | 优先级 | 验证点 |
|----|----------|--------|--------|
| AC-6 | `TestATDD_27_3_AC6_ProgressPayload_HasError_Field` | P0 | HasError 字段序列化/反序列化 |
| AC-6 | `TestATDD_27_3_AC6_ProgressPayload_DurationMs_Field` | P0 | DurationMs 字段序列化/反序列化 |
| AC-6 | `TestATDD_27_3_AC6_ProgressPayload_OmitEmpty` | P0 | HasError=false/DurationMs=0 时 JSON 省略 |
| AC-6 | `TestATDD_27_3_AC6_CallbackMux_OnStepComplete_HasError` | P0 | callbackMux 传递 HasError + DurationMs |
| AC-6 | `TestATDD_27_3_AC6_KernelCallbacks_Extended_Signature` | P0 | 编译时接口检查 |
| AC-8 | `TestATDD_27_3_AC8_MethodConstant` | P0 | MethodListSteps = "list_steps" |
| AC-8 | `TestATDD_27_3_AC8_ListStepsRequest_Serialization` | P0 | Request JSON 往返 |
| AC-8 | `TestATDD_27_3_AC8_ListStepsRequest_AfterStep_OmitEmpty` | P1 | AfterStep=0 时省略 |
| AC-8 | `TestATDD_27_3_AC8_ListStepsResponse_Serialization` | P0 | Response 含多步骤序列化 |
| AC-8 | `TestATDD_27_3_AC8_StepSummaryWire_Fields` | P0 | 单条摘要序列化 |
| AC-8 | `TestATDD_27_3_AC8_ServerHandler_AllSteps` | P0 | 全量拉取所有步骤摘要 |
| AC-8 | `TestATDD_27_3_AC8_ServerHandler_IncrementalFetch` | P0 | AfterStep 增量拉取 |
| AC-8 | `TestATDD_27_3_AC8_ServerHandler_HasError_FromToolError` | P1 | ToolError → HasError=true |
| AC-8 | `TestATDD_27_3_AC8_ServerHandler_DurationMs` | P1 | ToolDuration → DurationMs 转换 |
| AC-8 | `TestATDD_27_3_AC8_ServerHandler_ReapedProcess` | P0 | 已回收进程从磁盘读取 |
| AC-8 | `TestATDD_27_3_AC8_ServerHandler_PIDNotFound` | P0 | 不存在的 PID 返回 not_found |
| AC-8 | `TestATDD_27_3_AC8_ClientMethod` | P0 | Client.ListSteps 全量往返 |
| AC-8 | `TestATDD_27_3_AC8_ClientMethod_Incremental` | P1 | Client.ListSteps 增量往返 |
| AC-8 | `TestATDD_27_3_AC8_Performance` | P0 | 30 步 ≤ 200ms |

### Dashboard 渲染层 (cmd/rnix/atdd_27_3_dashboard_timeline_test.go)

| AC | 测试函数 | 优先级 | 验证点 |
|----|----------|--------|--------|
| AC-1 | `TestATDD_27_3_AC1_Level1_StepSummary_Rendered` | P0 | 渲染包含 Step N + action + target |
| AC-1 | `TestATDD_27_3_AC1_Level1_ShowsDuration` | P0 | 渲染包含耗时 |
| AC-1 | `TestATDD_27_3_AC1_Level1_ShowsErrorMarker` | P0 | 渲染包含错误步骤标记 |
| AC-1 | `TestATDD_27_3_AC1_Level1_AllSteps` | P0 | 所有步骤均显示 |
| AC-1 | `TestATDD_27_3_AC1_Level1_StepTotal` | P1 | 显示步骤总数 /N |
| AC-2 | `TestATDD_27_3_AC2_VKey_ExpandsToLevel2` | P0 | v 键切换到 Level 2 |
| AC-2 | `TestATDD_27_3_AC2_Level2_ShowsInput` | P0 | Level 2 显示 Input 标签 |
| AC-2 | `TestATDD_27_3_AC2_Level2_ShowsTokens` | P0 | Level 2 显示 token 消耗 |
| AC-2 | `TestATDD_27_3_AC2_Level2_ShowsError` | P1 | Level 2 显示 ToolError |
| AC-3 | `TestATDD_27_3_AC3_ShiftVKey_ExpandsToLevel3` | P0 | V 键从 L2→L3 |
| AC-3 | `TestATDD_27_3_AC3_Level3_ShowsMessageCount` | P1 | Level 3 显示消息数 |
| AC-3 | `TestATDD_27_3_AC3_Level3_ShowsTokenCount` | P1 | Level 3 显示 token 总数 |
| AC-3 | `TestATDD_27_3_AC3_Level3_ShowsFirstMessagePreview` | P1 | Level 3 显示首条消息预览 |
| AC-3 | `TestATDD_27_3_AC3_ShiftV_FromLevel1_GoesToLevel3` | P1 | V 键从 L1 直接到 L3 |
| AC-4 | `TestATDD_27_3_AC4_VKey_FromLevel2_CollapseToLevel1` | P0 | v 从 L2→L1 |
| AC-4 | `TestATDD_27_3_AC4_VKey_FromLevel3_CollapseToLevel1` | P0 | v 从 L3→L1 |
| AC-4 | `TestATDD_27_3_AC4_Collapse_PerStep` | P0 | 折叠仅影响当前步骤 |
| AC-5 | `TestATDD_27_3_AC5_AutoExpand_Error` | P0 | HasError 步骤自动展开到 L2 |
| AC-5 | `TestATDD_27_3_AC5_AutoExpand_SlowStep` | P0 | >1s 步骤自动展开到 L2 |
| AC-5 | `TestATDD_27_3_AC5_NoAutoExpand_NormalStep` | P1 | 正常步骤保持 L1 |
| AC-7 | `TestATDD_27_3_AC7_SpawnDashboard_FlagExists` | P2 | --dashboard flag 存在 |
| — | `TestATDD_27_3_StepCursor_JKey_MovesDown` | P1 | j 键下移游标 |
| — | `TestATDD_27_3_StepCursor_KKey_MovesUp` | P1 | k 键上移游标 |
| — | `TestATDD_27_3_StepTimelineMode_Default` | P0 | 默认启用 step 视图 |
| — | `TestATDD_27_3_ShiftV_FromLevel3_GoesToLevel2` | P1 | V 从 L3→L2（降级） |

## Implementation Dependencies

### 需要新增的生产代码

1. **`ipc/protocol.go`** — 类型定义
   - `MethodListSteps` 常量
   - `ListStepsRequest`、`ListStepsResponse`、`StepSummaryWire` 结构体
   - `ProgressPayload` 新增 `HasError bool` 和 `DurationMs float64` 字段

2. **`kernel/kernel.go`** — 接口签名扩展
   - `KernelCallbacks.OnStepComplete` 新增 `hasError bool, durationMs float64` 参数
   - 所有 `OnStepComplete` 调用点更新

3. **`ipc/server.go`** — Handler
   - `handleListSteps(conn, rawPayload)` 方法
   - `callbackMux.OnStepComplete` 签名更新
   - switch case 添加 `case MethodListSteps`

4. **`ipc/client.go`** — 客户端方法
   - `func (c *Client) ListSteps(pid types.PID, afterStep int) (*ListStepsResponse, error)`

5. **`cmd/rnix/dashboard.go`** — Dashboard 模型扩展
   - `stepDetailLevel` 类型和 `levelSummary`/`levelExpanded`/`levelDebug` 常量
   - `stepEntry` 结构体
   - `dashboardModel` 新增字段（stepEntries, stepCursor, stepDetailCache, stepTimelineMode, lastFetchedStep）
   - `applyNewSteps()` 方法（含自动展开逻辑）
   - `renderTimelinePane` 重写支持 step 渲染模式
   - v/V 键状态机
   - j/k 键游标导航

6. **`cmd/rnix/main.go`** — spawn --dashboard flag
   - `rootCmd.Flags().Bool("dashboard", false, "...")`

### 破坏性变更（需全局更新）

`KernelCallbacks.OnStepComplete` 签名扩展：
- `ipc/server.go` — `callbackMux.OnStepComplete`
- `kernel/kernel.go` — 所有调用点（~10 处）
- `kernel/stem_integration_test.go` — `testCallbacks.OnStepComplete`
- `ipc/atdd_3_6_step_output_streaming_test.go` — `atdd36Callbacks.OnStepComplete`
- `cmd/rnix/main.go` — `cliCallbacks.OnStepComplete`
- `cmd/rnix/main_test.go` — `TestCliCallbacks_OnStepComplete`
- 其他实现 `KernelCallbacks` 的 mock/stub

## Next Steps

进入绿阶段实现时，按以下顺序:

1. `ipc/protocol.go` — 添加 ListSteps wire 类型 + ProgressPayload 字段 → AC-8 序列化测试通过
2. `kernel/kernel.go` — 扩展 OnStepComplete 签名 → AC-6 接口测试通过
3. 全局更新所有 OnStepComplete 调用/实现 → 编译恢复
4. `ipc/server.go` — handleListSteps handler → AC-8 server 测试通过
5. `ipc/client.go` — ListSteps 方法 → AC-8 client + performance 测试通过
6. `cmd/rnix/dashboard.go` — 模型扩展 + 类型 → AC-1~5 dashboard 测试通过
7. `cmd/rnix/main.go` — --dashboard flag → AC-7 测试通过
8. `make all` 全部通过
