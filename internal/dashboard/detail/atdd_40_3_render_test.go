package detail

import (
	"os"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// ============================================================================
// ATDD Tests for Story 40.3: Dashboard Detail pane — driver metadata 渲染
//
// RED PHASE: These tests reference GetProcDetailResponse.DriverMeta (does not
// exist yet). They will fail to compile until DriverMeta field is added to
// GetProcDetailResponse and the render logic is implemented.
//
// AC1: Dashboard 展示 Binary/Permission/Capabilities
// AC1: ASCII 模式下 ✓ → Y, ✗ → N
// AC1: DriverMeta 为空时不渲染 driver 行
// ============================================================================

// ---------------------------------------------------------------------------
// AC #1: DriverMeta 非空 — 渲染 Binary/Permission/Capabilities 行
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_Render_DriverMeta_ShowsBinaryAndCaps(t *testing.T) {
	t.Parallel()

	// GIVEN: GetProcDetailResponse 含 DriverMeta (Claude CLI 进程)
	// WHEN:  Render() 渲染 Detail pane
	// THEN:  输出包含 "Binary:" 行和 "Capabilities:" 行（或 "Caps:"）

	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:      types.PID(42),
			UUID:     "test-uuid-render",
			State:    "Running",
			Intent:   "test driver meta render",
			Provider: "claude-cli",
			Model:    "claude-sonnet-4-6",
			DriverMeta: map[string]string{
				"resolved_bin":         "/usr/local/bin/claude",
				"permission_mode":      "bypassPermissions",
				"cap_partial_messages": "true",
				"cap_add_dir":          "true",
				"cap_permission_mode":  "true",
			},
		},
	}
	ctx := RenderContext{
		SelectedPID:  types.PID(42),
		SelectedUUID: "test-uuid-render",
	}

	got := Render(state, ctx, 100)

	if !strings.Contains(got, "Binary:") {
		t.Error("AC1 FAIL: rendered output missing 'Binary:' label when DriverMeta is set")
	}
	if !strings.Contains(got, "/usr/local/bin/claude") {
		t.Error("AC1 FAIL: rendered output missing resolved_bin path")
	}
	if !strings.Contains(got, "Permission:") {
		t.Error("AC1 FAIL: rendered output missing 'Permission:' label")
	}
	if !strings.Contains(got, "bypassPermissions") {
		t.Error("AC1 FAIL: rendered output missing permission_mode value")
	}
	// Check capabilities line (either "Caps:" or "Capabilities:")
	if !strings.Contains(got, "partialMessages=") {
		t.Error("AC1 FAIL: rendered output missing partialMessages capability flag")
	}
	if !strings.Contains(got, "addDir=") {
		t.Error("AC1 FAIL: rendered output missing addDir capability flag")
	}
	if !strings.Contains(got, "permissionMode=") {
		t.Error("AC1 FAIL: rendered output missing permissionMode capability flag")
	}
}

// ---------------------------------------------------------------------------
// AC #1: ASCII 模式 — ✓ → Y, ✗ → N
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_Render_DriverMeta_ASCIIMode(t *testing.T) {
	// no t.Parallel: modifies RNIX_ASCII env

	// GIVEN: RNIX_ASCII=1 (ASCII 模式)
	// AND:   DriverMeta 中 cap_partial_messages=true, cap_add_dir=false
	// WHEN:  Render() 渲染
	// THEN:  使用 Y/N 代替 ✓/✗

	t.Setenv("RNIX_ASCII", "1")
	defer os.Unsetenv("RNIX_ASCII")

	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:      types.PID(43),
			UUID:     "test-uuid-ascii",
			State:    "Running",
			Intent:   "test ascii mode",
			Provider: "claude-cli",
			Model:    "claude-sonnet-4-6",
			DriverMeta: map[string]string{
				"resolved_bin":         "/usr/bin/claude",
				"permission_mode":      "default",
				"cap_partial_messages": "true",
				"cap_add_dir":          "false",
				"cap_permission_mode":  "true",
			},
		},
	}
	ctx := RenderContext{
		SelectedPID:  types.PID(43),
		SelectedUUID: "test-uuid-ascii",
	}

	got := Render(state, ctx, 100)

	// In ASCII mode, expect Y/N
	if strings.Contains(got, "✓") || strings.Contains(got, "✗") {
		t.Error("AC1 FAIL: ASCII mode should use Y/N, but found Unicode ✓/✗")
	}
	if !strings.Contains(got, "=Y") && !strings.Contains(got, "= Y") {
		t.Error("AC1 FAIL: ASCII mode should show 'Y' for true capabilities")
	}
	if !strings.Contains(got, "=N") && !strings.Contains(got, "= N") {
		t.Error("AC1 FAIL: ASCII mode should show 'N' for false capabilities")
	}
}

// ---------------------------------------------------------------------------
// AC #1 (negative): DriverMeta 为空 — 不渲染 driver 行
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_Render_NoDriverMeta_SkipsDriverSection(t *testing.T) {
	t.Parallel()

	// GIVEN: GetProcDetailResponse 的 DriverMeta 为空（非 Claude CLI driver）
	// WHEN:  Render() 渲染 Detail pane
	// THEN:  输出不包含 "Binary:" 和 "Capabilities:" / "Caps:"

	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:      types.PID(44),
			UUID:     "test-uuid-nodriver",
			State:    "Running",
			Intent:   "test no driver meta",
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
			// DriverMeta intentionally NOT set (nil/empty)
		},
	}
	ctx := RenderContext{
		SelectedPID:  types.PID(44),
		SelectedUUID: "test-uuid-nodriver",
	}

	got := Render(state, ctx, 100)

	if strings.Contains(got, "Binary:") {
		t.Error("AC1 FAIL: non-Claude driver should NOT render 'Binary:' line")
	}
	if strings.Contains(got, "Caps:") || strings.Contains(got, "Capabilities:") {
		t.Error("AC1 FAIL: non-Claude driver should NOT render capabilities line")
	}
}
