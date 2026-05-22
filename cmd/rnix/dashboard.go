package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/dashboard/alertstrip"
	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	dashboarddebug "github.com/rnixai/rnix/internal/dashboard/debug"
	"github.com/rnixai/rnix/internal/dashboard/heatmap"
	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/internal/dashboard/eval"
	"github.com/rnixai/rnix/internal/dashboard/inspector"
	"github.com/rnixai/rnix/internal/dashboard/intent"
	"github.com/rnixai/rnix/internal/dashboard/plugin"
	"github.com/rnixai/rnix/internal/dashboard/security"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/dashboard/trace"
	"github.com/rnixai/rnix/internal/dashboard/tree"
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

type dashboardModel struct {
	client       *ipc.Client
	width        int
	height       int
	activePane   paneType
	rightPane    paneType // 右侧显示的面板（默认 Timeline）
	viewMode     viewMode // 当前视图模式（默认 viewDefault，零值即默认）
	expandedPane paneType // viewExpanded 模式下展开的面板

	// Story 38.1: 3-layer key dispatcher (Layer 0 Global / Layer 1 View / Layer 2 Pane)
	dispatcher *ui.Dispatcher

	selectedPID  types.PID
	selectedUUID string
	processes    []vfs.ProcInfo

	// Story 38-5 PR2 Step 1: TreeState 抽离（13 字段聚合到 internal/dashboard/tree.TreeState）
	tree tree.TreeState

	connected    bool
	err          error
	statusMsg    string
	startTime    time.Time
	confirmKill  bool
	confirmPID   types.PID

	// Story 38-5 PR4 Step 1: TimelineState 抽离（16 字段）— spec § AC4
	// 抽出字段：AttachedPID/AttachedUUID + StepEntries/StepCursor/StepScrollTop/
	// StepDetailCache/LastFetchedStep/FetchingDetail/StepFilters/StepFilterMode/
	// StepExpandedIdx + SortAsc/ExpandMode/UIState/MigrationChecked + ExpandedAggGroups。
	// 现有 cmd/rnix 端通过 deprecated getter `TimelineState()` 间接读取，PR11 删除。
	timeline timeline.TimelineState

	// Heatmap fields (Story 17-3) — Story 38-5 PR3 Step 1: 抽离至 internal/dashboard/heatmap.HeatmapState
	heatmap heatmap.HeatmapState

	// Pane linkage & process operations (Story 17-4)
	recording    map[string]string
	statusMsgTTL int

	// Story 38-5 PR10 Step 1: InspectorState 抽离（25 字段 · spec § AC6）— Story 36-1 / 38-3
	// / 36-6 / 38-3 AC#7-8 落地全部行为契约保留。详见 internal/dashboard/inspector/state.go
	// 包级注释。包含：核心步进 9 字段 + 5-lens 视觉 6 字段 + Diff mode 7 字段 + Follow Live 2
	// 字段 + 38-3 AC#7/#8 lens marks + byte search 2 字段 = 25 字段（spec § AC6 line 165 列举）。
	inspector inspector.InspectorState

	// Story 38-5 PR10 Step 4: SearchPlugin 抽离（7 字段 · spec § AC6 + § 04 风险 3）
	// 抽出 dashboardModel.search{Mode/Query/Matches/MatchIdx/Reverse/CrossLens/NoMatchExpireAt}
	// 7 字段到 internal/dashboard/plugin.SearchPlugin。SearchPlugin 通过 plugin.Searchable
	// interface 与 InspectorModel + TimelineModel 解耦（避免直接读子 Model 私有字段）。
	// Story 36-5 / 36-6 跨 pane 搜索行为完全保留。
	search plugin.SearchPlugin

	// Story 27-5: initial PID focus from --pid flag
	initialPIDFocus types.PID

	// Story 38-5 PR5 Step 1: DetailState 抽离（4 字段 · spec § AC5）— Detail/PID/Cache/Tick · Story 27-6 落地。
	detail detail.DetailState

	// Story 38-5 PR6 Step 1: IntentState 抽离（6 字段 · spec § AC5 · 27-7 + 38-4 落地 · 38-4 P1 stable RootIntent key）
	intent intent.IntentState

	// Story 38-5 PR7 Step 1: SecurityState 抽离（5 字段 · spec § AC5 · Story 27-8 + 38-4 Alert Immune 路由保留）
	security security.SecurityState

	// Story 38-5 PR8 Step 1: TraceState 抽离（10 字段 · spec § AC5 · Story 27-9 + 38-4 waterfall bar 保留）
	trace trace.TraceState

	// Story 38-5 PR9 Step 1: EvalState 抽离（13 字段 · spec § AC5 · Story 27-10 + 38-4 evalScoreColorStyle 颜色梯度保留）
	eval eval.EvalState

	// Offline replay fields (Story 17-5)
	replayMode       bool
	replayReader     *debug.RecordReader
	replayCursor     int
	replayPlaying    bool
	replaySpeed      float64
	prevReplayCursor int

	// HeartbeatStatus fields (Story 30.8)
	heartbeatStatus *ipc.HeartbeatStatusResponse

	// Story 30.8 AC#5: Timeline aggregation — Story 38-5 PR4 Step 1: 抽离到 timeline.TimelineState.ExpandedAggGroups

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
	lastScriptEventMs  int64 // Story 43-3: watermark for Script* syscall dedup
	fetchingCompact    bool
	sysEventSeen        map[string]struct{}
	historicalSeedDone  bool // seeded EXIT events for already-dead procs on startup

	// Health counters for title bar (Story 34.2)
	errorCount int
	warnCount  int

	// Story 34.3: Process tree visual enhancement (now in tree.TreeState)

	// Story 36-2: New process highlight (now in tree.TreeState)

	// Story 34.4: Alert strip + unified timeline
	// Story 38-5 PR12 Step 1 + PR11 Step 4(a) cascade fix: 4 字段全部抽出至
	// internal/dashboard/alertstrip.AlertStripState（commit a08ae3d 解除
	// UnifiedEvent 类型 cascade 阻塞 · spec § Tasks 11.3 line 187 「4 字段
	// Expanded/Cursor/Events/JumpTarget」最终对齐）。
	alertStrip alertstrip.AlertStripState

	// Story 38-4: cross-pane linkage (see dashboard_events.go for contract)
	paneHasUnread       [paneCount]bool
	lastUnreadEventKeys map[string]struct{} // Story 38-4 P3: identity-based diff (replaces prevUnifiedEventCount); nil means "next tick syncs without marking" (PID switch / startup)
	// 注：intentTreeCollapsed 已迁入 intent.IntentState.TreeCollapsed（PR6 Step 1）· 38-4 P1 行为不变

	// Story 34.6: Debug mode — strace fusion
	// Story 38-5 PR11 Step 1 + Step 4(a) cascade fix: 14 字段全部抽出至
	// internal/dashboard/debug.DebugState（commit a08ae3d 解除 UnifiedEvent
	// 类型 cascade 阻塞 · Events/StraceEvents 完整迁出）。
	debugState dashboarddebug.DebugState

	// Story 38-5 PR11 Step 4(b) Phase 1: 11 个子 Model hook 入口（spec § AC11
	// broadcast 通道 · 8 PaneModel + 3 OverlayModel）。当前 OnSelectPID 全部
	// 是 nil-safe stub · Phase 2 后续会话逐 pane 把 handleXxxPIDChange 主体
	// 迁入对应 OnSelectPID。State 双向同步留 Phase 2（当前 stub 不读 state）。
	treeM       *tree.TreeModel
	timelineM   *timeline.TimelineModel
	heatmapM    *heatmap.HeatmapModel
	detailM     *detail.DetailModel
	intentM     *intent.IntentModel
	securityM   *security.SecurityModel
	traceM      *trace.TraceModel
	evalM       *eval.EvalModel
	inspectorM  *inspector.InspectorModel
	debugM      *dashboarddebug.DebugModel
	alertStripM *alertstrip.AlertStripModel
}

