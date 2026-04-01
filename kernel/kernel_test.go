package kernel

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// --- Test helpers ---

// mockLLMFile simulates an LLM device for testing.
type mockLLMFile struct {
	mu        sync.Mutex
	writeData []byte
	readData  []byte
	closed    bool
	writeErr  error
	readErr   error
}

func (f *mockLLMFile) Write(_ gocontext.Context, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writeData = data
	return nil
}

func (f *mockLLMFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.readData, nil
}

func (f *mockLLMFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *mockLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// mockToolFile simulates a tool device for testing.
type mockToolFile struct {
	mu        sync.Mutex
	writeData []byte
	readData  []byte
	closed    bool
}

func (f *mockToolFile) Write(_ gocontext.Context, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeData = data
	return nil
}

func (f *mockToolFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readData, nil
}

func (f *mockToolFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *mockToolFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true}, nil
}

// newTestKernel creates a kernel with a VFS containing a mock LLM device.
// Registers t.Cleanup to call Shutdown automatically.
func newTestKernel(t testing.TB, llmFile *mockLLMFile) (*KernelImpl, *vfs.VFS, *rnixctx.Manager) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k, v, ctxMgr
}

// makeLLMResponse builds a JSON-encoded LLM response.
func makeLLMResponse(content string, tokens int) []byte {
	resp := llmResponse{Content: content, TokensUsed: tokens}
	data, _ := json.Marshal(resp)
	return data
}

// makeToolCallResponse builds a JSON-encoded LLM response containing a tool_call action.
func makeToolCallResponse(toolPath string, toolData map[string]any, tokens int) []byte {
	action := map[string]any{
		"action": "tool_call",
		"tool":   toolPath,
		"data":   toolData,
	}
	content, _ := json.Marshal(action)
	return makeLLMResponse(string(content), tokens)
}

// testAgentInfo creates a test AgentInfo mimicking the mock-skill behavior.
func testAgentInfo() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-agent",
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
				Fallback:  "haiku",
			},
			ContextBudget: 4096,
			Skills:        []string{"mock-skill"},
		},
		Instructions: "# Code Analyst\n\nYou are a code analysis agent.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "mock-skill",
					Description:     "A mock skill for testing",
					AllowedToolsRaw: "/dev/fs /dev/shell",
				},
				Body: "# Mock Skill\n\nReview source files for quality issues.",
			},
		},
	}
}

// --- Existing kernel tests ---

func newSimpleKernel(t testing.TB) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k
}

func TestNewKernel(t *testing.T) {
	k := newSimpleKernel(t)
	if k == nil {
		t.Fatal("NewKernel returned nil")
	}
	procs := k.ListProcesses()
	if len(procs) != 0 {
		t.Fatalf("expected empty process table, got %d entries", len(procs))
	}
}

func TestKernelAddGetRemove(t *testing.T) {
	k := newSimpleKernel(t)
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
	k := newSimpleKernel(t)
	_, ok := k.GetProcess(9999)
	if ok {
		t.Fatal("expected not found for non-existent PID")
	}
}

func TestProcessTableConcurrent(t *testing.T) {
	k := newSimpleKernel(t)
	const n = 100
	var wg sync.WaitGroup

	// Concurrent Add
	procs := make([]*Process, n)
	for i := range n {
		procs[i] = NewProcess(0, "test", nil)
	}

	for i := range n {
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
	for i := range n {
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
	for i := range n {
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
	k := newSimpleKernel(t)
	const n = 100
	var wg sync.WaitGroup

	// Mixed concurrent operations: Add, Get, Remove, List
	for range n {
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

// --- Spawn tests ---

func TestSpawn_Success(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("hello world", 42),
	}
	k, _, ctxMgr := newTestKernel(t, llmFile)

	pid, err := k.Spawn("test intent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if pid == 0 {
		t.Fatal("Spawn returned PID 0")
	}

	// Wait for process to complete
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found in table", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion")
	}

	// Verify process state
	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie state, got %d", proc.GetState())
	}
	if proc.Result != "hello world" {
		t.Fatalf("expected Result 'hello world', got %q", proc.Result)
	}
	if proc.TokensUsed != 42 {
		t.Fatalf("expected TokensUsed 42, got %d", proc.TokensUsed)
	}
	if proc.CtxID == 0 {
		t.Fatal("expected non-zero CtxID")
	}

	// Verify context was allocated
	_, err = ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("context should still exist: %v", err)
	}
}

func TestSpawn_WithSystemPrompt(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	k, _, ctxMgr := newTestKernel(t, llmFile)

	pid, err := k.Spawn("intent", nil, SpawnOpts{SystemPrompt: "you are a helper"})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Verify system prompt was set
	result, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	if result.SystemPrompt != "you are a helper" {
		t.Fatalf("expected system prompt 'you are a helper', got %q", result.SystemPrompt)
	}
}

func TestSpawn_VFSOpenFailure(t *testing.T) {
	// Create a kernel with no LLM device registered
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	_, err := k.Spawn("test", nil, SpawnOpts{})
	if err == nil {
		t.Fatal("expected error when LLM device not available")
	}

	// Verify CtxFree was called on the error path (kernel.go Spawn error recovery).
	// Spawn allocates CtxID 1 (first allocation), then fails at VFS Open.
	// The error path must call CtxFree to prevent context leak.
	_, ctxErr := ctxMgr.BuildPrompt(1)
	if ctxErr == nil {
		t.Error("context should be freed after Spawn VFS Open failure — CtxFree not called on error path")
	}
}

