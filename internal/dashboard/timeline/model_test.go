// Package timeline — model_test.go (Story 38-5 PR4 Step 3)
//
// TimelineModel 契约测试。验证：
//
//  1. tea.Model 接口（Init/Update/View）
//  2. PaneModel 接口（Name/KeyLayer/OnSelectPID/OnTick）
//  3. nil safety（所有方法在 receiver 为 nil 时不 panic）
//  4. WindowSizeMsg 同步 width/height
//  5. OnSelectPID 在同 PID 时 noop / 异 PID 时清空 step state
package timeline

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

func TestTimelineModel_PaneModelInterface(t *testing.T) {
	t.Parallel()
	// 编译期断言：TimelineModel 必须满足 PaneModel interface
	var _ dashboardmodel.PaneModel = (*TimelineModel)(nil)
	// 运行时断言：NewModel() 返回的实例可作为 PaneModel 使用
	var pm dashboardmodel.PaneModel = NewModel()
	// 用 Name() 验证（不能用 nil 检查 · NewModel 必返回非 nil）
	if pm.Name() != "Timeline" {
		t.Errorf("PaneModel.Name() = %q, want %q", pm.Name(), "Timeline")
	}
}

func TestTimelineModel_Name(t *testing.T) {
	t.Parallel()
	m := NewModel()
	if got := m.Name(); got != "Timeline" {
		t.Errorf("Name() = %q, want %q", got, "Timeline")
	}
}

func TestTimelineModel_KeyLayer(t *testing.T) {
	t.Parallel()
	m := NewModel()
	l := m.KeyLayer()
	if l == nil {
		t.Fatal("KeyLayer() returned nil")
	}
	if l.Name != "Timeline Pane" {
		t.Errorf("KeyLayer().Name = %q, want %q", l.Name, "Timeline Pane")
	}
}

func TestTimelineModel_StateRoundtrip(t *testing.T) {
	t.Parallel()
	m := NewModel()
	want := TimelineState{
		AttachedPID:  42,
		AttachedUUID: "uuid-42",
		StepCursor:   3,
		ExpandMode:   ExpandModeExpanded,
		SortAsc:      true,
	}
	m.SetState(want)
	got := m.State()
	if got.AttachedPID != want.AttachedPID || got.AttachedUUID != want.AttachedUUID ||
		got.StepCursor != want.StepCursor || got.ExpandMode != want.ExpandMode ||
		got.SortAsc != want.SortAsc {
		t.Errorf("State() roundtrip mismatch:\n  got=%+v\n  want=%+v", got, want)
	}
}

func TestTimelineModel_TimelineStateProvider(t *testing.T) {
	t.Parallel()
	m := NewModel()
	s := TimelineState{StepFilterMode: true, ExpandMode: ExpandModeErrorsOnly}
	m.SetState(s)

	// 编译期断言：TimelineModel 满足 StateProvider interface（让 KeyLayer ActiveModesFn 可读）
	var sp StateProvider = m
	got := sp.TimelineState()
	if !got.StepFilterMode || got.ExpandMode != ExpandModeErrorsOnly {
		t.Errorf("StateProvider.TimelineState() returned wrong values: %+v", got)
	}
}

func TestTimelineModel_SetActive(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetActive(true)
	if !m.active {
		t.Error("SetActive(true) did not set active flag")
	}
	m.SetActive(false)
	if m.active {
		t.Error("SetActive(false) did not clear active flag")
	}
}

func TestTimelineModel_Init(t *testing.T) {
	t.Parallel()
	m := NewModel()
	cmd := m.Init()
	if cmd != nil {
		t.Errorf("Init() returned non-nil cmd, want nil (Timeline pane 不主动发 IPC)")
	}
}

func TestTimelineModel_View(t *testing.T) {
	t.Parallel()
	m := NewModel()
	v := m.View()
	// 当前阶段 View() 返回空 tea.View（PR11 重构后才完整渲染）
	if v.Content != "" {
		t.Errorf("View().Content = %q, want empty (cmd/rnix wrapper renders · PR11 will fill this)", v.Content)
	}
}

func TestTimelineModel_UpdateWindowSize(t *testing.T) {
	t.Parallel()
	m := NewModel()
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if cmd != nil {
		t.Errorf("Update(WindowSizeMsg) returned non-nil cmd, want nil")
	}
	if m.width != 100 || m.height != 40 {
		t.Errorf("Update did not sync width/height: width=%d height=%d", m.width, m.height)
	}
}

