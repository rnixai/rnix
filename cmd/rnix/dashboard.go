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

type paneType int

const (
	paneTree paneType = iota
	paneTimeline
	paneHeatmap
	paneDetail
	paneIntent
	paneSecurity
	paneTrace
	paneEval
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

type procDetailResultMsg struct {
	pid    types.PID
	uuid   string
	detail *ipc.GetProcDetailResponse
	err    error
}

// --- Intent tree types (Story 27-7) ---

type intentFlatNode struct {
	treeIndex    int
	nodeID       string
	indent       int
	node         *ipc.IntentNodeWire
	isTreeHeader bool
	isCollapsed  bool // terminal tree shown collapsed
	treeWire     *ipc.IntentTreeWire
}

type intentTreesMsg struct {
	trees *ipc.IntentStatusResponse
	err   error
}

// --- Security pane types (Story 27-8) ---

type immuneStatusMsg struct {
	status *ipc.ImmuneStatusResponse
	err    error
}

// --- Trace pane types (Story 27-9) ---

type traceListMsg struct {
	summaries []ipc.TraceSummaryWire
	err       error
}

type traceTreeMsg struct {
	traceID string
	tree    *ipc.SpanTreeWire
	err     error
}

type spanFlatNode struct {
	spanID string
	pid    types.PID
	name   string
	durMs  int64
	tokens int
	status string
	depth  int
	prefix string
	isRoot bool
}

// --- Eval pane types (Story 27-10) ---

type evalReputationMsg struct {
	summaries []kernel.ReputationSummary
	err       error
}

type evalTopologyMsg struct {
	topology *ipc.TopologyQueryResponse
	err      error
}

type evalSynergyMsg struct {
	combos []kernel.ComboSummary
	err    error
}

type promptPagerMsg struct {
	pid    types.PID
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
	uuid       string
	recordID   string
	stopped    bool
	eventCount uint64
	err        error
}

type dashboardModel struct {
	client      *ipc.Client
	width       int
	height      int
	activePane   paneType
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

	// Fetch proc detail when Detail pane is active and data is missing or stale
	if m.activePane == paneDetail && m.selectedPID > 0 && m.connected {
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

	// Fetch intent trees when Intent pane is active
	if m.activePane == paneIntent && m.connected {
		if m.intentTrees == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchIntentTreesCmd())
		}
	}

	// Fetch immune status when Security pane is active (Story 27-8)
	if m.activePane == paneSecurity && m.connected {
		if m.immuneStatus == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchImmuneStatusCmd())
		}
	}

	// Fetch trace list when Trace pane is active (Story 27-9)
	if m.activePane == paneTrace && m.connected {
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
		uuidSet := make(map[string]bool, len(m.processes))
		for _, p := range m.processes {
			uuidSet[p.UUID] = true
		}
		for uuid := range m.recording {
			if !uuidSet[uuid] {
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

func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.promptPager {
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if key == "q" || key == "esc" {
			m.promptPager = false
			return m, nil
		}
		if key == "home" {
			m.promptViewport.GotoTop()
			return m, nil
		}
		if key == "end" {
			m.promptViewport.GotoBottom()
			return m, nil
		}
		var cmd tea.Cmd
		m.promptViewport, cmd = m.promptViewport.Update(msg)
		return m, cmd
	}

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
		m.activePane = (m.activePane + 1) % 8
		return m, nil
	}

	if m.replayMode {
		m2, cmd := m.handleReplayKey(key)
		return m2, cmd
	}

	if m.activePane == paneTimeline && m.stepTimelineMode && len(m.stepEntries) > 0 && m.stepCursor < len(m.stepEntries) {
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
		if msg.Code == 'p' {
			entry := m.stepEntries[m.stepCursor]
			if cached := m.stepDetailCache[entry.summary.Step]; cached != nil {
				m.enterPromptPager(cached, entry.summary.Step)
				return m, nil
			}
			if !m.fetchingDetail && m.selectedPID > 0 {
				m.fetchingDetail = true
				return m, fetchStepDetailForPagerCmd(m.selectedPID, entry.summary.Step)
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
		case "enter":
			if m.treeCursor < len(m.treeRows) {
				m = selectProcess(m, m.treeRows[m.treeCursor])
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

	if m.activePane == paneIntent {
		switch key {
		case "down", "j":
			if m.intentCursor < len(m.intentFlatNodes)-1 {
				m.intentCursor++
				intentAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.intentCursor > 0 {
				m.intentCursor--
				intentAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if m.intentCursor < len(m.intentFlatNodes) {
				n := m.intentFlatNodes[m.intentCursor]
				if n.node != nil && n.node.PID > 0 {
					targetPID := types.PID(n.node.PID)
					// Verify PID exists in current process list
					pidFound := false
					var targetUUID string
					for _, p := range m.processes {
						if p.PID == targetPID {
							pidFound = true
							targetUUID = p.UUID
							break
						}
					}
					if !pidFound {
						m.statusMsg = "该进程已不存在"
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
					m.selectedPID = targetPID
					m.selectedUUID = targetUUID
					m.activePane = paneTimeline
					m2, cmd := m.handlePIDChange()
					return m2, cmd
				} else if n.node != nil {
					m.statusMsg = "该节点尚未分配进程"
					m.statusMsgTTL = statusMsgDefaultTTL
				}
			}
			return m, nil
		}
	}

	if m.activePane == paneSecurity {
		switch key {
		case "down", "j":
			if len(m.securityAlerts) > 0 && m.securityCursor < len(m.securityAlerts)-1 {
				m.securityCursor++
				securityAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.securityCursor > 0 {
				m.securityCursor--
				securityAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.securityAlerts) > 0 && m.securityCursor < len(m.securityAlerts) {
				alert := m.securityAlerts[m.securityCursor]
				targetPID := types.PID(alert.PID)
				// Verify PID exists in current process list
				pidFound := false
				var targetUUID string
				for _, p := range m.processes {
					if p.PID == targetPID {
						pidFound = true
						targetUUID = p.UUID
						break
					}
				}
				if !pidFound {
					m.statusMsg = "该进程已不存在"
					m.statusMsgTTL = statusMsgDefaultTTL
					return m, nil
				}
				m.selectedPID = targetPID
				m.selectedUUID = targetUUID
				m.activePane = paneTimeline
				m2, cmd := m.handlePIDChange()
				return m2, cmd
			}
			return m, nil
		}
	}

	if m.activePane == paneTrace {
		return m.handleTraceKey(key)
	}

	if m.activePane == paneEval {
		return m.handleEvalKey(key)
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
			recordID := m.recording[m.selectedUUID]
			return m, toggleRecordCmd(m.selectedPID, m.selectedUUID, recordID)
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

	treeWidth := min(max(w*40/100, 30), 60)
	rightWidth := max(w-treeWidth, 10)

	treePane := m.renderDashboardTreePane(treeWidth, contentHeight)

	topRightH := contentHeight / 2
	bottomRightH := contentHeight - topRightH
	timelinePane := m.renderTimelinePane(rightWidth, topRightH)

	var bottomPane string
	switch m.activePane {
	case paneDetail:
		bottomPane = m.renderDetailPane(rightWidth, bottomRightH)
	case paneIntent:
		bottomPane = m.renderIntentPane(rightWidth, bottomRightH)
	case paneSecurity:
		bottomPane = m.renderSecurityPane(rightWidth, bottomRightH)
	case paneTrace:
		bottomPane = m.renderTracePane(rightWidth, bottomRightH)
	case paneEval:
		bottomPane = m.renderEvalPane(rightWidth, bottomRightH)
	default:
		bottomPane = m.renderHeatmapPane(rightWidth, bottomRightH)
	}

	rightPane := lipgloss.JoinVertical(lipgloss.Left, timelinePane, bottomPane)
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
	if m.selectedPID > 0 && m.recording[m.selectedUUID] != "" {
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
	if m.activePane == paneDetail {
		return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  Detail: process info%s", rec, ops)
	}
	if m.activePane == paneIntent {
		return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Jump to Process%s", rec, ops)
	}
	if m.activePane == paneSecurity {
		return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Jump to Process%s", rec, ops)
	}
	if m.activePane == paneTrace {
		if m.traceViewMode == 0 {
			return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Expand Trace%s", rec, ops)
		}
		return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Jump to Process  Esc:Back%s", rec, ops)
	}
	if m.activePane == paneEval {
		return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  1/2/3:Sub-view%s", rec, ops)
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
		if m.recording[row.proc.UUID] != "" {
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
	m.promptPager = false
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

// --- Prompt Pager (Story 27-4) ---

var (
	promptRoleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	promptRoleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("#6BCB77"))
	promptRoleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B9BD5"))
	promptRoleTool      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD93D"))
)

const promptContentTruncateLimit = 2000

func formatPromptContent(detail *ipc.GetStepDetailResponse, step int) string {
	var b strings.Builder

	sysLen := utf8.RuneCountInString(detail.SystemPrompt)
	fmt.Fprintf(&b, "═══ System Prompt (%s chars) ═══\n\n", formatCharCount(sysLen))
	b.WriteString(detail.SystemPrompt)
	b.WriteString("\n\n")

	tokenLabel := formatTokenCount(detail.TokenCount)
	fmt.Fprintf(&b, "═══ Messages (%d msgs, ~%s tokens) ═══\n\n", detail.MessageCount, tokenLabel)

	toolCallNames := make(map[string]string)
	for _, msg := range detail.Messages {
		for _, tc := range msg.ToolCalls {
			toolCallNames[tc.ID] = tc.Name
		}
	}

	for _, msg := range detail.Messages {
		roleTag := formatRoleTag(msg, toolCallNames)
		content := msg.Content
		contentLen := utf8.RuneCountInString(content)
		if contentLen > promptContentTruncateLimit {
			runes := []rune(content)
			content = string(runes[:promptContentTruncateLimit]) + fmt.Sprintf("\n... (truncated, %d chars total)", contentLen)
		}
		fmt.Fprintf(&b, "%s %s\n\n", roleTag, content)
	}

	fmt.Fprintf(&b, "═══ Tools (%d) ═══\n\n", len(detail.Tools))
	for _, tool := range detail.Tools {
		desc := tool.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "• %s — %s\n", tool.Name, desc)
	}

	return b.String()
}

func formatRoleTag(msg ipc.MessageWire, toolCallNames map[string]string) string {
	switch msg.Role {
	case "system":
		return promptRoleSystem.Render("[system]")
	case "user":
		return promptRoleUser.Render("[user]")
	case "assistant":
		return promptRoleAssistant.Render("[assistant]")
	case "tool":
		label := ""
		if name, ok := toolCallNames[msg.ToolCallID]; ok && name != "" {
			label = ":" + name
		} else if msg.ToolCallID != "" {
			label = ":" + msg.ToolCallID
		}
		return promptRoleTool.Render("[tool" + label + "]")
	default:
		return "[" + msg.Role + "]"
	}
}

func formatCharCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

func (m *dashboardModel) enterPromptPager(detail *ipc.GetStepDetailResponse, step int) {
	content := formatPromptContent(detail, step)
	vp := viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(max(m.height-2, 1)))
	vp.SetContent(content)
	m.promptViewport = vp
	m.promptContent = content
	m.promptStep = step
	m.promptPager = true
}

func (m dashboardModel) renderPromptPager() string {
	detail := m.stepDetailCache[m.promptStep]
	msgCount := 0
	tokenLabel := "0"
	if detail != nil {
		msgCount = detail.MessageCount
		tokenLabel = formatTokenCount(detail.TokenCount)
	}

	title := fmt.Sprintf("  Prompt View | PID %d Step %d | %d msgs ~%s tokens",
		m.selectedPID, m.promptStep, msgCount, tokenLabel)
	help := "  j/k:scroll  PgUp/PgDn:page  Home/End:jump  q:back"

	return lipgloss.JoinVertical(lipgloss.Left, title, m.promptViewport.View(), help)
}

func fetchStepDetailForPagerCmd(pid types.PID, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return promptPagerMsg{pid: pid, step: step, err: err}
		}
		defer client.Close()
		detail, err := client.GetStepDetail(pid, step)
		return promptPagerMsg{pid: pid, step: step, detail: detail, err: err}
	}
}

// --- Detail Pane (Story 27-6) ---

func fetchProcDetailCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return procDetailResultMsg{pid: pid, uuid: uuid, err: err}
		}
		defer client.Close()
		resp, err := client.GetProcDetail(pid)
		return procDetailResultMsg{pid: pid, uuid: uuid, detail: resp, err: err}
	}
}

func (m dashboardModel) renderDetailPane(width, height int) string {
	isActive := m.activePane == paneDetail

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

	b.WriteString(" Detail")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select a process to view detail")
		return style.Render(b.String())
	}

	d := m.procDetail
	if d == nil || d.PID != m.selectedPID {
		b.WriteString("\n    Loading...")
		return style.Render(b.String())
	}

	// Section 1: Basic info
	var uptime time.Duration
	if d.CreatedAtMs > 0 {
		created := time.UnixMilli(d.CreatedAtMs)
		if d.DeadAtMs > 0 {
			uptime = time.UnixMilli(d.DeadAtMs).Sub(created)
		} else {
			uptime = time.Since(created)
		}
	}
	fmt.Fprintf(&b, "  PID: %d  UUID: %s\n", d.PID, truncateUUID(d.UUID))
	fmt.Fprintf(&b, "  State: %s  Intent: %s\n", d.State, truncateStr(d.Intent, 40))
	fmt.Fprintf(&b, "  Provider: %s  Model: %s\n", d.Provider, d.Model)
	fmt.Fprintf(&b, "  Uptime: %s\n", ui.FormatDuration(uptime))

	// Section 1b: Allowed devices
	if len(d.AllowedDevices) > 0 {
		fmt.Fprintf(&b, "  Devices: %s\n", strings.Join(d.AllowedDevices, ", "))
	}

	// Section 2: Skills
	b.WriteString("  ──── Skills ────\n")
	if len(d.Skills) == 0 {
		b.WriteString("    (none)\n")
	}
	for _, sk := range d.Skills {
		tools := strings.Join(sk.AllowedTools, ", ")
		if tools == "" {
			tools = "—"
		}
		fmt.Fprintf(&b, "    %s → %s\n", sk.Name, tools)
	}

	// Section 3: FD table
	b.WriteString("  ──── FD Table ────\n")
	if len(d.FDTable) == 0 {
		if d.State == "dead" {
			b.WriteString("    (closed)\n")
		} else {
			b.WriteString("    (empty)\n")
		}
	}
	for _, fd := range d.FDTable {
		fmt.Fprintf(&b, "    %d: %s\n", fd.FD, fd.DevicePath)
	}

	// Section 4: Context stats
	b.WriteString("  ──── Context ────\n")
	fmt.Fprintf(&b, "    %d msgs | %s tok\n", d.ContextStats.MessageCount, ui.FormatTokens(d.ContextStats.TokensUsed))
	if d.ContextStats.ContextBudget > 0 {
		barWidth := max(innerW-10, 10)
		filled := int(d.ContextStats.UsagePct / 100.0 * float64(barWidth))
		filled = min(filled, barWidth)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		pct := min(d.ContextStats.UsagePct, 100.0)
		fmt.Fprintf(&b, "    [%s] %.0f%%\n", bar, pct)
	}

	return style.Render(b.String())
}

func truncateUUID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func truncateStr(s string, maxLen int) string {
	if maxLen < 4 {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen-3]) + "..."
	}
	return s
}

// --- Intent Tree Pane (Story 27-7) ---

func intentStateColor(state string) lipgloss.Color {
	switch state {
	case "pending":
		return lipgloss.Color("240")
	case "decomposing":
		return lipgloss.Color("220")
	case "await_confirm":
		return lipgloss.Color("220")
	case "executing":
		return lipgloss.Color("39")
	case "completed":
		return lipgloss.Color("42")
	case "failed":
		return lipgloss.Color("196")
	case "retrying":
		return lipgloss.Color("208")
	default:
		return lipgloss.Color("240")
	}
}

func intentStateIcon(state string) string {
	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	switch state {
	case "completed":
		if ascii {
			return "+"
		}
		return "✓"
	case "executing":
		if ascii {
			return ">"
		}
		return "⟳"
	case "pending":
		if ascii {
			return "o"
		}
		return "○"
	case "failed":
		if ascii {
			return "x"
		}
		return "✗"
	case "retrying":
		if ascii {
			return "r"
		}
		return "↻"
	case "await_confirm":
		return "?"
	case "decomposing":
		if ascii {
			return "."
		}
		return "…"
	default:
		if ascii {
			return "o"
		}
		return "○"
	}
}

func isIntentTreeTerminal(state string) bool {
	return state == "completed" || state == "failed"
}

func flattenIntentTrees(trees []*ipc.IntentTreeWire) []intentFlatNode {
	var result []intentFlatNode

	// AC-2: Sort trees — active first, completed/failed at bottom
	sorted := make([]*ipc.IntentTreeWire, len(trees))
	copy(sorted, trees)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := isIntentTreeTerminal(sorted[i].State)
		tj := isIntentTreeTerminal(sorted[j].State)
		if ti != tj {
			return !ti // active before terminal
		}
		return false
	})

	for treeIdx, tree := range sorted {
		collapsed := isIntentTreeTerminal(tree.State)
		result = append(result, intentFlatNode{
			treeIndex:    treeIdx,
			isTreeHeader: true,
			isCollapsed:  collapsed,
			treeWire:     tree,
		})

		// AC-2: Terminal trees shown collapsed (header only)
		if collapsed || len(tree.Nodes) == 0 {
			continue
		}

		// Compute indent levels via longest path from roots
		indent := make(map[string]int)

		// Identify root nodes (no valid dependencies)
		for _, node := range tree.Nodes {
			validDeps := 0
			for _, dep := range node.DependsOn {
				if _, exists := tree.Nodes[dep]; exists {
					validDeps++
				}
			}
			if validDeps == 0 {
				indent[node.ID] = 0
			}
		}

		// Iteratively resolve indent levels
		changed := true
		for changed {
			changed = false
			for _, node := range tree.Nodes {
				if _, done := indent[node.ID]; done {
					continue
				}
				maxDepIndent := -1
				allResolved := true
				for _, dep := range node.DependsOn {
					if _, exists := tree.Nodes[dep]; !exists {
						continue
					}
					depIndent, resolved := indent[dep]
					if !resolved {
						allResolved = false
						break
					}
					if depIndent > maxDepIndent {
						maxDepIndent = depIndent
					}
				}
				if allResolved && maxDepIndent >= 0 {
					indent[node.ID] = maxDepIndent + 1
					changed = true
				} else if allResolved {
					indent[node.ID] = 0
					changed = true
				}
			}
		}

		// Handle any unresolved nodes (cycles)
		for _, node := range tree.Nodes {
			if _, ok := indent[node.ID]; !ok {
				indent[node.ID] = 0
			}
		}

		// Collect and sort by (indent, nodeID)
		type nodeEntry struct {
			id     string
			indent int
			node   *ipc.IntentNodeWire
		}
		var nodes []nodeEntry
		for id, node := range tree.Nodes {
			nodes = append(nodes, nodeEntry{id: id, indent: indent[id], node: node})
		}
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].indent != nodes[j].indent {
				return nodes[i].indent < nodes[j].indent
			}
			return nodes[i].id < nodes[j].id
		})

		for _, n := range nodes {
			result = append(result, intentFlatNode{
				treeIndex: treeIdx,
				nodeID:    n.id,
				indent:    n.indent,
				node:      n.node,
				treeWire:  tree,
			})
		}
	}

	return result
}

func fetchIntentTreesCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return intentTreesMsg{err: err}
		}
		defer client.Close()
		resp, err := client.IntentList()
		return intentTreesMsg{trees: resp, err: err}
	}
}

func (m dashboardModel) renderIntentPane(width, height int) string {
	isActive := m.activePane == paneIntent

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
	b.WriteString(" Intent Tree\n")

	if len(m.intentFlatNodes) == 0 {
		b.WriteString("\n    当前无意图分解任务。使用 rnix apply 创建声明式意图。")
		return style.Render(b.String())
	}

	// Determine the treeIndex of the currently selected cursor
	cursorTreeIndex := -1
	if m.intentCursor < len(m.intentFlatNodes) {
		cursorTreeIndex = m.intentFlatNodes[m.intentCursor].treeIndex
	}

	// Viewport: render only visible range (innerH - 1 for the header line)
	visibleLines := max(innerH-1, 1)
	startIdx := m.intentScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.intentFlatNodes))

	for i := startIdx; i < endIdx; i++ {
		n := m.intentFlatNodes[i]
		cursor := "  "
		if i == m.intentCursor {
			cursor = "▸ "
		}

		if n.isTreeHeader {
			// AC-6: separator between trees (skip first tree)
			if n.treeIndex > 0 {
				b.WriteString("  ───\n")
			}
			icon := intentStateIcon(n.treeWire.State)
			headerStyle := lipgloss.NewStyle().Foreground(intentStateColor(n.treeWire.State))
			// AC-6: highlight tree title if cursor is in this tree
			if n.treeIndex == cursorTreeIndex {
				headerStyle = headerStyle.Bold(true)
			}
			// AC-2: collapsed indicator for terminal trees
			arrow := "▶"
			if n.isCollapsed {
				arrow = "▷"
			}
			line := fmt.Sprintf("%s%s %s [%s] %s", cursor, arrow, truncateStr(n.treeWire.RootIntent, 40), n.treeWire.State, icon)
			b.WriteString(headerStyle.Render(line))
			b.WriteString("\n")

			// Fix #8: Empty Nodes map shows "(分解中...)"
			if len(n.treeWire.Nodes) == 0 {
				b.WriteString("    (分解中...)\n")
			}
			continue
		}

		if n.node == nil {
			continue
		}

		indentStr := strings.Repeat("  ", n.indent+1)
		icon := intentStateIcon(n.node.State)
		intent := truncateStr(n.node.Intent, 40)

		var pidStr string
		if n.node.PID > 0 {
			pidStr = fmt.Sprintf(" (PID:%d)", n.node.PID)
		}

		stateColor := intentStateColor(n.node.State)
		nodeStyle := lipgloss.NewStyle().Foreground(stateColor)

		line := fmt.Sprintf("%s%s%s: %s %s%s", cursor, indentStr, n.nodeID, intent, icon, pidStr)
		b.WriteString(nodeStyle.Render(line))
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

// intentAdjustScroll ensures intentCursor is visible within the viewport.
func intentAdjustScroll(m *dashboardModel) {
	visibleLines := max(m.height/2-3, 1) // approximate bottom-right pane visible area
	if m.intentCursor < m.intentScrollOffset {
		m.intentScrollOffset = m.intentCursor
	}
	if m.intentCursor >= m.intentScrollOffset+visibleLines {
		m.intentScrollOffset = m.intentCursor - visibleLines + 1
	}
}

// =============================================================================
// Security Pane (Story 27-8)
// =============================================================================

func fetchImmuneStatusCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return immuneStatusMsg{err: err}
		}
		defer client.Close()
		resp, err := client.ImmuneStatus()
		return immuneStatusMsg{status: resp, err: err}
	}
}

