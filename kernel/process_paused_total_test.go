package kernel

import (
	"testing"
	"time"
)

// Regression test for "暂停的子进程按 r 恢复后，时间显示突变成和父进程一样了".
//
// Pause/Resume must accumulate the paused duration in Process.pausedTotal so
// the Dashboard can compute "wall-clock minus paused-time" and the displayed
// elapsed value stays continuous across resume rather than jumping to the
// parent-process value.

func TestProcess_PausedTotal_AccumulatesAcrossResume(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	if proc.pausedTotal != 0 {
		t.Fatalf("expected pausedTotal=0 initially, got %v", proc.pausedTotal)
	}

	proc.Pause()
	time.Sleep(40 * time.Millisecond)
	proc.Resume()

	first := proc.pausedTotal
	if first < 30*time.Millisecond {
		t.Fatalf("first resume should accumulate ≥30ms, got %v", first)
	}

	// Second pause/resume cycle must add on top of the first.
	proc.Pause()
	time.Sleep(40 * time.Millisecond)
	proc.Resume()

	second := proc.pausedTotal
	if second <= first {
		t.Fatalf("pausedTotal must grow across cycles: first=%v second=%v", first, second)
	}
	if second-first < 30*time.Millisecond {
		t.Fatalf("second cycle should add ≥30ms, got delta=%v", second-first)
	}
}

func TestProcess_PausedTotal_ResumeWithoutPauseIsNoop(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.Resume() // not paused — should be silently no-op
	if proc.pausedTotal != 0 {
		t.Fatalf("idempotent Resume must not change pausedTotal, got %v", proc.pausedTotal)
	}
}

func TestProcess_PausedTotal_ExposedViaDetailSnapshot(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.Pause()
	time.Sleep(20 * time.Millisecond)
	proc.Resume()

	snap := proc.GetDetailSnapshot()
	if snap.PausedTotal < 15*time.Millisecond {
		t.Fatalf("DetailSnapshot.PausedTotal should reflect kernel state, got %v", snap.PausedTotal)
	}
}
