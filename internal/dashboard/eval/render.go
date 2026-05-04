// Package eval — render.go (Story 38-5 PR11 Step 4(c) · 第 6 个 pane render
// 主体迁出 · 同 alertstrip / detail / security / intent / trace 模式)
//
// Render() + 3 个子视图（Reputation / Topology / Synergy）+ 38-4 落地的
// EvalScoreColorStyle 颜色梯度 helper 全部迁出自 cmd/rnix/dashboard_eval.go
// （renderEvalPane / renderEvalReputationView / renderEvalTopologyView /
// renderEvalSynergyView / evalScoreColorStyle / evalBottomInnerH / 3 个
// AdjustScroll · TopoItemCount）· 1:1 行为等价（Story 27-10 Eval pane +
// 38-4 AC#6 evalScoreColorStyle 颜色梯度行为契约保留）。
//
// 接口签名变化（与 alertstrip / detail / security / intent / trace 同模式）：
//   - cmd/rnix renderEvalPane(*dashboardModel, width, height) string
//   - eval Render(state EvalState, ctx RenderContext, innerW, innerH int) string
//
// 解耦理由：renderEvalPane 原依赖 dashboardModel.{eval, activePane, height} ·
// activePane 由 wrapper 注入 RenderContext.IsActive · eval 字段已抽出至
// EvalState（PR9 Step 1 落地 · 13 字段聚合三子视图）· height 仅 AdjustScroll
// 接收为参数。
//
// renderFixedPanel 外层 border 由 cmd/rnix wrapper 包裹（同 detail/security/intent/trace
// pattern · border / activePane 是 cmd/rnix 内部状态，不属于本 pane 内容渲染职责）。
//
// **Story 38-4 AC#6 行为契约（关键）保留**：
//   - EvalScoreColorStyle 阈值：score >= 0.9 green / >= 0.7 yellow / < 0.7 red；
//   - NaN / +Inf / -Inf / 越界 [0,1] → red（patch P12 显式保留 · godoc 一致性）；
//   - VS SOLO 颜色：improvement < 0 green / > 0 red / == 0 default；
//   - Recommended 整行 Bold（patch P8 修复 · 每段独立携带 SGR Bold · 不被
//     `\x1b[0m` 终止）；
//   - ★ ASCII 标记：ASCII 模式下 Synergy Recommended 行加 "★ " 前缀（"★"→"*"）。
package eval

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// RenderContext 注入 cmd/rnix 端运行时上下文（与 alertstrip/detail/security/intent/trace 同模式）。
//
// 字段语义：
//   - IsActive：当前 pane 是否为 activePane（影响 border 颜色 · 由 wrapper 应用 ·
//     Render() 本身不输出 border，只输出 inner content；本 Render 不显式使用，
//     保留为未来扩展位 + 与其他 pane RenderContext 形状对齐）；
//   - ASCII：RNIX_ASCII=1 模式 · 影响 cursor (▸→>) + reinforced/recommended (★→*) +
//     trend icon (↑↓→ → ^v-) + topology arrow (→ → ->)。
type RenderContext struct {
	IsActive bool
	ASCII    bool
}

// NewRenderContextFromEnv 构造 RenderContext，从环境变量读取 ASCII 模式。
//
// 与 cmd/rnix `os.Getenv("RNIX_ASCII")` 检查等价：
//   - "1" 或 "true" → ASCII = true；
//   - 其他值（含空字符串）→ ASCII = false。
//
// 公开是因为 cmd/rnix wrapper 需要构造 RenderContext 时复用同一逻辑（避免重复
// env 检查代码）。
func NewRenderContextFromEnv(isActive bool) RenderContext {
	v := os.Getenv("RNIX_ASCII")
	return RenderContext{
		IsActive: isActive,
		ASCII:    v == "1" || v == "true",
	}
}