func TestSpawn_DebugChanEvents(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("debug result", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("debug intent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Drain DebugChan and verify events were recorded
	var events []types.SyscallEvent
	draining := true
	for draining {
		select {
		case ev := <-proc.DebugChan:
			events = append(events, ev)
		default:
			draining = false
		}
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (Spawn + ReasonStep), got %d", len(events))
	}

	foundSpawn := false
	foundReasonStep := false
	for _, ev := range events {
		if ev.Syscall == "Spawn" {
			foundSpawn = true
			if ev.PID != pid {
				t.Errorf("Spawn event PID: got %d, want %d", ev.PID, pid)
			}
		}
		if ev.Syscall == "ReasonStep" {
			foundReasonStep = true
		}
	}
	if !foundSpawn {
		t.Error("missing Spawn syscall event")
	}
	if !foundReasonStep {
		t.Error("missing ReasonStep syscall event")
	}
}

// --- reasonStep tests ---

func TestReasonStep_TextAction(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("The answer is 42", 100),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("what is the answer", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "The answer is 42" {
		t.Fatalf("expected Result 'The answer is 42', got %q", proc.Result)
	}
	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
}

func TestReasonStep_ToolCallAction(t *testing.T) {
	// Multi-step: first LLM read returns tool_call, second returns text
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/read", map[string]any{"path": "/foo"}, 50),
			makeLLMResponse("file content is bar", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	// Register mock tool device
	_ = reg.Register("/dev/tools/read", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("bar")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("read a file", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "file content is bar" {
		t.Fatalf("expected Result 'file content is bar', got %q", proc.Result)
	}
	if proc.TokensUsed != 80 { // 50 + 30
		t.Fatalf("expected TokensUsed 80, got %d", proc.TokensUsed)
	}
}

// sequenceLLMFile returns different responses on each Read call.
type sequenceLLMFile struct {
	mu        sync.Mutex
	responses [][]byte
	readIdx   int
	closed    bool
}

func (f *sequenceLLMFile) Write(_ gocontext.Context, data []byte) error {
	return nil
}

func (f *sequenceLLMFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readIdx >= len(f.responses) {
		return makeLLMResponse("fallback", 1), nil
	}
	resp := f.responses[f.readIdx]
	f.readIdx++
	return resp, nil
}

func (f *sequenceLLMFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *sequenceLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true}, nil
}

func TestReasonStep_LLMError(t *testing.T) {
	llmFile := &mockLLMFile{
		writeErr: fmt.Errorf("connection refused"),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1, got %d", exit.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
}

func TestReasonStep_LLMReadError(t *testing.T) {
	llmFile := &mockLLMFile{
		readErr: fmt.Errorf("read timeout"),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1, got %d", exit.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
}

func TestReasonStep_ContextCancellation(t *testing.T) {
	// LLM that blocks so we can cancel
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test cancel", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)

	// Give goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the process context
	proc.cancel()

	// Unblock the LLM so the goroutine can check cancellation
	close(blockCh)

	select {
	case exit := <-proc.Done:
		// Should have non-zero exit or context cancelled
		if exit.Code != 1 {
			// Could be 0 if it completed before cancellation check
			t.Logf("exit code: %d, reason: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled process")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
}

// blockingLLMFile blocks on Write until blockCh is closed, then returns a text response.
type blockingLLMFile struct {
	blockCh chan struct{}
	closed  bool
}

func (f *blockingLLMFile) Write(_ gocontext.Context, data []byte) error {
	<-f.blockCh
	return nil
}

func (f *blockingLLMFile) Read(length int) ([]byte, error) {
	return makeLLMResponse("unblocked", 1), nil
}

func (f *blockingLLMFile) Close() error {
	f.closed = true
	return nil
}

func (f *blockingLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true}, nil
}

func TestReasonStep_MaxStepsExceeded(t *testing.T) {
	// LLM always returns tool_call to force max steps
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{
			readData: makeToolCallResponse("/dev/tools/echo", map[string]any{}, 5),
		}, nil
	})
	_ = reg.Register("/dev/tools/echo", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echoed")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("loop forever", nil, SpawnOpts{MaxTurns: 3})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1 for max steps exceeded, got %d", exit.Code)
		}
		if exit.Reason != "max steps exceeded" {
			t.Fatalf("expected 'max steps exceeded', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
}

// --- parseAction tests ---

func TestParseAction_PlainText(t *testing.T) {
	resp := &llmResponse{Content: "Hello, world!", TokensUsed: 10}
	action := parseAction(resp)
	if action.Type != ActionText {
		t.Fatalf("expected ActionText, got %s", action.Type)
	}
	if action.Content != "Hello, world!" {
		t.Fatalf("expected 'Hello, world!', got %q", action.Content)
	}
}

func TestParseAction_ToolCall(t *testing.T) {
	toolAction := map[string]any{
		"action": "tool_call",
		"tool":   "/dev/fs/read",
		"data":   map[string]any{"path": "/etc/config"},
	}
	content, _ := json.Marshal(toolAction)
	resp := &llmResponse{Content: string(content), TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionToolCall {
		t.Fatalf("expected ActionToolCall, got %s", action.Type)
	}
	if action.ToolPath != "/dev/fs/read" {
		t.Fatalf("expected '/dev/fs/read', got %q", action.ToolPath)
	}
}

func TestParseAction_InvalidJSON(t *testing.T) {
	resp := &llmResponse{Content: "not json at all", TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionText {
		t.Fatalf("expected ActionText for invalid JSON, got %s", action.Type)
	}
}

func TestParseAction_JSONWithoutAction(t *testing.T) {
	resp := &llmResponse{Content: `{"key": "value"}`, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionText {
		t.Fatalf("expected ActionText for JSON without action field, got %s", action.Type)
	}
}

func TestParseAction_ToolCallMissingTool(t *testing.T) {
	resp := &llmResponse{Content: `{"action": "tool_call"}`, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionText {
		t.Fatalf("expected ActionText for tool_call without tool, got %s", action.Type)
	}
}

func TestParseAction_Plan(t *testing.T) {
	content := `{"action": "plan", "data": {"steps": ["step1", "step2"], "reason": "complex task"}}`
	resp := &llmResponse{Content: content, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionPlan {
		t.Fatalf("expected ActionPlan, got %s", action.Type)
	}
	if action.Content != content {
		t.Fatalf("Content mismatch")
	}
	if len(action.ToolData) == 0 {
		t.Fatal("expected ToolData to be non-empty")
	}
}

func TestParseAction_PlanNoData(t *testing.T) {
	resp := &llmResponse{Content: `{"action": "plan"}`, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionPlan {
		t.Fatalf("expected ActionPlan, got %s", action.Type)
	}
	if string(action.ToolData) != "{}" {
		t.Fatalf("expected ToolData='{}', got %q", string(action.ToolData))
	}
}

func TestParseAction_Spawn(t *testing.T) {
	content := `{"action": "spawn", "tool": "analyze code", "data": {"agent": "analyst", "model": "haiku"}}`
	resp := &llmResponse{Content: content, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionSpawn {
		t.Fatalf("expected ActionSpawn, got %s", action.Type)
	}
	if action.ToolPath != "analyze code" {
		t.Fatalf("expected ToolPath='analyze code', got %q", action.ToolPath)
	}
	if len(action.ToolData) == 0 {
		t.Fatal("expected ToolData to be non-empty")
	}
}

func TestParseAction_SpawnEmptyTool(t *testing.T) {
	resp := &llmResponse{Content: `{"action": "spawn", "tool": "", "data": {}}`, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionSpawn {
		t.Fatalf("expected ActionSpawn, got %s", action.Type)
	}
	if action.ToolPath != "" {
		t.Fatalf("expected empty ToolPath, got %q", action.ToolPath)
	}
}

func TestParseAction_Complete(t *testing.T) {
	content := `{"action": "complete", "data": {"result": "task done"}}`
	resp := &llmResponse{Content: content, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionComplete {
		t.Fatalf("expected ActionComplete, got %s", action.Type)
	}
	if len(action.ToolData) == 0 {
		t.Fatal("expected ToolData to be non-empty")
	}
}

func TestParseAction_CompleteNoData(t *testing.T) {
	content := `{"action": "complete"}`
	resp := &llmResponse{Content: content, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionComplete {
		t.Fatalf("expected ActionComplete, got %s", action.Type)
	}
	if string(action.ToolData) != "{}" {
		t.Fatalf("expected ToolData='{}', got %q", string(action.ToolData))
	}
}

func TestParseAction_Replan(t *testing.T) {
	content := `{"action": "replan", "data": {"reason": "first approach failed"}}`
	resp := &llmResponse{Content: content, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionReplan {
		t.Fatalf("expected ActionReplan, got %s", action.Type)
	}
	if len(action.ToolData) == 0 {
		t.Fatal("expected ToolData to be non-empty")
	}
}

func TestParseAction_Specialize(t *testing.T) {
	content := `{"action": "specialize", "tool": "code-analyst"}`
	resp := &llmResponse{Content: content, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionSpecialize {
		t.Fatalf("expected ActionSpecialize, got %s", action.Type)
	}
	if action.ToolPath != "code-analyst" {
		t.Fatalf("expected ToolPath='code-analyst', got %q", action.ToolPath)
	}
	if len(action.ToolData) == 0 {
		t.Fatal("expected ToolData to be non-empty")
	}
}

func TestParseAction_UnknownAction(t *testing.T) {
	resp := &llmResponse{Content: `{"action": "unknown_type"}`, TokensUsed: 5}
	action := parseAction(resp)
	if action.Type != ActionText {
		t.Fatalf("expected ActionText for unknown action, got %s", action.Type)
	}
}

func TestParseAction_MarkdownCodeBlock(t *testing.T) {
	content := "我将读取相关文档。\n\n```json\n{\"action\": \"tool_call\", \"tool\": \"/dev/fs/workflow.md\", \"data\": {}}\n```"
	resp := &llmResponse{Content: content, TokensUsed: 100}
	action := parseAction(resp)
	if action.Type != ActionToolCall {
		t.Fatalf("expected ActionToolCall from markdown code block, got %s", action.Type)
	}
	if action.ToolPath != "/dev/fs/workflow.md" {
		t.Fatalf("expected tool path /dev/fs/workflow.md, got %s", action.ToolPath)
	}
}

func TestParseAction_PlainCodeBlock(t *testing.T) {
	content := "Let me read the file.\n\n```\n{\"action\": \"tool_call\", \"tool\": \"/dev/shell\", \"data\": {\"command\": \"ls\"}}\n```"
	resp := &llmResponse{Content: content, TokensUsed: 50}
	action := parseAction(resp)
	if action.Type != ActionToolCall {
		t.Fatalf("expected ActionToolCall from plain code block, got %s", action.Type)
	}
	if action.ToolPath != "/dev/shell" {
		t.Fatalf("expected tool path /dev/shell, got %s", action.ToolPath)
	}
}

func TestParseAction_TextFollowedByBareJSON(t *testing.T) {
	content := "I'll analyze the config.\n\n{\"action\": \"tool_call\", \"tool\": \"/dev/fs/config.yaml\", \"data\": {}}"
	resp := &llmResponse{Content: content, TokensUsed: 80}
	action := parseAction(resp)
	if action.Type != ActionToolCall {
		t.Fatalf("expected ActionToolCall from trailing JSON, got %s", action.Type)
	}
	if action.ToolPath != "/dev/fs/config.yaml" {
		t.Fatalf("expected tool path /dev/fs/config.yaml, got %s", action.ToolPath)
	}
}

func TestParseAction_EmbeddedPlanInCodeBlock(t *testing.T) {
	content := "Here is my plan:\n\n```json\n{\"action\": \"plan\", \"data\": {\"steps\": [\"step1\"]}}\n```"
	resp := &llmResponse{Content: content, TokensUsed: 50}
	action := parseAction(resp)
	if action.Type != ActionPlan {
		t.Fatalf("expected ActionPlan from code block, got %s", action.Type)
	}
}

func TestParseAction_NoEmbeddedJSON(t *testing.T) {
	content := "This is just regular text with no JSON at all.\nNothing to parse here."
	resp := &llmResponse{Content: content, TokensUsed: 20}
	action := parseAction(resp)
	if action.Type != ActionText {
		t.Fatalf("expected ActionText for pure text, got %s", action.Type)
	}
}

// --- Integration test ---

func TestSpawn_Integration(t *testing.T) {
	// End-to-end: Spawn → reasonStep → text → Zombie → Done channel
	// Uses real VFS + mock LLM device + real Context Manager
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("integration result", 77),
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("integration test", nil, SpawnOpts{
		SystemPrompt: "You are a test agent",
		Model:        "claude-test",
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process not found")
	}

	// Verify process is in table
	procs := k.ListProcesses()
	found := false
	for _, p := range procs {
		if p.PID == pid {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("spawned process not in ListProcesses")
	}

	// Wait for completion
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
		if exit.Reason != "completed" {
			t.Fatalf("expected reason 'completed', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Final state checks
	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
	if proc.Result != "integration result" {
		t.Fatalf("expected 'integration result', got %q", proc.Result)
	}
	if proc.TokensUsed != 77 {
		t.Fatalf("expected 77 tokens, got %d", proc.TokensUsed)
	}
	if proc.CtxID == 0 {
		t.Fatal("expected non-zero CtxID")
	}
	if proc.Intent != "integration test" {
		t.Fatalf("expected intent 'integration test', got %q", proc.Intent)
	}

	// Verify context content
	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	if prompt.SystemPrompt != "You are a test agent" {
		t.Fatalf("expected system prompt, got %q", prompt.SystemPrompt)
	}
	if len(prompt.Messages) < 1 {
		t.Fatal("expected at least 1 message in context")
	}
	if prompt.Messages[0].Content != "integration test" {
		t.Fatalf("expected first message to be intent, got %q", prompt.Messages[0].Content)
	}
}

// --- Process Table Consistency Tests ---

// assertProcessTableConsistency verifies all processes in the table have consistent state.
func assertProcessTableConsistency(t *testing.T, k *KernelImpl) {
	t.Helper()
	procs := k.ListProcesses()
	for _, p := range procs {
		state := p.GetState()
		if state == types.StateRunning {
			t.Errorf("PID %d still Running after expected completion", p.PID)
		}
		if state == types.StateZombie && p.Exit == nil {
			t.Errorf("PID %d is Zombie but Exit is nil", p.PID)
		}
	}
}

func TestProcessTableConsistency_AfterNormalExit(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("normal exit", 50),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found after spawn")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d", exit.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Verify consistency
	if proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie, got %d", proc.GetState())
	}
	if proc.Exit == nil {
		t.Error("expected non-nil Exit")
	}
	assertProcessTableConsistency(t, k)

	// Process should still be in table (Zombie, not yet reaped)
	_, ok = k.GetProcess(pid)
	if !ok {
		t.Error("Zombie process should still be in process table")
	}
}

func TestProcessTableConsistency_AfterError(t *testing.T) {
	llmFile := &mockLLMFile{
		writeErr: fmt.Errorf("device error"),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("error test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code == 0 {
			t.Fatal("expected non-zero exit code for error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Verify no dangling PID, state consistency
	if proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie, got %d", proc.GetState())
	}
	assertProcessTableConsistency(t, k)
}

func TestProcessTableConsistency_MultipleProcesses(t *testing.T) {
	const n = 5
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("multi", 10)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	var pids []types.PID
	var procs []*Process
	for i := range n {
		pid, err := k.Spawn(fmt.Sprintf("proc-%d", i), nil, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn %d failed: %v", i, err)
		}
		proc, _ := k.GetProcess(pid)
		pids = append(pids, pid)
		procs = append(procs, proc)
	}

	// Wait for all processes to complete
	for i, proc := range procs {
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatalf("process %d timed out", i)
		}
	}

	// Verify each process has correct state
	for _, pid := range pids {
		p, ok := k.GetProcess(pid)
		if !ok {
			t.Errorf("PID %d not found in process table", pid)
			continue
		}
		if p.GetState() != types.StateZombie {
			t.Errorf("PID %d: expected Zombie, got %d", pid, p.GetState())
		}
		if p.Exit == nil {
			t.Errorf("PID %d: expected non-nil Exit", pid)
		}
		if p.Result != "multi" {
			t.Errorf("PID %d: expected result 'multi', got %q", pid, p.Result)
		}
	}

	assertProcessTableConsistency(t, k)

	// Verify total process count
	listed := k.ListProcesses()
	if len(listed) != n {
		t.Errorf("expected %d processes in table, got %d", n, len(listed))
	}
}

func TestProcessTableConsistency_ConcurrentSpawn(t *testing.T) {
	const n = 10
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("concurrent", 5)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	var wg sync.WaitGroup
	pidCh := make(chan types.PID, n)
	errCh := make(chan error, n)

	// Spawn n processes concurrently
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pid, err := k.Spawn(fmt.Sprintf("concurrent-%d", i), nil, SpawnOpts{})
			if err != nil {
				errCh <- fmt.Errorf("spawn %d: %w", i, err)
				return
			}
			pidCh <- pid
		}(i)
	}
	wg.Wait()
	close(pidCh)
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent spawn error: %v", err)
	}

	var pids []types.PID
	for pid := range pidCh {
		pids = append(pids, pid)
	}

	if len(pids) != n {
		t.Fatalf("expected %d PIDs, got %d", n, len(pids))
	}

	// Wait for all processes to finish
	for _, pid := range pids {
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Errorf("PID %d not in table", pid)
			continue
		}
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatalf("PID %d timed out", pid)
		}
	}

	// Verify no data race issues (test runs with -race)
	assertProcessTableConsistency(t, k)

	// Verify unique PIDs
	seen := make(map[types.PID]bool)
	for _, pid := range pids {
		if seen[pid] {
			t.Errorf("duplicate PID %d", pid)
		}
		seen[pid] = true
	}
}

// --- Story 2.6: Agent Injection and Device Permission Whitelist Tests ---

func TestSpawn_WithAgent_InjectsInstructions(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	k, _, ctxMgr := newTestKernel(t, llmFile)

	agent := testAgentInfo()
	pid, err := k.Spawn("analyze code", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Verify instructions were injected into system prompt
	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	if !strings.Contains(prompt.SystemPrompt, "Code Analyst") {
		t.Errorf("expected system prompt to contain 'Code Analyst', got %q", prompt.SystemPrompt)
	}
	// Also verify skill body is in system prompt
	if !strings.Contains(prompt.SystemPrompt, "Mock Skill") {
		t.Errorf("expected system prompt to contain 'Mock Skill' from skill body, got %q", prompt.SystemPrompt)
	}
}

func TestSpawn_WithAgent_SetsAllowedDevices(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	agent := testAgentInfo()
	pid, err := k.Spawn("test", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// AllowedDevices aggregated from agent skills (sorted)
	if len(proc.AllowedDevices) != 2 {
		t.Fatalf("expected 2 AllowedDevices, got %d: %v", len(proc.AllowedDevices), proc.AllowedDevices)
	}
	if proc.AllowedDevices[0] != "/dev/fs" || proc.AllowedDevices[1] != "/dev/shell" {
		t.Errorf("unexpected AllowedDevices: %v", proc.AllowedDevices)
	}
}

func TestSpawn_WithAgent_ModelSelection(t *testing.T) {
	// Test 1: Agent preferred model used when CLI model is empty
	var capturedReq llmRequest
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	captureLLM := &capturingLLMFile{
		inner:       llmFile,
		capturedReq: &capturedReq,
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return captureLLM, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	agent := testAgentInfo()
	pid, err := k.Spawn("test model", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Agent manifest has models.preferred: "sonnet"
	if capturedReq.Model != "sonnet" {
		t.Errorf("expected model 'sonnet' from agent manifest, got %q", capturedReq.Model)
	}

	// Test 2: CLI model overrides Agent model
	llmFile2 := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	var capturedReq2 llmRequest
	captureLLM2 := &capturingLLMFile{
		inner:       llmFile2,
		capturedReq: &capturedReq2,
	}
	reg2 := vfs.NewDeviceRegistry()
	_ = reg2.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return captureLLM2, nil
	})
	v2 := vfs.NewVFS(reg2)
	ctxMgr2 := rnixctx.NewManager()
	k2 := NewKernel(v2, ctxMgr2, nil)

	pid2, err := k2.Spawn("test model override", agent, SpawnOpts{Model: "opus"})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc2, _ := k2.GetProcess(pid2)
	select {
	case <-proc2.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if capturedReq2.Model != "opus" {
		t.Errorf("expected CLI model 'opus' to override agent, got %q", capturedReq2.Model)
	}
}

func TestSpawn_WithoutAgent_AllDevicesAllowed(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("test no agent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if len(proc.AllowedDevices) != 0 {
		t.Errorf("expected empty AllowedDevices for no-agent mode, got %v", proc.AllowedDevices)
	}
}

// Permission check tests

func TestReasonStep_PermissionDenied_WhenDeviceNotInWhitelist(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/llm/claude", map[string]any{}, 10),
			makeLLMResponse("permission handled", 5),
		},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	agent := testAgentInfo()
	pid, err := k.Spawn("test perm denied", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0 (graceful denial), got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "permission handled" {
		t.Errorf("expected 'permission handled', got %q", proc.Result)
	}

	// Verify permission denied event was emitted
	var foundPermDenied bool
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == "ReasonStep" {
				if action, ok := ev.Args["action"]; ok && action == "permission_denied" {
					foundPermDenied = true
				}
			}
		default:
			goto drained
		}
	}
drained:
	if !foundPermDenied {
		t.Error("expected permission_denied event in DebugChan")
	}
}

func TestReasonStep_PermissionAllowed_WhenDeviceInWhitelist(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/fs", map[string]any{"path": "/test"}, 10),
			makeLLMResponse("tool result processed", 5),
		},
	}

	mockFS := &mockToolFile{readData: []byte("file content")}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return mockFS, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	agent := testAgentInfo()
	pid, err := k.Spawn("test perm allowed", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "tool result processed" {
		t.Errorf("expected 'tool result processed', got %q", proc.Result)
	}
}

func TestReasonStep_PrefixMatch_AllowsSubpath(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/fs/path/to/file", map[string]any{}, 10),
			makeLLMResponse("subpath ok", 5),
		},
	}

	mockFS := &mockToolFile{readData: []byte("content")}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return mockFS, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	agent := testAgentInfo()
	pid, err := k.Spawn("test prefix match", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "subpath ok" {
		t.Errorf("expected 'subpath ok', got %q", proc.Result)
	}
}

func TestReasonStep_NoWhitelist_AllowsAll(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/any/device", map[string]any{}, 10),
			makeLLMResponse("all allowed", 5),
		},
	}

	mockDevice := &mockToolFile{readData: []byte("result")}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/any/device", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return mockDevice, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil) // No agent
	defer k.Shutdown()

	pid, err := k.Spawn("test no whitelist", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "all allowed" {
		t.Errorf("expected 'all allowed', got %q", proc.Result)
	}
}

func TestReasonStep_PathTraversal_Blocked(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/fs/../llm/claude", map[string]any{}, 10),
			makeLLMResponse("traversal blocked", 5),
		},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	agent := testAgentInfo()
	pid, err := k.Spawn("test traversal", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "traversal blocked" {
		t.Errorf("expected 'traversal blocked', got %q", proc.Result)
	}

	// Verify permission_denied event was emitted for the traversal attempt
	var foundPermDenied bool
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == "ReasonStep" {
				if action, ok := ev.Args["action"]; ok && action == "permission_denied" {
					foundPermDenied = true
				}
			}
		default:
			goto drained
		}
	}
drained:
	if !foundPermDenied {
		t.Error("expected permission_denied event for path traversal attempt")
	}
}

// capturingLLMFile wraps an LLM file and captures the request JSON.
type capturingLLMFile struct {
	inner       *mockLLMFile
	capturedReq *llmRequest
}

func (f *capturingLLMFile) Write(_ gocontext.Context, data []byte) error {
	_ = json.Unmarshal(data, f.capturedReq)
	return f.inner.Write(gocontext.Background(), data)
}

func (f *capturingLLMFile) Read(length int) ([]byte, error) {
	return f.inner.Read(length)
}

func (f *capturingLLMFile) Close() error {
	return f.inner.Close()
}

func (f *capturingLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// --- Story 3.1: SyscallEvent Recording Infrastructure Tests ---

// drainEvents collects all events from a DebugChan.
func drainEvents(ch chan types.SyscallEvent) []types.SyscallEvent {
	var events []types.SyscallEvent
	for {
		select {
		case ev := <-ch:
			events = append(events, ev)
		default:
			return events
		}
	}
}

// eventNames extracts the Syscall names from a list of events.
func eventNames(events []types.SyscallEvent) []string {
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = ev.Syscall
	}
	return names
}

// containsEvent checks if any event has the given syscall name.
func containsEvent(events []types.SyscallEvent, syscall string) bool {
	for _, ev := range events {
		if ev.Syscall == syscall {
			return true
		}
	}
	return false
}

func TestSpawn_VFSEvents_OpenWriteRead(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("vfs events test", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("vfs test", nil, SpawnOpts{})
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

	// Expect: CtxAlloc, CtxWrite (AppendMessage), Open, Spawn, CtxRead (BuildPrompt), Write, Read, ReasonStep
	if !containsEvent(events, "Open") {
		t.Errorf("missing Open event; got events: %v", eventNames(events))
	}
	if !containsEvent(events, "Write") {
		t.Errorf("missing Write event; got events: %v", eventNames(events))
	}
	if !containsEvent(events, "Read") {
		t.Errorf("missing Read event; got events: %v", eventNames(events))
	}

	// Verify Open event has correct args
	for _, ev := range events {
		if ev.Syscall == "Open" {
			if ev.Args["path"] != "/dev/llm/claude" {
				t.Errorf("Open event path: got %v, want /dev/llm/claude", ev.Args["path"])
			}
			if ev.PID != pid {
				t.Errorf("Open event PID: got %d, want %d", ev.PID, pid)
			}
			if ev.Duration <= 0 {
				t.Errorf("Open event Duration should be positive, got %v", ev.Duration)
			}
			break
		}
	}

	// Verify Write event has correct args (fd and size)
	for _, ev := range events {
		if ev.Syscall == "Write" {
			if ev.Args["fd"] == nil {
				t.Errorf("Write event missing 'fd' arg")
			}
			if ev.Args["size"] == nil {
				t.Errorf("Write event missing 'size' arg")
			}
			if ev.PID != pid {
				t.Errorf("Write event PID: got %d, want %d", ev.PID, pid)
			}
			if ev.Duration <= 0 {
				t.Errorf("Write event Duration should be positive, got %v", ev.Duration)
			}
			break
		}
	}

	// Verify Read event has correct args (fd and length)
	for _, ev := range events {
		if ev.Syscall == "Read" {
			if ev.Args["fd"] == nil {
				t.Errorf("Read event missing 'fd' arg")
			}
			if ev.Args["length"] == nil {
				t.Errorf("Read event missing 'length' arg")
			}
			if ev.PID != pid {
				t.Errorf("Read event PID: got %d, want %d", ev.PID, pid)
			}
			if ev.Duration <= 0 {
				t.Errorf("Read event Duration should be positive, got %v", ev.Duration)
			}
			break
		}
	}
}

func TestSpawn_ContextEvents_CtxAllocCtxWriteCtxRead(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("ctx events test", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("ctx test", nil, SpawnOpts{SystemPrompt: "test prompt"})
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

	if !containsEvent(events, "CtxAlloc") {
		t.Errorf("missing CtxAlloc event; got events: %v", eventNames(events))
	}
	if !containsEvent(events, "CtxWrite") {
		t.Errorf("missing CtxWrite event; got events: %v", eventNames(events))
	}
	if !containsEvent(events, "CtxRead") {
		t.Errorf("missing CtxRead event; got events: %v", eventNames(events))
	}

	// Verify CtxAlloc event
	for _, ev := range events {
		if ev.Syscall == "CtxAlloc" {
			if ev.Args["size"] != DefaultCtxSize {
				t.Errorf("CtxAlloc size: got %v, want %d", ev.Args["size"], DefaultCtxSize)
			}
			if ev.PID != pid {
				t.Errorf("CtxAlloc PID: got %d, want %d", ev.PID, pid)
			}
			break
		}
	}

	// Verify CtxWrite events include SetSystemPrompt and AppendMessage
	var foundSetPrompt, foundAppendMsg bool
	for _, ev := range events {
		if ev.Syscall == "CtxWrite" {
			op, _ := ev.Args["op"].(string)
			if op == "SetSystemPrompt" {
				foundSetPrompt = true
			}
			if op == "AppendMessage" {
				foundAppendMsg = true
			}
		}
	}
	if !foundSetPrompt {
		t.Error("missing CtxWrite/SetSystemPrompt event")
	}
	if !foundAppendMsg {
		t.Error("missing CtxWrite/AppendMessage event")
	}

	// Verify CtxRead (BuildPrompt) event
	for _, ev := range events {
		if ev.Syscall == "CtxRead" {
			op, _ := ev.Args["op"].(string)
			if op != "BuildPrompt" {
				t.Errorf("CtxRead op: got %q, want BuildPrompt", op)
			}
			break
		}
	}
}

func TestToolCall_VFSAndContextEvents(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/echo", map[string]any{"msg": "hi"}, 10),
			makeLLMResponse("tool done", 5),
		},
	}
	mockTool := &mockToolFile{readData: []byte("echoed")}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return mockTool, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("tool test", nil, SpawnOpts{})
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

	// Tool call path should produce: Open (tool), Write (tool), Read (tool), Close (tool)
	openCount := 0
	writeCount := 0
	readCount := 0
	closeCount := 0
	ctxWriteCount := 0
	for _, ev := range events {
		switch ev.Syscall {
		case "Open":
			openCount++
		case "Write":
			writeCount++
		case "Read":
			readCount++
		case "Close":
			closeCount++
		case "CtxWrite":
			ctxWriteCount++
		}
	}

	// At least 2 Opens (LLM + tool), 2 Writes, 2 Reads, 1 Close (tool)
	if openCount < 2 {
		t.Errorf("expected at least 2 Open events, got %d", openCount)
	}
	if writeCount < 2 {
		t.Errorf("expected at least 2 Write events, got %d", writeCount)
	}
	if readCount < 2 {
		t.Errorf("expected at least 2 Read events, got %d", readCount)
	}
	if closeCount < 1 {
		t.Errorf("expected at least 1 Close event, got %d", closeCount)
	}
	// CtxWrite: AppendMessage (initial) + AppendMessage (assistant) + AppendToolResult
	if ctxWriteCount < 3 {
		t.Errorf("expected at least 3 CtxWrite events, got %d", ctxWriteCount)
	}
}

func TestNilDebugChan_ZeroOverhead(t *testing.T) {
	// Verify emitEvent with nil DebugChan does not panic.
	// AC #10: DebugChan 为 nil 时零开销（无 strace 附着）
	k := newSimpleKernel(t)
	proc := NewProcess(0, "nil chan test", nil)
	proc.DebugChan = nil // simulate no strace attached

	// Direct call to emitEvent — must not panic or block.
	k.emitEvent(proc, "Open", map[string]any{"path": "/dev/null"}, 1, nil, time.Millisecond)
	k.emitEvent(proc, "Write", map[string]any{"fd": 1, "size": 42}, nil, nil, time.Millisecond)
	k.emitEvent(proc, "Read", map[string]any{"fd": 1, "length": 100}, 100, nil, time.Millisecond)
	k.emitEvent(proc, "CtxAlloc", map[string]any{"size": 64}, types.CtxID(1), nil, time.Millisecond)
}

func BenchmarkEmitEvent_NilDebugChan(b *testing.B) {
	// AC #10: DebugChan 为 nil 时零开销 — verify zero allocations inside emitEvent.
	k := newSimpleKernel(b)
	proc := NewProcess(0, "bench", nil)
	proc.DebugChan = nil

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k.emitEvent(proc, "Open", map[string]any{"path": "/dev/null"}, 1, nil, time.Millisecond)
	}
}

// --- Story 4.1: Kill syscall tests ---

func TestKill_RunningProcess(t *testing.T) {
	// Kill a running process → proc.Cancel() called, reasonStep detects cancellation, process → Zombie
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("kill test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Let goroutine start and block on Write
	time.Sleep(50 * time.Millisecond)

	// Kill the running process
	if err := k.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	// Unblock LLM so goroutine can detect cancellation
	close(blockCh)

	select {
	case exit := <-proc.Done:
		if exit.Code == 0 {
			t.Log("process completed normally before cancellation detected")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for killed process")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
}

func TestKill_PIDNotFound(t *testing.T) {
	k := newSimpleKernel(t)

	err := k.Kill(9999, types.SIGTERM)
	if err == nil {
		t.Fatal("expected error for non-existent PID")
	}

	var syscallErr *SyscallError
	if !errors.As(err, &syscallErr) {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if syscallErr.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %s", syscallErr.Code)
	}
	if syscallErr.Syscall != "Kill" {
		t.Errorf("expected syscall 'Kill', got %q", syscallErr.Syscall)
	}
}

func TestKill_ZombieIdempotent(t *testing.T) {
	// Kill on a Zombie process should be a no-op (return nil)
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 1),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("zombie test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)

	// Wait for process to complete → Zombie
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}

	// Kill should be idempotent on Zombie
	if err := k.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill on Zombie should return nil, got %v", err)
	}
	if err := k.Kill(pid, types.SIGKILL); err != nil {
		t.Fatalf("Kill with SIGKILL on Zombie should return nil, got %v", err)
	}
}

func TestKill_SyscallEvent(t *testing.T) {
	// Verify Kill emits SyscallEvents when DebugChan is non-nil
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("event test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)

	// Let goroutine start
	time.Sleep(50 * time.Millisecond)

	// Kill
	_ = k.Kill(pid, types.SIGTERM)

	// Unblock
	close(blockCh)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Drain DebugChan and find Kill events
	var killEvents []types.SyscallEvent
	draining := true
	for draining {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == "Kill" {
				killEvents = append(killEvents, ev)
			}
		default:
			draining = false
		}
	}

	if len(killEvents) < 2 {
		t.Fatalf("expected at least 2 Kill events (entry + exit), got %d", len(killEvents))
	}

	// Verify first event has pid and signal args
	args := killEvents[0].Args
	if args["pid"] != pid {
		t.Errorf("Kill event pid: got %v, want %d", args["pid"], pid)
	}
	if args["signal"] != types.SIGTERM.String() {
		t.Errorf("Kill event signal: got %v, want %s", args["signal"], types.SIGTERM.String())
	}
}

func TestKill_InvalidSignal(t *testing.T) {
	// Kill with an invalid signal value should return ErrInvalid
	k := newSimpleKernel(t)

	err := k.Kill(1, types.Signal(999))
	if err == nil {
		t.Fatal("expected error for invalid signal")
	}

	var syscallErr *SyscallError
	if !errors.As(err, &syscallErr) {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if syscallErr.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %s", syscallErr.Code)
	}
}

func TestKill_CreatedState(t *testing.T) {
	// Kill on a process in Created state (goroutine not yet started).
	// Cancel() should be safe; goroutine detects ctx.Done() on start.
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("created-kill test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Kill immediately — process may still be in Created or just entering Running
	if err := k.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill on Created/Running process failed: %v", err)
	}

	// Unblock LLM so goroutine can proceed and detect cancellation
	close(blockCh)

	proc, ok := k.GetProcess(pid)
	if !ok {
		// Process may have already been reaped if finishProcess ran quickly
		return
	}

	select {
	case <-proc.Done:
		// Process finished (Zombie)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for killed process")
	}
}

func TestKill_RunningProcess_SIGKILL(t *testing.T) {
	// Kill a running process with SIGKILL (vs SIGTERM tested in TestKill_RunningProcess)
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("sigkill test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Let goroutine start and block on Write
	time.Sleep(50 * time.Millisecond)

	// Kill with SIGKILL
	if err := k.Kill(pid, types.SIGKILL); err != nil {
		t.Fatalf("Kill with SIGKILL failed: %v", err)
	}

	// Unblock LLM
	close(blockCh)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SIGKILL-ed process")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %s", proc.GetState())
	}
}

// --- Story 4.2: Spawn parent-child tracking tests ---

func TestSpawn_ParentPID(t *testing.T) {
	// Spawn parent, then spawn child with ParentPID set
	llmFile := &mockLLMFile{readData: makeLLMResponse("ok", 1)}
	k, _, _ := newTestKernel(t, llmFile)
	defer k.Shutdown()

	parentPID, err := k.Spawn("parent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("parent Spawn failed: %v", err)
	}

	childPID, err := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("child Spawn failed: %v", err)
	}

	child, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatal("child process not found")
	}
	if child.PPID != parentPID {
		t.Errorf("child PPID: got %d, want %d", child.PPID, parentPID)
	}

	parent, ok := k.GetProcess(parentPID)
	if !ok {
		t.Fatal("parent process not found")
	}
	children := parent.GetChildren()
	if len(children) != 1 || children[0] != childPID {
		t.Errorf("parent children: got %v, want [%d]", children, childPID)
	}
}

func TestSpawn_ParentNotFound(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("ok", 1)}
	k, _, _ := newTestKernel(t, llmFile)
	defer k.Shutdown()

	_, err := k.Spawn("orphan", nil, SpawnOpts{ParentPID: 9999})
	if err == nil {
		t.Fatal("expected error for non-existent parent")
	}

	var syscallErr *SyscallError
	if !errors.As(err, &syscallErr) {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if syscallErr.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %s", syscallErr.Code)
	}
}

func TestSpawn_ZeroParentPID_NoTracking(t *testing.T) {
	// Default ParentPID=0 should not attempt parent lookup
	llmFile := &mockLLMFile{readData: makeLLMResponse("ok", 1)}
	k, _, _ := newTestKernel(t, llmFile)
	defer k.Shutdown()

	pid, err := k.Spawn("top-level", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}
	if proc.PPID != 0 {
		t.Errorf("expected PPID 0, got %d", proc.PPID)
	}
}

// ========== Story 4.3: GetProcInfo / ListProcs Tests ==========

func TestGetProcInfo_ReturnsSnapshot(t *testing.T) {
	k := newSimpleKernel(t)

	proc := NewProcess(0, "test intent", []string{"skill-a", "skill-b"})
	proc.AllowedDevices = []string{"/dev/fs"}
	_ = proc.Start()
	k.AddProcess(proc)

	info, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo failed: %v", err)
	}

	if info.PID != proc.PID {
		t.Errorf("PID: got %d, want %d", info.PID, proc.PID)
	}
	if info.PPID != 0 {
		t.Errorf("PPID: got %d, want 0", info.PPID)
	}
	if info.State != types.StateRunning {
		t.Errorf("State: got %d, want %d", info.State, types.StateRunning)
	}
	if info.Intent != "test intent" {
		t.Errorf("Intent: got %q, want %q", info.Intent, "test intent")
	}
	if len(info.Skills) != 2 || info.Skills[0] != "skill-a" {
		t.Errorf("Skills: got %v, want [skill-a, skill-b]", info.Skills)
	}
	if len(info.AllowedDevices) != 1 || info.AllowedDevices[0] != "/dev/fs" {
		t.Errorf("AllowedDevices: got %v, want [/dev/fs]", info.AllowedDevices)
	}
}

func TestGetProcInfo_NotFound(t *testing.T) {
	k := newSimpleKernel(t)

	_, err := k.GetProcInfo(types.PID(9999))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var vfsErr *vfs.VFSError
	if !errors.As(err, &vfsErr) {
		t.Fatalf("expected *vfs.VFSError, got %T: %v", err, err)
	}
	if vfsErr.Code != types.ErrNotFound {
		t.Errorf("code: got %q, want %q", vfsErr.Code, types.ErrNotFound)
	}
}

func TestGetProcInfo_PID0NotFound(t *testing.T) {
	k := newSimpleKernel(t)

	_, err := k.GetProcInfo(types.PID(0))
	if err == nil {
		t.Fatal("expected error for PID 0, got nil")
	}
}

func TestGetProcInfo_MutableFieldsUnderLock(t *testing.T) {
	k := newSimpleKernel(t)

	proc := NewProcess(0, "mutable test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	// Modify mutable fields
	proc.mu.Lock()
	proc.TokensUsed = 500
	proc.Result = "done"
	proc.mu.Unlock()

	info, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo failed: %v", err)
	}
	if info.TokensUsed != 500 {
		t.Errorf("TokensUsed: got %d, want 500", info.TokensUsed)
	}
	if info.Result != "done" {
		t.Errorf("Result: got %q, want %q", info.Result, "done")
	}
}

func TestListProcs_ReturnsAll(t *testing.T) {
	k := newSimpleKernel(t)

	proc1 := NewProcess(0, "intent 1", nil)
	_ = proc1.Start()
	k.AddProcess(proc1)

	proc2 := NewProcess(0, "intent 2", []string{"skill-x"})
	_ = proc2.Start()
	k.AddProcess(proc2)

	infos := k.ListProcs()
	if len(infos) != 2 {
		t.Fatalf("ListProcs: got %d, want 2", len(infos))
	}

	pids := map[types.PID]bool{}
	for _, info := range infos {
		pids[info.PID] = true
	}
	if !pids[proc1.PID] || !pids[proc2.PID] {
		t.Errorf("ListProcs missing PIDs: got %v", pids)
	}
}

func TestListProcs_Empty(t *testing.T) {
	k := newSimpleKernel(t)

	infos := k.ListProcs()
	if len(infos) != 0 {
		t.Errorf("ListProcs: got %d, want 0", len(infos))
	}
}

func TestGetProcInfo_ConcurrentSafe(t *testing.T) {
	k := newSimpleKernel(t)

	proc := NewProcess(0, "concurrent", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			info, err := k.GetProcInfo(proc.PID)
			if err != nil {
				t.Errorf("GetProcInfo failed: %v", err)
				return
			}
			if info.PID != proc.PID {
				t.Errorf("unexpected PID: %d", info.PID)
			}
		})
	}
	wg.Wait()
}

// --- Story 13.4: GDB model override in reasonStep ---

// TestReasonStep_GdbModelOverride verifies that when a gdb model override is set
// on a process, the reasonStep loop uses the overridden model instead of opts.Model.
func TestReasonStep_GdbModelOverride(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("gdb override test", 10),
	}
	var capturedReq llmRequest
	captureLLM := &capturingLLMFile{
		inner:       llmFile,
		capturedReq: &capturedReq,
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return captureLLM, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test gdb model override", nil, SpawnOpts{Model: "original-model"})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)

	// Set gdb model override BEFORE the process completes its first step.
	// Since the mock LLM returns a text response immediately, the process
	// will complete in one step. We set the override before Spawn's goroutine
	// starts (race-safe because SetGdbModelOverride is mutex-protected).
	proc.SetGdbModelOverride("gdb-overridden-model")

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to complete")
	}

	// The captured request should use the gdb override model, not opts.Model
	if capturedReq.Model != "gdb-overridden-model" {
		t.Errorf("expected gdb-overridden model %q in LLM request, got %q", "gdb-overridden-model", capturedReq.Model)
	}
}

// TestReasonStep_GdbModelOverride_Empty verifies that when no gdb model override
// is set, the reasonStep uses opts.Model as normal.
func TestReasonStep_GdbModelOverride_Empty(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("no override test", 10),
	}
	var capturedReq llmRequest
	captureLLM := &capturingLLMFile{
		inner:       llmFile,
		capturedReq: &capturedReq,
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return captureLLM, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test no override", nil, SpawnOpts{Model: "original-model"})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	// Do NOT set gdb override — should use opts.Model

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to complete")
	}

	if capturedReq.Model != "original-model" {
		t.Errorf("expected original model %q, got %q", "original-model", capturedReq.Model)
	}
}

// TestReasonStep_GdbEnvVarsInjection verifies that gdb env vars are injected
// into the system prompt as a [GDB Environment Variables] section.
func TestReasonStep_GdbEnvVarsInjection(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("env test", 10),
	}
	var capturedReq llmRequest
	captureLLM := &capturingLLMFile{
		inner:       llmFile,
		capturedReq: &capturedReq,
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return captureLLM, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test env injection", nil, SpawnOpts{Model: "test-model", MaxTurns: 100})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	proc.SetGdbEnv("DEBUG", "true")
	proc.SetGdbEnv("LOG_LEVEL", "verbose")

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to complete")
	}

	if !strings.Contains(capturedReq.SystemPrompt, "[GDB Environment Variables]") {
		t.Error("expected system prompt to contain [GDB Environment Variables] section")
	}
	if !strings.Contains(capturedReq.SystemPrompt, "DEBUG=true") {
		t.Error("expected system prompt to contain DEBUG=true")
	}
	if !strings.Contains(capturedReq.SystemPrompt, "LOG_LEVEL=verbose") {
		t.Error("expected system prompt to contain LOG_LEVEL=verbose")
	}
}

// TestReasonStep_GdbEnvVarsEmpty verifies that no env section is injected
// when no gdb env vars are set.
func TestReasonStep_GdbEnvVarsEmpty(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("no env test", 10),
	}
	var capturedReq llmRequest
	captureLLM := &capturingLLMFile{
		inner:       llmFile,
		capturedReq: &capturedReq,
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return captureLLM, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test no env", nil, SpawnOpts{Model: "test-model"})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	// Do NOT set any env vars

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to complete")
	}

	if strings.Contains(capturedReq.SystemPrompt, "[GDB Environment Variables]") {
		t.Error("expected system prompt to NOT contain env vars section when none set")
	}
}

// --- resolveLLMDevice tests ---

// newMinimalKernelWithProviders creates a KernelImpl with mock provider resolver.
// If no providers are given, hasProvider is left nil (backward compat mode).
func newMinimalKernelWithProviders(providers ...string) *KernelImpl {
	k := &KernelImpl{}
	if len(providers) > 0 {
		providerSet := make(map[string]bool)
		for _, p := range providers {
			providerSet[p] = true
		}
		k.SetProviderResolver(
			func() []string {
				names := make([]string, 0, len(providerSet))
				for n := range providerSet {
					names = append(names, n)
				}
				sort.Strings(names)
				return names
			},
			func(name string) bool { return providerSet[name] },
		)
	}
	return k
}

func TestResolveLLMDevice_NilAgent(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	device, _, err := k.resolveLLMDevice(nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/claude" {
		t.Errorf("expected /dev/llm/claude, got %q", device)
	}
}

func TestResolveLLMDevice_EmptyProvider(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: ""},
		},
	}
	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/claude" {
		t.Errorf("expected /dev/llm/claude, got %q", device)
	}
}

