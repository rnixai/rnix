// Package timeline — searchable_test.go (Story 38-5 PR4 Step 4)
//
// SearchableLines + SearchPlugin.Apply(timeline) 行为验证（spec § 04 风险 3 缓解）。
package timeline

import (
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/plugin"
	"github.com/rnixai/rnix/ipc"
)

func TestSearchableLines_Empty(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if got := m.SearchableLines(); got != nil {
		t.Errorf("SearchableLines() on empty state = %v, want nil", got)
	}
}

func TestSearchableLines_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TimelineModel
	if got := m.SearchableLines(); got != nil {
		t.Errorf("nil receiver returned %v, want nil", got)
	}
}

func TestSearchableLines_FormatsStepLines(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", ToolPath: "/dev/fs", Summary: "read main.go"}},
			{Summary: ipc.StepSummaryWire{Step: 2, Action: "plan", Summary: "planning next"}},
			{Summary: ipc.StepSummaryWire{Step: 3, Action: "tool_call", ToolPath: "/dev/shell", Summary: "ls -la"}},
		},
	})
	lines := m.SearchableLines()
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	expected := []string{
		"step1  /dev/fs  read main.go",
		"step2  plan  planning next",
		"step3  /dev/shell  ls -la",
	}
	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

func TestSearchableLines_ToolPathPriority(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		StepEntries: []StepEntry{
			// ToolPath 非空时优先于 Action
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", ToolPath: "/dev/llm", Summary: "ask llm"}},
			// ToolPath 为空时 fallback 到 Action
			{Summary: ipc.StepSummaryWire{Step: 2, Action: "complete", Summary: "done"}},
		},
	})
	lines := m.SearchableLines()
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0] != "step1  /dev/llm  ask llm" {
		t.Errorf("lines[0] = %q, want toolpath priority", lines[0])
	}
	if lines[1] != "step2  complete  done" {
		t.Errorf("lines[1] = %q, want action fallback", lines[1])
	}
}

func TestSearchPlugin_Apply_FindsMatch(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", ToolPath: "/dev/fs", Summary: "read main.go"}},
			{Summary: ipc.StepSummaryWire{Step: 2, Action: "plan", Summary: "compile binary"}},
			{Summary: ipc.StepSummaryWire{Step: 3, Action: "tool_call", ToolPath: "/dev/shell", Summary: "compile.sh"}},
		},
	})
	p := &plugin.SearchPlugin{}
	matches := p.Apply(m, "compile")
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	// 匹配行 1 (step2 compile binary) 和行 2 (step3 compile.sh)
	if matches[0] != 1 || matches[1] != 2 {
		t.Errorf("matches = %v, want [1, 2]", matches)
	}
}

func TestSearchPlugin_Apply_CaseInsensitive(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Summary: "READ FILE"}},
		},
	})
	p := &plugin.SearchPlugin{}
	matches := p.Apply(m, "read")
	if len(matches) != 1 || matches[0] != 0 {
		t.Errorf("case-insensitive match failed: matches=%v", matches)
	}
}

func TestSearchPlugin_Apply_NoMatch_ReturnsNil(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "plan"}},
		},
	})
	p := &plugin.SearchPlugin{}
	if matches := p.Apply(m, "nonexistent"); matches != nil {
		t.Errorf("Apply(no-match) = %v, want nil", matches)
	}
}

func TestSearchPlugin_Apply_EmptyQuery_ReturnsNil(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		StepEntries: []StepEntry{{Summary: ipc.StepSummaryWire{Step: 1, Summary: "anything"}}},
	})
	p := &plugin.SearchPlugin{}
	if matches := p.Apply(m, ""); matches != nil {
		t.Errorf("Apply(empty query) = %v, want nil", matches)
	}
}

func TestSearchableInterface_TimelineModelSatisfies(t *testing.T) {
	t.Parallel()
	// 编译期断言：TimelineModel 必须满足 plugin.Searchable interface
	var _ plugin.Searchable = (*TimelineModel)(nil)
}
