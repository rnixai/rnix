package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

func (m dashboardModel) renderDashboardTreePane(width, height int) string {
	isActive := m.activePane == paneTree
	isExpanded := m.viewMode == viewExpanded && m.expandedPane == paneTree

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	// Determine which rows and cursor position to use
	displayRows := m.tree.Rows
	displayCursor := m.tree.Cursor
	displayOffset := m.tree.Offset
	if isExpanded && m.tree.SearchQuery != "" {
		displayRows = m.filteredExpandedRows()
		displayCursor = m.tree.SearchCursor
		displayOffset = m.tree.SearchOffset
	}

	var b strings.Builder
	sortLabel := treeSortLabels[m.tree.SortMode]
	dirArrow := "↓"
	if m.tree.SortAsc {
		dirArrow = "↑"
	}
	ascIdx := 0
	if m.tree.SortAsc {
		ascIdx = 1
	}
	dirLabel := treeSortDirLabels[m.tree.SortMode][ascIdx]
	if ui.IsASCIIMode() {
		dirLabel = treeSortDirLabelsASCII[m.tree.SortMode][ascIdx]
	}

	// Title line — show search state when in expanded mode
	if isExpanded {
		pos := min(displayCursor+1, len(displayRows))
		total := len(displayRows)
		if total == 0 {
			pos = 0
		}
		if m.tree.SearchMode {
			fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d | Search: %s_\n",
				sortLabel, dirArrow, dirLabel, pos, total, m.tree.SearchQuery)
		} else if m.tree.SearchQuery != "" {
			fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d | Filter: %q  (Esc to clear)\n",
				sortLabel, dirArrow, dirLabel, pos, total, m.tree.SearchQuery)
		} else {
			if len(m.tree.Rows) > 0 {
				fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d | / to search\n",
					sortLabel, dirArrow, dirLabel, min(m.tree.Cursor+1, len(m.tree.Rows)), len(m.tree.Rows))
			} else {
				fmt.Fprintf(&b, " Agent Tree [%s %s %s] | / to search\n", sortLabel, dirArrow, dirLabel)
			}
		}
	} else {
		if len(m.tree.Rows) > 0 {
			pos := min(m.tree.Cursor+1, len(m.tree.Rows))
			fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d\n", sortLabel, dirArrow, dirLabel, pos, len(m.tree.Rows))
		} else {
			fmt.Fprintf(&b, " Agent Tree [%s %s %s]\n", sortLabel, dirArrow, dirLabel)
		}
	}

	now := time.Now()
	reused := reusedPIDs(m.processes)
	// Stats bar occupies 2 lines (blank line + content) when expanded
	statsLines := 0
	if isExpanded {
		statsLines = 2
	}
	visibleLines := max(innerH-1-statsLines, 1)
	showCtxBar := innerW >= 30

	// Build collapsed intent map for common prefix folding (AC4)
	collapsedIntents := m.buildCollapsedIntents()

	linesRendered := 0
	for i := displayOffset; i < len(displayRows) && linesRendered < visibleLines; i++ {
		row := displayRows[i]
		cursor := "  "
		if i == displayCursor {
			cursor = "▸ "
		}

		// State badge (AC1)
		stateMark := ui.StateBadge(row.Proc.State, row.Proc.Result)

		// Paused indicator: override state badge when process is paused
		if row.Proc.IsPaused && (row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated) {
			if ui.IsASCIIMode() {
				stateMark = "[P]"
			} else {
				stateMark = "⏸"
			}
		}

		// Stale detection (Story 30.5) — skip paused processes (they intentionally stop heartbeats)
		isStale := !row.Proc.IsPaused &&
			(row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated) &&
			ui.IsStale(row.Proc.LastHeartbeat, row.Proc.StepTimeout)

		// Suspended reason abbreviation (Story 30.8 AC#6)
		isSuspended := row.Proc.State == types.StateSuspended
		if isSuspended {
			stateMark += " " + suspendReasonAbbrev(row.Proc.SuspendReason)
		}

		// Collapsed dead subtree prefix (AC3)
		collapsePrefix := ""
		if row.Collapsed {
			if ui.IsASCIIMode() {
				collapsePrefix = "> "
			} else {
				collapsePrefix = "▶ "
			}
		}

		// Intent: use collapsed prefix if available (AC4)
		intent := strings.ReplaceAll(row.Proc.Intent, "\n", " ")
		if intent == "" {
			intent = "—"
		}
		if collapsed, ok := collapsedIntents[row.Proc.UUID]; ok {
			intent = collapsed
		}

		// Compact metadata suffix
		pidPart := fmt.Sprintf("%d", row.Proc.PID)

		isDead := row.Proc.State == types.StateDead || row.Proc.State == types.StateZombie
		isDeadOk := isDead && !ui.IsFailedResult(row.Proc.Result)
		isDeadFail := isDead && ui.IsFailedResult(row.Proc.Result)
		var tokens string
		var elapsed string

		if isDead {
			if isExpanded {
				// Expanded mode: show reason text alongside the exit marker.
				// Strip newlines/control chars so embedded \n in LLM output
				// cannot split a single process row into multiple display lines.
				resultText := strings.Map(func(r rune) rune {
					if r == '\n' || r == '\r' || r == '\t' {
						return ' '
					}
					return r
				}, row.Proc.Result)
				resultText = strings.TrimSpace(resultText)
				if resultText == "" {
					resultText = "—"
				}
				runes := []rune(resultText)
				if len(runes) > 22 {
					resultText = string(runes[:21]) + "…"
				}
				if ui.IsFailedResult(row.Proc.Result) {
					tokens = ui.ErrorStyle.Render("✗ " + resultText)
				} else {
					tokens = "✓ " + resultText
				}
			} else {
				// Compact exit marker (result detail in Detail Card)
				if ui.IsFailedResult(row.Proc.Result) {
					if ui.IsASCIIMode() {
						tokens = ui.ErrorStyle.Render("x")
					} else {
						tokens = ui.ErrorStyle.Render("✗")
					}
				} else {
					if ui.IsASCIIMode() {
						tokens = "ok"
					} else {
						tokens = "✓"
					}
				}
			}
			if !row.Proc.DeadAt.IsZero() {
				elapsed = ui.FormatDuration(row.Proc.DeadAt.Sub(row.Proc.CreatedAt))
			} else if row.Proc.IsPaused && !row.Proc.PausedAt.IsZero() {
				elapsed = ui.FormatDuration(row.Proc.PausedAt.Sub(row.Proc.CreatedAt))
			} else {
				elapsed = ui.FormatDuration(now.Sub(row.Proc.CreatedAt))
			}
		} else {
			if row.Proc.ContextBudget > 0 {
				pct := max(0, min(int(int64(row.Proc.TokensUsed)*100/int64(row.Proc.ContextBudget)), 100))
				tokens = fmt.Sprintf("%s/%s(%d%%)",
					ui.FormatTokens(row.Proc.TokensUsed),
					ui.FormatTokens(row.Proc.ContextBudget), pct)
				if pct >= 80 {
					tokens = ui.WarningStyle.Render(tokens)
				}
			} else {
				tokens = ui.FormatTokens(row.Proc.TokensUsed)
			}
			if row.Proc.IsPaused && !row.Proc.PausedAt.IsZero() {
				elapsed = ui.FormatDuration(row.Proc.PausedAt.Sub(row.Proc.CreatedAt))
			} else {
				elapsed = ui.FormatDuration(now.Sub(row.Proc.CreatedAt))
			}
		}

		// PID reuse indicator
		if _, isReused := reused[row.Proc.PID]; isReused {
			uuidShort := row.Proc.UUID
			if len(uuidShort) > 6 {
				uuidShort = uuidShort[:6]
			}
			pidPart = fmt.Sprintf("%d(%s)", row.Proc.PID, uuidShort)
		}

		// Recording indicator
		rec := ""
		if m.recording[row.Proc.UUID] != "" {
			rec = " ●"
		}

		// Orchestration annotation (Story 34.7)
		orchAnnotation := orchestrationAnnotation(row.Proc)

		// Wall-clock start time (compact: HH:MM when width allows, expanded: HH:MM:SS)
		var wallClock string
		if !row.Proc.CreatedAt.IsZero() {
			if isExpanded {
				wallClock = ui.FormatWallClock(row.Proc.CreatedAt)
			} else if innerW >= 50 {
				wallClock = ui.FormatWallClockShort(row.Proc.CreatedAt)
			}
		}

		// New process highlight (Story 36-2 AC-4): sparkle icon + highlight bg
		isNewProcess := false
		if firstSeen, ok := m.tree.ProcessFirstSeenAt[row.Proc.PID]; ok && now.Sub(firstSeen) < highlightDisplayWindow {
			isNewProcess = true
		}

		// New process highlight prefix (Story 36-2 AC-4/AC-5)
		highlightPrefix := ""
		if isNewProcess {
			if ui.IsASCIIMode() {
				highlightPrefix = "* "
			} else {
				highlightPrefix = "✨ "
			}
		}

		// Calculate available width for intent (elastic).
		// Filter empty parts so an absent state badge (dead+success) doesn't produce double-space.
		prefixW := lipgloss.Width(highlightPrefix+cursor+collapsePrefix+row.Prefix)
		suffixParts := []string{pidPart, stateMark, wallClock, tokens, elapsed}
		if orchAnnotation != "" {
			suffixParts = append(suffixParts, orchAnnotation)
		}
		var filtered []string
		for _, p := range suffixParts {
			if p != "" {
				filtered = append(filtered, p)
			}
		}
		suffixStr := strings.Join(filtered, " ") + rec
		suffixW := lipgloss.Width(suffixStr)
		intentW := max(innerW-prefixW-suffixW-3, 8)
		intentTrunc := truncateRuneWidth(intent, intentW)

		// Most active process highlight (AC5): bold if event within 2s
		isMostActive := false
		if row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated {
			if lastEvt, ok := m.tree.LastEventByPID[row.Proc.PID]; ok {
				if now.Sub(lastEvt) < 2*time.Second {
					isMostActive = true
				}
			}
		}

		// Use display-width padding to avoid CJK double-width chars causing line wraps.
		intentPad := max(intentW-runewidth.StringWidth(intentTrunc), 0)

		line := fmt.Sprintf("%s%s%s%s%s%s %s",
			highlightPrefix, cursor, collapsePrefix, row.Prefix, intentTrunc, strings.Repeat(" ", intentPad), suffixStr)
		if isNewProcess {
			if ui.IsASCIIMode() {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			} else {
				line = ui.HighlightStyle.Render(line)
			}
		} else if isStale {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Render(line)
		} else if isSuspended {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFD93D")).
				Render(line)
		} else if isMostActive {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		} else if isDeadFail {
			// Style failed rows red so the row itself signals failure (badge removed).
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(line)
		} else if isDeadOk {
			// Dim successfully-completed rows so they recede visually.
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
		linesRendered++

		// Context bar for running/created processes only (AC2)
		if showCtxBar && (row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated) && linesRendered < visibleLines {
			if row.Proc.ContextBudget > 0 {
				barPrefix := strings.Repeat(" ", lipgloss.Width(cursor+collapsePrefix+row.Prefix))
				ctxBar := renderCtxBar(row.Proc.TokensUsed, row.Proc.ContextBudget, 10)
				b.WriteString(barPrefix + ctxBar + "\n")
				linesRendered++
			}
		}
	}

	// Stats bar — only in expanded mode
	if isExpanded {
		allProcs := make([]vfs.ProcInfo, len(m.tree.Rows))
		for i, r := range m.tree.Rows {
			allProcs[i] = r.Proc
		}
		b.WriteString(m.renderHistoryStats(allProcs))
		b.WriteString("\n")
	}

	return renderFixedPanel(b.String(), width, height, borderColor)
}

