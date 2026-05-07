// Package security — keys_test.go (Story 38-5 PR7 Step 2)
//
// 行为契约测试 · 与 PR2-PR6 同模式 · spec 38-3 教训应用（边界 case 子测试）。
package security

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// fakeProvider 实现 StateProvider · 让 ActiveModesFn cast 成功。
type fakeProvider struct {
	state SecurityState
}

func (f fakeProvider) SecurityState() SecurityState { return f.state }

// fakeNonProvider 不实现 StateProvider · 用于测试 cast 失败的防御性回路。
type fakeNonProvider struct{}

// 编译期断言：fakeProvider 满足 StateProvider。
var _ StateProvider = (*fakeProvider)(nil)

func TestKeyLayer_NameAndEmptyContract(t *testing.T) {
	layer := KeyLayer(nil)
	if layer == nil {
		t.Fatal("KeyLayer(nil) returned nil layer")
	}
	if layer.Name != "Security Pane" {
		t.Errorf("Name = %q, want %q", layer.Name, "Security Pane")
	}
	if len(layer.Bindings) != 0 {
		t.Errorf("Bindings should be empty, got %d", len(layer.Bindings))
	}
	if len(layer.Docs) != 1 {
		t.Errorf("Docs should have 1 entry (enter), got %d", len(layer.Docs))
	}
	if doc, ok := layer.Docs["enter"]; !ok {
		t.Error(`Docs["enter"] missing`)
	} else if doc.Description == "" {
		t.Error(`Docs["enter"].Description should be non-empty`)
	}
}

func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("KeyLayer(nil) should not panic, got %v", r)
		}
	}()
	_ = KeyLayer(nil)
}

func TestKeyLayer_DocEnterDescription(t *testing.T) {
	layer := KeyLayer(nil)
	doc, ok := layer.Docs["enter"]
	if !ok {
		t.Fatal(`Docs["enter"] missing`)
	}
	if doc.Description != "Drill in to process timeline" {
		t.Errorf(`Docs["enter"].Description = %q, want "Drill in to process timeline"`, doc.Description)
	}
	if doc.Key != "enter" {
		t.Errorf(`Docs["enter"].Key = %q, want "enter"`, doc.Key)
	}
}

// TestActiveModes_ListViewDefault 验证空状态默认仅返回 view: list。
func TestActiveModes_ListViewDefault(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: SecurityState{}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 1 {
		t.Fatalf("empty state: modes len = %d, want 1, got %v", len(modes), modes)
	}
	if modes[0].Name != "view" || modes[0].Value != "list" {
		t.Errorf("modes[0] = %+v, want {view list}", modes[0])
	}
}

func TestActiveModes_AlertsAppendedWhenPopulated(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: SecurityState{
		Alerts: make([]ipc.AlertWire, 3),
	}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 2 {
		t.Fatalf("populated: modes len = %d, want 2, got %v", len(modes), modes)
	}
	if modes[1].Name != "alerts" || modes[1].Value != "3" {
		t.Errorf("modes[1] = %+v, want {alerts 3}", modes[1])
	}
}

func TestActiveModes_LargeAlertCount(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: SecurityState{
		Alerts: make([]ipc.AlertWire, 999),
	}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 2 || modes[1].Value != "999" {
		t.Errorf("large count: modes = %v, want last value '999'", modes)
	}
}

func TestActiveModes_NonStateProviderContext(t *testing.T) {
	layer := KeyLayer(nil)
	modes := layer.ActiveModesFn(fakeNonProvider{})
	if modes != nil {
		t.Errorf("non-provider ctx: modes = %v, want nil", modes)
	}
}

func TestActiveModes_NilContextPanicSafety(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil ctx should not panic, got %v", r)
		}
	}()
	layer := KeyLayer(nil)
	modes := layer.ActiveModesFn(nil)
	if modes != nil {
		t.Errorf("nil ctx: modes = %v, want nil", modes)
	}
}

// TestActiveModes_ImmuneStatusDoesNotAffectMode 验证 ImmuneStatus 状态不直接影响 ActiveModes。
//
// 38-4 Alert Immune 路由：ImmuneStatus.Alerts 字段在 IPC 端填充后会被 sortAlertsByDeviation
// 复制到 SecurityState.Alerts；ActiveModesFn 只看后者，不直接读 ImmuneStatus。这避免在 IPC
// 响应到达但 sortAlertsByDeviation 未运行的瞬间出现状态不一致。
func TestActiveModes_ImmuneStatusDoesNotAffectMode(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{}, // non-nil
		Alerts:       nil,                          // empty
	}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 1 {
		t.Errorf("ImmuneStatus non-nil + Alerts nil: modes len = %d, want 1 (only view:list)", len(modes))
	}
}

func TestStateProvider_InterfaceShape(t *testing.T) {
	var _ StateProvider = fakeProvider{}
	_ = ipc.AlertWire{} // import sanity
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
		t.Error("Fallback function not invoked")
	}
}
