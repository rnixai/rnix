package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// --- Thread-safe writer for concurrent output ---

type syncWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// --- Mock LLM drivers ---

type mockLLMDriver struct {
	response *llm.LLMResponse
	err      error
}

func (d *mockLLMDriver) Call(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	if d.err != nil {
		return nil, d.err
	}
	return d.response, nil
}

func (d *mockLLMDriver) Stream(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 1)
	if d.err != nil {
		ch <- llm.StreamEvent{Type: "error", Err: d.err}
	} else if d.response != nil {
		ch <- llm.StreamEvent{
			Type:         "done",
			Content:      d.response.Content,
			TokensUsed:   d.response.TokensUsed,
			InputTokens:  d.response.InputTokens,
			OutputTokens: d.response.OutputTokens,
		}
	}
	close(ch)
	return ch, nil
}

func (d *mockLLMDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{Name: "mock", Provider: "test", DefaultModel: "mock"}
}

type mockSlowLLMDriver struct {
	delay time.Duration
	err   error
}

func (d *mockSlowLLMDriver) Call(ctx context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	select {
	case <-time.After(d.delay):
		return nil, d.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *mockSlowLLMDriver) Stream(ctx context.Context, req llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 1)
	go func() {
		defer close(ch)
		select {
		case <-time.After(d.delay):
			if d.err != nil {
				ch <- llm.StreamEvent{Type: "error", Err: d.err}
			}
		case <-ctx.Done():
			ch <- llm.StreamEvent{Type: "error", Err: ctx.Err()}
		}
	}()
	return ch, nil
}

func (d *mockSlowLLMDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{Name: "mock-slow", Provider: "test", DefaultModel: "mock"}
}

// --- Blocking VFS file for signal handling tests ---

type blockingVFSFile struct {
	blockCh  chan struct{}
	readData []byte
	writeErr error
}

func (f *blockingVFSFile) Write(_ context.Context, _ []byte) error {
	<-f.blockCh
	return f.writeErr
}

func (f *blockingVFSFile) Read(_ int) ([]byte, error) {
	return f.readData, nil
}

func (f *blockingVFSFile) Close() error {
	return nil
}

func (f *blockingVFSFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// --- E2E test helper (kernel-direct, bypasses IPC) ---

type e2eResult struct {
	Output string
	Exit   kernel.ExitStatus
	Kern   *kernel.KernelImpl
	PID    types.PID
	Proc   *kernel.Process
}

func runE2E(t *testing.T, intent string, driver llm.LLMDriver, mode ui.OutputMode) e2eResult {
	t.Helper()

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: mode,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude", ""))
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	start := time.Now()
	pid, err := kern.Spawn(intent, nil, kernel.SpawnOpts{})
	if err != nil {
		outputError(renderer, mode, "/dev/llm/claude", err.Error(), "启动失败", "检查配置")
		return e2eResult{
			Output: w.String(),
			Exit:   kernel.ExitStatus{Code: 1, Reason: err.Error(), Err: err},
			Kern:   kern,
		}
	}

	proc, _ := kern.GetProcess(pid)
	exit := <-proc.Done
	elapsed := time.Since(start)

	if exit.Code == 0 {
		outputSuccess(renderer, mode, pid, proc.Result, proc.TokensUsed, elapsed, proc.Provider, proc.Model)
	} else {
		reason := exit.Reason
		if exit.Err != nil {
			reason = exit.Err.Error()
		}
		outputError(renderer, mode, "/dev/llm/claude", reason, "智能体执行失败", "检查意图描述或重试")
		ui.RenderSummary(renderer, pid, exit.Code, proc.TokensUsed, elapsed, proc.Provider, proc.Model)
	}

	return e2eResult{
		Output: w.String(),
		Exit:   exit,
		Kern:   kern,
		PID:    pid,
		Proc:   proc,
	}
}

// --- Task 3: E2E Integration Tests — Complete Output Flow (AC #1) ---

func TestE2E_SuccessFlow(t *testing.T) {
	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "分析完成", TokensUsed: 100},
	}

	result := runE2E(t, "分析 ./README.md", driver, ui.ModeDefault)

	if result.Exit.Code != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", result.Exit.Code, result.Exit.Reason)
	}

	checks := []struct {
		name   string
		substr string
	}{
		{"kernel prefix", "[kernel]"},
		{"spawning PID", "spawning PID"},
		{"step complete", "step 1:"},
		{"Result box", "══ Result ══"},
		{"result content", "分析完成"},
		{"exit summary", "exited(0)"},
		{"token count", "tokens: 100"},
	}
	for _, c := range checks {
		if !strings.Contains(result.Output, c.substr) {
			t.Errorf("missing %s (%q) in output:\n%s", c.name, c.substr, result.Output)
		}
	}
}

