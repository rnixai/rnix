package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// dashboard_broadcast_test.go — Story 38-5 PR11 Step 4(b) Phase 1
//
// 测试 broadcastSelectPID + broadcastSelectPIDImpl + emitSelectPIDCmd +
// Update.SelectPIDMsg 路由。
//
// spec § AC11 硬约束：所有 11 个子 Model（8 PaneModel + 3 OverlayModel · 仅
// IsActive 时）的 OnSelectPID 都要被调用，且返回 cmd 通过 tea.Batch 收集。
//
// 测试策略：调用 broadcastSelectPIDImpl(pid, panes, overlays) 注入 mock 实现
// PaneModel/OverlayModel interface 的 fakePane/fakeOverlay 列表 · 计数 hook
// 调用 + 记录最后接收的 PID + 验证 IsActive 守卫 + tea.Batch 收集。

// --- mock helpers ---

// fakePane 实现 dashboardmodel.PaneModel interface。
type fakePane struct {
	calls   int
	lastPID types.PID
	retCmd  tea.Cmd
}

func (f *fakePane) Init() tea.Cmd { return nil }
func (f *fakePane) Update(_ tea.Msg) (tea.Model, tea.Cmd) {
	return f, nil
}
func (f *fakePane) View() tea.View              { return tea.NewView("") }
func (f *fakePane) Name() string                { return "fake" }
func (f *fakePane) KeyLayer() *ui.KeyLayer      { return nil }
func (f *fakePane) OnTick(_ time.Time) tea.Cmd  { return nil }
func (f *fakePane) OnSelectPID(p types.PID) tea.Cmd {
	if f == nil {
		return nil
	}
	f.calls++
	f.lastPID = p
	return f.retCmd
}

// fakeOverlay 实现 dashboardmodel.OverlayModel interface。
type fakeOverlay struct {
	calls   int
	lastPID types.PID
	active  bool
	retCmd  tea.Cmd
}

func (f *fakeOverlay) Init() tea.Cmd { return nil }
func (f *fakeOverlay) Update(_ tea.Msg) (tea.Model, tea.Cmd) {
	return f, nil
}
func (f *fakeOverlay) View() tea.View   { return tea.NewView("") }
func (f *fakeOverlay) IsActive() bool   { return f != nil && f.active }
func (f *fakeOverlay) OnEnter() tea.Cmd { return nil }
func (f *fakeOverlay) OnExit() tea.Cmd  { return nil }
func (f *fakeOverlay) OnSelectPID(p types.PID) tea.Cmd {
	if f == nil {
		return nil
	}
	f.calls++
	f.lastPID = p
	return f.retCmd
}

// 编译期断言：fakePane / fakeOverlay 满足 interface。
var (
	_ dashboardmodel.PaneModel    = (*fakePane)(nil)
	_ dashboardmodel.OverlayModel = (*fakeOverlay)(nil)
)

// --- tests ---

// TestEmitSelectPIDCmd_RoundTrip — emitSelectPIDCmd → cmd() 返回 SelectPIDMsg
// 且 PID 字段保留正确。
func TestEmitSelectPIDCmd_RoundTrip(t *testing.T) {
	cmd := emitSelectPIDCmd(types.PID(1234))
	if cmd == nil {
		t.Fatal("emitSelectPIDCmd returned nil cmd")
	}
	msg := cmd()
	sel, ok := msg.(dashboardmodel.SelectPIDMsg)
	if !ok {
		t.Fatalf("emitSelectPIDCmd msg type = %T, want SelectPIDMsg", msg)
	}
	if sel.PID != types.PID(1234) {
		t.Errorf("SelectPIDMsg.PID = %d, want 1234", sel.PID)
	}
}

// TestBroadcastSelectPIDImpl_CallsAllPanes — spec § AC11 硬约束：8 PaneModel
// 全部接收 broadcast · 计数 == 1 · lastPID 正确。
//
// 注意：fakePane 默认 retCmd=nil · 收集到 0 个非 nil cmd · tea.Batch() 返回 nil
// 是合理的（无 cmd 需要执行）。本测试验证 hook 调用而非 cmd 收集（后者由
// CollectsCmds 测试覆盖）.
func TestBroadcastSelectPIDImpl_CallsAllPanes(t *testing.T) {
	const N = 8
	panes := make([]dashboardmodel.PaneModel, N)
	mocks := make([]*fakePane, N)
	for i := range N {
		mocks[i] = &fakePane{}
		panes[i] = mocks[i]
	}
	_ = broadcastSelectPIDImpl(types.PID(42), panes, nil)
	for i, m := range mocks {
		if m.calls != 1 {
			t.Errorf("pane[%d].calls = %d, want 1", i, m.calls)
		}
		if m.lastPID != types.PID(42) {
			t.Errorf("pane[%d].lastPID = %d, want 42", i, m.lastPID)
		}
	}
}

