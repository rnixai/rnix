package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

const statusMsgDefaultTTL = 4

const slowStepThresholdMs = 1000.0

type stepDetailLevel int

const (
	levelSummary  stepDetailLevel = 0
	levelExpanded stepDetailLevel = 1
	levelDebug    stepDetailLevel = 2
)

type stepEntry struct {
	summary    ipc.StepSummaryWire
	level      stepDetailLevel
	autoExpand bool
}

type stepListMsg struct {
	steps []ipc.StepSummaryWire
	total int
	err   error
}

type stepDetailResultMsg struct {
	step   int
	detail *ipc.GetStepDetailResponse
	err    error
}

type timelineEvent struct {
	wire     ipc.SyscallEventWire
	category eventCategory
}

type timelineEventMsg struct {
	event ipc.SyscallEventWire
}

type timelineStreamDoneMsg struct{}

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

// --- Pane linkage & process operations types (Story 17-4) ---

type execResultMsg struct {
	err error
}

type recordToggleMsg struct {
	pid        types.PID
	recordID   string
	stopped    bool
	eventCount uint64
	err        error
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
	heatmapExpanded  bool
	heatmapTickCount int
	heatmapErr       error

	// Pane linkage & process operations (Story 17-4)
	recording    map[types.PID]string
	statusMsgTTL int

	// Step timeline fields (Story 27-3)
	stepTimelineMode bool
	stepEntries      []stepEntry
	stepCursor       int
	stepDetailCache  map[int]*ipc.GetStepDetailResponse
	lastFetchedStep  int
	fetchingDetail   bool

	// Offline replay fields (Story 17-5)
	replayMode       bool
	replayReader     *debug.RecordReader
	replayCursor     int
	replayPlaying    bool
	replaySpeed      float64
	prevReplayCursor int
}

func newDashboardModel(client *ipc.Client) dashboardModel {
	return dashboardModel{
		client:           client,
		startTime:        time.Now(),
		connected:        client != nil,
		timelineFilters:  defaultTimelineFilters(),
		recording:        make(map[types.PID]string),
		stepTimelineMode: true,
		stepDetailCache:  make(map[int]*ipc.GetStepDetailResponse),
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
		if msg.err != nil {
			m.heatmapErr = msg.err
			return m, nil
		}
		m.heatmapErr = nil
		if msg.profile != nil {
			m.heatmapProfile = msg.profile
			m.heatmapSegments = buildHeatmapSegments(msg.profile)
		}
		return m, nil
	case execResultMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Command error: %v", msg.err)
		} else {
			m.statusMsg = "Returned to dashboard"
		}
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	case recordToggleMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Record error: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil
		}
		if msg.stopped {
			delete(m.recording, msg.pid)
			m.statusMsg = fmt.Sprintf("Recording stopped (%d events)", msg.eventCount)
		} else {
			m.recording[msg.pid] = msg.recordID
			m.statusMsg = fmt.Sprintf("Recording started (ID: %s)", msg.recordID)
		}
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	case stepListMsg:
		if msg.err == nil && len(msg.steps) > 0 {
			m = m.applyNewSteps(msg.steps)
			if last := msg.steps[len(msg.steps)-1]; last.Step > m.lastFetchedStep {
				m.lastFetchedStep = last.Step
			}
		}
		return m, nil
	case stepDetailResultMsg:
		m.fetchingDetail = false
		if msg.err == nil && msg.detail != nil {
			m.stepDetailCache[msg.step] = msg.detail
		}
		return m, nil
	}
	return m, nil
}

