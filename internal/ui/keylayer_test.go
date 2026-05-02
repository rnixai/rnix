package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeKey constructs a tea.KeyPressMsg whose String() equals s.
// We use the simplest path: tea.Key with Code=rune for single chars.
func fakeKey(s string) tea.KeyPressMsg {
	// Special-case multi-char keys frequently used in tests
	switch s {
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	}
	if len(s) == 1 {
		return tea.KeyPressMsg(tea.Key{Code: rune(s[0])})
	}
	// Fallback: Text-only message; .String() will format from Text.
	return tea.KeyPressMsg(tea.Key{Text: s})
}

// modelStub is a minimal stand-in for dashboardModel used in tests.
type modelStub struct {
	counter int
	last    string
}

// TestDispatcher_Handle_Layer0Consumes verifies Layer 0 consumption stops the chain.
func TestDispatcher_Handle_Layer0Consumes(t *testing.T) {
	d := NewDispatcher()
	d.Layer0 = &KeyLayer{
		Name: "Global",
		Bindings: map[string]KeyHandler{
			"q": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.last = "layer0"
				return true, m, nil
			},
		},
		Docs: map[string]KeyDoc{},
	}
	d.Layer1[ViewID(0)] = &KeyLayer{
		Name: "View",
		Bindings: map[string]KeyHandler{
			"q": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.last = "layer1"
				return true, m, nil
			},
		},
		Docs: map[string]KeyDoc{},
	}

	got, _, consumed := d.Handle(fakeKey("q"), 0, 0, modelStub{})
	if !consumed {
		t.Fatalf("expected consumed=true")
	}
	m := got.(modelStub)
	if m.last != "layer0" {
		t.Fatalf("expected layer0 to handle, got %q", m.last)
	}
}

// TestDispatcher_Handle_FallThrough_Layer0_to_Layer1 verifies Layer 1 picks up
// when Layer 0 has no binding.
func TestDispatcher_Handle_FallThrough_Layer0_to_Layer1(t *testing.T) {
	d := NewDispatcher()
	d.Layer0 = &KeyLayer{Name: "Global", Bindings: map[string]KeyHandler{}, Docs: map[string]KeyDoc{}}
	d.Layer1[ViewID(0)] = &KeyLayer{
		Name: "View",
		Bindings: map[string]KeyHandler{
			"a": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.last = "layer1"
				return true, m, nil
			},
		},
		Docs: map[string]KeyDoc{},
	}

	got, _, consumed := d.Handle(fakeKey("a"), 0, 0, modelStub{})
	if !consumed {
		t.Fatalf("expected consumed=true")
	}
	m := got.(modelStub)
	if m.last != "layer1" {
		t.Fatalf("expected layer1 to handle, got %q", m.last)
	}
}

// TestDispatcher_Handle_FallThrough_Layer0_Layer1_to_Layer2 verifies Layer 2 picks up
// when both Layer 0 and Layer 1 have no binding.
func TestDispatcher_Handle_FallThrough_Layer0_Layer1_to_Layer2(t *testing.T) {
	d := NewDispatcher()
	d.Layer0 = &KeyLayer{Name: "Global", Bindings: map[string]KeyHandler{}, Docs: map[string]KeyDoc{}}
	d.Layer1[ViewID(0)] = &KeyLayer{Name: "View", Bindings: map[string]KeyHandler{}, Docs: map[string]KeyDoc{}}
	d.Layer2[PaneID(0)] = &KeyLayer{
		Name: "Pane",
		Bindings: map[string]KeyHandler{
			"f": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.last = "layer2"
				return true, m, nil
			},
		},
		Docs: map[string]KeyDoc{},
	}

	got, _, consumed := d.Handle(fakeKey("f"), 0, 0, modelStub{})
	if !consumed {
		t.Fatalf("expected consumed=true")
	}
	m := got.(modelStub)
	if m.last != "layer2" {
		t.Fatalf("expected layer2 to handle, got %q", m.last)
	}
}

