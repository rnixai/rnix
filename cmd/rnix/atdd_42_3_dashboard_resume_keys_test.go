package main

// =============================================================================
// ATDD Story 42.3: Dashboard r/f 快捷键 + Detail Lineage 渲染
//
// Test scenarios covered:
//   - CLI-001 / CLI-002: 选中 Dead/Zombie 进程按 `r` 触发 resumeProcessCmd
//   - CLI-003: 选中 Suspended 进程按 `r` 仍走原路径（AC#9 回归）
//   - CLI-004: 选中 Running 按 `r` → fallback false
//   - CLI-005: 大写 R / shift+R 在 Suspended 上保留行为
//   - CLI-006: 选中 Dead 按 `f` 触发 forkProcessCmd
//   - CLI-007: Timeline pane 激活时 `f` → timelineFilterHandler 先消费
//   - CLI-008: 选中 Running 按 `f` → fallback false
//   - CLI-009: forkProcessCmd 调用 ResumeWithOpts(uuid, true)（结构断言）
//   - CLI-010 / CLI-011: Detail render 包含 Lineage section 当 OriginUUID/Descendants 非空
//   - CLI-012: OriginUUID 空 且 Descendants 空 → 不渲染 Lineage section
//   - CLI-013: RNIX_ASCII=1 分隔线降级
//
// RED PHASE:
//   - resumeProcessHandler / forkProcessHandler / forkProcessCmd 已 stub 但未在
//     Layer 1 注册（dev-story 阶段替换 resumeSuspendedHandler 注册）
//   - detail.Render 尚未输出 Lineage section
// =============================================================================

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func new423ModelWithProc(state types.ProcessState, uuid string) dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	proc := vfs.ProcInfo{
		PID:       types.PID(7),
		UUID:      uuid,
		State:     state,
		Intent:    "42-3 test process",
		CreatedAt: time.Now(),
	}
	m.processes = []vfs.ProcInfo{proc}
	m.tree.Rows = []flatRow{{Proc: proc}}
	m.tree.Cursor = 0
	m = selectProcess(m, m.tree.Rows[0])
	return m
}

// fakeKeyCtx returns a ui.KeyContext from a dashboardModel so handlers can be
// invoked directly without going through the full dispatcher.
func fakeKeyCtx(m dashboardModel) ui.KeyContext { return m }

// ---------------------------------------------------------------------------
// CLI-001: Dead + `r` → resumeProcessCmd (AC#4)
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_001_Dead_R_TriggersResume(t *testing.T) {
	m := new423ModelWithProc(types.StateDead, "dead-uuid-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	consumed, _, cmd := resumeProcessHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if !consumed {
		t.Error("resumeProcessHandler should consume key for Dead state")
	}
	if cmd == nil {
		t.Error("resumeProcessHandler should return non-nil cmd")
	}
	// Skip cmd execution — it dials a real socket which is unavailable in unit tests.
	_ = cmd
}

// ---------------------------------------------------------------------------
// CLI-002: Zombie + `r` → resumeProcessCmd (AC#4)
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_002_Zombie_R_TriggersResume(t *testing.T) {
	m := new423ModelWithProc(types.StateZombie, "zombie-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	consumed, _, cmd := resumeProcessHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if !consumed {
		t.Error("resumeProcessHandler should consume key for Zombie state")
	}
	if cmd == nil {
		t.Error("resumeProcessHandler should return non-nil cmd")
	}
}

// ---------------------------------------------------------------------------
// CLI-003: Suspended + `r` → fallback false (AC#4 — 小写 r 仅 Dead/Zombie；
// Suspended 由大写 R/shift+R 通过 resumeSuspendedHandler 处理 · AC#9)
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_003_Suspended_R_FallsThroughToLegacyR(t *testing.T) {
	m := new423ModelWithProc(types.StateSuspended, "susp-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	consumed, _, _ := resumeProcessHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if consumed {
		t.Error("resumeProcessHandler should NOT consume `r` for Suspended (AC#4 — that's R/shift+R's job)")
	}
}

