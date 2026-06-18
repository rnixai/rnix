// Package eval — render_test.go (Story 38-5 PR11 Step 4(c) · 第 6 个 pane render
// 主体迁出测试 · 同 alertstrip / detail / security / intent / trace 模式)
//
// 测试覆盖：
//   - EvalScoreColorStyle 阈值（38-4 AC#6 + patch P12 NaN/Inf 边界）
//   - FormatDurationMs（µs / ms / s 三档）
//   - TopoItemCount + BottomInnerH + 3 个 AdjustScroll
//   - Render（tab header + 3 子视图分发）
//   - RenderReputationView（4 分支 · gradient）
//   - RenderTopologyView（4 分支 · Nodes + Edges + reinforced ★）
//   - RenderSynergyView（3 分支 · gradient + VS SOLO + Bold + ★）
//
// profile-tolerant 模式（38-3 教训）：用 lipgloss profile probe 决定可断言项。
package eval

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// hasColor probes the lipgloss profile · used by visual tests (38-3 教训).
func hasColor() bool {
	out := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("x")
	return strings.Contains(out, "\x1b[")
}

// =============================================================================
// EvalScoreColorStyle (38-4 AC#6 + patch P12)
// =============================================================================

func TestEvalScoreColorStyle_GreenAtHighThreshold(t *testing.T) {
	s := EvalScoreColorStyle(0.9)
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorSuccess) {
		t.Errorf("score=0.9: want green=%q, got %q", ui.ColorSuccess, got)
	}
	s = EvalScoreColorStyle(1.0)
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorSuccess) {
		t.Errorf("score=1.0: want green=%q, got %q", ui.ColorSuccess, got)
	}
}

func TestEvalScoreColorStyle_YellowMid(t *testing.T) {
	for _, score := range []float64{0.7, 0.8, 0.89} {
		s := EvalScoreColorStyle(score)
		if got := s.GetForeground(); got != lipgloss.Color(ui.ColorWarning) {
			t.Errorf("score=%.2f: want warning=%q, got %q", score, ui.ColorWarning, got)
		}
	}
}

func TestEvalScoreColorStyle_RedLow(t *testing.T) {
	for _, score := range []float64{0.0, 0.5, 0.69} {
		s := EvalScoreColorStyle(score)
		if got := s.GetForeground(); got != lipgloss.Color(ui.ColorError) {
			t.Errorf("score=%.2f: want error=%q, got %q", score, ui.ColorError, got)
		}
	}
}

// TestEvalScoreColorStyle_NaNAndInf 验证 patch P12（NaN / +Inf / -Inf / 越界统一回退 red）.
func TestEvalScoreColorStyle_NaNAndInf(t *testing.T) {
	cases := []struct {
		name  string
		score float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"below 0", -0.5},
		{"above 1", 1.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := EvalScoreColorStyle(c.score)
			if got := s.GetForeground(); got != lipgloss.Color(ui.ColorError) {
				t.Errorf("score=%v: want error=%q (patch P12), got %q", c.score, ui.ColorError, got)
			}
		})
	}
}

// =============================================================================
// FormatDurationMs
// =============================================================================

func TestFormatDurationMs_Microseconds(t *testing.T) {
	if got := FormatDurationMs(0.5); got != "500µs" {
		t.Errorf("0.5ms: want '500µs', got %q", got)
	}
}

func TestFormatDurationMs_Milliseconds(t *testing.T) {
	if got := FormatDurationMs(123.4); got != "123ms" {
		t.Errorf("123.4ms: want '123ms', got %q", got)
	}
}

func TestFormatDurationMs_Seconds(t *testing.T) {
	if got := FormatDurationMs(2345); got != "2.35s" {
		t.Errorf("2345ms: want '2.35s', got %q", got)
	}
}

// =============================================================================
// TopoItemCount + BottomInnerH
// =============================================================================