func sortAlertsByDeviation(alerts []ipc.AlertWire) []ipc.AlertWire {
	if len(alerts) == 0 {
		return nil
	}
	sorted := make([]ipc.AlertWire, len(alerts))
	copy(sorted, alerts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Deviation > sorted[j].Deviation
	})
	return sorted
}

func alertTypeColor(alertType string) lipgloss.Color {
	switch alertType {
	case "syscall_freq":
		return lipgloss.Color("220") // yellow
	case "token_rate":
		return lipgloss.Color("208") // orange
	case "device_access":
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("240") // gray
	}
}

func securityStatusColor(status string) lipgloss.Color {
	switch status {
	case "ok":
		return lipgloss.Color("42") // green
	case "warning":
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("240") // gray
	}
}

func formatUptimeShort(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func (m dashboardModel) renderSecurityPane(width, height int) string {
	isActive := m.activePane == paneSecurity

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
	b.WriteString(" Security \n")

	// nil guard: data not yet fetched
	if m.immuneStatus == nil {
		if m.immuneErr != nil {
			fmt.Fprintf(&b, " Error: %v\n", m.immuneErr)
		} else {
			b.WriteString(" Loading...\n")
		}
		return style.Render(b.String())
	}

	// AC-7: Immune Daemon not running
	if !m.immuneStatus.Running {
		b.WriteString(" Immune Daemon not running.\n")
		b.WriteString(" Security monitoring unavailable.\n")
		return style.Render(b.String())
	}

	// AC-5: Security status summary
	statusStr := m.immuneStatus.SecurityStatus
	statusStyle := lipgloss.NewStyle().Foreground(securityStatusColor(statusStr))

	if statusStr == "ok" {
		b.WriteString(statusStyle.Render(fmt.Sprintf(" Security: %s", strings.ToUpper(statusStr))))
		b.WriteString("\n")
		fmt.Fprintf(&b, " Immune Daemon: running (%s)\n", formatUptimeShort(m.immuneStatus.UptimeMs))
		fmt.Fprintf(&b, " Threats in memory: %d\n", m.immuneStatus.ThreatCount)
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(" All processes behaving normally"))
		b.WriteString("\n")
	} else {
		// warning status
		alertCount := len(m.securityAlerts)
		suspendedCount := len(m.immuneStatus.SuspendedPIDs)
		warnIcon := "!"
		ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
		if !ascii {
			warnIcon = "⚠"
		}
		summary := fmt.Sprintf(" %s Security: %d alerts, %d suspended", warnIcon, alertCount, suspendedCount)
		b.WriteString(statusStyle.Render(summary))
		b.WriteString("\n")
		fmt.Fprintf(&b, " Immune Daemon: running (%s)\n", formatUptimeShort(m.immuneStatus.UptimeMs))
		b.WriteString("\n")

		// AC-3: Alert list (with scroll offset)
		if alertCount > 0 {
			b.WriteString(" ALERTS\n")
			// Calculate visible range based on scroll offset
			visibleAlerts := max(innerH-6, 1) // reserve lines for header/summary/suspended
			startIdx := m.securityScrollOffset
			endIdx := min(startIdx+visibleAlerts, alertCount)
			if startIdx > 0 {
				fmt.Fprintf(&b, " ... %d more above\n", startIdx)
			}
			for i := startIdx; i < endIdx; i++ {
				alert := m.securityAlerts[i]
				cursor := "  "
				if i == m.securityCursor {
					cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Render("> ")
				}
				typeStyle := lipgloss.NewStyle().Foreground(alertTypeColor(alert.Type))
				ago := formatTimeAgo(alert.TimestampMs)
				line := fmt.Sprintf("%sPID:%-4d %-16s %s  (%.1fx)  %s",
					cursor,
					alert.PID,
					alert.AgentTemplate,
					typeStyle.Render(alert.Type),
					alert.Deviation,
					ago,
				)
				b.WriteString(line)
				b.WriteString("\n")
				// Detail line
				if alert.Detail != "" {
					fmt.Fprintf(&b, "    %s\n", alert.Detail)
				}
			}
			if endIdx < alertCount {
				fmt.Fprintf(&b, " ... %d more below\n", alertCount-endIdx)
			}
		}

		// AC-6: Suspended processes
		if suspendedCount > 0 {
			b.WriteString("\n SUSPENDED\n")
			for _, pid := range m.immuneStatus.SuspendedPIDs {
				fmt.Fprintf(&b, "   PID:%d → resume/kill\n", pid)
			}
		}
	}

	return style.Render(b.String())
}

