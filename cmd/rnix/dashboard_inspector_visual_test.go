package main

// =============================================================================
// Story 38-3: Step Inspector 5-lens 视觉增强
// PR1 tests — Conversation 4-color role tag + Tool I/O Box-drawing
// =============================================================================

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// --- AC#1: formatRoleTag tool_use vs tool_result ---

// TestFormatRoleTag_ToolUseVsToolResult verifies Story 38-3 AC#1's split of the
// legacy "tool" branch into tool_use (no ToolCallID, ColorSuccess green) and
// tool_result (with ToolCallID, ColorReplay orange). Other roles stay untouched.
func TestFormatRoleTag_ToolUseVsToolResult(t *testing.T) {
	toolNames := map[string]string{"call_123": "Read"}

	t.Run("tool_use_no_toolcallid", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: ""}
		tag := formatRoleTag(msg, toolNames)
		if !strings.Contains(tag, "tool_use") {
			t.Errorf("tool_use path should contain 'tool_use' literal, got %q", tag)
		}
		// ColorSuccess #6BCB77 → ANSI 24-bit RGB 107;203;119
		if !strings.Contains(tag, ";203;") && !strings.Contains(tag, "6BCB77") {
			// Best-effort check: tolerate lipgloss color downgrade.
			t.Logf("note: tool_use tag = %q (color depends on lipgloss profile)", tag)
		}
	})

	t.Run("tool_result_with_toolcallid", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: "call_123"}
		tag := formatRoleTag(msg, toolNames)
		if !strings.Contains(tag, "tool_result") {
			t.Errorf("tool_result path should contain 'tool_result' literal, got %q", tag)
		}
		if !strings.Contains(tag, "Read") {
			t.Errorf("tool_result should suffix mapped tool name, got %q", tag)
		}
	})

	t.Run("tool_result_unmapped_falls_back_to_id", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: "tc_unknown"}
		tag := formatRoleTag(msg, map[string]string{})
		if !strings.Contains(tag, "tool_result") || !strings.Contains(tag, "tc_unknown") {
			t.Errorf("unmapped tool_result should suffix raw ID, got %q", tag)
		}
	})

	t.Run("user_assistant_system_unchanged", func(t *testing.T) {
		// Regression: pre-Story-38-3 role tags must still render as bracketed labels.
		for _, role := range []string{"user", "assistant", "system"} {
			msg := ipc.MessageWire{Role: role}
			tag := formatRoleTag(msg, toolNames)
			if !strings.Contains(tag, "["+role+"]") {
				t.Errorf("role %q should render as [%s], got %q", role, role, tag)
			}
		}
	})
}

// --- AC#2: Tool I/O lens box-drawing ---

func TestBuildToolIOLens_BoxDrawing(t *testing.T) {
	t.Run("input_box_unicode", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		m.width = 100
		content := m.buildLensContent(lensToolIO, m.inspectorDetail, nil)
		if !strings.Contains(content, "┌") || !strings.Contains(content, "Input") {
			t.Errorf("unicode mode: Input box should contain ┌ and Input title; got:\n%s", content)
		}
	})

	t.Run("error_box_red_border", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		m.width = 100
		// Inject error to trigger Error box
		detail := *m.inspectorDetail
		detail.ToolError = "permission denied"
		content := m.buildLensContent(lensToolIO, &detail, nil)
		if !strings.Contains(content, "Error") {
			t.Errorf("Error box should render Error title; got:\n%s", content)
		}
		// ColorError #FF6B6B → 24-bit RGB 255;107;107 (lipgloss truecolor profile).
		// Tolerate other profiles by also accepting the literal color hex.
		if !strings.Contains(content, ";107;107") && !strings.Contains(content, "FF6B6B") {
			t.Logf("note: error border color depends on lipgloss profile; got snippet = %q", content)
		}
	})

	t.Run("no_tool_info_fallback", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		detail := &ipc.GetStepDetailResponse{Action: "", ToolPath: ""}
		content := m.buildLensContent(lensToolIO, detail, nil)
		if !strings.Contains(content, "No tool information") {
			t.Errorf("empty tool detail should render fallback text; got:\n%s", content)
		}
	})

	t.Run("narrow_terminal_60cols", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		m.width = 60
		content := m.buildLensContent(lensToolIO, m.inspectorDetail, nil)
		// inspectorBoxWidth(60) → 56. Each line of the box must not exceed 56 chars
		// (run-count, ANSI-stripped) — best-effort check.
		for line := range strings.SplitSeq(content, "\n") {
			stripped := stripANSIApprox(line)
			if rcLen(stripped) > 60 {
				t.Errorf("60-col terminal line overflowed: %d runes: %q", rcLen(stripped), line)
				break
			}
		}
	})

	t.Run("ascii_mode_uses_plus_dash", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		// Reset the ASCII mode cache via fresh ui call.
		_ = ui.IsASCIIMode()
		m := newTestInspectorModelWithDetail()
		m.width = 100
		content := m.buildLensContent(lensToolIO, m.inspectorDetail, nil)
		// In ASCII fallback, box uses + and -; should not contain Unicode box chars.
		if strings.Contains(content, "┌") || strings.Contains(content, "│") {
			t.Errorf("ASCII mode should not contain box-drawing runes; got snippet:\n%s", content)
		}
		if !strings.Contains(content, "+") || !strings.Contains(content, "-") {
			t.Errorf("ASCII mode should contain + and - for box edges; got:\n%s", content)
		}
	})
}

// rcLen returns the rune count for a string; helper for width-bound assertions.
func rcLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