func TestTimelineModel_OnSelectPID_SamePID_Noop(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		AttachedPID:  42,
		StepEntries:  []StepEntry{{Summary: ipc.StepSummaryWire{Step: 1}}},
		StepCursor:   5,
		ExpandMode:   ExpandModeExpanded,
	})
	cmd := m.OnSelectPID(types.PID(42))
	if cmd != nil {
		t.Errorf("OnSelectPID(same PID) returned non-nil cmd, want nil (noop)")
	}
	// state 应保持不变（noop）
	got := m.State()
	if len(got.StepEntries) != 1 || got.StepCursor != 5 || got.ExpandMode != ExpandModeExpanded {
		t.Errorf("OnSelectPID(same PID) modified state: %+v", got)
	}
}

func TestTimelineModel_OnSelectPID_DifferentPID_ClearsState(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetState(TimelineState{
		AttachedPID:  42,
		StepEntries:  []StepEntry{{Summary: ipc.StepSummaryWire{Step: 1}}},
		StepCursor:   5,
		StepScrollTop: 3,
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{1: nil},
		LastFetchedStep: 7,
		FetchingDetail: true,
		StepExpandedIdx: 2,
		ExpandedAggGroups: map[int]bool{1: true},
		ExpandMode: ExpandModeExpanded,
	})
	cmd := m.OnSelectPID(types.PID(99))
	if cmd != nil {
		t.Errorf("OnSelectPID(diff PID) returned non-nil cmd in Step 3 (IPC 由 cmd/rnix dashboardTick 触发)")
	}
	got := m.State()
	if got.AttachedPID != 99 {
		t.Errorf("AttachedPID = %d, want 99", got.AttachedPID)
	}
	if len(got.StepEntries) != 0 {
		t.Errorf("StepEntries not cleared, len=%d", len(got.StepEntries))
	}
	if got.StepCursor != 0 || got.StepScrollTop != 0 || got.LastFetchedStep != 0 {
		t.Errorf("step cursor/scroll/fetched not reset: cursor=%d scroll=%d fetched=%d",
			got.StepCursor, got.StepScrollTop, got.LastFetchedStep)
	}
	if got.FetchingDetail {
		t.Error("FetchingDetail not cleared")
	}
	if got.StepExpandedIdx != -1 {
		t.Errorf("StepExpandedIdx = %d, want -1", got.StepExpandedIdx)
	}
	if got.ExpandMode != ExpandModeCollapsed {
		t.Errorf("ExpandMode not reset to Collapsed, got %d (Story 36-4 行为)", got.ExpandMode)
	}
	if len(got.ExpandedAggGroups) != 0 {
		t.Errorf("ExpandedAggGroups not cleared, len=%d", len(got.ExpandedAggGroups))
	}
}

func TestTimelineModel_OnTick(t *testing.T) {
	t.Parallel()
	m := NewModel()
	cmd := m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()})
	if cmd != nil {
		t.Errorf("OnTick() returned non-nil cmd, want nil (IPC 由 cmd/rnix dashboardTick 触发)")
	}
}

func TestTimelineModel_NilSafety(t *testing.T) {
	t.Parallel()
	var m *TimelineModel
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	if got := m.State(); got.AttachedPID != 0 {
		t.Errorf("nil State() not zero value")
	}
	m.SetState(TimelineState{}) // noop
	if got := m.TimelineState(); got.AttachedPID != 0 {
		t.Errorf("nil TimelineState() not zero value")
	}
	m.SetActive(true) // noop
	if cmd := m.Init(); cmd != nil {
		t.Errorf("nil Init() returned non-nil")
	}
	if v := m.View(); v.Content != "" {
		t.Errorf("nil View() returned non-empty")
	}
	if _, cmd := m.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Errorf("nil Update() returned non-nil cmd")
	}
	if cmd := m.OnSelectPID(types.PID(1)); cmd != nil {
		t.Errorf("nil OnSelectPID() returned non-nil")
	}
	if cmd := m.OnTick(dashboardmodel.OnTickContext{Now: time.Now()}); cmd != nil {
		t.Errorf("nil OnTick() returned non-nil")
	}
}
