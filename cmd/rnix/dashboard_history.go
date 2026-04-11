package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// --- History view (Story 29-5) ---

// enterHistoryView switches to the full-screen history overlay.
func (m dashboardModel) enterHistoryView() (tea.Model, tea.Cmd) {
	m.viewMode = viewHistory
	m.historyCursor = 0
	m.historyScrollOffset = 0
	m.historySearchQuery = ""
	m.historySearchMode = false
	return m, fetchAllProcsCmd()
}

// fetchAllProcsCmd issues an IPC call to retrieve all (active+historical) processes.
func fetchAllProcsCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return historyProcsMsg{err: err}
		}
		defer client.Close()
		procs, err := client.ListAllProcs()
		return historyProcsMsg{procs: procs, err: err}
	}
}

// sortHistoryProcs sorts historyProcs in-place according to historySortMode.
func (m *dashboardModel) sortHistoryProcs() {
	switch m.historySortMode {
	case 0: // time — CreatedAt descending (newest first)
		slices.SortFunc(m.historyProcs, func(a, b vfs.ProcInfo) int {
			if a.CreatedAt.After(b.CreatedAt) {
				return -1
			}
			if a.CreatedAt.Before(b.CreatedAt) {
				return 1
			}
			return 0
		})
	case 1: // name — Agent ascending
		slices.SortFunc(m.historyProcs, func(a, b vfs.ProcInfo) int {
			al := strings.ToLower(agentLabel(a))
			bl := strings.ToLower(agentLabel(b))
			if al < bl {
				return -1
			}
			if al > bl {
				return 1
			}
			return 0
		})
	case 2: // PID — descending
		slices.SortFunc(m.historyProcs, func(a, b vfs.ProcInfo) int {
			if a.PID > b.PID {
				return -1
			}
			if a.PID < b.PID {
				return 1
			}
			return 0
		})
	}
}

// filteredHistoryProcs returns the subset of historyProcs matching the current search query.
func (m dashboardModel) filteredHistoryProcs() []vfs.ProcInfo {
	if m.historySearchQuery == "" {
		return m.historyProcs
	}
	q := strings.ToLower(m.historySearchQuery)
	var result []vfs.ProcInfo
	for _, p := range m.historyProcs {
		if strings.Contains(strings.ToLower(agentLabel(p)), q) {
			result = append(result, p)
		}
	}
	return result
}

// agentLabel returns a display label for a ProcInfo (model > provider > intent).
func agentLabel(p vfs.ProcInfo) string {
	if p.Model != "" {
		return p.Model
	}
	if p.Provider != "" {
		return p.Provider
	}
	if p.Intent != "" {
		runes := []rune(p.Intent)
		if len(runes) > 20 {
			return string(runes[:17]) + "..."
		}
		return p.Intent
	}
	return "—"
}

// historyKey handles key presses while the history overlay is active.
func (m dashboardModel) historyKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Search mode intercepts most keys
	if m.historySearchMode {
		return m.historySearchKey(msg)
	}

	filtered := m.filteredHistoryProcs()

	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		if m.historySearchQuery != "" {
			m.historySearchQuery = ""
			m.historyCursor = 0
			m.historyScrollOffset = 0
			return m, nil
		}
		m.viewMode = viewDefault
		return m, nil
	case "j", "down":
		if m.historyCursor < len(filtered)-1 {
			m.historyCursor++
			m.adjustHistoryScroll()
		}
		return m, nil
	case "k", "up":
		if m.historyCursor > 0 {
			m.historyCursor--
			m.adjustHistoryScroll()
		}
		return m, nil
	case "pgdown":
		if len(filtered) > 0 {
			pageSize := m.historyVisibleLines()
			m.historyCursor = min(m.historyCursor+pageSize, len(filtered)-1)
			m.adjustHistoryScroll()
		}
		return m, nil
	case "pgup":
		pageSize := m.historyVisibleLines()
		m.historyCursor = max(m.historyCursor-pageSize, 0)
		m.adjustHistoryScroll()
		return m, nil
	case "g", "home":
		m.historyCursor = 0
		m.historyScrollOffset = 0
		return m, nil
	case "G", "shift+G", "end":
		if len(filtered) > 0 {
			m.historyCursor = len(filtered) - 1
			m.adjustHistoryScroll()
		}
		return m, nil
	case "enter":
		if m.historyCursor < len(filtered) {
			proc := filtered[m.historyCursor]
			m.selectedPID = proc.PID
			m.selectedUUID = proc.UUID
			m.viewMode = viewDefault
			// Sync treeCursor to the selected process so the tick loop's
			// unconditional selectProcess(treeRows[treeCursor]) doesn't
			// immediately overwrite the selection we just made.
			for i, row := range m.treeRows {
				if (proc.UUID != "" && row.proc.UUID == proc.UUID) ||
					(proc.UUID == "" && row.proc.PID == proc.PID) {
					m.treeCursor = i
					break
				}
			}
			m2, cmd := m.handlePIDChange()
			return m2, cmd
		}
		return m, nil
	case "L", "shift+L":
		if m.historyCursor < len(filtered) {
			proc := filtered[m.historyCursor]
			m.selectedPID = proc.PID
			m.selectedUUID = proc.UUID
			return m.enterLLMViewer()
		}
		return m, nil
	case "/":
		m.historySearchMode = true
		m.historySearchQuery = ""
		return m, nil
	case "1":
		m.historySortMode = 0
		m.sortHistoryProcs()
		m.historyCursor = 0
		m.historyScrollOffset = 0
		return m, nil
	case "2":
		m.historySortMode = 1
		m.sortHistoryProcs()
		m.historyCursor = 0
		m.historyScrollOffset = 0
		return m, nil
	case "3":
		m.historySortMode = 2
		m.sortHistoryProcs()
		m.historyCursor = 0
		m.historyScrollOffset = 0
		return m, nil
	}

	return m, nil
}

