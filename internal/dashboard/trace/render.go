// Package trace — render.go (Story 38-5 PR11 Step 4(c) · 第 5 个 pane render
// 主体迁出 · 同 alertstrip / detail / security / intent 模式)
//
// Render() + 子视图 + 38-4 落地的 waterfall bar 全部迁出自 cmd/rnix/dashboard_trace.go
// （renderTracePane / renderTraceListView / renderTraceTreeView / renderWaterfallBar）
// · 1:1 行为等价（Story 27-9 Trace pane + 38-4 AC#5 waterfall bar 行为契约保留）。
//
// 接口签名变化（与 alertstrip / detail / security / intent 同模式）：
//   - cmd/rnix renderTracePane(*dashboardModel, width, height) string
//   - trace Render(state TraceState, ctx RenderContext, innerW, innerH int) string
//
// 解耦理由：renderTracePane 原依赖 dashboardModel.{trace, activePane} ·
// activePane 由 wrapper 注入 RenderContext.IsActive · trace 字段已抽出至
// TraceState（PR8 Step 1 落地）。
//
// helpers (SpanStatusColor / FlattenSpanTree / RenderWaterfallBar /
// FormatDurationMs / WaterfallBarWidth 常量) 一并迁入本包（pure functions ·
// 无 cmd/rnix 依赖）。
//
// renderFixedPanel 外层 border 由 cmd/rnix wrapper 包裹（同 detail/security/intent
// pattern · border / activePane 是 cmd/rnix 内部状态，不属于本 pane 内容渲染职责）。
//
// **Story 38-4 AC#5 行为契约（关键）保留**：
//   - WaterfallBarWidth = 20（固定列宽）；
//   - degraded plan A: 仅展示 duration 占比，不展示 startMs（IPC 无 StartMs 字段）；
//   - 颜色路由 SpanStatusColor (ok/error/timeout/other)；
//   - ASCII fallback (█→#, ·→.)；
//   - showBar 阈值 width >= 80（cmd/rnix wrapper 决定）。
package trace

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// WaterfallBarWidth 是 trace tree 视图右侧 mini waterfall bar 的固定总宽度（cells）。
//
// 公开常量是因为 cmd/rnix/dashboard_cross_pane_test.go (Story 38-4 落地) 直接 grep
// 该常量（waterfallBarWidth alias）做断言。
const WaterfallBarWidth = 20

// RenderContext 注入 cmd/rnix 端运行时上下文（与 alertstrip/detail/security/intent 同模式）。
//
// 字段语义：
//   - IsActive: 当前 pane 是否为 activePane（影响 border 颜色 · 由 wrapper 应用 ·
//     Render() 本身不输出 border，只输出 inner content）；
//   - ASCII: RNIX_ASCII=1 模式 · 影响 cursor (▸→>) + waterfall bar (█→#, ·→.) +
//     树连接符（└─→`--, ├─→|--, ┌─→+--, │→|）。
type RenderContext struct {
	IsActive bool
	ASCII    bool
}

// SpanStatusColor 返回 span status 对应的 lipgloss 颜色（与 cmd/rnix.spanStatusColor 等价）。
//
// Story 27-9 mapping (preserved from cmd/rnix):
//   - ok      → 42  (green)
//   - error   → 196 (red)
//   - timeout → 208 (orange)
//   - default → 240 (gray)
func SpanStatusColor(status string) lipgloss.Color {
	switch status {
	case "ok":
		return lipgloss.Color("42")
	case "error":
		return lipgloss.Color("196")
	case "timeout":
		return lipgloss.Color("208")
	default:
		return lipgloss.Color("240")
	}
}

