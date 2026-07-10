// ATDD Story 65.3 — dashboard debug pane input_delta 分片聚合（DriverToolCall 输入折叠）
//
// Red-phase tests for `BuildToolInputAggGroups` + `ToolInputAggGroup` type +
// `ReconstructToolInput` + `FormatToolInputSummary` helpers in
// internal/dashboard/event/helpers.go (Story 65.3 additions).
//
// Acceptance Criteria covered（本文件 = event 包纯函数层 · AC1/4/7①）:
//   - AC#1: 连续 DriverToolCall content==input_delta 分片聚合成单个折叠组（防刷屏）；
//           started 断块透传 · 工具名回溯 · 无 started 降级 · 任意 ≥1 条即成组 ·
//           交错切段（started→delta×N→user→delta×M → 2 组）· 数千分片仍 1 组。
//   - AC#2: 输入全文还原（partial_json 顺序拼接 · 越界 clamp）。
//   - AC#4: 专门视觉标识（🔧 / ASCII `[tool]` 降级 · 摘要文本格式 · 工具名降级）。
//   - AC#7①: 表驱动单测覆盖（Args 类型防御、空 events）。
//
// **RED 机制 = 骨架 + t.Skip**（Decker 既往偏好 · [[atdd-code-story-red-mechanism-preference]]
// · 37-5/37-6/44-5/54-5/60.2）：
//   - helpers.go 已就位最小骨架（ToolInputAggGroup struct + 3 个返回零值的函数）→ 本文件可编译；
//   - 断言「真实行为」的测试在实现前用 `t.Skip` 标注（dev-story 移除 skip + 填逻辑验
//     RED→GREEN）；
//   - GREEN-GUARD 测试（断言骨架已满足且实现后仍成立的不变量 · 如空输入→0 组、
//     零值安全）**不 skip** · 提交期即跑 · 实时拦回归红线。
//   - ATDD 提交期 `make all` 全绿（skip 在位）；这是本机制的核心收益。
//
// RED 信号（dev-story 实施前 · 移除对应 t.Skip 后应 FAIL）：
//   - BuildToolInputAggGroups 返回 nil → 所有「应成 1+ 组」断言 FAIL
//   - ReconstructToolInput 返回 "" → partial_json 拼接断言 FAIL
//   - FormatToolInputSummary 返回 "" → 图标/工具名/计数断言 FAIL
//
// 设计参考同包 helpers_thinking_test.go（Story 60.2 · BuildThinkingAggGroups 先例）。
// ⚠️ 分片事件构造只含 type/content/partial_json 三键——**无 subtype 键**（真实
// input_delta 分片没有 subtype · story「测试构造注意」）；started 事件才带 tool/call_id。

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

