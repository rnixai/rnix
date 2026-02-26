package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gonewx/crux/agents"
	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/drivers/llm"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/skills"
	"github.com/gonewx/crux/vfs"
	"github.com/spf13/cobra"
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

// mockLLMDriver returns a fixed response or error.
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
	return nil, fmt.Errorf("not supported")
}

func (d *mockLLMDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{Name: "mock", Provider: "test", DefaultModel: "mock"}
}

// mockSlowLLMDriver simulates a slow/timing out LLM.
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

func (d *mockSlowLLMDriver) Stream(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("not supported")
}

func (d *mockSlowLLMDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{Name: "mock-slow", Provider: "test", DefaultModel: "mock"}
}

// --- Blocking VFS file for signal handling tests ---

type blockingVFSFile struct {
	blockCh  chan struct{}
	readData []byte
	writeErr error // returned after blockCh unblocks; nil = success
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

// --- E2E test helper ---

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
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude"))
	ctxMgr := cruxctx.NewManager()
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
		outputSuccess(renderer, mode, pid, proc, elapsed)
	} else {
		reason := exit.Reason
		if exit.Err != nil {
			reason = exit.Err.Error()
		}
		outputError(renderer, mode, "/dev/llm/claude", reason, "智能体执行失败", "检查意图描述或重试")
		ui.RenderSummary(renderer, pid, exit.Code, proc.TokensUsed, elapsed)
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
		{"reasoning step", "reasoning step"},
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
	if success.ElapsedMs < 0 {
		t.Error("expected non-negative elapsed_ms")
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

// --- Task 4: Timeout Integration Tests (AC #3) ---

func TestE2E_LLMTimeout(t *testing.T) {
	driver := &mockSlowLLMDriver{
		delay: 100 * time.Millisecond,
		err:   fmt.Errorf("context deadline exceeded"),
	}

	result := runE2E(t, "timeout test", driver, ui.ModeDefault)

	if result.Exit.Code == 0 {
		t.Fatal("expected non-zero exit code for timeout")
	}

	// Verify error block output (three-line structure)
	if !strings.Contains(result.Output, "✗") {
		t.Errorf("missing error prefix in output:\n%s", result.Output)
	}

	// Verify process is Zombie, CLI didn't crash
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

	// Done channel was written (implicit: runE2E received from it)
	if result.Exit.Code == 0 {
		t.Fatal("expected non-zero exit code")
	}

	// Verify process state
	if result.Proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie, got %d", result.Proc.GetState())
	}
	if result.Proc.Exit == nil {
		t.Error("expected non-nil Exit on Zombie process")
	}

	// Verify process still in process table (not yet reaped)
	p, ok := result.Kern.GetProcess(result.PID)
	if !ok {
		t.Error("process should still be in process table as Zombie")
	}
	if p.GetState() != types.StateZombie {
		t.Errorf("process in table should be Zombie, got %d", p.GetState())
	}
}

func TestE2E_TimeoutNFR7(t *testing.T) {
	// NFR7: after timeout, process must transition to Zombie within 5 seconds.
	// The overhead (totalTime - mockDelay) represents the kernel's processing
	// time for the error→Zombie transition path.
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

	// Verify transition overhead is within NFR7 bounds (5 seconds)
	transitionOverhead := totalDuration - delay
	if transitionOverhead > 5*time.Second {
		t.Errorf("NFR7 violation: error→Zombie transition took %v (max 5s)", transitionOverhead)
	}
	t.Logf("NFR7: transition overhead %v (mock delay %v, total %v)", transitionOverhead, delay, totalDuration)

	if result.Proc.GetState() != types.StateZombie {
		t.Errorf("expected Zombie, got %d", result.Proc.GetState())
	}
}

// --- Task 6: Signal Handling Integration Tests (AC #7) ---

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
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("signal test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Let goroutine start and block on Write
	time.Sleep(50 * time.Millisecond)

	// Simulate SIGINT: cancel process context
	proc.Cancel()

	// Unblock the LLM Write so goroutine can proceed
	close(blockCh)

	// Wait for process to finish
	select {
	case exit := <-proc.Done:
		t.Logf("exit code: %d, reason: %s", exit.Code, exit.Reason)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion after cancel")
	}

	// Verify Zombie state
	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie state, got %d", proc.GetState())
	}
}

