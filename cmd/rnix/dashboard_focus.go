package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// aggregateFocusCard builds a focusCardState from existing dashboardModel fields.
// No new IPC calls — all data is already cached in the model.
func (m *dashboardModel) aggregateFocusCard() {
	if m.selectedPID == 0 || len(m.processes) == 0 {
		m.focusCardData = nil
		return
	}

	// P-8: Cache process reference — single lookup instead of repeated traversals
	var proc *procInfoRef
	for i := range m.processes {
		if m.processes[i].PID == m.selectedPID && (m.selectedUUID == "" || m.processes[i].UUID == m.selectedUUID) {
			proc = &procInfoRef{
				State:     m.processes[i].State,
				Intent:    m.processes[i].Intent,
				Result:    m.processes[i].Result,
				CreatedAt: m.processes[i].CreatedAt,
				DeadAt:    m.processes[i].DeadAt,
			}
			break
		}
	}
	if proc == nil {
		m.focusCardData = nil
		return
	}

	// Derive agent name from procDetail (Model field) or intent
	agentName := proc.Intent
	if m.procDetail != nil && m.procDetail.Model != "" {
		agentName = m.procDetail.Model
	}

	isHistory := proc.State == types.StateDead || proc.State == types.StateZombie

	fc := &focusCardState{
		pid:       m.selectedPID,
		agent:     agentName,
		state:     proc.State,
		isHistory: isHistory,
	}

	// Tokens card — from heatmapProfile, fallback to procDetail for reaped processes
	if m.heatmapProfile != nil {
		fc.totalTokens = m.heatmapProfile.TotalTokens
		fc.tokenBudget = m.heatmapProfile.ContextBudget
	} else if isHistory && m.procDetail != nil {
		fc.totalTokens = m.procDetail.ContextStats.TokensUsed
		fc.tokenBudget = m.procDetail.ContextStats.ContextBudget
	}

	// Token rate — estimate from process tokens and time
	if fc.totalTokens > 0 && !proc.CreatedAt.IsZero() {
		elapsed := time.Since(proc.CreatedAt).Seconds()
		// P-2: Use DeadAt for dead processes
		if isHistory && !proc.DeadAt.IsZero() {
			elapsed = proc.DeadAt.Sub(proc.CreatedAt).Seconds()
		}
		if elapsed > 0 {
			rate := float64(fc.totalTokens) / elapsed
			fc.tokenRate = fmt.Sprintf("%.0f tok/s", rate)
		}
	}

	// Context card — from heatmapProfile TopConsumers
	if m.heatmapProfile != nil {
		for _, c := range m.heatmapProfile.TopConsumers {
			switch c.Kind {
			case "system_prompt":
				fc.systemPct = c.Pct
			case "user":
				fc.userPct = c.Pct
			case "assistant":
				fc.assistantPct = c.Pct
			case "tool:result":
				fc.toolPct = c.Pct
			}
		}
	}

	// Status card — from procDetail
	if m.procDetail != nil {
		fc.ppid = m.procDetail.PPID
		for _, sk := range m.procDetail.Skills {
			fc.skills = append(fc.skills, sk.Name)
		}
		fc.devices = m.procDetail.AllowedDevices
	}

	// P-3: Elapsed — use DeadAt for dead processes, time.Since for running
	if !proc.CreatedAt.IsZero() {
		if isHistory && !proc.DeadAt.IsZero() {
			fc.elapsed = ui.FormatDuration(proc.DeadAt.Sub(proc.CreatedAt))
		} else {
			fc.elapsed = ui.FormatDuration(time.Since(proc.CreatedAt))
		}
	}

	// P-6: Steps count for both running and dead processes
	fc.stepCount = len(m.stepEntries)

	// P-7: Intent card — from intentTrees, sorted by key for deterministic order
	if len(m.intentTrees) > 0 {
		for _, tree := range m.intentTrees {
			// Sort node keys for deterministic display
			keys := make([]string, 0, len(tree.Nodes))
			for k := range tree.Nodes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				node := tree.Nodes[k]
				fc.intentTasks = append(fc.intentTasks, intentMiniTask{
					name:  node.Intent,
					state: node.State,
				})
				// P-9: Check limit inside inner loop
				if len(fc.intentTasks) >= 5 {
					break
				}
			}
			if len(fc.intentTasks) >= 5 {
				break
			}
		}
	}

	// Trace card — from traceSummaries
	if len(m.traceSummaries) > 0 {
		var totalDur int64
		for _, ts := range m.traceSummaries {
			fc.spanCount += ts.SpanCount
			totalDur += ts.TotalDurationMs
		}
		fc.avgLatMs = float64(totalDur) / float64(len(m.traceSummaries))
	}

	// Alerts card — from immuneStatus
	if m.immuneStatus != nil && len(m.securityAlerts) > 0 {
		fc.alertCount = len(m.securityAlerts)
		for i, a := range m.securityAlerts {
			if i >= 3 {
				break
			}
			fc.topAlerts = append(fc.topAlerts, a.Detail)
		}
	}

	// Dead process specific data
	if isHistory {
		// P-5: Infer exit code — empty result → success, non-empty result → check content
		exitCode := 0
		if proc.Result != "" {
			lower := strings.ToLower(proc.Result)
			if strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.Contains(lower, "timeout") {
				exitCode = 1
			}
		}
		fc.exitCode = exitCode
		// P-11: Consistent "Done" text
		if exitCode == 0 {
			fc.resultText = "✓ Done"
		} else {
			fc.resultText = fmt.Sprintf("✕ Failed (exit %d)", exitCode)
		}

		// P-2: Historical snapshot time range — use DeadAt
		endTime := proc.DeadAt
		if endTime.IsZero() {
			endTime = time.Now()
		}
		start := proc.CreatedAt.Format("15:04:05")
		end := endTime.Format("15:04:05")
		dur := endTime.Sub(proc.CreatedAt)
		fc.livedRange = fmt.Sprintf("%s–%s (%s)", start, end, ui.FormatDuration(dur))
	}

	m.focusCardData = fc
}

