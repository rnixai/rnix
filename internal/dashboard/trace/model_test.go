// Package trace — model_test.go (Story 38-5 PR8 Step 3)
package trace

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
		t.Fatal("nil")
	}
	s := m.State()
	if s.Summaries != nil || s.Err != nil || s.Cursor != 0 ||
		s.ScrollOffset != 0 || s.ViewMode != 0 || s.SelectedTraceID != "" ||
		s.SelectedSpanTree != nil || s.SpanFlatNodes != nil ||
		s.SpanCursor != 0 || s.SpanScrollOffset != 0 {
		t.Errorf("expected zero state, got %+v", s)
	}
}

func TestState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TraceModel
	if s := m.State(); s.ViewMode != 0 {
		t.Errorf("expected zero, got %+v", s)
	}
}

func TestSetState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	expected := TraceState{
		Summaries:     []ipc.TraceSummaryWire{{TraceID: "t1"}},
		ViewMode:      1,
		SpanFlatNodes: make([]SpanFlatNode, 5),
	}
	m.SetState(expected)
	got := m.State()
	if len(got.Summaries) != 1 {
		t.Errorf("Summaries len = %d, want 1", len(got.Summaries))
	}
	if got.ViewMode != 1 {
		t.Errorf("ViewMode = %d, want 1", got.ViewMode)
	}
	if len(got.SpanFlatNodes) != 5 {
		t.Errorf("SpanFlatNodes len = %d, want 5", len(got.SpanFlatNodes))
	}
}

func TestSetState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TraceModel
	m.SetState(TraceState{ViewMode: 1})
}

func TestSetActive(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetActive(true)
	if !m.active {
		t.Error("flag not flipped")
	}
}

func TestSetActive_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TraceModel
	m.SetActive(true)
}

func TestTraceState_StateProviderRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TraceState{ViewMode: 1, Cursor: 3})
	s := m.TraceState()
	if s.ViewMode != 1 || s.Cursor != 3 {
		t.Errorf("got %+v", s)
	}
}

func TestInit(t *testing.T) {
	t.Parallel()
	if cmd := NewModel().Init(); cmd != nil {
		t.Errorf("Init should be nil, got %v", cmd)
	}
}

func TestView_Empty(t *testing.T) {
	t.Parallel()
	_ = NewModel().View()
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	m := NewModel()
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	tm, ok := newM.(*TraceModel)
	if !ok {
		t.Fatal("type")
	}
	if tm.width != 100 || tm.height != 30 {
		t.Errorf("got %d/%d", tm.width, tm.height)
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TraceModel
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 50})
	if cmd != nil {
		t.Errorf("expected nil cmd")
	}
	if tm, _ := newM.(*TraceModel); tm != nil {
		t.Errorf("expected nil pointer")
	}
}

func TestUpdate_UnknownMsg_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TraceState{ViewMode: 1})
	type customMsg struct{}
	newM, cmd := m.Update(customMsg{})
	if cmd != nil {
		t.Errorf("nil cmd")
	}
	tm, _ := newM.(*TraceModel)
	if tm.State().ViewMode != 1 {
		t.Error("state mutated")
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if NewModel().Name() != "Trace" {
		t.Errorf("Name should be Trace")
	}
}

func TestKeyLayer(t *testing.T) {
	t.Parallel()
	l := NewModel().KeyLayer()
	if l == nil || l.Name != "Trace Pane" {
		t.Errorf("KeyLayer.Name should be 'Trace Pane'")
	}
}

// 与 IntentModel/SecurityModel 同模式：跨 PID 切换 state 完整保留
func TestOnSelectPID_PreservesState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TraceState{
		Summaries:        []ipc.TraceSummaryWire{{TraceID: "t1"}},
		ViewMode:         1,
		SelectedTraceID:  "t1",
		SelectedSpanTree: &ipc.SpanTreeWire{},
		SpanFlatNodes:    make([]SpanFlatNode, 3),
		Cursor:           1,
	})
	cmd := m.OnSelectPID(types.PID(99))
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	got := m.State()
	if len(got.Summaries) != 1 {
		t.Error("Summaries should be preserved")
	}
	if got.ViewMode != 1 {
		t.Error("ViewMode should be preserved")
	}
	if got.SelectedTraceID != "t1" {
		t.Error("SelectedTraceID should be preserved")
	}
	if got.SelectedSpanTree == nil {
		t.Error("SelectedSpanTree should be preserved")
	}
	if len(got.SpanFlatNodes) != 3 {
		t.Error("SpanFlatNodes should be preserved (38-4 waterfall data survives PID switch)")
	}
}

func TestOnSelectPID_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TraceModel
	if cmd := m.OnSelectPID(types.PID(1)); cmd != nil {
		t.Errorf("expected nil")
	}
}

func TestOnTick_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TraceState{Cursor: 5})
	cmd := m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()})
	if cmd != nil {
		t.Errorf("nil cmd")
	}
	if m.State().Cursor != 5 {
		t.Error("state mutated")
	}
}

func TestOnTick_NilSafe(t *testing.T) {
	t.Parallel()
	var m *TraceModel
	if cmd := m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()}); cmd != nil {
		t.Errorf("expected nil")
	}
}

func TestPaneModelInterface_Runtime(t *testing.T) {
	t.Parallel()
	var pm dashboardmodel.PaneModel = NewModel()
	if pm.Name() != "Trace" {
		t.Errorf("got %q", pm.Name())
	}
	if pm.KeyLayer() == nil {
		t.Error("KeyLayer nil")
	}
}