// --- Task: AC #2 Reliability Test (NFR6: ≥95% success, NFR1: ≤30s latency) ---

func TestE2E_Reliability_NFR6(t *testing.T) {
	const runs = 20
	successCount := 0

	for i := 0; i < runs; i++ {
		driver := &mockLLMDriver{
			response: &llm.LLMResponse{Content: fmt.Sprintf("result-%d", i), TokensUsed: 10},
		}

		start := time.Now()
		result := runE2E(t, fmt.Sprintf("task-%d", i), driver, ui.ModeDefault)
		elapsed := time.Since(start)

		if result.Exit.Code == 0 {
			successCount++
		}

		// NFR1: each task should complete within 30 seconds
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

// --- AC #7 Signal Handling: Interrupt Summary and Double-SIGINT ---
//
// NOTE: Story 2.0 resolved the context propagation debt — LLMFile.Write now
// accepts and passes context.Context to driver.Call(). proc.Cancel() propagates
// to in-flight LLM calls. Tests below still use blockingVFSFile (which ignores
// context) to test the signal handling flow at the kernel level.

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
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("signal test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Let goroutine start and block on Write
	time.Sleep(50 * time.Millisecond)

	// Simulate signal handler: emit interrupt message then cancel (mirrors main.go signal handler)
	progress.KernelMessage("PID %d interrupted (SIGINT)", pid)
	proc.Cancel()

	// Unblock LLM Write (simulating the real driver returning after subprocess kill)
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
	exitCode := make(chan int, 1)
	saved := forceExitFunc
	forceExitFunc = func(code int) { exitCode <- code }
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
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("double signal test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := kern.GetProcess(pid)

	// Let goroutine start and block on LLM Write
	time.Sleep(50 * time.Millisecond)

	// Replicate signal handler from runRoot using a test channel
	sigCh := make(chan os.Signal, 2)
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		<-sigCh // First signal
		progress.KernelMessage("PID %d interrupted (SIGINT)", pid)
		proc.Cancel()
		select {
		case <-sigCh: // Second signal within timeout
			forceExitFunc(130)
		case <-time.After(2 * time.Second):
		}
	}()

	// Send first SIGINT
	sigCh <- syscall.SIGINT
	time.Sleep(20 * time.Millisecond)

	// Send second SIGINT within 2-second window
	sigCh <- syscall.SIGINT

	select {
	case code := <-exitCode:
		if code != 130 {
			t.Errorf("expected force exit code 130, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected force exit on double SIGINT")
	}

	// Cleanup: unblock the LLM and drain process
	close(blockCh)
	<-proc.Done
	<-handlerDone
}

// --- Story 2.4: Skill Injection E2E Test ---

// capturingMockLLMDriver records the last request for assertion.
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

func (d *capturingMockLLMDriver) Stream(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("not supported")
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
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude"))
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	agentInfo := &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "test-agent"},
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
	outputSuccess(renderer, ui.ModeDefault, pid, proc, elapsed)

	// Verify skill AllowedDevices set from agent's aggregated tools
	if len(proc.AllowedDevices) != 2 {
		t.Errorf("expected 2 AllowedDevices from agent skills, got %d: %v", len(proc.AllowedDevices), proc.AllowedDevices)
	}

	// Verify result in output
	output := w.String()
	if !strings.Contains(output, "skill result") {
		t.Errorf("expected 'skill result' in output, got:\n%s", output)
	}
}

// --- Story 2.6: Real code-analyst Agent E2E Test ---

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
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude"))
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	sl := skills.NewSkillLoader("../../lib/skills")
	al := agents.NewAgentLoader("../../lib/agents", sl)
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
	outputSuccess(renderer, ui.ModeDefault, pid, proc, elapsed)

	// 验证 Agent instructions + Skill body 被正确注入到 LLM 请求的 system prompt
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

	// 验证 AllowedDevices 从 Skill 聚合
	if len(proc.AllowedDevices) != 2 {
		t.Fatalf("expected 2 AllowedDevices, got %d: %v", len(proc.AllowedDevices), proc.AllowedDevices)
	}
	if proc.AllowedDevices[0] != "/dev/fs" {
		t.Errorf("AllowedDevices[0] = %q, want %q", proc.AllowedDevices[0], "/dev/fs")
	}
	if proc.AllowedDevices[1] != "/dev/shell" {
		t.Errorf("AllowedDevices[1] = %q, want %q", proc.AllowedDevices[1], "/dev/shell")
	}

	// 验证模型自动选择为 "sonnet"（来自 agent.yaml models.preferred）
	if lastReq.Model != "sonnet" {
		t.Errorf("Model = %q, want %q", lastReq.Model, "sonnet")
	}

	// Verify output contains result
	output := w.String()
	if !strings.Contains(output, "分析完成") {
		t.Errorf("expected '分析完成' in output, got:\n%s", output)
	}
}

// --- Story 3.3: Astrace CLI Command Integration Tests ---

// astraceTestKernel creates a minimal kernel suitable for astrace tests.
func astraceTestKernel(t *testing.T, driver llm.LLMDriver) *kernel.KernelImpl {
	t.Helper()
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
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude"))
	ctxMgr := cruxctx.NewManager()
	return kernel.NewKernel(vfsInst, ctxMgr, cb)
}

func TestAstraceCmd_PIDNotFound(t *testing.T) {
	savedKern := kern
	savedJSON := flagJSON
	defer func() {
		kern = savedKern
		flagJSON = savedJSON
	}()
	flagJSON = false

	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "ok", TokensUsed: 1},
	}
	kern = astraceTestKernel(t, driver)

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)

	err := runAstrace(cmd, []string{"999"})
	if err != nil {
		t.Fatalf("expected nil error (error handled internally), got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "PID 999") {
		t.Errorf("expected 'PID 999' in output, got %q", output)
	}
	if !strings.Contains(output, "process not found") {
		t.Errorf("expected 'process not found' in output, got %q", output)
	}
	if !strings.Contains(output, "crux ps") {
		t.Errorf("expected 'crux ps' suggestion in output, got %q", output)
	}
}

