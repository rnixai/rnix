package main

// =============================================================================
// ATDD Story 27.5: top↔watch 双向导航
// TDD RED PHASE — All tests designed to FAIL until appModel is fully implemented
// =============================================================================
//
// Test Strategy:
//   AC-1:  top Enter 键进入 watch 视图 (mode switch, watchModel creation)
//   AC-2:  watch q 键返回 top 视图 (mode switch, cleanup, tick restore)
//   AC-3:  watch Ctrl+C 直接退出程序 (tea.Quit in both modes)
//   AC-4:  watch Pager 中 q 返回 watch（非 top）(pager isolation)
//   AC-5:  统一 BubbleTea Program (appModel as tea.Model)
//   AC-6:  watch 视图目标进程已结束 (completed + q → top)
//   AC-7:  IPC 连接生命周期管理 (topClient persistence)
//   AC-8:  独立 watch 命令不受影响 (standalone q → quit) [regression]
//   AC-9:  top 原有 detail 面板替换 (no detailPID via appModel)
//   AC-10: top 视图帮助栏更新 ("Watch" not "Details")
//
// Priority: P0 (core observation navigation)
// Test Level: Unit (appModel state machine) + Integration (view rendering)

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestAppModel() appModel {
	m := appModel{
		mode:     appModeTop,
		topModel: newTopModel(nil),
		dialFn:   func() (*ipc.Client, error) { return nil, nil },
	}
	m.topModel.width = 120
	m.topModel.height = 40
	return m
}

func newTestAppModelWithRows(rows []flatRow) appModel {
	m := newTestAppModel()
	m.topModel.rows = rows
	m.topModel.processes = make([]vfs.ProcInfo, len(rows))
	for i, r := range rows {
		m.topModel.processes[i] = r.proc
	}
	return m
}

func sampleFlatRows() []flatRow {
	return []flatRow{
		{proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, Intent: "analyze code"}},
		{proc: vfs.ProcInfo{PID: 2, State: types.StateRunning, Intent: "write tests"}},
		{proc: vfs.ProcInfo{PID: 3, State: types.StateZombie, Intent: "old task"}},
	}
}

// ---------------------------------------------------------------------------
// AC-5: 统一 BubbleTea Program (appModel as tea.Model)
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC5_AppModel_ImplementsTeaModel(t *testing.T) {
	var _ tea.Model = appModel{}
}

func TestATDD_27_5_AC5_AppModel_DefaultModeIsTop(t *testing.T) {
	m := newTestAppModel()
	if m.mode != appModeTop {
		t.Errorf("AC-5: default mode should be appModeTop(0), got %d", m.mode)
	}
}

func TestATDD_27_5_AC5_AppModel_Init_ReturnsCmd(t *testing.T) {
	m := newTestAppModel()
	cmd := m.Init()
	if cmd == nil {
		t.Error("AC-5: Init() should return a non-nil tick command (delegated from topModel)")
	}
}

func TestATDD_27_5_AC5_AppModel_View_TopMode_HasContent(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	v := m.View()
	if v.Content == "" {
		t.Error("AC-5: View in top mode should return non-empty content")
	}
	if !v.AltScreen {
		t.Error("AC-5: View should set AltScreen = true")
	}
}

func TestATDD_27_5_AC5_AppModel_View_WatchMode_HasContent(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModelWithSteps(sampleSteps())
	m.watch = &wm
	m.mode = appModeWatch

	v := m.View()
	if v.Content == "" {
		t.Error("AC-5: View in watch mode should return non-empty content")
	}
	if !v.AltScreen {
		t.Error("AC-5: View in watch mode should set AltScreen = true")
	}
}

func TestATDD_27_5_AC5_AppModel_WindowSizeMsg_Propagates(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModelWithSteps(sampleSteps())
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	am := updated.(appModel)

	if am.topModel.width != 200 || am.topModel.height != 50 {
		t.Errorf("AC-5: WindowSizeMsg should propagate to topModel, got %dx%d",
			am.topModel.width, am.topModel.height)
	}
	if am.watch != nil && (am.watch.width != 200 || am.watch.height != 50) {
		t.Errorf("AC-5: WindowSizeMsg should propagate to watchModel, got %dx%d",
			am.watch.width, am.watch.height)
	}
}

// ---------------------------------------------------------------------------
// AC-1: top Enter 键进入 watch 视图
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC1_AppModel_EnterKey_SwitchesToWatch(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := updated.(appModel)

	if am.mode != appModeWatch {
		t.Errorf("AC-1: Enter should switch to appModeWatch, got mode %d", am.mode)
	}
}

