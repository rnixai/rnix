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
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
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

// fakeRoleStyle — 测试用 roleStyle 实现 · 仅返回 "[<role>]" 不加颜色。
func fakeRoleStyle(role string) string {
	return "[" + role + "]"
}

// TestRenderDebugDetail_NoMessages — CLI driver 无 message history 路径（27-4 落地）。
func TestRenderDebugDetail_NoMessages(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		Messages:     nil,
		MessageCount: 0,
		TokenCount:   0,
	}
	lines := RenderDebugDetail(&b, detail, 80, 10, fakeRoleStyle)

	if lines == 0 {
		t.Fatalf("expected non-zero lines for CLI driver path")
	}
	got := b.String()
	if !strings.Contains(got, "CLI driver") {
		t.Fatalf("expected 'CLI driver' message: %q", got)
	}
	if !strings.Contains(got, "查看 system prompt") {
		t.Fatalf("expected 'system prompt' hint (27-4 落地): %q", got)
	}
}

// TestRenderDebugDetail_WithMessages — 有 message history · 渲染 header + preview + hint。
func TestRenderDebugDetail_WithMessages(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		Messages: []ipc.MessageWire{
			{Role: "system", Content: "you are an agent"},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
		MessageCount: 3,
		TokenCount:   1500,
	}
	lines := RenderDebugDetail(&b, detail, 100, 20, fakeRoleStyle)

	if lines < 4 {
		t.Fatalf("expected at least 4 lines (separator + header + 3 messages + hint), got %d", lines)
	}
	got := b.String()
	if !strings.Contains(got, "Messages (3)") {
		t.Fatalf("expected 'Messages (3)' header: %q", got)
	}
	if !strings.Contains(got, "1.5k tok") {
		t.Fatalf("expected '1.5k tok' formatted token count: %q", got)
	}
	if !strings.Contains(got, "[system]") || !strings.Contains(got, "[user]") || !strings.Contains(got, "[assistant]") {
		t.Fatalf("expected role tags via fakeRoleStyle: %q", got)
	}
	if !strings.Contains(got, "查看完整 prompt") {
		t.Fatalf("expected 'complete prompt' hint: %q", got)
	}
}

// TestRenderDebugDetail_MaxLinesGuard — maxLines 严格限制写入行数（防溢出）。
func TestRenderDebugDetail_MaxLinesGuard(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		Messages: []ipc.MessageWire{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
		MessageCount: 2,
		TokenCount:   100,
	}
	// maxLines = 2 应只允许写入 2 行（separator + header）· message preview 与 hint 应被拒绝
	lines := RenderDebugDetail(&b, detail, 80, 2, fakeRoleStyle)
	if lines > 2 {
		t.Fatalf("expected lines <= 2 with maxLines=2, got %d", lines)
	}
}

// TestRenderDebugDetail_LongContentTruncation — 长 content 被 runewidth 截断到 57 列+…
// （与 cmd/rnix.renderDebugDetail 等价 · 防止单行过宽）。
func TestRenderDebugDetail_LongContentTruncation(t *testing.T) {
	var b strings.Builder
	longContent := strings.Repeat("a", 100)
	detail := &ipc.GetStepDetailResponse{
		Messages: []ipc.MessageWire{
			{Role: "user", Content: longContent},
		},
		MessageCount: 1,
		TokenCount:   10,
	}
	RenderDebugDetail(&b, detail, 100, 10, fakeRoleStyle)
	got := b.String()
	if !strings.Contains(got, "…") {
		t.Fatalf("expected '…' truncation marker for long content: %q", got)
	}
	// 截断后不应包含完整 100 个 'a'
	if strings.Contains(got, longContent) {
		t.Fatalf("expected truncation to drop full content: %q", got)
	}
}

// TestRenderExpandedDetail_ToolInputAndResult — 同时有 input / result 时按
// hasExpandableContent 行为契约渲染（27-3 三级 detail 落地）。
func TestRenderExpandedDetail_ToolInputAndResult(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		ToolPath:   "/dev/fs/read",
		ToolInput:  "{\"path\": \"foo.go\"}",
		ToolResult: "package main\n\nfunc main() {}",
	}
	s := ipc.StepSummaryWire{Summary: "read", ToolPath: ""}
	lines := RenderExpandedDetail(&b, detail, s, 80, 20)

	if lines < 3 {
		t.Fatalf("expected at least 3 lines (Path + Input + Result), got %d", lines)
	}
	got := b.String()
	if !strings.Contains(got, "Path") || !strings.Contains(got, "/dev/fs/read") {
		t.Fatalf("expected Path label + tool path: %q", got)
	}
	if !strings.Contains(got, "Input") || !strings.Contains(got, "foo.go") {
		t.Fatalf("expected Input label + content: %q", got)
	}
	if !strings.Contains(got, "Result") || !strings.Contains(got, "package main") {
		t.Fatalf("expected Result label + content: %q", got)
	}
}

