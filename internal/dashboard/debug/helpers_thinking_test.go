// Package debug — helpers_thinking_test.go (Story 60.2)
//
// CollapseThinkingGroups 投影测试：把连续 DriverThinking 事件折叠成单个 EventThinking
// 摘要行（防刷屏 · AC#1）、展开时投影有界正文行（AC#2）、ASCII fold marker 降级
// （AC#3）、非 thinking 事件原样透传（AC#4）。
//
// 纯函数聚合/还原/摘要的单测在 internal/dashboard/event/helpers_thinking_test.go；
// 本文件只覆盖 debug 包新增的投影 + 展开/截断逻辑。
package debug

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/ipc"
)

func thinkEv(subtype, content string, ts int64) event.UnifiedEvent {
	return event.UnifiedEvent{
		Type:     event.EventSyscall,
		Severity: event.SevInfo,
		RawEvent: &ipc.SyscallEventWire{
			Syscall:     "DriverThinking",
			TimestampMs: ts,
			Args:        map[string]any{"type": "thinking", "subtype": subtype, "content": content},
		},
	}
}

func nonThinkEv(syscall string) event.UnifiedEvent {
	return event.UnifiedEvent{
		Type:     event.EventSyscall,
		Severity: event.SevInfo,
		Summary:  "passthrough",
		RawEvent: &ipc.SyscallEventWire{Syscall: syscall},
	}
}

// AC#1：1 started + N delta 折叠成单个 EventThinking 摘要行（防刷屏核心）。
func TestCollapseThinkingGroups_FoldsRunToOneRow(t *testing.T) {
	raw := []event.UnifiedEvent{
		thinkEv("started", "started", 1000),
		thinkEv("delta", "The user ", 1010),
		thinkEv("delta", "is asking.", 1020),
	}
	out := CollapseThinkingGroups(raw, nil, false)
	if len(out) != 1 {
		t.Fatalf("started + 2 delta: want 1 folded row, got %d", len(out))
	}
	row := out[0]
	if row.Type != event.EventThinking {
		t.Errorf("folded row Type = %q, want EventThinking", row.Type)
	}
	if row.RawEvent == nil || row.RawEvent.TimestampMs != 1000 {
		t.Errorf("summary row must carry expand key (first ts=1000), got %+v", row.RawEvent)
	}
	if !strings.Contains(row.Summary, "💭") || !strings.Contains(row.Summary, "2 delta") {
		t.Errorf("summary = %q, want 💭 + \"2 delta\"", row.Summary)
	}
	if !strings.Contains(row.Summary, "▶") {
		t.Errorf("collapsed summary must show ▶ fold marker: %q", row.Summary)
	}
	// 折叠态不预建全文（Story 60.2 code-review Patch P1：避免热路径重建无渲染
	// 消费者的 Detail · API driver 单会话上万 delta）；全文经展开时按需还原，
	// 见 TestCollapseThinkingGroups_ExpandedEmitsTextRows。
	if row.Detail != "" {
		t.Errorf("collapsed summary Detail = %q, want empty (lazy reconstruct)", row.Detail)
	}
}

// AC#1 防刷屏：上万 delta 仍折叠为 1 行（绝不逐 delta 占行）。
func TestCollapseThinkingGroups_HugeRunFoldsToOne(t *testing.T) {
	const n = 10000
	raw := make([]event.UnifiedEvent, 0, n+1)
	raw = append(raw, thinkEv("started", "started", 1000))
	for i := range n {
		raw = append(raw, thinkEv("delta", "x", int64(1001+i)))
	}
	out := CollapseThinkingGroups(raw, nil, false)
	if len(out) != 1 {
		t.Fatalf("1 started + %d delta: want 1 folded row, got %d", n, len(out))
	}
}

// AC#4：非 DriverThinking 事件原样透传 · thinking 块夹在中间正确分隔。
func TestCollapseThinkingGroups_PassesThroughNonThinking(t *testing.T) {
	raw := []event.UnifiedEvent{
		nonThinkEv("DriverToolCall"),
		thinkEv("started", "started", 1000),
		thinkEv("delta", "thinking", 1010),
		nonThinkEv("DriverInit"),
	}
	out := CollapseThinkingGroups(raw, nil, false)
	// tool(1) + thinking-fold(1) + init(1) = 3 行。
	if len(out) != 3 {
		t.Fatalf("want 3 rows (tool + fold + init), got %d", len(out))
	}
	if out[0].RawEvent == nil || out[0].RawEvent.Syscall != "DriverToolCall" {
		t.Errorf("row0 should passthrough DriverToolCall, got %+v", out[0].RawEvent)
	}
	if out[1].Type != event.EventThinking {
		t.Errorf("row1 should be folded thinking, got %q", out[1].Type)
	}
	if out[2].RawEvent == nil || out[2].RawEvent.Syscall != "DriverInit" {
		t.Errorf("row2 should passthrough DriverInit, got %+v", out[2].RawEvent)
	}
}

// 无 thinking 事件 → 原样返回（零折叠）。
func TestCollapseThinkingGroups_NoThinkingReturnsRaw(t *testing.T) {
	raw := []event.UnifiedEvent{nonThinkEv("DriverToolCall"), nonThinkEv("DriverInit")}
	out := CollapseThinkingGroups(raw, nil, false)
	if len(out) != 2 {
		t.Fatalf("no thinking: want raw (2 rows), got %d", len(out))
	}
}

