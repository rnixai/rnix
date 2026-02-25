package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
)

func TestFormatTraceLine_BasicFormat(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	event := types.SyscallEvent{
		Timestamp: 12 * time.Millisecond,
		PID:       1,
		Syscall:   "Open",
		Args:      map[string]any{"flags": 2, "path": "/dev/null"},
		Result:    "FD(3)",
		Err:       nil,
		Duration:  1 * time.Millisecond,
	}

	got := FormatTraceLine(r, event, false)

	// Verify timestamp
	if !strings.Contains(got, "[  0.012s]") {
		t.Errorf("expected timestamp [  0.012s], got %q", got)
	}
	// Verify syscall name
	if !strings.Contains(got, "Open(") {
		t.Errorf("expected 'Open(' in output, got %q", got)
	}
	// Verify args (sorted: flags before path)
	if !strings.Contains(got, "flags=2") {
		t.Errorf("expected 'flags=2' in output, got %q", got)
	}
	if !strings.Contains(got, `path="/dev/null"`) {
		t.Errorf("expected path=\"/dev/null\" in output, got %q", got)
	}
	// Verify result with → separator
	if !strings.Contains(got, "→ FD(3)") {
		t.Errorf("expected '→ FD(3)' in output, got %q", got)
	}
	// Verify duration
	if !strings.Contains(got, "1ms") {
		t.Errorf("expected '1ms' in output, got %q", got)
	}
	// Verify no annotations
	if strings.Contains(got, "← 慢操作") {
		t.Errorf("unexpected slow annotation in output: %q", got)
	}
	if strings.Contains(got, "← LLM 调用") {
		t.Errorf("unexpected LLM annotation in output: %q", got)
	}
	// Verify no [ERR] prefix
	if strings.Contains(got, "[ERR]") {
		t.Errorf("unexpected [ERR] prefix in output: %q", got)
	}
}

func TestFormatTraceLine_ErrorHighlight(t *testing.T) {
	// ColorLevel > 0: error line uses ErrorStyle (no [ERR] prefix)
	// Note: lipgloss v1 strips ANSI in non-TTY test env, so we verify
	// the logic path (no [ERR] prefix) rather than ANSI bytes.
	InitStyles(TerminalProfile{ColorLevel: 3})
	rColor := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 3, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	event := types.SyscallEvent{
		Timestamp: 100 * time.Millisecond,
		PID:       1,
		Syscall:   "Write",
		Args:      map[string]any{"fd": 3},
		Result:    nil,
		Err:       errors.New("device not found"),
		Duration:  5 * time.Millisecond,
	}

	got := FormatTraceLine(rColor, event, false)

	// Should contain error result
	if !strings.Contains(got, "err(device not found)") {
		t.Errorf("expected 'err(device not found)' in output, got %q", got)
	}
	// Should NOT have [ERR] prefix in color mode
	if strings.Contains(got, "[ERR]") {
		t.Errorf("unexpected [ERR] prefix in color mode, got %q", got)
	}

	// ColorLevel = 0: error line uses [ERR] prefix
	InitStyles(TerminalProfile{ColorLevel: 0})
	rNoColor := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	got2 := FormatTraceLine(rNoColor, event, false)

	if !strings.HasPrefix(got2, "[ERR] ") {
		t.Errorf("expected [ERR] prefix in no-color mode, got %q", got2)
	}
	if strings.Contains(got2, "\x1b[") {
		t.Errorf("unexpected ANSI codes in no-color mode, got %q", got2)
	}
}

func TestFormatTraceLine_SlowOperation(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	// Slow non-error event: should have ← 慢操作
	event := types.SyscallEvent{
		Timestamp: 5 * time.Second,
		PID:       1,
		Syscall:   "Read",
		Args:      map[string]any{"fd": 3},
		Result:    1024,
		Err:       nil,
		Duration:  2 * time.Second,
	}

	got := FormatTraceLine(r, event, false)
	if !strings.Contains(got, "← 慢操作") {
		t.Errorf("expected slow annotation for > 1s duration, got %q", got)
	}

	// Error + slow: should NOT have ← 慢操作 (error line skips gray annotation)
	eventErr := types.SyscallEvent{
		Timestamp: 5 * time.Second,
		PID:       1,
		Syscall:   "Read",
		Args:      map[string]any{"fd": 3},
		Result:    nil,
		Err:       errors.New("timeout"),
		Duration:  2 * time.Second,
	}

	gotErr := FormatTraceLine(r, eventErr, false)
	if strings.Contains(gotErr, "← 慢操作") {
		t.Errorf("error line should NOT have slow annotation, got %q", gotErr)
	}
}

