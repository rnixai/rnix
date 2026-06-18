// Package trace — render_test.go (Story 38-5 PR11 Step 4(c))
//
// 验证 helpers + Render() + 38-4 waterfall bar 行为契约（与 cmd/rnix.renderTracePane
// 1:1 等价）：
//   - SpanStatusColor / FormatDurationMs / FlattenSpanTree / RenderWaterfallBar
//     / BottomInnerH / AdjustListScroll / AdjustSpanScroll 各 helper 行为；
//   - Render() 关键路径：list (empty/error/data/cursor) + tree (loading/data/waterfall)；
//   - **Story 38-4 AC#5 waterfall bar 显式断言**：固定 20 列宽 / status 颜色路由 /
//     ASCII fallback / dimmed bar (traceTotalMs<=0) / overflow clamp。
//
// 视觉测试用 profile-tolerant 模式（应用 38-3 教训 · ASCII 路径用确定性字符做断言）。
package trace

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// --- Helpers: SpanStatusColor ---

func TestSpanStatusColor_KnownStatuses(t *testing.T) {
	cases := map[string]lipgloss.Color{
		"ok":      lipgloss.Color("42"),
		"error":   lipgloss.Color("196"),
		"timeout": lipgloss.Color("208"),
	}
	for status, want := range cases {
		got := SpanStatusColor(status)
		if got != want {
			t.Errorf("SpanStatusColor(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestSpanStatusColor_UnknownFallback(t *testing.T) {
	got := SpanStatusColor("future-state")
	want := lipgloss.Color("240")
	if got != want {
		t.Errorf("SpanStatusColor(unknown) = %v, want gray (240) fallback", got)
	}
}

// --- Helpers: FormatDurationMs ---

func TestFormatDurationMs_SubMillisecond(t *testing.T) {
	got := FormatDurationMs(0.5) // 500µs
	if !strings.HasSuffix(got, "µs") {
		t.Errorf("ms<1 should format with µs suffix, got %q", got)
	}
}

func TestFormatDurationMs_Milliseconds(t *testing.T) {
	got := FormatDurationMs(250)
	if got != "250ms" {
		t.Errorf("FormatDurationMs(250) = %q, want '250ms'", got)
	}
}

func TestFormatDurationMs_Seconds(t *testing.T) {
	got := FormatDurationMs(2500)
	if got != "2.50s" {
		t.Errorf("FormatDurationMs(2500) = %q, want '2.50s'", got)
	}
}

// --- Helpers: FlattenSpanTree ---

func TestFlattenSpanTree_NilTree(t *testing.T) {
	if got := FlattenSpanTree(nil); got != nil {
		t.Errorf("FlattenSpanTree(nil) = %v, want nil", got)
	}
}

func TestFlattenSpanTree_NilRoot(t *testing.T) {
	tree := &ipc.SpanTreeWire{Root: nil}
	if got := FlattenSpanTree(tree); got != nil {
		t.Errorf("FlattenSpanTree(nil root) = %v, want nil", got)
	}
}

func TestFlattenSpanTree_SingleRoot(t *testing.T) {
	tree := &ipc.SpanTreeWire{Root: &ipc.SpanNodeWire{
		SpanID: "s1", PID: 42, Name: "root", Status: "ok",
	}}
	flat := FlattenSpanTree(tree)
	if len(flat) != 1 {
		t.Fatalf("expected 1 flat node, got %d", len(flat))
	}
	if !flat[0].IsRoot {
		t.Errorf("expected IsRoot=true for root node")
	}
	if flat[0].Depth != 0 {
		t.Errorf("expected Depth=0 for root, got %d", flat[0].Depth)
	}
	if flat[0].PID != types.PID(42) {
		t.Errorf("expected PID=42, got %d", flat[0].PID)
	}
}

func TestFlattenSpanTree_NestedDepthFirst(t *testing.T) {
	tree := &ipc.SpanTreeWire{Root: &ipc.SpanNodeWire{
		SpanID: "root", PID: 1, Name: "root",
		Children: []ipc.SpanNodeWire{
			{SpanID: "c1", PID: 2, Name: "child-1"},
			{SpanID: "c2", PID: 3, Name: "child-2", Children: []ipc.SpanNodeWire{
				{SpanID: "gc1", PID: 4, Name: "grandchild"},
			}},
		},
	}}
	flat := FlattenSpanTree(tree)
	// Expected DFS order: root, c1, c2, gc1
	if len(flat) != 4 {
		t.Fatalf("expected 4 flat nodes, got %d", len(flat))
	}
	want := []string{"root", "c1", "c2", "gc1"}
	for i, w := range want {
		if flat[i].SpanID != w {
			t.Errorf("flat[%d].SpanID = %q, want %q", i, flat[i].SpanID, w)
		}
	}
	// Verify depths
	wantDepths := []int{0, 1, 1, 2}
	for i, d := range wantDepths {
		if flat[i].Depth != d {
			t.Errorf("flat[%d].Depth = %d, want %d", i, flat[i].Depth, d)
		}
	}
}

// --- Helpers: RenderWaterfallBar (Story 38-4 AC#5) ---

func TestRenderWaterfallBar_FixedWidthASCII(t *testing.T) {
	// Story 38-4 AC#5: output width is exactly WaterfallBarWidth runes
	out := RenderWaterfallBar(1000, 300, "ok", true)
	// In NoColor profile lipgloss strips styling → just count runes
	runes := []rune(out)
	if len(runes) != WaterfallBarWidth {
		// In TrueColor profile ANSI codes wrap content → check stripped length
		stripped := stripANSI(out)
		if len([]rune(stripped)) != WaterfallBarWidth {
			t.Errorf("expected %d runes, got %d (stripped %d) %q",
				WaterfallBarWidth, len(runes), len([]rune(stripped)), out)
		}
	}
}

func TestRenderWaterfallBar_TraceTotalZero_AllDim(t *testing.T) {
	// Story 38-4 AC#5: traceTotalMs <= 0 → entirely dim bar
	out := RenderWaterfallBar(0, 500, "ok", true) // ASCII deterministic
	want := strings.Repeat(".", WaterfallBarWidth)
	if !strings.Contains(out, want) {
		t.Errorf("expected all-dim bar %q, got %q", want, out)
	}
}

func TestRenderWaterfallBar_OverflowClamp(t *testing.T) {
	// Story 38-4 AC#5: spanDurMs > traceTotalMs → clamped to WaterfallBarWidth
	out := RenderWaterfallBar(100, 500, "ok", true)
	want := strings.Repeat("#", WaterfallBarWidth) // fully filled, no overflow
	if !strings.Contains(out, want) {
		t.Errorf("expected fully-filled bar (clamped), got %q", out)
	}
}

func TestRenderWaterfallBar_TinyDurationGetsAtLeastOne(t *testing.T) {
	// Story 38-4 AC#5: every non-zero span gets at least 1 cell of presence
	out := RenderWaterfallBar(10000, 1, "ok", true)
	if !strings.Contains(out, "#") {
		t.Errorf("tiny duration should still render at least 1 fill cell, got %q", out)
	}
}

func TestRenderWaterfallBar_ZeroDurationNoFill(t *testing.T) {
	// spanDurMs == 0 → no fill cells (just dim)
	out := RenderWaterfallBar(1000, 0, "ok", true)
	want := strings.Repeat(".", WaterfallBarWidth)
	if !strings.Contains(out, want) {
		t.Errorf("zero-duration span should be all dim, got %q", out)
	}
}

func TestRenderWaterfallBar_ErrorStatusColored(t *testing.T) {
	out := RenderWaterfallBar(1000, 500, "error", true)
	// In NoColor profile, lipgloss strips style → can only check the fill chars
	// In color profile, the bar would have escape codes. Both should contain
	// at least some "#" fill chars.
	if !strings.Contains(out, "#") {
		t.Errorf("error-status bar missing fill chars, got %q", out)
	}
}

// --- Helpers: BottomInnerH ---

func TestBottomInnerH_TypicalHeight(t *testing.T) {
	// height=30 → contentHeight=26 → bottomRightH=13 → return 11
	got := BottomInnerH(30)
	if got != 11 {
		t.Errorf("BottomInnerH(30) = %d, want 11", got)
	}
}

func TestBottomInnerH_TooSmallReturnsAtLeast1(t *testing.T) {
	got := BottomInnerH(0)
	if got < 1 {
		t.Errorf("BottomInnerH(0) = %d, want ≥ 1", got)
	}
}

// --- Helpers: AdjustListScroll / AdjustSpanScroll ---

func TestAdjustListScroll_NilStateNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AdjustListScroll(nil) panicked: %v", r)
		}
	}()
	AdjustListScroll(nil, 10)
}

