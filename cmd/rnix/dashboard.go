package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Visual debugging dashboard with multi-pane view",
	Long:  "Interactive TUI dashboard showing agent tree, timeline, and heatmap in a multi-pane layout.",
	Args:  cobra.NoArgs,
	RunE:  runDashboard,
}

type paneType int

const (
	paneTree paneType = iota
	paneTimeline
	paneHeatmap
)

type eventCategory int

const (
	catLLM eventCategory = iota
	catTool
	catIPC
	catVFS
	catError
)

const colorIPC = "#9B59B6"

const maxTimelineEvents = 1000

type timelineEvent struct {
	wire     ipc.SyscallEventWire
	category eventCategory
}

type timelineEventMsg struct {
	event ipc.SyscallEventWire
}

type timelineStreamDoneMsg struct {
	err error
}

// --- Heatmap types (Story 17-3) ---

type segmentKind int

const (
	segSystem segmentKind = iota
	segSkill
	segTool
	segUser
	segAssistant
	segLeaked
)

type activityLevel int

const (
	actActive activityLevel = iota
	actWarm
	actCold
	actLeaked
)

type heatmapSegment struct {
	label    string
	tokens   int
	pct      float64
	kind     segmentKind
	activity activityLevel
	summary  string
}

type heatmapProfileMsg struct {
	profile *debug.CtxProfileResult
	err     error
}

type dashboardModel struct {
	client      *ipc.Client
	width       int
	height      int
	activePane  paneType
	selectedPID types.PID
	processes   []vfs.ProcInfo
	treeRows    []flatRow
	treeCursor  int
	treeOffset  int
	connected   bool
	err         error
	statusMsg   string
	startTime   time.Time
	confirmKill bool
	confirmPID  types.PID

	// Timeline fields (Story 17-2)
	timelineEvents      []timelineEvent
	timelineAttachedPID types.PID
	timelineEventCh     <-chan ipc.SyscallEventWire
	timelineStopCh      chan struct{}
	timelineZoomLevel   int
	timelineViewStart   int64
	timelineEventCursor int
	timelineFilters     map[eventCategory]bool

	// Heatmap fields (Story 17-3)
	heatmapProfile   *debug.CtxProfileResult
	heatmapPID       types.PID
	heatmapSegments  []heatmapSegment
	heatmapCursor    int
	heatmapTickCount int
}

func newDashboardModel(client *ipc.Client) dashboardModel {
	return dashboardModel{
		client:          client,
		startTime:       time.Now(),
		connected:       client != nil,
		timelineFilters: defaultTimelineFilters(),
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tickCmd()
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.dashboardTick()
	case tea.KeyPressMsg:
		return m.dashboardKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case timelineStreamStartedMsg:
		m.timelineEventCh = msg.eventCh
		m.timelineStopCh = msg.stopCh
		return m, waitTimelineEventCmd(msg.eventCh)
	case timelineEventMsg:
		m = m.handleTimelineEvent(msg)
		return m, waitTimelineEventCmd(m.timelineEventCh)
	case timelineStreamDoneMsg:
		m.timelineEventCh = nil
		return m, nil
	case heatmapProfileMsg:
		return m, nil
	}
	return m, nil
}

func (m dashboardModel) dashboardTick() (tea.Model, tea.Cmd) {
	if m.client == nil {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			m.connected = false
			return m, tickCmd()
		}
		m.client = client
		m.connected = true
	}

	procs, err := m.client.ListProcs()
	if err != nil {
		m.client.Close()
		m.client = nil
		m.connected = false
		m.err = err
		return m, tickCmd()
	}

	m.err = nil
	m.connected = true
	m.statusMsg = ""
	m.processes = procs
	roots := buildProcessTree(procs)
	m.treeRows = flattenTree(roots)

	if m.treeCursor >= len(m.treeRows) {
		m.treeCursor = max(0, len(m.treeRows)-1)
	}
	if m.treeCursor < len(m.treeRows) {
		m.selectedPID = m.treeRows[m.treeCursor].proc.PID
	}

	if m.selectedPID != m.timelineAttachedPID && m.selectedPID > 0 {
		m = m.stopTimelineStream()
		m = m.handleTimelinePIDChange()
		m.timelineAttachedPID = m.selectedPID
		if m.connected {
			return m, tea.Batch(tickCmd(), startTimelineCmd(m.selectedPID))
		}
	}

	return m, tickCmd()
}

