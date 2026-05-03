package main

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// --- Severity levels for UnifiedEvent (Story 34.1 AC#1) ---

const (
	SevInfo     = 0
	SevWarn     = 1
	SevError    = 2
	SevCritical = 3
)

// --- Event type constants for UnifiedEvent (Story 34.1 AC#1) ---

const (
	EventStep    = "step"
	EventCompact = "compact"
	EventBudget  = "budget"
	EventSpawn   = "spawn"
	EventExit    = "exit"
	EventStall   = "stall"
	EventImmune  = "immune"
	EventError   = "error"
	EventSyscall = "syscall"
)

// UnifiedEvent merges reasoning steps and system events into a single type
// for the unified event stream (Story 34.1). This is a Dashboard-layer type only.
type UnifiedEvent struct {
	Type      string
	Severity  int
	Timestamp time.Time
	PID       types.PID
	UUID      string
	Summary   string
	Detail    string
	StepEntry *stepEntry
	RawEvent  *ipc.SyscallEventWire
	// IsSynthetic flags rows that were synthesised on the dashboard side
	// (currently only synthSecurityAlerts for AC#4) — buildAlertEvents
	// skips the TTL filter for these so the alert strip lifecycle is
	// driven by the upstream IPC list rather than wall-clock TTL.
	IsSynthetic bool
}

// UnifiedEventSlice implements sort.Interface, sorting by Timestamp descending (newest first).
type UnifiedEventSlice []UnifiedEvent

func (s UnifiedEventSlice) Len() int           { return len(s) }
func (s UnifiedEventSlice) Less(i, j int) bool { return s[i].Timestamp.After(s[j].Timestamp) }
func (s UnifiedEventSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// --- compactEventsMsg is the tea.Msg for async compact event fetching (Story 34.1) ---

type compactEventsMsg struct {
	pid    types.PID
	uuid   string
	events []ipc.SyscallEventWire
	err    error
}

// --- Pane and category enums ---

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
	// paneCount sentinel; MUST stay last. Used to size paneHasUnread
	// so adding a new pane up top forces a compile-time review of
	// every pane-indexed table rather than a silent OOB no-op
	// (Story 38-4 code-review patch P6).
	paneCount
)

type viewMode int

const (
	viewDefault  viewMode = iota // 默认：Tree + Timeline + 底部面板
	viewExpanded                 // 数字键展开某面板
	viewStepInspector            // L：全屏 Step Inspector（Story 36.1, replaces LLM Viewer + Prompt Pager）
	viewHistory // kept for iota stability; no longer used for the H key
	viewDebug                    // d：Debug 模式（Story 34.6 实现）
)

const colorIPC = "#9B59B6"

const statusMsgDefaultTTL = 4

const slowStepThresholdMs = 1000.0

// --- Step detail types ---

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

// Story 36-4: Timeline expand mode 三态
type timelineExpandMode int

const (
	expandModeCollapsed   timelineExpandMode = 0
	expandModeExpanded    timelineExpandMode = 1
	expandModeErrorsOnly  timelineExpandMode = 2
)

// --- Message types ---

type stepListMsg struct {
	uuid  string
	pid   types.PID
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

type heartbeatStatusMsg struct {
	status *ipc.HeartbeatStatusResponse
	err    error
}

type resumeResultMsg struct {
	result *ipc.ResumeResponse
	err    error
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

// --- Step Inspector types (Story 36-1, replaces LLM Viewer + Prompt Pager) ---

type inspectorLens int

const (
	lensConversation inspectorLens = iota // ❶ Message flow
	lensSystem                            // ❷ System prompt
	lensToolIO                            // ❸ Tool call details
	lensMeta                              // ❹ Metadata
	lensRawJSON                           // ❺ Raw JSON
)

const inspectorLensCount = 5

type inspectorDetailMsg struct {
	pid    types.PID
	uuid   string
	step   int
	detail *ipc.GetStepDetailResponse
	err    error
}

type inspectorStepListMsg struct {
	pid   types.PID
	uuid  string
	steps []ipc.StepSummaryWire
	total int
	err   error
}

// --- Timeline types ---

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

type pauseToggleMsg struct {
	pid      types.PID
	affected int
	paused   bool // true=SIGPAUSE, false=SIGRESUME
	err      error
}

// --- Prompt Pager styles (Story 27-4) ---

var (
	promptRoleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	promptRoleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("#6BCB77"))
	promptRoleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B9BD5"))
	promptRoleTool      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD93D"))
)

// Inspector truncation threshold: show first 10k chars, then truncation notice
const inspectorTruncateThreshold = 10000
