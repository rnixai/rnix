package kernel

// ATDD red-phase scaffolds for Story 65.1 — kernel 块级聚合事件补发与 LogChan
// 块级化 (SPEC CAP-1 + CAP-2 log 侧).
//
// 目标（story 裁决 1-6）:
//   - thinking 块结束（非 thinking 事件到达 / 新块 started / done / error）时
//     emit 一条 DriverThinking 聚合事件（subtype="aggregate"，content=分片原样
//     串接全文，附 fragments/duration_ms）。
//   - 每次工具调用在 flushTool 时机 emit 一条 DriverToolCall 聚合事件
//     （subtype="aggregate"，携权威 input/result/call_id/tool/step/duration_ms），
//     三条 flush 路径（user(tool_result)/completed/done backstop）全覆盖。
//   - LogChan 块级化：thinking 分片不再逐条 LogThink，每块恰好一条（80 rune
//     截断，经 driverEventToLog 聚合 evt 路径）；input_delta 不再产 LogTool。
//   - 落盘保真红线：原始分片事件（delta/input_delta）在 events.jsonl 中数量与
//     内容不变，聚合事件为纯增量。
//
// 红灯机制（[[atdd-code-story-red-mechanism-preference]]）:
//     RED→GREEN。无需生产骨架——断言只引用既有符号（readEvents/GetLogHistory/
//     driverEventToLog），提交期可编译、make all 全绿。
//   - green-guard 用例（INT-002/011/013 与 UNIT-009 的 started 分支）: 不 skip，
//     实时拦截聚合改造对既有行为（分片保真/OnThinking/started LogTool）的破坏。
//
// 测试级别: INT（复用 atdd_40_4_cli_toolinput_test.go 的 streamStubFile/
// streamHarness 注入模式驱动 setupDriverStreamHandler 闭包 + 读回 events.jsonl
// 与 LogHistory）+ driverEventToLog 纯函数 UNIT。

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// --- 65.1 事件构造器（thinking 分片四类来源形态） ------------------------------

// evt651Think 构造一条带 subtype 的 thinking 分片（claude/qwen: started/delta；
// cursor: subtype 透传自有值）。
func evt651Think(subtype, content string) map[string]any {
	return map[string]any{"type": "thinking", "subtype": subtype, "content": content}
}

// evt651ThinkStarted 对应 claude/qwen 块开始标记（content=="started" 的既有 quirk）。
func evt651ThinkStarted() map[string]any { return evt651Think("started", "started") }

// evt651ThinkNoSubtype 构造无 subtype 键的 thinking 事件（API driver 60.1 归一
// 分片 / codex 消息级全文——两者 Data 均无 subtype）。
func evt651ThinkNoSubtype(content string) map[string]any {
	return map[string]any{"type": "thinking", "content": content}
}

// evt651ToolCompleted 对应 codex/cursor 的 driver 原生 completed flush 事件。
func evt651ToolCompleted(callID, result string) map[string]any {
	m := map[string]any{"type": "tool_call", "content": "completed", "result": result}
	if callID != "" {
		m["call_id"] = callID
	}
	return m
}

// --- 65.1 events.jsonl 断言 helper --------------------------------------------

// args651Subtype 从落盘事件安全取 args.subtype。
func args651Subtype(ev SyscallEventDisk) string {
	if ev.Args == nil {
		return ""
	}
	s, _ := ev.Args["subtype"].(string)
	return s
}

// filter651 返回 syscall 名匹配且 pred 为真的事件子集。
func filter651(evs []SyscallEventDisk, syscall string, pred func(SyscallEventDisk) bool) []SyscallEventDisk {
	var out []SyscallEventDisk
	for _, ev := range evs {
		if ev.Syscall == syscall && (pred == nil || pred(ev)) {
			out = append(out, ev)
		}
	}
	return out
}

// agg651 返回 subtype=="aggregate" 的聚合事件（裁决 1 载体契约）。
func agg651(evs []SyscallEventDisk, syscall string) []SyscallEventDisk {
	return filter651(evs, syscall, func(ev SyscallEventDisk) bool {
		return args651Subtype(ev) == "aggregate"
	})
}

// argStr651 从事件 args 取 string 字段（缺失/类型不符返回 ""）。
func argStr651(ev SyscallEventDisk, key string) string {
	if ev.Args == nil {
		return ""
	}
	s, _ := ev.Args[key].(string)
	return s
}

