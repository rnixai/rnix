// Package eval — keys_test.go (Story 38-5 PR9 Step 2)
package eval

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
)

type fakeProvider struct {
	state EvalState
}

func (f fakeProvider) EvalState() EvalState { return f.state }

type fakeNonProvider struct{}

var _ StateProvider = (*fakeProvider)(nil)

func TestKeyLayer_NameAndContract(t *testing.T) {
	l := KeyLayer(nil)
	if l == nil {
		t.Fatal("nil")
	}
	if l.Name != "Eval Pane" {
		t.Errorf("Name = %q", l.Name)
	}
	if len(l.Bindings) != 0 {
		t.Errorf("Bindings should be empty")
	}
	if len(l.Docs) != 2 {
		t.Errorf("Docs len = %d, want 2", len(l.Docs))
	}
	for _, k := range []string{"1/2/3", "o"} {
		if doc, ok := l.Docs[k]; !ok {
			t.Errorf(`Docs[%q] missing`, k)
		} else if doc.Description == "" {
			t.Errorf(`Docs[%q].Description empty`, k)
		}
	}
}

func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v", r)
		}
	}()
	_ = KeyLayer(nil)
}

func TestKeyLayer_DocDescriptions(t *testing.T) {
	l := KeyLayer(nil)
	tests := map[string]string{
		"1/2/3": "Switch sub-view",
		"o":     "Sort by score",
	}
	for k, want := range tests {
		if got := l.Docs[k].Description; got != want {
			t.Errorf("Docs[%q].Description = %q, want %q", k, got, want)
		}
	}
}

func TestActiveModes_ReputationDefault(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: EvalState{SubView: 0}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Value != "reputation" {
		t.Errorf("got %+v, want [{view reputation}]", modes)
	}
}

func TestActiveModes_TopologyView(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: EvalState{SubView: 1}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Value != "topology" {
		t.Errorf("got %+v", modes)
	}
}

func TestActiveModes_SynergyView(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: EvalState{SubView: 2}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Value != "synergy" {
		t.Errorf("got %+v", modes)
	}
}

func TestActiveModes_OutOfRangeFallback(t *testing.T) {
	// SubView=99 越界 → fallback 为 reputation（防御性）
	l := KeyLayer(nil)
	provider := fakeProvider{state: EvalState{SubView: 99}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Value != "reputation" {
		t.Errorf("out-of-range: got %+v, want fallback to reputation", modes)
	}
}

func TestActiveModes_NonStateProviderContext(t *testing.T) {
	l := KeyLayer(nil)
	if modes := l.ActiveModesFn(fakeNonProvider{}); modes != nil {
		t.Errorf("non-provider: %v", modes)
	}
}

func TestActiveModes_NilContext(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v", r)
		}
	}()
	l := KeyLayer(nil)
	if modes := l.ActiveModesFn(nil); modes != nil {
		t.Errorf("nil ctx: %v", modes)
	}
}

func TestKeyLayer_FallbackPropagated(t *testing.T) {
	called := false
	fallback := ui.KeyHandler(func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		called = true
		return false, ctx, nil
	})
	l := KeyLayer(fallback)
	if l.Fallback == nil {
		t.Fatal("Fallback")
	}
	l.Fallback(tea.KeyPressMsg{}, nil)
	if !called {
		t.Error("not invoked")
	}
}
