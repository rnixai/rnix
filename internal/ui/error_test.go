package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderError_ThreeLines(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true}}

	RenderError(r, "/dev/llm/claude", "connection refused", "智能体无法推理", "检查网络连接")

	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), output)
	}

	if !strings.Contains(lines[0], "✗") {
		t.Errorf("line 1 missing ✗ prefix, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "/dev/llm/claude") {
		t.Errorf("line 1 missing device, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "connection refused") {
		t.Errorf("line 1 missing reason, got %q", lines[0])
	}

	if !strings.Contains(lines[1], "→") {
		t.Errorf("line 2 missing → arrow, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "智能体无法推理") {
		t.Errorf("line 2 missing impact, got %q", lines[1])
	}

	if !strings.Contains(lines[2], "→") {
		t.Errorf("line 3 missing → arrow, got %q", lines[2])
	}
	if !strings.Contains(lines[2], "suggestion:") {
		t.Errorf("line 3 missing suggestion: prefix, got %q", lines[2])
	}
	if !strings.Contains(lines[2], "检查网络连接") {
		t.Errorf("line 3 missing suggestion, got %q", lines[2])
	}
}

func TestRenderError_NoColor(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: false}}

	RenderError(r, "/dev/llm/claude", "timeout", "impact", "suggestion")

	output := buf.String()
	if !strings.Contains(output, "[ERR]") {
		t.Errorf("expected [ERR] prefix in no-color + no-unicode mode, got %q", output)
	}
	if strings.Contains(output, "✗") {
		t.Errorf("should not contain ✗ in ASCII mode, got %q", output)
	}
	// Should not contain ANSI escape codes
	if strings.Contains(output, "\x1b[") {
		t.Errorf("should not contain ANSI codes, got %q", output)
	}
}

func TestRenderError_QuietMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderError(r, "/dev/llm/claude", "error", "impact", "suggestion")

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestRenderError_JSONMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderError(r, "/dev/llm/claude", "error", "impact", "suggestion")

	if buf.Len() != 0 {
		t.Errorf("expected no output in JSON mode, got %q", buf.String())
	}
}

func TestRenderError_ASCIIArrow(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: false}}

	RenderError(r, "dev", "reason", "impact", "suggestion")

	output := buf.String()
	if !strings.Contains(output, "->") {
		t.Errorf("expected -> arrow in ASCII mode, got %q", output)
	}
}
