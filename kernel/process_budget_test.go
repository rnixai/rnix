package kernel

import (
	"testing"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// Story 30.7: 资源预算与预算暂停
// ============================================================

// --- 30.7-UNIT-001: ProcessBudget 初始化默认值 ---

func TestProcessBudget_DefaultsToZero(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	if proc.Budget.MaxTokens != 0 {
		t.Errorf("expected MaxTokens=0, got %d", proc.Budget.MaxTokens)
	}
	if proc.Budget.MaxCost != 0 {
		t.Errorf("expected MaxCost=0, got %f", proc.Budget.MaxCost)
	}
	if proc.Budget.UsedCost != 0 {
		t.Errorf("expected UsedCost=0, got %f", proc.Budget.UsedCost)
	}
}

// --- 30.7-UNIT-002: Budget 优先级解析 opts > manifest > 默认 ---

func TestProcessBudget_PriorityResolution(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{makeLLMResponse("done", 100)},
		}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	t.Run("opts override manifest", func(t *testing.T) {
		agent := &agents.AgentInfo{
			Manifest: agents.AgentManifest{
				MaxTokens: 5000,
				MaxCost:   1.0,
			},
		}
		pid, err := k.Spawn("test", agent, SpawnOpts{MaxTokens: 10000, MaxCost: 2.0})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, _ := k.GetProcess(pid)
		proc.mu.Lock()
		if proc.Budget.MaxTokens != 10000 {
			t.Errorf("expected MaxTokens=10000 (from opts), got %d", proc.Budget.MaxTokens)
		}
		if proc.Budget.MaxCost != 2.0 {
			t.Errorf("expected MaxCost=2.0 (from opts), got %f", proc.Budget.MaxCost)
		}
		proc.mu.Unlock()
	})

	t.Run("manifest when opts is zero", func(t *testing.T) {
		agent := &agents.AgentInfo{
			Manifest: agents.AgentManifest{
				MaxTokens: 5000,
				MaxCost:   1.0,
			},
		}
		pid, err := k.Spawn("test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, _ := k.GetProcess(pid)
		proc.mu.Lock()
		if proc.Budget.MaxTokens != 5000 {
			t.Errorf("expected MaxTokens=5000 (from manifest), got %d", proc.Budget.MaxTokens)
		}
		if proc.Budget.MaxCost != 1.0 {
			t.Errorf("expected MaxCost=1.0 (from manifest), got %f", proc.Budget.MaxCost)
		}
		proc.mu.Unlock()
	})

	t.Run("defaults to zero when neither set", func(t *testing.T) {
		pid, err := k.Spawn("test", nil, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, _ := k.GetProcess(pid)
		proc.mu.Lock()
		if proc.Budget.MaxTokens != 0 {
			t.Errorf("expected MaxTokens=0, got %d", proc.Budget.MaxTokens)
		}
		if proc.Budget.MaxCost != 0 {
			t.Errorf("expected MaxCost=0, got %f", proc.Budget.MaxCost)
		}
		proc.mu.Unlock()
	})
}

// --- 30.7-UNIT-003: MaxTokens 耗尽 → selfSuspend ---

func TestProcessBudget_MaxTokensExhausted_Suspends(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 2000),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 2000),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 2000),
				makeLLMResponse("should not reach", 1000),
			},
		}, nil
	})
	_ = reg.Register("/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	// MaxTokens=5000, each response uses 2000. After 3rd call (6000 >= 5000), should suspend.
	pid, err := k.Spawn("budget suspend test", nil, SpawnOpts{MaxTokens: 5000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	// selfSuspend sets exit code 2 with reason "suspended: budget_exhausted"
	if exit.Code != 2 {
		t.Errorf("expected exit code 2 (suspended), got %d", exit.Code)
	}
	if exit.Reason != "suspended: budget_exhausted" {
		t.Errorf("expected reason 'suspended: budget_exhausted', got %q", exit.Reason)
	}
}

// --- 30.7-UNIT-004: MaxCost 耗尽 → selfSuspend ---

func TestProcessBudget_MaxCostExhausted_Suspends(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 10000),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 10000),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 10000),
				makeLLMResponse("should not reach", 1000),
			},
		}, nil
	})
	_ = reg.Register("/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	// costPerToken = 0.00001, each response 10000 tokens → $0.10 per step
	// MaxCost=$0.25 → after 3rd step ($0.30 >= $0.25) → suspend
	k.SetCostPerToken(func(_ string) float64 { return 0.00001 })

	pid, err := k.Spawn("cost suspend test", nil, SpawnOpts{MaxCost: 0.25})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if exit.Code != 2 {
		t.Errorf("expected exit code 2 (suspended), got %d", exit.Code)
	}
	if exit.Reason != "suspended: budget_exhausted" {
		t.Errorf("expected reason 'suspended: budget_exhausted', got %q", exit.Reason)
	}
}

