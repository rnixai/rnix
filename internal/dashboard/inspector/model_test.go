// Package inspector — model_test.go (Story 38-5 PR10 Step 3)
//
// InspectorModel 行为契约测试 · 与 PR2-PR9 同模式（nil safety + 编译期断言 + 完整生命周期）。
package inspector

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
)

// --- 构造 + State / SetState API ---

func TestNewModel_ZeroState(t *testing.T) {
	m := NewModel()
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	s := m.State()
	if s.PID != 0 || s.UUID != "" || s.Step != 0 || s.Lens != LensConversation {
		t.Errorf("expected zero state, got %+v", s)
	}
	if s.DiffMode || s.FollowLive {
		t.Error("DiffMode/FollowLive should be false in zero state")
	}
}

func TestSetState_RoundTrip(t *testing.T) {
	m := NewModel()
	want := InspectorState{
		PID:        types.PID(42),
		UUID:       "abc",
		Step:       3,
		StepMax:    10,
		Lens:       LensToolIO,
		DiffMode:   true,
		DiffBase:   1,
		FollowLive: true,
		FollowGen:  5,
	}
	m.SetState(want)
	got := m.State()
	// Cannot use == because InspectorState contains slices/maps; compare scalar fields explicitly.
	if got.PID != want.PID || got.UUID != want.UUID || got.Step != want.Step || got.StepMax != want.StepMax {
		t.Errorf("scalar fields lost:\n got: %+v\nwant: %+v", got, want)
	}
	if got.Lens != want.Lens || got.DiffMode != want.DiffMode || got.DiffBase != want.DiffBase {
		t.Errorf("lens/diff fields lost:\n got: %+v\nwant: %+v", got, want)
	}
	if got.FollowLive != want.FollowLive || got.FollowGen != want.FollowGen {
		t.Errorf("follow fields lost:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestSetActive_RoundTrip(t *testing.T) {
	m := NewModel()
	if m.IsActive() {
		t.Error("default IsActive should be false")
	}
	m.SetActive(true)
	if !m.IsActive() {
		t.Error("IsActive should be true after SetActive(true)")
	}
	m.SetActive(false)
	if m.IsActive() {
		t.Error("IsActive should be false after SetActive(false)")
	}
}

func TestInspectorState_StateProviderRoundTrip(t *testing.T) {
	m := NewModel()
	want := InspectorState{PID: types.PID(7), Step: 2, Lens: LensRawJSON}
	m.SetState(want)
	got := m.InspectorState()
	if got.PID != want.PID || got.Step != want.Step || got.Lens != want.Lens {
		t.Errorf("StateProvider.InspectorState() round-trip broken:\n got: %+v\nwant: %+v", got, want)
	}
}

// --- nil safety ---

func TestState_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *InspectorModel
	got := m.State()
	if got.PID != 0 || got.Step != 0 || got.Lens != LensConversation {
		t.Errorf("nil State should return zero value, got %+v", got)
	}
}

func TestSetState_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *InspectorModel
	m.SetState(InspectorState{Step: 99})
}

func TestSetActive_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *InspectorModel
	m.SetActive(true)
}

func TestIsActive_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *InspectorModel
	if got := m.IsActive(); got {
		t.Errorf("nil IsActive should be false, got %v", got)
	}
}

// --- tea.Model interface ---

func TestInit_ReturnsNilCmd(t *testing.T) {
	m := NewModel()
	if cmd := m.Init(); cmd != nil {
		t.Error("Init should return nil cmd in PR10 stage")
	}
}

func TestView_ReturnsEmpty(t *testing.T) {
	m := NewModel()
	v := m.View()
	if v.Content != "" {
		t.Errorf("View should return empty in PR10 stage; got %q", v.Content)
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
	var m *InspectorModel
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30}); cmd != nil {
		t.Error("nil Update should return nil cmd")
	}
}

// --- OverlayModel interface (生命周期) ---