func (m dashboardModel) dashboardTick() (tea.Model, tea.Cmd) {
	m.heatmapTickCount++

	if m.statusMsgTTL > 0 {
		m.statusMsgTTL--
		if m.statusMsgTTL == 0 {
			m.statusMsg = ""
		}
	}

	if m.replayMode {
		return m.replayTick()
	}

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
	m.processes = procs
	roots := buildProcessTree(procs)
	m.treeRows = flattenTree(roots)

	if m.treeCursor >= len(m.treeRows) {
		m.treeCursor = max(0, len(m.treeRows)-1)
	}
	if m.treeCursor < len(m.treeRows) {
		m.selectedPID = m.treeRows[m.treeCursor].proc.PID
	}

	cmds := []tea.Cmd{tickCmd()}

	pidChanged := m.selectedPID != m.timelineAttachedPID || m.selectedPID != m.heatmapPID
	if pidChanged {
		m2, pidCmd := m.handlePIDChange()
		m = m2
		if pidCmd != nil {
			cmds = append(cmds, pidCmd)
		}
	} else if m.selectedPID > 0 && m.connected && m.heatmapTickCount%5 == 0 {
		cmds = append(cmds, fetchHeatmapCmd(m.selectedPID))
	}

	if m.stepTimelineMode && m.selectedPID > 0 && m.connected {
		cmds = append(cmds, fetchStepsCmd(m.selectedPID, m.lastFetchedStep))
	}

	if len(m.recording) > 0 {
		pidSet := make(map[types.PID]bool, len(m.processes))
		for _, p := range m.processes {
			pidSet[p.PID] = true
		}
		for pid := range m.recording {
			if !pidSet[pid] {
				delete(m.recording, pid)
			}
		}
	}

	return m, tea.Batch(cmds...)
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
					m.statusMsg = fmt.Sprintf("Killed PID %d", m.confirmPID)
				}
				m.statusMsgTTL = statusMsgDefaultTTL
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

	if m.replayMode {
		m2, cmd := m.handleReplayKey(key)
		return m2, cmd
	}

	if m.stepTimelineMode && len(m.stepEntries) > 0 && m.stepCursor < len(m.stepEntries) {
		if msg.Code == 'v' {
			entry := &m.stepEntries[m.stepCursor]
			if entry.level == levelSummary {
				entry.level = levelExpanded
				if m.stepDetailCache[entry.summary.Step] == nil && !m.fetchingDetail && m.selectedPID > 0 {
					m.fetchingDetail = true
					return m, fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
				}
			} else {
				entry.level = levelSummary
			}
			return m, nil
		}
		if msg.Code == 'V' {
			entry := &m.stepEntries[m.stepCursor]
			switch entry.level {
			case levelSummary, levelExpanded:
				entry.level = levelDebug
				if m.stepDetailCache[entry.summary.Step] == nil && !m.fetchingDetail && m.selectedPID > 0 {
					m.fetchingDetail = true
					return m, fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
				}
			case levelDebug:
				entry.level = levelExpanded
			}
			return m, nil
		}
	}

	if m.activePane == paneTree {
		prevPID := m.selectedPID
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
		if m.selectedPID != prevPID {
			m2, cmd := m.handlePIDChange()
			return m2, cmd
		}
		return m, nil
	}

	isPaneNavConflict := (m.activePane == paneTimeline && (key == "l" || key == "h" || key == "k")) ||
		(m.activePane == paneHeatmap && key == "k")
	if !isPaneNavConflict && m.selectedPID > 0 && m.connected {
		switch key {
		case "k":
			m.confirmKill = true
			m.confirmPID = m.selectedPID
			return m, nil
		case "a":
			c := exec.Command(os.Args[0], "gdb", fmt.Sprint(m.selectedPID))
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return execResultMsg{err: err}
			})
		case "l":
			c := exec.Command(os.Args[0], "log", fmt.Sprint(m.selectedPID))
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return execResultMsg{err: err}
			})
		case "r":
			recordID := m.recording[m.selectedPID]
			return m, toggleRecordCmd(m.selectedPID, recordID)
		}
	}

	if (msg.Code == 'K' || msg.ShiftedCode == 'K') && msg.Mod&tea.ModShift != 0 && m.selectedPID > 0 {
		m.confirmKill = true
		m.confirmPID = m.selectedPID
		return m, nil
	}

	switch m.activePane {
	case paneTimeline:
		m = m.handleTimelineKey(key)
	case paneHeatmap:
		m = m.handleHeatmapKey(key)
	}

	return m, nil
}

func (m dashboardModel) dashboardVisibleLines() int {
	v := max(m.height-7, 1)
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

	contentHeight := max(h-4, 3)

	treeWidth := min(max(w*40/100, 30), 60)
	rightWidth := max(w-treeWidth, 10)

	treePane := m.renderDashboardTreePane(treeWidth, contentHeight)

	topRightH := contentHeight / 2
	bottomRightH := contentHeight - topRightH
	timelinePane := m.renderTimelinePane(rightWidth, topRightH)
	heatmapPane := m.renderHeatmapPane(rightWidth, bottomRightH)

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
	if m.replayMode {
		return m.renderReplayStatus()
	}

	if m.confirmKill {
		return fmt.Sprintf("  Kill PID %d? [y/N]", m.confirmPID)
	}

	rec := ""
	if m.selectedPID > 0 && m.recording[m.selectedPID] != "" {
		rec = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("●REC") + "  "
	}

	ops := "  k:Kill  a:GDB  l:Log  r:Record"
	if m.statusMsg != "" {
		return fmt.Sprintf("  %s%s  |  q:Quit  Tab:Switch Pane%s", rec, m.statusMsg, ops)
	}
	if m.activePane == paneTimeline {
		return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  h/l:Scroll  +/-:Zoom  1-4:Filter%s", rec, ops)
	}
	if m.activePane == paneHeatmap {
		return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Select  Enter:Details%s", rec, ops)
	}
	return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Select%s", rec, ops)
}

