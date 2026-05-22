package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
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

	if m.selectedPID == 0 || m.detail.Detail == nil {
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

	d := m.detail.Detail

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

	if m.selectedPID == 0 || m.detail.Detail == nil {
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

	d := m.detail.Detail

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

	d := m.detail.Detail
	// Time range: "HH:MM:SS→HH:MM:SS dur" or just "HH:MM:SS dur"
	timeRange := formatDetailTimeRange(d)
	var line1 string
	if !ui.IsFailedResult(proc.Result) {
		line1 = fmt.Sprintf("  %s Done (exit 0) │ %s │ %s tokens",
			checkmark, timeRange, timeline.FormatTokenCount(d.ContextStats.TokensUsed))
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
	d := m.detail.Detail
	stepCount := len(m.timeline.StepEntries)
	line1 := fmt.Sprintf("  Steps: %d │ Tokens: %s", stepCount, timeline.FormatTokenCount(d.ContextStats.TokensUsed))
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

// countSuspendedDescendants returns how many processes in procs are Suspended
// and directly parented (by ParentUUID or PPID) to the origin identified by
// originPID/originUUID (Story 44.4 AC#5). Used by handleForkResult to warn the
// user that fork left the origin's Suspended subtree intact (Decision D2: fork
// does not copy the subtree). A scan one level deep matches the "descendants
// left intact" phrasing — the user resumes them via dashboard r on each subtree
// root, so we only need to know whether any survive.
func countSuspendedDescendants(procs []vfs.ProcInfo, originPID types.PID, originUUID string) int {
	count := 0
	for i := range procs {
		p := procs[i]
		if p.State != types.StateSuspended {
			continue
		}
		linkedByUUID := originUUID != "" && p.ParentUUID == originUUID
		linkedByPID := originPID != 0 && p.PPID == originPID
		if linkedByUUID || linkedByPID {
			count++
		}
	}
	return count
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
	if len(m.trace.Summaries) > 0 {
		ts := m.trace.Summaries[0]
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
//
// Epic 42 fix: passes ProjectDir + RNIX_ENV so the daemon rebuilds ProjectConfig
// (project .env / providers.yaml / LLMFileOpener), letting the resumed process
// inherit project-level API keys. Without this, resume falls back to the global
// VFS driver and short-lived sessions get 401 from provider APIs that only have
// keys in `.env` (e.g. DeepSeek).
func resumeProcessCmd(uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return resumeResultMsg{err: err}
		}
		defer client.Close()
		cwd, _ := os.Getwd()
		projectDir, _ := config.ProjectDir(cwd)
		rnixEnv := os.Getenv("RNIX_ENV")
		result, err := client.ResumeWithOptsV3(uuid, false, 0, projectDir, rnixEnv)
		return resumeResultMsg{result: result, err: err}
	}
}

// forkResultMsg — Story 42.3 stub.
// Returns the Fork variant of a Resume IPC call to dashboardModel.Update.
// Carries the new fork UUID + PID + origin UUID short hash for status display.
type forkResultMsg struct {
	result *ipc.ResumeResponse
	err    error
}

// forkProcessCmd — Story 42.3.
// Sends a Resume IPC call with fork=true for the given origin UUID. Routes
// through ResumeWithOptsV3 (Epic 42 fix) so ProjectDir/RnixEnv reach the
// daemon — without it, the forked process loses project-level API keys (same
// 401 root cause as resumeProcessCmd).
func forkProcessCmd(uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return forkResultMsg{err: err}
		}
		defer client.Close()
		cwd, _ := os.Getwd()
		projectDir, _ := config.ProjectDir(cwd)
		rnixEnv := os.Getenv("RNIX_ENV")
		result, err := client.ResumeWithOptsV3(uuid, true, 0, projectDir, rnixEnv)
		return forkResultMsg{result: result, err: err}
	}
}

// handleProcDetailResult applies a procDetailResultMsg to dashboardModel and
// returns the resulting model plus an optional lineage-fetch follow-up cmd
// (Story 42.3). Extracted from dashboard.go to keep that file under the
// Story 29.1 line-count cap.
//
// Lineage fetch policy: always trigger when the user has the process selected
// and it isn't already in LineageCache, regardless of whether OriginUUID is
// empty. A "root parent" process (OriginUUID empty but with fork descendants)
// would otherwise never display its "Forked: N descendants" line because no
// codepath populates the cache for it (Blind/Edge finding #1).
func handleProcDetailResult(m dashboardModel, msg procDetailResultMsg) (dashboardModel, tea.Cmd) {
	if msg.Err != nil || msg.Detail == nil {
		return m, nil
	}
	m.detail.Cache[msg.UUID] = msg.Detail
	if msg.PID == m.selectedPID && msg.UUID == m.selectedUUID {
		m.detail.Detail = msg.Detail
		m.detail.PID = msg.PID
	}
	if msg.UUID != m.selectedUUID {
		return m, nil
	}
	if m.detail.LineageCache == nil {
		m.detail.LineageCache = make(map[string]*ipc.GetResumeLineageResponse)
	}
	if _, ok := m.detail.LineageCache[msg.UUID]; ok {
		return m, nil
	}
	return m, detail.FetchResumeLineageCmd(ipc.SocketPath(), msg.UUID)
}

// handleResumeLineageResult writes a ResumeLineageResultMsg into the detail
// state's LineageCache (Story 42.3). On error, caches a sentinel empty response
// to prevent retry storms (handleProcDetailResult would otherwise re-fetch on
// every detail refresh) and surfaces the error in the status line.
func handleResumeLineageResult(m dashboardModel, msg detail.ResumeLineageResultMsg) dashboardModel {
	if m.detail.LineageCache == nil {
		m.detail.LineageCache = make(map[string]*ipc.GetResumeLineageResponse)
	}
	if msg.Err != nil {
		// Sentinel: empty response so the cache-hit check in
		// handleProcDetailResult short-circuits subsequent fetches for this
		// UUID. Without this, every tick that refreshes Detail would re-dial
		// the kernel and re-fail.
		m.detail.LineageCache[msg.UUID] = &ipc.GetResumeLineageResponse{}
		m.statusMsg = fmt.Sprintf("✗ Lineage fetch: %v", msg.Err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m
	}
	if msg.Lineage == nil {
		m.detail.LineageCache[msg.UUID] = &ipc.GetResumeLineageResponse{}
		return m
	}
	m.detail.LineageCache[msg.UUID] = msg.Lineage
	return m
}

// handleForkResult formats fork success/error status and chains a Detail fetch
// for the new PID (Story 42.3). On success, also invalidates the LineageCache
// entry for the origin UUID so the "Forked: N descendants" line reflects the
// new child immediately rather than serving stale cached descendants.
func handleForkResult(m dashboardModel, msg forkResultMsg) (dashboardModel, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("✗ Fork: %v", msg.err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	if msg.result == nil {
		// Defensive: kernel returned (nil, nil). Surface a fallback message so
		// the "Forking UUID ..." status from forkProcessHandler doesn't linger
		// silently until TTL expires.
		m.statusMsg = "✗ Fork: empty response"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	// Story 44.4 AC#5 / Decision D2: fork does NOT copy the subtree (kernel
	// resume.go:850 `if !opts.Fork` guard unchanged). Count the origin's
	// Suspended descendants BEFORE reassigning m.selectedPID/UUID below so we
	// can tell the user they were left intact and must be resumed separately.
	suspendedDescendants := countSuspendedDescendants(m.processes, m.selectedPID, m.selectedUUID)

	// Invalidate the origin's lineage cache so a subsequent Detail render sees
	// the freshly-added descendant. The new selected UUID gets its own fetch
	// chain via fetchProcDetailCmd → handleProcDetailResult below.
	if m.detail.LineageCache != nil {
		delete(m.detail.LineageCache, m.selectedUUID)
	}
	m.statusMsg = fmt.Sprintf("Forked UUID %s → PID %d (from %s)",
		shortUUIDForStatus(msg.result.UUID),
		msg.result.PID,
		shortUUIDForStatus(m.selectedUUID))
	if suspendedDescendants > 0 {
		m.statusMsg += fmt.Sprintf(" (%d suspended descendants left intact — resume them separately)",
			suspendedDescendants)
	}
	m.statusMsgTTL = statusMsgDefaultTTL
	m.selectedPID = msg.result.PID
	m.selectedUUID = msg.result.UUID
	return m, fetchProcDetailCmd(msg.result.PID, msg.result.UUID)
}

// forkResultMsgEnd — Story 42.3 separator (placed below the legacy
// resumeProcessCmd / pauseTreeCmd helpers below).

// pauseTreeSubtreeCmd sends a dedicated PauseSubtree IPC call (Story 44.4 AC#2),
// suspending the selected process and all its descendants. dashboard `p` routes
// here instead of pauseTreeCmd (raw SIGPAUSE via SignalTree).
func pauseTreeSubtreeCmd(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return pauseSubtreeResultMsg{pid: pid, err: err}
		}
		defer client.Close()
		result, err := client.PauseSubtree(pid)
		affected := 0
		if result != nil {
			affected = result.Affected
		}
		return pauseSubtreeResultMsg{pid: pid, affected: affected, err: err}
	}
}

// resumeSubtreeCmd sends a dedicated ResumeSubtree IPC call (Story 44.4 AC#3),
// resuming every Suspended node in the selected process's subtree and skipping
// Dead/Failed nodes. dashboard `r` on a Suspended process routes here — the
// direct fix for Decker's "父进程橙色恢复后整树停滞" bug.
func resumeSubtreeCmd(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return resumeSubtreeResultMsg{pid: pid, err: err}
		}
		defer client.Close()
		result, err := client.ResumeSubtree(pid)
		affected, skipped := 0, 0
		if result != nil {
			affected = result.Affected
			skipped = result.Skipped
		}
		return resumeSubtreeResultMsg{pid: pid, affected: affected, skipped: skipped, err: err}
	}
}

