package kernel

// ATDD red-phase scaffolds for Story 56.6 — CLI driver subagent 进程树重建
// (AC2 子节点合成 / AC3 内部步骤归属 / AC4 生命周期收尾 / AC5 gate 防双计 /
//  AC8 幂等).
//
// 缺口本质（调查 cli-driver-subagent-tree-invisible-investigation.md, High）:
//   CLI driver 把整个 agentic loop（含 Task subagent 派生）跑在单个 claude -p
//   OS 进程内，rnix 只把 CLI 工具调用观测为 step，从不走 ActionSpawn。修复 =
//   kernel/observe.go::setupDriverStreamHandler 观测到 CLI driver 进程的 Task
//   tool_use（input 含 subagent_type）时合成子进程历史节点（确定性 UUID +
//   ParentUUID=宿主 + PID=0），并把 parent_tool_use_id 命中的内部帧路由进子节点
//   自己的 steps.jsonl。
//
// 复用 harness（同包 atdd_40_4_cli_toolinput_test.go）: streamStubFile +
//   streamHarness.feed/flushAndReadSteps。本文件新增 CLI-gated 构造器（feed 前
//   设 proc.PrimaryDevice）+ 携带 parent_tool_use_id 的 sidechain 事件构造器。
//   事件 map 以「driver 经 AC1 透传后的 data map」形态喂入（parent_tool_use_id
//   为顶层键）——故本文件 RED 假定 AC1 已就位（AC1 由 drivers/llm 单测独立护栏）。
//
// 红灯机制（记忆 atdd-code-story-red-mechanism-preference）:
//   🔴 RED: t.Skip("RED: 56.6: …")，dev-story 移 skip 验 RED→GREEN。
//   🟢 GREEN-guard（INT-011/012，不 skip）: 合成路径实现前根本不存在 → "零合成
//      节点" 天然成立；实时拦"实现后对 API driver / 非-Task 工具过度触发/双计"。

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// newSubagent566Harness mirrors newStreamHarness but sets proc.PrimaryDevice
// BEFORE setupDriverStreamHandler so the CLI-driver synthesis gate sees it. The
// stub device path is distinct from 40.4's to avoid registry collisions.
func newSubagent566Harness(t *testing.T, primaryDevice string) *streamHarness {
	t.Helper()
	tk := newObserveTestKernel(t)
	tk.proc.PrimaryDevice = primaryDevice // gate 判据: PrimaryDevice ∈ CLI 设备集
	stub := &streamStubFile{}

	if err := tk.k.vfs.DeviceRegistry().Register("/dev/llm/teststream566",
		func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return stub, nil
		}); err != nil {
		t.Fatalf("Register stub stream device: %v", err)
	}

	fd, err := tk.k.vfsOpenWithEvent(tk.proc, "/dev/llm/teststream566", vfs.O_RDWR)
	if err != nil {
		t.Fatalf("vfsOpenWithEvent: %v", err)
	}
	tk.k.setupDriverStreamHandler(tk.proc, fd)
	if stub.handler == nil {
		t.Fatal("setupDriverStreamHandler did not attach a stream handler")
	}
	return &streamHarness{tk: tk, stub: stub}
}

// --- CLI subagent 事件构造器（parent_tool_use_id 为顶层键，模拟 AC1 透传后形态）---

// evtTaskToolUse566 是主线程派生 Task subagent 的 assistant 帧（含 subagent_type，
// 无 parent_tool_use_id）——这是合成触发点。model 非空时写入 input.model。
func evtTaskToolUse566(taskID, subagentType, description, model string) map[string]any {
	input := map[string]any{"subagent_type": subagentType, "description": description}
	if model != "" {
		input["model"] = model
	}
	return map[string]any{
		"type": "assistant", "role": "assistant",
		"content": []map[string]any{
			{"type": "tool_use", "id": taskID, "name": "Task", "input": input},
		},
	}
}

// evtTaskToolUsePromptOnly566 是无 description、仅 prompt 的 Task 帧（验 Intent 回退）。
func evtTaskToolUsePromptOnly566(taskID, subagentType, prompt string) map[string]any {
	return map[string]any{
		"type": "assistant", "role": "assistant",
		"content": []map[string]any{
			{"type": "tool_use", "id": taskID, "name": "Task",
				"input": map[string]any{"subagent_type": subagentType, "prompt": prompt}},
		},
	}
}

