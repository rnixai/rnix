// Package event — UnifiedEvent 与相关类型/常量（Story 38-5 PR11 Step 4(a)）。
//
// 本包从 cmd/rnix/dashboard_types.go 迁出 UnifiedEvent 类型族，作为 PR11 Step 4
// 拆分的基础（spec § 04 风险 5 中断保险原则 · 本 PR 是 5 个 PR11 Step 4 子任务的
// 第一个）。
//
// 设计动机（PR11 Step 4(a) · 2026-05-04）：
//
//   - 在 PR11 Step 1（DebugState 抽出）和 PR12 Step 1（AlertStripState 抽出）中，
//     `Events []UnifiedEvent` 与 `JumpTarget *UnifiedEvent` 因 cmd/rnix-private
//     UnifiedEvent 类型 cascade 阻塞而保留在 dashboardModel；
//   - 完整迁出这些字段（最终 dashboardModel 字段数 ≤ 30 · spec § AC8）需先把
//     UnifiedEvent 类型族搬到 internal/dashboard 子包；
//   - 同时也解锁 PR12 中 buildAlertEvents/synthSecurityAlerts/buildAlertEventsWith/
//     alertCountBadge/renderAlertStrip 等 helper 的迁出（这些 helper 都吃
//     UnifiedEvent · 不能跨 cmd/rnix 边界）；
//   - PR11 Step 4 的 render 主体迁出 11 个子 Model（dashboard.go ≤ 600 行 ·
//     spec § AC9）也需要 UnifiedEvent 在 internal/dashboard 可用。
//
// 依赖方向（无循环）：
//
//   internal/dashboard/event
//     ├── internal/types       (PID)
//     ├── internal/dashboard/timeline (StepEntry · UnifiedEvent.StepEntry 字段)
//     └── ipc                  (SyscallEventWire · UnifiedEvent.RawEvent 字段)
//
//   cmd/rnix
//     ├── internal/dashboard/event (本包 · 通过 type alias 让旧代码引用不变)
//     └── ...
//
// 兼容策略（cmd/rnix 端 155 处引用零修改）：
//
//   - cmd/rnix/dashboard_types.go 通过 `type UnifiedEvent = event.UnifiedEvent`
//     type alias 保留旧名（非新类型 · cmd/rnix 端 `m.unifiedEvents` 等代码继续
//     工作 · 无需改 import 或类型声明）；
//   - 所有 SevXxx / EventXxx 常量通过 `const SevInfo = event.SevInfo` 形式 re-export；
//   - UnifiedEventSlice 同模式 alias。
//
// **行为契约（Story 34.1 落地的 unified event stream · 38-2 alertCountBadge 行为
// 契约 · 38-4 IsSynthetic flag 等）全部保留**。本 PR 是 0-行为变更的纯重构
// （pure code motion）。
package event

import (
	"time"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// --- Severity levels for UnifiedEvent (Story 34.1 AC#1) ---

// Severity buckets for UnifiedEvent. Values are stable integer constants used
// as compare keys throughout the dashboard (alert strip ordering, title bar
// counters, mode strip badges).
//
// Ordering invariant: SevInfo < SevWarn < SevError < SevCritical (≤ comparison
// is meaningful · used in buildAlertEvents to filter ≥ SevWarn · alertCountBadge
// uses ≥ SevError to bucket "errors").
const (
	SevInfo     = 0
	SevWarn     = 1
	SevError    = 2
	SevCritical = 3
)

// --- Event type constants for UnifiedEvent (Story 34.1 AC#1) ---

// Event type string constants. Used as discriminator in UnifiedEvent.Type and
// for routing decisions (38-4 AC#2: EventImmune routes to Security pane;
// others route to Timeline).
//
// **Adding new event types**: must coordinate with:
//   - cmd/rnix/dashboard_alerts.go::synthSecurityAlerts (Immune routing)
//   - cmd/rnix/dashboard_events.go::mergeUnifiedEvents (severity assignment)
//   - cmd/rnix/dashboard_title.go (errorCount/warnCount tally)
//   - cmd/rnix/dashboard_keylayers.go::a-key handler (alert strip filter)
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
	// EventScript — Script trace events emitted by ScriptExecutor (Story 43-2/43-3).
	// Drives the 5 ScriptEventKind syscalls (ScriptStmtBegin/End, ScriptSpawn,
	// ScriptWhileIter, ScriptCondition) emitted under script-runner (SkipReasonLoop=true)
	// processes. Routed to the Timeline pane with blue (#5B9BD5) styling so users
	// can see "L47 ▸ spawn / L12 ↻ while iter=3" control-flow state at a glance
	// when a long-lived ash script appears stuck.
	EventScript = "script"
	// EventThinking — Story 60.2: dashboard debug pane 端 DriverThinking 聚合块的
	// 合成行类型（非来自 driver/kernel · 由 debug.CollapseThinkingGroups 投影产生）。
	// 一段连续 DriverThinking syscall 事件折叠成单个 EventThinking「折叠摘要行」
	// （RawEvent != nil · 携带 group 首事件 ts 作展开键）；展开时额外投影出若干
	// EventThinking「正文文本行」（RawEvent == nil · Summary 为缩进后的思考全文分行）。
	// 防刷屏：API driver 单会话 DriverThinking 可达 14841 条 · 折叠后仅占 1 行。
	EventThinking = "thinking"
)

