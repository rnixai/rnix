---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-aggregate
lastStep: step-05-aggregate
lastSaved: '2026-03-09'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/17-3-context-heatmap-pane.md
  - cmd/rnix/dashboard.go
  - cmd/rnix/dashboard_test.go
  - debug/ctx_profile.go
  - ipc/client.go
  - internal/ui/styles.go
---

# ATDD Checklist - Epic 17, Story 17-3: 上下文热力图窗格

**Date:** 2026-03-09
**Author:** Decker
**Primary Test Level:** Unit

---

## Story Summary

在 17-1/17-2 建立的 dashboard 框架上实现热力图窗格，通过复用 CtxProfile IPC 获取上下文数据，以 treemap 色块可视化 token 来源分布和活跃度，并支持区域选择查看详情。

**As a** 平台构建者
**I want** 在热力图窗格中可视化智能体的上下文组成
**So that** 我可以直观了解 token 分布和活跃度

---

## Acceptance Criteria

1. **AC#1** — Given 选中一个智能体节点, When 热力图窗格渲染, Then 按来源着色展示上下文组成（system prompt / skill 指令 / 工具结果 / 对话历史），面积正比 token 占比，深浅表示活跃度
2. **AC#2** — Given 热力图中某个区域, When 用户选中该区域, Then 显示具体 token 数、占比百分比和内容摘要

---

## Failing Tests Created (RED Phase)

### Unit Tests (13 tests)

**File:** `cmd/rnix/dashboard_test.go` (1141 lines)

