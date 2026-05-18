package main

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD Epic 42 fix: elapsedDuration 在 Dead/Zombie 状态冻结
//
// Scenario: 进程被 K kill 或自然 EXIT 后，DeadAt 字段被 reap 写入。
// 旧实现：time.Since(CreatedAt) 不区分状态，elapsed 持续增长。
// 新实现：DeadAt 非零 → DeadAt - CreatedAt（冻结）；零 → time.Since (live)。
// =============================================================================

func TestATDD_42_Fix_ElapsedDuration_FreezesAtDeadAt(t *testing.T) {
	createdAt := time.Now().Add(-10 * time.Second)
	deadAt := createdAt.Add(3 * time.Second)

	procDead := vfs.ProcInfo{CreatedAt: createdAt, DeadAt: deadAt}
	got := elapsedDuration(procDead)
	want := 3 * time.Second
	if got != want {
		t.Errorf("dead: elapsedDuration = %v, want %v (frozen)", got, want)
	}
}

func TestATDD_42_Fix_ElapsedDuration_TicksWhileRunning(t *testing.T) {
	createdAt := time.Now().Add(-2 * time.Second)
	procRunning := vfs.ProcInfo{CreatedAt: createdAt} // DeadAt zero

	got := elapsedDuration(procRunning)
	// Should be ~2s and definitely growing — strict bound: >= 1.5s, < 5s.
	if got < 1500*time.Millisecond {
		t.Errorf("running: elapsedDuration = %v, want >= 1.5s", got)
	}
	if got > 5*time.Second {
		t.Errorf("running: elapsedDuration = %v, suspiciously large", got)
	}
}

func TestATDD_42_Fix_ElapsedDuration_RejectsInvalidDeadAt(t *testing.T) {
	// DeadAt earlier than CreatedAt → fall back to live elapsed (defensive).
	createdAt := time.Now().Add(-1 * time.Second)
	procWeird := vfs.ProcInfo{
		CreatedAt: createdAt,
		DeadAt:    createdAt.Add(-5 * time.Second), // before CreatedAt
	}
	got := elapsedDuration(procWeird)
	if got < 500*time.Millisecond {
		t.Errorf("invalid-DeadAt: elapsedDuration = %v, want >= 0.5s (live fallback)", got)
	}
}
