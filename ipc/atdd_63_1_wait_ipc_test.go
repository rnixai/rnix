package ipc

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 63.1: `rnix wait <pid>` — IPC 层（MethodWait / handleWait）
//
// 覆盖 AC1（阻塞 + 退出码传播）、AC2（procHistory 立即返回 + 裁决 5 降级）、
// AC3（timeout 可轮询）、AC5（NOT_FOUND）、AC8（终态即时性 + broadcast 并发）。
//
// 时序纪律（Known Test Issues 42-2 教训）：需要进程驻留 Running 的用例一律用
// mockLLMFile 的 parkOnRead/parkOnWrite 握手 gate——等 reached 信号后再断言，
// close(release) 放行——绝不 sleep 碰运气。
//
// 连接纪律（裁决 3）：MethodWait dispatch 后 `return`，handler 终结连接
// （mirror MethodSpawn）。因此每次 Wait 调用都必须用新 Dial 的 client——
// waitViaNewConn 封装此约定；复用同一连接发第二个请求会读到 EOF。
// =============================================================================

// setupWaitIPCTest mirrors setupResumeIPCTest but exposes sockPath so tests
// can open one fresh connection per Wait call (裁决 3: the handler owns and
// terminates the connection).
func setupWaitIPCTest(t *testing.T) (string, *kernel.KernelImpl, string, *mockLLMFile) {
	t.Helper()

	completeResp := `{"action":"complete","summary":"done","content":"done"}`
	llmFile := &mockLLMFile{readData: []byte(completeResp)}
	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

	_, projBase := kernel.TestSetupDataDir(t, kern)

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	return sockPath, kern, projBase, llmFile
}

// waitViaNewConn performs one MethodWait round-trip on a fresh connection.
func waitViaNewConn(t *testing.T, sockPath string, pid types.PID, timeoutMs int64) (*WaitResponse, error) {
	t.Helper()
	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	return client.Wait(pid, timeoutMs)
}

// waitResult carries an async Wait outcome through a channel.
type waitResult struct {
	resp *WaitResponse
	err  error
}

// --- 63.1-INT-001: Running 进程阻塞等待 + exit 0 传播 (AC1) ---

