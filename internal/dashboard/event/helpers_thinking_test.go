// ATDD Story 60.2 — dashboard debug pane 思考阶段聚合渲染（DriverThinking 可视化）
//
// Red-phase tests for `BuildThinkingAggGroups` + `ThinkingAggGroup` type +
// `ReconstructThinkingText` + `FormatThinkingSummary` helpers in
// internal/dashboard/event/helpers.go (Story 60.2 additions).
//
// Acceptance Criteria covered:
//   - AC#1: 连续 DriverThinking 事件聚合成单个折叠思考块（防刷屏 · started 边界）
//   - AC#2: 思考全文还原（delta content 顺序拼接 · started 不入正文）
//   - AC#3: 专门视觉标识（💭 / ASCII `[think]` 降级 · 摘要文本格式）
//   - AC#5: 单测覆盖（started/delta 混合、空思考、超大 delta 数、ASCII 模式）
//
// **RED 机制 = 骨架 + t.Skip**（Decker 既往偏好 · 37-5/37-6/44-5/54-5）：
//   - helpers.go 已就位最小骨架（struct + 3 个返回零值的函数）→ 本文件可编译；
//   - 断言「真实行为」的测试在实现前用 `t.Skip` 标注（dev-story 移除 skip + 填逻辑验
//     RED→GREEN）；
//   - GREEN-GUARD 测试（断言骨架已满足且实现后仍成立的不变量 · 如空输入→0 组、
//     零值安全）**不 skip**，提交期即跑、实时拦回归红线。
//   - ATDD 提交期 `make all` 全绿（skip 在位）；这是本机制的核心收益。
//
// RED 信号（dev-story 实施前 · 移除对应 t.Skip 后应 FAIL）：
//   - BuildThinkingAggGroups 返回 nil → 所有「应成 1+ 组」断言 FAIL
//   - ReconstructThinkingText 返回 "" → delta 拼接断言 FAIL
//   - FormatThinkingSummary 返回 "" → 图标/计数断言 FAIL
//
// 设计参考同包 helpers_script_test.go（Story 43.3 · BuildScriptAggGroups 连续聚合）。

package event

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// 测试 helper
// =============================================================================

// mkThinkEv 构造一条 DriverThinking 事件（经 StraceToUnifiedEvent 后 Type==EventSyscall ·
// 聚合键在 RawEvent.Syscall · 分块边界在 args.subtype）。
func mkThinkEv(t *testing.T, subtype, content string, tsMs int64) UnifiedEvent {
	t.Helper()
	wire := ipc.SyscallEventWire{
		TimestampMs: tsMs,
		Syscall:     "DriverThinking",
		Args: map[string]any{
			"type":    "thinking",
			"subtype": subtype,
			"content": content,
		},
	}
	return UnifiedEvent{
		Type:      EventSyscall,
		Severity:  SevInfo,
		Timestamp: time.UnixMilli(tsMs),
		Summary:   "stub",
		RawEvent:  &wire,
	}
}

// mkNonThinkEv 构造一条非 DriverThinking 的 syscall 事件（用于断块边界测试）。
func mkNonThinkEv(syscall string) UnifiedEvent {
	return UnifiedEvent{
		Type:     EventSyscall,
		Severity: SevInfo,
		Summary:  "stub",
		RawEvent: &ipc.SyscallEventWire{Syscall: syscall},
	}
}

// =============================================================================
// BuildThinkingAggGroups — AC#1
// =============================================================================

// TestBuildThinkingAggGroups_EmptyInput — GREEN-GUARD（AC#1 边界）：
// nil / 空 slice → 0 组。骨架返回 nil 即满足；实现后仍须成立。**不 skip**。
func TestBuildThinkingAggGroups_EmptyInput(t *testing.T) {
	if got := BuildThinkingAggGroups(nil); len(got) != 0 {
		t.Errorf("nil input: want 0 groups, got %d", len(got))
	}
	if got := BuildThinkingAggGroups([]UnifiedEvent{}); len(got) != 0 {
		t.Errorf("empty input: want 0 groups, got %d", len(got))
	}
}

