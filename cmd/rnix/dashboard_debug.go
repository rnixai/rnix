package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// --- Constants ---

const maxDebugStraceEvents = 500

// --- Types ---

type deviceLatencyStats struct {
	Count      int
	TotalMs    float64
	ErrorCount int
}

func (s deviceLatencyStats) AvgMs() float64 {
	if s.Count == 0 {
		return 0
	}
	return s.TotalMs / float64(s.Count)
}

// --- tea.Msg types for Debug mode ---

type debugStraceStartedMsg struct {
	client *ipc.Client
	ch     <-chan ipc.SyscallEventWire
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
}

// handleDebugMsg dispatches debug-related tea.Msg types.
// Returns (model, cmd, handled). If handled is false, the msg was not a debug msg.
func (m dashboardModel) handleDebugMsg(msg tea.Msg) (dashboardModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case debugStraceStartedMsg:
		m.debugClient = msg.client
		m.debugStraceCh = msg.ch
		return m, nil, true
	case debugStraceErrorMsg:
		m.statusMsg = fmt.Sprintf("✗ strace: %v", msg.err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil, true
	case debugCtxProfileMsg:
		if msg.err == nil && msg.profile != nil {
			m.debugCtxProfile = msg.profile
		}
		return m, nil, true
	case debugHistoricalStraceMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ strace history: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil, true
		}
		for _, sew := range msg.events {
			ev := straceToUnifiedEvent(sew)
			m.appendStraceEvent(ev)
			m.updateDeviceLatency(sew)
		}
		m.debugEvents = mergeDebugEvents(m.stepEntries, m.debugStraceEvents, m.selectedPID)
		return m, nil, true
	}
	return m, nil, false
}

// debugTickCmds returns tea.Cmds needed for debug mode during the tick cycle.
func (m *dashboardModel) debugTickCmds() []tea.Cmd {
	var cmds []tea.Cmd
	m.debugTickProcess()
	if m.debugMode && m.debugAttachedPID != m.selectedPID {
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
	m.viewMode = viewDebug
	m.debugMode = true
	m.debugShowStrace = true
	m.debugStraceEvents = nil
	m.debugEvents = nil
	m.debugDeviceLatency = make(map[string]*deviceLatencyStats)
	m.debugScrollTop = 0
	m.debugCursor = 0
	m.debugCtxProfile = nil
	m.debugAttachedPID = m.selectedPID

	if m.isSelectedProcessDead() {
		return m, m.loadHistoricalStraceCmd()
	}
	return m, m.startStraceStreamCmd()
}

func (m dashboardModel) exitDebugMode() dashboardModel {
	m.viewMode = viewDefault
	m.debugMode = false
	m.stopStraceStream()
	m.debugEvents = nil
	m.debugStraceEvents = nil
	m.debugCtxProfile = nil
	m.debugDeviceLatency = nil
	m.debugAttachedPID = 0
	m.debugScrollTop = 0
	m.debugCursor = 0
	return m
}

// stopStraceStream closes the independent debug IPC client (which causes the
// scanner to stop) and resets channel/client references.
func (m *dashboardModel) stopStraceStream() {
	if m.debugClient != nil {
		m.debugClient.Close()
		m.debugClient = nil
	}
	m.debugStraceCh = nil
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
		go func() {
			defer close(ch)
			_ = client.AttachDebug(pid, func(sew ipc.SyscallEventWire) {
				select {
				case ch <- sew:
				default: // channel full, drop
				}
			})
		}()
		return debugStraceStartedMsg{client: client, ch: ch}
	}
}

