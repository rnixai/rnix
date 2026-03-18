package debug

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ATDD Tests for Story 3.5: ConfigResolve strace 事件 — FormatEvent 格式化
//
// Tests verify the FormatEvent ConfigResolve specialized format branch.

// ============================================================
// AC4: FormatEvent ConfigResolve 专用格式化
// ============================================================

func TestATDD_3_5_AC4_FormatEvent_ConfigResolve_BasicFormat(t *testing.T) {
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
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// Verify ConfigResolve specialized format: provider=X [source], model=Y [source]
	if !strings.Contains(got, "ConfigResolve(") {
		t.Fatalf("AC4: expected 'ConfigResolve(' in output, got %q", got)
	}
	if !strings.Contains(got, "provider=openrouter") {
		t.Fatalf("AC4: expected 'provider=openrouter' in output, got %q", got)
	}
	if !strings.Contains(got, "[agent]") {
		t.Fatalf("AC4: expected '[agent]' source tag in output, got %q", got)
	}
	if !strings.Contains(got, "model=hunter-alpha") {
		t.Fatalf("AC4: expected 'model=hunter-alpha' in output, got %q", got)
	}
	if !strings.Contains(got, "[cli]") {
		t.Fatalf("AC4: expected '[cli]' source tag in output, got %q", got)
	}
}

func TestATDD_3_5_AC4_FormatEvent_ConfigResolve_WithProjectDefault(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "openrouter",
			"provider_source": "agent",
			"model":           "",
			"model_source":    "driver",
			"project_default": "cursor",
		},
		Result:   nil,
		Err:      nil,
		Duration: 1 * time.Millisecond,
	}
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// Should include project_default field
	if !strings.Contains(got, "project_default=cursor") {
		t.Fatalf("AC4: expected 'project_default=cursor' in output, got %q", got)
	}
}

func TestATDD_3_5_AC4_FormatEvent_ConfigResolve_WithColor(t *testing.T) {
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
	opts := Options{ColorEnabled: true, Verbose: false}

	got := FormatEvent(event, opts)

	// Source tags should be wrapped in ansiGray when color enabled
	if !strings.Contains(got, ansiGray) {
		t.Fatalf("AC4: expected gray ANSI code for source tags in color mode, got %q", got)
	}
	// Verify [agent] and [cli] are present
	if !strings.Contains(got, "[agent]") {
		t.Fatalf("AC4: expected '[agent]' source tag with color, got %q", got)
	}
	if !strings.Contains(got, "[cli]") {
		t.Fatalf("AC4: expected '[cli]' source tag with color, got %q", got)
	}
}

func TestATDD_3_5_AC4_FormatEvent_ConfigResolve_NoColor(t *testing.T) {
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
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// No ANSI codes when color disabled
	if strings.Contains(got, ansiGray) {
		t.Fatalf("AC4: unexpected gray ANSI code when color disabled, got %q", got)
	}
	// Source tags should still be present as plain text
	if !strings.Contains(got, "[default]") {
		t.Fatalf("AC4: expected '[default]' source tag in plain text, got %q", got)
	}
	if !strings.Contains(got, "[driver]") {
		t.Fatalf("AC4: expected '[driver]' source tag in plain text, got %q", got)
	}
}

// ============================================================
// AC6: JSON 模式兼容 — FormatEventJSON with ConfigResolve
// ============================================================

func TestATDD_3_5_AC6_FormatEventJSON_ConfigResolve(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 5 * time.Millisecond,
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

	got := FormatEventJSON(event)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("AC6: FormatEventJSON output is not valid JSON: %v\ngot: %s", err, got)
	}

	// Verify syscall field
	if parsed["syscall"] != "ConfigResolve" {
		t.Fatalf("AC6: expected syscall=ConfigResolve, got %v", parsed["syscall"])
	}

	// Verify args contain all ConfigResolve fields
	args, ok := parsed["args"].(map[string]any)
	if !ok {
		t.Fatalf("AC6: expected args to be a map, got %T", parsed["args"])
	}

	expectedArgs := map[string]string{
		"provider":        "openrouter",
		"provider_source": "agent",
		"model":           "hunter-alpha",
		"model_source":    "cli",
		"project_default": "cursor",
	}
	for key, expected := range expectedArgs {
		if args[key] != expected {
			t.Errorf("AC6: expected args[%q]=%q, got %v", key, expected, args[key])
		}
	}
}