func TestATDD_63_1_INT_001_WaitBlocksUntilComplete_ExitZero(t *testing.T) {
	sockPath, kern, _, llmFile := setupWaitIPCTest(t)

	reached, release := llmFile.parkOnRead()

	pid, err := kern.Spawn("atdd 63.1 — wait blocks until complete", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitForReached(t, reached, 2*time.Second, "process to enter LLM Read (Running)")

	done := make(chan waitResult, 1)
	go func() {
		resp, err := waitViaNewConn(t, sockPath, pid, 0)
		done <- waitResult{resp, err}
	}()

	// The wait must still be blocked while the process is parked Running.
	select {
	case r := <-done:
		t.Fatalf("wait returned while process still Running: resp=%+v err=%v", r.resp, r.err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release) // let the process complete

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Wait failed: %v", r.err)
		}
		if r.resp.TimedOut {
			t.Error("TimedOut should be false for a terminal-state return")
		}
		if r.resp.ExitCode != 0 {
			t.Errorf("ExitCode = %d, want 0 (complete propagates zero)", r.resp.ExitCode)
		}
		if r.resp.PID != pid {
			t.Errorf("PID = %d, want %d", r.resp.PID, pid)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after process completed")
	}
}

// --- 63.1-INT-002: 非零退出码传播（kill Running 进程唤醒 wait）(AC1) ---

func TestATDD_63_1_INT_002_NonZeroExitPropagation_KillWakesWait(t *testing.T) {
	sockPath, kern, _, llmFile := setupWaitIPCTest(t)

	// parkOnWrite（非 parkOnRead）：SIGKILL 对 Running 进程只做 proc.Cancel()
	// （kernel/signal.go defaultSignalAction），唤醒依赖 mock 的 ctx.Done 分支——
	// Write gate 有该分支，Read gate 没有。
	reached, release := llmFile.parkOnWrite()
	defer close(release)

	pid, err := kern.Spawn("atdd 63.1 — kill propagates non-zero", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitForReached(t, reached, 2*time.Second, "process to enter LLM Write (Running)")

	done := make(chan waitResult, 1)
	go func() {
		resp, err := waitViaNewConn(t, sockPath, pid, 0)
		done <- waitResult{resp, err}
	}()

	// Ensure the waiter is parked before killing.
	time.Sleep(100 * time.Millisecond)

	if err := kern.Kill(pid, types.SIGKILL); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Wait failed: %v", r.err)
		}
		if r.resp.TimedOut {
			t.Error("TimedOut should be false when the process was killed")
		}
		if r.resp.ExitCode == 0 {
			t.Error("ExitCode should be non-zero for a killed process")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not wake after Kill")
	}
}

// --- 63.1-INT-003: 已终态进程（procTable 内 Zombie/Dead）立即返回 + 可重复 wait (AC8) ---

func TestATDD_63_1_INT_003_TerminatedInProcTable_ImmediateAndRepeatable(t *testing.T) {
	sockPath, kern, _, _ := setupWaitIPCTest(t)

	// No gate: the mock returns complete immediately, the process terminates
	// on its first reason step.
	pid, err := kern.Spawn("atdd 63.1 — already terminated", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// First wait rides the blocking path until termination.
	first, err := waitViaNewConn(t, sockPath, pid, 0)
	if err != nil {
		t.Fatalf("first Wait failed: %v", err)
	}
	if first.TimedOut || first.ExitCode != 0 {
		t.Fatalf("first Wait = %+v, want ExitCode 0, TimedOut false", first)
	}

	// Second wait on the same PID must return immediately (closed channel /
	// history — pollable semantics). The 2s bound would trip TimedOut if the
	// closed-channel fast path were broken.
	start := time.Now()
	second, err := waitViaNewConn(t, sockPath, pid, 2000)
	if err != nil {
		t.Fatalf("second Wait failed: %v", err)
	}
	if second.TimedOut {
		t.Error("second Wait must not time out — terminal state returns immediately")
	}
	if second.ExitCode != first.ExitCode {
		t.Errorf("repeat wait ExitCode = %d, want %d (consistent results)", second.ExitCode, first.ExitCode)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("second Wait took %v — expected immediate return on terminal state", elapsed)
	}
}

// --- 63.1-INT-004: procHistory 命中立即返回历史 exit code (AC2) ---

func TestATDD_63_1_INT_004_HistoryHit_ReturnsRecordedExitCode(t *testing.T) {
	sockPath, kern, projBase, _ := setupWaitIPCTest(t)

	histPID := types.PID(63104)
	info := vfs.ProcInfo{
		PID:         histPID,
		UUID:        "63100000-0000-7000-0000-000000000004",
		State:       types.StateDead,
		Intent:      "reaped with recorded exit",
		ExitCode:    7,
		ExitCodeSet: true,
		ExitReason:  "error",
	}
	if err := kernel.SaveProcInfo(projBase, info); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}
	if err := kern.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	start := time.Now()
	resp, err := waitViaNewConn(t, sockPath, histPID, 0)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if resp.TimedOut {
		t.Error("TimedOut should be false for a history hit")
	}
	if resp.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7 (recorded history exit code)", resp.ExitCode)
	}
	if resp.ExitReason != "error" {
		t.Errorf("ExitReason = %q, want %q", resp.ExitReason, "error")
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("history wait took %v — must return immediately, no blocking", elapsed)
	}
}

// --- 63.1-INT-005: 裁决 5 — ExitCodeSet=false 历史条目降级为 exit 1 (AC2) ---

func TestATDD_63_1_INT_005_HistoryNoExitCode_DegradesToOne(t *testing.T) {
	sockPath, kern, projBase, _ := setupWaitIPCTest(t)

	histPID := types.PID(63105)
	info := vfs.ProcInfo{
		PID:    histPID,
		UUID:   "63100000-0000-7000-0000-000000000005",
		State:  types.StateRunning, // daemon-crash leftover: running snapshot, no exit recorded
		Intent: "crash leftover without exit code",
		// ExitCodeSet deliberately false, ExitReason empty
	}
	if err := kernel.SaveProcInfo(projBase, info); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}
	if err := kern.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	resp, err := waitViaNewConn(t, sockPath, histPID, 0)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1 (裁决 5: never propagate zero for unrecorded exit)", resp.ExitCode)
	}
	if !strings.Contains(resp.ExitReason, "unknown") {
		t.Errorf("ExitReason = %q, want substring %q", resp.ExitReason, "unknown")
	}
}

// --- 63.1-INT-006: --timeout 超时返回 TimedOut，进程不受影响可再 wait (AC3) ---

