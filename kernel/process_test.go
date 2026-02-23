package kernel

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gonewx/crux/internal/types"
)

func TestNewProcess(t *testing.T) {
	p := NewProcess(0, "analyze code", []string{"code-analyst"})

	t.Run("PID is positive", func(t *testing.T) {
		if p.PID == 0 {
			t.Fatal("PID should be > 0")
		}
	})

	t.Run("PPID matches", func(t *testing.T) {
		if p.PPID != 0 {
			t.Fatalf("expected PPID 0, got %d", p.PPID)
		}
	})

	t.Run("initial state is Created", func(t *testing.T) {
		if p.State != types.StateCreated {
			t.Fatalf("expected StateCreated, got %d", p.State)
		}
	})

	t.Run("Intent is set", func(t *testing.T) {
		if p.Intent != "analyze code" {
			t.Fatalf("expected intent 'analyze code', got %q", p.Intent)
		}
	})

	t.Run("Skills are set", func(t *testing.T) {
		if len(p.Skills) != 1 || p.Skills[0] != "code-analyst" {
			t.Fatalf("unexpected skills: %v", p.Skills)
		}
	})

	t.Run("Children is empty slice not nil", func(t *testing.T) {
		if p.Children == nil {
			t.Fatal("Children should be empty slice, not nil")
		}
		if len(p.Children) != 0 {
			t.Fatalf("expected 0 children, got %d", len(p.Children))
		}
	})

	t.Run("FDTable is initialized", func(t *testing.T) {
		if p.FDTable == nil {
			t.Fatal("FDTable should be initialized, not nil")
		}
	})

	t.Run("Done channel is buffered", func(t *testing.T) {
		if cap(p.Done) != 1 {
			t.Fatalf("expected Done channel cap 1, got %d", cap(p.Done))
		}
	})

	t.Run("Exit is nil", func(t *testing.T) {
		if p.Exit != nil {
			t.Fatal("Exit should be nil for new process")
		}
	})

	t.Run("CreatedAt is set", func(t *testing.T) {
		if p.CreatedAt.IsZero() {
			t.Fatal("CreatedAt should be set")
		}
	})
}

func TestLegalTransitions(t *testing.T) {
	p := NewProcess(0, "test", nil)

	// Created → Running
	if err := p.Transition(types.StateRunning); err != nil {
		t.Fatalf("Created→Running failed: %v", err)
	}
	if p.State != types.StateRunning {
		t.Fatalf("expected StateRunning, got %d", p.State)
	}

	// Running → Zombie
	if err := p.Transition(types.StateZombie); err != nil {
		t.Fatalf("Running→Zombie failed: %v", err)
	}
	if p.State != types.StateZombie {
		t.Fatalf("expected StateZombie, got %d", p.State)
	}

	// Zombie → Dead
	if err := p.Transition(types.StateDead); err != nil {
		t.Fatalf("Zombie→Dead failed: %v", err)
	}
	if p.State != types.StateDead {
		t.Fatalf("expected StateDead, got %d", p.State)
	}
}

func TestStart(t *testing.T) {
	p := NewProcess(0, "test", nil)
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if p.State != types.StateRunning {
		t.Fatalf("expected StateRunning, got %d", p.State)
	}
}

func TestTerminate(t *testing.T) {
	p := NewProcess(0, "test", nil)
	_ = p.Start()

	exit := ExitStatus{Code: 0, Reason: "done"}
	if err := p.Terminate(exit); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	if p.State != types.StateZombie {
		t.Fatalf("expected StateZombie, got %d", p.State)
	}
	if p.Exit == nil {
		t.Fatal("Exit should be set after Terminate")
	}
	if p.Exit.Code != 0 {
		t.Fatalf("expected exit code 0, got %d", p.Exit.Code)
	}
	if p.Exit.Reason != "done" {
		t.Fatalf("expected reason 'done', got %q", p.Exit.Reason)
	}
}

func TestTerminateWithError(t *testing.T) {
	p := NewProcess(0, "test", nil)
	_ = p.Start()

	underlying := errors.New("timeout exceeded")
	exit := ExitStatus{Code: 1, Reason: "llm timeout", Err: underlying}
	if err := p.Terminate(exit); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	if p.GetState() != types.StateZombie {
		t.Fatalf("expected StateZombie, got %d", p.GetState())
	}
	if p.Exit == nil {
		t.Fatal("Exit should be set after Terminate")
	}
	if p.Exit.Code != 1 {
		t.Fatalf("expected exit code 1, got %d", p.Exit.Code)
	}
	if p.Exit.Reason != "llm timeout" {
		t.Fatalf("expected reason 'llm timeout', got %q", p.Exit.Reason)
	}
	if p.Exit.Err != underlying {
		t.Fatalf("expected underlying error preserved, got %v", p.Exit.Err)
	}
}

func TestReap(t *testing.T) {
	p := NewProcess(0, "test", nil)
	_ = p.Start()
	_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})

	if err := p.Reap(); err != nil {
		t.Fatalf("Reap failed: %v", err)
	}
	if p.State != types.StateDead {
		t.Fatalf("expected StateDead, got %d", p.State)
	}
}