// TestDispatcher_Handle_NoBinding_ReturnsNotConsumed verifies that all 3 layers
// missing the key returns consumed=false and ctx unchanged.
func TestDispatcher_Handle_NoBinding_ReturnsNotConsumed(t *testing.T) {
	d := NewDispatcher()
	d.Layer0 = &KeyLayer{Name: "Global", Bindings: map[string]KeyHandler{}, Docs: map[string]KeyDoc{}}
	d.Layer1[ViewID(0)] = &KeyLayer{Name: "View", Bindings: map[string]KeyHandler{}, Docs: map[string]KeyDoc{}}
	d.Layer2[PaneID(0)] = &KeyLayer{Name: "Pane", Bindings: map[string]KeyHandler{}, Docs: map[string]KeyDoc{}}

	got, cmd, consumed := d.Handle(fakeKey("z"), 0, 0, modelStub{counter: 42})
	if consumed {
		t.Fatalf("expected consumed=false")
	}
	if cmd != nil {
		t.Fatalf("expected cmd=nil, got %v", cmd)
	}
	if got.(modelStub).counter != 42 {
		t.Fatalf("expected ctx unchanged, got %+v", got)
	}
}

// TestDispatcher_Handle_NilDispatcher_Safe verifies nil dispatcher returns gracefully.
func TestDispatcher_Handle_NilDispatcher_Safe(t *testing.T) {
	var d *Dispatcher
	got, cmd, consumed := d.Handle(fakeKey("q"), 0, 0, modelStub{})
	if consumed {
		t.Fatalf("expected consumed=false on nil dispatcher")
	}
	if cmd != nil {
		t.Fatalf("expected cmd=nil")
	}
	if got == nil {
		// ctx is returned as-is even on nil dispatcher; modelStub is non-nil concrete.
		// We accept any non-panic outcome.
		t.Logf("ctx returned: nil — acceptable")
	}
}

// TestDispatcher_HelpFor_Aggregates3Layers verifies HelpFor pulls docs from all 3 layers.
func TestDispatcher_HelpFor_Aggregates3Layers(t *testing.T) {
	d := NewDispatcher()
	d.Layer0 = &KeyLayer{
		Name:     "Global",
		Bindings: map[string]KeyHandler{},
		Docs: map[string]KeyDoc{
			"q": {Key: "q", Description: "Quit"},
		},
	}
	d.Layer1[ViewID(0)] = &KeyLayer{
		Name:     "Default",
		Bindings: map[string]KeyHandler{},
		Docs: map[string]KeyDoc{
			"1": {Key: "1", Description: "Tree pane"},
		},
	}
	d.Layer2[PaneID(0)] = &KeyLayer{
		Name:     "Tree",
		Bindings: map[string]KeyHandler{},
		Docs: map[string]KeyDoc{
			"K": {Key: "K", Description: "Kill"},
		},
	}

	docs := d.HelpFor(0, 0)
	if len(docs) != 3 {
		t.Fatalf("expected 3 docs, got %d: %+v", len(docs), docs)
	}
	// Sorted by key per layer; layers concatenated in 0→1→2 order.
	if docs[0].Key != "q" || docs[1].Key != "1" || docs[2].Key != "K" {
		t.Fatalf("unexpected doc order: %+v", docs)
	}
}

// TestDispatcher_HelpGroupedFor_3Sections verifies HelpGroupedFor returns 3 groups.
func TestDispatcher_HelpGroupedFor_3Sections(t *testing.T) {
	d := NewDispatcher()
	d.Layer0 = &KeyLayer{
		Name:     "Global",
		Bindings: map[string]KeyHandler{},
		Docs:     map[string]KeyDoc{"q": {Key: "q", Description: "Quit"}},
	}
	d.Layer1[ViewID(0)] = &KeyLayer{
		Name:     "Default",
		Bindings: map[string]KeyHandler{},
		Docs:     map[string]KeyDoc{"1": {Key: "1", Description: "Tree"}},
	}
	d.Layer2[PaneID(0)] = &KeyLayer{
		Name:     "Tree",
		Bindings: map[string]KeyHandler{},
		Docs:     map[string]KeyDoc{"K": {Key: "K", Description: "Kill"}},
	}

	groups := d.HelpGroupedFor(0, 0)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].Layer != "Global" || groups[1].Layer != "View" || groups[2].Layer != "Pane" {
		t.Fatalf("unexpected layer order: %+v", groups)
	}
	if groups[1].Name != "Default" || groups[2].Name != "Tree" {
		t.Fatalf("unexpected layer names: %+v", groups)
	}
}