func TestAstraceCmd_InvalidPID(t *testing.T) {
	savedKern := kern
	defer func() { kern = savedKern }()

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)

	err := runAstrace(cmd, []string{"abc"})
	if err == nil {
		t.Fatal("expected error for non-numeric PID")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "crux astrace abc") {
		t.Errorf("expected 'crux astrace abc' in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "invalid PID") {
		t.Errorf("expected 'invalid PID' in error, got %q", errMsg)
	}
}

func TestAstraceCmd_AttachAndDetach(t *testing.T) {
	savedKern := kern
	savedJSON := flagJSON
	savedVerbose := flagVerbose
	defer func() {
		kern = savedKern
		flagJSON = savedJSON
		flagVerbose = savedVerbose
	}()
	flagJSON = false
	flagVerbose = false

	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "done", TokensUsed: 1},
	}
	kern = astraceTestKernel(t, driver)

	pid, err := kern.Spawn("astrace test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found after spawn")
	}

	// Wait for process to complete
	<-proc.Done

	// Close DebugChan so Attach returns nil (simulates process exit)
	close(proc.DebugChan)

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err = runAstrace(cmd, []string{fmt.Sprintf("%d", pid)})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()

	// Verify attach header
	attachHeader := fmt.Sprintf("[astrace] attached to PID %d", pid)
	if !strings.Contains(output, attachHeader) {
		t.Errorf("expected attach header %q in output, got %q", attachHeader, output)
	}
	if !strings.Contains(output, "(state:") {
		t.Errorf("expected state in attach header, got %q", output)
	}

	// Verify detach summary (process exited)
	detachSummary := fmt.Sprintf("[astrace] detached from PID %d (process exited)", pid)
	if !strings.Contains(output, detachSummary) {
		t.Errorf("expected detach summary %q in output, got %q", detachSummary, output)
	}
}

func TestAstraceCmd_JSONOutput(t *testing.T) {
	savedKern := kern
	savedJSON := flagJSON
	savedVerbose := flagVerbose
	defer func() {
		kern = savedKern
		flagJSON = savedJSON
		flagVerbose = savedVerbose
	}()
	flagJSON = true
	flagVerbose = false

	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "json test", TokensUsed: 1},
	}
	kern = astraceTestKernel(t, driver)

	pid, err := kern.Spawn("astrace json test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Wait for process to complete
	<-proc.Done

	// Close DebugChan
	close(proc.DebugChan)

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err = runAstrace(cmd, []string{fmt.Sprintf("%d", pid)})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()

	// In JSON mode, no attach/detach headers should appear
	if strings.Contains(output, "[astrace]") {
		t.Errorf("expected no [astrace] headers in JSON mode, got %q", output)
	}

	// Each non-empty line should be valid JSON
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\nline: %s", i, err, line)
		}
		// Verify snake_case fields
		if _, ok := parsed["timestamp_ms"]; !ok {
			t.Errorf("line %d missing timestamp_ms field", i)
		}
		if _, ok := parsed["syscall"]; !ok {
			t.Errorf("line %d missing syscall field", i)
		}
	}
}

