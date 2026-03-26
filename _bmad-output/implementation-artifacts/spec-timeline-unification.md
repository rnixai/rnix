---
title: 'Timeline 面板统一重设计：移除 Syscall 视图，统一为 Step 视图'
type: 'refactor'
created: '2026-03-26'
status: 'done'
baseline_commit: '03eb8f2'
context:
  - '_bmad-output/planning-artifacts/ux-timeline-unification.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Dashboard Timeline 面板有 Syscall 和 Step 两种模式，按 `s` 切换导致上下文断裂，且 Syscall 视图与 Step Detail 信息约 80% 重复，`rnix strace` 已覆盖高级调试需求。

**Approach:** 移除 Syscall 视图及相关状态/键位/渲染代码，统一为三级 Step 视图（折叠/展开/调试），实现宽度自适应列布局、Action 类型过滤、Prompt 查看器 Tab 页签，并在 `StepSummaryWire` 增加 `TokenCount` 字段。

## Boundaries & Constraints

**Always:**
- 保留 `kernel/event_writer.go`、`debug/` 包、`ipc/client.go` 的 `AttachDebug`/`ListEvents`（`rnix strace` 依赖）
- 保留 `stepEntries`、`stepCursor`、`stepDetailCache`、`lastFetchedStep`、`fetchingDetail` 字段
- Level 1 Token 显示：`TokenCount == 0` 显示 `—` 而非 `0 tok`
- 使用 `go-runewidth` 处理 CJK 双宽字符的宽度计算
- 错误 Step (`HasError`) 和慢 Step (`>1s`) 自动展开至 Level 2
- Prompt 查看器数据全部来自已缓存的 `GetStepDetailResponse`，无需额外 IPC

**Ask First:**
- 如果移除 Syscall 代码影响到除 Dashboard 以外的其他功能（如 `rnix strace`）

**Never:**
- 不修改 `kernel/`、`debug/`、`ipc/client.go` 的 Syscall 相关代码
- 不增加新的 IPC 方法（除 `StepSummaryWire` 增加字段外）
- 不添加 feature flag——TUI 内部重构无需兼容层

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| 进程无 Step | 刚启动的进程 | 显示 `Waiting for steps…` + spinner | N/A |
| Step 全被过滤 | 过滤后无匹配 | 显示 `No steps match filter` + 过滤条件 | N/A |
| ToolResult 过大 | >10KB 结果 | Level 2 显示前 3 行 + `… (N total)` | N/A |
| Detail 加载失败 | IPC 错误 | 展开区显示 `⚠ Failed to load detail` | 暗灰提示，不崩溃 |
| 历史进程 | TokenCount=0 in steps.jsonl | Level 1 显示 `—` | N/A |
| 窄屏 <80 cols | 终端宽度不足 | Action 缩写，隐藏 Token/耗时列 | 最低保留 Step 号 + Summary |

</frozen-after-approval>

## Code Map

- `cmd/rnix/dashboard_timeline.go` -- Timeline 面板主文件：Syscall 渲染/事件流/键位（~600 行待移除），Step 渲染/获取（~280 行待重写增强）
- `cmd/rnix/dashboard_nav.go` -- 键位分发：Syscall 键位路由、Step 模式分支（移除 Syscall 分支，简化为单一模式）
- `cmd/rnix/dashboard_types.go` -- 类型定义：eventCategory、timelineEvent、timelineStreamMsg 等（移除 Syscall 类型）
- `cmd/rnix/dashboard.go` -- 主 model 状态字段和 tick/消息处理（移除 Syscall 状态字段和消息处理）
- `ipc/protocol.go` -- `StepSummaryWire` 增加 `TokenCount` 字段
- `ipc/server.go` -- `handleListSteps` 填充 `TokenCount`
- `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` -- Timeline 面板测试（重写）

## Tasks & Acceptance

**Execution:**
- [x] `ipc/protocol.go` -- 在 `StepSummaryWire` 增加 `TokenCount int` 字段
- [x] `ipc/server.go` -- `handleListSteps` 中从 step record 填充 `TokenCount`
- [x] `cmd/rnix/dashboard_types.go` -- 移除 `eventCategory`、`timelineEvent`、`timelineEventMsg`、`timelineStreamStartedMsg`、`timelineDiskEventsMsg`、`maxTimelineEvents`、`zoomWindowMs` 等 Syscall 类型；新增 `stepFilters map[string]bool`、`stepFilterMode bool`、`stepExpandedIdx int` 字段到 model 或类型中
- [x] `cmd/rnix/dashboard.go` -- 移除 `timelineEvents`/`timelineAttachedPID`/`timelineEventCh`/`timelineStopCh`/`timelineZoomLevel`/`timelineViewStart`/`timelineEventCursor`/`timelineFilters`/`timelineFilterMode`/`timelineExpandedIdx`/`stepTimelineMode` 字段；移除 `timelineEventMsg`/`timelineStreamStartedMsg`/`timelineDiskEventsMsg` 消息处理；移除 PID 切换时的 Syscall 流启动逻辑；在 tick 中移除 Syscall 条件分支
- [x] `cmd/rnix/dashboard_timeline.go` -- 移除 Syscall 函数（`classifySyscall`、`renderTimelinePane`、`renderTimelineBar`、`handleTimelineEvent`、`startTimelineCmd`、`waitTimelineEventCmd`、`fetchEventsFromDiskCmd`、`stopTimelineStream`、`handleTimelineFilterKey`、`extractResource`、`formatEventLine`、`renderEventDetail`、`filteredTimelineEvents` 等）；重写 `renderStepTimeline` 实现 UX 设计的三级视图 + 宽度自适应 + 色彩编码；重写 `handleStepTimelineKey` 实现新键位（Enter/v/V/f/p/g/G）；实现 Action 类型过滤模式；实现 Prompt 查看器 Tab 页签（Messages/System/Tools）和搜索功能
- [x] `cmd/rnix/dashboard_nav.go` -- 移除 Syscall 键位分发分支；移除 `stepTimelineMode` 条件判断，Timeline 面板统一走 Step 键位处理
- [x] `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` -- 移除 Syscall 模式测试；新增三级 Step 视图测试、过滤测试、宽度自适应测试

