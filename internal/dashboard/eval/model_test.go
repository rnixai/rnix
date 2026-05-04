// Package eval — model_test.go (Story 38-5 PR9 Step 3)
package eval

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

func TestNewModel_Defaults(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if m == nil {
		t.Fatal("nil")
	}
	s := m.State()
	if s.SubView != 0 || s.Reputations != nil || s.Topology != nil || s.Synergies != nil {
		t.Errorf("expected zero state, got %+v", s)
	}
}

func TestState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *EvalModel
	if s := m.State(); s.SubView != 0 {
		t.Errorf("expected zero, got %+v", s)
	}
}

func TestSetState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	expected := EvalState{
		SubView:     2,
		Reputations: []kernel.ReputationSummary{{}, {}},
		Topology:    &ipc.TopologyQueryResponse{},
		Synergies:   []kernel.ComboSummary{{}},
	}
	m.SetState(expected)
	got := m.State()
	if got.SubView != 2 {
		t.Errorf("SubView = %d, want 2", got.SubView)
	}
	if len(got.Reputations) != 2 {
		t.Errorf("Reputations len = %d, want 2", len(got.Reputations))
	}
	if got.Topology == nil {
		t.Error("Topology should be preserved")
	}
	if len(got.Synergies) != 1 {
		t.Errorf("Synergies len = %d, want 1", len(got.Synergies))
	}
}

func TestSetState_NilSafe(t *testing.T) {
	t.Parallel()
	var m *EvalModel
	m.SetState(EvalState{SubView: 1})
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
	var m *EvalModel
	m.SetActive(true)
}

func TestEvalState_StateProviderRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(EvalState{SubView: 2, RepCursor: 5, SynCursor: 3})
	s := m.EvalState()
	if s.SubView != 2 || s.RepCursor != 5 || s.SynCursor != 3 {
		t.Errorf("got %+v", s)
	}
}

func TestInit(t *testing.T) {
	t.Parallel()
	if cmd := NewModel().Init(); cmd != nil {
		t.Errorf("Init nil")
	}
}

func TestView_Empty(t *testing.T) {
	t.Parallel()
	_ = NewModel().View()
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	m := NewModel()
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd != nil {
		t.Errorf("nil cmd")
	}
	em, ok := newM.(*EvalModel)
	if !ok {
		t.Fatal("type")
	}
	if em.width != 80 || em.height != 24 {
		t.Errorf("got %d/%d", em.width, em.height)
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	t.Parallel()
	var m *EvalModel
	newM, cmd := m.Update(tea.WindowSizeMsg{Width: 50})
	if cmd != nil {
		t.Errorf("nil cmd")
	}
	if em, _ := newM.(*EvalModel); em != nil {
		t.Errorf("nil pointer")
	}
}

func TestUpdate_UnknownMsg_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(EvalState{SubView: 1})
	type customMsg struct{}
	newM, cmd := m.Update(customMsg{})
	if cmd != nil {
		t.Errorf("nil cmd")
	}
	em, _ := newM.(*EvalModel)
	if em.State().SubView != 1 {
		t.Error("state mutated")
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	if NewModel().Name() != "Eval" {
		t.Errorf("Name should be Eval")
	}
}

func TestKeyLayer(t *testing.T) {
	t.Parallel()
	l := NewModel().KeyLayer()
	if l == nil || l.Name != "Eval Pane" {
		t.Errorf("KeyLayer.Name should be 'Eval Pane'")
	}
}

// 跨 PID 全局视图：state 完整保留（含 38-4 evalScoreColorStyle 数据）
func TestOnSelectPID_PreservesState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(EvalState{
		SubView:     2,
		Reputations: []kernel.ReputationSummary{{}, {}, {}},
		Topology:    &ipc.TopologyQueryResponse{},
		Synergies:   []kernel.ComboSummary{{}, {}},
		RepCursor:   1,
		SynCursor:   0,
	})
	cmd := m.OnSelectPID(types.PID(99))
	if cmd != nil {
		t.Errorf("nil cmd")
	}
	got := m.State()
	if got.SubView != 2 {
		t.Error("SubView should be preserved across PID switch")
	}
	if len(got.Reputations) != 3 {
		t.Error("Reputations should be preserved (38-4 evalScoreColorStyle data)")
	}
	if got.Topology == nil {
		t.Error("Topology should be preserved")
	}
	if len(got.Synergies) != 2 {
		t.Error("Synergies should be preserved (38-4 VS SOLO + Recommended)")
	}
	if got.RepCursor != 1 {
		t.Error("RepCursor should be preserved")
	}
}

func TestOnSelectPID_NilSafe(t *testing.T) {
	t.Parallel()
	var m *EvalModel
	if cmd := m.OnSelectPID(types.PID(1)); cmd != nil {
		t.Errorf("nil cmd")
	}
}

func TestOnTick_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(EvalState{SubView: 1})
	cmd := m.OnTick(time.Now())
	if cmd != nil {
		t.Errorf("nil cmd")
	}
	if m.State().SubView != 1 {
		t.Error("state mutated")
	}
}

func TestOnTick_NilSafe(t *testing.T) {
	t.Parallel()
	var m *EvalModel
	if cmd := m.OnTick(time.Now()); cmd != nil {
		t.Errorf("nil cmd")
	}
}

func TestPaneModelInterface_Runtime(t *testing.T) {
	t.Parallel()
	var pm dashboardmodel.PaneModel = NewModel()
	if pm.Name() != "Eval" {
		t.Errorf("got %q", pm.Name())
	}
	if pm.KeyLayer() == nil {
		t.Error("KeyLayer nil")
	}
}
