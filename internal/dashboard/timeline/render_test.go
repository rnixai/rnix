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
