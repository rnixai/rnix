// Package debug — helpers_toolinput_test.go (Story 65.3)
//
// CollapseToolInputGroups 投影测试：把连续 DriverToolCall input_delta 分片折叠成单个
// EventToolInput 摘要行（防刷屏 · AC#1）、展开时投影有界正文行（AC#2）、非分片事件
// 逐字段原样透传（AC#3 · 收编 65-1 defer 的 aggregate guard）、ASCII fold marker 降级
// （AC#4）、与 thinking 折叠串联复合正确 + ShowStrace=false 组整体隐藏（AC#5）。
//
// 纯函数聚合/还原/摘要的单测在 internal/dashboard/event/helpers_toolinput_test.go；
// 本文件只覆盖 debug 包新增的投影 + 展开/截断逻辑。
//
// ATDD 期以骨架+t.Skip 机制提交（[[atdd-code-story-red-mechanism-preference]]），
// dev-story 已填实 CollapseToolInputGroups 并移除全部 RED skip（验 RED→GREEN）；
// GREEN-GUARD（透传 DeepEqual guard + 无分片返 raw + 60.2 串联零回归）全程未 skip 保持绿。
//
// ⚠️ 分片事件构造只含 type/content/partial_json 三键——**无 subtype 键**（story
// 「测试构造注意」· 别顺手加 subtype）；started 才带 tool/call_id/subtype；aggregate
// 无 content 键（65-1 形态）。
package debug

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/ipc"
)

// inputDeltaEv 构造 DriverToolCall input_delta 分片事件（无 subtype 键 · 真实形态）。
func inputDeltaEv(partialJSON string, ts int64) event.UnifiedEvent {
	return event.UnifiedEvent{
		Type:     event.EventSyscall,
		Severity: event.SevInfo,
		RawEvent: &ipc.SyscallEventWire{
			Syscall:     "DriverToolCall",
			TimestampMs: ts,
			Args:        map[string]any{"type": "tool_call", "content": "input_delta", "partial_json": partialJSON},
		},
	}
}

// toolStartedEv 构造 DriverToolCall started 事件（携带 tool/call_id · 工具名回溯源）。
func toolStartedEv(tool, callID string, ts int64) event.UnifiedEvent {
	return event.UnifiedEvent{
		Type:     event.EventSyscall,
		Severity: event.SevInfo,
		Summary:  "started passthrough",
		RawEvent: &ipc.SyscallEventWire{
			Syscall:     "DriverToolCall",
			TimestampMs: ts,
			Args:        map[string]any{"type": "tool_call", "content": "started", "tool": tool, "call_id": callID, "subtype": "started"},
		},
	}
}

// toolAggregateEv 构造 65-1 aggregate 事件（**无 content 键** → 天然透传 · 裁决 7 guard）。
func toolAggregateEv(tool string, ts int64) event.UnifiedEvent {
	return event.UnifiedEvent{
		Type:     event.EventSyscall,
		Severity: event.SevInfo,
		Summary:  "aggregate passthrough",
		RawEvent: &ipc.SyscallEventWire{
			Syscall:     "DriverToolCall",
			TimestampMs: ts,
			Args:        map[string]any{"type": "tool_call", "subtype": "aggregate", "tool": tool, "input": "{}", "result": "ok"},
		},
	}
}

// completedEv 构造 codex/cursor 原生 completed 终端事件（无 input_delta · SPEC Non-goal 透传）。
func completedEv(ts int64) event.UnifiedEvent {
	return event.UnifiedEvent{
		Type:     event.EventSyscall,
		Severity: event.SevInfo,
		Summary:  "completed passthrough",
		RawEvent: &ipc.SyscallEventWire{
			Syscall:     "DriverToolCall",
			TimestampMs: ts,
			Args:        map[string]any{"type": "tool_call", "content": "completed"},
		},
	}
}

