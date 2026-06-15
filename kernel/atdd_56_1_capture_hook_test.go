package kernel

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Story 56.1 — kernel raw-capture hook + 截断 + 失败容忍 (AC#6/#7/#9)
//
// 56-1-INT-002..007. Hook 落点：reason.go:606-619 LLM Write 成功分支。
//
// RED 机制：
//   - INT-002/003/004/005/007 全部 t.Skip("RED")，dev-story 实现 hook + 截断
//     + 二次脱敏 + 失败容忍后移除 skip。
//   - INT-006（AC#9 不改归一化层）是 GREEN-stays-GREEN 护栏，不 skip — 即使
//     hook 未接好也必须真实跑通，确保 dev-story 不会把新字段塞进现有 Write
//     事件。
// ============================================================================

// fakeRawLLMFile 是 56.1 测试专用 fake — 同时实现 vfs.RawCaptureProvider
// 给 kernel hook 拉取，避免真 driver 实现 rawCaptureDriver（56.1 红线）。
type fakeRawLLMFile struct {
	mockLLMFile
	capture *vfs.RawCapture
}

func (f *fakeRawLLMFile) LastRawCapture() *vfs.RawCapture { return f.capture }

func newRawCaptureTestKernel(t *testing.T, llmFile vfs.VFSFile, cfg RawCaptureConfig) (*KernelImpl, *Process, string) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	baseDir := t.TempDir()
	k := NewKernel(v, ctxMgr, nil)
	k.dataDir = baseDir
	k.SetRawCaptureConfig(cfg)
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("56.1 raw-capture hook test", nil, SpawnOpts{
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			return NewEventWriter(baseDir, proc.UUID)
		},
		RawWriterFactory: func(proc *Process) (*RawWriter, error) {
			return NewRawWriter(baseDir, proc.UUID)
		},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	<-proc.Done
	return k, proc, baseDir
}

// 56-1-INT-002 (P0): fake provider + Enabled=true → raw.jsonl 写出一条 NDJSON。
func TestATDD_56_1_INT002_CaptureHook_WritesRawJsonl(t *testing.T) {
	rec := &vfs.RawCapture{
		TsMs:     1000,
		Step:     1,
		Kind:     "api",
		Request:  map[string]any{"url": "https://api.example/v1"},
		Response: map[string]any{"status": float64(200), "body": "fake"},
	}
	llm := &fakeRawLLMFile{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 5)},
		capture:     rec,
	}
	cfg := RawCaptureConfig{Enabled: true, MaxOutputBytes: 4 << 20}
	_, proc, baseDir := newRawCaptureTestKernel(t, llm, cfg)

	rawPath := filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl")
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("raw.jsonl not created at %s: %v", rawPath, err)
	}

	records, err := ReadAllRaw(rawPath)
	if err != nil {
		t.Fatalf("ReadAllRaw: %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("raw.jsonl empty — hook did not pull from RawCaptureProvider")
	}
	if records[0].Kind != "api" {
		t.Errorf("Kind = %q, want %q", records[0].Kind, "api")
	}
}

// 56-1-INT-003 (P0): Enabled=false → 无 raw.jsonl 文件 / 空文件。
func TestATDD_56_1_INT003_CaptureHook_DisabledSkipsWrite(t *testing.T) {
	rec := &vfs.RawCapture{TsMs: 1, Step: 1, Kind: "api"}
	llm := &fakeRawLLMFile{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 5)},
		capture:     rec,
	}
	cfg := RawCaptureConfig{Enabled: false, MaxOutputBytes: 4 << 20}
	_, proc, baseDir := newRawCaptureTestKernel(t, llm, cfg)

	rawPath := filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl")
	if info, err := os.Stat(rawPath); err == nil && info.Size() > 0 {
		t.Errorf("raw.jsonl unexpectedly written when Enabled=false: size=%d", info.Size())
	}
}