// evtSidechainStarted566 是 subagent 内部工具的 tool_call/started（带 parent_tool_use_id）。
func evtSidechainStarted566(parentToolUseID, tool, callID string) map[string]any {
	return map[string]any{
		"type": "tool_call", "content": "started",
		"tool": tool, "call_id": callID,
		"parent_tool_use_id": parentToolUseID,
	}
}

// evtSidechainAssistant566 是 subagent 内部 assistant 帧（带 parent_tool_use_id +
// tool_use 权威 input，驱动 flush）。
func evtSidechainAssistant566(parentToolUseID, callID, tool string, input any) map[string]any {
	return map[string]any{
		"type": "assistant", "role": "assistant",
		"parent_tool_use_id": parentToolUseID,
		"content": []map[string]any{
			{"type": "tool_use", "id": callID, "name": tool, "input": input},
		},
	}
}

// evtSidechainUserResult566 是 subagent 内部工具的 user(tool_result)（带 parent_tool_use_id）。
func evtSidechainUserResult566(parentToolUseID, toolUseID, result string) map[string]any {
	return map[string]any{
		"type": "user", "role": "user",
		"parent_tool_use_id": parentToolUseID,
		"content": []map[string]any{
			{"type": "tool_result", "tool_use_id": toolUseID, "content": result},
		},
	}
}

// evtTaskResult566 是主线程 Task tool_result（命中父 tool_use_id → 触发子节点 finalize）。
func evtTaskResult566(taskID, report string) map[string]any {
	return map[string]any{
		"type": "user", "role": "user",
		"content": []map[string]any{
			{"type": "tool_result", "tool_use_id": taskID, "content": report},
		},
	}
}

// evtDone566 是流结束帧（兜底 finalize 触发点）。
func evtDone566(content string) map[string]any {
	return map[string]any{"type": "done", "content": content}
}

// evtRnixAgentToolUse566 是 API driver 的 rnix Agent 工具调用（schema = intent/agent/
// model，**无** subagent_type）——gate 反例：绝不触发合成。
func evtRnixAgentToolUse566(callID, intent, agent string) map[string]any {
	return map[string]any{
		"type": "assistant", "role": "assistant",
		"content": []map[string]any{
			{"type": "tool_use", "id": callID, "name": "Agent",
				"input": map[string]any{"intent": intent, "agent": agent}},
		},
	}
}

// evtNormalToolUse566 是普通工具调用（无 subagent_type）——gate 反例：仅 Task/Agent
// 含 subagent_type 才合成。
func evtNormalToolUse566(callID, tool string, input any) map[string]any {
	return map[string]any{
		"type": "assistant", "role": "assistant",
		"content": []map[string]any{
			{"type": "tool_use", "id": callID, "name": tool, "input": input},
		},
	}
}

// --- 查询 helper ---

// syntheticChildren566 returns the synthesized child nodes of hostUUID registered
// in the in-memory procHistory (recommended "即时注册" path, Dev Notes decision #2).
// Before implementation this is always empty — the basis of the RED assertions.
func syntheticChildren566(k *KernelImpl, hostUUID string) []vfs.ProcInfo {
	var out []vfs.ProcInfo
	for _, p := range k.procHistory.List() {
		if p.Synthetic && p.ParentUUID == hostUUID {
			out = append(out, p)
		}
	}
	return out
}