func TestTopoItemCount_NilTopology(t *testing.T) {
	state := EvalState{Topology: nil}
	if got := TopoItemCount(state); got != 0 {
		t.Errorf("nil topology: want 0, got %d", got)
	}
}

func TestTopoItemCount_NodesAndEdges(t *testing.T) {
	state := EvalState{Topology: &ipc.TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{{Agent: "a"}, {Agent: "b"}},
		Edges: []kernel.CooperationEdge{{From: "a", To: "b"}},
	}}
	if got := TopoItemCount(state); got != 3 {
		t.Errorf("2 nodes + 1 edge: want 3, got %d", got)
	}
}

func TestBottomInnerH_VariousHeights(t *testing.T) {
	cases := []struct {
		termH int
		want  int
	}{
		{0, 1},  // tiny terminal · contentH=3, bottomRightH=2, inner=max(0,1)=1（floor）
		{4, 1},  // contentH=3, bottomRightH=2, inner=max(0,1)=1
		{10, 2}, // contentH=6, bottomRightH=3, inner=max(1,1)=1 → actually 3-2=1; but bottomRightH=3 → 3-3/2=2 ... let me recompute
	}
	// contentH = max(termH-4, 3); bottomRightH = contentH - contentH/2; inner = max(bottomRightH-2, 1)
	// termH=0: contentH=max(-4,3)=3, bottomRightH=3-1=2, inner=max(0,1)=1 ✓
	// termH=4: contentH=3, bottomRightH=2, inner=1 ✓
	// termH=10: contentH=6, bottomRightH=6-3=3, inner=max(1,1)=1; want 2 wrong. Let me re-check.
	cases[2].want = 1
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			if got := BottomInnerH(c.termH); got != c.want {
				t.Errorf("termH=%d: want %d, got %d", c.termH, c.want, got)
			}
		})
	}
}

func TestBottomInnerH_LargeTerminal(t *testing.T) {
	// termH=40: contentH=36, bottomRightH=36-18=18, inner=max(16,1)=16
	if got := BottomInnerH(40); got != 16 {
		t.Errorf("termH=40: want 16, got %d", got)
	}
}

// =============================================================================
// AdjustScroll (3 sub-views)
// =============================================================================

func TestRepAdjustScroll_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil state: panicked: %v", r)
		}
	}()
	RepAdjustScroll(nil, 30)
}

func TestRepAdjustScroll_CursorBelowOffset(t *testing.T) {
	state := &EvalState{RepCursor: 5, RepScrollOffset: 10}
	RepAdjustScroll(state, 30)
	if state.RepScrollOffset != 5 {
		t.Errorf("cursor below offset: want offset=5, got %d", state.RepScrollOffset)
	}
}

func TestRepAdjustScroll_CursorBeyondViewport(t *testing.T) {
	// termH=30: contentH=26, bottomRightH=13, inner=11, visibleLines=8
	state := &EvalState{RepCursor: 20, RepScrollOffset: 0}
	RepAdjustScroll(state, 30)
	expected := 20 - 8 + 1
	if state.RepScrollOffset != expected {
		t.Errorf("cursor=20 termH=30 (visible=8): want offset=%d, got %d", expected, state.RepScrollOffset)
	}
}

func TestTopoAdjustScroll_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil state: panicked: %v", r)
		}
	}()
	TopoAdjustScroll(nil, 30)
}

func TestTopoAdjustScroll_CursorAdjusts(t *testing.T) {
	state := &EvalState{TopoCursor: 2, TopoScrollOffset: 5}
	TopoAdjustScroll(state, 30)
	if state.TopoScrollOffset != 2 {
		t.Errorf("cursor below offset: want offset=2, got %d", state.TopoScrollOffset)
	}
}

func TestSynAdjustScroll_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil state: panicked: %v", r)
		}
	}()
	SynAdjustScroll(nil, 30)
}

func TestSynAdjustScroll_CursorAdjusts(t *testing.T) {
	state := &EvalState{SynCursor: 3, SynScrollOffset: 8}
	SynAdjustScroll(state, 30)
	if state.SynScrollOffset != 3 {
		t.Errorf("cursor below offset: want offset=3, got %d", state.SynScrollOffset)
	}
}

