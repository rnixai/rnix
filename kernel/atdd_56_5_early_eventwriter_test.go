package kernel

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Tests for Story 56.5: spawn 早期 strace 事件落盘（EventWriter 早挂 · CAP-5）
//
// 缺口本质：普通 agent spawn 路径（不传 EventWriterFactory、SkipReasonLoop=false）
// 在 spawn 早期 emit `ConfigResolve`（spawn.go:779）时 EventWriter 尚未 attach
// （现 attach 在 :868，AddProcess 之后），事件经 emitEvent 的 `ew != nil` 门控
// （observe.go:48）被静默丢弃。修复 = 把 attach 提前到 ConfigResolve emit 之前
// （推荐 ① auto-attach 兜底 + 选点 (a)：Open 成功后、ConfigResolve 前）。
//
// 🔑 与 atdd_40_3_strace_events_test.go 的关键差异：
//    40.3 的 helper **传了** EventWriterFactory（绕过缺口，测的是 factory 路径）；
//    本 story 的 helper **必须不传 factory**，验证 auto-attach 兜底自身生效——
//    直接复制 40.3 helper 会“假绿”。
//
// RED/GREEN 划分（记忆 atdd-code-story-red-mechanism-preference）：
//   🔴 RED（t.Skip 占位，保 ATDD 提交期 make all 绿）：
//      - AC1 NormalSpawnNoFactory_EventsContainConfigResolve（CAP-5 success 核心）
//      - AC8 ConfigResolveCarriesProviderModelEffort（携带字段可见）
//      dev-story 移 skip → 实跑验真 FAIL（缺口存在）→ 打补丁 → GREEN。
//   🟢 GREEN-guard（不 skip · GREEN-stays-GREEN 实时拦范围红线）：
//      - AC3 单 events.jsonl 文件 / 无双写（幂等）
//      - AC2 early Open 失败不留孤儿 events.jsonl（无 FD 泄漏）
//      - AC4 dataDir=="" 行为不变（always-on 无门控）
//      - AC5 SkipReasonLoop+factory 零回归（factory 不被 auto-attach 夺权）
// ============================================================================

// waitEarlyEWDone waits for a process to terminate using the `terminated`
// broadcast channel (closed exactly once on Zombie transition, reason.go:194 →
// process.go:552). This is deterministic — unlike the one-shot `proc.Done`
// channel whose send is `select { case proc.Done <- exit: default: }`
// (reason.go:224) and can be dropped if no receiver is ready at that instant.
func waitEarlyEWDone(t *testing.T, proc *Process) {
	t.Helper()
	select {
	case <-proc.terminated:
	case <-time.After(5 * time.Second):
		t.Fatalf("56.5: timed out waiting for pid=%d to terminate", proc.PID)
	}
}

// earlyEWEventsPath derives the events.jsonl path the same way production does:
// via k.ResolveStepBaseDir(proc) (NOT baseDir直传). For a no-ProjectConfig proc
// this resolves to <dataDir>/global/steps/<uuid>/events.jsonl — the exact path
// the kernel's auto-attach writes to. (56.1 dev note反复犯错：用 baseDir 直传
// 而非 ResolveStepBaseDir 导致 INT-008 失败。)
func earlyEWEventsPath(k *KernelImpl, proc *Process) string {
	return filepath.Join(k.ResolveStepBaseDir(proc), "steps", proc.UUID, "events.jsonl")
}

// countEventsJSONL walks root and counts files named events.jsonl. Missing
// directories are tolerated (returns 0). Used to assert single-file (no double
// writer) and no-orphan (early error-return) invariants.
func countEventsJSONL(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "events.jsonl" {
			count++
		}
		return nil
	})
	return count
}

// spawnEarlyEWNoFactory spawns a normal agent process WITHOUT an
// EventWriterFactory (SkipReasonLoop=false), with dataDir set to a temp dir, and
// waits for the process to run its single reasonStep and terminate. This is the
// canonical CAP-5 fixture: it exercises the auto-attach fallback, NOT the
// factory path.
func spawnEarlyEWNoFactory(t *testing.T, llmFile vfs.VFSFile) (*KernelImpl, *Process) {
	t.Helper()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	k.dataDir = t.TempDir()
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("56.5 early eventwriter", nil, SpawnOpts{}) // 不传 factory；SkipReasonLoop=false
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	waitEarlyEWDone(t, proc)
	return k, proc
}

