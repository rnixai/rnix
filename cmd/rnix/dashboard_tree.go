package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

func (m dashboardModel) renderDashboardTreePane(width, height int) string {
	isActive := m.activePane == paneTree

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	var b strings.Builder
	b.WriteString(" Agent Tree \n")

	now := time.Now()
	visibleLines := max(innerH-1, 1)

	endIdx := min(m.treeOffset+visibleLines, len(m.treeRows))

	for i := m.treeOffset; i < endIdx; i++ {
		row := m.treeRows[i]
		cursor := "  "
		if i == m.treeCursor {
			cursor = "▸ "
		}

		// Use StateSymbol for dead/zombie processes (AC-9), colorState for others
		var stateStr string
		if row.proc.State == types.StateDead || row.proc.State == types.StateZombie {
			stateStr = ui.StateSymbol(row.proc.State, row.proc.Result)
		} else {
			stateStr = colorState(row.proc.State)
		}

		skills := ui.FormatSkills(row.proc.Skills, 12, "—")

		tokens := ui.FormatTokens(row.proc.TokensUsed)
		if row.proc.ContextBudget > 0 {
			pct := row.proc.TokensUsed * 100 / row.proc.ContextBudget
			tokens = fmt.Sprintf("%s/%s(%d%%)",
				ui.FormatTokens(row.proc.TokensUsed),
				ui.FormatTokens(row.proc.ContextBudget), pct)
			if pct >= 80 {
				tokens = ui.WarningStyle.Render(tokens)
			}
		}

		// AC-9: Dead processes show elapsed using DeadAt, plus exit code
		var elapsed string
		if row.proc.State == types.StateDead && !row.proc.DeadAt.IsZero() {
			elapsed = ui.FormatDuration(row.proc.DeadAt.Sub(row.proc.CreatedAt))
			exitStr := "exit:0"
			if ui.IsFailedResult(row.proc.Result) {
				exitStr = "exit:1"
			}
			elapsed = elapsed + " " + exitStr
		} else {
			elapsed = ui.FormatDuration(now.Sub(row.proc.CreatedAt))
		}

		line := fmt.Sprintf("%s%sPID %-3d %-9s %-12s %s %s",
			cursor, row.prefix, row.proc.PID, stateStr, skills, tokens, elapsed)
		if m.recording[row.proc.UUID] != "" {
			line += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("●")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

func renderDashboardPlaceholder(title, placeholder string, width, height int, active bool) string { //nolint:unused // reserved for future pane rendering
	borderColor := lipgloss.Color(ui.ColorMuted)
	if active {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	content := fmt.Sprintf(" %s \n\n    (%s)", title, placeholder)
	return style.Render(content)
}

// --- Process tree construction ---

func buildProcessTree(procs []vfs.ProcInfo) []*treeNode {
	if len(procs) == 0 {
		return nil
	}

	nodes := make(map[types.PID]*treeNode, len(procs))
	for i := range procs {
		nodes[procs[i].PID] = &treeNode{proc: procs[i]}
	}

	var roots []*treeNode
	for _, n := range nodes {
		if parent, ok := nodes[n.proc.PPID]; ok && n.proc.PID != n.proc.PPID {
			parent.children = append(parent.children, n)
		} else {
			roots = append(roots, n)
		}
	}

	sortNodes := func(ns []*treeNode) {
		sort.Slice(ns, func(i, j int) bool {
			return ns[i].proc.PID < ns[j].proc.PID
		})
	}
	sortNodes(roots)
	for _, n := range nodes {
		if len(n.children) > 1 {
			sortNodes(n.children)
		}
	}

	return roots
}