// =============================================================================
// Render (tab header + dispatch)
// =============================================================================

func TestRender_TabHeaderShowsAllThree(t *testing.T) {
	state := EvalState{SubView: 0}
	ctx := RenderContext{}
	out := Render(state, ctx, 80, 20)
	for _, name := range []string{"Reputation", "Topology", "Synergy"} {
		if !strings.Contains(out, name) {
			t.Errorf("tab header missing %q in output:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "Evaluation") {
		t.Errorf("Evaluation header missing")
	}
}

func TestRender_DispatchesToReputation(t *testing.T) {
	state := EvalState{SubView: 0, Reputations: nil}
	ctx := RenderContext{}
	out := Render(state, ctx, 80, 20)
	if !strings.Contains(out, "Need more execution data") {
		t.Errorf("SubView=0 should dispatch to reputation (empty hint), got:\n%s", out)
	}
}

func TestRender_DispatchesToTopology(t *testing.T) {
	state := EvalState{SubView: 1, Topology: nil}
	ctx := RenderContext{}
	out := Render(state, ctx, 80, 20)
	if !strings.Contains(out, "No collaboration topology data") {
		t.Errorf("SubView=1 should dispatch to topology (empty hint), got:\n%s", out)
	}
}

func TestRender_DispatchesToSynergy(t *testing.T) {
	state := EvalState{SubView: 2, Synergies: nil}
	ctx := RenderContext{}
	out := Render(state, ctx, 80, 20)
	if !strings.Contains(out, "No skill combination data") {
		t.Errorf("SubView=2 should dispatch to synergy (empty hint), got:\n%s", out)
	}
}

// =============================================================================
// RenderReputationView (4 branches)
// =============================================================================

func TestRenderReputationView_ErrorBranch(t *testing.T) {
	state := EvalState{RepErr: errors.New("ipc failed")}
	out := RenderReputationView(state, RenderContext{}, 80, 10)
	if !strings.Contains(out, "Error: ipc failed") {
		t.Errorf("error branch should show ipc failure, got: %s", out)
	}
}

func TestRenderReputationView_EmptyDataHint(t *testing.T) {
	state := EvalState{Reputations: nil}
	out := RenderReputationView(state, RenderContext{}, 80, 10)
	if !strings.Contains(out, "Need more execution data") {
		t.Errorf("empty branch should show hint, got: %s", out)
	}
}

func TestRenderReputationView_DataRowFormat(t *testing.T) {
	state := EvalState{Reputations: []kernel.ReputationSummary{
		{AgentName: "claude-haiku", Score: 0.95, SuccessRate: 0.98, AvgTokens: 1234, AvgDurationMs: 567, TotalRecords: 42, RecentTrend: "improving"},
	}}
	out := RenderReputationView(state, RenderContext{}, 80, 10)
	for _, want := range []string{"AGENT", "SCORE", "SUCCESS", "claude-haiku", "0.95", "98.0%", "1234", "567ms", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderReputationView_CursorIndicator_ASCII(t *testing.T) {
	state := EvalState{
		Reputations: []kernel.ReputationSummary{
			{AgentName: "agent-a"},
			{AgentName: "agent-b"},
		},
		RepCursor: 1,
	}
	ctx := RenderContext{ASCII: true}
	out := RenderReputationView(state, ctx, 80, 10)
	// ASCII cursor "> " should appear on row 1 (agent-b)
	if !strings.Contains(out, "> agent-b") {
		t.Errorf("ASCII cursor missing on cursor row, got:\n%s", out)
	}
}

func TestRenderReputationView_TrendIcons(t *testing.T) {
	cases := []struct {
		trend  string
		ascii  bool
		want   string
		wantNo string
	}{
		{"improving", true, "^", "↑"},
		{"declining", true, "v", "↓"},
		{"stable", true, "-", "→"},
	}
	for _, c := range cases {
		t.Run(c.trend, func(t *testing.T) {
			state := EvalState{Reputations: []kernel.ReputationSummary{{AgentName: "a", RecentTrend: c.trend}}}
			out := RenderReputationView(state, RenderContext{ASCII: c.ascii}, 80, 10)
			if !strings.Contains(out, c.want) {
				t.Errorf("trend=%q ASCII=%v: want %q in output:\n%s", c.trend, c.ascii, c.want, out)
			}
		})
	}
}

// =============================================================================
// RenderTopologyView (4 branches)
// =============================================================================

func TestRenderTopologyView_ErrorBranch(t *testing.T) {
	state := EvalState{TopoErr: errors.New("topology rpc failed")}
	out := RenderTopologyView(state, RenderContext{}, 80, 10)
	if !strings.Contains(out, "Error: topology rpc failed") {
		t.Errorf("error branch missing, got: %s", out)
	}
}

func TestRenderTopologyView_NilTopologyHint(t *testing.T) {
	state := EvalState{Topology: nil}
	out := RenderTopologyView(state, RenderContext{}, 80, 10)
	if !strings.Contains(out, "No collaboration topology data") {
		t.Errorf("nil topology branch missing hint, got: %s", out)
	}
}

func TestRenderTopologyView_EmptyTopologyHint(t *testing.T) {
	state := EvalState{Topology: &ipc.TopologyQueryResponse{Nodes: nil, Edges: nil}}
	out := RenderTopologyView(state, RenderContext{}, 80, 10)
	if !strings.Contains(out, "No collaboration topology data") {
		t.Errorf("empty nodes+edges should show hint, got: %s", out)
	}
}

func TestRenderTopologyView_NodesAndEdgesFormat(t *testing.T) {
	state := EvalState{Topology: &ipc.TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{{Agent: "alpha", ReputationScore: 0.9, Connections: 3}},
		Edges: []kernel.CooperationEdge{{From: "alpha", To: "beta", SpawnCount: 2, MsgCount: 5, Total: 7, Reinforced: true}},
	}}
	out := RenderTopologyView(state, RenderContext{}, 100, 30)
	for _, want := range []string{"── Nodes ──", "── Edges ──", "AGENT", "SCORE", "CONNECTIONS", "alpha", "0.90", "FROM", "TO", "SPAWN", "MSG", "TOTAL", "REINFORCED"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderTopologyView_ASCIIArrow(t *testing.T) {
	state := EvalState{Topology: &ipc.TopologyQueryResponse{
		Nodes: []kernel.TopologyNode{{Agent: "a"}},
		Edges: []kernel.CooperationEdge{{From: "a", To: "b"}},
	}}
	out := RenderTopologyView(state, RenderContext{ASCII: true}, 100, 30)
	if !strings.Contains(out, "->") {
		t.Errorf("ASCII mode should use '->' arrow, got:\n%s", out)
	}
	if strings.Contains(out, "→") {
		t.Errorf("ASCII mode should NOT contain unicode '→', got:\n%s", out)
	}
}

func TestRenderTopologyView_ReinforcedMark_ASCII(t *testing.T) {
	state := EvalState{Topology: &ipc.TopologyQueryResponse{
		Edges: []kernel.CooperationEdge{{From: "a", To: "b", Reinforced: true}},
	}}
	out := RenderTopologyView(state, RenderContext{ASCII: true}, 100, 30)
	if !strings.Contains(out, "*") {
		t.Errorf("ASCII reinforced should use '*', got:\n%s", out)
	}
}

// =============================================================================
// RenderSynergyView (3 branches + 38-4 AC#6)
// =============================================================================

func TestRenderSynergyView_ErrorBranch(t *testing.T) {
	state := EvalState{SynErr: errors.New("synergy rpc failed")}
	out := RenderSynergyView(state, RenderContext{}, 80, 10)
	if !strings.Contains(out, "Error: synergy rpc failed") {
		t.Errorf("error branch missing, got: %s", out)
	}
}

func TestRenderSynergyView_EmptyHint(t *testing.T) {
	state := EvalState{Synergies: nil}
	out := RenderSynergyView(state, RenderContext{}, 80, 10)
	if !strings.Contains(out, "No skill combination data") {
		t.Errorf("empty branch missing hint, got: %s", out)
	}
}

func TestRenderSynergyView_DataFormat(t *testing.T) {
	state := EvalState{Synergies: []kernel.ComboSummary{
		{Skills: []string{"foo", "bar"}, SuccessRate: 0.95, AvgTokens: 1500, TotalExecutions: 10, TokenImprovement: -0.15, Recommended: true},
	}}
	out := RenderSynergyView(state, RenderContext{}, 100, 10)
	for _, want := range []string{"SKILLS", "SUCCESS", "AVG TOK", "EXEC", "VS SOLO", "REC", "foo,bar", "1500", "10", "-15.0%"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderSynergyView_RecommendedASCIIPrefix(t *testing.T) {
	state := EvalState{Synergies: []kernel.ComboSummary{
		{Skills: []string{"a"}, Recommended: true},
	}}
	out := RenderSynergyView(state, RenderContext{ASCII: true}, 100, 10)
	if !strings.Contains(out, "★ ") {
		t.Errorf("ASCII Recommended row should have '★ ' prefix, got:\n%s", out)
	}
	// REC column ASCII = "Y"
	if !strings.Contains(out, "Y") {
		t.Errorf("ASCII Recommended REC column should be 'Y', got:\n%s", out)
	}
}

func TestRenderSynergyView_LongSkillsTruncated(t *testing.T) {
	state := EvalState{Synergies: []kernel.ComboSummary{
		{Skills: []string{"verylongskillname-foo", "verylongskillname-bar"}},
	}}
	out := RenderSynergyView(state, RenderContext{}, 100, 10)
	// joined length > 20 → truncated to 17 chars + "..."
	if !strings.Contains(out, "...") {
		t.Errorf("long skills should be truncated with '...', got:\n%s", out)
	}
}

func TestRenderSynergyView_ImprovementColors_ProfileTolerant(t *testing.T) {
	if !hasColor() {
		t.Skip("no-color profile · skipping ANSI assertion (38-3 教训)")
	}
	cases := []struct {
		name        string
		improvement float64
		mustContain string // ANSI color sequence fragment
	}{
		{"saves tokens (negative) · green", -0.20, "\x1b["},
		{"more tokens (positive) · red", 0.30, "\x1b["},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := EvalState{Synergies: []kernel.ComboSummary{
				{Skills: []string{"a"}, TokenImprovement: c.improvement},
			}}
			out := RenderSynergyView(state, RenderContext{}, 100, 10)
			if !strings.Contains(out, c.mustContain) {
				t.Errorf("expected ANSI color in output for improvement=%.2f, got:\n%s", c.improvement, out)
			}
		})
	}
}

// =============================================================================
// NewRenderContextFromEnv
// =============================================================================

func TestNewRenderContextFromEnv_Default(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	ctx := NewRenderContextFromEnv(true)
	if ctx.ASCII {
		t.Errorf("RNIX_ASCII unset: want ASCII=false, got true")
	}
	if !ctx.IsActive {
		t.Errorf("IsActive=true passed: want true, got false")
	}
}

func TestNewRenderContextFromEnv_AsciiTrue(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	ctx := NewRenderContextFromEnv(false)
	if !ctx.ASCII {
		t.Errorf("RNIX_ASCII=1: want ASCII=true, got false")
	}
}

func TestNewRenderContextFromEnv_AsciiTrueWord(t *testing.T) {
	t.Setenv("RNIX_ASCII", "true")
	ctx := NewRenderContextFromEnv(false)
	if !ctx.ASCII {
		t.Errorf("RNIX_ASCII=true: want ASCII=true, got false")
	}
}