func TestFormatTraceLine_LLMAnnotation(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	// Test with "path" key containing /dev/llm/
	event := types.SyscallEvent{
		Timestamp: 2 * time.Millisecond,
		PID:       1,
		Syscall:   "Open",
		Args:      map[string]any{"path": "/dev/llm/claude", "flags": 2},
		Result:    "FD(3)",
		Err:       nil,
		Duration:  0,
	}

	got := FormatTraceLine(r, event, false)
	if !strings.Contains(got, "← LLM 调用") {
		t.Errorf("expected LLM annotation for path key, got %q", got)
	}

	// Test with "tool" key containing /dev/llm/
	event2 := types.SyscallEvent{
		Timestamp: 3 * time.Millisecond,
		PID:       1,
		Syscall:   "Exec",
		Args:      map[string]any{"tool": "/dev/llm/gpt"},
		Result:    nil,
		Err:       nil,
		Duration:  0,
	}

	got2 := FormatTraceLine(r, event2, false)
	if !strings.Contains(got2, "← LLM 调用") {
		t.Errorf("expected LLM annotation for tool key, got %q", got2)
	}

	// Test without LLM path
	event3 := types.SyscallEvent{
		Timestamp: 4 * time.Millisecond,
		PID:       1,
		Syscall:   "Read",
		Args:      map[string]any{"fd": 3},
		Result:    256,
		Err:       nil,
		Duration:  0,
	}

	got3 := FormatTraceLine(r, event3, false)
	if strings.Contains(got3, "← LLM 调用") {
		t.Errorf("unexpected LLM annotation without LLM path, got %q", got3)
	}
}

func TestFormatTraceLine_NoColor(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	event := types.SyscallEvent{
		Timestamp: 1 * time.Second,
		PID:       1,
		Syscall:   "Open",
		Args:      map[string]any{"path": "/dev/llm/claude"},
		Result:    "FD(3)",
		Err:       nil,
		Duration:  2 * time.Second,
	}

	got := FormatTraceLine(r, event, false)

	// Should NOT contain any ANSI escape codes
	if strings.Contains(got, "\x1b[") {
		t.Errorf("expected no ANSI codes in ColorLevel=0, got %q", got)
	}
	// Should still have content
	if !strings.Contains(got, "Open(") {
		t.Errorf("expected 'Open(' in output, got %q", got)
	}
	// Should have slow annotation (plain text)
	if !strings.Contains(got, "← 慢操作") {
		t.Errorf("expected slow annotation in plain text, got %q", got)
	}
	// Should have LLM annotation (plain text)
	if !strings.Contains(got, "← LLM 调用") {
		t.Errorf("expected LLM annotation in plain text, got %q", got)
	}
}

func TestFormatTraceLine_Verbose(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	longPath := "/very/long/path/" + strings.Repeat("a", 60)
	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "Open",
		Args:      map[string]any{"path": longPath},
		Result:    "FD(3)",
		Err:       nil,
		Duration:  0,
	}

	// verbose=false: should truncate
	gotShort := FormatTraceLine(r, event, false)
	if strings.Contains(gotShort, longPath) {
		t.Errorf("expected truncation in non-verbose mode, got %q", gotShort)
	}
	if !strings.Contains(gotShort, `..."`) {
		t.Errorf("expected truncation marker '...\"' in non-verbose mode, got %q", gotShort)
	}

	// verbose=true: should NOT truncate
	gotFull := FormatTraceLine(r, event, true)
	if !strings.Contains(gotFull, longPath) {
		t.Errorf("expected full path in verbose mode, got %q", gotFull)
	}
}
