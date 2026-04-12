package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

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
