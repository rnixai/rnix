package main

import (
	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/internal/dashboard/eval"
	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/dashboard/heatmap"
	"github.com/rnixai/rnix/internal/dashboard/inspector"
	"github.com/rnixai/rnix/internal/dashboard/intent"
	"github.com/rnixai/rnix/internal/dashboard/security"
	"github.com/rnixai/rnix/internal/dashboard/status"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/dashboard/trace"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// --- Severity levels for UnifiedEvent (Story 34.1 AC#1) ---
//
// Story 38-5 PR11 Step 4(a) (2026-05-04): Migrated to internal/dashboard/event.
// cmd/rnix retains const aliases via const SevXxx = event.SevXxx so all 155
// callsites continue to work unchanged (zero behavior change · pure code motion).

const (
	SevInfo     = event.SevInfo
	SevWarn     = event.SevWarn
	SevError    = event.SevError
	SevCritical = event.SevCritical
)

// --- Event type constants for UnifiedEvent (Story 34.1 AC#1) ---
//
// Story 38-5 PR11 Step 4(a): Migrated to internal/dashboard/event with const
// alias re-export pattern (same as Severity above).

const (
	EventStep      = event.EventStep
	EventCompact   = event.EventCompact
	EventBudget    = event.EventBudget
	EventSpawn     = event.EventSpawn
	EventExit      = event.EventExit
	EventStall     = event.EventStall
	EventImmune    = event.EventImmune
	EventError     = event.EventError
	EventSyscall   = event.EventSyscall
	EventScript    = event.EventScript    // Story 43-3: script trace events from ScriptExecutor
	EventThinking  = event.EventThinking  // Story 60.2: folded DriverThinking aggregation rows
	EventToolInput = event.EventToolInput // Story 65.3: folded DriverToolCall input_delta aggregation rows
)

// UnifiedEvent merges reasoning steps and system events into a single type
// for the unified event stream (Story 34.1).
//
// Story 38-5 PR11 Step 4(a) (2026-05-04): Migrated to internal/dashboard/event.
// cmd/rnix retains a type alias `type UnifiedEvent = event.UnifiedEvent` so all
// 155 callsites continue to work unchanged (zero behavior change · pure code
// motion). New code in cmd/rnix should still use UnifiedEvent for readability;
// new code in internal/dashboard/* should use event.UnifiedEvent directly.
type UnifiedEvent = event.UnifiedEvent

// UnifiedEventSlice implements sort.Interface, sorting by Timestamp descending
// (newest first).
//
// Story 38-5 PR11 Step 4(a): Type alias to event.UnifiedEventSlice; behavior
// unchanged.
type UnifiedEventSlice = event.UnifiedEventSlice

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
	viewDefault       viewMode = iota // 默认：Tree + Timeline + 底部面板
	viewExpanded                      // 数字键展开某面板
	viewStepInspector                 // L：全屏 Step Inspector（Story 36.1, replaces LLM Viewer + Prompt Pager）
	viewHistory                       // kept for iota stability; no longer used for the H key
	viewDebug                         // d：Debug 模式（Story 34.6 实现）
)

// colorIPC is the canonical [INSPECTOR] mode label color.
//
// Story 38-5 PR11 Step 4(c)：renderModeLabel 迁出后 cmd/rnix 端无直接 caller·
// 唯一权威值由 internal/dashboard/status.ColorIPC 持有。这里保留 alias 同步
// 让 cmd/rnix 历史 grep 检索（"colorIPC"）+ 任何潜在外部 caller 仍可工作；
// status 子包内有 TestColorIPC_Constant 守住值漂移。
//
//nolint:unused // 单一权威迁至 internal/dashboard/status.ColorIPC · alias 保留契约
const colorIPC = status.ColorIPC

const statusMsgDefaultTTL = 4

// statusGlyphWarn / statusGlyphFail return the leading glyph for a statusMsg,
// honouring RNIX_ASCII=1 (see internal/ui.StateSymbol for the same pattern).
// The dashboard's statusMsg strings hardcoded these glyphs historically; these
// helpers route them through the one authoritative ASCII switch so a terminal
// without Unicode support does not render replacement boxes in the status bar.
func statusGlyphWarn() string {
	if ui.IsASCIIMode() {
		return "!"
	}
	return "⚠"
}

func statusGlyphFail() string {
	if ui.IsASCIIMode() {
		return "x"
	}
	return "✗"
}

const slowStepThresholdMs = 1000.0

