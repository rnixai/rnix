package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Visual debugging dashboard with multi-pane view",
	Long:  "Interactive TUI dashboard showing agent tree, timeline, and heatmap in a multi-pane layout.",
	Args:  cobra.NoArgs,
	RunE:  runDashboard,
}

type dashboardModel struct {
	client      *ipc.Client
	width       int
	height      int
	activePane   paneType
	viewMode     viewMode  // 当前视图模式（默认 viewDefault，零值即默认）
	expandedPane paneType  // viewExpanded 模式下展开的面板
	selectedPID  types.PID
	selectedUUID string
	processes    []vfs.ProcInfo
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
	recording    map[string]string
	statusMsgTTL int

	// Step timeline fields (Story 27-3)
	stepTimelineMode bool
	stepEntries      []stepEntry
	stepCursor       int
	stepDetailCache  map[int]*ipc.GetStepDetailResponse
	lastFetchedStep  int
	fetchingDetail   bool

	// Prompt pager fields (Story 27-4)
	promptPager    bool
	promptViewport viewport.Model
	promptContent  string
	promptStep     int

	// Story 27-5: initial PID focus from --pid flag
	initialPIDFocus types.PID

	// Detail pane fields (Story 27-6)
	procDetail      *ipc.GetProcDetailResponse
	procDetailPID   types.PID
	procDetailCache map[string]*ipc.GetProcDetailResponse
	procDetailTick  int // tick counter for periodic cache refresh

	// Intent tree pane fields (Story 27-7)
	intentTrees        []*ipc.IntentTreeWire
	intentTreeErr      error
	intentFlatNodes    []intentFlatNode
	intentCursor       int
	intentScrollOffset int

	// Security pane fields (Story 27-8)
	immuneStatus   *ipc.ImmuneStatusResponse
	immuneErr      error
	securityAlerts       []ipc.AlertWire
	securityCursor       int
	securityScrollOffset int

	// Trace pane fields (Story 27-9)
	traceSummaries    []ipc.TraceSummaryWire
	traceErr          error
	traceCursor       int
	traceScrollOffset int
	traceViewMode     int // 0=list, 1=tree
	selectedTraceID   string
	selectedSpanTree  *ipc.SpanTreeWire
	spanFlatNodes     []spanFlatNode
	spanCursor        int
	spanScrollOffset  int

	// Eval pane fields (Story 27-10)
	evalSubView        int // 0=reputation, 1=topology, 2=synergy
	evalReputations    []kernel.ReputationSummary
	evalRepErr         error
	evalRepCursor      int
	evalRepScrollOffset int
	evalTopology       *ipc.TopologyQueryResponse
	evalTopoErr        error
	evalTopoCursor     int
	evalTopoScrollOffset int
	evalSynergies      []kernel.ComboSummary
	evalSynErr         error
	evalSynCursor      int
	evalSynScrollOffset int

	// Offline replay fields (Story 17-5)
	replayMode       bool
	replayReader     *debug.RecordReader
	replayCursor     int
	replayPlaying    bool
	replaySpeed      float64
	prevReplayCursor int

	// Focus Card fields (Story 29-3)
	focusCardData *focusCardState

	// LLM Viewer fields (Story 29-6)
	llmViewerPID      types.PID
	llmViewerUUID     string
	llmViewerStep     int
	llmViewerStepMax  int
	llmViewerSteps    []ipc.StepSummaryWire
	llmViewerDetail   *ipc.GetStepDetailResponse
	llmViewerViewport viewport.Model
	llmViewerContent  string
	llmViewerPrevMode viewMode

	// History view fields (Story 29-5)
	historyProcs        []vfs.ProcInfo
	historyCursor       int
	historyScrollOffset int
	historySortMode     int // 0=time, 1=name, 2=pid
	historySearchQuery  string
	historySearchMode   bool
}

func newDashboardModel(client *ipc.Client) dashboardModel {
	return dashboardModel{
		client:           client,
		startTime:        time.Now(),
		connected:        client != nil,
		timelineFilters:  defaultTimelineFilters(),
		recording:        make(map[string]string),
		stepTimelineMode: true,
		stepDetailCache:  make(map[int]*ipc.GetStepDetailResponse),
		procDetailCache:  make(map[string]*ipc.GetProcDetailResponse),
	}
}