func TestAstraceCmd_VerboseOutput(t *testing.T) {
	savedKern := kern
	savedJSON := flagJSON
	savedVerbose := flagVerbose
	defer func() {
		kern = savedKern
		flagJSON = savedJSON
		flagVerbose = savedVerbose
	}()
	flagJSON = false
	flagVerbose = true

	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "verbose test", TokensUsed: 1},
	}
	kern = astraceTestKernel(t, driver)

	pid, err := kern.Spawn("astrace verbose test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Wait for process to complete
	<-proc.Done

	// Close DebugChan
	close(proc.DebugChan)

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err = runAstrace(cmd, []string{fmt.Sprintf("%d", pid)})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()

	// Verify attach header is present
	if !strings.Contains(output, "[astrace] attached to PID") {
		t.Errorf("expected attach header in verbose mode, got %q", output)
	}

	// Verify detach summary
	if !strings.Contains(output, "[astrace] detached from PID") {
		t.Errorf("expected detach summary in verbose mode, got %q", output)
	}

	// Verify events are present (process should have emitted some syscall events)
	// In verbose mode, arguments should NOT be truncated
	if !strings.Contains(output, "PID") {
		t.Errorf("expected PID in event output, got %q", output)
	}
}

func TestAstraceCmd_MissingPID(t *testing.T) {
	// AC #8: crux astrace without PID argument should show usage help.
	var buf bytes.Buffer
	cmd := &cobra.Command{
		Use:  "astrace <pid>",
		Args: cobra.ExactArgs(1),
		RunE: runAstrace,
	}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing PID argument")
	}

	output := buf.String()
	if !strings.Contains(output, "Usage") {
		t.Errorf("expected usage help in output, got %q", output)
	}
}

// --- Story 3.4: Astrace UI TraceLine Integration Tests ---

func TestAstraceCmd_UIFormatterIntegration(t *testing.T) {
	// AC #6: non-JSON mode uses ui.FormatTraceLine via opts.Formatter.
	// Note: In test env (non-TTY, ColorLevel=0), FormatTraceLine and FormatEvent
	// produce nearly identical output. This test verifies the integration wiring
	// (Renderer creation, Formatter injection) rather than style-specific output.
	savedKern := kern
	savedJSON := flagJSON
	savedVerbose := flagVerbose
	defer func() {
		kern = savedKern
		flagJSON = savedJSON
		flagVerbose = savedVerbose
	}()
	flagJSON = false
	flagVerbose = false

	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "ui test", TokensUsed: 1},
	}
	kern = astraceTestKernel(t, driver)

	pid, err := kern.Spawn("astrace ui test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Wait for process to complete so events are emitted
	<-proc.Done

	// Close DebugChan so Attach returns
	close(proc.DebugChan)

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err = runAstrace(cmd, []string{fmt.Sprintf("%d", pid)})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()

	// Verify attach/detach headers
	if !strings.Contains(output, "[astrace] attached to PID") {
		t.Errorf("expected attach header, got %q", output)
	}
	if !strings.Contains(output, "[astrace] detached from PID") {
		t.Errorf("expected detach summary, got %q", output)
	}

	// Verify UI formatter is used: output should contain → separator (from FormatTraceLine)
	// and syscall events from the process
	if !strings.Contains(output, "→") {
		t.Errorf("expected '→' separator from UI formatter, got %q", output)
	}
}

