package main

import (
	"fmt"
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
			m.evalSubView = 0
		} else {
			// h/left: 向左切换子视图
			m.evalSubView = (m.evalSubView + 2) % 3
		}
		return m, m.evalEnsureSubViewData()
	case "@":
		m.evalSubView = 1
		return m, m.evalEnsureSubViewData()
	case "#", "l", "right":
		if key == "#" {
			m.evalSubView = 2
		} else {
			// l/right: 向右切换子视图
			m.evalSubView = (m.evalSubView + 1) % 3
		}
		return m, m.evalEnsureSubViewData()
	case "down", "j":
		switch m.evalSubView {
		case 0:
			if len(m.evalReputations) > 0 && m.evalRepCursor < len(m.evalReputations)-1 {
				m.evalRepCursor++
				evalRepAdjustScroll(&m)
			}
		case 1:
			totalItems := evalTopoItemCount(&m)
			if totalItems > 0 && m.evalTopoCursor < totalItems-1 {
				m.evalTopoCursor++
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if len(m.evalSynergies) > 0 && m.evalSynCursor < len(m.evalSynergies)-1 {
				m.evalSynCursor++
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	case "up", "k":
		switch m.evalSubView {
		case 0:
			if m.evalRepCursor > 0 {
				m.evalRepCursor--
				evalRepAdjustScroll(&m)
			}
		case 1:
			if m.evalTopoCursor > 0 {
				m.evalTopoCursor--
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if m.evalSynCursor > 0 {
				m.evalSynCursor--
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	}
	return m, nil
}

// evalEnsureSubViewData 确保当前子视图的数据已加载
func (m dashboardModel) evalEnsureSubViewData() tea.Cmd {
	switch m.evalSubView {
	case 1:
		if m.evalTopology == nil && m.connected {
			return fetchTopologyCmd()
		}
	case 2:
		if m.evalSynergies == nil && m.connected {
			return fetchSynergyCmd()
		}
	}
	return nil
}

func evalTopoItemCount(m *dashboardModel) int {
	if m.evalTopology == nil {
		return 0
	}
	return len(m.evalTopology.Nodes) + len(m.evalTopology.Edges)
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
		if i == m.evalSubView {
			tabParts = append(tabParts, lipgloss.NewStyle().Bold(true).Render("▸ "+name))
		} else {
			tabParts = append(tabParts, "  "+name)
		}
	}
	fmt.Fprintf(&b, " Evaluation  %s\n", strings.Join(tabParts, "  "))

	switch m.evalSubView {
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

	if m.evalRepErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.evalRepErr)
		return b.String()
	}

	if len(m.evalReputations) == 0 {
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
	startIdx := m.evalRepScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.evalReputations))

	for i := startIdx; i < endIdx; i++ {
		r := m.evalReputations[i]
		cursor := "  "
		if i == m.evalRepCursor {
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

		fmt.Fprintf(&b, "%s%-16s %5.2f  %5.1f%%  %7d  %7s  %3d  %s\n",
			cursor, r.AgentName, r.Score, r.SuccessRate*100, r.AvgTokens, durStr, r.TotalRecords, trendStr)
	}

	return b.String()
}

func (m dashboardModel) renderEvalTopologyView(_, height int) string {
	var b strings.Builder

	if m.evalTopoErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.evalTopoErr)
		return b.String()
	}

	if m.evalTopology == nil {
		b.WriteString("\n    无协作拓扑数据。运行多智能体编排以生成协作关系。\n")
		return b.String()
	}

	nodeCount := len(m.evalTopology.Nodes)
	edgeCount := len(m.evalTopology.Edges)

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
	startIdx := m.evalTopoScrollOffset
	endIdx := min(startIdx+visibleLines, totalItems)

	edgesHeaderPrinted := false
	for i := startIdx; i < endIdx; i++ {
		cursor := "  "
		if i == m.evalTopoCursor {
			cursor = cursorChar + " "
		}
		if i < nodeCount {
			node := m.evalTopology.Nodes[i]
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
			edge := m.evalTopology.Edges[i-nodeCount]
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

	if m.evalSynErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.evalSynErr)
		return b.String()
	}

	if len(m.evalSynergies) == 0 {
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
	startIdx := m.evalSynScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.evalSynergies))

	for i := startIdx; i < endIdx; i++ {
		combo := m.evalSynergies[i]
		cursor := "  "
		if i == m.evalSynCursor {
			cursor = cursorChar + " "
		}

		skills := strings.Join(combo.Skills, ",")
		if len(skills) > 20 {
			skills = skills[:17] + "..."
		}

		var recStr string
		if combo.Recommended {
			if ascii {
				recStr = "Y"
			} else {
				recStr = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render("✓")
			}
		}

		improvement := fmt.Sprintf("%+.1f%%", combo.TokenImprovement*100)

		fmt.Fprintf(&b, "%s%-20s  %5.1f%%  %7d  %4d  %8s  %s\n",
			cursor, skills, combo.SuccessRate*100, combo.AvgTokens, combo.TotalExecutions, improvement, recStr)
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
	if m.evalRepCursor < m.evalRepScrollOffset {
		m.evalRepScrollOffset = m.evalRepCursor
	}
	if m.evalRepCursor >= m.evalRepScrollOffset+visibleLines {
		m.evalRepScrollOffset = m.evalRepCursor - visibleLines + 1
	}
}

// evalTopoAdjustScroll ensures evalTopoCursor is visible within the viewport.
func evalTopoAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalTopologyView
	if m.evalTopoCursor < m.evalTopoScrollOffset {
		m.evalTopoScrollOffset = m.evalTopoCursor
	}
	if m.evalTopoCursor >= m.evalTopoScrollOffset+visibleLines {
		m.evalTopoScrollOffset = m.evalTopoCursor - visibleLines + 1
	}
}

// evalSynAdjustScroll ensures evalSynCursor is visible within the viewport.
func evalSynAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalSynergyView
	if m.evalSynCursor < m.evalSynScrollOffset {
		m.evalSynScrollOffset = m.evalSynCursor
	}
	if m.evalSynCursor >= m.evalSynScrollOffset+visibleLines {
		m.evalSynScrollOffset = m.evalSynCursor - visibleLines + 1
	}
}