func TestResolveLLMDevice_Claude(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "claude"},
		},
	}
	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/claude" {
		t.Errorf("expected /dev/llm/claude, got %q", device)
	}
}

func TestResolveLLMDevice_Cursor(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "cursor"},
		},
	}
	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/cursor" {
		t.Errorf("expected /dev/llm/cursor, got %q", device)
	}
}

func TestResolveLLMDevice_Unsupported(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "nonexistent"},
		},
	}
	_, _, err := k.resolveLLMDevice(agent, "")
	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported LLM provider") {
		t.Errorf("expected 'unsupported LLM provider' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "(available:") {
		t.Errorf("expected '(available:' in error, got: %v", err)
	}
}

func TestResolveLLMDevice_PathTraversal(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	tests := []string{"../fs", "claude/../../shell"}
	for _, provider := range tests {
		agent := &agents.AgentInfo{
			Manifest: agents.AgentManifest{
				Models: agents.AgentModels{Provider: provider},
			},
		}
		_, _, err := k.resolveLLMDevice(agent, "")
		if err == nil {
			t.Errorf("expected error for provider %q, got nil", provider)
		}
	}
}

func TestResolveLLMDevice_OverrideAgent(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	// Agent says "claude", but CLI override says "cursor"
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "claude"},
		},
	}
	device, _, err := k.resolveLLMDevice(agent, "cursor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/cursor" {
		t.Errorf("expected /dev/llm/cursor, got %q", device)
	}
}