func (m dashboardModel) renderDashboardTreePane(width, height int) string {
	isActive := m.activePane == paneTree

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

	var b strings.Builder
	b.WriteString(" Agent Tree \n")

	now := time.Now()
	visibleLines := max(innerH-1, 1)

	endIdx := min(m.treeOffset+visibleLines, len(m.treeRows))

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
		if m.recording[row.proc.PID] != "" {
			line += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("●")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

func renderDashboardPlaceholder(title, placeholder string, width, height int, active bool) string { //nolint:unused // reserved for future pane rendering
	borderColor := lipgloss.Color(ui.ColorMuted)
	if active {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

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
	m.stepEntries = nil
	m.stepCursor = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.lastFetchedStep = 0
	m.fetchingDetail = false
	return m
}

func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	if m.stepTimelineMode {
		return m.handleStepTimelineKey(key)
	}
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
	case "s":
		m.stepTimelineMode = true
	}
	return m
}

func (m dashboardModel) handleStepTimelineKey(key string) dashboardModel {
	switch key {
	case "up", "k":
		if m.stepCursor > 0 {
			m.stepCursor--
		}
	case "down", "j":
		if m.stepCursor < len(m.stepEntries)-1 {
			m.stepCursor++
		}
	case "s":
		m.stepTimelineMode = false
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

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	if m.stepTimelineMode {
		return style.Render(m.renderStepTimeline(innerW, innerH))
	}

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
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render("[" + label + "]"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(categoryColor(cat))).Render("[" + label + "]"))
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

	barWidth := max(innerW-2, 10)
	b.WriteString(m.renderTimelineBar(filtered, barWidth))
	b.WriteString("\n")

	listLines := max(innerH-3, 1)
	if m.timelineEventCursor >= len(filtered) {
		m.timelineEventCursor = len(filtered) - 1
	}

	startIdx := 0
	if m.timelineEventCursor >= listLines {
		startIdx = m.timelineEventCursor - listLines + 1
	}
	endIdx := min(startIdx+listLines, len(filtered))

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

func (m dashboardModel) renderStepTimeline(width, height int) string {
	var b strings.Builder

	b.WriteString(" Timeline")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
	}
	total := len(m.stepEntries)
	fmt.Fprintf(&b, " | %d/%d steps", total, total)
	b.WriteString("\n")

	if total == 0 {
		b.WriteString("\n    Waiting for steps...")
		return b.String()
	}

	listLines := max(height-2, 1)
	startIdx := 0
	if m.stepCursor >= listLines {
		startIdx = m.stepCursor - listLines + 1
	}
	endIdx := min(startIdx+listLines, total)

	for i := startIdx; i < endIdx; i++ {
		entry := m.stepEntries[i]
		s := entry.summary

		cursor := "  "
		if i == m.stepCursor {
			cursor = "▸ "
		}

		errMark := ""
		if s.HasError {
			errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
		}

		dur := formatTimelineDuration(s.DurationMs)
		line := fmt.Sprintf("%sStep %d/%d  %s  %s  %s%s",
			cursor, s.Step, total, s.Action, s.Summary, dur, errMark)
		b.WriteString(line)
		b.WriteString("\n")

		if entry.level >= levelExpanded {
			detail := m.stepDetailCache[s.Step]
			if detail != nil {
				if detail.ToolInput != "" {
					input := detail.ToolInput
					if utf8.RuneCountInString(input) > 60 {
						runes := []rune(input)
						input = string(runes[:57]) + "..."
					}
					fmt.Fprintf(&b, "      Input: %s\n", input)
				}
				if detail.ToolResult != "" {
					result := detail.ToolResult
					if utf8.RuneCountInString(result) > 60 {
						runes := []rune(result)
						result = string(runes[:57]) + "..."
					}
					fmt.Fprintf(&b, "      Result: %s\n", result)
				}
				if detail.ToolError != "" {
					fmt.Fprintf(&b, "      Error: %s\n",
						lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(detail.ToolError))
				}
				fmt.Fprintf(&b, "      Tokens: %d req / %d resp\n", detail.RequestTokens, detail.ResponseTokens)
			}
		}

		if entry.level >= levelDebug {
			detail := m.stepDetailCache[s.Step]
			if detail != nil {
				fmt.Fprintf(&b, "      Messages: %d  Tokens: %s\n",
					detail.MessageCount, formatTokenCount(detail.TokenCount))
				if len(detail.Messages) > 0 {
					preview := detail.Messages[0].Content
					if utf8.RuneCountInString(preview) > 60 {
						runes := []rune(preview)
						preview = string(runes[:57]) + "..."
					}
					fmt.Fprintf(&b, "      [%s] %s\n", detail.Messages[0].Role, preview)
				}
			}
		}
	}

	return b.String()
}

func formatTokenCount(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000.0)
	}
	return fmt.Sprintf("%d", tokens)
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
	for i := range width {
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

// --- Step timeline logic (Story 27-3) ---

func (m dashboardModel) applyNewSteps(steps []ipc.StepSummaryWire) dashboardModel {
	for _, s := range steps {
		level := levelSummary
		autoExpand := false
		if s.HasError || s.DurationMs > slowStepThresholdMs {
			level = levelExpanded
			autoExpand = true
		}
		m.stepEntries = append(m.stepEntries, stepEntry{
			summary:    s,
			level:      level,
			autoExpand: autoExpand,
		})
	}
	return m
}

// --- Heatmap logic (Story 17-3) ---

func segmentKindLabel(kind segmentKind) string {
	switch kind {
	case segSystem:
		return "System Prompt"
	case segSkill:
		return "Skill"
	case segTool:
		return "Tool Results"
	case segUser:
		return "User Messages"
	case segAssistant:
		return "Assistant"
	case segLeaked:
		return "Leaked"
	default:
		return "Unknown"
	}
}

func activityLabel(a activityLevel) string {
	switch a {
	case actActive:
		return "Active"
	case actWarm:
		return "Warm"
	case actCold:
		return "Cold"
	case actLeaked:
		return "Leaked"
	default:
		return "Unknown"
	}
}

func mapConsumerKind(kind string) segmentKind {
	switch {
	case kind == "system_prompt":
		return segSystem
	case kind == "user":
		return segUser
	case kind == "assistant":
		return segAssistant
	case strings.HasPrefix(kind, "tool:"):
		return segTool
	case kind == "skill" || strings.HasPrefix(kind, "skill:"):
		return segSkill
	default:
		return segAssistant
	}
}

func dim(hexColor string) string {
	switch hexColor {
	case "#5B9BD5":
		return "#3A6B94"
	case "#9B59B6":
		return "#6A3D7E"
	case "#6BCB77":
		return "#4A8C52"
	case "#FFD93D":
		return "#B3982B"
	case "#888888":
		return "#5E5E5E"
	default:
		return "#666666"
	}
}

func segmentColor(kind segmentKind, activity activityLevel) string {
	if activity == actLeaked || kind == segLeaked {
		return "#FF6B6B"
	}
	var base string
	switch kind {
	case segSystem:
		base = "#5B9BD5"
	case segSkill:
		base = colorIPC
	case segTool:
		base = "#6BCB77"
	case segUser:
		base = "#FFD93D"
	case segAssistant:
		base = "#888888"
	default:
		base = "#888888"
	}
	if activity == actCold {
		return dim(base)
	}
	return base
}

func estimateActivity(profile *debug.CtxProfileResult, kind segmentKind, rank int) activityLevel {
	if kind == segSystem {
		return actActive
	}
	if kind == segLeaked {
		return actLeaked
	}
	cl := profile.Classification
	if cl.Active.Pct > cl.Cold.Pct {
		if rank <= 2 {
			return actActive
		}
		return actWarm
	}
	if cl.Cold.Pct > cl.Active.Pct {
		return actCold
	}
	return actWarm
}

func buildHeatmapSegments(profile *debug.CtxProfileResult) []heatmapSegment {
	if profile == nil || len(profile.TopConsumers) == 0 {
		return nil
	}

	type kindBucket struct {
		tokens   int
		pct      float64
		kind     segmentKind
		activity activityLevel
		bestRank int
	}
	merged := make(map[segmentKind]*kindBucket)

	for _, c := range profile.TopConsumers {
		kind := mapConsumerKind(c.Kind)
		activity := estimateActivity(profile, kind, c.Rank)
		if b, ok := merged[kind]; ok {
			b.tokens += c.Tokens
			b.pct += c.Pct
			if c.Rank < b.bestRank {
				b.bestRank = c.Rank
				b.activity = activity
			}
		} else {
			merged[kind] = &kindBucket{
				tokens: c.Tokens, pct: c.Pct,
				kind: kind, activity: activity, bestRank: c.Rank,
			}
		}
	}

	var segments []heatmapSegment
	var otherTokens int
	var otherPct float64

	for _, b := range merged {
		if b.pct < 3.0 {
			otherTokens += b.tokens
			otherPct += b.pct
			continue
		}
		segments = append(segments, heatmapSegment{
			label:    segmentKindLabel(b.kind),
			tokens:   b.tokens,
			pct:      b.pct,
			kind:     b.kind,
			activity: b.activity,
		})
	}

	if profile.Classification.Leaked.Tokens > 0 {
		if profile.Classification.Leaked.Pct < 3.0 {
			otherTokens += profile.Classification.Leaked.Tokens
			otherPct += profile.Classification.Leaked.Pct
		} else {
			segments = append(segments, heatmapSegment{
				label:    "Leaked",
				tokens:   profile.Classification.Leaked.Tokens,
				pct:      profile.Classification.Leaked.Pct,
				kind:     segLeaked,
				activity: actLeaked,
			})
		}
	}

	if otherTokens > 0 {
		segments = append(segments, heatmapSegment{
			label:    "Other",
			tokens:   otherTokens,
			pct:      otherPct,
			kind:     segAssistant,
			activity: actCold,
		})
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].tokens > segments[j].tokens
	})

	return segments
}