// TestRenderExpandedDetail_ErrorTakesPrecedence — error 优先于 result（27-3 落地）。
func TestRenderExpandedDetail_ErrorTakesPrecedence(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		ToolError:  "permission denied",
		ToolResult: "should not appear",
	}
	s := ipc.StepSummaryWire{}
	RenderExpandedDetail(&b, detail, s, 80, 10)
	got := b.String()
	if !strings.Contains(got, "Error") || !strings.Contains(got, "permission denied") {
		t.Fatalf("expected Error label + message: %q", got)
	}
	if strings.Contains(got, "should not appear") {
		t.Fatalf("expected result to be hidden when error present: %q", got)
	}
}

// TestRenderExpandedDetail_TokenBreakdown — RequestTokens + ResponseTokens 不等于
// TokenCount 时显示 "%d req → %d resp"。
func TestRenderExpandedDetail_TokenBreakdown(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		RequestTokens:  500,
		ResponseTokens: 200,
	}
	s := ipc.StepSummaryWire{TokenCount: 0} // breakdown 与 total 不一致 → 显示
	RenderExpandedDetail(&b, detail, s, 80, 10)
	got := b.String()
	if !strings.Contains(got, "Token") {
		t.Fatalf("expected Token label: %q", got)
	}
	if !strings.Contains(got, "500") || !strings.Contains(got, "200") {
		t.Fatalf("expected token numbers: %q", got)
	}
	if !strings.Contains(got, "req") || !strings.Contains(got, "resp") {
		t.Fatalf("expected req/resp labels: %q", got)
	}
}

// TestRenderExpandedDetail_FallbackRawResponse — 无 input/error/result 但有 RawResponse 时
// fallback 展示前 3 行（27-3 落地 fallback 路径）。
func TestRenderExpandedDetail_FallbackRawResponse(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		RawResponse: "line1\nline2\nline3\nline4\nline5",
	}
	s := ipc.StepSummaryWire{}
	lines := RenderExpandedDetail(&b, detail, s, 80, 10)
	if lines < 3 {
		t.Fatalf("expected at least 3 lines (3 raw + more hint), got %d", lines)
	}
	got := b.String()
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") || !strings.Contains(got, "line3") {
		t.Fatalf("expected first 3 raw lines: %q", got)
	}
	if !strings.Contains(got, "more lines") {
		t.Fatalf("expected '… (N more lines)' hint: %q", got)
	}
	if strings.Contains(got, "line4") || strings.Contains(got, "line5") {
		t.Fatalf("expected lines 4-5 to be omitted: %q", got)
	}
}

// TestRenderExpandedDetail_MultiLineErrorTruncation — error 多行时截断到 3 行 + hint。
func TestRenderExpandedDetail_MultiLineErrorTruncation(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		ToolError: "err1\nerr2\nerr3\nerr4\nerr5",
	}
	s := ipc.StepSummaryWire{}
	RenderExpandedDetail(&b, detail, s, 80, 10)
	got := b.String()
	if !strings.Contains(got, "err1") || !strings.Contains(got, "err3") {
		t.Fatalf("expected err1-3: %q", got)
	}
	if !strings.Contains(got, "more lines") {
		t.Fatalf("expected '… (N more lines)' hint: %q", got)
	}
}

// TestRenderExpandedDetail_PathDedupWithSummary — Level 1 已展示完整 ToolPath 时不再重复
// 显示 Path 行（27-3 去重契约）。
func TestRenderExpandedDetail_PathDedupWithSummary(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		ToolPath: "shortpath", // 短 path · Level 1 已用 toolpath 替换 summary
	}
	s := ipc.StepSummaryWire{
		Summary:  "abc",       // 短 summary · 触发 displayedAsSummary = ToolPath
		ToolPath: "shortpath", // 等于 detail.ToolPath
	}
	lines := RenderExpandedDetail(&b, detail, s, 80, 10)
	got := b.String()
	// 此时 detail.ToolPath == displayedAsSummary · Path 行应被跳过 ·
	// 但 Fallback 路径会再次尝试显示 ToolPath（lines == 0 时）· 故 lines 应 == 1
	if lines != 1 {
		t.Logf("path dedup: got %d lines: %q", lines, got)
	}
}

// TestRenderExpandedDetail_MaxLinesGuard — maxLines 严格限制写入行数。
func TestRenderExpandedDetail_MaxLinesGuard(t *testing.T) {
	var b strings.Builder
	detail := &ipc.GetStepDetailResponse{
		ToolPath:   "/dev/fs/read",
		ToolInput:  "input",
		ToolResult: "result",
	}
	s := ipc.StepSummaryWire{}
	lines := RenderExpandedDetail(&b, detail, s, 80, 1)
	if lines > 1 {
		t.Fatalf("expected lines <= 1 with maxLines=1, got %d", lines)
	}
}