// EvalScoreColorStyle 将 0-1 score 映射为 lipgloss 前景颜色（与 cmd/rnix.evalScoreColorStyle 等价）。
//
// Story 38-4 AC#6 / spec § 04 改进点 13 阈值：
//   - score >= 0.9 → ColorSuccess (green)；
//   - 0.7 <= score < 0.9 → ColorWarning (yellow)；
//   - score < 0.7 → ColorError (red)。
//
// 与 38-2 pctColorStyle 的差异：pctColorStyle 接受 0-100 整数（alert/budget UX），
// 本 helper 接受 0-1 浮点（Synergy SUCCESS rate / Reputation SCORE）；阈值不同
// （60/80 vs 70/90）；两者合并需要 dispatch 或第三 "kind" 参数 · 弊大于利。
//
// **Code-review patch P12（2026-05-03）显式保留**：原实现依赖 `score >= 0.9`
// 对 NaN 求值 false（正确）但对 +Inf 求值 TRUE（错误 · +Inf 漏入 green bucket
// 与 godoc 矛盾）· 显式 IsNaN / IsInf / 越界 [0,1] 判断恢复文档契约（统一回退 red）。
//
// 性能：O(1) 纯函数 · render 热路径（每行调一次）安全。
func EvalScoreColorStyle(score float64) lipgloss.Style {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	}
	switch {
	case score >= 0.9:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	case score >= 0.7:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	}
}

