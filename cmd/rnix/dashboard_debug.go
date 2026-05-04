package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/debug"
	dashboarddebug "github.com/rnixai/rnix/internal/dashboard/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// --- Constants ---

const maxDebugStraceEvents = 500

// --- Types ---

// deviceLatencyStats 类型迁出自 internal/dashboard/debug.DeviceLatencyStats（Story 38-5 PR11 Step 1）。
// cmd/rnix 端通过 type alias 保留旧名零行为变化（与 PR2/PR3/PR4/PR6/PR8/PR10 同模式）。
//
//nolint:unused // 通过 alias 暴露给现有 caller。
type deviceLatencyStats = dashboarddebug.DeviceLatencyStats

// --- tea.Msg types for Debug mode ---

type debugStraceStartedMsg struct {
	client *ipc.Client
	ch     <-chan ipc.SyscallEventWire
	errCh  <-chan error // F6: reports AttachDebug error when goroutine finishes
}

type debugStraceErrorMsg struct {
	err error
}

type debugCtxProfileMsg struct {
	profile *debug.CtxProfileResult
	err     error
}

type debugHistoricalStraceMsg struct {
	events []ipc.SyscallEventWire
	err    error
	pid    types.PID // PID the request was made for (staleness guard)
	uuid   string    // UUID the request was made for (staleness guard)
}

// debugStraceStreamEndMsg is sent when the AttachDebug goroutine finishes.
type debugStraceStreamEndMsg struct {
	err error
}

// handleDebugMsg dispatches debug-related tea.Msg types.
// Returns (model, cmd, handled). If handled is false, the msg was not a debug msg.
func (m dashboardModel) handleDebugMsg(msg tea.Msg) (dashboardModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case debugStraceStartedMsg:
		// F2: If debug mode was exited before this msg arrived, clean up the leaked client.
		if !m.debugState.Mode {
			msg.client.Close()
			return m, nil, true
		}
		m.debugState.Client = msg.client
		m.debugState.StraceCh = msg.ch
		// F6: Watch for AttachDebug completion to report errors.
		var cmd tea.Cmd
		if msg.errCh != nil {
			errCh := msg.errCh
			cmd = func() tea.Msg {
				err := <-errCh
				return debugStraceStreamEndMsg{err: err}
			}
		}
		return m, cmd, true
	case debugStraceStreamEndMsg:
		if msg.err != nil && m.debugState.Mode {
			m.statusMsg = fmt.Sprintf("✗ strace stream: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
		}
		return m, nil, true
	case debugStraceErrorMsg:
		m.statusMsg = fmt.Sprintf("✗ strace: %v", msg.err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil, true
	case debugCtxProfileMsg:
		// F6: Show error feedback instead of silently swallowing.
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ ctx profile: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
		} else if msg.profile != nil {
			m.debugState.CtxProfile = msg.profile
		}
		return m, nil, true
	case debugHistoricalStraceMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ strace history: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil, true
		}
		// F1: Guard against async msg arriving after exitDebugMode nils the map.
		if !m.debugState.Mode {
			return m, nil, true
		}
		// F2: Guard against stale async responses from a previous PID selection.
		// loadHistoricalStraceCmd captures PID/UUID at dispatch time. If the user
		// (or periodic tick) switched to a different process before the response
		// arrived, discard it to prevent overwriting the current process's events.
		if msg.pid != m.selectedPID || (msg.uuid != "" && msg.uuid != m.selectedUUID) {
			return m, nil, true
		}
		// Historical events from disk are the authoritative complete set.
		// Replace strace events with disk data, preserving any live stream events
		// that arrived after the disk watermark.
		var newWatermark int64
		for _, sew := range msg.events {
			if sew.TimestampMs > newWatermark {
				newWatermark = sew.TimestampMs
			}
		}
		// Preserve live stream events newer than what's on disk.
		var preserved []UnifiedEvent
		for _, ev := range m.debugStraceEvents {
			if ev.RawEvent != nil && ev.RawEvent.TimestampMs > newWatermark {
				preserved = append(preserved, ev)
			}
		}
		m.debugStraceEvents = nil
		m.debugState.DeviceLatency = make(map[string]*deviceLatencyStats)
		for _, sew := range msg.events {
			ev := straceToUnifiedEvent(sew)
			m.appendStraceEvent(ev)
			m.updateDeviceLatency(sew)
		}
		// Re-add preserved live stream events.
		m.debugStraceEvents = append(m.debugStraceEvents, preserved...)
		m.debugState.HistWatermark = newWatermark
		// Events panel shows only syscall events (steps are in the Steps panel).
		if len(m.debugStraceEvents) > 0 || len(m.debugEvents) == 0 {
			m.debugEvents = m.debugStraceEvents
		}
		return m, nil, true
	}
	return m, nil, false
}