func TestAdjustListScroll_CursorBeyondVisible(t *testing.T) {
	state := &TraceState{Cursor: 12, ScrollOffset: 0}
	AdjustListScroll(state, 5)
	// visibleLines=5 → if cursor >= offset+5 → offset = cursor-5+1 = 8
	if state.ScrollOffset != 8 {
		t.Errorf("expected ScrollOffset=8, got %d", state.ScrollOffset)
	}
}

func TestAdjustSpanScroll_CursorBeforeOffset(t *testing.T) {
	state := &TraceState{SpanCursor: 2, SpanScrollOffset: 5}
	AdjustSpanScroll(state, 10)
	if state.SpanScrollOffset != 2 {
		t.Errorf("expected SpanScrollOffset to drop to SpanCursor (2), got %d", state.SpanScrollOffset)
	}
}

// --- Render: List View ---

func TestRender_ListEmpty(t *testing.T) {
	state := TraceState{ViewMode: 0}
	got := Render(state, RenderContext{ASCII: true}, 80, 20)
	if !strings.Contains(got, "Traces") {
		t.Errorf("expected 'Traces' header, got %q", got)
	}
	if !strings.Contains(got, "No trace data") {
		t.Errorf("expected empty-state prompt, got %q", got)
	}
}

func TestRender_ListErrorState(t *testing.T) {
	state := TraceState{ViewMode: 0, Err: errSentinel("boom")}
	got := Render(state, RenderContext{ASCII: true}, 80, 20)
	if !strings.Contains(got, "Error:") {
		t.Errorf("expected 'Error:' marker, got %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("expected error message in render, got %q", got)
	}
}

func TestRender_ListShowsSummaries(t *testing.T) {
	state := TraceState{
		ViewMode: 0,
		Summaries: []ipc.TraceSummaryWire{
			{TraceID: "trace-abc", RootSpanName: "root", SpanCount: 5, TotalDurationMs: 1500},
		},
	}
	got := Render(state, RenderContext{ASCII: true}, 80, 20)
	if !strings.Contains(got, "trace-abc") {
		t.Errorf("expected trace id in render, got %q", got)
	}
	if !strings.Contains(got, "root") {
		t.Errorf("expected root span name, got %q", got)
	}
	if !strings.Contains(got, "1.50s") {
		t.Errorf("expected formatted duration '1.50s', got %q", got)
	}
}

func TestRender_ListCursorASCII(t *testing.T) {
	state := TraceState{
		ViewMode: 0,
		Summaries: []ipc.TraceSummaryWire{
			{TraceID: "t1", RootSpanName: "r", SpanCount: 1, TotalDurationMs: 100},
		},
		Cursor: 0,
	}
	got := Render(state, RenderContext{ASCII: true}, 80, 20)
	if !strings.Contains(got, "> ") {
		t.Errorf("ASCII mode cursor should be '> ', got %q", got)
	}
}

// --- Render: Tree View ---

func TestRender_TreeLoadingWhenNil(t *testing.T) {
	state := TraceState{ViewMode: 1, SelectedSpanTree: nil}
	got := Render(state, RenderContext{ASCII: true}, 80, 20)
	if !strings.Contains(got, "loading") {
		t.Errorf("expected 'loading' prompt for nil span tree, got %q", got)
	}
}

func TestRender_TreeShowsHeader(t *testing.T) {
	tree := &ipc.SpanTreeWire{
		Root: &ipc.SpanNodeWire{SpanID: "root", PID: 42, Name: "root", DurationMs: 500, TokensUsed: 100, Status: "ok"},
		Metadata: ipc.TraceMetaWire{
			TotalSpans: 1, TotalDurationMs: 500, TotalTokens: 100,
		},
	}
	state := TraceState{
		ViewMode:         1,
		SelectedTraceID:  "trace-id",
		SelectedSpanTree: tree,
		SpanFlatNodes:    FlattenSpanTree(tree),
	}
	got := Render(state, RenderContext{ASCII: true}, 80, 20)
	if !strings.Contains(got, "Trace:") {
		t.Errorf("expected 'Trace:' header, got %q", got)
	}
	if !strings.Contains(got, "trace-id") {
		t.Errorf("expected trace id in header, got %q", got)
	}
	if !strings.Contains(got, "1 spans") {
		t.Errorf("expected '1 spans' count, got %q", got)
	}
}

func TestRender_TreeShowsWaterfallWhenWideEnough(t *testing.T) {
	// Story 38-4 AC#5: showBar threshold is width >= 80
	tree := &ipc.SpanTreeWire{
		Root: &ipc.SpanNodeWire{SpanID: "s1", PID: 1, Name: "n", DurationMs: 100, Status: "ok"},
		Metadata: ipc.TraceMetaWire{
			TotalSpans: 1, TotalDurationMs: 100,
		},
	}
	state := TraceState{
		ViewMode:         1,
		SelectedTraceID:  "t1",
		SelectedSpanTree: tree,
		SpanFlatNodes:    FlattenSpanTree(tree),
	}

	// width=80 → waterfall should appear
	gotWide := Render(state, RenderContext{ASCII: true}, 80, 20)
	if !strings.Contains(gotWide, "#") {
		t.Errorf("at width=80, waterfall fill chars should appear, got %q", gotWide)
	}

	// width=70 → waterfall should NOT appear (no '#' from waterfall · though
	// ANSI render of non-waterfall content may have other chars)
	gotNarrow := Render(state, RenderContext{ASCII: true}, 70, 20)
	// The narrow version should have fewer total characters. Best heuristic:
	// it should not contain a long run of '#' chars.
	if strings.Contains(gotNarrow, strings.Repeat("#", 5)) {
		t.Errorf("at width=70, waterfall should be hidden, got %q", gotNarrow)
	}
}

// --- Test helpers ---

// errSentinel is a deterministic error type for tests.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// stripANSI strips ANSI escape sequences for length assertions in TrueColor
// profile (lipgloss may emit color codes around our content).
func stripANSI(s string) string {
	var out strings.Builder
	in := false
	for _, r := range s {
		if r == '\x1b' {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
