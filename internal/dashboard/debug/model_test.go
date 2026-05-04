// Package debug — model_test.go (Story 38-5 PR11 Step 3)
package debug

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
)

func TestNewModel_ZeroState(t *testing.T) {
	m := NewModel()
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	s := m.State()
	if s.Mode || s.ShowStrace || s.AutoScroll {
		t.Errorf("expected zero state bools, got Mode=%v ShowStrace=%v AutoScroll=%v", s.Mode, s.ShowStrace, s.AutoScroll)
	}
	if s.AttachedPID != 0 || s.Cursor != 0 || s.ScrollTop != 0 {
		t.Errorf("expected zero numeric state, got %+v", s)
	}
}

func TestSetState_RoundTrip(t *testing.T) {
	m := NewModel()
	want := DebugState{
		Mode:          true,
		ShowStrace:    true,
		AutoScroll:    true,
		AttachedPID:   types.PID(42),
		HistWatermark: 12345,
		ScrollTop:     5,
		Cursor:        7,
	}
	m.SetState(want)
	got := m.State()
	if got.Mode != want.Mode || got.ShowStrace != want.ShowStrace || got.AutoScroll != want.AutoScroll {
		t.Errorf("bool fields lost:\n got: %+v\nwant: %+v", got, want)
	}
	if got.AttachedPID != want.AttachedPID || got.HistWatermark != want.HistWatermark {
		t.Errorf("pid/watermark fields lost:\n got: %+v\nwant: %+v", got, want)
	}
	if got.ScrollTop != want.ScrollTop || got.Cursor != want.Cursor {
		t.Errorf("scroll/cursor fields lost:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestSetActive_RoundTrip(t *testing.T) {
	m := NewModel()
	if m.IsActive() {
		t.Error("default IsActive should be false")
	}
	m.SetActive(true)
	if !m.IsActive() {
		t.Error("SetActive(true) failed")
	}
	m.SetActive(false)
	if m.IsActive() {
		t.Error("SetActive(false) failed")
	}
}

func TestDebugState_StateProviderRoundTrip(t *testing.T) {
	m := NewModel()
	want := DebugState{Mode: true, ShowStrace: true, AttachedPID: types.PID(99)}
	m.SetState(want)
	got := m.DebugState()
	if got.Mode != want.Mode || got.ShowStrace != want.ShowStrace || got.AttachedPID != want.AttachedPID {
		t.Errorf("StateProvider round-trip broken")
	}
}

// nil safety

func TestState_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *DebugModel
	got := m.State()
	if got.Mode || got.AttachedPID != 0 {
		t.Errorf("nil State should return zero, got %+v", got)
	}
}

func TestSetState_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *DebugModel
	m.SetState(DebugState{Mode: true})
}

func TestSetActive_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *DebugModel
	m.SetActive(true)
}

func TestIsActive_NilSafe(t *testing.T) {
	var m *DebugModel
	if got := m.IsActive(); got {
		t.Errorf("nil IsActive should be false, got %v", got)
	}
}

// tea.Model

func TestInit_ReturnsNilCmd(t *testing.T) {
	m := NewModel()
	if cmd := m.Init(); cmd != nil {
		t.Error("Init should return nil cmd in PR11 Step 3 stage")
	}
}

func TestView_ReturnsEmpty(t *testing.T) {
	m := NewModel()
	v := m.View()
	if v.Content != "" {
		t.Errorf("View should return empty in PR11 Step 3 stage; got %q", v.Content)
	}
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	m := NewModel()
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if updated != m {
		t.Error("Update should return same receiver pointer")
	}
	if cmd != nil {
		t.Error("Update should return nil cmd for WindowSizeMsg")
	}
	if m.width != 100 || m.height != 30 {
		t.Errorf("WindowSizeMsg not synced; got width=%d height=%d", m.width, m.height)
	}
}

func TestUpdate_UnknownMsg_NoOp(t *testing.T) {
	m := NewModel()
	type customMsg struct{}
	updated, cmd := m.Update(customMsg{})
	if cmd != nil {
		t.Error("Update should noop for unknown msg")
	}
	if updated != m {
		t.Error("Update should return same receiver")
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *DebugModel
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30}); cmd != nil {
		t.Error("nil Update should return nil cmd")
	}
}

// OverlayModel lifecycle

func TestOverlayLifecycle_OrderEnterActiveExit(t *testing.T) {
	m := NewModel()
	if m.IsActive() {
		t.Error("initial IsActive should be false")
	}
	if cmd := m.OnEnter(); cmd != nil {
		t.Error("OnEnter should return nil cmd in PR11 Step 3 stub")
	}
	m.SetActive(true)
	if !m.IsActive() {
		t.Error("IsActive should be true after SetActive")
	}
	if cmd := m.OnExit(); cmd != nil {
		t.Error("OnExit should return nil cmd in PR11 Step 3 stub")
	}
	m.SetActive(false)
	if m.IsActive() {
		t.Error("IsActive should be false after SetActive(false)")
	}
}

func TestOnEnter_NilSafe(t *testing.T) {
	var m *DebugModel
	if cmd := m.OnEnter(); cmd != nil {
		t.Error("nil OnEnter should return nil cmd")
	}
}

func TestOnExit_NilSafe(t *testing.T) {
	var m *DebugModel
	if cmd := m.OnExit(); cmd != nil {
		t.Error("nil OnExit should return nil cmd")
	}
}

// Interface assertions

func TestRuntimeOverlayModelAssertion(t *testing.T) {
	m := NewModel()
	var _ dashboardmodel.OverlayModel = m
	var iface dashboardmodel.OverlayModel = m
	_ = iface.IsActive()
	_ = iface.OnEnter()
	_ = iface.OnExit()
}

func TestStateProviderInterfaceAssertion(t *testing.T) {
	m := NewModel()
	var _ StateProvider = m
}

// 34.6 strace fusion 行为契约保留显式测试 — 防止 DebugState 字段被误删。
func TestSetState_PreservesStraceFields(t *testing.T) {
	m := NewModel()
	want := DebugState{
		Mode:          true,
		ShowStrace:    true,
		AutoScroll:    true,
		AttachedPID:   types.PID(42),
		AutoReloaded:  true,
		HistWatermark: 999,
		Cursor:        3,
		ScrollTop:     1,
	}
	m.SetState(want)
	got := m.State()
	if got.AutoReloaded != true || got.HistWatermark != 999 {
		t.Error("strace history fields lost — Story 34.6 contract violation")
	}
	if got.AutoScroll != true || got.ShowStrace != true {
		t.Error("strace UI fields lost")
	}
}
