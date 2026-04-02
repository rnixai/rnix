package kernel

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// stallRecord tracks the stalled state of a single process.
type stallRecord struct {
	PID               types.PID
	UUID              string
	ConsecutiveStalls int       // number of consecutive stalled detections
	FirstStalledAt    time.Time // when first detected as stalled
	LastActionAt      time.Time // when the last recovery action was taken
}

// HeartbeatMonitor periodically scans all Running processes for stalled heartbeats
// and executes a three-level recovery strategy: retry → suspend → notify.
type HeartbeatMonitor struct {
	kernel        *KernelImpl
	checkInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu                   sync.Mutex
	stalledProcs         map[types.PID]*stallRecord
	totalStalledDetected int
	running              bool
}

// NewHeartbeatMonitor creates a new HeartbeatMonitor with the given check interval.
func NewHeartbeatMonitor(kernel *KernelImpl, checkInterval time.Duration) *HeartbeatMonitor {
	return &HeartbeatMonitor{
		kernel:        kernel,
		checkInterval: checkInterval,
		stalledProcs:  make(map[types.PID]*stallRecord),
	}
}

// Start launches the background heartbeat scanning goroutine.
func (hm *HeartbeatMonitor) Start() {
	hm.mu.Lock()
	if hm.running {
		hm.mu.Unlock()
		return
	}
	hm.ctx, hm.cancel = context.WithCancel(context.Background())
	hm.running = true
	hm.mu.Unlock()

	hm.wg.Add(1)
	go hm.loop()
}

// Stop signals the monitor to stop and waits for the goroutine to exit.
func (hm *HeartbeatMonitor) Stop() {
	hm.mu.Lock()
	if !hm.running {
		hm.mu.Unlock()
		return
	}
	hm.mu.Unlock()

	hm.cancel()
	hm.wg.Wait()

	hm.mu.Lock()
	hm.running = false
	hm.mu.Unlock()
}

// loop is the main scan loop running in a background goroutine.
func (hm *HeartbeatMonitor) loop() {
	defer hm.wg.Done()

	ticker := time.NewTicker(hm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hm.ctx.Done():
			return
		case <-ticker.C:
			hm.scan()
		}
	}
}

// scan iterates all Running processes and checks heartbeat liveness.
func (hm *HeartbeatMonitor) scan() {
	hm.kernel.procTable.Range(func(pid types.PID, proc *Process) bool {
		state := proc.GetState()
		if state != types.StateRunning {
			hm.cleanupIfTracked(pid)
			return true
		}

		proc.mu.Lock()
		lastHB := proc.LastHeartbeat
		timeout := proc.StepTimeout
		proc.mu.Unlock()

		// StepTimeout == 0 means user disabled timeout detection
		if timeout == 0 {
			return true
		}

		// Skip if heartbeat hasn't been set yet (process just started)
		if lastHB.IsZero() {
			return true
		}

		stalledDuration := time.Since(lastHB)
		if stalledDuration <= timeout {
			// Heartbeat is healthy — check if recovering from stalled
			hm.checkRecovery(pid, proc)
			return true
		}

		// Stalled! Execute recovery strategy
		hm.handleStalled(pid, proc, stalledDuration)
		return true
	})
}

