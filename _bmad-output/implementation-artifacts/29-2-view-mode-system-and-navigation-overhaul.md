# Story 29.2: 视图模式系统与导航重构

Status: done

## Story

As a 平台构建者,
I want 通过数字键 1-8 直达面板、Esc 回退默认视图、Shift-Tab 反向导航,
So that 我可以零等待直达目标面板，消除 Tab 循环痛点。

## Acceptance Criteria

1. **Given** dashboardModel
   **When** 引入视图模式系统
   **Then** 新增 `viewMode` 枚举：viewDefault（默认）、viewExpanded（面板展开）、viewLLM（LLM 查看器）、viewHistory（历史列表）
   **And** 新增 `expandedPane paneType` 字段

2. **Given** 用户在任何主视图中
   **When** 按数字键 1-8
   **Then** 切换到对应面板的展开视图
   **And** 再按同一数字键回到默认视图（toggle 行为）

3. **Given** 用户在展开视图中
   **When** 按 Esc
   **Then** 回到默认视图

4. **Given** 用户在任何主视图中
   **When** 按 Shift-Tab
   **Then** 反向切换面板（与 Tab 方向相反）

5. **Given** Title bar
   **When** 处于展开视图
   **Then** 激活的面板标签显示为 `N:Name*`（无方括号 + 星号）
   **And** 其他面板标签显示为 `[N]Name`

6. **Given** Status bar
   **When** 视图模式变化
   **Then** 右侧快捷键提示根据当前视图动态更新

7. **Given** Tab 键行为
   **When** 在默认视图按 Tab
   **Then** 切换到展开视图（viewExpanded）并展开下一面板
   **And** 保留与现有行为的向后兼容

## Tasks / Subtasks