// TestKeyLayer_ActiveModes_NilSafe verifies ActiveModes handles nil layer / nil fn.
func TestKeyLayer_ActiveModes_NilSafe(t *testing.T) {
	var l *KeyLayer
	if got := l.ActiveModes(nil); got != nil {
		t.Fatalf("nil layer should return nil, got %v", got)
	}
	l2 := &KeyLayer{Name: "Tree"}
	if got := l2.ActiveModes(nil); got != nil {
		t.Fatalf("nil ActiveModesFn should return nil, got %v", got)
	}
	l3 := &KeyLayer{
		Name: "Tree",
		ActiveModesFn: func(ctx KeyContext) []Mode {
			return []Mode{{Name: "sort", Value: "time"}}
		},
	}
	got := l3.ActiveModes(nil)
	if len(got) != 1 || got[0].Name != "sort" {
		t.Fatalf("unexpected ActiveModes result: %+v", got)
	}
}

// TestDispatcher_NonConsumingHandler_PassesNewCtx verifies that even when a
// handler returns consumed=false, its newCtx is forwarded to the next layer.
func TestDispatcher_NonConsumingHandler_PassesNewCtx(t *testing.T) {
	d := NewDispatcher()
	d.Layer0 = &KeyLayer{
		Name: "Global",
		Bindings: map[string]KeyHandler{
			"x": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.counter++
				// Don't consume: counter side-effect should still propagate.
				return false, m, nil
			},
		},
		Docs: map[string]KeyDoc{},
	}
	d.Layer1[ViewID(0)] = &KeyLayer{
		Name: "View",
		Bindings: map[string]KeyHandler{
			"x": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.counter += 10 // expect Layer 0 to have already incremented
				return true, m, nil
			},
		},
		Docs: map[string]KeyDoc{},
	}

	got, _, consumed := d.Handle(fakeKey("x"), 0, 0, modelStub{counter: 0})
	if !consumed {
		t.Fatalf("expected consumed=true (layer1)")
	}
	if got.(modelStub).counter != 11 {
		t.Fatalf("expected counter=11 (layer0 +1, layer1 +10), got %d", got.(modelStub).counter)
	}
}

// TestDispatcher_ActiveModesFor_NilSafe verifies ActiveModesFor handles nil
// dispatcher and missing pane gracefully.
func TestDispatcher_ActiveModesFor_NilSafe(t *testing.T) {
	var d *Dispatcher
	if got := d.ActiveModesFor(0, nil); got != nil {
		t.Fatalf("nil dispatcher should return nil, got %v", got)
	}

	d2 := NewDispatcher() // empty Layer2
	if got := d2.ActiveModesFor(99, nil); got != nil {
		t.Fatalf("missing pane should return nil, got %v", got)
	}
}

// TestKeyLayer_Fallback verifies Fallback is called when no specific binding matches.
func TestKeyLayer_Fallback(t *testing.T) {
	d := NewDispatcher()
	d.Layer2[PaneID(0)] = &KeyLayer{
		Name: "Tree",
		Bindings: map[string]KeyHandler{
			"K": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.last = "K-binding"
				return true, m, nil
			},
		},
		Fallback: func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
			m := ctx.(modelStub)
			m.last = "fallback"
			return true, m, nil
		},
		Docs: map[string]KeyDoc{},
	}

	// Specific key → binding wins
	got, _, consumed := d.Handle(fakeKey("K"), 0, 0, modelStub{})
	if !consumed || got.(modelStub).last != "K-binding" {
		t.Fatalf("expected K-binding, got %+v consumed=%v", got, consumed)
	}

	// Other key → fallback handles
	got, _, consumed = d.Handle(fakeKey("z"), 0, 0, modelStub{})
	if !consumed || got.(modelStub).last != "fallback" {
		t.Fatalf("expected fallback, got %+v consumed=%v", got, consumed)
	}
}

