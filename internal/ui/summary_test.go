package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderSummary_Success(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderSummary(r, 1, 0, 1847, 6200*time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "[kernel]") {
		t.Errorf("missing [kernel] prefix, got %q", output)
	}
	if !strings.Contains(output, "PID 1 exited(0)") {
		t.Errorf("missing PID exit info, got %q", output)
	}
	if !strings.Contains(output, "tokens: 1847") {
		t.Errorf("missing tokens info, got %q", output)
	}
	if !strings.Contains(output, "elapsed: 6.2s") {
		t.Errorf("missing elapsed info, got %q", output)
	}
}

func TestRenderSummary_Failure(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderSummary(r, 2, 1, 500, 3*time.Second)

	output := buf.String()
	if !strings.Contains(output, "PID 2 exited(1)") {
		t.Errorf("missing PID exit info for failure, got %q", output)
	}
	if !strings.Contains(output, "tokens: 500") {
		t.Errorf("missing tokens info, got %q", output)
	}
	if !strings.Contains(output, "elapsed: 3.0s") {
		t.Errorf("missing elapsed info, got %q", output)
	}
}

func TestRenderSummary_QuietMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderSummary(r, 1, 0, 100, time.Second)

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestRenderSummary_JSONMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderSummary(r, 1, 0, 100, time.Second)

	if buf.Len() != 0 {
		t.Errorf("expected no output in JSON mode, got %q", buf.String())
	}
}

func TestRenderSummary_Format(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderSummary(r, 5, 0, 42, 100*time.Millisecond)

	output := buf.String()
	// Verify pipe separators
	if strings.Count(output, "|") != 2 {
		t.Errorf("expected 2 pipe separators, got %d in %q", strings.Count(output, "|"), output)
	}
}
