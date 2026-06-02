package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// --- response builders (native ToolCalls format) ---

func makePlanResponse(steps []string, reason string, tokens int) []byte {
	resp := llmResponse{
		TokensUsed: tokens,
		ToolCalls: []llmToolCall{{
			ID:    "call_plan",
			Name:  "EnterPlanMode",
			Input: map[string]any{"steps": steps, "reason": reason},
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

func makeCompleteResponse(result string, tokens int) []byte {
	resp := llmResponse{
		TokensUsed: tokens,
		ToolCalls: []llmToolCall{{
			ID:    "call_complete",
			Name:  "complete",
			Input: map[string]any{"result": result},
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

func makeSpawnResponse(intent string, agent string, tokens int) []byte {
	input := map[string]any{"intent": intent}
	if agent != "" {
		input["agent"] = agent
	}
	resp := llmResponse{
		TokensUsed: tokens,
		ToolCalls: []llmToolCall{{
			ID:    "call_spawn",
			Name:  "Agent",
			Input: input,
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

// --- Scenario 1: tool_call executes and injects result ---

func TestUnified_ToolCall_ExecutesAndInjectsResult(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/echo", map[string]any{"msg": "hello"}, 50),
			makeCompleteResponse("done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echo-result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test tool call", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	if proc.Result != "done" {
		t.Fatalf("expected Result 'done', got %q", proc.Result)
	}

	prompt, buildErr := ctxMgr.BuildPrompt(proc.CtxID)
	if buildErr != nil {
		t.Fatalf("BuildPrompt: %v", buildErr)
	}
	var found bool
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool && strings.Contains(m.Content, "echo-result") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("tool result not injected into context")
	}
}

// --- Scenario 2: plan with planning=true writes RoleAssistant ---

func TestUnified_Plan_PlanningEnabled_WritesAssistant(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makePlanResponse([]string{"step1", "step2"}, "planning", 50),
			makeCompleteResponse("planned-done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test plan", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	prompt, buildErr := ctxMgr.BuildPrompt(proc.CtxID)
	if buildErr != nil {
		t.Fatalf("BuildPrompt: %v", buildErr)
	}
	var planFound bool
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleAssistant && strings.Contains(m.Content, "[Plan]") {
			planFound = true
			break
		}
	}
	if !planFound {
		t.Fatal("plan not written as RoleAssistant in context")
	}

	if proc.Result != "planned-done" {
		t.Fatalf("expected Result 'planned-done', got %q", proc.Result)
	}
}

// --- Scenario 3: plan with planning=false treats as text ---

func TestUnified_Plan_PlanningDisabled_TreatsAsText(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makePlanResponse([]string{"s1"}, "reason", 50),
		},
	}
	gate := make(chan struct{})
	gated := &gatedLLMFile{inner: seqFile, gate: gate}

	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return gated, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test plan disabled", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	proc.mu.Lock()
	proc.PlanningEnabled = false
	proc.FeatureFlags.Planning = false
	proc.mu.Unlock()
	close(gate)

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// Plan content should become the result since planning is disabled
	if proc.Result == "" {
		t.Fatal("expected non-empty Result when plan treated as text")
	}
}

// --- Scenario 4: complete exits with code 0 ---

func TestUnified_Complete_ExitsWithCodeZero(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeCompleteResponse("task finished", 42),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("complete test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
		if exit.Reason != "completed" {
			t.Fatalf("expected reason 'completed', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	if proc.Result != "task finished" {
		t.Fatalf("expected Result 'task finished', got %q", proc.Result)
	}
}

// --- Scenario 5: spawn creates child and waits for result ---

func TestUnified_Spawn_CreatesChildAndWaitsResult(t *testing.T) {
	reg := vfs.NewDeviceRegistry()

	var callCount atomic.Int32
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		n := callCount.Add(1)
		if n == 1 {
			// Parent: spawn → complete
			return &sequenceLLMFile{
				responses: [][]byte{
					makeSpawnResponse("child task", "", 50),
					makeCompleteResponse("parent done", 30),
				},
			}, nil
		}
		// Child: immediate complete
		return &mockLLMFile{
			readData: makeCompleteResponse("child result", 20),
		}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("parent task", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}

	prompt, buildErr := ctxMgr.BuildPrompt(proc.CtxID)
	if buildErr != nil {
		t.Fatalf("BuildPrompt: %v", buildErr)
	}
	var spawnResultFound bool
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool && strings.Contains(m.Content, "child result") {
			spawnResultFound = true
			break
		}
	}
	if !spawnResultFound {
		t.Fatal("child result not injected into parent context")
	}
}

// --- Scenario 7: circuit breaker with 3 consecutive errors ---

func TestUnified_CircuitBreaker_ThreeConsecutiveErrors(t *testing.T) {
	// spec-tool-error-handling-fidelity: circuit_breaker now uses (errCode, toolPath)
	// fingerprint deduplication. 3 consecutive same-fingerprint errors (here:
	// PERMISSION | /dev/nonexistent) trip the breaker.
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10),
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10),
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("circuit breaker test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1, got %d", exit.Code)
		}
		if !strings.Contains(exit.Reason, "circuit_breaker") {
			t.Fatalf("expected reason containing 'circuit_breaker', got %q", exit.Reason)
		}
		if !strings.Contains(exit.Reason, "/dev/nonexistent") {
			t.Fatalf("expected fingerprint in reason, got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

// --- Scenario: circuit breaker resets on success ---

func TestUnified_CircuitBreaker_ResetsOnSuccess(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10), // fail 1
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10), // fail 2
			makeToolCallResponse("/dev/tools/ok", map[string]any{"x": 1}, 10), // success → reset
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10), // fail 1 again
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10), // fail 2 again
			makeCompleteResponse("survived", 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/ok", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("reset test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	if proc.Result != "survived" {
		t.Fatalf("expected Result 'survived', got %q", proc.Result)
	}
}

// --- Scenario: spawn failure counts toward circuit breaker ---

func TestUnified_CircuitBreaker_SpawnFailureCounts(t *testing.T) {
	// spec-tool-error-handling-fidelity: spawn failures share the synthetic toolPath="spawn"
	// fingerprint, so 3 consecutive spawn failures trip the breaker. Mixed tool+spawn
	// errors would have different fingerprints (intentionally) and would NOT trip.
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpawnResponse("child", "nonexistent-agent", 10), // spawn fail 1 (no agent loader)
			makeSpawnResponse("child", "nonexistent-agent", 10), // spawn fail 2
			makeSpawnResponse("child", "nonexistent-agent", 10), // spawn fail 3 → trip
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("spawn breaker test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1, got %d", exit.Code)
		}
		if !strings.Contains(exit.Reason, "circuit_breaker") {
			t.Fatalf("expected 'circuit_breaker' in reason, got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

// --- Scenario: specialize error does NOT count toward circuit breaker ---

func TestUnified_CircuitBreaker_SpecializeErrorIgnored(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10), // tool fail 1
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10), // tool fail 2
			makeSpecializeResponse("bad-skill", 10),                         // specialize fail (should NOT count)
			makeCompleteResponse("alive", 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return nil, fmt.Errorf("skill %q not found", name)
	})

	pid, err := k.Spawn("specialize no breaker", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0 (specialize fail doesn't trip breaker), got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	if proc.Result != "alive" {
		t.Fatalf("expected Result 'alive', got %q", proc.Result)
	}
}

// --- Scenario 8: tool error injected into context ---

func TestUnified_ToolError_InjectsToContext(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/nonexistent", map[string]any{}, 10),
			makeCompleteResponse("recovered", 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("tool error test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	prompt, buildErr := ctxMgr.BuildPrompt(proc.CtxID)
	if buildErr != nil {
		t.Fatalf("BuildPrompt: %v", buildErr)
	}
	var errorFound bool
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool && (strings.Contains(m.Content, "Tool error") || strings.Contains(m.Content, "error: unknown tool")) {
			errorFound = true
			break
		}
	}
	if !errorFound {
		t.Fatal("tool error not injected into context")
	}

	if proc.Result != "recovered" {
		t.Fatalf("expected Result 'recovered', got %q", proc.Result)
	}
}

// --- Scenario 9: VFS flags auto-downgrade for empty payload ---

func TestUnified_VFSFlags_EmptyPayload_UsesReadOnly(t *testing.T) {
	reg := vfs.NewDeviceRegistry()

	var capturedFlags vfs.OpenFlag
	var flagsMu sync.Mutex

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/reader", nil, 10),
			makeCompleteResponse("flags-done", 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/reader", func(_ string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		flagsMu.Lock()
		capturedFlags = flags
		flagsMu.Unlock()
		return &mockToolFile{readData: []byte("read-data")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("flags test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	flagsMu.Lock()
	got := capturedFlags
	flagsMu.Unlock()
	if got != vfs.O_RDONLY {
		t.Fatalf("expected O_RDONLY (%d), got %d", vfs.O_RDONLY, got)
	}
}

// --- Scenario: VFS flags non-empty payload uses O_RDWR ---

func TestUnified_VFSFlags_NonEmptyPayload_UsesReadWrite(t *testing.T) {
	reg := vfs.NewDeviceRegistry()

	var capturedFlags vfs.OpenFlag
	var flagsMu sync.Mutex

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/writer", map[string]any{"content": "hello"}, 10),
			makeCompleteResponse("write-done", 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/writer", func(_ string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		flagsMu.Lock()
		capturedFlags = flags
		flagsMu.Unlock()
		return &mockToolFile{readData: []byte("written")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("write flags test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	flagsMu.Lock()
	got := capturedFlags
	flagsMu.Unlock()
	if got != vfs.O_RDWR {
		t.Fatalf("expected O_RDWR (%d), got %d", vfs.O_RDWR, got)
	}
}

// --- Scenario: VFS flags empty JSON object "{}" uses O_RDONLY ---

func TestUnified_VFSFlags_EmptyJSONObject_UsesReadOnly(t *testing.T) {
	reg := vfs.NewDeviceRegistry()

	var capturedFlags vfs.OpenFlag
	var flagsMu sync.Mutex

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/reader2", map[string]any{}, 10),
			makeCompleteResponse("empty-obj-done", 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/reader2", func(_ string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		flagsMu.Lock()
		capturedFlags = flags
		flagsMu.Unlock()
		return &mockToolFile{readData: []byte("data")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("empty obj flags", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	flagsMu.Lock()
	got := capturedFlags
	flagsMu.Unlock()
	if got != vfs.O_RDONLY {
		t.Fatalf("expected O_RDONLY (%d) for empty JSON '{}', got %d", vfs.O_RDONLY, got)
	}
}