// mockLLMFileWithEffort extends mockLLMFile with DefaultModel() + ReasoningEffort()
// so the kernel backfills proc.Model (spawn.go:717-723) and snapshots
// proc.ReasoningEffort (spawn.go:736-738), making ConfigResolve.Args carry
// model + reasoning_effort.
type mockLLMFileWithEffort struct {
	mockLLMFile
	model  string
	effort string
}

func (f *mockLLMFileWithEffort) DefaultModel() string    { return f.model }
func (f *mockLLMFileWithEffort) ReasoningEffort() string { return f.effort }

// ---------------------------------------------------------------------------
// 🔴 AC #1 (RED · CAP-5 success 核心): 普通 spawn（不传 factory）后 events.jsonl 含 ConfigResolve
// ---------------------------------------------------------------------------

func TestATDD_56_5_AC1_NormalSpawnNoFactory_EventsContainConfigResolve(t *testing.T) {
	llm := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, proc := spawnEarlyEWNoFactory(t, llm)

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	rows, err := ReadAllEvents(earlyEWEventsPath(k, proc))
	if err != nil {
		t.Fatalf("AC1 FAIL: events.jsonl 不可读: %v", err)
	}

	found := false
	for _, row := range rows {
		if row.Syscall == "ConfigResolve" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("AC1 FAIL: events.jsonl 不含 ConfigResolve 事件（命中=0）—— spawn 早期事件被丢进 nil writer，CAP-5 缺口未修复")
	}
}

// ---------------------------------------------------------------------------
// 🔴 AC #8 (RED · 携带字段可见): ConfigResolve.Args 含 provider / model / reasoning_effort
// ---------------------------------------------------------------------------

func TestATDD_56_5_AC8_ConfigResolveCarriesProviderModelEffort(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "high",
	}
	k, proc := spawnEarlyEWNoFactory(t, llm)

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	rows, err := ReadAllEvents(earlyEWEventsPath(k, proc))
	if err != nil {
		t.Fatalf("AC8 FAIL: events.jsonl 不可读: %v", err)
	}

	var cfg *SyscallEventDisk
	for i := range rows {
		if rows[i].Syscall == "ConfigResolve" {
			cfg = &rows[i]
			break
		}
	}
	if cfg == nil {
		t.Fatalf("AC8 FAIL: events.jsonl 不含 ConfigResolve（无法验证携带字段）")
	}

	// provider 由 kernel 解析（默认 claude），断言非空。
	if p, _ := cfg.Args["provider"].(string); p == "" {
		t.Errorf("AC8 FAIL: ConfigResolve.Args 缺 provider 或为空，got %v", cfg.Args["provider"])
	}
	// model 来自 mock DefaultModel() 回填。
	if got, _ := cfg.Args["model"].(string); got != "claude-test-model" {
		t.Errorf("AC8 FAIL: ConfigResolve.Args[model] = %q, want %q", got, "claude-test-model")
	}
	// reasoning_effort 来自 mock ReasoningEffort() 快照（非空才入 configArgs）。
	if got, _ := cfg.Args["reasoning_effort"].(string); got != "high" {
		t.Errorf("AC8 FAIL: ConfigResolve.Args[reasoning_effort] = %q, want %q", got, "high")
	}
}

// ---------------------------------------------------------------------------
// 🟢 AC #3 (GREEN-guard · 幂等无双写): 完整 spawn 后 steps 树下恰好一个 events.jsonl
// ---------------------------------------------------------------------------

