package kernel

import (
	gocontext "context"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.1 — Shared helpers for the "unified pause-resume statemachine +
// subtree propagation" story.
//
// Conventions:
//   - Helpers are scoped to this story to avoid bleeding into other tests.
//   - All helpers call t.Helper() so failures point at the call site, not here.
//   - Process fixtures use kernel.newSimpleKernel as the underlying builder
//     (see kernel/kernel_test.go) so behaviour stays in step with the canonical
//     kernel constructor.
// =============================================================================

// newSubtreeKernel returns a freshly initialised kernel suitable for
// subtree-signal tests. Thin wrapper around newSimpleKernel for readability.
func newSubtreeKernel(t *testing.T) *KernelImpl {
	t.Helper()
	return newSimpleKernel(t)
}

// makeRunningProc44_1 creates a Running process registered in the kernel and
// linked to its parent (if any). Mirrors newSignalTestProcess from
// signal_test.go but kept self-contained to keep this story's helpers
// independent of other test files' churn.
func makeRunningProc44_1(t *testing.T, k *KernelImpl, parentPID types.PID, intent string) *Process {
	t.Helper()
	proc := NewProcess(parentPID, intent, nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	if parentPID != 0 {
		if parent, ok := k.GetProcess(parentPID); ok {
			parent.AddChild(proc.PID)
		}
	}
	return proc
}

// makeSuspendedProc44_1 creates a process and immediately transitions it to
// Suspended with the given reason, suitable as a fixture for resume-subtree
// tests where the initial state must already be Suspended.
func makeSuspendedProc44_1(t *testing.T, k *KernelImpl, parentPID types.PID, intent, reason string) *Process {
	t.Helper()
	proc := makeRunningProc44_1(t, k, parentPID, intent)
	proc.SetSuspendReason(reason)
	if err := proc.Suspend(); err != nil {
		t.Fatalf("proc.Suspend: %v", err)
	}
	return proc
}

// makeDeadProc44_1 creates a process directly in Dead state (no reasonStep
// goroutine ever runs). Used to assert that ResumeSubtree skips dead
// descendants without panicking.
func makeDeadProc44_1(t *testing.T, k *KernelImpl, parentPID types.PID, intent string) *Process {
	t.Helper()
	proc := NewProcess(parentPID, intent, nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	if err := proc.Terminate(ExitStatus{Code: 0, Reason: "completed"}); err != nil {
		t.Fatalf("proc.Terminate: %v", err)
	}
	if err := proc.Transition(types.StateDead); err != nil {
		t.Fatalf("proc.Transition(Dead): %v", err)
	}
	k.AddProcess(proc)
	if parentPID != 0 {
		if parent, ok := k.GetProcess(parentPID); ok {
			parent.AddChild(proc.PID)
		}
	}
	return proc
}

// makeFailedProc44_1 creates a Dead-state process whose Exit reason marks it
// as failed (non-zero exit). The story's AC2 distinguishes "Failed" from "Dead"
// for the purpose of emitted Resume events even though there is no
// StateFailed in the process state machine.
func makeFailedProc44_1(t *testing.T, k *KernelImpl, parentPID types.PID, intent string) *Process {
	t.Helper()
	proc := NewProcess(parentPID, intent, nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	if err := proc.Terminate(ExitStatus{Code: 1, Reason: "failed: test fixture"}); err != nil {
		t.Fatalf("proc.Terminate: %v", err)
	}
	if err := proc.Transition(types.StateDead); err != nil {
		t.Fatalf("proc.Transition(Dead): %v", err)
	}
	k.AddProcess(proc)
	if parentPID != 0 {
		if parent, ok := k.GetProcess(parentPID); ok {
			parent.AddChild(proc.PID)
		}
	}
	return proc
}

// drainDebugChan44_1 reads up to max events from proc.DebugChan, returning
// whatever arrived before the overall timeout. The kernel's DebugChan is
// buffered (256) so emitted events will be queued.
func drainDebugChan44_1(t *testing.T, proc *Process, max int, timeout time.Duration) []types.SyscallEvent {
	t.Helper()
	var events []types.SyscallEvent
	deadline := time.After(timeout)
	for len(events) < max {
		select {
		case ev := <-proc.DebugChan:
			events = append(events, ev)
		case <-deadline:
			return events
		}
	}
	return events
}

// assertProcState44_1 waits up to 500ms for proc to enter the desired state.
// Useful for transitions that ride on goroutine scheduling.
func assertProcState44_1(t *testing.T, proc *Process, want types.ProcessState, msg string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if proc.GetState() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := proc.GetState(); got != want {
		t.Fatalf("%s: state = %s, want %s", msg, got, want)
	}
}

// findEvent44_1 returns the first event in events whose Syscall matches name
// and whose Args[k] matches v for every (k,v) pair in want. Returns nil if not
// found. Used to assert specific Suspend/Resume event payloads.
func findEvent44_1(events []types.SyscallEvent, name string, want map[string]any) *types.SyscallEvent {
	for i := range events {
		ev := &events[i]
		if ev.Syscall != name {
			continue
		}
		match := true
		for k, v := range want {
			if ev.Args[k] != v {
				match = false
				break
			}
		}
		if match {
			return ev
		}
	}
	return nil
}
