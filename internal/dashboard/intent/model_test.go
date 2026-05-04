// Package intent — model_test.go (Story 38-5 PR6 Step 3)
//
// IntentModel 契约测试。验证：
//
//  1. State / SetState / IntentState getter 一致性
//  2. SetActive / Init / View / Update + WindowSizeMsg 同步
//  3. Name / KeyLayer 行为
//  4. OnSelectPID 行为（与 PR5 DetailModel 不同：Intent 跨 PID 切换不清空 state · 38-4 P1）
//  5. OnTick 行为（noop · IPC 由 cmd/rnix 端 dashboardTick 触发）
//  6. nil safety: 全部方法在 receiver 为 nil 时返回零值不 panic
//  7. PaneModel interface 编译期断言（model.go 已声明 var _ ）
package intent

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

func TestNewModel_Defaults(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	s := m.State()
	if s.Trees != nil || s.TreeErr != nil || s.FlatNodes != nil ||
		s.Cursor != 0 || s.ScrollOffset != 0 || s.TreeCollapsed != nil {
		t.Errorf("expected zero-value state, got %+v", s)
	}
}

func TestState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *IntentModel
	s := m.State()
	if s.Trees != nil || s.Cursor != 0 {
		t.Errorf("expected zero state for nil receiver, got %+v", s)
	}
}

func TestSetState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	expected := IntentState{
		Trees:         []*ipc.IntentTreeWire{{ID: "tree-1", RootIntent: "root-a"}},
		Cursor:        2,
		ScrollOffset:  1,
		TreeCollapsed: map[string]bool{"root-a": true},
	}
	m.SetState(expected)
	got := m.State()
	if len(got.Trees) != 1 || got.Trees[0].RootIntent != "root-a" {
		t.Errorf("SetState/State: Trees not preserved, got %+v", got.Trees)
	}
	if got.Cursor != 2 || got.ScrollOffset != 1 {
		t.Errorf("SetState/State: Cursor/ScrollOffset = %d/%d, want 2/1", got.Cursor, got.ScrollOffset)
	}
	if !got.TreeCollapsed["root-a"] {
		t.Error("SetState/State: TreeCollapsed[root-a] should be true")
	}
}

func TestSetState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *IntentModel
	// Should not panic
	m.SetState(IntentState{Cursor: 1})
}

func TestSetActive(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetActive(true)
	if !m.active {
		t.Error("SetActive(true) did not flip active flag")
	}
	m.SetActive(false)
	if m.active {
		t.Error("SetActive(false) did not flip back")
	}
}

func TestSetActive_NilSafe(t *testing.T) {
	t.Parallel()
	var m *IntentModel
	// Should not panic
	m.SetActive(true)
}

func TestIntentState_StateProviderRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(IntentState{
		FlatNodes: make([]IntentFlatNode, 7),
		Cursor:    3,
	})
	s := m.IntentState()
	if len(s.FlatNodes) != 7 || s.Cursor != 3 {
		t.Errorf("IntentState() (StateProvider) returned %+v, want FlatNodes len=7 Cursor=3", s)
	}
}

func TestInit(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() should return nil, got %v", cmd)
	}
}

func TestView_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	// PR6 Step 3: View() 返回空 view（cmd/rnix wrapper 完成实际渲染）
	m := NewModel()
	v := m.View()
	_ = v // 仅验证不 panic + 类型正确（编译期保证）
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	m := NewModel()
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		t.Errorf("Update(WindowSizeMsg) should return nil cmd, got %v", cmd)
	}
	im, ok := newM.(*IntentModel)
	if !ok {
		t.Fatal("Update should return *IntentModel")
	}
	if im.width != 100 || im.height != 30 {
		t.Errorf("Width/Height not updated: got %d/%d, want 100/30", im.width, im.height)
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	t.Parallel()
	var m *IntentModel
	// Should not panic
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 100})
	if cmd != nil {
		t.Errorf("expected nil cmd on nil receiver, got %v", cmd)
	}
	if im, _ := newM.(*IntentModel); im != nil {
		t.Errorf("expected returned model to be nil pointer, got %v", im)
	}
}

