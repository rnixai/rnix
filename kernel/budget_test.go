package kernel

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD RED PHASE — Story 10.3: Token 预算管理
//
// All tests reference ContextBudget fields that do NOT exist yet.
// They will fail to COMPILE until implementation adds:
//   - SpawnOpts.ContextBudget int
//   - Process.ContextBudget int
//   - Budget check logic in reasonStep
//
// RED → GREEN: implement the fields and logic, tests compile and pass.
// ============================================================

// --- 10.3-UNIT-001: [P0] Budget enforcement terminates process at limit ---

func TestBudgetEnforcement_TerminatesAtBudget(t *testing.T) {
	// LLM returns 1000 tokens per call; budget=2500 → 3rd call (3000 >= 2500) triggers termination
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 1000),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 1000),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 1000),
				makeLLMResponse("should not reach here", 1000),
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

	pid, err := k.Spawn("budget test", nil, SpawnOpts{ContextBudget: 2500})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 2 {
			t.Fatalf("expected exit code 2 (budget_exceeded), got %d: %s", exit.Code, exit.Reason)
		}
		if exit.Reason != "budget_exceeded" {
			t.Fatalf("expected reason 'budget_exceeded', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for budget termination")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie state, got %d", proc.GetState())
	}
}

// --- 10.3-UNIT-002: [P0] No budget (0) runs without limit ---

func TestBudgetEnforcement_ZeroBudgetNoLimit(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("completed normally", 5000),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("no budget", nil, SpawnOpts{ContextBudget: 0})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0 (normal completion), got %d: %s", exit.Code, exit.Reason)
		}
		if exit.Reason != "completed" {
			t.Fatalf("expected reason 'completed', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.TokensUsed != 5000 {
		t.Fatalf("expected 5000 tokens, got %d", proc.TokensUsed)
	}
}

// --- 10.3-UNIT-003: [P0] SpawnOpts.ContextBudget overrides agent manifest ---

func TestBudgetPriority_OptsOverridesAgent(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:          "budget-agent",
			ContextBudget: 1000,
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
			},
		},
		Instructions: "test instructions",
	}

	pid, err := k.Spawn("override test", agent, SpawnOpts{ContextBudget: 3000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 3000 {
		t.Fatalf("expected ContextBudget 3000 (from SpawnOpts), got %d", proc.ContextBudget)
	}
}

// --- 10.3-UNIT-004: [P0] Agent manifest budget used when opts is 0 ---

func TestBudgetPriority_AgentManifestWhenOptsZero(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:          "budget-agent",
			ContextBudget: 4096,
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
			},
		},
		Instructions: "test instructions",
	}

	pid, err := k.Spawn("agent budget", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 4096 {
		t.Fatalf("expected ContextBudget 4096 (from agent manifest), got %d", proc.ContextBudget)
	}
}

// --- 10.3-UNIT-005: [P0] ExitStatus Code=2 for budget exceeded ---