// TestKeyLayer_Binding_NotConsumed_FallthroughToFallback verifies that when a
// specific binding matches but returns consumed=false, the same layer's
// Fallback is then invoked.
func TestKeyLayer_Binding_NotConsumed_FallthroughToFallback(t *testing.T) {
	d := NewDispatcher()
	d.Layer2[PaneID(0)] = &KeyLayer{
		Name: "Tree",
		Bindings: map[string]KeyHandler{
			"K": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
				m := ctx.(modelStub)
				m.counter++
				return false, m, nil // does not consume
			},
		},
		Fallback: func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
			m := ctx.(modelStub)
			m.counter += 10
			return true, m, nil
		},
		Docs: map[string]KeyDoc{},
	}

	got, _, consumed := d.Handle(fakeKey("K"), 0, 0, modelStub{counter: 0})
	if !consumed {
		t.Fatalf("expected consumed=true (fallback)")
	}
	if got.(modelStub).counter != 11 {
		t.Fatalf("expected counter=11 (binding +1, fallback +10), got %d", got.(modelStub).counter)
	}
}

// TestPaneSpecificFKey verifies that the same physical key 'f' routes to
// different KeyHandlers based on m.activePane (AC4 contract test).
func TestPaneSpecificFKey(t *testing.T) {
	d := NewDispatcher()
	for _, p := range []PaneID{0, 1, 2, 3} {
		paneID := p
		d.Layer2[paneID] = &KeyLayer{
			Name: "Pane",
			Bindings: map[string]KeyHandler{
				"f": func(_ tea.KeyPressMsg, ctx KeyContext) (bool, KeyContext, tea.Cmd) {
					m := ctx.(modelStub)
					m.last = string(rune('a' + int(paneID)))
					return true, m, nil
				},
			},
			Docs: map[string]KeyDoc{},
		}
	}

	for _, p := range []PaneID{0, 1, 2, 3} {
		got, _, consumed := d.Handle(fakeKey("f"), 0, p, modelStub{})
		if !consumed {
			t.Fatalf("pane %d: expected consumed=true", p)
		}
		want := string(rune('a' + int(p)))
		if got.(modelStub).last != want {
			t.Fatalf("pane %d: expected last=%q, got %q", p, want, got.(modelStub).last)
		}
	}
}


func TestDispatcher_HelpFor_NilSafe(t *testing.T) {
	var d *Dispatcher
	if got := d.HelpFor(0, 0); got != nil {
		t.Fatalf("nil dispatcher should return nil, got %v", got)
	}

	var d2 *Dispatcher
	if got := d2.HelpGroupedFor(0, 0); got != nil {
		t.Fatalf("nil dispatcher should return nil, got %v", got)
	}
}

// TestDispatcher_ActiveModesFor_RoutesByPane verifies Mode Strip data source
// routes by current active pane.
func TestDispatcher_ActiveModesFor_RoutesByPane(t *testing.T) {
	d := NewDispatcher()
	d.Layer2[PaneID(0)] = &KeyLayer{
		Name: "Tree",
		ActiveModesFn: func(ctx KeyContext) []Mode {
			return []Mode{{Name: "sort", Value: "pid"}}
		},
	}
	d.Layer2[PaneID(1)] = &KeyLayer{
		Name: "Timeline",
		ActiveModesFn: func(ctx KeyContext) []Mode {
			return []Mode{{Name: "filter", Value: "tool"}, {Name: "follow", Value: "on"}}
		},
	}

	tree := d.ActiveModesFor(0, nil)
	if len(tree) != 1 || tree[0].Name != "sort" || tree[0].Value != "pid" {
		t.Fatalf("expected sort=pid for Tree, got %+v", tree)
	}
	timeline := d.ActiveModesFor(1, nil)
	if len(timeline) != 2 {
		t.Fatalf("expected 2 modes for Timeline, got %d", len(timeline))
	}
}