func formatTimeAgo(timestampMs int64) string {
	if timestampMs <= 0 {
		return ""
	}
	elapsed := time.Since(time.UnixMilli(timestampMs))
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(elapsed.Seconds()), 0))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	}
}

// securityAdjustScroll ensures securityCursor is visible within the viewport.
func securityAdjustScroll(m *dashboardModel) {
	visibleLines := max(m.height/2-3, 1)
	if m.securityCursor < m.securityScrollOffset {
		m.securityScrollOffset = m.securityCursor
	}
	if m.securityCursor >= m.securityScrollOffset+visibleLines {
		m.securityScrollOffset = m.securityCursor - visibleLines + 1
	}
}

// =============================================================================
// Trace Pane (Story 27-9)
// =============================================================================

func fetchTraceListCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return traceListMsg{err: err}
		}
		defer client.Close()
		summaries, err := client.TraceList()
		return traceListMsg{summaries: summaries, err: err}
	}
}

func fetchTraceTreeCmd(traceID string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return traceTreeMsg{traceID: traceID, err: err}
		}
		defer client.Close()
		tree, err := client.TraceTree(traceID)
		return traceTreeMsg{traceID: traceID, tree: tree, err: err}
	}
}

func (m dashboardModel) handleTraceKey(key string) (tea.Model, tea.Cmd) {
	if m.traceViewMode == 0 {
		// List mode
		switch key {
		case "down", "j":
			if len(m.traceSummaries) > 0 && m.traceCursor < len(m.traceSummaries)-1 {
				m.traceCursor++
				traceAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.traceCursor > 0 {
				m.traceCursor--
				traceAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.traceSummaries) > 0 && m.traceCursor < len(m.traceSummaries) {
				traceID := m.traceSummaries[m.traceCursor].TraceID
				m.selectedTraceID = traceID
				return m, fetchTraceTreeCmd(traceID)
			}
			return m, nil
		}
	} else {
		// Tree mode
		switch key {
		case "down", "j":
			if len(m.spanFlatNodes) > 0 && m.spanCursor < len(m.spanFlatNodes)-1 {
				m.spanCursor++
				spanAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.spanCursor > 0 {
				m.spanCursor--
				spanAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.spanFlatNodes) > 0 && m.spanCursor < len(m.spanFlatNodes) {
				node := m.spanFlatNodes[m.spanCursor]
				if node.pid > 0 {
					targetPID := node.pid
					pidFound := false
					var targetUUID string
					for _, p := range m.processes {
						if p.PID == targetPID {
							pidFound = true
							targetUUID = p.UUID
							break
						}
					}
					if !pidFound {
						m.statusMsg = "该进程已不存在"
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
					m.selectedPID = targetPID
					m.selectedUUID = targetUUID
					m.activePane = paneTimeline
					m2, cmd := m.handlePIDChange()
					return m2, cmd
				}
			}
			return m, nil
		case "esc", "escape":
			m.traceViewMode = 0
			// Reset scroll and clamp cursor to current list bounds
			if m.traceCursor >= len(m.traceSummaries) {
				m.traceCursor = max(0, len(m.traceSummaries)-1)
			}
			m.traceScrollOffset = 0
			traceAdjustScroll(&m)
			return m, nil
		}
	}
	return m, nil
}

func flattenSpanTree(tree *ipc.SpanTreeWire) []spanFlatNode {
	if tree == nil || tree.Root == nil {
		return nil
	}
	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	var nodes []spanFlatNode
	flattenSpanNode(tree.Root, 0, true, "", ascii, &nodes)
	return nodes
}

func flattenSpanNode(node *ipc.SpanNodeWire, depth int, isLast bool, parentPrefix string, ascii bool, out *[]spanFlatNode) {
	var prefix string
	if depth == 0 {
		if ascii {
			prefix = "+-- "
		} else {
			prefix = "┌─ "
		}
	} else {
		if isLast {
			if ascii {
				prefix = parentPrefix + "`-- "
			} else {
				prefix = parentPrefix + "└─ "
			}
		} else {
			if ascii {
				prefix = parentPrefix + "|-- "
			} else {
				prefix = parentPrefix + "├─ "
			}
		}
	}
	*out = append(*out, spanFlatNode{
		spanID: node.SpanID,
		pid:    types.PID(node.PID),
		name:   node.Name,
		durMs:  node.DurationMs,
		tokens: node.TokensUsed,
		status: node.Status,
		depth:  depth,
		prefix: prefix,
		isRoot: depth == 0,
	})
	childPrefix := parentPrefix
	if depth > 0 {
		if isLast {
			childPrefix += "   "
		} else {
			if ascii {
				childPrefix += "|  "
			} else {
				childPrefix += "│  "
			}
		}
	}
	for i, child := range node.Children {
		flattenSpanNode(&child, depth+1, i == len(node.Children)-1, childPrefix, ascii, out)
	}
}

func spanStatusColor(status string) lipgloss.Color {
	switch status {
	case "ok":
		return lipgloss.Color("42")
	case "error":
		return lipgloss.Color("196")
	case "timeout":
		return lipgloss.Color("208")
	default:
		return lipgloss.Color("240")
	}
}

func (m dashboardModel) renderTracePane(width, height int) string {
	isActive := m.activePane == paneTrace

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

	if m.traceViewMode == 1 {
		return style.Render(m.renderTraceTreeView(innerW, innerH))
	}
	return style.Render(m.renderTraceListView(innerW, innerH))
}

func (m dashboardModel) renderTraceListView(width, height int) string {
	var b strings.Builder
	b.WriteString(" Traces\n")

	if m.traceErr != nil {
		fmt.Fprintf(&b, "\n    Error: %v\n", m.traceErr)
		return b.String()
	}

	if len(m.traceSummaries) == 0 {
		b.WriteString("\n    无活跃的 Compose 追踪数据。使用 rnix compose up 启动编排以生成追踪。\n")
		return b.String()
	}

	// Header
	fmt.Fprintf(&b, " %-16s  %-12s  %5s  %8s\n", "TRACE ID", "ROOT", "SPANS", "DUR")

	visibleLines := max(height-3, 1)
	startIdx := m.traceScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.traceSummaries))

	for i := startIdx; i < endIdx; i++ {
		ts := m.traceSummaries[i]
		cursor := "  "
		if i == m.traceCursor {
			if os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true" {
				cursor = "> "
			} else {
				cursor = "▸ "
			}
		}
		tid := ts.TraceID
		if len(tid) > 16 {
			tid = tid[:16]
		}
		dur := formatTimelineDuration(float64(ts.TotalDurationMs))
		fmt.Fprintf(&b, "%s%-16s  %-12s  %5d  %8s\n", cursor, tid, ts.RootSpanName, ts.SpanCount, dur)
	}

	return b.String()
}

