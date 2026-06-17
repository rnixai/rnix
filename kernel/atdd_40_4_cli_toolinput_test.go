package kernel

// ATDD red-phase scaffolds for Story 40.4 — Claude CLI 工具 Input 权威回填
// (修复 Dashboard Timeline 工具参数显示缺失).
//
// 根因（raw.jsonl 实证 / story Background）: claude CLI 在 --include-partial-messages
// 下把上一轮 user(tool_result) 与下一轮工具的 partial input deltas 交错输出。
// kernel/observe.go::setupDriverStreamHandler 的 user handler 无条件 flushPendingTool()
// (observe.go:730-731)，把刚 started、只累积首分片的下一轮工具提前截断 flush。
// 修复 = assistant 块按 call_id 捕获权威 input + 修正 flush 触发时序。
//
// 红灯机制（[[atdd-code-story-red-mechanism-preference]]）:
//   - 真 RED 用例: t.Skip("RED: 40.4: <原因>")，dev-story 移 skip 填逻辑验 RED→GREEN。
//   - green-guard 用例（INT-004 happy path）: 不 skip，实时拦截 flush 时序修正对正常
//     路径的破坏（GREEN-stays-GREEN）。
//
// 测试级别: INT（驱动 setupDriverStreamHandler 闭包 + 读回 steps.jsonl，跨
// kernel+vfs+stepWriter 持久化协作）。注入机制: stub StreamObserver 设备捕获
// handler 闭包 → 喂 claude-cli 真实事件序列（字段名以 claude_cli.go:1119-1195
// extractStreamEvent + contentBlocksToAny:806-832 为准）。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// jsonHasField 解析 toolInput JSON 并断言顶层字段 key 等于 want（string 值）。
// 解析失败（如截断的 '{"file_p'）或字段缺失/不符均返回 false。
func jsonHasField(t *testing.T, toolInput, key, want string) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(toolInput), &m); err != nil {
		return false
	}
	got, ok := m[key].(string)
	return ok && got == want
}

// streamStubFile is a minimal VFSFile that implements vfs.StreamObserver so the
// kernel's setupDriverStreamHandler can attach a handler we then drive directly
// with synthetic claude-cli stream events.
type streamStubFile struct {
	handler func(evt map[string]any)
}

func (f *streamStubFile) Read(_ int) ([]byte, error)              { return nil, nil }
func (f *streamStubFile) Write(_ context.Context, _ []byte) error { return nil }
func (f *streamStubFile) Close() error                            { return nil }
func (f *streamStubFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{Name: "teststream", IsDevice: true}, nil
}
func (f *streamStubFile) SetStreamHandler(fn func(map[string]any)) { f.handler = fn }

// streamHarness wires an observeTestKernel to a stub StreamObserver device,
// attaches the kernel stream handler, and exposes feed() to inject events.
type streamHarness struct {
	tk   *observeTestKernel
	stub *streamStubFile
}

// newStreamHarness registers a stub StreamObserver device under
// /dev/llm/teststream, opens it (allocating an FD on the test process), and runs
// setupDriverStreamHandler so the stub captures the kernel's stream handler.
func newStreamHarness(t *testing.T) *streamHarness {
	t.Helper()
	tk := newObserveTestKernel(t)
	stub := &streamStubFile{}

	if err := tk.k.vfs.DeviceRegistry().Register("/dev/llm/teststream",
		func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return stub, nil
		}); err != nil {
		t.Fatalf("Register stub stream device: %v", err)
	}

	fd, err := tk.k.vfsOpenWithEvent(tk.proc, "/dev/llm/teststream", vfs.O_RDWR)
	if err != nil {
		t.Fatalf("vfsOpenWithEvent: %v", err)
	}
	tk.k.setupDriverStreamHandler(tk.proc, fd)
	if stub.handler == nil {
		t.Fatal("setupDriverStreamHandler did not attach a stream handler")
	}
	return &streamHarness{tk: tk, stub: stub}
}

// feed injects one stream event into the captured handler.
func (h *streamHarness) feed(evt map[string]any) { h.stub.handler(evt) }

// flushAndReadSteps closes the stepWriter so its buffer flushes, then reads back
// all persisted step records from steps.jsonl.
func (h *streamHarness) flushAndReadSteps(t *testing.T) []types.StepRecord {
	t.Helper()
	if h.tk.proc.stepWriter != nil {
		_ = h.tk.proc.stepWriter.Close()
		h.tk.proc.stepWriter = nil
	}
	return h.tk.readSteps(t)
}