func TestATDD_27_5_AC1_AppModel_EnterKey_CreatesWatchModel(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := updated.(appModel)

	if am.watch == nil {
		t.Error("AC-1: Enter should create a non-nil watchModel")
	}
}

func TestATDD_27_5_AC1_AppModel_EnterKey_WatchTargetsPID(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := updated.(appModel)

	if am.watch == nil {
		t.Fatal("AC-1: watch should be non-nil after Enter")
	}
	if am.watch.pid != 2 {
		t.Errorf("AC-1: watch should target PID 2 (cursor=1), got PID %d", am.watch.pid)
	}
}

func TestATDD_27_5_AC1_AppModel_EnterKey_ReturnsInitCmd(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 0

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Error("AC-1: Enter should return a non-nil cmd (watchModel.Init)")
	}
}

func TestATDD_27_5_AC1_AppModel_EnterKey_EmptyRows_NoOp(t *testing.T) {
	m := newTestAppModel()
	m.topModel.rows = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := updated.(appModel)

	if am.mode != appModeTop {
		t.Errorf("AC-1: Enter on empty rows should stay in top mode, got %d", am.mode)
	}
	if am.watch != nil {
		t.Error("AC-1: Enter on empty rows should not create watchModel")
	}
}

// ---------------------------------------------------------------------------
// AC-2: watch q 键返回 top 视图
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC2_AppModel_QKey_WatchNormal_ReturnsToTop(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModel()
	wm.state = watchStateNormal
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q'})
	am := updated.(appModel)

	if am.mode != appModeTop {
		t.Errorf("AC-2: q in watch Normal should return to top, got mode %d", am.mode)
	}
}

func TestATDD_27_5_AC2_AppModel_QKey_WatchExpanded_ReturnsToTop(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModel()
	wm.state = watchStateExpanded
	wm.expandLevel = 2
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q'})
	am := updated.(appModel)

	if am.mode != appModeTop {
		t.Errorf("AC-2: q in watch Expanded should return to top, got mode %d", am.mode)
	}
}

func TestATDD_27_5_AC2_AppModel_BackToTop_ClearsWatch(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModel()
	wm.state = watchStateNormal
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q'})
	am := updated.(appModel)

	if am.watch != nil {
		t.Error("AC-2: backToTop should set watch to nil")
	}
}

func TestATDD_27_5_AC2_AppModel_BackToTop_ReturnsTick(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModel()
	wm.state = watchStateNormal
	m.watch = &wm
	m.mode = appModeWatch

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("AC-2: backToTop should return a non-nil tick cmd to resume polling")
	}
}

func TestATDD_27_5_AC2_AppModel_CursorPreserved_AfterRoundTrip(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := updated.(appModel)
	if am.mode != appModeWatch {
		t.Skip("AC-2: prerequisite failed — Enter did not switch to watch")
	}

	am.watch.state = watchStateNormal
	updated, _ = am.Update(tea.KeyPressMsg{Code: 'q'})
	am = updated.(appModel)

	if am.topModel.cursor != 2 {
		t.Errorf("AC-2: cursor should be preserved at 2 after round-trip, got %d",
			am.topModel.cursor)
	}
}

// ---------------------------------------------------------------------------
// AC-3: watch Ctrl+C 直接退出程序
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC3_AppModel_CtrlC_TopMode_Quits(t *testing.T) {
	m := newTestAppModel()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("AC-3: Ctrl+C in top mode should return tea.Quit (non-nil cmd)")
	}
}

func TestATDD_27_5_AC3_AppModel_CtrlC_WatchMode_Quits(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModel()
	m.watch = &wm
	m.mode = appModeWatch

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("AC-3: Ctrl+C in watch mode should return tea.Quit (non-nil cmd)")
	}
}

// ---------------------------------------------------------------------------
// AC-4: watch Pager 中 q 返回 watch（非 top）
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC4_AppModel_QKey_Pager_StaysInWatch(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModelWithSteps(sampleSteps())
	wm.state = watchStatePager
	wm.pagerLines = []string{"test content"}
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q'})
	am := updated.(appModel)

	if am.mode != appModeWatch {
		t.Errorf("AC-4: q in Pager should stay in watch mode, got mode %d", am.mode)
	}
	if am.watch == nil {
		t.Fatal("AC-4: watch should still exist after q in Pager")
	}
	if am.watch.state != watchStateNormal {
		t.Errorf("AC-4: q in Pager should return watch to Normal state, got %d",
			am.watch.state)
	}
}

