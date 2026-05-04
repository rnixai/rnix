package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// renderDashboardTitle renders the health indicator title bar (Story 34.2).
// Format: rnix {conn} {provider} ── {counts} ── ctx {pct}% ── budget {pct}% ── {elapsed}
// Segments are progressively trimmed for narrow terminals.
func (m dashboardModel) renderDashboardTitle() string {
	ascii := ui.IsASCIIMode()

	sep := " ── "
	if ascii {
		sep = " -- "
	}

	// Connection indicator
	var connChar string
	if m.connected {
		if ascii {
			connChar = "*"
		} else {
			connChar = "●"
		}
	} else {
		if ascii {
			connChar = "o"
		} else {
			connChar = "○"
		}
	}

	// Active process count
	active := 0
	for _, p := range m.processes {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			active++
		}
	}

	// Selected process
	var selectedProc *vfs.ProcInfo
	if m.selectedPID > 0 {
		for i := range m.processes {
			if m.processes[i].PID == m.selectedPID {
				selectedProc = &m.processes[i]
				break
			}
		}
	}

	// Provider segment (colored)
	providerStyled := ""
	if selectedProc != nil && selectedProc.Provider != "" {
		providerStyled = styleProviderName(m.connected, selectedProc)
	}

	// Count segment: NP ME KW with color
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))

	ePart := fmt.Sprintf("%dE", m.errorCount)
	if m.errorCount > 0 {
		ePart = errStyle.Render(ePart)
	}
	wPart := fmt.Sprintf("%dW", m.warnCount)
	if m.warnCount > 0 {
		wPart = warnStyle.Render(wPart)
	}
	countSeg := fmt.Sprintf("%dP %s %s", active, ePart, wPart)
	countPlain := fmt.Sprintf("%dP %dE %dW", active, m.errorCount, m.warnCount)

	// ctx% segment
	ctxPct := computeCtxPercent(m.selectedPID, m.processes)
	ctxPlain := fmt.Sprintf("ctx %d%%", ctxPct)
	ctxSeg := pctColorStyle(ctxPct).Render(ctxPlain)

	// budget% segment
	budgetPct := computeBudgetPercent(m.selectedPID, m.processes)
	budgetPlain := fmt.Sprintf("budget %d%%", budgetPct)
	budgetSeg := pctColorStyle(budgetPct).Render(budgetPlain)

	// elapsed segment
	elapsedSeg := ""
	if selectedProc != nil {
		elapsedSeg = formatElapsedHHMMSS(time.Since(selectedProc.CreatedAt))
	}

	base := "  rnix " + connChar

	// Story 34.6: Add DEBUG marker with PID and agent name when in debug mode
	if m.debugState.Mode {
		debugInfo := fmt.Sprintf("DEBUG: PID %d", m.selectedPID)
		for _, p := range m.processes {
			if p.PID == m.selectedPID && p.Intent != "" {
				debugInfo = fmt.Sprintf("DEBUG: PID %d (%s)", m.selectedPID, p.Intent)
				break
			}
		}
		debugLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true).Render(debugInfo)
		base += " " + debugLabel
	}

	// Build left part (base + optional provider)
	leftPart := base
	leftPlain := base
	if providerStyled != "" {
		leftPart = base + " " + providerStyled
		leftPlain = base + " " + selectedProc.Provider
	}

	// Build candidates from widest to narrowest (trim order: elapsed → budget → ctx → provider)
	type candidate struct {
		rendered string
		plain    string
	}

	candidates := []candidate{}

	// Level 5 (full): left + counts + ctx + budget + elapsed
	if elapsedSeg != "" {
		candidates = append(candidates, candidate{
			leftPart + sep + countSeg + sep + ctxSeg + sep + budgetSeg + sep + elapsedSeg,
			leftPlain + sep + countPlain + sep + ctxPlain + sep + budgetPlain + sep + elapsedSeg,
		})
	}
	// Level 4: left + counts + ctx + budget
	candidates = append(candidates, candidate{
		leftPart + sep + countSeg + sep + ctxSeg + sep + budgetSeg,
		leftPlain + sep + countPlain + sep + ctxPlain + sep + budgetPlain,
	})
	// Level 3: left + counts + ctx
	candidates = append(candidates, candidate{
		leftPart + sep + countSeg + sep + ctxSeg,
		leftPlain + sep + countPlain + sep + ctxPlain,
	})
	// Level 2: left + counts
	candidates = append(candidates, candidate{
		leftPart + sep + countSeg,
		leftPlain + sep + countPlain,
	})
	// Level 1 (minimum): base + NP ME (no provider, no W)
	minEPart := fmt.Sprintf("%dE", m.errorCount)
	if m.errorCount > 0 {
		minEPart = errStyle.Render(minEPart)
	}
	minCount := fmt.Sprintf("%dP %s", active, minEPart)
	candidates = append(candidates, candidate{
		base + " " + minCount,
		fmt.Sprintf("%s %dP %dE", base, active, m.errorCount),
	})

	maxW := m.width
	if maxW == 0 {
		maxW = 120
	}

	var titleLine string
	for _, c := range candidates {
		if lipgloss.Width(c.plain) <= maxW {
			titleLine = c.rendered
			break
		}
	}
	if titleLine == "" {
		titleLine = candidates[len(candidates)-1].rendered
	}

	// Panel tabs on second line (AC5: move pane labels to second row)
	if maxW >= 60 {
		tabLine := m.renderPanelTabsLine()
		// Story 38.1 AC#6: Mode Strip third line (only when modes are non-empty).
		modeStrip := m.renderModeStrip()
		if modeStrip != "" {
			return titleLine + "\n" + tabLine + "\n" + modeStrip
		}
		return titleLine + "\n" + tabLine
	}
	return titleLine
}