// --- claude-cli stream event constructors (字段名以 claude_cli.go 为准) -------

// evtStarted 对应 content_block_start(tool_use) → tool_call/started，携带 call_id。
func evtStarted(tool, callID string) map[string]any {
	return map[string]any{
		"type":    "tool_call",
		"content": "started",
		"tool":    tool,
		"call_id": callID,
		"subtype": "started",
	}
}

// evtInputDelta 对应 content_block_delta(input_json_delta) → tool_call/input_delta。
func evtInputDelta(partial string) map[string]any {
	return map[string]any{
		"type":         "tool_call",
		"content":      "input_delta",
		"partial_json": partial,
	}
}

// evtAssistantToolUse 对应 assistant 事件（contentBlocksToAny 形态 []map[string]any），
// 携带 tool_use block 的权威 input（已 unmarshal 的 map）。
func evtAssistantToolUse(id, name string, input any) map[string]any {
	return map[string]any{
		"type": "assistant",
		"role": "assistant",
		"content": []map[string]any{
			{"type": "tool_use", "id": id, "name": name, "input": input},
		},
	}
}

// evtUserToolResult 对应上一轮工具的 user(tool_result) 事件 —— 截断元凶触发点。
func evtUserToolResult(toolUseID, result string) map[string]any {
	return map[string]any{
		"type": "user",
		"role": "user",
		"content": []map[string]any{
			{"type": "tool_result", "tool_use_id": toolUseID, "content": result},
		},
	}
}

// PLACEHOLDER_TESTS

// findStepByPathContains 返回 ToolPath/Summary 含 needle 的首个 step record。
func findStepByToolInputOwner(t *testing.T, recs []types.StepRecord, tool string) types.StepRecord {
	t.Helper()
	for _, r := range recs {
		if strings.Contains(r.ToolPath, tool) || strings.Contains(r.Summary, tool) || r.Action == tool {
			return r
		}
	}
	t.Fatalf("no step record found for tool %q in %d records", tool, len(recs))
	return types.StepRecord{}
}

// -----------------------------------------------------------------------------
// 40.4-INT-001 (P0, AC #2/#3/#4/#6a): 交错序列 —— USER(上一轮 tool_result) 夹在
// 下一轮工具的首 delta 与剩余 delta 之间，工具不得被提前 flush；权威 input 完整。
// RED: 当前 user handler 无条件 flush，把 toolA 截断成首分片 '{"file_p'。
// -----------------------------------------------------------------------------
func TestATDD_40_4_INT_001_InterleavedUserDoesNotTruncate(t *testing.T) {
	h := newStreamHarness(t)
	// 错位序列（story raw[907..921] tooluse_dPiNSh）:
	h.feed(evtStarted("Read", "call_A"))                                   // 工具A started
	h.feed(evtInputDelta(`{"file_p`))                                      // A 首分片
	h.feed(evtUserToolResult("call_PREV", "previous tool output"))         // ⚡ 属上一轮，不该 flush A
	h.feed(evtInputDelta(`ath":"/etc/hosts"}`))                            // A 剩余分片
	h.feed(evtAssistantToolUse("call_A", "Read", map[string]any{"file_path": "/etc/hosts"})) // A 权威 input

	recs := h.flushAndReadSteps(t)
	rec := findStepByToolInputOwner(t, recs, "Read")
	// 修复前落盘的是截断分片 '{"file_p'（json.Unmarshal 失败）→ jsonHasField=false。
	if !jsonHasField(t, rec.ToolInput, "file_path", "/etc/hosts") {
		t.Errorf("ToolInput 被截断或缺字段: got %q, want 完整 JSON 含 file_path=/etc/hosts", rec.ToolInput)
	}
}