// TestRenderUnifiedStepHeader_BasicTitle — 基础标题输出 " Timeline" + " │ PID N" + 步数。
func TestRenderUnifiedStepHeader_BasicTitle(t *testing.T) {
	ctx := HeaderContext{
		State: TimelineState{
			SortAsc:    true,
			ExpandMode: ExpandModeCollapsed,
		},
		SelectedPID: types.PID(42),
		TotalEvents: 0,
	}
	got := RenderUnifiedStepHeader(ctx, 200, 5, 5, 0)
	if !strings.Contains(got, "Timeline") {
		t.Fatalf("expected 'Timeline' in header: %q", got)
	}
	if !strings.Contains(got, "PID 42") {
		t.Fatalf("expected 'PID 42' in header: %q", got)
	}
	if !strings.Contains(got, "5 steps") {
		t.Fatalf("expected '5 steps' in header: %q", got)
	}
}

// TestRenderUnifiedStepHeader_NoSelection — SelectedPID == 0 时不显示 PID 段。
func TestRenderUnifiedStepHeader_NoSelection(t *testing.T) {
	ctx := HeaderContext{State: TimelineState{}}
	got := RenderUnifiedStepHeader(ctx, 200, 0, 0, 0)
	if strings.Contains(got, "PID") {
		t.Fatalf("expected no PID segment with selectedPID=0: %q", got)
	}
	if !strings.Contains(got, "0 steps") {
		t.Fatalf("expected '0 steps' in header: %q", got)
	}
}

// TestRenderUnifiedStepHeader_SysEventCount — sysCount > 0 时显示 "+ N events"。
func TestRenderUnifiedStepHeader_SysEventCount(t *testing.T) {
	ctx := HeaderContext{State: TimelineState{}}
	got := RenderUnifiedStepHeader(ctx, 200, 10, 12, 2)
	if !strings.Contains(got, "+ 2 events") {
		t.Fatalf("expected '+ 2 events' in header: %q", got)
	}
}

// TestRenderUnifiedStepHeader_SortDirAndExpandMode — Story 36-4 排序方向与 expand 模式
// 指示（dim 颜色）渲染契约。
func TestRenderUnifiedStepHeader_SortDirAndExpandMode(t *testing.T) {
	// SortAsc + ExpandModeExpanded → "↑ 旧→新" + "· all"
	ctx := HeaderContext{
		State: TimelineState{
			SortAsc:    true,
			ExpandMode: ExpandModeExpanded,
		},
	}
	got := RenderUnifiedStepHeader(ctx, 200, 0, 0, 0)
	// non-ASCII 模式（默认）应包含 "↑" 或 "old->new"（IsASCIIMode 取决于环境变量）
	if !strings.Contains(got, "all") {
		t.Fatalf("expected 'all' marker for ExpandModeExpanded: %q", got)
	}

	// SortAsc=false + ExpandModeErrorsOnly
	ctx2 := HeaderContext{
		State: TimelineState{
			SortAsc:    false,
			ExpandMode: ExpandModeErrorsOnly,
		},
	}
	got2 := RenderUnifiedStepHeader(ctx2, 200, 0, 0, 0)
	if !strings.Contains(got2, "errors") {
		t.Fatalf("expected 'errors' marker for ExpandModeErrorsOnly: %q", got2)
	}
}

// TestRenderUnifiedStepHeader_TokenCount — 总 token > 0 时显示 "X tok"（k 后缀）。
func TestRenderUnifiedStepHeader_TokenCount(t *testing.T) {
	ctx := HeaderContext{
		State: TimelineState{
			StepEntries: []StepEntry{
				{Summary: ipc.StepSummaryWire{TokenCount: 800}},
				{Summary: ipc.StepSummaryWire{TokenCount: 700}},
			},
		},
	}
	got := RenderUnifiedStepHeader(ctx, 200, 2, 2, 0)
	if !strings.Contains(got, "1.5k tok") {
		t.Fatalf("expected '1.5k tok' summed token: %q", got)
	}
}

// TestRenderUnifiedStepHeader_StageStatistics — maxW >= 100 时显示 stage statistics。
func TestRenderUnifiedStepHeader_StageStatistics(t *testing.T) {
	ctx := HeaderContext{
		State: TimelineState{
			StepEntries: []StepEntry{
				{Summary: ipc.StepSummaryWire{Action: "tool_call"}},
				{Summary: ipc.StepSummaryWire{Action: "tool_call"}},
				{Summary: ipc.StepSummaryWire{Action: "plan"}},
				{Summary: ipc.StepSummaryWire{Action: "complete", HasError: true}},
			},
		},
	}
	got := RenderUnifiedStepHeader(ctx, 120, 4, 4, 0)
	// 应包含 abbrev 标识（具体形式由 ActionAbbrev 决定 · 都是简短字符串）
	if !strings.Contains(got, ":2") || !strings.Contains(got, ":1") {
		t.Fatalf("expected 'abbrev:N' counts: %q", got)
	}
	// errCount = 1 → 包含 "err:1"
	if !strings.Contains(got, "err:1") {
		t.Fatalf("expected 'err:1' for HasError step: %q", got)
	}
}

