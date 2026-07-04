package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gocontext "context"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Story 56.7 — 失败调用落盘 raw.jsonl（kernel 侧, AC1/AC2/AC3/AC6/AC7）
//
// 56.1–56.4 的 raw capture 链路只在成功路径工作（hook 位于 Write-成功分支）。
// 本文件验证裁决 2/3：
//   - AC1: primary Write 失败（非 transient、无 fallback）→ outcome=error 记录
//   - AC2: fallback 场景同 step 双记录 + ReadRawForStep last-match
//   - AC3: transient retry 失败记录（step N）与重试成功记录（step N+1）共存
//   - AC6: ReadRawForStepWithErrors last-match 语义锚定
//   - AC7: 成功记录 JSON 无 outcome/error 键；suspend 早退不产生记录；
//          fbFD Open 失败不产生记录
// ============================================================================

// nonTransientErrText 不含 socket/connection/overloaded 等关键字 →
// isTransientLLMError == false，Write 失败直接走终态失败/fallback 分支。
const nonTransientErrText = "gateway returned malformed 200 response"

// flakyRawLLMFile: 前 failures 次 Write 返回 writeErr，之后成功；实现
// vfs.RawCaptureProvider（capture.Step 留 0 由 kernel hook 盖 step 号）。
type flakyRawLLMFile struct {
	mu       sync.Mutex
	failures int
	writeErr error
	readData []byte
	capture  *vfs.RawCapture
}

func (f *flakyRawLLMFile) Write(_ gocontext.Context, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures > 0 {
		f.failures--
		return f.writeErr
	}
	return nil
}

func (f *flakyRawLLMFile) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readData, nil
}

func (f *flakyRawLLMFile) Close() error { return nil }
func (f *flakyRawLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *flakyRawLLMFile) SupportsToolCalling() bool       { return true }
func (f *flakyRawLLMFile) LastRawCapture() *vfs.RawCapture { return f.capture }

// suspendParkingRawLLM: Write 阻塞到 ctx 取消（模拟 SIGPAUSE 打断 in-flight
// Write），同时实现 RawCaptureProvider——若 suspend 早退 guard 失效，hook 就会
// 拉到这个 capture 并落盘，测试即失败。
type suspendParkingRawLLM struct {
	mu        sync.Mutex
	reached   bool
	reachedCh chan struct{}
	capture   *vfs.RawCapture
}

func (f *suspendParkingRawLLM) Write(ctx gocontext.Context, _ []byte) error {
	f.mu.Lock()
	if !f.reached {
		f.reached = true
		close(f.reachedCh)
	}
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (f *suspendParkingRawLLM) Read(_ int) ([]byte, error) { return makeLLMResponse("parked", 1), nil }
func (f *suspendParkingRawLLM) Close() error               { return nil }
func (f *suspendParkingRawLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *suspendParkingRawLLM) SupportsToolCalling() bool       { return true }
func (f *suspendParkingRawLLM) LastRawCapture() *vfs.RawCapture { return f.capture }

// newFailureRawKernel 组装双设备 kernel（primary + 可选 fallback），raw capture
// 开启，RawWriter 注入 baseDir。fallbackName == "" 时只注册 primary。
func newFailureRawKernel(t *testing.T, primary, fallback vfs.VFSFile, primaryName, fallbackName string) (*KernelImpl, string) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/"+primaryName, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return primary, nil
	})
	if fallback != nil && fallbackName != "" && fallbackName != primaryName {
		_ = reg.Register("/dev/llm/"+fallbackName, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return fallback, nil
		})
	}
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	baseDir := t.TempDir()
	k := NewKernel(v, ctxMgr, nil)
	k.dataDir = baseDir
	k.SetRawCaptureConfig(RawCaptureConfig{Enabled: true, MaxOutputBytes: 4 << 20})
	t.Cleanup(k.Shutdown)

	if fallbackName != "" {
		providerSet := map[string]bool{primaryName: true, fallbackName: true}
		k.SetProviderResolver(
			func() []string {
				names := make([]string, 0, len(providerSet))
				for n := range providerSet {
					names = append(names, n)
				}
				return names
			},
			func(name string) bool { return providerSet[name] },
		)
	}
	return k, baseDir
}