func selectProcess(m dashboardModel, row flatRow) dashboardModel {
	m.selectedPID = row.proc.PID
	m.selectedUUID = row.proc.UUID
	return m
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
		if m.promptPager {
			m.promptViewport.SetWidth(msg.Width)
			m.promptViewport.SetHeight(max(msg.Height-2, 1))
		}
		if m.viewMode == viewLLM {
			m.llmViewerViewport.SetWidth(msg.Width)
			m.llmViewerViewport.SetHeight(max(msg.Height-4, 1))
		}
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
			delete(m.recording, msg.uuid)
			m.statusMsg = fmt.Sprintf("Recording stopped (%d events)", msg.eventCount)
		} else {
			m.recording[msg.uuid] = msg.recordID
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
	case procDetailResultMsg:
		if msg.err == nil && msg.detail != nil {
			m.procDetailCache[msg.uuid] = msg.detail
			if msg.pid == m.selectedPID && msg.uuid == m.selectedUUID {
				m.procDetail = msg.detail
				m.procDetailPID = msg.pid
			}
		}
		return m, nil
	case intentTreesMsg:
		if msg.err != nil {
			m.intentTreeErr = msg.err
			return m, nil
		}
		m.intentTreeErr = nil
		if msg.trees == nil {
			m.intentTrees = nil
			m.intentFlatNodes = nil
			return m, nil
		}
		m.intentTrees = msg.trees.Intents
		m.intentFlatNodes = flattenIntentTrees(m.intentTrees)
		if m.intentCursor >= len(m.intentFlatNodes) {
			m.intentCursor = max(0, len(m.intentFlatNodes)-1)
		}
		return m, nil
	case immuneStatusMsg:
		if msg.err != nil {
			m.immuneErr = msg.err
			return m, nil
		}
		m.immuneErr = nil
		m.immuneStatus = msg.status
		if msg.status != nil {
			m.securityAlerts = sortAlertsByDeviation(msg.status.Alerts)
			if m.securityCursor >= len(m.securityAlerts) {
				m.securityCursor = max(0, len(m.securityAlerts)-1)
			}
		}
		return m, nil
	case traceListMsg:
		if msg.err != nil {
			m.traceErr = msg.err
			return m, nil
		}
		m.traceErr = nil
		m.traceSummaries = msg.summaries
		// Sort by StartTimeMs descending (newest first)
		sort.Slice(m.traceSummaries, func(i, j int) bool {
			return m.traceSummaries[i].StartTimeMs > m.traceSummaries[j].StartTimeMs
		})
		if m.traceCursor >= len(m.traceSummaries) {
			m.traceCursor = max(0, len(m.traceSummaries)-1)
		}
		return m, nil
	case traceTreeMsg:
		if msg.err != nil {
			m.traceErr = msg.err
			return m, nil
		}
		m.traceErr = nil
		m.selectedTraceID = msg.traceID
		m.selectedSpanTree = msg.tree
		m.spanFlatNodes = flattenSpanTree(msg.tree)
		m.spanCursor = 0
		m.spanScrollOffset = 0
		if msg.tree == nil || msg.tree.Root == nil {
			// Empty trace — stay in list mode, show status message
			m.statusMsg = "此追踪无 span 数据"
			m.statusMsgTTL = statusMsgDefaultTTL
		} else {
			m.traceViewMode = 1
		}
		return m, nil
	case evalReputationMsg:
		if msg.err != nil {
			m.evalRepErr = msg.err
			return m, nil
		}
		m.evalRepErr = nil
		m.evalReputations = msg.summaries
		// Sort by score descending
		sort.Slice(m.evalReputations, func(i, j int) bool {
			return m.evalReputations[i].Score > m.evalReputations[j].Score
		})
		if m.evalRepCursor >= len(m.evalReputations) {
			m.evalRepCursor = max(0, len(m.evalReputations)-1)
		}
		return m, nil
	case evalTopologyMsg:
		if msg.err != nil {
			m.evalTopoErr = msg.err
			return m, nil
		}
		m.evalTopoErr = nil
		m.evalTopology = msg.topology
		if msg.topology != nil {
			totalItems := len(msg.topology.Nodes) + len(msg.topology.Edges)
			if m.evalTopoCursor >= totalItems {
				m.evalTopoCursor = max(0, totalItems-1)
			}
		}
		return m, nil
	case evalSynergyMsg:
		if msg.err != nil {
			m.evalSynErr = msg.err
			return m, nil
		}
		m.evalSynErr = nil
		m.evalSynergies = msg.combos
		if m.evalSynCursor >= len(m.evalSynergies) {
			m.evalSynCursor = max(0, len(m.evalSynergies)-1)
		}
		return m, nil
	case promptPagerMsg:
		m.fetchingDetail = false
		if msg.pid != m.selectedPID {
			return m, nil
		}
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ prompt load: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil
		}
		if msg.detail != nil {
			m.stepDetailCache[msg.step] = msg.detail
			m.enterPromptPager(msg.detail, msg.step)
		}
		return m, nil
	case llmViewerMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ LLM viewer: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
		} else if msg.detail != nil && msg.pid == m.llmViewerPID {
			m.llmViewerDetail = msg.detail
			m.llmViewerStep = msg.step
			content := m.buildLLMViewerContent()
			m.llmViewerContent = content
			m.llmViewerViewport.SetContent(content)
		}
		return m, nil
	case llmStepListMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ LLM steps: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
		} else if len(msg.steps) > 0 && msg.pid == m.llmViewerPID {
			m.llmViewerSteps = msg.steps
			m.llmViewerStepMax = msg.steps[len(msg.steps)-1].Step
		}
		return m, nil
	case historyProcsMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ history: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil
		}
		m.historyProcs = msg.procs
		m.sortHistoryProcs()
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

	procs, err := m.client.ListAllProcs()
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

	m.applyInitialPIDFocus()

	// Validate selected process UUID consistency BEFORE updating selection (AC-2, AC-3)
	if m.selectedPID > 0 {
		found := false
		for _, p := range m.processes {
			if p.PID == m.selectedPID {
				if m.selectedUUID == "" || p.UUID == m.selectedUUID {
					found = true // PID exists and UUID matches (or UUID empty for backward compat)
				}
				// PID match but UUID mismatch → PID reuse detected
				break
			}
		}
		if !found {
			m.selectedPID = 0
			m.selectedUUID = ""
		}
	}

	// Update selection from current cursor position (after validation)
	if m.treeCursor < len(m.treeRows) {
		m = selectProcess(m, m.treeRows[m.treeCursor])
	}

	// Aggregate Focus Card data from cached fields (Story 29-3)
	m.aggregateFocusCard()

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

	// Fetch proc detail when Detail pane is active or in default view (Focus Card needs it)
	focusCardNeedsData := m.viewMode == viewDefault
	if (m.activePane == paneDetail || focusCardNeedsData) && m.selectedPID > 0 && m.connected {
		m.procDetailTick++
		needsFetch := m.procDetail == nil || m.procDetailPID != m.selectedPID
		// Refresh every 5 ticks (~5s) for live processes
		if !needsFetch && m.procDetail != nil && m.procDetail.State != "dead" && m.procDetailTick%5 == 0 {
			delete(m.procDetailCache, m.selectedUUID)
			needsFetch = true
		}
		if needsFetch {
			// Check cache first (only for initial load, not periodic refresh)
			if cached, ok := m.procDetailCache[m.selectedUUID]; ok {
				m.procDetail = cached
				m.procDetailPID = m.selectedPID
			} else {
				cmds = append(cmds, fetchProcDetailCmd(m.selectedPID, m.selectedUUID))
			}
		}
	}

	// Fetch intent trees when Intent pane is active or in default view (Focus Card)
	if (m.activePane == paneIntent || focusCardNeedsData) && m.connected {
		if m.intentTrees == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchIntentTreesCmd())
		}
	}

	// Fetch immune status when Security pane is active or in default view (Focus Card)
	if (m.activePane == paneSecurity || focusCardNeedsData) && m.connected {
		if m.immuneStatus == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchImmuneStatusCmd())
		}
	}

	// Fetch trace list when Trace pane is active or in default view (Focus Card)
	if (m.activePane == paneTrace || focusCardNeedsData) && m.connected {
		if m.traceSummaries == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchTraceListCmd())
		}
	}

	// Fetch eval data when Eval pane is active (Story 27-10)
	if m.activePane == paneEval && m.connected {
		if m.evalReputations == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchReputationCmd())
		}
		if m.evalSubView == 1 && (m.evalTopology == nil || m.heatmapTickCount%5 == 0) {
			cmds = append(cmds, fetchTopologyCmd())
		}
		if m.evalSubView == 2 && (m.evalSynergies == nil || m.heatmapTickCount%5 == 0) {
			cmds = append(cmds, fetchSynergyCmd())
		}
	}

	if len(m.recording) > 0 {
		activeUUIDs := make(map[string]bool, len(m.processes))
		for _, p := range m.processes {
			if p.State != types.StateDead {
				activeUUIDs[p.UUID] = true
			}
		}
		for uuid := range m.recording {
			if !activeUUIDs[uuid] {
				delete(m.recording, uuid)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *dashboardModel) applyInitialPIDFocus() {
	if m.initialPIDFocus <= 0 || len(m.treeRows) == 0 {
		return
	}
	found := false
	for i, row := range m.treeRows {
		if row.proc.PID == m.initialPIDFocus {
			m.treeCursor = i
			visibleLines := m.dashboardVisibleLines()
			if visibleLines > 0 && m.treeCursor >= m.treeOffset+visibleLines {
				m.treeOffset = m.treeCursor - visibleLines/2
			}
			found = true
			break
		}
	}
	if !found {
		m.statusMsg = fmt.Sprintf("⚠ PID %d not found, showing all processes", m.initialPIDFocus)
		m.statusMsgTTL = 10
	}
	m.initialPIDFocus = 0
}


func (m dashboardModel) dashboardVisibleLines() int {
	v := max(m.height-7, 1)
	return v
}

func (m dashboardModel) View() tea.View {
	var content string
	if m.promptPager {
		content = m.renderPromptPager()
	} else {
		content = m.renderDashboard()
	}
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
	if m.promptPager {
		return m.renderPromptPager()
	}

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

	var mainContent string
	switch m.viewMode {
	case viewExpanded:
		mainContent = m.renderExpandedLayout(w, contentHeight)
	case viewHistory:
		mainContent = m.renderHistoryView(w, contentHeight)
	case viewLLM:
		mainContent = m.renderLLMViewer(w, contentHeight)
	default: // viewDefault
		mainContent = m.renderDefaultLayout(w, contentHeight)
	}

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, mainContent, statusBar)
}

func (m dashboardModel) renderDefaultLayout(w, h int) string {
	treeWidth := min(max(w*40/100, 30), 60)
	rightWidth := max(w-treeWidth, 10)

	treePane := m.renderDashboardTreePane(treeWidth, h)

	topRightH := h / 2
	bottomRightH := h - topRightH
	timelinePane := m.renderTimelinePane(rightWidth, topRightH)
	focusCard := m.renderFocusCard(rightWidth, bottomRightH)

	rightPane := lipgloss.JoinVertical(lipgloss.Left, timelinePane, focusCard)
	return lipgloss.JoinHorizontal(lipgloss.Top, treePane, rightPane)
}

func (m dashboardModel) renderExpandedLayout(w, h int) string {
	if m.expandedPane == paneTree {
		return m.renderDashboardTreePane(w, h)
	}
	treeWidth := min(max(w*40/100, 30), 60)
	rightWidth := max(w-treeWidth, 10)
	treePane := m.renderDashboardTreePane(treeWidth, h)

	var expandedPane string
	switch m.expandedPane {
	case paneTimeline:
		expandedPane = m.renderTimelinePane(rightWidth, h)
	case paneHeatmap:
		expandedPane = m.renderHeatmapPane(rightWidth, h)
	case paneDetail:
		expandedPane = m.renderDetailPane(rightWidth, h)
	case paneIntent:
		expandedPane = m.renderIntentPane(rightWidth, h)
	case paneSecurity:
		expandedPane = m.renderSecurityPane(rightWidth, h)
	case paneTrace:
		expandedPane = m.renderTracePane(rightWidth, h)
	case paneEval:
		expandedPane = m.renderEvalPane(rightWidth, h)
	default:
		expandedPane = m.renderHeatmapPane(rightWidth, h)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, treePane, expandedPane)
}

func (m dashboardModel) renderDashboardTitle() string {
	var b strings.Builder
	b.WriteString("  Rnix Dashboard")
	if m.connected {
		b.WriteString(" ●")
	} else {
		b.WriteString(" ○")
	}
	b.WriteString(" ──")

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

	for _, p := range panes {
		if m.viewMode == viewExpanded && m.expandedPane == p.pane {
			fmt.Fprintf(&b, " %s:%s*", p.key, p.name)
		} else {
			fmt.Fprintf(&b, " [%s]%s", p.key, p.name)
		}
	}

	active := 0
	totalTokens := 0
	for _, p := range m.processes {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			active++
			totalTokens += p.TokensUsed
		}
	}
	if active > 0 || totalTokens > 0 {
		fmt.Fprintf(&b, " │ %d proc %s tok", active, ui.FormatTokens(totalTokens))
	}

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
	if m.selectedPID > 0 && m.recording[m.selectedUUID] != "" {
		rec = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("●REC") + "  "
	}

	ops := "  k:Kill a:GDB l:Log r:Rec"

	if m.statusMsg != "" {
		return fmt.Sprintf("  %s%s  |  q:Quit  Tab:Switch%s", rec, m.statusMsg, ops)
	}

	var hints string
	switch m.viewMode {
	case viewExpanded:
		hints = m.paneSpecificHints()
	case viewHistory:
		if m.historySearchMode {
			hints = "Type to search | Enter:confirm Esc:cancel"
		} else {
			hints = "j/k:nav Enter:focus L:llm /:search 1/2/3:sort Esc:back q:quit"
		}
	case viewLLM:
		d := m.llmViewerDetail
		reqTok := 0
		respTok := 0
		durMs := 0.0
		if d != nil {
			reqTok = d.RequestTokens
			respTok = d.ResponseTokens
			durMs = d.ToolDurationMs
		}
		hints = fmt.Sprintf("req:%d tok │ resp:%d tok │ %.0fms ── j/k:scroll h/l:prev/next y:copy Esc:close",
			reqTok, respTok, durMs)
	default: // viewDefault
		hints = "Tab:cycle 1-8:jump L:llm H:hist Esc:back q:quit"
	}

	// AC-10: Append "(filtered: PID N)" when a Dead process is selected in Timeline context
	if m.viewMode != viewHistory && m.isSelectedProcessDead() && m.selectedPID > 0 {
		hints += fmt.Sprintf("  (filtered: PID %d)", m.selectedPID)
	}

	return fmt.Sprintf("  %s%s%s", rec, hints, ops)
}

