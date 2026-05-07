// Package detail — render_test.go (Story 38-5 PR11 Step 4(c))
//
// 验证 Render() 行为契约（与 cmd/rnix.renderDetailPane 1:1 等价）：
//   - SelectedPID == 0 → "Select a process to view detail"
//   - state.Detail nil OR PID/UUID mismatch → "Loading..." (Story 28-4 AC-4 stale-data guard)
//   - 完整渲染 4 个 sections (Basic / Skills / FD / Context)
//   - Context budget bar 渲染 + 0% 边界 + 100% clamp
//   - TruncateUUID / TruncateStr helpers 行为契约
package detail

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// --- Render() ---

func TestRender_NoSelection(t *testing.T) {
	state := DetailState{}
	ctx := RenderContext{SelectedPID: 0}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "Select a process") {
		t.Errorf("expected no-selection message, got %q", got)
	}
}

func TestRender_LoadingWhenDetailNil(t *testing.T) {
	state := DetailState{Detail: nil}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "Loading...") {
		t.Errorf("expected Loading... when Detail is nil, got %q", got)
	}
}

func TestRender_LoadingWhenPIDMismatch(t *testing.T) {
	// Story 28-4 AC-4 stale-data guard
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{PID: 99, UUID: "abc"},
	}
	ctx := RenderContext{SelectedPID: types.PID(42), SelectedUUID: ""}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "Loading...") {
		t.Errorf("expected Loading... when PID mismatches, got %q", got)
	}
}

func TestRender_LoadingWhenUUIDMismatch(t *testing.T) {
	// Story 28-4 AC-4 stale-data guard (UUID validates beyond PID)
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{PID: 42, UUID: "old-uuid"},
	}
	ctx := RenderContext{SelectedPID: types.PID(42), SelectedUUID: "new-uuid"}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "Loading...") {
		t.Errorf("expected Loading... when UUID mismatches (cross-PID staleness), got %q", got)
	}
}

func TestRender_BasicInfo(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:      42,
			UUID:     "01234567-89ab-cdef",
			State:    "running",
			Intent:   "test-intent",
			Provider: "claude",
			Model:    "sonnet",
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	cases := []string{"PID: 42", "running", "test-intent", "claude", "sonnet"}
	for _, want := range cases {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got %q", want, got)
		}
	}
}

func TestRender_SkillsSection(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:   42,
			State: "running",
			Skills: []ipc.SkillInfoWire{
				{Name: "skill1", AllowedTools: []string{"fs", "shell"}},
				{Name: "skill2", AllowedTools: nil},
			},
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "skill1") || !strings.Contains(got, "fs, shell") {
		t.Errorf("expected skill1 with tools 'fs, shell', got %q", got)
	}
	if !strings.Contains(got, "skill2") || !strings.Contains(got, "—") {
		t.Errorf("expected skill2 with em-dash for empty tools, got %q", got)
	}
}

func TestRender_EmptySkills(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{PID: 42, State: "running"},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "(none)") {
		t.Errorf("expected '(none)' for empty Skills, got %q", got)
	}
}

func TestRender_FDTable(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:   42,
			State: "running",
			FDTable: []ipc.FDEntryWire{
				{FD: 0, DevicePath: "/dev/llm/claude"},
				{FD: 1, DevicePath: "/dev/fs"},
			},
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "/dev/llm/claude") {
		t.Errorf("expected FD entry /dev/llm/claude, got %q", got)
	}
}

func TestRender_FDTableEmptyDead(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{PID: 42, State: "dead"},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "(closed)") {
		t.Errorf("expected '(closed)' for dead process empty FD, got %q", got)
	}
}

func TestRender_FDTableEmptyAlive(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{PID: 42, State: "running"},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "(empty)") {
		t.Errorf("expected '(empty)' for running process empty FD, got %q", got)
	}
}

func TestRender_ContextWithBudget(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:   42,
			State: "running",
			ContextStats: ipc.ContextStatsWire{
				MessageCount:  10,
				TokensUsed:    5000,
				ContextBudget: 10000,
				UsagePct:      50.0,
			},
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "10 msgs") {
		t.Errorf("expected '10 msgs', got %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Errorf("expected '50%%' usage pct, got %q", got)
	}
	if !strings.Contains(got, "█") {
		t.Errorf("expected filled bar character, got %q", got)
	}
}

func TestRender_ContextOverBudgetClampedTo100(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:   42,
			State: "running",
			ContextStats: ipc.ContextStatsWire{
				MessageCount:  100,
				TokensUsed:    20000,
				ContextBudget: 10000,
				UsagePct:      200.0, // over budget
			},
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if strings.Contains(got, "200%") {
		t.Errorf("UsagePct should be clamped to 100%%, got %q", got)
	}
	if !strings.Contains(got, "100%") {
		t.Errorf("expected '100%%' clamped value, got %q", got)
	}
}

func TestRender_AllowedDevices(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:            42,
			State:          "running",
			AllowedDevices: []string{"/dev/fs", "/dev/llm"},
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)
	if !strings.Contains(got, "/dev/fs, /dev/llm") {
		t.Errorf("expected joined AllowedDevices, got %q", got)
	}
}

// --- TruncateUUID / TruncateStr helpers ---

func TestTruncateUUID_LongString(t *testing.T) {
	got := TruncateUUID("0123456789abcdef")
	if got != "01234567" {
		t.Errorf("expected first 8 chars, got %q", got)
	}
}

func TestTruncateUUID_ShortString(t *testing.T) {
	got := TruncateUUID("short")
	if got != "short" {
		t.Errorf("short string should be returned unchanged, got %q", got)
	}
}

func TestTruncateUUID_Exactly8(t *testing.T) {
	got := TruncateUUID("12345678")
	if got != "12345678" {
		t.Errorf("8-char string should be returned unchanged, got %q", got)
	}
}

func TestTruncateStr_NoTruncation(t *testing.T) {
	got := TruncateStr("hello", 10)
	if got != "hello" {
		t.Errorf("string fitting maxLen should be returned unchanged, got %q", got)
	}
}

func TestTruncateStr_Truncates(t *testing.T) {
	got := TruncateStr("this is a long string", 10)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated string should end with '...', got %q", got)
	}
	// "this is a " (7 chars) + "..." (3 chars) = 10 chars total
	if len([]rune(got)) != 10 {
		t.Errorf("expected truncated length 10, got %d (%q)", len([]rune(got)), got)
	}
}

func TestTruncateStr_MaxLenLessThan4(t *testing.T) {
	// maxLen < 4 disables truncation (the "..." would consume all budget)
	got := TruncateStr("hello world", 3)
	if got != "hello world" {
		t.Errorf("maxLen<4 should disable truncation, got %q", got)
	}
}

func TestTruncateStr_UTF8Safety(t *testing.T) {
	// Multi-byte runes (Chinese) should be truncated by rune count, not byte count
	got := TruncateStr("中文测试字符串很长", 6)
	// "中文测..." (3 runes + 3 dots = 6 runes total)
	if len([]rune(got)) != 6 {
		t.Errorf("expected 6 runes, got %d (%q)", len([]rune(got)), got)
	}
}