// mkInputDeltaEv 构造一条 DriverToolCall input_delta 分片事件（经 StraceToUnifiedEvent
// 后 Type==EventSyscall · 聚合键在 RawEvent.Syscall + args.content=="input_delta"）。
// ⚠️ 无 tool / call_id / subtype 键——分片事件的真实形态（story 分片事件形态表）。
func mkInputDeltaEv(t *testing.T, partialJSON string, tsMs int64) UnifiedEvent {
	t.Helper()
	wire := ipc.SyscallEventWire{
		TimestampMs: tsMs,
		Syscall:     "DriverToolCall",
		Args: map[string]any{
			"type":         "tool_call",
			"content":      "input_delta",
			"partial_json": partialJSON,
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

// mkToolStartedEv 构造一条 DriverToolCall started 事件（携带 tool/call_id/subtype ·
// 工具名回溯来源 · started 本身透传不入组）。
func mkToolStartedEv(t *testing.T, tool, callID string, tsMs int64) UnifiedEvent {
	t.Helper()
	wire := ipc.SyscallEventWire{
		TimestampMs: tsMs,
		Syscall:     "DriverToolCall",
		Args: map[string]any{
			"type":    "tool_call",
			"content": "started",
			"tool":    tool,
			"call_id": callID,
			"subtype": "started",
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

// mkToolAggregateEv 构造一条 65-1 aggregate 事件（**无 content 键** · content 断言取
// 零值 "" ≠ "input_delta" → 天然透传 · 裁决 7 收编 65-1 defer 的 guard 形态）。
func mkToolAggregateEv(t *testing.T, tool string, tsMs int64) UnifiedEvent {
	t.Helper()
	wire := ipc.SyscallEventWire{
		TimestampMs: tsMs,
		Syscall:     "DriverToolCall",
		Args: map[string]any{
			"type":        "tool_call",
			"subtype":     "aggregate",
			"tool":        tool,
			"input":       "{\"path\":\"/x\"}",
			"result":      "ok",
			"duration_ms": float64(120),
			"step":        float64(3),
		},
	}
	return UnifiedEvent{Type: EventSyscall, Severity: SevInfo, RawEvent: &wire}
}

// mkNonToolCallEv 构造一条非 DriverToolCall syscall 事件（断块边界 · 交错序列的 user 行等）。
func mkNonToolCallEv(syscall string) UnifiedEvent {
	return UnifiedEvent{
		Type:     EventSyscall,
		Severity: SevInfo,
		Summary:  "stub",
		RawEvent: &ipc.SyscallEventWire{Syscall: syscall},
	}
}

// =============================================================================
// BuildToolInputAggGroups — AC#1
// =============================================================================

// TestBuildToolInputAggGroups_EmptyInput — GREEN-GUARD（AC#1 边界）：
// nil / 空 slice → 0 组。骨架返回 nil 即满足；实现后仍须成立。**不 skip**。
func TestBuildToolInputAggGroups_EmptyInput(t *testing.T) {
	if got := BuildToolInputAggGroups(nil); len(got) != 0 {
		t.Errorf("nil input: want 0 groups, got %d", len(got))
	}
	if got := BuildToolInputAggGroups([]UnifiedEvent{}); len(got) != 0 {
		t.Errorf("empty input: want 0 groups, got %d", len(got))
	}
}

// TestBuildToolInputAggGroups_NonDeltaOnly — GREEN-GUARD（AC#1 + AC#3）：
// 全是非 input_delta 事件（started/aggregate/completed/其它 syscall）→ 0 组。
// 聚合只看 content==input_delta 分片 · 不碰 started/aggregate/completed。
// 骨架返回 nil 即满足；实现后仍须成立。**不 skip**。
func TestBuildToolInputAggGroups_NonDeltaOnly(t *testing.T) {
	events := []UnifiedEvent{
		mkToolStartedEv(t, "fs_write", "call-1", 1000),
		mkToolAggregateEv(t, "fs_write", 1100),
		mkNonToolCallEv("DriverInit"),
		{Type: EventStep},
	}
	if got := BuildToolInputAggGroups(events); len(got) != 0 {
		t.Errorf("non-delta events: want 0 groups, got %d", len(got))
	}
}

// TestToolInputAggGroup_ZeroValueIsSafe — GREEN-GUARD：零值结构体安全（renderer 边界
// 场景可能用零值 · 与 ThinkingAggGroup 同模式）。**不 skip**。
func TestToolInputAggGroup_ZeroValueIsSafe(t *testing.T) {
	var g ToolInputAggGroup
	if g.StartIdx != 0 || g.EndIdx != 0 || g.ToolName != "" || g.DeltaCount != 0 || g.TotalBytes != 0 || g.DurationMs != 0 {
		t.Errorf("zero value not zero: %+v", g)
	}
}

// TestBuildToolInputAggGroups_StartedPlusDeltas — AC#1：started 透传不入组 · 后随 3 条
// input_delta 分片聚合成单组 [1,4)，DeltaCount=3，ToolName 回溯 started 的 "fs_write"。
func TestBuildToolInputAggGroups_StartedPlusDeltas(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkToolStartedEv(t, "fs_write", "call-1", 1000),
		mkInputDeltaEv(t, `{"path":`, 1010),
		mkInputDeltaEv(t, `"/tmp/a"`, 1020),
		mkInputDeltaEv(t, `,"data":"x"}`, 1030),
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("started + 3 delta: want 1 group, got %d", len(got))
	}
	g := got[0]
	// started(idx 0) 透传不入组 → 组从 idx 1 起（裁决 1）。
	if g.StartIdx != 1 || g.EndIdx != 4 {
		t.Errorf("group bounds: want [1,4) (started passthrough), got [%d,%d)", g.StartIdx, g.EndIdx)
	}
	if g.DeltaCount != 3 {
		t.Errorf("DeltaCount = %d, want 3", g.DeltaCount)
	}
	if g.ToolName != "fs_write" {
		t.Errorf("ToolName = %q, want \"fs_write\" (回溯最近 started 的 args.tool)", g.ToolName)
	}
}

// TestBuildToolInputAggGroups_StartedNotAbsorbed — AC#1（裁决 1 关键）：started 事件
// **不吸进组** · 组范围绝不覆盖 started 下标（与 60.2 把 started 吸进组不同）。
func TestBuildToolInputAggGroups_StartedNotAbsorbed(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkToolStartedEv(t, "fs_write", "call-1", 1000), // idx 0 · 透传
		mkInputDeltaEv(t, `{"a":1}`, 1010),             // idx 1 · 组首
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].StartIdx <= 0 {
		t.Errorf("started (idx 0) absorbed into group [%d,%d) — 裁决 1 要求 started 透传不入组", got[0].StartIdx, got[0].EndIdx)
	}
}

// TestBuildToolInputAggGroups_SingleDeltaFolds — AC#1（裁决 2）：任意 ≥1 条分片即成组
// （不用 AggThreshold=3）· 单分片工具输入也折叠为一组。
func TestBuildToolInputAggGroups_SingleDeltaFolds(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{mkInputDeltaEv(t, `{"x":1}`, 1000)}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("single delta: want 1 group (裁决 2 · ≥1 即折叠), got %d", len(got))
	}
	if got[0].DeltaCount != 1 {
		t.Errorf("DeltaCount = %d, want 1", got[0].DeltaCount)
	}
}

// TestBuildToolInputAggGroups_NonDeltaBreaksGroup — AC#1：遇到非 input_delta 事件
// （started）当前组结束 → 两段 input_delta run 独立成组，中间 started 不在任何组内。
func TestBuildToolInputAggGroups_NonDeltaBreaksGroup(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkInputDeltaEv(t, `{"a":`, 1000),               // idx 0 · 组1
		mkInputDeltaEv(t, `1}`, 1010),                  // idx 1 · 组1
		mkToolStartedEv(t, "fs_read", "call-2", 2000),  // idx 2 · 断块 · 透传
		mkInputDeltaEv(t, `{"path":"/y"}`, 2010),       // idx 3 · 组2
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 2 {
		t.Fatalf("started between delta runs: want 2 groups, got %d", len(got))
	}
	// 关键：started（idx=2）不在任何 group 范围内。
	for _, g := range got {
		if 2 >= g.StartIdx && 2 < g.EndIdx {
			t.Errorf("started event (idx=2) absorbed into group [%d,%d)", g.StartIdx, g.EndIdx)
		}
	}
}

// TestBuildToolInputAggGroups_InterleavedSplitsToTwo — AC#1（裁决 1 交错容忍语义）：
// 62-5 教训——claude CLI 把上一轮 user(tool_result) 与下一轮工具分片交错 · 交错事件把
// 一个工具的分片切成两组（各自折叠成两行摘要 · 可接受 · 勿新增缝合状态机）。
// 序列 started→delta×2→user→delta×2 → 2 组。
func TestBuildToolInputAggGroups_InterleavedSplitsToTwo(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkToolStartedEv(t, "fs_write", "call-1", 1000), // idx 0 · 透传
		mkInputDeltaEv(t, `{"path":`, 1010),            // idx 1 · 组1
		mkInputDeltaEv(t, `"/a"`, 1020),                // idx 2 · 组1
		mkNonToolCallEv("DriverToolCall"),              // idx 3 · user(tool_result) 交错断块——用普通 syscall 近似
		mkInputDeltaEv(t, `,"data":`, 1040),            // idx 4 · 组2
		mkInputDeltaEv(t, `"x"}`, 1050),                // idx 5 · 组2
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 2 {
		t.Fatalf("interleaved started→delta×2→break→delta×2: want 2 groups (裁决 1 容忍), got %d", len(got))
	}
	if got[0].DeltaCount != 2 || got[1].DeltaCount != 2 {
		t.Errorf("group DeltaCounts = [%d,%d], want [2,2]", got[0].DeltaCount, got[1].DeltaCount)
	}
}

// TestBuildToolInputAggGroups_ToolNameBacktrackAcrossGap — AC#1（裁决 1 · 工具名游标）：
// started 与分片间隔着非分片事件（交错）· 游标是「最近 started」而非「紧邻前一事件」。
func TestBuildToolInputAggGroups_ToolNameBacktrackAcrossGap(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkToolStartedEv(t, "shell_exec", "call-9", 1000), // idx 0 · 记住 tool
		mkNonToolCallEv("DriverInit"),                    // idx 1 · 间隔事件
		mkInputDeltaEv(t, `{"cmd":"ls"}`, 1020),          // idx 2 · 组首 · ToolName 应回溯 shell_exec
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].ToolName != "shell_exec" {
		t.Errorf("ToolName = %q, want \"shell_exec\" (游标=最近 started · 跨间隔事件)", got[0].ToolName)
	}
}

// TestBuildToolInputAggGroups_NoStartedDegradesToolName — AC#1（裁决 1/5 · 降级）：
// 组前无 started（边界/流截断）→ ToolName="" · 摘要降级省略工具名。
func TestBuildToolInputAggGroups_NoStartedDegradesToolName(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkInputDeltaEv(t, `{"orphan":true}`, 1000), // 无前置 started
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].ToolName != "" {
		t.Errorf("ToolName = %q, want \"\" (组前无 started → 降级)", got[0].ToolName)
	}
}

// TestBuildToolInputAggGroups_HugeDeltaCountFoldsToOne — AC#1 防刷屏核心：
// 10000 条分片 → 仍折叠为单个组，绝不逐分片占行。DeltaCount=10000。
func TestBuildToolInputAggGroups_HugeDeltaCountFoldsToOne(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	const n = 10000
	events := make([]UnifiedEvent, 0, n+1)
	events = append(events, mkToolStartedEv(t, "fs_write", "call-1", 1000))
	for i := range n {
		events = append(events, mkInputDeltaEv(t, "x", int64(1001+i)))
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("started + %d delta: want 1 fold group (防刷屏), got %d", n, len(got))
	}
	if got[0].DeltaCount != n {
		t.Errorf("DeltaCount = %d, want %d", got[0].DeltaCount, n)
	}
}

// TestBuildToolInputAggGroups_TotalBytes — AC#1：TotalBytes 累计各分片 partial_json
// 字节数。`{"a":`(5)+`1}`(2)=7。
func TestBuildToolInputAggGroups_TotalBytes(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkInputDeltaEv(t, `{"a":`, 1000),
		mkInputDeltaEv(t, `1}`, 1010),
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].TotalBytes != 7 {
		t.Errorf("TotalBytes = %d, want 7 (len(`{\"a\":`)+len(`1}`))", got[0].TotalBytes)
	}
}

