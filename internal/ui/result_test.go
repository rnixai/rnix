package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderResult_Border(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true}}

	RenderResult(r, "分析结果", "发现 2 个性能瓶颈")

	output := buf.String()
	if !strings.Contains(output, "══ 分析结果 ══") {
		t.Errorf("missing top border with title, got %q", output)
	}
	if !strings.Contains(output, "  发现 2 个性能瓶颈") {
		t.Errorf("missing indented content, got %q", output)
	}
	// Bottom border should be all ═
	lines := strings.Split(output, "\n")
	found := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && strings.Trim(trimmed, "═") == "" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing bottom border of ═ characters, got:\n%s", output)
	}
}

func TestRenderResult_WidthAdapt(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})

	t.Run("narrow_terminal", func(t *testing.T) {
		var buf bytes.Buffer
		r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 40, ColorLevel: 0}}

		RenderResult(r, "Result", "content")

		output := buf.String()
		if !strings.Contains(output, "══ Result ══") {
			t.Errorf("missing border, got %q", output)
		}
	})

	t.Run("wide_terminal_capped_120", func(t *testing.T) {
		var buf bytes.Buffer
		r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 200, ColorLevel: 0}}

		RenderResult(r, "Result", "content")

		lines := strings.SplitSeq(buf.String(), "\n")
		// Bottom border should be capped at 120 runes
		for line := range lines {
			if len([]rune(line)) > 0 && strings.Trim(line, "═") == "" {
				if len([]rune(line)) != 120 {
					t.Errorf("expected bottom border of 120 chars, got %d", len([]rune(line)))
				}
			}
		}
	})
}

func TestRenderResult_NoColor(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderResult(r, "Title", "content")

	output := buf.String()
	// Should not contain ANSI escape codes
	if strings.Contains(output, "\x1b[") {
		t.Errorf("should not contain ANSI codes in no-color mode, got %q", output)
	}
	// Should still have ══ characters
	if !strings.Contains(output, "══") {
		t.Errorf("should preserve ══ characters in no-color mode, got %q", output)
	}
}

func TestRenderResult_QuietMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderResult(r, "Title", "content")

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestRenderResult_JSONMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderResult(r, "Title", "content")

	if buf.Len() != 0 {
		t.Errorf("expected no output in JSON mode, got %q", buf.String())
	}
}

func TestRenderResult_MultilineContent(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderResult(r, "Results", "line1\nline2\nline3")

	output := buf.String()
	if !strings.Contains(output, "  line1\n") {
		t.Errorf("missing indented line1, got %q", output)
	}
	if !strings.Contains(output, "  line2\n") {
		t.Errorf("missing indented line2, got %q", output)
	}
	if !strings.Contains(output, "  line3\n") {
		t.Errorf("missing indented line3, got %q", output)
	}
}