// readStepsForUUID566 reads <baseDir>/steps/<uuid>/steps.jsonl, tolerating a
// missing file (returns nil) so a not-yet-synthesized child yields an empty slice.
func readStepsForUUID566(t *testing.T, baseDir, uuid string) []types.StepRecord {
	t.Helper()
	path := filepath.Join(baseDir, "steps", uuid, "steps.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []types.StepRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var rec types.StepRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("unmarshal child step: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

// stepsContainTool566 reports whether any step record references the given tool.
func stepsContainTool566(recs []types.StepRecord, tool string) bool {
	for _, r := range recs {
		if strings.Contains(r.ToolPath, tool) || strings.Contains(r.Summary, tool) || r.Action == tool {
			return true
		}
	}
	return false
}

// feedSubagentSequence566 feeds a complete CLI subagent lifecycle: Task派生 →
// 内部 Bash（started + 权威 assistant + result）→ Task 完成 → done。
func feedSubagentSequence566(h *streamHarness, taskID, report string) {
	h.feed(evtTaskToolUse566(taskID, "code-reviewer", "review the diff", ""))
	h.feed(evtSidechainStarted566(taskID, "Bash", "call_bash1"))
	h.feed(evtSidechainAssistant566(taskID, "call_bash1", "Bash", map[string]any{"command": "ls -la"}))
	h.feed(evtSidechainUserResult566(taskID, "call_bash1", "file listing"))
	h.feed(evtTaskResult566(taskID, report))
	h.feed(evtDone566("all done"))
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-001 (P0, AC2): CLI host 观测 Task tool_use（含 subagent_type）→ 合成
// 1 个子节点，ParentUUID==宿主UUID 且 PID==0。RED: 无合成逻辑 → 0 个节点。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_001_SynthesizesChildForTaskToolUse(t *testing.T) {
	t.Skip("RED: 56.6: observe.go 无 CLI subagent 合成逻辑；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	h.feed(evtTaskToolUse566("toolu_task1", "code-reviewer", "review the diff", ""))

	children := syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if len(children) != 1 {
		t.Fatalf("AC2 FAIL: 合成子节点数 = %d, want 1（Task tool_use 未触发合成）", len(children))
	}
	c := children[0]
	if c.ParentUUID != h.tk.proc.UUID {
		t.Errorf("AC2 FAIL: 子节点 ParentUUID = %q, want 宿主 %q", c.ParentUUID, h.tk.proc.UUID)
	}
	if c.PID != 0 {
		t.Errorf("AC2 FAIL: 合成节点 PID = %d, want 0（走 UUID 链路非 PID 链路）", c.PID)
	}
	if c.UUID == "" {
		t.Error("AC2 FAIL: 合成节点 UUID 为空（tree builder 视为非法节点）")
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-003 (P1, AC2): Intent==description；无 description 时回退截断 prompt。
// RED: 无合成逻辑。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_003_ChildIntentFromDescriptionElsePrompt(t *testing.T) {
	t.Skip("RED: 56.6: 无合成逻辑无法填 Intent；dev-story 移 skip 验 RED→GREEN")

	// (a) 有 description → Intent==description。
	h1 := newSubagent566Harness(t, "/dev/llm/claude")
	h1.feed(evtTaskToolUse566("toolu_a", "code-reviewer", "审查认证模块", ""))
	c1 := syntheticChildren566(h1.tk.k, h1.tk.proc.UUID)
	if len(c1) != 1 || c1[0].Intent != "审查认证模块" {
		t.Errorf("AC2 FAIL: Intent 应取 description=审查认证模块, got %+v", c1)
	}

	// (b) 无 description、仅 prompt → Intent 为（截断的）prompt，非空。
	h2 := newSubagent566Harness(t, "/dev/llm/claude")
	h2.feed(evtTaskToolUsePromptOnly566("toolu_b", "code-reviewer", "这是一个很长的 prompt 用来在缺少 description 时回退"))
	c2 := syntheticChildren566(h2.tk.k, h2.tk.proc.UUID)
	if len(c2) != 1 || c2[0].Intent == "" {
		t.Errorf("AC2 FAIL: 无 description 时 Intent 应回退为 prompt（非空）, got %+v", c2)
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-004 (P2, AC2): Provider/Model 取自 input.model，缺失则继承宿主。
// RED: 无合成逻辑。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_004_ChildModelFromInputElseInheritHost(t *testing.T) {
	t.Skip("RED: 56.6: 无合成逻辑；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	h.tk.proc.Model = "host-sonnet"
	// (a) input 带 model → 子节点 Model 取之。
	h.feed(evtTaskToolUse566("toolu_m", "code-reviewer", "task with model", "child-haiku"))
	c := syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if len(c) != 1 || c[0].Model != "child-haiku" {
		t.Errorf("AC2 FAIL: Model 应取 input.model=child-haiku, got %+v", c)
	}

	// (b) input 无 model → 继承宿主 Model。
	h2 := newSubagent566Harness(t, "/dev/llm/claude")
	h2.tk.proc.Model = "host-sonnet"
	h2.feed(evtTaskToolUse566("toolu_n", "code-reviewer", "task no model", ""))
	c2 := syntheticChildren566(h2.tk.k, h2.tk.proc.UUID)
	if len(c2) != 1 || c2[0].Model != "host-sonnet" {
		t.Errorf("AC2 FAIL: 无 input.model 应继承宿主 Model=host-sonnet, got %+v", c2)
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-005 (P1, AC2): CreatedAt 非零（deferred-work#765 红线）+ State 初始 Running。
// RED: 无合成逻辑。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_005_ChildCreatedAtNonZeroStateRunning(t *testing.T) {
	t.Skip("RED: 56.6: 无合成逻辑；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	h.feed(evtTaskToolUse566("toolu_t", "code-reviewer", "running check", ""))
	c := syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if len(c) != 1 {
		t.Fatalf("AC2 FAIL: 合成子节点数 = %d, want 1", len(c))
	}
	if c[0].CreatedAt.IsZero() {
		t.Error("AC2 FAIL: 合成节点 CreatedAt 为零值（deferred-work#765 ELAPSED 渲染 garbage）")
	}
	if c[0].State != types.StateRunning {
		t.Errorf("AC2 FAIL: 合成节点初始 State = %v, want Running（finalize 前）", c[0].State)
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-006 (P0, AC3 核心): subagent 内部工具步骤写入子节点 steps.jsonl。
// RED: 无合成逻辑 → 无子节点 steps.jsonl。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_006_InternalStepsRoutedToChildStepsJSONL(t *testing.T) {
	t.Skip("RED: 56.6: 无内部步骤归属；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	feedSubagentSequence566(h, "toolu_task1", "subagent report")
	// 关闭宿主 stepWriter 确保任何 buffer flush（子 writer 由 finalize 关闭）。
	_ = h.flushAndReadSteps(t)

	children := syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if len(children) == 0 {
		t.Fatal("AC3 FAIL: 无合成子节点 → 无法验证内部步骤归属")
	}
	childSteps := readStepsForUUID566(t, h.tk.baseDir, children[0].UUID)
	if !stepsContainTool566(childSteps, "Bash") {
		t.Errorf("AC3 FAIL: 子节点 steps.jsonl 不含内部 Bash 步骤（%d 条），内部步骤未归属子节点", len(childSteps))
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-007 (P0, AC3): 宿主 step 流不再被 subagent 内部帧污染。
// RED: 当前 parent_tool_use_id 被忽略，subagent 的 Bash 步骤误灌宿主 steps.jsonl。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_007_HostStepStreamNotPollutedBySubagentFrames(t *testing.T) {
	t.Skip("RED: 56.6: subagent 内部帧仍污染宿主 step 流；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	feedSubagentSequence566(h, "toolu_task1", "subagent report")
	hostSteps := h.flushAndReadSteps(t)

	if stepsContainTool566(hostSteps, "Bash") {
		t.Errorf("AC3 FAIL: 宿主 steps.jsonl 含 subagent 内部 Bash 步骤（%d 条），未从宿主视图剔除/改灌子节点", len(hostSteps))
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-008 (P1, AC4): 宿主收 Task tool_result → 子节点 finalize（Dead+Result+DeadAt）。
// RED: 无合成/finalize 逻辑。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_008_ChildFinalizedOnTaskToolResult(t *testing.T) {
	t.Skip("RED: 56.6: 无 finalize 逻辑；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	feedSubagentSequence566(h, "toolu_task1", "final subagent report text")

	children := syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if len(children) != 1 {
		t.Fatalf("AC4 FAIL: 合成子节点数 = %d, want 1", len(children))
	}
	c := children[0]
	if c.State != types.StateDead {
		t.Errorf("AC4 FAIL: finalize 后 State = %v, want Dead", c.State)
	}
	if c.Result == "" {
		t.Error("AC4 FAIL: finalize 后 Result 为空（应为 report 文本）")
	}
	if c.DeadAt.IsZero() {
		t.Error("AC4 FAIL: finalize 后 DeadAt 为零值")
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-010 (P1, AC4): 流 done 时仍 Running 的子节点兜底 finalize（防泄漏）。
// RED: 无兜底逻辑。喂 Task + 内部步骤但**不**喂 Task tool_result，只喂 done。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_010_DanglingChildFinalizedOnStreamDone(t *testing.T) {
	t.Skip("RED: 56.6: 无 stream-end 兜底 finalize；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	h.feed(evtTaskToolUse566("toolu_task1", "code-reviewer", "no explicit result", ""))
	h.feed(evtSidechainStarted566("toolu_task1", "Read", "call_read1"))
	h.feed(evtSidechainAssistant566("toolu_task1", "call_read1", "Read", map[string]any{"file_path": "/tmp/x"}))
	// 故意不喂 Task tool_result —— 仅靠 done 兜底。
	h.feed(evtDone566("stream ended"))

	children := syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if len(children) != 1 {
		t.Fatalf("AC4 FAIL: 合成子节点数 = %d, want 1", len(children))
	}
	if children[0].State != types.StateDead {
		t.Errorf("AC4 FAIL: 流结束后悬挂子节点 State = %v, want Dead（兜底 finalize 防泄漏）", children[0].State)
	}
}

// ---------------------------------------------------------------------------
// 🟢 56-6-INT-011 (P0, AC5 红线 · GREEN-guard 不 skip): API driver host 的 rnix
// Agent tool_use（无 subagent_type）→ 零合成节点（防"真子进程 + 合成节点"双计）。
// 实现前合成路径不存在 → 0（天然真）；实现后 gate 须排除非 CLI host → 仍 0。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_011_APIDriverAgentNoSyntheticNode(t *testing.T) {
	h := newSubagent566Harness(t, "/dev/llm/openai") // API driver host
	h.feed(evtRnixAgentToolUse566("call_agent1", "summarize repo", "summarizer"))

	if n := len(syntheticChildren566(h.tk.k, h.tk.proc.UUID)); n != 0 {
		t.Errorf("AC5 FAIL: API driver host 产生了 %d 个合成节点, want 0（双计红线——Agent 已走 ActionSpawn 真子进程）", n)
	}
}

// ---------------------------------------------------------------------------
// 🟢 56-6-INT-012 (P0, AC5 gate · GREEN-guard 不 skip): CLI host 的普通工具调用
// （无 subagent_type）→ 零合成节点（仅 Task/Agent 含 subagent_type 才合成）。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_012_CLIHostNonTaskToolNoSyntheticNode(t *testing.T) {
	h := newSubagent566Harness(t, "/dev/llm/claude") // CLI host
	h.feed(evtSidechainStarted566("", "Bash", "call_b"))
	h.feed(evtNormalToolUse566("call_b", "Bash", map[string]any{"command": "echo hi"}))

	if n := len(syntheticChildren566(h.tk.k, h.tk.proc.UUID)); n != 0 {
		t.Errorf("AC5 FAIL: CLI host 普通工具产生了 %d 个合成节点, want 0（仅 subagent_type 形状才合成）", n)
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-022 (P1, AC2/AC8 幂等): 同一 Task tool_use 在流中重复出现（重放）
// → 恰好 1 个合成节点（确定性 UUID 去重，daemon-restart/LoadHistory 不产生重复）。
// RED: 无合成 → 0 个（≠1）。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_022_DeterministicUUIDDedupesReplay(t *testing.T) {
	t.Skip("RED: 56.6: 无确定性 UUID 合成；dev-story 移 skip 验 RED→GREEN")

	h := newSubagent566Harness(t, "/dev/llm/claude")
	// 同一 taskID 的 Task tool_use 出现两次（模拟重放 / 二次观测）。
	h.feed(evtTaskToolUse566("toolu_dup", "code-reviewer", "dedupe me", ""))
	h.feed(evtTaskToolUse566("toolu_dup", "code-reviewer", "dedupe me", ""))

	if n := len(syntheticChildren566(h.tk.k, h.tk.proc.UUID)); n != 1 {
		t.Errorf("AC8 FAIL: 重复 Task tool_use 产生 %d 个合成节点, want 1（确定性 UUID 未去重）", n)
	}
}