// historySearchKey handles keys while in search mode.
func (m dashboardModel) historySearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.historySearchMode = false
		m.historySearchQuery = ""
		m.historyCursor = 0
		m.historyScrollOffset = 0
		return m, nil
	case "enter":
		m.historySearchMode = false
		m.historyCursor = 0
		m.historyScrollOffset = 0
		return m, nil
	case "backspace":
		if len(m.historySearchQuery) > 0 {
			runes := []rune(m.historySearchQuery)
			m.historySearchQuery = string(runes[:len(runes)-1])
			m.historyCursor = 0
			m.historyScrollOffset = 0
		}
		return m, nil
	default:
		// Only accept printable runes
		if utf8.RuneCountInString(key) == 1 {
			r := []rune(key)[0]
			if r >= 32 { // printable
				m.historySearchQuery += key
				m.historyCursor = 0
				m.historyScrollOffset = 0
			}
		}
		return m, nil
	}
}

// historyVisibleLines returns the number of process rows visible in the history overlay,
// matching the layout used by renderHistoryView. This keeps scroll logic and render in sync.
func (m *dashboardModel) historyVisibleLines() int {
	h := m.height
	if h == 0 {
		h = 40
	}
	// Mirror renderDashboard's contentHeight calculation:
	//   titleLines(1) + statusBar(1) + alertReserve
	alertH := alertStripHeight(len(m.alertEvents), m.alertExpanded)
	alertReserve := alertH
	if alertH > 0 {
		alertReserve = alertH + 1
	}
	contentHeight := max(h-1-1-alertReserve, 3) // 1 = titleBar, 1 = statusBar
	// renderHistoryView subtracts 4: title(1) + header(1) + stats_gap(1) + stats(1)
	return max(contentHeight-4, 1)
}

// adjustHistoryScroll ensures the cursor is visible within the scroll window.
func (m *dashboardModel) adjustHistoryScroll() {
	visibleLines := m.historyVisibleLines()
	if m.historyCursor < m.historyScrollOffset {
		m.historyScrollOffset = m.historyCursor
	}
	if m.historyCursor >= m.historyScrollOffset+visibleLines {
		m.historyScrollOffset = m.historyCursor - visibleLines + 1
	}
	m.historyScrollOffset = max(m.historyScrollOffset, 0)
}

