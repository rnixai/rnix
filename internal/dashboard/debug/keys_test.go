// Package debug — keys_test.go (Story 38-5 PR11 Step 2)
package debug

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
)

type fakeProvider struct {
	state DebugState
}

func (f fakeProvider) DebugState() DebugState { return f.state }

type fakeNonProvider struct{}

var _ StateProvider = (*fakeProvider)(nil)

func TestKeyLayer_NameAndContract(t *testing.T) {
	l := KeyLayer(nil)
	if l == nil {
		t.Fatal("nil")
	}
	if l.Name != "Debug View" {
		t.Errorf("Name = %q, want Debug View", l.Name)
	}
	if len(l.Bindings) != 0 {
		t.Errorf("Bindings should be empty (catch-all delegated to handleDebugKey)")
	}
	if len(l.Docs) != 5 {
		t.Errorf("Docs len = %d, want 5", len(l.Docs))
	}
	for _, k := range []string{"s", "f", "v", "j/k", "d"} {
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
		"s":   "Toggle strace",
		"f":   "Filter events",
		"v":   "Expand detail",
		"j/k": "Navigate events",
		"d":   "Exit debug mode",
	}
	for k, want := range tests {
		if got := l.Docs[k].Description; got != want {
			t.Errorf("Docs[%q].Description = %q, want %q", k, got, want)
		}
	}
}

func TestActiveModes_DefaultStraceOffScrollManual(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: DebugState{}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 2 {
		t.Fatalf("modes count = %d, want 2", len(modes))
	}
	if modes[0].Name != "strace" || modes[0].Value != "off" {
		t.Errorf("strace mode wrong: %+v", modes[0])
	}
	if modes[1].Name != "scroll" || modes[1].Value != "manual" {
		t.Errorf("scroll mode wrong: %+v", modes[1])
	}
}

func TestActiveModes_StraceOnScrollAuto(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: DebugState{ShowStrace: true, AutoScroll: true}}
	modes := l.ActiveModesFn(provider)
	if modes[0].Value != "on" || modes[1].Value != "auto" {
		t.Errorf("got %+v, want strace:on + scroll:auto", modes)
	}
}

func TestActiveModes_StraceOnScrollManual(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: DebugState{ShowStrace: true, AutoScroll: false}}
	modes := l.ActiveModesFn(provider)
	if modes[0].Value != "on" || modes[1].Value != "manual" {
		t.Errorf("got %+v, want strace:on + scroll:manual", modes)
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
		t.Fatal("Fallback should not be nil")
	}
	l.Fallback(tea.KeyPressMsg{}, nil)
	if !called {
		t.Error("Fallback closure not invoked")
	}
}
