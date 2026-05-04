// Package timeline — render_test.go (Story 38-5 PR11 Step 4(c) timeline render
// body 第一个 commit · RenderStepFilterBar 行为契约测试)
//
// 测试范围：
//   - 行 1 "Step:" 7 个 action [t/p/a/c/s/r/z] 显示 + filter on/off mark 切换
//   - 行 2 "Events:" 6 个 system event [C/b/x/X/T/i] 显示 + filter on/off mark 切换
//   - filters == nil 行为：所有类型显示为 on
//   - 末尾固定提示 "[*]all  f/Esc:done"
//   - maxW 边界：负值 / 零值 / 极大值
//
// **零 cmd/rnix 反向依赖**：本测试只 import internal/dashboard/timeline + stdlib · 不
// 引入 lipgloss profile（默认 NoColor 下 ANSI 自动 strip · 测试通过 strings.Contains
// 检查可见文本而非 ANSI bytes）。
package timeline

import (
	"strings"
	"testing"
)

// TestRenderStepFilterBar_AllOn — filters == nil 时所有 type 显示为 ✓ on（默认全开）。
func TestRenderStepFilterBar_AllOn(t *testing.T) {
	got := RenderStepFilterBar(nil, 200)
	// Row 1
	if !strings.Contains(got, "[t]") || !strings.Contains(got, "[p]") || !strings.Contains(got, "[a]") {
		t.Fatalf("missing step action keys [t/p/a] in: %q", got)
	}
	if !strings.Contains(got, "[c]") || !strings.Contains(got, "[s]") || !strings.Contains(got, "[r]") || !strings.Contains(got, "[z]") {
		t.Fatalf("missing step action keys [c/s/r/z] in: %q", got)
	}
	// Row 2
	if !strings.Contains(got, "[C]") || !strings.Contains(got, "[b]") || !strings.Contains(got, "[x]") {
		t.Fatalf("missing system event keys [C/b/x] in: %q", got)
	}
	if !strings.Contains(got, "[X]") || !strings.Contains(got, "[T]") || !strings.Contains(got, "[i]") {
		t.Fatalf("missing system event keys [X/T/i] in: %q", got)
	}
	// 末尾提示
	if !strings.Contains(got, "[*]all") {
		t.Fatalf("missing [*]all hint in: %q", got)
	}
	if !strings.Contains(got, "f/Esc:done") {
		t.Fatalf("missing f/Esc:done hint in: %q", got)
	}
	// 至少包含 1 个 ✓ mark（filters == nil 时全部 on · profile 无关）
	if !strings.Contains(got, "✓") {
		t.Fatalf("expected at least one ✓ mark with nil filters in: %q", got)
	}
}

// TestRenderStepFilterBar_EmptyMap — filters 是空 map（非 nil） 时所有 type 显示为 · off
// （map 默认零值 false · 与 cmd/rnix 行为一致）。
func TestRenderStepFilterBar_EmptyMap(t *testing.T) {
	got := RenderStepFilterBar(map[string]bool{}, 200)
	// 至少包含 1 个 · off mark
	if !strings.Contains(got, "·") {
		t.Fatalf("expected at least one · off mark with empty map in: %q", got)
	}
	// 仍然包含所有 key（不应被截断）
	for _, key := range []string{"[t]", "[p]", "[a]", "[c]", "[s]", "[r]", "[z]", "[C]", "[b]", "[x]", "[X]", "[T]", "[i]"} {
		if !strings.Contains(got, key) {
			t.Fatalf("missing key %s with empty map in: %q", key, got)
		}
	}
}

