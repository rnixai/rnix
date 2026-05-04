package main

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// =============================================================================
// Eval Pane (Story 27-10)
// =============================================================================

// evalScoreColorStyle maps a 0-1 score to a foreground colour following the
// Story 38-4 AC#6 / spec § 04 改进点 13 thresholds:
//   - score ≥ 0.9 → ColorSuccess (green)
//   - 0.7 ≤ score < 0.9 → ColorWarning (yellow)
//   - score < 0.7 → ColorError (red)
//
// Why a separate helper from 38-2's pctColorStyle:
//   - pctColorStyle takes 0-100 ints and uses 60/80 thresholds (alert /
//     budget UX);
//   - evalScoreColorStyle takes 0-1 floats and uses 70/90 thresholds
//     (Synergy SUCCESS rate / Reputation SCORE);
//   - merging the two would require either a dispatch on type or a third
//     "kind" parameter that obscures intent at every callsite.
//
// Performance: O(1), pure function — safe to call per row inside render
// hot paths.
//
// Out-of-range guard: NaN, +Inf, -Inf, and any score outside [0, 1] are
// treated as below-threshold (red). Code-review patch P12 (2026-05-03):
// the previous implementation relied on `score >= 0.9` evaluating to false
// for NaN (correct) but TRUE for +Inf, leaking +Inf into the green bucket
// and contradicting the godoc. The explicit IsNaN / IsInf guard restores
// the documented contract.
func evalScoreColorStyle(score float64) lipgloss.Style {
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

func fetchReputationCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return evalReputationMsg{err: err}
		}
		defer client.Close()
		resp, err := client.ReputationStatus("")
		if err != nil {
			return evalReputationMsg{err: err}
		}
		if resp != nil {
			return evalReputationMsg{summaries: resp.Summaries}
		}
		return evalReputationMsg{}
	}
}

func fetchTopologyCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return evalTopologyMsg{err: err}
		}
		defer client.Close()
		resp, err := client.TopologyQuery()
		if err != nil {
			return evalTopologyMsg{err: err}
		}
		return evalTopologyMsg{topology: resp}
	}
}

func fetchSynergyCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return evalSynergyMsg{err: err}
		}
		defer client.Close()
		resp, err := client.SynergyList()
		if err != nil {
			return evalSynergyMsg{err: err}
		}
		if resp != nil {
			return evalSynergyMsg{combos: resp.Combos}
		}
		return evalSynergyMsg{}
	}
}

