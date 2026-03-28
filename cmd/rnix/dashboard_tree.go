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
	sortLabel := treeSortLabels[m.treeSortMode]
	dirArrow := "↓"
	if m.treeSortAsc {
		dirArrow = "↑"
	}
	fmt.Fprintf(&b, " Agent Tree [%s %s]\n", sortLabel, dirArrow)

	now := time.Now()
	reused := reusedPIDs(m.processes)
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

		// PID reuse indicator: append short UUID suffix when multiple processes share the same PID
		pidStr := fmt.Sprintf("PID %-3d", row.proc.PID)
		if _, isReused := reused[row.proc.PID]; isReused {
			uuidShort := row.proc.UUID
			if len(uuidShort) > 8 {
				uuidShort = uuidShort[:8]
			}
			if row.proc.State == types.StateDead {
				pidStr = fmt.Sprintf("PID %-3d %s", row.proc.PID,
					ui.MutedStyle.Render("("+uuidShort+")"))
			} else {
				pidStr = fmt.Sprintf("PID %-3d %s", row.proc.PID,
					ui.WarningStyle.Render("("+uuidShort+")"))
			}
		}

		line := fmt.Sprintf("%s%s%s %-9s %-12s %s %s",
			cursor, row.prefix, pidStr, stateStr, skills, tokens, elapsed)
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

// treeSortMode constants define sort orders for the tree pane.
const (
	treeSortTime  = 0 // CreatedAt descending (newest first); active before dead
	treeSortPID   = 1 // PID ascending
	treeSortState = 2 // State: running → created → zombie → dead; within each by PID
)

var treeSortLabels = []string{"Time", "PID", "State"}

// buildProcessTree constructs a process tree from a flat list of ProcInfo.
// Uses UUID as the node key so that PID-reused processes (old dead + new active)
// can coexist in the tree without overwriting each other.
// Active processes link by parent PID; dead processes are placed as roots.
// Falls back to PID key when UUID is empty (e.g. test data).
func buildProcessTree(procs []vfs.ProcInfo, sortMode int, asc bool) []*treeNode {
	if len(procs) == 0 {
		return nil
	}

	// UUID-keyed nodes — allows multiple processes with the same PID.
	// Falls back to "!pid:<N>" key when UUID is empty.
	nodes := make(map[string]*treeNode, len(procs))
	// PID → node key index for parent lookup (active processes only)
	pidToKey := make(map[types.PID]string, len(procs))

	nodeKey := func(p vfs.ProcInfo) string {
		if p.UUID != "" {
			return p.UUID
		}
		return fmt.Sprintf("!pid:%d", p.PID)
	}

	for i := range procs {
		p := procs[i]
		key := nodeKey(p)
		nodes[key] = &treeNode{proc: p}
		if p.State != types.StateDead {
			pidToKey[p.PID] = key
		}
	}

	var roots []*treeNode
	for _, n := range nodes {
		p := n.proc
		// Dead processes are always roots (parent may have been reaped or PID-reused)
		if p.State == types.StateDead {
			roots = append(roots, n)
			continue
		}
		// Self-parent or orphan → root
		if p.PID == p.PPID || p.PPID == 0 {
			roots = append(roots, n)
			continue
		}
		// Try to find parent by PPID in active processes
		if parentKey, ok := pidToKey[p.PPID]; ok {
			if parent, ok := nodes[parentKey]; ok {
				parent.children = append(parent.children, n)
				continue
			}
		}
		roots = append(roots, n)
	}

	sortTreeNodes(roots, sortMode, asc)
	for _, n := range nodes {
		if len(n.children) > 1 {
			sortTreeNodes(n.children, sortMode, asc)
		}
	}

	return roots
}

// stateRank maps process state to a sort priority (lower = higher priority).
func stateRank(s types.ProcessState) int {
	switch s {
	case types.StateRunning:
		return 0
	case types.StateCreated:
		return 1
	case types.StateZombie:
		return 2
	case types.StateDead:
		return 3
	default:
		return 4
	}
}

// sortTreeNodes sorts a slice of treeNode pointers by the given sort mode.
// All sort modes use PID as a secondary key to prevent position jumping
// when the primary sort key is equal (e.g., same CreatedAt or same state).
func sortTreeNodes(ns []*treeNode, sortMode int, asc bool) {
	cmp := func(a, b int) bool {
		if asc {
			return a < b
		}
		return a > b
	}
	cmpTime := func(a, b time.Time) bool {
		if asc {
			return a.Before(b)
		}
		return a.After(b)
	}

	switch sortMode {
	case treeSortTime:
		sort.SliceStable(ns, func(i, j int) bool {
			ai, aj := ns[i].proc, ns[j].proc
			ri, rj := stateRank(ai.State), stateRank(aj.State)
			if ri != rj {
				return cmp(ri, rj)
			}
			if !ai.CreatedAt.Equal(aj.CreatedAt) {
				return cmpTime(ai.CreatedAt, aj.CreatedAt)
			}
			return ai.PID < aj.PID // secondary: PID ascending always
		})
	case treeSortState:
		sort.SliceStable(ns, func(i, j int) bool {
			ai, aj := ns[i].proc, ns[j].proc
			ri, rj := stateRank(ai.State), stateRank(aj.State)
			if ri != rj {
				return cmp(ri, rj)
			}
			return ai.PID < aj.PID // secondary: PID ascending always
		})
	default:
		// PID sort
		sort.SliceStable(ns, func(i, j int) bool {
			pi, pj := ns[i].proc.PID, ns[j].proc.PID
			if pi != pj {
				return cmp(int(pi), int(pj))
			}
			return ns[i].proc.CreatedAt.Before(ns[j].proc.CreatedAt) // secondary: time ascending
		})
	}
}

// reusedPIDs returns a set of PIDs that appear more than once in the process list.
// Used by the tree renderer to visually distinguish PID-reused processes.
func reusedPIDs(procs []vfs.ProcInfo) map[types.PID]int {
	counts := make(map[types.PID]int)
	for _, p := range procs {
		counts[p.PID]++
	}
	reused := make(map[types.PID]int)
	for pid, count := range counts {
		if count > 1 {
			reused[pid] = count
		}
	}
	return reused
}