func (m dashboardModel) paneSpecificHints() string {
	base := "1-8:jump Esc:back q:quit"
	switch m.expandedPane {
	case paneTimeline:
		if m.stepTimelineMode && len(m.stepEntries) > 0 {
			return "j/k:nav v:expand p:prompt h/l:scroll +/-:zoom " + base
		}
		return "j/k:nav h/l:scroll +/-:zoom " + base
	case paneHeatmap:
		return "j/k:select Enter:details " + base
	case paneDetail:
		return "Detail " + base
	case paneIntent:
		return "j/k:nav Enter:jump " + base
	case paneSecurity:
		return "j/k:nav Enter:jump " + base
	case paneTrace:
		if m.traceViewMode == 0 {
			return "j/k:nav Enter:expand " + base
		}
		return "j/k:nav Enter:jump Esc:back " + base
	case paneEval:
		return "j/k:nav 1/2/3:sub-view " + base
	default:
		return "j/k:nav Enter:select " + base
	}
}


func toggleRecordCmd(pid types.PID, uuid string, currentRecordID string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return recordToggleMsg{pid: pid, uuid: uuid, err: err}
		}
		defer client.Close()

		if currentRecordID == "" {
			recordID, err := client.RecordStart(pid)
			return recordToggleMsg{pid: pid, uuid: uuid, recordID: recordID, err: err}
		}
		count, err := client.RecordStop(pid)
		return recordToggleMsg{pid: pid, uuid: uuid, stopped: true, eventCount: count, err: err}
	}
}

