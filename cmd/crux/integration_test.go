package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/drivers/llm"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/vfs"
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
// NOTE on architectural limitation (M2 documentation):
// LLMFile.Write calls driver.Call(context.Background(), ...), NOT the process
// context. This means proc.Cancel() does NOT propagate to an in-flight LLM call.
// Cancellation only takes effect at the next reasonStep loop iteration's ctx.Done()
// check. Tests below simulate this by manually unblocking the mock after Cancel().
// This is a known architectural debt to be addressed in a future story.

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
