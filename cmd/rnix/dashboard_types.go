package main

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

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
)

type viewMode int

const (
	viewDefault  viewMode = iota // 默认：Tree + Timeline + 底部面板
	viewExpanded                 // 数字键展开某面板
	viewLLM                      // L：全屏 LLM 对话查看器（Story 29.6 实现）
	viewHistory                  // H：全屏历史进程列表（Story 29.5 实现）
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

// --- Message types ---

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

// --- LLM Viewer types (Story 29-6) ---

type llmViewerMsg struct {
	pid    types.PID
	step   int
	detail *ipc.GetStepDetailResponse
	err    error
}

type llmStepListMsg struct {
	pid   types.PID
	steps []ipc.StepSummaryWire
	total int
	err   error
}

// --- History view types (Story 29-5) ---

type historyProcsMsg struct {
	procs []vfs.ProcInfo
	err   error
}

// --- Timeline types ---

// promptPagerTab represents the active tab in the Prompt Viewer.
type promptPagerTab int

const (
	promptTabMessages promptPagerTab = iota
	promptTabSystem
	promptTabTools
)

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

// --- Prompt Pager styles (Story 27-4) ---

var (
	promptRoleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	promptRoleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("#6BCB77"))
	promptRoleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B9BD5"))
	promptRoleTool      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD93D"))
)

const promptContentTruncateLimit = 2000

// --- Focus Card types (Story 29-3) ---

type focusCardState struct {
	pid       types.PID
	agent     string
	state     types.ProcessState
	elapsed   string
	isHistory bool
	exitCode  int

	// Tokens card data
	totalTokens int
	tokenBudget int
	tokenRate   string
	stepCount   int

	// Context card data
	systemPct    float64
	userPct      float64
	assistantPct float64
	toolPct      float64

	// Status card data
	skills  []string
	devices []string
	ppid    types.PID

	// Intent card data
	intentTasks []intentMiniTask

	// Trace card data
	spanCount int
	avgLatMs  float64
	errCount  int

	// Alerts card data
	alertCount int
	topAlerts  []string

	// Result card data (Dead processes only)
	resultText string

	// Historical snapshot info (Dead processes)
	livedRange string
}

type intentMiniTask struct {
	name  string
	state string
}