- ✅ **Test:** `TestBuildHeatmapSegments_Empty`
  - **Status:** PASS — nil profile 返回 nil segments 是正确的零值行为
  - **Verifies:** buildHeatmapSegments 对空输入的安全处理 (AC#1)

- ✅ **Test:** `TestBuildHeatmapSegments_SortedByTokenDesc`
  - **Status:** RED — stub 返回 nil, expected non-empty segments
  - **Verifies:** buildHeatmapSegments 按 token 降序排列段 (AC#1)

- ✅ **Test:** `TestBuildHeatmapSegments_MergeSmall`
  - **Status:** RED — stub 返回 nil, 无 "Other" 段
  - **Verifies:** 占比 < 3% 的小段合并为 "Other" (AC#1)

- ✅ **Test:** `TestSegmentColor_KindAndActivity`
  - **Status:** RED — stub 返回空字符串, Active/Cold 颜色相同
  - **Verifies:** 不同来源分类和活跃度返回不同颜色 (AC#1)

- ✅ **Test:** `TestDashboardModel_HeatmapProfileMsg`
  - **Status:** RED — Update 不存储 profile, 不触发 buildHeatmapSegments
  - **Verifies:** heatmapProfileMsg 消息将 profile 存储到 model 并构建 segments (AC#1)

- ✅ **Test:** `TestDashboardModel_HeatmapRenderEmpty`
  - **Status:** PASS — timeline 窗格已显示 "Select an agent"
  - **Verifies:** 无选中 PID 时显示 "Select an agent" 提示 (AC#1)

- ✅ **Test:** `TestDashboardModel_HeatmapRenderWithSegments`
  - **Status:** RED — 占位符仍显示 "Coming Soon"
  - **Verifies:** 有 segments 时不再显示 "Coming Soon" (AC#1)

- ✅ **Test:** `TestDashboardModel_HeatmapCursorJK`
  - **Status:** RED — 无 paneHeatmap 键盘处理, cursor 不移动
  - **Verifies:** j/k 键移动 heatmapCursor 选择不同段 (AC#2)

- ✅ **Test:** `TestDashboardModel_HeatmapSelectedDetails`
  - **Status:** RED — 占位符不含 token 数和百分比
  - **Verifies:** 选中段显示具体 token 数和占比百分比 (AC#2)

- ✅ **Test:** `TestDashboardModel_HeatmapPIDChange`
  - **Status:** RED — handleHeatmapPIDChange stub 不清空数据
  - **Verifies:** PID 变化时清空 heatmapProfile/segments/cursor (AC#1)

- ✅ **Test:** `TestDashboardModel_TabToHeatmap`
  - **Status:** PASS — Tab 循环逻辑已在 17-1 实现
  - **Verifies:** Tab 从 timeline 切换到 paneHeatmap (AC#1)

- ✅ **Test:** `TestMapConsumerKindToSegmentKind`
  - **Status:** RED — stub 全返回 segSystem, user/assistant/tool 分类错误
  - **Verifies:** ConsumerEntry.Kind 正确映射到 segmentKind (AC#1)

- ✅ **Test:** `TestDashboardModel_HeatmapRefreshTick`
  - **Status:** RED — dashboardTick 不递增 heatmapTickCount
  - **Verifies:** 5 次 tick 后 heatmapTickCount == 5 (用于触发 2.5s 刷新) (AC#1)

---

## Data Factories Created

### Heatmap Profile Factory

**File:** `cmd/rnix/dashboard_test.go` — 内联测试 helpers

**Exports:**

- `mockHeatmapProfile()` — 创建包含 4 个 TopConsumers 和完整 Classification 的 CtxProfileResult
- `newTestHeatmapDashboardModel()` — 创建已配置 heatmap 数据的 dashboardModel（含 profile、segments、selectedPID）

---

## Fixtures Created

### Heatmap Model Fixture

**File:** `cmd/rnix/dashboard_test.go`

**Fixtures:**

- `newTestHeatmapDashboardModel()` — 预配置完整 heatmap 数据的 dashboardModel
  - **Setup:** 基于 mockDashboardProcs 创建 model, 设置 selectedPID=2, activePane=paneHeatmap, 填充 heatmapProfile 和 heatmapSegments
  - **Provides:** 包含 5 个 segments (System/Tool/User/Assistant/Leaked) 的完整 dashboardModel
  - **Cleanup:** Go testing 自动管理，无外部状态

---

## Mock Requirements

无外部服务 mock 需求。所有测试使用内存中的 dashboardModel 和预构造的 `debug.CtxProfileResult` 数据。IPC 调用通过 `ipc.SocketPathOverride` 指向不存在的 socket 路径来隔离。

---

## Stub Requirements (Go-specific)

以下类型和函数 stub 已添加到 `cmd/rnix/dashboard.go`，使测试编译通过但返回零值：

### 类型 Stubs

- `segmentKind` — int 类型 + 6 个常量 (segSystem/segSkill/segTool/segUser/segAssistant/segLeaked)
- `activityLevel` — int 类型 + 4 个常量 (actActive/actWarm/actCold/actLeaked)
- `heatmapSegment` — struct (label/tokens/pct/kind/activity/summary)
- `heatmapProfileMsg` — struct (profile/err)

### 函数 Stubs

- `buildHeatmapSegments(_ *debug.CtxProfileResult) []heatmapSegment` → returns nil
- `segmentColor(_ segmentKind, _ activityLevel) string` → returns ""
- `mapConsumerKind(_ string) segmentKind` → returns segSystem
- `(m dashboardModel) handleHeatmapPIDChange() dashboardModel` → returns m unchanged
- `(m dashboardModel) handleHeatmapKey(_ string) dashboardModel` → returns m unchanged
- `(m dashboardModel) renderHeatmapPane(_ int, _ int) string` → returns ""
- `fetchHeatmapCmd(_ types.PID) tea.Cmd` → returns nil

### Model Fields Added

- `heatmapProfile *debug.CtxProfileResult`
- `heatmapPID types.PID`
- `heatmapSegments []heatmapSegment`
- `heatmapCursor int`
- `heatmapTickCount int`

### Update Handler Added

- `case heatmapProfileMsg:` → returns m, nil (no-op stub)

---

## Implementation Checklist

### Test: TestBuildHeatmapSegments_SortedByTokenDesc

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `buildHeatmapSegments(profile *debug.CtxProfileResult) []heatmapSegment`：从 TopConsumers 提取 segments
- [ ] 实现 `mapConsumerKind(kind string) segmentKind`：映射 ConsumerEntry.Kind 到 segmentKind
- [ ] 分配活跃度：根据 Classification 温度分布估算每段的 activityLevel
- [ ] 按 tokens 降序排列 segments
- [ ] Run test: `go test -run TestBuildHeatmapSegments_SortedByTokenDesc ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestBuildHeatmapSegments_MergeSmall

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 buildHeatmapSegments 末尾添加合并逻辑：遍历 segments, 将 pct < 3.0 的段合并为 "Other"
- [ ] "Other" 段的 tokens = 所有小段 tokens 之和, pct = 所有小段 pct 之和
- [ ] Run test: `go test -run TestBuildHeatmapSegments_MergeSmall ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestSegmentColor_KindAndActivity

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `segmentColor(kind segmentKind, activity activityLevel) string`
- [ ] 基色映射：segSystem→#5B9BD5, segSkill→#9B59B6, segTool→#6BCB77, segUser→#FFD93D, segAssistant→#888888, segLeaked→#FF6B6B
- [ ] 活跃度调制：Active=原色, Warm=原色, Cold=暗色变体（dimmed ~30%）, Leaked=#FF6B6B
- [ ] 实现 `dim(hexColor string) string` 辅助函数降低亮度
- [ ] Run test: `go test -run TestSegmentColor ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestDashboardModel_HeatmapProfileMsg

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 Update 的 `case heatmapProfileMsg:` 中：存储 profile → 调用 buildHeatmapSegments → 存储 segments
- [ ] Run test: `go test -run TestDashboardModel_HeatmapProfileMsg ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestDashboardModel_HeatmapRenderWithSegments

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `renderHeatmapPane(width, height int) string`
- [ ] 替换 `renderDashboardPlaceholder("Heatmap", ...)` 为 `m.renderHeatmapPane(rightWidth, bottomRightH)`
- [ ] 标题行："Heatmap" + PID + 总 token 数 + budget 百分比
- [ ] 空状态处理：无 selectedPID → "Select an agent"; 有 PID 无 profile → "Loading..."
- [ ] Run test: `go test -run TestDashboardModel_HeatmapRender ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestDashboardModel_HeatmapCursorJK

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 dashboardKey 的 switch 中添加 `case paneHeatmap:` 分支
- [ ] 调用 `m.handleHeatmapKey(key)` 处理 j/k/enter 按键
- [ ] 实现 handleHeatmapKey：j 下移 cursor, k 上移 cursor, 边界检查
- [ ] Run test: `go test -run TestDashboardModel_HeatmapCursorJK ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestDashboardModel_HeatmapSelectedDetails

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 renderHeatmapPane 中渲染 treemap 色块条
- [ ] 渲染段详情列表：选中段用 ▸ 标记 + 显示 token 数和百分比
- [ ] 底部详情区：选中段的 token 数、占比、活跃度、内容摘要
- [ ] Run test: `go test -run TestDashboardModel_HeatmapSelectedDetails ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 1 hour

---

### Test: TestDashboardModel_HeatmapPIDChange

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 实现 handleHeatmapPIDChange：清空 heatmapProfile, heatmapSegments, 重置 heatmapCursor
- [ ] 在 dashboardTick 中检测 selectedPID 变化时调用 handleHeatmapPIDChange
- [ ] Run test: `go test -run TestDashboardModel_HeatmapPIDChange ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

### Test: TestMapConsumerKindToSegmentKind

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 实现 mapConsumerKind："system_prompt"→segSystem, "user"→segUser, "assistant"→segAssistant, "tool:*"→segTool
- [ ] Run test: `go test -run TestMapConsumerKindToSegmentKind ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.25 hours

---

### Test: TestDashboardModel_HeatmapRefreshTick

**File:** `cmd/rnix/dashboard_test.go`

**Tasks to make this test pass:**

- [ ] 在 dashboardTick 中添加 `m.heatmapTickCount++`
- [ ] 添加刷新逻辑：`needRefresh := m.selectedPID != m.heatmapPID || m.heatmapTickCount%5 == 0`
- [ ] 刷新时调用 fetchHeatmapCmd 并 batch 到 tick 命令中
- [ ] Run test: `go test -run TestDashboardModel_HeatmapRefreshTick ./cmd/rnix/`
- [ ] ✅ Test passes (green phase)

**Estimated Effort:** 0.5 hours

---

## Running Tests

```bash
# Run all failing tests for this story
go test -v -run "Heatmap|SegmentColor|MapConsumerKind|BuildHeatmap" ./cmd/rnix/

# Run specific test
go test -v -run TestBuildHeatmapSegments_SortedByTokenDesc ./cmd/rnix/

# Run all dashboard tests (17-1 + 17-2 + 17-3)
go test -v -run "Dashboard|Classify|Category|Timeline|Heatmap|SegmentColor|MapConsumerKind|BuildHeatmap" ./cmd/rnix/

# Run with race detector
go test -race -run "Heatmap|SegmentColor|MapConsumerKind|BuildHeatmap" ./cmd/rnix/

# Run with coverage
go test -coverprofile=coverage.out -run "Heatmap|SegmentColor|MapConsumerKind|BuildHeatmap" ./cmd/rnix/ && go tool cover -func=coverage.out
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 13 tests written and compiled
- ✅ 10/13 tests failing as expected (RED phase verified)
- ✅ 3/13 tests pass early (zero-value behavior + pre-existing infrastructure)
- ✅ Stub types, functions, and model fields created
- ✅ Implementation checklist created mapping tests to code tasks

**Verification:**

- All tests compile and run without panic
- 10 tests fail due to missing implementation (stubs return zero values)
- 3 tests pass due to correct zero-value behavior or pre-existing functionality
- Failure messages are clear and actionable

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Pick one failing test** from implementation checklist (start with highest priority)
2. **Read the test** to understand expected behavior
3. **Implement minimal code** to make that specific test pass
4. **Run the test** to verify it now passes (green)
5. **Check off the task** in implementation checklist
6. **Move to next test** and repeat

**Recommended Order:**

1. `TestMapConsumerKindToSegmentKind` — 最简单的纯函数
2. `TestSegmentColor_KindAndActivity` — 颜色映射纯函数
3. `TestBuildHeatmapSegments_SortedByTokenDesc` — 核心数据转换
4. `TestBuildHeatmapSegments_MergeSmall` — 合并逻辑
5. `TestDashboardModel_HeatmapProfileMsg` — 消息处理
6. `TestDashboardModel_HeatmapPIDChange` — PID 变化清理
7. `TestDashboardModel_HeatmapRefreshTick` — tick 计数
8. `TestDashboardModel_HeatmapRenderWithSegments` — 渲染核心
9. `TestDashboardModel_HeatmapSelectedDetails` — 详情显示
10. `TestDashboardModel_HeatmapCursorJK` — 键盘交互

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

**DEV Agent Responsibilities:**

1. **Verify all 13 tests pass** (green phase complete)
2. **Review code for quality** — 确保 treemap 渲染逻辑清晰
3. **Extract duplications** — dim() 辅助函数、颜色常量
4. **Ensure tests still pass** after each refactor
5. **Run full test suite** — `go test ./cmd/rnix/`

---

## Next Steps

1. **Review this checklist** 确认测试覆盖所有 AC
2. **Run failing tests** 确认 RED phase: `go test -v -run "Heatmap|SegmentColor|MapConsumerKind|BuildHeatmap" ./cmd/rnix/`
3. **Begin implementation** 使用 implementation checklist 作为指南
4. **Work one test at a time** (red → green for each)
5. **When all tests pass**, refactor for quality
6. **When complete**, 更新 sprint-status.yaml 中的 story 状态

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -v -run "Heatmap|SegmentColor|MapConsumerKind|BuildHeatmap" ./cmd/rnix/`

**Results:**

```
=== RUN   TestBuildHeatmapSegments_Empty
--- PASS: TestBuildHeatmapSegments_Empty (0.00s)
=== RUN   TestBuildHeatmapSegments_SortedByTokenDesc
    dashboard_test.go:900: expected non-empty segments for profile with TopConsumers
--- FAIL: TestBuildHeatmapSegments_SortedByTokenDesc (0.00s)
=== RUN   TestBuildHeatmapSegments_MergeSmall
    dashboard_test.go:940: expected 'Other' segment for merged small entries (<3%%)
--- FAIL: TestBuildHeatmapSegments_MergeSmall (0.00s)
=== RUN   TestSegmentColor_KindAndActivity
    dashboard_test.go:962: segmentColor(0, 0) returned empty string
    dashboard_test.go:969: segSystem active and cold should have different colors, both got ""
--- FAIL: TestSegmentColor_KindAndActivity (0.00s)
=== RUN   TestDashboardModel_HeatmapProfileMsg
    dashboard_test.go:984: heatmapProfileMsg should store profile in model
    dashboard_test.go:987: heatmapProfileMsg should trigger buildHeatmapSegments and produce segments
--- FAIL: TestDashboardModel_HeatmapProfileMsg (0.00s)
=== RUN   TestDashboardModel_HeatmapRenderEmpty
--- PASS: TestDashboardModel_HeatmapRenderEmpty (0.00s)
=== RUN   TestDashboardModel_HeatmapRenderWithSegments
    dashboard_test.go:1015: heatmap with segments should not show 'Coming Soon'
--- FAIL: TestDashboardModel_HeatmapRenderWithSegments (0.00s)
=== RUN   TestDashboardModel_HeatmapCursorJK
    dashboard_test.go:1031: j should move heatmapCursor down: expected 1, got 0
--- FAIL: TestDashboardModel_HeatmapCursorJK (0.00s)
=== RUN   TestDashboardModel_HeatmapSelectedDetails
    dashboard_test.go:1059: heatmap should display selected segment token count (360)
    dashboard_test.go:1062: heatmap should display selected segment percentage (30.0%%)
--- FAIL: TestDashboardModel_HeatmapSelectedDetails (0.00s)
=== RUN   TestDashboardModel_HeatmapPIDChange
    dashboard_test.go:1078: PID change should clear heatmapProfile
    dashboard_test.go:1081: PID change should clear heatmapSegments, got 5
--- FAIL: TestDashboardModel_HeatmapPIDChange (0.00s)
=== RUN   TestDashboardModel_TabToHeatmap
--- PASS: TestDashboardModel_TabToHeatmap (0.00s)
=== RUN   TestMapConsumerKindToSegmentKind
    dashboard_test.go:1117: mapConsumerKind("user") = 0, want 3
    dashboard_test.go:1117: mapConsumerKind("tool:read_file") = 0, want 2
--- FAIL: TestMapConsumerKindToSegmentKind (0.00s)
=== RUN   TestDashboardModel_HeatmapRefreshTick
    dashboard_test.go:1139: after 5 ticks, heatmapTickCount should be 5, got 0
--- FAIL: TestDashboardModel_HeatmapRefreshTick (0.00s)
FAIL
```

**Summary:**

- Total tests: 13
- Passing: 3 (zero-value behavior + pre-existing)
- Failing: 10 (expected — stubs not implemented)
- Status: ✅ RED phase verified

---

## Notes

- 所有测试和 stubs 放在 `cmd/rnix/dashboard.go` 和 `cmd/rnix/dashboard_test.go`，不新增文件
- `debug` 包新增为 dashboard.go 的 import（用于 `heatmapProfileMsg` 的 `*debug.CtxProfileResult` 类型引用）
- 3 个早期通过的测试（Empty/RenderEmpty/TabToHeatmap）不影响 RED phase 有效性 — 它们验证零值行为和已实现的基础设施
- IPC 隔离通过 `ipc.SocketPathOverride` 指向不存在的 socket 实现
- 现有 17-1/17-2 测试全部仍然通过，无回归

---

**Generated by BMad TEA Agent** - 2026-03-09