// renderModeStrip renders the active modes for the current pane, sourced from
// Layer 2 KeyLayer.ActiveModesFn (Story 38.1 AC#6).
//
// Format: "  ⌥ filter:tool · sort:time · follow:on" (UTF-8) or
//         "  [M] filter:tool | sort:time | follow:on" (ASCII).
//
// Returns empty string when no modes are active (caller should skip the line).
func (m dashboardModel) renderModeStrip() string {
	if m.dispatcher == nil {
		return ""
	}
	modes := m.dispatcher.ActiveModesFor(ui.PaneID(m.activePane), m)
	if len(modes) == 0 {
		return ""
	}
	ascii := ui.IsASCIIMode()
	prefix := "⌥"
	sep := " · "
	if ascii {
		prefix = "[M]"
		sep = " | "
	}
	parts := make([]string, 0, len(modes))
	for _, md := range modes {
		parts = append(parts, md.Name+":"+md.Value)
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	return "  " + dim.Render(prefix+" "+strings.Join(parts, sep))
}

// renderPanelTabsLine renders the [1]Tree [2]Time ... pane navigation labels.
//
// Story 38-4 AC#1: when m.paneHasUnread[pane] is true, append a red dot
// (`●` in unicode mode, `*` in ASCII mode) immediately after the tab label
// — e.g. `[2]Time●` indicates the Timeline pane has unread events. The dot
// inherits the active-bold style when on the active pane (which only
// happens transiently before clearPaneUnread fires) and uses ColorError red
// otherwise. Active vs inactive ANSI colour for the rest of the label is
// unchanged from previous Stories.
//
// Width contract: this entire line is hidden by the caller when terminal
// width < 60 cols (see renderDashboardTitle), so the extra rune does not
// affect narrow-terminal layouts. Each dot adds 1 rune (no East Asian
// Width inflation; ASCII fallback is also 1 byte).
func (m dashboardModel) renderPanelTabsLine() string {
	selectedStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	dotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	ascii := ui.IsASCIIMode()
	dotChar := "●"
	if ascii {
		dotChar = "*"
	}

	panes := []struct {
		key  string
		name string
		pane paneType
	}{
		{"1", "Tree", paneTree},
		{"2", "Time", paneTimeline},
		{"3", "Heat", paneHeatmap},
		{"4", "Detail", paneDetail},
		{"5", "Intent", paneIntent},
		{"6", "Sec", paneSecurity},
		{"7", "Trace", paneTrace},
		{"8", "Eval", paneEval},
	}

	var b strings.Builder
	b.WriteString("  ")
	for i, p := range panes {
		label := fmt.Sprintf("[%s]%s", p.key, p.name)
		// Append unread dot before the trailing separator so spacing stays consistent.
		if int(p.pane) >= 0 && int(p.pane) < len(m.paneHasUnread) && m.paneHasUnread[p.pane] {
			label += dotStyle.Render(dotChar)
		}
		if i < len(panes)-1 {
			label += " "
		}
		if m.activePane == p.pane {
			b.WriteString(selectedStyle.Render(label))
		} else {
			b.WriteString(dimStyle.Render(label))
		}
	}
	return b.String()
}

// computeHealthCounts counts errors and warnings from process state and events.
// E: Dead+failed processes + error events (deduped by PID).
// W: Processes with ctx>=80% + heartbeat-stalled processes.
func computeHealthCounts(processes []vfs.ProcInfo, events []UnifiedEvent, heartbeat *ipc.HeartbeatStatusResponse) (errorCount, warnCount int) {
	errorPIDs := make(map[types.PID]struct{})

	for _, p := range processes {
		if p.State == types.StateDead && ui.IsFailedResult(p.Result) {
			errorPIDs[p.PID] = struct{}{}
		}
	}

	for _, e := range events {
		if e.Type == EventError || (e.Type == EventExit && e.Severity >= SevError) {
			errorPIDs[e.PID] = struct{}{}
		}
	}

	warnPIDs := make(map[types.PID]struct{})
	for _, p := range processes {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			// ctx usage >= 80%
			if p.ContextBudget > 0 && int64(p.TokensUsed)*100/int64(p.ContextBudget) >= 80 {
				warnPIDs[p.PID] = struct{}{}
			}
			// cost budget >= 80%
			if p.MaxCost > 0 && p.UsedCost*100/p.MaxCost >= 80 {
				warnPIDs[p.PID] = struct{}{}
			}
			// token budget >= 80%
			if p.MaxTokens > 0 && int64(p.TokensUsed)*100/p.MaxTokens >= 80 {
				warnPIDs[p.PID] = struct{}{}
			}
		}
	}

	if heartbeat != nil {
		for _, s := range heartbeat.CurrentStalled {
			warnPIDs[s.PID] = struct{}{}
		}
	}

	warnCount = len(warnPIDs)

	errorCount = len(errorPIDs)
	return
}