// nonToolEv 构造非 DriverToolCall syscall 事件（断块 + 透传）。
func nonToolEv(syscall string) event.UnifiedEvent {
	return event.UnifiedEvent{
		Type:     event.EventSyscall,
		Severity: event.SevInfo,
		Summary:  "passthrough",
		RawEvent: &ipc.SyscallEventWire{Syscall: syscall},
	}
}

// thinkEvTI — 本文件复用 thinkEv（helpers_thinking_test.go 同包定义）构造 DriverThinking
// 事件用于 AC#5 串联复合测试。

// =============================================================================
// AC#1 — 折叠
// =============================================================================

// AC#1：started 透传 + N delta 折叠成单个 EventToolInput 摘要行（防刷屏核心）。
func TestCollapseToolInputGroups_FoldsRunToOneRow(t *testing.T) {
	raw := []event.UnifiedEvent{
		toolStartedEv("fs_write", "call-1", 1000),
		inputDeltaEv(`{"path":`, 1010),
		inputDeltaEv(`"/a"}`, 1020),
	}
	out := CollapseToolInputGroups(raw, nil, false)
	// started 透传(1) + 折叠摘要(1) = 2 行。
	if len(out) != 2 {
		t.Fatalf("started + 2 delta: want 2 rows (started + fold), got %d", len(out))
	}
	row := out[1]
	if row.Type != event.EventToolInput {
		t.Errorf("folded row Type = %q, want EventToolInput", row.Type)
	}
	if row.RawEvent == nil || row.RawEvent.TimestampMs != 1010 {
		t.Errorf("summary row must carry expand key (first delta ts=1010), got %+v", row.RawEvent)
	}
	if !strings.Contains(row.Summary, "🔧") || !strings.Contains(row.Summary, "2 delta") {
		t.Errorf("summary = %q, want 🔧 + \"2 delta\"", row.Summary)
	}
	if !strings.Contains(row.Summary, "fs_write") {
		t.Errorf("summary should show backtracked tool name fs_write: %q", row.Summary)
	}
	if !strings.Contains(row.Summary, "▶") {
		t.Errorf("collapsed summary must show ▶ fold marker: %q", row.Summary)
	}
	// 折叠态不预建全文（60.2 Patch P1 lazy · 分片可达数千条）。
	if row.Detail != "" {
		t.Errorf("collapsed summary Detail = %q, want empty (lazy reconstruct)", row.Detail)
	}
}

// AC#1 防刷屏：上万分片仍折叠为 1 行摘要（绝不逐分片占行）。
func TestCollapseToolInputGroups_HugeRunFoldsToOne(t *testing.T) {
	const n = 10000
	raw := make([]event.UnifiedEvent, 0, n)
	for i := range n {
		raw = append(raw, inputDeltaEv("x", int64(1000+i)))
	}
	out := CollapseToolInputGroups(raw, nil, false)
	if len(out) != 1 {
		t.Fatalf("%d delta: want 1 folded row, got %d", n, len(out))
	}
}

// AC#1 交错切段（裁决 1 容忍语义）：started→delta×2→break→delta×2 → 2 组各折 1 行。
func TestCollapseToolInputGroups_InterleavedFoldsToTwoRows(t *testing.T) {
	raw := []event.UnifiedEvent{
		toolStartedEv("fs_write", "call-1", 1000), // 透传
		inputDeltaEv(`{"a":`, 1010),               // 组1
		inputDeltaEv(`1}`, 1020),                  // 组1
		nonToolEv("DriverInit"),                   // 断块 · 透传
		inputDeltaEv(`{"b":2}`, 1040),             // 组2
	}
	out := CollapseToolInputGroups(raw, nil, false)
	// started(1) + fold1(1) + init(1) + fold2(1) = 4 行。
	if len(out) != 4 {
		t.Fatalf("interleaved: want 4 rows (started + fold + init + fold), got %d", len(out))
	}
	foldCount := 0
	for _, ev := range out {
		if ev.Type == event.EventToolInput {
			foldCount++
		}
	}
	if foldCount != 2 {
		t.Errorf("want 2 fold rows (裁决 1 交错切段), got %d", foldCount)
	}
}

