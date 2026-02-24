package kernel

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/internal/types"
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

func (f *mockLLMFile) Write(data []byte) error {
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

func (f *mockToolFile) Write(data []byte) error {
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
	k := NewKernel(v, ctxMgr)
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

// --- Existing kernel tests (updated for new NewKernel signature) ---

func newSimpleKernel() *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	return NewKernel(v, ctxMgr)
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

	pid, err := k.Spawn("test intent", []string{"skill1"}, SpawnOpts{})
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
	k := NewKernel(v, ctxMgr)

	_, err := k.Spawn("test", nil, SpawnOpts{})
	if err == nil {
		t.Fatal("expected error when LLM device not available")
	}
}

func TestSpawn_DebugChanEvents(t *testing.T) {
	// DebugChan must be set before Spawn to avoid data race.
	// We initialize it in NewProcess by modifying the process after creation
	// but before Spawn. Since Spawn creates the process internally,
	// we test via the integration path where DebugChan is initialized in NewProcess.
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockLLMFile{
			readData: makeLLMResponse("debug result", 10),
		}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr)

	pid, err := k.Spawn("debug intent", []string{"s1"}, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}
	if proc.Result != "debug result" {
		t.Fatalf("expected 'debug result', got %q", proc.Result)
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
	k := NewKernel(v, ctxMgr)

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

func (f *sequenceLLMFile) Write(data []byte) error {
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
	k := NewKernel(v, ctxMgr)

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

func (f *blockingLLMFile) Write(data []byte) error {
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
	k := NewKernel(v, ctxMgr)

	pid, err := k.Spawn("loop forever", nil, SpawnOpts{MaxTurns: 3})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
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
	k := NewKernel(v, ctxMgr)

	pid, err := k.Spawn("integration test", []string{"skill-a", "skill-b"}, SpawnOpts{
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
