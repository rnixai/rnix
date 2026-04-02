package kernel

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
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

	for range n {
		wg.Go(func() {
			p := NewProcess(0, "test", nil)
			pids <- p.PID
		})
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

func TestGetPID(t *testing.T) {
	p := NewProcess(0, "test-getpid", nil)
	if p.GetPID() != p.PID {
		t.Fatalf("GetPID() = %d, want %d", p.GetPID(), p.PID)
	}

	// Verify consistency across multiple calls
	pid1 := p.GetPID()
	pid2 := p.GetPID()
	if pid1 != pid2 {
		t.Fatalf("GetPID() not stable: %d vs %d", pid1, pid2)
	}
}

func TestConcurrentStartSameProcess(t *testing.T) {
	const n = 100
	p := NewProcess(0, "test", nil)
	var wg sync.WaitGroup
	var successCount atomic.Int32

	for range n {
		wg.Go(func() {
			if err := p.Start(); err == nil {
				successCount.Add(1)
			}
		})
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
	for range n {
		wg.Go(func() {
			if err := p.Terminate(ExitStatus{Code: 0, Reason: "done"}); err == nil {
				terminateOK.Add(1)
			}
		})
	}
	wg.Wait()

	if terminateOK.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Terminate, got %d", terminateOK.Load())
	}
	if p.GetState() != types.StateZombie {
		t.Fatalf("expected StateZombie, got %d", p.GetState())
	}

	// n goroutines race to Reap
	for range n {
		wg.Go(func() {
			if err := p.Reap(); err == nil {
				reapOK.Add(1)
			}
		})
	}
	wg.Wait()

	if reapOK.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Reap, got %d", reapOK.Load())
	}
	if p.GetState() != types.StateDead {
		t.Fatalf("expected StateDead, got %d", p.GetState())
	}
}

// --- Story 4.2: Children management tests ---

func TestAddChild(t *testing.T) {
	p := NewProcess(0, "parent", nil)
	p.AddChild(10)
	p.AddChild(20)

	children := p.GetChildren()
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	if !slices.Contains(children, types.PID(10)) {
		t.Error("expected child PID 10")
	}
	if !slices.Contains(children, types.PID(20)) {
		t.Error("expected child PID 20")
	}
}

func TestRemoveChild(t *testing.T) {
	p := NewProcess(0, "parent", nil)
	p.AddChild(10)
	p.AddChild(20)
	p.AddChild(30)

	p.RemoveChild(20)

	children := p.GetChildren()
	if len(children) != 2 {
		t.Fatalf("expected 2 children after remove, got %d", len(children))
	}
	if slices.Contains(children, types.PID(20)) {
		t.Error("child PID 20 should have been removed")
	}
}