// defaultProcPageSize is the dashboard's list_all_procs page size (Story 34.8).
// At the measured ~1.28 KB/proc mean a 100-proc page is ≈128 KB — an order of
// magnitude under the 1 MB legacy scanner frame and far under the new 64 MB
// buffer. A long-running daemon's ~900 historical procs span ~9 pages, fully
// loadable by scrolling the tree to the bottom.
const defaultProcPageSize = 100

// procPaging is the dashboard's list_all_procs pagination cursor (Story 34.8).
// Aggregated into one struct (not loose dashboardModel fields) per the field-count
// guard's sub-state convention. The dashboard pages list_all_procs
// "most-recent-first" and merges pages by UUID into dashboardModel.processes so a
// single wire response stays well under the IPC scanner buffer (root cause: a
// 1.11 MB full response tripped the 1 MB frame → "token too long" → blank UI).
type procPaging struct {
	PageSize    int  // page size for ListAllProcsPaged (0 → defaultProcPageSize)
	LoadedPages int  // pages fetched each tick (≥1); grows on scroll-to-bottom
	Total       int  // deduped total process count (from pagination metadata)
	HasMore     bool // whether older pages remain beyond the loaded range
}

// --- Step detail types ---
//
// Story 38-5 PR4 Step 1: stepDetailLevel / stepEntry / timelineExpandMode 类型迁出至
// internal/dashboard/timeline，cmd/rnix 端通过 alias 保留旧名（与 PR2 flatRow / PR3 heatmapSegment
// 同模式 · 让现有代码 + 测试 grep 字符串零变化）。

// stepDetailLevel 是 timeline.StepDetailLevel 的 alias（用于 atdd_29_1 grep 契约 +
// cmd/rnix 端简化访问 levelSummary/Expanded/Debug 常量；通过 levelXxx 常量隐式使用）。
//
//nolint:unused // 通过 levelXxx 常量隐式引用
type stepDetailLevel = timeline.StepDetailLevel

const (
	levelSummary  = timeline.LevelSummary
	levelExpanded = timeline.LevelExpanded
	levelDebug    = timeline.LevelDebug
)

// stepEntry 是 timeline.StepEntry 的 alias，让 TimelineState.StepEntries 字段
// 类型在 cmd/rnix 端可直接以 []stepEntry 赋值（避免类型转换 wrapper）。
//
// 注意：alias 形式 `type stepEntry = timeline.StepEntry` 不包含 "struct" 关键字，atdd_29_1
// 的字面契约需放宽（详见 atdd_29_1_dashboard_file_splitting_test.go 行 334 注释）。
type stepEntry = timeline.StepEntry

// timelineExpandMode 是 timeline.TimelineExpandMode 的 alias（Story 36-4 三态 · 通过
// expandModeXxx 常量隐式使用）。
//
//nolint:unused // 通过 expandModeXxx 常量隐式引用
type timelineExpandMode = timeline.TimelineExpandMode

const (
	expandModeCollapsed  = timeline.ExpandModeCollapsed
	expandModeExpanded   = timeline.ExpandModeExpanded
	expandModeErrorsOnly = timeline.ExpandModeErrorsOnly
)

// --- Message types ---

// stepListMsg is a type alias to timeline.StepListMsg (Story 38-5 Code Review G3-1 fix).
//
// Originally a private struct duplicated in cmd/rnix; aliased to the canonical
// internal/dashboard/timeline.StepListMsg so messages produced by both
// cmd/rnix.fetchStepsCmd and timeline.FetchStepsCmd (TimelineModel.OnTick path)
// route to the same `case stepListMsg` in dashboard.go::Update — fixing a silent
// drop where TimelineModel.OnTick → timeline.StepListMsg never matched the case.
//
// ATDD 29.1 grep contract `"type stepListMsg"` still passes (alias declaration
// matches the substring). Field access uses Pascal case (UUID/PID/Steps/Total/Err).
type stepListMsg = timeline.StepListMsg

type stepDetailResultMsg struct {
	step   int
	detail *ipc.GetStepDetailResponse
	err    error
}

// procDetailResultMsg is a type alias to detail.DetailResultMsg (Story 38-5
// Code Review G3-2 fix). See stepListMsg above for rationale.
//
// Field access: PID / UUID / Detail / Err (Pascal case).
type procDetailResultMsg = detail.DetailResultMsg

// --- Intent tree types (Story 27-7) ---