// TestBuildThinkingAggGroups_NonThinkingOnly — GREEN-GUARD（AC#1 + AC#4）：
// 全是非 DriverThinking 事件 → 0 组（聚合只看 DriverThinking · 不碰其它 syscall）。
// 骨架返回 nil 即满足；实现后仍须成立。**不 skip**。
func TestBuildThinkingAggGroups_NonThinkingOnly(t *testing.T) {
	events := []UnifiedEvent{
		mkNonThinkEv("DriverToolCall"),
		mkNonThinkEv("DriverInit"),
		{Type: EventStep},
	}
	if got := BuildThinkingAggGroups(events); len(got) != 0 {
		t.Errorf("non-thinking events: want 0 groups, got %d", len(got))
	}
}

// TestThinkingAggGroup_ZeroValueIsSafe — GREEN-GUARD：零值结构体安全（renderer 边界
// 场景可能用零值 · 与 ToolAggGroup/ScriptAggGroup 同模式）。**不 skip**。
func TestThinkingAggGroup_ZeroValueIsSafe(t *testing.T) {
	var g ThinkingAggGroup
	if g.StartIdx != 0 || g.EndIdx != 0 || g.DeltaCount != 0 || g.TotalBytes != 0 || g.DurationMs != 0 {
		t.Errorf("zero value not zero: %+v", g)
	}
}

// TestBuildThinkingAggGroups_StartedPlusDeltas — AC#1：1 条 started + 3 条 delta
// 聚合成单个块 [0,4)，DeltaCount=3。
func TestBuildThinkingAggGroups_StartedPlusDeltas(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 1): BuildThinkingAggGroups 未实现（骨架返回 nil）")
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "The user ", 1010),
		mkThinkEv(t, "delta", "is asking ", 1020),
		mkThinkEv(t, "delta", "about X.", 1030),
	}
	got := BuildThinkingAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("started + 3 delta: want 1 group, got %d", len(got))
	}
	g := got[0]
	if g.StartIdx != 0 || g.EndIdx != 4 {
		t.Errorf("group bounds: want [0,4), got [%d,%d)", g.StartIdx, g.EndIdx)
	}
	if g.DeltaCount != 3 {
		t.Errorf("DeltaCount = %d, want 3", g.DeltaCount)
	}
}

// TestBuildThinkingAggGroups_StartedBoundarySplitsBlocks — AC#1：subtype=started 是
// 新思考块的边界 → [started,delta,started,delta,delta] 切成 2 个块。
func TestBuildThinkingAggGroups_StartedBoundarySplitsBlocks(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 1): started 分块边界未实现")
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "block one", 1010),
		mkThinkEv(t, "started", "started", 2000),
		mkThinkEv(t, "delta", "block ", 2010),
		mkThinkEv(t, "delta", "two", 2020),
	}
	got := BuildThinkingAggGroups(events)
	if len(got) != 2 {
		t.Fatalf("two started markers: want 2 groups, got %d", len(got))
	}
	if got[0].DeltaCount != 1 {
		t.Errorf("group0 DeltaCount = %d, want 1", got[0].DeltaCount)
	}
	if got[1].DeltaCount != 2 {
		t.Errorf("group1 DeltaCount = %d, want 2", got[1].DeltaCount)
	}
}

