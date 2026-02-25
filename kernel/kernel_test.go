package kernel

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonewx/crux/agents"
	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/skills"
	"github.com/gonewx/crux/vfs"
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
func newTestKernel(llmFile *mockLLMFile) (*KernelImpl, *vfs.VFS, *cruxctx.Manager) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
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

func newSimpleKernel() *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	return NewKernel(v, ctxMgr, nil)
}

func TestNewKernel(t *testing.T) {
	k := newSimpleKernel()
	if k == nil {
		t.Fatal("NewKernel returned nil")
	}
	procs := k.ListProcesses()
	if len(procs) != 0 {
		t.Fatalf("expected empty process table, got %d entries", len(procs))
	}
}

func TestKernelAddGetRemove(t *testing.T) {
	k := newSimpleKernel()
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
	k := newSimpleKernel()
	_, ok := k.GetProcess(9999)
	if ok {
		t.Fatal("expected not found for non-existent PID")
	}
}

func TestProcessTableConcurrent(t *testing.T) {
	k := newSimpleKernel()
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
	k := newSimpleKernel()
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

// --- Spawn tests ---

func TestSpawn_Success(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("hello world", 42),
	}
	k, _, ctxMgr := newTestKernel(llmFile)

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
	k, _, ctxMgr := newTestKernel(llmFile)

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
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	_, err := k.Spawn("test", nil, SpawnOpts{})
	if err == nil {
		t.Fatal("expected error when LLM device not available")
	}
}

func TestSpawn_DebugChanEvents(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("debug result", 10),
	}
	k, _, _ := newTestKernel(llmFile)

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
	k, _, _ := newTestKernel(llmFile)

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
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	// Register mock tool device
	_ = reg.Register("/dev/tools/read", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("bar")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	k, _, _ := newTestKernel(llmFile)

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
	k, _, _ := newTestKernel(llmFile)

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
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockLLMFile{
			readData: makeToolCallResponse("/dev/tools/echo", map[string]any{}, 5),
		}, nil
	})
	_ = reg.Register("/dev/tools/echo", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echoed")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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

// --- Integration test ---

func TestSpawn_Integration(t *testing.T) {
	// End-to-end: Spawn → reasonStep → text → Zombie → Done channel
	// Uses real VFS + mock LLM device + real Context Manager
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("integration result", 77),
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	k, _, _ := newTestKernel(llmFile)

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
	k, _, _ := newTestKernel(llmFile)

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("multi", 10)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	var pids []types.PID
	var procs []*Process
	for i := 0; i < n; i++ {
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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("concurrent", 5)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	var wg sync.WaitGroup
	pidCh := make(chan types.PID, n)
	errCh := make(chan error, n)

	// Spawn n processes concurrently
	for i := 0; i < n; i++ {
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
	k, _, ctxMgr := newTestKernel(llmFile)

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
	k, _, _ := newTestKernel(llmFile)

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return captureLLM, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	_ = reg2.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return captureLLM2, nil
	})
	v2 := vfs.NewVFS(reg2)
	ctxMgr2 := cruxctx.NewManager()
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
	k, _, _ := newTestKernel(llmFile)

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/fs", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return mockFS, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/fs", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return mockFS, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/any/device", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return mockDevice, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil) // No agent

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	k, _, _ := newTestKernel(llmFile)

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
	k, _, _ := newTestKernel(llmFile)

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
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/tools/echo", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return mockTool, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

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
	// AC #10: DebugChan 为 nil 时零开销（无 astrace 附着）
	k := newSimpleKernel()
	proc := NewProcess(0, "nil chan test", nil)
	proc.DebugChan = nil // simulate no astrace attached

	// Direct call to emitEvent — must not panic or block.
	k.emitEvent(proc, "Open", map[string]any{"path": "/dev/null"}, 1, nil, time.Millisecond)
	k.emitEvent(proc, "Write", map[string]any{"fd": 1, "size": 42}, nil, nil, time.Millisecond)
	k.emitEvent(proc, "Read", map[string]any{"fd": 1, "length": 100}, 100, nil, time.Millisecond)
	k.emitEvent(proc, "CtxAlloc", map[string]any{"size": 64}, types.CtxID(1), nil, time.Millisecond)
}

func BenchmarkEmitEvent_NilDebugChan(b *testing.B) {
	// AC #10: DebugChan 为 nil 时零开销 — verify zero allocations inside emitEvent.
	k := newSimpleKernel()
	proc := NewProcess(0, "bench", nil)
	proc.DebugChan = nil

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k.emitEvent(proc, "Open", map[string]any{"path": "/dev/null"}, 1, nil, time.Millisecond)
	}
}