// TestBuildToolInputAggGroups_DurationFromTimestamps — AC#1：DurationMs = 组内首末
// 分片 ts_ms 之差（1000→2200 = 1200ms）。
func TestBuildToolInputAggGroups_DurationFromTimestamps(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkInputDeltaEv(t, "a", 1000),
		mkInputDeltaEv(t, "b", 1500),
		mkInputDeltaEv(t, "c", 2200),
	}
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group, got %d", len(got))
	}
	if got[0].DurationMs != 1200 {
		t.Errorf("DurationMs = %v, want 1200 (2200-1000)", got[0].DurationMs)
	}
}

// TestBuildToolInputAggGroups_ArgsTypeDefense — AC#1 关键约束（Args 类型防御 · 60.2
// thinkingArgs 先例）：partial_json 非 string / content 缺失 / Args 为 nil 时安全降级
// （不 panic）。构造混合：合法分片 + partial_json 为 int 的畸形分片。
func TestBuildToolInputAggGroups_ArgsTypeDefense(t *testing.T) {
	t.Skip("RED: BuildToolInputAggGroups 未实现（骨架返回 nil）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		// 合法 input_delta 分片
		mkInputDeltaEv(t, `{"ok":1}`, 1000),
		// partial_json 非 string（int）· content 仍 input_delta → 应仍入组、字节数 0 降级
		{Type: EventSyscall, RawEvent: &ipc.SyscallEventWire{
			TimestampMs: 1010,
			Syscall:     "DriverToolCall",
			Args:        map[string]any{"type": "tool_call", "content": "input_delta", "partial_json": 42},
		}},
	}
	// 不得 panic；连续 input_delta 仍聚合为组（防御降级而非丢弃）。
	got := BuildToolInputAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("defensive delta run: want 1 group (no panic), got %d", len(got))
	}
	if got[0].DeltaCount != 2 {
		t.Errorf("DeltaCount = %d, want 2 (畸形 partial_json 仍计数)", got[0].DeltaCount)
	}
}