// 56-1-INT-004: 超 MaxOutputBytes → Truncated=true + OriginalBytes 正确，进程不 crash。
func TestATDD_56_1_INT004_CaptureHook_TruncatesOverMaxBytes(t *testing.T) {
	const oversize = 1024
	bigBody := make([]byte, oversize)
	for i := range bigBody {
		bigBody[i] = 'A'
	}
	rec := &vfs.RawCapture{
		TsMs:     1,
		Step:     1,
		Kind:     "api",
		Request:  map[string]any{"url": "https://api.example/v1"},
		Response: map[string]any{"body": string(bigBody)},
	}
	llm := &fakeRawLLMFile{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 5)},
		capture:     rec,
	}
	cfg := RawCaptureConfig{Enabled: true, MaxOutputBytes: 16}
	_, proc, baseDir := newRawCaptureTestKernel(t, llm, cfg)

	rawPath := filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl")
	records, err := ReadAllRaw(rawPath)
	if err != nil {
		t.Fatalf("ReadAllRaw: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	got := records[0]
	if !got.Truncated {
		t.Errorf("Truncated = false, want true (body=%d > MaxOutputBytes=16)", oversize)
	}
	if got.OriginalBytes < int64(oversize) {
		t.Errorf("OriginalBytes = %d, want >= %d", got.OriginalBytes, oversize)
	}
}

// 56-1-INT-005: 写盘错误 → 仅 log，reasonStep 继续完成（fault-tolerance）。
// Review patch P11: 真正注入会失败的 RawWriter——构造一个 spawn 前已 Close 的
// writer，hook 内 WriteRaw 必然返回 "write after close" 错误。断言：
// (a) 进程到达 Done 且 Exit.Code == 0；(b) hook 内的 log 不会让 reasonStep 中断。
func TestATDD_56_1_INT005_CaptureHook_WriteErrorNonFatal(t *testing.T) {
	rec := &vfs.RawCapture{TsMs: 1, Step: 1, Kind: "api"}
	llm := &fakeRawLLMFile{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 5)},
		capture:     rec,
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	baseDir := t.TempDir()
	k := NewKernel(v, ctxMgr, nil)
	k.dataDir = baseDir
	k.SetRawCaptureConfig(RawCaptureConfig{Enabled: true, MaxOutputBytes: 4 << 20})
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("56.1 INT-005 raw-write fail", nil, SpawnOpts{
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			return NewEventWriter(baseDir, proc.UUID)
		},
		// 注入一个已经 Close 的 RawWriter — 之后的 WriteRaw 必返回 "write after close"。
		RawWriterFactory: func(proc *Process) (*RawWriter, error) {
			rw, err := NewRawWriter(baseDir, proc.UUID)
			if err != nil {
				return nil, err
			}
			_ = rw.Close()
			return rw, nil
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	<-proc.Done

	if proc.GetState() == types.StateDead && proc.Exit != nil && proc.Exit.Code != 0 {
		t.Errorf("process exited non-zero after raw-write error; raw-capture must be best-effort, "+
			"got code=%d reason=%q", proc.Exit.Code, proc.Exit.Reason)
	}
	if proc.Exit == nil {
		t.Errorf("process Done but Exit is nil; expected normal completion despite raw-write failure")
	}
}

// 56-1-INT-006 (P0, GREEN-stays-GREEN, 不 skip): events.jsonl 的 Write 事件
// 字段集与 baseline 一致（AC#9 不改归一化层）。
//
// dev-story 不得给现有 Write 事件加新 key — 这条护栏即时拦截。
func TestATDD_56_1_INT006_WriteEvent_NormalizationLayerUnchanged(t *testing.T) {
	llm := &mockLLMFile{readData: makeLLMResponse("done", 5)}
	_, proc, baseDir := newRawCaptureTestKernel(t, llm, RawCaptureConfig{Enabled: true, MaxOutputBytes: 4 << 20})

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}
	eventsPath := filepath.Join(baseDir, "steps", proc.UUID, "events.jsonl")
	f, err := os.Open(eventsPath)
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer f.Close()

	// reason.go:611 emit Write {fd,size,model,reasoning_effort} —— baseline 字段集
	wantKeys := []string{"fd", "size", "model", "reasoning_effort"}
	sort.Strings(wantKeys)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var foundWrite bool
	for scanner.Scan() {
		var ev SyscallEventDisk
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("malformed event line: %v", err)
		}
		if ev.Syscall != "Write" {
			continue
		}
		foundWrite = true
		gotKeys := make([]string, 0, len(ev.Args))
		for k := range ev.Args {
			gotKeys = append(gotKeys, k)
		}
		sort.Strings(gotKeys)

		// 严格相等 — 多一个 key（如 dev-story 误加 "raw_bytes"）就拦截
		if !sliceEqual(gotKeys, wantKeys) {
			t.Errorf("Write event arg key set drift (AC#9 红线):\n  got  = %v\n  want = %v\n  args = %+v",
				gotKeys, wantKeys, ev.Args)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner.Err: %v", err)
	}
	if !foundWrite {
		t.Skip("no Write event recorded (mock did not exercise the LLM Write path); " +
			"this protector activates once the reason loop emits Write — keep in place")
	}
}

// 56-1-INT-007: raw.jsonl 内 JSON tag 全 snake_case（外部合约）。
func TestATDD_56_1_INT007_RawCapture_SnakeCaseTag(t *testing.T) {
	rec := &vfs.RawCapture{
		TsMs: 42, Step: 7, Kind: "api",
		Request:       map[string]any{"k": "v"},
		Response:      map[string]any{"status": float64(200)},
		Truncated:     false,
		OriginalBytes: 0,
	}
	llm := &fakeRawLLMFile{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 5)},
		capture:     rec,
	}
	cfg := RawCaptureConfig{Enabled: true, MaxOutputBytes: 4 << 20}
	_, proc, baseDir := newRawCaptureTestKernel(t, llm, cfg)

	rawPath := filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl")
	data, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read raw.jsonl: %v", err)
	}
	s := string(data)
	for _, want := range []string{`"ts_ms"`, `"step"`, `"kind"`, `"truncated"`, `"original_bytes"`} {
		if !strings.Contains(s, want) {
			t.Errorf("raw.jsonl missing %s tag: %s", want, s)
		}
	}
	// 反向：禁止 PascalCase tag 漏出
	for _, forbid := range []string{`"TsMs"`, `"Step":`, `"Kind"`, `"Truncated"`, `"OriginalBytes"`} {
		if strings.Contains(s, forbid) {
			t.Errorf("raw.jsonl leaked PascalCase tag %s: %s", forbid, s)
		}
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 56-1-INT-008 (P0, CAP-4 default-on E2E, review patch P12): Spawn 时不传
// RawWriterFactory → 走 kernel/spawn.go 的 auto-attach 兜底分支 → 真实落盘
// raw.jsonl。这是 production 路径（CLI 直接 spawn / IPC server / intent /
// compose 都不传 factory），ATDD 其余用例都 inject factory 跳过这条分支。
func TestATDD_56_1_INT008_AutoAttach_DefaultOnE2E(t *testing.T) {
	rec := &vfs.RawCapture{
		TsMs:     1234,
		Step:     1,
		Kind:     "api",
		Request:  map[string]any{"url": "https://api.example/v1"},
		Response: map[string]any{"status": float64(200), "body": "auto-attach ok"},
	}
	llm := &fakeRawLLMFile{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 5)},
		capture:     rec,
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	baseDir := t.TempDir()
	k := NewKernel(v, ctxMgr, nil)
	k.dataDir = baseDir
	// 使用默认 cfg（Enabled=true 由 DefaultRawCaptureConfig 保证），不显式 Set。
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("56.1 INT-008 auto-attach default-on", nil, SpawnOpts{
		// 关键：不传 RawWriterFactory — 走 spawn.go:887-895 的 auto-attach。
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			return NewEventWriter(baseDir, proc.UUID)
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	<-proc.Done

	// auto-attach 走 ResolveStepBaseDir → GlobalDataDir(baseDir) for procs
	// without ProjectConfig.
	stepBaseDir := k.ResolveStepBaseDir(proc)
	if stepBaseDir == "" {
		t.Fatalf("ResolveStepBaseDir returned empty — auto-attach gating depends on this being non-empty")
	}
	rawPath := filepath.Join(stepBaseDir, "steps", proc.UUID, "raw.jsonl")
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("raw.jsonl not created at %s — spawn auto-attach branch did not fire: %v", rawPath, err)
	}
	records, err := ReadAllRaw(rawPath)
	if err != nil {
		t.Fatalf("ReadAllRaw: %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("raw.jsonl empty — auto-attach RawWriter not properly hooked to captureRawLLMCall")
	}
	if records[0].Kind != "api" {
		t.Errorf("Kind = %q, want %q", records[0].Kind, "api")
	}
	if r, ok := records[0].Response["body"].(string); !ok || r != "auto-attach ok" {
		t.Errorf("Response body = %v, want %q", records[0].Response["body"], "auto-attach ok")
	}
}

// 56-1-INT-009 (review patch P1): lazy file creation — driver 不实现
// RawCaptureProvider 时（real-world 56.1 状态）spawn 走 auto-attach 仍**不**
// 应在 <uuid>/ 目录留下空 raw.jsonl 文件。
func TestATDD_56_1_INT009_LazyFileCreation_NoEmptyFile(t *testing.T) {
	// mockLLMFile 不实现 RawCaptureProvider —— 与 56.1 全部真实 driver 状态一致。
	llm := &mockLLMFile{readData: makeLLMResponse("done", 5)}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	baseDir := t.TempDir()
	k := NewKernel(v, ctxMgr, nil)
	k.dataDir = baseDir
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("56.1 INT-009 lazy creation", nil, SpawnOpts{
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			return NewEventWriter(baseDir, proc.UUID)
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	<-proc.Done

	stepBaseDir := k.ResolveStepBaseDir(proc)
	rawPath := filepath.Join(stepBaseDir, "steps", proc.UUID, "raw.jsonl")
	if _, err := os.Stat(rawPath); err == nil {
		t.Errorf("raw.jsonl exists at %s — review patch P1 (lazy creation) expects no file "+
			"when no driver implements RawCaptureProvider", rawPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("os.Stat: %v", err)
	}
}
