package kernel

import (
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 69.2 — token 轴刻度接通（AC1）+ trigger 分类校正（AC5）。
//
// 基线缺陷：context.Manager.SetTokenLimit 全仓零调用方 → ctx.TokenLimit 恒 0 →
// effectiveTokenLimit() 回落 DefaultTokenLimit(200k)。声明 context_window:
// 983616 的 provider 也被当 20 万用（差 4.9 倍），token 阈值永不触发，256 条
// 消息槽位成为事实上的容量天花板（实证：23 步连续 trigger="slot_threshold"）。
//
// 接通点时序约束（裁决 2）：proc.Provider / proc.Model 在 spawn 的
// SkipReasonLoop 块内经 Open 解析，contextWindowFunc 在 spawn.go:874 才被调。
// 在 CtxAlloc(spawn.go:652) 处接通必得 0 —— 最容易写出的静默失效版本。故这些
// 用例断言的是 TokenUsage().Limit 的实测值，而非"代码能编译"。
//
// 三条 CtxAlloc 路径缺一即刻度退化，本文件逐条覆盖：
//   ①spawn.go:652 正常 spawn  ②resume.go:620 checkpoint resume
//   ③rehydrate.go:170 disk resume / load_suspended
// =============================================================================

const testWindow = 983_616                  // 卷宗现场的真实配置值
const testBudget = testWindow * 9 / 10      // 885254 —— AC1 的期望刻度

// --- AC1 路径①：正常 spawn ---

func TestATDD_69_2_AC1_SpawnAppliesCtxTokenLimit(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return testWindow })

	pid, err := k.Spawn("token limit spawn", nil, SpawnOpts{})
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

	if proc.ContextBudget != testBudget {
		t.Fatalf("precondition: ContextBudget = %d, want %d", proc.ContextBudget, testBudget)
	}

	stats, err := ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != testBudget {
		t.Errorf("TokenUsage().Limit = %d, want %d (context_window %d * 9/10 must reach ctx.TokenLimit)",
			stats.Limit, testBudget, testWindow)
	}
}

// --- AC1 降级语义（green-guard：现状即绿，防"总是写 limit"的实现）---

func TestATDD_69_2_AC1_NoWindowFallsBackToDefault(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	// 不注入 contextWindowFunc —— provider 未配 context_window 的等价场景。

	pid, err := k.Spawn("no window", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 0 {
		t.Fatalf("precondition: ContextBudget = %d, want 0", proc.ContextBudget)
	}

	stats, err := ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != rnixctx.DefaultTokenLimit {
		t.Errorf("Limit = %d, want DefaultTokenLimit %d (must NOT write a fallback value into ctx.TokenLimit)",
			stats.Limit, rnixctx.DefaultTokenLimit)
	}
}

func TestATDD_69_2_AC1_ZeroWindowFuncFallsBackToDefault(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return 0 }) // 显式返回 0 = 未知模型

	pid, err := k.Spawn("zero window", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	stats, err := ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != rnixctx.DefaultTokenLimit {
		t.Errorf("Limit = %d, want DefaultTokenLimit %d", stats.Limit, rnixctx.DefaultTokenLimit)
	}
}

// --- AC1 边界：显式 per-step leash 不得被误当容量刻度 ---