func failureRawSpawnOpts(baseDir string) SpawnOpts {
	return SpawnOpts{
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			return NewEventWriter(baseDir, proc.UUID)
		},
		RawWriterFactory: func(proc *Process) (*RawWriter, error) {
			return NewRawWriter(baseDir, proc.UUID)
		},
	}
}

func waitDone(t *testing.T, proc *Process) ExitStatus {
	t.Helper()
	select {
	case exit := <-proc.Done:
		return exit
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion")
		return ExitStatus{}
	}
}

func rawRecordsFor(t *testing.T, baseDir, uuid string) []vfs.RawCapture {
	t.Helper()
	records, err := ReadAllRaw(filepath.Join(baseDir, "steps", uuid, "raw.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllRaw: %v", err)
	}
	return records
}

// --- AC1: 失败 primary（非 transient、无 fallback）落盘 outcome=error ---

func TestATDD_56_7_AC1_FailedPrimary_PersistedWithOutcomeError(t *testing.T) {
	capA := &vfs.RawCapture{
		TsMs:     100,
		Kind:     "cli",
		Request:  map[string]any{"argv": []string{"claude", "--print"}, "stdin": "the intent"},
		Response: map[string]any{"stdout": "partial", "stderr": "boom from gateway", "exit_code": 1},
	}
	llm := &flakyRawLLMFile{
		failures: 999, // 永远失败
		writeErr: fmt.Errorf("%s", nonTransientErrText),
		capture:  capA,
	}
	k, baseDir := newFailureRawKernel(t, llm, nil, "claude", "")

	pid, err := k.Spawn("56.7 AC1 failed primary", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code == 0 {
		t.Fatalf("expected non-zero exit for failed primary, got %+v", exit)
	}

	records := rawRecordsFor(t, baseDir, proc.UUID)
	if len(records) == 0 {
		t.Fatal("raw.jsonl empty — failed primary call was not persisted (G3)")
	}
	rec := records[0]
	if rec.Outcome != "error" {
		t.Errorf("Outcome = %q, want %q", rec.Outcome, "error")
	}
	if !strings.Contains(rec.Error, nonTransientErrText) {
		t.Errorf("Error = %q, want to contain driver error %q", rec.Error, nonTransientErrText)
	}
	if rec.Step != 1 {
		t.Errorf("Step = %d, want 1", rec.Step)
	}
	// Request 完整可核对（AC1: argv/body 可审计）
	argv := rec.Request["argv"]
	if argv == nil {
		t.Errorf("Request[argv] missing — request shape must survive the failure path: %+v", rec.Request)
	}
	// Response 含已累积的 stdout/stderr/exit_code
	if stderr, _ := rec.Response["stderr"].(string); !strings.Contains(stderr, "boom from gateway") {
		t.Errorf("Response[stderr] = %v, want accumulated stderr", rec.Response["stderr"])
	}
}

// --- AC2: fallback 场景同 step 双记录 + last-match ---

func TestATDD_56_7_AC2_FallbackSuccess_TwoRecordsSameStep(t *testing.T) {
	capPrimary := &vfs.RawCapture{
		TsMs:    100,
		Kind:    "api",
		Request: map[string]any{"url": "https://primary.example/v1", "body": "primary req"},
	}
	capFallback := &vfs.RawCapture{
		TsMs:     200,
		Kind:     "api",
		Request:  map[string]any{"url": "https://fallback.example/v1", "body": "fallback req"},
		Response: map[string]any{"status": 200, "body": "fallback ok"},
	}
	primary := &flakyRawLLMFile{
		failures: 999,
		writeErr: fmt.Errorf("%s", nonTransientErrText),
		capture:  capPrimary,
	}
	fallback := &flakyRawLLMFile{
		readData: makeLLMResponse("fallback response", 10),
		capture:  capFallback,
	}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "primary", "fb")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "fb")
	pid, err := k.Spawn("56.7 AC2 fallback success", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code != 0 {
		t.Fatalf("expected exit 0 (fallback succeeded), got %+v", exit)
	}

	records := rawRecordsFor(t, baseDir, proc.UUID)
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2 (primary failure + fallback success): %+v", len(records), records)
	}
	prim, fb := records[0], records[1]
	if prim.Outcome != "error" || !strings.Contains(prim.Error, nonTransientErrText) {
		t.Errorf("primary record: Outcome=%q Error=%q, want outcome=error with driver message", prim.Outcome, prim.Error)
	}
	if fb.Outcome != "" || fb.Error != "" {
		t.Errorf("fallback record must have no outcome marker, got Outcome=%q Error=%q", fb.Outcome, fb.Error)
	}
	if prim.Step != fb.Step {
		t.Errorf("both records must share the step (same reason-loop iteration): primary=%d fallback=%d", prim.Step, fb.Step)
	}

	// AC2/AC6: ReadRawForStep 返回 fallback 记录（last-match，终态优先）
	rawPath := filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl")
	rec, err := ReadRawForStep(rawPath, prim.Step)
	if err != nil {
		t.Fatalf("ReadRawForStep: %v", err)
	}
	if rec == nil {
		t.Fatal("ReadRawForStep returned nil")
	}
	if url, _ := rec.Request["url"].(string); url != "https://fallback.example/v1" {
		t.Errorf("ReadRawForStep must return the fallback record (last-match), got url=%v outcome=%q", rec.Request["url"], rec.Outcome)
	}
}