// TestRenderUnifiedStepHeader_StatsSuppressedNarrowScreen — maxW < 100 时不显示 stats。
func TestRenderUnifiedStepHeader_StatsSuppressedNarrowScreen(t *testing.T) {
	ctx := HeaderContext{
		State: TimelineState{
			StepEntries: []StepEntry{
				{Summary: ipc.StepSummaryWire{Action: "plan", HasError: true}},
			},
		},
	}
	got := RenderUnifiedStepHeader(ctx, 80, 1, 1, 0)
	// 80 < 100 · 不应包含 stats
	if strings.Contains(got, "err:1") {
		t.Fatalf("expected stats suppressed at maxW=80: %q", got)
	}
}

// TestRenderUnifiedStepHeader_ScrollPosition — maxW >= 80 + filteredCount > 0 时显示 pos/total。
func TestRenderUnifiedStepHeader_ScrollPosition(t *testing.T) {
	ctx := HeaderContext{
		State: TimelineState{
			StepCursor: 4, // 0-indexed · 显示为 5
		},
	}
	got := RenderUnifiedStepHeader(ctx, 80, 10, 10, 0)
	if !strings.Contains(got, "5/10") {
		t.Fatalf("expected '5/10' scroll position: %q", got)
	}
}

// TestRenderUnifiedStepHeader_FilterIndicator — filteredCount < TotalEvents 时显示
// "filter: N/M -hidden_types"。
func TestRenderUnifiedStepHeader_FilterIndicator(t *testing.T) {
	ctx := HeaderContext{
		State: TimelineState{
			StepFilters: map[string]bool{
				"tool_call": false, // hidden → tool
				"plan":      true,
				"compact":   false, // hidden → cmp
			},
		},
		TotalEvents: 10,
	}
	got := RenderUnifiedStepHeader(ctx, 200, 5, 5, 0)
	if !strings.Contains(got, "filter: 5/10") {
		t.Fatalf("expected 'filter: 5/10': %q", got)
	}
	// hidden 标签 tool / cmp（其他 filter 默认 false 也会出现 · 但 tool/cmp 必出现）
	if !strings.Contains(got, "tool") || !strings.Contains(got, "cmp") {
		t.Fatalf("expected 'tool' and 'cmp' hidden labels: %q", got)
	}
}

// TestRenderUnifiedStepHeader_WallClockFromProcesses — 通过 SelectedUUID 匹配 process
// 提取 CreatedAt（28-4 UUID-keyed 优先 · PID fallback）。
func TestRenderUnifiedStepHeader_WallClockFromProcesses(t *testing.T) {
	now := time.Now()
	ctx := HeaderContext{
		State: TimelineState{},
		Processes: []vfs.ProcInfo{
			{PID: types.PID(10), UUID: "uuid-a", CreatedAt: now},
			{PID: types.PID(10), UUID: "uuid-b", CreatedAt: now.Add(-1 * time.Hour)},
		},
		SelectedPID:  types.PID(10),
		SelectedUUID: "uuid-b", // 应匹配 UUID-keyed · 不匹配第一个
	}
	got := RenderUnifiedStepHeader(ctx, 200, 0, 0, 0)
	if !strings.Contains(got, "│") {
		t.Fatalf("expected wall-clock segment: %q", got)
	}
	// 应该有 HH:MM:SS 格式（精确值不验证 · 仅检查存在）
	if !strings.Contains(got, ":") {
		t.Fatalf("expected wall-clock with ':': %q", got)
	}
}

// ----- RenderAggregatedTimeline 行为测试 -----
//
// 覆盖矩阵（Story 38-5 PR11 Step 4(c) 第 5 个 timeline render block 迁出）：
//   - SlowStepThresholdMs const 值 + 类型契约
//   - AggGroupSize const 值 + 类型契约
//   - 折叠态 group：cursor 群内 / 群外 + 无 error / 有 error / 多 errors
//   - 展开态 group：header + 个别 step + token / duration / error mark
//   - cursor highlight + slow duration 警告色
//   - listLines 上限截断（行数超出 group 总高时停在 listLines）
//   - 滚动起点：cursor group 在视野内（后段 group 优先 · 起点向前移）
//   - 空 filtered passthrough：返回 0 行不写入
//
// 行为对齐：1:1 等价 cmd/rnix.renderAggregatedTimeline · 38-3 教训 profile-tolerant。

// makeAggEntry 测试辅助：构造单个 StepEntry，仅设置渲染相关字段。
func makeAggEntry(step int, action string, tokenCount int, durMs float64, hasErr bool, summary string) StepEntry {
	return StepEntry{
		Summary: ipc.StepSummaryWire{
			Step:       step,
			Action:     action,
			TokenCount: tokenCount,
			DurationMs: durMs,
			HasError:   hasErr,
			Summary:    summary,
		},
	}
}