func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.confirmKill {
		switch key {
		case "y":
			if m.client != nil && m.confirmPID > 0 {
				if err := m.client.Kill(m.confirmPID, types.SIGTERM); err != nil {
					m.statusMsg = fmt.Sprintf("✗ kill PID %d: %v", m.confirmPID, err)
				} else {
					m.statusMsg = fmt.Sprintf("✓ signal sent to PID %d (SIGTERM)", m.confirmPID)
				}
			}
			m.confirmKill = false
			m.confirmPID = 0
		default:
			m.confirmKill = false
			m.confirmPID = 0
		}
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.activePane = (m.activePane + 1) % 3
		return m, nil
	}

	switch m.activePane {
	case paneTree:
		visibleLines := m.dashboardVisibleLines()
		switch key {
		case "up", "k":
			if m.treeCursor > 0 {
				m.treeCursor--
				if m.treeCursor < len(m.treeRows) {
					m.selectedPID = m.treeRows[m.treeCursor].proc.PID
				}
				if m.treeCursor < m.treeOffset {
					m.treeOffset = m.treeCursor
				}
			}
		case "down", "j":
			if m.treeCursor < len(m.treeRows)-1 {
				m.treeCursor++
				if m.treeCursor < len(m.treeRows) {
					m.selectedPID = m.treeRows[m.treeCursor].proc.PID
				}
				if visibleLines > 0 && m.treeCursor >= m.treeOffset+visibleLines {
					m.treeOffset = m.treeCursor - visibleLines + 1
				}
			}
		case "enter":
			if m.treeCursor < len(m.treeRows) {
				m.selectedPID = m.treeRows[m.treeCursor].proc.PID
			}
		default:
			if (msg.Code == 'K' || msg.ShiftedCode == 'K') && msg.Mod&tea.ModShift != 0 {
				if len(m.treeRows) > 0 && m.treeCursor < len(m.treeRows) {
					m.confirmKill = true
					m.confirmPID = m.treeRows[m.treeCursor].proc.PID
				}
			}
		}
	case paneTimeline:
		m = m.handleTimelineKey(key)
	}

	return m, nil
}

func (m dashboardModel) dashboardVisibleLines() int {
	v := m.height - 7
	if v < 1 {
		v = 1
	}
	return v
}

func (m dashboardModel) View() tea.View {
	content := m.renderDashboard()
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func colorState(s types.ProcessState) string {
	name := strings.ToLower(s.String())
	switch s {
	case types.StateRunning:
		return ui.SuccessStyle.Render(name)
	case types.StateZombie:
		return ui.WarningStyle.Render(name)
	case types.StateDead:
		return ui.MutedStyle.Render(name)
	case types.StateCreated:
		return ui.KernelStyle.Render(name)
	default:
		return name
	}
}

// --- Layout rendering ---

func (m dashboardModel) renderDashboard() string {
	w := m.width
	h := m.height
	if w == 0 {
		w = 120
	}
	if h == 0 {
		h = 40
	}

	titleBar := m.renderDashboardTitle()
	statusBar := m.renderDashboardStatus()

	contentHeight := h - 4
	if contentHeight < 3 {
		contentHeight = 3
	}

	treeWidth := w * 40 / 100
	if treeWidth < 30 {
		treeWidth = 30
	}
	if treeWidth > 60 {
		treeWidth = 60
	}
	rightWidth := w - treeWidth
	if rightWidth < 10 {
		rightWidth = 10
	}

	treePane := m.renderDashboardTreePane(treeWidth, contentHeight)

	topRightH := contentHeight / 2
	bottomRightH := contentHeight - topRightH
	timelinePane := m.renderTimelinePane(rightWidth, topRightH)
	heatmapPane := renderDashboardPlaceholder("Heatmap", "Coming Soon", rightWidth, bottomRightH, m.activePane == paneHeatmap)

	rightPane := lipgloss.JoinVertical(lipgloss.Left, timelinePane, heatmapPane)
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, treePane, rightPane)

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, mainContent, statusBar)
}

