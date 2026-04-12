package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
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
	client       *ipc.Client
	width        int
	height       int
	activePane   paneType
	rightPane    paneType // 右侧显示的面板（默认 Timeline）
	viewMode     viewMode // 当前视图模式（默认 viewDefault，零值即默认）
	expandedPane paneType // viewExpanded 模式下展开的面板
	selectedPID  types.PID
	selectedUUID string
	processes    []vfs.ProcInfo
	treeRows     []flatRow
	treeCursor   int
	treeOffset   int
	treeSortMode int  // 0=Time, 1=PID, 2=State
	treeSortAsc  bool // false=descending (default), true=ascending

	// Expanded-tree search (only active in viewExpanded + paneTree)
	treeSearchQuery  string
	treeSearchMode   bool
	treeSearchCursor int
	treeSearchOffset int
	connected    bool
	err          error
	statusMsg    string
	startTime    time.Time
	confirmKill  bool
	confirmPID   types.PID

	// Timeline tracking
	timelineAttachedPID  types.PID
	timelineAttachedUUID string

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

	// Step timeline fields (unified)
	stepEntries     []stepEntry
	stepCursor      int
	stepScrollTop   int // index in filtered view of first visible item
	stepDetailCache map[int]*ipc.GetStepDetailResponse
	lastFetchedStep int
	fetchingDetail  bool
	stepFilters     map[string]bool
	stepFilterMode  bool
	stepExpandedIdx int

	// Prompt pager fields (Story 27-4)
	promptPager    bool
	promptViewport viewport.Model
	promptContent  string
	promptStep     int
	promptTab      promptPagerTab

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
	immuneStatus         *ipc.ImmuneStatusResponse
	immuneErr            error
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
	evalSubView          int // 0=reputation, 1=topology, 2=synergy
	evalReputations      []kernel.ReputationSummary
	evalRepErr           error
	evalRepCursor        int
	evalRepScrollOffset  int
	evalTopology         *ipc.TopologyQueryResponse
	evalTopoErr          error
	evalTopoCursor       int
	evalTopoScrollOffset int
	evalSynergies        []kernel.ComboSummary
	evalSynErr           error
	evalSynCursor        int
	evalSynScrollOffset  int

	// Offline replay fields (Story 17-5)
	replayMode       bool
	replayReader     *debug.RecordReader
	replayCursor     int
	replayPlaying    bool
	replaySpeed      float64
	prevReplayCursor int

	// HeartbeatStatus fields (Story 30.8)
	heartbeatStatus *ipc.HeartbeatStatusResponse

	// Timeline aggregation (Story 30.8 AC#5)
	expandedAggGroups map[int]bool

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

	// History view fields have been removed — H key now expands Agent Tree directly.

	// Help overlay
	helpOverlay bool

	// Unified event stream fields (Story 34.1)
	unifiedEvents      []UnifiedEvent
	sysEvents          []UnifiedEvent
	prevProcessPIDs    map[types.PID]vfs.ProcInfo
	budgetAlertSeen    map[types.PID]int
	stallSeen          map[types.PID]struct{}
	compactEvents      []ipc.SyscallEventWire
	lastCompactEventMs int64
	fetchingCompact    bool
	sysEventSeen       map[string]struct{}

	// Health counters for title bar (Story 34.2)
	errorCount int
	warnCount  int

	// Story 34.3: Process tree visual enhancement
	lastEventByPID     map[types.PID]time.Time // AC5: most active tracking
	userManualSelect   bool                     // AC5: user manual select flag
	collapsedDeadTrees map[string]bool          // AC3: dead subtree collapse state (key=UUID)

	// Story 34.4: Alert strip + unified timeline
	alertExpanded  bool             // alert strip expanded state
	alertCursor    int              // cursor position within alert strip
	alertEvents    []UnifiedEvent   // cached alerts (Severity >= SevWarn, sorted)
	alertJumpTarget *UnifiedEvent   // pending alert jump after PID change

	// Story 34.6: Debug mode — strace fusion
	debugMode           bool                        // Debug mode active
	debugEvents         []UnifiedEvent              // merged debug timeline (steps + strace)
	debugStraceEvents   []UnifiedEvent              // strace event ring buffer
	debugClient         *ipc.Client                 // independent IPC connection for strace stream
	debugStraceCh       <-chan ipc.SyscallEventWire  // strace event channel from goroutine
	debugShowStrace     bool                        // toggle strace visibility (default true)
	debugCtxProfile     *debug.CtxProfileResult     // Context Profile data
	debugDeviceLatency  map[string]*deviceLatencyStats // device latency stats
	debugAttachedPID    types.PID                   // currently attached PID for strace
	debugScrollTop      int                         // debug timeline scroll offset
	debugCursor         int                         // debug timeline cursor
}