func (m dashboardModel) loadHistoricalStraceCmd() tea.Cmd {
	pid := m.selectedPID
	uuid := m.selectedUUID
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return debugHistoricalStraceMsg{err: err}
		}
		defer client.Close()
		events, err := client.ListEvents(pid, uuid)
		return debugHistoricalStraceMsg{events: events, err: err}
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
	resource := ""
	if p, ok := sew.Args["path"]; ok {
		resource = fmt.Sprintf("%v", p)
	} else if fd, ok := sew.Args["fd"]; ok {
		resource = fmt.Sprintf("fd=%v", fd)
	}

	ascii := ui.IsASCIIMode()
	prefix := "·"
	if ascii {
		prefix = "."
	}

	summary := fmt.Sprintf("%s %s %s %.0fms", prefix, sew.Syscall, resource, sew.DurationMs)
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

// --- Event merging ---

// mergeDebugEvents merges step entries and strace events into a single timeline,
// sorted by timestamp descending (newest first).
func mergeDebugEvents(stepEntries []stepEntry, straceEvents []UnifiedEvent, pid types.PID) []UnifiedEvent {
	var result []UnifiedEvent

	// Convert step entries to unified events
	baseTime := time.Now()
	for i := range stepEntries {
		ue := stepToUnifiedEvent(&stepEntries[i], baseTime, i)
		ue.PID = pid
		result = append(result, ue)
	}

	// Append strace events
	result = append(result, straceEvents...)

	// Sort descending by timestamp (newest first)
	sort.Sort(UnifiedEventSlice(result))
	return result
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
	stats := m.debugDeviceLatency[dev]
	if stats == nil {
		stats = &deviceLatencyStats{}
		m.debugDeviceLatency[dev] = stats
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

func (m dashboardModel) filteredDebugEvents() []UnifiedEvent {
	if len(m.debugEvents) == 0 {
		return nil
	}
	var result []UnifiedEvent
	for _, ev := range m.debugEvents {
		if ev.Type == EventSyscall {
			if !m.debugShowStrace {
				continue
			}
			if !m.stepFilters[EventSyscall] {
				continue
			}
		} else if ev.Type == EventStep {
			if ev.StepEntry != nil && !m.stepFilters[ev.StepEntry.summary.Action] {
				continue
			}
		}
		result = append(result, ev)
	}
	return result
}

// --- Debug tick processing ---

func (m *dashboardModel) debugTickProcess() {
	if !m.debugMode {
		return
	}
	// Non-blocking read from strace channel
	if m.debugStraceCh != nil {
		for {
			select {
			case sew, ok := <-m.debugStraceCh:
				if !ok {
					m.debugStraceCh = nil
					return
				}
				ev := straceToUnifiedEvent(sew)
				m.appendStraceEvent(ev)
				m.updateDeviceLatency(sew)
			default:
				goto doneReading
			}
		}
	doneReading:
	}
	// Merge debug events
	m.debugEvents = mergeDebugEvents(m.stepEntries, m.debugStraceEvents, m.selectedPID)
}

// --- Debug PID change handling ---

func (m dashboardModel) handleDebugPIDChange() (dashboardModel, tea.Cmd) {
	if !m.debugMode {
		return m, nil
	}
	m.stopStraceStream()
	m.debugStraceEvents = nil
	m.debugEvents = nil
	m.debugDeviceLatency = make(map[string]*deviceLatencyStats)
	m.debugCtxProfile = nil
	m.debugScrollTop = 0
	m.debugCursor = 0
	m.debugAttachedPID = m.selectedPID

	if m.selectedPID == 0 {
		return m, nil
	}
	if m.isSelectedProcessDead() {
		return m, m.loadHistoricalStraceCmd()
	}
	return m, m.startStraceStreamCmd()
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

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	return style.Render(m.renderDebugTimelineContent(innerW, innerH))
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
	cursor := min(m.debugCursor, max(len(filtered)-1, 0))

	// Scroll management
	startIdx := m.debugScrollTop
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

// renderSyscallLine renders a single strace event line.
func (m dashboardModel) renderSyscallLine(ev UnifiedEvent, cursorMark string, maxWidth int) string {
	ts := ev.Timestamp.Format("15:04:05.000")
	tsStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render(ts)

	summaryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	if ev.Severity >= SevError {
		summaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	}

	return fmt.Sprintf("%s%s %s", cursorMark, tsStyled, summaryStyle.Render(ev.Summary))
}

// renderDebugStepLine renders a step event in the debug timeline.
func (m dashboardModel) renderDebugStepLine(ev UnifiedEvent, cursorMark string, maxWidth int) string {
	if ev.StepEntry == nil {
		return cursorMark + ev.Summary
	}
	s := ev.StepEntry.summary
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
func (m dashboardModel) renderDebugDetailLeft(width, height int) string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorMuted)).
		Width(max(width-2, 1)).
		Height(height)

	if m.debugCtxProfile == nil {
		return borderStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render("No ctx data"))
	}

	p := m.debugCtxProfile
	c := p.Classification

	sep := "│"
	if ui.IsASCIIMode() {
		sep = "|"
	}

	line := fmt.Sprintf("active:%s(%.0f%%) %s warm:%s(%.0f%%) %s cold:%s(%.0f%%) %s leaked:%s(%.0f%%)",
		ui.FormatTokens(c.Active.Tokens), c.Active.Pct, sep,
		ui.FormatTokens(c.Warm.Tokens), c.Warm.Pct, sep,
		ui.FormatTokens(c.Cold.Tokens), c.Cold.Pct, sep,
		ui.FormatTokens(c.Leaked.Tokens), c.Leaked.Pct,
	)

	return borderStyle.Render(truncateAnsi(line, max(width-4, 1)))
}