func (m dashboardModel) renderDashboardTitle() string {
	var b strings.Builder
	b.WriteString("  Rnix Dashboard")
	if m.connected {
		b.WriteString("  ● Connected")
	} else {
		b.WriteString("  ○ Disconnected")
	}

	active := 0
	totalTokens := 0
	for _, p := range m.processes {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			active++
		}
		totalTokens += p.TokensUsed
	}
	fmt.Fprintf(&b, " | Processes: %d | Tokens: %s", active, ui.FormatTokens(totalTokens))

	return b.String()
}

func (m dashboardModel) renderDashboardStatus() string {
	if m.confirmKill {
		return fmt.Sprintf("  Kill PID %d? [y/N]", m.confirmPID)
	}
	if m.statusMsg != "" {
		return fmt.Sprintf("  %s  |  q:Quit  Tab:Switch Pane  j/k:Navigate  K:Kill", m.statusMsg)
	}
	if m.activePane == paneTimeline {
		return "  q:Quit  Tab:Switch Pane  h/l:Scroll  +/-:Zoom  1-4:Filter  j/k:Select"
	}
	return "  q:Quit  Tab:Switch Pane  j/k:Navigate  K:Kill  Enter:Select"
}

func (m dashboardModel) renderDashboardTreePane(width, height int) string {
	isActive := m.activePane == paneTree

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	var b strings.Builder
	b.WriteString(" Agent Tree \n")

	now := time.Now()
	visibleLines := innerH - 1
	if visibleLines < 1 {
		visibleLines = 1
	}

	endIdx := m.treeOffset + visibleLines
	if endIdx > len(m.treeRows) {
		endIdx = len(m.treeRows)
	}

	for i := m.treeOffset; i < endIdx; i++ {
		row := m.treeRows[i]
		cursor := "  "
		if i == m.treeCursor {
			cursor = "▸ "
		}

		state := colorState(row.proc.State)

		skills := ui.FormatSkills(row.proc.Skills, 12, "—")

		tokens := ui.FormatTokens(row.proc.TokensUsed)
		if row.proc.ContextBudget > 0 {
			pct := row.proc.TokensUsed * 100 / row.proc.ContextBudget
			tokens = fmt.Sprintf("%s/%s(%d%%)",
				ui.FormatTokens(row.proc.TokensUsed),
				ui.FormatTokens(row.proc.ContextBudget), pct)
			if pct >= 80 {
				tokens = ui.WarningStyle.Render(tokens)
			}
		}

		elapsed := ui.FormatDuration(now.Sub(row.proc.CreatedAt))

		line := fmt.Sprintf("%s%sPID %-3d %-9s %-12s %s %s",
			cursor, row.prefix, row.proc.PID, state, skills, tokens, elapsed)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

func renderDashboardPlaceholder(title, placeholder string, width, height int, active bool) string {
	borderColor := lipgloss.Color(ui.ColorMuted)
	if active {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	content := fmt.Sprintf(" %s \n\n    (%s)", title, placeholder)
	return style.Render(content)
}

// --- Process tree construction ---

func buildProcessTree(procs []vfs.ProcInfo) []*treeNode {
	if len(procs) == 0 {
		return nil
	}

	nodes := make(map[types.PID]*treeNode, len(procs))
	for i := range procs {
		nodes[procs[i].PID] = &treeNode{proc: procs[i]}
	}

	var roots []*treeNode
	for _, n := range nodes {
		if parent, ok := nodes[n.proc.PPID]; ok && n.proc.PID != n.proc.PPID {
			parent.children = append(parent.children, n)
		} else {
			roots = append(roots, n)
		}
	}

	sortNodes := func(ns []*treeNode) {
		sort.Slice(ns, func(i, j int) bool {
			return ns[i].proc.PID < ns[j].proc.PID
		})
	}
	sortNodes(roots)
	for _, n := range nodes {
		if len(n.children) > 1 {
			sortNodes(n.children)
		}
	}

	return roots
}

// --- Timeline logic (Story 17-2) ---

func classifySyscall(ev ipc.SyscallEventWire) eventCategory {
	if ev.Error != "" {
		return catError
	}
	if isLLMEvent(ev) {
		return catLLM
	}
	switch ev.Syscall {
	case "Send", "Recv", "Pipe", "Signal", "SigBlock", "SigUnblock",
		"JoinGroup", "LeaveGroup", "GetProcGroup", "SignalGroup":
		return catIPC
	case "Spawn", "Kill", "Wait", "Reparent", "SpawnThread", "JoinThread",
		"SpawnSupervisor", "SpawnCoroutine", "Yield", "ResumeCoroutine":
		return catTool
	}
	if isToolPathEvent(ev) {
		return catTool
	}
	return catVFS
}

func isLLMEvent(ev ipc.SyscallEventWire) bool {
	for _, key := range []string{"path", "tool"} {
		if v, ok := ev.Args[key]; ok {
			if s, ok := v.(string); ok && strings.Contains(s, "/dev/llm/") {
				return true
			}
		}
	}
	return false
}

func isToolPathEvent(ev ipc.SyscallEventWire) bool {
	if v, ok := ev.Args["path"]; ok {
		if s, ok := v.(string); ok {
			return strings.Contains(s, "/dev/shell/") || strings.Contains(s, "/dev/fs/")
		}
	}
	return false
}

func categoryColor(cat eventCategory) string {
	switch cat {
	case catLLM:
		return ui.ColorAgent
	case catTool:
		return ui.ColorSuccess
	case catIPC:
		return colorIPC
	case catVFS:
		return ui.ColorWarning
	case catError:
		return ui.ColorError
	default:
		return ui.ColorMuted
	}
}

func categoryLabel(cat eventCategory) string {
	switch cat {
	case catLLM:
		return "LLM"
	case catTool:
		return "Tool"
	case catIPC:
		return "IPC"
	case catVFS:
		return "VFS"
	case catError:
		return "Err"
	default:
		return "?"
	}
}

func defaultTimelineFilters() map[eventCategory]bool {
	return map[eventCategory]bool{
		catLLM:   true,
		catTool:  true,
		catIPC:   true,
		catVFS:   true,
		catError: true,
	}
}

func (m dashboardModel) handleTimelineEvent(msg timelineEventMsg) dashboardModel {
	ev := timelineEvent{
		wire:     msg.event,
		category: classifySyscall(msg.event),
	}
	m.timelineEvents = append(m.timelineEvents, ev)
	if len(m.timelineEvents) > maxTimelineEvents {
		m.timelineEvents = m.timelineEvents[len(m.timelineEvents)-maxTimelineEvents:]
	}
	return m
}

type timelineStreamStartedMsg struct {
	eventCh <-chan ipc.SyscallEventWire
	stopCh  chan struct{}
}

func startTimelineCmd(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return timelineStreamDoneMsg{}
		}
		ch := make(chan ipc.SyscallEventWire, 64)
		stopCh := make(chan struct{})
		go func() {
			defer close(ch)
			defer client.Close()
			_ = client.AttachDebug(pid, func(ev ipc.SyscallEventWire) {
				select {
				case ch <- ev:
				case <-stopCh:
				}
			})
		}()
		return timelineStreamStartedMsg{eventCh: ch, stopCh: stopCh}
	}
}

func waitTimelineEventCmd(ch <-chan ipc.SyscallEventWire) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return timelineStreamDoneMsg{}
		}
		return timelineEventMsg{event: ev}
	}
}