func TestATDD_63_1_INT_006_Timeout_PollableSemantics(t *testing.T) {
	sockPath, kern, _, llmFile := setupWaitIPCTest(t)

	reached, release := llmFile.parkOnRead()

	pid, err := kern.Spawn("atdd 63.1 — timeout pollable", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitForReached(t, reached, 2*time.Second, "process to enter LLM Read (Running)")

	// First bounded wait: times out, business result inside OK envelope.
	resp1, err := waitViaNewConn(t, sockPath, pid, 150)
	if err != nil {
		t.Fatalf("first bounded Wait failed: %v", err)
	}
	if !resp1.TimedOut {
		t.Fatal("first bounded Wait should report TimedOut=true")
	}

	// The process must be untouched: still Running after the timeout.
	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("process vanished after a timed-out wait — wait must be a pure observer")
	}
	if got := proc.GetState(); got != types.StateRunning {
		t.Fatalf("process state = %v after timeout, want Running (wait must not disturb the target)", got)
	}

	// Pollable: an immediate second bounded wait works the same.
	resp2, err := waitViaNewConn(t, sockPath, pid, 150)
	if err != nil {
		t.Fatalf("second bounded Wait failed: %v", err)
	}
	if !resp2.TimedOut {
		t.Fatal("second bounded Wait should also report TimedOut=true")
	}

	// Release; a final unbounded wait rides to the terminal state.
	close(release)
	resp3, err := waitViaNewConn(t, sockPath, pid, 0)
	if err != nil {
		t.Fatalf("final Wait failed: %v", err)
	}
	if resp3.TimedOut || resp3.ExitCode != 0 {
		t.Errorf("final Wait = %+v, want ExitCode 0, TimedOut false", resp3)
	}
}

// --- 63.1-INT-007: NOT_FOUND — procTable 与 procHistory 均无 (AC5) ---

func TestATDD_63_1_INT_007_NotFound(t *testing.T) {
	sockPath, _, _, _ := setupWaitIPCTest(t)

	_, err := waitViaNewConn(t, sockPath, types.PID(999999), 0)
	if err == nil {
		t.Fatal("Wait on a nonexistent PID should fail")
	}
	if !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Errorf("error = %v, want NOT_FOUND code in message", err)
	}
}

// Regression: malformed IPC timeout values must fail fast instead of being
// interpreted as unbounded waits or overflowing time.Duration.
func TestATDD_63_1_INT_007a_InvalidTimeoutMsRejected(t *testing.T) {
	sockPath, _, _, _ := setupWaitIPCTest(t)

	for _, tc := range []struct {
		name      string
		timeoutMs int64
	}{
		{name: "negative", timeoutMs: -1},
		{name: "overflow", timeoutMs: maxWaitTimeoutMs + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := waitViaNewConn(t, sockPath, types.PID(999999), tc.timeoutMs)
			if err == nil {
				t.Fatal("Wait with invalid timeout_ms should fail")
			}
			if !strings.Contains(err.Error(), "INVALID") {
				t.Errorf("error = %v, want INVALID code in message", err)
			}
		})
	}
}

// --- 63.1-INT-008: 并发多 waiter 同 PID — broadcast 全部唤醒且结果一致 (AC8) ---

func TestATDD_63_1_INT_008_ConcurrentWaiters_ConsistentResults(t *testing.T) {
	sockPath, kern, _, llmFile := setupWaitIPCTest(t)

	reached, release := llmFile.parkOnRead()

	pid, err := kern.Spawn("atdd 63.1 — concurrent waiters", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitForReached(t, reached, 2*time.Second, "process to enter LLM Read (Running)")

	const waiters = 4
	results := make([]waitResult, waiters)
	var wg sync.WaitGroup
	for i := range waiters {
		wg.Go(func() {
			resp, err := waitViaNewConn(t, sockPath, pid, 0)
			results[i] = waitResult{resp, err}
		})
	}

	// Give the waiters a moment to park, then release the process.
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("waiter %d failed: %v", i, r.err)
		}
		if r.resp.TimedOut {
			t.Errorf("waiter %d: TimedOut should be false", i)
		}
		if r.resp.ExitCode != 0 {
			t.Errorf("waiter %d: ExitCode = %d, want 0 (all waiters see the same result)", i, r.resp.ExitCode)
		}
	}
}