// renderDebugDetailRight renders the Device Latency summary card.
func (m dashboardModel) renderDebugDetailRight(width, height int) string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorMuted)).
		Width(max(width-2, 1)).
		Height(height)

	if len(m.debugDeviceLatency) == 0 {
		return borderStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render("No latency data"))
	}

	// Sort by avg latency descending
	type devEntry struct {
		name  string
		stats deviceLatencyStats
	}
	var entries []devEntry
	for name, stats := range m.debugDeviceLatency {
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

func (m dashboardModel) handleDebugKey(key string) (dashboardModel, tea.Cmd) {
	filtered := m.filteredDebugEvents()
	switch key {
	case "up", "k":
		if m.debugCursor > 0 {
			m.debugCursor--
		}
		if m.debugCursor < m.debugScrollTop {
			m.debugScrollTop = m.debugCursor
		}
	case "down", "j":
		if m.debugCursor < len(filtered)-1 {
			m.debugCursor++
		}
		visibleLines := max(m.dashboardVisibleLines()-4, 1)
		if m.debugCursor >= m.debugScrollTop+visibleLines {
			m.debugScrollTop = m.debugCursor - visibleLines + 1
		}
	case "s":
		m.debugShowStrace = !m.debugShowStrace
		if m.debugShowStrace {
			m.statusMsg = "strace: on"
		} else {
			m.statusMsg = "strace: off"
		}
		m.statusMsgTTL = statusMsgDefaultTTL
	case "f":
		m.stepFilterMode = !m.stepFilterMode
	case "tab":
		if m.activePane == paneTree {
			m.activePane = paneTimeline
		} else {
			m.activePane = paneTree
		}
	case "shift+tab":
		if m.activePane == paneTree {
			m.activePane = paneTimeline
		} else {
			m.activePane = paneTree
		}
	case "v", "enter":
		// Step expand (if cursor on a step event)
		if m.debugCursor >= 0 && m.debugCursor < len(filtered) {
			ev := filtered[m.debugCursor]
			if ev.StepEntry != nil {
				entry := ev.StepEntry
				if entry.level == levelSummary {
					entry.level = levelExpanded
					if m.stepDetailCache[entry.summary.Step] == nil && !m.fetchingDetail && m.selectedPID > 0 {
						m.fetchingDetail = true
						return m, fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
					}
				} else {
					entry.level = levelSummary
				}
			}
		}
	}
	return m, nil
}