// computeCtxPercent returns context usage percentage for the selected process,
// or average across running processes if none is selected.
func computeCtxPercent(selectedPID types.PID, processes []vfs.ProcInfo) int {
	if selectedPID > 0 {
		for _, p := range processes {
			if p.PID == selectedPID {
				if p.ContextBudget > 0 {
					return clampPercent(int(int64(p.TokensUsed) * 100 / int64(p.ContextBudget)))
				}
				return 0
			}
		}
		return 0 // selected PID not found (reaped between ticks)
	}
	var totalUsed, totalBudget int64
	for _, p := range processes {
		if (p.State == types.StateRunning || p.State == types.StateCreated) && p.ContextBudget > 0 {
			totalUsed += int64(p.TokensUsed)
			totalBudget += int64(p.ContextBudget)
		}
	}
	if totalBudget > 0 {
		return clampPercent(int(totalUsed * 100 / totalBudget))
	}
	return 0
}

// computeBudgetPercent returns budget usage percentage for the selected process.
// Tries cost-based (UsedCost/MaxCost) first, falls back to token-based (TokensUsed/MaxTokens).
func computeBudgetPercent(selectedPID types.PID, processes []vfs.ProcInfo) int {
	if selectedPID <= 0 {
		return 0
	}
	for _, p := range processes {
		if p.PID == selectedPID {
			if p.MaxCost > 0 {
				return clampPercent(int(p.UsedCost * 100 / p.MaxCost))
			}
			if p.MaxTokens > 0 {
				return clampPercent(int(int64(p.TokensUsed) * 100 / p.MaxTokens))
			}
			return 0
		}
	}
	return 0
}

// clampPercent restricts a percentage value to the displayable range [0, 999].
func clampPercent(v int) int {
	if v < 0 {
		return 0
	}
	if v > 999 {
		return 999
	}
	return v
}

// styleProviderName colors the provider name based on process health.
// Green=healthy, Yellow=warning (ctx>80%), Red=failed/disconnected.
func styleProviderName(connected bool, proc *vfs.ProcInfo) string {
	if proc == nil || proc.Provider == "" {
		return ""
	}

	var color string
	switch {
	case !connected:
		color = ui.ColorError
	case proc.State == types.StateDead && ui.IsFailedResult(proc.Result):
		color = ui.ColorError
	case proc.ContextBudget > 0 && int64(proc.TokensUsed)*100/int64(proc.ContextBudget) >= 80:
		color = ui.ColorWarning
	default:
		color = ui.ColorSuccess
	}

	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(proc.Provider)
}

// formatElapsedHHMMSS formats a duration as HH:MM:SS.
func formatElapsedHHMMSS(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// pctColorStyle returns a lipgloss style coloured by usage percentage thresholds
// for the Title Bar ctx/budget segments (Story 38.2 AC#1):
//
//	< 60%  → ColorMuted (dim grey)
//	60-79% → ColorWarning (yellow)
//	≥ 80%  → ColorError (red, bold)
//
// The thresholds intentionally mirror styleProviderName's three-tier health logic
// so users see one consistent colour language for "approaching limit" vs "over limit".
func pctColorStyle(pct int) lipgloss.Style {
	switch {
	case pct >= 80:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Bold(true)
	case pct >= 60:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	}
}