func TestATDD_56_5_AC3_FullSpawn_SingleEventsFileNoDoubleWrite(t *testing.T) {
	// GREEN-guard（不 skip）：早挂后 attachStepObservation（reason.go:58 `if proc.eventWriter == nil`
	// 守卫）必须对已早挂 proc no-op，不建第二个 writer。现状（晚挂）单文件即过；修复后
	// （早挂 + attachStepObservation no-op）仍须单文件——本护栏拦截双 writer / 双文件回归。
	llm := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, proc := spawnEarlyEWNoFactory(t, llm)

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	if n := countEventsJSONL(k.dataDir); n != 1 {
		t.Errorf("AC3 FAIL: steps 树下 events.jsonl 文件数 = %d, want 1（双 writer/双文件回归）", n)
	}

	// 文件可完整解析（无双写交错损坏）。
	if _, err := ReadAllEvents(earlyEWEventsPath(k, proc)); err != nil {
		t.Errorf("AC3 FAIL: events.jsonl 解析失败（疑似双写损坏）: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 🟢 AC #2 (GREEN-guard · early error-return 无孤儿): Open 失败不留 events.jsonl
// ---------------------------------------------------------------------------

func TestATDD_56_5_AC2_EarlyOpenError_NoOrphanEventsFile(t *testing.T) {
	// GREEN-guard（不 skip）：推荐选点 (a) 把 attach 放在 Open 成功之后、ConfigResolve emit 之前。
	// Open 失败路径（spawn.go:707 CtxFree + return）在 attach 之前返回 → 永不 attach → 无孤儿
	// events.jsonl + 无 FD 泄漏。现状 attach 在 :868（Open 之后），Open 失败同样未 attach。
	// 本护栏拦截 dev 选 (b) 早挂却漏清理导致孤儿 / FD 泄漏。
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return nil, errors.New("simulated open failure")
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	k.dataDir = t.TempDir()
	t.Cleanup(k.Shutdown)

	if _, err := k.Spawn("56.5 open failure", nil, SpawnOpts{}); err == nil {
		t.Fatal("AC2 FAIL: 预期 Open 失败使 Spawn 返回错误，got nil")
	}

	if n := countEventsJSONL(k.dataDir); n != 0 {
		t.Errorf("AC2 FAIL: early error-return 路径留下 %d 个孤儿 events.jsonl, want 0（FD 泄漏 / 孤儿文件）", n)
	}
}

// ---------------------------------------------------------------------------
// 🟢 AC #4 (GREEN-guard · always-on 无门控): dataDir=="" 不 attach、无文件、行为不变
// ---------------------------------------------------------------------------

func TestATDD_56_5_AC4_EmptyDataDir_NoWriterNoFileUnchanged(t *testing.T) {
	// GREEN-guard（不 skip）：EventWriter always-on（无 Enabled 开关），但 dataDir=="" 时
	// ResolveStepBaseDir 返回 "" → auto-attach 自然跳过。行为须与现状一致：无 writer、无文件、
	// 进程正常完成。本护栏防止 auto-attach 误在裸 fixture 上建 writer 改变断言。
	reg := vfs.NewDeviceRegistry()
	llm := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	// 故意不设 k.dataDir（留空）——ResolveStepBaseDir 返回 ""。
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("56.5 empty datadir", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC4 FAIL: dataDir=='' spawn 不应失败: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("AC4 FAIL: process %d not found", pid)
	}
	waitEarlyEWDone(t, proc)

	if base := k.ResolveStepBaseDir(proc); base != "" {
		t.Errorf("AC4 FAIL: dataDir=='' 时 ResolveStepBaseDir 应返回 \"\", got %q", base)
	}
	if proc.eventWriter != nil {
		t.Errorf("AC4 FAIL: dataDir=='' 不应 attach EventWriter（always-on 但无落点）")
	}
}

// ---------------------------------------------------------------------------
// 🟢 AC #5 (GREEN-guard · SkipReasonLoop 零回归): factory 仍被调用，Spawn 事件落盘
// ---------------------------------------------------------------------------

func TestATDD_56_5_AC5_SkipReasonLoopWithFactory_FactoryStillInvoked(t *testing.T) {
	// GREEN-guard（不 skip）：script-runner 形态（SkipReasonLoop=true + 显式 EventWriterFactory）。
	// SkipReasonLoop=true 不进 !SkipReasonLoop 块（无块内早挂点），故 factory 块（spawn.go:863）
	// 必须保留并被调用；auto-attach 兜底不得夺权（else-if 互斥，镜像 RawWriter）。Spawn 事件正常落盘。
	factoryCalled := false
	reg := vfs.NewDeviceRegistry()
	llm := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	k.dataDir = t.TempDir()
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("56.5 script-runner shape", nil, SpawnOpts{
		SkipReasonLoop: true,
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			factoryCalled = true
			return NewEventWriter(k.ResolveStepBaseDir(proc), proc.UUID)
		},
	})
	if err != nil {
		t.Fatalf("AC5 FAIL: spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("AC5 FAIL: process %d not found", pid)
	}

	if !factoryCalled {
		t.Error("AC5 FAIL: EventWriterFactory 未被调用（script-runner 早挂被 auto-attach 夺权？）")
	}

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}
	rows, err := ReadAllEvents(earlyEWEventsPath(k, proc))
	if err != nil {
		t.Fatalf("AC5 FAIL: events.jsonl 不可读: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Syscall == "Spawn" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AC5 FAIL: events.jsonl 不含 Spawn 事件（factory 早挂应使 Spawn 落盘）")
	}
}