func TestOverlayLifecycle_OrderEnterActiveExit(t *testing.T) {
	m := NewModel()
	// 默认 inactive
	if m.IsActive() {
		t.Error("initial IsActive should be false")
	}
	// OnEnter（当前阶段 stub · 不会自动激活 · cmd/rnix 端 enterStepInspector 显式调 SetActive）
	if cmd := m.OnEnter(); cmd != nil {
		t.Error("OnEnter should return nil cmd in PR10 stub stage")
	}
	// 显式激活
	m.SetActive(true)
	if !m.IsActive() {
		t.Error("IsActive should be true after SetActive")
	}
	// OnExit
	if cmd := m.OnExit(); cmd != nil {
		t.Error("OnExit should return nil cmd in PR10 stub stage")
	}
	// 显式退出
	m.SetActive(false)
	if m.IsActive() {
		t.Error("IsActive should be false after SetActive(false)")
	}
}

func TestOnEnter_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *InspectorModel
	if cmd := m.OnEnter(); cmd != nil {
		t.Error("nil OnEnter should return nil cmd")
	}
}

func TestOnExit_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panicked: %v", r)
		}
	}()
	var m *InspectorModel
	if cmd := m.OnExit(); cmd != nil {
		t.Error("nil OnExit should return nil cmd")
	}
}

// --- 编译期 + 运行时接口断言 ---

// TestRuntimeOverlayModelAssertion — 防止 model.go 编译期断言被误删（runtime 层再次验证）。
func TestRuntimeOverlayModelAssertion(t *testing.T) {
	m := NewModel()
	var _ dashboardmodel.OverlayModel = m
	// IsActive / OnEnter / OnExit 通过接口调用一次（防止 method signature 漂移）
	var iface dashboardmodel.OverlayModel = m
	_ = iface.IsActive()
	_ = iface.OnEnter()
	_ = iface.OnExit()
}

// TestStateProviderInterfaceAssertion — InspectorModel 满足 StateProvider · 用于
// KeyLayer 的 ActiveModesFn 通过 ctx.(StateProvider) 路径读取 state。
func TestStateProviderInterfaceAssertion(t *testing.T) {
	m := NewModel()
	var _ StateProvider = m
}

// --- 38-3 / 36-6 行为契约保留显式测试 ---

// TestSetState_PreservesAllFields — 确保所有 25 字段在 SetState/State round-trip
// 后保持一致（包含 Diff mode 7 字段 + Follow Live 2 字段 + 38-3 AC#7/#8 lens marks
// + byte search · 防止 InspectorState struct 字段被误删）。
func TestSetState_PreservesAllFields(t *testing.T) {
	m := NewModel()
	want := InspectorState{
		PID:              types.PID(42),
		UUID:             "uuid-test",
		Step:             5,
		StepMax:          10,
		PrevStep:         4,
		CurDetailStep:    5,
		Lens:             LensRawJSON,
		PrevMode:         3,
		Fetching:         true,
		SystemExpanded:   true,
		DiffMode:         true,
		DiffBase:         2,
		DiffDelta:        3,
		DiffUnfolded:     map[int]bool{1: true, 2: true},
		DiffPicker:       true,
		DiffPickerCursor: 1,
		DiffDdDeadline:   time.Unix(1700000000, 0),
		FollowLive:       true,
		FollowGen:        7,
	}
	want.DiffLensMarks[LensConversation] = true
	want.DiffLensMarks[LensRawJSON] = true
	want.SearchPos = []SearchMatchPos{{LineIdx: 0, ByteStart: 0, ByteEnd: 4}}

	m.SetState(want)
	got := m.State()

	if got.PID != want.PID || got.UUID != want.UUID || got.Step != want.Step {
		t.Error("core step state lost")
	}
	if got.Lens != want.Lens || got.PrevMode != want.PrevMode {
		t.Error("lens / prev mode lost")
	}
	if got.DiffMode != want.DiffMode || got.DiffBase != want.DiffBase || got.DiffDelta != want.DiffDelta {
		t.Error("diff mode fields lost")
	}
	if got.FollowLive != want.FollowLive || got.FollowGen != want.FollowGen {
		t.Error("follow live fields lost")
	}
	if got.DiffLensMarks[LensConversation] != true || got.DiffLensMarks[LensRawJSON] != true {
		t.Error("DiffLensMarks (38-3 AC#7) lost")
	}
	if len(got.SearchPos) != 1 || got.SearchPos[0].ByteEnd != 4 {
		t.Error("SearchPos (38-3 AC#8) lost")
	}
	if len(got.DiffUnfolded) != 2 {
		t.Error("DiffUnfolded map lost")
	}
}