func TestResolveLLMDevice_OverrideNoAgent(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	// No agent, but CLI override says "cursor"
	device, _, err := k.resolveLLMDevice(nil, "cursor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/cursor" {
		t.Errorf("expected /dev/llm/cursor, got %q", device)
	}
}

func TestResolveLLMDevice_OverrideUnsupported(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor")
	_, _, err := k.resolveLLMDevice(nil, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unsupported override provider, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported LLM provider") {
		t.Errorf("expected 'unsupported LLM provider' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "(available:") {
		t.Errorf("expected '(available:' in error, got: %v", err)
	}
}

// --- New tests for Story 23.3 ---

func TestResolveLLMDevice_DynamicProvider(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor", "ollama")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "ollama"},
		},
	}
	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/ollama" {
		t.Errorf("expected /dev/llm/ollama, got %q", device)
	}
}

func TestResolveLLMDevice_UnsupportedListsAvailable(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "cursor", "ollama")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "nonexist"},
		},
	}
	_, _, err := k.resolveLLMDevice(agent, "")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "(available: claude, cursor, ollama)") {
		t.Errorf("expected '(available: claude, cursor, ollama)' in error, got: %s", errMsg)
	}
}

func TestResolveLLMDevice_NilResolver(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{} // No resolver injected
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "anything"},
		},
	}
	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("nil resolver should allow all providers, got error: %v", err)
	}
	if device != "/dev/llm/anything" {
		t.Errorf("expected /dev/llm/anything, got %q", device)
	}
}