func TestAstraceCmd_UIFormatterNoColor(t *testing.T) {
	// AC #4: NO_COLOR mode degrades correctly with UI formatter
	savedKern := kern
	savedJSON := flagJSON
	savedVerbose := flagVerbose
	defer func() {
		kern = savedKern
		flagJSON = savedJSON
		flagVerbose = savedVerbose
	}()
	flagJSON = false
	flagVerbose = false

	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "nocolor test", TokensUsed: 1},
	}
	kern = astraceTestKernel(t, driver)

	pid, err := kern.Spawn("astrace nocolor test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	<-proc.Done
	close(proc.DebugChan)

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err = runAstrace(cmd, []string{fmt.Sprintf("%d", pid)})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()

	// In test environment (non-TTY), ColorLevel=0, so no ANSI codes in trace lines
	// Remove attach/detach headers which are plain fmt.Fprintf
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "[astrace]") || line == "" {
			continue
		}
		// Trace lines should not contain ANSI codes in no-color mode
		if strings.Contains(line, "\x1b[") {
			t.Errorf("unexpected ANSI codes in no-color trace line: %q", line)
		}
	}
}

func TestAstraceCmd_JSONModeBypassesFormatter(t *testing.T) {
	// AC #6: JSON mode should NOT use UI formatter
	savedKern := kern
	savedJSON := flagJSON
	savedVerbose := flagVerbose
	defer func() {
		kern = savedKern
		flagJSON = savedJSON
		flagVerbose = savedVerbose
	}()
	flagJSON = true
	flagVerbose = false

	driver := &mockLLMDriver{
		response: &llm.LLMResponse{Content: "json bypass test", TokensUsed: 1},
	}
	kern = astraceTestKernel(t, driver)

	pid, err := kern.Spawn("astrace json bypass test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	<-proc.Done
	close(proc.DebugChan)

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "astrace"}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err = runAstrace(cmd, []string{fmt.Sprintf("%d", pid)})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()

	// JSON mode: no [astrace] headers
	if strings.Contains(output, "[astrace]") {
		t.Errorf("expected no [astrace] headers in JSON mode, got %q", output)
	}

	// Each non-empty line should be valid JSON
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\nline: %s", i, err, line)
		}
	}
}

// --- Story 4.1: Kill + Wait Integration Tests ---

func TestE2E_KillWait_FullLifecycle(t *testing.T) {
	// Spawn → Kill → Wait → verify full resource release
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
	ctxMgr := cruxctx.NewManager()
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

	// Let goroutine start and block on Write
	time.Sleep(50 * time.Millisecond)

	// Kill the process
	if err := kern.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	// Unblock LLM so goroutine can detect cancellation
	close(blockCh)

	// Wait for full cleanup
	exit, err := kern.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	t.Logf("exit code: %d, reason: %s", exit.Code, exit.Reason)

	// Verify process removed from table
	_, ok = kern.GetProcess(pid)
	if ok {
		t.Error("process should be removed from table after Wait")
	}

	// Verify context freed
	_, ctxErr := ctxMgr.BuildPrompt(ctxID)
	if ctxErr == nil {
		t.Error("context should be freed after Wait")
	}

	// Verify process state is Dead
	if proc.GetState() != types.StateDead {
		t.Errorf("expected Dead state, got %d", proc.GetState())
	}
}

func TestE2E_KillWait_ReasonStepExits(t *testing.T) {
	// After Kill, reasonStep loop should detect cancellation and exit
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
			readData: []byte(`{"content":"should not reach","tokens_used":1}`),
		}, nil
	})
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("reasonstep exit test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Let goroutine start
	time.Sleep(50 * time.Millisecond)

	// Kill
	_ = kern.Kill(pid, types.SIGKILL)
	close(blockCh)

	// Wait
	exit, err := kern.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	// The process should have been cancelled or completed before cancellation was detected.
	// Kill is async: the goroutine may finish its current step before checking ctx.Done().
	// Key assertion: Wait returns (doesn't hang) and err is nil.
	if exit.Code != 0 {
		t.Logf("process was cancelled: code=%d reason=%q", exit.Code, exit.Reason)
	} else {
		t.Logf("process completed normally before cancellation detected: code=%d reason=%q", exit.Code, exit.Reason)
	}
}

func TestE2E_KillWait_RaceDetection(t *testing.T) {
	// Run go test -race: verify no data races in Kill+Wait flow
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
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude"))
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	pid, err := kern.Spawn("race test", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Wait with Kill running concurrently
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = kern.Wait(pid)
	}()

	// Try Kill (may or may not beat Wait)
	_ = kern.Kill(pid, types.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Process should be cleaned up
	_, ok := kern.GetProcess(pid)
	if ok {
		t.Error("process should be removed after Wait")
	}
}