// logHistory651 加锁读回进程日志历史（AppendLogHistory/GetLogHistory 要求持 p.mu）。
func logHistory651(h *streamHarness) []types.LogEntry {
	h.tk.proc.mu.Lock()
	defer h.tk.proc.mu.Unlock()
	return h.tk.proc.GetLogHistory()
}

// countLog651 统计指定分类的日志条数。
func countLog651(entries []types.LogEntry, cat types.LogCategory) int {
	n := 0
	for _, e := range entries {
		if e.Category == cat {
			n++
		}
	}
	return n
}

// -----------------------------------------------------------------------------
// 65.1-INT-001 (P0, AC #1): claude 形态 thinking 块——started + delta×3 后一条
// 非 thinking 事件（token 级 content 分片）到达即触发块 flush → 恰好一条
// subtype="aggregate" 的 DriverThinking 事件，content=分片原样串接全文（无分隔
// 符），fragments==3，duration_ms 键存在。
// 用 content 分片作触发事件是刻意的：裁决 2 要求块结束检测放在 handler 696-700
// 的 content 早退**之前**，否则纯文本流阶段块悬置到 done 才 flush。
// RED: 当前 handler 无累积器，永远不产 aggregate 事件。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_001_ThinkingBlockAggregateOnNonThinkingEvent(t *testing.T) {

	h := newStreamHarness(t)
	h.feed(evt651ThinkStarted())
	h.feed(evt651Think("delta", "Hel"))
	h.feed(evt651Think("delta", "lo w"))
	h.feed(evt651Think("delta", "orld"))
	// 非 thinking 事件（token 级 content，无 agent_message subtype → 早退路径）
	h.feed(map[string]any{"type": "content", "content": "tok"})

	aggs := agg651(h.tk.readEvents(t), "DriverThinking")
	if len(aggs) != 1 {
		t.Fatalf("DriverThinking aggregate 事件数 = %d, want 1", len(aggs))
	}
	if got := argStr651(aggs[0], "content"); got != "Hello world" {
		t.Errorf("aggregate content = %q, want 分片原样串接 %q", got, "Hello world")
	}
	if frags, ok := aggs[0].Args["fragments"].(float64); !ok || int(frags) != 3 {
		t.Errorf("aggregate fragments = %v, want 3", aggs[0].Args["fragments"])
	}
	if _, ok := aggs[0].Args["duration_ms"]; !ok {
		t.Errorf("aggregate 缺 duration_ms 键: %v", aggs[0].Args)
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-002 (P0, AC #1 边界): 仅 started 无有效分片的块（如 thinking 被立即
// 打断）不产聚合事件。GREEN-GUARD: 不 skip——当前实现不产任何 aggregate 事件即
// 满足；实现后"零有效分片零聚合"仍须成立。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_002_StartedOnlyBlockNoAggregate(t *testing.T) {
	h := newStreamHarness(t)
	h.feed(evt651ThinkStarted())
	h.feed(map[string]any{"type": "done"})

	aggs := agg651(h.tk.readEvents(t), "DriverThinking")
	if len(aggs) != 0 {
		t.Fatalf("仅 started 块不应产聚合事件, got %d 条: %v", len(aggs), aggs)
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-003 (P0, AC #1): 新块 started 断块——两个相邻 thinking 块各自产一条
// 聚合事件，正文互不混入（对齐 60.2 断块规则）。
// RED: 当前不产 aggregate 事件。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_003_NewStartedSplitsBlocks(t *testing.T) {

	h := newStreamHarness(t)
	h.feed(evt651ThinkStarted())
	h.feed(evt651Think("delta", "first"))
	h.feed(evt651Think("delta", " block"))
	h.feed(evt651ThinkStarted()) // 新块边界 → flush 块 1
	h.feed(evt651Think("delta", "second"))
	h.feed(map[string]any{"type": "done"}) // backstop → flush 块 2

	aggs := agg651(h.tk.readEvents(t), "DriverThinking")
	if len(aggs) != 2 {
		t.Fatalf("aggregate 事件数 = %d, want 2（started 断块）", len(aggs))
	}
	if got := argStr651(aggs[0], "content"); got != "first block" {
		t.Errorf("块 1 content = %q, want %q", got, "first block")
	}
	if got := argStr651(aggs[1], "content"); got != "second" {
		t.Errorf("块 2 content = %q, want %q", got, "second")
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-004 (P0, AC #1): done / error backstop——流终止事件 flush 悬置的
// thinking 块。
// RED: 当前 done/error 分支无 thinking flush 兜底。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_004_DoneAndErrorBackstopFlushThinking(t *testing.T) {

	for _, terminal := range []string{"done", "error"} {
		t.Run(terminal, func(t *testing.T) {
			h := newStreamHarness(t)
			h.feed(evt651ThinkStarted())
			h.feed(evt651Think("delta", "tail thought"))
			h.feed(map[string]any{"type": terminal})

			aggs := agg651(h.tk.readEvents(t), "DriverThinking")
			if len(aggs) != 1 {
				t.Fatalf("%s backstop: aggregate 事件数 = %d, want 1", terminal, len(aggs))
			}
			if got := argStr651(aggs[0], "content"); got != "tail thought" {
				t.Errorf("%s backstop: content = %q, want %q", terminal, got, "tail thought")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-005 (P0, AC #2): 工具 user(tool_result) flush 路径（claude/qwen）——
// flushTool 时机 emit 一条 DriverToolCall 聚合事件，携权威 input（authInputs
// 优先于截断的 inputBuf）、result、call_id、tool。
// RED: flushTool 只写 steps.jsonl，不 emit 聚合事件。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_005_ToolAggregateOnUserToolResultFlush(t *testing.T) {

	h := newStreamHarness(t)
	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtInputDelta(`{"file_p`)) // 截断分片——聚合事件须携权威 input
	h.feed(evtAssistantToolUse("call_A", "Read", map[string]any{"file_path": "/etc/hosts"}))
	h.feed(evtUserToolResult("call_A", "file contents"))

	aggs := agg651(h.tk.readEvents(t), "DriverToolCall")
	if len(aggs) != 1 {
		t.Fatalf("DriverToolCall aggregate 事件数 = %d, want 1", len(aggs))
	}
	ev := aggs[0]
	if got := argStr651(ev, "tool"); got != "Read" {
		t.Errorf("aggregate tool = %q, want Read", got)
	}
	if got := argStr651(ev, "call_id"); got != "call_A" {
		t.Errorf("aggregate call_id = %q, want call_A", got)
	}
	if input := argStr651(ev, "input"); !jsonHasField(t, input, "file_path", "/etc/hosts") {
		t.Errorf("aggregate input 非权威回填值: %q", input)
	}
	if got := argStr651(ev, "result"); !strings.Contains(got, "file contents") {
		t.Errorf("aggregate result = %q, want 含 %q", got, "file contents")
	}
	if _, ok := ev.Args["duration_ms"]; !ok {
		t.Errorf("aggregate 缺 duration_ms 键: %v", ev.Args)
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-006 (P0, AC #2): done backstop flush 路径——尾部工具（无 tool_result、
// 无 completed）由 done 兜底 flush，同样产聚合事件（result 允许为空）。
// RED: 同 INT-005。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_006_ToolAggregateOnDoneBackstop(t *testing.T) {

	h := newStreamHarness(t)
	h.feed(evtStarted("Bash", "call_A"))
	h.feed(evtInputDelta(`{"command":"ls"}`))
	h.feed(map[string]any{"type": "done"})

	aggs := agg651(h.tk.readEvents(t), "DriverToolCall")
	if len(aggs) != 1 {
		t.Fatalf("done backstop: aggregate 事件数 = %d, want 1", len(aggs))
	}
	if input := argStr651(aggs[0], "input"); !jsonHasField(t, input, "command", "ls") {
		t.Errorf("aggregate input = %q, want inputBuf 回退值", input)
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-007 (P1, AC #2): driver 原生 completed flush 路径（codex/cursor）——
// 统一契约：completed 事件触发 flushTool 后同样产聚合事件（对下游冗余但一致）。
// RED: 同 INT-005。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_007_ToolAggregateOnCompletedFlush(t *testing.T) {

	h := newStreamHarness(t)
	h.feed(evtStarted("shell", "call_C"))
	h.feed(evtInputDelta(`{"cmd":"pwd"}`))
	h.feed(evt651ToolCompleted("call_C", "/tmp"))

	aggs := agg651(h.tk.readEvents(t), "DriverToolCall")
	if len(aggs) != 1 {
		t.Fatalf("completed: aggregate 事件数 = %d, want 1", len(aggs))
	}
	if got := argStr651(aggs[0], "result"); !strings.Contains(got, "/tmp") {
		t.Errorf("aggregate result = %q, want 含 /tmp", got)
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-008 (P0, AC #4): LogChan 块级化——thinking 分片不再逐条产 LogThink；
// 每个块恰好一条块级 LogThink（80 rune 截断先例，经 driverEventToLog 聚合 evt
// 路径）。
// RED: 当前每 delta 分片一条 LogThink（started 因 content==subtype 被滤）——
// 3 分片产 3 条而非 1 条。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_008_LogThinkBlockLevel(t *testing.T) {

	h := newStreamHarness(t)
	longTail := strings.Repeat("x", 100) // 迫使 80 rune 截断路径被走到
	h.feed(evt651ThinkStarted())
	h.feed(evt651Think("delta", "part1 "))
	h.feed(evt651Think("delta", "part2 "))
	h.feed(evt651Think("delta", longTail))
	h.feed(map[string]any{"type": "done"})

	entries := logHistory651(h)
	if n := countLog651(entries, types.LogThink); n != 1 {
		t.Fatalf("LogThink 条数 = %d, want 恰好 1（块级）", n)
	}
	for _, e := range entries {
		if e.Category != types.LogThink {
			continue
		}
		if !strings.HasPrefix(e.Content, "part1 part2 ") {
			t.Errorf("块级 LogThink 应为全文前缀截断: %q", e.Content)
		}
		if r := []rune(e.Content); len(r) > 83 { // 80 + "..."
			t.Errorf("块级 LogThink 超 80 rune 截断上限: len=%d", len(r))
		}
	}
}

// -----------------------------------------------------------------------------
// 65.1-UNIT-009 (P1, AC #4): driverEventToLog 纯函数修正——tool_call 分支对
// content=="input_delta" 返回 false（输入分片不是日志条目）；started 的 LogTool
// 行为不变（guard 子测试不受 skip 影响单独存在于 INT-010）。
// RED: 当前 input_delta 走 tool_call 分支返回 (LogTool, "input_delta", ..., true)。
// -----------------------------------------------------------------------------
func TestATDD_65_1_UNIT_009_InputDeltaNoLogTool(t *testing.T) {

	_, _, _, ok := driverEventToLog(evtInputDelta(`{"partial`))
	if ok {
		t.Fatal("input_delta 不应产出日志条目, driverEventToLog 返回 ok=true")
	}
}

// -----------------------------------------------------------------------------
// 65.1-UNIT-010 (P1, AC #4): started LogTool 行为不变。GREEN-GUARD: 不 skip——
// 锁定 input_delta 修正不误伤 started 映射（rnix log 工具侧现状 = started 一条）。
// -----------------------------------------------------------------------------
func TestATDD_65_1_UNIT_010_StartedLogToolUnchanged(t *testing.T) {
	cat, content, toolPath, ok := driverEventToLog(evtStarted("Read", "call_A"))
	if !ok {
		t.Fatal("started 应保持产出 LogTool 条目")
	}
	if cat != types.LogTool {
		t.Errorf("category = %q, want %q", cat, types.LogTool)
	}
	if content != "started" {
		t.Errorf("content = %q, want started", content)
	}
	if !strings.Contains(toolPath, "Read") {
		t.Errorf("toolPath = %q, want 含 Read", toolPath)
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-011 (P1, AC #5): 分片来源矩阵——cursor（subtype 透传自有值）、API
// driver（无 subtype、60.1 归一）、codex（消息级全文、无 subtype、多 item 归一
// 块）三形态各自正确聚合。判定谓词必须是 content != subtype（而非
// subtype=="delta"），否则无 subtype 来源全部漏聚合。
// RED: 无累积器。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_011_SourceMatrixAggregation(t *testing.T) {

	t.Run("cursor_subtype_passthrough", func(t *testing.T) {
		h := newStreamHarness(t)
		h.feed(evt651Think("cursor_reasoning", "cur "))
		h.feed(evt651Think("cursor_reasoning", "sor"))
		h.feed(map[string]any{"type": "done"})

		aggs := agg651(h.tk.readEvents(t), "DriverThinking")
		if len(aggs) != 1 || argStr651(aggs[0], "content") != "cur sor" {
			t.Fatalf("cursor 形态聚合失败: %v", aggs)
		}
	})

	t.Run("api_driver_no_subtype", func(t *testing.T) {
		h := newStreamHarness(t)
		h.feed(evt651ThinkNoSubtype("api "))
		h.feed(evt651ThinkNoSubtype("delta"))
		h.feed(map[string]any{"type": "done"})

		aggs := agg651(h.tk.readEvents(t), "DriverThinking")
		if len(aggs) != 1 || argStr651(aggs[0], "content") != "api delta" {
			t.Fatalf("API 无 subtype 形态聚合失败: %v", aggs)
		}
	})

	t.Run("codex_message_level_items", func(t *testing.T) {
		// codex 每 item 一条完整 thinking 事件（消息级、无 subtype），多 item
		// 归一块（词粘连可接受，story 裁决 2）。
		h := newStreamHarness(t)
		h.feed(evt651ThinkNoSubtype("item one."))
		h.feed(evt651ThinkNoSubtype("item two."))
		h.feed(map[string]any{"type": "done"})

		aggs := agg651(h.tk.readEvents(t), "DriverThinking")
		if len(aggs) != 1 {
			t.Fatalf("codex 形态: aggregate 数 = %d, want 1（多 item 归一块）", len(aggs))
		}
		got := argStr651(aggs[0], "content")
		if !strings.Contains(got, "item one.") || !strings.Contains(got, "item two.") {
			t.Errorf("codex 聚合正文缺 item: %q", got)
		}
	})
}

// -----------------------------------------------------------------------------
// 65.1-INT-012 (P1, AC #6): OnThinking 每分片回调不变（60.1 契约）。GREEN-GUARD:
// 不 skip——累积器旁路不得拦截/延迟 OnThinking。started 的 content="started"
// quirk 亦回调（60.1 既有行为，零改动）。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_012_OnThinkingPerFragmentUnchanged(t *testing.T) {
	h := newStreamHarness(t)
	cb := &atdd60Callbacks{}
	h.tk.k.callbacks = cb

	h.feed(evt651ThinkStarted())
	h.feed(evt651Think("delta", "alpha"))
	h.feed(evt651Think("delta", "beta"))
	h.feed(map[string]any{"type": "done"})

	cb.mu.Lock()
	defer cb.mu.Unlock()
	// started("started") + 2 delta = 3 次逐分片回调
	if len(cb.thinkings) != 3 {
		t.Fatalf("OnThinking 调用数 = %d, want 3（每分片一次，含 started quirk）", len(cb.thinkings))
	}
	wants := []string{"started", "alpha", "beta"}
	for i, w := range wants {
		if cb.thinkings[i].Text != w {
			t.Errorf("OnThinking[%d].Text = %q, want %q", i, cb.thinkings[i].Text, w)
		}
	}
}

// -----------------------------------------------------------------------------
// 65.1-INT-013 (P0, AC #3): 落盘保真红线。GREEN-GUARD: 不 skip——原始分片事件
// （DriverThinking started/delta、DriverToolCall started/input_delta）在
// events.jsonl 中数量与内容不变；聚合事件（subtype=aggregate）不计入原始分片。
// 实现后此断言必须继续成立（聚合为纯增量）。
// -----------------------------------------------------------------------------
func TestATDD_65_1_INT_013_FragmentPersistenceUnchanged(t *testing.T) {
	h := newStreamHarness(t)
	h.feed(evt651ThinkStarted())
	h.feed(evt651Think("delta", "d1"))
	h.feed(evt651Think("delta", "d2"))
	h.feed(evt651Think("delta", "d3"))
	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtInputDelta(`{"file_`))
	h.feed(evtInputDelta(`path":"x"}`))
	h.feed(map[string]any{"type": "done"})

	evs := h.tk.readEvents(t)

	rawThinks := filter651(evs, "DriverThinking", func(ev SyscallEventDisk) bool {
		return args651Subtype(ev) != "aggregate"
	})
	if len(rawThinks) != 4 {
		t.Fatalf("原始 DriverThinking 分片数 = %d, want 4（started+3 delta 保真）", len(rawThinks))
	}
	wantContents := []string{"started", "d1", "d2", "d3"}
	for i, w := range wantContents {
		if got := argStr651(rawThinks[i], "content"); got != w {
			t.Errorf("分片[%d] content = %q, want %q（内容保真）", i, got, w)
		}
	}

	rawTools := filter651(evs, "DriverToolCall", func(ev SyscallEventDisk) bool {
		return args651Subtype(ev) != "aggregate"
	})
	if len(rawTools) != 3 {
		t.Fatalf("原始 DriverToolCall 事件数 = %d, want 3（started+2 input_delta 保真）", len(rawTools))
	}
	deltaCount := 0
	for _, ev := range rawTools {
		if argStr651(ev, "content") == "input_delta" {
			deltaCount++
		}
	}
	if deltaCount != 2 {
		t.Errorf("input_delta 分片数 = %d, want 2", deltaCount)
	}
}
