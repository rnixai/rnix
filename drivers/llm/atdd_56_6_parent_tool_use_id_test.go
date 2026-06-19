package llm

// ATDD red-phase scaffolds for Story 56.6 — CLI driver subagent 进程树重建
// (AC1: 驱动层保真 — claude-cli stream-json 解析保留 parent_tool_use_id).
//
// 根因（调查 cli-driver-subagent-tree-invisible-investigation.md, High 置信度）:
//   claudeStreamEvent (claude_cli.go:381) 无 parent_tool_use_id 字段 → JSON
//   反序列化即丢弃；assistant/user/stream_event 帧的 data map (claude_cli.go
//   :519/535/556 + parser.extractStreamEvent) 不含该跨进程关联 join key。
//   修复 = 结构体加 ParentToolUseID 字段 + 三 case 非空写入 data map（omitempty 风格）。
//
// cc-src 权威帧结构 (coreSchemas.ts:1290-1504): assistant/user/stream_event 帧
//   顶层均带 parent_tool_use_id（主线程消息 = null；subagent 内部消息 = 派生它的
//   父 Task 工具的 tool_use_id）+ session_id。
//
// 红灯机制（记忆 atdd-code-story-red-mechanism-preference）:
//   🔴 RED: t.Skip("RED: 56.6: <原因>")，dev-story 移 skip 填逻辑验 RED→GREEN。
//   🟢 GREEN-guard（UNIT-004，不 skip）: 主线程帧 parent_tool_use_id==null →
//      data map 不写该键（omitempty）。现状即真（根本不写任何 parent_tool_use_id）
//      → 修复后须保持（仅非空才写）。实时拦"无条件写空键污染主线程消息"回归。
//
// 测试级别: UNIT（drivers/llm 纯 stream 解析；driver 不 import kernel，继承
// Epic 56 Constraint）。注入: 自包含 helper process（GO_TEST_PROCESS_566 守卫，
// 不污染既有 TestHelperProcess switch）产带 parent_tool_use_id 的 stream-json。

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestHelperProcessSubagent566 是 56.6 AC1 专用的 mock claude CLI 子进程，按真实
// CLI subagent 内联流形态产出：主线程 Task 派生 → subagent 内部 assistant/user/
// stream_event 帧（带顶层 parent_tool_use_id）→ 主线程 Task tool_result → result。
// 仅在 GO_TEST_PROCESS_566=1 时执行（普通测试运行时 return，no-op）。
func TestHelperProcessSubagent566(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS_566") != "1" {
		return
	}
	lines := []string{
		// 1) 主线程 assistant：派生 Task subagent（无 parent_tool_use_id）。
		`{"type":"assistant","message":{"id":"msg_main","content":[{"type":"tool_use","id":"toolu_task1","name":"Task","input":{"subagent_type":"code-reviewer","description":"review diff"}}]}}`,
		// 2) subagent 内部 assistant（parent_tool_use_id = 父 Task 的 tool_use_id）。
		`{"type":"assistant","parent_tool_use_id":"toolu_task1","session_id":"sess_1","message":{"id":"msg_sub","content":[{"type":"tool_use","id":"toolu_bash1","name":"Bash","input":{"command":"ls"}}]}}`,
		// 3) subagent 内部 user(tool_result)（带 parent_tool_use_id）。
		`{"type":"user","parent_tool_use_id":"toolu_task1","session_id":"sess_1","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_bash1","content":"file listing"}]}}`,
		// 4) subagent 内部 stream_event（带 parent_tool_use_id）。
		`{"type":"stream_event","parent_tool_use_id":"toolu_task1","session_id":"sess_1","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","id":"toolu_read1"}}}`,
		// 5) 主线程 user(tool_result)：Task 完成（命中父 tool_use_id，主线程无 parent_tool_use_id）。
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_task1","content":"subagent report"}]}}`,
		// 6) result 收尾。
		`{"type":"result","subtype":"success","result":"done","is_error":false,"input_tokens":10,"output_tokens":5}`,
	}
	for _, l := range lines {
		_, _ = os.Stdout.WriteString(l + "\n")
	}
	os.Exit(0)
}

// subagent566CmdBuilder re-execs TestHelperProcessSubagent566 as the mock claude
// CLI, mirroring mockCmdBuilder but with an isolated env guard.
func subagent566CmdBuilder() CommandBuilder {
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcessSubagent566", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS_566=1")
		return cmd
	}
}

// collectSubagentStream566 drains the driver's Stream channel into a slice.
func collectSubagentStream566(t *testing.T) []StreamEvent {
	t.Helper()
	d := NewClaudeCliDriver(WithCommandBuilder(subagent566CmdBuilder()))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "review the diff with a subagent"})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}
	return events
}