func newDashboardModel(client *ipc.Client) dashboardModel {
	uiState, _ := ui.LoadUIState()
	if uiState == nil {
		uiState = &ui.UIState{}
	}
	return dashboardModel{
		client:             client,
		dispatcher:         newDispatcher(),
		startTime:          time.Now(),
		connected:          client != nil,
		rightPane:          paneTimeline,
		recording:          make(map[string]string),
		detail:             detail.DetailState{Cache: make(map[string]*ipc.GetProcDetailResponse)}, // Story 38-5 PR5 Step 1: DetailState init
		prevProcessPIDs:    make(map[types.PID]vfs.ProcInfo),
		budgetAlertSeen:    make(map[types.PID]int),
		stallSeen:          make(map[types.PID]struct{}),
		sysEventSeen:       make(map[string]struct{}),
		// Story 38-5 PR2 Step 1: TreeState 抽离（13 字段）
		tree: tree.TreeState{
			LastEventByPID:     make(map[types.PID]time.Time),
			CollapsedDeadTrees: make(map[string]bool),
			ProcessFirstSeenAt: make(map[types.PID]time.Time),
		},
		// Story 38-5 PR4 Step 1: TimelineState 抽离（16 字段）
		timeline: timeline.TimelineState{
			StepDetailCache:   make(map[int]*ipc.GetStepDetailResponse),
			StepExpandedIdx:   -1,
			StepFilters:       defaultStepFilters(),
			ExpandedAggGroups: make(map[int]bool),
			SortAsc:           true,                       // Story 36-4: 默认升序（最新在底）
			ExpandMode:        timeline.ExpandModeCollapsed, // Story 36-4
			UIState:           uiState,                    // Story 36-4: 首次升级提示持久化状态
		},
		intent: intent.IntentState{
			TreeCollapsed: make(map[string]bool), // Story 38-4 AC#3 / P1: keyed by RootIntent
		},
		// Story 38-5 PR11 Step 1: DebugState 抽离（12 字段）— Story 34.6 落地
		debugState: dashboarddebug.DebugState{
			ShowStrace:    true, // Story 34.6: show strace events by default
			DeviceLatency: make(map[string]*deviceLatencyStats),
		},
		// Story 38-5 PR11 Step 4(b) Phase 1: 11 个子 Model hook 入口（spec § AC11）
		treeM:       tree.NewModel(),
		timelineM:   timeline.NewModel(),
		heatmapM:    heatmap.NewModel(),
		detailM:     detail.NewModel(),
		intentM:     intent.NewModel(),
		securityM:   security.NewModel(),
		traceM:      trace.NewModel(),
		evalM:       eval.NewModel(),
		inspectorM:  inspector.NewModel(),
		debugM:      dashboarddebug.NewModel(),
		alertStripM: alertstrip.NewModel(),
	}
}

