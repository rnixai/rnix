// Package inspector — searchable_test.go (Story 38-5 PR10 Step 4)
//
// InspectorModel + plugin.SearchPlugin 协同行为契约测试 · 与 PR4 TimelineModel 同模式。
package inspector

import (
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/plugin"
)

// --- SearchableLines 基础行为 ---

func TestSearchableLines_NilReceiver(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *InspectorModel
	if got := m.SearchableLines(); got != nil {
		t.Errorf("nil receiver should return nil, got %v", got)
	}
}

func TestSearchableLines_EmptyContents(t *testing.T) {
	m := NewModel()
	// 默认 Contents[Lens] 为 "" → 返回 nil
	if got := m.SearchableLines(); got != nil {
		t.Errorf("empty content should return nil, got %v", got)
	}
}

func TestSearchableLines_OutOfRangeLens(t *testing.T) {
	m := NewModel()
	state := m.State()
	state.Lens = Lens(99) // 越界
	state.Contents[0] = "hello"
	m.SetState(state)
	if got := m.SearchableLines(); got != nil {
		t.Errorf("out-of-range Lens should return nil, got %v", got)
	}
}

func TestSearchableLines_SingleLine(t *testing.T) {
	m := NewModel()
	state := m.State()
	state.Lens = LensConversation
	state.Contents[LensConversation] = "user: hello"
	m.SetState(state)
	got := m.SearchableLines()
	if len(got) != 1 || got[0] != "user: hello" {
		t.Errorf("single line: got %v, want [user: hello]", got)
	}
}

func TestSearchableLines_MultipleLines(t *testing.T) {
	m := NewModel()
	state := m.State()
	state.Lens = LensSystem
	state.Contents[LensSystem] = "line1\nline2\nline3"
	m.SetState(state)
	got := m.SearchableLines()
	if len(got) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(got), got)
	}
	want := []string{"line1", "line2", "line3"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSearchableLines_LensSwitch(t *testing.T) {
	// 切换 lens 后 SearchableLines 返回不同 lens 的内容
	m := NewModel()
	state := m.State()
	state.Contents[LensConversation] = "convo content"
	state.Contents[LensRawJSON] = `{"key":"value"}`
	state.Lens = LensConversation
	m.SetState(state)
	if got := m.SearchableLines(); len(got) != 1 || got[0] != "convo content" {
		t.Errorf("LensConversation: got %v", got)
	}
	state.Lens = LensRawJSON
	m.SetState(state)
	if got := m.SearchableLines(); len(got) != 1 || got[0] != `{"key":"value"}` {
		t.Errorf("LensRawJSON: got %v", got)
	}
}

// --- SearchPlugin.Apply 协同 ---

func TestSearchPlugin_Apply_FindsMatch(t *testing.T) {
	m := NewModel()
	state := m.State()
	state.Lens = LensConversation
	state.Contents[LensConversation] = "line1: hello world\nline2: foo bar\nline3: hello again"
	m.SetState(state)

	p := &plugin.SearchPlugin{}
	matches := p.Apply(m, "hello")
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(matches), matches)
	}
	if matches[0] != 0 || matches[1] != 2 {
		t.Errorf("matches indices = %v, want [0, 2]", matches)
	}
}

func TestSearchPlugin_Apply_CaseInsensitive(t *testing.T) {
	m := NewModel()
	state := m.State()
	state.Lens = LensSystem
	state.Contents[LensSystem] = "Hello World"
	m.SetState(state)

	p := &plugin.SearchPlugin{}
	if matches := p.Apply(m, "hello"); len(matches) != 1 {
		t.Errorf("case insensitive: got %v, want [0]", matches)
	}
	if matches := p.Apply(m, "WORLD"); len(matches) != 1 {
		t.Errorf("case insensitive (upper): got %v, want [0]", matches)
	}
}

func TestSearchPlugin_Apply_NoMatch(t *testing.T) {
	m := NewModel()
	state := m.State()
	state.Lens = LensConversation
	state.Contents[LensConversation] = "abc def"
	m.SetState(state)

	p := &plugin.SearchPlugin{}
	if matches := p.Apply(m, "xyz"); matches != nil {
		t.Errorf("no match: got %v, want nil", matches)
	}
}

func TestSearchPlugin_Apply_EmptyQuery(t *testing.T) {
	m := NewModel()
	state := m.State()
	state.Lens = LensConversation
	state.Contents[LensConversation] = "any content"
	m.SetState(state)

	p := &plugin.SearchPlugin{}
	if matches := p.Apply(m, ""); matches != nil {
		t.Errorf("empty query: got %v, want nil", matches)
	}
}

func TestSearchPlugin_Apply_NilTarget(t *testing.T) {
	p := &plugin.SearchPlugin{}
	var m *InspectorModel
	if matches := p.Apply(m, "anything"); matches != nil {
		t.Errorf("nil target: got %v, want nil", matches)
	}
}

// TestSearchableInterface_InspectorModelSatisfies — 编译期 + 运行时双保障。
func TestSearchableInterface_InspectorModelSatisfies(t *testing.T) {
	m := NewModel()
	var _ plugin.Searchable = m
	// Runtime invocation
	var iface plugin.Searchable = m
	_ = iface.SearchableLines()
}
