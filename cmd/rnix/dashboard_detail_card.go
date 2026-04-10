package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// renderDetailCardLeft renders the left detail card (below tree pane, 2 content lines).
// Shows Provider/Devices + Skills for running processes, or exit summary for dead ones.
func renderDetailCardLeft(m *dashboardModel, width, height int) string {
	// Separator line at top
	sep := lipgloss.NewStyle().
		Width(width).
		Foreground(lipgloss.Color(ui.ColorMuted)).
		Render(safeRepeat("─", width))

	if m.selectedPID == 0 || m.procDetail == nil {
		placeholder := lipgloss.NewStyle().
			Width(width).Height(height).
			Foreground(lipgloss.Color(ui.ColorMuted)).
			Render("  Select a process")
		return lipgloss.JoinVertical(lipgloss.Left, sep, placeholder)
	}

	proc := findSelectedProcess(m)

	// Dead/Zombie process: merged summary
	if proc != nil && (proc.State == types.StateDead || proc.State == types.StateZombie) {
		return renderDeadDetailCard(m, proc, width, height, sep)
	}

	d := m.procDetail

	// Line 1: Provider + Device count
	line1 := fmt.Sprintf("  Provider: %s │ D:%d", d.Provider, len(d.AllowedDevices))
	line1 = fitLine(line1, width)

	// Line 2: Skills
	var skillNames []string
	for _, sk := range d.Skills {
		skillNames = append(skillNames, sk.Name)
	}
	var line2 string
	if len(skillNames) > 0 {
		line2 = "  Skills: " + strings.Join(skillNames, ", ")
	} else {
		line2 = "  Skills: —"
	}
	line2 = fitLine(line2, width)

	content := lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, line1, line2),
	)
	return lipgloss.JoinVertical(lipgloss.Left, sep, content)
}

// renderDetailCardRight renders the right detail card (below timeline, 2 content lines).
// Shows Intent/Compact + Trace/Budget/Steps for running, or exit details for dead.
func renderDetailCardRight(m *dashboardModel, width, height int) string {
	sep := lipgloss.NewStyle().
		Width(width).
		Foreground(lipgloss.Color(ui.ColorMuted)).
		Render(safeRepeat("─", width))

	if m.selectedPID == 0 || m.procDetail == nil {
		placeholder := lipgloss.NewStyle().
			Width(width).Height(height).
			Foreground(lipgloss.Color(ui.ColorMuted)).
			Render("")
		return lipgloss.JoinVertical(lipgloss.Left, sep, placeholder)
	}

	proc := findSelectedProcess(m)

	// Dead/Zombie: right side shows tokens + duration
	if proc != nil && (proc.State == types.StateDead || proc.State == types.StateZombie) {
		return renderDeadDetailCardRight(m, proc, width, height, sep)
	}

	d := m.procDetail

	// Line 1: Intent + Compact stats
	intent := "—"
	if proc != nil && proc.Intent != "" {
		intent = proc.Intent
	}
	compactCount := countCompactEvents(m.sysEvents, m.selectedPID)
	line1 := fmt.Sprintf("  Intent: %s │ Compact: %d×", intent, compactCount)
	line1 = fitLine(line1, width)

	// Line 2: Trace + Budget + Steps
	stepCount := len(m.stepEntries)
	budgetPct := 0
	if d.ContextStats.ContextBudget > 0 {
		budgetPct = int(int64(d.ContextStats.TokensUsed) * 100 / int64(d.ContextStats.ContextBudget))
	}
	line2 := fmt.Sprintf("  Steps: %d │ Budget: %d%% │ Tokens: %s",
		stepCount, budgetPct, ui.FormatTokens(d.ContextStats.TokensUsed))
	line2 = fitLine(line2, width)

	content := lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, line1, line2),
	)
	return lipgloss.JoinVertical(lipgloss.Left, sep, content)
}

