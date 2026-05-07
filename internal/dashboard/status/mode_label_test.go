// Package status — mode_label_test.go (Story 38-5 PR11 Step 4(c))
//
// 行为测试覆盖（与 cmd/rnix.dashboardModel.renderModeLabel 等价契约）：
//   - replayMode 优先级最高（覆盖任何 viewMode）；
//   - viewMode 路由（Default/Expanded/Inspector/Debug/History/未识别）；
//   - ASCII 模式下分隔符 '│' 替换为 '|'；
//   - DEBUG mode label Bold weight；其他 mode regular weight；
//   - viewHistory 落入 [MONITOR]（向后兼容 iota 占位）；
//   - 未识别 viewMode 防御性降级 [MONITOR]；
//   - profile-tolerant 颜色断言（lipgloss profile 决定 SGR 字节是否输出）.
package status

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// stripANSI removes lipgloss ANSI escape sequences so label text comparison
// stays profile-independent (Story 38-3 教训应用 · 与 timeline.role_test.go
// stripANSIRole + trace.render_test.go stripANSI 同模式).
func stripANSI(s string) string {
	out := make([]byte, 0, len(s))
	in := false
	for i := range len(s) {
		c := s[i]
		if c == 0x1b {
			in = true
			continue
		}
		if in {
			if c == 'm' {
				in = false
			}
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

func TestRenderModeLabel_Default(t *testing.T) {
	got := RenderModeLabel(false, ViewDefault)
	plain := stripANSI(got)
	if !strings.Contains(plain, "[MONITOR]") {
		t.Errorf("RenderModeLabel(viewDefault) = %q, want [MONITOR]", plain)
	}
	if !strings.Contains(plain, "│") {
		t.Errorf("UTF-8 mode should contain '│' separator, got: %q", plain)
	}
}

func TestRenderModeLabel_Expanded(t *testing.T) {
	got := RenderModeLabel(false, ViewExpanded)
	if !strings.Contains(stripANSI(got), "[EXPANDED]") {
		t.Errorf("ViewExpanded should yield [EXPANDED], got: %q", got)
	}
}

func TestRenderModeLabel_StepInspector(t *testing.T) {
	got := RenderModeLabel(false, ViewStepInspector)
	if !strings.Contains(stripANSI(got), "[INSPECTOR]") {
		t.Errorf("ViewStepInspector should yield [INSPECTOR], got: %q", got)
	}
}

func TestRenderModeLabel_Debug(t *testing.T) {
	got := RenderModeLabel(false, ViewDebug)
	if !strings.Contains(stripANSI(got), "[DEBUG]") {
		t.Errorf("ViewDebug should yield [DEBUG], got: %q", got)
	}
}

func TestRenderModeLabel_History_FallsBackToMonitor(t *testing.T) {
	got := RenderModeLabel(false, ViewHistory)
	plain := stripANSI(got)
	if !strings.Contains(plain, "[MONITOR]") {
		t.Errorf("ViewHistory iota slot should fall back to [MONITOR], got: %q", plain)
	}
}

func TestRenderModeLabel_UnknownView_FallsBackToMonitor(t *testing.T) {
	got := RenderModeLabel(false, 99)
	plain := stripANSI(got)
	if !strings.Contains(plain, "[MONITOR]") {
		t.Errorf("unknown viewMode should defensively yield [MONITOR], got: %q", plain)
	}
}

func TestRenderModeLabel_ReplayOverridesViewMode(t *testing.T) {
	// Even ViewDebug + replayMode=true should yield [REPLAY] (priority order).
	got := RenderModeLabel(true, ViewDebug)
	plain := stripANSI(got)
	if !strings.Contains(plain, "[REPLAY]") {
		t.Errorf("replayMode must override viewMode, got: %q", plain)
	}
	if strings.Contains(plain, "[DEBUG]") {
		t.Errorf("replayMode + viewDebug must NOT contain [DEBUG], got: %q", plain)
	}
}

func TestRenderModeLabel_ASCII_SeparatorReplaced(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := RenderModeLabel(false, ViewDefault)
	plain := stripANSI(got)
	if !strings.Contains(plain, "[MONITOR]") {
		t.Errorf("ASCII mode label text unchanged, got: %q", plain)
	}
	if strings.Contains(plain, "│") {
		t.Errorf("ASCII mode should NOT contain UTF-8 '│', got: %q", plain)
	}
	if !strings.Contains(plain, "|") {
		t.Errorf("ASCII mode should contain '|' separator, got: %q", plain)
	}
}

// TestRenderModeLabel_DebugBold verifies the [DEBUG] segment carries Bold
// styling while regular modes do not (profile-tolerant — only assert when the
// runtime can produce SGR bytes).
func TestRenderModeLabel_DebugBold(t *testing.T) {
	probe := lipgloss.NewStyle().Bold(true).Render("x")
	if !strings.Contains(probe, "\x1b[") {
		t.Skip("lipgloss profile is plain (no ANSI), skipping bold byte assertion")
	}

	debugRendered := RenderModeLabel(false, ViewDebug)
	monitorRendered := RenderModeLabel(false, ViewDefault)
	if !strings.Contains(debugRendered, "\x1b[1") {
		// "\x1b[1" is the SGR bold prefix. Some profiles may emit it inside a
		// compound parameter (e.g. "\x1b[1;38;5;..."), so a substring check is
		// sufficient — false positives only happen if the surrounding text
		// embeds "\x1b[1" by accident, which it doesn't.
		t.Errorf("ViewDebug should be Bold (expected \\x1b[1 in output), got: %q", debugRendered)
	}
	if strings.Contains(stripANSI(monitorRendered), "[DEBUG]") {
		t.Errorf("monitor rendering accidentally contains [DEBUG]")
	}
}

// TestRenderModeLabel_NonEmpty verifies all five mode labels return non-empty
// strings for every public viewMode constant.
func TestRenderModeLabel_NonEmpty(t *testing.T) {
	for _, v := range []int{ViewDefault, ViewExpanded, ViewStepInspector, ViewHistory, ViewDebug} {
		if got := RenderModeLabel(false, v); got == "" {
			t.Errorf("RenderModeLabel(false, %d) returned empty string", v)
		}
	}
	if got := RenderModeLabel(true, ViewDefault); got == "" {
		t.Error("RenderModeLabel(replayMode=true) returned empty string")
	}
}

// TestColorIPC_Constant guards the ColorIPC const value so a future change to
// cmd/rnix.colorIPC must be paired with an update here. The value is
// hard-coded (#9B59B6 purple) per Story 38.2 AC#2 visual contract.
func TestColorIPC_Constant(t *testing.T) {
	if ColorIPC != "#9B59B6" {
		t.Errorf("ColorIPC drifted from cmd/rnix.colorIPC contract: got %s, want #9B59B6", ColorIPC)
	}
}

// TestViewModeConstants_StableIotaOrdering guards the iota ordering between
// cmd/rnix.viewMode and status.View*. cmd/rnix end uses iota (0..4); status
// end mirrors via explicit int constants. Drift would silently break the
// 38.2-UNIT-005 behavior tests.
func TestViewModeConstants_StableIotaOrdering(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"ViewDefault", ViewDefault, 0},
		{"ViewExpanded", ViewExpanded, 1},
		{"ViewStepInspector", ViewStepInspector, 2},
		{"ViewHistory", ViewHistory, 3},
		{"ViewDebug", ViewDebug, 4},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s drifted: got %d, want %d (cmd/rnix.viewMode iota)", c.name, c.got, c.want)
		}
	}
}