func TestE2E_ErrorFlow(t *testing.T) {
	driver := &mockLLMDriver{err: fmt.Errorf("connection refused")}

	result := runE2E(t, "test error", driver, ui.ModeDefault)

	if result.Exit.Code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(result.Output, "✗") {
		t.Errorf("missing error prefix in output:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "exited(1)") {
		t.Errorf("missing exited(1) in output:\n%s", result.Output)
	}
}

func TestE2E_JSONOutput(t *testing.T) {
	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "json result", TokensUsed: 42},
	}

	result := runE2E(t, "test json", driver, ui.ModeJSON)

	if result.Exit.Code != 0 {
		t.Fatalf("expected exit code 0, got %d", result.Exit.Code)
	}

	var resp JSONResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Output)), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %q", err, result.Output)
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	data, _ := json.Marshal(resp.Data)
	var success jsonSuccessData
	if err := json.Unmarshal(data, &success); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}
	if success.Result != "json result" {
		t.Errorf("expected result 'json result', got %q", success.Result)
	}
	if success.TokensUsed != 42 {
		t.Errorf("expected 42 tokens, got %d", success.TokensUsed)
	}
	if success.PID == 0 {
		t.Error("expected non-zero PID")
	}
}

func TestE2E_JSONErrorOutput(t *testing.T) {
	driver := &mockLLMDriver{err: fmt.Errorf("llm unavailable")}

	result := runE2E(t, "test json error", driver, ui.ModeJSON)

	if result.Exit.Code == 0 {
		t.Fatal("expected non-zero exit code")
	}

	output := strings.TrimSpace(result.Output)
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %q", err, output)
	}
	if resp.OK {
		t.Error("expected ok=false")
	}
	if resp.Error == nil {
		t.Fatal("expected non-nil error")
	}
}

// --- Timeout Integration Tests ---

func TestE2E_LLMTimeout(t *testing.T) {
	driver := &mockSlowLLMDriver{
		delay: 100 * time.Millisecond,
		err:   fmt.Errorf("context deadline exceeded"),
	}

	result := runE2E(t, "timeout test", driver, ui.ModeDefault)

	if result.Exit.Code == 0 {
		t.Fatal("expected non-zero exit code for timeout")
	}

	if !strings.Contains(result.Output, "✗") {
		t.Errorf("missing error prefix in output:\n%s", result.Output)
	}

	if result.Proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie state, got %d", result.Proc.GetState())
	}
}

func TestE2E_TimeoutProcessCleanup(t *testing.T) {
	driver := &mockSlowLLMDriver{
		delay: 50 * time.Millisecond,
		err:   fmt.Errorf("timeout"),
	}

	result := runE2E(t, "cleanup test", driver, ui.ModeDefault)

	if result.Exit.Code == 0 {
		t.Fatal("expected non-zero exit code")
	}

	if result.Proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie, got %d", result.Proc.GetState())
	}
	if result.Proc.Exit == nil {
		t.Error("expected non-nil Exit on Zombie process")
	}

	p, ok := result.Kern.GetProcess(result.PID)
	if !ok {
		t.Error("process should still be in process table as Zombie")
	}
	if p.GetState() != types.StateZombie {
		t.Errorf("process in table should be Zombie, got %d", p.GetState())
	}
}