// FormatDurationMs 格式化毫秒级 duration（与 cmd/rnix.formatTimelineDuration / trace.FormatDurationMs 等价）。
//
//   - ms < 1 → 微秒（"%.0fµs"）；
//   - ms < 1000 → 毫秒（"%.0fms"）；
//   - ms >= 1000 → 秒（"%.2fs"）。
//
// 复制（而非引用 trace.FormatDurationMs）：保持 eval 包零跨 pane 依赖（纯函数 · 8
// 行 · 无重复维护成本）。
func FormatDurationMs(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.0fµs", ms*1000)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

// TopoItemCount 返回 Topology 子视图的总条目数（节点 + 边）。
//
// 用于 cursor 边界计算 + scroll offset clamping。Topology nil → 0。
func TopoItemCount(state EvalState) int {
	if state.Topology == nil {
		return 0
	}
	return len(state.Topology.Nodes) + len(state.Topology.Edges)
}

// BottomInnerH 计算 eval pane 的 inner height 给 AdjustScroll 用。
//
// 与 cmd/rnix.evalBottomInnerH 等价：渲染路径 renderDashboard → renderEvalPane →
// sub-view · 三层减除（top status bar + bottom split + border）。
//
// 公式：max(termHeight - 4, 3) → 内容高度；contentHeight - contentHeight/2 →
// bottomRightH；bottomRightH - 2 → inner height（border 减 2）· 最小 1。
func BottomInnerH(termHeight int) int {
	contentHeight := max(termHeight-4, 3)
	bottomRightH := contentHeight - contentHeight/2
	return max(bottomRightH-2, 1)
}

// RepAdjustScroll 调整 Reputation 子视图的 ScrollOffset 让 RepCursor 落在可见
// 视口内（与 cmd/rnix.evalRepAdjustScroll 等价）。
//
// visibleLines = max(BottomInnerH(termHeight) - 3, 1)（减 3 = column header + tab
// header + 顶部 border 行）· 与 RenderReputationView 内的 visibleLines 计算保持一致。
//
// nil safety：state==nil 时直接返回（不 panic）。
func RepAdjustScroll(state *EvalState, termHeight int) {
	if state == nil {
		return
	}
	visibleLines := max(BottomInnerH(termHeight)-3, 1)
	if state.RepCursor < state.RepScrollOffset {
		state.RepScrollOffset = state.RepCursor
	}
	if state.RepCursor >= state.RepScrollOffset+visibleLines {
		state.RepScrollOffset = state.RepCursor - visibleLines + 1
	}
}

// TopoAdjustScroll 调整 Topology 子视图的 ScrollOffset（与 cmd/rnix.evalTopoAdjustScroll 等价）。
//
// nil safety：state==nil 时直接返回。
func TopoAdjustScroll(state *EvalState, termHeight int) {
	if state == nil {
		return
	}
	visibleLines := max(BottomInnerH(termHeight)-3, 1)
	if state.TopoCursor < state.TopoScrollOffset {
		state.TopoScrollOffset = state.TopoCursor
	}
	if state.TopoCursor >= state.TopoScrollOffset+visibleLines {
		state.TopoScrollOffset = state.TopoCursor - visibleLines + 1
	}
}

// SynAdjustScroll 调整 Synergy 子视图的 ScrollOffset（与 cmd/rnix.evalSynAdjustScroll 等价）。
//
// nil safety：state==nil 时直接返回。
func SynAdjustScroll(state *EvalState, termHeight int) {
	if state == nil {
		return
	}
	visibleLines := max(BottomInnerH(termHeight)-3, 1)
	if state.SynCursor < state.SynScrollOffset {
		state.SynScrollOffset = state.SynCursor
	}
	if state.SynCursor >= state.SynScrollOffset+visibleLines {
		state.SynScrollOffset = state.SynCursor - visibleLines + 1
	}
}

// Render 渲染 Eval pane 主体（不含外层 border · 由 cmd/rnix wrapper renderFixedPanel 包裹）。
//
// 输入：
//   - state：EvalState（13 字段 · SubView + 三子视图各 4 字段）；
//   - ctx：RenderContext（IsActive · ASCII）；
//   - innerW / innerH：border 内的可用宽高（cmd/rnix wrapper 已减 2）。
//
// 输出格式（1:1 与 cmd/rnix.renderEvalPane 等价）：
//   - 第 1 行：" Evaluation  ▸ Reputation  Topology  Synergy"（当前 SubView 加 ▸ + Bold）；
//   - 第 2-N 行：根据 SubView 调用对应 RenderXxxView，innerH-1（减去 tab header 行）。
//
// 与 cmd/rnix 不同：
//   - 不输出 renderFixedPanel border（由 wrapper 处理）；
//   - 仅返回 inner content（含 tab header）。
func Render(state EvalState, ctx RenderContext, innerW, innerH int) string {
	var b strings.Builder

	// Header with sub-view tabs (h/l to cycle)
	tabs := []string{"Reputation", "Topology", "Synergy"}
	tabParts := make([]string, 0, len(tabs))
	for i, name := range tabs {
		if i == state.SubView {
			tabParts = append(tabParts, lipgloss.NewStyle().Bold(true).Render("▸ "+name))
		} else {
			tabParts = append(tabParts, "  "+name)
		}
	}
	fmt.Fprintf(&b, " Evaluation  %s\n", strings.Join(tabParts, "  "))

	switch state.SubView {
	case 0:
		b.WriteString(RenderReputationView(state, ctx, innerW, innerH-1))
	case 1:
		b.WriteString(RenderTopologyView(state, ctx, innerW, innerH-1))
	case 2:
		b.WriteString(RenderSynergyView(state, ctx, innerW, innerH-1))
	}

	return b.String()
}

// RenderReputationView 渲染 Reputation 子视图（与 cmd/rnix.renderEvalReputationView 等价）。
//
// 4 个分支：
//   - RepErr != nil → " Error: %v"；
//   - len(Reputations) == 0 → 提示信息；
//   - 正常路径：column header + 数据行（含 cursor / score gradient / trend icon / duration）。
//
// **Story 38-4 AC#6 行为契约保留**：SCORE column 用 EvalScoreColorStyle 渲染（gradient）。
func RenderReputationView(state EvalState, ctx RenderContext, _, height int) string {
	var b strings.Builder

	if state.RepErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", state.RepErr)
		return b.String()
	}

	if len(state.Reputations) == 0 {
		b.WriteString("\n    需要更多执行数据以生成评价。使用 rnix spawn 或 rnix compose up 执行任务以积累数据。\n")
		return b.String()
	}

	cursorChar := "▸"
	if ctx.ASCII {
		cursorChar = ">"
	}

	// Header
	fmt.Fprintf(&b, " %-16s %5s  %7s  %7s  %7s  %3s  %s\n",
		"AGENT", "SCORE", "SUCCESS", "AVG TOK", "AVG DUR", "N", "TREND")

	visibleLines := max(height-2, 1)
	startIdx := state.RepScrollOffset
	endIdx := min(startIdx+visibleLines, len(state.Reputations))

	for i := startIdx; i < endIdx; i++ {
		r := state.Reputations[i]
		cursor := "  "
		if i == state.RepCursor {
			cursor = cursorChar + " "
		}

		var trendIcon string
		var trendColor string
		switch r.RecentTrend {
		case "improving":
			trendColor = ui.ColorSuccess
			if ctx.ASCII {
				trendIcon = "^"
			} else {
				trendIcon = "↑"
			}
		case "declining":
			trendColor = ui.ColorError
			if ctx.ASCII {
				trendIcon = "v"
			} else {
				trendIcon = "↓"
			}
		default:
			trendColor = ui.ColorMuted
			if ctx.ASCII {
				trendIcon = "-"
			} else {
				trendIcon = "→"
			}
		}
		trendStr := lipgloss.NewStyle().Foreground(lipgloss.Color(trendColor)).Render(trendIcon)

		durStr := FormatDurationMs(float64(r.AvgDurationMs))

		// Story 38-4 AC#6: SCORE column uses gradient (Success/Warning/Error).
		scoreStr := EvalScoreColorStyle(r.Score).Render(fmt.Sprintf("%5.2f", r.Score))
		fmt.Fprintf(&b, "%s%-16s %s  %5.1f%%  %7d  %7s  %3d  %s\n",
			cursor, r.AgentName, scoreStr, r.SuccessRate*100, r.AvgTokens, durStr, r.TotalRecords, trendStr)
	}

	return b.String()
}

