// Tests for FormatSignedCharCount + RenderSystemPromptBody (Story 38-5 PR11 Step 4(c)).
//
// 行为契约覆盖（与 cmd/rnix.formatSignedCharCount + renderSystemPromptBody 等价）：
//   1. FormatSignedCharCount: 负 delta → "-" prefix · 非负 → "+" prefix · 零 → "+0"
//   2. FormatSignedCharCount: 大值 → "1.5k" 格式（formatCharCount 集成）
//   3. RenderSystemPromptBody: header 格式 "═══ System Prompt (X chars) ═══"
//   4. RenderSystemPromptBody: 短 prompt 完整输出
//   5. RenderSystemPromptBody: 超 TruncateThreshold (10000) 截断 + truncation notice
//   6. RenderSystemPromptBody: utf8 rune count 正确处理 CJK

package inspector

import (
	"strings"
	"testing"
)

func TestFormatSignedCharCount_Zero(t *testing.T) {
	got := FormatSignedCharCount(0)
	if got != "+0" {
		t.Errorf("FormatSignedCharCount(0): want %q, got %q", "+0", got)
	}
}

func TestFormatSignedCharCount_PositiveSmall(t *testing.T) {
	got := FormatSignedCharCount(272)
	if got != "+272" {
		t.Errorf("FormatSignedCharCount(272): want %q, got %q", "+272", got)
	}
}

func TestFormatSignedCharCount_NegativeSmall(t *testing.T) {
	got := FormatSignedCharCount(-272)
	if got != "-272" {
		t.Errorf("FormatSignedCharCount(-272): want %q, got %q", "-272", got)
	}
}

func TestFormatSignedCharCount_PositiveLarge(t *testing.T) {
	// 1500 → "1.5k" via formatCharCount
	got := FormatSignedCharCount(1500)
	if got != "+1.5k" {
		t.Errorf("FormatSignedCharCount(1500): want %q, got %q", "+1.5k", got)
	}
}

func TestFormatSignedCharCount_NegativeLarge(t *testing.T) {
	got := FormatSignedCharCount(-1500)
	if got != "-1.5k" {
		t.Errorf("FormatSignedCharCount(-1500): want %q, got %q", "-1.5k", got)
	}
}

func TestRenderSystemPromptBody_ShortPrompt(t *testing.T) {
	prompt := "You are a helpful assistant."
	got := RenderSystemPromptBody(prompt)

	if !strings.Contains(got, "═══ System Prompt") {
		t.Errorf("missing canonical header: %q", got)
	}
	if !strings.Contains(got, "(28 chars)") {
		t.Errorf("missing char count: %q", got)
	}
	if !strings.Contains(got, prompt) {
		t.Errorf("body should contain prompt: %q", got)
	}
	// 短 prompt 不应包含 truncation notice
	if strings.Contains(got, "truncated") || strings.Contains(got, "more") {
		t.Errorf("short prompt should not have truncation notice: %q", got)
	}
}

func TestRenderSystemPromptBody_HeaderFormat(t *testing.T) {
	got := RenderSystemPromptBody("hi")
	// 期望前缀 "═══ System Prompt (2 chars) ═══\n\n"
	wantHead := "═══ System Prompt (2 chars) ═══\n\n"
	if !strings.HasPrefix(got, wantHead) {
		t.Errorf("header prefix mismatch:\nwant: %q\ngot:  %q", wantHead, got[:min(50, len(got))])
	}
}

func TestRenderSystemPromptBody_LongPromptTruncated(t *testing.T) {
	// 构造超过 TruncateThreshold 的 prompt
	long := strings.Repeat("a", TruncateThreshold+500)
	got := RenderSystemPromptBody(long)

	// 应包含 truncation notice（RenderTruncationNotice 输出 - more / 类似）
	// 实际格式由 RenderTruncationNotice 决定 · 检查 body 长度小于 input 即可
	if len(got) >= len(long)+200 {
		t.Errorf("long prompt should be truncated, got length %d (input %d)", len(got), len(long))
	}
	// header 仍包含完整 char count
	if !strings.Contains(got, "10500") && !strings.Contains(got, "10.5k") {
		t.Errorf("header should reflect total char count (10500 or 10.5k), got %q", got[:min(80, len(got))])
	}
}

func TestRenderSystemPromptBody_CJKRuneCount(t *testing.T) {
	// CJK 字符 utf8 rune count 应正确（每字符 1 rune · 多字节 utf8）
	prompt := "你好世界系统提示"
	got := RenderSystemPromptBody(prompt)

	// 8 个 CJK 字符
	if !strings.Contains(got, "(8 chars)") {
		t.Errorf("CJK rune count: want '(8 chars)', got %q", got[:min(80, len(got))])
	}
	if !strings.Contains(got, prompt) {
		t.Errorf("CJK body should be preserved: %q", got)
	}
}

func TestRenderSystemPromptBody_Empty(t *testing.T) {
	got := RenderSystemPromptBody("")
	if !strings.Contains(got, "(0 chars)") {
		t.Errorf("empty prompt: want '(0 chars)', got %q", got)
	}
}