// =============================================================================
// AC#3 — 透传（GREEN-GUARD · 收编 65-1 defer · 骨架天然满足 · 不 skip）
// =============================================================================

// AC#3 GREEN-GUARD：无分片事件 → 原样返回（零折叠）。骨架原样返回即满足。**不 skip**。
func TestCollapseToolInputGroups_NoDeltaReturnsRaw(t *testing.T) {
	raw := []event.UnifiedEvent{
		toolStartedEv("fs_write", "call-1", 1000),
		toolAggregateEv("fs_write", 1100),
		completedEv(1200),
		nonToolEv("DriverInit"),
	}
	out := CollapseToolInputGroups(raw, nil, false)
	if len(out) != len(raw) {
		t.Fatalf("no delta: want raw (%d rows), got %d", len(raw), len(out))
	}
}

// AC#3 GREEN-GUARD（收编 65-1 defer）：started / aggregate（无 content 键）/ completed
// 三形态 DriverToolCall + 非 DriverToolCall 事件经 CollapseToolInputGroups 逐字段原样
// 透传（reflect.DeepEqual · 同 60.2 PreservesNonThinkingEventsExactly 断言模式）。
// 骨架原样返回 raw 天然满足；实现后（这些事件均非 input_delta）仍须成立。**不 skip**。
func TestCollapseToolInputGroups_PreservesNonDeltaEventsExactly(t *testing.T) {
	// mkEvents 每次调用独立构造（含独立 RawEvent 指针）——raw 与 want 不共享任何指针，
	// 函数若 mutate RawEvent 指向的字段 DeepEqual 也能捕获（真深拷贝语义）。
	mkEvents := func() []event.UnifiedEvent {
		started := toolStartedEv("fs_write", "call-1", 1000)
		started.Detail = "started detail"
		started.RawEvent.Result = "pending"
		started.RawEvent.TraceID = "trace-s"

		aggregate := toolAggregateEv("fs_write", 1100)
		aggregate.Severity = event.SevWarn
		aggregate.RawEvent.DurationMs = 120
		aggregate.RawEvent.TraceID = "trace-a"

		completed := completedEv(1200)
		completed.RawEvent.SpanID = "span-c"

		other := nonToolEv("DriverInit")
		other.Severity = event.SevError
		other.RawEvent.Args = map[string]any{"provider": "claude"}

		return []event.UnifiedEvent{started, aggregate, completed, other}
	}
	raw := mkEvents()
	want := mkEvents()

	out := CollapseToolInputGroups(raw, nil, false)
	if len(out) != len(want) {
		t.Fatalf("want %d passthrough rows, got %d", len(want), len(out))
	}
	for i := range want {
		if !reflect.DeepEqual(out[i], want[i]) {
			t.Errorf("row %d changed during tool-input collapse:\n got: %#v\nwant: %#v", i, out[i], want[i])
		}
	}
}

// =============================================================================
// AC#2 — 展开
// =============================================================================