// RenderTopologyView 渲染 Topology 子视图（与 cmd/rnix.renderEvalTopologyView 等价）。
//
// 4 个分支：
//   - TopoErr != nil → Error；
//   - Topology == nil → 空提示；
//   - nodeCount == 0 && edgeCount == 0 → 空提示；
//   - 正常路径：Nodes section（agent / score / connections）+ Edges section
//     （from → to / spawn / msg / total / reinforced ★）。
func RenderTopologyView(state EvalState, ctx RenderContext, _, height int) string {
	var b strings.Builder

	if state.TopoErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", state.TopoErr)
		return b.String()
	}

	if state.Topology == nil {
		b.WriteString("\n    无协作拓扑数据。运行多智能体编排以生成协作关系。\n")
		return b.String()
	}

	nodeCount := len(state.Topology.Nodes)
	edgeCount := len(state.Topology.Edges)

	if nodeCount == 0 && edgeCount == 0 {
		b.WriteString("\n    无协作拓扑数据。运行多智能体编排以生成协作关系。\n")
		return b.String()
	}

	cursorChar := "▸"
	if ctx.ASCII {
		cursorChar = ">"
	}
	reinforcedMark := "★"
	if ctx.ASCII {
		reinforcedMark = "*"
	}

	// Nodes section header (always visible)
	b.WriteString(" ── Nodes ──\n")
	fmt.Fprintf(&b, " %-16s  %5s  %s\n", "AGENT", "SCORE", "CONNECTIONS")

	totalItems := nodeCount + edgeCount
	visibleLines := max(height-2, 1) // -2 for section header + column header
	startIdx := state.TopoScrollOffset
	endIdx := min(startIdx+visibleLines, totalItems)

	edgesHeaderPrinted := false
	for i := startIdx; i < endIdx; i++ {
		cursor := "  "
		if i == state.TopoCursor {
			cursor = cursorChar + " "
		}
		if i < nodeCount {
			node := state.Topology.Nodes[i]
			fmt.Fprintf(&b, "%s%-16s  %5.2f  %11d\n", cursor, node.Agent, node.ReputationScore, node.Connections)
		} else {
			if !edgesHeaderPrinted {
				b.WriteString("\n ── Edges ──\n")
				headerArrow := "→"
				if ctx.ASCII {
					headerArrow = "->"
				}
				fmt.Fprintf(&b, " %-26s  %5s  %3s  %5s  %s\n", "FROM "+headerArrow+" TO", "SPAWN", "MSG", "TOTAL", "REINFORCED")
				edgesHeaderPrinted = true
			}
			edge := state.Topology.Edges[i-nodeCount]
			rMark := ""
			if edge.Reinforced {
				rMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render(reinforcedMark)
			}
			arrowStr := " → "
			if ctx.ASCII {
				arrowStr = " -> "
			}
			label := fmt.Sprintf("%s%s%s", edge.From, arrowStr, edge.To)
			fmt.Fprintf(&b, "%s%-26s  %5d  %3d  %5d  %s\n", cursor, label, edge.SpawnCount, edge.MsgCount, edge.Total, rMark)
		}
	}

	return b.String()
}

