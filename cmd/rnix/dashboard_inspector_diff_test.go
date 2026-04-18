package main

import (
	"strings"
	"testing"
)

// ===== computeLineDiff table tests =====

func TestComputeLineDiff(t *testing.T) {
	cases := []struct {
		name        string
		base, cur   []string
		wantAdds    int
		wantDels    int
		wantEquals  int
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := computeLineDiff(tc.base, tc.cur)
			var adds, dels, eqs int
			for _, l := range lines {
				switch l.kind {
				case diffAdd:
					adds++
				case diffDel:
					dels++
				case diffEqual:
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

// ===== renderDiff fold behaviour =====

func TestRenderDiff_FoldThreshold(t *testing.T) {
	// 2 equal lines (below threshold) → not folded.
	lines := []diffLine{
		{diffEqual, "a"},
		{diffEqual, "b"},
		{diffAdd, "c"},
	}
	out := renderDiff(lines, nil, true)
	if strings.Contains(out, "unchanged lines") {
		t.Errorf("run of 2 equal should not fold: %q", out)
	}

	// 3 equal lines (at threshold) → folded.
	lines = []diffLine{
		{diffEqual, "a"},
		{diffEqual, "b"},
		{diffEqual, "c"},
		{diffAdd, "d"},
	}
	out = renderDiff(lines, nil, true)
	if !strings.Contains(out, "... 3 unchanged lines") {
		t.Errorf("run of 3 equal should fold: %q", out)
	}
}

func TestRenderDiff_UnfoldedMapExpands(t *testing.T) {
	lines := []diffLine{
		{diffEqual, "a"},
		{diffEqual, "b"},
		{diffEqual, "c"},
	}
	// Folded form has placeholder; unfolded should show raw lines.
	folded := renderDiff(lines, nil, true)
	unfolded := renderDiff(lines, map[int]bool{0: true}, true)
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

// ===== Combined: diff → render sanity =====

func TestDiffRoundtrip(t *testing.T) {
	base := []string{"hello", "world", "foo", "bar", "baz"}
	cur := []string{"hello", "world", "foo", "BAR", "baz"}
	lines := computeLineDiff(base, cur)
	out := renderDiff(lines, nil, true)
	if !strings.Contains(out, "-bar") {
		t.Errorf("expected -bar; got %q", out)
	}
	if !strings.Contains(out, "+BAR") {
		t.Errorf("expected +BAR; got %q", out)
	}
}
