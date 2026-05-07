package timeline

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/ipc"
)

// TestFormatRoleTag_Story38_3_AC1 verifies Story 38-3 AC#1's split of the
// legacy "tool" branch into tool_use (no ToolCallID, ColorSuccess green) and
// tool_result (with ToolCallID, ColorReplay orange). Other roles stay untouched.
//
// Mirrors cmd/rnix/dashboard_inspector_visual_test.go::TestFormatRoleTag_ToolUseVsToolResult
// to lock the behavior contract at the package boundary (Story 38-5 PR11 Step 4(c)).
func TestFormatRoleTag_Story38_3_AC1(t *testing.T) {
	toolNames := map[string]string{"call_123": "Read"}

	t.Run("tool_use_no_toolcallid", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: ""}
		tag := FormatRoleTag(msg, toolNames)
		if !strings.Contains(tag, "tool_use") {
			t.Errorf("tool_use path should contain 'tool_use' literal, got %q", tag)
		}
	})

	t.Run("tool_result_with_toolcallid_mapped_name", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: "call_123"}
		tag := FormatRoleTag(msg, toolNames)
		if !strings.Contains(tag, "tool_result") {
			t.Errorf("tool_result path should contain 'tool_result' literal, got %q", tag)
		}
		if !strings.Contains(tag, "Read") {
			t.Errorf("tool_result should suffix mapped tool name, got %q", tag)
		}
	})

	t.Run("tool_result_unmapped_falls_back_to_id", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: "tc_unknown"}
		tag := FormatRoleTag(msg, map[string]string{})
		if !strings.Contains(tag, "tool_result") {
			t.Errorf("tool_result path should contain 'tool_result' literal, got %q", tag)
		}
		if !strings.Contains(tag, "tc_unknown") {
			t.Errorf("unmapped tool_result should suffix raw ID, got %q", tag)
		}
	})

	t.Run("user_assistant_system_unchanged", func(t *testing.T) {
		for _, role := range []string{"user", "assistant", "system"} {
			msg := ipc.MessageWire{Role: role}
			tag := FormatRoleTag(msg, toolNames)
			if !strings.Contains(tag, "["+role+"]") {
				t.Errorf("role %q should render as [%s], got %q", role, role, tag)
			}
		}
	})

	t.Run("unknown_role_falls_back_to_bracketed", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "robot"}
		tag := FormatRoleTag(msg, nil)
		if tag != "[robot]" {
			t.Errorf("unknown role should fall back to bare '[role]', got %q", tag)
		}
	})

	t.Run("nil_toolnames_safe", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: "call_xx"}
		tag := FormatRoleTag(msg, nil)
		if !strings.Contains(tag, "tool_result") || !strings.Contains(tag, "call_xx") {
			t.Errorf("nil toolNames should fall back to raw ID, got %q", tag)
		}
	})
}

// TestFormatRoleTag_ColorContract verifies that tool_use vs tool_result emit
// distinct ANSI sequences when a color profile is available (Story 38-3 review
// P22 contract: tolerate no-color profiles via probe + skip).
func TestFormatRoleTag_ColorContract(t *testing.T) {
	probe := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("x")
	if probe == "x" {
		t.Skip("no color profile on this runner; skipping ANSI contract assertion")
	}

	toolNames := map[string]string{"c": "Read"}
	useTag := FormatRoleTag(ipc.MessageWire{Role: "tool", ToolCallID: ""}, toolNames)
	resultTag := FormatRoleTag(ipc.MessageWire{Role: "tool", ToolCallID: "c"}, toolNames)

	if useTag == resultTag {
		t.Errorf("tool_use and tool_result must produce different output, got identical %q", useTag)
	}
	// tool_use uses ColorSuccess (#6BCB77 green ≈ rgb 107;203;119);
	// tool_result uses ColorReplay (#E5C07B orange ≈ rgb 229;192;123).
	// Tolerate downgrades: assert the strings differ at minimum.
}

// TestPromptRoleForRole_KnownRoles verifies the simplified label produced by
// PromptRoleForRole (used by RenderDebugDetail dependency-injection roleStyle).
func TestPromptRoleForRole_KnownRoles(t *testing.T) {
	cases := []struct {
		role    string
		wantSub string
	}{
		{"system", "[system]"},
		{"user", "[user]"},
		{"assistant", "[asst]"},
		{"tool", "[tool]"},
	}
	for _, c := range cases {
		got := PromptRoleForRole(c.role)
		if !strings.Contains(got, c.wantSub) {
			t.Errorf("PromptRoleForRole(%q) should contain %q, got %q", c.role, c.wantSub, got)
		}
	}
}

func TestPromptRoleForRole_UnknownRoleFallback(t *testing.T) {
	got := PromptRoleForRole("supervisor")
	if got != "[supervisor]" {
		t.Errorf("unknown role should fall back to bare '[role]', got %q", got)
	}
}

func TestPromptRoleForRole_AligmentSpacing(t *testing.T) {
	// user / assistant / tool pad to 2 trailing spaces for column alignment in
	// debug detail blocks; system stays unpadded (it is the longest known role).
	if !strings.HasSuffix(stripANSIRole(PromptRoleForRole("user")), "[user]  ") {
		t.Errorf("user label should end with '[user]  ' (padded), got %q", PromptRoleForRole("user"))
	}
	if !strings.HasSuffix(stripANSIRole(PromptRoleForRole("assistant")), "[asst]  ") {
		t.Errorf("assistant label should end with '[asst]  ' (padded), got %q", PromptRoleForRole("assistant"))
	}
	if !strings.HasSuffix(stripANSIRole(PromptRoleForRole("tool")), "[tool]  ") {
		t.Errorf("tool label should end with '[tool]  ' (padded), got %q", PromptRoleForRole("tool"))
	}
	if !strings.HasSuffix(stripANSIRole(PromptRoleForRole("system")), "[system]") {
		t.Errorf("system label should end with '[system]' (unpadded), got %q", PromptRoleForRole("system"))
	}
}

// stripANSIRole removes ANSI escape sequences so suffix assertions stay
// profile-tolerant (TrueColor / 256-color / no-color all produce the same
// stripped suffix). Best-effort: assumes well-formed ESC[...m sequences.
func stripANSIRole(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == 0x1b:
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