func (m dashboardModel) renderTraceTreeView(width, height int) string {
	var b strings.Builder

	if m.selectedSpanTree == nil || len(m.spanFlatNodes) == 0 {
		b.WriteString(" Trace: (loading...)\n")
		return b.String()
	}

	meta := m.selectedSpanTree.Metadata
	tid := m.selectedTraceID
	if len(tid) > 16 {
		tid = tid[:16]
	}
	dur := formatTimelineDuration(float64(meta.TotalDurationMs))
	fmt.Fprintf(&b, " Trace: %s  %d spans  %s  %d tok\n", tid, meta.TotalSpans, dur, meta.TotalTokens)

	visibleLines := max(height-2, 1)
	startIdx := m.spanScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.spanFlatNodes))

	for i := startIdx; i < endIdx; i++ {
		node := m.spanFlatNodes[i]
		cursor := "  "
		if i == m.spanCursor {
			if os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true" {
				cursor = "> "
			} else {
				cursor = "▸ "
			}
		}

		dur := formatTimelineDuration(float64(node.durMs))
		tokStr := fmt.Sprintf("%dtok", node.tokens)
		statusStyle := lipgloss.NewStyle().Foreground(spanStatusColor(node.status))

		line := fmt.Sprintf("%s%s%s (PID %d)  %s  %s  %s",
			cursor, node.prefix, node.name, node.pid, dur, tokStr, statusStyle.Render(node.status))
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// traceBottomInnerH computes the inner height of the bottom-right pane from terminal height.
func traceBottomInnerH(termHeight int) int {
	contentHeight := max(termHeight-4, 3)
	bottomRightH := contentHeight - contentHeight/2
	return max(bottomRightH-2, 1) // subtract border
}

