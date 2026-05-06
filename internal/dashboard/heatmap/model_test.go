// Package heatmap — model_test.go (Story 38-5 PR3 Step 3d)
//
// HeatmapModel 契约测试：State/SetState/SetActive/Init/View/Update/Name/KeyLayer/
// OnSelectPID/OnTick + nil safety + WindowSizeMsg 同步 + 编译期 PaneModel 接口断言。
package heatmap

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
)

func TestNewModel_ZeroState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	if m.state.PID != 0 || m.state.Profile != nil || m.state.Cursor != 0 {
		t.Errorf("NewModel state = %+v, want zero", m.state)
	}
}

func TestState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *HeatmapModel
	if got := m.State(); got.PID != 0 {
		t.Errorf("nil receiver State() = %+v, want zero", got)
	}
}

func TestSetState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	s := HeatmapState{PID: 42, Cursor: 3, Expanded: true}
	m.SetState(s)
	if got := m.State(); got.PID != 42 || got.Cursor != 3 || !got.Expanded {
		t.Errorf("SetState/State roundtrip = %+v, want %+v", got, s)
	}
	// nil safety
	var nilM *HeatmapModel
	nilM.SetState(s) // should not panic
}

func TestSetActive(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetActive(true)
	if !m.active {
		t.Error("SetActive(true) did not update active")
	}
	m.SetActive(false)
	if m.active {
		t.Error("SetActive(false) did not update active")
	}
	var nilM *HeatmapModel
	nilM.SetActive(true) // should not panic
}

func TestHeatmapState_StateProvider(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(HeatmapState{Expanded: true})
	if got := m.HeatmapState(); !got.Expanded {
		t.Errorf("HeatmapState() = %+v, want Expanded=true", got)
	}
	// 编译期断言
	var _ StateProvider = (*HeatmapModel)(nil)
}

func TestInit(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() returned non-nil cmd, want nil")
	}
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	m := NewModel()
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.width != 100 || m.height != 30 {
		t.Errorf("Update(WindowSizeMsg) → width=%d height=%d, want 100/30", m.width, m.height)
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	t.Parallel()
	var m *HeatmapModel
	if _, cmd := m.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Errorf("nil receiver Update returned non-nil cmd")
	}
}

func TestView(t *testing.T) {
	t.Parallel()
	m := NewModel()
	v := m.View()
	if v.Content != "" {
		t.Errorf("View().Content = %q, want empty (cmd/rnix wrapper renders)", v.Content)
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m.Name() != "Heatmap" {
		t.Errorf("Name() = %q, want %q", m.Name(), "Heatmap")
	}
}

func TestKeyLayer_NotNil(t *testing.T) {
	t.Parallel()
	m := NewModel()
	l := m.KeyLayer()
	if l == nil {
		t.Fatal("KeyLayer() returned nil")
	}
	if l.Name != "Heatmap Pane" {
		t.Errorf("KeyLayer.Name = %q, want %q", l.Name, "Heatmap Pane")
	}
}

func TestOnSelectPID(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(HeatmapState{PID: 1, Cursor: 5, Expanded: true})
	cmd := m.OnSelectPID(types.PID(2))
	if cmd != nil {
		t.Errorf("OnSelectPID returned non-nil cmd (current stub), got %v", cmd)
	}
	if m.state.PID != 2 {
		t.Errorf("OnSelectPID did not update PID: got %d, want 2", m.state.PID)
	}
	if m.state.Cursor != 0 || m.state.Expanded {
		t.Errorf("OnSelectPID did not reset state: cursor=%d expanded=%v", m.state.Cursor, m.state.Expanded)
	}
}

func TestOnSelectPID_SamePID(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(HeatmapState{PID: 5, Cursor: 3, Expanded: true})
	_ = m.OnSelectPID(types.PID(5))
	// 同 PID 不应清空 cursor/expanded
	if m.state.Cursor != 3 || !m.state.Expanded {
		t.Error("OnSelectPID(samePID) should not reset state")
	}
}

func TestOnTick(t *testing.T) {
	t.Parallel()
	m := NewModel()
	for range 5 {
		_ = m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()})
	}
	if m.state.TickCount != 5 {
		t.Errorf("OnTick called 5x, TickCount = %d, want 5", m.state.TickCount)
	}
}

func TestPaneModel_Interface(t *testing.T) {
	t.Parallel()
	// 编译期断言已在 model.go 顶部确认；此处运行时双保险
	m := NewModel()
	if m.Name() == "" || m.KeyLayer() == nil {
		t.Error("HeatmapModel does not satisfy PaneModel contract")
	}
}