func TestResolveLLMDevice_OverrideDynamic(t *testing.T) {
	t.Parallel()
	k := newMinimalKernelWithProviders("claude", "groq")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "claude"},
		},
	}
	device, _, err := k.resolveLLMDevice(agent, "groq")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device != "/dev/llm/groq" {
		t.Errorf("expected /dev/llm/groq, got %q", device)
	}
}

// --- SetProviderResolver tests ---

func TestSetProviderResolver(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	if k.providerNames != nil || k.hasProvider != nil {
		t.Fatal("expected nil providerNames and hasProvider before SetProviderResolver")
	}
	k.SetProviderResolver(
		func() []string { return []string{"claude"} },
		func(name string) bool { return name == "claude" },
	)
	if k.providerNames == nil || k.hasProvider == nil {
		t.Fatal("expected non-nil providerNames and hasProvider after SetProviderResolver")
	}
}

func TestSetProviderResolver_NamesCallable(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor", "ollama"} },
		func(name string) bool { return true },
	)
	names := k.providerNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 provider names, got %d", len(names))
	}
	expected := []string{"claude", "cursor", "ollama"}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("expected names[%d]=%q, got %q", i, expected[i], n)
		}
	}
}

func TestSetProviderResolver_HasProviderCallable(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	k.SetProviderResolver(
		func() []string { return []string{"claude"} },
		func(name string) bool { return name == "claude" },
	)
	if !k.hasProvider("claude") {
		t.Error("expected hasProvider('claude') to return true")
	}
	if k.hasProvider("nonexist") {
		t.Error("expected hasProvider('nonexist') to return false")
	}
}