**Acceptance Criteria:**
- Given 用户进入 Timeline 面板，when 面板加载，then 直接显示 Step 列表（无 Syscall 模式切换）
- Given Step 有 `HasError=true`，when Step 到达，then 自动展开至 Level 2 并红色高亮
- Given 用户按 `f` 进入过滤模式，when 按 `t` 切换 tool_call 过滤，then 仅显示 tool_call 类型 Step，标题显示 `filter: tool_call (N/M)`
- Given 用户在 Level 2/3 按 `p`，when Prompt 查看器打开，then 显示完整消息历史，Tab 可切换 Messages/System/Tools
- Given 终端宽度 < 80 cols，when 渲染 Level 1，then Action 列缩写，Token/耗时列按需隐藏
- Given 历史进程 `TokenCount=0`，when Level 1 渲染，then Token 列显示 `—`
- Given `make all` 执行，when 所有变更完成，then 编译通过、测试通过、lint 通过

## Verification

**Commands:**
- `make all` -- expected: lint + vet + test + build 全部通过
- `go test -race -run TestTimeline ./cmd/rnix/...` -- expected: Timeline 相关测试通过

## Suggested Review Order

**IPC 协议扩展**

- StepSummaryWire 新增 TokenCount 字段，向后兼容（零值=无数据）
  [`protocol.go:935`](../../ipc/protocol.go#L935)

- handleListSteps 从 StepRecord 填充 TokenCount
  [`server.go:2050`](../../ipc/server.go#L2050)

**Timeline 面板核心重写**

- 统一入口：renderTimelinePane 不再分支 Syscall/Step，直接调用 renderStepTimeline
  [`dashboard_timeline.go:199`](../../cmd/rnix/dashboard_timeline.go#L199)

- 三级视图渲染：Level 1 弹性列宽 + 色彩编码 + 宽度自适应
  [`dashboard_timeline.go:225`](../../cmd/rnix/dashboard_timeline.go#L225)

- Level 2 展开详情：ToolInput/Result/Error + Token 分解
  [`dashboard_timeline.go:385`](../../cmd/rnix/dashboard_timeline.go#L385)

- Level 3 调试态：消息预览 + 累计 Token + prompt 提示
  [`dashboard_timeline.go:440`](../../cmd/rnix/dashboard_timeline.go#L440)

**Action 类型过滤**

- handleStepFilterKey：7 种 action 类型独立切换
  [`dashboard_timeline.go:121`](../../cmd/rnix/dashboard_timeline.go#L121)

- filteredStepEntries + resolveStepIndex：过滤索引映射
  [`dashboard_timeline.go:149`](../../cmd/rnix/dashboard_timeline.go#L149)

**Prompt 查看器 Tab 页签**

- 三 Tab 内容生成：Messages/System/Tools 独立渲染
  [`dashboard_timeline.go:681`](../../cmd/rnix/dashboard_timeline.go#L681)

- Tab 键切换：在 Prompt Pager Layer 1 拦截 Tab 键
  [`dashboard_nav.go:41`](../../cmd/rnix/dashboard_nav.go#L41)

**Model 状态清理**

- 移除 11 个 Syscall 状态字段，新增 stepFilters/stepFilterMode/stepExpandedIdx/promptTab
  [`dashboard.go:53`](../../cmd/rnix/dashboard.go#L53)

- 移除 Syscall 消息处理（4 个 case 分支）
  [`dashboard.go:219`](../../cmd/rnix/dashboard.go#L219)

**键位分发简化**

- dispatchPaneKey：移除 stepTimelineMode 条件，enter/v/V/p 统一走 resolveStepIndex
  [`dashboard_nav.go:211`](../../cmd/rnix/dashboard_nav.go#L211)

**类型定义**

- 移除 eventCategory/timelineEvent/zoomWindowMs，新增 promptPagerTab
  [`dashboard_types.go:191`](../../cmd/rnix/dashboard_types.go#L191)

**测试**

- Timeline 三级视图测试（AC-1~5 + cursor 导航）
  [`atdd_27_3_dashboard_timeline_test.go:32`](../../cmd/rnix/atdd_27_3_dashboard_timeline_test.go#L32)

- Prompt 查看器 Tab 测试更新
  [`atdd_27_4_dashboard_prompt_view_test.go:303`](../../cmd/rnix/atdd_27_4_dashboard_prompt_view_test.go#L303)

- dashboard_test.go 移除 25 个 Syscall 测试，新增 step filter 测试
  [`dashboard_test.go:2063`](../../cmd/rnix/dashboard_test.go#L2063)