// procInfoRef caches process fields to avoid repeated m.processes traversals.
type procInfoRef struct {
	State     types.ProcessState
	Intent    string
	Result    string
	CreatedAt time.Time
	DeadAt    time.Time
}

// renderFocusCard renders a 2×3 grid of info cards for the selected process.
func (m dashboardModel) renderFocusCard(width, height int) string {
	if m.focusCardData == nil {
		style := lipgloss.NewStyle().
			Width(width).Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(lipgloss.Color(ui.ColorMuted))
		return style.Render("Select a process to view Focus Card")
	}

	cardW := max(width/3, 8)

	// Title bar
	fc := m.focusCardData
	var title string
	if fc.isHistory {
		if fc.exitCode == 0 {
			title = fmt.Sprintf(" FOCUS: PID %d %s ━━━ ✓ Done (exit 0)", fc.pid, fc.agent)
		} else {
			title = fmt.Sprintf(" FOCUS: PID %d %s ━━━ ✕ Failed (exit %d)", fc.pid, fc.agent, fc.exitCode)
		}
	} else {
		title = fmt.Sprintf(" FOCUS: PID %d %s ━━━ %s %s", fc.pid, fc.agent, colorState(fc.state), fc.elapsed)
	}

	titleStyle := lipgloss.NewStyle().
		Width(width).
		Foreground(lipgloss.Color(ui.ColorAgent)).
		Bold(true)
	titleBar := titleStyle.Render(title)

	// Historical snapshot banner for dead processes
	var histBanner string
	if fc.isHistory && fc.livedRange != "" {
		histBanner = lipgloss.NewStyle().
			Width(width).
			Foreground(lipgloss.Color(ui.ColorMuted)).
			Italic(true).
			Render(fmt.Sprintf(" Historical snapshot — lived %s", fc.livedRange))
	}

	// Adjust card heights for title/banner
	usedH := 1
	if histBanner != "" {
		usedH = 2
	}
	gridH := max(height-usedH, 2)
	cardH := gridH / 2

	row0 := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderTokensCard(cardW, cardH),
		m.renderContextCard(cardW, cardH),
		m.renderStatusCard(cardW, cardH),
	)
	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderIntentCard(cardW, cardH),
		m.renderTraceCard(cardW, cardH),
		m.renderAlertsOrResultCard(cardW, cardH),
	)

	parts := []string{titleBar}
	if histBanner != "" {
		parts = append(parts, histBanner)
	}
	parts = append(parts, row0, row1)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// --- Card rendering helpers ---

func cardStyle(w, h int) lipgloss.Style {
	innerW := max(w-2, 1)
	innerH := max(h-3, 1)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorMuted)).
		Width(innerW).Height(innerH)
}

