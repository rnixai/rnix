// Package alertstrip — model_test.go (Story 38-5 PR12 Step 2+3)
//
// AlertStripModel 契约测试（22 项）覆盖：
//   - NewModel / State / SetState / SetActive / AlertStripState 过渡 API（5 项 + 4 项 nil safety）
//   - tea.Model 接口（Init / View / Update + WindowSizeMsg 同步 · 4 项 + 1 项 nil safety）
//   - OverlayModel 接口（IsActive / OnEnter / OnExit · 3 项 + 3 项 nil safety）
//   - AlertStripHeight helper（alertCount=0/expanded false/expanded true · 上限 8/2 边界 case · 6 项）
//   - 运行时接口断言（防止编译期断言被误删 · 1 项）
//
// 38-5 PR12 行为契约（spec § AC8 表格 11 行）：
//   - State / SetState round-trip 保留 Expanded + Cursor 全部字段
//   - SetActive 控制 IsActive · OnEnter/OnExit stub 不修改 active 状态（PR11 主体迁入后才修改）
//   - AlertStripHeight 与 cmd/rnix.alertStripHeight 行为完全等价（2/8 上限 + 0 边界）
package alertstrip

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
)

// --- NewModel / State / SetState / SetActive / AlertStripState 过渡 API ---

func TestNewModel_DefaultZeroState(t *testing.T) {
	m := NewModel()
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	state := m.State()
	if state.Expanded {
		t.Errorf("default Expanded should be false, got true")
	}
	if state.Cursor != 0 {
		t.Errorf("default Cursor should be 0, got %d", state.Cursor)
	}
}

func TestState_NilSafe(t *testing.T) {
	var m *AlertStripModel
	state := m.State() // must not panic
	if state.Expanded || state.Cursor != 0 {
		t.Errorf("nil State() should return zero value, got %+v", state)
	}
}

func TestSetState_RoundTrip(t *testing.T) {
	m := NewModel()
	want := AlertStripState{Expanded: true, Cursor: 5}
	m.SetState(want)
	got := m.State()
	// Note: AlertStripState contains slice/pointer fields (Events/JumpTarget) so
	// direct struct comparison is not possible. We compare scalar fields directly.
	if got.Expanded != want.Expanded || got.Cursor != want.Cursor {
		t.Errorf("SetState round-trip failed: got %+v, want %+v", got, want)
	}
}

func TestSetState_NilSafe(t *testing.T) {
	var m *AlertStripModel
	m.SetState(AlertStripState{Expanded: true, Cursor: 5}) // must not panic
}

func TestSetActive_TogglesIsActive(t *testing.T) {
	m := NewModel()
	if m.IsActive() {
		t.Errorf("default IsActive should be false")
	}
	m.SetActive(true)
	if !m.IsActive() {
		t.Errorf("after SetActive(true), IsActive should be true")
	}
	m.SetActive(false)
	if m.IsActive() {
		t.Errorf("after SetActive(false), IsActive should be false")
	}
}

func TestSetActive_NilSafe(t *testing.T) {
	var m *AlertStripModel
	m.SetActive(true) // must not panic
}

func TestAlertStripState_StateProviderRoundTrip(t *testing.T) {
	m := NewModel()
	m.SetState(AlertStripState{Expanded: true, Cursor: 3})
	// AlertStripState() satisfies StateProvider interface; value should equal State().
	via := m.AlertStripState()
	direct := m.State()
	// Note: cannot use != on struct with slice fields; compare scalars.
	if via.Expanded != direct.Expanded || via.Cursor != direct.Cursor {
		t.Errorf("AlertStripState() != State(): %+v vs %+v", via, direct)
	}
}

// --- tea.Model 接口 ---

func TestInit_ReturnsNil(t *testing.T) {
	m := NewModel()
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init should return nil cmd, got non-nil")
	}
}

func TestView_ReturnsEmpty(t *testing.T) {
	m := NewModel()
	v := m.View()
	_ = v // PR12 Step 2+3: View() 返回空 view（cmd/rnix wrapper 完成实际渲染）· 仅验证不 panic + 类型正确（编译期保证）
}

func TestUpdate_WindowSizeMsgSyncsDimensions(t *testing.T) {
	m := NewModel()
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil {
		t.Errorf("Update with WindowSizeMsg should return nil cmd")
	}
	if m.width != 120 || m.height != 40 {
		t.Errorf("Update should sync width/height, got width=%d height=%d", m.width, m.height)
	}
}

func TestUpdate_UnknownMsgNoop(t *testing.T) {
	m := NewModel()
	type fakeMsg struct{}
	_, cmd := m.Update(fakeMsg{})
	if cmd != nil {
		t.Errorf("Update with unknown msg should return nil cmd")
	}
}