// debugTickCmds returns tea.Cmds needed for debug mode during the tick cycle.
func (m *dashboardModel) debugTickCmds() []tea.Cmd {
	var cmds []tea.Cmd
	streamClosed := m.debugTickProcess()
	// When the live stream ends, auto-reload complete events from disk.
	// This ensures all events are shown even if some were missed during streaming
	// (e.g., events emitted before dashboard attached, or dropped due to buffer full).
	if streamClosed && !m.debugState.AutoReloaded && m.selectedPID != 0 {
		m.debugState.AutoReloaded = true
		cmds = append(cmds, m.loadHistoricalStraceCmd())
	}
	if m.debugState.Mode && m.debugState.AttachedPID != m.selectedPID {
		m2, debugCmd := m.handleDebugPIDChange()
		*m = m2
		if debugCmd != nil {
			cmds = append(cmds, debugCmd)
		}
	}
	return cmds
}

// --- Mode lifecycle ---

func (m dashboardModel) enterDebugMode() (dashboardModel, tea.Cmd) {
	// F2: Clean up any existing strace stream before starting a new one.
	m.stopStraceStream()
	m.viewMode = viewDebug
	m.debugState.Mode = true
	m.debugState.ShowStrace = true
	m.debugStraceEvents = nil
	m.debugEvents = nil
	m.debugState.DeviceLatency = make(map[string]*deviceLatencyStats)
	m.debugState.ScrollTop = 0
	m.debugState.Cursor = 0
	m.debugState.AutoScroll = true
	m.debugState.CtxProfile = nil
	m.debugState.AttachedPID = m.selectedPID
	m.debugState.AutoReloaded = false
	m.debugState.HistWatermark = 0
	m.debugState.AutoScroll = true

	if m.isSelectedProcessDead() {
		return m, m.loadHistoricalStraceCmd()
	}
	// For running processes, load historical events from disk immediately
	// (for events emitted before dashboard attached) AND start live stream
	// (for real-time new events). The DebugChan is single-consumer, so events
	// emitted before attachment are only available from disk persistence.
	return m, tea.Batch(m.loadHistoricalStraceCmd(), m.startStraceStreamCmd())
}

func (m dashboardModel) exitDebugMode() dashboardModel {
	m.viewMode = viewDefault
	m.debugState.Mode = false
	m.stopStraceStream()
	m.debugEvents = nil
	m.debugStraceEvents = nil
	m.debugState.CtxProfile = nil
	m.debugState.DeviceLatency = nil
	m.debugState.AttachedPID = 0
	m.debugState.ScrollTop = 0
	m.debugState.Cursor = 0
	return m
}

// stopStraceStream closes the independent debug IPC client (which causes the
// scanner to stop) and resets channel/client references.
func (m *dashboardModel) stopStraceStream() {
	if m.debugState.Client != nil {
		m.debugState.Client.Close()
		m.debugState.Client = nil
	}
	m.debugState.StraceCh = nil
}

// --- strace stream commands ---

func (m dashboardModel) startStraceStreamCmd() tea.Cmd {
	pid := m.selectedPID
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return debugStraceErrorMsg{err: err}
		}
		ch := make(chan ipc.SyscallEventWire, 100)
		errCh := make(chan error, 1)
		go func() {
			defer close(ch)
			err := client.AttachDebug(pid, func(sew ipc.SyscallEventWire) {
				select {
				case ch <- sew:
				default: // channel full, drop
				}
			})
			errCh <- err
		}()
		return debugStraceStartedMsg{client: client, ch: ch, errCh: errCh}
	}
}

func (m dashboardModel) loadHistoricalStraceCmd() tea.Cmd {
	pid := m.selectedPID
	uuid := m.selectedUUID
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return debugHistoricalStraceMsg{err: err, pid: pid, uuid: uuid}
		}
		defer client.Close()
		events, err := client.ListEvents(pid, uuid)
		return debugHistoricalStraceMsg{events: events, err: err, pid: pid, uuid: uuid}
	}
}