func selectProcess(m dashboardModel, row flatRow) dashboardModel {
	m.selectedPID = row.Proc.PID
	m.selectedUUID = row.Proc.UUID
	return m
}

// TreeState returns the embedded tree state.
//
// Story 38-5 PR11 Step 4(b) Code Review (G2-6): originally tagged "Deprecated:
// removed in PR11" with the intent of inlining accessors after the migration.
// That plan was reversed during PR11 because the KeyLayer ActiveModesFn
// closures (registered via tree.KeyLayer / timeline.KeyLayer / etc.) read
// state through the StateProvider interfaces defined in each subpackage —
// and those interfaces are satisfied precisely by these methods. Removing
// them would break key dispatching. Treat the getters as **stable
// StateProvider implementations**, not deprecated transitional helpers.
//
// Test code may still use them to read state without importing
// internal/dashboard/tree types.
func (m dashboardModel) TreeState() tree.TreeState { return m.tree }

// HeatmapState implements heatmap.StateProvider for KeyLayer ActiveModesFn
// (Story 38-5 PR3 Step 1; retained as part of the stable StateProvider
// contract — see TreeState above for the reversal of the original
// "Deprecated: removed in PR11" plan).
func (m dashboardModel) HeatmapState() heatmap.HeatmapState { return m.heatmap }

// TimelineState implements timeline.StateProvider for KeyLayer ActiveModesFn
// (Story 38-5 PR4 Step 1; retained as a stable StateProvider — see TreeState).
//
// Allows tests to read the timeline state without importing
// internal/dashboard/timeline directly.
func (m dashboardModel) TimelineState() timeline.TimelineState { return m.timeline }

// DetailState implements detail.StateProvider (Story 38-5 PR5 Step 1; retained
// as a stable StateProvider — see TreeState).
func (m dashboardModel) DetailState() detail.DetailState { return m.detail }
func (m dashboardModel) SelectedPID() types.PID          { return m.selectedPID } // Story 38-5 PR5 Step 2 · detail.SelectedPIDProvider
func (m dashboardModel) IntentState() intent.IntentState { return m.intent }      // Story 38-5 PR6 Step 1 · intent.StateProvider (retained as stable contract)
func (m dashboardModel) SecurityState() security.SecurityState { return m.security } // Story 38-5 PR7 Step 1 · security.StateProvider (retained)
func (m dashboardModel) TraceState() trace.TraceState          { return m.trace }    // Story 38-5 PR8 Step 1 · trace.StateProvider (retained)
func (m dashboardModel) EvalState() eval.EvalState             { return m.eval }     // Story 38-5 PR9 Step 1 · eval.StateProvider (retained)
func (m dashboardModel) InspectorState() inspector.InspectorState { return m.inspector } // Story 38-5 PR10 Step 1 · inspector.StateProvider (retained)
func (m dashboardModel) DebugState() dashboarddebug.DebugState  { return m.debugState } // Story 38-5 PR11 Step 1 · dashboarddebug.StateProvider (retained)
func (m dashboardModel) AlertStripState() alertstrip.AlertStripState { return m.alertStrip } // Story 38-5 PR12 Step 1 · alertstrip.StateProvider (retained)

