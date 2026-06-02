package kernel

import (
	gocontext "context"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ATDD Tests for Story 3.6: Step Output Streaming — OnStepComplete 回调
//
// RED PHASE: The kernel does not yet call OnStepComplete (the method is not
// part of KernelCallbacks). Tests will timeout/fail until the interface is
// extended and call sites are added in reasonStep.
//
// Tests verify:
// AC1: tool_call 步骤完成后 OnStepComplete(pid, step, "tool_call", summary) 被调用
// AC2: plan 步骤完成后 OnStepComplete(pid, step, "plan", "plan (N steps)") 被调用
// AC3: spawn 步骤完成后 OnStepComplete(pid, step, "spawn", summary) 被调用

// stepCompleteRecord captures a single OnStepComplete invocation.
type stepCompleteRecord struct {
	PID     types.PID
	Step    int
	Action  string
	Summary string
}

// atdd36Callbacks captures OnStepComplete invocations for assertions.
// It satisfies KernelCallbacks (existing 4 methods) plus the new OnStepComplete
// that will be added as part of Story 3.6 implementation.
type atdd36Callbacks struct {
	mu            sync.Mutex
	stepCompletes []stepCompleteRecord
}

func (c *atdd36Callbacks) OnSpawn(_ types.PID, _, _, _, _ string)         {}
func (c *atdd36Callbacks) OnStep(_ types.PID, _ int, _ int)               {}
func (c *atdd36Callbacks) OnComplete(_ types.PID, _ string, _ ExitStatus) {}
func (c *atdd36Callbacks) OnError(_ types.PID, _ error)                   {}
func (c *atdd36Callbacks) OnAskUser(_ types.PID, _ string, _ []byte) ([]byte, error) {
	return nil, nil
}
func (c *atdd36Callbacks) OnStemDiff(_ types.PID, _ []StemMatchResult, _ []string, _ bool) {}

func (c *atdd36Callbacks) OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stepCompletes = append(c.stepCompletes, stepCompleteRecord{
		PID: pid, Step: step, Action: action, Summary: summary,
	})
}

func (c *atdd36Callbacks) getStepCompletes() []stepCompleteRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]stepCompleteRecord, len(c.stepCompletes))
	copy(cp, c.stepCompletes)
	return cp
}

func (c *atdd36Callbacks) waitForAction(action string, timeout time.Duration) *stepCompleteRecord {
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			return nil
		case <-ticker.C:
			for _, rec := range c.getStepCompletes() {
				if rec.Action == action {
					r := rec
					return &r
				}
			}
		}
	}
}

func (c *atdd36Callbacks) waitForAnyAction(timeout time.Duration) *stepCompleteRecord {
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			return nil
		case <-ticker.C:
			recs := c.getStepCompletes()
			if len(recs) > 0 {
				r := recs[0]
				return &r
			}
		}
	}
}

// ============================================================
// AC1: tool_call 步骤摘要 — OnStepComplete 被调用
// ============================================================

func TestATDD_3_6_AC1_OnStepComplete_ToolCall(t *testing.T) {
	toolCallData := map[string]any{
		"path":      "/test.txt",
		"operation": "read",
	}
	toolCallResp := makeToolCallResponse("/dev/fs", toolCallData, 10)
	completeResp := makeLLMResponse("done", 5)

	llm := &sequenceLLMFile{
		responses: [][]byte{toolCallResp, completeResp},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	registerMockTool(reg, "/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &atdd36ToolFile{readData: []byte("file content")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	cb := &atdd36Callbacks{}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "atdd36-toolcall"},
	}

	pid, err := k.Spawn("test tool_call step complete", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC1: Spawn failed: %v", err)
	}

	rec := cb.waitForAction("tool_call", 3*time.Second)
	if rec == nil {
		t.Fatal("AC1: timed out — OnStepComplete never called with action='tool_call'")
	}
	if rec.PID != pid {
		t.Errorf("AC1: expected PID %d, got %d", pid, rec.PID)
	}
	if rec.Step < 1 {
		t.Errorf("AC1: expected step >= 1, got %d", rec.Step)
	}
	if rec.Summary == "" {
		t.Error("AC1: expected non-empty summary for tool_call")
	}
}

// ============================================================
// AC2: plan 步骤摘要 — OnStepComplete(pid, step, "plan", "plan (N steps)")
// ============================================================

func TestATDD_3_6_AC2_OnStepComplete_Plan(t *testing.T) {
	planResp := makePlanResponse([]string{"step1", "step2", "step3"}, "test", 10)
	completeResp := makeLLMResponse("done", 5)

	llm := &sequenceLLMFile{
		responses: [][]byte{planResp, completeResp},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	cb := &atdd36Callbacks{}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	planEnabled := true
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:     "atdd36-plan",
			Planning: &planEnabled,
		},
	}

	pid, err := k.Spawn("test plan step complete", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC2: Spawn failed: %v", err)
	}

	rec := cb.waitForAction("plan", 3*time.Second)
	if rec == nil {
		t.Fatal("AC2: timed out — OnStepComplete never called with action='plan'")
	}
	if rec.PID != pid {
		t.Errorf("AC2: expected PID %d, got %d", pid, rec.PID)
	}
	if rec.Summary != "plan (3 steps)" {
		t.Errorf("AC2: expected summary 'plan (3 steps)', got %q", rec.Summary)
	}
}