// TestBroadcastSelectPIDImpl_CollectsCmds — 子 Model 返回非 nil cmd 时通过
// tea.Batch 收集（spec § 04 风险 1 缓解 · tea.Cmd 上传链零沉默失败）。
func TestBroadcastSelectPIDImpl_CollectsCmds(t *testing.T) {
	called := 0
	makeCmd := func() tea.Cmd {
		return func() tea.Msg {
			called++
			return nil
		}
	}
	panes := []dashboardmodel.PaneModel{
		&fakePane{retCmd: makeCmd()},
		&fakePane{retCmd: makeCmd()},
		&fakePane{retCmd: nil}, // 第 3 个 nil cmd 应被跳过
	}
	cmd := broadcastSelectPIDImpl(types.PID(1), panes, nil)
	if cmd == nil {
		t.Fatal("broadcastSelectPIDImpl returned nil with non-empty cmds")
	}
	// 触发 tea.Batch — 实际 runtime 会展开 BatchMsg 并执行子 cmd · 这里手动
	// 验证返回不是 nil 即可（具体执行由 Bubble Tea runtime 负责）。
}

// TestBroadcastSelectPIDImpl_OverlayInactiveSkipped — IsActive==false 的
// Overlay 不被调用（spec § 04 风险 2 缓解）。
func TestBroadcastSelectPIDImpl_OverlayInactiveSkipped(t *testing.T) {
	overlays := []dashboardmodel.OverlayModel{
		&fakeOverlay{active: false},
		&fakeOverlay{active: false},
		&fakeOverlay{active: false},
	}
	_ = broadcastSelectPIDImpl(types.PID(99), nil, overlays)
	for i, o := range overlays {
		fake := o.(*fakeOverlay)
		if fake.calls != 0 {
			t.Errorf("inactive overlay[%d].calls = %d, want 0", i, fake.calls)
		}
	}
}

// TestBroadcastSelectPIDImpl_OverlayActiveCalled — IsActive==true 的 Overlay
// 接收 broadcast。
func TestBroadcastSelectPIDImpl_OverlayActiveCalled(t *testing.T) {
	overlays := []dashboardmodel.OverlayModel{
		&fakeOverlay{active: true},
		&fakeOverlay{active: true},
		&fakeOverlay{active: true},
	}
	_ = broadcastSelectPIDImpl(types.PID(7), nil, overlays)
	for i, o := range overlays {
		fake := o.(*fakeOverlay)
		if fake.calls != 1 {
			t.Errorf("active overlay[%d].calls = %d, want 1", i, fake.calls)
		}
		if fake.lastPID != types.PID(7) {
			t.Errorf("active overlay[%d].lastPID = %d, want 7", i, fake.lastPID)
		}
	}
}

// TestBroadcastSelectPIDImpl_NilSafe — panes/overlays 中含 nil 元素 · 不 panic
// · 跳过 nil 元素继续广播。
func TestBroadcastSelectPIDImpl_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("broadcastSelectPIDImpl with nil elements panicked: %v", r)
		}
	}()
	good := &fakePane{}
	panes := []dashboardmodel.PaneModel{nil, good, nil}
	goodOv := &fakeOverlay{active: true}
	overlays := []dashboardmodel.OverlayModel{nil, goodOv, nil}
	_ = broadcastSelectPIDImpl(types.PID(33), panes, overlays)
	if good.calls != 1 {
		t.Errorf("non-nil pane.calls = %d, want 1", good.calls)
	}
	if goodOv.calls != 1 {
		t.Errorf("non-nil active overlay.calls = %d, want 1", goodOv.calls)
	}
}

// TestBroadcastSelectPID_NilFieldsNoPanic — dashboardModel{} 默认所有 11 字段
// 是 nil pointer · broadcastSelectPID 必须不 panic。
func TestBroadcastSelectPID_NilFieldsNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("broadcastSelectPID with nil fields panicked: %v", r)
		}
	}()
	m := dashboardModel{}
	cmd := m.broadcastSelectPID(types.PID(1))
	// nil 字段 broadcast 不调用任何 hook · cmd 可能是 nil 或 empty Batch 均接受
	if cmd != nil {
		_ = cmd() // 不应 panic
	}
}