func (m dashboardModel) Init() tea.Cmd {
	return tickCmd()
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.dashboardTick()
	case dashboardmodel.SelectPIDMsg:
		// Story 38-5 PR11 Step 4(b)（Phase 1 + Phase 2）: spec § AC11 broadcast 路由。
		// dashboardTick pidChanged 分支 emit · 此处分发到 11 个子 Model.OnSelectPID。
		// Phase 2 双向同步：broadcastSelectPID 内含 SetState/State sync · 返回更新
		// 后的 dashboardModel + tea.Batch 收集的 cmd。
		var pidCmd tea.Cmd
		m, pidCmd = m.broadcastSelectPID(msg.PID)
		return m, pidCmd
	case tea.KeyPressMsg:
		return m.dashboardKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.viewMode == viewStepInspector {
			contentH := m.inspectorContentHeight()
			for i := range m.inspector.Viewports {
				m.inspector.Viewports[i].SetWidth(msg.Width)
				m.inspector.Viewports[i].SetHeight(contentH)
			}
		}
		return m, nil
	case heatmapProfileMsg:
		if msg.Err != nil {
			m.heatmap.Err = msg.Err
			return m, nil
		}
		m.heatmap.Err = nil
		if msg.Profile != nil {
			m.heatmap.Profile = msg.Profile
			m.heatmap.Segments = buildHeatmapSegments(msg.Profile)
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
		// UUID == "" means old daemon without UUID tracking — skip check for backward compat.
		if msg.UUID != "" && msg.UUID != m.selectedUUID {
			return m, nil
		}
		if msg.Err == nil && len(msg.Steps) > 0 {
			m = m.applyNewSteps(msg.Steps)
			if last := msg.Steps[len(msg.Steps)-1]; last.Step > m.timeline.LastFetchedStep {
				m.timeline.LastFetchedStep = last.Step
			}
		}
		// Auto-fetch detail for expanded steps (e.g., auto-expanded on error/slow)
		if cmd := m.fetchNextExpandedDetail(); cmd != nil {
			m.timeline.FetchingDetail = true
			return m, cmd
		}
		return m, nil
	case stepDetailResultMsg:
		m.timeline.FetchingDetail = false
		if msg.err == nil && msg.detail != nil {
			m.timeline.StepDetailCache[msg.step] = msg.detail
		}
		// Chain-fetch next expanded step without cached detail
		if cmd := m.fetchNextExpandedDetail(); cmd != nil {
			m.timeline.FetchingDetail = true
			return m, cmd
		}
		return m, nil
	case procDetailResultMsg:
		m, lineageCmd := handleProcDetailResult(m, msg)
		return m, lineageCmd
	case detail.ResumeLineageResultMsg:
		m = handleResumeLineageResult(m, msg)
		return m, nil
	case intentTreesMsg:
		if msg.Err != nil {
			m.intent.TreeErr = msg.Err
			return m, nil
		}
		m.intent.TreeErr = nil
		if msg.Trees == nil {
			m.intent.Trees = nil
			m.intent.FlatNodes = nil
			return m, nil
		}
		m.intent.Trees = msg.Trees.Intents
		m.intent.FlatNodes = flattenIntentTreesWithCollapse(m.intent.Trees, m.intent.TreeCollapsed)
		if m.intent.Cursor >= len(m.intent.FlatNodes) {
			m.intent.Cursor = max(0, len(m.intent.FlatNodes)-1)
		}
		return m, nil
	case immuneStatusMsg:
		if msg.Err != nil {
			m.security.ImmuneErr = msg.Err
			return m, nil
		}
		m.security.ImmuneErr = nil
		m.security.ImmuneStatus = msg.Status
		if msg.Status != nil {
			m.security.Alerts = sortAlertsByDeviation(msg.Status.Alerts)
			if m.security.Cursor >= len(m.security.Alerts) {
				m.security.Cursor = max(0, len(m.security.Alerts)-1)
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
				continue
			}
			m = m.mergeScriptEvent(ev, msg.uuid) // Story 43-3: route Script* syscalls
		}
		// Cap compact events FIFO
		if len(m.compactEvents) > maxSysEvents {
			m.compactEvents = m.compactEvents[len(m.compactEvents)-maxSysEvents:]
		}
		return m, nil
	case resumeResultMsg:
		return handleResumeResult(m, msg)
	case forkResultMsg:
		m, forkCmd := handleForkResult(m, msg)
		return m, forkCmd
	case pauseToggleMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ Pause/resume: %v", msg.err)
		} else {
			action := "Resumed"
			if msg.paused {
				action = "Paused"
			}
			if msg.affected > 1 {
				m.statusMsg = fmt.Sprintf("%s PID %d (+%d children)", action, msg.pid, msg.affected-1)
			} else {
				m.statusMsg = fmt.Sprintf("%s PID %d", action, msg.pid)
			}
		}
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	case pauseSubtreeResultMsg:
		return handlePauseSubtreeResult(m, msg), nil
	case resumeSubtreeResultMsg:
		return handleResumeSubtreeResult(m, msg), nil
	case traceListMsg:
		if msg.Err != nil {
			m.trace.Err = msg.Err
			return m, nil
		}
		m.trace.Err = nil
		m.trace.Summaries = msg.Summaries
		// Sort by StartTimeMs descending (newest first)
		sort.Slice(m.trace.Summaries, func(i, j int) bool {
			return m.trace.Summaries[i].StartTimeMs > m.trace.Summaries[j].StartTimeMs
		})
		if m.trace.Cursor >= len(m.trace.Summaries) {
			m.trace.Cursor = max(0, len(m.trace.Summaries)-1)
		}
		return m, nil
	case traceTreeMsg:
		if msg.err != nil {
			m.trace.Err = msg.err
			return m, nil
		}
		m.trace.Err = nil
		m.trace.SelectedTraceID = msg.traceID
		m.trace.SelectedSpanTree = msg.tree
		m.trace.SpanFlatNodes = flattenSpanTree(msg.tree)
		m.trace.SpanCursor = 0
		m.trace.SpanScrollOffset = 0
		if msg.tree == nil || msg.tree.Root == nil {
			// Empty trace — stay in list mode, show status message
			m.statusMsg = "此追踪无 span 数据"
			m.statusMsgTTL = statusMsgDefaultTTL
		} else {
			m.trace.ViewMode = 1
		}
		return m, nil
	case evalReputationMsg:
		if msg.Err != nil {
			m.eval.RepErr = msg.Err
			return m, nil
		}
		m.eval.RepErr = nil
		m.eval.Reputations = msg.Summaries
		// Sort by score descending
		sort.Slice(m.eval.Reputations, func(i, j int) bool {
			return m.eval.Reputations[i].Score > m.eval.Reputations[j].Score
		})
		if m.eval.RepCursor >= len(m.eval.Reputations) {
			m.eval.RepCursor = max(0, len(m.eval.Reputations)-1)
		}
		return m, nil
	case evalTopologyMsg:
		if msg.Err != nil {
			m.eval.TopoErr = msg.Err
			return m, nil
		}
		m.eval.TopoErr = nil
		m.eval.Topology = msg.Topology
		if msg.Topology != nil {
			totalItems := len(msg.Topology.Nodes) + len(msg.Topology.Edges)
			if m.eval.TopoCursor >= totalItems {
				m.eval.TopoCursor = max(0, totalItems-1)
			}
		}
		return m, nil
	case evalSynergyMsg:
		if msg.Err != nil {
			m.eval.SynErr = msg.Err
			return m, nil
		}
		m.eval.SynErr = nil
		m.eval.Synergies = msg.Combos
		if m.eval.SynCursor >= len(m.eval.Synergies) {
			m.eval.SynCursor = max(0, len(m.eval.Synergies)-1)
		}
		return m, nil
	case promptPagerMsg:
		m.timeline.FetchingDetail = false
		if msg.pid != m.selectedPID {
			return m, nil
		}
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("✗ prompt load: %v", msg.err)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil
		}
		if msg.detail != nil {
			m.timeline.StepDetailCache[msg.step] = msg.detail
			// Story 36-1: redirect to Inspector (guard: skip if already in Inspector)
			if m.viewMode != viewStepInspector {
				m2, cmd := m.enterStepInspector()
				m3 := m2.(dashboardModel)
				m3.inspector.Lens = lensSystem
				return m3, cmd
			}
		}
		return m, nil
	case inspectorDetailMsg:
		return m.handleInspectorDetailMsg(msg)
	case inspectorStepListMsg:
		return m.handleInspectorStepListMsg(msg)
	case followLiveTickMsg:
		return m.handleFollowLiveTickMsg(msg)
	default:
		// Story 34.6: Debug mode messages
		if m2, cmd, handled := m.handleDebugMsg(msg); handled {
			return m2, cmd
		}
	}
	return m, nil
}