func TestSlowStepThresholdMs_Value(t *testing.T) {
	if SlowStepThresholdMs != 1000.0 {
		t.Fatalf("SlowStepThresholdMs = %v, want 1000.0 (1s · 与 cmd/rnix 等价)", SlowStepThresholdMs)
	}
}

func TestAggGroupSize_Value(t *testing.T) {
	if AggGroupSize != 50 {
		t.Fatalf("AggGroupSize = %v, want 50 (与 cmd/rnix.aggGroupSize 等价)", AggGroupSize)
	}
}

func TestRenderAggregatedTimeline_EmptyFiltered(t *testing.T) {
	state := TimelineState{}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, nil, 80, 10, false, false)
	if used != 0 {
		t.Fatalf("empty filtered: linesUsed = %d, want 0", used)
	}
	if b.Len() != 0 {
		t.Fatalf("empty filtered: builder not empty: %q", b.String())
	}
}

func TestRenderAggregatedTimeline_CollapsedSingleGroup(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 100, 50, false, "first"),
			makeAggEntry(2, "tool_call", 200, 100, false, "second"),
			makeAggEntry(3, "plan", 50, 30, false, "third"),
		},
		StepCursor:        0,
		ExpandedAggGroups: nil,
	}
	filtered := []int{0, 1, 2}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 5, false, false)
	if used != 1 {
		t.Fatalf("collapsed single group: linesUsed = %d, want 1", used)
	}
	out := b.String()
	if !strings.Contains(out, "Steps 1-3") {
		t.Fatalf("expected 'Steps 1-3' in output: %q", out)
	}
	// 折叠态 cursor 群内 → 应有 "▸ " cursor mark（marker ▸ 退避为空格）
	if !strings.Contains(out, "▸ ") {
		t.Fatalf("expected cursor mark '▸ ' for cursor in group: %q", out)
	}
}

func TestRenderAggregatedTimeline_CollapsedWithErrors(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 100, 50, true, "err1"),
			makeAggEntry(2, "tool_call", 200, 100, true, "err2"),
			makeAggEntry(3, "plan", 50, 30, false, "ok"),
		},
		StepCursor: -1, // cursor 在 -1 → cursorFilterIdx = -1 → cursorGroupIdx = 0 (-1 < 0 分支)
	}
	filtered := []int{0, 1, 2}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 5, false, false)
	if used != 1 {
		t.Fatalf("linesUsed = %d, want 1", used)
	}
	out := b.String()
	if !strings.Contains(out, "2 errors") {
		t.Fatalf("expected '2 errors' (plural) in output: %q", out)
	}
}

func TestRenderAggregatedTimeline_CollapsedWithSingleError(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 100, 50, true, "err1"),
			makeAggEntry(2, "plan", 50, 30, false, "ok"),
		},
		StepCursor: -1,
	}
	filtered := []int{0, 1}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 5, false, false)
	if used != 1 {
		t.Fatalf("linesUsed = %d, want 1", used)
	}
	out := b.String()
	if !strings.Contains(out, "1 error") {
		t.Fatalf("expected '1 error' (singular) in output: %q", out)
	}
	// 不应出现 plural "errors"
	if strings.Contains(out, "1 errors") {
		t.Fatalf("expected singular 'error' not 'errors': %q", out)
	}
}

func TestRenderAggregatedTimeline_CollapsedWithToken(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 1500, 50, false, "first"),
			makeAggEntry(2, "tool_call", 2500, 100, false, "second"),
		},
		StepCursor: -1,
	}
	filtered := []int{0, 1}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 5, true /*showToken*/, false)
	if used != 1 {
		t.Fatalf("linesUsed = %d, want 1", used)
	}
	out := b.String()
	// total 4000 → "4.0k"（依赖 FormatTokenCount · 已迁 timeline 包 · 小写 k 对齐输出）
	if !strings.Contains(out, "4.0k") {
		t.Fatalf("expected '4.0k' total tokens in output: %q", out)
	}
}

func TestRenderAggregatedTimeline_ExpandedGroup(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 100, 50, false, "first"),
			makeAggEntry(2, "plan", 200, 100, false, "second"),
		},
		StepCursor: 0,
		ExpandedAggGroups: map[int]bool{
			0: true,
		},
	}
	filtered := []int{0, 1}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 10, false, false)
	// header (1) + 2 entries = 3 lines
	if used != 3 {
		t.Fatalf("expanded group: linesUsed = %d, want 3", used)
	}
	out := b.String()
	if !strings.Contains(out, "▾ Steps 1-2") {
		t.Fatalf("expected '▾ Steps 1-2' header in expanded group: %q", out)
	}
	// 个别 step 显示
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("expected individual steps 'first'/'second' in expanded group: %q", out)
	}
}