// -----------------------------------------------------------------------------
// 40.4-INT-002 (P0, AC #1/#3/#4/#6b): USER 抢在首 delta 之前到达 —— inputBuf 为空，
// 必须靠 assistant 权威 input 回填，治"完全空"(story step 60/61)。
// RED: 当前 USER flush 落盘空串，且 assistant input 被丢弃。
// -----------------------------------------------------------------------------
func TestATDD_40_4_INT_002_UserBeforeFirstDeltaBackfilled(t *testing.T) {
	h := newStreamHarness(t)
	h.feed(evtStarted("Write", "call_A"))                          // started，inputBuf 空
	h.feed(evtUserToolResult("call_PREV", "prev"))                 // ⚡ USER 抢在首 delta 前
	h.feed(evtInputDelta(`{"file_path":"a.txt","content":"hi"}`))  // delta（已被截断 flush，丢失）
	h.feed(evtAssistantToolUse("call_A", "Write", map[string]any{"file_path": "a.txt", "content": "hi"}))

	recs := h.flushAndReadSteps(t)
	rec := findStepByToolInputOwner(t, recs, "Write")
	if strings.TrimSpace(rec.ToolInput) == "" {
		t.Fatalf("ToolInput 为空——权威回填失败（治全空场景）")
	}
	if !jsonHasField(t, rec.ToolInput, "file_path", "a.txt") {
		t.Errorf("ToolInput 缺 file_path: %q", rec.ToolInput)
	}
}

// -----------------------------------------------------------------------------
// 40.4-INT-003 (P0, AC #1/#5/#6c): 多个连续工具各自 input 不串（按 call_id 关联）。
// 两个工具都 USER 抢先制造空 inputBuf，逼其依赖按 call_id 的权威回填——若权威 input
// 未按 call_id 关联，当前代码两者都空 → FAIL；关联错误则串味。
// RED: 当前既不读 block["input"]，也无 call_id 关联。
// -----------------------------------------------------------------------------
func TestATDD_40_4_INT_003_SequentialToolsNoCrosstalk(t *testing.T) {
	h := newStreamHarness(t)
	// 工具A：USER 抢先 → 空 inputBuf，仅能靠权威回填
	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtUserToolResult("call_PREV", "prev"))
	h.feed(evtInputDelta(`{"file_path":"a.txt"}`)) // 截断后丢失
	h.feed(evtAssistantToolUse("call_A", "Read", map[string]any{"file_path": "a.txt"}))
	// 工具B：同样 USER 抢先
	h.feed(evtStarted("Bash", "call_B"))
	h.feed(evtUserToolResult("call_A", "contents of a"))
	h.feed(evtInputDelta(`{"command":"ls -la"}`)) // 截断后丢失
	h.feed(evtAssistantToolUse("call_B", "Bash", map[string]any{"command": "ls -la"}))
	h.feed(evtUserToolResult("call_B", "dir listing"))

	recs := h.flushAndReadSteps(t)
	readRec := findStepByToolInputOwner(t, recs, "Read")
	bashRec := findStepByToolInputOwner(t, recs, "Bash")
	if !jsonHasField(t, readRec.ToolInput, "file_path", "a.txt") {
		t.Errorf("Read.ToolInput 串味或落空: %q", readRec.ToolInput)
	}
	if !jsonHasField(t, bashRec.ToolInput, "command", "ls -la") {
		t.Errorf("Bash.ToolInput 串味或落空: %q", bashRec.ToolInput)
	}
	if strings.Contains(readRec.ToolInput, "command") || strings.Contains(bashRec.ToolInput, "file_path") {
		t.Errorf("input 串味: Read=%q Bash=%q", readRec.ToolInput, bashRec.ToolInput)
	}
}

// -----------------------------------------------------------------------------
// 40.4-INT-004 (P0, AC #2/#3/#6d): happy path —— 无交错的正常序列仍正确。
// GREEN-GUARD: 不 skip。修复前此路径已正确（多分片完整拼接），用于实时拦截 flush
// 时序修正对正常路径的破坏。
// -----------------------------------------------------------------------------
func TestATDD_40_4_INT_004_HappyPathStillCorrect(t *testing.T) {
	h := newStreamHarness(t)
	// 正常序列：started → 全部 delta → assistant → user(本轮 tool_result)
	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtInputDelta(`{"file_`))
	h.feed(evtInputDelta(`path":`))
	h.feed(evtInputDelta(`"/tmp/x"}`))
	h.feed(evtAssistantToolUse("call_A", "Read", map[string]any{"file_path": "/tmp/x"}))
	h.feed(evtUserToolResult("call_A", "file contents"))

	recs := h.flushAndReadSteps(t)
	rec := findStepByToolInputOwner(t, recs, "Read")
	if !jsonHasField(t, rec.ToolInput, "file_path", "/tmp/x") {
		t.Errorf("happy path ToolInput 不正确: %q (want file_path=/tmp/x)", rec.ToolInput)
	}
}

