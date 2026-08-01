package kernel

import (
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 71.1 — token 轴刻度接通（AC1）+ backpressure 迁 token 轴（AC2）+
//             槽位触发废弃的反向固化（AC7）。
//
// R5 断点：SetContextWindowFunc 的唯一生产注入点（cmd/rnix/main.go）按值捕获
// daemon 启动期的 **全局** providers 快照。项目级 .rnix/providers.yaml 经
// ipc/server_spawn.go 的 DeepMergeYAML 合并后流向 ProjectConfig.Providers /
// driver factories / status cache 三处，无任何一条边回到 contextWindowFunc。
// 于是任何**只在项目级**声明的 provider，其 context_window 恒为 0。
//
// ⚠️ RED 有效性：既有 9 个 69.2 用例全部用 SetContextWindowFunc 桩注入，天然
// 绕过 providersCfg 这个真实来源，故该缺陷在它们之下完全不可见。本文件的 AC1
// 用例**禁用**该桩，只走 SpawnOpts.ProjectConfig 真实路径，否则是真空 PASS。
// =============================================================================

const (
	projWindow = 983_616               // 卷宗现场的真实配置值
	projBudget = projWindow * 9 / 10   // 885254 —— AC1 的期望刻度
	projModel  = "qwen3.8-max-preview" // 只在项目级 providers.yaml 声明
	projName   = "qwen"                // 只在项目级 providers.yaml 声明
)

// newProjectWindowKernel builds a kernel whose VFS knows /dev/llm/<provider>
// and which has NO contextWindowFunc injected — the daemon-global providers
// snapshot deliberately does not know this provider, exactly like a project-only
// entry in .rnix/providers.yaml.
func newProjectWindowKernel(t testing.TB, provider string, llmFile *mockLLMFile) (*KernelImpl, *rnixctx.Manager) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/"+provider, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k, ctxMgr
}

// projectConfigWithModelWindow builds the ProjectConfig snapshot the IPC layer
// hands to Spawn: Providers is the already-DeepMergeYAML'd view, so the
// models[<model>].context_window declared in .rnix/providers.yaml is present
// here and nowhere else.
func projectConfigWithModelWindow(provider, model string, window int) *config.ProjectConfig {
	return &config.ProjectConfig{
		Providers: &llm.ProvidersConfig{
			Providers: []llm.ProviderConfig{{
				Name:         provider,
				Driver:       "openai-compat",
				DefaultModel: model,
				Models:       map[string]llm.ModelConfig{model: {ContextWindow: window}},
			}},
		},
	}
}

// --- AC1: 项目级 provider 的 context_window 必须接通 token 轴 ---

func TestATDD_71_1_AC1_ProjectProviderWindowReachesCtx(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, ctxMgr := newProjectWindowKernel(t, projName, llmFile)
	// 刻意不调 k.SetContextWindowFunc —— 全局快照不认识这个 provider。

	pid, err := k.Spawn("project window", nil, SpawnOpts{
		Provider:      projName,
		Model:         projModel,
		ProjectConfig: projectConfigWithModelWindow(projName, projModel, projWindow),
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("pid %d missing from procTable", pid)
	}
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process")
	}

	if proc.ContextWindow != projWindow {
		t.Errorf("ContextWindow = %d, want %d (project-level providers.yaml must be consulted)",
			proc.ContextWindow, projWindow)
	}
	if proc.ContextBudget != projBudget {
		t.Errorf("ContextBudget = %d, want %d (window*9/10)", proc.ContextBudget, projBudget)
	}
	stats, err := ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != projBudget {
		t.Errorf("TokenUsage().Limit = %d, want %d — the project-level scale never reached ctx.TokenLimit",
			stats.Limit, projBudget)
	}
}

// --- AC1 优先级：项目级命中时胜出于全局快照 ---