// TestBuildThinkingAggGroups_NonThinkingBreaksBlock — AC#1：遇到非 DriverThinking
// 事件（DriverToolCall）当前思考块结束 → 前后两段独立成块。
func TestBuildThinkingAggGroups_NonThinkingBreaksBlock(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 1): 非 thinking 事件断块未实现")
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "before tool", 1010),
		mkNonThinkEv("DriverToolCall"),
		mkThinkEv(t, "started", "started", 3000),
		mkThinkEv(t, "delta", "after tool", 3010),
	}
	got := BuildThinkingAggGroups(events)
	if len(got) != 2 {
		t.Fatalf("tool call between thinking runs: want 2 groups, got %d", len(got))
	}
	// 关键：DriverToolCall (idx=2) 不在任何 group 范围内。
	for _, g := range got {
		if 2 >= g.StartIdx && 2 < g.EndIdx {
			t.Errorf("non-thinking event (idx=2) absorbed into group [%d,%d)", g.StartIdx, g.EndIdx)
		}
	}
}

// TestBuildThinkingAggGroups_HugeDeltaCountFoldsToOne — AC#1 防刷屏核心：
// 1 started + 10000 delta（API driver 真实量级可达 14841）→ 仍折叠为单个块，
// 绝不逐 delta 占行。DeltaCount=10000。
func TestBuildThinkingAggGroups_HugeDeltaCountFoldsToOne(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 1): 大 delta 量折叠未实现")
	const n = 10000
	events := make([]UnifiedEvent, 0, n+1)
	events = append(events, mkThinkEv(t, "started", "started", 1000))
	for i := range n {
		events = append(events, mkThinkEv(t, "delta", "x", int64(1001+i)))
	}
	got := BuildThinkingAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("1 started + %d delta: want 1 fold group (防刷屏), got %d", n, len(got))
	}
	if got[0].DeltaCount != n {
		t.Errorf("DeltaCount = %d, want %d", got[0].DeltaCount, n)
	}
}

// TestBuildThinkingAggGroups_TotalBytes — AC#1：TotalBytes 累计各 delta content 字节数
// （started 标记的 content 不计入正文字节）。"Hello"(5)+" world"(6)=11。
func TestBuildThinkingAggGroups_TotalBytes(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 1): TotalBytes 累计未实现")
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "Hello", 1010),
		mkThinkEv(t, "delta", " world", 1020),
	}
	got := BuildThinkingAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].TotalBytes != 11 {
		t.Errorf("TotalBytes = %d, want 11 (len(\"Hello\")+len(\" world\"))", got[0].TotalBytes)
	}
}

// TestBuildThinkingAggGroups_DurationFromTimestamps — AC#1：DurationMs = 首末事件
// ts_ms 之差（1000→3100 = 2100ms）。
func TestBuildThinkingAggGroups_DurationFromTimestamps(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 1): DurationMs 计算未实现")
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "a", 2000),
		mkThinkEv(t, "delta", "b", 3100),
	}
	got := BuildThinkingAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].DurationMs != 2100 {
		t.Errorf("DurationMs = %v, want 2100 (3100-1000)", got[0].DurationMs)
	}
}

// TestBuildThinkingAggGroups_ArgsTypeDefense — AC#1 关键约束（Args 类型防御）：
// subtype 缺失 / content 非 string / Args 为 nil 时安全降级（视为无 subtype 的
// thinking 事件 · 不 panic · 仍能成块）。
func TestBuildThinkingAggGroups_ArgsTypeDefense(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 1): Args 类型防御 + 成块未实现")
	events := []UnifiedEvent{
		// subtype 缺失（只有 type）
		{Type: EventSyscall, RawEvent: &ipc.SyscallEventWire{
			Syscall: "DriverThinking",
			Args:    map[string]any{"type": "thinking"},
		}},
		// content 非 string（int）
		{Type: EventSyscall, RawEvent: &ipc.SyscallEventWire{
			Syscall: "DriverThinking",
			Args:    map[string]any{"subtype": "delta", "content": 42},
		}},
		// Args 为 nil
		{Type: EventSyscall, RawEvent: &ipc.SyscallEventWire{
			Syscall: "DriverThinking",
			Args:    nil,
		}},
	}
	// 不得 panic；连续 DriverThinking 仍聚合为块（防御降级而非丢弃）。
	got := BuildThinkingAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("defensive thinking run: want 1 group (no panic), got %d", len(got))
	}
}