// --- SetDefaultProvider tests ---

func TestSetDefaultProvider(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	if k.defaultProvider != "" {
		t.Fatalf("expected empty defaultProvider before SetDefaultProvider, got %q", k.defaultProvider)
	}
	k.SetDefaultProvider("groq")
	if k.defaultProvider != "groq" {
		t.Errorf("expected defaultProvider = %q, got %q", "groq", k.defaultProvider)
	}
}

func TestResolveLLMDevice_UsesDefaultProvider(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	k.SetDefaultProvider("groq")
	// No provider resolver → allow all (backward compat)
	got, _, err := k.resolveLLMDevice(nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/dev/llm/groq" {
		t.Errorf("expected /dev/llm/groq, got %q", got)
	}
}

func TestResolveLLMDevice_DefaultProviderOverriddenByAgent(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	k.SetDefaultProvider("groq")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "ollama"},
		},
	}
	got, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/dev/llm/ollama" {
		t.Errorf("expected /dev/llm/ollama (agent manifest), got %q", got)
	}
}

func TestResolveLLMDevice_DefaultProviderOverriddenBySpawnOpts(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	k.SetDefaultProvider("groq")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "ollama"},
		},
	}
	got, _, err := k.resolveLLMDevice(agent, "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/dev/llm/claude" {
		t.Errorf("expected /dev/llm/claude (SpawnOpts override), got %q", got)
	}
}