// Story 38-5 PR6 Step 1: intentFlatNode 类型迁出至 internal/dashboard/intent.IntentFlatNode。
// 此处保留 alias 让旧代码（dashboard_intent.go / atdd_36_5 测试 / atdd_29_1 grep 契约）零行为变化。
// 字段访问需用大写（TreeIndex/NodeID/Indent/Node/IsTreeHeader/IsCollapsed/TreeWire）。
//
//nolint:unused // alias 通过 ATDD 29.1 line 344 grep 契约 "type intentFlatNode" + 测试构造 []intentFlatNode{} 隐式使用
type intentFlatNode = intent.IntentFlatNode

type heartbeatStatusMsg struct {
	status *ipc.HeartbeatStatusResponse
	err    error
}

type resumeResultMsg struct {
	result *ipc.ResumeResponse
	err    error
}

// intentTreesMsg 是 intent.TreesMsg 的 type alias（Story 38-5 PR11 Step 4(b) Phase 3）。
//
// PR6 落地的 cmd/rnix-private struct 字段（trees/err 小写）已迁出至 internal/dashboard/intent
// 包内 TreesMsg（公开字段 Trees/Err）· 与 heatmap.ProfileMsg 同模式。
type intentTreesMsg = intent.TreesMsg

// --- Security pane types (Story 27-8) ---

// immuneStatusMsg 是 security.ImmuneStatusMsg 的 type alias（Story 38-5 PR11 Step 4(b) Phase 3）。
type immuneStatusMsg = security.ImmuneStatusMsg

// --- Trace pane types (Story 27-9) ---

// traceListMsg 是 trace.ListMsg 的 type alias（Story 38-5 PR11 Step 4(b) Phase 3）。
type traceListMsg = trace.ListMsg

type traceTreeMsg struct {
	traceID string
	tree    *ipc.SpanTreeWire
	err     error
}

// Story 38-5 PR8 Step 1: spanFlatNode 类型迁出至 internal/dashboard/trace.SpanFlatNode。
// alias 让旧代码（dashboard_trace.go / atdd_27_9 测试 / atdd_29_1 grep 契约）零行为变化。
// 字段访问需用大写（SpanID/PID/Name/DurMs/Tokens/Status/Depth/Prefix/IsRoot）。
//
//nolint:unused // alias 通过 ATDD 29.1 grep 契约 + 测试构造 []spanFlatNode{} 隐式使用
type spanFlatNode = trace.SpanFlatNode

// --- Eval pane types (Story 27-10) ---

// evalReputationMsg / evalTopologyMsg / evalSynergyMsg are eval.XxxMsg type
// aliases (Story 38-5 PR11 Step 4(b) Phase 3).
type evalReputationMsg = eval.ReputationMsg
type evalTopologyMsg = eval.TopologyMsg
type evalSynergyMsg = eval.SynergyMsg

type promptPagerMsg struct {
	pid    types.PID
	step   int
	detail *ipc.GetStepDetailResponse
	err    error
}

// --- Step Inspector types (Story 36-1, replaces LLM Viewer + Prompt Pager) ---
//
// Story 38-5 PR10 Step 1: 类型迁出至 internal/dashboard/inspector，cmd/rnix 端
// 通过 type alias + const ref 保留旧名零行为变化（与 PR2 flatRow / PR3
// heatmapSegment / PR4 stepEntry / PR6 intentFlatNode / PR8 spanFlatNode 同模式）。

// inspectorLens alias 保留旧名 · ATDD 测试 grep 字符串 / 现有 caller 零行为变化。
//
//nolint:unused // 通过 lensConversation/lensSystem/... 常量隐式使用。
type inspectorLens = inspector.Lens

// 5-lens 常量保留旧名 · 38-3 落地视觉规则的 5 个 case 分支依赖此名称。
const (
	lensConversation = inspector.LensConversation // ❶ Message flow
	lensSystem       = inspector.LensSystem       // ❷ System prompt
	lensToolIO       = inspector.LensToolIO       // ❸ Tool call details
	lensMeta         = inspector.LensMeta         // ❹ Metadata
	lensRawJSON      = inspector.LensRawJSON      // ❺ Raw JSON
	lensRaw          = inspector.LensRaw          // ❻ Raw I/O (Story 56.4)
)

const inspectorLensCount = inspector.LensCount

type inspectorDetailMsg struct {
	pid    types.PID
	uuid   string
	step   int
	detail *ipc.GetStepDetailResponse
	err    error
}