func (m dashboardModel) fetchDebugCtxProfileCmd() tea.Cmd {
	pid := m.selectedPID
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return debugCtxProfileMsg{err: err}
		}
		defer client.Close()
		profile, err := client.CtxProfile(pid)
		return debugCtxProfileMsg{profile: profile, err: err}
	}
}

// --- strace → UnifiedEvent conversion ---

func straceToUnifiedEvent(sew ipc.SyscallEventWire) UnifiedEvent {
	ts := time.UnixMilli(sew.TimestampMs)

	// Convert wire format to SyscallEvent and format with debug.FormatEvent
	// for full strace output (e.g. "[  0.000s] CtxAlloc(size=64) → 17    1µs").
	se := wireToSyscallEvent(sew)
	summary := debug.FormatEvent(se, debug.Options{ColorEnabled: false})

	sev := SevInfo
	if sew.Error != "" {
		sev = SevError
	}
	rawCopy := sew
	return UnifiedEvent{
		Type:      EventSyscall,
		Severity:  sev,
		Timestamp: ts,
		PID:       sew.PID,
		Summary:   summary,
		RawEvent:  &rawCopy,
	}
}

// --- Ring buffer ---

func (m *dashboardModel) appendStraceEvent(ev UnifiedEvent) {
	m.debugStraceEvents = append(m.debugStraceEvents, ev)
	if len(m.debugStraceEvents) > maxDebugStraceEvents {
		m.debugStraceEvents = m.debugStraceEvents[len(m.debugStraceEvents)-maxDebugStraceEvents:]
	}
}

// --- Device latency ---

func (m *dashboardModel) updateDeviceLatency(sew ipc.SyscallEventWire) {
	dev := extractDeviceName(sew)
	if dev == "" {
		return
	}
	stats := m.debugState.DeviceLatency[dev]
	if stats == nil {
		stats = &deviceLatencyStats{}
		m.debugState.DeviceLatency[dev] = stats
	}
	stats.Count++
	stats.TotalMs += sew.DurationMs
	if sew.Error != "" {
		stats.ErrorCount++
	}
}

// extractDeviceName derives a short device name from syscall args.
func extractDeviceName(sew ipc.SyscallEventWire) string {
	path := ""
	if p, ok := sew.Args["path"]; ok {
		path = fmt.Sprintf("%v", p)
	}
	if path == "" {
		return ""
	}
	// Extract device from paths like /dev/llm/claude, /dev/fs, /dev/shell, /dev/mcp/xxx
	if strings.HasPrefix(path, "/dev/") {
		parts := strings.SplitN(path[5:], "/", 2)
		return parts[0]
	}
	return path
}

// --- Filtered debug events ---

// F9: Reuse isEventVisible() for filter logic, adding debugShowStrace as the only extra check.
// Returns consolidated events for user-friendly display (driver stream deltas merged into logical entries).
func (m dashboardModel) filteredDebugEvents() []UnifiedEvent {
	if len(m.debugEvents) == 0 {
		return nil
	}
	var result []UnifiedEvent
	for _, ev := range m.debugEvents {
		if ev.Type == EventSyscall && !m.debugState.ShowStrace {
			continue
		}
		if !isEventVisible(ev, m.timeline.StepFilters) {
			continue
		}
		result = append(result, ev)
	}
	return result
}

// clampDebugCursor ensures the cursor stays within the filtered event range.
func (m *dashboardModel) clampDebugCursor() {
	filtered := m.filteredDebugEvents()
	if m.debugState.Cursor >= len(filtered) {
		m.debugState.Cursor = max(len(filtered)-1, 0)
	}
	if m.debugState.ScrollTop > m.debugState.Cursor {
		m.debugState.ScrollTop = m.debugState.Cursor
	}
}

// --- Debug tick processing ---

// debugTickProcess drains the strace channel and merges events.
// Returns true if the stream channel was closed this tick (triggers auto-reload).
func (m *dashboardModel) debugTickProcess() bool {
	if !m.debugState.Mode {
		return false
	}
	streamClosed := false
	drainedAny := false
	// Non-blocking read from strace channel
	if m.debugState.StraceCh != nil {
		for {
			select {
			case sew, ok := <-m.debugState.StraceCh:
				if !ok {
					m.debugState.StraceCh = nil
					streamClosed = true
					goto doneReading
				}
				// Skip events already covered by historical load to avoid duplicates.
				if m.debugState.HistWatermark > 0 && sew.TimestampMs <= m.debugState.HistWatermark {
					continue
				}
				drainedAny = true
				ev := straceToUnifiedEvent(sew)
				m.appendStraceEvent(ev)
				m.updateDeviceLatency(sew)
			default:
				goto doneReading
			}
		}
	doneReading:
	}
	// Re-assign only when new strace data arrived or stream closed;
	// avoid overwriting debugEvents during PID transitions.
	if streamClosed || drainedAny {
		m.debugEvents = m.debugStraceEvents
	}
	return streamClosed
}