func TestRemoveChild_NotPresent(t *testing.T) {
	p := NewProcess(0, "parent", nil)
	p.AddChild(10)

	// Removing non-existent child should not panic
	p.RemoveChild(999)

	children := p.GetChildren()
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestGetChildren_ReturnsCopy(t *testing.T) {
	p := NewProcess(0, "parent", nil)
	p.AddChild(10)

	children := p.GetChildren()
	children[0] = 999 // modify returned slice

	original := p.GetChildren()
	if original[0] != 10 {
		t.Error("GetChildren should return a copy, not a reference")
	}
}

func TestChildren_ConcurrentSafe(t *testing.T) {
	p := NewProcess(0, "parent", nil)
	var wg sync.WaitGroup
	const n = 100

	// Concurrent AddChild
	for i := range n {
		wg.Add(1)
		go func(pid types.PID) {
			defer wg.Done()
			p.AddChild(pid)
		}(types.PID(i + 1))
	}
	wg.Wait()

	children := p.GetChildren()
	if len(children) != n {
		t.Fatalf("expected %d children, got %d", n, len(children))
	}

	// Concurrent RemoveChild
	for i := range n {
		wg.Add(1)
		go func(pid types.PID) {
			defer wg.Done()
			p.RemoveChild(pid)
		}(types.PID(i + 1))
	}
	wg.Wait()

	children = p.GetChildren()
	if len(children) != 0 {
		t.Fatalf("expected 0 children after removal, got %d", len(children))
	}
}

// ============================================================
// Story 25-3: Project Config Merge & Module Adaptation
//
// Tests verify ProjectConfig field on Process struct.
// ============================================================

// --- 25.3-PROC-001: ProjectConfig field is set and accessible ---

func TestProcess_ProjectConfig_SetOnSpawn(t *testing.T) {
	p := NewProcess(0, "project test", []string{"code-analyst"})

	// Initially nil
	if p.ProjectConfig != nil {
		t.Fatal("ProjectConfig should be nil on new process")
	}

	// Set ProjectConfig (as kernel.Spawn does)
	projCfg := &config.ProjectConfig{
		ProjectDir: "/home/user/my-project",
		AgentDirs:  []string{"/home/user/my-project/.rnix/agents", "/home/user/.config/rnix/agents"},
		SkillDirs:  []string{"/home/user/my-project/.rnix/skills", "/home/user/.config/rnix/skills"},
	}
	p.ProjectConfig = projCfg

	// Verify it is accessible and matches
	if p.ProjectConfig == nil {
		t.Fatal("ProjectConfig should be non-nil after set")
	}
	if p.ProjectConfig.ProjectDir != "/home/user/my-project" {
		t.Errorf("ProjectConfig.ProjectDir = %q, want %q", p.ProjectConfig.ProjectDir, "/home/user/my-project")
	}
	if len(p.ProjectConfig.AgentDirs) != 2 {
		t.Fatalf("ProjectConfig.AgentDirs length = %d, want 2", len(p.ProjectConfig.AgentDirs))
	}
	if p.ProjectConfig.AgentDirs[0] != "/home/user/my-project/.rnix/agents" {
		t.Errorf("ProjectConfig.AgentDirs[0] = %q, want project dir first", p.ProjectConfig.AgentDirs[0])
	}
	if len(p.ProjectConfig.SkillDirs) != 2 {
		t.Fatalf("ProjectConfig.SkillDirs length = %d, want 2", len(p.ProjectConfig.SkillDirs))
	}

	// Verify immutability: ProjectConfig is a pointer, but changing the referenced
	// config should reflect since it's immutable after spawn (by convention)
	if p.ProjectConfig != projCfg {
		t.Error("ProjectConfig pointer should be the same as the one assigned")
	}
}

// ============================================================
// Story 30-3: Suspended State Machine Extension
// ============================================================

func TestSuspendedTransitions_Legal(t *testing.T) {
	t.Run("Running→Suspended", func(t *testing.T) {
		p := NewProcess(0, "test", nil)
		_ = p.Start()
		if err := p.Transition(types.StateSuspended); err != nil {
			t.Fatalf("Running→Suspended failed: %v", err)
		}
		if p.GetState() != types.StateSuspended {
			t.Fatalf("expected StateSuspended, got %d", p.GetState())
		}
	})

	t.Run("Suspended→Running", func(t *testing.T) {
		p := NewProcess(0, "test", nil)
		_ = p.Start()
		_ = p.Transition(types.StateSuspended)
		if err := p.Transition(types.StateRunning); err != nil {
			t.Fatalf("Suspended→Running failed: %v", err)
		}
		if p.GetState() != types.StateRunning {
			t.Fatalf("expected StateRunning, got %d", p.GetState())
		}
	})

	t.Run("Suspended→Dead", func(t *testing.T) {
		p := NewProcess(0, "test", nil)
		_ = p.Start()
		_ = p.Transition(types.StateSuspended)
		if err := p.Transition(types.StateDead); err != nil {
			t.Fatalf("Suspended→Dead failed: %v", err)
		}
		if p.GetState() != types.StateDead {
			t.Fatalf("expected StateDead, got %d", p.GetState())
		}
	})
}

func TestSuspendedTransitions_Illegal(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*Process)
		target    types.ProcessState
		wantState types.ProcessState
	}{
		{
			name:      "Created→Suspended",
			setup:     func(p *Process) {},
			target:    types.StateSuspended,
			wantState: types.StateCreated,
		},
		{
			name: "Zombie→Suspended",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Terminate(ExitStatus{Code: 0, Reason: "done"})
			},
			target:    types.StateSuspended,
			wantState: types.StateZombie,
		},
		{
			name: "Suspended→Zombie",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Transition(types.StateSuspended)
			},
			target:    types.StateZombie,
			wantState: types.StateSuspended,
		},
		{
			name: "Suspended→Created",
			setup: func(p *Process) {
				_ = p.Start()
				_ = p.Transition(types.StateSuspended)
			},
			target:    types.StateCreated,
			wantState: types.StateSuspended,
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
			if p.GetState() != tc.wantState {
				t.Fatalf("state should be %d, got %d", tc.wantState, p.GetState())
			}
		})
	}
}

func TestProcess_SuspendUnsuspend(t *testing.T) {
	p := NewProcess(0, "test", nil)
	_ = p.Start()

	if err := p.Suspend(); err != nil {
		t.Fatalf("Suspend failed: %v", err)
	}
	if p.GetState() != types.StateSuspended {
		t.Fatalf("expected StateSuspended, got %d", p.GetState())
	}

	if err := p.Unsuspend(); err != nil {
		t.Fatalf("Unsuspend failed: %v", err)
	}
	if p.GetState() != types.StateRunning {
		t.Fatalf("expected StateRunning, got %d", p.GetState())
	}
}

func TestProcess_IsSuspendRequested(t *testing.T) {
	p := NewProcess(0, "test", nil)
	if p.IsSuspendRequested() {
		t.Fatal("new process should not have suspend requested")
	}
	p.suspendRequested.Store(true)
	if !p.IsSuspendRequested() {
		t.Fatal("suspend should be requested after Store(true)")
	}
}
