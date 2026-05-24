package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
)

func TestNewHeartbeatMonitor(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	hm := NewHeartbeatMonitor(k, 100*time.Millisecond)
	if hm == nil {
		t.Fatal("NewHeartbeatMonitor returned nil")
	}
	if hm.checkInterval != 100*time.Millisecond {
		t.Errorf("checkInterval = %v, want 100ms", hm.checkInterval)
	}
}

func TestHeartbeatMonitor_StartStop(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.Start()
	status := hm.Status()
	if !status.Running {
		t.Error("expected running=true after Start")
	}
	hm.Stop()
	status = hm.Status()
	if status.Running {
		t.Error("expected running=false after Stop")
	}
}

func TestHeartbeatMonitor_StartIdempotent(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.Start()
	hm.Start() // second call should be no-op
	hm.Stop()
}

func TestHeartbeatMonitor_StopIdempotent(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.Stop() // stop before start should be no-op
	hm.Start()
	hm.Stop()
	hm.Stop() // second stop should be no-op
}

func TestHeartbeatMonitor_ScanSkipsStepTimeoutZero(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-timeout-zero", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 0 // disabled
	proc.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	proc.PrimaryDevice = "/dev/llm/claude" // simulate reasonStep-driven proc (Story 44.5 v2 review附 heartbeat fix)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 0 {
		t.Errorf("expected 0 stalled, got %d", status.TotalStalledDetected)
	}
}

func TestHeartbeatMonitor_ScanSkipsNonRunning(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-zombie", nil)
	proc.mu.Lock()
	proc.State = types.StateZombie
	proc.StepTimeout = 5 * time.Second
	proc.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	proc.PrimaryDevice = "/dev/llm/claude" // simulate reasonStep-driven proc (Story 44.5 v2 review附 heartbeat fix)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 0 {
		t.Errorf("expected 0 stalled for non-running proc, got %d", status.TotalStalledDetected)
	}
}

func TestHeartbeatMonitor_ScanDetectsStalled(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-stalled", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond) // way past timeout
	proc.PrimaryDevice = "/dev/llm/claude"                       // non-empty → reasonStep-driven
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 1 {
		t.Errorf("expected 1 stalled, got %d", status.TotalStalledDetected)
	}
}

// TestHeartbeatMonitor_ScanSkipsScriptRunnerWithActiveChild asserts that a
// script-runner (empty PrimaryDevice) WITH a Running child is NOT scanned for
// heartbeat staleness — it is parked in SpawnAndWait waiting for the child,
// which is the happy path. The user-visible regression on dev-auto.ash
// (PID 1 "STALL no heartbeat 328s") is exactly this case.
//
// Story 44.5 v2 review (附 heartbeat fix). Coexists with the Story 45.4
// AC3.002 invariant — see TestHeartbeatMonitor_ScanDetectsIdleScriptRunner
// below for the inverse "no children → genuine deadlock → still report" case.
func TestHeartbeatMonitor_ScanSkipsScriptRunnerWithActiveChild(t *testing.T) {
	k := newHeartbeatTestKernel(t)

	parent := NewProcess(0, "test-script-runner-with-child", nil)
	parent.mu.Lock()
	parent.State = types.StateRunning
	parent.StepTimeout = 100 * time.Millisecond
	parent.LastHeartbeat = time.Now().Add(-10 * time.Minute) // way past any timeout
	parent.PrimaryDevice = ""                                // script-runner marker
	parent.mu.Unlock()
	k.procTable.Store(parent.PID, parent)

	child := NewProcess(parent.PID, "child-of-script-runner", nil)
	child.mu.Lock()
	child.State = types.StateRunning
	child.LastHeartbeat = time.Now()
	child.PrimaryDevice = "/dev/llm/claude"
	child.mu.Unlock()
	k.procTable.Store(child.PID, child)
	parent.AddChild(child.PID)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 0 {
		t.Errorf("script-runner with active Running child must NOT be detected "+
			"as stalled, got TotalStalledDetected=%d "+
			"(parent is parked in SpawnAndWait waiting for the child — that is "+
			"the happy path, not a deadlock)", status.TotalStalledDetected)
	}
	for _, s := range status.CurrentStalled {
		if s.PID == parent.PID {
			t.Errorf("script-runner parent (pid=%d) must not appear in CurrentStalled while "+
				"a child is Running", parent.PID)
		}
	}
}