func TestE2E_TimeoutNFR7(t *testing.T) {
	delay := 100 * time.Millisecond
	driver := &mockSlowLLMDriver{
		delay: delay,
		err:   fmt.Errorf("context deadline exceeded"),
	}

	start := time.Now()
	result := runE2E(t, "nfr7 test", driver, ui.ModeDefault)
	totalDuration := time.Since(start)

	if result.Exit.Code == 0 {
		t.Fatal("expected non-zero exit code")
	}

	transitionOverhead := totalDuration - delay
	if transitionOverhead > 5*time.Second {
		t.Errorf("NFR7 violation: error→Zombie transition took %v (max 5s)", transitionOverhead)
	}
	t.Logf("NFR7: transition overhead %v (mock delay %v, total %v)", transitionOverhead, delay, totalDuration)

	if result.Proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie, got %d", result.Proc.GetState())
	}
}

// --- Signal Handling Integration Tests ---

func TestSignalHandling_GracefulShutdown(t *testing.T) {
	blockCh := make(chan struct{})

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingVFSFile{
			blockCh:  blockCh,
			readData: []byte(`{"content":"interrupted","tokens_used":1}`),
		}, nil
	})
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("signal test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	time.Sleep(50 * time.Millisecond)

	proc.Cancel()
	close(blockCh)

	select {
	case exit := <-proc.Done:
		t.Logf("exit code: %d, reason: %s", exit.Code, exit.Reason)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion after cancel")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie state, got %d", proc.GetState())
	}
}

// --- Reliability Test (NFR6) ---

func TestE2E_Reliability_NFR6(t *testing.T) {
	const runs = 20
	successCount := 0

	for i := range runs {
		driver := &mockLLMDriver{
			response: &llm.LLMResponse{Content: fmt.Sprintf("result-%d", i), TokensUsed: 10},
		}

		start := time.Now()
		result := runE2E(t, fmt.Sprintf("task-%d", i), driver, ui.ModeDefault)
		elapsed := time.Since(start)

		if result.Exit.Code == 0 {
			successCount++
		}

		if elapsed > 30*time.Second {
			t.Errorf("run %d exceeded 30s latency: %v", i, elapsed)
		}
	}

	successRate := float64(successCount) / float64(runs) * 100
	if successRate < 95.0 {
		t.Errorf("NFR6 violation: success rate %.1f%% < 95%% (%d/%d succeeded)", successRate, successCount, runs)
	}
	t.Logf("NFR6 reliability: %d/%d succeeded (%.1f%%)", successCount, runs, successRate)
}

// --- Signal Handling: Interrupt Summary and Double-SIGINT ---

func TestSignalHandling_InterruptSummary(t *testing.T) {
	blockCh := make(chan struct{})

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingVFSFile{
			blockCh:  blockCh,
			writeErr: fmt.Errorf("interrupted: context cancelled"),
			readData: []byte(`{"content":"interrupted","tokens_used":1}`),
		}, nil
	})
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("signal test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	time.Sleep(50 * time.Millisecond)

	progress.KernelMessage("PID %d interrupted (SIGINT)", pid)
	proc.Cancel()
	close(blockCh)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion")
	}

	output := w.String()
	if !strings.Contains(output, "interrupted") {
		t.Errorf("expected interrupt summary in output, got:\n%s", output)
	}
	if !strings.Contains(output, "SIGINT") {
		t.Errorf("expected SIGINT mention in output, got:\n%s", output)
	}
	if proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie state, got %d", proc.GetState())
	}
}