// FormatDurationMs 把 ms 浮点数格式化为 trace 视图常用的人类可读时长。
//
// 行为等价于 cmd/rnix.formatTimelineDuration（µs/ms/s 三档）：
//   - ms < 1     → "%.0fµs"  (亚毫秒级 · 显示 µs)
//   - ms < 1000  → "%.0fms"
//   - 否则        → "%.2fs"
//
// 这是 trace pane 自包含的 helper（不引入 cmd/rnix.formatTimelineDuration 跨包
// 反向依赖）· 与 cmd/rnix 端 timeline / eval 中的同名 helper 行为完全等价。
func FormatDurationMs(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.0fµs", ms*1000)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

// FlattenSpanTree flattens a SpanTreeWire into a depth-first flat list of
// SpanFlatNode entries with rendered ASCII / Unicode tree-connector prefixes.
//
// Behaviour preserved from cmd/rnix.flattenSpanTree:
//   - tree == nil OR tree.Root == nil → returns nil；
//   - depth-first traversal · root prefix `┌─` (or `+--` ASCII)；
//   - non-root: middle child `├─` (`|--`) / last child `└─` (`'--`)；
//   - child prefix accumulates `│  ` (`|  `) / spaces；
//   - ASCII mode detected via RNIX_ASCII env var (与 cmd/rnix 端原行为 1:1 等价)。
//
// nil safety + O(N) over total nodes。
func FlattenSpanTree(tree *ipc.SpanTreeWire) []SpanFlatNode {
	if tree == nil || tree.Root == nil {
		return nil
	}
	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	var nodes []SpanFlatNode
	flattenSpanNode(tree.Root, 0, true, "", ascii, &nodes)
	return nodes
}

// flattenSpanNode is the internal recursive helper for FlattenSpanTree.
func flattenSpanNode(node *ipc.SpanNodeWire, depth int, isLast bool, parentPrefix string, ascii bool, out *[]SpanFlatNode) {
	var prefix string
	if depth == 0 {
		if ascii {
			prefix = "+-- "
		} else {
			prefix = "┌─ "
		}
	} else {
		if isLast {
			if ascii {
				prefix = parentPrefix + "`-- "
			} else {
				prefix = parentPrefix + "└─ "
			}
		} else {
			if ascii {
				prefix = parentPrefix + "|-- "
			} else {
				prefix = parentPrefix + "├─ "
			}
		}
	}
	*out = append(*out, SpanFlatNode{
		SpanID: node.SpanID,
		PID:    types.PID(node.PID),
		Name:   node.Name,
		DurMs:  node.DurationMs,
		Tokens: node.TokensUsed,
		Status: node.Status,
		Depth:  depth,
		Prefix: prefix,
		IsRoot: depth == 0,
	})
	childPrefix := parentPrefix
	if depth > 0 {
		if isLast {
			childPrefix += "   "
		} else {
			if ascii {
				childPrefix += "|  "
			} else {
				childPrefix += "│  "
			}
		}
	}
	for i, child := range node.Children {
		flattenSpanNode(&child, depth+1, i == len(node.Children)-1, childPrefix, ascii, out)
	}
}

// RenderWaterfallBar returns a fixed-width (WaterfallBarWidth) string
// approximating the relative duration of one span vs the trace total.
//
// Story 38-4 AC#5 — degraded plan A (preserved 1:1 from cmd/rnix.renderWaterfallBar):
//   - Bar is left-aligned: position 0..barLen-1 carries the span (`█`),
//     positions barLen..WaterfallBarWidth-1 carry the dim filler (`·`).
//   - We do NOT model startMs because SpanNodeWire currently has no
//     StartMs field (Dev Notes 3); users see "how long" but not "when
//     started" or "what overlaps".
//
// Inputs:
//   - traceTotalMs: total span duration of the trace; ≤ 0 means we have no
//     scale info → render an entirely dim bar.
//   - spanDurMs: this span's duration; clamped to [1, WaterfallBarWidth]
//     after proportional scaling so width never overflows.
//   - status: routes through SpanStatusColor (ok/error/timeout/other).
//   - ascii: ASCII fallback (`█`→`#`, `·`→`.`).
//
// Output width is exactly WaterfallBarWidth runes regardless of inputs;
// callers do NOT need to truncate or pad before / after this string.
func RenderWaterfallBar(traceTotalMs, spanDurMs int64, status string, ascii bool) string {
	fillChar := "█"
	dimChar := "·"
	if ascii {
		fillChar = "#"
		dimChar = "."
	}
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	if traceTotalMs <= 0 {
		return dimStyle.Render(strings.Repeat(dimChar, WaterfallBarWidth))
	}
	barLen := int(int64(WaterfallBarWidth) * spanDurMs / traceTotalMs)
	if spanDurMs > 0 && barLen < 1 {
		barLen = 1 // every non-zero span gets at least 1 cell of presence
	}
	if barLen < 0 {
		barLen = 0
	}
	if barLen > WaterfallBarWidth {
		barLen = WaterfallBarWidth
	}
	rightPad := WaterfallBarWidth - barLen
	colored := lipgloss.NewStyle().Foreground(SpanStatusColor(status)).Render(strings.Repeat(fillChar, barLen))
	if rightPad > 0 {
		colored += dimStyle.Render(strings.Repeat(dimChar, rightPad))
	}
	return colored
}

// Render renders the Trace pane inner content (excluding outer border).
//
// Behaviour contract (preserved from cmd/rnix.renderTracePane · zero behavior
// change · spec § AC5 + Story 27-9 + 38-4 AC#5):
//   - state.ViewMode == 0 → list view (TraceList summaries · cursor + scroll)；
//   - state.ViewMode == 1 → tree view (SpanFlatNodes + waterfall bar)。
//
// innerW / innerH 由 cmd/rnix wrapper 计算（width-2 / height-2 减去 border）。
//
// Return value: inner content string · caller (cmd/rnix wrapper) 用
// renderFixedPanel(content, width, height, borderColor) 包裹外层 border。
func Render(state TraceState, ctx RenderContext, innerW, innerH int) string {
	if state.ViewMode == 1 {
		return renderTreeView(state, ctx, innerW, innerH)
	}
	return renderListView(state, ctx, innerW, innerH)
}

// renderListView renders the Trace List view (ViewMode == 0).
//
// Behaviour preserved from cmd/rnix.renderTraceListView:
//   - state.Err != nil → "    Error: <err>"
//   - empty Summaries → 中文提示 "无追踪数据..."
//   - 否则 column header (TRACE ID / ROOT / SPANS / DUR) + viewport rows。
func renderListView(state TraceState, ctx RenderContext, _ int, height int) string {
	var b strings.Builder
	b.WriteString(" Traces\n")

	if state.Err != nil {
		fmt.Fprintf(&b, "\n    Error: %v\n", state.Err)
		return b.String()
	}

	if len(state.Summaries) == 0 {
		b.WriteString("\n    无追踪数据。使用 rnix spawn 或 rnix compose up 执行任务以生成追踪。\n")
		return b.String()
	}

	// Header
	fmt.Fprintf(&b, " %-16s  %-12s  %5s  %8s\n", "TRACE ID", "ROOT", "SPANS", "DUR")

	visibleLines := max(height-3, 1)
	startIdx := max(state.ScrollOffset, 0)
	endIdx := min(startIdx+visibleLines, len(state.Summaries))

	for i := startIdx; i < endIdx; i++ {
		ts := state.Summaries[i]
		cursor := "  "
		if i == state.Cursor {
			if ctx.ASCII {
				cursor = "> "
			} else {
				cursor = "▸ "
			}
		}
		tid := ts.TraceID
		if len(tid) > 16 {
			tid = tid[:16]
		}
		dur := FormatDurationMs(float64(ts.TotalDurationMs))
		fmt.Fprintf(&b, "%s%-16s  %-12s  %5d  %8s\n", cursor, tid, ts.RootSpanName, ts.SpanCount, dur)
	}

	return b.String()
}

// renderTreeView renders the Trace Tree view (ViewMode == 1).
//
// Behaviour preserved from cmd/rnix.renderTraceTreeView:
//   - SelectedSpanTree == nil OR empty SpanFlatNodes → "Trace: (loading...)"；
//   - 否则 metadata header (TRACE ID / spans / dur / tokens) + viewport rows；
//   - 每行 cursor + Prefix + Name + (PID N) + dur + tokStr + status；
//   - showBar 阈值：innerW >= 80 → 末尾追加 20-char waterfall bar (Story 38-4 AC#5)。
func renderTreeView(state TraceState, ctx RenderContext, width, height int) string {
	var b strings.Builder

	if state.SelectedSpanTree == nil || len(state.SpanFlatNodes) == 0 {
		b.WriteString(" Trace: (loading...)\n")
		return b.String()
	}

	meta := state.SelectedSpanTree.Metadata
	tid := state.SelectedTraceID
	if len(tid) > 16 {
		tid = tid[:16]
	}
	dur := FormatDurationMs(float64(meta.TotalDurationMs))
	fmt.Fprintf(&b, " Trace: %s  %d spans  %s  %s tok\n", tid, meta.TotalSpans, dur, timeline.FormatTokenCount(meta.TotalTokens))

	visibleLines := max(height-2, 1)
	startIdx := max(state.SpanScrollOffset, 0)
	endIdx := min(startIdx+visibleLines, len(state.SpanFlatNodes))

	// Story 38-4 AC#5: append a 20-char waterfall bar to each row when the
	// terminal can spare the columns.
	showBar := width >= 80
	traceTotalMs := meta.TotalDurationMs

	for i := startIdx; i < endIdx; i++ {
		node := state.SpanFlatNodes[i]
		cursor := "  "
		if i == state.SpanCursor {
			if ctx.ASCII {
				cursor = "> "
			} else {
				cursor = "▸ "
			}
		}

		dur := FormatDurationMs(float64(node.DurMs))
		tokStr := fmt.Sprintf("%stok", timeline.FormatTokenCount(node.Tokens))
		statusStyle := lipgloss.NewStyle().Foreground(SpanStatusColor(node.Status))

		line := fmt.Sprintf("%s%s%s (PID %d)  %s  %s  %s",
			cursor, node.Prefix, node.Name, node.PID, dur, tokStr, statusStyle.Render(node.Status))
		if showBar {
			line += "  " + RenderWaterfallBar(traceTotalMs, node.DurMs, node.Status, ctx.ASCII)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// BottomInnerH 计算 trace pane 在 dashboard bottom-right slot 中的 inner height
// （减去 border + 横向 split）。与 cmd/rnix.traceBottomInnerH 1:1 等价。
//
// 公开供 cmd/rnix wrapper 端 traceAdjustScroll / spanAdjustScroll 复用。
func BottomInnerH(termHeight int) int {
	contentHeight := max(termHeight-4, 3)
	bottomRightH := contentHeight - contentHeight/2
	return max(bottomRightH-2, 1) // subtract border
}

// AdjustListScroll ensures state.Cursor is visible within the viewport.
//
// visibleLines computed by caller (cmd/rnix wrapper · BottomInnerH(termHeight)-3
// 与 renderListView 内部 visibleLines 公式一致 · zero behavior change)。
func AdjustListScroll(state *TraceState, visibleLines int) {
	if state == nil {
		return
	}
	visibleLines = max(visibleLines, 1)
	if state.Cursor < state.ScrollOffset {
		state.ScrollOffset = state.Cursor
	}
	if state.Cursor >= state.ScrollOffset+visibleLines {
		state.ScrollOffset = state.Cursor - visibleLines + 1
	}
}

// AdjustSpanScroll ensures state.SpanCursor is visible within the viewport.
//
// visibleLines computed by caller (cmd/rnix wrapper · BottomInnerH(termHeight)-2
// 与 renderTreeView 内部 visibleLines 公式一致 · zero behavior change)。
func AdjustSpanScroll(state *TraceState, visibleLines int) {
	if state == nil {
		return
	}
	visibleLines = max(visibleLines, 1)
	if state.SpanCursor < state.SpanScrollOffset {
		state.SpanScrollOffset = state.SpanCursor
	}
	if state.SpanCursor >= state.SpanScrollOffset+visibleLines {
		state.SpanScrollOffset = state.SpanCursor - visibleLines + 1
	}
}
