// Package timeline — render.go (Story 38-5 PR11 Step 4(c) timeline render body
// 主体迁出的第一个 commit · renderStepFilterBar 子 render block)
//
// 本文件迁出 timeline pane 的 render body 子块。第一个迁出目标 RenderStepFilterBar
// 是 timeline 中最独立的 render block：
//   - 仅依赖 TimelineState.StepFilters map[string]bool（通过参数传入 · 不需要 dashboardModel）
//   - 其他依赖均为已迁出的公开 helper / 常量：ActionColor / ActionAbbrev /
//     TruncateAnsi（本包） + EventCompact / EventBudget / EventExit / EventStall /
//     EventImmune（已迁至 internal/dashboard/event）
//   - 64 行函数体（含 row 1 + row 2 + 末尾 done 提示） · 行为契约固化在 38-3 落地
//
// 后续 commit 将逐步迁出更复杂的 render block：
//   - RenderUnifiedStepHeader（依赖 m.processes / m.unifiedEvents / m.selectedPID 共享 EventStream
//     字段 · 需要通过 RenderContext struct 注入运行时数据）
//   - RenderTimelinePane（顶层入口 · 调用所有子 render block）
//   - RenderStepTimeline（最大块 · ~480 行 · 依赖大量共享字段 · 留至最后）
//   - RenderAggregatedTimeline（aggregation mode · 依赖 step entries + filter）
//   - RenderExpandedDetail / RenderDebugDetail（expand mode 详情 · 依赖 detail cache）
//
// 与 PR11 Step 4(c) 其他 pane render 主体迁出（detail/security/intent/trace/eval）
// 同模式：先迁最独立的子 render block · 让 timeline 包逐步累积 render 能力 · 最终
// dashboard_timeline.go 瘦身为 thin wrapper · 不破坏 38-1/2/3/4 行为契约。
//
// **零 cmd/rnix 反向依赖**：本包只 import internal/dashboard/event + ui + lipgloss + stdlib。
package timeline

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// System event type 常量字符串字面量（与 internal/dashboard/event.Event* 等价 ·
// 自包含 · 避免反向 import event 包形成循环依赖）：
//
//   - event 包 import timeline（UnifiedEvent.StepEntry *timeline.StepEntry）·
//     timeline 反向 import event 会形成循环 · Go module 边界硬约束
//   - 6 个字符串字面量复制成本远低于循环依赖的工程成本（与 FormatDurationMs
//     在 timeline / trace / eval 三包各 8 行复制同模式）
//   - 行为契约保证：值与 event.EventCompact / EventBudget / EventExit / EventStall /
//     EventImmune 完全等价（"compact"/"budget"/"exit"/"stall"/"immune"）·
//     event 包变更时本地复制需同步更新（grep "compact\\|budget\\|exit\\|stall\\|immune" 检查）
const (
	stepEventCompact = "compact"
	stepEventBudget  = "budget"
	stepEventExit    = "exit"
	stepEventStall   = "stall"
	stepEventImmune  = "immune"
)

// RenderStepFilterBar 渲染 timeline filter editing mode 的双行 header（与 cmd/rnix
// .renderStepFilterBar 等价 · Story 38-5 PR11 Step 4(c) 第一个 timeline render
// block 迁出 · 行为契约固化在 38-3 落地）。
//
// **行为契约（不变性 · ATDD 27-3 + 36-3 + 38-3 测试覆盖）**：
//   - Row 1 "Step:"（行首加 2 空格）：7 个 step action 类型 [t/p/a/c/s/r/z] 一行展示
//     - 每个 type 显示 "[<key>]<abbrev> <mark>" 格式
//     - mark = ✓（filter on · ActionColor 上色）/ · （filter off · 灰色）
//     - filter on 判定：filters == nil（默认全开）OR filters[action] == true
//   - Row 2 "Events:"（行首加 1 空格）：6 个 system event 类型 [C/b/x/X/T/i] 一行展示
//     - 每个 type 显示 "[<key>]<label> <mark>" 格式
//     - mark = ✓（filter on · 青色 #00CED1 默认 system event 颜色）/ ·（灰色）
//     - 相同 filter on 判定逻辑（含 nil 防御）
//   - 末尾固定提示 "  [*]all  f/Esc:done"（用 ColorMuted 灰色样式）
//   - 整体 TruncateAnsi 截断到 maxW（防止超宽 · profile-tolerant）
//
// **filters == nil 行为**：所有类型显示为 on（与 cmd/rnix 等价 · 防御默认空 map 渲染）。
// **maxW <= 0 行为**：返回空字符串（TruncateAnsi 边界保护）。
//
// 不再返回 (b strings.Builder) · 直接返回 string · 与其他 RenderXxx helper 同模式。
func RenderStepFilterBar(filters map[string]bool, maxW int) string {
	var b strings.Builder
	b.WriteString(" Step:  ")

	// Row 1: Step action types
	stepTypes := []struct {
		key    string
		label  string
		action string
	}{
		{"t", ActionAbbrev("tool_call"), "tool_call"},
		{"p", ActionAbbrev("plan"), "plan"},
		{"a", ActionAbbrev("text"), "text"},
		{"c", ActionAbbrev("complete"), "complete"},
		{"s", ActionAbbrev("spawn"), "spawn"},
		{"r", ActionAbbrev("replan"), "replan"},
		{"z", ActionAbbrev("specialize"), "specialize"},
	}

	for _, t := range stepTypes {
		on := filters == nil || filters[t.action]
		mark := "✓"
		color := ActionColor(t.action)
		if !on {
			mark = "·"
			color = lipgloss.Color(ui.ColorMuted)
		}
		catStyle := lipgloss.NewStyle().Foreground(color)
		fmt.Fprintf(&b, " [%s]%s %s", t.key, catStyle.Render(t.label), mark)
	}

	// Row 2: System event types
	b.WriteString("\n Events:")

	sysTypes := []struct {
		key       string
		label     string
		eventType string
	}{
		{"C", "compact", stepEventCompact},
		{"b", "budget", stepEventBudget},
		{"x", "spawn", "sys_spawn"},
		{"X", "exit", stepEventExit},
		{"T", "stall", stepEventStall},
		{"i", "immune", stepEventImmune},
	}

	for _, t := range sysTypes {
		on := filters == nil || filters[t.eventType]
		mark := "✓"
		color := lipgloss.Color("#00CED1") // default system event color
		if !on {
			mark = "·"
			color = lipgloss.Color(ui.ColorMuted)
		}
		catStyle := lipgloss.NewStyle().Foreground(color)
		fmt.Fprintf(&b, " [%s]%s %s", t.key, catStyle.Render(t.label), mark)
	}
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	b.WriteString("  [*]all  " + dimStyle.Render("f/Esc:done"))
	return TruncateAnsi(b.String(), maxW)
}