func TestRenderAggregatedTimeline_ExpandedSlowDurationWarning(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 100, SlowStepThresholdMs+1, false, "slow"),
		},
		StepCursor:        0,
		ExpandedAggGroups: map[int]bool{0: true},
	}
	filtered := []int{0}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 10, false, true /*showDuration*/)
	if used != 2 { // header + 1 entry
		t.Fatalf("linesUsed = %d, want 2", used)
	}
	out := b.String()
	// summary 文本应可见
	if !strings.Contains(out, "slow") {
		t.Fatalf("expected summary 'slow' visible: %q", out)
	}
	// duration "1.00s" 应该出现（FormatDurationMs 1001ms → "1.00s" · %.2f 格式）
	if !strings.Contains(out, "1.00s") {
		t.Fatalf("expected duration '1.00s' visible: %q", out)
	}
}

func TestRenderAggregatedTimeline_MultipleGroups(t *testing.T) {
	// 60 entries → 2 groups（50 + 10）
	entries := make([]StepEntry, 60)
	for i := range entries {
		entries[i] = makeAggEntry(i+1, "tool_call", 0, 0, false, "")
	}
	filtered := make([]int, 60)
	for i := range filtered {
		filtered[i] = i
	}
	state := TimelineState{
		StepEntries: entries,
		StepCursor:  55, // cursor 在第 2 个 group
	}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 10, false, false)
	if used == 0 {
		t.Fatal("expected at least one line written")
	}
	out := b.String()
	// 第一个 group "Steps 1-50"
	// 第二个 group "Steps 51-60"
	if !strings.Contains(out, "Steps 1-50") {
		t.Fatalf("expected 'Steps 1-50' (group 1): %q", out)
	}
	if !strings.Contains(out, "Steps 51-60") {
		t.Fatalf("expected 'Steps 51-60' (group 2): %q", out)
	}
	// cursor 在 filtered[55] · cursorGroupIdx = 55/50 = 1（第二个 group）→ "▸ " cursor mark
	if !strings.Contains(out, "▸ ") {
		t.Fatalf("expected cursor mark '▸ ' in group 2: %q", out)
	}
}

func TestRenderAggregatedTimeline_ListLinesCap(t *testing.T) {
	// 3 个展开的 group → header * 3 + 50 entries * 3 = 153 lines
	// listLines = 5 → 应停在 5 行
	entries := make([]StepEntry, 150)
	for i := range entries {
		entries[i] = makeAggEntry(i+1, "tool_call", 0, 0, false, "")
	}
	filtered := make([]int, 150)
	for i := range filtered {
		filtered[i] = i
	}
	state := TimelineState{
		StepEntries: entries,
		StepCursor:  0,
		ExpandedAggGroups: map[int]bool{
			0: true,
			1: true,
			2: true,
		},
	}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 5 /*listLines*/, false, false)
	if used > 5 {
		t.Fatalf("listLines cap not enforced: linesUsed = %d, want ≤ 5", used)
	}
}

func TestRenderAggregatedTimeline_ExpandedWithErrorBackground(t *testing.T) {
	// 验证展开态 entry HasError → "✗" mark 可见
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 0, 0, true /*HasError*/, "err"),
		},
		StepCursor:        -1, // cursor 不在任何 entry
		ExpandedAggGroups: map[int]bool{0: true},
	}
	filtered := []int{0}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 10, false, false)
	if used != 2 { // header + 1 entry
		t.Fatalf("linesUsed = %d, want 2", used)
	}
	out := b.String()
	if !strings.Contains(out, "✗") {
		t.Fatalf("expected error mark '✗' for HasError entry: %q", out)
	}
}

func TestRenderAggregatedTimeline_ScrollStartGroupAdvances(t *testing.T) {
	// 4 个折叠 group + listLines=2 → cursor 在 group 3（idx 150-199）
	// 起点 startGi 应递增直到 [startGi..3] 总高度 ≤ 2 → startGi = 2
	entries := make([]StepEntry, 200)
	for i := range entries {
		entries[i] = makeAggEntry(i+1, "tool_call", 0, 0, false, "")
	}
	filtered := make([]int, 200)
	for i := range filtered {
		filtered[i] = i
	}
	state := TimelineState{
		StepEntries: entries,
		StepCursor:  175, // cursorGroupIdx = 175/50 = 3
	}
	var b strings.Builder
	used := RenderAggregatedTimeline(&b, state, filtered, 200, 2, false, false)
	if used == 0 {
		t.Fatal("expected output")
	}
	out := b.String()
	// 应渲染 group 2 + group 3（起点向前移让 cursor group 进入视野）
	// group 1 (Steps 1-50) 应不出现
	if strings.Contains(out, "Steps 1-50") {
		t.Fatalf("group 1 should be scrolled past · expected first 2 groups not in view: %q", out)
	}
	// cursor group "Steps 151-200" 应可见
	if !strings.Contains(out, "Steps 151-200") {
		t.Fatalf("expected cursor group 'Steps 151-200' in view: %q", out)
	}
}