// filteredExpandedRows returns tree rows whose agent label or intent text
// contains the current treeSearchQuery (case-insensitive).
func (m dashboardModel) filteredExpandedRows() []flatRow {
	if m.tree.SearchQuery == "" {
		return m.tree.Rows
	}
	q := strings.ToLower(m.tree.SearchQuery)
	result := make([]flatRow, 0, len(m.tree.Rows))
	for _, row := range m.tree.Rows {
		label := strings.ToLower(agentLabel(row.Proc))
		intent := strings.ToLower(row.Proc.Intent)
		if strings.Contains(label, q) || strings.Contains(intent, q) {
			result = append(result, row)
		}
	}
	return result
}

// suspendReasonAbbrev maps a SuspendReason string to a short tag for the tree pane (Story 30.8 AC#6).
func suspendReasonAbbrev(reason string) string {
	switch {
	case reason == "":
		return ""
	case strings.HasPrefix(reason, "budget_exhausted"):
		return "[budget]"
	case reason == "heartbeat_timeout":
		return "[stalled]"
	case reason == "loop_detected":
		return "[loop]"
	default:
		return "[user]"
	}
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

// highlightDisplayWindow is how long a newly-spawned process row is
// highlighted in the Agent Tree (sparkle icon + background).
const highlightDisplayWindow = 1500 * time.Millisecond

// treeSortMode constants define sort orders for the tree pane.
const (
	treeSortTime  = 0 // CreatedAt descending (newest first); active before dead
	treeSortPID   = 1 // PID ascending
	treeSortState = 2 // State: running → created → zombie → dead; within each by PID
)

var treeSortLabels = []string{"Time", "PID", "State"}

// treeSortDirLabels[sortMode][ascIndex] — ascIndex 0=desc, 1=asc
var treeSortDirLabels = [3][2]string{
	{"新→旧", "旧→新"},   // treeSortTime
	{"大→小", "小→大"},   // treeSortPID
	{"↓", "↑"},          // treeSortState
}

var treeSortDirLabelsASCII = [3][2]string{
	{"新->旧", "旧->新"},
	{"大->小", "小->大"},
	{"v", "^"},
}

// buildProcessTree constructs a process tree from a flat list of ProcInfo.
// Uses UUID as the node key so that PID-reused processes (old dead + new active)
// can coexist in the tree without overwriting each other.
// Parent lookup uses ParentUUID first (exact match across daemon restarts),
// then falls back to PPID-based lookup for backwards compatibility.
func buildProcessTree(procs []vfs.ProcInfo, sortMode int, asc bool) []*treeNode {
	if len(procs) == 0 {
		return nil
	}

	// UUID-keyed nodes — allows multiple processes with the same PID.
	// Falls back to "!pid:<N>" key when UUID is empty.
	nodes := make(map[string]*treeNode, len(procs))
	// PID → node key index for parent lookup (active processes only, PPID fallback)
	pidToKey := make(map[types.PID]string, len(procs))
	// allPidToKey includes dead processes for dead→dead parent lookup (AC3, PPID fallback)
	allPidToKey := make(map[types.PID]string, len(procs))

	nodeKey := func(p vfs.ProcInfo) string {
		if p.UUID != "" {
			return p.UUID
		}
		return fmt.Sprintf("!pid:%d", p.PID)
	}

	for i := range procs {
		p := procs[i]
		key := nodeKey(p)
		nodes[key] = &treeNode{Proc: p}
		if p.State != types.StateDead {
			pidToKey[p.PID] = key
		}
		// For allPidToKey, prefer the earliest CreatedAt (most likely true parent)
		if existing, ok := allPidToKey[p.PID]; ok {
			if existingNode, ok2 := nodes[existing]; ok2 {
				if p.CreatedAt.Before(existingNode.Proc.CreatedAt) {
					allPidToKey[p.PID] = key
				}
			}
		} else {
			allPidToKey[p.PID] = key
		}
	}

	var roots []*treeNode
	// Track orphans by ParentUUID for synthetic parent grouping.
	// When multiple children share the same missing ParentUUID, we create a
	// synthetic placeholder node so the tree structure is preserved.
	orphansByParent := make(map[string][]*treeNode)
	for _, n := range nodes {
		p := n.Proc
		myKey := nodeKey(p)
		// Self-parent → root
		if p.PID == p.PPID {
			roots = append(roots, n)
			continue
		}

		// PPID == 0 with ParentUUID: reparented orphan — try to preserve tree
		// structure by resolving via ParentUUID before falling through to root.
		if p.PPID == 0 && p.ParentUUID != "" {
			if parent, ok := nodes[p.ParentUUID]; ok {
				parent.Children = append(parent.Children, n)
				continue
			}
			// Parent not in current view — collect for synthetic grouping
			orphansByParent[p.ParentUUID] = append(orphansByParent[p.ParentUUID], n)
			continue
		}

		// True root: PPID == 0 with no ParentUUID
		if p.PPID == 0 {
			roots = append(roots, n)
			continue
		}

		// Primary: use ParentUUID for exact parent matching (avoids PID reuse confusion)
		if p.ParentUUID != "" {
			if parent, ok := nodes[p.ParentUUID]; ok {
				parent.Children = append(parent.Children, n)
				continue
			}
			// ParentUUID set but parent not in current view → collect for synthetic grouping
			orphansByParent[p.ParentUUID] = append(orphansByParent[p.ParentUUID], n)
			continue
		}

		// Fallback: PPID-based lookup for old processes without ParentUUID
		if parentKey, ok := pidToKey[p.PPID]; ok {
			if parentKey != myKey {
				if parent, ok := nodes[parentKey]; ok {
					parent.Children = append(parent.Children, n)
					continue
				}
			}
		}
		// For dead processes, also try allPidToKey (dead→dead hierarchy, AC3)
		if p.State == types.StateDead {
			if parentKey, ok := allPidToKey[p.PPID]; ok {
				if parentKey != myKey {
					if parent, ok := nodes[parentKey]; ok {
						parent.Children = append(parent.Children, n)
						continue
					}
				}
			}
		}
		roots = append(roots, n)
	}

	// Create synthetic parent nodes for orphan groups sharing the same missing ParentUUID.
	// This preserves tree structure when the parent process was not persisted (e.g., daemon crash).
	for parentUUID, children := range orphansByParent {
		if len(children) == 1 {
			// Single orphan: just make it a root, no need for synthetic wrapper
			roots = append(roots, children[0])
			continue
		}
		// Create synthetic parent placeholder
		uuidShort := parentUUID
		if len(uuidShort) > 8 {
			uuidShort = uuidShort[:8]
		}
		synthetic := &treeNode{
			Proc: vfs.ProcInfo{
				PID:    children[0].Proc.PPID,
				UUID:   parentUUID,
				State:  types.StateDead,
				Intent: fmt.Sprintf("[missing parent %s…]", uuidShort),
			},
			Children: children,
		}
		roots = append(roots, synthetic)
	}

	sortTreeNodes(roots, sortMode, asc)
	for _, n := range nodes {
		if len(n.Children) > 1 {
			sortTreeNodes(n.Children, sortMode, asc)
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
	case types.StateSuspended:
		return 2
	case types.StateZombie:
		return 3
	case types.StateDead:
		return 4
	default:
		return 5
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
			ai, aj := ns[i].Proc, ns[j].Proc
			if !ai.CreatedAt.Equal(aj.CreatedAt) {
				return cmpTime(ai.CreatedAt, aj.CreatedAt) // primary: time
			}
			ri, rj := stateRank(ai.State), stateRank(aj.State)
			if ri != rj {
				return cmp(ri, rj) // secondary: state (running first when same time)
			}
			return ai.PID < aj.PID // tertiary: PID ascending always
		})
	case treeSortState:
		sort.SliceStable(ns, func(i, j int) bool {
			ai, aj := ns[i].Proc, ns[j].Proc
			ri, rj := stateRank(ai.State), stateRank(aj.State)
			if ri != rj {
				return cmp(ri, rj)
			}
			return ai.PID < aj.PID // secondary: PID ascending always
		})
	default:
		// PID sort
		sort.SliceStable(ns, func(i, j int) bool {
			pi, pj := ns[i].Proc.PID, ns[j].Proc.PID
			if pi != pj {
				return cmp(int(pi), int(pj))
			}
			return ns[i].Proc.CreatedAt.Before(ns[j].Proc.CreatedAt) // secondary: time ascending
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

// --- Story 34.3 helper functions ---

// renderCtxBar renders a context usage bar: "ctx ████████░░ 78%"
// barWidth is the number of bar characters (typically 10).
// Returns an empty string if contextBudget <= 0.
func renderCtxBar(tokensUsed, contextBudget, barWidth int) string {
	if contextBudget <= 0 {
		return ""
	}
	pct := max(0, min(int(int64(tokensUsed)*100/int64(contextBudget)), 100))
	filled := max(0, barWidth*pct/100)

	ascii := ui.IsASCIIMode()
	var filledChar, emptyChar string
	if ascii {
		filledChar = "#"
		emptyChar = "."
	} else {
		filledChar = "█"
		emptyChar = "░"
	}

	bar := strings.Repeat(filledChar, filled) + strings.Repeat(emptyChar, barWidth-filled)
	label := fmt.Sprintf("ctx %s %d%%", bar, pct)

	// Color based on percentage
	switch {
	case pct > 80:
		return ui.ErrorStyle.Render(label)
	case pct >= 50:
		return ui.WarningStyle.Render(label)
	default:
		return ui.SuccessStyle.Render(label)
	}
}

// flattenTreeWithCollapse converts a tree into a flat list, skipping children
// of collapsed dead subtrees. Collapsed nodes get the collapsed flag set.
func flattenTreeWithCollapse(roots []*treeNode, collapsedSet map[string]bool) []flatRow {
	if len(roots) == 0 {
		return nil
	}

	var rows []flatRow
	var walk func(node *treeNode, depth int, parentPrefix string, isLast bool, isRoot bool)
	walk = func(node *treeNode, depth int, parentPrefix string, isLast bool, isRoot bool) {
		var prefix string
		if isRoot {
			prefix = ""
		} else if isLast {
			prefix = parentPrefix + "└─ "
		} else {
			prefix = parentPrefix + "├─ "
		}

		isCollapsed := node.Proc.UUID != "" && collapsedSet[node.Proc.UUID] && len(node.Children) > 0

		rows = append(rows, flatRow{
			Proc:      node.Proc,
			Prefix:    prefix,
			Depth:     depth,
			Collapsed: isCollapsed,
		})

		if isCollapsed {
			return // skip children
		}

		var childPrefix string
		if isRoot {
			childPrefix = parentPrefix
		} else if isLast {
			childPrefix = parentPrefix + "   "
		} else {
			childPrefix = parentPrefix + "│  "
		}

		for i, child := range node.Children {
			walk(child, depth+1, childPrefix, i == len(node.Children)-1, false)
		}
	}

	for _, root := range roots {
		walk(root, 0, "", true, true)
	}
	return rows
}

// collapseCommonPrefix replaces a shared prefix among intents with "…/" (or ".../" in ASCII).
// Only collapses when the common prefix exceeds 50% of the average intent length
// and is truncated to the nearest word boundary.
func collapseCommonPrefix(intents []string) []string {
	if len(intents) <= 1 {
		return intents
	}
	prefix := longestCommonPrefix(intents)
	prefix = truncateToWordBoundary(prefix)
	if len(prefix) == 0 {
		return intents
	}
	// Only collapse if prefix > 50% of average length
	totalLen := 0
	for _, s := range intents {
		totalLen += len(s)
	}
	avgLen := totalLen / len(intents)
	if len(prefix) <= avgLen/2 {
		return intents
	}

	ellipsis := "…/"
	if ui.IsASCIIMode() {
		ellipsis = ".../"
	}

	result := make([]string, len(intents))
	changed := false
	for i, s := range intents {
		if len(prefix) >= len(s) {
			result[i] = s // prefix covers entire intent, don't collapse to empty
		} else {
			result[i] = ellipsis + s[len(prefix):]
			changed = true
		}
	}
	if !changed {
		return intents
	}
	return result
}

// longestCommonPrefix finds the longest common prefix among strings.
func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for len(prefix) > 0 && !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// truncateToWordBoundary truncates a string to the last word boundary character.
func truncateToWordBoundary(s string) string {
	if s == "" {
		return ""
	}
	// Find last word boundary in s
	lastBound := -1
	for i, c := range s {
		if c == '/' || c == ' ' || c == '-' || c == '_' {
			lastBound = i + 1 // include the boundary character
		}
	}
	if lastBound <= 0 {
		return ""
	}
	return s[:lastBound]
}

// buildCollapsedIntents groups child intents by parent UUID and applies
// common prefix collapsing (AC4). Returns a map of UUID → collapsed intent string.
func (m dashboardModel) buildCollapsedIntents() map[string]string {
	result := make(map[string]string)
	if len(m.tree.Rows) == 0 {
		return result
	}

	// Build parent UUID → child rows mapping from the tree structure
	parentChildren := make(map[string][]int) // parentUUID → indices in treeRows
	for i, row := range m.tree.Rows {
		puuid := row.Proc.ParentUUID
		if puuid != "" {
			parentChildren[puuid] = append(parentChildren[puuid], i)
			continue
		}
		// Fallback: PPID → UUID lookup for old processes without ParentUUID
		if row.Proc.PPID > 0 && row.Proc.PPID != row.Proc.PID {
			for _, other := range m.tree.Rows {
				if other.Proc.PID == row.Proc.PPID && other.Proc.UUID != "" {
					parentChildren[other.Proc.UUID] = append(parentChildren[other.Proc.UUID], i)
					break
				}
			}
		}
	}

	// For each parent group with >1 children, try prefix collapse
	for _, indices := range parentChildren {
		if len(indices) <= 1 {
			continue
		}
		intents := make([]string, len(indices))
		for j, idx := range indices {
			intents[j] = m.tree.Rows[idx].Proc.Intent
		}
		collapsed := collapseCommonPrefix(intents)
		// If collapse changed anything, store the results
		for j, idx := range indices {
			if collapsed[j] != intents[j] {
				result[m.tree.Rows[idx].Proc.UUID] = collapsed[j]
			}
		}
	}

	return result
}

// orchestrationAnnotation returns a compact orchestration label for the tree row.
// Compose: "╌╌►deps" or "-->deps" (ASCII). Pipeline: "│►[i/n]" or "|>[i/n]" (ASCII).
func orchestrationAnnotation(p vfs.ProcInfo) string {
	if p.ComposeNode != "" {
		if len(p.ComposeDeps) > 0 {
			deps := strings.Join(p.ComposeDeps, ",")
			if len(deps) > 30 {
				deps = deps[:27] + "..."
			}
			if ui.IsASCIIMode() {
				return "-->" + deps
			}
			return "╌╌►" + deps
		}
		if ui.IsASCIIMode() {
			return "[C:" + p.ComposeNode + "]"
		}
		return "◆" + p.ComposeNode
	}
	if p.PipelineTotal > 0 {
		label := fmt.Sprintf("[%d/%d]", p.PipelineIndex+1, p.PipelineTotal)
		if ui.IsASCIIMode() {
			return "|>" + label
		}
		return "│►" + label
	}
	return ""
}
