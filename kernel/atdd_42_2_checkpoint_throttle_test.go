package kernel

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.2: 韧性层 — Checkpoint 限流（shouldCheckpoint）验收测试
//
// Covers AC#1 (step-count trigger), AC#2 (time-window trigger), AC#3 (dedup).
// Tests target the throttling decision function only; actual disk writes are
// validated indirectly via the proc.lastCheckpointStep / lastCheckpointTime
// state updates after a successful trigger.
//
// RED PHASE: kernel.ShouldCheckpoint always returns false (see checkpoint_config.go).
// Remove t.Skip() in dev-story once the real throttle logic lands.
// =============================================================================

// newThrottleTestKernel builds a kernel with stepDataDir set so checkpoint
// machinery has a target directory; LLM device is unused here.
func newThrottleTestKernel(t *testing.T) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	k.SetStepDataDir(t.TempDir())
	return k
}

// newThrottleProc returns a minimal Process suitable for checkpoint decisions.
// Throttle state lives on Process (lastCheckpointStep / lastCheckpointTime,
// to be added by dev-story); accessors are package-internal so tests reach in.
func newThrottleProc() *Process {
	proc := NewProcess(0, "throttle test", nil)
	proc.UUID = "throttle-test-0000-0000-000000000001"
	return proc
}

// setLastCheckpoint primes the proc's last-checkpoint tracking fields used by
// the throttle decision (kernel/process.go fields added by Story 42.2).
func setLastCheckpoint(proc *Process, lastStep int, lastTime time.Time) {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	proc.lastCheckpointStep = lastStep
	proc.lastCheckpointTime = lastTime
}

// --- 42.2-UNIT-001: step 计数触发 (AC#1) ---

func TestATDD_42_2_001_StepCount_Triggers(t *testing.T) {
	

	k := newThrottleTestKernel(t)
	k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 2, IntervalSeconds: 3600})
	proc := newThrottleProc()

	setLastCheckpoint(proc, 0, time.Time{})
	if k.ShouldCheckpoint(proc, 1) {
		t.Error("step=1, lastStep=0, delta=1 < N=2: expected false")
	}
	if !k.ShouldCheckpoint(proc, 2) {
		t.Error("step=2, lastStep=0, delta=2 == N=2: expected true (AC#1)")
	}

	setLastCheckpoint(proc, 5, time.Now())
	if k.ShouldCheckpoint(proc, 6) {
		t.Error("step=6, lastStep=5, delta=1 < N=2: expected false")
	}
	if !k.ShouldCheckpoint(proc, 7) {
		t.Error("step=7, lastStep=5, delta=2 == N=2: expected true (AC#1)")
	}
}

// --- 42.2-UNIT-002: 时间窗口触发 (AC#2) ---

func TestATDD_42_2_002_TimeWindow_Triggers(t *testing.T) {
	

	k := newThrottleTestKernel(t)
	k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 1000, IntervalSeconds: 1})
	proc := newThrottleProc()

	now := time.Now()
	setLastCheckpoint(proc, 10, now)
	if k.ShouldCheckpoint(proc, 11) {
		t.Error("just-now lastTime: expected false")
	}

	setLastCheckpoint(proc, 10, now.Add(-2*time.Second))
	if !k.ShouldCheckpoint(proc, 11) {
		t.Error("step=11, lastStep=10, elapsed>=T=1s: expected true (AC#2)")
	}

	setLastCheckpoint(proc, 10, now.Add(-2*time.Second))
	if k.ShouldCheckpoint(proc, 10) {
		t.Error("step=10 == lastStep=10: time elapsed must not override AC#3 dedup")
	}
}

// --- 42.2-UNIT-003: 同 step 限流去重 (AC#3) ---

func TestATDD_42_2_003_SameStep_Dedup(t *testing.T) {
	

	k := newThrottleTestKernel(t)
	k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 1, IntervalSeconds: 1})
	proc := newThrottleProc()

	setLastCheckpoint(proc, 7, time.Now().Add(-10*time.Second))
	if k.ShouldCheckpoint(proc, 7) {
		t.Error("step=7 == lastStep=7: expected false (AC#3 dedup)")
	}
	if k.ShouldCheckpoint(proc, 6) {
		t.Error("step=6 < lastStep=7: expected false (AC#3 dedup, monotonic)")
	}
}

// --- 42.2-UNIT-003b: 初始状态触发 (边界 — lastStep=0, lastTime=zero) ---

func TestATDD_42_2_003b_InitialState_DoesNotPanicAndAllows(t *testing.T) {
	

	k := newThrottleTestKernel(t)
	k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 1, IntervalSeconds: 1})
	proc := newThrottleProc()

	// proc.lastCheckpointStep = 0, proc.lastCheckpointTime = zero (never written)
	// step=1, delta = 1 >= N=1 → should trigger.
	if !k.ShouldCheckpoint(proc, 1) {
		t.Error("initial state, step=1 >= N=1: expected true")
	}
}