// handleResumeResult applies a resumeResultMsg (single-process resume via
// resumeProcessCmd → ResumeWithOptsV3; Epic 42 UUID 续跑 for Dead/Zombie). On
// success it retargets the selection to the resumed PID and chains a Detail
// fetch. Extracted from dashboard.go to keep that file under the Story 29.1 cap.
func handleResumeResult(m dashboardModel, msg resumeResultMsg) (dashboardModel, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("✗ Resume: %v", msg.err)
	} else if msg.result != nil {
		m.statusMsg = fmt.Sprintf("Resumed PID %d from step %d", msg.result.PID, msg.result.ResumedFromStep)
		// Update selectedPID to the new PID (may change after resume)
		m.selectedPID = msg.result.PID
		m.selectedUUID = msg.result.UUID
	}
	m.statusMsgTTL = statusMsgDefaultTTL
	if msg.err == nil && msg.result != nil {
		return m, fetchProcDetailCmd(msg.result.PID, msg.result.UUID)
	}
	return m, nil
}

// handlePauseSubtreeResult applies a pauseSubtreeResultMsg (Story 44.4 AC#2,
// dashboard `p` → client.PauseSubtree) to the model's status line.
func handlePauseSubtreeResult(m dashboardModel, msg pauseSubtreeResultMsg) dashboardModel {
	switch {
	case msg.err != nil:
		m.statusMsg = fmt.Sprintf("✗ Pause: %v", msg.err)
	case msg.affected > 1:
		m.statusMsg = fmt.Sprintf("Paused PID %d (+%d descendants)", msg.pid, msg.affected-1)
	default:
		m.statusMsg = fmt.Sprintf("Paused PID %d", msg.pid)
	}
	m.statusMsgTTL = statusMsgDefaultTTL
	return m
}

// handleResumeSubtreeResult applies a resumeSubtreeResultMsg (Story 44.4 AC#3,
// dashboard `r` on Suspended → client.ResumeSubtree) to the model's status line.
func handleResumeSubtreeResult(m dashboardModel, msg resumeSubtreeResultMsg) dashboardModel {
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("✗ Resume: %v", msg.err)
	} else {
		m.statusMsg = fmt.Sprintf("Resumed %d (skipped %d)", msg.affected, msg.skipped)
	}
	m.statusMsgTTL = statusMsgDefaultTTL
	return m
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
