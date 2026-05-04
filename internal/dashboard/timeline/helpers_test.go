// Package timeline — helpers_test.go (Story 38-5 PR11 Step 4(a-2) timeline 纯 helpers
// 迁出测试 · 同 alertstrip Step 4(a-2) AlertStripHeight test 模式)
//
// 测试覆盖：
//   - ActionColor 8 项（7 known + 1 default fallback）
//   - ActionAbbrev 5 项（4 known + 1 passthrough）
//   - ShortenArgs 4 项（短输入 / 长输入 / 多行 / 边界）
//   - FormatDefaultLine 5 项（detail 主路径 / detail Summary 空 / detail ToolInput
//     fallback / detail nil StepSummaryWire fallback / 全空）
//   - FormatTokenCount 3 项（k 后缀 / 个位 / 边界 999/1000）
//   - FormatDurationMs 3 项（µs / ms / s · 与 trace/eval 同测试矩阵）
//   - TruncateRuneWidth 4 项（短字符串 / 长字符串 / CJK 双宽 / 边界）
//   - TruncateAnsi 4 项（短 / 长 / maxWidth=0 / maxWidth<0）
//   - FormatCharCount 3 项（k / 个位 / 边界）
//   - HasExpandableContent 7 项（nil detail / 全空 / ToolPath/Input/Error/Result/RawResponse/Token 各 1）
//   - EstimateExpandedLines 6 项（fallback 1 / Path / Input / Result clamp / Error 多行 clamp / Token）
//   - EstimateDebugLines 4 项（空 messages / 满 messages / over-cap clamp / 边界 6）
//   - FilteredStepEntries 6 项（nil filters / 空 filters / all-on shortcut /
//     filter 单一 action / filter 多 action / 全空 entries）
package timeline

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// ActionColor (8 项)
// =============================================================================

func TestActionColor_Known(t *testing.T) {
	cases := []struct {
		action string
		want   lipgloss.Color
	}{
		{"tool_call", "#6BCB77"},
		{"plan", "#5B9BD5"},
		{"text", "#FFFFFF"},
		{"complete", "#6BCB77"},
		{"spawn", "#9B59B6"},
		{"specialize", "#4EC9B0"},
		{"replan", "#E5C07B"},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			if got := ActionColor(c.action); got != c.want {
				t.Errorf("ActionColor(%q) = %q, want %q", c.action, got, c.want)
			}
		})
	}
}

func TestActionColor_DefaultFallback(t *testing.T) {
	if got := ActionColor("unknown"); got != lipgloss.Color("#FFFFFF") {
		t.Errorf("ActionColor(unknown) = %q, want #FFFFFF", got)
	}
	if got := ActionColor(""); got != lipgloss.Color("#FFFFFF") {
		t.Errorf("ActionColor('') = %q, want #FFFFFF", got)
	}
}

// =============================================================================
// ActionAbbrev (5 项)
// =============================================================================

func TestActionAbbrev_Known(t *testing.T) {
	cases := []struct {
		action string
		want   string
	}{
		{"tool_call", "tool"},
		{"complete", "done"},
		{"specialize", "spec"},
		{"text", "x"},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			if got := ActionAbbrev(c.action); got != c.want {
				t.Errorf("ActionAbbrev(%q) = %q, want %q", c.action, got, c.want)
			}
		})
	}
}

func TestActionAbbrev_PassthroughDefault(t *testing.T) {
	if got := ActionAbbrev("plan"); got != "plan" {
		t.Errorf("ActionAbbrev(plan) = %q, want passthrough 'plan'", got)
	}
	if got := ActionAbbrev("custom"); got != "custom" {
		t.Errorf("ActionAbbrev(custom) = %q, want passthrough 'custom'", got)
	}
}

// =============================================================================
// ShortenArgs (4 项)
// =============================================================================

func TestShortenArgs_ShortInput(t *testing.T) {
	if got := ShortenArgs("hello", 20); got != "hello" {
		t.Errorf("ShortenArgs(hello, 20) = %q, want 'hello'", got)
	}
}

func TestShortenArgs_LongInput(t *testing.T) {
	got := ShortenArgs("aaaaabbbbbcccccddddd", 10)
	if !strings.Contains(got, "…") {
		t.Errorf("ShortenArgs long input should end with …, got %q", got)
	}
	// rune width should be exactly 10 after truncation
	if len([]rune(got)) > 10 {
		t.Errorf("ShortenArgs long input rune width %d > 10", len([]rune(got)))
	}
}

