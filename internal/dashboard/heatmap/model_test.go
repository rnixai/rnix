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

// TestOnTick_NoIncrementsTickCount — Story 38-5 PR11 Step 4(b) Phase 3:
// HeatmapModel.OnTick 不再自增 m.state.TickCount（节流计数器由 cmd/rnix 端
// dashboardModel 维护并通过 ctx.TickCount 注入）。
func TestOnTick_NoIncrementsTickCount(t *testing.T) {
	t.Parallel()
	m := NewModel()
	for range 5 {
		_ = m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()})
	}
	if m.state.TickCount != 0 {
		t.Errorf("OnTick should not increment TickCount; got %d, want 0 (Phase 3 决策：节流计数器由 cmd/rnix 注入)", m.state.TickCount)
	}
}

// TestOnTick_NoFetch_WhenDisconnected — Story 38-5 PR11 Step 4(b) Phase 3:
// 未连接时不发 IPC fetch（与 cmd/rnix 端 dashboardTick 原行为字面等价）.
func TestOnTick_NoFetch_WhenDisconnected(t *testing.T) {
	t.Parallel()
	m := NewModel()
	cmd := m.OnTick(dashboardmodel.OnTickContext{
		Now:         time.Now(),
		Connected:   false,
		SocketPath:  "/tmp/rnix.sock",
		SelectedPID: 100,
		TickCount:   5,
	})
	if cmd != nil {
		t.Errorf("OnTick(disconnected) should return nil cmd")
	}
}

// TestOnTick_NoFetch_WhenSelectedProcessIsDead — Story 38-5 PR11 Step 4(b) Phase 3:
// 选中进程已 dead 时不发 fetch（与 cmd/rnix.isSelectedProcessDead 守卫等价）.
func TestOnTick_NoFetch_WhenSelectedProcessIsDead(t *testing.T) {
	t.Parallel()
	m := NewModel()
	cmd := m.OnTick(dashboardmodel.OnTickContext{
		Now:                   time.Now(),
		Connected:             true,
		SocketPath:            "/tmp/rnix.sock",
		SelectedPID:           100,
		SelectedProcessIsDead: true,
		TickCount:             5,
	})
	if cmd != nil {
		t.Errorf("OnTick(dead process) should return nil cmd")
	}
}

// TestOnTick_NoFetch_WhenTickCountNotMod5 — 5 秒一次节流（与 cmd/rnix 原行为等价）.
func TestOnTick_NoFetch_WhenTickCountNotMod5(t *testing.T) {
	t.Parallel()
	m := NewModel()
	for tick := 1; tick <= 4; tick++ {
		cmd := m.OnTick(dashboardmodel.OnTickContext{
			Now:         time.Now(),
			Connected:   true,
			SocketPath:  "/tmp/rnix.sock",
			SelectedPID: 100,
			TickCount:   tick,
		})
		if cmd != nil {
			t.Errorf("OnTick(TickCount=%d) should return nil (5 秒节流)", tick)
		}
	}
}

// TestOnTick_FetchTriggered_OnAllConditionsMet — 全条件满足时返回 fetch cmd.
func TestOnTick_FetchTriggered_OnAllConditionsMet(t *testing.T) {
	t.Parallel()
	m := NewModel()
	cmd := m.OnTick(dashboardmodel.OnTickContext{
		Now:                   time.Now(),
		Connected:             true,
		SocketPath:            "/tmp/rnix.sock",
		SelectedPID:           100,
		SelectedProcessIsDead: false,
		TickCount:             5,
	})
	if cmd == nil {
		t.Errorf("OnTick(all-conditions-met) should return non-nil cmd")
	}
}

// TestOnTick_NilSafe — receiver nil 时不 panic.
func TestOnTick_NilSafe(t *testing.T) {
	t.Parallel()
	var m *HeatmapModel
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil receiver OnTick panicked: %v", r)
		}
	}()
	if cmd := m.OnTick(dashboardmodel.OnTickContext{TickCount: 5}); cmd != nil {
		t.Errorf("nil OnTick should return nil cmd, got %v", cmd)
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