// parentToolUseID extracts the parent_tool_use_id string from a StreamEvent's Data
// map (present+non-empty → value, ok=true). Returns ok=false when absent/empty.
func parentToolUseID(e StreamEvent) (string, bool) {
	if e.Data == nil {
		return "", false
	}
	v, ok := e.Data["parent_tool_use_id"].(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// -----------------------------------------------------------------------------
// 56-6-UNIT-001 (P0, AC1): assistant 帧透传 parent_tool_use_id 进 StreamEvent.Data。
// RED: claudeStreamEvent 无该字段 + assistant case data map 不写 → 命中 0。
// -----------------------------------------------------------------------------
func TestATDD_56_6_UNIT_001_AssistantFrameCarriesParentToolUseID(t *testing.T) {
	t.Skip("RED: 56.6: claudeStreamEvent 缺 parent_tool_use_id 字段 + assistant data map 未透传；dev-story 移 skip 验 RED→GREEN")

	events := collectSubagentStream566(t)
	found := false
	for _, e := range events {
		if e.Type != "assistant" {
			continue
		}
		if id, ok := parentToolUseID(e); ok && id == "toolu_task1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AC1 FAIL: 无 assistant StreamEvent 携带 Data[parent_tool_use_id]=toolu_task1（join key 被丢弃）")
	}
}

// -----------------------------------------------------------------------------
// 56-6-UNIT-002 (P0, AC1): user(tool_result) 帧透传 parent_tool_use_id。
// RED: user case data map 仅写 role+content，丢弃 parent_tool_use_id。
// -----------------------------------------------------------------------------
func TestATDD_56_6_UNIT_002_UserFrameCarriesParentToolUseID(t *testing.T) {
	t.Skip("RED: 56.6: user 帧 data map 未透传 parent_tool_use_id；dev-story 移 skip 验 RED→GREEN")

	events := collectSubagentStream566(t)
	found := false
	for _, e := range events {
		if e.Type != "user" {
			continue
		}
		if id, ok := parentToolUseID(e); ok && id == "toolu_task1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AC1 FAIL: 无 user StreamEvent 携带 Data[parent_tool_use_id]=toolu_task1")
	}
}

// -----------------------------------------------------------------------------
// 56-6-UNIT-003 (P1, AC1): stream_event 帧经 extractStreamEvent 透传 parent_tool_use_id。
// RED: extractStreamEvent 不知顶层 parent_tool_use_id，派生的 tool_call 事件 Data 缺该键。
// -----------------------------------------------------------------------------
func TestATDD_56_6_UNIT_003_StreamEventFrameCarriesParentToolUseID(t *testing.T) {
	t.Skip("RED: 56.6: extractStreamEvent 路径未透传 parent_tool_use_id；dev-story 移 skip 验 RED→GREEN")

	events := collectSubagentStream566(t)
	found := false
	for _, e := range events {
		// stream_event(tool_use content_block_start) → 派生事件 Data["tool"]="Read"。
		if tool, _ := e.Data["tool"].(string); tool != "Read" {
			continue
		}
		if id, ok := parentToolUseID(e); ok && id == "toolu_task1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AC1 FAIL: stream_event 派生事件未携带 Data[parent_tool_use_id]=toolu_task1")
	}
}

// -----------------------------------------------------------------------------
// 56-6-UNIT-004 (P1, AC1): 主线程帧（parent_tool_use_id==null）data map 不写该键。
// GREEN-guard（不 skip）: 现状根本不写任何 parent_tool_use_id（trivially 真）；
// 修复后仅非空才写——主线程 Task 帧仍须无该键。拦"无条件写空键污染主线程消息"回归。
// 主线程 Task assistant 帧最先发出 → 首个 Type=="assistant" 事件即主线程。
// -----------------------------------------------------------------------------
func TestATDD_56_6_UNIT_004_MainThreadFrameOmitsParentToolUseID(t *testing.T) {
	events := collectSubagentStream566(t)
	for _, e := range events {
		if e.Type != "assistant" {
			continue
		}
		// 首个 assistant = 主线程 Task 派生帧（无 parent_tool_use_id）。
		if e.Data == nil {
			return // 无 Data 即不可能有该键，合规。
		}
		if _, present := e.Data["parent_tool_use_id"]; present {
			t.Errorf("AC1 FAIL: 主线程 assistant 帧不应含 parent_tool_use_id 键（omitempty 被破坏，空键污染主线程消息）: %v", e.Data["parent_tool_use_id"])
		}
		return // 仅校验首个 assistant（主线程）。
	}
	t.Fatal("AC1 FAIL: 未收到任何 assistant StreamEvent（fixture 异常）")
}