func TestShortenArgs_MultiLineTakesFirst(t *testing.T) {
	got := ShortenArgs("first\nsecond\nthird", 20)
	if got != "first" {
		t.Errorf("ShortenArgs multi-line = %q, want only 'first'", got)
	}
}

func TestShortenArgs_ExactBoundary(t *testing.T) {
	// rune width == maxLen → no truncation
	got := ShortenArgs("hello", 5)
	if got != "hello" {
		t.Errorf("ShortenArgs exact boundary = %q, want 'hello'", got)
	}
}

// =============================================================================
// FormatDefaultLine (5 项)
// =============================================================================

func TestFormatDefaultLine_DetailPrimary(t *testing.T) {
	s := ipc.StepSummaryWire{Action: "fallback", Summary: "fallback summary"}
	detail := &ipc.GetStepDetailResponse{Action: "primary", Summary: "primary summary"}
	action, summary := FormatDefaultLine(s, detail)
	if action != "primary" {
		t.Errorf("action: want 'primary', got %q", action)
	}
	if summary != "primary summary" {
		t.Errorf("summary: want 'primary summary', got %q", summary)
	}
}

func TestFormatDefaultLine_DetailToolPathOverride(t *testing.T) {
	s := ipc.StepSummaryWire{Action: "fallback"}
	detail := &ipc.GetStepDetailResponse{Action: "primary", ToolPath: "Read"}
	action, _ := FormatDefaultLine(s, detail)
	if action != "Read" {
		t.Errorf("ToolPath should override Action: want 'Read', got %q", action)
	}
}

func TestFormatDefaultLine_DetailToolInputFallback(t *testing.T) {
	s := ipc.StepSummaryWire{}
	detail := &ipc.GetStepDetailResponse{Action: "tool_call", ToolInput: "long input here"}
	_, summary := FormatDefaultLine(s, detail)
	if summary == "" {
		t.Errorf("Summary should fallback to ShortenArgs(ToolInput), got empty")
	}
}

func TestFormatDefaultLine_NilDetailFallback(t *testing.T) {
	s := ipc.StepSummaryWire{Action: "tool_call", ToolPath: "Read", Summary: "wire summary"}
	action, summary := FormatDefaultLine(s, nil)
	if action != "Read" {
		t.Errorf("action: want 'Read' (ToolPath override), got %q", action)
	}
	if summary != "wire summary" {
		t.Errorf("summary: want 'wire summary', got %q", summary)
	}
}

func TestFormatDefaultLine_AllEmpty(t *testing.T) {
	s := ipc.StepSummaryWire{}
	action, summary := FormatDefaultLine(s, nil)
	if action != "" {
		t.Errorf("all empty: want action='', got %q", action)
	}
	if summary != "" {
		t.Errorf("all empty: want summary='', got %q", summary)
	}
}

// =============================================================================
// FormatTokenCount (3 项)
// =============================================================================

func TestFormatTokenCount_KSuffix(t *testing.T) {
	if got := FormatTokenCount(1500); got != "1.5k" {
		t.Errorf("FormatTokenCount(1500) = %q, want '1.5k'", got)
	}
}

func TestFormatTokenCount_Single(t *testing.T) {
	if got := FormatTokenCount(42); got != "42" {
		t.Errorf("FormatTokenCount(42) = %q, want '42'", got)
	}
}

func TestFormatTokenCount_Boundary(t *testing.T) {
	if got := FormatTokenCount(999); got != "999" {
		t.Errorf("FormatTokenCount(999) = %q, want '999'", got)
	}
	if got := FormatTokenCount(1000); got != "1.0k" {
		t.Errorf("FormatTokenCount(1000) = %q, want '1.0k'", got)
	}
}

// =============================================================================
// FormatDurationMs (3 项 · 与 trace/eval 同测试矩阵)
// =============================================================================

func TestFormatDurationMs_Microseconds(t *testing.T) {
	if got := FormatDurationMs(0.5); got != "500µs" {
		t.Errorf("FormatDurationMs(0.5) = %q, want '500µs'", got)
	}
}

func TestFormatDurationMs_Milliseconds(t *testing.T) {
	if got := FormatDurationMs(123.4); got != "123ms" {
		t.Errorf("FormatDurationMs(123.4) = %q, want '123ms'", got)
	}
}

func TestFormatDurationMs_Seconds(t *testing.T) {
	if got := FormatDurationMs(2345); got != "2.35s" {
		t.Errorf("FormatDurationMs(2345) = %q, want '2.35s'", got)
	}
}

// =============================================================================
// TruncateRuneWidth (4 项)
// =============================================================================

