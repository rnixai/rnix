package kernel

import (
	"sync"
	"testing"

	"github.com/gonewx/crux/internal/types"
)

func TestNewKernel(t *testing.T) {
	k := NewKernel()
	if k == nil {
		t.Fatal("NewKernel returned nil")
	}
	procs := k.ListProcesses()
	if len(procs) != 0 {
		t.Fatalf("expected empty process table, got %d entries", len(procs))
	}
}

func TestKernelAddGetRemove(t *testing.T) {
	k := NewKernel()
	p := NewProcess(0, "test", nil)

	k.AddProcess(p)

	got, ok := k.GetProcess(p.PID)
	if !ok {
		t.Fatalf("GetProcess(%d) not found", p.PID)
	}
	if got != p {
		t.Fatal("GetProcess returned different pointer")
	}

	procs := k.ListProcesses()
	if len(procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(procs))
	}

	k.RemoveProcess(p.PID)
	_, ok = k.GetProcess(p.PID)
	if ok {
		t.Fatal("process should be removed")
	}

	procs = k.ListProcesses()
	if len(procs) != 0 {
		t.Fatalf("expected 0 processes after remove, got %d", len(procs))
	}
}

func TestKernelGetProcessNotFound(t *testing.T) {
	k := NewKernel()
	_, ok := k.GetProcess(9999)
	if ok {
		t.Fatal("expected not found for non-existent PID")
	}
}

func TestProcessTableConcurrent(t *testing.T) {
	k := NewKernel()
	const n = 100
	var wg sync.WaitGroup

	// Concurrent Add
	procs := make([]*Process, n)
	for i := 0; i < n; i++ {
		procs[i] = NewProcess(0, "test", nil)
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(p *Process) {
			defer wg.Done()
			k.AddProcess(p)
		}(procs[i])
	}
	wg.Wait()

	listed := k.ListProcesses()
	if len(listed) != n {
		t.Fatalf("expected %d processes, got %d", n, len(listed))
	}

	// Concurrent Get
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(pid types.PID) {
			defer wg.Done()
			_, ok := k.GetProcess(pid)
			if !ok {
				t.Errorf("GetProcess(%d) not found during concurrent read", pid)
			}
		}(procs[i].PID)
	}
	wg.Wait()

	// Concurrent Remove
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(pid types.PID) {
			defer wg.Done()
			k.RemoveProcess(pid)
		}(procs[i].PID)
	}
	wg.Wait()

	listed = k.ListProcesses()
	if len(listed) != 0 {
		t.Fatalf("expected 0 processes after concurrent remove, got %d", len(listed))
	}
}

func TestProcessTableConcurrentMixed(t *testing.T) {
	k := NewKernel()
	const n = 100
	var wg sync.WaitGroup

	// Mixed concurrent operations: Add, Get, Remove, List
	for i := 0; i < n; i++ {
		wg.Add(4)
		p := NewProcess(0, "test", nil)

		go func(p *Process) {
			defer wg.Done()
			k.AddProcess(p)
		}(p)

		go func(pid types.PID) {
			defer wg.Done()
			k.GetProcess(pid)
		}(p.PID)

		go func(pid types.PID) {
			defer wg.Done()
			k.RemoveProcess(pid)
		}(p.PID)

		go func() {
			defer wg.Done()
			k.ListProcesses()
		}()
	}
	wg.Wait()
}
