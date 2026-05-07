// Package trace — keys_test.go (Story 38-5 PR8 Step 2)
package trace

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
)

type fakeProvider struct {
	state TraceState
}

func (f fakeProvider) TraceState() TraceState { return f.state }

type fakeNonProvider struct{}

var _ StateProvider = (*fakeProvider)(nil)

func TestKeyLayer_NameAndContract(t *testing.T) {
	layer := KeyLayer(nil)
	if layer == nil {
		t.Fatal("nil layer")
	}
	if layer.Name != "Trace Pane" {
		t.Errorf("Name = %q", layer.Name)
	}
	if len(layer.Bindings) != 0 {
		t.Errorf("Bindings should be empty")
	}
	if len(layer.Docs) != 3 {
		t.Errorf("Docs len = %d, want 3 (enter/c/f)", len(layer.Docs))
	}
	for _, k := range []string{"enter", "c", "f"} {
		if doc, ok := layer.Docs[k]; !ok {
			t.Errorf(`Docs[%q] missing`, k)
		} else if doc.Description == "" {
			t.Errorf(`Docs[%q].Description empty`, k)
		}
	}
}

func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("should not panic: %v", r)
		}
	}()
	_ = KeyLayer(nil)
}

func TestKeyLayer_DocDescriptions(t *testing.T) {
	layer := KeyLayer(nil)
	tests := map[string]string{
		"enter": "Drill in to span tree",
		"c":     "Collapse",
		"f":     "Filter by status",
	}
	for k, want := range tests {
		if got := layer.Docs[k].Description; got != want {
			t.Errorf("Docs[%q].Description = %q, want %q", k, got, want)
		}
	}
}

func TestActiveModes_OverviewDefault(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: TraceState{ViewMode: 0}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Name != "view" || modes[0].Value != "overview" {
		t.Errorf("ViewMode=0: modes = %+v, want [{view overview}]", modes)
	}
}

func TestActiveModes_SpansView(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: TraceState{ViewMode: 1}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Value != "spans" {
		t.Errorf("ViewMode=1: modes = %+v, want [{view spans}]", modes)
	}
}

func TestActiveModes_OutOfRangeViewMode(t *testing.T) {
	// ViewMode=99 越界 → fallback 为 overview（防御性）
	layer := KeyLayer(nil)
	provider := fakeProvider{state: TraceState{ViewMode: 99}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Value != "overview" {
		t.Errorf("ViewMode=99: should fallback to overview, got %+v", modes)
	}
}

func TestActiveModes_NonStateProviderContext(t *testing.T) {
	layer := KeyLayer(nil)
	if modes := layer.ActiveModesFn(fakeNonProvider{}); modes != nil {
		t.Errorf("non-provider: modes = %v, want nil", modes)
	}
}

func TestActiveModes_NilContext(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("should not panic: %v", r)
		}
	}()
	layer := KeyLayer(nil)
	if modes := layer.ActiveModesFn(nil); modes != nil {
		t.Errorf("nil ctx: modes = %v, want nil", modes)
	}
}

func TestKeyLayer_FallbackPropagated(t *testing.T) {
	called := false
	fallback := ui.KeyHandler(func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		called = true
		return false, ctx, nil
	})
	layer := KeyLayer(fallback)
	if layer.Fallback == nil {
		t.Fatal("Fallback should be propagated")
	}
	layer.Fallback(tea.KeyPressMsg{}, nil)
	if !called {
		t.Error("Fallback not invoked")
	}
}

// 38-4 waterfall 契约：SpanFlatNodes 字段不影响 ActiveModes（只通过 ViewMode）
func TestActiveModes_SpanFlatNodesDoesNotAffectMode(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: TraceState{
		ViewMode:      0,
		SpanFlatNodes: make([]SpanFlatNode, 100),
	}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Value != "overview" {
		t.Errorf("SpanFlatNodes len shouldn't affect mode, got %+v", modes)
	}
}