// AC#2：展开组在摘要行后投影出正文行（▼ marker + 缩进拼接输入 + 正文行 RawEvent==nil +
// Type=EventToolInput）· 展开态摘要行 Detail 携全文（折叠态留空 · Patch P1）。
func TestCollapseToolInputGroups_ExpandedEmitsTextRows(t *testing.T) {
	raw := []event.UnifiedEvent{
		inputDeltaEv(`{"path":`, 1000),
		inputDeltaEv(`"/tmp/a"}`, 1010),
	}
	expanded := map[int64]bool{1000: true} // 键=组首分片 ts
	out := CollapseToolInputGroups(raw, expanded, false)
	if len(out) < 2 {
		t.Fatalf("expanded group: want summary + text row(s), got %d", len(out))
	}
	if !strings.Contains(out[0].Summary, "▼") {
		t.Errorf("expanded summary must show ▼ open marker: %q", out[0].Summary)
	}
	if out[0].Detail != `{"path":"/tmp/a"}` {
		t.Errorf("expanded summary Detail = %q, want reconstructed full input", out[0].Detail)
	}
	// 正文行 RawEvent==nil（区别于摘要行）+ Type=EventToolInput（否则渲染分支用错样式 · 裁决 5 ⚠️）。
	if out[1].RawEvent != nil {
		t.Errorf("text row should have nil RawEvent (distinguish from summary), got %+v", out[1].RawEvent)
	}
	if out[1].Type != event.EventToolInput {
		t.Errorf("text row Type = %q, want EventToolInput (renderer 分支正确性)", out[1].Type)
	}
	if !strings.Contains(out[1].Summary, `"/tmp/a"`) {
		t.Errorf("text row should contain reconstructed input, got %q", out[1].Summary)
	}
}

// AC#2「可截断/限高」：超 MaxThinkingExpandLines 的展开正文被截断 + 尾标。
func TestCollapseToolInputGroups_ExpandedTruncatesLongText(t *testing.T) {
	var big strings.Builder
	for range MaxThinkingExpandLines + 10 {
		big.WriteString(strings.Repeat("a", thinkingExpandWrapWidth))
		big.WriteString("\n")
	}
	raw := []event.UnifiedEvent{inputDeltaEv(big.String(), 1000)}
	out := CollapseToolInputGroups(raw, map[int64]bool{1000: true}, false)
	// summary(1) + 最多 MaxThinkingExpandLines 正文 + 1 截断尾标。
	if len(out) > 1+MaxThinkingExpandLines+1 {
		t.Fatalf("expanded rows not bounded: got %d, want ≤ %d", len(out), 1+MaxThinkingExpandLines+1)
	}
	last := out[len(out)-1]
	if !strings.Contains(last.Summary, "truncated") {
		t.Errorf("last row should be truncation marker, got %q", last.Summary)
	}
}

// AC#2 空输入占位（裁决 5）：展开一个 partial_json 全空的组 → 正文行占位 "(no input)"
// （非 60.2 的 "(no thinking text)" · expandRows 参数化后的新占位文案）。
func TestCollapseToolInputGroups_ExpandedEmptyInputPlaceholder(t *testing.T) {
	raw := []event.UnifiedEvent{inputDeltaEv("", 1000)} // partial_json 空 → 拼接结果 ""
	out := CollapseToolInputGroups(raw, map[int64]bool{1000: true}, false)
	if len(out) < 2 {
		t.Fatalf("expanded empty group: want summary + placeholder row, got %d", len(out))
	}
	foundPlaceholder := false
	for _, ev := range out {
		if strings.Contains(ev.Summary, "(no input)") {
			foundPlaceholder = true
		}
		if strings.Contains(ev.Summary, "no thinking text") {
			t.Errorf("must not reuse thinking placeholder text (裁决 5 参数化 (no input)): %q", ev.Summary)
		}
	}
	if !foundPlaceholder {
		t.Errorf("expanded empty input want \"(no input)\" placeholder row")
	}
}

// =============================================================================
// AC#4 — ASCII
// =============================================================================

func TestCollapseToolInputGroups_ASCIIMarkers(t *testing.T) {
	raw := []event.UnifiedEvent{inputDeltaEv(`{"a":1}`, 1000)}
	collapsed := CollapseToolInputGroups(raw, nil, true)
	if !strings.Contains(collapsed[0].Summary, ">") || !strings.Contains(collapsed[0].Summary, "[tool]") {
		t.Errorf("ascii collapsed summary want > + [tool], got %q", collapsed[0].Summary)
	}
	if strings.Contains(collapsed[0].Summary, "🔧") || strings.Contains(collapsed[0].Summary, "▶") {
		t.Errorf("ascii summary must not contain Unicode glyphs: %q", collapsed[0].Summary)
	}
	expanded := CollapseToolInputGroups(raw, map[int64]bool{1000: true}, true)
	if !strings.Contains(expanded[0].Summary, "v") {
		t.Errorf("ascii expanded summary want v open marker, got %q", expanded[0].Summary)
	}
}