func newDashboardModel(client *ipc.Client) dashboardModel {
	return dashboardModel{
		client:             client,
		startTime:          time.Now(),
		connected:          client != nil,
		rightPane:          paneTimeline,
		recording:          make(map[string]string),
		stepDetailCache:    make(map[int]*ipc.GetStepDetailResponse),
		procDetailCache:    make(map[string]*ipc.GetProcDetailResponse),
		stepExpandedIdx:    -1,
		stepFilters:        defaultStepFilters(),
		expandedAggGroups:  make(map[int]bool),
		prevProcessPIDs:    make(map[types.PID]vfs.ProcInfo),
		budgetAlertSeen:    make(map[types.PID]int),
		stallSeen:          make(map[types.PID]struct{}),
		sysEventSeen:       make(map[string]struct{}),
		lastEventByPID:     make(map[types.PID]time.Time),
		collapsedDeadTrees: make(map[string]bool),
		debugShowStrace:    true, // Story 34.6: show strace events by default
		debugDeviceLatency: make(map[string]*deviceLatencyStats),
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
		// Discard steps from a stale process (in-flight fetch from before process change).
		// uuid == "" means old daemon without UUID tracking — skip check for backward compat.
		if msg.uuid != "" && msg.uuid != m.selectedUUID {
			return m, nil
		}
		if msg.err == nil && len(msg.steps) > 0 {
			m = m.applyNewSteps(msg.steps)
			if last := msg.steps[len(msg.steps)-1]; last.Step > m.lastFetchedStep {
				m.lastFetchedStep = last.Step
			}
		}
		// Auto-fetch detail for expanded steps (e.g., auto-expanded on error/slow)
		if cmd := m.fetchNextExpandedDetail(); cmd != nil {
			m.fetchingDetail = true
			return m, cmd
		}
		return m, nil
	case stepDetailResultMsg:
		m.fetchingDetail = false
		if msg.err == nil && msg.detail != nil {
			m.stepDetailCache[msg.step] = msg.detail
		}
		// Chain-fetch next expanded step without cached detail
		if cmd := m.fetchNextExpandedDetail(); cmd != nil {
			m.fetchingDetail = true
			return m, cmd
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
	case heartbeatStatusMsg:
		if msg.err == nil && msg.status != nil {
			m.heartbeatStatus = msg.status
		}
		return m, nil
	case compactEventsMsg:
		m.fetchingCompact = false
		if msg.err != nil || msg.pid != m.selectedPID || msg.uuid != m.selectedUUID {
			return m, nil
		}
		for _, ev := range msg.events {
			if ev.Syscall == "Compact" && ev.TimestampMs > m.lastCompactEventMs {
				m.compactEvents = append(m.compactEvents, ev)
				m.lastCompactEventMs = ev.TimestampMs
				ue := compactEventFromSyscall(ev)
				deduped := sysEventDedup([]UnifiedEvent{ue}, m.sysEventSeen)
				m.sysEvents = append(m.sysEvents, deduped...)
				m.sysEvents = sysEventFIFO(m.sysEvents, m.sysEventSeen)
			}
		}
		// Cap compact events FIFO
		if len(m.compactEvents) > maxSysEvents {
			m.compactEvents = m.compactEvents[len(m.compactEvents)-maxSysEvents:]
		}
		return m, nil
	case resumeResultMsg:
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
	default:
		// Story 34.6: Debug mode messages
		if m2, cmd, handled := m.handleDebugMsg(msg); handled {
			return m2, cmd
		}
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

	// Set a read deadline so a hung server doesn't block the UI forever.
	if err := m.client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		m.client.Close()
		m.client = nil
		m.connected = false
		return m, tickCmd()
	}
	procs, err := m.client.ListAllProcs()
	// Clear deadline for future non-tick operations.
	_ = m.client.SetReadDeadline(time.Time{})
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
	roots := buildProcessTree(procs, m.treeSortMode, m.treeSortAsc)
	m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)

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

	// --- Unified event stream detection (Story 34.1) ---
	// Detect spawn/exit events by comparing with previous tick's process list
	spawnExitEvents := detectSpawnExitEvents(m.prevProcessPIDs, m.processes)
	if len(spawnExitEvents) > 0 {
		deduped := sysEventDedup(spawnExitEvents, m.sysEventSeen)
		m.sysEvents = append(m.sysEvents, deduped...)
	}

	// Detect budget threshold events
	budgetEvents := detectBudgetEvents(m.processes, m.budgetAlertSeen)
	if len(budgetEvents) > 0 {
		deduped := sysEventDedup(budgetEvents, m.sysEventSeen)
		m.sysEvents = append(m.sysEvents, deduped...)
	}

	// Detect heartbeat stall events
	stallEvents := detectStallEvents(m.heartbeatStatus, m.stallSeen)
	if len(stallEvents) > 0 {
		deduped := sysEventDedup(stallEvents, m.sysEventSeen)
		m.sysEvents = append(m.sysEvents, deduped...)
	}

	// FIFO eviction on system events
	m.sysEvents = sysEventFIFO(m.sysEvents, m.sysEventSeen)

	// Prune budget alert entries for dead processes (F5)
	pruneBudgetAlertSeen(m.budgetAlertSeen, m.processes)

	// Update previous process snapshot for next tick
	m.prevProcessPIDs = make(map[types.PID]vfs.ProcInfo, len(m.processes))
	for _, p := range m.processes {
		m.prevProcessPIDs[p.PID] = p
	}

	// Merge step entries + system events into unified event list
	m.unifiedEvents = mergeUnifiedEvents(m.stepEntries, m.sysEvents, m.selectedPID)

	// Build alert events for alert strip (Story 34.4)
	m.alertEvents = buildAlertEvents(m.unifiedEvents)
	// F5: Always clamp alertCursor to valid range (even when expanded)
	if len(m.alertEvents) == 0 {
		m.alertCursor = 0
	} else {
		visible := alertStripHeight(len(m.alertEvents), m.alertExpanded)
		if m.alertCursor >= visible {
			m.alertCursor = max(visible-1, 0)
		}
	}

	// F1: Resolve pending alert jump target after PID change
	if m.alertJumpTarget != nil {
		target := m.alertJumpTarget
		m.alertJumpTarget = nil
		filtered := m.filteredUnifiedEvents()
		for i, ev := range filtered {
			if ev.Type == target.Type && ev.Timestamp.Equal(target.Timestamp) && ev.PID == target.PID {
				m.stepCursor = i
				break
			}
		}
	}

	// Compute health counters for title bar (Story 34.2)
	m.errorCount, m.warnCount = computeHealthCounts(m.processes, m.unifiedEvents, m.heartbeatStatus)

	// Most active process tracking — update lastEventByPID from unified events
	now := time.Now()
	for _, ev := range m.unifiedEvents {
		if ev.PID > 0 {
			if existing, ok := m.lastEventByPID[ev.PID]; !ok || ev.Timestamp.After(existing) {
				m.lastEventByPID[ev.PID] = ev.Timestamp
			}
		}
	}
	activePIDs := make(map[types.PID]struct{}, len(m.processes)) // prune stale entries
	for _, p := range m.processes {
		activePIDs[p.PID] = struct{}{}
	}
	for pid := range m.lastEventByPID {
		if _, ok := activePIDs[pid]; !ok {
			delete(m.lastEventByPID, pid)
		}
	}
	// Auto-track: if user hasn't manually selected, follow most active process
	if !m.userManualSelect {
		var mostActivePID types.PID
		var mostActiveTime time.Time
		for pid, t := range m.lastEventByPID {
			if now.Sub(t) < 2*time.Second && t.After(mostActiveTime) {
				// Only track running processes
				for _, p := range m.processes {
					if p.PID == pid && (p.State == types.StateRunning || p.State == types.StateCreated) {
						mostActivePID = pid
						mostActiveTime = t
						break
					}
				}
			}
		}
		if mostActivePID > 0 && mostActivePID != m.selectedPID {
			for i, row := range m.treeRows {
				if row.proc.PID == mostActivePID {
					m.treeCursor = i
					m = selectProcess(m, row)
					break
				}
			}
		}
	}
	// Reset userManualSelect after 5s of all processes being silent
	if m.userManualSelect && len(m.lastEventByPID) > 0 {
		allSilent := true
		for _, t := range m.lastEventByPID {
			if now.Sub(t) < 5*time.Second {
				allSilent = false
				break
			}
		}
		if allSilent {
			m.userManualSelect = false
		}
	}

	cmds := []tea.Cmd{tickCmd()}

	pidChanged := m.selectedUUID != m.timelineAttachedUUID || m.selectedPID != m.heatmapPID
	if pidChanged {
		m2, pidCmd := m.handlePIDChange()
		m = m2
		if pidCmd != nil {
			cmds = append(cmds, pidCmd)
		}
	} else if m.selectedPID > 0 && m.connected && m.heatmapTickCount%5 == 0 && !m.isSelectedProcessDead() {
		cmds = append(cmds, fetchHeatmapCmd(m.selectedPID))
	}

	if m.selectedPID > 0 && m.connected && !pidChanged {
		cmds = append(cmds, fetchStepsCmd(m.selectedUUID, m.selectedPID, m.lastFetchedStep))
	}

	// Fetch proc detail when Detail pane is active or in default view (detail card needs it)
	detailCardNeedsData := m.viewMode == viewDefault && m.selectedPID > 0
	if (m.activePane == paneDetail || detailCardNeedsData) && m.selectedPID > 0 && m.connected {
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

	// Fetch intent trees only when Intent pane is active (no longer needed every tick)
	if m.activePane == paneIntent && m.connected {
		if m.intentTrees == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchIntentTreesCmd())
		}
	}

	// Fetch immune status only when Security pane is active
	if m.activePane == paneSecurity && m.connected {
		if m.immuneStatus == nil || m.heatmapTickCount%5 == 0 {
			cmds = append(cmds, fetchImmuneStatusCmd())
		}
	}

	// Fetch heartbeat status when Security pane is active or in default view (reduced frequency)
	// Stall detection and health counts depend on heartbeat data regardless of active pane.
	if m.connected && m.heatmapTickCount%5 == 0 {
		if m.activePane == paneSecurity || m.viewMode == viewDefault {
			cmds = append(cmds, fetchHeartbeatStatusCmd())
		}
	}

	// Fetch compact events for selected process (Story 34.1)
	if m.selectedPID > 0 && m.connected && !m.fetchingCompact && m.heatmapTickCount%3 == 0 {
		m.fetchingCompact = true
		cmds = append(cmds, fetchCompactEventsCmd(m.selectedPID, m.selectedUUID))
	}

	// Fetch trace list when Trace pane is active or in default view (detail card needs it)
	if m.connected {
		if m.activePane == paneTrace || m.viewMode == viewDefault {
			if m.traceSummaries == nil || m.heatmapTickCount%5 == 0 {
				cmds = append(cmds, fetchTraceListCmd())
			}
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

	// Story 34.6: Debug mode tick processing
	cmds = append(cmds, m.debugTickCmds()...)
	if m.debugMode && m.connected && m.heatmapTickCount%5 == 0 && m.selectedPID > 0 {
		cmds = append(cmds, m.fetchDebugCtxProfileCmd())
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
	detailOffset := 0
	if m.viewMode == viewDefault {
		detailOffset = 3 // detail card: 1 separator + 2 content lines
	}
	// In expanded tree mode subtract the 2-line stats bar rendered at the bottom of the pane.
	statsOffset := 0
	if m.viewMode == viewExpanded && m.expandedPane == paneTree {
		statsOffset = 2
	}
	// titleBar(2) + statusBar(1) + panelBorder(2) + headerLine(1) = 6
	return max(m.height-6-detailOffset-statsOffset, 1)
}

func (m dashboardModel) View() tea.View {
	var content string
	if m.helpOverlay {
		content = m.renderHelpOverlay()
	} else if m.promptPager {
		content = m.renderPromptPager()
	} else {
		content = m.renderDashboard()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
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

	titleLines := strings.Count(titleBar, "\n") + 1

	// Reserve lines for alert strip (Story 34.4)
	alertH := alertStripHeight(len(m.alertEvents), m.alertExpanded)
	// Alert strip with border takes 1 extra line for the top border
	alertReserve := alertH
	if alertH > 0 {
		alertReserve = alertH + 1
	}

	contentHeight := max(h-titleLines-1-alertReserve, 3)

	var mainContent string
	switch m.viewMode {
	case viewExpanded:
		mainContent = m.renderExpandedLayout(w, contentHeight)
	case viewLLM:
		mainContent = m.renderLLMViewer(w, contentHeight)
	case viewDebug:
		mainContent = m.renderDebugLayout(w, contentHeight)
	default: // viewDefault
		mainContent = m.renderDefaultLayout(w, contentHeight)
	}

	// Build alert strip if there are alerts
	alertStrip := renderAlertStrip(&m, w, alertH)
	if alertStrip != "" {
		return lipgloss.JoinVertical(lipgloss.Left, titleBar, mainContent, alertStrip, statusBar)
	}
	return lipgloss.JoinVertical(lipgloss.Left, titleBar, mainContent, statusBar)
}

func (m dashboardModel) renderDefaultLayout(w, h int) string {
	treeWidth := max(28, min(w*35/100, 50))
	rightWidth := max(w-treeWidth, 1)

	detailH := 3 // 1 separator + 2 content lines
	mainH := max(h-detailH, 3)

	// Main area: Tree + right pane
	treePane := m.renderDashboardTreePane(treeWidth, mainH)
	var rightPane string
	switch m.rightPane {
	case paneTimeline:
		rightPane = m.renderTimelinePane(rightWidth, mainH)
	default:
		rightPane = m.renderSinglePane(m.rightPane, rightWidth, mainH)
	}
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, treePane, rightPane)

	// Detail card area (replaces Focus Card)
	detailLeft := renderDetailCardLeft(&m, treeWidth, 2)
	detailRight := renderDetailCardRight(&m, rightWidth, 2)
	detailRow := lipgloss.JoinHorizontal(lipgloss.Top, detailLeft, detailRight)

	return lipgloss.JoinVertical(lipgloss.Left, mainRow, detailRow)
}

func (m dashboardModel) renderExpandedLayout(w, h int) string {
	// expandedPane 全屏
	return m.renderSinglePane(m.expandedPane, w, h)
}

// renderSinglePane 渲染单个面板（全宽）
func (m dashboardModel) renderSinglePane(p paneType, w, h int) string {
	switch p {
	case paneTree:
		return m.renderDashboardTreePane(w, h)
	case paneTimeline:
		return m.renderTimelinePane(w, h)
	case paneHeatmap:
		return m.renderHeatmapPane(w, h)
	case paneDetail:
		return m.renderDetailPane(w, h)
	case paneIntent:
		return m.renderIntentPane(w, h)
	case paneSecurity:
		return m.renderSecurityPane(w, h)
	case paneTrace:
		return m.renderTracePane(w, h)
	case paneEval:
		return m.renderEvalPane(w, h)
	default:
		return m.renderTimelinePane(w, h)
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
		m = m.handleTimelinePIDChange()
		m = m.handleHeatmapPIDChange()
		m.timelineAttachedPID = 0
		m.heatmapPID = 0
		return m, nil
	}
	m = m.handleTimelinePIDChange()
	m = m.handleHeatmapPIDChange()
	m.timelineAttachedPID = m.selectedPID
	m.heatmapPID = m.selectedPID

	// Reset compact event tracking for new process (Story 34.1)
	m.compactEvents = nil
	m.lastCompactEventMs = 0
	m.fetchingCompact = false

	// Filter out stale compact UnifiedEvents from sysEvents on PID change (F4)
	filtered := m.sysEvents[:0]
	for _, ev := range m.sysEvents {
		if ev.Type != EventCompact {
			filtered = append(filtered, ev)
		}
	}
	m.sysEvents = filtered

	m.statusMsg = fmt.Sprintf("Switched to PID %d, fetching steps...", m.selectedPID)
	m.statusMsgTTL = statusMsgDefaultTTL

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
		cmds = append(cmds, fetchHeatmapCmd(m.selectedPID))
		cmds = append(cmds, fetchStepsCmd(m.selectedUUID, m.selectedPID, 0))
		if m.activePane == paneDetail && m.procDetail == nil {
			cmds = append(cmds, fetchProcDetailCmd(m.selectedPID, m.selectedUUID))
		}
	}
	return m, tea.Batch(cmds...)
}

// --- Offline replay (Story 17-5) ---

func newReplayDashboardModel(reader *debug.RecordReader) dashboardModel {
	return dashboardModel{
		startTime:         time.Now(),
		connected:         false,
		recording:         make(map[string]string),
		stepExpandedIdx:   -1,
		stepFilters:       defaultStepFilters(),
		expandedAggGroups: make(map[int]bool),
		prevProcessPIDs:   make(map[types.PID]vfs.ProcInfo),
		budgetAlertSeen:   make(map[types.PID]int),
		stallSeen:         make(map[types.PID]struct{}),
		sysEventSeen:      make(map[string]struct{}),
		replayMode:        true,
		replayReader:      reader,
		replayCursor:      -1,
		replayPlaying:     false,
		replaySpeed:       1.0,
		prevReplayCursor:  -2,
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
		roots := buildProcessTree(m.processes, treeSortPID, false)
		m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)
		if len(m.treeRows) > 0 {
			m = selectProcess(m, m.treeRows[0])
		}
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
		// F5: Close debug IPC client if active.
		if fm.debugClient != nil {
			fm.debugClient.Close()
		}
		fm.client.Close()
	} else {
		client.Close()
	}
	return nil
}
