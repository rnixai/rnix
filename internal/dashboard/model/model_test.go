// Package model — model_test.go (Story 38-5 PR1 contract tests)
//
// 本测试文件强制 PaneModel / OverlayModel / PaneState 的契约：
//   - TestPaneModel_NilSafety: nil PaneModel 调用各 hook 不 panic（model.go godoc 契约）
//   - TestOverlayModel_LifecycleOrder: OnEnter → IsActive==true → OnExit → IsActive==false
//   - TestPaneState_DefaultValues: 零值 PaneState 字段语义符合 spec § 02 line 232-248
package model

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// fakePaneModel 用于契约测试：实现 PaneModel 全部 5 方法 + tea.Model 3 方法 = 8 个方法。
// 每个方法在 receiver 为 nil 时返回零值不 panic。
type fakePaneModel struct {
	tickCalled       int
	selectPIDCalled  int
	lastSelectedPID  types.PID
}

func (f *fakePaneModel) Init() tea.Cmd { return nil }
func (f *fakePaneModel) Update(_ tea.Msg) (tea.Model, tea.Cmd) {
	if f == nil {
		return f, nil
	}
	return f, nil
}
func (f *fakePaneModel) View() tea.View {
	if f == nil {
		return tea.NewView("")
	}
	return tea.NewView("fake-pane")
}
func (f *fakePaneModel) Name() string {
	if f == nil {
		return ""
	}
	return "fake"
}
func (f *fakePaneModel) KeyLayer() *ui.KeyLayer {
	if f == nil {
		return nil
	}
	return &ui.KeyLayer{Name: "fake"}
}
func (f *fakePaneModel) OnSelectPID(pid types.PID) tea.Cmd {
	if f == nil {
		return nil
	}
	f.selectPIDCalled++
	f.lastSelectedPID = pid
	return nil
}
func (f *fakePaneModel) OnTick(_ time.Time) tea.Cmd {
	if f == nil {
		return nil
	}
	f.tickCalled++
	return nil
}

// fakeOverlayModel 用于 OverlayModel 契约测试：实现 4 方法 + tea.Model 3 方法 = 7 方法。
type fakeOverlayModel struct {
	enterCalled bool
	exitCalled  bool
	active      bool
}

func (f *fakeOverlayModel) Init() tea.Cmd { return nil }
func (f *fakeOverlayModel) Update(_ tea.Msg) (tea.Model, tea.Cmd) {
	if f == nil {
		return f, nil
	}
	return f, nil
}
func (f *fakeOverlayModel) View() tea.View {
	if f == nil {
		return tea.NewView("")
	}
	return tea.NewView("fake-overlay")
}
func (f *fakeOverlayModel) IsActive() bool {
	if f == nil {
		return false
	}
	return f.active
}
func (f *fakeOverlayModel) OnEnter() tea.Cmd {
	if f == nil {
		return nil
	}
	f.enterCalled = true
	f.active = true
	return nil
}
func (f *fakeOverlayModel) OnExit() tea.Cmd {
	if f == nil {
		return nil
	}
	f.exitCalled = true
	f.active = false
	return nil
}
func (f *fakeOverlayModel) OnSelectPID(pid types.PID) tea.Cmd {
	if f == nil {
		return nil
	}
	_ = pid
	return nil
}

// 编译期断言：fakePaneModel/fakeOverlayModel 实现 PaneModel/OverlayModel interface。
var (
	_ PaneModel    = (*fakePaneModel)(nil)
	_ OverlayModel = (*fakeOverlayModel)(nil)
)

// TestPaneModel_NilSafety 强制契约：nil PaneModel 调用 5 个 hook 不 panic。
func TestPaneModel_NilSafety(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil PaneModel hook panicked: %v", r)
		}
	}()

	var f *fakePaneModel // nil
	if got := f.Name(); got != "" {
		t.Errorf("Name() on nil = %q, want empty", got)
	}
	if got := f.KeyLayer(); got != nil {
		t.Errorf("KeyLayer() on nil = %v, want nil", got)
	}
	if got := f.OnSelectPID(types.PID(42)); got != nil {
		t.Errorf("OnSelectPID() on nil = %v, want nil", got)
	}
	if got := f.OnTick(time.Now()); got != nil {
		t.Errorf("OnTick() on nil = %v, want nil", got)
	}
	if got := f.View(); got.Content != "" {
		t.Errorf("View() on nil content = %q, want empty", got.Content)
	}
}
func TestOverlayModel_LifecycleOrder(t *testing.T) {
	o := &fakeOverlayModel{}

	if o.IsActive() {
		t.Fatal("freshly constructed Overlay should be inactive")
	}

	o.OnEnter()
	if !o.enterCalled {
		t.Fatal("OnEnter not invoked")
	}
	if !o.IsActive() {
		t.Fatal("IsActive() must be true after OnEnter")
	}

	o.OnExit()
	if !o.exitCalled {
		t.Fatal("OnExit not invoked")
	}
	if o.IsActive() {
		t.Fatal("IsActive() must be false after OnExit")
	}
}

// TestOverlayModel_NilSafety 强制契约：nil OverlayModel 调用各 hook 不 panic。
func TestOverlayModel_NilSafety(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil OverlayModel hook panicked: %v", r)
		}
	}()

	var o *fakeOverlayModel // nil
	if got := o.IsActive(); got {
		t.Errorf("IsActive() on nil = true, want false")
	}
	if got := o.OnEnter(); got != nil {
		t.Errorf("OnEnter() on nil = %v, want nil", got)
	}
	if got := o.OnExit(); got != nil {
		t.Errorf("OnExit() on nil = %v, want nil", got)
	}
	if got := o.View(); got.Content != "" {
		t.Errorf("View() on nil content = %q, want empty", got.Content)
	}
}

// TestPaneState_DefaultValues 强制契约：零值 PaneState 字段语义符合 spec。
func TestPaneState_DefaultValues(t *testing.T) {
	var s PaneState
	if s.PID != 0 {
		t.Errorf("zero PaneState.PID = %d, want 0", s.PID)
	}
	if s.UUID != "" {
		t.Errorf("zero PaneState.UUID = %q, want empty", s.UUID)
	}
	if s.Width != 0 || s.Height != 0 {
		t.Errorf("zero PaneState size = (%d, %d), want (0, 0)", s.Width, s.Height)
	}
	if s.Active {
		t.Errorf("zero PaneState.Active = true, want false")
	}
}

// TestPaneModel_OnSelectPID_Counts 验证 OnSelectPID 多次调用累加（用于 PR2-PR9 子 Model 的回归契约）。
func TestPaneModel_OnSelectPID_Counts(t *testing.T) {
	f := &fakePaneModel{}
	f.OnSelectPID(types.PID(1))
	f.OnSelectPID(types.PID(2))
	f.OnSelectPID(types.PID(3))

	if f.selectPIDCalled != 3 {
		t.Errorf("OnSelectPID call count = %d, want 3", f.selectPIDCalled)
	}
	if f.lastSelectedPID != types.PID(3) {
		t.Errorf("last selected PID = %d, want 3", f.lastSelectedPID)
	}
}
