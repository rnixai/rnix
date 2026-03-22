package kernel

// =============================================================================
// ATDD Story 28.3: GetProcessByUUID 内核方法
// TDD RED PHASE — Tests reference methods not yet implemented
// =============================================================================
//
// Test Strategy:
//   AC-1: GetProcessByUUID finds running process by UUID
//   AC-6: ProcessManager interface includes GetProcessByUUID
//
// Priority: P0 (Kernel process lookup)
// Test Level: Unit

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// ---------------------------------------------------------------------------
// GetProcessByUUID: 基本查找
// ---------------------------------------------------------------------------

func TestATDD_28_3_GetProcessByUUID_BasicLookup(t *testing.T) {
	k, _, _ := newTestKernel(t, &mockLLMFile{})

	proc := NewProcess(0, "uuid basic lookup", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	// RED: GetProcessByUUID does not exist on KernelImpl
	found, ok := k.GetProcessByUUID(proc.UUID)
	if !ok {
		t.Fatal("GetProcessByUUID should find existing process")
	}
	if found.PID != proc.PID {
		t.Errorf("PID = %d, want %d", found.PID, proc.PID)
	}
	if found.UUID != proc.UUID {
		t.Errorf("UUID = %q, want %q", found.UUID, proc.UUID)
	}
}

func TestATDD_28_3_GetProcessByUUID_NotFound(t *testing.T) {
	k, _, _ := newTestKernel(t, &mockLLMFile{})

	// RED: GetProcessByUUID does not exist on KernelImpl
	_, ok := k.GetProcessByUUID("019576f5-0000-7000-8000-000000000000")
	if ok {
		t.Fatal("GetProcessByUUID should return false for non-existent UUID")
	}
}

func TestATDD_28_3_GetProcessByUUID_EmptyUUID(t *testing.T) {
	k, _, _ := newTestKernel(t, &mockLLMFile{})

	// RED: GetProcessByUUID does not exist on KernelImpl
	_, ok := k.GetProcessByUUID("")
	if ok {
		t.Fatal("GetProcessByUUID should return false for empty UUID")
	}
}

func TestATDD_28_3_GetProcessByUUID_AmongMultiple(t *testing.T) {
	k, _, _ := newTestKernel(t, &mockLLMFile{})

	procs := make([]*Process, 5)
	for i := range procs {
		procs[i] = NewProcess(0, "multi process", nil)
		_ = procs[i].Start()
		k.AddProcess(procs[i])
	}

	// Find the 3rd process by UUID
	target := procs[2]
	// RED: GetProcessByUUID does not exist on KernelImpl
	found, ok := k.GetProcessByUUID(target.UUID)
	if !ok {
		t.Fatal("GetProcessByUUID should find process among multiple")
	}
	if found.PID != target.PID {
		t.Errorf("PID = %d, want %d", found.PID, target.PID)
	}
}

func TestATDD_28_3_GetProcessByUUID_ZombieProcess_StillInTable(t *testing.T) {
	k, _, _ := newTestKernel(t, &mockLLMFile{})

	proc := NewProcess(0, "dead but in table", nil)
	_ = proc.Start()
	k.AddProcess(proc)
	proc.State = types.StateZombie

	// Zombie processes still in procTable should be findable
	// RED: GetProcessByUUID does not exist on KernelImpl
	found, ok := k.GetProcessByUUID(proc.UUID)
	if !ok {
		t.Fatal("GetProcessByUUID should find zombie process still in procTable")
	}
	if found.PID != proc.PID {
		t.Errorf("PID = %d, want %d", found.PID, proc.PID)
	}
}
