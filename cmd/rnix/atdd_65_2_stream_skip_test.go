// ATDD Story 65.2 — strace/gdb 过滤接线 + AC3 log 侧回归 guard（cmd/rnix 侧）。
//
// 红灯机制：shouldSkipStreamEvent 终版已就位（stream_filter.go 一行 helper），
// RED 经 ui.IsStreamFragment 骨架（恒 false）传导——默认模式跳分片用例移除
// t.Skip 后必 FAIL，dev-story 实现裁决 1 后转 GREEN。
// json/verbose 恒不跳（AC4/AC5 结构性保证）与 FormatLogEntry guard（AC3）为
// green-guard，不 skip。
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

func atdd652Event(syscall string, args map[string]any) types.SyscallEvent {
	return types.SyscallEvent{
		Timestamp: 42 * time.Millisecond,
		PID:       1,
		Syscall:   syscall,
		Args:      args,
	}
}

// UNIT-008 (P0, AC1/AC2/AC7③) — 默认模式：分片跳过、聚合/started 保留。
// RED：ui.IsStreamFragment 骨架恒 false → 分片不被跳过 → FAIL。
func TestATDD_65_2_UNIT008_ShouldSkip_DefaultModeFiltersFragments(t *testing.T) {
	fragments := []types.SyscallEvent{
		atdd652Event("DriverThinking", map[string]any{"type": "thinking", "content": "分片…", "subtype": "delta"}),
		atdd652Event("DriverThinking", map[string]any{"type": "thinking", "content": "frag"}), // API/codex 无 subtype
		atdd652Event("DriverToolCall", map[string]any{"type": "tool_call", "content": "input_delta", "partial_json": `{"f`}),
	}
	for i, ev := range fragments {
		if !shouldSkipStreamEvent(ev, false, false) {
			t.Errorf("fragment[%d] 默认模式应跳过: %+v", i, ev.Args)
		}
	}

	passthrough := []types.SyscallEvent{
		atdd652Event("DriverThinking", map[string]any{"type": "thinking", "content": "started", "subtype": "started"}),
		atdd652Event("DriverThinking", map[string]any{"type": "thinking", "subtype": "aggregate", "content": "全文", "fragments": float64(3), "duration_ms": float64(100)}),
		atdd652Event("DriverToolCall", map[string]any{"type": "tool_call", "subtype": "aggregate", "tool": "fs_read", "input": "{}", "duration_ms": float64(50)}),
		atdd652Event("DriverToolCall", map[string]any{"type": "tool_call", "content": "completed", "tool": "shell"}),
		atdd652Event("Open", map[string]any{"path": "/dev/null"}),
	}
	for i, ev := range passthrough {
		if shouldSkipStreamEvent(ev, false, false) {
			t.Errorf("passthrough[%d] 默认模式不应跳过: %+v", i, ev.Args)
		}
	}
}

// UNIT-009 (P0, AC4/AC5) — green-guard（不 skip）：json / verbose 模式恒不过滤。
// AC5 机器模式红线：--json 逐分片原样输出；AC4：--verbose 显示全部分片。
func TestATDD_65_2_UNIT009_ShouldSkip_JSONAndVerboseNeverFilter(t *testing.T) {
	fragment := atdd652Event("DriverThinking", map[string]any{"type": "thinking", "content": "分片…", "subtype": "delta"})

	if shouldSkipStreamEvent(fragment, true, false) {
		t.Error("json 模式恒不过滤（AC5 红线）")
	}
	if shouldSkipStreamEvent(fragment, false, true) {
		t.Error("verbose 模式恒不过滤（AC4）")
	}
	if shouldSkipStreamEvent(fragment, true, true) {
		t.Error("json+verbose 组合恒不过滤")
	}
}

// UNIT-010 (P1, AC3/AC7④) — green-guard（不 skip）：FormatLogEntry 对块级
// LogThink 条目单行渲染、内容原样、零客户端聚合/拆分（渲染端零聚合不变量）。
// 块级化本身是 65-1 kernel 产物（LogChan 断言见 kernel/atdd_65_1_*_test.go），
// 此处只锁 CLI 渲染层不引入任何聚合逻辑。
func TestATDD_65_2_UNIT010_FormatLogEntry_BlockLevelThink_SingleLine(t *testing.T) {
	// 模拟 65-1 块级 LogThink 条目：80-rune 截断后的摘要内容
	content := strings.Repeat("思", 77) + "..."
	lew := ipc.LogEntryWire{
		TimestampMs: 1234,
		PID:         1,
		Step:        2,
		Category:    "think",
		Content:     content,
	}
	got := FormatLogEntry(nil, lew)

	if strings.Count(got, "\n") != 0 {
		t.Errorf("FormatLogEntry 必须单行渲染, got %q", got)
	}
	if !strings.Contains(got, content) {
		t.Errorf("块级内容必须原样出现（零客户端聚合/拆分）, got %q", got)
	}
	if !strings.Contains(got, "[think]") {
		t.Errorf("expected [think] category label, got %q", got)
	}

	// tool 条目：ToolPath 前缀既有行为不变
	toolLew := ipc.LogEntryWire{TimestampMs: 2000, PID: 1, Category: "tool", Content: "done", ToolPath: "/dev/fs"}
	gotTool := FormatLogEntry(nil, toolLew)
	if !strings.Contains(gotTool, "/dev/fs → done") {
		t.Errorf("LogTool ToolPath 前缀行为不变, got %q", gotTool)
	}

	// 确认 ui.IsStreamFragment 对 LogEntry 渲染路径零参与（编译期引用防误删共享谓词）
	_ = ui.IsStreamFragment
}