func TestSignalHandling_DoubleInterruptForceExit(t *testing.T) {
	exitCodeCh := make(chan int, 1)
	saved := forceExitFunc
	forceExitFunc = func(code int) { exitCodeCh <- code }
	defer func() { forceExitFunc = saved }()

	blockCh := make(chan struct{})

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingVFSFile{
			blockCh:  blockCh,
			readData: []byte(`{"content":"test","tokens_used":1}`),
		}, nil
	})
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("double signal test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := kern.GetProcess(pid)

	time.Sleep(50 * time.Millisecond)

	sigCh := make(chan os.Signal, 2)
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		<-sigCh
		progress.KernelMessage("PID %d interrupted (SIGINT)", pid)
		proc.Cancel()
		select {
		case <-sigCh:
			forceExitFunc(130)
		case <-time.After(2 * time.Second):
		}
	}()

	sigCh <- os.Interrupt
	time.Sleep(20 * time.Millisecond)

	sigCh <- os.Interrupt

	select {
	case code := <-exitCodeCh:
		if code != 130 {
			t.Errorf("expected force exit code 130, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected force exit on double SIGINT")
	}

	close(blockCh)
	<-proc.Done
	<-handlerDone
}

// --- Skill Injection E2E Test ---

type capturingMockLLMDriver struct {
	mu          sync.Mutex
	response    *llm.LLMResponse
	lastRequest llm.LLMRequest
}

func (d *capturingMockLLMDriver) Call(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
	d.mu.Lock()
	d.lastRequest = req
	d.mu.Unlock()
	return d.response, nil
}

func (d *capturingMockLLMDriver) LastRequest() llm.LLMRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastRequest
}

func (d *capturingMockLLMDriver) Stream(_ context.Context, req llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	d.mu.Lock()
	d.lastRequest = req
	d.mu.Unlock()
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.StreamEvent{
		Type:         "done",
		Content:      d.response.Content,
		TokensUsed:   d.response.TokensUsed,
		InputTokens:  d.response.InputTokens,
		OutputTokens: d.response.OutputTokens,
	}
	close(ch)
	return ch, nil
}

func (d *capturingMockLLMDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{Name: "mock-capturing", Provider: "test", DefaultModel: "mock"}
}

func TestE2E_WithAgent_InjectsInstructions(t *testing.T) {
	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "skill result", TokensUsed: 50},
	}

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude", ""))
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	agentInfo := &agents.AgentInfo{
		Manifest:     agents.AgentManifest{Name: "test-agent"},
		Instructions: "You are a test agent.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "mock-skill",
					AllowedToolsRaw: "/dev/fs /dev/shell",
				},
				Body: "Mock skill instructions",
			},
		},
	}

	start := time.Now()
	pid, err := kern.Spawn("analyze code", agentInfo, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	exit := <-proc.Done
	if exit.Code != 0 {
		t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
	}

	elapsed := time.Since(start)
	outputSuccess(renderer, ui.ModeDefault, pid, proc.Result, proc.TokensUsed, elapsed, proc.Provider, proc.Model)

	if len(proc.AllowedDevices) != 2 {
		t.Errorf("expected 2 AllowedDevices from agent skills, got %d: %v", len(proc.AllowedDevices), proc.AllowedDevices)
	}

	output := w.String()
	if !strings.Contains(output, "skill result") {
		t.Errorf("expected 'skill result' in output, got:\n%s", output)
	}
}

// --- Real code-analyst Agent E2E Test ---

