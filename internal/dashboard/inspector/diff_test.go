package inspector

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// ===== ComputeLineDiff table tests =====

func TestComputeLineDiff(t *testing.T) {
	cases := []struct {
		name       string
		base, cur  []string
		wantAdds   int
		wantDels   int
		wantEquals int
	}{
		{"both empty", nil, nil, 0, 0, 0},
		{"all add", nil, []string{"a", "b"}, 2, 0, 0},
		{"all del", []string{"a", "b"}, nil, 0, 2, 0},
		{"identical", []string{"x", "y"}, []string{"x", "y"}, 0, 0, 2},
		{"middle change",
			[]string{"a", "b", "c"},
			[]string{"a", "B", "c"},
			1, 1, 2,
		},
		{"insert middle",
			[]string{"a", "c"},
			[]string{"a", "b", "c"},
			1, 0, 2,
		},
		{"remove middle",
			[]string{"a", "b", "c"},
			[]string{"a", "c"},
			0, 1, 2,
		},
		{"completely disjoint",
			[]string{"a", "b"},
			[]string{"c", "d"},
			2, 2, 0,
		},
		{"prefix shared",
			[]string{"a", "b", "c"},
			[]string{"a", "b"},
			0, 1, 2,
		},
		{"suffix shared",
			[]string{"a", "b", "c"},
			[]string{"b", "c"},
			0, 1, 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := ComputeLineDiff(tc.base, tc.cur)
			var adds, dels, eqs int
			for _, l := range lines {
				switch l.Kind {
				case DiffAdd:
					adds++
				case DiffDel:
					dels++
				case DiffEqual:
					eqs++
				}
			}
			if adds != tc.wantAdds || dels != tc.wantDels || eqs != tc.wantEquals {
				t.Fatalf("adds=%d dels=%d eqs=%d; want %d/%d/%d",
					adds, dels, eqs, tc.wantAdds, tc.wantDels, tc.wantEquals)
			}
		})
	}
}

// ===== ComputeLineDiff specific output ordering =====

// TestComputeLineDiff_Empty confirms (n=0 && m=0) → nil and not [] —
// caller hot paths short-circuit on nil.
func TestComputeLineDiff_Empty(t *testing.T) {
	got := ComputeLineDiff(nil, nil)
	if got != nil {
		t.Errorf("empty input should return nil; got %v", got)
	}
}

// TestComputeLineDiff_Ordered confirms output preserves top-to-bottom
// ordering (LCS backtrack reverses internally then flips).
func TestComputeLineDiff_Ordered(t *testing.T) {
	base := []string{"a", "b", "c"}
	cur := []string{"a", "X", "c"}
	got := ComputeLineDiff(base, cur)
	if len(got) != 4 {
		t.Fatalf("want 4 lines (a equal, -b, +X, c equal); got %d", len(got))
	}
	if got[0].Kind != DiffEqual || got[0].Text != "a" {
		t.Errorf("got[0]=%+v; want {Equal a}", got[0])
	}
	// Tie-break: del before add (`-b` then `+X`)
	if got[1].Kind != DiffDel || got[1].Text != "b" {
		t.Errorf("got[1]=%+v; want {Del b}", got[1])
	}
	if got[2].Kind != DiffAdd || got[2].Text != "X" {
		t.Errorf("got[2]=%+v; want {Add X}", got[2])
	}
	if got[3].Kind != DiffEqual || got[3].Text != "c" {
		t.Errorf("got[3]=%+v; want {Equal c}", got[3])
	}
}

// ===== RenderDiff fold behaviour =====

func TestRenderDiff_FoldThreshold(t *testing.T) {
	// 2 equal lines (below threshold) → not folded.
	lines := []DiffLine{
		{Kind: DiffEqual, Text: "a"},
		{Kind: DiffEqual, Text: "b"},
		{Kind: DiffAdd, Text: "c"},
	}
	out := RenderDiff(lines, nil, true)
	if strings.Contains(out, "unchanged lines") {
		t.Errorf("run of 2 equal should not fold: %q", out)
	}

	// 3 equal lines (at threshold) → folded.
	lines = []DiffLine{
		{Kind: DiffEqual, Text: "a"},
		{Kind: DiffEqual, Text: "b"},
		{Kind: DiffEqual, Text: "c"},
		{Kind: DiffAdd, Text: "d"},
	}
	out = RenderDiff(lines, nil, true)
	if !strings.Contains(out, "... 3 unchanged lines") {
		t.Errorf("run of 3 equal should fold: %q", out)
	}
}