// =============================================================================
// ReconstructToolInput — AC#2
// =============================================================================

// TestReconstructToolInput_EmptyGroup — GREEN-GUARD（AC#2 边界）：
// 空组（无分片）→ "". 骨架返回 "" 即满足；实现后仍须成立。**不 skip**。
func TestReconstructToolInput_EmptyGroup(t *testing.T) {
	events := []UnifiedEvent{mkToolStartedEv(t, "fs_write", "call-1", 1000)}
	g := ToolInputAggGroup{StartIdx: 1, EndIdx: 1, DeltaCount: 0}
	if got := ReconstructToolInput(events, g); got != "" {
		t.Errorf("empty group: want \"\", got %q", got)
	}
}

// TestReconstructToolInput_ConcatenatesPartialsInOrder — AC#2：按顺序拼接各分片的
// partial_json 还原完整输入 JSON 文本（不做 pretty-print · 裁决 5）。
func TestReconstructToolInput_ConcatenatesPartialsInOrder(t *testing.T) {
	t.Skip("RED: ReconstructToolInput 未实现（骨架返回 \"\"）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkInputDeltaEv(t, `{"path":`, 1000),
		mkInputDeltaEv(t, `"/tmp/a",`, 1010),
		mkInputDeltaEv(t, `"data":"x"}`, 1020),
	}
	g := ToolInputAggGroup{StartIdx: 0, EndIdx: 3, DeltaCount: 3}
	want := `{"path":"/tmp/a","data":"x"}`
	if got := ReconstructToolInput(events, g); got != want {
		t.Errorf("ReconstructToolInput = %q, want %q", got, want)
	}
}

