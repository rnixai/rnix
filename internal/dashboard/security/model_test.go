// Package security — model_test.go (Story 38-5 PR7 Step 3)
//
// SecurityModel 契约测试（与 PR6 IntentModel 同模式：跨 PID 全局视图 · OnSelectPID
// 不清空 state · OnTick noop）。
package security

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
	if s.ImmuneStatus != nil || s.ImmuneErr != nil || s.Alerts != nil ||
		s.Cursor != 0 || s.ScrollOffset != 0 {
		t.Errorf("expected zero-value state, got %+v", s)
	}
}

func TestState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *SecurityModel
	s := m.State()
	if s.ImmuneStatus != nil || s.Cursor != 0 {
		t.Errorf("expected zero state for nil receiver, got %+v", s)
	}
}

func TestSetState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	expected := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{},
		Alerts:       []ipc.AlertWire{{}, {}},
		Cursor:       1,
		ScrollOffset: 0,
	}
	m.SetState(expected)
	got := m.State()
	if got.ImmuneStatus == nil {
		t.Error("ImmuneStatus should be preserved")
	}
	if len(got.Alerts) != 2 {
		t.Errorf("Alerts len = %d, want 2", len(got.Alerts))
	}
	if got.Cursor != 1 {
		t.Errorf("Cursor = %d, want 1", got.Cursor)
	}
}

func TestSetState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *SecurityModel
	m.SetState(SecurityState{Cursor: 1})
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
	var m *SecurityModel
	m.SetActive(true)
}

func TestSecurityState_StateProviderRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(SecurityState{
		Alerts: make([]ipc.AlertWire, 4),
		Cursor: 2,
	})
	s := m.SecurityState()
	if len(s.Alerts) != 4 || s.Cursor != 2 {
		t.Errorf("SecurityState() (StateProvider) returned %+v, want Alerts len=4 Cursor=2", s)
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
	m := NewModel()
	v := m.View()
	_ = v
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	m := NewModel()
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil {
		t.Errorf("Update(WindowSizeMsg) should return nil cmd, got %v", cmd)
	}
	sm, ok := newM.(*SecurityModel)
	if !ok {
		t.Fatal("Update should return *SecurityModel")
	}
	if sm.width != 120 || sm.height != 40 {
		t.Errorf("Width/Height not updated: got %d/%d, want 120/40", sm.width, sm.height)
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	t.Parallel()
	var m *SecurityModel
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 100})
	if cmd != nil {
		t.Errorf("expected nil cmd on nil receiver, got %v", cmd)
	}
	if sm, _ := newM.(*SecurityModel); sm != nil {
		t.Errorf("expected nil pointer, got %v", sm)
	}
}

func TestUpdate_UnknownMsg_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(SecurityState{Cursor: 5})
	type customMsg struct{}
	newM, cmd := m.Update(customMsg{})
	if cmd != nil {
		t.Errorf("unknown msg should return nil cmd, got %v", cmd)
	}
	sm, _ := newM.(*SecurityModel)
	if sm.State().Cursor != 5 {
		t.Error("unknown msg should not modify state")
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m.Name() != "Security" {
		t.Errorf("Name() = %q, want %q", m.Name(), "Security")
	}
}

func TestKeyLayer(t *testing.T) {
	t.Parallel()
	m := NewModel()
	l := m.KeyLayer()
	if l == nil {
		t.Fatal("KeyLayer() returned nil")
	}
	if l.Name != "Security Pane" {
		t.Errorf("KeyLayer.Name = %q, want %q", l.Name, "Security Pane")
	}
}

// TestOnSelectPID_PreservesState 验证跨 PID 切换 state 完整保留（与 IntentModel 同模式 ·
// Security pane 是跨进程的全局视图 · 38-4 Alert Immune 路由不依赖 PID 局部 state）。
func TestOnSelectPID_PreservesState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	original := SecurityState{
		ImmuneStatus: &ipc.ImmuneStatusResponse{},
		Alerts:       make([]ipc.AlertWire, 3),
		Cursor:       1,
		ScrollOffset: 0,
	}
	m.SetState(original)
	cmd := m.OnSelectPID(types.PID(99))
	if cmd != nil {
		t.Errorf("OnSelectPID PR7 Step 3 stub should return nil cmd, got %v", cmd)
	}
	got := m.State()
	if got.ImmuneStatus == nil {
		t.Error("ImmuneStatus should be preserved across PID switch")
	}
	if len(got.Alerts) != 3 {
		t.Error("Alerts should be preserved across PID switch")
	}
	if got.Cursor != 1 {
		t.Errorf("Cursor should be preserved (got %d)", got.Cursor)
	}
}

func TestOnSelectPID_NilSafe(t *testing.T) {
	t.Parallel()
	var m *SecurityModel
	if cmd := m.OnSelectPID(types.PID(1)); cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
}

// TestOnTick_Noop 验证 OnTick 当前为 noop · IPC 由 cmd/rnix 端 dashboardTick 触发。
func TestOnTick_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(SecurityState{Cursor: 7})
	cmd := m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()})
	if cmd != nil {
		t.Errorf("OnTick stub should return nil cmd, got %v", cmd)
	}
	if m.State().Cursor != 7 {
		t.Errorf("OnTick should not modify state (noop), got %d", m.State().Cursor)
	}
}

func TestOnTick_NilSafe(t *testing.T) {
	t.Parallel()
	var m *SecurityModel
	if cmd := m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()}); cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
}

func TestPaneModelInterface_CompileTimeAssertion(t *testing.T) {
	t.Parallel()
	var pm dashboardmodel.PaneModel = NewModel()
	if pm.Name() != "Security" {
		t.Errorf("PaneModel.Name() = %q, want Security", pm.Name())
	}
	if l := pm.KeyLayer(); l == nil {
		t.Error("PaneModel.KeyLayer() returned nil")
	}
}