func fetchHeatmapCmd(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return heatmapProfileMsg{err: err}
		}
		defer client.Close()
		profile, err := client.CtxProfile(pid)
		return heatmapProfileMsg{profile: profile, err: err}
	}
}

func fetchStepsCmd(pid types.PID, afterStep int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return stepListMsg{err: err}
		}
		defer client.Close()
		resp, err := client.ListSteps(pid, afterStep)
		if err != nil {
			return stepListMsg{err: err}
		}
		return stepListMsg{steps: resp.Steps, total: resp.Total}
	}
}

func fetchStepDetailCmd(pid types.PID, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return stepDetailResultMsg{step: step, err: err}
		}
		defer client.Close()
		detail, err := client.GetStepDetail(pid, step)
		return stepDetailResultMsg{step: step, detail: detail, err: err}
	}
}

func toggleRecordCmd(pid types.PID, currentRecordID string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return recordToggleMsg{pid: pid, err: err}
		}
		defer client.Close()

		if currentRecordID == "" {
			recordID, err := client.RecordStart(pid)
			return recordToggleMsg{pid: pid, recordID: recordID, err: err}
		}
		count, err := client.RecordStop(pid)
		return recordToggleMsg{pid: pid, stopped: true, eventCount: count, err: err}
	}
}

func (m dashboardModel) handlePIDChange() (dashboardModel, tea.Cmd) {
	if m.selectedPID == 0 {
		m = m.stopTimelineStream()
		m = m.handleTimelinePIDChange()
		m = m.handleHeatmapPIDChange()
		m.timelineAttachedPID = 0
		m.heatmapPID = 0
		return m, nil
	}
	m = m.stopTimelineStream()
	m = m.handleTimelinePIDChange()
	m = m.handleHeatmapPIDChange()
	m.timelineAttachedPID = m.selectedPID
	m.heatmapPID = m.selectedPID
	var cmds []tea.Cmd
	if m.connected {
		cmds = append(cmds, startTimelineCmd(m.selectedPID))
		cmds = append(cmds, fetchHeatmapCmd(m.selectedPID))
	}
	return m, tea.Batch(cmds...)
}

