package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ATDD Tests for Story 3.5: ConfigResolve strace 事件 — FormatTraceLine UI 格式化
//
// RED PHASE: All tests use t.Skip() — will be enabled after implementation.

// ============================================================
// AC5: FormatTraceLine ConfigResolve 支持
// ============================================================

func TestATDD_3_5_AC5_FormatTraceLine_ConfigResolve_BasicFormat(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "openrouter",
			"provider_source": "agent",
			"model":           "hunter-alpha",
			"model_source":    "cli",
		},
		Result:   nil,
		Err:      nil,
		Duration: 1 * time.Millisecond,
	}

	got := FormatTraceLine(r, event, false)

	// Verify ConfigResolve in output
	if !strings.Contains(got, "ConfigResolve") {
		t.Errorf("AC5: expected 'ConfigResolve' in output, got %q", got)
	}
	// Verify provider value
	if !strings.Contains(got, "provider=openrouter") {
		t.Errorf("AC5: expected 'provider=openrouter' in output, got %q", got)
	}
	// Verify source tags
	if !strings.Contains(got, "[agent]") {
		t.Errorf("AC5: expected '[agent]' source tag, got %q", got)
	}
	if !strings.Contains(got, "[cli]") {
		t.Errorf("AC5: expected '[cli]' source tag, got %q", got)
	}
	// Verify model value
	if !strings.Contains(got, "model=hunter-alpha") {
		t.Errorf("AC5: expected 'model=hunter-alpha' in output, got %q", got)
	}
}

func TestATDD_3_5_AC5_FormatTraceLine_ConfigResolve_SourceMutedStyle(t *testing.T) {
	// ColorLevel > 0: source tags should use MutedStyle
	InitStyles(TerminalProfile{ColorLevel: 3})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 3, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "openrouter",
			"provider_source": "agent",
			"model":           "",
			"model_source":    "driver",
		},
		Result:   nil,
		Err:      nil,
		Duration: 0,
	}

	got := FormatTraceLine(r, event, false)

	// Source tags should be present (lipgloss v1 strips ANSI in non-TTY,
	// so we verify logical content, not ANSI bytes — consistent with existing tests)
	if !strings.Contains(got, "[agent]") {
		t.Errorf("AC5: expected '[agent]' source tag with MutedStyle, got %q", got)
	}
	if !strings.Contains(got, "[driver]") {
		t.Errorf("AC5: expected '[driver]' source tag with MutedStyle, got %q", got)
	}
	// Provider value should be plain text (not styled)
	if !strings.Contains(got, "openrouter") {
		t.Errorf("AC5: expected provider value 'openrouter' in output, got %q", got)
	}
}

func TestATDD_3_5_AC5_FormatTraceLine_ConfigResolve_NoColor(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "claude",
			"provider_source": "default",
			"model":           "",
			"model_source":    "driver",
		},
		Result:   nil,
		Err:      nil,
		Duration: 0,
	}

	got := FormatTraceLine(r, event, false)

	// No ANSI codes in ColorLevel=0
	if strings.Contains(got, "\x1b[") {
		t.Errorf("AC5: expected no ANSI codes in ColorLevel=0, got %q", got)
	}
	// Source tags as plain text
	if !strings.Contains(got, "[default]") {
		t.Errorf("AC5: expected '[default]' source tag as plain text, got %q", got)
	}
	if !strings.Contains(got, "[driver]") {
		t.Errorf("AC5: expected '[driver]' source tag as plain text, got %q", got)
	}
}

func TestATDD_3_5_AC5_FormatTraceLine_ConfigResolve_WithProjectDefault(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	r := &Renderer{
		Profile:    TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
		OutputMode: ModeDefault,
	}

	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "openrouter",
			"provider_source": "agent",
			"model":           "hunter-alpha",
			"model_source":    "cli",
			"project_default": "cursor",
		},
		Result:   nil,
		Err:      nil,
		Duration: 1 * time.Millisecond,
	}

	got := FormatTraceLine(r, event, false)

	// project_default should appear in output
	if !strings.Contains(got, "project_default=cursor") {
		t.Errorf("AC5: expected 'project_default=cursor' in output, got %q", got)
	}
}