// ============================================================
// AC3: spawn 步骤摘要 — OnStepComplete(pid, step, "spawn", "spawn PID ...")
// ============================================================

func TestATDD_3_6_AC3_OnStepComplete_Spawn(t *testing.T) {
	spawnResp := makeSpawnResponse("analyze code", "", 10)
	completeResp := makeLLMResponse("done", 5)

	llm := &sequenceLLMFile{
		responses: [][]byte{spawnResp, completeResp},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	cb := &atdd36Callbacks{}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "atdd36-spawn"},
	}

	pid, err := k.Spawn("test spawn step complete", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC3: Spawn failed: %v", err)
	}

	rec := cb.waitForAction("spawn", 3*time.Second)
	if rec == nil {
		t.Fatal("AC3: timed out — OnStepComplete never called with action='spawn'")
	}
	if rec.PID != pid {
		t.Errorf("AC3: expected PID %d, got %d", pid, rec.PID)
	}
	if rec.Summary == "" {
		t.Error("AC3: expected non-empty summary for spawn")
	}
}

// ============================================================
// AC1 补充: complete/text action 的空 summary
// ============================================================

func TestATDD_3_6_AC1_OnStepComplete_Complete_EmptySummary(t *testing.T) {
	completeResp := makeLLMResponse("final result", 10)
	llmFile := &mockLLMFile{readData: completeResp}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	cb := &atdd36Callbacks{}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "atdd36-complete"},
	}

	_, err := k.Spawn("test complete empty summary", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC1-complete: Spawn failed: %v", err)
	}

	rec := cb.waitForAnyAction(3 * time.Second)
	if rec == nil {
		t.Fatal("AC1-complete: timed out — OnStepComplete never called")
	}
	if rec.Summary == "" {
		t.Errorf("AC1-complete: expected non-empty summary for %s action with result content", rec.Action)
	}
}

// ============================================================
// AC1 补充: tool_call summary 截断规则（briefResult > 60 字符追加 "..."）
// ============================================================

func TestATDD_3_6_AC1_OnStepComplete_ToolCall_SummaryTruncation(t *testing.T) {
	toolCallData := map[string]any{
		"path":      "/long-output.txt",
		"operation": "read",
	}
	toolCallResp := makeToolCallResponse("/dev/fs", toolCallData, 10)
	completeResp := makeLLMResponse("done", 5)

	llm := &sequenceLLMFile{
		responses: [][]byte{toolCallResp, completeResp},
	}

	longResult := make([]byte, 200)
	for i := range longResult {
		longResult[i] = 'x'
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	registerMockTool(reg, "/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &atdd36ToolFile{readData: longResult}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	cb := &atdd36Callbacks{}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "atdd36-truncation"},
	}

	_, err := k.Spawn("test truncation", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC1-truncation: Spawn failed: %v", err)
	}

	rec := cb.waitForAction("tool_call", 3*time.Second)
	if rec == nil {
		t.Fatal("AC1-truncation: timed out — OnStepComplete never called")
	}

	// Total summary should not be excessively long (briefResult <= 63 chars)
	if len(rec.Summary) > 130 {
		t.Errorf("AC1-truncation: summary too long (%d chars), expected truncation", len(rec.Summary))
	}
}

// ============================================================
// Helper: VFS tool file mock for /dev/fs
// ============================================================

type atdd36ToolFile struct {
	mu       sync.Mutex
	readData []byte
}

func (f *atdd36ToolFile) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readData, nil
}

func (f *atdd36ToolFile) Write(_ gocontext.Context, _ []byte) error {
	return nil
}

func (f *atdd36ToolFile) Close() error { return nil }

func (f *atdd36ToolFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true}, nil
}