func (m dashboardModel) handleHeatmapPIDChange() dashboardModel {
	if m.selectedPID == m.heatmapPID {
		return m
	}
	m.heatmapProfile = nil
	m.heatmapSegments = nil
	m.heatmapCursor = 0
	m.heatmapExpanded = false
	m.heatmapErr = nil
	return m
}

func (m dashboardModel) handleHeatmapKey(key string) dashboardModel {
	switch key {
	case "up", "k":
		if m.heatmapCursor > 0 {
			m.heatmapCursor--
		}
	case "down", "j":
		if m.heatmapCursor < len(m.heatmapSegments)-1 {
			m.heatmapCursor++
		}
	case "enter":
		m.heatmapExpanded = !m.heatmapExpanded
	}
	return m
}

func (m dashboardModel) renderHeatmapPane(width, height int) string {
	isActive := m.activePane == paneHeatmap

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

	var b strings.Builder

	b.WriteString(" Heatmap")
	if m.selectedPID > 0 && m.heatmapProfile != nil {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
		pct := 0
		if m.heatmapProfile.ContextBudget > 0 {
			pct = m.heatmapProfile.TotalTokens * 100 / m.heatmapProfile.ContextBudget
		}
		fmt.Fprintf(&b, " | ~%d tok / %d budget (%d%%)",
			m.heatmapProfile.TotalTokens, m.heatmapProfile.ContextBudget, pct)
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select an agent to view heatmap")
		return style.Render(b.String())
	}
	if m.heatmapErr != nil {
		fmt.Fprintf(&b, "\n    ✗ %v", m.heatmapErr)
		return style.Render(b.String())
	}
	if len(m.heatmapSegments) == 0 {
		b.WriteString("\n    Loading context profile...")
		return style.Render(b.String())
	}

	barWidth := max(innerW-2, 10)
	for _, seg := range m.heatmapSegments {
		segW := max(int(seg.pct/100.0*float64(barWidth)), 1)
		color := segmentColor(seg.kind, seg.activity)
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		b.WriteString(catStyle.Render(strings.Repeat("█", segW)))
	}
	b.WriteString("\n")

	for _, seg := range m.heatmapSegments {
		fmt.Fprintf(&b, "%s(%.0f%%) ", seg.label, seg.pct)
	}
	b.WriteString("\n")

	for i, seg := range m.heatmapSegments {
		cursor := "  "
		if i == m.heatmapCursor {
			cursor = "▸ "
		}
		actStr := activityLabel(seg.activity)
		fmt.Fprintf(&b, "%s%-15s %4d tok  %5.1f%%  %s\n",
			cursor, seg.label, seg.tokens, seg.pct, actStr)
	}

	if m.heatmapExpanded && m.heatmapCursor < len(m.heatmapSegments) {
		seg := m.heatmapSegments[m.heatmapCursor]
		actStr := activityLabel(seg.activity)
		fmt.Fprintf(&b, "\n── Selected: %s ──\n", seg.label)
		fmt.Fprintf(&b, "%d tokens | %.1f%% | %s\n", seg.tokens, seg.pct, actStr)
		if seg.summary != "" {
			if utf8.RuneCountInString(seg.summary) > 60 {
				runes := []rune(seg.summary)
				b.WriteString("\"" + string(runes[:57]) + "...\"\n")
			} else {
				b.WriteString("\"" + seg.summary + "\"\n")
			}
		}
	}

	return style.Render(b.String())
}

