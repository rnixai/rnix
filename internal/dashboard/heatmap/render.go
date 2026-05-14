// Package heatmap — render.go (Story 38-5 PR3 Step 3)
//
// Heatmap pane 的渲染主体。从 cmd/rnix/dashboard_heatmap.go::renderHeatmapPane 整体迁入。
//
// RenderContext 注入运行时数据（IsActive/SelectedPID）让 Render 函数自洽 — cmd/rnix 端
// 通过 thin wrapper 构造 RenderContext + 调用 Render，避免本包反向依赖 cmd/rnix。
//
// 与 PR2 Step 3b tree/render.go 的设计模式一致。
package heatmap

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// RenderContext 注入 cmd/rnix 端运行时数据，避免本包反向依赖 cmd/rnix。
type RenderContext struct {
	IsActive    bool      // 当前是否为 activePane（决定 border 颜色）
	SelectedPID types.PID // 当前 attached 进程
}

// Render 渲染 Heatmap pane 主体（不含外层 fixed-panel border 包裹 · cmd/rnix 端 wrapper 负责）。
//
// 输入：
//   - state: 当前 HeatmapState（从 dashboardModel.heatmap 读取）
//   - ctx:   RenderContext（IsActive + SelectedPID）
//   - innerW, innerH: pane 内容区尺寸（已减去 border · cmd/rnix 端 renderFixedPanel 之前传入）
//
// 行为分支（与 38-1/38-2 落地等价）：
//  1. SelectedPID == 0      → "Select an agent to view heatmap"
//  2. state.Err != nil      → "✗ <err>"
//  3. len(Segments) == 0    → "Loading context profile..."
//  4. 正常渲染：横条 + label/pct 行 + 详情列表 + （Expanded 时）选中片段详情
//
// 性能：单次 Render 调用 O(len(Segments))，segments 个数上界 ≤ 7。
//
// nil 安全：state.Profile 可为 nil（branches 1/2/3 处理）；其他字段零值有 sane defaults。
func Render(state HeatmapState, ctx RenderContext, innerW, innerH int) string {
	_ = innerH // height 仅用于 cmd/rnix wrapper 包裹 renderFixedPanel，主体不消费

	var b strings.Builder

	b.WriteString(" Heatmap")
	if ctx.SelectedPID > 0 && state.Profile != nil {
		fmt.Fprintf(&b, " | PID %d", ctx.SelectedPID)
		pct := 0
		if state.Profile.ContextBudget > 0 {
			pct = state.Profile.TotalTokens * 100 / state.Profile.ContextBudget
		}
		fmt.Fprintf(&b, " | ~%s tok / %s budget (%d%%)",
			timeline.FormatTokenCount(state.Profile.TotalTokens), timeline.FormatTokenCount(state.Profile.ContextBudget), pct)
	}
	b.WriteString("\n")

	if ctx.SelectedPID == 0 {
		b.WriteString("\n    Select an agent to view heatmap")
		return b.String()
	}
	if state.Err != nil {
		fmt.Fprintf(&b, "\n    ✗ %v", state.Err)
		return b.String()
	}
	if len(state.Segments) == 0 {
		b.WriteString("\n    Loading context profile...")
		return b.String()
	}

	barWidth := max(innerW-2, 10)
	for _, seg := range state.Segments {
		segW := max(int(seg.Pct/100.0*float64(barWidth)), 1)
		color := SegmentColor(seg.Kind, seg.Activity)
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		b.WriteString(catStyle.Render(strings.Repeat("█", segW)))
	}
	b.WriteString("\n")

	for _, seg := range state.Segments {
		fmt.Fprintf(&b, "%s(%.0f%%) ", seg.Label, seg.Pct)
	}
	b.WriteString("\n")

	for i, seg := range state.Segments {
		cursor := "  "
		if i == state.Cursor {
			cursor = "▸ "
		}
		actStr := ActivityLabel(seg.Activity)
		fmt.Fprintf(&b, "%s%-15s %5s tok  %5.1f%%  %s\n",
			cursor, seg.Label, timeline.FormatTokenCount(seg.Tokens), seg.Pct, actStr)
	}

	if state.Expanded && state.Cursor < len(state.Segments) {
		seg := state.Segments[state.Cursor]
		actStr := ActivityLabel(seg.Activity)
		fmt.Fprintf(&b, "\n── Selected: %s ──\n", seg.Label)
		fmt.Fprintf(&b, "%s tokens | %.1f%% | %s\n", timeline.FormatTokenCount(seg.Tokens), seg.Pct, actStr)
		if seg.Summary != "" {
			if utf8.RuneCountInString(seg.Summary) > 60 {
				runes := []rune(seg.Summary)
				b.WriteString("\"" + string(runes[:57]) + "...\"\n")
			} else {
				b.WriteString("\"" + seg.Summary + "\"\n")
			}
		}
	}

	return b.String()
}

// HandleKey 处理 Heatmap pane 的 pane-specific 键位（统一列表导航 + enter 切换 expanded）。
//
// 从 cmd/rnix/dashboard_heatmap.go::handleHeatmapKey 整体迁入；行为零变化。
//
// 输入参数：
//   - state: 当前 HeatmapState（值传递 · 修改通过返回值传回）
//   - key: 键名（"j"/"k"/"pgdn"/"pgup"/"enter"/"/"等）
//
// 返回值：
//   - 修改后的 HeatmapState
//   - statusMsg: 提示文本（key=="/"时返回 "Search not available in this pane"），其他情况空
//   - handled: 是否消费了该 key（true 表示已处理，cmd/rnix 端不再 fallthrough）
//
// 行为说明：
//   - 复用 ui.HandleListKey（38 Story 36-5 落地的统一列表导航）
//   - "/" 提示用户该 pane 不支持搜索（38 Story 36-5 P-8 落地）
//   - "enter" 切换 expanded 状态；空列表时不切换（38 Story 36-5 P-5 落地）
func HandleKey(state HeatmapState, key string) (newState HeatmapState, statusMsg string, handled bool) {
	if ui.HandleListKey(key, nil, &state.Cursor, len(state.Segments), ui.ListNavOpts{PageSize: 8}) {
		return state, "", true
	}
	if key == "/" {
		return state, "Search not available in this pane", true
	}
	if key == "enter" {
		if len(state.Segments) == 0 {
			return state, "", true
		}
		state.Expanded = !state.Expanded
		return state, "", true
	}
	return state, "", false
}

// HandlePIDChange 处理 PID 切换：清空 Profile/Segments/Cursor/Expanded/Err。
//
// 从 cmd/rnix/dashboard_heatmap.go::handleHeatmapPIDChange 迁入；行为零变化。
//
// 调用方应在 selectedPID 切换时调用：
//
//	if newPID != state.PID { state = HandlePIDChange(state) }
func HandlePIDChange(state HeatmapState) HeatmapState {
	state.Profile = nil
	state.Segments = nil
	state.Cursor = 0
	state.Expanded = false
	state.Err = nil
	return state
}