func TestRenderDiff_UnfoldedMapExpands(t *testing.T) {
	lines := []DiffLine{
		{Kind: DiffEqual, Text: "a"},
		{Kind: DiffEqual, Text: "b"},
		{Kind: DiffEqual, Text: "c"},
	}
	folded := RenderDiff(lines, nil, true)
	unfolded := RenderDiff(lines, map[int]bool{0: true}, true)
	if !strings.Contains(folded, "unchanged lines") {
		t.Fatalf("expected folded placeholder, got %q", folded)
	}
	if strings.Contains(unfolded, "unchanged lines") {
		t.Fatalf("expected unfolded output, got %q", unfolded)
	}
	if !strings.Contains(unfolded, " a") {
		t.Fatalf("unfolded output missing `a`; got %q", unfolded)
	}
}

func TestRenderDiff_Empty(t *testing.T) {
	if got := RenderDiff(nil, nil, true); got != "" {
		t.Errorf("RenderDiff(nil) → %q; want empty", got)
	}
	if got := RenderDiff([]DiffLine{}, nil, true); got != "" {
		t.Errorf("RenderDiff([]) → %q; want empty", got)
	}
}

// TestRenderDiff_AddDelPrefixes confirms +/-/space prefixes regardless of
// asciiMode (prefixes are part of the contract; only colour bytes change).
func TestRenderDiff_AddDelPrefixes(t *testing.T) {
	lines := []DiffLine{
		{Kind: DiffAdd, Text: "new"},
		{Kind: DiffDel, Text: "old"},
		{Kind: DiffEqual, Text: "same"},
	}
	out := RenderDiff(lines, nil, true)
	if !strings.Contains(out, "+new") {
		t.Errorf("missing +new prefix: %q", out)
	}
	if !strings.Contains(out, "-old") {
		t.Errorf("missing -old prefix: %q", out)
	}
	if !strings.Contains(out, " same") {
		t.Errorf("missing equal prefix: %q", out)
	}
}

// TestDiffRoundtrip combines ComputeLineDiff + RenderDiff to verify the
// 1:1 pipeline behaviour.
func TestDiffRoundtrip(t *testing.T) {
	base := []string{"hello", "world", "foo", "bar", "baz"}
	cur := []string{"hello", "world", "foo", "BAR", "baz"}
	lines := ComputeLineDiff(base, cur)
	out := RenderDiff(lines, nil, true)
	if !strings.Contains(out, "-bar") {
		t.Errorf("expected -bar; got %q", out)
	}
	if !strings.Contains(out, "+BAR") {
		t.Errorf("expected +BAR; got %q", out)
	}
}

// ===== RenderDiffBasePicker =====

func TestRenderDiffBasePicker_Empty(t *testing.T) {
	if got := RenderDiffBasePicker(nil, 0, 80); got != "" {
		t.Errorf("nil steps → %q; want empty", got)
	}
	if got := RenderDiffBasePicker([]ipc.StepSummaryWire{}, 0, 80); got != "" {
		t.Errorf("[] steps → %q; want empty", got)
	}
}

func TestRenderDiffBasePicker_HighlightsCursor(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}}
	out := RenderDiffBasePicker(steps, 1, 80)
	if !strings.Contains(out, "[#2]") {
		t.Errorf("active item should be wrapped in []: %q", out)
	}
	if !strings.Contains(out, "Pick base") {
		t.Errorf("missing Pick base label: %q", out)
	}
	if !strings.Contains(out, "Enter=select") {
		t.Errorf("missing Enter=select hint: %q", out)
	}
}

