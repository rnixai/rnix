# Story 29.3: 默认视图 Focus Card

Status: done

## Story

作为平台构建者，
我希望默认视图右下区域显示选中进程的 2×3 信息摘要卡片（Focus Card），
以便无需切换面板即可一览进程的关键指标。

## 验收准则

1. **AC-1: Focus Card 2×3 网格渲染** — 默认视图（viewDefault）右下区域显示 2×3 Focus Card：Row 0（Tokens / Context / Status）、Row 1（Intent / Trace / Alerts），替代当前 activePane 驱动的底部面板
2. **AC-2: Running 进程实时数据** — 选中 Running 进程时 Focus Card 显示实时递增的 elapsed/tokens/steps，数据随 500ms tick 刷新
3. **AC-3: Dead 进程快照** — 选中 Dead 进程时标题显示 `✓ Done (exit 0)` 或 `✕ Failed (exit N)`，顶部显示 `Historical snapshot — lived HH:MM:SS–HH:MM:SS (Xs)`，steps 显示 `N (final)`，Alerts 卡片替换为 Result 卡片
4. **AC-4: 默认视图布局变更** — 布局变为：左 Tree (40%w) + 右上 Timeline (h/2) + 右下 Focus Card (h/2)
5. **AC-5: lipgloss 卡片渲染** — 每个卡片有边框、标题居左、内容区 cardH-3 行，宽度=rightWidth/3，高度=focusHeight/2

## 任务 / 子任务

