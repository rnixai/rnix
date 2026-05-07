// Package intent — keys_test.go (Story 38-5 PR6 Step 2)
//
// 行为契约测试 · 与 PR2/PR3/PR4/PR5 同模式 · spec 38-3 教训应用（边界 case 子测试）。
package intent

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// fakeProvider 实现 StateProvider · 让 ActiveModesFn cast 成功。
type fakeProvider struct {
	state IntentState
}

func (f fakeProvider) IntentState() IntentState { return f.state }

// fakeNonProvider 不实现 StateProvider · 用于测试 cast 失败的防御性回路。
type fakeNonProvider struct{}

// 编译期断言：fakeProvider 满足 StateProvider，fakeNonProvider 不满足。
var _ StateProvider = (*fakeProvider)(nil)

// TestKeyLayer_NameAndEmptyContract 验证 KeyLayer 基础结构与 38-1 落地约定。
func TestKeyLayer_NameAndEmptyContract(t *testing.T) {
	layer := KeyLayer(nil)
	if layer == nil {
		t.Fatal("KeyLayer(nil) returned nil layer")
	}
	if layer.Name != "Intent Pane" {
		t.Errorf("Name = %q, want %q", layer.Name, "Intent Pane")
	}
	if len(layer.Bindings) != 0 {
		t.Errorf("Bindings should be empty (38-1 落地状态), got %d", len(layer.Bindings))
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

// TestKeyLayer_FallbackNilSafe 验证 nil fallback 不 panic（dispatcher 内部容错）。
func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("KeyLayer(nil) should not panic, got %v", r)
		}
	}()
	layer := KeyLayer(nil)
	_ = layer
}

// TestKeyLayer_DocEnterDescription 验证 enter Docs 文案与 38-1 落地一致。
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

// TestActiveModes_TreeViewDefault 验证空状态下默认仅返回 view: tree。
func TestActiveModes_TreeViewDefault(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: IntentState{}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 1 {
		t.Fatalf("empty state: modes len = %d, want 1, got %v", len(modes), modes)
	}
	if modes[0].Name != "view" || modes[0].Value != "tree" {
		t.Errorf("empty state: modes[0] = %+v, want {view tree}", modes[0])
	}
}

// TestActiveModes_NodesAppendedWhenPopulated 验证 FlatNodes 非空时追加 nodes:N。
func TestActiveModes_NodesAppendedWhenPopulated(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: IntentState{
		FlatNodes: make([]IntentFlatNode, 5),
	}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 2 {
		t.Fatalf("populated state: modes len = %d, want 2, got %v", len(modes), modes)
	}
	if modes[0].Name != "view" || modes[0].Value != "tree" {
		t.Errorf("modes[0] = %+v, want {view tree}", modes[0])
	}
	if modes[1].Name != "nodes" || modes[1].Value != "5" {
		t.Errorf("modes[1] = %+v, want {nodes 5}", modes[1])
	}
}

// TestActiveModes_LargeNodesCount 验证大值正确格式化。
func TestActiveModes_LargeNodesCount(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: IntentState{
		FlatNodes: make([]IntentFlatNode, 1234),
	}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 2 || modes[1].Value != "1234" {
		t.Errorf("large count: modes = %v, want last value '1234'", modes)
	}
}

// TestActiveModes_NonStateProviderContext 验证 ctx 不实现 StateProvider 时返回 nil。
func TestActiveModes_NonStateProviderContext(t *testing.T) {
	layer := KeyLayer(nil)
	modes := layer.ActiveModesFn(fakeNonProvider{})
	if modes != nil {
		t.Errorf("non-provider ctx: modes = %v, want nil", modes)
	}
}

// TestActiveModes_NilContextPanicSafety 验证 nil ctx 不 panic（防御性）。
func TestActiveModes_NilContextPanicSafety(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil ctx should not panic, got %v", r)
		}
	}()
	layer := KeyLayer(nil)
	// nil 接口 cast StateProvider 返回 false，不 panic。
	modes := layer.ActiveModesFn(nil)
	if modes != nil {
		t.Errorf("nil ctx: modes = %v, want nil", modes)
	}
}

// TestActiveModes_TreeCollapsedDoesNotAffectMode 验证折叠 map 状态不影响 ActiveModes。
//
// 38-4 P1 落地：TreeCollapsed 影响 FlatNodes 内容（折叠后 FlatNodes 长度减少），
// 但 ActiveModesFn 只看 FlatNodes 总数，不直接读 TreeCollapsed map。
func TestActiveModes_TreeCollapsedDoesNotAffectMode(t *testing.T) {
	layer := KeyLayer(nil)
	provider := fakeProvider{state: IntentState{
		FlatNodes:     make([]IntentFlatNode, 3),
		TreeCollapsed: map[string]bool{"some-root": true},
	}}
	modes := layer.ActiveModesFn(provider)
	if len(modes) != 2 || modes[1].Value != "3" {
		t.Errorf("TreeCollapsed should not affect modes: %v", modes)
	}
}

// TestStateProvider_InterfaceShape 编译期断言保留：interface 形态稳定（防 PR11 误删 getter）。
func TestStateProvider_InterfaceShape(t *testing.T) {
	var _ StateProvider = fakeProvider{}
	// 确保类型在测试包内可被消费 — 间接验证 interface 公开。
	_ = ipc.IntentTreeWire{} // import sanity
}

// TestKeyLayer_FallbackPropagated 验证 fallback handler 被正确写入 layer.Fallback。
func TestKeyLayer_FallbackPropagated(t *testing.T) {
	called := false
	fallback := ui.KeyHandler(func(msg tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		called = true
		return false, ctx, nil
	})
	layer := KeyLayer(fallback)
	if layer.Fallback == nil {
		t.Fatal("Fallback should be propagated")
	}
	// 调用一次以验证 closure 正确捕获
	layer.Fallback(tea.KeyPressMsg{}, nil)
	if !called {
		t.Error("Fallback function not invoked")
	}
}