func TestResolveLLMDevice_NoDefaultProvider_FallsBackToClaude(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{}
	// defaultProvider is "" (zero value), no setter called
	got, _, err := k.resolveLLMDevice(nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/dev/llm/claude" {
		t.Errorf("expected /dev/llm/claude (backward compat), got %q", got)
	}
}

// ============================================================
// Story 25-3: Project Config Merge & Module Adaptation
//
// Tests verify ProjectConfig propagation through kernel.Spawn.
// ============================================================

// --- 25.3-KERN-001: SpawnOpts.ProjectConfig is set on spawned process ---

func TestSpawn_ProjectConfig_PassedToProcess(t *testing.T) {
	k := newSimpleKernel(t)

	projCfg := &config.ProjectConfig{
		ProjectDir: "/home/user/test-project",
		AgentDirs:  []string{"/home/user/test-project/.rnix/agents", "/global/agents"},
		SkillDirs:  []string{"/home/user/test-project/.rnix/skills", "/global/skills"},
	}

	pid, err := k.Spawn("project-aware intent", nil, SpawnOpts{
		SkipReasonLoop: true,
		ProjectConfig:  projCfg,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if pid == 0 {
		t.Fatal("Spawn returned PID 0")
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}

	// Verify ProjectConfig was propagated to the process
	if proc.ProjectConfig == nil {
		t.Fatal("process.ProjectConfig should be non-nil when set via SpawnOpts")
	}
	if proc.ProjectConfig.ProjectDir != "/home/user/test-project" {
		t.Errorf("ProjectConfig.ProjectDir = %q, want %q", proc.ProjectConfig.ProjectDir, "/home/user/test-project")
	}
	if len(proc.ProjectConfig.AgentDirs) != 2 {
		t.Fatalf("ProjectConfig.AgentDirs length = %d, want 2", len(proc.ProjectConfig.AgentDirs))
	}
	if proc.ProjectConfig.AgentDirs[0] != "/home/user/test-project/.rnix/agents" {
		t.Errorf("ProjectConfig.AgentDirs[0] = %q, want project dir first", proc.ProjectConfig.AgentDirs[0])
	}
	if len(proc.ProjectConfig.SkillDirs) != 2 {
		t.Fatalf("ProjectConfig.SkillDirs length = %d, want 2", len(proc.ProjectConfig.SkillDirs))
	}

	// Verify the pointer identity (should be the exact same pointer)
	if proc.ProjectConfig != projCfg {
		t.Error("ProjectConfig should be the same pointer instance set via SpawnOpts")
	}
}

// --- 25.3-KERN-002: SpawnOpts.ProjectConfig nil gives nil on process ---

func TestSpawn_ProjectConfig_NilWhenNotSet(t *testing.T) {
	k := newSimpleKernel(t)

	pid, err := k.Spawn("global intent", nil, SpawnOpts{
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}

	if proc.ProjectConfig != nil {
		t.Errorf("ProjectConfig should be nil when not set in SpawnOpts, got %+v", proc.ProjectConfig)
	}
}

func TestExtractContentText(t *testing.T) {
	tests := []struct {
		name    string
		content any
		want    string
	}{
		{"nil content", nil, ""},
		{"string content", "hello world", "hello world"},
		{"empty string", "", ""},
		{"map text", map[string]any{"type": "text", "text": "from map"}, "from map"},
		{"map non-text type", map[string]any{"type": "image", "data": "xxx"}, ""},
		{"map missing text", map[string]any{"type": "text"}, ""},
		{"[]map single text", []map[string]any{{"type": "text", "text": "block1"}}, "block1"},
		{"[]map multiple text", []map[string]any{{"type": "text", "text": "a"}, {"type": "text", "text": "b"}}, "a\nb"},
		{"[]map mixed types", []map[string]any{{"type": "text", "text": "ok"}, {"type": "image", "data": "img"}}, "ok"},
		{"[]map nil block", []map[string]any{nil, {"type": "text", "text": "safe"}}, "safe"},
		{"[]map empty text skipped", []map[string]any{{"type": "text", "text": ""}}, ""},
		{"[]any single text", []any{map[string]any{"type": "text", "text": "from any"}}, "from any"},
		{"[]any multiple text", []any{map[string]any{"type": "text", "text": "x"}, map[string]any{"type": "text", "text": "y"}}, "x\ny"},
		{"[]any non-map item skipped", []any{"plain string", map[string]any{"type": "text", "text": "ok"}}, "ok"},
		{"[]any nil item skipped", []any{nil, map[string]any{"type": "text", "text": "ok"}}, "ok"},
		{"empty slice []map", []map[string]any{}, ""},
		{"empty slice []any", []any{}, ""},
		{"int content returns empty", 42, ""},
		{"bool content returns empty", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractContentText(tt.content)
			if got != tt.want {
				t.Errorf("extractContentText(%v) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}