func TestIllegalTransitions(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*Process)
		target    types.ProcessState
		wantState types.ProcessState
	}{
		{
			name:      "Running→Created",
			setup:     func(p *Process) { _ = p.Start() },
			target:    types.StateCreated,
			wantState: types.StateRunning,
		},
		{
			name: "Zombie→Running",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})
			},
			target:    types.StateRunning,
			wantState: types.StateZombie,
		},
		{
			name: "Zombie→Created",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})
			},
			target:    types.StateCreated,
			wantState: types.StateZombie,
		},
		{
			name: "Dead→Running",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})
				_ = p.Reap()
			},
			target:    types.StateRunning,
			wantState: types.StateDead,
		},
		{
			name: "Dead→Created",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})
				_ = p.Reap()
			},
			target:    types.StateCreated,
			wantState: types.StateDead,
		},
		{
			name: "Dead→Zombie",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})
				_ = p.Reap()
			},
			target:    types.StateZombie,
			wantState: types.StateDead,
		},
		{
			name:      "Created→Zombie (must go through Running)",
			setup:     func(p *Process) {},
			target:    types.StateZombie,
			wantState: types.StateCreated,
		},
		{
			name:      "Created→Dead (must go through Running→Zombie)",
			setup:     func(p *Process) {},
			target:    types.StateDead,
			wantState: types.StateCreated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProcess(0, "test", nil)
			tc.setup(p)

			err := p.Transition(tc.target)
			if err == nil {
				t.Fatal("expected error for illegal transition, got nil")
			}

			// Verify it's a *SyscallError
			se, ok := err.(*SyscallError)
			if !ok {
				t.Fatalf("expected *SyscallError, got %T", err)
			}
			if se.Code != types.ErrInternal {
				t.Fatalf("expected ErrInternal, got %s", se.Code)
			}

			// Verify state unchanged
			if p.State != tc.wantState {
				t.Fatalf("state should be %d, got %d", tc.wantState, p.State)
			}
		})
	}
}

func TestTerminateIllegal(t *testing.T) {
	t.Run("Terminate from Created", func(t *testing.T) {
		p := NewProcess(0, "test", nil)
		err := p.Terminate(ExitStatus{Code: 1, Reason: "error"})
		if err == nil {
			t.Fatal("expected error")
		}
		if p.State != types.StateCreated {
			t.Fatalf("state should remain Created, got %d", p.State)
		}
	})

	t.Run("Terminate from Zombie", func(t *testing.T) {
		p := NewProcess(0, "test", nil)
		_ = p.Start()
		_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})
		err := p.Terminate(ExitStatus{Code: 1, Reason: "again"})
		if err == nil {
			t.Fatal("expected error")
		}
		if p.State != types.StateZombie {
			t.Fatalf("state should remain Zombie, got %d", p.State)
		}
	})
}

func TestPIDUniqueness(t *testing.T) {
	const n = 100
	pids := make(chan types.PID, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := NewProcess(0, "test", nil)
			pids <- p.PID
		}()
	}

	wg.Wait()
	close(pids)

	seen := make(map[types.PID]bool)
	for pid := range pids {
		if pid == 0 {
			t.Fatal("PID should not be 0")
		}
		if seen[pid] {
			t.Fatalf("duplicate PID: %d", pid)
		}
		seen[pid] = true
	}

	if len(seen) != n {
		t.Fatalf("expected %d unique PIDs, got %d", n, len(seen))
	}
}

func TestPIDMonotonic(t *testing.T) {
	p1 := NewProcess(0, "test1", nil)
	p2 := NewProcess(0, "test2", nil)
	p3 := NewProcess(0, "test3", nil)

	if p2.PID <= p1.PID {
		t.Fatalf("PID should increase: p1=%d p2=%d", p1.PID, p2.PID)
	}
	if p3.PID <= p2.PID {
		t.Fatalf("PID should increase: p2=%d p3=%d", p2.PID, p3.PID)
	}
}

func TestGetState(t *testing.T) {
	p := NewProcess(0, "test", nil)
	if p.GetState() != types.StateCreated {
		t.Fatalf("expected StateCreated, got %d", p.GetState())
	}
	_ = p.Start()
	if p.GetState() != types.StateRunning {
		t.Fatalf("expected StateRunning, got %d", p.GetState())
	}
}

func TestConcurrentStartSameProcess(t *testing.T) {
	const n = 100
	p := NewProcess(0, "test", nil)
	var wg sync.WaitGroup
	var successCount atomic.Int32

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Start(); err == nil {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Start, got %d", successCount.Load())
	}
	if p.GetState() != types.StateRunning {
		t.Fatalf("expected StateRunning, got %d", p.GetState())
	}
}

func TestConcurrentTransitionsSameProcess(t *testing.T) {
	const n = 100
	p := NewProcess(0, "test", nil)
	_ = p.Start()

	var wg sync.WaitGroup
	var terminateOK atomic.Int32
	var reapOK atomic.Int32

	// n goroutines race to Terminate
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Terminate(ExitStatus{Code: 0, Reason: "done"}); err == nil {
				terminateOK.Add(1)
			}
		}()
	}
	wg.Wait()

	if terminateOK.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Terminate, got %d", terminateOK.Load())
	}
	if p.GetState() != types.StateZombie {
		t.Fatalf("expected StateZombie, got %d", p.GetState())
	}

	// n goroutines race to Reap
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Reap(); err == nil {
				reapOK.Add(1)
			}
		}()
	}
	wg.Wait()

	if reapOK.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Reap, got %d", reapOK.Load())
	}
	if p.GetState() != types.StateDead {
		t.Fatalf("expected StateDead, got %d", p.GetState())
	}
}
