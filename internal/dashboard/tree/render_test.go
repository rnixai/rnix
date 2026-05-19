// Tests for Render (Epic 42 follow-up fix).
//
// 行为契约覆盖：
//   - Zombie/Dead 进程的 elapsed 计算 fallback 链：
//     DeadAt 非零 > LastHeartbeat 非零 > wall-clock(ctx.Now)
//   - 修复用户报告的 bug：Dashboard Agent Tree 中 kill / interrupted 进程的
//     时间值持续增长（因为 reap 未运行导致 DeadAt 始终为零，原回落直接走
//     wall-clock）。LastHeartbeat 兜底确保渲染稳定。

package tree

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func makeRenderCtx(now time.Time) RenderContext {
	return RenderContext{
		IsActive:         true,
		IsExpanded:       false,
		Now:              now,
		ReusedPIDs:       map[types.PID]int{},
		Recording:        map[string]string{},
		CollapsedIntents: map[string]string{},
	}
}

func makeState(rows []FlatRow) TreeState {
	return TreeState{
		Rows:               rows,
		Cursor:             0,
		Offset:             0,
		SortMode:           0,
		LastEventByPID:     map[types.PID]time.Time{},
		CollapsedDeadTrees: map[string]bool{},
		ProcessFirstSeenAt: map[types.PID]time.Time{},
	}
}

// TestRender_ZombieWithoutDeadAt_UsesLastHeartbeat verifies the defensive
// fallback: when a Zombie/Dead process has DeadAt == zero (reap did not run
// or daemon-crash-loaded history with state=zombie + dead_at empty), the
// elapsed value is computed from LastHeartbeat, not wall-clock — so rendering
// is stable across refresh ticks.
func TestRender_ZombieWithoutDeadAt_UsesLastHeartbeat(t *testing.T) {
	created := time.Date(2026, 5, 18, 17, 10, 18, 0, time.UTC)
	heartbeat := created.Add(5 * time.Second)
	// Two render calls with different `now` values — should produce identical elapsed.
	now1 := created.Add(1 * time.Minute)
	now2 := created.Add(15 * time.Minute) // 15 minutes later

	row := FlatRow{
		Proc: vfs.ProcInfo{
			PID:           3,
			UUID:          "test-zombie-no-deadat",
			State:         types.StateZombie,
			Intent:        "hi",
			Result:        "llm write failed",
			CreatedAt:     created,
			LastHeartbeat: heartbeat,
			// DeadAt intentionally left zero (the bug fixture)
		},
		Prefix: "",
	}

	state := makeState([]FlatRow{row})

	out1 := Render(state, makeRenderCtx(now1), 200, 10)
	out2 := Render(state, makeRenderCtx(now2), 200, 10)

	// Extract the data row (skip title line)
	rows1 := strings.Split(out1, "\n")
	rows2 := strings.Split(out2, "\n")
	if len(rows1) < 2 || len(rows2) < 2 {
		t.Fatalf("unexpected render output: out1=%q out2=%q", out1, out2)
	}

	// The elapsed segment for both renders must be IDENTICAL — proves
	// LastHeartbeat freezes the value rather than wall-clock-grow.
	if rows1[1] != rows2[1] {
		t.Errorf("elapsed not frozen across renders (LastHeartbeat fallback regression):\n  now1 row: %q\n  now2 row: %q",
			rows1[1], rows2[1])
	}

	// Also check it shows a small value (5s), not large (15m+).
	if strings.Contains(rows1[1], "15m") || strings.Contains(rows1[1], "10m") {
		t.Errorf("elapsed appears to use wall-clock (got large minute value), row=%q", rows1[1])
	}
}

// TestRender_DeadWithDeadAt_UsesDeadAt verifies normal reap path: DeadAt
// non-zero takes precedence over LastHeartbeat.
func TestRender_DeadWithDeadAt_UsesDeadAt(t *testing.T) {
	created := time.Date(2026, 5, 18, 17, 0, 0, 0, time.UTC)
	deadAt := created.Add(3 * time.Second)
	heartbeat := created.Add(2 * time.Second) // earlier than DeadAt
	now := time.Now()

	row := FlatRow{
		Proc: vfs.ProcInfo{
			PID:           1,
			UUID:          "test-dead-normal",
			State:         types.StateDead,
			Intent:        "task",
			Result:        "ok",
			CreatedAt:     created,
			LastHeartbeat: heartbeat,
			DeadAt:        deadAt,
		},
		Prefix: "",
	}

	out := Render(makeState([]FlatRow{row}), makeRenderCtx(now), 200, 10)
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		t.Fatalf("unexpected render output: %q", out)
	}
	// Should show ~3s elapsed (DeadAt - CreatedAt), not 2s (LastHeartbeat - CreatedAt)
	if !strings.Contains(rows[1], "3.0s") && !strings.Contains(rows[1], "3s") {
		t.Errorf("expected DeadAt path to produce 3s elapsed, got %q", rows[1])
	}
}