func TestATDD_27_5_AC4_AppModel_DoubleQ_PagerThenTop(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModelWithSteps(sampleSteps())
	wm.state = watchStatePager
	wm.pagerLines = []string{"test content"}
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q'})
	am := updated.(appModel)
	if am.mode != appModeWatch {
		t.Skip("AC-4: prerequisite failed — first q did not stay in watch")
	}

	updated, _ = am.Update(tea.KeyPressMsg{Code: 'q'})
	am = updated.(appModel)

	if am.mode != appModeTop {
		t.Errorf("AC-4: second q from Normal should return to top, got mode %d", am.mode)
	}
}

// ---------------------------------------------------------------------------
// AC-6: watch 视图目标进程已结束
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC6_AppModel_WatchCompleted_QReturnsToTop(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModel()
	wm.state = watchStateNormal
	wm.completed = true
	wm.exitCode = 0
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q'})
	am := updated.(appModel)

	if am.mode != appModeTop {
		t.Errorf("AC-6: q on completed watch should return to top, got mode %d", am.mode)
	}
}

// ---------------------------------------------------------------------------
// AC-7: IPC 连接生命周期管理
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC7_AppModel_TopClient_InTopModel(t *testing.T) {
	m := newTestAppModel()
	if m.topModel.client != nil {
		t.Error("AC-7: topModel.client should be nil when created with nil client")
	}
}

// ---------------------------------------------------------------------------
// AC-8: 独立 watch 命令不受影响 [regression]
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC8_StandaloneWatch_QKey_Quits(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateNormal

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("AC-8: standalone watch q should return tea.Quit (non-nil cmd)")
	}
}

func TestATDD_27_5_AC8_StandaloneWatch_CtrlC_Quits(t *testing.T) {
	m := newTestWatchModelWithSteps(sampleSteps())
	m.state = watchStateExpanded

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("AC-8: standalone watch Ctrl+C should return tea.Quit")
	}
}

// ---------------------------------------------------------------------------
// AC-9: top 原有 detail 面板替换
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC9_AppModel_EnterKey_SwitchesToWatch(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 1

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := updated.(appModel)

	if am.mode != appModeWatch {
		t.Errorf("AC-9: Enter via appModel should switch to watch (replacing detail panel), got mode %d", am.mode)
	}
}

// ---------------------------------------------------------------------------
// AC-10: top 视图帮助栏更新
// ---------------------------------------------------------------------------

func TestATDD_27_5_AC10_AppModel_TopHelpBar_ShowsWatch(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	v := m.View()

	if !strings.Contains(v.Content, "Watch") {
		t.Error("AC-10: top help bar should contain 'Watch'")
	}
	if strings.Contains(v.Content, "Details") {
		t.Error("AC-10: top help bar should NOT contain 'Details'")
	}
}

// ---------------------------------------------------------------------------
// Integration: tickMsg routing
// ---------------------------------------------------------------------------

func TestATDD_27_5_TickMsg_InWatchMode_Ignored(t *testing.T) {
	m := newTestAppModel()
	wm := newTestWatchModelWithSteps(sampleSteps())
	m.watch = &wm
	m.mode = appModeWatch

	updated, _ := m.Update(tickMsg(time.Now()))
	am := updated.(appModel)

	if am.mode != appModeWatch {
		t.Errorf("tickMsg in watch mode should be ignored, mode should stay watch, got %d", am.mode)
	}
}

// ---------------------------------------------------------------------------
// Integration: top q in app model
// ---------------------------------------------------------------------------

func TestATDD_27_5_TopMode_QKey_Quits(t *testing.T) {
	m := newTestAppModel()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("q in top mode should return tea.Quit (non-nil cmd)")
	}
}

// ---------------------------------------------------------------------------
// Integration: j/k navigation through appModel
// ---------------------------------------------------------------------------

func TestATDD_27_5_TopMode_JK_Navigation(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	am := updated.(appModel)

	if am.topModel.cursor != 1 {
		t.Errorf("j in top mode should move cursor down, expected 1 got %d",
			am.topModel.cursor)
	}

	updated, _ = am.Update(tea.KeyPressMsg{Code: 'k'})
	am = updated.(appModel)

	if am.topModel.cursor != 0 {
		t.Errorf("k in top mode should move cursor up, expected 0 got %d",
			am.topModel.cursor)
	}
}

// ---------------------------------------------------------------------------
// Integration: K key kill through appModel
// ---------------------------------------------------------------------------

func TestATDD_27_5_TopMode_KKey_Kill(t *testing.T) {
	m := newTestAppModelWithRows(sampleFlatRows())
	m.topModel.cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'K', ShiftedCode: 'K', Mod: tea.ModShift})
	am := updated.(appModel)

	if am.mode != appModeTop {
		t.Errorf("K key should stay in top mode, got %d", am.mode)
	}
}