func TestTruncateRuneWidth_ShortPassthrough(t *testing.T) {
	if got := TruncateRuneWidth("hello", 10); got != "hello" {
		t.Errorf("TruncateRuneWidth(hello, 10) = %q, want 'hello'", got)
	}
}

func TestTruncateRuneWidth_LongTruncated(t *testing.T) {
	got := TruncateRuneWidth("hello world", 8)
	if !strings.Contains(got, "…") {
		t.Errorf("TruncateRuneWidth long should end with …, got %q", got)
	}
}

func TestTruncateRuneWidth_CJKDoubleWidth(t *testing.T) {
	// 中 = 2 width · "中文" = 4 width
	if got := TruncateRuneWidth("中文", 4); got != "中文" {
		t.Errorf("TruncateRuneWidth(中文, 4) = %q, want '中文'", got)
	}
	// 中文 truncated to 3 width should become "中…"
	got := TruncateRuneWidth("中文", 3)
	if !strings.Contains(got, "…") {
		t.Errorf("TruncateRuneWidth CJK truncated should contain …, got %q", got)
	}
}

func TestTruncateRuneWidth_Boundary(t *testing.T) {
	// rune width == maxWidth → passthrough
	if got := TruncateRuneWidth("hello", 5); got != "hello" {
		t.Errorf("TruncateRuneWidth boundary = %q, want 'hello'", got)
	}
}

// =============================================================================
// TruncateAnsi (4 项)
// =============================================================================

func TestTruncateAnsi_ShortPassthrough(t *testing.T) {
	if got := TruncateAnsi("hello", 10); got != "hello" {
		t.Errorf("TruncateAnsi(hello, 10) = %q, want 'hello'", got)
	}
}

func TestTruncateAnsi_LongTruncated(t *testing.T) {
	long := "abcdefghijklmnop"
	got := TruncateAnsi(long, 5)
	if lipgloss.Width(got) > 5 {
		t.Errorf("TruncateAnsi width %d > maxWidth 5", lipgloss.Width(got))
	}
}

func TestTruncateAnsi_ZeroWidth(t *testing.T) {
	if got := TruncateAnsi("hello", 0); got != "" {
		t.Errorf("TruncateAnsi(_, 0) should return '', got %q", got)
	}
}

func TestTruncateAnsi_NegativeWidth(t *testing.T) {
	if got := TruncateAnsi("hello", -5); got != "" {
		t.Errorf("TruncateAnsi(_, -5) should return '', got %q", got)
	}
}

// =============================================================================
// FormatCharCount (3 项)
// =============================================================================

func TestFormatCharCount_KSuffix(t *testing.T) {
	if got := FormatCharCount(1500); got != "1.5k" {
		t.Errorf("FormatCharCount(1500) = %q, want '1.5k'", got)
	}
}

func TestFormatCharCount_Single(t *testing.T) {
	if got := FormatCharCount(42); got != "42" {
		t.Errorf("FormatCharCount(42) = %q, want '42'", got)
	}
}

func TestFormatCharCount_Boundary(t *testing.T) {
	if got := FormatCharCount(999); got != "999" {
		t.Errorf("FormatCharCount(999) = %q, want '999'", got)
	}
	if got := FormatCharCount(1000); got != "1.0k" {
		t.Errorf("FormatCharCount(1000) = %q, want '1.0k'", got)
	}
}

// =============================================================================
// HasExpandableContent (7 项)
// =============================================================================

func TestHasExpandableContent_NilDetail(t *testing.T) {
	if !HasExpandableContent(nil, ipc.StepSummaryWire{}) {
		t.Error("nil detail should be treated as potentially expandable (true)")
	}
}

func TestHasExpandableContent_AllEmpty(t *testing.T) {
	if HasExpandableContent(&ipc.GetStepDetailResponse{}, ipc.StepSummaryWire{}) {
		t.Error("all-empty detail should not be expandable (false)")
	}
}

func TestHasExpandableContent_ToolPathDifferent(t *testing.T) {
	d := &ipc.GetStepDetailResponse{ToolPath: "fs.read_file"}
	s := ipc.StepSummaryWire{Summary: "loading", ToolPath: ""}
	if !HasExpandableContent(d, s) {
		t.Error("ToolPath != Level 1 displaySummary should be expandable")
	}
}

func TestHasExpandableContent_ToolPathMatchesShortSummary(t *testing.T) {
	// When s.ToolPath != "" and len(s.Summary) < 8, displayedAsSummary becomes s.ToolPath
	// → if detail.ToolPath equals it, no new info
	d := &ipc.GetStepDetailResponse{ToolPath: "fs.read"}
	s := ipc.StepSummaryWire{Summary: "abc", ToolPath: "fs.read"}
	if HasExpandableContent(d, s) {
		t.Error("ToolPath matching Level 1 short-summary fallback should not be expandable")
	}
}