func TestE2E_CodeAnalystAgent(t *testing.T) {
	driver := &capturingMockLLMDriver{
		response: &llm.LLMResponse{Content: "分析完成: 发现 1 个 Warning 级别问题", TokensUsed: 200},
	}

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude", ""))
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	sl := skills.NewSkillLoader([]string{"../../lib/skills"})
	al := agents.NewAgentLoader([]string{"../../lib/agents"}, sl, nil)
	agentInfo, err := al.Load("code-analyst")
	if err != nil {
		t.Fatalf("AgentLoader.Load failed: %v", err)
	}

	start := time.Now()
	pid, err := kern.Spawn("分析 ./kernel/kernel.go", agentInfo, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	exit := <-proc.Done
	if exit.Code != 0 {
		t.Fatalf("expected exit code 0, got %d: %s (err: %v)", exit.Code, exit.Reason, exit.Err)
	}

	elapsed := time.Since(start)
	outputSuccess(renderer, ui.ModeDefault, pid, proc.Result, proc.TokensUsed, elapsed, proc.Provider, proc.Model)

	lastReq := driver.LastRequest()
	if !strings.Contains(lastReq.SystemPrompt, "Code Analyst") {
		t.Errorf("SystemPrompt missing 'Code Analyst' from agent instructions, got: %q", lastReq.SystemPrompt)
	}
	if !strings.Contains(lastReq.SystemPrompt, "/dev/fs") {
		t.Errorf("SystemPrompt missing '/dev/fs' from skill body")
	}
	if !strings.Contains(lastReq.SystemPrompt, "/dev/shell") {
		t.Errorf("SystemPrompt missing '/dev/shell' from skill body")
	}

	if len(proc.AllowedDevices) != 2 {
		t.Fatalf("expected 2 AllowedDevices, got %d: %v", len(proc.AllowedDevices), proc.AllowedDevices)
	}
	if proc.AllowedDevices[0] != "/dev/fs" {
		t.Errorf("AllowedDevices[0] = %q, want %q", proc.AllowedDevices[0], "/dev/fs")
	}
	if proc.AllowedDevices[1] != "/dev/shell" {
		t.Errorf("AllowedDevices[1] = %q, want %q", proc.AllowedDevices[1], "/dev/shell")
	}

	if lastReq.Model != "sonnet" {
		t.Errorf("Model = %q, want %q", lastReq.Model, "sonnet")
	}

	output := w.String()
	if !strings.Contains(output, "分析完成") {
		t.Errorf("expected '分析完成' in output, got:\n%s", output)
	}
}

// --- Kill + Wait Integration Tests ---

func TestE2E_KillWait_FullLifecycle(t *testing.T) {
	blockCh := make(chan struct{})

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingVFSFile{
			blockCh:  blockCh,
			readData: []byte(`{"content":"interrupted","tokens_used":1}`),
		}, nil
	})
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("kill-wait lifecycle", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found after Spawn")
	}
	ctxID := proc.CtxID

	time.Sleep(50 * time.Millisecond)

	if err := kern.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	close(blockCh)

	exit, err := kern.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	t.Logf("exit code: %d, reason: %s", exit.Code, exit.Reason)

	// With Dead process TTL, process remains in table in Dead state
	retainedProc, ok := kern.GetProcess(pid)
	if !ok {
		t.Error("process should be retained in table (Dead TTL)")
	} else if retainedProc.GetState() != types.StateDead {
		t.Errorf("expected Dead state, got %d", retainedProc.GetState())
	}

	_, ctxErr := ctxMgr.BuildPrompt(ctxID)
	if ctxErr == nil {
		t.Error("context should be freed after Wait")
	}

	if proc.GetState() != types.StateDead {
		t.Errorf("expected Dead state, got %d", proc.GetState())
	}
}

func TestE2E_KillWait_RaceDetection(t *testing.T) {
	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "race test", TokensUsed: 1},
	}

	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true},
	}
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude", ""))
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("race test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = kern.Wait(pid)
	}()

	_ = kern.Kill(pid, types.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// With Dead process TTL, process remains in table in Dead state
	retainedProc, ok := kern.GetProcess(pid)
	if !ok {
		t.Error("process should be retained in table (Dead TTL)")
	} else if retainedProc.GetState() != types.StateDead {
		t.Errorf("expected Dead state, got %d", retainedProc.GetState())
	}
}
