// ATDD Story 65.2 — strace/gdb 人类可读默认模式块级渲染（internal/ui 侧）。
//
// 红灯机制（Go 不用 test.skip，按项目偏好用骨架 + t.Skip）：
//   - 生产骨架已就位：stream_filter.go IsStreamFragment 恒 false；
//     FormatTraceLine 尚无 aggregate 特判（聚合事件走通用 traceArgs 路径）。
//   - RED 用例带 t.Skip("RED: 65.2: ...")，dev-story 移除 skip 后必须先 FAIL
//     （RED 有效性验证），实现裁决 1/3 后转 GREEN。
//   - green-guard 用例不 skip，锁既有行为。
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

func atdd652Renderer(colorLevel int) *Renderer {
	InitStyles(TerminalProfile{ColorLevel: colorLevel})
	return &Renderer{
		Profile:    TerminalProfile{Width: 120, ColorLevel: colorLevel, IsUnicode: true},
		OutputMode: ModeDefault,
	}
}

func driverEvent(syscall string, args map[string]any) types.SyscallEvent {
	return types.SyscallEvent{
		Timestamp: 12 * time.Millisecond,
		PID:       1,
		Syscall:   syscall,
		Args:      args,
		Result:    "ok",
		// 65-1 契约：aggregate 事件 SyscallEvent.Duration 恒 0，真实时长在 args duration_ms
		Duration: 0,
	}
}

