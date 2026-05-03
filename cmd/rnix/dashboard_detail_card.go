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
	"github.com/rnixai/rnix/vfs"
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

	// Line 1: Provider + Started + Device list
	startedSeg := ""
	if d.CreatedAtMs > 0 {
		startedSeg = fmt.Sprintf(" │ Started: %s", ui.FormatWallClock(time.UnixMilli(d.CreatedAtMs)))
	}
	deviceList := "—"
	if len(d.AllowedDevices) > 0 {
		deviceList = strings.Join(d.AllowedDevices, ", ")
	}
	line1 := fmt.Sprintf("  Provider: %s%s │ Devices: %s", d.Provider, startedSeg, deviceList)
	line1 = fitLine(line1, width)

	// Line 2: Skills + Orchestration info
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
	orchInfo := detailOrchestrationInfo(d, m.processes)
	if orchInfo != "" {
		line2 += " │ " + orchInfo
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

	// Line 1: Intent + Compact stats with avg pct
	intent := "—"
	if proc != nil && proc.Intent != "" {
		intent = proc.Intent
	}
	compactCount, avgPct := compactStats(m.sysEvents, m.selectedPID)
	if compactCount > 0 {
		line1 := fmt.Sprintf("  Intent: %s │ Compact: %d× (avg %d%%)", intent, compactCount, avgPct)
		line1 = fitLine(line1, width)

		// Line 2: Trace + Budget + Steps (spec format)
		line2 := formatTraceBudgetSteps(m, d)
		line2 = fitLine(line2, width)

		content := lipgloss.NewStyle().Width(width).Height(height).Render(
			lipgloss.JoinVertical(lipgloss.Left, line1, line2),
		)
		return lipgloss.JoinVertical(lipgloss.Left, sep, content)
	}

	line1 := fmt.Sprintf("  Intent: %s │ Compact: 0×", intent)
	line1 = fitLine(line1, width)

	// Line 2: Trace + Budget + Steps (spec format)
	line2 := formatTraceBudgetSteps(m, d)
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
	// Time range: "HH:MM:SS→HH:MM:SS dur" or just "HH:MM:SS dur"
	timeRange := formatDetailTimeRange(d)
	var line1 string
	if !ui.IsFailedResult(proc.Result) {
		line1 = fmt.Sprintf("  %s Done (exit 0) │ %s │ %s tokens",
			checkmark, timeRange, ui.FormatTokens(d.ContextStats.TokensUsed))
	} else {
		summary := truncateRuneSafe(proc.Result, 40)
		line1 = fmt.Sprintf("  %s Failed (exit 1) │ %s │ %s", failmark, timeRange, summary)
	}
	line1 = fitLine(line1, width)

	deadDeviceList := "—"
	if len(d.AllowedDevices) > 0 {
		deadDeviceList = strings.Join(d.AllowedDevices, ", ")
	}
	line2 := fmt.Sprintf("  Provider: %s │ Devices: %s", d.Provider, deadDeviceList)
	line2 = fitLine(line2, width)

	content := lipgloss.NewStyle().Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, line1, line2),
	)
	return lipgloss.JoinVertical(lipgloss.Left, sep, content)
}

// renderDeadDetailCardRight renders right detail card for dead/zombie processes.
func renderDeadDetailCardRight(m *dashboardModel, proc *selectedProcRef, width, height int, sep string) string {
	d := m.procDetail
	stepCount := len(m.timeline.StepEntries)
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
	State    types.ProcessState
	Intent   string
	Result   string
	IsPaused bool
}

// findSelectedProcess returns a reference to the selected process, or nil.
func findSelectedProcess(m *dashboardModel) *selectedProcRef {
	if m.selectedPID == 0 {
		return nil
	}
	for i := range m.processes {
		if m.processes[i].PID == m.selectedPID && (m.selectedUUID == "" || m.processes[i].UUID == m.selectedUUID) {
			return &selectedProcRef{
				State:    m.processes[i].State,
				Intent:   m.processes[i].Intent,
				Result:   m.processes[i].Result,
				IsPaused: m.processes[i].IsPaused,
			}
		}
	}
	return nil
}

// compactStats returns the count of compact events and average compaction percentage for a PID.
func compactStats(events []UnifiedEvent, pid types.PID) (count int, avgPct int) {
	var totalPct float64
	for _, ev := range events {
		if ev.Type == EventCompact && ev.PID == pid {
			count++
			var pre, post int
			if _, err := fmt.Sscanf(ev.Detail, "pre=%d post=%d", &pre, &post); err == nil && pre > 0 && post < pre {
				totalPct += float64(pre-post) / float64(pre) * 100
			}
		}
	}
	if count > 0 {
		avgPct = int(totalPct / float64(count))
	}
	return
}