func (m dashboardModel) renderTokensCard(w, h int) string {
	style := cardStyle(w, h)
	var b strings.Builder
	b.WriteString(" Tokens\n")

	fc := m.focusCardData
	if fc == nil {
		return style.Render(b.String())
	}

	fmt.Fprintf(&b, " total: %s\n", ui.FormatTokens(fc.totalTokens))
	if fc.tokenBudget > 0 {
		pct := float64(fc.totalTokens) * 100.0 / float64(fc.tokenBudget)
		fmt.Fprintf(&b, " budget: %s (%.0f%%)\n", ui.FormatTokens(fc.tokenBudget), pct)
	}
	if fc.tokenRate != "" {
		fmt.Fprintf(&b, " rate: %s\n", fc.tokenRate)
	}
	// P-4/P-6: Show steps separately from token rate
	if fc.stepCount > 0 {
		if fc.isHistory {
			fmt.Fprintf(&b, " steps: %d (final)\n", fc.stepCount)
		} else {
			fmt.Fprintf(&b, " steps: %d\n", fc.stepCount)
		}
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderContextCard(w, h int) string {
	style := cardStyle(w, h)
	var b strings.Builder
	b.WriteString(" Context\n")

	fc := m.focusCardData
	if fc == nil {
		return style.Render(b.String())
	}

	fmt.Fprintf(&b, " system:    %5.1f%%\n", fc.systemPct)
	fmt.Fprintf(&b, " user:      %5.1f%%\n", fc.userPct)
	fmt.Fprintf(&b, " assistant: %5.1f%%\n", fc.assistantPct)
	fmt.Fprintf(&b, " tool:      %5.1f%%\n", fc.toolPct)

	return style.Render(b.String())
}

func (m dashboardModel) renderStatusCard(w, h int) string {
	style := cardStyle(w, h)
	var b strings.Builder
	b.WriteString(" Status\n")

	fc := m.focusCardData
	if fc == nil {
		return style.Render(b.String())
	}

	fmt.Fprintf(&b, " state: %s\n", colorState(fc.state))
	if fc.ppid > 0 {
		fmt.Fprintf(&b, " ppid: %d\n", fc.ppid)
	}
	if len(fc.skills) > 0 {
		skills := fc.skills
		if len(skills) > 3 {
			skills = skills[:3]
		}
		fmt.Fprintf(&b, " skills: %s\n", strings.Join(skills, ","))
	}
	if len(fc.devices) > 0 {
		devs := fc.devices
		if len(devs) > 3 {
			devs = devs[:3]
		}
		fmt.Fprintf(&b, " devs: %s\n", strings.Join(devs, ","))
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderIntentCard(w, h int) string {
	style := cardStyle(w, h)
	var b strings.Builder
	b.WriteString(" Intent\n")

	fc := m.focusCardData
	if fc == nil || len(fc.intentTasks) == 0 {
		b.WriteString(" (no intents)")
		return style.Render(b.String())
	}

	for i, task := range fc.intentTasks {
		if i >= max(h-4, 2) {
			fmt.Fprintf(&b, " +%d more\n", len(fc.intentTasks)-i)
			break
		}
		icon := "○"
		switch task.state {
		case "completed":
			icon = "✓"
		case "executing", "running":
			icon = "▶"
		case "failed":
			icon = "✕"
		}
		name := task.name
		// P-1: Safe rune-aware truncation with non-negative guard
		name = truncateRuneSafe(name, w-6)
		fmt.Fprintf(&b, " %s %s\n", icon, name)
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderTraceCard(w, h int) string {
	style := cardStyle(w, h)
	var b strings.Builder
	b.WriteString(" Trace\n")

	fc := m.focusCardData
	if fc == nil || fc.spanCount == 0 {
		b.WriteString(" (no traces)")
		return style.Render(b.String())
	}

	fmt.Fprintf(&b, " spans: %d\n", fc.spanCount)
	fmt.Fprintf(&b, " avg: %.0fms\n", fc.avgLatMs)
	if fc.errCount > 0 {
		fmt.Fprintf(&b, " errors: %d\n", fc.errCount)
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderAlertsOrResultCard(w, h int) string {
	fc := m.focusCardData

	// Dead processes show Result card instead of Alerts
	if fc != nil && fc.isHistory {
		return m.renderResultCard(w, h)
	}
	return m.renderAlertsCard(w, h)
}

func (m dashboardModel) renderAlertsCard(w, h int) string {
	style := cardStyle(w, h)
	var b strings.Builder
	b.WriteString(" Alerts\n")

	fc := m.focusCardData
	if fc == nil || fc.alertCount == 0 {
		b.WriteString(" (no alerts)")
		return style.Render(b.String())
	}

	fmt.Fprintf(&b, " count: %d\n", fc.alertCount)
	for _, alert := range fc.topAlerts {
		// P-1: Safe rune-aware truncation
		alert = truncateRuneSafe(alert, w-4)
		fmt.Fprintf(&b, " • %s\n", alert)
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderResultCard(w, h int) string {
	style := cardStyle(w, h)
	var b strings.Builder
	b.WriteString(" Result\n")

	fc := m.focusCardData
	if fc == nil {
		return style.Render(b.String())
	}

	b.WriteString(" " + fc.resultText + "\n")
	if fc.isHistory && fc.stepCount > 0 {
		fmt.Fprintf(&b, " steps: %d (final)\n", fc.stepCount)
	}

	return style.Render(b.String())
}

// truncateRuneSafe truncates a string to maxLen runes, appending "…" if truncated.
// Returns the original string if maxLen <= 0 or string is within limit.
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