// traceAdjustScroll ensures traceCursor is visible within the viewport.
func traceAdjustScroll(m *dashboardModel) {
	visibleLines := max(traceBottomInnerH(m.height)-3, 1) // match renderTraceListView
	if m.traceCursor < m.traceScrollOffset {
		m.traceScrollOffset = m.traceCursor
	}
	if m.traceCursor >= m.traceScrollOffset+visibleLines {
		m.traceScrollOffset = m.traceCursor - visibleLines + 1
	}
}

// spanAdjustScroll ensures spanCursor is visible within the viewport.
func spanAdjustScroll(m *dashboardModel) {
	visibleLines := max(traceBottomInnerH(m.height)-2, 1) // match renderTraceTreeView
	if m.spanCursor < m.spanScrollOffset {
		m.spanScrollOffset = m.spanCursor
	}
	if m.spanCursor >= m.spanScrollOffset+visibleLines {
		m.spanScrollOffset = m.spanCursor - visibleLines + 1
	}
}

// =============================================================================
// Eval Pane (Story 27-10)
// =============================================================================

func fetchReputationCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return evalReputationMsg{err: err}
		}
		defer client.Close()
		resp, err := client.ReputationStatus("")
		if err != nil {
			return evalReputationMsg{err: err}
		}
		if resp != nil {
			return evalReputationMsg{summaries: resp.Summaries}
		}
		return evalReputationMsg{}
	}
}

func fetchTopologyCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return evalTopologyMsg{err: err}
		}
		defer client.Close()
		resp, err := client.TopologyQuery()
		if err != nil {
			return evalTopologyMsg{err: err}
		}
		return evalTopologyMsg{topology: resp}
	}
}

func fetchSynergyCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return evalSynergyMsg{err: err}
		}
		defer client.Close()
		resp, err := client.SynergyList()
		if err != nil {
			return evalSynergyMsg{err: err}
		}
		if resp != nil {
			return evalSynergyMsg{combos: resp.Combos}
		}
		return evalSynergyMsg{}
	}
}

func (m dashboardModel) handleEvalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "1":
		m.evalSubView = 0
		return m, nil
	case "2":
		m.evalSubView = 1
		if m.evalTopology == nil && m.connected {
			return m, fetchTopologyCmd()
		}
		return m, nil
	case "3":
		m.evalSubView = 2
		if m.evalSynergies == nil && m.connected {
			return m, fetchSynergyCmd()
		}
		return m, nil
	case "down", "j":
		switch m.evalSubView {
		case 0:
			if len(m.evalReputations) > 0 && m.evalRepCursor < len(m.evalReputations)-1 {
				m.evalRepCursor++
				evalRepAdjustScroll(&m)
			}
		case 1:
			totalItems := evalTopoItemCount(&m)
			if totalItems > 0 && m.evalTopoCursor < totalItems-1 {
				m.evalTopoCursor++
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if len(m.evalSynergies) > 0 && m.evalSynCursor < len(m.evalSynergies)-1 {
				m.evalSynCursor++
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	case "up", "k":
		switch m.evalSubView {
		case 0:
			if m.evalRepCursor > 0 {
				m.evalRepCursor--
				evalRepAdjustScroll(&m)
			}
		case 1:
			if m.evalTopoCursor > 0 {
				m.evalTopoCursor--
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if m.evalSynCursor > 0 {
				m.evalSynCursor--
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	}
	return m, nil
}

func evalTopoItemCount(m *dashboardModel) int {
	if m.evalTopology == nil {
		return 0
	}
	return len(m.evalTopology.Nodes) + len(m.evalTopology.Edges)
}

func (m dashboardModel) renderEvalPane(width, height int) string {
	isActive := m.activePane == paneEval

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

	// Header with sub-view tabs
	repTab := "[1]Reputation"
	topoTab := "[2]Topology"
	synTab := "[3]Synergy"
	switch m.evalSubView {
	case 0:
		repTab = lipgloss.NewStyle().Bold(true).Render(repTab)
	case 1:
		topoTab = lipgloss.NewStyle().Bold(true).Render(topoTab)
	case 2:
		synTab = lipgloss.NewStyle().Bold(true).Render(synTab)
	}
	fmt.Fprintf(&b, " Evaluation  %s %s %s\n", repTab, topoTab, synTab)

	switch m.evalSubView {
	case 0:
		b.WriteString(m.renderEvalReputationView(innerW, innerH-1))
	case 1:
		b.WriteString(m.renderEvalTopologyView(innerW, innerH-1))
	case 2:
		b.WriteString(m.renderEvalSynergyView(innerW, innerH-1))
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderEvalReputationView(width, height int) string {
	var b strings.Builder

	if m.evalRepErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.evalRepErr)
		return b.String()
	}

	if len(m.evalReputations) == 0 {
		b.WriteString("\n    需要更多执行数据以生成评价。使用 rnix spawn 或 rnix compose up 执行任务以积累数据。\n")
		return b.String()
	}

	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	cursorChar := "▸"
	if ascii {
		cursorChar = ">"
	}

	// Header
	fmt.Fprintf(&b, " %-16s %5s  %7s  %7s  %7s  %3s  %s\n",
		"AGENT", "SCORE", "SUCCESS", "AVG TOK", "AVG DUR", "N", "TREND")

	visibleLines := max(height-2, 1)
	startIdx := m.evalRepScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.evalReputations))

	for i := startIdx; i < endIdx; i++ {
		r := m.evalReputations[i]
		cursor := "  "
		if i == m.evalRepCursor {
			cursor = cursorChar + " "
		}

		var trendIcon string
		var trendColor string
		switch r.RecentTrend {
		case "improving":
			trendColor = ui.ColorSuccess
			if ascii {
				trendIcon = "^"
			} else {
				trendIcon = "↑"
			}
		case "declining":
			trendColor = ui.ColorError
			if ascii {
				trendIcon = "v"
			} else {
				trendIcon = "↓"
			}
		default:
			trendColor = ui.ColorMuted
			if ascii {
				trendIcon = "-"
			} else {
				trendIcon = "→"
			}
		}
		trendStr := lipgloss.NewStyle().Foreground(lipgloss.Color(trendColor)).Render(trendIcon)

		durStr := formatTimelineDuration(float64(r.AvgDurationMs))

		fmt.Fprintf(&b, "%s%-16s %5.2f  %5.1f%%  %7d  %7s  %3d  %s\n",
			cursor, r.AgentName, r.Score, r.SuccessRate*100, r.AvgTokens, durStr, r.TotalRecords, trendStr)
	}

	return b.String()
}

func (m dashboardModel) renderEvalTopologyView(_, height int) string {
	var b strings.Builder

	if m.evalTopoErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.evalTopoErr)
		return b.String()
	}

	if m.evalTopology == nil {
		b.WriteString("\n    无协作拓扑数据。运行多智能体编排以生成协作关系。\n")
		return b.String()
	}

	nodeCount := len(m.evalTopology.Nodes)
	edgeCount := len(m.evalTopology.Edges)

	if nodeCount == 0 && edgeCount == 0 {
		b.WriteString("\n    无协作拓扑数据。运行多智能体编排以生成协作关系。\n")
		return b.String()
	}

	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	cursorChar := "▸"
	if ascii {
		cursorChar = ">"
	}
	reinforcedMark := "★"
	if ascii {
		reinforcedMark = "*"
	}

	// Nodes section header (always visible)
	b.WriteString(" ── Nodes ──\n")
	fmt.Fprintf(&b, " %-16s  %5s  %s\n", "AGENT", "SCORE", "CONNECTIONS")

	totalItems := nodeCount + edgeCount
	visibleLines := max(height-2, 1) // -2 for section header + column header
	startIdx := m.evalTopoScrollOffset
	endIdx := min(startIdx+visibleLines, totalItems)

	edgesHeaderPrinted := false
	for i := startIdx; i < endIdx; i++ {
		cursor := "  "
		if i == m.evalTopoCursor {
			cursor = cursorChar + " "
		}
		if i < nodeCount {
			node := m.evalTopology.Nodes[i]
			fmt.Fprintf(&b, "%s%-16s  %5.2f  %11d\n", cursor, node.Agent, node.ReputationScore, node.Connections)
		} else {
			if !edgesHeaderPrinted {
				b.WriteString("\n ── Edges ──\n")
				headerArrow := "→"
				if ascii {
					headerArrow = "->"
				}
				fmt.Fprintf(&b, " %-26s  %5s  %3s  %5s  %s\n", "FROM "+headerArrow+" TO", "SPAWN", "MSG", "TOTAL", "REINFORCED")
				edgesHeaderPrinted = true
			}
			edge := m.evalTopology.Edges[i-nodeCount]
			rMark := ""
			if edge.Reinforced {
				rMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render(reinforcedMark)
			}
			arrowStr := " → "
			if ascii {
				arrowStr = " -> "
			}
			label := fmt.Sprintf("%s%s%s", edge.From, arrowStr, edge.To)
			fmt.Fprintf(&b, "%s%-26s  %5d  %3d  %5d  %s\n", cursor, label, edge.SpawnCount, edge.MsgCount, edge.Total, rMark)
		}
	}

	return b.String()
}

