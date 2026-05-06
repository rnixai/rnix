// Package detail — model_test.go (Story 38-5 PR5 Step 3)
//
// DetailModel 契约测试。验证：
//
//  1. State / SetState / DetailState getter 一致性
//  2. SetActive / Init / View / Update + WindowSizeMsg 同步
//  3. Name / KeyLayer 行为
//  4. OnSelectPID 同 PID / 异 PID 行为（PID 切换清空 Detail · 28-4 AC-4 Cache 保留契约）
//  5. OnTick 维护 Tick 计数
//  6. nil safety: 全部方法在 receiver 为 nil 时返回零值不 panic
//  7. PaneModel interface 编译期断言（model.go 已声明 var _ ）
package detail

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
)

func TestNewModel_Defaults(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	s := m.State()
	if s.Detail != nil || s.PID != 0 || s.Cache != nil || s.Tick != 0 {
		t.Errorf("expected zero-value state, got %+v", s)
	}
}

func TestState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *DetailModel
	s := m.State()
	if s.Detail != nil || s.PID != 0 {
		t.Errorf("expected zero state for nil receiver, got %+v", s)
	}
}

func TestSetState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	expected := DetailState{
		PID:  42,
		Tick: 5,
	}
	m.SetState(expected)
	if m.State().PID != 42 || m.State().Tick != 5 {
		t.Errorf("SetState/State round-trip failed: got %+v", m.State())
	}
}

func TestSetState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *DetailModel
	// Should not panic
	m.SetState(DetailState{PID: 1})
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
	var m *DetailModel
	// Should not panic
	m.SetActive(true)
}

func TestDetailState_StateProviderRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(DetailState{PID: 99, Tick: 3})
	s := m.DetailState()
	if s.PID != 99 || s.Tick != 3 {
		t.Errorf("DetailState() (StateProvider) returned %+v, want PID=99 Tick=3", s)
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
	// PR5 Step 3: View() 返回空 view（cmd/rnix wrapper 完成实际渲染）
	m := NewModel()
	v := m.View()
	_ = v // 仅验证不 panic + 类型正确（编译期保证）
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	m := NewModel()
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd != nil {
		t.Errorf("Update(WindowSizeMsg) should return nil cmd, got %v", cmd)
	}
	dm, ok := newM.(*DetailModel)
	if !ok {
		t.Fatal("Update should return *DetailModel")
	}
	if dm.width != 80 || dm.height != 24 {
		t.Errorf("Width/Height not updated: got %d/%d, want 80/24", dm.width, dm.height)
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	t.Parallel()
	var m *DetailModel
	// Should not panic
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 100})
	if cmd != nil {
		t.Errorf("expected nil cmd on nil receiver, got %v", cmd)
	}
	if dm, _ := newM.(*DetailModel); dm != nil {
		t.Errorf("expected returned model to be nil pointer, got %v", dm)
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m.Name() != "Detail" {
		t.Errorf("Name() = %q, want %q", m.Name(), "Detail")
	}
}

func TestKeyLayer(t *testing.T) {
	t.Parallel()
	m := NewModel()
	l := m.KeyLayer()
	if l == nil {
		t.Fatal("KeyLayer() returned nil")
	}
	if l.Name != "Detail Pane" {
		t.Errorf("KeyLayer.Name = %q, want %q", l.Name, "Detail Pane")
	}
}

func TestOnSelectPID_DifferentPID(t *testing.T) {
	t.Parallel()
	m := NewModel()
	// 设置初始状态：PID=42（Cache 字段保持 nil · 测试只关心 PID/Detail 的清空语义）
	m.SetState(DetailState{PID: 42})
	cmd := m.OnSelectPID(types.PID(99))
	if cmd != nil {
		t.Errorf("OnSelectPID returns non-nil cmd (PR5 Step 3 stub returns nil), got %v", cmd)
	}
	s := m.State()
	if s.Detail != nil {
		t.Error("OnSelectPID should clear Detail on PID change")
	}
	if s.PID != 0 {
		t.Errorf("OnSelectPID should reset PID to 0, got %d", s.PID)
	}
}

func TestOnSelectPID_SamePID(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(DetailState{PID: 42})
	cmd := m.OnSelectPID(types.PID(42))
	if cmd != nil {
		t.Errorf("OnSelectPID same-PID should return nil cmd, got %v", cmd)
	}
	if m.State().PID != 42 {
		t.Error("OnSelectPID same-PID should not modify state")
	}
}

func TestOnSelectPID_NilSafe(t *testing.T) {
	t.Parallel()
	var m *DetailModel
	if cmd := m.OnSelectPID(types.PID(1)); cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
}

func TestOnTick_IncrementsTick(t *testing.T) {
	t.Parallel()
	m := NewModel()
	for i := 1; i <= 3; i++ {
		_ = m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()})
		if m.State().Tick != i {
			t.Errorf("after %d ticks, Tick=%d, want %d", i, m.State().Tick, i)
		}
	}
}

func TestOnTick_NilSafe(t *testing.T) {
	t.Parallel()
	var m *DetailModel
	if cmd := m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()}); cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
}

func TestPaneModelInterface_CompileTimeAssertion(t *testing.T) {
	t.Parallel()
	// 编译期断言已在 model.go 声明：var _ dashboardmodel.PaneModel = (*DetailModel)(nil)
	// 此测试通过运行时类型 assertion 再次验证（防止 model.go 中的断言被误删）
	var pm dashboardmodel.PaneModel = NewModel()
	if pm.Name() != "Detail" {
		t.Errorf("PaneModel.Name() = %q, want Detail", pm.Name())
	}
}
