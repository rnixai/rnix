package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestKernelMessage_Format(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.KernelMessage("spawning PID %d...", 1)

	output := buf.String()
	if !strings.Contains(output, "[kernel]") {
		t.Errorf("expected [kernel] prefix, got %q", output)
	}
	if !strings.Contains(output, "spawning PID 1...") {
		t.Errorf("expected message content, got %q", output)
	}
}

func TestAgentMessage_Format(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentMessage(1, "processing intent")

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("expected [agent/1] prefix, got %q", output)
	}
	if !strings.Contains(output, "processing intent") {
		t.Errorf("expected message content, got %q", output)
	}
}

func TestAgentStep_Format(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStep(1, 2, 3)

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("expected [agent/1] prefix, got %q", output)
	}
	if !strings.Contains(output, "reasoning step 2...") {
		t.Errorf("expected reasoning step format, got %q", output)
	}
}

func TestProgress_QuietMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.KernelMessage("should not appear")
	p.AgentMessage(1, "should not appear")
	p.AgentStep(1, 1, 3)

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestProgress_JSONMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.KernelMessage("should not appear")
	p.AgentMessage(1, "should not appear")
	p.AgentStep(1, 1, 3)

	if buf.Len() != 0 {
		t.Errorf("expected no output in JSON mode, got %q", buf.String())
	}
}