// TestRenderDiffBasePicker_CursorClamps verifies negative and out-of-range
// cursor values are clamped (preserves cmd/rnix behaviour contract).
func TestRenderDiffBasePicker_CursorClamps(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}}

	// cursor < 0 → clamped to 0; first step gets [..]
	out := RenderDiffBasePicker(steps, -5, 80)
	if !strings.Contains(out, "[#1]") {
		t.Errorf("cursor=-5 should clamp to 0 → highlight #1; got %q", out)
	}

	// cursor >= len → clamped to len-1
	out = RenderDiffBasePicker(steps, 99, 80)
	if !strings.Contains(out, "[#3]") {
		t.Errorf("cursor=99 should clamp to last → highlight #3; got %q", out)
	}
}

// TestRenderDiffBasePicker_WidthIgnored verifies width is currently a
// no-op parameter (cmd/rnix contract: `_ = width`).
func TestRenderDiffBasePicker_WidthIgnored(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}}
	out80 := RenderDiffBasePicker(steps, 0, 80)
	out200 := RenderDiffBasePicker(steps, 0, 200)
	if out80 != out200 {
		t.Errorf("width should be ignored; got %q vs %q", out80, out200)
	}
}

// ===== FollowLiveTickCmd =====

func TestFollowLiveTickCmd_ReturnsMsg(t *testing.T) {
	cmd := FollowLiveTickCmd(types.PID(42), "uuid-x", 7)
	if cmd == nil {
		t.Fatal("FollowLiveTickCmd returned nil cmd")
	}
	// tea.Tick sleeps for FollowLiveTickInterval; force the inner closure
	// to execute via the returned tea.Msg by invoking cmd() — Bubble Tea
	// returns the time-tick result wrapped, so we accept either path:
	// 1) raw FollowLiveTickMsg if the runtime passed-through
	// 2) wrapped via tea.batchMsg / tea.sequenceMsg path (rare)
	// Most realistic: rely on tea.Tick returning a callable cmd that
	// delivers FollowLiveTickMsg when fired. We do a short-blocking call
	// via select with a 1.5s deadline.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		tickMsg, ok := msg.(FollowLiveTickMsg)
		if !ok {
			t.Fatalf("expected FollowLiveTickMsg; got %T (%+v)", msg, msg)
		}
		if tickMsg.PID != types.PID(42) {
			t.Errorf("PID=%d; want 42", tickMsg.PID)
		}
		if tickMsg.UUID != "uuid-x" {
			t.Errorf("UUID=%q; want uuid-x", tickMsg.UUID)
		}
		if tickMsg.Gen != 7 {
			t.Errorf("Gen=%d; want 7", tickMsg.Gen)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FollowLiveTickCmd did not fire within 2s")
	}
}

// TestFollowLiveTickInterval verifies the cadence constant is the
// documented 800ms (Story 36-6 AC-13).
func TestFollowLiveTickInterval(t *testing.T) {
	if FollowLiveTickInterval != 800*time.Millisecond {
		t.Errorf("FollowLiveTickInterval=%v; want 800ms (Story 36-6 AC-13 spec)", FollowLiveTickInterval)
	}
}

// TestDDWindow verifies the dd inter-tap window is the documented 200ms
// (Story 36-6 AC-3).
func TestDDWindow(t *testing.T) {
	if DDWindow != 200*time.Millisecond {
		t.Errorf("DDWindow=%v; want 200ms (Story 36-6 AC-3 spec)", DDWindow)
	}
}

// TestDiffConstants verifies the DiffFoldThreshold and DiffMaxLines
// constants stay aligned with the documented contract.
func TestDiffConstants(t *testing.T) {
	if DiffFoldThreshold != 3 {
		t.Errorf("DiffFoldThreshold=%d; want 3", DiffFoldThreshold)
	}
	if DiffMaxLines != 5000 {
		t.Errorf("DiffMaxLines=%d; want 5000", DiffMaxLines)
	}
}

// TestDiffKindIotaOrdering verifies DiffEqual=0, DiffAdd=1, DiffDel=2
// (cmd/rnix const aliases depend on this stable iota order).
func TestDiffKindIotaOrdering(t *testing.T) {
	if DiffEqual != 0 {
		t.Errorf("DiffEqual=%d; want 0", DiffEqual)
	}
	if DiffAdd != 1 {
		t.Errorf("DiffAdd=%d; want 1", DiffAdd)
	}
	if DiffDel != 2 {
		t.Errorf("DiffDel=%d; want 2", DiffDel)
	}
}
