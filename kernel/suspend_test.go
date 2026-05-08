package kernel

import (
	"context"
	"fmt"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// mockClosableVFSFile is a minimal VFSFile mock that tracks Close calls.
type mockClosableVFSFile struct {
	path   string
	closed bool
}

func (f *mockClosableVFSFile) Read(n int) ([]byte, error)                        { return nil, nil }
func (f *mockClosableVFSFile) Write(_ context.Context, _ []byte) error           { return nil }
func (f *mockClosableVFSFile) Close() error                                      { f.closed = true; return nil }
func (f *mockClosableVFSFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{DevicePath: f.path}, nil
}

func TestSuspendProcess_ClosesAllFDs(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test suspend fd", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	fd0 := &mockClosableVFSFile{path: "/dev/llm/claude"}
	fd1 := &mockClosableVFSFile{path: "/dev/fs"}
	proc.mu.Lock()
	proc.FDTable[types.FD(0)] = fd0
	proc.FDTable[types.FD(1)] = fd1
	proc.mu.Unlock()

	err := k.suspendProcess(proc, "test_reason", ExitSuspended)
	if err != nil {
		t.Fatalf("suspendProcess failed: %v", err)
	}

	if proc.GetState() != types.StateSuspended {
		t.Fatalf("expected StateSuspended, got %s", proc.GetState())
	}

	proc.mu.Lock()
	fdCount := len(proc.FDTable)
	reason := proc.SuspendReason
	proc.mu.Unlock()

	if fdCount != 0 {
		t.Fatalf("expected 0 FDs after suspend, got %d", fdCount)
	}
	if reason != "test_reason" {
		t.Fatalf("expected SuspendReason 'test_reason', got %q", reason)
	}
	if !fd0.closed || !fd1.closed {
		t.Fatal("all FDs should be closed after suspend")
	}
}

func TestSuspendProcess_DrainsCheckpointChannel(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test suspend drain", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	proc.mu.Lock()
	proc.checkpointErrCh = make(chan error, 1)
	proc.checkpointErrCh <- fmt.Errorf("stale error")
	proc.mu.Unlock()

	err := k.suspendProcess(proc, "drain_test", ExitSuspended)
	if err != nil {
		t.Fatalf("suspendProcess failed: %v", err)
	}

	// Channel should be drained
	select {
	case <-proc.checkpointErrCh:
		t.Fatal("checkpoint error channel should have been drained")
	default:
		// OK — empty as expected
	}
}

func TestSuspend_NonRunningProcess(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test suspend non-running", nil)
	k.AddProcess(proc)
	// Process is in Created state
	err := k.Suspend(proc.PID)
	if err == nil {
		t.Fatal("expected error suspending non-Running process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Fatalf("expected ErrInvalid, got %s", se.Code)
	}
}

func TestSuspend_NotFound(t *testing.T) {
	k := newSimpleKernel(t)
	err := k.Suspend(types.PID(9999))
	if err == nil {
		t.Fatal("expected error for non-existent process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %s", se.Code)
	}
}

func TestSelfSuspend(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test self-suspend", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	err := k.selfSuspend(proc, "loop_detected", ExitSuspended)
	if err != nil {
		t.Fatalf("selfSuspend failed: %v", err)
	}
	if proc.GetState() != types.StateSuspended {
		t.Fatalf("expected StateSuspended, got %s", proc.GetState())
	}
	if !proc.IsSuspendRequested() {
		t.Fatal("suspendRequested should be true after selfSuspend")
	}
	proc.mu.Lock()
	reason := proc.SuspendReason
	proc.mu.Unlock()
	if reason != "loop_detected" {
		t.Fatalf("expected reason 'loop_detected', got %q", reason)
	}
}

func TestSuspendProcess_DoesNotRemoveFromProcTable(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test suspend visibility", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	_ = k.suspendProcess(proc, "test", ExitSuspended)

	// Process should still be in table
	_, ok := k.GetProcess(proc.PID)
	if !ok {
		t.Fatal("suspended process should remain in process table")
	}
}

func TestKill_SuspendedProcess_Termination(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test kill suspended", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	// Suspend first
	_ = k.suspendProcess(proc, "test", ExitSuspended)
	if proc.GetState() != types.StateSuspended {
		t.Fatalf("expected StateSuspended, got %s", proc.GetState())
	}

	// Kill with SIGKILL
	err := k.Kill(proc.PID, types.SIGKILL)
	if err != nil {
		t.Fatalf("Kill on suspended process failed: %v", err)
	}

	if proc.GetState() != types.StateDead {
		t.Fatalf("expected StateDead after kill, got %s", proc.GetState())
	}

	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil {
		t.Fatal("Exit should be set after kill")
	}
	if exit.Code != 1 {
		t.Fatalf("expected exit code 1, got %d", exit.Code)
	}
}

func TestKill_SuspendedProcess_SIGTERM(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test sigterm suspended", nil)
	_ = proc.Start()
	k.AddProcess(proc)
	_ = k.suspendProcess(proc, "test", ExitSuspended)

	err := k.Kill(proc.PID, types.SIGTERM)
	if err != nil {
		t.Fatalf("Kill SIGTERM on suspended failed: %v", err)
	}
	if proc.GetState() != types.StateDead {
		t.Fatalf("expected StateDead, got %s", proc.GetState())
	}
}

func TestKill_SuspendedProcess_NonTermination(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test sigpause suspended", nil)
	_ = proc.Start()
	k.AddProcess(proc)
	_ = k.suspendProcess(proc, "test", ExitSuspended)

	err := k.Kill(proc.PID, types.SIGPAUSE)
	if err != nil {
		t.Fatalf("Kill SIGPAUSE on suspended failed: %v", err)
	}
	// Should remain suspended
	if proc.GetState() != types.StateSuspended {
		t.Fatalf("expected StateSuspended after SIGPAUSE, got %s", proc.GetState())
	}
}

func TestSignal_SuspendedProcess_Termination(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test signal suspended", nil)
	_ = proc.Start()
	k.AddProcess(proc)
	_ = k.suspendProcess(proc, "test", ExitSuspended)

	err := k.Signal(proc.PID, types.SIGKILL)
	if err != nil {
		t.Fatalf("Signal SIGKILL on suspended failed: %v", err)
	}
	if proc.GetState() != types.StateDead {
		t.Fatalf("expected StateDead, got %s", proc.GetState())
	}
}
