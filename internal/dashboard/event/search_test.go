// Package event — search_test.go (Story 38-5 PR11 Step 4(c))
package event

import (
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/ipc"
)

func TestFindMatches_EmptyQuery_ReturnsNil(t *testing.T) {
	events := []UnifiedEvent{{Summary: "anything"}}
	if got := FindMatches(events, ""); got != nil {
		t.Errorf("empty query: got %v, want nil", got)
	}
}

func TestFindMatches_NilEvents_ReturnsNil(t *testing.T) {
	if got := FindMatches(nil, "x"); got != nil {
		t.Errorf("nil events: got %v, want nil", got)
	}
}

func TestFindMatches_NoMatch_ReturnsNil(t *testing.T) {
	events := []UnifiedEvent{
		{Summary: "hello world"},
		{Detail: "foo bar"},
	}
	if got := FindMatches(events, "xyz"); got != nil {
		t.Errorf("no match: got %v, want nil", got)
	}
}

func TestFindMatches_SummaryHit(t *testing.T) {
	events := []UnifiedEvent{
		{Summary: "alpha"},
		{Summary: "beta foo"},
		{Summary: "gamma"},
	}
	got := FindMatches(events, "foo")
	want := []int{1}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindMatches_DetailHit(t *testing.T) {
	events := []UnifiedEvent{
		{Detail: "this is a detail entry"},
		{Detail: "match THIS one"},
	}
	got := FindMatches(events, "match")
	want := []int{1}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindMatches_StepEntryFields(t *testing.T) {
	events := []UnifiedEvent{
		// Hit via Summary.Action
		{StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{Action: "tool_call"}}},
		// Hit via Summary.Summary
		{StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{Summary: "running checks"}}},
		// Hit via Summary.ToolPath
		{StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{ToolPath: "/dev/llm/claude"}}},
		// No hit
		{StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{Action: "plan"}}},
	}
	got := FindMatches(events, "claude")
	want := []int{2}
	if !equal(got, want) {
		t.Errorf("ToolPath hit: got %v, want %v", got, want)
	}

	got = FindMatches(events, "tool_call")
	want = []int{0}
	if !equal(got, want) {
		t.Errorf("Action hit: got %v, want %v", got, want)
	}

	got = FindMatches(events, "checks")
	want = []int{1}
	if !equal(got, want) {
		t.Errorf("Summary hit: got %v, want %v", got, want)
	}
}

func TestFindMatches_CaseInsensitive(t *testing.T) {
	events := []UnifiedEvent{{Summary: "Hello WORLD"}}
	got := FindMatches(events, "WoRlD")
	want := []int{0}
	if !equal(got, want) {
		t.Errorf("case-insensitive: got %v, want %v", got, want)
	}
}

func TestFindMatches_MultipleHits_OrderPreserved(t *testing.T) {
	events := []UnifiedEvent{
		{Summary: "abc foo"},
		{Summary: "xyz"},
		{Detail: "more foo here"},
		{Summary: "FOO bar"},
	}
	got := FindMatches(events, "foo")
	want := []int{0, 2, 3}
	if !equal(got, want) {
		t.Errorf("multi hits: got %v, want %v", got, want)
	}
}

func TestFindMatches_NilStepEntry_OnlySummaryDetailSearched(t *testing.T) {
	events := []UnifiedEvent{
		{Summary: "match", StepEntry: nil},
	}
	got := FindMatches(events, "match")
	want := []int{0}
	if !equal(got, want) {
		t.Errorf("nil StepEntry: got %v, want %v", got, want)
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