func (m dashboardModel) handleEvalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "!", "h", "left":
		if key == "!" {
			m.eval.SubView = 0
		} else {
			// h/left: 向左切换子视图
			m.eval.SubView = (m.eval.SubView + 2) % 3
		}
		return m, m.evalEnsureSubViewData()
	case "@":
		m.eval.SubView = 1
		return m, m.evalEnsureSubViewData()
	case "#", "l", "right":
		if key == "#" {
			m.eval.SubView = 2
		} else {
			// l/right: 向右切换子视图
			m.eval.SubView = (m.eval.SubView + 1) % 3
		}
		return m, m.evalEnsureSubViewData()
	case "down", "j":
		switch m.eval.SubView {
		case 0:
			if len(m.eval.Reputations) > 0 && m.eval.RepCursor < len(m.eval.Reputations)-1 {
				m.eval.RepCursor++
				evalRepAdjustScroll(&m)
			}
		case 1:
			totalItems := evalTopoItemCount(&m)
			if totalItems > 0 && m.eval.TopoCursor < totalItems-1 {
				m.eval.TopoCursor++
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if len(m.eval.Synergies) > 0 && m.eval.SynCursor < len(m.eval.Synergies)-1 {
				m.eval.SynCursor++
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	case "up", "k":
		switch m.eval.SubView {
		case 0:
			if m.eval.RepCursor > 0 {
				m.eval.RepCursor--
				evalRepAdjustScroll(&m)
			}
		case 1:
			if m.eval.TopoCursor > 0 {
				m.eval.TopoCursor--
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if m.eval.SynCursor > 0 {
				m.eval.SynCursor--
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	}
	return m, nil
}

// evalEnsureSubViewData 确保当前子视图的数据已加载
func (m dashboardModel) evalEnsureSubViewData() tea.Cmd {
	switch m.eval.SubView {
	case 1:
		if m.eval.Topology == nil && m.connected {
			return fetchTopologyCmd()
		}
	case 2:
		if m.eval.Synergies == nil && m.connected {
			return fetchSynergyCmd()
		}
	}
	return nil
}

func evalTopoItemCount(m *dashboardModel) int {
	if m.eval.Topology == nil {
		return 0
	}
	return len(m.eval.Topology.Nodes) + len(m.eval.Topology.Edges)
}

func (m dashboardModel) renderEvalPane(width, height int) string {
	isActive := m.activePane == paneEval

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	var b strings.Builder

	// Header with sub-view tabs (h/l to cycle)
	tabs := []string{"Reputation", "Topology", "Synergy"}
	var tabParts []string
	for i, name := range tabs {
		if i == m.eval.SubView {
			tabParts = append(tabParts, lipgloss.NewStyle().Bold(true).Render("▸ "+name))
		} else {
			tabParts = append(tabParts, "  "+name)
		}
	}
	fmt.Fprintf(&b, " Evaluation  %s\n", strings.Join(tabParts, "  "))

	switch m.eval.SubView {
	case 0:
		b.WriteString(m.renderEvalReputationView(innerW, innerH-1))
	case 1:
		b.WriteString(m.renderEvalTopologyView(innerW, innerH-1))
	case 2:
		b.WriteString(m.renderEvalSynergyView(innerW, innerH-1))
	}

	return renderFixedPanel(b.String(), width, height, borderColor)
}

func (m dashboardModel) renderEvalReputationView(width, height int) string {
	var b strings.Builder

	if m.eval.RepErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.eval.RepErr)
		return b.String()
	}

	if len(m.eval.Reputations) == 0 {
		b.WriteString("\n    需要更多执行数据以生成评价。使用 rnix spawn 或 rnix compose up 执行任务以积累数据。\n")
		return b.String()
	}

	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	cursorChar := "▸"
	if ascii {
		cursorChar = ">"
	}

	// Header
	fmt.Fprintf(&b, " %-16s %5s  %7s  %7s  %7s  %3s  %s\n",
		"AGENT", "SCORE", "SUCCESS", "AVG TOK", "AVG DUR", "N", "TREND")

	visibleLines := max(height-2, 1)
	startIdx := m.eval.RepScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.eval.Reputations))

	for i := startIdx; i < endIdx; i++ {
		r := m.eval.Reputations[i]
		cursor := "  "
		if i == m.eval.RepCursor {
			cursor = cursorChar + " "
		}

		var trendIcon string
		var trendColor string
		switch r.RecentTrend {
		case "improving":
			trendColor = ui.ColorSuccess
			if ascii {
				trendIcon = "^"
			} else {
				trendIcon = "↑"
			}
		case "declining":
			trendColor = ui.ColorError
			if ascii {
				trendIcon = "v"
			} else {
				trendIcon = "↓"
			}
		default:
			trendColor = ui.ColorMuted
			if ascii {
				trendIcon = "-"
			} else {
				trendIcon = "→"
			}
		}
		trendStr := lipgloss.NewStyle().Foreground(lipgloss.Color(trendColor)).Render(trendIcon)

		durStr := formatTimelineDuration(float64(r.AvgDurationMs))

		// Story 38-4 AC#6: SCORE column uses gradient (Success/Warning/Error).
		scoreStr := evalScoreColorStyle(r.Score).Render(fmt.Sprintf("%5.2f", r.Score))
		fmt.Fprintf(&b, "%s%-16s %s  %5.1f%%  %7d  %7s  %3d  %s\n",
			cursor, r.AgentName, scoreStr, r.SuccessRate*100, r.AvgTokens, durStr, r.TotalRecords, trendStr)
	}

	return b.String()
}

func (m dashboardModel) renderEvalTopologyView(_, height int) string {
	var b strings.Builder

	if m.eval.TopoErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.eval.TopoErr)
		return b.String()
	}

	if m.eval.Topology == nil {
		b.WriteString("\n    无协作拓扑数据。运行多智能体编排以生成协作关系。\n")
		return b.String()
	}

	nodeCount := len(m.eval.Topology.Nodes)
	edgeCount := len(m.eval.Topology.Edges)

	if nodeCount == 0 && edgeCount == 0 {
		b.WriteString("\n    无协作拓扑数据。运行多智能体编排以生成协作关系。\n")
		return b.String()
	}

	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	cursorChar := "▸"
	if ascii {
		cursorChar = ">"
	}
	reinforcedMark := "★"
	if ascii {
		reinforcedMark = "*"
	}

	// Nodes section header (always visible)
	b.WriteString(" ── Nodes ──\n")
	fmt.Fprintf(&b, " %-16s  %5s  %s\n", "AGENT", "SCORE", "CONNECTIONS")

	totalItems := nodeCount + edgeCount
	visibleLines := max(height-2, 1) // -2 for section header + column header
	startIdx := m.eval.TopoScrollOffset
	endIdx := min(startIdx+visibleLines, totalItems)

	edgesHeaderPrinted := false
	for i := startIdx; i < endIdx; i++ {
		cursor := "  "
		if i == m.eval.TopoCursor {
			cursor = cursorChar + " "
		}
		if i < nodeCount {
			node := m.eval.Topology.Nodes[i]
			fmt.Fprintf(&b, "%s%-16s  %5.2f  %11d\n", cursor, node.Agent, node.ReputationScore, node.Connections)
		} else {
			if !edgesHeaderPrinted {
				b.WriteString("\n ── Edges ──\n")
				headerArrow := "→"
				if ascii {
					headerArrow = "->"
				}
				fmt.Fprintf(&b, " %-26s  %5s  %3s  %5s  %s\n", "FROM "+headerArrow+" TO", "SPAWN", "MSG", "TOTAL", "REINFORCED")
				edgesHeaderPrinted = true
			}
			edge := m.eval.Topology.Edges[i-nodeCount]
			rMark := ""
			if edge.Reinforced {
				rMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render(reinforcedMark)
			}
			arrowStr := " → "
			if ascii {
				arrowStr = " -> "
			}
			label := fmt.Sprintf("%s%s%s", edge.From, arrowStr, edge.To)
			fmt.Fprintf(&b, "%s%-26s  %5d  %3d  %5d  %s\n", cursor, label, edge.SpawnCount, edge.MsgCount, edge.Total, rMark)
		}
	}

	return b.String()
}