func (m dashboardModel) handlePIDChange() (dashboardModel, tea.Cmd) {
	if m.selectedPID == 0 {
		m.selectedUUID = ""
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

	// Detail pane: check cache or reset
	if cached, ok := m.procDetailCache[m.selectedUUID]; ok {
		m.procDetail = cached
		m.procDetailPID = m.selectedPID
	} else {
		m.procDetail = nil
		m.procDetailPID = 0
	}

	var cmds []tea.Cmd
	if m.connected {
		cmds = append(cmds, startTimelineCmd(m.selectedPID))
		cmds = append(cmds, fetchHeatmapCmd(m.selectedPID))
		if m.activePane == paneDetail && m.procDetail == nil {
			cmds = append(cmds, fetchProcDetailCmd(m.selectedPID, m.selectedUUID))
		}
	}
	return m, tea.Batch(cmds...)
}


// --- Offline replay (Story 17-5) ---

func newReplayDashboardModel(reader *debug.RecordReader) dashboardModel {
	return dashboardModel{
		startTime:        time.Now(),
		connected:        false,
		timelineFilters:  defaultTimelineFilters(),
		recording:        make(map[string]string),
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
			m = selectProcess(m, m.treeRows[0])
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
					m = selectProcess(m, m.treeRows[m.treeCursor])
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
						m = selectProcess(m, m.treeRows[m.treeCursor])
					}
					if m.treeCursor < m.treeOffset {
						m.treeOffset = m.treeCursor
					}
				}
			case "down", "j":
				if m.treeCursor < len(m.treeRows)-1 {
					m.treeCursor++
					if m.treeCursor < len(m.treeRows) {
						m = selectProcess(m, m.treeRows[m.treeCursor])
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
			model.initialPIDFocus = types.PID(focusPID)
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