func TestATDD_71_1_AC1_ProjectWindowWinsOverGlobalSnapshot(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, ctxMgr := newProjectWindowKernel(t, projName, llmFile)
	// 全局快照给一个陈旧的小窗口；项目级声明才是当前真相。
	k.SetContextWindowFunc(func(_, _ string) int { return 200_000 })

	pid, err := k.Spawn("project beats global", nil, SpawnOpts{
		Provider:      projName,
		Model:         projModel,
		ProjectConfig: projectConfigWithModelWindow(projName, projModel, projWindow),
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextWindow != projWindow {
		t.Errorf("ContextWindow = %d, want %d (project entry must win)", proc.ContextWindow, projWindow)
	}
	stats, _ := ctxMgr.TokenUsage(proc.CtxID)
	if stats.Limit != projBudget {
		t.Errorf("Limit = %d, want %d", stats.Limit, projBudget)
	}
}

// --- AC1 降级（green-guard）：项目级查表 miss 必须回落全局，绝不 panic ---

func TestATDD_71_1_AC1_ProjectMissFallsBackToGlobal(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, ctxMgr := newProjectWindowKernel(t, projName, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return projWindow })

	// 项目配置存在但 models 表里没有本次的 model —— 查表 miss。
	pc := projectConfigWithModelWindow(projName, "some-other-model", 12_345)

	pid, err := k.Spawn("project miss", nil, SpawnOpts{
		Provider:      projName,
		Model:         projModel,
		ProjectConfig: pc,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextWindow != projWindow {
		t.Errorf("ContextWindow = %d, want %d (miss must fall back to the global snapshot)",
			proc.ContextWindow, projWindow)
	}
	stats, _ := ctxMgr.TokenUsage(proc.CtxID)
	if stats.Limit != projBudget {
		t.Errorf("Limit = %d, want %d", stats.Limit, projBudget)
	}
}

// TestATDD_71_1_AC1_ProjectProvidersWrongTypeIsMiss pins the降级 semantics
// copied from projectHasProvider: a failed type-assert is a lookup miss, never
// a panic. ProjectConfig.Providers is typed `any` to break an import cycle, so
// nothing in the type system prevents a caller from putting something else in.
func TestATDD_71_1_AC1_ProjectProvidersWrongTypeIsMiss(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, ctxMgr := newProjectWindowKernel(t, projName, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return projWindow })

	pid, err := k.Spawn("wrong type", nil, SpawnOpts{
		Provider:      projName,
		Model:         projModel,
		ProjectConfig: &config.ProjectConfig{Providers: "not a *llm.ProvidersConfig"},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	stats, _ := ctxMgr.TokenUsage(proc.CtxID)
	if stats.Limit != projBudget {
		t.Errorf("Limit = %d, want %d (bad Providers type must degrade to the global func)",
			stats.Limit, projBudget)
	}
}

// =============================================================================
// AC2 (F1) — backpressure 迁 token 轴。
//
// 取消槽位上限后 slotMax 恒 0，旧 ComputeFn 的 `slotMax == 0 → ""` 守卫会让
// Story 69.1 的 backpressure 机制被**静默摘除**（即 Story 70.2 刚踩的坑：删掉
// 注入后 LLM 对压力零感知）。故数据源改挂 token 轴。
// =============================================================================

// backpressureProc builds a process with a configured token capacity scale and
// NO slot ceiling — the post-71.1 production shape.
func backpressureProc(t *testing.T, window int) (*KernelImpl, *Process) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(0) // 无上限
	if err != nil {
		t.Fatalf("CtxAlloc(0): %v", err)
	}
	proc := NewProcess(0, "backpressure token axis", nil)
	proc.CtxID = cid
	proc.ContextWindow = window
	proc.ContextBudget = window * 9 / 10
	return k, proc
}

func TestATDD_71_1_AC2_BackpressureFiresOnTokenAxisWithoutSlotCeiling(t *testing.T) {
	k, proc := backpressureProc(t, 100_000) // budget 90_000
	proc.LastInputTokens = 72_000           // 80% of 90_000 → elevated

	got := registerSections(proc, k, "").Build()
	if !strings.Contains(got, "Context Resource Warning") {
		t.Fatalf("backpressure section missing at 80%% token usage with no slot ceiling — "+
			"the 69.1 mechanism was silently removed:\n%s", got)
	}
	if !strings.Contains(got, "Context message slots are running low.") {
		t.Errorf("expected elevated-tier body, got:\n%s", got)
	}
}

func TestATDD_71_1_AC2_BackpressureCriticalTierOnTokenAxis(t *testing.T) {
	k, proc := backpressureProc(t, 100_000)
	proc.LastInputTokens = 81_000 // 90% of 90_000 → critical (boundary 85)

	got := registerSections(proc, k, "").Build()
	if !strings.Contains(got, backpressureText(backpressureTierCritical)) {
		t.Errorf("expected critical-tier body at 90%% token usage, got:\n%s", got)
	}
}

// TestATDD_71_1_AC2_BackpressureSilentBelowThreshold is the false-alarm half:
// the slot axis crossed 70% at roughly 36k real tokens, so this section had been
// warning the model long before capacity was in danger.
func TestATDD_71_1_AC2_BackpressureSilentBelowThreshold(t *testing.T) {
	k, proc := backpressureProc(t, 100_000)
	proc.LastInputTokens = 36_000 // 40% of 90_000 — the old slot-axis alarm point

	got := registerSections(proc, k, "").Build()
	if strings.Contains(got, "Context Resource Warning") {
		t.Errorf("backpressure fired at 40%% token usage (must stay quiet below the threshold):\n%s", got)
	}
}

// TestATDD_71_1_AC2_BackpressureFirstStepSilent pins the degradation: before the
// first provider response LastInputTokens is 0, so pct is 0 and no section is
// injected. Correct — the context is at its smallest then.
func TestATDD_71_1_AC2_BackpressureFirstStepSilent(t *testing.T) {
	k, proc := backpressureProc(t, 100_000)

	got := registerSections(proc, k, "").Build()
	if strings.Contains(got, "Context Resource Warning") {
		t.Errorf("backpressure fired with LastInputTokens == 0:\n%s", got)
	}
}

// TestATDD_71_1_AC2_PerStepLeashIsNotACapacityDenominator is the trap guard.
// proc.ContextBudget is an overloaded field: without a context_window behind it,
// it is a PER-STEP InputTokens leash (4096 is a realistic agent.yaml value).
// Using it as the denominator would put such a process permanently in the
// critical tier from its very first response.
func TestATDD_71_1_AC2_PerStepLeashIsNotACapacityDenominator(t *testing.T) {
	k, proc := backpressureProc(t, 0) // no window …
	proc.ContextBudget = 4096         // … but a tight per-step leash
	proc.LastInputTokens = 4000       // 97.6% of the leash, 2% of the 200k default

	got := registerSections(proc, k, "").Build()
	if strings.Contains(got, "Context Resource Warning") {
		t.Errorf("a per-step leash was used as the capacity denominator — the process would sit "+
			"in the critical tier forever:\n%s", got)
	}

	if limit := proc.effectiveContextTokenLimit(); limit != rnixctx.DefaultTokenLimit {
		t.Errorf("effectiveContextTokenLimit() = %d, want DefaultTokenLimit %d",
			limit, rnixctx.DefaultTokenLimit)
	}
}

// TestATDD_71_1_AC2_BackpressureNeverReentersTokenUsage guards the 🔴 trap: the
// ComputeFn must read kernel-side proc fields only. TokenUsage() calls
// Sections.Build(), which calls this ComputeFn — a TokenUsage() call from inside
// would recurse until the stack blows. A plain TokenUsage() call must therefore
// return normally even with the backpressure tier active.
func TestATDD_71_1_AC2_BackpressureNeverReentersTokenUsage(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(vfs.NewVFS(reg), ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(0)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc := NewProcess(0, "reentry guard", nil)
	proc.CtxID = cid
	proc.ContextWindow = 100_000
	proc.ContextBudget = 90_000
	proc.LastInputTokens = 81_000 // critical tier → section is non-empty

	sections := registerSections(proc, k, "")
	if err := ctxMgr.SetSections(cid, sections); err != nil {
		t.Fatalf("SetSections: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := ctxMgr.TokenUsage(cid); err != nil {
			t.Errorf("TokenUsage: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TokenUsage() did not return — backpressure ComputeFn re-entered the Manager")
	}
}

// =============================================================================
// AC7 — 耦合固化（反向红线）。
//
// epic AC5 原本要求「单独实现 AC1 而不实现 AC3 时 token 轴仍不触发」，但两者都
// 实现后该测试按字面无法构造（slot 轴已不存在，无从模拟"只做 AC1"）。改为反向
// 固化：**高消息条数 + 低 token 用量 → 不得触发**。
//
// 这比字面测试更有牙：当前代码下必红（旧实现会以 slot_threshold 触发）、实现后
// 转绿、后人若把 slot 轴加回来会再红 —— 是永久护栏而非一次性验证。
// =============================================================================

// TestATDD_71_1_AC7_HighMessageCountLowTokensDoesNotCompact replaces the deleted
// TestAutoCompactIfNeeded_SlotThresholdTrigger with its inverse.
//
// ⚠️ The fixture deliberately configures an EXPLICIT ceiling (256, the retired
// default) rather than the production no-ceiling shape. With MaxSize 0 a
// re-introduced slot trigger could never fire — `slotMax > 0` would be false —
// and this guard would be toothless: it would pass against the very
// implementation it is supposed to reject. An explicit ceiling puts slot usage at
// 82%, above the old 80% line, so the assertion fails the moment anyone lets
// message count drive compaction again.
func TestATDD_71_1_AC7_HighMessageCountLowTokensDoesNotCompact(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 256)
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	// 210/256 = 82%，越过旧 80% 线。
	fillTo(t, ctxMgr, proc, 210)

	usage, err := ctxMgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if usage.Percentage > 80.0 {
		t.Fatalf("precondition: token usage %.1f%% must stay below the 80%% threshold — "+
			"otherwise this proves nothing about the slot axis", usage.Percentage)
	}
	if usage.SlotUsed < 205 {
		t.Fatalf("precondition: %d messages, want >= 205 (the old slot trigger point)", usage.SlotUsed)
	}

	k.autoCompactIfNeeded(proc, 1)

	after, _, err := ctxMgr.SlotUsage(cid)
	if err != nil {
		t.Fatalf("SlotUsage: %v", err)
	}
	if after != usage.SlotUsed {
		t.Errorf("messages changed %d → %d: a compaction fired on message COUNT alone. "+
			"Slots measure structure, tokens measure capacity — the two have no stable "+
			"conversion rate, so a count-based trigger is a量纲 error.", usage.SlotUsed, after)
	}
	if trigger := readCompactTrigger(t, proc); trigger != "" {
		t.Errorf("Compact event emitted with trigger=%q, want none", trigger)
	}
}

// TestATDD_71_1_AC7_HighMessageCountLowTokensDoesNotReclaim is the same red line
// for the SECOND slot track (Story 69.4's proactive reclamation), which the epic
// missed entirely: reclaimLeakedIfNeeded had its own slot watermark at
// threshold × 0.75 and its own `slot_watermark` trigger label.
//
// Same teeth requirement as above: MaxSize 84 for 82 messages = 97.6% slots, far
// over the 60% watermark. With no ceiling the guard would be vacuous.
func TestATDD_71_1_AC7_HighMessageCountLowTokensDoesNotReclaim(t *testing.T) {
	k, ctxMgr, proc, cid := setupReclaimKernel(t, 84)
	buildIncidentFixture(t, ctxMgr, cid) // 82 条，含大量可回收 tool 正文

	usage, err := ctxMgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	watermark := proc.effectiveCompactThreshold() * proactiveReclaimWatermarkRatio
	if usage.Percentage > watermark {
		t.Fatalf("precondition: token usage %.1f%% must sit below the %.1f%% watermark",
			usage.Percentage, watermark)
	}
	// 有东西可回收，否则"未回收"这一断言是真空的。
	if countLeakedInColdZone(snapshotKernelMessages(t, ctxMgr, cid)) == 0 {
		t.Fatal("precondition: fixture must hold reclaimable payload")
	}

	before := snapshotKernelMessages(t, ctxMgr, cid)
	k.reclaimLeakedIfNeeded(proc, 9)
	after := snapshotKernelMessages(t, ctxMgr, cid)

	for i := range before {
		if before[i].Content != after[i].Content {
			t.Fatalf("msg[%d] rewritten: the slot watermark fired on message count alone", i)
		}
	}
	if events := drainReclaimEvents(t, proc); len(events) != 0 {
		t.Errorf("emitted %d CtxReclaim events, want 0 (token usage is below the watermark)", len(events))
	}
}

// TestATDD_71_1_AC7_TokenAxisStillTriggersWithoutSlotCeiling is the positive
// half. Without it the two tests above could be satisfied by a compaction path
// that never fires at all.
func TestATDD_71_1_AC7_TokenAxisStillTriggersWithoutSlotCeiling(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 0)
	proc.DebugChan = make(chan types.SyscallEvent, 64)
	fillTo(t, ctxMgr, proc, 210)
	raiseTokenWatermark(t, ctxMgr, cid, 90)

	k.autoCompactIfNeeded(proc, 1)

	if trigger := readCompactTrigger(t, proc); trigger != "token_threshold" {
		t.Errorf("trigger = %q, want token_threshold (the token axis must still fire "+
			"with no slot ceiling configured)", trigger)
	}
	after, _, _ := ctxMgr.SlotUsage(cid)
	if after >= 210 {
		t.Errorf("messages = %d, want fewer than 210 after a token-triggered compaction", after)
	}
}