// --- Debug PID change handling ---

func (m dashboardModel) handleDebugPIDChange() (dashboardModel, tea.Cmd) {
	if !m.debugState.Mode {
		return m, nil
	}
	m.stopStraceStream()
	m.debugStraceEvents = nil
	m.debugEvents = nil
	m.debugState.DeviceLatency = make(map[string]*deviceLatencyStats)
	m.debugState.CtxProfile = nil
	m.debugState.ScrollTop = 0
	m.debugState.Cursor = 0
	m.debugState.AttachedPID = m.selectedPID
	m.debugState.AutoReloaded = false
	m.debugState.HistWatermark = 0

	if m.selectedPID == 0 {
		return m, nil
	}
	if m.isSelectedProcessDead() {
		return m, m.loadHistoricalStraceCmd()
	}
	return m, tea.Batch(m.loadHistoricalStraceCmd(), m.startStraceStreamCmd())
}

// --- Rendering ---

// renderDebugLayout renders the debug mode layout (tree + debug timeline + detail cards).
func (m dashboardModel) renderDebugLayout(w, h int) string {
	treeWidth := max(28, min(w*35/100, 50))
	rightWidth := max(w-treeWidth, 1)

	detailH := 3 // 1 separator + 2 content lines
	mainH := max(h-detailH, 3)

	treePane := m.renderDashboardTreePane(treeWidth, mainH)
	rightPane := m.renderDebugTimeline(rightWidth, mainH)
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, treePane, rightPane)

	detailLeft := m.renderDebugDetailLeft(treeWidth, 2)
	detailRight := m.renderDebugDetailRight(rightWidth, 2)
	detailRow := lipgloss.JoinHorizontal(lipgloss.Top, detailLeft, detailRight)

	return lipgloss.JoinVertical(lipgloss.Left, mainRow, detailRow)
}

// renderDebugTimeline renders the debug event stream panel.
func (m dashboardModel) renderDebugTimeline(width, height int) string {
	isActive := m.activePane == paneTimeline
	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	return renderFixedPanel(m.renderDebugTimelineContent(innerW, innerH), width, height, borderColor)
}

func (m dashboardModel) renderDebugTimelineContent(width, height int) string {
	var b strings.Builder
	truncW := max(width-1, 1)

	filtered := m.filteredDebugEvents()

	// Header
	debugLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true).Render("DEBUG")
	var agentName string
	for _, p := range m.processes {
		if p.PID == m.selectedPID {
			agentName = p.Intent
			break
		}
	}
	header := fmt.Sprintf("Events [%s PID %d %s]", debugLabel, m.selectedPID, agentName)
	b.WriteString(truncateAnsi(header, truncW))
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select a process first")
		return b.String()
	}
	if len(filtered) == 0 {
		b.WriteString("\n    Waiting for events…")
		return b.String()
	}

	listLines := max(height-2, 1)

	// Auto-scroll: keep cursor at the end (latest events, ascending order)
	if m.debugState.AutoScroll {
		m.debugState.Cursor = max(len(filtered)-1, 0)
	}
	cursor := min(m.debugState.Cursor, max(len(filtered)-1, 0))

	// Scroll management — ensure cursor is visible
	startIdx := m.debugState.ScrollTop
	if startIdx < 0 || startIdx >= len(filtered) {
		startIdx = 0
	}
	if cursor < startIdx {
		startIdx = cursor
	}
	if cursor >= startIdx+listLines {
		startIdx = cursor - listLines + 1
	}

	linesUsed := 0
	for fi := startIdx; fi < len(filtered) && linesUsed < listLines; fi++ {
		ev := filtered[fi]
		cursorMark := "  "
		if fi == cursor {
			cursorMark = "▸ "
		}

		var line string
		if ev.Type == EventSyscall {
			line = m.renderSyscallLine(ev, cursorMark, truncW)
		} else if ev.StepEntry != nil {
			// Reuse step rendering logic
			line = m.renderDebugStepLine(ev, cursorMark, truncW)
		} else {
			// System event
			icon := ui.EventTypeIcon(ev.Type)
			style := sysEventStyle(ev)
			line = fmt.Sprintf("%s %s %s", cursorMark, style.Render(icon), style.Render(ev.Summary))
		}

		if fi == cursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2D2D3D")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Render(line)
		}

		b.WriteString(truncateAnsi(line, truncW))
		b.WriteString("\n")
		linesUsed++
	}

	return b.String()
}

