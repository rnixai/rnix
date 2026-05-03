// Package heatmap — builder_test.go (Story 38-5 PR3 Step 3d)
//
// BuildSegments / SegmentColor / SegmentKindLabel / ActivityLabel / Dim / MapConsumerKind 行为测试。
package heatmap

import (
	"testing"

	"github.com/rnixai/rnix/debug"
)

func TestSegmentKindLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind SegmentKind
		want string
	}{
		{SegSystem, "System Prompt"},
		{SegSkill, "Skill"},
		{SegTool, "Tool Results"},
		{SegUser, "User Messages"},
		{SegAssistant, "Assistant"},
		{SegLeaked, "Leaked"},
		{SegmentKind(99), "Unknown"},
	}
	for _, c := range cases {
		if got := SegmentKindLabel(c.kind); got != c.want {
			t.Errorf("SegmentKindLabel(%d) = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestActivityLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a    ActivityLevel
		want string
	}{
		{ActActive, "Active"},
		{ActWarm, "Warm"},
		{ActCold, "Cold"},
		{ActLeaked, "Leaked"},
		{ActivityLevel(99), "Unknown"},
	}
	for _, c := range cases {
		if got := ActivityLabel(c.a); got != c.want {
			t.Errorf("ActivityLabel(%d) = %q, want %q", c.a, got, c.want)
		}
	}
}

func TestMapConsumerKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  SegmentKind
	}{
		{"system_prompt", SegSystem},
		{"user", SegUser},
		{"assistant", SegAssistant},
		{"tool:read", SegTool},
		{"tool:write", SegTool},
		{"skill", SegSkill},
		{"skill:code-analyst", SegSkill},
		{"unknown_kind", SegAssistant}, // default fallback
	}
	for _, c := range cases {
		if got := MapConsumerKind(c.input); got != c.want {
			t.Errorf("MapConsumerKind(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestDim(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"#5B9BD5", "#3A6B94"},
		{"#9B59B6", "#6A3D7E"},
		{"#6BCB77", "#4A8C52"},
		{"#FFD93D", "#B3982B"},
		{"#888888", "#5E5E5E"},
		{"unknown", "#666666"}, // default fallback
	}
	for _, c := range cases {
		if got := Dim(c.input); got != c.want {
			t.Errorf("Dim(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestSegmentColor(t *testing.T) {
	t.Parallel()
	// Leaked overrides any kind
	if got := SegmentColor(SegSystem, ActLeaked); got != "#FF6B6B" {
		t.Errorf("SegmentColor(System, Leaked) = %q, want #FF6B6B", got)
	}
	if got := SegmentColor(SegLeaked, ActActive); got != "#FF6B6B" {
		t.Errorf("SegmentColor(Leaked, Active) = %q, want #FF6B6B", got)
	}
	// Active state returns base color
	if got := SegmentColor(SegSystem, ActActive); got != "#5B9BD5" {
		t.Errorf("SegmentColor(System, Active) = %q, want #5B9BD5", got)
	}
	// Cold state returns dimmed color
	if got := SegmentColor(SegSystem, ActCold); got != "#3A6B94" {
		t.Errorf("SegmentColor(System, Cold) = %q, want #3A6B94 (dimmed)", got)
	}
}

func TestBuildSegments_Empty(t *testing.T) {
	t.Parallel()
	if got := BuildSegments(nil); got != nil {
		t.Errorf("BuildSegments(nil) = %v, want nil", got)
	}
	empty := &debug.CtxProfileResult{}
	if got := BuildSegments(empty); got != nil {
		t.Errorf("BuildSegments(empty) = %v, want nil", got)
	}
}

func TestBuildSegments_BasicAggregation(t *testing.T) {
	t.Parallel()
	profile := &debug.CtxProfileResult{
		TopConsumers: []debug.ConsumerEntry{
			{Kind: "system_prompt", Tokens: 400, Pct: 40, Rank: 1},
			{Kind: "tool:read", Tokens: 200, Pct: 20, Rank: 2},
			{Kind: "tool:write", Tokens: 200, Pct: 20, Rank: 3}, // merge with tool:read
			{Kind: "user", Tokens: 100, Pct: 10, Rank: 4},
			{Kind: "assistant", Tokens: 100, Pct: 10, Rank: 5},
		},
		Classification: debug.ClassificationResult{
			Active: debug.ClassBucket{Pct: 60},
			Cold:   debug.ClassBucket{Pct: 40},
		},
	}
	segs := BuildSegments(profile)
	if len(segs) == 0 {
		t.Fatal("expected non-empty segments")
	}
	// Sorted by tokens desc
	for i := 1; i < len(segs); i++ {
		if segs[i].Tokens > segs[i-1].Tokens {
			t.Errorf("segs[%d].Tokens=%d > segs[%d].Tokens=%d (not desc)",
				i, segs[i].Tokens, i-1, segs[i-1].Tokens)
		}
	}
	// First segment should be System (largest)
	if segs[0].Kind != SegSystem {
		t.Errorf("segs[0].Kind = %d, want SegSystem", segs[0].Kind)
	}
}

func TestBuildSegments_OtherBucket(t *testing.T) {
	t.Parallel()
	// Small buckets (<3%) should merge into "Other"
	profile := &debug.CtxProfileResult{
		TopConsumers: []debug.ConsumerEntry{
			{Kind: "system_prompt", Tokens: 950, Pct: 95, Rank: 1},
			{Kind: "user", Tokens: 25, Pct: 2.5, Rank: 2},      // <3% → other
			{Kind: "assistant", Tokens: 25, Pct: 2.5, Rank: 3}, // <3% → other
		},
	}
	segs := BuildSegments(profile)
	hasOther := false
	for _, s := range segs {
		if s.Label == "Other" {
			hasOther = true
			if s.Pct < 4.9 || s.Pct > 5.1 {
				t.Errorf("Other.Pct = %.1f, want ~5.0", s.Pct)
			}
		}
	}
	if !hasOther {
		t.Error("expected Other segment for sub-3% buckets")
	}
}

func TestBuildSegments_LeakedDistinct(t *testing.T) {
	t.Parallel()
	profile := &debug.CtxProfileResult{
		TopConsumers: []debug.ConsumerEntry{
			{Kind: "system_prompt", Tokens: 800, Pct: 80, Rank: 1},
		},
		Classification: debug.ClassificationResult{
			Leaked: debug.ClassBucket{Tokens: 200, Pct: 20},
		},
	}
	segs := BuildSegments(profile)
	hasLeaked := false
	for _, s := range segs {
		if s.Kind == SegLeaked {
			hasLeaked = true
			if s.Activity != ActLeaked {
				t.Errorf("Leaked.Activity = %d, want ActLeaked", s.Activity)
			}
		}
	}
	if !hasLeaked {
		t.Error("expected Leaked segment for >3% leaked classification")
	}
}