// --- Offline replay (Story 17-5) ---

func newReplayDashboardModel(reader *debug.RecordReader) dashboardModel {
	return dashboardModel{
		startTime:        time.Now(),
		connected:        false,
		timelineFilters:  defaultTimelineFilters(),
		recording:        make(map[types.PID]string),
		replayMode:       true,
		replayReader:     reader,
		replayCursor:     -1,
		replayPlaying:    false,
		replaySpeed:      1.0,
		prevReplayCursor: -2,
	}
}

func recordEventToWire(ev debug.RecordEvent) ipc.SyscallEventWire {
	if ev.Type != debug.RecordSyscall || ev.Syscall == nil {
		return ipc.SyscallEventWire{}
	}
	return ipc.SyscallEventWire{
		TimestampMs: ev.Timestamp.Milliseconds(),
		PID:         ev.PID,
		Syscall:     ev.Syscall.Syscall,
		Args:        ev.Syscall.Args,
		Result:      ev.Syscall.Result,
		Error:       ev.Syscall.Err,
		DurationMs:  float64(ev.Syscall.Duration.Milliseconds()),
	}
}

func buildReplayProcessTree(reader *debug.RecordReader, cursor int) []vfs.ProcInfo {
	meta := reader.Metadata()
	state := types.StateRunning
	var tokensUsed int

	events := reader.Events()
	for _, ev := range events {
		if int(ev.SeqNum) > cursor {
			break
		}
		if ev.Type == debug.RecordStateChange && ev.State != nil {
			switch ev.State.ToState {
			case "zombie", "dead":
				state = types.StateZombie
			case "running":
				state = types.StateRunning
			}
		}
		if ev.Type == debug.RecordContextSnapshot && ev.Context != nil {
			// TokenEstimate removed in Story 27.1 AC-9; token tracking via StepRecord now
			_ = ev.Context
		}
	}

	return []vfs.ProcInfo{{
		PID:        meta.PID,
		PPID:       1,
		State:      state,
		Intent:     meta.Intent,
		TokensUsed: tokensUsed,
		CreatedAt:  meta.StartTime,
	}}
}