func (m dashboardModel) stopTimelineStream() dashboardModel {
	if m.timelineStopCh != nil {
		close(m.timelineStopCh)
		m.timelineStopCh = nil
	}
	m.timelineEventCh = nil
	return m
}

func (m dashboardModel) handleTimelinePIDChange() dashboardModel {
	if m.selectedPID == m.timelineAttachedPID {
		return m
	}
	m.timelineEvents = nil
	m.timelineZoomLevel = 0
	m.timelineViewStart = 0
	m.timelineEventCursor = 0
	m.timelineAttachedPID = m.selectedPID
	return m
}

func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	if m.timelineFilters == nil {
		m.timelineFilters = defaultTimelineFilters()
	}
	switch key {
	case "+", "=":
		if m.timelineZoomLevel < 5 {
			m.timelineZoomLevel++
		}
	case "-":
		if m.timelineZoomLevel > 0 {
			m.timelineZoomLevel--
		}
	case "h", "left":
		step := m.timelineScrollStep()
		if m.timelineViewStart > step {
			m.timelineViewStart -= step
		} else {
			m.timelineViewStart = 0
		}
	case "l", "right":
		m.timelineViewStart += m.timelineScrollStep()
	case "up", "k":
		if m.timelineEventCursor > 0 {
			m.timelineEventCursor--
		}
	case "down", "j":
		visible := m.filteredTimelineEvents()
		if m.timelineEventCursor < len(visible)-1 {
			m.timelineEventCursor++
		}
	case "1":
		m.timelineFilters[catLLM] = !m.timelineFilters[catLLM]
	case "2":
		m.timelineFilters[catTool] = !m.timelineFilters[catTool]
	case "3":
		m.timelineFilters[catIPC] = !m.timelineFilters[catIPC]
	case "4":
		m.timelineFilters[catVFS] = !m.timelineFilters[catVFS]
	}
	return m
}