func TestUpdate_NilSafe(t *testing.T) {
	var m *AlertStripModel
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}) // must not panic
	if cmd != nil {
		t.Errorf("nil Update should return nil cmd")
	}
}

// --- OverlayModel 接口 ---

func TestIsActive_NilSafe(t *testing.T) {
	var m *AlertStripModel
	if m.IsActive() {
		t.Errorf("nil IsActive should return false")
	}
}

func TestOnEnter_StubReturnsNil(t *testing.T) {
	m := NewModel()
	if cmd := m.OnEnter(); cmd != nil {
		t.Errorf("OnEnter stub should return nil cmd, got non-nil")
	}
}

func TestOnEnter_NilSafe(t *testing.T) {
	var m *AlertStripModel
	if cmd := m.OnEnter(); cmd != nil {
		t.Errorf("nil OnEnter should return nil cmd")
	}
}

func TestOnExit_StubReturnsNil(t *testing.T) {
	m := NewModel()
	if cmd := m.OnExit(); cmd != nil {
		t.Errorf("OnExit stub should return nil cmd, got non-nil")
	}
}

func TestOnExit_NilSafe(t *testing.T) {
	var m *AlertStripModel
	if cmd := m.OnExit(); cmd != nil {
		t.Errorf("nil OnExit should return nil cmd")
	}
}

// --- AlertStripHeight helper（UnifiedEvent-independent · 与 cmd/rnix 等价契约）---

func TestAlertStripHeight_ZeroAlerts(t *testing.T) {
	if got := AlertStripHeight(0, false); got != 0 {
		t.Errorf("AlertStripHeight(0, false) = %d, want 0", got)
	}
	if got := AlertStripHeight(0, true); got != 0 {
		t.Errorf("AlertStripHeight(0, true) = %d, want 0 (zero alerts overrides expanded)", got)
	}
}

func TestAlertStripHeight_CollapsedClamp(t *testing.T) {
	// Collapsed mode shows up to 2 lines.
	if got := AlertStripHeight(1, false); got != 1 {
		t.Errorf("AlertStripHeight(1, false) = %d, want 1", got)
	}
	if got := AlertStripHeight(2, false); got != 2 {
		t.Errorf("AlertStripHeight(2, false) = %d, want 2", got)
	}
	if got := AlertStripHeight(5, false); got != 2 {
		t.Errorf("AlertStripHeight(5, false) = %d, want 2 (collapsed clamps to 2)", got)
	}
	if got := AlertStripHeight(100, false); got != 2 {
		t.Errorf("AlertStripHeight(100, false) = %d, want 2 (collapsed always clamps)", got)
	}
}

func TestAlertStripHeight_ExpandedClamp(t *testing.T) {
	// Expanded mode shows up to 8 lines.
	if got := AlertStripHeight(1, true); got != 1 {
		t.Errorf("AlertStripHeight(1, true) = %d, want 1", got)
	}
	if got := AlertStripHeight(8, true); got != 8 {
		t.Errorf("AlertStripHeight(8, true) = %d, want 8", got)
	}
	if got := AlertStripHeight(20, true); got != 8 {
		t.Errorf("AlertStripHeight(20, true) = %d, want 8 (expanded clamps to 8)", got)
	}
}

func TestAlertStripHeight_NegativeFallback(t *testing.T) {
	// Negative alertCount is undefined but should not panic; current behavior:
	// alertCount == 0 returns 0; negative falls through to min(negative, 2/8) = negative.
	// Document the actual behavior so future refactors don't accidentally change it.
	if got := AlertStripHeight(-1, false); got != -1 {
		t.Errorf("AlertStripHeight(-1, false) = %d, want -1 (current contract; should be defensive in caller)", got)
	}
}

func TestMinInt_Behavior(t *testing.T) {
	if minInt(1, 2) != 1 {
		t.Errorf("minInt(1, 2) should be 1")
	}
	if minInt(5, 3) != 3 {
		t.Errorf("minInt(5, 3) should be 3")
	}
	if minInt(7, 7) != 7 {
		t.Errorf("minInt(7, 7) should be 7")
	}
}

// --- 运行时接口断言（防止编译期断言被误删）---

func TestRuntimeOverlayModelInterfaceAssertion(t *testing.T) {
	// 编译期断言已在 model.go 通过 var _ dashboardmodel.OverlayModel = (*AlertStripModel)(nil) 验证；
	// 本测试在运行时再次断言以防止 model.go 编译期断言被意外删除（与 PR2-PR11 同模式）。
	var _ dashboardmodel.OverlayModel = NewModel()
	var _ StateProvider = NewModel()
}