// ---------------------------------------------------------------------------
// CLI-004: Running + `r` → fallback false (handler skipped)
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_004_Running_R_FallthroughFalse(t *testing.T) {
	m := new423ModelWithProc(types.StateRunning, "run-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	consumed, _, _ := resumeProcessHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if consumed {
		t.Error("resumeProcessHandler should NOT consume key for Running state (fallback false)")
	}
}

// ---------------------------------------------------------------------------
// CLI-005: Suspended R / shift+R → resumeSuspendedHandler 保留 (AC#9)
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_005_Suspended_ShiftR_LegacyHandlerPreserved(t *testing.T) {
	m := new423ModelWithProc(types.StateSuspended, "shiftR-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	consumed, _, cmd := resumeSuspendedHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if !consumed {
		t.Error("resumeSuspendedHandler should consume key for Suspended state (legacy parity)")
	}
	if cmd == nil {
		t.Error("resumeSuspendedHandler should return resumeProcessCmd for Suspended state")
	}
}

// ---------------------------------------------------------------------------
// CLI-006: Dead + `f` → forkProcessCmd (AC#5)
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_006_Dead_F_TriggersFork(t *testing.T) {
	m := new423ModelWithProc(types.StateDead, "fork-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	consumed, _, cmd := forkProcessHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if !consumed {
		t.Error("forkProcessHandler should consume key for Dead state")
	}
	if cmd == nil {
		t.Error("forkProcessHandler should return forkProcessCmd")
	}
	_ = cmd
}