func TestATDD_56_7_AC2_FallbackAlsoFails_BothRecordsMarkedError(t *testing.T) {
	primary := &flakyRawLLMFile{
		failures: 999,
		writeErr: fmt.Errorf("%s", nonTransientErrText),
		capture:  &vfs.RawCapture{TsMs: 1, Kind: "api", Request: map[string]any{"url": "https://primary.example"}},
	}
	fallback := &flakyRawLLMFile{
		failures: 999,
		writeErr: fmt.Errorf("fallback also dead"),
		capture:  &vfs.RawCapture{TsMs: 2, Kind: "api", Request: map[string]any{"url": "https://fallback.example"}},
	}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "primary", "fb")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "fb")
	pid, err := k.Spawn("56.7 AC2 both fail", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code == 0 {
		t.Fatalf("expected non-zero exit (all providers exhausted), got %+v", exit)
	}

	records := rawRecordsFor(t, baseDir, proc.UUID)
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2: %+v", len(records), records)
	}
	for i, rec := range records {
		if rec.Outcome != "error" {
			t.Errorf("records[%d].Outcome = %q, want error", i, rec.Outcome)
		}
	}
	if !strings.Contains(records[1].Error, "fallback also dead") {
		t.Errorf("fallback record Error = %q, want fallback driver message", records[1].Error)
	}
}

// --- AC3: transient retry — 失败记录与重试成功记录互不覆盖 ---