// renderSyscallLine renders a single strace event line using the full strace format.
func (m dashboardModel) renderSyscallLine(ev UnifiedEvent, cursorMark string, maxWidth int) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	if ev.Severity >= SevError {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	}

	return fmt.Sprintf("%s%s", cursorMark, style.Render(ev.Summary))
}

// renderDebugStepLine renders a step event in the debug timeline.
func (m dashboardModel) renderDebugStepLine(ev UnifiedEvent, cursorMark string, maxWidth int) string {
	if ev.StepEntry == nil {
		return cursorMark + ev.Summary
	}
	s := ev.StepEntry.Summary
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	stepLabel := dimStyle.Render(fmt.Sprintf("#%d", s.Step))

	action := s.Action
	summary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		summary = s.ToolPath
	}

	errMark := ""
	if s.HasError {
		errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
	}

	return fmt.Sprintf("%s%s %s %s%s", cursorMark, stepLabel, action, summary, errMark)
}

// --- Bottom detail cards ---

// renderDebugDetailLeft renders the Context Profile summary card.
// F10: Aggregates TopConsumers by role (system, user, asst, tools) for clarity.
func (m dashboardModel) renderDebugDetailLeft(width, height int) string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorMuted)).
		Width(max(width-2, 1)).
		Height(height)

	if m.debugState.CtxProfile == nil {
		return borderStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render("No ctx data"))
	}

	p := m.debugState.CtxProfile

	// Aggregate consumers by role category.
	type roleAgg struct {
		tokens int
		pct    float64
	}
	roles := map[string]*roleAgg{
		"system": {},
		"user":   {},
		"asst":   {},
		"tools":  {},
	}
	for _, c := range p.TopConsumers {
		switch {
		case c.Kind == "system_prompt":
			roles["system"].tokens += c.Tokens
			roles["system"].pct += c.Pct
		case c.Kind == "user":
			roles["user"].tokens += c.Tokens
			roles["user"].pct += c.Pct
		case c.Kind == "assistant":
			roles["asst"].tokens += c.Tokens
			roles["asst"].pct += c.Pct
		case strings.HasPrefix(c.Kind, "tool"):
			roles["tools"].tokens += c.Tokens
			roles["tools"].pct += c.Pct
		default:
			roles["tools"].tokens += c.Tokens
			roles["tools"].pct += c.Pct
		}
	}

	sep := "│"
	if ui.IsASCIIMode() {
		sep = "|"
	}

	// Order: system | user | asst | tools
	order := []struct {
		label string
		key   string
	}{
		{"sys", "system"},
		{"user", "user"},
		{"asst", "asst"},
		{"tools", "tools"},
	}

	var parts []string
	for _, o := range order {
		r := roles[o.key]
		if r.tokens > 0 {
			parts = append(parts, fmt.Sprintf("%s:%s(%.0f%%)", o.label, ui.FormatTokens(r.tokens), r.pct))
		}
	}

	var line string
	if len(parts) == 0 {
		// Fallback to classification view if no TopConsumers.
		c := p.Classification
		line = fmt.Sprintf("active:%s(%.0f%%) %s cold:%s(%.0f%%)",
			ui.FormatTokens(c.Active.Tokens), c.Active.Pct, sep,
			ui.FormatTokens(c.Cold.Tokens), c.Cold.Pct,
		)
	} else {
		line = strings.Join(parts, " "+sep+" ")
	}

	return borderStyle.Render(truncateAnsi(line, max(width-4, 1)))
}