// renderHistoryView renders the full-screen history overlay.
func (m dashboardModel) renderHistoryView(width, height int) string {
	filtered := m.filteredHistoryProcs()

	// Clamp cursor to valid range
	if m.historyCursor >= len(filtered) {
		m.historyCursor = max(len(filtered)-1, 0)
	}

	var b strings.Builder

	// Title bar
	sortLabels := []string{"Time", "Name", "PID"}
	sortInfo := fmt.Sprintf("Sort: %s", sortLabels[m.historySortMode])
	if m.historySearchMode {
		fmt.Fprintf(&b, " History | %d procs | %s | Search: %s_\n", len(filtered), sortInfo, m.historySearchQuery)
	} else if m.historySearchQuery != "" {
		fmt.Fprintf(&b, " History | %d/%d procs | %s | Filter: %q\n", len(filtered), len(m.historyProcs), sortInfo, m.historySearchQuery)
	} else {
		fmt.Fprintf(&b, " History | %d procs | %s\n", len(filtered), sortInfo)
	}

	// Column header
	header := fmt.Sprintf("  %-5s %-3s %-14s %-14s %8s %10s %8s %5s %s",
		"PID", "ST", "AGENT", "MODEL", "TOKENS", "CREATED", "ELAPSED", "EXIT", "REASON")
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Process rows
	visibleLines := max(height-4, 1) // title + header + stats + margin

	// Safety: clamp scrollOffset to valid range
	if len(filtered) > 0 {
		m.historyScrollOffset = max(m.historyScrollOffset, 0)
		m.historyScrollOffset = min(m.historyScrollOffset, len(filtered)-1)
	} else {
		m.historyScrollOffset = 0
	}

	endIdx := min(m.historyScrollOffset+visibleLines, len(filtered))

	now := time.Now()
	for i := m.historyScrollOffset; i < endIdx; i++ {
		p := filtered[i]
		cursor := "  "
		if i == m.historyCursor {
			cursor = "▸ "
		}

		stSym := ui.StateSymbol(p.State, p.Result)
		agent := agentLabel(p)
		if utf8.RuneCountInString(agent) > 14 {
			agent = truncateRuneSafe(agent, 13)
		}

		model := p.Model
		if utf8.RuneCountInString(model) > 14 {
			model = truncateRuneSafe(model, 13)
		}
		if model == "" {
			model = "—"
		}

		tokens := ui.FormatTokens(p.TokensUsed)

		created := p.CreatedAt.Format("15:04:05")

		var elapsed string
		if p.State == types.StateDead && !p.DeadAt.IsZero() {
			elapsed = ui.FormatDuration(p.DeadAt.Sub(p.CreatedAt))
		} else {
			elapsed = ui.FormatDuration(now.Sub(p.CreatedAt))
		}

		exitStr := "—"
		if p.State == types.StateDead {
			if ui.IsFailedResult(p.Result) {
				exitStr = "1"
			} else {
				exitStr = "0"
			}
		}

		reason := p.Result
		if utf8.RuneCountInString(reason) > 20 {
			reason = truncateRuneSafe(reason, 19)
		}
		if reason == "" {
			reason = "—"
		}

		line := fmt.Sprintf("%s%-5d %-3s %-14s %-14s %8s %10s %8s %5s %s",
			cursor, p.PID, stSym, agent, model, tokens, created, elapsed, exitStr, reason)

		// Color the line based on state
		switch p.State {
		case types.StateDead:
			if ui.IsFailedResult(p.Result) {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(line)
			} else {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render(line)
			}
		case types.StateRunning:
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Bottom stats: Running/Done/Failed counts, total tokens, avg elapsed
	b.WriteString(m.renderHistoryStats(filtered))

	return b.String()
}

// renderHistoryStats renders the summary statistics line.
// Stats: Running, Done, Failed counts + total tokens + average elapsed.
func (m dashboardModel) renderHistoryStats(procs []vfs.ProcInfo) string {
	var running, done, failed, totalTokens int
	var totalElapsed time.Duration
	deadCount := 0

	for _, p := range procs {
		totalTokens += p.TokensUsed
		switch p.State {
		case types.StateRunning, types.StateCreated:
			running++
		case types.StateDead:
			if ui.IsFailedResult(p.Result) {
				failed++
			} else {
				done++
			}
			if !p.DeadAt.IsZero() {
				totalElapsed += p.DeadAt.Sub(p.CreatedAt)
				deadCount++
			}
		case types.StateZombie:
			running++ // count zombie as still active for display
		}
	}

	avg := "—"
	if deadCount > 0 {
		avgDur := totalElapsed / time.Duration(deadCount)
		avg = ui.FormatDuration(avgDur)
	}

	runStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))

	return fmt.Sprintf("\n %s  %s  %s  |  Total: %s tok  |  Avg: %s",
		runStyle.Render(fmt.Sprintf("Running: %d●", running)),
		doneStyle.Render(fmt.Sprintf("Done: %d✓", done)),
		failStyle.Render(fmt.Sprintf("Failed: %d✕", failed)),
		ui.FormatTokens(totalTokens),
		avg,
	)
}