// formatTraceBudgetSteps formats the detail card right line 2 per AC2 spec:
// "Trace: span-{id} {dur}s │ Budget: {pct}% │ Steps: {N}"
func formatTraceBudgetSteps(m *dashboardModel, d *ipc.GetProcDetailResponse) string {
	stepCount := len(m.timeline.StepEntries)
	budgetPct := 0
	if d.ContextStats.ContextBudget > 0 {
		budgetPct = int(int64(d.ContextStats.TokensUsed) * 100 / int64(d.ContextStats.ContextBudget))
	}

	traceInfo := "—"
	if len(m.traceSummaries) > 0 {
		ts := m.traceSummaries[0]
		durS := float64(ts.TotalDurationMs) / 1000.0
		spanID := ts.TraceID
		if len(spanID) > 8 {
			spanID = spanID[:8]
		}
		traceInfo = fmt.Sprintf("span-%s %.1fs", spanID, durS)
	}

	return fmt.Sprintf("  Trace: %s │ Budget: %d%% │ Steps: %d",
		traceInfo, budgetPct, stepCount)
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

// formatDetailTimeRange returns "HH:MM:SS→HH:MM:SS dur" for dead processes.
func formatDetailTimeRange(d *ipc.GetProcDetailResponse) string {
	if d.CreatedAtMs <= 0 {
		return formatLivedDuration(d)
	}
	start := ui.FormatWallClock(time.UnixMilli(d.CreatedAtMs))
	if d.DeadAtMs > 0 {
		end := ui.FormatWallClock(time.UnixMilli(d.DeadAtMs))
		durMs := d.DeadAtMs - d.CreatedAtMs
		if durMs > 0 {
			dur := ui.FormatDuration(time.Duration(durMs) * time.Millisecond)
			return start + "→" + end + " " + dur
		}
		return start + "→" + end
	}
	return start
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
		return ""
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

// pauseTreeCmd sends SIGPAUSE or SIGRESUME to a process and its entire subtree via IPC.
func pauseTreeCmd(pid types.PID, signal types.Signal) tea.Cmd {
	paused := signal == types.SIGPAUSE
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return pauseToggleMsg{pid: pid, paused: paused, err: err}
		}
		defer client.Close()
		result, err := client.SignalTree(pid, signal)
		affected := 0
		if result != nil {
			affected = result.Affected
		}
		return pauseToggleMsg{pid: pid, affected: affected, paused: paused, err: err}
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

// detailOrchestrationInfo returns a compact orchestration description for the detail card.
// Includes PID mapping for deps and DAG stage calculation (AC4).
func detailOrchestrationInfo(d *ipc.GetProcDetailResponse, procs []vfs.ProcInfo) string {
	if d.ComposeNode != "" {
		// Build node→PID mapping from sibling compose processes (same PPID)
		nodeToPID := make(map[string]types.PID)
		nodeGraph := make(map[string][]string)
		for _, p := range procs {
			if p.ComposeNode != "" && p.PPID == d.PPID {
				nodeToPID[p.ComposeNode] = p.PID
				nodeGraph[p.ComposeNode] = p.ComposeDeps
			}
		}

		s := "Compose:" + d.ComposeNode
		if len(d.ComposeDeps) > 0 {
			var depLabels []string
			for _, dep := range d.ComposeDeps {
				if pid, ok := nodeToPID[dep]; ok {
					depLabels = append(depLabels, fmt.Sprintf("PID %d", pid))
				} else {
					depLabels = append(depLabels, dep)
				}
			}
			s += " depends_on:[" + strings.Join(depLabels, ",") + "]"
		}
		if stage, total := composeDAGStage(d.ComposeNode, nodeGraph); total > 0 {
			s += fmt.Sprintf(" stage %d/%d", stage, total)
		}
		return s
	}
	if d.PipelineTotal > 0 {
		return fmt.Sprintf("Pipeline[%d/%d]", d.PipelineIndex+1, d.PipelineTotal)
	}
	return ""
}

// composeDAGStage computes the topological layer (1-based) for a node using Kahn's algorithm.
func composeDAGStage(node string, graph map[string][]string) (stage, total int) {
	if len(graph) == 0 {
		return 0, 0
	}

	inDegree := make(map[string]int, len(graph))
	dependedBy := make(map[string][]string)
	for n, deps := range graph {
		count := 0
		for _, dep := range deps {
			if _, exists := graph[dep]; exists {
				count++
				dependedBy[dep] = append(dependedBy[dep], n)
			}
		}
		inDegree[n] = count
	}

	var currentLayer []string
	for n, deg := range inDegree {
		if deg == 0 {
			currentLayer = append(currentLayer, n)
		}
	}

	nodeLayer := make(map[string]int)
	layerNum := 0
	processed := 0

	for len(currentLayer) > 0 {
		layerNum++
		for _, n := range currentLayer {
			nodeLayer[n] = layerNum
		}
		processed += len(currentLayer)

		var nextLayer []string
		for _, n := range currentLayer {
			for _, downstream := range dependedBy[n] {
				inDegree[downstream]--
				if inDegree[downstream] == 0 {
					nextLayer = append(nextLayer, downstream)
				}
			}
		}
		currentLayer = nextLayer
	}

	if processed != len(graph) {
		return 0, 0
	}

	return nodeLayer[node], layerNum
}