// RenderSynergyView 渲染 Synergy 子视图（与 cmd/rnix.renderEvalSynergyView 等价）。
//
// 3 个分支：
//   - SynErr != nil → Error；
//   - len(Synergies) == 0 → 提示；
//   - 正常路径：column header + 数据行。
//
// **Story 38-4 AC#6 行为契约保留**（关键 · code-review patch P8 修复）：
//   - SUCCESS column gradient via EvalScoreColorStyle（0-1 float）；
//   - VS SOLO 颜色：improvement < 0 → green (saves tokens) / > 0 → red (more) / == 0 → default；
//   - Recommended 整行 Bold（每段独立携带 SGR Bold 而非外层包裹 · 解决
//     `\x1b[0m` 终止外层 Bold 的问题 · plain 段也通过 plainStyle 渲染）；
//   - ASCII 模式 Recommended 行加 "★ " 前缀。
func RenderSynergyView(state EvalState, ctx RenderContext, _, height int) string {
	var b strings.Builder

	if state.SynErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", state.SynErr)
		return b.String()
	}

	if len(state.Synergies) == 0 {
		b.WriteString("\n    无技能组合数据。当 Agent 使用多个 Skill 执行任务时将自动记录。\n")
		return b.String()
	}

	cursorChar := "▸"
	if ctx.ASCII {
		cursorChar = ">"
	}

	fmt.Fprintf(&b, " %-20s  %7s  %7s  %4s  %8s  %s\n",
		"SKILLS", "SUCCESS", "AVG TOK", "EXEC", "VS SOLO", "REC")

	visibleLines := max(height-2, 1)
	startIdx := state.SynScrollOffset
	endIdx := min(startIdx+visibleLines, len(state.Synergies))

	for i := startIdx; i < endIdx; i++ {
		combo := state.Synergies[i]
		cursor := "  "
		if i == state.SynCursor {
			cursor = cursorChar + " "
		}

		skills := strings.Join(combo.Skills, ",")
		if len(skills) > 20 {
			skills = skills[:17] + "..."
		}

		// Story 38-4 AC#6 / patch P8: bold injected per-segment so each SGR
		// sequence carries `\x1b[1m` independently (avoids `\x1b[0m` terminating
		// outer Bold mid-line).
		boldOnRecommended := func(style lipgloss.Style) lipgloss.Style {
			if combo.Recommended {
				return style.Bold(true)
			}
			return style
		}

		successStyle := boldOnRecommended(EvalScoreColorStyle(combo.SuccessRate))
		successStr := successStyle.Render(fmt.Sprintf("%5.1f%%", combo.SuccessRate*100))

		improvementRaw := fmt.Sprintf("%+.1f%%", combo.TokenImprovement*100)
		var improvementBase lipgloss.Style
		switch {
		case combo.TokenImprovement < 0:
			improvementBase = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
		case combo.TokenImprovement > 0:
			improvementBase = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
		default:
			improvementBase = lipgloss.NewStyle()
		}
		improvementStr := boldOnRecommended(improvementBase).Render(improvementRaw)

		var recStr string
		if combo.Recommended {
			if ctx.ASCII {
				recStr = "Y"
			} else {
				recStr = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Bold(true).Render("✓")
			}
		}

		plainStyle := boldOnRecommended(lipgloss.NewStyle())
		cursorStr := plainStyle.Render(cursor)
		skillsStr := plainStyle.Render(fmt.Sprintf("%-20s", skills))
		tokStr := plainStyle.Render(fmt.Sprintf("%7d", combo.AvgTokens))
		execStr := plainStyle.Render(fmt.Sprintf("%4d", combo.TotalExecutions))

		line := cursorStr + skillsStr + "  " + successStr + "  " + tokStr + "  " + execStr + "  " + improvementStr + "  " + recStr
		if combo.Recommended && ctx.ASCII {
			line = "★ " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}