func TestATDD_56_7_AC3_TransientRetry_FailureAndSuccessRecordsCoexist(t *testing.T) {
	cap := &vfs.RawCapture{
		TsMs:     1,
		Kind:     "api",
		Request:  map[string]any{"url": "https://api.example/v1"},
		Response: map[string]any{"status": 200, "body": "ok"},
	}
	llm := &flakyRawLLMFile{
		failures: 1,                                      // 第一次 transient 失败，第二次成功
		writeErr: fmt.Errorf("connection reset by peer"), // 命中 isTransientLLMError
		readData: makeLLMResponse("recovered", 5),
		capture:  cap,
	}
	k, baseDir := newFailureRawKernel(t, llm, nil, "claude", "")

	pid, err := k.Spawn("56.7 AC3 transient retry", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code != 0 {
		t.Fatalf("expected exit 0 (retry succeeded), got %+v", exit)
	}

	records := rawRecordsFor(t, baseDir, proc.UUID)
	if len(records) < 2 {
		t.Fatalf("len(records) = %d, want >= 2 (transient failure + retry success): %+v", len(records), records)
	}
	fail, ok := records[0], records[1]
	if fail.Outcome != "error" || !strings.Contains(fail.Error, "connection reset") {
		t.Errorf("failure record: Outcome=%q Error=%q", fail.Outcome, fail.Error)
	}
	if ok.Outcome != "" {
		t.Errorf("retry-success record must have empty Outcome, got %q", ok.Outcome)
	}
	// 关键时序事实：continue 会执行 step++，retry 实际落在下一个 step
	if ok.Step != fail.Step+1 {
		t.Errorf("retry success lands on the NEXT step (continue does step++): fail=%d success=%d", fail.Step, ok.Step)
	}
}

// --- AC6: last-match 语义单测锚定 ---

func TestATDD_56_7_AC6_ReadRawForStepWithErrors_LastMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.jsonl")
	lines := []string{
		`{"ts_ms":1,"step":3,"kind":"api","request":{"url":"first"},"outcome":"error","error":"primary failed","truncated":false,"original_bytes":0}`,
		`{"ts_ms":2,"step":3,"kind":"api","request":{"url":"second"},"truncated":false,"original_bytes":0}`,
		`{"ts_ms":3,"step":4,"kind":"api","request":{"url":"other"},"truncated":false,"original_bytes":0}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec, parseErrors, err := ReadRawForStepWithErrors(path, 3)
	if err != nil {
		t.Fatalf("ReadRawForStepWithErrors: %v", err)
	}
	if parseErrors != 0 {
		t.Errorf("parseErrors = %d, want 0", parseErrors)
	}
	if rec == nil {
		t.Fatal("rec == nil, want the last step-3 record")
	}
	if url, _ := rec.Request["url"].(string); url != "second" {
		t.Errorf("last-match violated: got url=%q, want %q (the later record)", url, "second")
	}
	// 单条/step 的旧数据行为不变
	rec4, _, err := ReadRawForStepWithErrors(path, 4)
	if err != nil || rec4 == nil {
		t.Fatalf("step 4 lookup: rec=%v err=%v", rec4, err)
	}
	if url, _ := rec4.Request["url"].(string); url != "other" {
		t.Errorf("single-record step drifted: got url=%q", url)
	}
}

// --- AC7: 向后兼容 + 边界 ---

// 成功记录 JSON 不含 outcome / error 键（omitempty 零破坏）。
func TestATDD_56_7_AC7_SuccessRecord_NoOutcomeKeys(t *testing.T) {
	cap := &vfs.RawCapture{
		TsMs:     1,
		Kind:     "api",
		Request:  map[string]any{"url": "https://api.example/v1"},
		Response: map[string]any{"status": 200, "body": "ok"},
	}
	llm := &flakyRawLLMFile{
		readData: makeLLMResponse("done", 5),
		capture:  cap,
	}
	k, baseDir := newFailureRawKernel(t, llm, nil, "claude", "")

	pid, err := k.Spawn("56.7 AC7 success no keys", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	waitDone(t, proc)

	data, err := os.ReadFile(filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl"))
	if err != nil {
		t.Fatalf("read raw.jsonl: %v", err)
	}
	s := string(data)
	if strings.Contains(s, `"outcome"`) {
		t.Errorf("success record leaked \"outcome\" key: %s", s)
	}
	if strings.Contains(s, `"error"`) {
		t.Errorf("success record leaked \"error\" key: %s", s)
	}
}

// 旧格式 raw.jsonl（无新字段）读取零值不变。
func TestATDD_56_7_AC7_OldFormatRecords_ParseWithZeroValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.jsonl")
	old := `{"ts_ms":42,"step":1,"kind":"cli","request":{"argv":["claude"]},"response":{"stdout":"x","stderr":"","exit_code":0},"truncated":false,"original_bytes":0}`
	if err := os.WriteFile(path, []byte(old+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	records, err := ReadAllRaw(path)
	if err != nil {
		t.Fatalf("ReadAllRaw: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len = %d, want 1", len(records))
	}
	if records[0].Outcome != "" || records[0].Error != "" {
		t.Errorf("old-format record must decode with zero-value markers, got Outcome=%q Error=%q",
			records[0].Outcome, records[0].Error)
	}
	// round-trip 后依然不产生新键
	out, err := json.Marshal(records[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(out), `"outcome"`) || strings.Contains(string(out), `"error"`) {
		t.Errorf("re-marshal of old record leaked new keys: %s", out)
	}
}

func TestATDD_56_7_AC7_ErrorField_RedactedAndTruncated(t *testing.T) {
	t.Run("redacts header-like credentials", func(t *testing.T) {
		const secret = "sk-super-secret-token"
		got := redactRawCaptureError("gateway echoed Authorization: Bearer " + secret)
		if strings.Contains(got, secret) {
			t.Fatalf("raw error leaked credential: %q", got)
		}
		if !strings.Contains(got, "Authorization: Bearer redacted(") {
			t.Fatalf("raw error did not keep auth scheme with fingerprint: %q", got)
		}
	})

	t.Run("truncates top-level error within raw budget", func(t *testing.T) {
		rec := &vfs.RawCapture{
			TsMs:  1,
			Step:  1,
			Kind:  "api",
			Error: strings.Repeat("E", 5000),
		}
		if !truncateRawCapture(rec, 512) {
			t.Fatal("truncateRawCapture returned false for oversized top-level Error")
		}
		if !rec.Truncated {
			t.Fatal("rec.Truncated = false, want true")
		}
		if strings.Contains(rec.Error, strings.Repeat("E", 1024)) {
			t.Fatalf("top-level Error was not truncated: len=%d", len(rec.Error))
		}
		if !strings.HasPrefix(rec.Error, "<truncated:") {
			t.Fatalf("top-level Error should be replaced by truncation marker, got %q", rec.Error)
		}
		if rec.OriginalBytes < 5000 {
			t.Fatalf("OriginalBytes = %d, want at least 5000", rec.OriginalBytes)
		}
	})
}

// suspend 早退（SIGPAUSE 取消 in-flight Write）不产生失败记录。
func TestATDD_56_7_AC7_SuspendEarlyReturn_NoFailureRecord(t *testing.T) {
	llm := &suspendParkingRawLLM{
		reachedCh: make(chan struct{}),
		capture: &vfs.RawCapture{
			TsMs:    1,
			Kind:    "api",
			Request: map[string]any{"url": "https://api.example/v1"},
		},
	}
	k, baseDir := newFailureRawKernel(t, llm, nil, "claude", "")

	pid, err := k.Spawn("56.7 AC7 suspend early-return", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-llm.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Write in flight")
	}
	if err := k.Kill(pid, types.SIGPAUSE); err != nil {
		t.Fatalf("Kill(SIGPAUSE): %v", err)
	}

	// 等待进入 Suspended（挂起完成 = reasonStep 已经从 suspend guard return）
	deadline := time.After(5 * time.Second)
	for proc.GetState() != types.StateSuspended {
		select {
		case <-deadline:
			t.Fatalf("process never reached Suspended, state=%v", proc.GetState())
		case <-time.After(10 * time.Millisecond):
		}
	}

	rawPath := filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl")
	if info, err := os.Stat(rawPath); err == nil && info.Size() > 0 {
		t.Errorf("raw.jsonl written during suspend early-return (size=%d) — pause is not a failure", info.Size())
	}
}

// fbFD Open 失败（fallback 设备未注册）→ 只有 primary 失败记录，无 fallback 记录。
func TestATDD_56_7_AC7_FallbackOpenFails_NoFallbackRecord(t *testing.T) {
	primary := &flakyRawLLMFile{
		failures: 999,
		writeErr: fmt.Errorf("%s", nonTransientErrText),
		capture:  &vfs.RawCapture{TsMs: 1, Kind: "api", Request: map[string]any{"url": "https://primary.example"}},
	}
	// fallback 设备故意不注册：newFailureRawKernel(fallback=nil) 但 resolver 认识 "ghost"
	k, baseDir := newFailureRawKernel(t, primary, nil, "primary", "ghost")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "ghost")
	pid, err := k.Spawn("56.7 AC7 fbFD open fails", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code == 0 {
		t.Fatalf("expected non-zero exit, got %+v", exit)
	}

	records := rawRecordsFor(t, baseDir, proc.UUID)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want exactly 1 (primary failure only): %+v", len(records), records)
	}
	if records[0].Outcome != "error" {
		t.Errorf("records[0].Outcome = %q, want error", records[0].Outcome)
	}
}