// TestReconstructToolInput_ClampsOutOfRange — AC#2（renderer 边界防御）：
// StartIdx/EndIdx 越界安全 clamp（对齐 ReconstructThinkingText），不 panic。
func TestReconstructToolInput_ClampsOutOfRange(t *testing.T) {
	t.Skip("RED: ReconstructToolInput 未实现（骨架返回 \"\"）· dev-story 移除 skip 验 GREEN")
	events := []UnifiedEvent{
		mkInputDeltaEv(t, `{"a":1}`, 1000),
	}
	// EndIdx 远超 len(events) · StartIdx 负 → clamp 到 [0, len)，不 panic。
	g := ToolInputAggGroup{StartIdx: -5, EndIdx: 999, DeltaCount: 1}
	got := ReconstructToolInput(events, g)
	if got != `{"a":1}` {
		t.Errorf("clamped reconstruct = %q, want `{\"a\":1}`", got)
	}
}

// =============================================================================
// FormatToolInputSummary — AC#4
// =============================================================================

// TestFormatToolInputSummary_Unicode — AC#4：默认（非 ASCII）摘要用 🔧 图标 + 工具名 +
// "input" + delta 计数。形如 `🔧 fs_write input (14 delta · 2.3KB · 1.2s)`。
func TestFormatToolInputSummary_Unicode(t *testing.T) {
	t.Skip("RED: FormatToolInputSummary 未实现（骨架返回 \"\"）· dev-story 移除 skip 验 GREEN")
	g := ToolInputAggGroup{ToolName: "fs_write", DeltaCount: 14, TotalBytes: 2355, DurationMs: 1200}
	got := FormatToolInputSummary(g, false)
	if !strings.Contains(got, "🔧") {
		t.Errorf("unicode summary missing 🔧 glyph: %q", got)
	}
	if !strings.Contains(got, "fs_write") {
		t.Errorf("summary missing tool name \"fs_write\": %q", got)
	}
	if !strings.Contains(got, "14 delta") {
		t.Errorf("summary missing delta count \"14 delta\": %q", got)
	}
	if !strings.Contains(got, "input") {
		t.Errorf("summary missing \"input\" label: %q", got)
	}
}