// =============================================================================
// ReconstructThinkingText — AC#2
// =============================================================================

// TestReconstructThinkingText_EmptyThinking — GREEN-GUARD（AC#2 边界）：
// 空思考（仅 started · 无 delta）→ "". 骨架返回 "" 即满足；实现后仍须成立。**不 skip**。
func TestReconstructThinkingText_EmptyThinking(t *testing.T) {
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
	}
	g := ThinkingAggGroup{StartIdx: 0, EndIdx: 1, DeltaCount: 0}
	if got := ReconstructThinkingText(events, g); got != "" {
		t.Errorf("empty thinking (started only): want \"\", got %q", got)
	}
}

// TestReconstructThinkingText_ConcatenatesDeltasInOrder — AC#2：按顺序拼接各 delta
// 的 content 还原思考全文。
func TestReconstructThinkingText_ConcatenatesDeltasInOrder(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 3): ReconstructThinkingText 未实现（骨架返回 \"\"）")
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "The user ", 1010),
		mkThinkEv(t, "delta", "is asking ", 1020),
		mkThinkEv(t, "delta", "about X.", 1030),
	}
	g := ThinkingAggGroup{StartIdx: 0, EndIdx: 4, DeltaCount: 3}
	want := "The user is asking about X."
	if got := ReconstructThinkingText(events, g); got != want {
		t.Errorf("ReconstructThinkingText = %q, want %q", got, want)
	}
}

// TestReconstructThinkingText_SkipsStartedMarker — AC#2：started 的 content（"started"
// 标记）绝不进入正文。
func TestReconstructThinkingText_SkipsStartedMarker(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 3): started 排除未实现")
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "real content", 1010),
	}
	g := ThinkingAggGroup{StartIdx: 0, EndIdx: 2, DeltaCount: 1}
	got := ReconstructThinkingText(events, g)
	if strings.Contains(got, "started") {
		t.Errorf("reconstructed text contains started marker: %q", got)
	}
	if got != "real content" {
		t.Errorf("ReconstructThinkingText = %q, want \"real content\"", got)
	}
}

// =============================================================================
// FormatThinkingSummary — AC#3
// =============================================================================

// TestFormatThinkingSummary_Unicode — AC#3：默认（非 ASCII）摘要用 💭 图标，
// 含 delta 计数。形如 `💭 thinking (74 delta · 6.5KB · 2.1s)`。
func TestFormatThinkingSummary_Unicode(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 2): FormatThinkingSummary 未实现（骨架返回 \"\"）")
	g := ThinkingAggGroup{DeltaCount: 74, TotalBytes: 6660, DurationMs: 2100}
	got := FormatThinkingSummary(g, false)
	if !strings.Contains(got, "💭") {
		t.Errorf("unicode summary missing 💭 glyph: %q", got)
	}
	if !strings.Contains(got, "74 delta") {
		t.Errorf("summary missing delta count \"74 delta\": %q", got)
	}
}

// TestFormatThinkingSummary_ASCII — AC#3：ASCII 模式（RNIX_ASCII=1 · 调用方传 ascii=true）
// 用 `[think]` 降级标记，不含 Unicode glyph 💭。
func TestFormatThinkingSummary_ASCII(t *testing.T) {
	t.Skip("RED (Story 60.2 Task 2): ASCII 降级未实现")
	g := ThinkingAggGroup{DeltaCount: 74, TotalBytes: 6660, DurationMs: 2100}
	got := FormatThinkingSummary(g, true)
	if !strings.Contains(got, "[think]") {
		t.Errorf("ascii summary missing [think] marker: %q", got)
	}
	if strings.Contains(got, "💭") {
		t.Errorf("ascii summary must not contain Unicode 💭 glyph: %q", got)
	}
	if !strings.Contains(got, "74 delta") {
		t.Errorf("summary missing delta count \"74 delta\": %q", got)
	}
}