func TestHasExpandableContent_ToolInputNonempty(t *testing.T) {
	d := &ipc.GetStepDetailResponse{ToolInput: `{"path":"/tmp/x"}`}
	if !HasExpandableContent(d, ipc.StepSummaryWire{}) {
		t.Error("ToolInput non-empty should be expandable")
	}
}

func TestHasExpandableContent_ErrorOrResult(t *testing.T) {
	d1 := &ipc.GetStepDetailResponse{ToolError: "permission denied"}
	if !HasExpandableContent(d1, ipc.StepSummaryWire{}) {
		t.Error("ToolError non-empty should be expandable")
	}
	d2 := &ipc.GetStepDetailResponse{ToolResult: "OK"}
	if !HasExpandableContent(d2, ipc.StepSummaryWire{}) {
		t.Error("ToolResult non-empty should be expandable")
	}
	d3 := &ipc.GetStepDetailResponse{RawResponse: "raw payload"}
	if !HasExpandableContent(d3, ipc.StepSummaryWire{}) {
		t.Error("RawResponse non-empty should be expandable")
	}
}

func TestHasExpandableContent_TokenBreakdownMismatch(t *testing.T) {
	// breakdown 100+200 = 300 ≠ s.TokenCount 250 → expandable
	d := &ipc.GetStepDetailResponse{RequestTokens: 100, ResponseTokens: 200}
	s := ipc.StepSummaryWire{TokenCount: 250}
	if !HasExpandableContent(d, s) {
		t.Error("token breakdown != s.TokenCount should be expandable")
	}
	// matching breakdown → not expandable
	s2 := ipc.StepSummaryWire{TokenCount: 300}
	if HasExpandableContent(d, s2) {
		t.Error("token breakdown matching s.TokenCount should not be expandable")
	}
}

// =============================================================================
// EstimateExpandedLines (6 项)
// =============================================================================

func TestEstimateExpandedLines_AllEmptyFallback(t *testing.T) {
	// 全空 → 至少 1 行 fallback（与 HasExpandableContent==false 边界保护对齐）
	if got := EstimateExpandedLines(&ipc.GetStepDetailResponse{}, ipc.StepSummaryWire{}); got != 1 {
		t.Errorf("EstimateExpandedLines(all-empty) = %d, want 1 (fallback)", got)
	}
}

func TestEstimateExpandedLines_PathLineCounted(t *testing.T) {
	d := &ipc.GetStepDetailResponse{ToolPath: "fs.read_file"}
	s := ipc.StepSummaryWire{Summary: "loading"}
	if got := EstimateExpandedLines(d, s); got != 1 {
		t.Errorf("EstimateExpandedLines(only path) = %d, want 1", got)
	}
}

func TestEstimateExpandedLines_InputAndPath(t *testing.T) {
	d := &ipc.GetStepDetailResponse{ToolPath: "fs.read", ToolInput: `{"path":"/x"}`}
	s := ipc.StepSummaryWire{Summary: "abc"}
	if got := EstimateExpandedLines(d, s); got != 2 {
		t.Errorf("EstimateExpandedLines(path+input) = %d, want 2", got)
	}
}

func TestEstimateExpandedLines_ResultClampedTo4(t *testing.T) {
	// ToolResult 10 行 → clamp 到 4
	result := strings.Repeat("line\n", 10)
	d := &ipc.GetStepDetailResponse{ToolResult: result}
	got := EstimateExpandedLines(d, ipc.StepSummaryWire{})
	if got != 4 {
		t.Errorf("EstimateExpandedLines(10-line result) = %d, want 4 (clamped)", got)
	}
}

func TestEstimateExpandedLines_ErrorTakesPrecedenceOverResult(t *testing.T) {
	// ToolError 非空 → Result/RawResponse 跳过（else if 链）
	d := &ipc.GetStepDetailResponse{
		ToolError:   "boom",
		ToolResult:  strings.Repeat("ignored\n", 5),
		RawResponse: "ignored",
	}
	if got := EstimateExpandedLines(d, ipc.StepSummaryWire{}); got != 1 {
		t.Errorf("EstimateExpandedLines(error+result+raw) = %d, want 1 (only error 1 line)", got)
	}
}

func TestEstimateExpandedLines_TokenLineCounted(t *testing.T) {
	d := &ipc.GetStepDetailResponse{RequestTokens: 100, ResponseTokens: 200}
	s := ipc.StepSummaryWire{TokenCount: 250} // mismatch → token line counted
	if got := EstimateExpandedLines(d, s); got != 1 {
		t.Errorf("EstimateExpandedLines(only token mismatch) = %d, want 1", got)
	}
}