func TestUpdate_UnknownMsg_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(IntentState{Cursor: 5})
	type customMsg struct{}
	newM, cmd := m.Update(customMsg{})
	if cmd != nil {
		t.Errorf("unknown msg should return nil cmd, got %v", cmd)
	}
	im, _ := newM.(*IntentModel)
	if im.State().Cursor != 5 {
		t.Error("unknown msg should not modify state")
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m.Name() != "Intent" {
		t.Errorf("Name() = %q, want %q", m.Name(), "Intent")
	}
}

func TestKeyLayer(t *testing.T) {
	t.Parallel()
	m := NewModel()
	l := m.KeyLayer()
	if l == nil {
		t.Fatal("KeyLayer() returned nil")
	}
	if l.Name != "Intent Pane" {
		t.Errorf("KeyLayer.Name = %q, want %q", l.Name, "Intent Pane")
	}
}

// TestOnSelectPID_PreservesState 验证 38-4 P1 契约：PID 切换**不**清空 state。
//
// 与 PR5 DetailModel 不同 — Intent tree 是跨进程的全局视图，PID 切换时数据应保持，
// TreeCollapsed 用户折叠状态也应跨切换稳定（38-4 P1 keyed by RootIntent）。
func TestOnSelectPID_PreservesState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	original := IntentState{
		Trees: []*ipc.IntentTreeWire{
			{ID: "t1", RootIntent: "root-a"},
		},
		FlatNodes:     make([]IntentFlatNode, 5),
		Cursor:        2,
		ScrollOffset:  1,
		TreeCollapsed: map[string]bool{"root-a": true},
	}
	m.SetState(original)
	cmd := m.OnSelectPID(types.PID(99))
	if cmd != nil {
		t.Errorf("OnSelectPID PR6 Step 3 stub should return nil cmd, got %v", cmd)
	}
	got := m.State()
	if len(got.Trees) != 1 || got.Trees[0].RootIntent != "root-a" {
		t.Error("Trees should be preserved across PID switch (38-4 P1 contract)")
	}
	if len(got.FlatNodes) != 5 {
		t.Error("FlatNodes should be preserved across PID switch")
	}
	if got.Cursor != 2 {
		t.Errorf("Cursor should be preserved (got %d)", got.Cursor)
	}
	if !got.TreeCollapsed["root-a"] {
		t.Error("TreeCollapsed should be preserved across PID switch (38-4 P1)")
	}
}

func TestOnSelectPID_NilSafe(t *testing.T) {
	t.Parallel()
	var m *IntentModel
	if cmd := m.OnSelectPID(types.PID(1)); cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
}

// TestOnTick_Noop 验证 OnTick 当前为 noop · IPC 由 cmd/rnix 端 dashboardTick 触发。
func TestOnTick_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(IntentState{Cursor: 7})
	cmd := m.OnTick(time.Now())
	if cmd != nil {
		t.Errorf("OnTick PR6 Step 3 stub should return nil cmd, got %v", cmd)
	}
	// state.Cursor 仍为 7（noop · 与 DetailModel.OnTick 不同 · DetailModel 会 Tick++）
	if m.State().Cursor != 7 {
		t.Errorf("OnTick should not modify state (noop), Cursor changed to %d", m.State().Cursor)
	}
}

func TestOnTick_NilSafe(t *testing.T) {
	t.Parallel()
	var m *IntentModel
	if cmd := m.OnTick(time.Now()); cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
}

func TestPaneModelInterface_CompileTimeAssertion(t *testing.T) {
	t.Parallel()
	// 编译期断言已在 model.go 声明：var _ dashboardmodel.PaneModel = (*IntentModel)(nil)
	// 此测试通过运行时类型 assertion 再次验证（防止 model.go 中的断言被误删）
	var pm dashboardmodel.PaneModel = NewModel()
	if pm.Name() != "Intent" {
		t.Errorf("PaneModel.Name() = %q, want Intent", pm.Name())
	}
	// 验证 KeyLayer 接口可调用
	if l := pm.KeyLayer(); l == nil {
		t.Error("PaneModel.KeyLayer() returned nil")
	}
}