// handleStalled executes the three-level recovery strategy for a stalled process.
func (hm *HeartbeatMonitor) handleStalled(pid types.PID, proc *Process, stalledDuration time.Duration) {
	hm.mu.Lock()
	record, exists := hm.stalledProcs[pid]
	if !exists {
		// First detection — create record
		record = &stallRecord{
			PID:               pid,
			UUID:              proc.UUID,
			ConsecutiveStalls: 0,
			FirstStalledAt:    time.Now(),
		}
		hm.stalledProcs[pid] = record
		hm.totalStalledDetected++
	}
	record.ConsecutiveStalls++
	consecutiveStalls := record.ConsecutiveStalls
	hm.mu.Unlock()

	proc.mu.Lock()
	stepTimeout := proc.StepTimeout
	lastHB := proc.LastHeartbeat
	proc.mu.Unlock()

	// Level 3 (Notify): always emit ProcessStalled event
	action := "retry"
	if consecutiveStalls >= 2 {
		action = "suspend"
	}
	hm.kernel.emitEvent(proc, "HeartbeatMonitor", map[string]any{
		"event":              "ProcessStalled",
		"pid":                pid,
		"uuid":               proc.UUID,
		"last_heartbeat_ms":  lastHB.UnixMilli(),
		"stalled_duration_ms": stalledDuration.Milliseconds(),
		"step_timeout_ms":    stepTimeout.Milliseconds(),
		"consecutive_stalls": consecutiveStalls,
		"action":             action,
	}, nil, nil, 0)

	if consecutiveStalls >= 2 {
		// Level 2 (Suspend): consecutive stalls, suspend the process
		log.Printf("[heartbeat] pid=%d stalled %d times, suspending (heartbeat_timeout)", pid, consecutiveStalls)
		proc.mu.Lock()
		proc.SuspendReason = "heartbeat_timeout"
		proc.mu.Unlock()
		if err := hm.kernel.Suspend(pid); err != nil {
			log.Printf("[heartbeat] pid=%d suspend failed: %v", pid, err)
		}
		// Remove from stalledProcs since process is now Suspended
		hm.mu.Lock()
		delete(hm.stalledProcs, pid)
		hm.mu.Unlock()
	} else {
		// Level 1 (Retry): cancel current step to trigger retry
		log.Printf("[heartbeat] pid=%d stalled, cancelling current step for retry", pid)
		proc.CancelStep()
		hm.mu.Lock()
		record.LastActionAt = time.Now()
		hm.mu.Unlock()
	}
}

// checkRecovery checks if a previously stalled process has recovered.
func (hm *HeartbeatMonitor) checkRecovery(pid types.PID, proc *Process) {
	hm.mu.Lock()
	record, exists := hm.stalledProcs[pid]
	if !exists {
		hm.mu.Unlock()
		return
	}
	recoveredAfter := time.Since(record.FirstStalledAt)
	delete(hm.stalledProcs, pid)
	hm.mu.Unlock()

	// Emit ProcessRecovered event
	hm.kernel.emitEvent(proc, "HeartbeatMonitor", map[string]any{
		"event":             "ProcessRecovered",
		"pid":               pid,
		"uuid":              proc.UUID,
		"recovered_after_ms": recoveredAfter.Milliseconds(),
		"consecutive_stalls": record.ConsecutiveStalls,
	}, nil, nil, 0)

	log.Printf("[heartbeat] pid=%d recovered after %v", pid, recoveredAfter)
}

// cleanupIfTracked removes a process from stalledProcs if it's tracked
// (process is no longer Running — Dead/Zombie/Suspended).
func (hm *HeartbeatMonitor) cleanupIfTracked(pid types.PID) {
	hm.mu.Lock()
	delete(hm.stalledProcs, pid)
	hm.mu.Unlock()
}

// Status returns the current heartbeat monitor status for IPC reporting.
func (hm *HeartbeatMonitor) Status() HeartbeatStatusInfo {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	stalled := make([]StalledProcInfo, 0, len(hm.stalledProcs))
	for _, record := range hm.stalledProcs {
		lastAction := "retry"
		if record.ConsecutiveStalls >= 2 {
			lastAction = "suspend"
		}
		stalled = append(stalled, StalledProcInfo{
			PID:               record.PID,
			UUID:              record.UUID,
			ConsecutiveStalls: record.ConsecutiveStalls,
			StalledDuration:   time.Since(record.FirstStalledAt),
			LastAction:        lastAction,
		})
	}

	return HeartbeatStatusInfo{
		Running:              hm.running,
		CheckInterval:        hm.checkInterval,
		TotalStalledDetected: hm.totalStalledDetected,
		CurrentStalled:       stalled,
	}
}

// HeartbeatStatusInfo is the kernel-side status snapshot.
type HeartbeatStatusInfo struct {
	Running              bool
	CheckInterval        time.Duration
	TotalStalledDetected int
	CurrentStalled       []StalledProcInfo
}

// StalledProcInfo describes a currently stalled process.
type StalledProcInfo struct {
	PID               types.PID
	UUID              string
	ConsecutiveStalls int
	StalledDuration   time.Duration
	LastAction        string
}