func TestBudgetExceeded_ExitCode2(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("big response", 5000)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("exceed on first call", nil, SpawnOpts{ContextBudget: 3000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 2 {
			t.Fatalf("expected Code 2, got %d", exit.Code)
		}
		if exit.Reason != "budget_exceeded" {
			t.Fatalf("expected Reason 'budget_exceeded', got %q", exit.Reason)
		}
		if exit.Err == nil {
			t.Fatal("expected non-nil Err in ExitStatus")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- 10.3-UNIT-006: [P1] emitLog called with budget exceeded message ---

func TestBudgetExceeded_EmitsLog(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("response", 3000)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("log test", nil, SpawnOpts{ContextBudget: 2000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	var foundBudgetLog bool
	for {
		select {
		case entry := <-proc.LogChan:
			if entry.Category == types.LogOutput && strings.Contains(entry.Content, "budget") {
				foundBudgetLog = true
			}
		default:
			goto drained
		}
	}
drained:
	if !foundBudgetLog {
		t.Error("expected [output] log entry about budget exceeded")
	}
}

// --- 10.3-UNIT-007: [P1] emitEvent called with budget_exceeded action ---

func TestBudgetExceeded_EmitsEvent(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("response", 3000)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("event test", nil, SpawnOpts{ContextBudget: 2000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	events := drainEvents(proc.DebugChan)
	var foundBudgetEvent bool
	for _, ev := range events {
		if ev.Syscall == "ReasonStep" {
			if action, ok := ev.Args["action"]; ok && action == "budget_exceeded" {
				foundBudgetEvent = true
				if ev.Args["tokens"] == nil {
					t.Error("budget_exceeded event should include 'tokens' arg")
				}
				if ev.Args["budget"] == nil {
					t.Error("budget_exceeded event should include 'budget' arg")
				}
			}
		}
	}
	if !foundBudgetEvent {
		t.Error("expected ReasonStep event with action=budget_exceeded")
	}
}

// --- 10.3-UNIT-008: [P1] Exact budget boundary (tokens == budget) triggers termination ---

func TestBudgetEnforcement_ExactBoundary(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("exact hit", 500)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("exact budget", nil, SpawnOpts{ContextBudget: 500})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 2 {
			t.Fatalf("expected Code 2 for tokens == budget, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- 10.3-UNIT-009: [P2] Negative budget treated as 0 (no limit) ---

func TestBudgetEnforcement_NegativeTreatedAsZero(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("negative budget", 100),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("negative budget", nil, SpawnOpts{ContextBudget: -1})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected normal exit for negative budget, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- 10.3-UNIT-010: [P0] GetProcInfo includes ContextBudget ---

func TestGetProcInfo_IncludesContextBudget(t *testing.T) {
	k := newSimpleKernel(t)

	proc := NewProcess(0, "budget info test", nil)
	proc.ContextBudget = 5000
	proc.TokensUsed = 2500
	_ = proc.Start()
	k.AddProcess(proc)

	info, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo failed: %v", err)
	}
	if info.ContextBudget != 5000 {
		t.Errorf("expected ContextBudget 5000, got %d", info.ContextBudget)
	}
}

// --- 10.3-UNIT-011: [P0] Budget check uses >= not > (TOCTOU safe under lock) ---

func TestBudgetEnforcement_MultiStep_CumulativeCheck(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 800),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 800),
				makeToolCallResponse("/dev/tools/echo", map[string]any{}, 800),
				makeLLMResponse("should not reach", 800),
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

	pid, err := k.Spawn("cumulative", nil, SpawnOpts{ContextBudget: 2000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 2 {
			t.Fatalf("expected budget termination (code 2), got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// After 3 steps of 800 = 2400 tokens, should exceed budget of 2000
	// Budget check happens after token accumulation in each step
	if proc.TokensUsed < 2000 {
		t.Errorf("expected tokens >= 2000, got %d", proc.TokensUsed)
	}
}

// --- 10.3-UNIT-012: [P0] Without agent, SpawnOpts.ContextBudget=0 means no limit ---

func TestBudgetEnforcement_DefaultNoLimit(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("default behavior", 100),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("no budget default", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected normal exit with default opts, got code %d", exit.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 0 {
		t.Errorf("expected ContextBudget 0 for default opts, got %d", proc.ContextBudget)
	}
}

// --- 10.3-UNIT-013: [P0] Budget check prevents action execution after limit ---

func TestBudgetEnforcement_PreventsActionAfterExceeded(t *testing.T) {
	actionExecuted := false
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeToolCallResponse("/dev/tools/track", map[string]any{}, 5000),
				makeLLMResponse("should not run", 100),
			},
		}, nil
	})
	_ = reg.Register("/dev/tools/track", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		actionExecuted = true
		return &mockToolFile{readData: []byte("tracked")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("prevent action", nil, SpawnOpts{ContextBudget: 3000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 2 {
			t.Fatalf("expected code 2, got %d", exit.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if actionExecuted {
		t.Error("tool action should NOT execute after budget exceeded; budget check must precede parseAction")
	}
}

// --- 10.3-INT-001: [P0] Existing tests pass with budget=0 (backward compatibility) ---

func TestBudget_BackwardCompatibility(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("backward compat", 77),
	}
	k, _, ctxMgr := newTestKernel(t, llmFile)

	pid, err := k.Spawn("compat test", nil, SpawnOpts{
		SystemPrompt: "you are a test",
		Model:        "claude-test",
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "backward compat" {
		t.Fatalf("expected result 'backward compat', got %q", proc.Result)
	}
	if proc.TokensUsed != 77 {
		t.Fatalf("expected 77 tokens, got %d", proc.TokensUsed)
	}

	_, err = ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("context should exist: %v", err)
	}
}

// --- 10.3-UNIT-014: [P0] testAgentInfo() includes ContextBudget for existing tests ---

func TestAgentContextBudget_ExistingHelper(t *testing.T) {
	agent := testAgentInfo()
	if agent.Manifest.ContextBudget != 4096 {
		t.Errorf("testAgentInfo ContextBudget: got %d, want 4096", agent.Manifest.ContextBudget)
	}
}

// --- 10.3-INT-002: [P1] Spawn with agent uses manifest budget when opts has no budget ---

func TestSpawn_WithAgent_UsesBudgetFromManifest(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	agent := testAgentInfo()
	pid, err := k.Spawn("manifest budget", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 4096 {
		t.Errorf("expected ContextBudget 4096 from agent manifest, got %d", proc.ContextBudget)
	}
}

// --- 15.5-INT: Budget Warning integration tests ---

func TestBudgetWarning_EmitsWarningLevel(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("done", 850)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("warning test", nil, SpawnOpts{ContextBudget: 1000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	var foundWarningLog bool
	for {
		select {
		case entry := <-proc.LogChan:
			if entry.Category == types.LogWarning && strings.Contains(entry.Content, "Budget warning") {
				foundWarningLog = true
			}
		default:
			goto drainedWarn
		}
	}
drainedWarn:
	if !foundWarningLog {
		t.Error("expected LogWarning entry with 'Budget warning'")
	}

	events := drainEvents(proc.DebugChan)
	var foundWarningEvent bool
	for _, ev := range events {
		if ev.Syscall == "ReasonStep" {
			if action, ok := ev.Args["action"]; ok && action == "budget_warning" {
				if level, ok := ev.Args["alert_level"]; ok && level == "warning" {
					foundWarningEvent = true
				}
			}
		}
	}
	if !foundWarningEvent {
		t.Error("expected ReasonStep event with action=budget_warning and alert_level=warning")
	}
}

func TestBudgetWarning_EmitsCriticalLevel(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("done", 920)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("critical test", nil, SpawnOpts{ContextBudget: 1000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	events := drainEvents(proc.DebugChan)
	var foundCritical bool
	for _, ev := range events {
		if ev.Syscall == "ReasonStep" {
			if action, ok := ev.Args["action"]; ok && action == "budget_warning" {
				if level, ok := ev.Args["alert_level"]; ok && level == "critical" {
					foundCritical = true
				}
			}
		}
	}
	if !foundCritical {
		t.Error("expected ReasonStep event with action=budget_warning and alert_level=critical")
	}
}

func TestCheckBudgetWarning_NoEmitAboveThreshold(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)

	proc := NewProcess(0, "threshold test", nil)
	proc.ContextBudget = 1000
	k.AddProcess(proc)

	k.checkBudgetWarning(proc, 1, 700, 1000)

	select {
	case entry := <-proc.LogChan:
		t.Errorf("no log expected for 30%% remaining, got: %+v", entry)
	default:
	}
}

func TestCheckBudgetWarning_EstimatedStepsLeft(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)

	proc := NewProcess(0, "steps test", nil)
	proc.ContextBudget = 1000
	k.AddProcess(proc)

	k.checkBudgetWarning(proc, 5, 850, 1000)

	var foundLog bool
	for {
		select {
		case entry := <-proc.LogChan:
			if entry.Category == types.LogWarning && strings.Contains(entry.Content, "~0 steps left") {
				foundLog = true
			}
		default:
			goto drainedSteps
		}
	}
drainedSteps:
	if !foundLog {
		t.Error("expected warning log with estimated steps left")
	}
}