// -----------------------------------------------------------------------------
// 40.4-UNIT-005 (P1, AC #1): assistant 三种 content 形态均按 call_id 捕获权威 input。
// RED: 三分支当前只取 name/id，丢弃 block["input"]。
// -----------------------------------------------------------------------------
func TestATDD_40_4_UNIT_005_AssistantThreeContentShapes(t *testing.T) {
	cases := []struct {
		name    string
		content any
	}{
		{
			name:    "slice_of_map",
			content: []map[string]any{{"type": "tool_use", "id": "c1", "name": "Read", "input": map[string]any{"file_path": "m.txt"}}},
		},
		{
			name:    "slice_of_any",
			content: []any{map[string]any{"type": "tool_use", "id": "c1", "name": "Read", "input": map[string]any{"file_path": "m.txt"}}},
		},
		{
			name:    "single_map",
			content: map[string]any{"type": "tool_use", "id": "c1", "name": "Read", "input": map[string]any{"file_path": "m.txt"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newStreamHarness(t)
			h.feed(evtStarted("Read", "c1"))
			// USER 抢先制造空 inputBuf，逼测试只能依赖权威 input 形态解析。
			h.feed(evtUserToolResult("prev", "x"))
			h.feed(map[string]any{"type": "assistant", "role": "assistant", "content": tc.content})

			recs := h.flushAndReadSteps(t)
			rec := findStepByToolInputOwner(t, recs, "Read")
			if !jsonHasField(t, rec.ToolInput, "file_path", "m.txt") {
				t.Errorf("形态 %s: 权威 input 未捕获: %q", tc.name, rec.ToolInput)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 40.4-INT-006 (P1, AC #3): 权威 input 不可用时回退 inputBuf 累积值。
// GREEN-GUARD: 不 skip。当前代码本就用 inputBuf，无交错路径已正确；此用例锁定
// 「权威缺失（assistant 无 input）→ 必须保留 inputBuf 累积值」的回退契约，防止
// 修复（引入权威覆盖）误清空回退路径。
// -----------------------------------------------------------------------------
func TestATDD_40_4_INT_006_FallbackToInputBufWhenNoAuthoritative(t *testing.T) {

	h := newStreamHarness(t)
	// 正常累积完整 input，但 assistant 不携带 input（权威不可用）。
	h.feed(evtStarted("Bash", "call_A"))
	h.feed(evtInputDelta(`{"command":"echo hi"}`))
	h.feed(map[string]any{ // assistant tool_use 但无 input 字段
		"type": "assistant", "role": "assistant",
		"content": []map[string]any{{"type": "tool_use", "id": "call_A", "name": "Bash"}},
	})
	h.feed(evtUserToolResult("call_A", "hi"))

	recs := h.flushAndReadSteps(t)
	rec := findStepByToolInputOwner(t, recs, "Bash")
	if !jsonHasField(t, rec.ToolInput, "command", "echo hi") {
		t.Errorf("回退失败: ToolInput 应回退 inputBuf 累积值, got %q", rec.ToolInput)
	}
}

// -----------------------------------------------------------------------------
// 40.4-INT-007 (P2, AC #5): 单条 assistant 含 ≥2 个 tool_use block，各自 input 不串。
// RED: 同消息多 block 按各自 call_id 存。
// -----------------------------------------------------------------------------
func TestATDD_40_4_INT_007_MultiBlockSingleAssistant(t *testing.T) {
	h := newStreamHarness(t)
	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtUserToolResult("prev", "x")) // 制造 A 空 inputBuf
	// 一条 assistant 同时携带 A、B 两个 tool_use block。
	h.feed(map[string]any{
		"type": "assistant", "role": "assistant",
		"content": []map[string]any{
			{"type": "tool_use", "id": "call_A", "name": "Read", "input": map[string]any{"file_path": "a.txt"}},
			{"type": "tool_use", "id": "call_B", "name": "Bash", "input": map[string]any{"command": "pwd"}},
		},
	})
	h.feed(evtStarted("Bash", "call_B"))
	h.feed(evtUserToolResult("call_A", "done"))

	recs := h.flushAndReadSteps(t)
	readRec := findStepByToolInputOwner(t, recs, "Read")
	if !jsonHasField(t, readRec.ToolInput, "file_path", "a.txt") {
		t.Errorf("多 block: Read 权威 input 未正确关联: %q", readRec.ToolInput)
	}
}
