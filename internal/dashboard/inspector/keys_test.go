// Package inspector — keys_test.go (Story 38-5 PR10 Step 2)
//
// Inspector KeyLayer 行为契约测试 · 与 PR2-PR9 同模式（详尽 godoc + 边界 case）。
package inspector

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
)

type fakeProvider struct {
	state InspectorState
}

func (f fakeProvider) InspectorState() InspectorState { return f.state }

type fakeNonProvider struct{}

var _ StateProvider = (*fakeProvider)(nil)

// TestKeyLayer_NameAndContract — Name="Step Inspector" + Bindings 空 + 11 Docs（38-1
// registerLayer1StepInspector 落地完全等价）。
func TestKeyLayer_NameAndContract(t *testing.T) {
	l := KeyLayer(nil)
	if l == nil {
		t.Fatal("nil")
	}
	if l.Name != "Step Inspector" {
		t.Errorf("Name = %q, want %q", l.Name, "Step Inspector")
	}
	if len(l.Bindings) != 0 {
		t.Errorf("Bindings should be empty (catch-all delegated to inspectorKey)")
	}
	if len(l.Docs) != 11 {
		t.Errorf("Docs len = %d, want 11", len(l.Docs))
	}
	for _, k := range []string{"1-5", "h/l", "H/L", "j/k", "/", "n/N", "d", "F", "y", "o", "esc"} {
		if doc, ok := l.Docs[k]; !ok {
			t.Errorf(`Docs[%q] missing`, k)
		} else if doc.Description == "" {
			t.Errorf(`Docs[%q].Description empty`, k)
		}
	}
}

// TestKeyLayer_FallbackNilSafe — fallback=nil 不 panic。
func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v", r)
		}
	}()
	_ = KeyLayer(nil)
}

// TestKeyLayer_DocDescriptions — 11 个 Docs 的 Description 与 38-1 落地完全一致
// （防止 Docs 文案漂移破坏 help overlay 显示）。
func TestKeyLayer_DocDescriptions(t *testing.T) {
	l := KeyLayer(nil)
	tests := map[string]string{
		"1-5": "Switch lens",
		"h/l": "Prev / next step",
		"H/L": "First / last step",
		"j/k": "Scroll lens content",
		"/":   "Search",
		"n/N": "Next / previous match",
		"d":   "Diff mode (dd to pick base)",
		"F":   "Follow live",
		"y":   "Copy to clipboard",
		"o":   "Open in pager",
		"esc": "Close inspector",
	}
	for k, want := range tests {
		if got := l.Docs[k].Description; got != want {
			t.Errorf("Docs[%q].Description = %q, want %q", k, got, want)
		}
	}
}

// TestActiveModes_DefaultEmpty — DiffMode=false + FollowLive=false → ActiveModes 为 nil
// （默认状态不显示任何子模式 · 与 38-1 落地一致）。
func TestActiveModes_DefaultEmpty(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: InspectorState{}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 0 {
		t.Errorf("got %+v, want empty", modes)
	}
}

// TestActiveModes_DiffOn — DiffMode=true → modes=[{diff, on}]（36-6 落地）。
func TestActiveModes_DiffOn(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: InspectorState{DiffMode: true}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Name != "diff" || modes[0].Value != "on" {
		t.Errorf("got %+v, want [{diff on}]", modes)
	}
}

// TestActiveModes_FollowLive — FollowLive=true → modes=[{follow, live}]（36-6 落地）。
func TestActiveModes_FollowLive(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: InspectorState{FollowLive: true}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 1 || modes[0].Name != "follow" || modes[0].Value != "live" {
		t.Errorf("got %+v, want [{follow live}]", modes)
	}
}

// TestActiveModes_DiffAndFollow — 两子模式同时激活 → modes=[{diff on}, {follow live}]
// （顺序契约：diff 先 · follow 后 · 防止 ActiveModes 渲染顺序在 38-2 mode strip 中漂移）。
func TestActiveModes_DiffAndFollow(t *testing.T) {
	l := KeyLayer(nil)
	provider := fakeProvider{state: InspectorState{DiffMode: true, FollowLive: true}}
	modes := l.ActiveModesFn(provider)
	if len(modes) != 2 {
		t.Fatalf("got %+v, want 2 modes", modes)
	}
	if modes[0].Name != "diff" || modes[1].Name != "follow" {
		t.Errorf("order broken: got %+v, want [diff, follow]", modes)
	}
}

// TestActiveModes_NonStateProviderContext — ctx 不实现 StateProvider → nil（防御性）。
func TestActiveModes_NonStateProviderContext(t *testing.T) {
	l := KeyLayer(nil)
	if modes := l.ActiveModesFn(fakeNonProvider{}); modes != nil {
		t.Errorf("non-provider: %v", modes)
	}
}

// TestActiveModes_NilContext — ctx=nil 不 panic（防御性 · 与 PR4-PR9 同模式）。
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

// TestKeyLayer_FallbackPropagated — fallback closure 正确传递给 ui.KeyLayer.Fallback
// （catch-all 模式：所有 inspector 键经 fallback 路由到 dashboard_inspector.go::inspectorKey）。
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

// TestStateProvider_InterfaceShape — interface 编译期可被引用（防止误删 method signature）。
func TestStateProvider_InterfaceShape(t *testing.T) {
	var _ StateProvider = fakeProvider{}
}