// TestATDD_69_2_AC1_ExplicitBudgetWithoutWindowNotAScale pins the distinction
// the story's AC1 conflated: "ContextBudget == 0" is NOT the same condition as
// "provider declares no context_window".
//
// proc.ContextBudget is an overloaded field. Besides being derived from the
// window (window*9/10) it is also an explicit per-step input-token leash coming
// from agent.yaml's context_budget / init.yaml / SpawnOpts / a supervisor
// ChildSpec, which reason.go compares against ONE step's InputTokens to suspend
// a runaway process. Manifest values are deliberately small — kernel_test.go's
// testAgentInfo() uses 4096 — and say nothing about the model's total window.
//
// Writing such a leash into ctx.TokenLimit would park the context permanently
// above the 80% compact threshold and compact it on every single step. That was
// a real regression during this story's implementation: it silently compacted
// away the tool-result messages that four unrelated permission / capability
// tests assert on. The scale must only engage when a real context_window backs
// the budget.
func TestATDD_69_2_AC1_ExplicitBudgetWithoutWindowNotAScale(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	// 不注入 contextWindowFunc → ContextWindow 恒 0，但显式给一个紧勒绳 budget。

	pid, err := k.Spawn("leash only", nil, SpawnOpts{ContextBudget: 4096})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.ContextBudget != 4096 {
		t.Fatalf("precondition: ContextBudget = %d, want 4096 preserved for the per-step leash",
			proc.ContextBudget)
	}
	if proc.ContextWindow != 0 {
		t.Fatalf("precondition: ContextWindow = %d, want 0", proc.ContextWindow)
	}

	stats, err := ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != rnixctx.DefaultTokenLimit {
		t.Errorf("Limit = %d, want DefaultTokenLimit %d — an explicit per-step leash must not "+
			"become the ctx capacity scale (doing so compacts the context on every step)",
			stats.Limit, rnixctx.DefaultTokenLimit)
	}
}

// --- AC1 clamp 复用：显式 ContextBudget 越界须被 clamp 后才接通 ---