// =============================================================================
// AC#5 — 与 thinking 折叠串联复合 + ShowStrace 隐藏 + 零回归
// =============================================================================

// AC#5 复合场景：thinking 块、input_delta 组、透传事件交错的序列 · 先 CollapseThinkingGroups
// 再 CollapseToolInputGroups（裁决 4 固定后置）· 投影行数与顺序正确 · 两折叠互不干扰。
func TestCollapseToolInputGroups_ComposesWithThinkingCollapse(t *testing.T) {
	raw := []event.UnifiedEvent{
		thinkEv("started", "thinking...", 1000), // thinking 块
		thinkEv("delta", "reasoning", 1010),
		toolStartedEv("fs_write", "call-1", 2000), // tool started 透传
		inputDeltaEv(`{"x":1}`, 2010),             // input_delta 组
		inputDeltaEv(`{"y":2}`, 2020),
		nonToolEv("DriverInit"), // 透传
	}
	// 裁决 4 固定顺序：thinking 先折 → 合成 EventThinking 行对 input_delta 组扫描是非分片
	// 事件（自然断块）→ 再折 input_delta。
	collapsed := CollapseThinkingGroups(raw, nil, false)
	out := CollapseToolInputGroups(collapsed, nil, false)
	// thinking-fold(1) + started(1) + toolinput-fold(1) + init(1) = 4 行。
	if len(out) != 4 {
		t.Fatalf("composed collapse: want 4 rows, got %d", len(out))
	}
	if out[0].Type != event.EventThinking {
		t.Errorf("row0 should be folded thinking, got %q", out[0].Type)
	}
	if out[2].Type != event.EventToolInput {
		t.Errorf("row2 should be folded tool input, got %q", out[2].Type)
	}
}

// AC#5 GREEN-GUARD：折叠在 FilterDebugEvents 之后 → ShowStrace=false 时 input_delta
// syscall 事件先被过滤 → 组整体隐藏。骨架原样返回下 ShowStrace=false 也是 0 行（过滤已清空）
// → 天然满足；实现后仍须成立。**不 skip**。
func TestCollapseToolInputGroups_HiddenWhenSyscallFilteredOut(t *testing.T) {
	raw := []event.UnifiedEvent{
		inputDeltaEv(`{"a":`, 1000),
		inputDeltaEv(`1}`, 1010),
	}
	// ShowStrace=false → FilterDebugEvents 丢弃所有 EventSyscall（含 input_delta 分片）。
	state := DebugState{Events: raw, ShowStrace: false}
	filtered := FilterDebugEvents(state, nil)
	out := CollapseToolInputGroups(filtered, nil, false)
	if len(out) != 0 {
		t.Fatalf("syscall filtered out: tool input group must be fully hidden, got %d rows", len(out))
	}
}

// AC#5 零回归 GREEN-GUARD：CollapseThinkingGroups 经本 story 改动后行为不变——thinking
// 折叠独立成 1 行（裁决 4 独立函数）。骨架期与实现期均须成立。**不 skip**。
func TestCollapseThinkingGroups_StillFoldsAfter653(t *testing.T) {
	raw := []event.UnifiedEvent{
		thinkEv("started", "started", 1000),
		thinkEv("delta", "reasoning text", 1010),
	}
	out := CollapseThinkingGroups(raw, nil, false)
	if len(out) != 1 {
		t.Fatalf("thinking collapse regression: want 1 folded row, got %d", len(out))
	}
	if out[0].Type != event.EventThinking {
		t.Errorf("thinking fold Type = %q, want EventThinking (65.3 零回归)", out[0].Type)
	}
}