// TestHeartbeatMonitor_ScanSkipsScriptRunnerWithSuspendedChild is the
// EchoMatrix-observed case: the user pressed `p` on a child (PID 3 in the
// real-world repro), which suspended it; the script-runner parent (PID 1)
// is parked in SpawnAndWait waiting for ResumeSubtree. The Running-only
// child filter would have classified this as "idle" and fired STALL. The
// corrected filter recognises Suspended children as a wait-for-user signal,
// not a deadlock.
func TestHeartbeatMonitor_ScanSkipsScriptRunnerWithSuspendedChild(t *testing.T) {
	k := newHeartbeatTestKernel(t)

	parent := NewProcess(0, "test-script-runner-suspended-child", nil)
	parent.mu.Lock()
	parent.State = types.StateRunning
	parent.StepTimeout = 100 * time.Millisecond
	parent.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	parent.PrimaryDevice = ""
	parent.mu.Unlock()
	k.procTable.Store(parent.PID, parent)

	deadChild := NewProcess(parent.PID, "previous-child-now-dead", nil)
	deadChild.mu.Lock()
	deadChild.State = types.StateDead
	deadChild.PrimaryDevice = "/dev/llm/claude"
	deadChild.mu.Unlock()
	k.procTable.Store(deadChild.PID, deadChild)
	parent.AddChild(deadChild.PID)

	suspendedChild := NewProcess(parent.PID, "user-paused-child", nil)
	suspendedChild.mu.Lock()
	suspendedChild.State = types.StateSuspended
	suspendedChild.PrimaryDevice = "/dev/llm/claude"
	suspendedChild.mu.Unlock()
	k.procTable.Store(suspendedChild.PID, suspendedChild)
	parent.AddChild(suspendedChild.PID)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 0 {
		t.Errorf("script-runner with a Suspended child must NOT be detected "+
			"as stalled, got TotalStalledDetected=%d "+
			"(parent is parked in SpawnAndWait waiting for the user to Resume; "+
			"this is the EchoMatrix PID 1 + paused PID 3 case observed at "+
			"16:45:21 STALL — Running-only filter mis-classified it as idle)",
			status.TotalStalledDetected)
	}
}


// previous case: a script-runner WITHOUT any active children is still scanned
// for staleness — this preserves the Story 45.4 AC3.002 invariant that HB-1's
// removal must leave genuine deadlocks observable, just without surfacing
// happy-path "waiting for child" idleness as STALL.
func TestHeartbeatMonitor_ScanDetectsIdleScriptRunner(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-script-runner-idle", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond) // past timeout
	proc.PrimaryDevice = ""                                      // script-runner marker
	// No children added — the genuinely-idle case.
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 1 {
		t.Errorf("idle script-runner (no active children) must still be detected "+
			"as stalled, got TotalStalledDetected=%d "+
			"(Story 45.4 AC3.002 invariant: HB-1 removal must leave genuine "+
			"deadlocks observable)", status.TotalStalledDetected)
	}
}