func (m dashboardModel) renderEvalSynergyView(width, height int) string {
	var b strings.Builder

	if m.evalSynErr != nil {
		fmt.Fprintf(&b, " Error: %v\n", m.evalSynErr)
		return b.String()
	}

	if len(m.evalSynergies) == 0 {
		b.WriteString("\n    无技能组合数据。当 Agent 使用多个 Skill 执行任务时将自动记录。\n")
		return b.String()
	}

	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	cursorChar := "▸"
	if ascii {
		cursorChar = ">"
	}

	fmt.Fprintf(&b, " %-20s  %7s  %7s  %4s  %8s  %s\n",
		"SKILLS", "SUCCESS", "AVG TOK", "EXEC", "VS SOLO", "REC")

	visibleLines := max(height-2, 1)
	startIdx := m.evalSynScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.evalSynergies))

	for i := startIdx; i < endIdx; i++ {
		combo := m.evalSynergies[i]
		cursor := "  "
		if i == m.evalSynCursor {
			cursor = cursorChar + " "
		}

		skills := strings.Join(combo.Skills, ",")
		if len(skills) > 20 {
			skills = skills[:17] + "..."
		}

		var recStr string
		if combo.Recommended {
			if ascii {
				recStr = "Y"
			} else {
				recStr = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render("✓")
			}
		}

		improvement := fmt.Sprintf("%+.1f%%", combo.TokenImprovement*100)

		fmt.Fprintf(&b, "%s%-20s  %5.1f%%  %7d  %4d  %8s  %s\n",
			cursor, skills, combo.SuccessRate*100, combo.AvgTokens, combo.TotalExecutions, improvement, recStr)
	}

	return b.String()
}

// evalBottomInnerH returns the inner height available for eval sub-views,
// matching the render path: renderDashboard → renderEvalPane → sub-view.
func evalBottomInnerH(termHeight int) int {
	contentHeight := max(termHeight-4, 3)
	bottomRightH := contentHeight - contentHeight/2
	return max(bottomRightH-2, 1) // subtract border
}

// evalRepAdjustScroll ensures evalRepCursor is visible within the viewport.
func evalRepAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalReputationView
	if m.evalRepCursor < m.evalRepScrollOffset {
		m.evalRepScrollOffset = m.evalRepCursor
	}
	if m.evalRepCursor >= m.evalRepScrollOffset+visibleLines {
		m.evalRepScrollOffset = m.evalRepCursor - visibleLines + 1
	}
}

// evalTopoAdjustScroll ensures evalTopoCursor is visible within the viewport.
func evalTopoAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalTopologyView
	if m.evalTopoCursor < m.evalTopoScrollOffset {
		m.evalTopoScrollOffset = m.evalTopoCursor
	}
	if m.evalTopoCursor >= m.evalTopoScrollOffset+visibleLines {
		m.evalTopoScrollOffset = m.evalTopoCursor - visibleLines + 1
	}
}

// evalSynAdjustScroll ensures evalSynCursor is visible within the viewport.
func evalSynAdjustScroll(m *dashboardModel) {
	visibleLines := max(evalBottomInnerH(m.height)-3, 1) // match renderEvalSynergyView
	if m.evalSynCursor < m.evalSynScrollOffset {
		m.evalSynScrollOffset = m.evalSynCursor
	}
	if m.evalSynCursor >= m.evalSynScrollOffset+visibleLines {
		m.evalSynScrollOffset = m.evalSynCursor - visibleLines + 1
	}
}