func loadReplayTimeline(reader *debug.RecordReader, cursor int) []timelineEvent {
	if cursor < 0 {
		return nil
	}
	var result []timelineEvent
	for _, ev := range reader.Events() {
		if int(ev.SeqNum) > cursor {
			break
		}
		if ev.Type != debug.RecordSyscall || ev.Syscall == nil {
			continue
		}
		wire := recordEventToWire(ev)
		result = append(result, timelineEvent{
			wire:     wire,
			category: classifySyscall(wire),
		})
	}
	return result
}

func buildReplayHeatmap(reader *debug.RecordReader, cursor int) *debug.CtxProfileResult {
	events := reader.Events()
	var snap *debug.ContextSnapshotData
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if int(ev.SeqNum) > cursor {
			continue
		}
		if ev.Type == debug.RecordContextSnapshot && ev.Context != nil {
			snap = ev.Context
			break
		}
	}
	if snap == nil {
		return nil
	}

	totalTokens := 0 // TokenEstimate removed in Story 27.1 AC-9
	var consumers []debug.ConsumerEntry
	kindTokens := map[string]int{}

	for _, msg := range snap.Messages {
		kind := "assistant"
		switch {
		case strings.HasPrefix(msg, "[system]"):
			kind = "system_prompt"
		case strings.HasPrefix(msg, "[skill]"):
			kind = "skill"
		case strings.HasPrefix(msg, "[tool]"):
			kind = "tool:result"
		case strings.HasPrefix(msg, "[user]"):
			kind = "user"
		case strings.HasPrefix(msg, "[assistant]"):
			kind = "assistant"
		}
		est := totalTokens / max(len(snap.Messages), 1)
		kindTokens[kind] += est
	}

	rank := 1
	for kind, tokens := range kindTokens {
		pct := 0.0
		if totalTokens > 0 {
			pct = float64(tokens) * 100.0 / float64(totalTokens)
		}
		consumers = append(consumers, debug.ConsumerEntry{
			Kind:   kind,
			Tokens: tokens,
			Pct:    pct,
			Rank:   rank,
		})
		rank++
	}

	sort.Slice(consumers, func(i, j int) bool {
		return consumers[i].Tokens > consumers[j].Tokens
	})
	for i := range consumers {
		consumers[i].Rank = i + 1
	}

	return &debug.CtxProfileResult{
		PID:          reader.Metadata().PID,
		TotalTokens:  totalTokens,
		TopConsumers: consumers,
		Classification: debug.ClassificationResult{
			Active: debug.ClassBucket{
				Tokens:   totalTokens,
				Pct:      100.0,
				Messages: len(snap.Messages),
			},
		},
	}
}

func resolveRecordDir(loadArg string) (string, error) {
	if info, err := os.Stat(loadArg); err == nil && info.IsDir() {
		if _, err := os.Stat(filepath.Join(loadArg, "metadata.json")); err == nil {
			return loadArg, nil
		}
		return "", fmt.Errorf("directory %s does not contain metadata.json", loadArg)
	}
	mgr := debug.NewRecordManager(findRecordBaseDir())
	dir, err := mgr.FindRecord(loadArg)
	if err != nil {
		return "", fmt.Errorf("recording %q not found: %w", loadArg, err)
	}
	return dir, nil
}

func (m dashboardModel) replayTick() (tea.Model, tea.Cmd) {
	if m.replayPlaying && m.replayReader != nil && m.replayCursor < m.replayReader.EventCount()-1 {
		advance := int(m.replaySpeed)
		if m.replaySpeed < 1.0 {
			if m.heatmapTickCount%int(1.0/m.replaySpeed) == 0 {
				advance = 1
			} else {
				advance = 0
			}
		}
		m.replayCursor = min(m.replayCursor+advance, m.replayReader.EventCount()-1)
		if m.replayCursor >= m.replayReader.EventCount()-1 {
			m.replayPlaying = false
		}
	}

	if m.replayCursor != m.prevReplayCursor && m.replayReader != nil {
		m.processes = buildReplayProcessTree(m.replayReader, m.replayCursor)
		roots := buildProcessTree(m.processes)
		m.treeRows = flattenTree(roots)
		if len(m.treeRows) > 0 {
			m.selectedPID = m.treeRows[0].proc.PID
		}
		m.timelineEvents = loadReplayTimeline(m.replayReader, m.replayCursor)
		m.heatmapProfile = buildReplayHeatmap(m.replayReader, m.replayCursor)
		if m.heatmapProfile != nil {
			m.heatmapSegments = buildHeatmapSegments(m.heatmapProfile)
		} else {
			m.heatmapSegments = nil
		}
		m.prevReplayCursor = m.replayCursor
	}

	return m, tickCmd()
}