var zoomWindowMs = []int64{0, 1000, 5000, 30000, 300000, 1800000}

func (m dashboardModel) timelineScrollStep() int64 {
	if m.timelineZoomLevel >= 1 && m.timelineZoomLevel < len(zoomWindowMs) {
		return zoomWindowMs[m.timelineZoomLevel] / 10
	}
	return 500
}

func (m dashboardModel) filteredTimelineEvents() []timelineEvent {
	if m.timelineFilters == nil {
		return m.timelineEvents
	}
	var result []timelineEvent
	for _, ev := range m.timelineEvents {
		if m.timelineFilters[ev.category] {
			result = append(result, ev)
		}
	}
	return result
}

func (m dashboardModel) renderTimelinePane(width, height int) string {
	isActive := m.activePane == paneTimeline

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	var b strings.Builder

	b.WriteString(" Timeline")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
	}
	fmt.Fprintf(&b, " | %d events", len(m.timelineEvents))

	b.WriteString("  ")
	for _, cat := range []eventCategory{catLLM, catTool, catIPC, catVFS} {
		label := categoryLabel(cat)
		if m.timelineFilters != nil && !m.timelineFilters[cat] {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render("["+label+"]"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(categoryColor(cat))).Render("["+label+"]"))
		}
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select an agent to view timeline")
		return style.Render(b.String())
	}

	filtered := m.filteredTimelineEvents()
	if len(filtered) == 0 {
		b.WriteString("\n    Waiting for events...")
		return style.Render(b.String())
	}

	barWidth := innerW - 2
	if barWidth < 10 {
		barWidth = 10
	}
	b.WriteString(m.renderTimelineBar(filtered, barWidth))
	b.WriteString("\n")

	listLines := innerH - 3
	if listLines < 1 {
		listLines = 1
	}
	if m.timelineEventCursor >= len(filtered) {
		m.timelineEventCursor = len(filtered) - 1
	}

	startIdx := 0
	if m.timelineEventCursor >= listLines {
		startIdx = m.timelineEventCursor - listLines + 1
	}
	endIdx := startIdx + listLines
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}

	for i := startIdx; i < endIdx; i++ {
		ev := filtered[i]
		cursor := "  "
		if i == m.timelineEventCursor {
			cursor = "▸ "
		}

		ts := fmt.Sprintf("[%7.3fs]", float64(ev.wire.TimestampMs)/1000.0)
		dur := formatTimelineDuration(ev.wire.DurationMs)
		catColor := categoryColor(ev.category)
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(catColor))

		args := formatTimelineArgs(ev.wire.Args, 30)
		line := fmt.Sprintf("%s%s %s(%s) %s",
			cursor, ts, catStyle.Render(ev.wire.Syscall), args, dur)
		if ev.wire.Error != "" {
			line += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("ERR:"+ev.wire.Error)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderTimelineBar(events []timelineEvent, width int) string {
	if len(events) == 0 || width <= 0 {
		return ""
	}

	minTs := events[0].wire.TimestampMs
	maxTs := events[len(events)-1].wire.TimestampMs
	if m.timelineZoomLevel > 0 && m.timelineZoomLevel < len(zoomWindowMs) {
		windowMs := zoomWindowMs[m.timelineZoomLevel]
		minTs = m.timelineViewStart
		maxTs = m.timelineViewStart + windowMs
	}

	span := maxTs - minTs
	if span <= 0 {
		span = 1
	}

	bar := make([]eventCategory, width)
	barSet := make([]bool, width)
	for _, ev := range events {
		pos := int((ev.wire.TimestampMs - minTs) * int64(width) / span)
		if pos < 0 || pos >= width {
			continue
		}
		if !barSet[pos] || ev.category == catError {
			bar[pos] = ev.category
			barSet[pos] = true
		}
	}

	var b strings.Builder
	for i := 0; i < width; i++ {
		if !barSet[i] {
			b.WriteString("·")
		} else {
			catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(categoryColor(bar[i])))
			b.WriteString(catStyle.Render("█"))
		}
	}
	return b.String()
}