// renderDebugDetailRight renders the Device Latency summary card.
func (m dashboardModel) renderDebugDetailRight(width, height int) string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorMuted)).
		Width(max(width-2, 1)).
		Height(height)

	if len(m.debugState.DeviceLatency) == 0 {
		return borderStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render("No latency data"))
	}

	// Sort by avg latency descending
	type devEntry struct {
		name  string
		stats deviceLatencyStats
	}
	var entries []devEntry
	for name, stats := range m.debugState.DeviceLatency {
		entries = append(entries, devEntry{name, *stats})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].stats.AvgMs() > entries[j].stats.AvgMs()
	})

	sep := "│"
	if ui.IsASCIIMode() {
		sep = "|"
	}

	var parts []string
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	for _, e := range entries {
		avg := e.stats.AvgMs()
		var label string
		if avg >= 1000 {
			label = fmt.Sprintf("%s:%.1fs", e.name, avg/1000)
		} else {
			label = fmt.Sprintf("%s:%.0fms", e.name, avg)
		}
		if e.stats.ErrorCount > 0 {
			label += errStyle.Render(fmt.Sprintf("(%derr)", e.stats.ErrorCount))
		}
		parts = append(parts, label)
	}

	line := strings.Join(parts, " "+sep+" ")
	return borderStyle.Render(truncateAnsi(line, max(width-4, 1)))
}

// --- Debug mode navigation ---

func (m dashboardModel) handleDebugKey(key string) (dashboardModel, tea.Cmd, bool) {
	// F8: When in filter sub-mode, delegate to handleStepFilterKey.
	if m.timeline.StepFilterMode {
		m = m.handleStepFilterKey(key)
		m.clampDebugCursor()
		return m, nil, true
	}

	// Keys handled regardless of active pane
	switch key {
	case "tab":
		if m.activePane == paneTree {
			m.activePane = paneTimeline
		} else {
			m.activePane = paneTree
		}
		return m, nil, true
	case "shift+tab":
		if m.activePane == paneTree {
			m.activePane = paneTimeline
		} else {
			m.activePane = paneTree
		}
		return m, nil, true
	case "f":
		m.timeline.StepFilterMode = !m.timeline.StepFilterMode
		// F4: Clamp cursor after filter change.
		m.clampDebugCursor()
		return m, nil, true
	}

	// Timeline-specific keys: only when debug timeline pane is active.
	// When tree pane is active, unhandled keys fall through to Layer 3+
	// so global shortcuts (q, ?, L, p, 1-8, etc.) and tree navigation work.
	if m.activePane != paneTimeline {
		return m, nil, false
	}

	filtered := m.filteredDebugEvents()
	switch key {
	case "up", "k":
		m.debugState.AutoScroll = false
		if m.debugState.Cursor > 0 {
			m.debugState.Cursor--
		}
		if m.debugState.Cursor < m.debugState.ScrollTop {
			m.debugState.ScrollTop = m.debugState.Cursor
		}
		return m, nil, true
	case "down", "j":
		m.debugState.AutoScroll = false
		if m.debugState.Cursor < len(filtered)-1 {
			m.debugState.Cursor++
		}
		visibleLines := max(m.dashboardVisibleLines()-4, 1)
		if m.debugState.Cursor >= m.debugState.ScrollTop+visibleLines {
			m.debugState.ScrollTop = m.debugState.Cursor - visibleLines + 1
		}
		// Re-enable auto-scroll when user reaches the bottom
		if m.debugState.Cursor >= len(filtered)-1 {
			m.debugState.AutoScroll = true
		}
		return m, nil, true
	case "s":
		m.debugState.ShowStrace = !m.debugState.ShowStrace
		if m.debugState.ShowStrace {
			m.statusMsg = "strace: on"
		} else {
			m.statusMsg = "strace: off"
		}
		m.statusMsgTTL = statusMsgDefaultTTL
		// F4: Clamp cursor after visibility change.
		m.clampDebugCursor()
		return m, nil, true
	case "v", "enter":
		// Step expand (if cursor on a step event)
		if m.debugState.Cursor >= 0 && m.debugState.Cursor < len(filtered) {
			ev := filtered[m.debugState.Cursor]
			if ev.StepEntry != nil {
				entry := ev.StepEntry
				if entry.Level == levelSummary {
					entry.Level = levelExpanded
					if m.timeline.StepDetailCache[entry.Summary.Step] == nil && !m.timeline.FetchingDetail && m.selectedPID > 0 {
						m.timeline.FetchingDetail = true
						return m, fetchStepDetailCmd(m.selectedPID, entry.Summary.Step), true
					}
				} else {
					entry.Level = levelSummary
				}
			}
		}
		return m, nil, true
	default:
		return m, nil, false
	}
}
