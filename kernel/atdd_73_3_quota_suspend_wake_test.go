package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gocontext "context"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 73.3 — 终态配额转挂起与窗口恢复续跑（FR6 + NFR5）
//
// AC 覆盖：
//   - AC1  终态配额/超上限走挂起而非杀进程（含 fallback 不触发断言）
//   - AC2  ResumeAt 全链载体 + 盘往返 + LoadSuspendedFromDisk 恢复
//   - AC3  daemon 侧扫描器唤醒（+ 未到期 / 零值 / 已手动 resume 三反例）
//   - AC4  gc 豁免回归钉住
//   - AC5  resume 路径零 quota 分支 + ResumeAt 清空（三链路）
//   - AC6  LLM 零感知（上下文消息数不变）
//   - AC7  事件形状（quota_suspend / quota_window_wake / quota_wake_failed /
//          SupervisorChildSuspended + 共享 Suspend 事件保留）
//   - AC8  invariant 护栏 + 全部回归
//   - D3   唤醒失败 5min 推迟；D4 supervisor gate 三态；D5 快速通道（+反证对偶）；
//          D6 无等待 overload 永不挂起
// =============================================================================

// -----------------------------------------------------------------------------
// mock 基础设施
// -----------------------------------------------------------------------------

// scriptedQuotaLLM plays a per-call script: Write #i fails with writeErrs[i]
// (nil entry = success); calls beyond the script succeed. Read #i returns
// readData[i]; the LAST entry repeats forever, so a two-entry read script
// drives one in-loop step and then completes. The SAME instance is handed out
// on every Open (registry closure), so a wake's FD reopen continues the script
// where the suspend left it.
type scriptedQuotaLLM struct {
	mu        sync.Mutex
	writeErrs []error
	readData  [][]byte
	writes    int
	reads     int
}

func (f *scriptedQuotaLLM) Write(_ gocontext.Context, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.writes
	f.writes++
	if i < len(f.writeErrs) {
		return f.writeErrs[i]
	}
	return nil
}

func (f *scriptedQuotaLLM) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.readData) == 0 {
		return makeLLMResponse("done", 1), nil
	}
	i := f.reads
	f.reads++
	if i >= len(f.readData) {
		i = len(f.readData) - 1
	}
	return f.readData[i], nil
}

func (f *scriptedQuotaLLM) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

func (f *scriptedQuotaLLM) Close() error { return nil }
func (f *scriptedQuotaLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *scriptedQuotaLLM) SupportsToolCalling() bool { return true }

// parkedWriteLLM fails Write #0 with quotaErr and blocks Write #1 until
// release closes — it parks a woken/resumed process INSIDE the LLM call so
// assertions can inspect the Running state deterministically before the step
// completes.
type parkedWriteLLM struct {
	mu       sync.Mutex
	quotaErr error
	writes   int
	release  chan struct{}
}

func (f *parkedWriteLLM) Write(_ gocontext.Context, _ []byte) error {
	f.mu.Lock()
	i := f.writes
	f.writes++
	f.mu.Unlock()
	if i == 0 {
		return f.quotaErr
	}
	<-f.release
	return nil
}

func (f *parkedWriteLLM) Read(_ int) ([]byte, error) { return makeLLMResponse("done", 1), nil }
func (f *parkedWriteLLM) Close() error               { return nil }
func (f *parkedWriteLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *parkedWriteLLM) SupportsToolCalling() bool { return true }

// countingLLMFile records how many Writes reach it — the fallback probe: a
// quota suspension must never fall through to attemptFallback, so a fallback
// device wearing this file must end with zero writes.
type countingLLMFile struct {
	writes atomic.Int64
}

func (f *countingLLMFile) Write(_ gocontext.Context, _ []byte) error {
	f.writes.Add(1)
	return nil
}
func (f *countingLLMFile) Read(_ int) ([]byte, error) { return makeLLMResponse("done", 1), nil }
func (f *countingLLMFile) Close() error               { return nil }
func (f *countingLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/fb"}, nil
}
func (f *countingLLMFile) SupportsToolCalling() bool { return true }

// quotaOnceRouter hands out per-FD files whose first N Writes across the
// whole router fail with quotaErr and every later Write (any FD) succeeds.
// The failure state is router-level, not per-FD: a wake reopens the LLM
// device — a fresh per-FD "fail once" would suspend the child again on the
// reopened FD and it would never complete (D4 subtests ②/③). failTimes <= 1
// means fail-once (the legacy D4 shape); the wake-rehit-loop test sets 2.
type quotaOnceRouter struct {
	mu        sync.Mutex
	quotaErr  error
	failTimes int
	failures  int
}

func (r *quotaOnceRouter) newFile() vfs.VFSFile { return &quotaOnceRoutedFile{router: r} }

func (r *quotaOnceRouter) firstWriteFails() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	limit := max(r.failTimes, 1)
	if r.failures < limit {
		r.failures++
		return r.quotaErr
	}
	return nil
}

type quotaOnceRoutedFile struct {
	router *quotaOnceRouter
}

func (f *quotaOnceRoutedFile) Write(_ gocontext.Context, _ []byte) error {
	return f.router.firstWriteFails()
}

func (f *quotaOnceRoutedFile) Read(_ int) ([]byte, error) { return makeLLMResponse("done", 1), nil }
func (f *quotaOnceRoutedFile) Close() error               { return nil }
func (f *quotaOnceRoutedFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *quotaOnceRoutedFile) SupportsToolCalling() bool { return true }

// -----------------------------------------------------------------------------
// kernel / spawn / event helpers
// -----------------------------------------------------------------------------

// newQuotaWakeKernel builds a kernel whose persistence layout is UNIFIED: the
// spawn-time EventWriterFactory base and ResolveStepBaseDir both resolve to
// <dataDir>/global, so events.jsonl and proc-info.json of the original and of
// any resumed incarnation land in the SAME directory (resume paths attach a
// fresh writer at ResolveStepBaseDir; a split layout would scatter the two
// halves of one process's event trail). Returns the kernel and that shared
// base directory.
func newQuotaWakeKernel(t *testing.T, primary vfs.VFSFile) (*KernelImpl, string) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return primary, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	dataDir := t.TempDir()
	k.SetDataDir(dataDir)
	t.Cleanup(k.Shutdown)
	return k, filepath.Join(dataDir, "global")
}

func spawnWithEventBase(t *testing.T, k *KernelImpl, eventsBase, intent string) *Process {
	t.Helper()
	pid, err := k.Spawn(intent, nil, failureRawSpawnOpts(eventsBase))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not in procTable")
	}
	return proc
}

func readDiskEvents(t *testing.T, eventsBase, uuid string) []SyscallEventDisk {
	t.Helper()
	evs, err := ReadAllEvents(filepath.Join(eventsBase, "steps", uuid, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	return evs
}

func diskEventsWithAction(evs []SyscallEventDisk, syscall, action string) []SyscallEventDisk {
	var out []SyscallEventDisk
	for _, ev := range evs {
		if ev.Syscall == syscall && ev.Args["action"] == action {
			out = append(out, ev)
		}
	}
	return out
}

func diskEventsWithSyscall(evs []SyscallEventDisk, syscall string) []SyscallEventDisk {
	var out []SyscallEventDisk
	for _, ev := range evs {
		if ev.Syscall == syscall {
			out = append(out, ev)
		}
	}
	return out
}

// quotaErrWithResetAt builds the over-cap quota failure shape: a server
// absolute reset instant (source=body, the captured qwen shape) far beyond
// maxInProcessWait.
func quotaErrWithResetAt(resetAt time.Time) error {
	return llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindQuota, kernelQuotaBody, 0, resetAt, "body"))
}