func TestATDD_69_2_AC1_ClampedBudgetReachesCtx(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	k.SetContextWindowFunc(func(_, _ string) int { return 100_000 })

	pid, err := k.Spawn("clamp", nil, SpawnOpts{ContextBudget: 200_000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	stats, err := ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != 100_000 {
		t.Errorf("Limit = %d, want 100000 (clamped budget, not the pre-clamp 200000)", stats.Limit)
	}
}

// --- AC1 路径②：checkpoint resume（新发现 4 —— 须先重算再接通）---

func TestATDD_69_2_AC1_CheckpointResumeRecomputesAndApplies(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	k.SetContextWindowFunc(func(_, _ string) int { return testWindow })

	uuid := "69200001-aaaa-bbbb-cccc-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 7)

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed pid %d missing", result.PID)
	}

	// checkpoint.go 的快照 schema 只有 CtxSize，没有 ContextWindow/ContextBudget，
	// 且 contextWindowFunc 原本仅 spawn.go 一处调用 → 该路径必须重算，否则
	// ContextBudget 恒 0，接通 helper 的 budget>0 守卫会静默跳过。
	if proc.ContextBudget != testBudget {
		t.Errorf("checkpoint resume ContextBudget = %d, want %d (must be recomputed from Provider/Model)",
			proc.ContextBudget, testBudget)
	}

	stats, err := k.ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != testBudget {
		t.Errorf("checkpoint resume Limit = %d, want %d (resume path must not degrade to 200k)",
			stats.Limit, testBudget)
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- AC1 路径③：disk resume（从 diskInfo 继承窗口，直接接通）---

func TestATDD_69_2_AC1_DiskResumeAppliesCtxTokenLimit(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	k.SetContextWindowFunc(func(_, _ string) int { return testWindow })

	uuid := "69200002-aaaa-bbbb-cccc-000000000002"
	writeTestStepsAndMetaWithParent(t, baseDir, uuid, "", 4)

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed pid %d missing", result.PID)
	}

	stats, err := k.ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != testBudget {
		t.Errorf("disk resume Limit = %d, want %d (this is the regression the incident hit — "+
			"resume immediately replayed the spiral because the scale fell back to 200k)",
			stats.Limit, testBudget)
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- AC5：trigger 分类在正确刻度 + 完整统计下反映真实先到者 ---

// appendToolHeavyMessage appends an assistant turn whose payload lives almost
// entirely in ToolCalls[].Input — the shape that the pre-69.2 accounting saw as
// ~zero tokens. Each call consumes 1 + 1 = 2 slots (assistant + tool result).
func appendToolHeavyMessage(t *testing.T, ctxMgr *rnixctx.Manager, cid types.CtxID, id string, payloadBytes int) {
	t.Helper()
	calls := []rnixctx.ToolCall{{
		ID:   id,
		Name: "Write",
		Input: map[string]any{
			"file_path": "/tmp/" + id,
			"content":   strings.Repeat("x", payloadBytes),
		},
	}}
	if err := ctxMgr.AppendAssistantWithToolCalls(cid, "writing a file", "", nil, calls); err != nil {
		t.Fatalf("AppendAssistantWithToolCalls(%s): %v", id, err)
	}
	if err := ctxMgr.AppendToolResult(cid, id, "ok"); err != nil {
		t.Fatalf("AppendToolResult(%s): %v", id, err)
	}
}

// readCompactTrigger drains proc.DebugChan and returns the Compact event's
// trigger label ("" when no Compact event was emitted).
func readCompactTrigger(t *testing.T, proc *Process) string {
	t.Helper()
	trigger := ""
	for {
		select {
		case evt := <-proc.DebugChan:
			if evt.Syscall != "Compact" {
				continue
			}
			got, ok := evt.Args["trigger"].(string)
			if !ok {
				t.Fatalf("trigger field missing or not string: %v", evt.Args["trigger"])
			}
			trigger = got
		default:
			return trigger
		}
	}
}

func TestATDD_69_2_AC5_TokenThresholdTriggerLabel(t *testing.T) {
	// 工具调用密集、消息条数少：槽位远不满（大 maxSize），但 token 越阈。
	// 基线实现下 ToolCalls.Input 不计 → token% ≈ 0 → 必为不触发或 slot_threshold。
	k, ctxMgr, proc, cid := setupCompactKernel(t, 1000)
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	if err := ctxMgr.SetTokenLimit(cid, 20_000); err != nil {
		t.Fatalf("SetTokenLimit: %v", err)
	}
	for i := range 4 {
		appendToolHeavyMessage(t, ctxMgr, cid, "call_"+string(rune('a'+i)), 20_000)
	}

	usage, err := ctxMgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if usage.Percentage <= 80.0 {
		t.Fatalf("token%% = %.1f, want > 80 (tool-call payload must be visible to the token axis)",
			usage.Percentage)
	}
	slotUsed, slotMax, _ := ctxMgr.SlotUsage(cid)
	slotPct := float64(slotUsed) / float64(slotMax) * 100
	if slotPct > 70.0 {
		t.Fatalf("precondition: slot%% = %.1f, want low so the token axis is isolated", slotPct)
	}

	k.autoCompactIfNeeded(proc, 1)

	if got := readCompactTrigger(t, proc); got != "token_threshold" {
		t.Errorf("trigger = %q, want token_threshold", got)
	}
}

// TestATDD_69_2_AC5_BothThresholdTriggerLabel used to assert trigger == "both"
// when the slot and token axes crossed together. Story 71.1 AC3 retired the slot
// axis, so "both" is unreachable and the label collapses to token_threshold. The
// case is kept — with its fixture intact and only the expectation moved — because
// "high message count no longer changes the label" is exactly the耦合 the newer
// story fixes; a deleted test would leave that silent.
func TestATDD_69_2_AC5_BothThresholdTriggerLabel(t *testing.T) {
	// maxSize=10，填 5 轮工具调用 = 10 槽位满；token limit 设小值同时越阈。
	k, ctxMgr, proc, cid := setupCompactKernel(t, 10)
	proc.DebugChan = make(chan types.SyscallEvent, 64)

	if err := ctxMgr.SetTokenLimit(cid, 5_000); err != nil {
		t.Fatalf("SetTokenLimit: %v", err)
	}
	for i := range 5 {
		appendToolHeavyMessage(t, ctxMgr, cid, "call_"+string(rune('a'+i)), 4_000)
	}

	usage, err := ctxMgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if usage.Percentage <= 80.0 {
		t.Fatalf("token%% = %.1f, want > 80", usage.Percentage)
	}
	// The fixture above fills 10/10 slots (>70% slot usage) — the exact shape
	// that pre-71.1 produced trigger "both". The slot axis is deleted, so this
	// high message count must NOT change the label: token_threshold only.
	// (Code-review 2026-08-01: the former hard slotPct>70 precondition was
	// inert — the trigger assertion below is independent of slot count, and the
	// fixture's high message count IS the variation under test.)

	k.autoCompactIfNeeded(proc, 1)

	if got := readCompactTrigger(t, proc); got != "token_threshold" {
		t.Errorf("trigger = %q, want token_threshold — the slot axis must contribute nothing to the label", got)
	}
}