// TestRenderStepFilterBar_PartialOn — 部分 type on / 部分 off 时 mark 正确切换。
func TestRenderStepFilterBar_PartialOn(t *testing.T) {
	filters := map[string]bool{
		"tool_call":      true,
		"plan":           false,
		"complete":       true,
		stepEventCompact: true,
		stepEventExit:    false,
	}
	got := RenderStepFilterBar(filters, 200)
	// 同时包含 ✓ 和 ·
	if !strings.Contains(got, "✓") {
		t.Fatalf("expected ✓ mark with partial on in: %q", got)
	}
	if !strings.Contains(got, "·") {
		t.Fatalf("expected · mark with partial off in: %q", got)
	}
}

// TestRenderStepFilterBar_TwoRows — 输出包含一个 \n 把 Step / Events 分两行。
func TestRenderStepFilterBar_TwoRows(t *testing.T) {
	got := RenderStepFilterBar(nil, 200)
	rows := strings.Split(got, "\n")
	if len(rows) < 2 {
		t.Fatalf("expected 2 rows, got %d: %q", len(rows), got)
	}
	if !strings.Contains(rows[0], "Step:") {
		t.Fatalf("row 0 missing 'Step:' header: %q", rows[0])
	}
	if !strings.Contains(rows[1], "Events:") {
		t.Fatalf("row 1 missing 'Events:' header: %q", rows[1])
	}
}

// TestRenderStepFilterBar_TruncateZero — maxW <= 0 → 空字符串（与 TruncateAnsi 边界保护一致）。
func TestRenderStepFilterBar_TruncateZero(t *testing.T) {
	if got := RenderStepFilterBar(nil, 0); got != "" {
		t.Fatalf("expected empty string for maxW=0, got: %q", got)
	}
	if got := RenderStepFilterBar(nil, -10); got != "" {
		t.Fatalf("expected empty string for maxW=-10, got: %q", got)
	}
}

// TestRenderStepFilterBar_AllSystemEventTypes — 所有 6 个 system event 类型 + sys_spawn
// 字符串字面量值与 internal/dashboard/event 包对应常量等价（防御本地复制 drift）。
//
// 等价值列表（grep checkpoint · event 包变更时同步）：
//   - stepEventCompact ↔ event.EventCompact = "compact"
//   - stepEventBudget  ↔ event.EventBudget  = "budget"
//   - stepEventExit    ↔ event.EventExit    = "exit"
//   - stepEventStall   ↔ event.EventStall   = "stall"
//   - stepEventImmune  ↔ event.EventImmune  = "immune"
//   - "sys_spawn"      ↔ literal in cmd/rnix（spec 27-3 落地）
func TestRenderStepFilterBar_LocalConstantsMatchEvent(t *testing.T) {
	if stepEventCompact != "compact" {
		t.Errorf("stepEventCompact drift: %q (expected \"compact\")", stepEventCompact)
	}
	if stepEventBudget != "budget" {
		t.Errorf("stepEventBudget drift: %q (expected \"budget\")", stepEventBudget)
	}
	if stepEventExit != "exit" {
		t.Errorf("stepEventExit drift: %q (expected \"exit\")", stepEventExit)
	}
	if stepEventStall != "stall" {
		t.Errorf("stepEventStall drift: %q (expected \"stall\")", stepEventStall)
	}
	if stepEventImmune != "immune" {
		t.Errorf("stepEventImmune drift: %q (expected \"immune\")", stepEventImmune)
	}
}

// TestRenderStepFilterBar_FilterByLiteralKey — filters map key 用字符串字面量
// （等价于 event.EventCompact 等公开常量）能正确 toggle on/off。
func TestRenderStepFilterBar_FilterByLiteralKey(t *testing.T) {
	// compact off, budget on
	filters := map[string]bool{
		"compact":   false,
		"budget":    true,
		"exit":      true,
		"stall":     true,
		"immune":    true,
		"sys_spawn": true,
	}
	got := RenderStepFilterBar(filters, 300)
	// budget 应有 ✓ · compact 应有 ·
	if !strings.Contains(got, "✓") || !strings.Contains(got, "·") {
		t.Fatalf("expected mixed ✓ and · marks: %q", got)
	}
}