// TestHeartbeatMonitor_ScanScriptRunnerCleansStaleRecord asserts that a
// previously-tracked stall record for a script-runner is cleaned up once it
// gains an active child (e.g., its SpawnAndWait kicks in).
func TestHeartbeatMonitor_ScanScriptRunnerCleansStaleRecord(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	parent := NewProcess(0, "test-script-runner-clean", nil)
	parent.mu.Lock()
	parent.State = types.StateRunning
	parent.StepTimeout = 100 * time.Millisecond
	parent.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	parent.PrimaryDevice = ""
	parent.mu.Unlock()
	k.procTable.Store(parent.PID, parent)

	child := NewProcess(parent.PID, "child", nil)
	child.mu.Lock()
	child.State = types.StateRunning
	child.PrimaryDevice = "/dev/llm/claude"
	child.mu.Unlock()
	k.procTable.Store(child.PID, child)
	parent.AddChild(child.PID)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed a stale record from an earlier scan (simulating the
	// pre-fix state where this process WAS tracked).
	hm.mu.Lock()
	hm.stalledProcs[parent.PID] = &stallRecord{PID: parent.PID, UUID: parent.UUID}
	hm.mu.Unlock()

	hm.scan()

	hm.mu.Lock()
	_, exists := hm.stalledProcs[parent.PID]
	hm.mu.Unlock()
	if exists {
		t.Error("stale stall record for script-runner with active child must be removed on the next scan")
	}
}

func TestHeartbeatMonitor_Level1WarnOnly(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-level1", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.PrimaryDevice = "/dev/llm/claude" // simulate reasonStep-driven proc (Story 44.5 v2 review附 heartbeat fix)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// Set a step cancel to verify it does NOT get called at Level 1
	var cancelCalled bool
	var mu sync.Mutex
	proc.SetStepCancel(func() {
		mu.Lock()
		cancelCalled = true
		mu.Unlock()
	})

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	mu.Lock()
	called := cancelCalled
	mu.Unlock()
	if called {
		t.Error("expected CancelStep NOT to be called at Level 1 (warn only)")
	}

	// Verify consecutive stalls = 1
	hm.mu.Lock()
	record, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if !exists {
		t.Fatal("expected stall record to exist")
	}
	if record.ConsecutiveStalls != 1 {
		t.Errorf("expected ConsecutiveStalls=1, got %d", record.ConsecutiveStalls)
	}
}

// TestHeartbeatMonitor_Level3CancelStep
//
// Story 45.2: passive mode — CancelStep is NOT called, only warn event emitted.
// Pre-impl behavior (CancelStep invoked at Level 3) was removed; the assertion
// is inverted to defend the new contract. See AC5 / Task 5.1 + Decision D2
// (in-place rewrite to preserve git blame trace to Story 30.5 / 30.6 / 43.1).
func TestHeartbeatMonitor_Level3CancelStep(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-level3", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.PrimaryDevice = "/dev/llm/claude" // simulate reasonStep-driven proc (Story 44.5 v2 review附 heartbeat fix)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// Set a step cancel to verify it does NOT get called at Level 3 (Story 45.2).
	var cancelCalled bool
	var mu sync.Mutex
	proc.SetStepCancel(func() {
		mu.Lock()
		cancelCalled = true
		mu.Unlock()
	})

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed with ConsecutiveStalls=2 so next scan triggers Level 3
	hm.mu.Lock()
	hm.stalledProcs[proc.PID] = &stallRecord{
		PID:               proc.PID,
		UUID:              proc.UUID,
		ConsecutiveStalls: 2,
		FirstStalledAt:    time.Now().Add(-1 * time.Second),
	}
	hm.mu.Unlock()

	hm.scan()

	mu.Lock()
	called := cancelCalled
	mu.Unlock()
	if called {
		t.Error("Story 45.2: passive mode — CancelStep MUST NOT be called at Level 3")
	}

	// Story 45.2: stallRecord must persist (not deleted), accumulate ConsecutiveStalls,
	// and update LastActionAt so dashboard renders "last warned at" timestamps.
	hm.mu.Lock()
	record, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if !exists {
		t.Fatal("Story 45.2: stallRecord MUST persist after Level 3 scan (passive mode keeps tracking)")
	}
	if record.ConsecutiveStalls != 3 {
		t.Errorf("Story 45.2: ConsecutiveStalls = %d, want 3 (2 pre-seed + 1 this scan)", record.ConsecutiveStalls)
	}
	if record.LastActionAt.IsZero() {
		t.Error("Story 45.2: LastActionAt MUST be set after Level 3 scan (kept for dashboard 'last warned' timestamp)")
	}
}

// TestHeartbeatMonitor_Level4Suspend
//
// Story 45.2: passive mode — process MUST remain StateRunning after Level 4,
// SuspendReason MUST NOT be written, stallRecord MUST persist for continued
// tracking. Pre-impl behavior (Suspend(pid) + delete(stalledProcs)) was
// removed; the assertions are inverted to defend the new contract. See AC5 /
// Task 5.2 + Decision D2 (in-place rewrite to preserve git blame trace to
// Story 30.5 / 30.6 / 43.1).
func TestHeartbeatMonitor_Level4Suspend(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-level4", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.PrimaryDevice = "/dev/llm/claude" // simulate reasonStep-driven proc (Story 44.5 v2 review附 heartbeat fix)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// Story 45.2: passive mode no longer invokes Suspend(pid), so the goroutine
	// setup `proc.wg.Go(func() { <-proc.ctx.Done() })` is no longer needed and
	// is intentionally omitted (would leak past t.Cleanup boundary).

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed stall record with ConsecutiveStalls=3 to trigger Level 4
	hm.mu.Lock()
	hm.stalledProcs[proc.PID] = &stallRecord{
		PID:               proc.PID,
		UUID:              proc.UUID,
		ConsecutiveStalls: 3,
		FirstStalledAt:    time.Now().Add(-2 * time.Second),
		LastActionAt:      time.Now().Add(-500 * time.Millisecond),
	}
	hm.mu.Unlock()

	hm.scan()

	state := proc.GetState()
	if state != types.StateRunning {
		t.Errorf("Story 45.2: passive mode — process MUST remain StateRunning after Level 4, got %s", state)
	}

	// Story 45.2: stallRecord MUST persist (passive mode no longer deletes).
	hm.mu.Lock()
	record, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if !exists {
		t.Fatal("Story 45.2: passive mode — stall record MUST persist after Level 4 for continued tracking")
	}
	if record.ConsecutiveStalls != 4 {
		t.Errorf("Story 45.2: ConsecutiveStalls = %d, want 4 (3 pre-seed + 1 this scan)", record.ConsecutiveStalls)
	}

	// Story 45.2: SuspendReason MUST NOT be written by HeartbeatMonitor.
	if reason := proc.GetSuspendReason(); reason != "" {
		t.Errorf("Story 45.2: SuspendReason MUST remain empty (was %q) — daemon no longer writes \"heartbeat_timeout\"", reason)
	}
}

func TestHeartbeatMonitor_Recovery(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-recovery", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now() // healthy heartbeat
	proc.PrimaryDevice = "/dev/llm/claude" // simulate reasonStep-driven proc (Story 44.5 v2 review附 heartbeat fix)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed stall record
	hm.mu.Lock()
	hm.stalledProcs[proc.PID] = &stallRecord{
		PID:               proc.PID,
		UUID:              proc.UUID,
		ConsecutiveStalls: 1,
		FirstStalledAt:    time.Now().Add(-1 * time.Second),
	}
	hm.mu.Unlock()

	hm.scan()

	// Should be cleared
	hm.mu.Lock()
	_, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if exists {
		t.Error("expected stall record to be cleared after recovery")
	}
}

func TestHeartbeatMonitor_CleanupOnProcessExit(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-cleanup", nil)
	proc.mu.Lock()
	proc.State = types.StateDead // no longer running
	proc.StepTimeout = 100 * time.Millisecond
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed stall record
	hm.mu.Lock()
	hm.stalledProcs[proc.PID] = &stallRecord{
		PID:  proc.PID,
		UUID: proc.UUID,
	}
	hm.mu.Unlock()

	hm.scan()

	hm.mu.Lock()
	_, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if exists {
		t.Error("expected stall record to be removed for dead process")
	}
}

func TestHeartbeatMonitor_Status(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	hm := NewHeartbeatMonitor(k, 30*time.Second)
	hm.Start()
	defer hm.Stop()

	status := hm.Status()
	if !status.Running {
		t.Error("expected running=true")
	}
	if status.CheckInterval != 30*time.Second {
		t.Errorf("checkInterval = %v, want 30s", status.CheckInterval)
	}
	if status.TotalStalledDetected != 0 {
		t.Errorf("totalStalledDetected = %d, want 0", status.TotalStalledDetected)
	}
	if len(status.CurrentStalled) != 0 {
		t.Errorf("currentStalled len = %d, want 0", len(status.CurrentStalled))
	}
}

// newHeartbeatTestKernel creates a minimal KernelImpl for heartbeat monitor tests.
func newHeartbeatTestKernel(t *testing.T) *KernelImpl {
	t.Helper()
	k := &KernelImpl{
		procTable:   xsync.NewSyncMap[types.PID, *Process](),
		procHistory: NewProcessHistory(100),
	}
	return k
}