- [x] Task 1: 新增 `focusCardState` 类型和 `intentMiniTask` 类型 (AC: #1, #5)
  - [x] 1.1 在 `dashboard_types.go` 新增 `focusCardState` 结构体（含 pid/agent/state/elapsed/isHistory + 6 个卡片数据区）
  - [x] 1.2 在 `dashboard_types.go` 新增 `intentMiniTask` 结构体（name + state 字段）
  - [x] 1.3 在 `dashboardModel` 新增 `focusCardData *focusCardState` 字段

- [x] Task 2: 创建 `dashboard_focus.go` — Focus Card 数据聚合 + 渲染 (AC: #1, #2, #3, #5)
  - [x] 2.1 实现 `aggregateFocusCard()` 方法：从 dashboardModel 已有字段聚合 focusCardState
  - [x] 2.2 实现 `renderFocusCard(width, height int) string` 方法：2×3 网格布局
  - [x] 2.3 实现 6 个卡片渲染辅助函数：`renderTokensCard`, `renderContextCard`, `renderStatusCard`, `renderIntentCard`, `renderTraceCard`, `renderAlertsCard`
  - [x] 2.4 实现 Dead 进程差异渲染（Result 卡片替换 Alerts）

- [x] Task 3: 修改 `renderDefaultLayout` — Focus Card 替代底部面板 (AC: #4)
  - [x] 3.1 默认视图右下调用 `renderFocusCard(rightWidth, bottomRightH)` 替代当前 activePane switch
  - [x] 3.2 保持 viewExpanded 布局不变（展开视图仍然显示具体面板）

- [x] Task 4: 在 tick 中触发 Focus Card 数据刷新 (AC: #2)
  - [x] 4.1 在 `dashboardTick()` 中每次 tick 调用 `aggregateFocusCard()` 更新 focusCardData

- [x] Task 5: 运行 `make all` 验证 (AC: 全部)
  - [x] 5.1 编译通过，无 lint 错误
  - [x] 5.2 所有现有测试通过（20 个包全部通过，包含 16 个 ATDD 29.3 测试）

## Dev Notes

### 核心实现策略

在默认视图（viewDefault）中，**右下区域固定显示 Focus Card**，替代当前的 activePane 驱动面板切换。具体面板（Heatmap/Detail/Intent/Security/Trace/Eval）仍可通过数字键 3-8 展开查看（viewExpanded 模式），但默认视图始终显示 Focus Card 摘要。

### 数据聚合策略 — 复用已有字段，零新 IPC

`aggregateFocusCard()` 从 `dashboardModel` 已有缓存字段聚合，**不新增任何 IPC 调用**：

| 卡片 | 数据来源（已有字段） |
|------|---------------------|
| **Tokens** | `m.heatmapProfile.TotalTokens` / `m.procDetail.TokenBudget` / 从 stepEntries 计算 rate |
| **Context** | `m.heatmapProfile` 的 `SystemPct/UserPct/AssistantPct/ToolPct`（已有 heatmapProfile 获取逻辑） |
| **Status** | `m.procDetail` 的 Skills/Devices/PPID + 选中进程的 State |
| **Intent** | `m.intentTrees` 的子任务列表（取前 N 项摘要） |
| **Trace** | `m.traceSummaries` 的 span 计数 / 平均延迟 / 错误计数 |
| **Alerts** | `m.immuneStatus.Alerts` 的告警列表 |

**关键**：这些数据已经在 `dashboardTick()` 中定期获取（heatmapProfile 每 2 tick、procDetail 每 5 tick、intentTrees/traceSummaries/immuneStatus 每次 tick），Focus Card 只做聚合渲染，不增加 daemon 负载。

### 2×3 网格布局实现

```go
// dashboard_focus.go
func (m dashboardModel) renderFocusCard(width, height int) string {
    cardW := width / 3
    cardH := height / 2

    row0 := lipgloss.JoinHorizontal(lipgloss.Top,
        m.renderTokensCard(cardW, cardH),
        m.renderContextCard(cardW, cardH),
        m.renderStatusCard(cardW, cardH),
    )
    row1 := lipgloss.JoinHorizontal(lipgloss.Top,
        m.renderIntentCard(cardW, cardH),
        m.renderTraceCard(cardW, cardH),
        m.renderAlertsOrResultCard(cardW, cardH),
    )
    return lipgloss.JoinVertical(lipgloss.Left, row0, row1)
}
```

### 卡片渲染模式

遵循已有面板渲染惯例：

```go
func (m dashboardModel) renderTokensCard(w, h int) string {
    innerW := max(w-2, 1)
    innerH := max(h-3, 1) // cardH - 边框(2) - 标题(1)
    style := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color(ui.ColorMuted)).
        Width(innerW).Height(innerH)

    var b strings.Builder
    fmt.Fprintf(&b, " Tokens\n")
    // ... 内容渲染
    return style.Render(b.String())
}
```

### Dead 进程 Focus Card 差异

| 元素 | Running | Dead |
|------|---------|------|
| 标题 | `FOCUS: PID N agent ━━━ Running Xm` | `FOCUS: PID N agent ━━━ ✓ Done (exit 0)` |
| 顶部 | 无 | `Historical snapshot — lived HH:MM:SS–HH:MM:SS (Xs)` |
| Tokens rate | `rate: X tok/s`（实时） | `rate: X tok/s`（最终值） |
| Steps | `steps: N`（递增） | `steps: N (final)` |
| Elapsed | `elapsed: Xm`（递增） | `lived: Xs`（固定） |
| Row 1 第 3 格 | Alerts 卡片 | **Result 卡片**：`✓ Completed / output → PID N` |

通过 `focusCardState.isHistory` 布尔值判断：当选中进程的 `State == types.StateDead` 时设为 true。

### renderDefaultLayout 修改要点

**修改前**（`dashboard.go:619-647`）：
```go
func (m dashboardModel) renderDefaultLayout(w, h int) string {
    // ...
    var bottomPane string
    switch m.activePane {           // ← 删除这个 switch
    case paneDetail: ...
    case paneIntent: ...
    default: bottomPane = m.renderHeatmapPane(...)
    }
    // ...
}
```

**修改后**：
```go
func (m dashboardModel) renderDefaultLayout(w, h int) string {
    treeWidth := min(max(w*40/100, 30), 60)
    rightWidth := max(w-treeWidth, 10)
    treePane := m.renderDashboardTreePane(treeWidth, h)
    topRightH := h / 2
    bottomRightH := h - topRightH
    timelinePane := m.renderTimelinePane(rightWidth, topRightH)
    focusCard := m.renderFocusCard(rightWidth, bottomRightH)  // ← 固定 Focus Card
    rightPane := lipgloss.JoinVertical(lipgloss.Left, timelinePane, focusCard)
    return lipgloss.JoinHorizontal(lipgloss.Top, treePane, rightPane)
}
```

### 需要从 Story 29.2 了解的关键上下文

- **viewMode 系统已就绪**：`viewDefault`/`viewExpanded`/`viewLLM`/`viewHistory` 枚举已定义
- **键盘分发已就绪**：`dashboard_nav.go` 的 6 层分发结构，数字键 1-8 跳转到 viewExpanded 模式
- **默认视图底部面板**切换仍由 `activePane` 控制 — **本 Story 将其替换为固定 Focus Card**
- **展开视图（viewExpanded）不受影响**，数字键跳转后仍显示具体面板

### 不应该做的事

- **不要新增 IPC 方法** — 所有数据源已有
- **不要修改 dashboard_nav.go** — 键盘分发逻辑无需变更
- **不要修改 viewExpanded 布局** — 展开视图保持不变
- **不要在 Focus Card 内处理按键事件** — Focus Card 是纯展示组件，无交互
- **不要从 dashboard_types.go 删除 paneHeatmap** — Heatmap 在展开视图中仍需使用

### Project Structure Notes

| 文件 | 操作 | 说明 |
|------|------|------|
| `cmd/rnix/dashboard_types.go` | 修改 | 新增 focusCardState、intentMiniTask 类型；dashboardModel 新增字段 |
| `cmd/rnix/dashboard_focus.go` | **新增** | Focus Card 数据聚合 + 2×3 网格渲染 + 6 个卡片函数 |
| `cmd/rnix/dashboard.go` | 修改 | renderDefaultLayout 改为调用 renderFocusCard；tick 中触发聚合 |

### 已有代码中可复用的函数

- `ui.FormatTokens(n int) string` — token 格式化（已有）
- `ui.FormatDuration(d time.Duration) string` — 时间格式化（已有）
- `ui.FormatSkills(skills []string) string` — 技能列表格式化（已有）
- `colorState(s types.ProcessState) string` — 状态着色（`dashboard.go:571`）
- `lipgloss.RoundedBorder()` — 圆角边框（所有面板统一使用）
- `ui.ColorMuted` / `ui.ColorAgent` — 边框颜色常量（已有）

### 测试策略

- **结构验证**：用 `go/parser` + `go/ast` 验证 `dashboard_focus.go` 存在且包含预期函数
- **类型验证**：验证 `focusCardState` 和 `intentMiniTask` 在 `dashboard_types.go` 中定义
- **渲染验证**：构造 `dashboardModel` + 模拟数据，调用 `renderFocusCard(80, 20)`，验证输出包含 6 个卡片标题
- **布局验证**：调用 `renderDefaultLayout`，验证输出不再包含 Heatmap 默认面板

### References

- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 3.2.3] — focusCardState 类型定义
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 5.5] — 2×3 网格布局规格
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 9.1] — Dead 进程 Focus Card 差异
- [Source: _bmad-output/planning-artifacts/epics/epic-29-dashboard-ux-redesign.md#Story 29.3] — 验收准则
- [Source: _bmad-output/implementation-artifacts/29-2-view-mode-system-and-navigation-overhaul.md] — 前置 Story 实现细节
- [Source: cmd/rnix/dashboard.go:619-647] — 当前 renderDefaultLayout 实现
- [Source: cmd/rnix/dashboard_types.go] — 现有类型定义和 dashboardModel 字段

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6

### Debug Log References
无

### Completion Notes List
- ✅ Task 1: 在 `dashboard_types.go` 新增 `focusCardState`（含 pid/agent/state/elapsed/isHistory + tokens/context/status/intent/trace/alerts/result 数据区 + `stepCount` 字段）和 `intentMiniTask`（name + state）类型；在 `dashboardModel` 新增 `focusCardData *focusCardState` 字段
- ✅ Task 2: 创建 `dashboard_focus.go`（~440 行）包含：`aggregateFocusCard` 从已有缓存字段聚合数据（零新 IPC）；`renderFocusCard` 实现 2×3 网格布局；6 个卡片渲染方法（Tokens/Context/Status/Intent/Trace/AlertsOrResult）；Dead 进程差异渲染（Result 卡片替代 Alerts，Historical snapshot 标记，✓ Done / ✕ Failed 标题）
- ✅ Task 3: 修改 `renderDefaultLayout` 移除 `switch m.activePane` 块，改为固定调用 `renderFocusCard(rightWidth, bottomRightH)`；`renderExpandedLayout` 保持不变
- ✅ Task 4: 在 `dashboardTick()` 中 PID 验证和选择更新之后调用 `m.aggregateFocusCard()` 每次 tick 刷新 Focus Card 数据
- ✅ Task 5: `make all` 全部通过（lint 0 issues，22 包测试通过，编译成功）
- ⚠️ 测试修复：更新了 6 个既有测试以适配默认视图布局变更 — 需要看具体面板内容的测试改为使用 `viewExpanded` 模式（`dashboard_test.go` 3 处、`atdd_27_6_dashboard_detail_panel_test.go` 1 处）

### Code Review Fixes (Post-Review)
- ✅ P-1 [Critical]: 修复窄终端下字符串截断 panic — 新增 `truncateRuneSafe` 函数，rune-safe 截断 + 非负索引守卫
- ✅ P-2 [High]: Dead 进程 `livedRange` 改用 `p.DeadAt` 而非 `time.Now()`
- ✅ P-3 [High]: Dead 进程 `elapsed` 改用 `DeadAt.Sub(CreatedAt)` 固定值
- ✅ P-4 [High]: 新增 `stepCount` 独立字段，不再覆盖 `tokenRate`
- ✅ P-5 [High]: 退出码推断改为检查 error/fail/timeout 关键词（更保守）
- ✅ P-6 [Medium]: Running 进程也显示 steps 计数
- ✅ P-7 [Medium]: Intent 节点按 key 排序确保确定性显示
- ✅ P-8 [Medium]: 缓存 process 引用（`procInfoRef`），消除 5 次重复遍历
- ✅ P-9 [Medium]: Intent 任务截断限制移入内层循环
- ✅ P-10 [Low]: `focusCardData` 字段移至 struct 末尾（不再打断 replay 分组）
- ✅ P-11 [Low]: Result 卡片文案统一为 "✓ Done"
- ✅ BS-1 [High]: dashboardTick IPC 获取条件放宽 — viewDefault 模式下也获取 procDetail/intentTrees/immuneStatus/traceSummaries

### File List
- `cmd/rnix/dashboard_types.go` — 修改：新增 focusCardState（含 stepCount 字段）、intentMiniTask 类型
- `cmd/rnix/dashboard_focus.go` — **新增+重写**：Focus Card 数据聚合 + 2×3 网格渲染 + 6 个卡片函数 + Dead 进程差异渲染 + truncateRuneSafe + procInfoRef 缓存
- `cmd/rnix/dashboard.go` — 修改：renderDefaultLayout 改为调用 renderFocusCard；focusCardData 字段移至 struct 末尾；dashboardTick 中调用 aggregateFocusCard；IPC 获取条件放宽支持 viewDefault
- `cmd/rnix/dashboard_test.go` — 修改：更新测试以适配默认视图 Focus Card 替代 Heatmap
- `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go` — 修改：newDetailPanelModel 设置 viewExpanded 模式
- `cmd/rnix/atdd_29_3_default_view_focus_card_test.go` — **新增**：16 个 ATDD 测试
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 修改：29-3 状态更新为 done