func formatTimelineDuration(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.0fµs", ms*1000)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

func formatTimelineArgs(args map[string]any, maxLen int) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := fmt.Sprintf("%v", args[k])
		if utf8.RuneCountInString(v) > 20 {
			runes := []rune(v)
			v = string(runes[:17]) + "..."
		}
		parts = append(parts, k+"="+v)
	}
	result := strings.Join(parts, ", ")
	if utf8.RuneCountInString(result) > maxLen {
		runes := []rune(result)
		result = string(runes[:maxLen-3]) + "..."
	}
	return result
}

// --- Heatmap logic (Story 17-3) ---

func buildHeatmapSegments(_ *debug.CtxProfileResult) []heatmapSegment {
	return nil
}

func segmentColor(_ segmentKind, _ activityLevel) string {
	return ""
}

func mapConsumerKind(_ string) segmentKind {
	return segSystem
}

func (m dashboardModel) handleHeatmapPIDChange() dashboardModel {
	return m
}

func (m dashboardModel) handleHeatmapKey(_ string) dashboardModel {
	return m
}

func (m dashboardModel) renderHeatmapPane(_ int, _ int) string {
	return ""
}

func fetchHeatmapCmd(_ types.PID) tea.Cmd {
	return nil
}

// --- Command runner ---

func runDashboard(_ *cobra.Command, _ []string) error {
	profile := ui.DetectProfile(os.Stdout)
	ui.InitStyles(profile)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), "✗ No rnix daemon running. Start an agent first with: rnix -i \"intent\"")
		return nil
	}

	model := newDashboardModel(client)
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		client.Close()
		return fmt.Errorf("dashboard: %w", err)
	}
	if fm, ok := final.(dashboardModel); ok && fm.client != nil {
		fm.client.Close()
	} else {
		client.Close()
	}
	return nil
}