func (m dashboardModel) dashboardTick() (tea.Model, tea.Cmd) {
	m.heatmap.TickCount++

	// Story 36-4: 首次升级到升序时展示一次提示（写入在 statusMsgTTL 衰减之前）
	m = m.maybeShowTimelineMigrationNotice()

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

	// Story 36-2: Track first-seen time for new process highlight
	seenNow := make(map[types.PID]bool, len(procs))
	nowHL := time.Now()
	for _, p := range procs {
		seenNow[p.PID] = true
		if _, exists := m.tree.ProcessFirstSeenAt[p.PID]; !exists {
			m.tree.ProcessFirstSeenAt[p.PID] = nowHL
		}
	}
	// Clean up entries for PIDs no longer in procs. No time-based cleanup —
	// the map is bounded by the number of active+visible processes and entries
	// for exited PIDs are removed immediately when they leave the procs list.
	for pid := range m.tree.ProcessFirstSeenAt {
		if !seenNow[pid] {
			delete(m.tree.ProcessFirstSeenAt, pid)
		}
	}

	roots := buildProcessTree(procs, m.tree.SortMode, m.tree.SortAsc)
	m.tree.Rows = flattenTreeWithCollapse(roots, m.tree.CollapsedDeadTrees)

	if m.tree.Cursor >= len(m.tree.Rows) {
		m.tree.Cursor = max(0, len(m.tree.Rows)-1)
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
			// In debug mode, preserve selection for dead processes with loaded events.
			// The process list may briefly omit a reaped process during TTL cleanup
			// transitions. Resetting selectedPID here cascades into handleDebugPIDChange
			// which clears all events, causing the "Waiting for events…" flicker.
			if m.debugState.Mode && m.debugState.AttachedPID == m.selectedPID && len(m.debugState.Events) > 0 {
				found = true
			}
		}
		if !found {
			m.selectedPID = 0
			m.selectedUUID = ""
		}
	}

	// Stabilize cursor after tree rebuild: if treeRows reordered and the cursor
	// now points to a different PID, relocate cursor to the previously selected PID.
	// This prevents unintentional PID changes (which clear debug events, reset detail, etc.)
	// while still allowing user-initiated cursor moves (handled in keyboard path).
	//
	// In debug mode, use debugAttachedPID as the anchor — it survives even when
	// the UUID validation above resets selectedPID to 0 (which happens when the
	// daemon's process list briefly omits a recently reaped process).
	anchorPID := m.selectedPID
	if m.debugState.Mode && anchorPID == 0 && m.debugState.AttachedPID > 0 {
		anchorPID = m.debugState.AttachedPID
	}
	if anchorPID > 0 && m.tree.Cursor < len(m.tree.Rows) {
		if m.tree.Rows[m.tree.Cursor].Proc.PID != anchorPID {
			for i, row := range m.tree.Rows {
				if row.Proc.PID == anchorPID {
					m.tree.Cursor = i
					break
				}
			}
		}
	}

	// Update selection from current cursor position (after stabilization)
	if m.tree.Cursor < len(m.tree.Rows) {
		m = selectProcess(m, m.tree.Rows[m.tree.Cursor])
	}

	// --- Unified event stream detection (Story 34.1) ---
	// On first successful tick, seed EXIT/SPAWN events for already-dead historical processes.
	// Scope to treeRows only so we don't generate events for processes from unrelated old sessions.
	if !m.historicalSeedDone && len(m.tree.Rows) > 0 {
		m.historicalSeedDone = true
		visibleProcs := make([]vfs.ProcInfo, 0, len(m.tree.Rows))
		for _, row := range m.tree.Rows {
			visibleProcs = append(visibleProcs, row.Proc)
		}
		if seedEvents := seedHistoricalSysEvents(visibleProcs); len(seedEvents) > 0 {
			m.sysEvents = append(m.sysEvents, sysEventDedup(seedEvents, m.sysEventSeen)...)
		}
	}

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
	m.unifiedEvents = mergeUnifiedEvents(m.timeline.StepEntries, m.sysEvents, m.selectedPID, m.selectedUUID, m.processes, m.timeline.SortAsc)
	m = m.applyUnreadMarks() // Story 38-4 AC#1

	// Build alert events for alert strip (Story 34.4)
	// Story 38-4 AC#4: include synthesised security alerts so the strip's
	// count badge and time-ordered list reflect immune-daemon findings.
	m.alertStrip.Events = buildAlertEventsWith(m.unifiedEvents, m.security.Alerts)
	// F5: Always clamp alertCursor to valid range (even when expanded)
	if len(m.alertStrip.Events) == 0 {
		m.alertStrip.Cursor = 0
	} else {
		visible := alertStripHeight(len(m.alertStrip.Events), m.alertStrip.Expanded)
		if m.alertStrip.Cursor >= visible {
			m.alertStrip.Cursor = max(visible-1, 0)
		}
	}

	// F1: Resolve pending alert jump target after PID change (Story 38-4 AC#2
	// extends this to route Immune targets to the Security pane cursor).
	m = m.resolveAlertJumpTarget()

	// Compute health counters for title bar (Story 34.2)
	m.errorCount, m.warnCount = computeHealthCounts(m.processes, m.unifiedEvents, m.heartbeatStatus)

	// Most active process tracking — update lastEventByPID from unified events
	now := time.Now()
	for _, ev := range m.unifiedEvents {
		if ev.PID > 0 {
			if existing, ok := m.tree.LastEventByPID[ev.PID]; !ok || ev.Timestamp.After(existing) {
				m.tree.LastEventByPID[ev.PID] = ev.Timestamp
			}
		}
	}
	activePIDs := make(map[types.PID]struct{}, len(m.processes)) // prune stale entries
	for _, p := range m.processes {
		activePIDs[p.PID] = struct{}{}
	}
	for pid := range m.tree.LastEventByPID {
		if _, ok := activePIDs[pid]; !ok {
			delete(m.tree.LastEventByPID, pid)
		}
	}
	// Auto-track: if user hasn't manually selected, follow most active process
	if !m.tree.UserManualSelect {
		var mostActivePID types.PID
		var mostActiveTime time.Time
		for pid, t := range m.tree.LastEventByPID {
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
			for i, row := range m.tree.Rows {
				if row.Proc.PID == mostActivePID {
					m.tree.Cursor = i
					m = selectProcess(m, row)
					break
				}
			}
		}
	}
	// Reset userManualSelect after 5s of all processes being silent
	if m.tree.UserManualSelect && len(m.tree.LastEventByPID) > 0 {
		allSilent := true
		for _, t := range m.tree.LastEventByPID {
			if now.Sub(t) < 5*time.Second {
				allSilent = false
				break
			}
		}
		if allSilent {
			m.tree.UserManualSelect = false
		}
	}

	cmds := []tea.Cmd{tickCmd()}

	pidChanged := m.selectedUUID != m.timeline.AttachedUUID || m.selectedPID != m.heatmap.PID
	if pidChanged {
		m2, pidCmd := m.handlePIDChange()
		m = m2
		if pidCmd != nil {
			cmds = append(cmds, pidCmd)
		}
		// Story 38-5 PR11 Step 4(b) Phase 1: spec § AC11 broadcast 通道
		// 与现有 handlePIDChange 散点处理叠加（Phase 2 后续会话逐 pane 把
		// handleXxxPIDChange 主体迁入对应 OnSelectPID · 当前 stub 返回 nil）。
		cmds = append(cmds, emitSelectPIDCmd(m.selectedPID))
	} else {
		// Story 38-5 PR11 Step 4(b) Phase 3: HeatmapModel.OnTick 真实化路径
		// 取代原 inline `fetchHeatmapCmd` 调用 · 行为字面等价（spec § AC8）.
		if cmd := m.heatmapM.OnTick(m.makeTickCtx(paneHeatmap)); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Story 38-5 PR11 Step 4(b) Phase 3: TimelineModel.OnTick 真实化路径
	// 每 tick 在 !pidChanged + Connected + SelectedPID > 0 时增量 fetch steps。
	if !pidChanged {
		if cmd := m.timelineM.OnTick(m.makeTickCtx(paneTimeline)); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Story 38-5 PR11 Step 4(b) Phase 3: DetailModel.OnTick 真实化路径
	// detailCardNeedsData logic migrated to DetailModel.OnTick (ViewMode==ViewDefault guard).
	// ctx.Active 由 makeTickCtx(paneDetail) 设置；ViewMode==viewDefault 由 OnTick 内部判断。
	{
		ctx := m.makeTickCtx(paneDetail)
		if cmd := m.detailM.OnTick(ctx); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.detail = m.detailM.State()
	}

	// Story 38-5 PR11 Step 4(b) Phase 3: IntentModel.OnTick 真实化路径
	if cmd := m.intentM.OnTick(m.makeTickCtx(paneIntent)); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Story 38-5 PR11 Step 4(b) Phase 3: SecurityModel.OnTick 真实化路径
	if cmd := m.securityM.OnTick(m.makeTickCtx(paneSecurity)); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Fetch heartbeat status when Security pane is active or in default view (reduced frequency)
	// Stall detection and health counts depend on heartbeat data regardless of active pane.
	if m.connected && m.heatmap.TickCount%5 == 0 {
		if m.activePane == paneSecurity || m.viewMode == viewDefault {
			cmds = append(cmds, fetchHeartbeatStatusCmd())
		}
	}

	// Fetch compact events for selected process (Story 34.1)
	if m.selectedPID > 0 && m.connected && !m.fetchingCompact && m.heatmap.TickCount%3 == 0 {
		m.fetchingCompact = true
		cmds = append(cmds, fetchCompactEventsCmd(m.selectedPID, m.selectedUUID))
	}

	// Story 38-5 PR11 Step 4(b) Phase 3: TraceModel.OnTick 真实化路径
	// Trace 在 viewDefault 下也需要 fetch（detail card 消费 trace 数据 ·
	// 与 cmd/rnix dashboardTick 原 line 908-911 等价）。
	{
		ctx := m.makeTickCtx(paneTrace)
		ctx.Active = ctx.Active || m.viewMode == viewDefault
		if cmd := m.traceM.OnTick(ctx); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Story 38-5 PR11 Step 4(b) Phase 3: EvalModel.OnTick 真实化路径
	// 含 SubView 路由：reputation 总是 fetch · topology 仅 SubView==1 · synergy 仅 SubView==2.
	if cmd := m.evalM.OnTick(m.makeTickCtx(paneEval)); cmd != nil {
		cmds = append(cmds, cmd)
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
	if m.debugState.Mode && m.connected && m.heatmap.TickCount%5 == 0 && m.selectedPID > 0 {
		cmds = append(cmds, m.fetchDebugCtxProfileCmd())
	}

	return m, tea.Batch(cmds...)
}

func (m *dashboardModel) applyInitialPIDFocus() {
	if m.initialPIDFocus <= 0 || len(m.tree.Rows) == 0 {
		return
	}
	found := false
	for i, row := range m.tree.Rows {
		if row.Proc.PID == m.initialPIDFocus {
			m.tree.Cursor = i
			visibleLines := m.dashboardVisibleLines()
			if visibleLines > 0 && m.tree.Cursor >= m.tree.Offset+visibleLines {
				m.tree.Offset = m.tree.Cursor - visibleLines/2
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
	// Alert strip height: alerts occupy extra lines at the bottom of the screen.
	alertH := alertStripHeight(len(m.alertStrip.Events), m.alertStrip.Expanded)
	alertOffset := 0
	if alertH > 0 {
		alertOffset = alertH + 1 // +1 for top border
	}
	// titleBar(2) + statusBar(1) + panelBorder(2) + headerLine(1) = 6
	return max(m.height-6-detailOffset-statsOffset-alertOffset, 1)
}

func (m dashboardModel) View() tea.View {
	var content string
	if m.helpOverlay {
		content = m.renderHelpOverlay()
	} else {
		content = m.renderDashboard()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
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

	titleLines := strings.Count(titleBar, "\n") + 1

	// Reserve lines for alert strip (Story 34.4)
	alertH := alertStripHeight(len(m.alertStrip.Events), m.alertStrip.Expanded)
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
	case viewStepInspector:
		mainContent = m.renderStepInspector(w, contentHeight)
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
	m.lastUnreadEventKeys = nil // patch P2: first post-switch tick syncs without flooding red dots
	m.lastScriptEventMs = 0     // Story 43-3: reset Script* watermark on every PID change
	if m.selectedPID == 0 {
		m.selectedUUID = ""
		// Story 38-5 PR11 Step 4(b) Phase 2: handleTimelinePIDChange wrapper 删除·迁至 timeline 包
		m.timeline = timeline.HandlePIDUUIDChangeWithSearch(m.timeline, &m.search, m.selectedPID, m.selectedUUID)
		// Story 38-5 PR11 Step 4(b) Phase 2: handleHeatmapPIDChange wrapper 删除·inline 调用·行为零变化
		m.heatmap = heatmap.HandlePIDChange(m.heatmap)
		m.timeline.AttachedPID = 0
		m.heatmap.PID = 0
		return m, nil
	}
	m.timeline = timeline.HandlePIDUUIDChangeWithSearch(m.timeline, &m.search, m.selectedPID, m.selectedUUID)
	m.heatmap = heatmap.HandlePIDChange(m.heatmap) // Story 38-5 PR11 Step 4(b) Phase 2: inline 化
	m.timeline.AttachedPID = m.selectedPID
	m.heatmap.PID = m.selectedPID

	// Reset compact event tracking for new process (Story 34.1). lastScriptEventMs reset above.
	m.compactEvents = nil
	m.lastCompactEventMs = 0
	m.fetchingCompact = false

	// Filter out stale compact UnifiedEvents from sysEvents on PID change (F4)
	filtered := m.sysEvents[:0]
	for _, ev := range m.sysEvents {
		if ev.Type != EventCompact && ev.Type != EventScript {
			filtered = append(filtered, ev)
		}
	}
	m.sysEvents = filtered

	m.statusMsg = fmt.Sprintf("Switched to PID %d, fetching steps...", m.selectedPID)
	m.statusMsgTTL = statusMsgDefaultTTL

	// Detail pane: cache 命中复用 / 否则清空（Story 28-4 AC-4 PID 复用契约）
	// Story 38-5 PR11 Step 4(b) Phase 2: 6 行 inline → detail.HandlePIDChangeWithCache 一行
	m.detail = detail.HandlePIDChangeWithCache(m.detail, m.selectedPID, m.selectedUUID)

	var cmds []tea.Cmd
	if m.connected {
		cmds = append(cmds, fetchHeatmapCmd(m.selectedPID))
		cmds = append(cmds, fetchStepsCmd(m.selectedUUID, m.selectedPID, 0))
		if m.activePane == paneDetail && m.detail.Detail == nil {
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
		// Story 38-5 PR4 Step 1: TimelineState 抽离（16 字段）
		timeline: timeline.TimelineState{
			StepExpandedIdx:   -1,
			StepFilters:       defaultStepFilters(),
			ExpandedAggGroups: make(map[int]bool),
		},
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
		// Story 38-5 PR11 Step 4(b) Phase 1: 11 个子 Model hook 入口（spec § AC11）
		treeM:       tree.NewModel(),
		timelineM:   timeline.NewModel(),
		heatmapM:    heatmap.NewModel(),
		detailM:     detail.NewModel(),
		intentM:     intent.NewModel(),
		securityM:   security.NewModel(),
		traceM:      trace.NewModel(),
		evalM:       eval.NewModel(),
		inspectorM:  inspector.NewModel(),
		debugM:      dashboarddebug.NewModel(),
		alertStripM: alertstrip.NewModel(),
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
			if m.heatmap.TickCount%int(1.0/m.replaySpeed) == 0 {
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
		m.tree.Rows = flattenTreeWithCollapse(roots, m.tree.CollapsedDeadTrees)
		if len(m.tree.Rows) > 0 {
			m = selectProcess(m, m.tree.Rows[0])
		}
		m.heatmap.Profile = buildReplayHeatmap(m.replayReader, m.replayCursor)
		if m.heatmap.Profile != nil {
			m.heatmap.Segments = buildHeatmapSegments(m.heatmap.Profile)
		} else {
			m.heatmap.Segments = nil
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
			if m.tree.Cursor > 0 {
				m.tree.Cursor--
				if m.tree.Cursor < len(m.tree.Rows) {
					m = selectProcess(m, m.tree.Rows[m.tree.Cursor])
				}
				if m.tree.Cursor < m.tree.Offset {
					m.tree.Offset = m.tree.Cursor
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
				if m.tree.Cursor > 0 {
					m.tree.Cursor--
					if m.tree.Cursor < len(m.tree.Rows) {
						m = selectProcess(m, m.tree.Rows[m.tree.Cursor])
					}
					if m.tree.Cursor < m.tree.Offset {
						m.tree.Offset = m.tree.Cursor
					}
				}
			case "down", "j":
				if m.tree.Cursor < len(m.tree.Rows)-1 {
					m.tree.Cursor++
					if m.tree.Cursor < len(m.tree.Rows) {
						m = selectProcess(m, m.tree.Rows[m.tree.Cursor])
					}
					if visibleLines > 0 && m.tree.Cursor >= m.tree.Offset+visibleLines {
						m.tree.Offset = m.tree.Cursor - visibleLines + 1
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

	// Story 38.2 AC#2: prefix the [REPLAY] mode label (orange ColorReplay) so the
	// "what mode am I in" question stays answerable at a glance across views.
	// Code-review patch (Decision 1, 2026-05-03): renamed legacy "REPLAY: %s" to
	// "rec: %s" so the literal "REPLAY" no longer appears twice within ~12 cols.
	modeLabel := m.renderModeLabel()

	if m.statusMsg != "" {
		return fmt.Sprintf("  %s%s rec: %s  %s  |  %s", modeLabel, indicator, recordID, progress, m.statusMsg)
	}
	return fmt.Sprintf("  %s%s rec: %s  %s  |  Space:Play/Pause  [/]:Speed  ,/.:Step  0:Start  $:End  q:Quit",
		modeLabel, indicator, recordID, progress)
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
		if fm.debugState.Client != nil {
			fm.debugState.Client.Close()
		}
		fm.client.Close()
	} else {
		client.Close()
	}
	return nil
}