func (m dashboardModel) handleReplayKey(key string) (dashboardModel, tea.Cmd) {
	switch key {
	case " ", "space":
		m.replayPlaying = !m.replayPlaying
	case "[":
		if m.replaySpeed > 0.5 {
			m.replaySpeed /= 2.0
		}
	case "]":
		if m.replaySpeed < 8.0 {
			m.replaySpeed *= 2.0
		}
	case ".":
		if m.replayReader != nil && m.replayCursor < m.replayReader.EventCount()-1 {
			m.replayCursor++
		}
		m.replayPlaying = false
	case ",":
		if m.replayCursor > 0 {
			m.replayCursor--
		}
		m.replayPlaying = false
	case "0":
		m.replayCursor = 0
		m.replayPlaying = false
	case "$":
		if m.replayReader != nil {
			m.replayCursor = m.replayReader.EventCount() - 1
		}
		m.replayPlaying = false
	case "k":
		switch m.activePane {
		case paneTree:
			if m.treeCursor > 0 {
				m.treeCursor--
				if m.treeCursor < len(m.treeRows) {
					m.selectedPID = m.treeRows[m.treeCursor].proc.PID
				}
				if m.treeCursor < m.treeOffset {
					m.treeOffset = m.treeCursor
				}
			}
		case paneTimeline:
			m = m.handleTimelineKey(key)
		case paneHeatmap:
			m = m.handleHeatmapKey(key)
		}
	case "l":
		if m.activePane == paneTimeline {
			m = m.handleTimelineKey(key)
		} else {
			m.statusMsg = "Not available in replay mode"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	case "a", "r":
		m.statusMsg = "Not available in replay mode"
		m.statusMsgTTL = statusMsgDefaultTTL
	default:
		switch m.activePane {
		case paneTimeline:
			m = m.handleTimelineKey(key)
		case paneHeatmap:
			m = m.handleHeatmapKey(key)
		case paneTree:
			visibleLines := m.dashboardVisibleLines()
			switch key {
			case "up":
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
			}
		}
	}
	return m, nil
}

func (m dashboardModel) renderReplayStatus() string {
	indicator := "⏸"
	if m.replayPlaying {
		indicator = "▶"
	}
	recordID := ""
	if m.replayReader != nil {
		recordID = m.replayReader.Metadata().RecordID
	}
	total := 0
	if m.replayReader != nil {
		total = m.replayReader.EventCount()
	}
	progress := fmt.Sprintf("[%d/%d] %.1f×", m.replayCursor, total, m.replaySpeed)

	if m.statusMsg != "" {
		return fmt.Sprintf("  %s REPLAY: %s  %s  |  %s", indicator, recordID, progress, m.statusMsg)
	}
	return fmt.Sprintf("  %s REPLAY: %s  %s  |  Space:Play/Pause  [/]:Speed  ,/.:Step  0:Start  $:End  q:Quit",
		indicator, recordID, progress)
}

// --- Command runner ---

func runDashboard(cmd *cobra.Command, _ []string) error {
	profile := ui.DetectProfile(os.Stdout)
	ui.InitStyles(profile)

	var loadArg string
	if cmd != nil {
		loadArg, _ = cmd.Flags().GetString("load")
	}
	if loadArg != "" {
		recordDir, err := resolveRecordDir(loadArg)
		if err != nil {
			return fmt.Errorf("--load: %w", err)
		}
		reader, err := debug.NewRecordReader(recordDir)
		if err != nil {
			return fmt.Errorf("--load: %w", err)
		}
		model := newReplayDashboardModel(reader)
		p := tea.NewProgram(model)
		_, err = p.Run()
		if err != nil {
			return fmt.Errorf("dashboard: %w", err)
		}
		return nil
	}

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), "✗ No rnix daemon running. Start an agent first with: rnix -i \"intent\"")
		return nil
	}

	model := newDashboardModel(client)
	if cmd != nil {
		if focusPID, _ := cmd.Flags().GetInt("pid"); focusPID > 0 {
			model.selectedPID = types.PID(focusPID)
		}
	}
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