func (m dashboardModel) renderEvalSynergyView(width, height int) string {
	var b strings.Builder

	if m.eval.SynErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.eval.SynErr)
		return b.String()
	}

	if len(m.eval.Synergies) == 0 {
		b.WriteString("\n    无技能组合数据。当 Agent 使用多个 Skill 执行任务时将自动记录。\n")
		return b.String()
	}

	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	cursorChar := "▸"
	if ascii {
		cursorChar = ">"
	}

	fmt.Fprintf(&b, " %-20s  %7s  %7s  %4s  %8s  %s\n",
		"SKILLS", "SUCCESS", "AVG TOK", "EXEC", "VS SOLO", "REC")

	visibleLines := max(height-2, 1)
	startIdx := m.eval.SynScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.eval.Synergies))

	for i := startIdx; i < endIdx; i++ {
		combo := m.eval.Synergies[i]
		cursor := "  "
		if i == m.eval.SynCursor {
			cursor = cursorChar + " "
		}

		skills := strings.Join(combo.Skills, ",")
		if len(skills) > 20 {
			skills = skills[:17] + "..."
		}

		// Story 38-4 AC#6:
		//   - SUCCESS column gets gradient via evalScoreColorStyle (0-1 float).
		//   - VS SOLO column: improvement < 0 saves tokens → green; > 0
		//     spends more → red; == 0 → default.
		//   - Recommended rows render Bold across the entire line.
		//
		// Code-review patch P8 (2026-05-03): the old implementation wrapped
		// the already-styled `line` in `Bold(true).Render()`. Each pre-styled
		// segment ends with `\x1b[0m` which terminates the outer Bold
		// before the next column — visually only the leading segment was
		// bold, contradicting AC#6 ("整行 Bold"). The fix is to inject
		// Bold(true) into every per-segment Style so each SGR sequence
		// already carries `\x1b[1m`, and to render plain text segments
		// (cursor / skills / tokens / exec count) through the same plainStyle.
		boldOnRecommended := func(style lipgloss.Style) lipgloss.Style {
			if combo.Recommended {
				return style.Bold(true)
			}
			return style
		}

		successStyle := boldOnRecommended(evalScoreColorStyle(combo.SuccessRate))
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
			if ascii {
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
		if combo.Recommended && ascii {
			line = "★ " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// evalBottomInnerH returns the inner height available for eval sub-views,
// matching the render path: renderDashboard → renderEvalPane → sub-view.
func evalBottomInnerH(termHeight int) int {
	contentHeight := max(termHeight-4, 3)
	bottomRightH := contentHeight - contentHeight/2
	return max(bottomRightH-2, 1) // subtract border
}

// evalRepAdjustScroll ensures evalRepCursor is visible within the viewport.
func evalRepAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalReputationView
	if m.eval.RepCursor < m.eval.RepScrollOffset {
		m.eval.RepScrollOffset = m.eval.RepCursor
	}
	if m.eval.RepCursor >= m.eval.RepScrollOffset+visibleLines {
		m.eval.RepScrollOffset = m.eval.RepCursor - visibleLines + 1
	}
}

// evalTopoAdjustScroll ensures evalTopoCursor is visible within the viewport.
func evalTopoAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalTopologyView
	if m.eval.TopoCursor < m.eval.TopoScrollOffset {
		m.eval.TopoScrollOffset = m.eval.TopoCursor
	}
	if m.eval.TopoCursor >= m.eval.TopoScrollOffset+visibleLines {
		m.eval.TopoScrollOffset = m.eval.TopoCursor - visibleLines + 1
	}
}

// evalSynAdjustScroll ensures evalSynCursor is visible within the viewport.
func evalSynAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalSynergyView
	if m.eval.SynCursor < m.eval.SynScrollOffset {
		m.eval.SynScrollOffset = m.eval.SynCursor
	}
	if m.eval.SynCursor >= m.eval.SynScrollOffset+visibleLines {
		m.eval.SynScrollOffset = m.eval.SynCursor - visibleLines + 1
	}
}