// AC#2：展开块在摘要行后投影出正文行（▼ marker + 缩进文本）。
func TestCollapseThinkingGroups_ExpandedEmitsTextRows(t *testing.T) {
	raw := []event.UnifiedEvent{
		thinkEv("started", "started", 1000),
		thinkEv("delta", "hello world", 1010),
	}
	expanded := map[int64]bool{1000: true}
	out := CollapseThinkingGroups(raw, expanded, false)
	if len(out) < 2 {
		t.Fatalf("expanded block: want summary + text row(s), got %d", len(out))
	}
	if !strings.Contains(out[0].Summary, "▼") {
		t.Errorf("expanded summary must show ▼ open marker: %q", out[0].Summary)
	}
	// 展开态摘要行 Detail 携带还原全文（折叠态则留空 · Patch P1 lazy 还原）。
	if out[0].Detail != "hello world" {
		t.Errorf("expanded summary Detail = %q, want reconstructed full text", out[0].Detail)
	}
	// 正文行 RawEvent==nil（区别于摘要行）· 含还原文本。
	if out[1].RawEvent != nil {
		t.Errorf("text row should have nil RawEvent (distinguish from summary), got %+v", out[1].RawEvent)
	}
	if !strings.Contains(out[1].Summary, "hello world") {
		t.Errorf("text row should contain reconstructed text, got %q", out[1].Summary)
	}
}

// AC#2「可截断/限高」：超 MaxThinkingExpandLines 的展开正文被截断 + 尾标。
func TestCollapseThinkingGroups_ExpandedTruncatesLongText(t *testing.T) {
	// 构造远超 12 行的思考全文（每行 thinkingExpandWrapWidth 宽 → 多行）。
	var big strings.Builder
	for range MaxThinkingExpandLines + 10 {
		big.WriteString(strings.Repeat("a", thinkingExpandWrapWidth))
		big.WriteString("\n")
	}
	raw := []event.UnifiedEvent{
		thinkEv("started", "started", 1000),
		thinkEv("delta", big.String(), 1010),
	}
	out := CollapseThinkingGroups(raw, map[int64]bool{1000: true}, false)
	// summary(1) + 最多 MaxThinkingExpandLines 正文 + 1 截断尾标。
	if len(out) > 1+MaxThinkingExpandLines+1 {
		t.Fatalf("expanded rows not bounded: got %d, want ≤ %d", len(out), 1+MaxThinkingExpandLines+1)
	}
	last := out[len(out)-1]
	if !strings.Contains(last.Summary, "truncated") {
		t.Errorf("last row should be truncation marker, got %q", last.Summary)
	}
}

// AC#4：折叠发生在 FilterDebugEvents 之后 → ShowStrace=false 时 syscall 事件先被
// 过滤掉 → CollapseThinkingGroups 看不到 thinking 事件 → 块整体隐藏。
func TestCollapseThinkingGroups_HiddenWhenSyscallFilteredOut(t *testing.T) {
	raw := []event.UnifiedEvent{
		thinkEv("started", "started", 1000),
		thinkEv("delta", "x", 1010),
	}
	// ShowStrace=false → FilterDebugEvents 丢弃所有 EventSyscall。
	state := DebugState{Events: raw, ShowStrace: false}
	filtered := FilterDebugEvents(state, nil)
	out := CollapseThinkingGroups(filtered, nil, false)
	if len(out) != 0 {
		t.Fatalf("syscall filtered out: thinking block must be fully hidden, got %d rows", len(out))
	}
	// ShowStrace=true → 显示 → 折叠为 1 行。
	state.ShowStrace = true
	out = CollapseThinkingGroups(FilterDebugEvents(state, nil), nil, false)
	if len(out) != 1 {
		t.Fatalf("ShowStrace on: want 1 folded row, got %d", len(out))
	}
}

func TestCollapseThinkingGroups_ASCIIMarkers(t *testing.T) {
	raw := []event.UnifiedEvent{
		thinkEv("started", "started", 1000),
		thinkEv("delta", "x", 1010),
	}
	collapsed := CollapseThinkingGroups(raw, nil, true)
	if !strings.Contains(collapsed[0].Summary, ">") || !strings.Contains(collapsed[0].Summary, "[think]") {
		t.Errorf("ascii collapsed summary want > + [think], got %q", collapsed[0].Summary)
	}
	if strings.Contains(collapsed[0].Summary, "💭") || strings.Contains(collapsed[0].Summary, "▶") {
		t.Errorf("ascii summary must not contain Unicode glyphs: %q", collapsed[0].Summary)
	}
	expanded := CollapseThinkingGroups(raw, map[int64]bool{1000: true}, true)
	if !strings.Contains(expanded[0].Summary, "v") {
		t.Errorf("ascii expanded summary want v open marker, got %q", expanded[0].Summary)
	}
}

// Patch P2（Story 60.2 code-review）：wrapThinkingText 按**显示宽度**（CJK 占 2 列）
// 换行，而非 rune 数。10 个全角中文 = 20 显示列，width=10 → 切成 2 行各 5 个中文
// （旧 rune-count 逻辑会误判为 10 rune ≤ 10 → 单行、再被外层 truncateAnsi 砍掉尾半）。
func TestWrapThinkingText_CJKWrapsByDisplayWidth(t *testing.T) {
	const cjk = "一二三四五六七八九十" // 10 个全角 rune = 20 显示列
	lines := wrapThinkingText(cjk, 10)
	if len(lines) != 2 {
		t.Fatalf("10 CJK runes @ display-width=10: want 2 lines, got %d (%q)", len(lines), lines)
	}
	// 每行 ≤ 5 个 CJK rune（5×2 列 = 10 显示列），坐实「按显示宽度切」。
	for _, ln := range lines {
		if n := len([]rune(ln)); n > 5 {
			t.Errorf("line has %d runes, want ≤ 5 (5 CJK = 10 display cols): %q", n, ln)
		}
	}
}