// --- 30.7-UNIT-005: MaxTokens=0 和 MaxCost=0 不触发预算检查 ---

func TestProcessBudget_ZeroBudget_NoCheck(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeLLMResponse("done", 50000),
			},
		}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	// No budget set — even with 50000 tokens, should complete normally
	pid, err := k.Spawn("no budget", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if exit.Code != 0 {
		t.Errorf("expected exit code 0, got %d (reason: %s)", exit.Code, exit.Reason)
	}
}

// --- 30.7-UNIT-007: Checkpoint round-trip 测试 ---

func TestCheckpoint_BudgetFields_Preserved(t *testing.T) {
	proc := NewProcess(0, "checkpoint test", nil)
	proc.mu.Lock()
	proc.Budget = ProcessBudget{
		MaxTokens: 50000,
		MaxCost:   2.5,
		UsedCost:  0.75,
	}
	proc.TokensUsed = 12345
	proc.mu.Unlock()

	cpData := buildCheckpointData(proc, 5, []byte("{}"), 0)

	if cpData.ProcState.MaxTokens != 50000 {
		t.Errorf("checkpoint MaxTokens: expected 50000, got %d", cpData.ProcState.MaxTokens)
	}
	if cpData.ProcState.MaxCost != 2.5 {
		t.Errorf("checkpoint MaxCost: expected 2.5, got %f", cpData.ProcState.MaxCost)
	}
	if cpData.ProcState.UsedCost != 0.75 {
		t.Errorf("checkpoint UsedCost: expected 0.75, got %f", cpData.ProcState.UsedCost)
	}

	// Write + read round-trip
	dir := t.TempDir()
	if err := writeCheckpoint(dir, cpData); err != nil {
		t.Fatalf("writeCheckpoint failed: %v", err)
	}
	loaded, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("readCheckpoint failed: %v", err)
	}
	if loaded.ProcState.MaxTokens != 50000 {
		t.Errorf("loaded MaxTokens: expected 50000, got %d", loaded.ProcState.MaxTokens)
	}
	if loaded.ProcState.MaxCost != 2.5 {
		t.Errorf("loaded MaxCost: expected 2.5, got %f", loaded.ProcState.MaxCost)
	}
	if loaded.ProcState.UsedCost != 0.75 {
		t.Errorf("loaded UsedCost: expected 0.75, got %f", loaded.ProcState.UsedCost)
	}
}

// --- 30.7-UNIT-008: getCostPerToken 降级 ---

func TestGetCostPerToken_NilCallback_ReturnsZero(t *testing.T) {
	k := &KernelImpl{}
	if cost := k.getCostPerToken("claude"); cost != 0 {
		t.Errorf("expected 0 when costPerToken is nil, got %f", cost)
	}
}

func TestGetCostPerToken_WithCallback(t *testing.T) {
	k := &KernelImpl{}
	k.SetCostPerToken(func(provider string) float64 {
		if provider == "claude" {
			return 0.000003
		}
		return 0
	})
	if cost := k.getCostPerToken("claude"); cost != 0.000003 {
		t.Errorf("expected 0.000003, got %f", cost)
	}
	if cost := k.getCostPerToken("unknown"); cost != 0 {
		t.Errorf("expected 0 for unknown provider, got %f", cost)
	}
}

// --- 30.7-UNIT-009: ListProcs/GetProcInfo 填充预算字段 ---

func TestProcQuery_BudgetFieldsFilled(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{makeLLMResponse("done", 100)},
		}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("query test", nil, SpawnOpts{MaxTokens: 9999, MaxCost: 3.14})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Wait for process to complete
	_, _ = k.Wait(pid)

	// Test GetProcInfo
	info, err := k.GetProcInfo(pid)
	if err != nil {
		t.Fatalf("GetProcInfo failed: %v", err)
	}
	if info.MaxTokens != 9999 {
		t.Errorf("GetProcInfo MaxTokens: expected 9999, got %d", info.MaxTokens)
	}
	if info.MaxCost != 3.14 {
		t.Errorf("GetProcInfo MaxCost: expected 3.14, got %f", info.MaxCost)
	}
}