// TestFormatToolInputSummary_ASCII — AC#4：ASCII 模式（RNIX_ASCII=1 · 调用方传 ascii=true）
// 用 `[tool]` 降级标记 · 不含 Unicode glyph 🔧 · 无中点分隔符。
func TestFormatToolInputSummary_ASCII(t *testing.T) {
	t.Skip("RED: FormatToolInputSummary 未实现（骨架返回 \"\"）· dev-story 移除 skip 验 GREEN")
	g := ToolInputAggGroup{ToolName: "fs_write", DeltaCount: 14, TotalBytes: 2355, DurationMs: 1200}
	got := FormatToolInputSummary(g, true)
	if !strings.Contains(got, "[tool]") {
		t.Errorf("ascii summary missing [tool] marker: %q", got)
	}
	if strings.Contains(got, "🔧") {
		t.Errorf("ascii summary must not contain Unicode 🔧 glyph: %q", got)
	}
	if strings.Contains(got, "·") {
		t.Errorf("ascii summary must not contain middle-dot separator: %q", got)
	}
	if !strings.Contains(got, "14 delta") {
		t.Errorf("summary missing delta count \"14 delta\": %q", got)
	}
}

// TestFormatToolInputSummary_ToolNameDegraded — AC#4（裁决 5 降级）：ToolName=="" 时
// 摘要省略工具名 · 降级为 `🔧 tool input (…)`（工具名位置用 "tool" 占位）。
func TestFormatToolInputSummary_ToolNameDegraded(t *testing.T) {
	t.Skip("RED: FormatToolInputSummary 未实现（骨架返回 \"\"）· dev-story 移除 skip 验 GREEN")
	g := ToolInputAggGroup{ToolName: "", DeltaCount: 3, TotalBytes: 40, DurationMs: 100}
	got := FormatToolInputSummary(g, false)
	if !strings.Contains(got, "tool input") {
		t.Errorf("degraded summary should contain \"tool input\" (无工具名降级): %q", got)
	}
	if !strings.Contains(got, "3 delta") {
		t.Errorf("summary missing delta count: %q", got)
	}
}