// renderDeadDetailCard renders left detail card for dead/zombie processes.
func renderDeadDetailCard(m *dashboardModel, proc *selectedProcRef, width, height int, sep string) string {
	checkmark := "✓"
	failmark := "✕"
	if ui.IsASCIIMode() {
		checkmark = "[ok]"
		failmark = "[FAIL]"
	}

	d := m.procDetail
	var line1 string
	if !ui.IsFailedResult(proc.Result) {
		line1 = fmt.Sprintf("  %s Done (exit 0) │ %s │ %s tokens",
			checkmark, formatLivedDuration(d), ui.FormatTokens(d.ContextStats.TokensUsed))
	} else {
		summary := proc.Result
		if len(summary) > 40 {
			summary = summary[:40] + "…"
		}
		line1 = fmt.Sprintf("  %s Failed │ %s", failmark, summary)
	}
	line1 = fitLine(line1, width)

	line2 := fmt.Sprintf("  Provider: %s │ D:%d", d.Provider, len(d.AllowedDevices))
	line2 = fitLine(line2, width)

	content := lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, line1, line2),
	)
	return lipgloss.JoinVertical(lipgloss.Left, sep, content)
}

// renderDeadDetailCardRight renders right detail card for dead/zombie processes.
func renderDeadDetailCardRight(m *dashboardModel, proc *selectedProcRef, width, height int, sep string) string {
	d := m.procDetail
	stepCount := len(m.stepEntries)
	line1 := fmt.Sprintf("  Steps: %d │ Tokens: %s", stepCount, ui.FormatTokens(d.ContextStats.TokensUsed))
	line1 = fitLine(line1, width)

	line2 := fmt.Sprintf("  %s", formatLivedDuration(d))
	line2 = fitLine(line2, width)

	content := lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, line1, line2),
	)
	return lipgloss.JoinVertical(lipgloss.Left, sep, content)
}

// selectedProcRef caches selected process fields for detail card rendering.
type selectedProcRef struct {
	State  types.ProcessState
	Intent string
	Result string
}

// findSelectedProcess returns a reference to the selected process, or nil.
func findSelectedProcess(m *dashboardModel) *selectedProcRef {
	if m.selectedPID == 0 {
		return nil
	}
	for i := range m.processes {
		if m.processes[i].PID == m.selectedPID && (m.selectedUUID == "" || m.processes[i].UUID == m.selectedUUID) {
			return &selectedProcRef{
				State:  m.processes[i].State,
				Intent: m.processes[i].Intent,
				Result: m.processes[i].Result,
			}
		}
	}
	return nil
}

// countCompactEvents counts compact events for a specific PID.
func countCompactEvents(events []UnifiedEvent, pid types.PID) int {
	count := 0
	for _, ev := range events {
		if ev.Type == EventCompact && ev.PID == pid {
			count++
		}
	}
	return count
}

// formatLivedDuration returns a formatted duration string from procDetail timestamps.
func formatLivedDuration(d *ipc.GetProcDetailResponse) string {
	if d.DeadAtMs > 0 && d.CreatedAtMs > 0 {
		durMs := d.DeadAtMs - d.CreatedAtMs
		if durMs > 0 {
			dur := time.Duration(durMs) * time.Millisecond
			return "lived " + ui.FormatDuration(dur)
		}
	}
	return "—"
}

// fitLine truncates a line to fit within width using lipgloss.Width for measurement.
func fitLine(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for lipgloss.Width(string(runes)) > width && len(runes) > 0 {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// safeRepeat calls strings.Repeat with n clamped to >= 0.
func safeRepeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// truncateRuneSafe truncates a string to maxLen runes, appending "…" if truncated.
// Moved from dashboard_focus.go (Story 34-5).
func truncateRuneSafe(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen >= len(runes) {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// resumeProcessCmd sends a Resume IPC call for the given UUID (Story 30.8 AC#4).
// Moved from dashboard_focus.go (Story 34-5).
func resumeProcessCmd(uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return resumeResultMsg{err: err}
		}
		defer client.Close()
		result, err := client.Resume(uuid)
		return resumeResultMsg{result: result, err: err}
	}
}

// fetchHeartbeatStatusCmd fetches the heartbeat monitor status via IPC (Story 30.8).
// Moved from dashboard_focus.go (Story 34-5).
func fetchHeartbeatStatusCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return heartbeatStatusMsg{err: err}
		}
		defer client.Close()
		status, err := client.HeartbeatStatus()
		return heartbeatStatusMsg{status: status, err: err}
	}
}