// TestRender_ZombieWithoutDeadAtOrHeartbeat_FallsBackToNow verifies the final
// fallback: if BOTH DeadAt and LastHeartbeat are zero, render does the
// existing wall-clock behavior (preserves backward compat for corner cases).
func TestRender_ZombieWithoutDeadAtOrHeartbeat_FallsBackToNow(t *testing.T) {
	created := time.Date(2026, 5, 18, 17, 0, 0, 0, time.UTC)
	now := created.Add(7 * time.Second)

	row := FlatRow{
		Proc: vfs.ProcInfo{
			PID:       9,
			UUID:      "test-zombie-bare",
			State:     types.StateZombie,
			Intent:    "task",
			Result:    "error",
			CreatedAt: created,
			// DeadAt and LastHeartbeat both zero
		},
		Prefix: "",
	}

	out := Render(makeState([]FlatRow{row}), makeRenderCtx(now), 200, 10)
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		t.Fatalf("unexpected render output: %q", out)
	}
	if !strings.Contains(rows[1], "7.0s") && !strings.Contains(rows[1], "7s") {
		t.Errorf("expected 7s wall-clock fallback, got %q", rows[1])
	}
}

// TestRender_RunningProc_PausedTotalSubtracted is the regression test for the
// user-reported bug "暂停的子进程，按 r 恢复后，时间显示突变成和父进程一样了".
//
// Pause/Resume must accumulate paused duration into ProcInfo.PausedTotal. The
// rendered elapsed value MUST exclude that paused time, otherwise a resumed
// child process "jumps" to the wall-clock elapsed of its parent because the
// entire paused window was retroactively counted as runtime.
func TestRender_RunningProc_PausedTotalSubtracted(t *testing.T) {
	created := time.Date(2026, 5, 18, 17, 0, 0, 0, time.UTC)
	now := created.Add(60 * time.Second)

	// Process was paused for 50s, so wall-clock=60s but runtime=10s.
	row := FlatRow{
		Proc: vfs.ProcInfo{
			PID:         11,
			UUID:        "test-resumed-child",
			State:       types.StateRunning,
			Intent:      "task",
			CreatedAt:   created,
			PausedTotal: 50 * time.Second,
		},
		Prefix: "",
	}

	out := Render(makeState([]FlatRow{row}), makeRenderCtx(now), 200, 10)
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		t.Fatalf("unexpected render output: %q", out)
	}

	// Must show ~10s, NOT the wall-clock 60s/1.0m.
	if !strings.Contains(rows[1], "10.0s") && !strings.Contains(rows[1], "10s") {
		t.Errorf("expected elapsed=10s after subtracting 50s of paused time, got %q", rows[1])
	}
	if strings.Contains(rows[1], "1.0m") || strings.Contains(rows[1], "60s") {
		t.Errorf("elapsed appears to include paused window (regression), got %q", rows[1])
	}
}

// TestRender_PausedProc_FreezesAtPausedAtMinusTotal verifies that while the
// process is still paused, the rendered elapsed is the runtime up to (but not
// including) the current pause — so it stays consistent with what will be
// shown after resume.
func TestRender_PausedProc_FreezesAtPausedAtMinusTotal(t *testing.T) {
	created := time.Date(2026, 5, 18, 17, 0, 0, 0, time.UTC)
	pausedAt := created.Add(30 * time.Second)
	now1 := pausedAt.Add(5 * time.Second)
	now2 := pausedAt.Add(120 * time.Second)

	// Earlier pause cycle already accounted for 8s.
	row := FlatRow{
		Proc: vfs.ProcInfo{
			PID:         12,
			UUID:        "test-paused-child",
			State:       types.StateRunning,
			Intent:      "task",
			CreatedAt:   created,
			IsPaused:    true,
			PausedAt:    pausedAt,
			PausedTotal: 8 * time.Second,
		},
		Prefix: "",
	}
	state := makeState([]FlatRow{row})

	out1 := Render(state, makeRenderCtx(now1), 200, 10)
	out2 := Render(state, makeRenderCtx(now2), 200, 10)
	rows1 := strings.Split(out1, "\n")
	rows2 := strings.Split(out2, "\n")
	if len(rows1) < 2 || len(rows2) < 2 {
		t.Fatalf("unexpected render: %q / %q", out1, out2)
	}

	// Both renders MUST be identical (frozen while paused) AND must reflect
	// runtime 30s - 8s = 22s, not the wall-clock 30s.
	if rows1[1] != rows2[1] {
		t.Errorf("paused elapsed not frozen across ticks:\n  now1: %q\n  now2: %q", rows1[1], rows2[1])
	}
	if !strings.Contains(rows1[1], "22.0s") && !strings.Contains(rows1[1], "22s") {
		t.Errorf("expected elapsed=22s (30s wall - 8s paused-total), got %q", rows1[1])
	}
}