- [x] Task 1: 新增 viewMode 枚举和字段 (AC: #1)
  - [x] 在 `dashboard_types.go` 新增 `viewMode` 类型和 4 个常量
  - [x] 在 `dashboard.go` 的 `dashboardModel` 新增 `viewMode` 和 `expandedPane` 字段
- [x] Task 2: 创建 `dashboard_nav.go` 统一导航文件 (AC: #2, #3, #4, #7)
  - [x] 将 `dashboardKey` 方法从 `dashboard.go` 迁移到 `dashboard_nav.go`
  - [x] 重构为 viewMode 感知的分层分发：全局键 → 覆盖层分发 → promptPager/confirmKill → 主视图全局键 → 面板内键
  - [x] 实现数字键 1-8 直达 (`jumpToPane`)，二次按键 toggle 回 viewDefault
  - [x] 实现 Shift-Tab 反向面板切换
  - [x] 修改 Tab 行为：设置 viewMode = viewExpanded
  - [x] 实现 Esc 回退逻辑
  - [x] L/H 键映射为 stub（显示 statusMsg 提示待实现）
- [x] Task 3: 修改布局引擎 (AC: #2, #3)
  - [x] 修改 `renderDashboard()` 支持三种布局模式
  - [x] viewDefault：保持现有布局不变
  - [x] viewExpanded + paneTree：Tree 占满全宽全高
  - [x] viewExpanded + 其他 pane：Tree(40%) + 展开面板占满右侧全高（不分割 Timeline/底部）
- [x] Task 4: 重写 Title Bar (AC: #5)
  - [x] 重写 `renderDashboardTitle()` 显示带编号的面板标签
  - [x] 展开时：激活面板 `N:Name*`，其他 `[N]Name`
  - [x] 默认时：全部 `[N]Name`
  - [x] 保留连接状态和统计信息
- [x] Task 5: 重写 Status Bar (AC: #6)
  - [x] 重写 `renderDashboardStatus()` 根据 viewMode 动态显示快捷键
  - [x] viewDefault: `Tab:cycle 1-8:jump L:llm H:history Esc:back q:quit`
  - [x] viewExpanded: 面板特定提示
  - [x] 保留 ●REC 指示和 statusMsg
- [x] Task 6: 运行 `make all` 验证 (AC: 全部)
  - [x] 确保编译、lint、vet、测试全部通过
  - [x] 确保现有 11 个 dashboard 测试文件不受影响

## Dev Notes

### 实现策略：分层改造

本 Story 在 29.1 拆分基础上进行。核心变更是**引入 viewMode 状态机**控制布局和键分发，同时保持所有现有面板逻辑不变。

### 关键原则

- **向后兼容**：Tab 仍可用（行为微调：进入 viewExpanded），所有面板内 j/k/Enter 不变
- **渐进式**：viewLLM 和 viewHistory 仅定义枚举值，不实现内容（留给 Story 29.5/29.6）
- **不引入新依赖**：仅用现有 Bubbletea v2 + Lipgloss

### 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `cmd/rnix/dashboard_types.go` | 修改 | 新增 viewMode 类型和 4 个常量 |
| `cmd/rnix/dashboard.go` | 修改 | dashboardModel 新增 2 字段 + renderDashboard/Title/Status 重写 + 删除 dashboardKey 方法 |
| `cmd/rnix/dashboard_nav.go` | **新增** | 统一键绑定分发（从 dashboard.go 迁移 + 重构 dashboardKey） |

### Task 1: viewMode 枚举 — `dashboard_types.go`

在 `paneType` 定义下方（当前 L14-25）追加：

```go
type viewMode int

const (
    viewDefault  viewMode = iota // 默认：Tree + Timeline + 底部面板
    viewExpanded                 // 数字键展开某面板
    viewLLM                      // L：全屏 LLM 对话查看器（Story 29.6 实现）
    viewHistory                  // H：全屏历史进程列表（Story 29.5 实现）
)
```

### Task 1: dashboardModel 新增字段 — `dashboard.go`

在 `dashboardModel` 结构体（当前 L33-145）的 `activePane paneType` 后追加：

```go
viewMode     viewMode  // 当前视图模式（默认 viewDefault，零值即默认）
expandedPane paneType  // viewExpanded 模式下展开的面板
```

`newDashboardModel()` 无需修改——Go 零值 `viewDefault = 0` 即为默认。

### Task 2: `dashboard_nav.go` — 关键实现

**从 `dashboard.go` 迁移 `dashboardKey` 方法**（当前 L552-831），并重构为以下分层结构：

```go
package main

import (
    "fmt"
    "os"
    "os/exec"

    tea "charm.land/bubbletea/v2"

    "github.com/rnixai/rnix/internal/types"
)

func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    key := msg.String()

    // === Layer 0: 全局退出（任何模式） ===
    if key == "q" || key == "ctrl+c" {
        if m.promptPager {
            if key == "ctrl+c" { return m, tea.Quit }
            // q in promptPager = close pager
        }
        return m, tea.Quit
    }

    // === Layer 1: Prompt Pager 覆盖层（已有逻辑迁移） ===
    if m.promptPager { ... 保持现有逻辑 ... }

    // === Layer 2: Kill 确认（已有逻辑迁移） ===
    if m.confirmKill { ... 保持现有逻辑 ... }

    // === Layer 3: 覆盖层视图（viewLLM / viewHistory）===
    // Story 29.5/29.6 实现时在此处理。
    // 当前 stub：这些 viewMode 不会被进入

    // === Layer 4: Replay 模式 ===
    if m.replayMode { return m.handleReplayKey(key) }

    // === Layer 5: 主视图全局快捷键 ===
    switch key {
    case "L":
        m.statusMsg = "LLM 查看器尚未实现（Story 29.6）"
        m.statusMsgTTL = statusMsgDefaultTTL
        return m, nil
    case "H":
        m.statusMsg = "历史视图尚未实现（Story 29.5）"
        m.statusMsgTTL = statusMsgDefaultTTL
        return m, nil
    case "esc":
        if m.viewMode == viewExpanded {
            m.viewMode = viewDefault
            return m, nil
        }
        // 默认视图中 Esc 无操作（或面板内有特定行为如 traceViewMode）
    case "tab":
        m.activePane = (m.activePane + 1) % 8
        m.viewMode = viewExpanded
        m.expandedPane = m.activePane
        return m, nil
    case "shift+tab":
        m.activePane = (m.activePane + 7) % 8
        m.viewMode = viewExpanded
        m.expandedPane = m.activePane
        return m, nil
    case "1": return m.jumpToPane(paneTree)
    case "2": return m.jumpToPane(paneTimeline)
    case "3": return m.jumpToPane(paneHeatmap)
    case "4": return m.jumpToPane(paneDetail)
    case "5": return m.jumpToPane(paneIntent)
    case "6": return m.jumpToPane(paneSecurity)
    case "7": return m.jumpToPane(paneTrace)
    case "8": return m.jumpToPane(paneEval)
    }

    // === Layer 6: 面板内按键分发（与现有逻辑相同） ===
    return m.dispatchPaneKey(msg)
}

func (m dashboardModel) jumpToPane(p paneType) (tea.Model, tea.Cmd) {
    if m.viewMode == viewExpanded && m.expandedPane == p {
        m.viewMode = viewDefault
    } else {
        m.viewMode = viewExpanded
        m.expandedPane = p
    }
    m.activePane = p
    return m, nil
}

func (m dashboardModel) dispatchPaneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    // 将现有 dashboardKey L609-828 的面板内按键逻辑整体搬入此函数
    // 包括：stepTimeline v/V/p、Tree j/k/enter、Intent/Security/Trace/Eval 面板导航
    // 以及全局操作 k/a/l/r（非冲突场景）
    ...
}
```

#### 数字键与 Eval 面板冲突处理

**重要**：当前 Eval 面板内用 `1/2/3` 切换子视图（reputation/topology/synergy）。数字键 1-8 直达面板在 Layer 5 处理，优先级高于面板内按键。

解决方案：**仅在 viewDefault 模式下响应数字键直达**。在 viewExpanded 且 expandedPane == paneEval 时，数字键 1/2/3 应传递给 Eval 面板的 handleEvalKey。

```go
// Layer 5 中数字键判断需增加条件：
case "1", "2", "3", "4", "5", "6", "7", "8":
    // 如果当前已展开 Eval 面板且按的是 1/2/3，让面板处理
    if m.viewMode == viewExpanded && m.expandedPane == paneEval {
        n, _ := strconv.Atoi(key)
        if n >= 1 && n <= 3 {
            break // fall through to dispatchPaneKey
        }
    }
    // 同理 Timeline 的 1-4 filter 键
    if m.viewMode == viewExpanded && m.expandedPane == paneTimeline {
        n, _ := strconv.Atoi(key)
        if n >= 1 && n <= 4 {
            break // fall through to dispatchPaneKey
        }
    }
    // 否则执行 jumpToPane
    paneNum, _ := strconv.Atoi(key)
    return m.jumpToPane(paneType(paneNum - 1))
```

#### Esc 与 Trace 面板冲突

当前 Trace 面板 tree view 用 Esc 返回 list view（`traceViewMode` 0→1 切换）。

解决方案：在 viewExpanded + paneTrace 时，Esc 优先传递给 traceKey 处理 `traceViewMode`。只有当 `traceViewMode == 0`（已在 list view）时 Esc 才回退到 viewDefault。

```go
case "esc":
    if m.viewMode == viewExpanded {
        // 让面板先处理内部 Esc（如 Trace 的 tree→list）
        if m.expandedPane == paneTrace && m.traceViewMode != 0 {
            return m.handleTraceKey(key)
        }
        m.viewMode = viewDefault
        return m, nil
    }
```

### Task 3: 布局引擎修改 — `renderDashboard()` 在 `dashboard.go`

当前布局（L868-916）固定为：Tree(40%) + Timeline(h/2) + 底部面板(h/2)。

修改为根据 viewMode 分支：

```go
func (m dashboardModel) renderDashboard() string {
    if m.promptPager {
        return m.renderPromptPager()
    }

    w, h := m.width, m.height
    if w == 0 { w = 120 }
    if h == 0 { h = 40 }

    titleBar := m.renderDashboardTitle()
    statusBar := m.renderDashboardStatus()
    contentHeight := max(h-4, 3)

    var mainContent string
    switch m.viewMode {
    case viewExpanded:
        mainContent = m.renderExpandedLayout(w, contentHeight)
    default: // viewDefault
        mainContent = m.renderDefaultLayout(w, contentHeight)
    }

    return lipgloss.JoinVertical(lipgloss.Left, titleBar, mainContent, statusBar)
}

func (m dashboardModel) renderDefaultLayout(w, h int) string {
    // 保持现有布局：Tree(40%) + 右上 Timeline(h/2) + 右下切换面板(h/2)
    treeWidth := min(max(w*40/100, 30), 60)
    rightWidth := max(w-treeWidth, 10)
    treePane := m.renderDashboardTreePane(treeWidth, h)
    topH := h / 2
    bottomH := h - topH
    timelinePane := m.renderTimelinePane(rightWidth, topH)
    var bottomPane string
    switch m.activePane {
    case paneDetail:   bottomPane = m.renderDetailPane(rightWidth, bottomH)
    case paneIntent:   bottomPane = m.renderIntentPane(rightWidth, bottomH)
    case paneSecurity: bottomPane = m.renderSecurityPane(rightWidth, bottomH)
    case paneTrace:    bottomPane = m.renderTracePane(rightWidth, bottomH)
    case paneEval:     bottomPane = m.renderEvalPane(rightWidth, bottomH)
    default:           bottomPane = m.renderHeatmapPane(rightWidth, bottomH)
    }
    rightPane := lipgloss.JoinVertical(lipgloss.Left, timelinePane, bottomPane)
    return lipgloss.JoinHorizontal(lipgloss.Top, treePane, rightPane)
}

func (m dashboardModel) renderExpandedLayout(w, h int) string {
    if m.expandedPane == paneTree {
        // Tree 全屏
        return m.renderDashboardTreePane(w, h)
    }
    // Tree(40%) + 展开面板全高
    treeWidth := min(max(w*40/100, 30), 60)
    rightWidth := max(w-treeWidth, 10)
    treePane := m.renderDashboardTreePane(treeWidth, h)
    var expandedPane string
    switch m.expandedPane {
    case paneTimeline: expandedPane = m.renderTimelinePane(rightWidth, h)
    case paneHeatmap:  expandedPane = m.renderHeatmapPane(rightWidth, h)
    case paneDetail:   expandedPane = m.renderDetailPane(rightWidth, h)
    case paneIntent:   expandedPane = m.renderIntentPane(rightWidth, h)
    case paneSecurity: expandedPane = m.renderSecurityPane(rightWidth, h)
    case paneTrace:    expandedPane = m.renderTracePane(rightWidth, h)
    case paneEval:     expandedPane = m.renderEvalPane(rightWidth, h)
    default:           expandedPane = m.renderHeatmapPane(rightWidth, h)
    }
    return lipgloss.JoinHorizontal(lipgloss.Top, treePane, expandedPane)
}
```

### Task 4: Title Bar — `renderDashboardTitle()`

当前（L918-938）显示：`Rnix Dashboard  ● Connected | Processes: N | Tokens: X`

重写为带编号标签的导航栏：

```go
func (m dashboardModel) renderDashboardTitle() string {
    var b strings.Builder
    b.WriteString(" Rnix Dashboard")
    if m.connected {
        b.WriteString(" ●")
    } else {
        b.WriteString(" ○")
    }
    b.WriteString(" ────")

    panes := []struct {
        key  string
        name string
        pane paneType
    }{
        {"1", "Tree", paneTree},
        {"2", "Time", paneTimeline},
        {"3", "Heat", paneHeatmap},
        {"4", "Detail", paneDetail},
        {"5", "Intent", paneIntent},
        {"6", "Sec", paneSecurity},
        {"7", "Trace", paneTrace},
        {"8", "Eval", paneEval},
    }

    for _, p := range panes {
        if m.viewMode == viewExpanded && m.expandedPane == p.pane {
            fmt.Fprintf(&b, " %s:%s*", p.key, p.name)
        } else {
            fmt.Fprintf(&b, " [%s]%s", p.key, p.name)
        }
    }

    // 追加进程和 token 统计
    active := 0
    totalTokens := 0
    for _, p := range m.processes {
        if p.State == types.StateRunning || p.State == types.StateCreated {
            active++
        }
        totalTokens += p.TokensUsed
    }
    if active > 0 || totalTokens > 0 {
        fmt.Fprintf(&b, " │ %d proc %s tok", active, ui.FormatTokens(totalTokens))
    }

    return b.String()
}
```

### Task 5: Status Bar — `renderDashboardStatus()`

当前（L940-983）按 activePane 硬编码快捷键提示。

重写为按 viewMode 分层 + 面板特定提示：

```go
func (m dashboardModel) renderDashboardStatus() string {
    if m.replayMode { return m.renderReplayStatus() }
    if m.confirmKill { return fmt.Sprintf("  Kill PID %d? [y/N]", m.confirmPID) }

    rec := ""
    if m.selectedPID > 0 && m.recording[m.selectedUUID] != "" {
        rec = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("●REC") + "  "
    }

    ops := "  k:Kill a:GDB l:Log r:Rec"

    if m.statusMsg != "" {
        return fmt.Sprintf("  %s%s  |  q:Quit  Tab:Switch%s", rec, m.statusMsg, ops)
    }

    var hints string
    switch m.viewMode {
    case viewExpanded:
        hints = m.paneSpecificHints()
    default: // viewDefault
        hints = "Tab:cycle 1-8:jump L:llm H:hist Esc:back q:quit"
    }

    return fmt.Sprintf("  %s%s%s", rec, hints, ops)
}

func (m dashboardModel) paneSpecificHints() string {
    base := "1-8:jump Esc:back q:quit"
    switch m.expandedPane {
    case paneTimeline:
        return "j/k:nav v:expand p:prompt h/l:scroll +/-:zoom " + base
    case paneHeatmap:
        return "j/k:select Enter:details " + base
    case paneDetail:
        return "Detail " + base
    case paneIntent:
        return "j/k:nav Enter:jump " + base
    case paneSecurity:
        return "j/k:nav Enter:jump " + base
    case paneTrace:
        if m.traceViewMode == 0 {
            return "j/k:nav Enter:expand " + base
        }
        return "j/k:nav Enter:jump Esc:back " + base
    case paneEval:
        return "j/k:nav 1/2/3:sub-view " + base
    default:
        return "j/k:nav Enter:select " + base
    }
}
```

### 反模式防护

1. **不要创建新的 msg 类型** — viewMode 切换是同步操作，不需要异步消息
2. **不要修改任何面板渲染函数的签名** — 所有 `render*Pane(width, height int) string` 保持不变
3. **不要修改 `Update()` 方法** — 消息分发不变，只有键盘分发（dashboardKey）需要重构
4. **不要修改 `dashboardTick()`** — 数据获取逻辑不变
5. **不要修改 `handlePIDChange()`** — PID 切换逻辑不变
6. **不要引入 `strconv` 以外的新导入** — 数字键解析用 strconv.Atoi 即可
7. **Shift-Tab 键名**: Bubbletea v2 中 Shift-Tab 的 `msg.String()` 返回 `"shift+tab"`，确认测试

### 现有测试兼容性

11 个 dashboard 测试文件分布：
- `dashboard_test.go` — 基础框架测试
- `atdd_27_3..27_10` — 各面板功能测试
- `atdd_28_4` — PID 有效性测试
- `atdd_29_1` — 文件拆分结构测试

这些测试直接调用 `newDashboardModel()` 和各面板方法，不依赖 `dashboardKey` 的具体实现。viewMode 默认零值 = viewDefault，不影响现有行为。

**但要注意**：如果有测试直接调用 `renderDashboard()`，新布局在 viewDefault 下的行为必须与旧布局一致。

### Project Structure Notes

- 新增文件 `cmd/rnix/dashboard_nav.go` 遵循 29.1 建立的 `dashboard_*.go` 命名模式
- 所有文件位于 `cmd/rnix/` 目录，`package main`
- 与 `dashboard_tree.go`、`dashboard_timeline.go` 等同级

### References

- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 4] — 导航系统规格（键绑定分发表、jumpToPane、Esc 行为栈）
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 5] — 布局引擎规格（默认/展开/覆盖层布局）
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 8] — Title Bar 标签系统规格
- [Source: _bmad-output/planning-artifacts/epics/epic-29-dashboard-ux-redesign.md#Story 29.2] — Epic 定义
- [Source: _bmad-output/planning-artifacts/sprint-change-proposal-2026-03-23.md#Story 29.2] — Sprint 变更提案
- [Source: cmd/rnix/dashboard.go#L33-145] — dashboardModel 结构体
- [Source: cmd/rnix/dashboard.go#L552-831] — 当前 dashboardKey 实现（迁移目标）
- [Source: cmd/rnix/dashboard.go#L868-916] — 当前 renderDashboard 布局
- [Source: cmd/rnix/dashboard.go#L918-938] — 当前 renderDashboardTitle
- [Source: cmd/rnix/dashboard.go#L940-983] — 当前 renderDashboardStatus
- [Source: cmd/rnix/dashboard_types.go#L14-25] — paneType 枚举

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

无

### Completion Notes List

- viewMode 枚举（viewDefault/viewExpanded/viewLLM/viewHistory）和 dashboardModel 字段已添加
- dashboardKey 从 dashboard.go 迁移到新建的 dashboard_nav.go，重构为 6 层分发
- 数字键 1-8 在 viewDefault 下直达面板（jumpToPane），viewExpanded+paneEval/paneTimeline 时传递给面板处理子视图/filter
- Esc 在 viewExpanded 下回退 viewDefault，Trace 面板内 Esc 优先处理 traceViewMode
- Tab/Shift-Tab 切换面板并进入 viewExpanded
- L/H 键显示 stub 提示（Story 29.5/29.6 实现）
- 布局引擎拆分为 renderDefaultLayout（保持原有布局）和 renderExpandedLayout（Tree+全高展开面板）
- Title Bar 重写为带编号标签导航栏（[N]Name / N:Name*）
- Status Bar 按 viewMode 分层显示：viewDefault 显示全局提示，viewExpanded 显示面板特定提示
- 更新了 8 个既有测试以适配新的状态栏格式和数字键行为变更

#### Code Review 修复（CR Pass）

- **P1 修复**: 补充 9 个键盘导航集成测试（通过 dashboardKey 入口）：Tab→viewExpanded、Shift-Tab 反向导航、Esc 回退、Esc 在 viewDefault 无操作、数字键跳转、数字键 toggle、Eval 面板 1/2/3 冲突、Timeline 面板 1-4 冲突、Timeline 提示条件化
- **P2 修复**: paneSpecificHints() 中 Timeline 的 v:expand/p:prompt 提示改为仅在 stepTimelineMode=true 且有 stepEntries 时显示

### File List

- `cmd/rnix/dashboard_types.go` — 新增 viewMode 类型和 4 个常量
- `cmd/rnix/dashboard.go` — dashboardModel 新增 2 字段，renderDashboard/Title/Status 重写，删除 dashboardKey，paneSpecificHints Timeline 提示条件化
- `cmd/rnix/dashboard_nav.go` — **新增** 统一键绑定分发（dashboardKey/jumpToPane/dispatchPaneKey）
- `cmd/rnix/atdd_29_2_view_mode_nav_overhaul_test.go` — 29.2 ATDD 测试 + 9 个键盘集成测试（CR 补充）
- `cmd/rnix/dashboard_test.go` — 适配新格式（title bar/status bar/timeline filter）
- `cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go` — 适配 viewExpanded 状态栏格式
- `cmd/rnix/atdd_27_8_dashboard_security_panel_test.go` — 适配 viewExpanded 状态栏格式
- `cmd/rnix/atdd_27_9_dashboard_trace_panel_test.go` — 适配 viewExpanded 状态栏格式
- `cmd/rnix/atdd_27_10_dashboard_eval_panel_test.go` — 适配 viewExpanded 数字键和状态栏格式
