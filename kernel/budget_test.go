package kernel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD — Story 10.3: Token 预算管理 (updated for C2 semantics)
//
// context_budget = per-step prompt input token ceiling (context window guard).
// Exceed → selfSuspend with ExitContextFull (code 3).
// ============================================================

func makeLLMResponseWithInput(content string, totalTokens, inputTokens int) []byte {
	resp := llmResponse{Content: content, TokensUsed: totalTokens, InputTokens: inputTokens}
	data, _ := json.Marshal(resp)
	return data
}

func makeToolCallResponseWithInput(toolName string, toolInput map[string]any, totalTokens, inputTokens int) []byte {
	resp := llmResponse{
		TokensUsed:  totalTokens,
		InputTokens: inputTokens,
		ToolCalls: []llmToolCall{{
			ID:    "call_" + toolName,
			Name:  toolName,
			Input: toolInput,
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

// --- 10.3-UNIT-001: [P0] Context budget suspends process when input exceeds limit ---

func TestBudgetEnforcement_TerminatesAtBudget(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponseWithInput("big prompt", 3500, 3000)}, nil
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
		if exit.Code != ExitContextFull {
			t.Fatalf("expected exit code %d (ExitContextFull), got %d: %s", ExitContextFull, exit.Code, exit.Reason)
		}
		if !strings.Contains(exit.Reason, "context_full") {
			t.Fatalf("expected reason containing 'context_full', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for budget suspension")
	}

	if proc.GetState() != types.StateSuspended {
		t.Fatalf("expected Suspended state, got %s", proc.GetState())
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

// --- 10.3-UNIT-005: [P0] ExitStatus Code=ExitContextFull for context budget exceeded ---

func TestBudgetExceeded_ExitCode2(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponseWithInput("big response", 5500, 5000)}, nil
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
		if exit.Code != ExitContextFull {
			t.Fatalf("expected Code %d, got %d", ExitContextFull, exit.Code)
		}
		if !strings.Contains(exit.Reason, "context_full") {
			t.Fatalf("expected Reason containing 'context_full', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- 10.3-UNIT-006: [P1] emitLog called with context budget exceeded message ---

func TestBudgetExceeded_EmitsLog(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponseWithInput("response", 3500, 3000)}, nil
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
			if entry.Category == types.LogOutput && strings.Contains(entry.Content, "Context budget exceeded") {
				foundBudgetLog = true
			}
		default:
			goto drained
		}
	}
drained:
	if !foundBudgetLog {
		t.Error("expected [output] log entry about context budget exceeded")
	}
}

// --- 10.3-UNIT-007: [P1] emitEvent called with context_full action ---

func TestBudgetExceeded_EmitsEvent(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponseWithInput("response", 3500, 3000)}, nil
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
			if action, ok := ev.Args["action"]; ok && action == "context_full" {
				foundBudgetEvent = true
				if ev.Args["input_tokens"] == nil {
					t.Error("context_full event should include 'input_tokens' arg")
				}
				if ev.Args["budget"] == nil {
					t.Error("context_full event should include 'budget' arg")
				}
			}
		}
	}
	if !foundBudgetEvent {
		t.Error("expected ReasonStep event with action=context_full")
	}
}

// --- 10.3-UNIT-008: [P1] Exact budget boundary (inputTokens == budget) triggers suspension ---

func TestBudgetEnforcement_ExactBoundary(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponseWithInput("exact hit", 600, 500)}, nil
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
		if exit.Code != ExitContextFull {
			t.Fatalf("expected Code %d for inputTokens == budget, got %d: %s", ExitContextFull, exit.Code, exit.Reason)
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

// --- 10.3-UNIT-011: [P0] Per-step check — cumulative exceeding budget does NOT trigger if per-step is under ---

func TestBudgetEnforcement_MultiStep_CumulativeCheck(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeToolCallResponseWithInput("/dev/tools/echo", map[string]any{}, 800, 500),
				makeToolCallResponseWithInput("/dev/tools/echo", map[string]any{}, 800, 600),
				makeLLMResponseWithInput("done", 800, 700),
			},
		}, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
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
		if exit.Code != 0 {
			t.Fatalf("expected normal completion (per-step InputTokens < budget), got code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.TokensUsed < 2000 {
		t.Errorf("expected cumulative tokens >= 2000, got %d", proc.TokensUsed)
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

	// With auto-derive, ContextBudget may be non-zero if ContextWindow is set.
	// In test kernel with no contextWindowFunc, ContextWindow = 0, so no auto-derive.
}

// --- 10.3-UNIT-013: [P0] Budget check prevents tool execution when context exceeded ---

func TestBudgetEnforcement_PreventsActionAfterExceeded(t *testing.T) {
	actionExecuted := false
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeToolCallResponseWithInput("/dev/tools/track", map[string]any{}, 5500, 5000),
				makeLLMResponse("should not run", 100),
			},
		}, nil
	})
	registerMockTool(reg, "/dev/tools/track", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
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
		if exit.Code != ExitContextFull {
			t.Fatalf("expected code %d, got %d", ExitContextFull, exit.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if actionExecuted {
		t.Error("tool action should NOT execute after context budget exceeded; budget check must precede tool execution")
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

// --- 15.5-INT: Budget Warning integration tests (per-step semantics) ---

func TestBudgetWarning_EmitsWarningLevel(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponseWithInput("done", 900, 850)}, nil
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
			if entry.Category == types.LogWarning && strings.Contains(entry.Content, "Context warning") {
				foundWarningLog = true
			}
		default:
			goto drainedWarn
		}
	}
drainedWarn:
	if !foundWarningLog {
		t.Error("expected LogWarning entry with 'Context warning'")
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
		return &mockLLMFile{readData: makeLLMResponseWithInput("done", 950, 920)}, nil
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
		t.Errorf("no log expected for 70%% usage, got: %+v", entry)
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
			if entry.Category == types.LogWarning && strings.Contains(entry.Content, "85%") {
				foundLog = true
			}
		default:
			goto drainedSteps
		}
	}
drainedSteps:
	if !foundLog {
		t.Error("expected warning log with 85% usage")
	}
}

// --- C2-UNIT-001: Auto-derive ContextBudget from ContextWindow ---

func TestContextBudget_AutoDerive(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return 1_000_000 })

	pid, err := k.Spawn("auto derive", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 900_000 {
		t.Errorf("expected auto-derived ContextBudget 900000, got %d", proc.ContextBudget)
	}
}

// --- C2-UNIT-002: Clamp ContextBudget to ContextWindow ---

func TestContextBudget_ClampToWindow(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return 100_000 })

	pid, err := k.Spawn("clamp test", nil, SpawnOpts{ContextBudget: 200_000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 100_000 {
		t.Errorf("expected clamped ContextBudget 100000, got %d", proc.ContextBudget)
	}
}

// --- C2-UNIT-003: Explicit ContextBudget < ContextWindow preserved ---

func TestContextBudget_ExplicitPreserved(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return 1_000_000 })

	pid, err := k.Spawn("explicit budget", nil, SpawnOpts{ContextBudget: 50_000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 50_000 {
		t.Errorf("expected explicit ContextBudget 50000, got %d", proc.ContextBudget)
	}
}
