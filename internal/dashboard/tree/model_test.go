// Package tree — model_test.go (Story 38-5 PR2 Step 3d)
//
// TreeModel 的 PaneModel interface 行为契约测试。覆盖：
//   - 编译期 interface 满足（已通过 var _ dashboardmodel.PaneModel = (*TreeModel)(nil)）
//   - State / SetState / TreeState getter 一致性
//   - Init / View / Name / KeyLayer / OnSelectPID / OnTick 行为
//   - nil receiver 安全（不 panic 返回零值）
//   - WindowSizeMsg → width/height 同步
package tree

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
)

func TestTreeModel_NewModel_ReturnsZeroState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	got := m.State()
	if got.Cursor != 0 || got.Offset != 0 || got.SortMode != 0 || got.SortAsc != false {
		t.Errorf("State() initial values = %+v, want zero", got)
	}
	if len(got.Rows) != 0 {
		t.Errorf("State().Rows len = %d, want 0", len(got.Rows))
	}
}

func TestTreeModel_SetStateAndState_RoundTrip(t *testing.T) {
	t.Parallel()
	m := NewModel()
	want := TreeState{
		Cursor:      5,
		Offset:      2,
		SortMode:    2,
		SortAsc:     true,
		SearchQuery: "init",
	}
	m.SetState(want)
	got := m.State()
	if got.Cursor != want.Cursor || got.Offset != want.Offset ||
		got.SortMode != want.SortMode || got.SortAsc != want.SortAsc ||
		got.SearchQuery != want.SearchQuery {
		t.Errorf("State() = %+v, want %+v", got, want)
	}
}

func TestTreeModel_TreeStateMatchesState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TreeState{Cursor: 3, SortMode: 1})
	a := m.State()
	b := m.TreeState()
	if a.Cursor != b.Cursor || a.SortMode != b.SortMode || a.SortAsc != b.SortAsc {
		t.Errorf("State() and TreeState() should return identical TreeState (StateProvider contract): %+v vs %+v", a, b)
	}
}

func TestTreeModel_NilReceiver_Safe(t *testing.T) {
	t.Parallel()
	var m *TreeModel // nil
	// State 应返回零值
	if got := m.State(); got.Cursor != 0 || got.Rows != nil {
		t.Errorf("nil.State() = %+v, want zero", got)
	}
	// TreeState 应返回零值
	if got := m.TreeState(); got.Cursor != 0 {
		t.Errorf("nil.TreeState() = %+v, want zero", got)
	}
	// Init 应返回 nil（不 panic）
	if cmd := m.Init(); cmd != nil {
		t.Errorf("nil.Init() should return nil cmd, got %v", cmd)
	}
	// SetState 应是 no-op（不 panic）
	m.SetState(TreeState{Cursor: 5})
	// SetActive 应是 no-op
	m.SetActive(true)
	// Update 不应 panic
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	if cmd != nil {
		t.Errorf("nil.Update() should return nil cmd")
	}
	// View 不应 panic（默认 zero View）
	_ = m.View()
	// OnSelectPID 应返回 nil cmd
	if cmd := m.OnSelectPID(types.PID(42)); cmd != nil {
		t.Errorf("nil.OnSelectPID() should return nil cmd")
	}
	// OnTick 应返回 nil cmd
	if cmd := m.OnTick(time.Now()); cmd != nil {
		t.Errorf("nil.OnTick() should return nil cmd")
	}
}

func TestTreeModel_Name(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if got := m.Name(); got != "Tree" {
		t.Errorf("Name() = %q, want %q", got, "Tree")
	}
}

func TestTreeModel_KeyLayer_NotNil(t *testing.T) {
	t.Parallel()
	m := NewModel()
	l := m.KeyLayer()
	if l == nil {
		t.Fatal("KeyLayer() returned nil")
	}
	if l.Name != "Tree Pane" {
		t.Errorf("KeyLayer().Name = %q, want %q", l.Name, "Tree Pane")
	}
	// Docs 应包含 38-1 落地的 5 个键
	for _, k := range []string{"K", "s", "o", "enter", "/"} {
		if _, ok := l.Docs[k]; !ok {
			t.Errorf("KeyLayer().Docs missing key %q", k)
		}
	}
}

func TestTreeModel_Init_ReturnsNil(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil (PR2 Step 3c stub)", cmd)
	}
}

func TestTreeModel_View_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	m := NewModel()
	v := m.View()
	// PR2 Step 3c 阶段 View() 返回空 view（实际渲染由 cmd/rnix wrapper 完成）。
	// 此处不强检查 v 的内部字段，仅断言不 panic + 返回值合法 tea.View 类型。
	_ = v
}

func TestTreeModel_Update_WindowSize_SyncsDimensions(t *testing.T) {
	t.Parallel()
	m := NewModel()
	model, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil {
		t.Errorf("Update(WindowSizeMsg) should return nil cmd, got %v", cmd)
	}
	tm, ok := model.(*TreeModel)
	if !ok {
		t.Fatalf("Update returned non-*TreeModel type: %T", model)
	}
	if tm.width != 120 || tm.height != 40 {
		t.Errorf("after WindowSizeMsg width=%d height=%d, want 120/40", tm.width, tm.height)
	}
}

func TestTreeModel_Update_OtherMsg_NoChange(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TreeState{Cursor: 5})
	original := m.State()
	_, cmd := m.Update("not-a-window-size-msg")
	if cmd != nil {
		t.Errorf("Update(non-WindowSizeMsg) should return nil cmd")
	}
	if m.State().Cursor != original.Cursor {
		t.Errorf("Update should not modify state for non-WindowSizeMsg")
	}
}

func TestTreeModel_OnSelectPID_ReturnsNilStub(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if cmd := m.OnSelectPID(types.PID(99)); cmd != nil {
		t.Errorf("OnSelectPID() = %v, want nil (PR2 Step 3c stub · cmd/rnix 端继续处理)", cmd)
	}
}

func TestTreeModel_OnTick_ReturnsNilStub(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if cmd := m.OnTick(time.Now()); cmd != nil {
		t.Errorf("OnTick() = %v, want nil (PR2 Step 3c stub · cmd/rnix 端 dashboardTick 继续处理)", cmd)
	}
}

func TestTreeModel_SetActive_PersistsValue(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetActive(true)
	if !m.active {
		t.Errorf("SetActive(true) should set m.active = true")
	}
	m.SetActive(false)
	if m.active {
		t.Errorf("SetActive(false) should set m.active = false")
	}
}
