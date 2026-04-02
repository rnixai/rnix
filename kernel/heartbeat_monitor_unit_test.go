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
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 1 {
		t.Errorf("expected 1 stalled, got %d", status.TotalStalledDetected)
	}
}

func TestHeartbeatMonitor_Level1CancelStep(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-level1", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// Set a step cancel to verify it gets called
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
	if !called {
		t.Error("expected CancelStep to be called for Level 1 retry")
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

func TestHeartbeatMonitor_Level2Suspend(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-level2", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// Start the process goroutine so Suspend can wait on wg
	proc.wg.Go(func() {
		<-proc.ctx.Done()
	})

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed stall record with ConsecutiveStalls=1 to trigger Level 2
	hm.mu.Lock()
	hm.stalledProcs[proc.PID] = &stallRecord{
		PID:               proc.PID,
		UUID:              proc.UUID,
		ConsecutiveStalls: 1,
		FirstStalledAt:    time.Now().Add(-1 * time.Second),
		LastActionAt:      time.Now().Add(-500 * time.Millisecond),
	}
	hm.mu.Unlock()

	hm.scan()

	state := proc.GetState()
	if state != types.StateSuspended {
		t.Errorf("expected StateSuspended after Level 2, got %s", state)
	}

	// Verify removed from stalledProcs
	hm.mu.Lock()
	_, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if exists {
		t.Error("expected stall record to be removed after suspend")
	}
}

func TestHeartbeatMonitor_Recovery(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-recovery", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now() // healthy heartbeat
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