// ---------------------------------------------------------------------------
// CLI-007: Timeline pane active + `f` → timelineFilterHandler 先消费 (AC#5 回归)
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_007_TimelineActive_F_DefersToTimelineHandler(t *testing.T) {
	m := new423ModelWithProc(types.StateDead, "tl-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	m.activePane = paneTimeline

	// timelineFilterHandler 在 timeline pane active 时应返回 (true, ...)
	consumed, _, _ := timelineFilterHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if !consumed {
		t.Error("timelineFilterHandler should consume `f` when Timeline pane is active (priority over fork)")
	}
}

// ---------------------------------------------------------------------------
// CLI-008: Running + `f` → fallback false
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_008_Running_F_FallthroughFalse(t *testing.T) {
	m := new423ModelWithProc(types.StateRunning, "runf-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	consumed, _, _ := forkProcessHandler(tea.KeyPressMsg{}, fakeKeyCtx(m))
	if consumed {
		t.Error("forkProcessHandler should NOT consume `f` for Running state")
	}
}

// ---------------------------------------------------------------------------
// CLI-009: forkProcessCmd structure assertion
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_009_ForkProcessCmd_ReturnsForkResultMsg(t *testing.T) {
	cmd := forkProcessCmd("any-uuid-aaaaaaaa-bbbb-cccc-dddd-000000000001")
	if cmd == nil {
		t.Fatal("forkProcessCmd returned nil")
	}
	// Executing the cmd dials the real socket — in unit tests this returns
	// forkResultMsg with err set; either way the type assertion is what matters.
	msg := cmd()
	if _, ok := msg.(forkResultMsg); !ok {
		t.Errorf("msg type = %T, want forkResultMsg", msg)
	}
}

// ---------------------------------------------------------------------------
// CLI-010: Detail render shows "Origin: <hash> (from step N)" when OriginUUID set
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_010_DetailRender_OriginSection(t *testing.T) {
	state := detail.DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:             types.PID(42),
			UUID:            "render-aaaaaaaa-bbbb-cccc-dddd-000000000001",
			State:           "dead",
			Intent:          "rendered process",
			Provider:        "claude",
			Model:           "claude-4",
			OriginUUID:      "origin12-aaaaaaaa-bbbb-cccc-dddd-000000000002",
			ResumedFromStep: 31,
		},
	}
	ctx := detail.RenderContext{SelectedPID: 42, SelectedUUID: state.Detail.UUID, IsActive: true}
	out := detail.Render(state, ctx, 80)

	if !strings.Contains(out, "Lineage") {
		t.Errorf("render missing 'Lineage' section\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Origin:") {
		t.Errorf("render missing 'Origin:' line\noutput:\n%s", out)
	}
	if !strings.Contains(out, "from step 31") {
		t.Errorf("render missing 'from step 31'\noutput:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// CLI-011: Detail render shows "Forked: N descendants" when descendants exist
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_011_DetailRender_DescendantsSection(t *testing.T) {
	state := detail.DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:    types.PID(42),
			UUID:   "fork-parent-aaaaaaaa-bbbb-cccc-dddd-000000000001",
			State:  "dead",
			Intent: "parent of two forks",
		},
		LineageCache: map[string]*ipc.GetResumeLineageResponse{
			"fork-parent-aaaaaaaa-bbbb-cccc-dddd-000000000001": {
				Descendants: []ipc.ResumeLineageNode{
					{UUID: "fork-d1-aaaaaaaa-bbbb-cccc-dddd-000000000002"},
					{UUID: "fork-d2-aaaaaaaa-bbbb-cccc-dddd-000000000003"},
				},
			},
		},
	}
	ctx := detail.RenderContext{
		SelectedPID:  42,
		SelectedUUID: state.Detail.UUID,
		IsActive:     true,
	}
	out := detail.Render(state, ctx, 80)
	if !strings.Contains(out, "Forked: 2 descendants") {
		t.Errorf("render missing 'Forked: 2 descendants'\noutput:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// CLI-012: Detail render omits Lineage section when no origin and no descendants
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_012_DetailRender_NoLineageNoise(t *testing.T) {
	state := detail.DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:    types.PID(42),
			UUID:   "plain-aaaaaaaa-bbbb-cccc-dddd-000000000001",
			State:  "running",
			Intent: "plain spawn process",
			// OriginUUID 留空、LineageCache 留空
		},
	}
	ctx := detail.RenderContext{
		SelectedPID:  42,
		SelectedUUID: state.Detail.UUID,
		IsActive:     true,
	}
	out := detail.Render(state, ctx, 80)
	// Lineage section 不应渲染（避免普通 spawn 进程的噪声）
	if strings.Contains(out, "Lineage") {
		t.Errorf("render should NOT emit 'Lineage' section when no origin + no descendants\noutput:\n%s", out)
	}
	if strings.Contains(out, "Origin:") {
		t.Errorf("render should NOT emit 'Origin:' line for plain process\noutput:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// CLI-013: RNIX_ASCII=1 → 分隔线降级 ----
// ---------------------------------------------------------------------------

func TestATDD_42_3_CLI_013_DetailRender_ASCII_Fallback(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")

	state := detail.DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:             types.PID(42),
			UUID:            "ascii-aaaaaaaa-bbbb-cccc-dddd-000000000001",
			State:           "dead",
			Intent:          "ASCII fallback test",
			OriginUUID:      "origin42-aaaaaaaa-bbbb-cccc-dddd-000000000002",
			ResumedFromStep: 10,
		},
	}
	ctx := detail.RenderContext{
		SelectedPID:  42,
		SelectedUUID: state.Detail.UUID,
		IsActive:     true,
	}
	out := detail.Render(state, ctx, 80)

	// ASCII 模式下 Lineage 分隔线应包含 "----"
	if !strings.Contains(out, "---- Lineage ----") {
		t.Errorf("ASCII mode should use '---- Lineage ----' separator\noutput:\n%s", out)
	}
	if strings.Contains(out, "──── Lineage ────") {
		t.Errorf("ASCII mode should NOT use unicode '──── Lineage ────'\noutput:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Test helper — execute a tea.Cmd safely (cmd may dial real socket; ignore err).
// Kept for tests that want to assert on msg type (currently unused since most
// CLI tests verify cmd != nil without executing it).
// ---------------------------------------------------------------------------

//nolint:unused // retained for future cmd execution assertions
func executeCmdForTest(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	return cmd()
}