// UNIT-001 (P0, AC1/AC2/AC7①) — IsStreamFragment 谓词表驱动：镜像 kernel 吸收谓词（裁决 1）。
func TestATDD_65_2_UNIT001_IsStreamFragment_Predicate(t *testing.T) {
	t.Skip("RED: 65.2: IsStreamFragment 为骨架恒 false，dev-story 实现裁决 1 后移除本行")

	cases := []struct {
		name string
		ev   types.SyscallEvent
		want bool
	}{
		// —— 分片（true）：会被 65-1 累积器吸收，必有 aggregate 行补偿 ——
		{"claude/qwen thinking delta", driverEvent("DriverThinking", map[string]any{"type": "thinking", "content": "推理分片…", "subtype": "delta"}), true},
		{"cursor thinking 透传 subtype", driverEvent("DriverThinking", map[string]any{"type": "thinking", "content": "frag", "subtype": "reasoning"}), true},
		{"API driver thinking 无 subtype", driverEvent("DriverThinking", map[string]any{"type": "thinking", "content": "frag"}), true},
		{"codex thinking 消息级无 subtype", driverEvent("DriverThinking", map[string]any{"type": "thinking", "content": "整段消息级思考全文"}), true},
		{"claude input_delta（无 subtype 键）", driverEvent("DriverToolCall", map[string]any{"type": "tool_call", "content": "input_delta", "partial_json": `{"file_p`}), true},
		// —— 非分片（false）：透传 ——
		{"thinking started (content==subtype)", driverEvent("DriverThinking", map[string]any{"type": "thinking", "content": "started", "subtype": "started"}), false},
		{"tool started", driverEvent("DriverToolCall", map[string]any{"type": "tool_call", "content": "started", "tool": "fs_read", "call_id": "c1", "subtype": "started"}), false},
		{"thinking aggregate", driverEvent("DriverThinking", map[string]any{"type": "thinking", "subtype": "aggregate", "content": "全文", "fragments": 14, "duration_ms": 3200}), false},
		{"tool aggregate", driverEvent("DriverToolCall", map[string]any{"type": "tool_call", "subtype": "aggregate", "tool": "fs_read", "input": "{}", "result": "ok", "duration_ms": 1200, "step": 3}), false},
		{"codex/cursor 原生 completed", driverEvent("DriverToolCall", map[string]any{"type": "tool_call", "content": "completed", "tool": "shell"}), false},
		{"thinking content 缺失（零值安全）", driverEvent("DriverThinking", map[string]any{"type": "thinking"}), false},
		{"DriverUser 非流事件", driverEvent("DriverUser", map[string]any{"content": "tool_result 摘要"}), false},
		{"非 Driver syscall (Open)", driverEvent("Open", map[string]any{"path": "/dev/null"}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStreamFragment(tc.ev); got != tc.want {
				t.Errorf("IsStreamFragment(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// UNIT-002 (P0, AC6/AC7②) — thinking 聚合行：content 预览 + fragments + duration 取 args duration_ms。
func TestATDD_65_2_UNIT002_FormatTraceLine_ThinkingAggregate(t *testing.T) {
	t.Skip("RED: 65.2: formatAggregateTrace 特判未实现，dev-story 实现裁决 3 后移除本行")

	r := atdd652Renderer(0)
	ev := driverEvent("DriverThinking", map[string]any{
		"type": "thinking", "subtype": "aggregate",
		"content":     "用户想要重构渲染层，先读 trace.go 锚点",
		"fragments":   float64(14), // IPC 陷阱：线上恒 float64
		"duration_ms": float64(3200),
	})
	got := FormatTraceLine(r, ev, false)

	if strings.Count(got, "\n") != 0 {
		t.Errorf("aggregate 行必须单行, got %q", got)
	}
	if !strings.Contains(got, "DriverThinking") {
		t.Errorf("expected syscall name in output, got %q", got)
	}
	if !strings.Contains(got, "用户想要重构渲染层") {
		t.Errorf("expected content preview, got %q", got)
	}
	if !strings.Contains(got, "14") {
		t.Errorf("expected fragments=14 rendered as integer, got %q", got)
	}
	if !strings.Contains(got, "3.20s") {
		t.Errorf("expected duration 3.20s from args duration_ms (event.Duration 恒 0), got %q", got)
	}
}

// UNIT-003 (P0, AC6/AC7②) — tool 聚合行：tool/path/step + input 160 / result 80 双上限。
func TestATDD_65_2_UNIT003_FormatTraceLine_ToolAggregate(t *testing.T) {
	t.Skip("RED: 65.2: formatAggregateTrace 特判未实现，dev-story 实现裁决 3 后移除本行")

	r := atdd652Renderer(0)
	longInput := strings.Repeat("甲", 170) + "末"   // 171 runes，超 160 上限
	longResult := strings.Repeat("乙", 90) + "尾"   // 91 runes，超 80 上限
	ev := driverEvent("DriverToolCall", map[string]any{
		"type": "tool_call", "subtype": "aggregate",
		"tool": "fs_read", "path": "/dev/fs", "call_id": "c9",
		"input": longInput, "result": longResult,
		"duration_ms": float64(1200), "step": float64(3),
	})
	got := FormatTraceLine(r, ev, false)

	if strings.Count(got, "\n") != 0 {
		t.Errorf("aggregate 行必须单行, got %q", got)
	}
	for _, want := range []string{"DriverToolCall", "fs_read", "/dev/fs", "3", "1.20s"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in tool aggregate line, got %q", want, got)
		}
	}
	// input 截断到 maxAggregatePreviewRunes=160：第 171 rune "末" 不可出现，且有尾标
	if strings.Contains(got, "末") {
		t.Errorf("input 超 160-rune 上限的内容泄漏, got %q", got)
	}
	// result 截断到 80：第 91 rune "尾" 不可出现
	if strings.Contains(got, "尾") {
		t.Errorf("result 超 80-rune 上限的内容泄漏, got %q", got)
	}
	if !strings.Contains(got, "...") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	// 必须比通用 traceArgs 的 50 字符上限宽：input 预览应超过 50 rune
	if !strings.Contains(got, strings.Repeat("甲", 60)) {
		t.Errorf("input 预览应放宽到 160 runes（>50 通用截断）, got %q", got)
	}
}

// UNIT-004 (P0, AC6/AC7②) — 截断边界 159/160/161 runes（rune 计数非 byte，CJK 安全）+ 多行压单行。
func TestATDD_65_2_UNIT004_AggregatePreview_TruncationBoundary(t *testing.T) {
	t.Skip("RED: 65.2: formatAggregateTrace 特判未实现，dev-story 实现裁决 3 后移除本行")

	r := atdd652Renderer(0)
	mk := func(content string) string {
		return FormatTraceLine(r, driverEvent("DriverThinking", map[string]any{
			"type": "thinking", "subtype": "aggregate",
			"content": content, "fragments": float64(2), "duration_ms": float64(100),
		}), false)
	}

	// 159 / 160 runes：不超上限，全文出现、无截断尾标
	for _, n := range []int{159, 160} {
		content := strings.Repeat("思", n-1) + "终"
		got := mk(content)
		if !strings.Contains(got, "终") {
			t.Errorf("%d runes（≤160）不应截断, got %q", n, got)
		}
	}
	// 161 runes：截断，末 rune 不可见 + 尾标
	content161 := strings.Repeat("思", 160) + "溢"
	got := mk(content161)
	if strings.Contains(got, "溢") {
		t.Errorf("161 runes 应截断（rune 计数）, got tail leaked: %q", got)
	}
	if !strings.Contains(got, "...") {
		t.Errorf("161 runes 截断应带尾标, got %q", got)
	}

	// 多行压单行：先压后截（避免截断点落换行）
	multi := "第一段\n\n第二段落内容"
	gotMulti := mk(multi)
	if strings.Count(gotMulti, "\n") != 0 {
		t.Errorf("多行内容必须压成单行, got %q", gotMulti)
	}
	if !strings.Contains(gotMulti, "第一段") || !strings.Contains(gotMulti, "第二段落内容") {
		t.Errorf("压单行后两段内容都应保留, got %q", gotMulti)
	}
}

// UNIT-005 (P1, AC6) — verbose=true 聚合行全文不截断。
func TestATDD_65_2_UNIT005_AggregateVerbose_NoTruncation(t *testing.T) {
	t.Skip("RED: 65.2: formatAggregateTrace 特判未实现，dev-story 实现裁决 3 后移除本行")

	r := atdd652Renderer(0)
	content := strings.Repeat("思", 200) + "终"
	ev := driverEvent("DriverThinking", map[string]any{
		"type": "thinking", "subtype": "aggregate",
		"content": content, "fragments": 5, "duration_ms": 100, // int 形态（单测直构陷阱的另一侧）
	})
	got := FormatTraceLine(r, ev, true)
	if !strings.Contains(got, "终") {
		t.Errorf("verbose 模式聚合行不应截断, got %q", got)
	}
	if !strings.Contains(got, "5") {
		t.Errorf("int 形态 fragments 也应正确渲染, got %q", got)
	}
	// duration 必须取 args duration_ms（event.Duration 恒 0 会渲染 "0µs"）——
	// 该断言保证本用例在通用 traceArgs 路径下真 RED（非假 RED）
	if !strings.Contains(got, "100ms") {
		t.Errorf("verbose 聚合行 duration 应取 args duration_ms=100 → 100ms, got %q", got)
	}
}

// UNIT-006 (P1, AC6) — NoColor（ColorLevel==0）路径无 ANSI；彩色路径不 panic。
func TestATDD_65_2_UNIT006_Aggregate_NoColorBranch(t *testing.T) {
	t.Skip("RED: 65.2: formatAggregateTrace 特判未实现，dev-story 实现裁决 3 后移除本行")

	ev := driverEvent("DriverThinking", map[string]any{
		"type": "thinking", "subtype": "aggregate",
		"content": "简短思考", "fragments": float64(3), "duration_ms": float64(50),
	})

	rNoColor := atdd652Renderer(0)
	got := FormatTraceLine(rNoColor, ev, false)
	if strings.Contains(got, "\x1b[") {
		t.Errorf("NoColor 路径不应输出 ANSI, got %q", got)
	}
	if !strings.Contains(got, "简短思考") {
		t.Errorf("NoColor 路径应含 content, got %q", got)
	}
	// 特判行不以通用 k=v 形态渲染 subtype 元数据（通用路径会输出 subtype="aggregate"）——
	// 该断言保证本用例在通用 traceArgs 路径下真 RED（非假 RED）
	if strings.Contains(got, "subtype=") {
		t.Errorf("聚合特判行不应渲染 subtype= 原始 k=v, got %q", got)
	}

	rColor := atdd652Renderer(3)
	gotColor := FormatTraceLine(rColor, ev, false)
	if !strings.Contains(gotColor, "简短思考") {
		t.Errorf("彩色路径应含 content, got %q", gotColor)
	}
	// 恢复 NoColor styles，避免污染同包其它测试
	InitStyles(TerminalProfile{ColorLevel: 0})
}