// writeRehydrateSidecars adds the two files rehydrateRuntimeStateFromDisk
// needs beside an existing proc-info.json so LoadSuspendedFromDisk can rebuild
// a placeholder (steps.jsonl with a single empty-message record, in the 44.3
// fixture shape, + process-meta.json).
func writeRehydrateSidecars(t *testing.T, stepsDir string) {
	t.Helper()
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", stepsDir, err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"),
		[]byte(`{"step":1,"messages":[]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
	meta := []byte(`{"system_prompt":"You are a resumed test agent.","tools":[]}`)
	if err := os.WriteFile(filepath.Join(stepsDir, "process-meta.json"), meta, 0o644); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}
}

// ctxMsgCount returns the number of persisted context messages for proc.
func ctxMsgCount(t *testing.T, k *KernelImpl, proc *Process) int {
	t.Helper()
	raw, err := k.ctxMgr.CtxRead(proc.CtxID, 0, 0)
	if err != nil {
		t.Fatalf("CtxRead: %v", err)
	}
	var data debug.ContextData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal context data: %v", err)
	}
	return len(data.Messages)
}

// -----------------------------------------------------------------------------
// AC1 / D6 — 超上限走挂起而非杀进程
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC1_QuotaBeyondCap_SuspendsWithResetAt: a quota failure whose
// server-declared reset instant lies beyond maxInProcessWait suspends the
// process instead of killing it. ResumeAt carries the server absolute instant
// verbatim (source=reset_at), the quota_suspend event is fully populated, the
// shared Suspend event survives with the new reason, and attemptFallback is
// NEVER reached (the fallback device ends with zero writes).
func TestATDD_73_3_AC1_QuotaBeyondCap_SuspendsWithResetAt(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErrWithResetAt(resetAt)},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	fallback := &countingLLMFile{}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "claude", "fb")
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "fb")

	pid, err := k.Spawn("73.3 AC1 quota over-cap", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	if exit.Code != ExitSuspended {
		t.Fatalf("exit.Code = %d, want %d (ExitSuspended) — over-cap quota is a suspension, not a death", exit.Code, ExitSuspended)
	}
	if st := proc.GetState(); st != types.StateSuspended {
		t.Errorf("state = %s, want %s", st, types.StateSuspended)
	}
	if r := proc.GetSuspendReason(); r != SuspendReasonQuotaExhausted {
		t.Errorf("SuspendReason = %q, want %q", r, SuspendReasonQuotaExhausted)
	}
	if got := proc.GetResumeAt(); !got.Equal(resetAt) {
		t.Errorf("ResumeAt = %v, want the server reset instant %v verbatim", got, resetAt)
	}

	evs := readDiskEvents(t, baseDir, proc.UUID)

	suspends := diskEventsWithAction(evs, "ReasonStep", "quota_suspend")
	if len(suspends) != 1 {
		t.Fatalf("quota_suspend events = %d, want exactly 1", len(suspends))
	}
	qs := suspends[0].Args
	if qs["rate_limit_kind"] != "quota" {
		t.Errorf("rate_limit_kind = %v, want quota", qs["rate_limit_kind"])
	}
	if qs["provider"] != "claude" {
		t.Errorf("provider = %v, want claude", qs["provider"])
	}
	if qs["resume_at"] != resetAt.Format(time.RFC3339) {
		t.Errorf("resume_at = %v, want %v", qs["resume_at"], resetAt.Format(time.RFC3339))
	}
	if qs["resume_at_source"] != "reset_at" {
		t.Errorf("resume_at_source = %v, want reset_at (server absolute instant)", qs["resume_at_source"])
	}
	if wm, ok := argMillis(qs["required_wait_ms"]); !ok || wm <= maxInProcessWait.Milliseconds() {
		t.Errorf("required_wait_ms = %v, want above the cap", qs["required_wait_ms"])
	}
	if lm, ok := argMillis(qs["limit_ms"]); !ok || lm != maxInProcessWait.Milliseconds() {
		t.Errorf("limit_ms = %v, want %d", qs["limit_ms"], maxInProcessWait.Milliseconds())
	}

	// AC7 — the shared suspendProcess Suspend event is preserved verbatim, its
	// reason naturally carrying the new value.
	var sawSharedSuspend bool
	for _, ev := range diskEventsWithSyscall(evs, "Suspend") {
		if ev.Args["reason"] == SuspendReasonQuotaExhausted {
			sawSharedSuspend = true
		}
	}
	if !sawSharedSuspend {
		t.Error("no shared Suspend event with reason=quota_exhausted — suspendProcess's event must survive")
	}

	// D6 — the suspend branch skips attemptFallback entirely.
	if n := fallback.writes.Load(); n != 0 {
		t.Errorf("fallback device received %d writes, want 0 — quota suspension must not fall back", n)
	}
}

// TestATDD_73_3_AC1_ThrottleBeyondCap_SuspendsAtNowPlusRetryAfter: the D6
// rule is keyed on the wait DURATION, not the kind — a throttle with a
// server-declared retry-after beyond the cap suspends too, with ResumeAt
// derived as now+retryAfter (a relative value has no absolute anchor).
func TestATDD_73_3_AC1_ThrottleBeyondCap_SuspendsAtNowPlusRetryAfter(t *testing.T) {
	// CONSTRUCTED fixture — not captured from production (provenance
	// discipline, §7): a 7200s Retry-After throttle header.
	err := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, "try again after 7200 seconds", 7200*time.Second, time.Time{}, "header"))
	primary := &scriptedQuotaLLM{
		writeErrs: []error{err},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	before := time.Now()
	proc := spawnWithEventBase(t, k, baseDir, "73.3 AC1 throttle over-cap")
	exit := waitDone(t, proc)

	if exit.Code != ExitSuspended {
		t.Fatalf("exit.Code = %d, want %d — over-cap throttle is a suspension too (D6 keys on duration)", exit.Code, ExitSuspended)
	}
	resumeAt := proc.GetResumeAt()
	wantLow, wantHigh := before.Add(7200*time.Second), time.Now().Add(7200*time.Second)
	if resumeAt.Before(wantLow) || resumeAt.After(wantHigh) {
		t.Errorf("ResumeAt = %v, want within [%v, %v] (now + retryAfter)", resumeAt, wantLow, wantHigh)
	}

	evs := readDiskEvents(t, baseDir, proc.UUID)
	suspends := diskEventsWithAction(evs, "ReasonStep", "quota_suspend")
	if len(suspends) != 1 {
		t.Fatalf("quota_suspend events = %d, want exactly 1", len(suspends))
	}
	if suspends[0].Args["rate_limit_kind"] != "throttle" {
		t.Errorf("rate_limit_kind = %v, want throttle", suspends[0].Args["rate_limit_kind"])
	}
	if suspends[0].Args["resume_at_source"] != "retry_after" {
		t.Errorf("resume_at_source = %v, want retry_after (relative derivation)", suspends[0].Args["resume_at_source"])
	}
}

// -----------------------------------------------------------------------------
// D5 — 快速通道：KindQuota 无服务端等待证据 → 立即挂起，零重试
// -----------------------------------------------------------------------------

// TestATDD_73_3_D5_FastPath_NoWaitQuotaSuspendsImmediately: a quota failure
// carrying NO server wait fields suspends immediately — zero retries, zero
// backoff waits, and a zero ResumeAt (manual-resume-only; the scanner's IsZero
// gate never wakes it). The judgement is RateLimitWaitOf's ok flag.
func TestATDD_73_3_D5_FastPath_NoWaitQuotaSuspendsImmediately(t *testing.T) {
	rec := installSleepRecorder(t, false, 0)
	// No wait fields: the plain constructor (CLI-shaped / unclassified body).
	quotaErr := llm.NewLLMError("claude", 429,
		llm.NewRateLimitError(llm.KindQuota, `{"error":{"code":"insufficient_quota","message":"You exceeded your current quota"}}`))
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErr, quotaErr, quotaErr, quotaErr},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 D5 fast path")
	exit := waitDone(t, proc)

	if exit.Code != ExitSuspended {
		t.Fatalf("exit.Code = %d, want %d — waitless quota suspends immediately", exit.Code, ExitSuspended)
	}
	if st := proc.GetState(); st != types.StateSuspended {
		t.Errorf("state = %s, want %s", st, types.StateSuspended)
	}
	if !proc.GetResumeAt().IsZero() {
		t.Errorf("ResumeAt = %v, want zero — no server evidence, no wake instant", proc.GetResumeAt())
	}
	if n := primary.writeCount(); n != 1 {
		t.Errorf("LLM writes = %d, want exactly 1 — the fast path retries zero times", n)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Errorf("sleepFunc called %d times, want 0 — the fast path never waits", n)
	}

	evs := readDiskEvents(t, baseDir, proc.UUID)
	if retries := diskEventsWithAction(evs, "ReasonStep", "transient_retry"); len(retries) != 0 {
		t.Errorf("transient_retry events = %d, want 0 — no retry budget consumed", len(retries))
	}
	suspends := diskEventsWithAction(evs, "ReasonStep", "quota_suspend")
	if len(suspends) != 1 {
		t.Fatalf("quota_suspend events = %d, want exactly 1", len(suspends))
	}
	qs := suspends[0].Args
	if qs["rate_limit_kind"] != "quota" {
		t.Errorf("rate_limit_kind = %v, want quota", qs["rate_limit_kind"])
	}
	for _, field := range []string{"resume_at", "resume_at_source", "required_wait_ms", "limit_ms"} {
		if v, present := qs[field]; present {
			t.Errorf("fast-path quota_suspend carries %q=%v — over-cap-only fields must be omitted", field, v)
		}
	}
}

// TestATDD_73_3_D5_FastPath_Counterpart_PastResetAtRetries: the D5 trap — a
// quota error whose resetAt is already in the PAST must NOT take the fast
// path. RateLimitWaitOf still says ok=true (wait fields present), and a past
// reset means the window just recovered: the right move is an ordinary retry,
// which here recovers the process.
func TestATDD_73_3_D5_FastPath_Counterpart_PastResetAtRetries(t *testing.T) {
	rec := installSleepRecorder(t, false, 0)
	pastErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindQuota, kernelQuotaBody, 0, time.Now().Add(-time.Hour), "body"))
	primary := &scriptedQuotaLLM{
		writeErrs: []error{pastErr},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 D5 past resetAt")
	exit := waitDone(t, proc)

	if exit.Code != 0 {
		t.Fatalf("exit = %+v, want success — a past resetAt means the window recovered; retry, don't suspend", exit)
	}
	if st := proc.GetState(); st == types.StateSuspended {
		t.Error("process suspended — the past-resetAt counterpart must stay on the retry path")
	}
	if n := primary.writeCount(); n != 2 {
		t.Errorf("LLM writes = %d, want 2 (one failed + one recovered)", n)
	}

	evs := readDiskEvents(t, baseDir, proc.UUID)
	if suspends := diskEventsWithAction(evs, "ReasonStep", "quota_suspend"); len(suspends) != 0 {
		t.Errorf("quota_suspend events = %d, want 0 — the fast path must not fire on a past resetAt", len(suspends))
	}
	if retries := diskEventsWithAction(evs, "ReasonStep", "transient_retry"); len(retries) != 1 {
		t.Errorf("transient_retry events = %d, want 1 (local backoff, inside the cap)", len(retries))
	}
	if n := len(rec.snapshot()); n != 1 {
		t.Errorf("sleepFunc called %d times, want 1 (the local-backoff wait)", n)
	}
}

// -----------------------------------------------------------------------------
// AC2 — ResumeAt 盘往返与 daemon 重启恢复
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC2_DiskRoundTrip: quota suspend persists resume_at into
// proc-info.json; procInfoFromDisk restores it; LoadSuspendedFromDisk carries
// it onto the daemon-restart placeholder — the line the scanner's post-restart
// wakeup depends on (D1).
func TestATDD_73_3_AC2_DiskRoundTrip(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErrWithResetAt(resetAt)},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 AC2 disk round-trip")
	waitDone(t, proc)

	if st := proc.GetState(); st != types.StateSuspended {
		t.Fatalf("state = %s, want Suspended before inspecting the disk snapshot", st)
	}

	// ① The real suspend's proc-info.json carries resume_at (RFC3339Nano,
	// omitempty — present here because the value is non-zero).
	stepsDir := filepath.Join(baseDir, "steps", proc.UUID)
	raw, err := os.ReadFile(filepath.Join(stepsDir, "proc-info.json"))
	if err != nil {
		t.Fatalf("read proc-info.json: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("parse proc-info.json: %v", err)
	}
	if fields["resume_at"] != resetAt.Format(time.RFC3339Nano) {
		t.Errorf("proc-info.json resume_at = %v, want %v", fields["resume_at"], resetAt.Format(time.RFC3339Nano))
	}
	if fields["suspend_reason"] != SuspendReasonQuotaExhausted {
		t.Errorf("proc-info.json suspend_reason = %v, want %v", fields["suspend_reason"], SuspendReasonQuotaExhausted)
	}
	if fields["state"] != "suspended" {
		t.Errorf("proc-info.json state = %v, want suspended", fields["state"])
	}

	// ② procInfoFromDisk restores ResumeAt.
	var d procInfoDisk
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal procInfoDisk: %v", err)
	}
	info := procInfoFromDisk(d)
	if !info.ResumeAt.Equal(resetAt) {
		t.Errorf("procInfoFromDisk ResumeAt = %v, want %v", info.ResumeAt, resetAt)
	}

	// ③ A fresh kernel over the same dataDir rebuilds the placeholder WITH the
	// wake instant (the rehydrate sidecars stand in for the step records a
	// longer-running process would have left).
	writeRehydrateSidecars(t, stepsDir)

	reg2 := vfs.NewDeviceRegistry()
	_ = reg2.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &scriptedQuotaLLM{}, nil
	})
	k2 := NewKernel(vfs.NewVFS(reg2), rnixctx.NewManager(), nil)
	k2.SetDataDir(k.GetDataDir())
	t.Cleanup(k2.Shutdown)

	loaded, err := k2.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded < 1 {
		t.Fatalf("loaded = %d, want >= 1", loaded)
	}
	placeholder, ok := k2.GetProcessByUUID(proc.UUID)
	if !ok {
		t.Fatalf("placeholder for uuid=%s not in the restarted kernel's procTable", proc.UUID)
	}
	if got := placeholder.GetState(); got != types.StateSuspended {
		t.Errorf("placeholder state = %s, want Suspended", got)
	}
	if got := placeholder.GetSuspendReason(); got != SuspendReasonQuotaExhausted {
		t.Errorf("placeholder SuspendReason = %q, want %q", got, SuspendReasonQuotaExhausted)
	}
	if got := placeholder.GetResumeAt(); !got.Equal(resetAt) {
		t.Errorf("placeholder ResumeAt = %v, want %v — daemon restart must not lose the wake instant (D1)", got, resetAt)
	}
}

// TestATDD_73_3_AC2_OmittedOnDiskWhenZero: the fast-path suspension records no
// wake instant, and omitempty keeps legacy proc-info.json clean of the field.
func TestATDD_73_3_AC2_OmittedOnDiskWhenZero(t *testing.T) {
	quotaErr := llm.NewLLMError("claude", 429, llm.NewRateLimitError(llm.KindQuota, "insufficient_quota"))
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErr},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 AC2 zero resume_at")
	waitDone(t, proc)

	raw, err := os.ReadFile(filepath.Join(baseDir, "steps", proc.UUID, "proc-info.json"))
	if err != nil {
		t.Fatalf("read proc-info.json: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("parse proc-info.json: %v", err)
	}
	if _, present := fields["resume_at"]; present {
		t.Errorf("proc-info.json carries resume_at for a zero ResumeAt — omitempty must drop it")
	}
}

// -----------------------------------------------------------------------------
// AC3 — 扫描器唤醒 + 三反例
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC3_ScannerWakesDueProcess: a due quota-suspended process is
// woken by scanQuotaWakeups — state returns to Running, reasonStep restarts
// (the mock LLM receives the NEXT write), and quota_window_wake is emitted.
func TestATDD_73_3_AC3_ScannerWakesDueProcess(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErrWithResetAt(resetAt)},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 AC3 scanner wake")
	firstExit := waitDone(t, proc)
	if firstExit.Code != ExitSuspended {
		t.Fatalf("first exit = %+v, want the quota suspension", firstExit)
	}

	// Force due: the recorded instant lies in the past.
	proc.SetResumeAt(time.Now().Add(-time.Second))
	k.scanQuotaWakeups(time.Now())

	// reasonStep restarted: the second write succeeds and the process completes.
	secondExit := waitDone(t, proc)
	if secondExit.Code != 0 {
		t.Fatalf("post-wake exit = %+v, want success (reasonStep restarted and completed)", secondExit)
	}
	if n := primary.writeCount(); n != 2 {
		t.Errorf("LLM writes = %d, want 2 — the wake must restart reasonStep with the next LLM call", n)
	}

	evs := readDiskEvents(t, baseDir, proc.UUID)
	if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 1 {
		t.Fatalf("quota_window_wake events = %d, want exactly 1", len(wakes))
	} else if _, has := wakes[0].Args["resume_at"]; !has {
		t.Error("quota_window_wake lacks resume_at")
	}
}

// TestATDD_73_3_AC3_ScannerNegativeCases: ① not due → not woken; ② zero
// ResumeAt (fast path) → never woken by the scanner; ③ already manually
// resumed → silently skipped, never double-woken.
func TestATDD_73_3_AC3_ScannerNegativeCases(t *testing.T) {
	t.Run("not due stays suspended", func(t *testing.T) {
		resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
		primary := &scriptedQuotaLLM{
			writeErrs: []error{quotaErrWithResetAt(resetAt)},
			readData:  [][]byte{makeLLMResponse("done", 1)},
		}
		k, baseDir := newQuotaWakeKernel(t, primary)
		proc := spawnWithEventBase(t, k, baseDir, "73.3 AC3 not due")
		waitDone(t, proc)

		k.scanQuotaWakeups(time.Now()) // ResumeAt still +2h away

		if st := proc.GetState(); st != types.StateSuspended {
			t.Errorf("state = %s, want Suspended — a not-yet-due process must not wake", st)
		}
		evs := readDiskEvents(t, baseDir, proc.UUID)
		if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 0 {
			t.Errorf("quota_window_wake events = %d, want 0", len(wakes))
		}
	})

	t.Run("zero ResumeAt never wakes", func(t *testing.T) {
		quotaErr := llm.NewLLMError("claude", 429, llm.NewRateLimitError(llm.KindQuota, "insufficient_quota"))
		primary := &scriptedQuotaLLM{
			writeErrs: []error{quotaErr},
			readData:  [][]byte{makeLLMResponse("done", 1)},
		}
		k, baseDir := newQuotaWakeKernel(t, primary)
		proc := spawnWithEventBase(t, k, baseDir, "73.3 AC3 zero resume-at")
		waitDone(t, proc)

		// Scan with a far-future now: even "all instants have passed" must not
		// wake a manual-resume-only process.
		k.scanQuotaWakeups(time.Now().Add(1000 * time.Hour))

		if st := proc.GetState(); st != types.StateSuspended {
			t.Errorf("state = %s, want Suspended — a zero ResumeAt is manual-resume-only", st)
		}
	})

	t.Run("already manually resumed is not woken again", func(t *testing.T) {
		resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
		release := make(chan struct{})
		defer close(release)
		primary := &parkedWriteLLM{quotaErr: quotaErrWithResetAt(resetAt), release: release}
		k, baseDir := newQuotaWakeKernel(t, primary)
		proc := spawnWithEventBase(t, k, baseDir, "73.3 AC3 manual resume first")
		waitDone(t, proc)

		proc.SetResumeAt(time.Now().Add(-time.Second)) // due
		if _, _, err := k.ResumeSubtree(proc.PID); err != nil {
			t.Fatalf("ResumeSubtree: %v", err)
		}
		if st := proc.GetState(); st != types.StateRunning {
			t.Fatalf("state after manual resume = %s, want Running (parked in the LLM write)", st)
		}

		// The scanner must see a Running process and skip — no double wake.
		k.scanQuotaWakeups(time.Now())

		if st := proc.GetState(); st != types.StateRunning {
			t.Errorf("state after scan = %s, want still Running — the scanner must not touch a manually resumed process", st)
		}
		evs := readDiskEvents(t, baseDir, proc.UUID)
		if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 0 {
			t.Errorf("quota_window_wake events = %d, want 0 — wake belongs to the scanner alone here", len(wakes))
		}
	})
}

// -----------------------------------------------------------------------------
// D3 — 唤醒失败推迟
// -----------------------------------------------------------------------------

// TestATDD_73_3_D3_WakeFailurePostpones: when the wake cannot succeed (the LLM
// device is no longer registered), the process STAYS suspended — never killed
// — and ResumeAt is pushed back by quotaWakeRetryBackoff so the 60s scan does
// not turn into a hot loop. quota_wake_failed records the cause.
func TestATDD_73_3_D3_WakeFailurePostpones(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErrWithResetAt(resetAt)},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 D3 wake failure")
	waitDone(t, proc)

	// Break the reopen, then force due.
	proc.PrimaryDevice = "/dev/llm/ghost" // never registered
	before := time.Now()
	proc.SetResumeAt(before.Add(-time.Second))

	k.scanQuotaWakeups(before)

	if st := proc.GetState(); st != types.StateSuspended {
		t.Fatalf("state = %s, want Suspended — a failed wake must NOT kill or unsuspend", st)
	}
	if r := proc.GetSuspendReason(); r != SuspendReasonQuotaExhausted {
		t.Errorf("SuspendReason = %q, want %q", r, SuspendReasonQuotaExhausted)
	}
	resumeAt := proc.GetResumeAt()
	want := before.Add(quotaWakeRetryBackoff)
	if resumeAt.Before(want.Add(-30*time.Second)) || resumeAt.After(want.Add(30*time.Second)) {
		t.Errorf("ResumeAt = %v, want ≈ %v (postponed by %v from the failed attempt)", resumeAt, want, quotaWakeRetryBackoff)
	}

	evs := readDiskEvents(t, baseDir, proc.UUID)
	if failed := diskEventsWithAction(evs, "Resume", "quota_wake_failed"); len(failed) != 1 {
		t.Fatalf("quota_wake_failed events = %d, want exactly 1", len(failed))
	} else {
		if _, has := failed[0].Args["error"]; !has {
			t.Error("quota_wake_failed lacks the error field")
		}
	}
	if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 1 {
		t.Errorf("quota_window_wake events = %d, want 1 — the attempt is logged before the failure", len(wakes))
	}
}

// TestATDD_73_3_D3_NoPostponeWhenNoLongerQuotaSuspended: the D3 re-check — a
// process killed concurrently with the scan is silently skipped; the failure
// path must not push the wake instant of a process that is no longer waiting
// on a quota window.
func TestATDD_73_3_D3_NoPostponeWhenNoLongerQuotaSuspended(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErrWithResetAt(resetAt)},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 D3 killed re-check")
	waitDone(t, proc)

	staleResumeAt := time.Now().Add(-time.Second)
	proc.SetResumeAt(staleResumeAt)

	// Kill the suspended process, then drive both the collection predicate and
	// the under-lock re-check against the now-Dead process.
	if err := k.Kill(proc.PID, types.SIGKILL); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if st := proc.GetState(); st != types.StateDead {
		t.Fatalf("state after kill = %s, want Dead", st)
	}
	// Review P5 contract: the terminal kill leg itself clears the wake
	// instant (ResumeAt is zero whenever the process is not
	// quota-Suspended). Capture the post-kill value — the skipped scan below
	// must leave it untouched.
	postKillResumeAt := proc.GetResumeAt()
	if !postKillResumeAt.IsZero() {
		t.Fatalf("ResumeAt after kill = %v, want zero — the terminal leg clears the stale wake instant", postKillResumeAt)
	}

	k.scanQuotaWakeups(time.Now())       // collection predicate: state != Suspended → skip
	k.wakeQuotaProcess(proc, time.Now()) // re-check under resumeMu → silent skip

	if got := proc.GetResumeAt(); !got.Equal(postKillResumeAt) {
		t.Errorf("ResumeAt changed to %v after a skipped wake — a non-quota-Suspended process must never be postponed", got)
	}
	evs := readDiskEvents(t, baseDir, proc.UUID)
	if failed := diskEventsWithAction(evs, "Resume", "quota_wake_failed"); len(failed) != 0 {
		t.Errorf("quota_wake_failed events = %d, want 0 — the skip is silent", len(failed))
	}
	if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 0 {
		t.Errorf("quota_window_wake events = %d, want 0", len(wakes))
	}
}

// -----------------------------------------------------------------------------
// AC4 — gc 豁免回归
// -----------------------------------------------------------------------------

// writeQuotaSuspendedProcInfoFixture writes a proc-info.json shaped like a
// real quota suspension (state=suspended + suspend_reason + resume_at, no
// dead_at) for the gc exemption regression.
func writeQuotaSuspendedProcInfoFixture(t *testing.T, baseDir, uuid string, resumeAt time.Time) {
	t.Helper()
	dir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	info := map[string]any{
		"pid":            7303,
		"uuid":           uuid,
		"state":          "suspended",
		"intent":         "quota gc exemption fixture",
		"provider":       "claude",
		"model":          "claude-4",
		"tokens_used":    100,
		"created_at":     time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano),
		"suspend_reason": SuspendReasonQuotaExhausted,
		"resume_at":      resumeAt.Format(time.RFC3339Nano),
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), data, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}

// TestATDD_73_3_AC4_GcExemption: a quota-suspended snapshot survives a gc
// round that IS actively evicting (MaxEntries pressure removes the dead
// neighbours) — the state filter at gc.go:225 exempts everything that is not
// dead/zombie, and this test pins suspended inside that exemption.
func TestATDD_73_3_AC4_GcExemption(t *testing.T) {
	k := newThrottleTestKernel(t)
	k.SetGcConfig(GcConfig{RetentionDays: 0, MaxEntries: 1, IntervalSeconds: 3600})
	projBase := TestProjectBaseDir(k.GetDataDir())

	// Two stale DEAD entries: under MaxEntries=1 at least one is evicted, so
	// the suspended entry's survival cannot be explained away by an inert gc.
	deadUUID1 := timestampedUUID(1)
	deadUUID2 := timestampedUUID(2)
	writeProcInfoWithDeadAt(t, projBase, deadUUID1, "dead", time.Now().Add(-72*time.Hour).UTC().Format(time.RFC3339Nano))
	writeProcInfoWithDeadAt(t, projBase, deadUUID2, "dead", time.Now().Add(-96*time.Hour).UTC().Format(time.RFC3339Nano))

	quotaUUID := "quota-exempt-aaaa-bbbb-cccc-000000000073"
	writeQuotaSuspendedProcInfoFixture(t, projBase, quotaUUID, time.Now().Add(24*time.Hour))

	result, err := k.RunGc(false, true)
	if err != nil {
		t.Fatalf("RunGc: %v", err)
	}
	if result.RemovedCount < 1 {
		t.Fatalf("RemovedCount = %d, want >= 1 — gc must actually evict the dead entries, or the exemption below proves nothing", result.RemovedCount)
	}

	quotaDir := filepath.Join(projBase, "steps", quotaUUID)
	if _, statErr := os.Stat(filepath.Join(quotaDir, "proc-info.json")); statErr != nil {
		t.Errorf("quota-suspended proc-info.json was removed by gc (stat: %v) — suspended must stay exempt (D11)", statErr)
	}
}

// -----------------------------------------------------------------------------
// AC5 — resume 路径零 quota 分支 + ResumeAt 清空
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC5_ResumeOneClearsResumeAt: chain 1 — resumeOneForSubtree
// (the scanner's own mechanism) clears SuspendReason AND ResumeAt on wake, so
// the invariant's Running row holds and the scanner never matches the process
// again.
func TestATDD_73_3_AC5_ResumeOneClearsResumeAt(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	release := make(chan struct{})
	defer close(release)
	primary := &parkedWriteLLM{quotaErr: quotaErrWithResetAt(resetAt), release: release}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 AC5 chain 1")
	waitDone(t, proc)

	proc.SetResumeAt(time.Now().Add(-time.Second))
	k.resumeMu.Lock()
	err := k.resumeOneForSubtree(proc)
	k.resumeMu.Unlock()
	if err != nil {
		t.Fatalf("resumeOneForSubtree: %v", err)
	}

	// Parked inside the resumed step: Running with both fields cleared.
	if st := proc.GetState(); st != types.StateRunning {
		t.Fatalf("state = %s, want Running", st)
	}
	if r := proc.GetSuspendReason(); r != "" {
		t.Errorf("SuspendReason = %q after wake, want empty", r)
	}
	if ra := proc.GetResumeAt(); !ra.IsZero() {
		t.Errorf("ResumeAt = %v after wake, want zero — the scanner's re-check relies on this clearing", ra)
	}
}

// newQuotaCheckpointKernel wires the tool-call-capable kernel the AC5
// checkpoint/history chains need: scripted LLM (step 1 tool_call, step 2
// quota suspend, post-resume success) + a registered /dev/echo tool device.
func newQuotaCheckpointKernel(t *testing.T, primary vfs.VFSFile) (*KernelImpl, string) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return primary, nil
	})
	registerMockTool(reg, "/dev/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echo-ok")}, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	dataDir := t.TempDir()
	k.SetDataDir(dataDir)
	t.Cleanup(k.Shutdown)
	return k, filepath.Join(dataDir, "global")
}

// quotaWithToolStepScript returns the scripted LLM that drives one successful
// tool_call step and then fails the NEXT write with the quota error: write#1
// ok (step 1, tool call), write#2 quota over-cap (step 2, suspend), write#3+
// ok (post-resume). Reads: tool-call response, then "done".
func quotaWithToolStepScript(resetAt time.Time) *scriptedQuotaLLM {
	return &scriptedQuotaLLM{
		writeErrs: []error{nil, quotaErrWithResetAt(resetAt)},
		readData: [][]byte{
			makeToolCallResponse("/dev/echo", map[string]any{}, 1),
			makeLLMResponse("done", 1),
		},
	}
}

// TestATDD_73_3_AC5_CheckpointPathNoQuotaBranch: chain 2 — the last
// checkpoint before the quota suspension was written at a step boundary while
// Running (SuspendReason=""), so ResumeWithOpts via the checkpoint path goes
// straight into reasonStep: no compact branch fires, no quota-specific code
// runs, and the process completes.
func TestATDD_73_3_AC5_CheckpointPathNoQuotaBranch(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := quotaWithToolStepScript(resetAt)
	k, baseDir := newQuotaCheckpointKernel(t, primary)
	k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 1}) // step 1 checkpoints

	opts := failureRawSpawnOpts(baseDir)
	opts.AllowedDevices = []string{"/dev/echo"}
	pid, err := k.Spawn("73.3 AC5 checkpoint chain", nil, opts)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	firstExit := waitDone(t, proc)
	if firstExit.Code != ExitSuspended {
		t.Fatalf("first exit = %+v, want the quota suspension", firstExit)
	}

	// The checkpoint exists (step 1, written while Running) — this is what
	// routes ResumeWithOpts through resumeFromCheckpoint.
	uuid := proc.UUID
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, serr := os.Stat(filepath.Join(baseDir, "steps", uuid, "checkpoint.json")); serr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("checkpoint.json never appeared — the step-boundary write did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}

	res, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err != nil {
		t.Fatalf("ResumeWithOpts: %v", err)
	}
	resumed, ok := k.GetProcess(res.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	finalExit := waitDone(t, resumed)
	if finalExit.Code != 0 {
		t.Fatalf("post-resume exit = %+v, want success via the checkpoint path", finalExit)
	}

	evs := readDiskEvents(t, baseDir, uuid)
	if compacts := diskEventsWithSyscall(evs, "Compact"); len(compacts) != 0 {
		t.Errorf("Compact events = %d, want 0 — cp.SuspendReason is empty at the step boundary, the context_full compact branch must not fire", len(compacts))
	}
}

// TestATDD_73_3_AC5_HistoryPathNoQuotaBranch: chain 3 — with no checkpoint
// (default 5-step interval, only one completed step), resume takes the
// history path; history_reconcile normalizes SuspendReason to "" and
// isContextFull is false for "suspended: quota_exhausted" — again no compact
// branch, and the process completes.
func TestATDD_73_3_AC5_HistoryPathNoQuotaBranch(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := quotaWithToolStepScript(resetAt)
	k, baseDir := newQuotaCheckpointKernel(t, primary)
	// Default checkpoint config (5 steps): step 1 does NOT checkpoint, so the
	// suspended process has step records but no checkpoint.json.

	opts := failureRawSpawnOpts(baseDir)
	opts.AllowedDevices = []string{"/dev/echo"}
	pid, err := k.Spawn("73.3 AC5 history chain", nil, opts)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	firstExit := waitDone(t, proc)
	if firstExit.Code != ExitSuspended {
		t.Fatalf("first exit = %+v, want the quota suspension", firstExit)
	}
	uuid := proc.UUID
	if _, serr := os.Stat(filepath.Join(baseDir, "steps", uuid, "checkpoint.json")); serr == nil {
		t.Fatal("checkpoint.json present — the history chain requires its absence")
	}

	res, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err != nil {
		t.Fatalf("ResumeWithOpts: %v", err)
	}
	resumed, ok := k.GetProcess(res.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	finalExit := waitDone(t, resumed)
	if finalExit.Code != 0 {
		t.Fatalf("post-resume exit = %+v, want success via the history path", finalExit)
	}

	evs := readDiskEvents(t, baseDir, uuid)
	if len(diskEventsWithSyscall(evs, "ResumeFromHistory")) == 0 {
		t.Error("no ResumeFromHistory event — the history path was not taken")
	}
	if compacts := diskEventsWithSyscall(evs, "Compact"); len(compacts) != 0 {
		t.Errorf("Compact events = %d, want 0 — quota_exhausted is not context_full, no compact branch", len(compacts))
	}
}

// -----------------------------------------------------------------------------
// AC6 — LLM 零感知
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC6_NoContextInjection: suspend → wake → suspend again leaves
// the context message count EXACTLY unchanged (D8). Any implementation that
// injects a "you were suspended" message goes red here.
func TestATDD_73_3_AC6_NoContextInjection(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	quotaErr := quotaErrWithResetAt(resetAt)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErr, quotaErr},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 AC6 zero injection")

	waitDone(t, proc) // first quota suspension
	before := ctxMsgCount(t, k, proc)
	if before < 1 {
		t.Fatalf("context messages before wake = %d, want >= 1 (the spawn intent)", before)
	}

	proc.SetResumeAt(time.Now().Add(-time.Second))
	k.scanQuotaWakeups(time.Now())

	waitDone(t, proc) // woken, second write fails quota, suspended again
	after := ctxMsgCount(t, k, proc)

	if after != before {
		t.Errorf("context messages changed across suspend→wake→suspend: %d → %d — the LLM must be UNAWARE of the quota suspension (D8)", before, after)
	}
	if st := proc.GetState(); st != types.StateSuspended {
		t.Fatalf("state = %s, want Suspended (second suspension)", st)
	}
}

// -----------------------------------------------------------------------------
// AC8 — invariant 护栏
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC8_InvariantHoldsWhenQuotaSuspended is the permanent
// guardrail (shape of 71.4's latch guardrail): the quota-suspended snapshot
// validates the invariant matrix, and — crucially — the woken Running snapshot
// does too, proving the resumeOneForSubtree clearing leaves no SuspendReason
// leak on a Running process.
func TestATDD_73_3_AC8_InvariantHoldsWhenQuotaSuspended(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	release := make(chan struct{})
	defer close(release)
	primary := &parkedWriteLLM{quotaErr: quotaErrWithResetAt(resetAt), release: release}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 AC8 invariant")
	waitDone(t, proc)

	// Suspended snapshot: (Suspended × quota_exhausted × "suspended: …") is a
	// legal matrix row.
	snap, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo (suspended): %v", err)
	}
	if snap.SuspendReason != SuspendReasonQuotaExhausted {
		t.Fatalf("SuspendReason = %q, want %q", snap.SuspendReason, SuspendReasonQuotaExhausted)
	}
	if err := ValidateProcInfoInvariant(snap); err != nil {
		t.Errorf("invariant violated for the quota-suspended snapshot: %v", err)
	}

	// Wake and snapshot the Running state: SuspendReason must be cleared.
	proc.SetResumeAt(time.Now().Add(-time.Second))
	k.scanQuotaWakeups(time.Now())
	if st := proc.GetState(); st != types.StateRunning {
		t.Fatalf("state after wake = %s, want Running (parked in the LLM write)", st)
	}
	runningSnap, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo (running): %v", err)
	}
	if runningSnap.SuspendReason != "" {
		t.Errorf("Running snapshot carries SuspendReason %q — the wake-time clearing is the invariant's guard", runningSnap.SuspendReason)
	}
	if err := ValidateProcInfoInvariant(runningSnap); err != nil {
		t.Errorf("invariant violated for the post-wake Running snapshot: %v", err)
	}
}

// -----------------------------------------------------------------------------
// D4 — supervisor gate 三态
// -----------------------------------------------------------------------------

// newQuotaSupervisorKernel builds a kernel whose LLM factory routes through a
// shared quotaOnceRouter: the first Write suspends the child, and every Write
// after that (including the wake-time reopened FD) succeeds.
func newQuotaSupervisorKernel(t *testing.T, quotaErr error) *KernelImpl {
	t.Helper()
	return newQuotaSupervisorKernelFailN(t, quotaErr, 1)
}

// newQuotaSupervisorKernelFailN is the same fixture with a router that fails
// the first N writes across all FDs — the wake-rehit-loop test needs two
// suspensions (failTimes=2).
func newQuotaSupervisorKernelFailN(t *testing.T, quotaErr error, failTimes int) *KernelImpl {
	t.Helper()
	router := &quotaOnceRouter{quotaErr: quotaErr, failTimes: failTimes}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return router.newFile(), nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	t.Cleanup(k.Shutdown)
	return k
}

// waitForSyscallEvent drains proc.DebugChan until an event with the wanted
// syscall arrives (or the deadline kills the test). Returns everything
// collected so far, including the match.
func waitForSyscallEvent(t *testing.T, proc *Process, syscall string, timeout time.Duration) []types.SyscallEvent {
	t.Helper()
	var collected []types.SyscallEvent
	deadline := time.Now().Add(timeout)
	for {
		select {
		case ev := <-proc.DebugChan:
			collected = append(collected, ev)
			if ev.Syscall == syscall {
				return collected
			}
		case <-time.After(5 * time.Millisecond):
			if time.Now().After(deadline) {
				names := make([]string, 0, len(collected))
				for _, ev := range collected {
					names = append(names, ev.Syscall)
				}
				t.Fatalf("timed out waiting for %s event; saw: %v", syscall, names)
			}
		}
	}
}

// drainEvents collects further events for a fixed window after the expected
// signal has been observed — the window in which a WRONG restart would show.
func drainProcEventsFor(proc *Process, d time.Duration) []types.SyscallEvent {
	var out []types.SyscallEvent
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		select {
		case ev := <-proc.DebugChan:
			out = append(out, ev)
		case <-time.After(5 * time.Millisecond):
		}
	}
	return out
}

func countSupEvents(evs []types.SyscallEvent, syscall string) int {
	n := 0
	for _, ev := range evs {
		if ev.Syscall == syscall {
			n++
		}
	}
	return n
}

func childPIDFromEvent(t *testing.T, ev types.SyscallEvent) types.PID {
	t.Helper()
	switch v := ev.Args["child_pid"].(type) {
	case types.PID:
		return v
	case uint64:
		return types.PID(v)
	default:
		t.Fatalf("child_pid has unexpected type %T in event %+v", ev.Args["child_pid"], ev)
		return 0
	}
}

// TestATDD_73_3_D4_SupervisorGate_QuotaChildNotRestarted: ① a quota-suspended
// supervised child is NOT restarted — even under the permanent policy that
// restarts everything else. The supervisor emits SupervisorChildSuspended (with
// resume_at), keeps the child alive-flag, and waits for the window instead of
// burning its restart budget on a thundering herd.
func TestATDD_73_3_D4_SupervisorGate_QuotaChildNotRestarted(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	k := newQuotaSupervisorKernel(t, quotaErrWithResetAt(resetAt))

	supPID, err := k.SpawnSupervisor(SupervisorSpec{
		Strategy: OneForOne,
		Children: []ChildSpec{{Name: "worker", Intent: "quota worker", Restart: RestartPermanent}},
	})
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}
	supProc, _ := k.GetProcess(supPID)

	evs := waitForSyscallEvent(t, supProc, "SupervisorChildSuspended", 5*time.Second)
	// Give a wrongful restart a generous window to appear before counting.
	evs = append(evs, drainProcEventsFor(supProc, 300*time.Millisecond)...)

	if n := countSupEvents(evs, "SupervisorChildSuspended"); n != 1 {
		t.Errorf("SupervisorChildSuspended events = %d, want exactly 1", n)
	}
	var suspEvent *types.SyscallEvent
	for i := range evs {
		if evs[i].Syscall == "SupervisorChildSuspended" {
			suspEvent = &evs[i]
			break
		}
	}
	if suspEvent == nil {
		t.Fatal("no SupervisorChildSuspended event collected")
	}
	if suspEvent.Args["resume_at"] != resetAt.Format(time.RFC3339) {
		t.Errorf("SupervisorChildSuspended resume_at = %v, want %v", suspEvent.Args["resume_at"], resetAt.Format(time.RFC3339))
	}
	if n := countSupEvents(evs, "SupervisorRestart"); n != 0 {
		t.Errorf("SupervisorRestart events = %d, want 0 — the quota child must NOT be restarted", n)
	}
	if n := countSupEvents(evs, "SupervisorStartChild"); n != 1 {
		t.Errorf("SupervisorStartChild events = %d, want 1 — no replacement child may be spawned", n)
	}

	// The child stays Suspended; the supervisor stays Running (allDone false).
	childPID := childPIDFromEvent(t, *suspEvent)
	childProc, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatal("child process not found")
	}
	if st := childProc.GetState(); st != types.StateSuspended {
		t.Errorf("child state = %s, want Suspended", st)
	}
	if st := supProc.GetState(); st != types.StateRunning {
		t.Errorf("supervisor state = %s, want Running — it waits for the window", st)
	}

	// Cleanup: kill the tree.
	_ = k.Kill(supPID, types.SIGKILL)
}

// TestATDD_73_3_D4_SupervisorGate_WakeThenNormalExit: ② after the scanner
// wakes the child, the reasonStep restarted on the SAME process object writes
// its terminal Done to the same channel; the re-armed monitor delivers it to a
// SECOND handleChildExit round, which runs the normal shouldRestart path
// (temporary policy + clean exit → no restart, supervisor completes).
func TestATDD_73_3_D4_SupervisorGate_WakeThenNormalExit(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	k := newQuotaSupervisorKernel(t, quotaErrWithResetAt(resetAt))

	supPID, err := k.SpawnSupervisor(SupervisorSpec{
		Strategy: OneForOne,
		Children: []ChildSpec{{Name: "worker", Intent: "quota worker", Restart: RestartTemporary}},
	})
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}
	supProc, _ := k.GetProcess(supPID)

	evs := waitForSyscallEvent(t, supProc, "SupervisorChildSuspended", 5*time.Second)
	var suspEvent *types.SyscallEvent
	for i := range evs {
		if evs[i].Syscall == "SupervisorChildSuspended" {
			suspEvent = &evs[i]
			break
		}
	}
	if suspEvent == nil {
		t.Fatal("no SupervisorChildSuspended event collected")
	}
	childPID := childPIDFromEvent(t, *suspEvent)
	childProc, _ := k.GetProcess(childPID)

	// Wake: the same process object resumes, completes, and the supervisor
	// observes a NORMAL child exit through the ordinary path.
	childProc.SetResumeAt(time.Now().Add(-time.Second))
	k.scanQuotaWakeups(time.Now())

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("supervisor exit = %+v, want clean completion after the woken child's normal exit", exit)
	}

	evs = append(evs, drainProcEventsFor(supProc, 100*time.Millisecond)...)
	if n := countSupEvents(evs, "SupervisorStartChild"); n != 1 {
		t.Errorf("SupervisorStartChild events = %d, want 1 — the second round must NOT restart (temporary + clean exit)", n)
	}
	if n := countSupEvents(evs, "SupervisorChildExit"); n != 1 {
		t.Errorf("SupervisorChildExit events = %d, want 1 — the second handleChildExit round ran the normal path", n)
	}
	if n := countSupEvents(evs, "SupervisorChildSuspended"); n != 1 {
		t.Errorf("SupervisorChildSuspended events = %d, want exactly 1 (the gate fires once, before the wake)", n)
	}
}

// TestATDD_73_3_D4_SupervisorGate_KilledWhileSuspended: ③ a child KILLED
// during the quota suspension must not be mistaken for a window-waiter (the
// state guard: killSuspendedProcess leaves it Dead and does not reliably clear
// SuspendReason). The second round takes the normal exit path, the supervisor
// completes instead of hanging forever.
func TestATDD_73_3_D4_SupervisorGate_KilledWhileSuspended(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	k := newQuotaSupervisorKernel(t, quotaErrWithResetAt(resetAt))

	supPID, err := k.SpawnSupervisor(SupervisorSpec{
		Strategy: OneForOne,
		Children: []ChildSpec{{Name: "worker", Intent: "quota worker", Restart: RestartTemporary}},
	})
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}
	supProc, _ := k.GetProcess(supPID)

	evs := waitForSyscallEvent(t, supProc, "SupervisorChildSuspended", 5*time.Second)
	var suspEvent *types.SyscallEvent
	for i := range evs {
		if evs[i].Syscall == "SupervisorChildSuspended" {
			suspEvent = &evs[i]
			break
		}
	}
	if suspEvent == nil {
		t.Fatal("no SupervisorChildSuspended event collected")
	}
	childPID := childPIDFromEvent(t, *suspEvent)
	childProc, _ := k.GetProcess(childPID)

	// Kill while suspended — the gate's state guard must recognize the Dead
	// child on the second round and NOT hold the supervisor waiting for a
	// window that no process will ever wake.
	if err := k.Kill(childPID, types.SIGKILL); err != nil {
		t.Fatalf("Kill child: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("supervisor exit = %+v, want completion — the killed child must route through the normal exit path, not a permanent wait", exit)
	}

	evs = append(evs, drainProcEventsFor(supProc, 100*time.Millisecond)...)
	if n := countSupEvents(evs, "SupervisorChildExit"); n != 1 {
		t.Errorf("SupervisorChildExit events = %d, want 1 — the killed child went through the normal shouldRestart path", n)
	}
	if n := countSupEvents(evs, "SupervisorChildSuspended"); n != 1 {
		t.Errorf("SupervisorChildSuspended events = %d, want exactly 1 — the gate must not re-fire for a Dead child", n)
	}
	if n := countSupEvents(evs, "SupervisorStartChild"); n != 1 {
		t.Errorf("SupervisorStartChild events = %d, want 1 — no restart of the killed child (temporary policy)", n)
	}
	if st := childProc.GetState(); st != types.StateDead && st != types.StateZombie {
		t.Errorf("child state = %s, want a terminal state", st)
	}
}

// -----------------------------------------------------------------------------
// D6 — 无等待信息的 overload 永不挂起
// -----------------------------------------------------------------------------

// TestATDD_73_3_D6_WaitlessOverloadNeverSuspends: the production 503 shape —
// empty body, no Retry-After — carries no window information, so it must stay
// on the legacy path: in-cap local backoff, budget exhaustion, fallback,
// death. Suspending on it would PRETEND to know when the window recovers.
func TestATDD_73_3_D6_WaitlessOverloadNeverSuspends(t *testing.T) {
	installSleepRecorder(t, false, 0)
	overloadErr := llm.NewLLMError("qwen", 503, llm.NewRateLimitError(llm.KindOverload, ""))
	errs := make([]error, 8) // far more failures than any budget
	for i := range errs {
		errs[i] = overloadErr
	}
	primary := &writeErrSequenceLLM{errs: errs}
	proc, exit, steps := runBackoffProcess(t, primary, "73.3 D6 waitless overload")

	if exit.Code == 0 {
		t.Fatal("exit code 0 — a relentless waitless overload must stay fatal")
	}
	if exit.Code == ExitSuspended {
		t.Fatal("exit code ExitSuspended — a waitless overload must never suspend")
	}
	if st := proc.GetState(); st == types.StateSuspended {
		t.Fatalf("state = %s — waitless overload runs the old path to its old end", st)
	}
	if suspends := filterAction(steps, "quota_suspend"); len(suspends) != 0 {
		t.Errorf("quota_suspend events = %d, want 0 — no window info, no suspension", len(suspends))
	}
	if retries := filterAction(steps, "transient_retry"); len(retries) != maxRateLimitRetries {
		t.Errorf("transient_retry events = %d, want %d — local backoff (≤30s) stays inside the cap and consumes the retry budget", len(retries), maxRateLimitRetries)
	}
}

// =============================================================================
// code-review 2026-08-06 补强测试（Review Findings P8/P9）
// =============================================================================

// -----------------------------------------------------------------------------
// P8 — AC2-6 钉住：resume 后盘上旧 resume_at 被抹掉
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC2_DiskErasureOnResume pins AC2-6: the quota suspension
// persists resume_at into proc-info.json; after a checkpoint- or
// history-path resume the SAME file must no longer carry the key — the new
// process's zero ResumeAt plus omitempty erases the stale value. Behaviour
// holds structurally (new process + omitempty); the spec demands the pin.
func TestATDD_73_3_AC2_DiskErasureOnResume(t *testing.T) {
	run := func(t *testing.T, checkpointing bool) {
		resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
		primary := quotaWithToolStepScript(resetAt)
		k, baseDir := newQuotaCheckpointKernel(t, primary)
		if checkpointing {
			k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 1})
		}

		opts := failureRawSpawnOpts(baseDir)
		opts.AllowedDevices = []string{"/dev/echo"}
		pid, err := k.Spawn("73.3 review P8 disk erasure", nil, opts)
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		proc, _ := k.GetProcess(pid)
		firstExit := waitDone(t, proc)
		if firstExit.Code != ExitSuspended {
			t.Fatalf("first exit = %+v, want the quota suspension", firstExit)
		}
		uuid := proc.UUID

		if checkpointing {
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, serr := os.Stat(filepath.Join(baseDir, "steps", uuid, "checkpoint.json")); serr == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("checkpoint.json never appeared")
				}
				time.Sleep(5 * time.Millisecond)
			}
		}

		// The suspend leg persisted resume_at.
		infoPath := filepath.Join(baseDir, "steps", uuid, "proc-info.json")
		raw, err := os.ReadFile(infoPath)
		if err != nil {
			t.Fatalf("read proc-info.json: %v", err)
		}
		var before map[string]any
		if err := json.Unmarshal(raw, &before); err != nil {
			t.Fatalf("parse proc-info.json: %v", err)
		}
		if _, present := before["resume_at"]; !present {
			t.Fatal("proc-info.json lacks resume_at before resume — the suspend leg must persist it")
		}

		res, err := k.ResumeWithOpts(uuid, ResumeOpts{})
		if err != nil {
			t.Fatalf("ResumeWithOpts: %v", err)
		}
		resumed, ok := k.GetProcess(res.PID)
		if !ok {
			t.Fatal("resumed process not in procTable")
		}
		finalExit := waitDone(t, resumed)
		if finalExit.Code != 0 {
			t.Fatalf("post-resume exit = %+v, want success", finalExit)
		}
		k.Reap(res.PID) // persist the terminal snapshot

		raw, err = os.ReadFile(infoPath)
		if err != nil {
			t.Fatalf("re-read proc-info.json: %v", err)
		}
		var after map[string]any
		if err := json.Unmarshal(raw, &after); err != nil {
			t.Fatalf("re-parse proc-info.json: %v", err)
		}
		if v, present := after["resume_at"]; present {
			t.Errorf("proc-info.json still carries resume_at=%v after resume — the new process's zero ResumeAt + omitempty must erase the stale value (AC2-6)", v)
		}
	}
	t.Run("checkpoint path", func(t *testing.T) { run(t, true) })
	t.Run("history path", func(t *testing.T) { run(t, false) })
}

// -----------------------------------------------------------------------------
// P9① — daemon 重启恢复 + 扫描唤醒一条链
// -----------------------------------------------------------------------------

// TestATDD_73_3_AC2_RestartThenScannerWakeChain: the two AC2/AC3 halves in ONE
// chain — quota suspend persists resume_at, a fresh kernel over the same
// dataDir rehydrates the placeholder WITH the wake instant, and the scanner
// wakes it there: reasonStep restarts on the restarted daemon's device and
// the process completes. This is the "95.5h window survives daemon restarts"
// value proposition end to end.
func TestATDD_73_3_AC2_RestartThenScannerWakeChain(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErrWithResetAt(resetAt)},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 restart+wake chain")
	waitDone(t, proc)
	if st := proc.GetState(); st != types.StateSuspended {
		t.Fatalf("state = %s, want Suspended", st)
	}
	// Guarantee the rehydrate sidecars in the 44.3 fixture shape (a longer
	// running process would have left richer step records; the scanner chain
	// only needs a rehydratable placeholder).
	writeRehydrateSidecars(t, filepath.Join(baseDir, "steps", proc.UUID))

	// --- daemon restart: fresh kernel, same dataDir, device re-registered ---
	primary2 := &scriptedQuotaLLM{readData: [][]byte{makeLLMResponse("done", 1)}}
	reg2 := vfs.NewDeviceRegistry()
	_ = reg2.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return primary2, nil
	})
	k2 := NewKernel(vfs.NewVFS(reg2), rnixctx.NewManager(), nil)
	k2.SetDataDir(k.GetDataDir())
	t.Cleanup(k2.Shutdown)

	loaded, err := k2.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded < 1 {
		t.Fatalf("loaded = %d, want >= 1", loaded)
	}
	placeholder, ok := k2.GetProcessByUUID(proc.UUID)
	if !ok {
		t.Fatal("placeholder not in the restarted kernel's procTable")
	}
	if got := placeholder.GetResumeAt(); !got.Equal(resetAt) {
		t.Fatalf("placeholder ResumeAt = %v, want %v — restart must not lose the wake instant (D1)", got, resetAt)
	}

	// --- the window recovers; the restarted daemon's scanner wakes it ---
	placeholder.SetResumeAt(time.Now().Add(-time.Second))
	k2.scanQuotaWakeups(time.Now())

	exit := waitDone(t, placeholder)
	if exit.Code != 0 {
		t.Fatalf("post-restart wake exit = %+v, want success — the placeholder must resume reasoning on the new daemon", exit)
	}
	if n := primary2.writeCount(); n != 1 {
		t.Errorf("restarted daemon LLM writes = %d, want 1 — the wake must restart reasonStep with a fresh LLM call", n)
	}
	evs := readDiskEvents(t, baseDir, placeholder.UUID)
	if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 1 {
		t.Errorf("quota_window_wake events after restart = %d, want 1", len(wakes))
	}
}

// -----------------------------------------------------------------------------
// P9② — supervisor 唤醒→再撞配额→重入 gate 的自然循环
// -----------------------------------------------------------------------------

// TestATDD_73_3_D4_SupervisorGate_WakeRehitLoop: the D4 retry loop the gate
// comment promises — wake → the window was NOT truly recovered → the child
// suspends again → the re-armed monitor delivers the second ExitSuspended →
// the gate fires AGAIN. Throughout: no restart, no replacement child, the
// supervisor alive-flag untouched.
func TestATDD_73_3_D4_SupervisorGate_WakeRehitLoop(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	k := newQuotaSupervisorKernelFailN(t, quotaErrWithResetAt(resetAt), 2)

	supPID, err := k.SpawnSupervisor(SupervisorSpec{
		Strategy: OneForOne,
		Children: []ChildSpec{{Name: "worker", Intent: "quota worker", Restart: RestartPermanent}},
	})
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}
	supProc, _ := k.GetProcess(supPID)
	defer func() { _ = k.Kill(supPID, types.SIGKILL) }()

	// First suspension: the gate fires (event 1) and re-arms the monitor.
	evs := waitForSyscallEvent(t, supProc, "SupervisorChildSuspended", 5*time.Second)
	childPID := childPIDFromEvent(t, evs[len(evs)-1])
	childProc, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatal("child process not found")
	}

	// Wake the child; its next write hits quota AGAIN (router fails twice).
	childProc.SetResumeAt(time.Now().Add(-time.Second))
	k.scanQuotaWakeups(time.Now())

	// The re-armed monitor delivers the second ExitSuspended; the gate must
	// re-fire — the natural retry loop, one LLM request per turn.
	evs = append(evs, waitForSyscallEvent(t, supProc, "SupervisorChildSuspended", 5*time.Second)...)
	evs = append(evs, drainProcEventsFor(supProc, 300*time.Millisecond)...)

	if n := countSupEvents(evs, "SupervisorChildSuspended"); n != 2 {
		t.Errorf("SupervisorChildSuspended events = %d, want 2 — the gate must re-fire after the rehit", n)
	}
	if n := countSupEvents(evs, "SupervisorStartChild"); n != 1 {
		t.Errorf("SupervisorStartChild events = %d, want 1 — no replacement child across the whole loop", n)
	}
	if n := countSupEvents(evs, "SupervisorRestart"); n != 0 {
		t.Errorf("SupervisorRestart events = %d, want 0", n)
	}
	if st := childProc.GetState(); st != types.StateSuspended {
		t.Errorf("child state = %s, want Suspended again after the rehit", st)
	}
	if got := childProc.GetResumeAt(); got.IsZero() || got.Before(time.Now()) {
		t.Errorf("child ResumeAt = %v, want the rehit's fresh future reset instant", got)
	}
	if st := supProc.GetState(); st != types.StateRunning {
		t.Errorf("supervisor state = %s, want Running — still waiting for the window", st)
	}
}

// -----------------------------------------------------------------------------
// P9③ — D12：手动 resume 早于 ResumeAt 不拦截
// -----------------------------------------------------------------------------

// TestATDD_73_3_D12_ManualResumeBeforeResumeAtNotBlocked: D12 — rnix resume /
// SIGRESUME ahead of the recorded reset instant is NOT gated. The operator
// may know the quota recovered early; the scanner automates what the
// operator could always do manually. No warn, no block, no scanner event.
func TestATDD_73_3_D12_ManualResumeBeforeResumeAtNotBlocked(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := &scriptedQuotaLLM{
		writeErrs: []error{quotaErrWithResetAt(resetAt)},
		readData:  [][]byte{makeLLMResponse("done", 1)},
	}
	k, baseDir := newQuotaWakeKernel(t, primary)
	proc := spawnWithEventBase(t, k, baseDir, "73.3 D12 early manual resume")
	waitDone(t, proc)

	// ResumeAt lies ~2h in the future — the manual resume must not care.
	if got := proc.GetResumeAt(); got.Before(time.Now().Add(time.Hour)) {
		t.Fatalf("ResumeAt = %v, want far-future for this test to mean anything", got)
	}
	if _, _, err := k.ResumeSubtree(proc.PID); err != nil {
		t.Fatalf("ResumeSubtree ahead of ResumeAt: %v — D12 forbids any gate", err)
	}

	exit := waitDone(t, proc)
	if exit.Code != 0 {
		t.Fatalf("post-resume exit = %+v, want success", exit)
	}
	evs := readDiskEvents(t, baseDir, proc.UUID)
	if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 0 {
		t.Errorf("quota_window_wake events = %d, want 0 — this wake belongs to the operator, not the scanner", len(wakes))
	}
}

// -----------------------------------------------------------------------------
// P9④ — scanner vs 手动 resume 并发竞态回归（P1/P2 修复的牙齿）
// -----------------------------------------------------------------------------

// TestATDD_73_3_ScannerSkipsDetachedPlaceholder: the P1 regression — a
// non-fork manual resume racing the scanner deletes the old placeholder from
// procTable WITHOUT transitioning it (it still reads Suspended/quota/due).
// wakeQuotaProcess on the stale pointer must silently skip: no revival, no
// reasonStep on the freed CtxID, no double agent run.
func TestATDD_73_3_ScannerSkipsDetachedPlaceholder(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	primary := quotaWithToolStepScript(resetAt)
	k, baseDir := newQuotaCheckpointKernel(t, primary)
	k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 1})

	opts := failureRawSpawnOpts(baseDir)
	opts.AllowedDevices = []string{"/dev/echo"}
	pid, err := k.Spawn("73.3 P1 detached placeholder", nil, opts)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	oldProc, _ := k.GetProcess(pid)
	waitDone(t, oldProc)
	uuid := oldProc.UUID

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, serr := os.Stat(filepath.Join(baseDir, "steps", uuid, "checkpoint.json")); serr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("checkpoint.json never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Make the old placeholder due, then race: manual resume deletes it from
	// procTable (cleanupOldProcessAndHistory) while the scanner "holds" the
	// stale pointer.
	oldProc.SetResumeAt(time.Now().Add(-time.Second))
	res, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err != nil {
		t.Fatalf("ResumeWithOpts: %v", err)
	}
	resumed, ok := k.GetProcess(res.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	if res.PID == oldProc.PID {
		t.Fatal("resume reused the old PID — this test needs the detach shape")
	}
	if _, still := k.GetProcess(oldProc.PID); still {
		t.Fatal("old placeholder still in procTable after non-fork resume")
	}
	writesBeforeStaleWake := primary.writeCount()

	// The stale wake: the exact call the scanner would make on the collected
	// pointer. The membership re-check must reject the husk.
	k.wakeQuotaProcess(oldProc, time.Now())

	if st := oldProc.GetState(); st != types.StateSuspended {
		t.Errorf("detached placeholder state = %s after the stale wake, want untouched Suspended — it must never be revived", st)
	}
	if n := primary.writeCount(); n != writesBeforeStaleWake {
		t.Errorf("LLM writes grew %d → %d on the stale wake — reasonStep ran on the husk (freed CtxID, double agent run)", writesBeforeStaleWake, n)
	}
	if got, present := k.GetProcessByUUID(uuid); !present || got.PID != res.PID {
		t.Errorf("uuid lookup after stale wake = (pid=%v, ok=%v), want the resumed pid=%d alone", got.GetPID(), present, res.PID)
	}
	evs := readDiskEvents(t, baseDir, uuid)
	if wakes := diskEventsWithAction(evs, "Resume", "quota_window_wake"); len(wakes) != 0 {
		t.Errorf("quota_window_wake events = %d, want 0 — the stale wake must be silent", len(wakes))
	}

	finalExit := waitDone(t, resumed)
	if finalExit.Code != 0 {
		t.Fatalf("resumed process exit = %+v, want success — the legitimate resume is unaffected", finalExit)
	}
}

// newManualSupervisorFixture builds the TOCTOU fixtures' supervisor BY HAND
// (SpawnSupervisor's run loop would consume exitCh itself; these tests need
// to inject the stale event at a controlled instant). Returns the supervisor
// and its process.
func newManualSupervisorFixture(t *testing.T, k *KernelImpl, child *Process) *Supervisor {
	t.Helper()
	spec := SupervisorSpec{
		Strategy: OneForOne,
		Children: []ChildSpec{{Name: "worker", Intent: child.Intent, Restart: RestartPermanent}},
	}
	supProc := NewProcess(0, "supervisor:toctou-fixture", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	ctx = ContextWithPID(ctx, supProc.PID)
	supProc.cancel = cancel
	supProc.ctx = ctx
	if err := supProc.Start(); err != nil {
		t.Fatalf("start supervisor fixture proc: %v", err)
	}
	k.AddProcess(supProc)
	sup := newSupervisor(supProc, spec, k)
	sup.children[0] = &supervisedChild{
		spec:  spec.Children[0],
		pid:   child.PID,
		uuid:  child.UUID,
		index: 0,
		alive: true,
	}
	return sup
}

// drainStaleSuspendDone consumes the suspend-leg ExitSuspended from proc.Done
// — standing in for the original monitor goroutine that delivered it into
// exitCh in the real flow.
func drainStaleSuspendDone(proc *Process) {
	select {
	case <-proc.Done:
	default:
	}
}

// waitUntilState polls proc until it reaches want (or the deadline kills the
// test).
func waitUntilState(t *testing.T, proc *Process, want types.ProcessState) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if st := proc.GetState(); st == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("state = %s, never reached %s within 3s", proc.GetState(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// quotaSuspendExitStatus is the stale event payload: the ExitStatus
// notifySuspendDone delivers for a quota suspension (suspendProcess stamps
// "suspended: <reason>").
func quotaSuspendExitStatus() ExitStatus {
	return ExitStatus{Code: ExitSuspended, Reason: "suspended: " + SuspendReasonQuotaExhausted}
}

// TestATDD_73_3_SupervisorStaleEventAfterWake_NoRestart: the P2 regression —
// the ExitSuspended event sits in exitCh while the supervisor loop is busy;
// within that window the child is woken; the loop then processes the STALE
// event. The event-based gate + Running dispatch must re-arm the monitor,
// NOT restart: no duplicate child, no zombie leak, and the re-armed monitor
// still delivers the real terminal exit.
func TestATDD_73_3_SupervisorStaleEventAfterWake_NoRestart(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseNow := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseNow()
	primary := &parkedWriteLLM{quotaErr: quotaErrWithResetAt(resetAt), release: release}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return primary, nil
	})
	k := NewKernel(vfs.NewVFS(reg), rnixctx.NewManager(), nil)
	t.Cleanup(k.Shutdown)

	childPID, err := k.Spawn("73.3 P2 stale event after wake", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}
	childProc, _ := k.GetProcess(childPID)
	waitDone(t, childProc) // quota suspension; Done carries ExitSuspended
	sup := newManualSupervisorFixture(t, k, childProc)
	drainStaleSuspendDone(childProc) // the original monitor already delivered it

	// Wake the child in place (scanner shape); write #2 parks inside the LLM
	// call so the child stays Running while the stale event is processed.
	childProc.SetResumeAt(time.Now().Add(-time.Second))
	k.scanQuotaWakeups(time.Now())
	waitUntilState(t, childProc, types.StateRunning)

	// The supervisor loop only NOW gets around to the queued event — but the
	// child is already Running.
	sup.handleChildExit(0, childProc.PID, quotaSuspendExitStatus())

	if !sup.children[0].alive {
		t.Error("child.alive = false — the stale event must not mark a woken child dead")
	}
	if sup.children[0].pid != childProc.PID {
		t.Errorf("child.pid = %d, want %d — no replacement spawn", sup.children[0].pid, childProc.PID)
	}
	if n := len(sup.restartTimes); n != 0 {
		t.Errorf("restarts recorded = %d, want 0 — a woken child must not be restarted", n)
	}
	if st := childProc.GetState(); st != types.StateRunning {
		t.Errorf("child state = %s, want still Running (parked in the LLM write)", st)
	}
	count := 0
	k.procTable.Range(func(_ types.PID, _ *Process) bool { count++; return true })
	if count != 2 { // supervisor fixture + the ONE child
		t.Errorf("procTable size = %d, want 2 — a duplicate child beside the woken original is the bug this pins", count)
	}

	// The re-armed monitor must deliver the REAL terminal exit once the child
	// finishes.
	releaseNow()
	select {
	case ce := <-sup.exitCh:
		if ce.pid != childProc.PID {
			t.Errorf("delivered exit pid = %d, want %d", ce.pid, childProc.PID)
		}
		if ce.exit.Code != 0 {
			t.Errorf("delivered exit = %+v, want the clean terminal completion", ce.exit)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("re-armed monitor never delivered the terminal exit — the re-arm is broken")
	}
}

// quotaThenParkLLM drives the adoption fixture: write#1 succeeds (the
// checkpointed step 1), write#2 fails quota (the suspension), write#3+ park
// until release — so the post-resume incarnation stays Running while the
// stale event is processed.
type quotaThenParkLLM struct {
	mu       sync.Mutex
	quotaErr error
	writes   int
	reads    int
	release  chan struct{}
	readData [][]byte
}

func (f *quotaThenParkLLM) Write(_ gocontext.Context, _ []byte) error {
	f.mu.Lock()
	i := f.writes
	f.writes++
	f.mu.Unlock()
	switch i {
	case 0:
		return nil
	case 1:
		return f.quotaErr
	default:
		<-f.release
		return nil
	}
}

func (f *quotaThenParkLLM) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.reads
	f.reads++
	if i >= len(f.readData) {
		i = len(f.readData) - 1
	}
	return f.readData[i], nil
}

func (f *quotaThenParkLLM) Close() error { return nil }
func (f *quotaThenParkLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *quotaThenParkLLM) SupportsToolCalling() bool { return true }

// TestATDD_73_3_SupervisorStaleEventAfterDetachResume_Adopts: the detach
// variant of the P2 race — the operator resumes the quota-suspended child via
// rnix resume <uuid> (non-fork: NEW process object under the same UUID, old
// placeholder deleted). The stale ExitSuspended then finds no process at the
// old PID; the gate must adopt the new incarnation by UUID, re-arm on ITS
// Done channel, and never spawn a duplicate.
func TestATDD_73_3_SupervisorStaleEventAfterDetachResume_Adopts(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseNow := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseNow()
	primary := &quotaThenParkLLM{
		quotaErr: quotaErrWithResetAt(resetAt),
		release:  release,
		readData: [][]byte{
			makeToolCallResponse("/dev/echo", map[string]any{}, 1),
			makeLLMResponse("done", 1),
		},
	}
	k, baseDir := newQuotaCheckpointKernel(t, primary)
	k.SetCheckpointConfig(CheckpointConfig{IntervalSteps: 1})

	opts := failureRawSpawnOpts(baseDir)
	opts.AllowedDevices = []string{"/dev/echo"}
	childPID, err := k.Spawn("73.3 P2 detach adoption", nil, opts)
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}
	childProc, _ := k.GetProcess(childPID)
	waitDone(t, childProc) // step 1 tool ok + checkpoint, step 2 quota suspend
	uuid := childProc.UUID

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, serr := os.Stat(filepath.Join(baseDir, "steps", uuid, "checkpoint.json")); serr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("checkpoint.json never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}

	sup := newManualSupervisorFixture(t, k, childProc)
	drainStaleSuspendDone(childProc)

	// Detach-style manual resume: new process object, old placeholder deleted.
	res, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err != nil {
		t.Fatalf("ResumeWithOpts: %v", err)
	}
	newProc, ok := k.GetProcess(res.PID)
	if !ok {
		t.Fatal("resumed incarnation not in procTable")
	}
	if res.PID == childProc.PID {
		t.Fatal("resume reused the old PID — this test needs the detach shape")
	}
	waitUntilState(t, newProc, types.StateRunning) // parked on the post-resume write

	// The stale event arrives for a PID that no longer exists.
	sup.handleChildExit(0, childProc.PID, quotaSuspendExitStatus())

	if !sup.children[0].alive {
		t.Error("child.alive = false — adoption must keep the slot alive")
	}
	if sup.children[0].pid != newProc.PID {
		t.Errorf("child.pid = %d, want the adopted %d", sup.children[0].pid, newProc.PID)
	}
	if n := len(sup.restartTimes); n != 0 {
		t.Errorf("restarts recorded = %d, want 0 — adoption replaces restart", n)
	}
	count := 0
	k.procTable.Range(func(_ types.PID, _ *Process) bool { count++; return true })
	if count != 2 { // supervisor fixture + the adopted incarnation
		t.Errorf("procTable size = %d, want 2 — a duplicate beside the adopted incarnation is the bug this pins", count)
	}

	// The adopted monitor delivers the terminal exit.
	releaseNow()
	select {
	case ce := <-sup.exitCh:
		if ce.pid != newProc.PID {
			t.Errorf("delivered exit pid = %d, want the adopted %d", ce.pid, newProc.PID)
		}
		if ce.exit.Code != 0 {
			t.Errorf("delivered exit = %+v, want the clean terminal completion", ce.exit)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("adopted monitor never delivered the terminal exit")
	}
}
