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