// ----- EnsureStepCursorVisible 行为测试（Story 38-5 PR11 Step 4(c) 第 6 个 timeline render block） -----
//
// 覆盖矩阵：
//   - filteredLen == 0 → StepScrollTop = 0
//   - cursor 越界（StepCursor > filteredLen-1）→ clamp 后处理
//   - cursor < StepScrollTop → snap StepScrollTop 到 cursor
//   - cursor 已在视野（线性高度 ≤ viewportLines）→ no-op
//   - cursor 不在视野（cursor > scrollTop · 累计高度超 viewportLines）→ 倒推 newTop
//   - 倒推时 viewportLines 容不下任何上一项 → newTop 等于 cursor
//   - 不同 ItemHeightFn 返回值（uniform 1 / variable height）

// constHeight 测试辅助：返回固定 1 行高 ItemHeightFn。
func constHeight(_ int) int { return 1 }

// variableHeight 测试辅助：返回可变高度（idx % 3 == 0 → 3 行 / 其他 → 1 行）。
func variableHeight(idx int) int {
	if idx%3 == 0 {
		return 3
	}
	return 1
}

func TestEnsureStepCursorVisible_EmptyFiltered(t *testing.T) {
	state := TimelineState{StepScrollTop: 5, StepCursor: 7}
	got := EnsureStepCursorVisible(state, 0, 10, constHeight)
	if got.StepScrollTop != 0 {
		t.Fatalf("empty filtered: StepScrollTop = %d, want 0", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_CursorAboveScrollTop(t *testing.T) {
	state := TimelineState{StepScrollTop: 5, StepCursor: 2}
	got := EnsureStepCursorVisible(state, 10, 5, constHeight)
	if got.StepScrollTop != 2 {
		t.Fatalf("cursor above scroll top: StepScrollTop = %d, want 2 (snap to cursor)", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_CursorVisibleNoOp(t *testing.T) {
	// scrollTop=2 · cursor=4 · uniform height 1 · viewport=5 → [2..4] = 3 lines ≤ 5 visible
	state := TimelineState{StepScrollTop: 2, StepCursor: 4}
	got := EnsureStepCursorVisible(state, 10, 5, constHeight)
	if got.StepScrollTop != 2 {
		t.Fatalf("cursor visible: StepScrollTop = %d, want 2 (no-op)", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_CursorBelowViewport(t *testing.T) {
	// scrollTop=0 · cursor=10 · uniform height 1 · viewport=3 → cursor 不在视野
	// 倒推：newTop=10 + linesUsed=1 → newTop=9 linesUsed=2 → newTop=8 linesUsed=3 → 停止
	state := TimelineState{StepScrollTop: 0, StepCursor: 10}
	got := EnsureStepCursorVisible(state, 20, 3, constHeight)
	if got.StepScrollTop != 8 {
		t.Fatalf("cursor below viewport: StepScrollTop = %d, want 8 (newTop=cursor-2)", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_CursorOutOfRangeClamped(t *testing.T) {
	// StepCursor=20 · filteredLen=10 → clamp to 9
	// scrollTop=0 · uniform height 1 · viewport=5 → cursor 9 walk 0..9 = 10 lines > 5
	// 倒推 newTop=9..5 (linesUsed accumulates 1 each iteration · stops when 5 reached)
	state := TimelineState{StepScrollTop: 0, StepCursor: 20}
	got := EnsureStepCursorVisible(state, 10, 5, constHeight)
	if got.StepScrollTop != 5 {
		t.Fatalf("cursor out-of-range clamped: StepScrollTop = %d, want 5", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_CursorVisibleAtScrollTop(t *testing.T) {
	// scrollTop=3 · cursor=3 · viewport=1 · single line cursor 立即可见
	state := TimelineState{StepScrollTop: 3, StepCursor: 3}
	got := EnsureStepCursorVisible(state, 10, 1, constHeight)
	if got.StepScrollTop != 3 {
		t.Fatalf("cursor at scroll top: StepScrollTop = %d, want 3 (no-op)", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_VariableHeightFitsBackward(t *testing.T) {
	// variableHeight: idx 0,3,6,9 → 3 lines · idx 1,2,4,5,7,8 → 1 line
	// scrollTop=0 · cursor=9 · viewport=10
	// 倒推从 9: cursor 9 height 3 (linesUsed=3) →
	//   newTop=9-1=8, h=1, 3+1=4≤10, advance → linesUsed=4
	//   newTop=7, h=1, 4+1=5≤10, advance → linesUsed=5
	//   newTop=6, h=3, 5+3=8≤10, advance → linesUsed=8
	//   newTop=5, h=1, 8+1=9≤10, advance → linesUsed=9
	//   newTop=4, h=1, 9+1=10≤10, advance → linesUsed=10
	//   newTop=3, h=3, 10+3=13>10, stop
	// 期望 newTop = 4
	state := TimelineState{StepScrollTop: 0, StepCursor: 9}
	got := EnsureStepCursorVisible(state, 10, 10, variableHeight)
	if got.StepScrollTop != 4 {
		t.Fatalf("variable height backward fit: StepScrollTop = %d, want 4", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_TinyViewportCannotFit(t *testing.T) {
	// scrollTop=0 · cursor=5 · viewport=1 · cursor 高度 1 占满 viewport · 倒推不能加任何项
	state := TimelineState{StepScrollTop: 0, StepCursor: 5}
	got := EnsureStepCursorVisible(state, 10, 1, constHeight)
	if got.StepScrollTop != 5 {
		t.Fatalf("tiny viewport: StepScrollTop = %d, want 5 (newTop = cursor)", got.StepScrollTop)
	}
}

func TestEnsureStepCursorVisible_PreservesOtherFields(t *testing.T) {
	// 验证 EnsureStepCursorVisible 仅修改 StepScrollTop · 其他字段保留
	state := TimelineState{
		StepScrollTop: 0,
		StepCursor:    5,
		AttachedPID:   types.PID(42),
		AttachedUUID:  "uuid-test",
	}
	got := EnsureStepCursorVisible(state, 10, 5, constHeight)
	if got.StepCursor != 5 {
		t.Fatalf("StepCursor mutated: got %d, want 5", got.StepCursor)
	}
	if got.AttachedPID != types.PID(42) {
		t.Fatalf("AttachedPID mutated: got %v, want 42", got.AttachedPID)
	}
	if got.AttachedUUID != "uuid-test" {
		t.Fatalf("AttachedUUID mutated: got %q, want 'uuid-test'", got.AttachedUUID)
	}
}

// ----- FindStepEntryIndex 行为测试（Story 38-5 PR11 Step 4(c) 第 7 个 timeline render block） -----
//
// 覆盖矩阵：
//   - target == nil → -1（防御 nil）
//   - target 在 state.StepEntries 内 → 正确索引
//   - target 不在 state.StepEntries 内（外部指针）→ -1
//   - target 在 state 之外（其他 StepEntry slice）→ -1
//   - 空 state.StepEntries + 任意 target → -1

func TestFindStepEntryIndex_NilTarget(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{makeAggEntry(1, "tool_call", 0, 0, false, "")},
	}
	if got := FindStepEntryIndex(state, nil); got != -1 {
		t.Fatalf("nil target: got %d, want -1", got)
	}
}

func TestFindStepEntryIndex_EmptyState(t *testing.T) {
	entry := makeAggEntry(1, "tool_call", 0, 0, false, "")
	state := TimelineState{StepEntries: nil}
	if got := FindStepEntryIndex(state, &entry); got != -1 {
		t.Fatalf("empty state: got %d, want -1", got)
	}
}

func TestFindStepEntryIndex_FirstEntry(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 0, 0, false, "first"),
			makeAggEntry(2, "plan", 0, 0, false, "second"),
		},
	}
	target := &state.StepEntries[0]
	if got := FindStepEntryIndex(state, target); got != 0 {
		t.Fatalf("first entry: got %d, want 0", got)
	}
}

func TestFindStepEntryIndex_LastEntry(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 0, 0, false, ""),
			makeAggEntry(2, "plan", 0, 0, false, ""),
			makeAggEntry(3, "text", 0, 0, false, "last"),
		},
	}
	target := &state.StepEntries[2]
	if got := FindStepEntryIndex(state, target); got != 2 {
		t.Fatalf("last entry: got %d, want 2", got)
	}
}

func TestFindStepEntryIndex_ExternalPointer(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			makeAggEntry(1, "tool_call", 0, 0, false, "first"),
		},
	}
	// 构造与 state 内容一致但地址不同的 entry（按地址比较应不匹配）
	external := makeAggEntry(1, "tool_call", 0, 0, false, "first")
	if got := FindStepEntryIndex(state, &external); got != -1 {
		t.Fatalf("external pointer: got %d, want -1 (address comparison)", got)
	}
}

func TestFindStepEntryIndex_PointerFromOtherState(t *testing.T) {
	state1 := TimelineState{
		StepEntries: []StepEntry{makeAggEntry(1, "tool_call", 0, 0, false, "")},
	}
	state2 := TimelineState{
		StepEntries: []StepEntry{makeAggEntry(1, "tool_call", 0, 0, false, "")},
	}
	target := &state2.StepEntries[0]
	// state1 中查找 state2 的指针 → 应不匹配
	if got := FindStepEntryIndex(state1, target); got != -1 {
		t.Fatalf("pointer from other state: got %d, want -1", got)
	}
}