// inspectorRawMsg carries a lazily-fetched raw LLM capture for one step
// (Story 56.4 · CAP-3 路② Raw lens 懒加载回填). capture==nil = no raw record
// for this step (cached as a negative hit to avoid re-fetching). parseErrors
// carries the malformed-line count surfaced from the read backend (56.4 review
// decision 1→a) so the lens can show "N line(s) skipped" like strace --raw.
type inspectorRawMsg struct {
	pid         types.PID
	uuid        string
	step        int
	capture     *vfs.RawCapture
	parseErrors int
	err         error
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
//
// Story 38-5 PR3 Step 1: 类型定义迁出至 internal/dashboard/heatmap，cmd/rnix 端通过 alias 保留
// 旧名字让现有代码（包括测试 grep 字符串）零变化。这与 PR2 处理 flatRow / treeNode 的方式一致：
// spec § Project Structure Notes 字面要求"保留 cmd/rnix"，但实际只有 Heatmap pane 使用，按"必要扩边界"
// 原则迁入子包 + alias 兼容，避免 internal → cmd/rnix 反向 import。

type segmentKind = heatmap.SegmentKind

const (
	segSystem    = heatmap.SegSystem
	segSkill     = heatmap.SegSkill
	segTool      = heatmap.SegTool
	segUser      = heatmap.SegUser
	segAssistant = heatmap.SegAssistant
	segLeaked    = heatmap.SegLeaked
)

type activityLevel = heatmap.ActivityLevel

const (
	actActive = heatmap.ActActive
	actWarm   = heatmap.ActWarm
	actCold   = heatmap.ActCold
	actLeaked = heatmap.ActLeaked
)

// heatmapSegment 是 internal/dashboard/heatmap.Segment 的 type alias，让 HeatmapState.Segments 字段
// 类型在 cmd/rnix 端可直接以 []heatmapSegment 赋值（避免类型转换 wrapper）。
//
// 注意：alias 形式 `type heatmapSegment = heatmap.Segment` 不包含 "struct" 关键字，atdd_29_1
// 的字面契约需放宽（详见 atdd_29_1_dashboard_file_splitting_test.go 行 335 注释）。
type heatmapSegment = heatmap.Segment

// heatmapProfileMsg 是 heatmap.ProfileMsg 的 type alias（Story 38-5 PR11 Step 4(b) Phase 3）。
//
// PR3 落地的 cmd/rnix-private struct 字段（profile/err 小写）已迁出至 internal/dashboard/heatmap
// 包内 ProfileMsg（公开字段 Profile/Err · 与 cmd/rnix.fetchHeatmapCmd 同模式）。本 alias
// 让 dashboard.go::Update case heatmapProfileMsg 路由零修改通过。
//
// **重要**：alias 字段公开化迁移（profile→Profile, err→Err）已在 dashboard.go::Update
// 内同步更新（msg.Profile, msg.Err）· 与 PR3 heatmapSegment 同模式。
type heatmapProfileMsg = heatmap.ProfileMsg

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

// pauseSubtreeResultMsg — Story 44.4: result of dashboard `p` → client.PauseSubtree.
// Distinct from the legacy pauseToggleMsg (SignalTree) so the resume/pause
// handler ATDD can assert the new subtree command identity.
type pauseSubtreeResultMsg struct {
	pid      types.PID
	affected int
	err      error
}

// resumeSubtreeResultMsg — Story 44.4: result of dashboard `r` on a Suspended
// process → client.ResumeSubtree. Distinct from resumeResultMsg (single-process
// ResumeWithOptsV3, kept for Dead/Zombie Epic 42 UUID 续跑).
type resumeSubtreeResultMsg struct {
	pid      types.PID
	affected int
	skipped  int
	err      error
}

// --- Prompt Pager styles (Story 27-4) ---
//
// Story 38-5 PR11 Step 4(c)：promptRoleSystem/User/Assistant/Tool 全部迁出至
// internal/dashboard/timeline/role.go 包内私有 var · cmd/rnix 端不再持有 ·
// 调用方通过 timeline.PromptRoleForRole / timeline.FormatRoleTag 访问.

// Inspector truncation threshold: show first 10k chars, then truncation notice.
//
// Story 38-5 PR11 Step 4(a-2): 主常量迁出至 internal/dashboard/inspector.TruncateThreshold。
// 本端保留 alias 以兼容 cmd/rnix 内部 caller（dashboard_inspector.go System/Conv lens）。
const inspectorTruncateThreshold = inspector.TruncateThreshold