// UnifiedEvent merges reasoning steps and system events into a single type
// for the unified event stream (Story 34.1). This is a Dashboard-layer type only.
//
// Story 38-5 PR11 Step 4(a) (2026-05-04): Migrated from cmd/rnix/dashboard_types.go
// to internal/dashboard/event. cmd/rnix retains a `type UnifiedEvent =
// event.UnifiedEvent` alias for backwards compatibility (155 callsites unchanged).
//
// **StepEntry field type cascade**:
//   - Original: `*stepEntry` where `type stepEntry = timeline.StepEntry` in cmd/rnix;
//   - Migrated: `*timeline.StepEntry` directly (cmd/rnix.stepEntry alias still
//     resolves to the same underlying type · no behavior change).
//
// Field semantics:
//   - Type      : event-type discriminator (one of EventStep / EventCompact / ...);
//   - Severity  : SevInfo / SevWarn / SevError / SevCritical (used by alert strip
//     filtering and severity badges);
//   - Timestamp : event wall-clock time (time.Time{} zero value means "no
//     timestamp" — buildAlertEvents bypasses TTL filter for zero-time events
//     when IsSynthetic == true, see 38-4 P0 patch);
//   - PID/UUID  : associated process (PID 0 for system-level events without
//     a specific process · alert-enter routing checks PID > 0 before jumping);
//   - Summary   : single-line label shown in alert strip / timeline (already
//     formatted with severity icon prefix where applicable);
//   - Detail    : long-form event detail (multi-line · shown in step inspector
//     and detail card);
//   - StepEntry : pointer to the originating reasoning step (Type == EventStep) ·
//     nil for non-step events;
//   - RawEvent  : pointer to the originating syscall event wire (Type ==
//     EventSyscall / EventCompact / EventImmune from strace) · nil otherwise;
//   - IsSynthetic: marker for dashboard-side synthesised rows (currently only
//     synthSecurityAlerts for AC#4 · buildAlertEvents skips TTL filter for
//     these so the alert strip lifecycle is driven by the upstream IPC list
//     rather than wall-clock TTL · 38-4 P0 patch).
type UnifiedEvent struct {
	Type        string
	Severity    int
	Timestamp   time.Time
	PID         types.PID
	UUID        string
	Summary     string
	Detail      string
	StepEntry   *timeline.StepEntry
	RawEvent    *ipc.SyscallEventWire
	IsSynthetic bool
}

// UnifiedEventSlice implements sort.Interface, sorting by Timestamp descending
// (newest first).
//
// Behavior contract (preserved from cmd/rnix · Story 34.1):
//   - Less(i, j) returns true when events[i].Timestamp is AFTER events[j].Timestamp
//     (descending order · newest at slice head);
//   - Stable across zero-time events (sort.SliceStable preserves insertion order
//     for equal timestamps).
type UnifiedEventSlice []UnifiedEvent

// Len returns the slice length. Implements sort.Interface.
func (s UnifiedEventSlice) Len() int { return len(s) }

// Less reports whether s[i] sorts before s[j] (newest first).
// Implements sort.Interface.
func (s UnifiedEventSlice) Less(i, j int) bool { return s[i].Timestamp.After(s[j].Timestamp) }

// Swap swaps elements i and j. Implements sort.Interface.
func (s UnifiedEventSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