// =============================================================================
// EstimateDebugLines (4 项)
// =============================================================================

func TestEstimateDebugLines_NoMessages(t *testing.T) {
	// 0 messages → 2 (separator + header) + 0 + 1 (hint) = 3
	if got := EstimateDebugLines(&ipc.GetStepDetailResponse{}); got != 3 {
		t.Errorf("EstimateDebugLines(0 msgs) = %d, want 3", got)
	}
}

func TestEstimateDebugLines_FewMessages(t *testing.T) {
	// 3 messages → 2 + 3 + 1 = 6
	d := &ipc.GetStepDetailResponse{Messages: make([]ipc.MessageWire, 3)}
	if got := EstimateDebugLines(d); got != 6 {
		t.Errorf("EstimateDebugLines(3 msgs) = %d, want 6", got)
	}
}

func TestEstimateDebugLines_OverCapClamped(t *testing.T) {
	// 100 messages → 2 + min(100, 6) + 1 = 9
	d := &ipc.GetStepDetailResponse{Messages: make([]ipc.MessageWire, 100)}
	if got := EstimateDebugLines(d); got != 9 {
		t.Errorf("EstimateDebugLines(100 msgs) = %d, want 9 (clamped)", got)
	}
}

func TestEstimateDebugLines_AtCap(t *testing.T) {
	// 6 messages exactly → 2 + 6 + 1 = 9
	d := &ipc.GetStepDetailResponse{Messages: make([]ipc.MessageWire, 6)}
	if got := EstimateDebugLines(d); got != 9 {
		t.Errorf("EstimateDebugLines(6 msgs) = %d, want 9", got)
	}
}

// =============================================================================
// FilteredStepEntries (6 项)
// =============================================================================

func TestFilteredStepEntries_NilFilters(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}}, {Summary: ipc.StepSummaryWire{Step: 2, Action: "plan"}}},
		StepFilters: nil,
	}
	got := FilteredStepEntries(state)
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("FilteredStepEntries(nil filters) = %v, want [0 1]", got)
	}
}

func TestFilteredStepEntries_EmptyFilters(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}}},
		StepFilters: map[string]bool{},
	}
	got := FilteredStepEntries(state)
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("FilteredStepEntries(empty map) = %v, want [0]", got)
	}
}

func TestFilteredStepEntries_AllOnShortcut(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}},
			{Summary: ipc.StepSummaryWire{Step: 2, Action: "plan"}},
			{Summary: ipc.StepSummaryWire{Step: 3, Action: "text"}},
		},
		StepFilters: map[string]bool{"tool_call": true, "plan": true, "text": true},
	}
	got := FilteredStepEntries(state)
	if len(got) != 3 {
		t.Errorf("FilteredStepEntries(all-on) = %v, want full indices", got)
	}
}

func TestFilteredStepEntries_SingleAction(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}},
			{Summary: ipc.StepSummaryWire{Step: 2, Action: "plan"}},
			{Summary: ipc.StepSummaryWire{Step: 3, Action: "tool_call"}},
		},
		StepFilters: map[string]bool{"tool_call": true, "plan": false},
	}
	got := FilteredStepEntries(state)
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("FilteredStepEntries(only tool_call) = %v, want [0 2]", got)
	}
}

func TestFilteredStepEntries_MultipleActions(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}},
			{Summary: ipc.StepSummaryWire{Step: 2, Action: "plan"}},
			{Summary: ipc.StepSummaryWire{Step: 3, Action: "text"}},
			{Summary: ipc.StepSummaryWire{Step: 4, Action: "spawn"}},
		},
		StepFilters: map[string]bool{"tool_call": true, "plan": true, "text": false, "spawn": false},
	}
	got := FilteredStepEntries(state)
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("FilteredStepEntries(tool_call+plan) = %v, want [0 1]", got)
	}
}

func TestFilteredStepEntries_EmptyEntries(t *testing.T) {
	// StepFilters="tool_call":true · 单一 filter 触发 allOn 分支（filters 全为 true）
	// → make([]int, 0) 返回 []int{} 空切片（与 cmd/rnix 原版行为对齐）
	state := TimelineState{
		StepEntries: nil,
		StepFilters: map[string]bool{"tool_call": true},
	}
	got := FilteredStepEntries(state)
	if len(got) != 0 {
		t.Errorf("FilteredStepEntries(empty entries) = %v, want empty slice", got)
	}
}
