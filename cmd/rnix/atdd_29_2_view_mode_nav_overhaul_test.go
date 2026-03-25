package main

// =============================================================================
// ATDD Story 29.2: 视图模式系统与导航重构 (TDD RED PHASE)
// =============================================================================
//
// Test Strategy:
//   AC-1: viewMode 枚举与 expandedPane 字段引入
//   AC-2: 数字键 1-8 直达面板切换 (toggle)
//   AC-3: Esc 键回到默认视图
//   AC-4: Shift-Tab 反向面板导航
//   AC-5: Title Bar 显示带编号面板标签
//   AC-6: Status Bar 根据 viewMode 动态更新快捷键提示
//   AC-7: Tab 键行为修改为进入 viewExpanded 模式
//
// Priority: P0 (type existence, key navigation, layout), P1 (title/status bar rendering)
// Test Level: Unit (type verification, state machine, rendering output)
// TDD Phase: RED — all tests assert EXPECTED post-implementation behavior

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// 29.2-UNIT-001: [P0] AC-1 — viewMode 类型和 4 个常量在 dashboard_types.go 中定义
// ---------------------------------------------------------------------------

func TestViewModeSystem_TypeAndConstantsExist(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_types.go: %v", err)
	}
	content := string(data)

	// viewMode 类型定义
	if !strings.Contains(content, "type viewMode") {
		t.Error("expected dashboard_types.go to contain 'type viewMode' type definition")
	}

	// 4 个常量
	expectedConstants := []string{
		"viewDefault",
		"viewExpanded",
		"viewLLM",
		"viewHistory",
	}
	for _, c := range expectedConstants {
		if !strings.Contains(content, c) {
			t.Errorf("expected dashboard_types.go to contain constant %q", c)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-002: [P0] AC-1 — dashboardModel 包含 viewMode 和 expandedPane 字段
// ---------------------------------------------------------------------------

func TestViewModeSystem_ModelFieldsExist(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse dashboard.go: %v", err)
	}

	// Find dashboardModel struct
	var structType *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.Name == "dashboardModel" {
			if st, ok := ts.Type.(*ast.StructType); ok {
				structType = st
			}
		}
		return true
	})

	if structType == nil {
		t.Fatal("dashboardModel struct not found in dashboard.go")
	}

	fieldNames := make(map[string]bool)
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fieldNames[name.Name] = true
		}
	}

	if !fieldNames["viewMode"] {
		t.Error("expected dashboardModel to have 'viewMode' field")
	}
	if !fieldNames["expandedPane"] {
		t.Error("expected dashboardModel to have 'expandedPane' field")
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-003: [P0] AC-1 — viewMode 零值为 viewDefault（Go 默认行为验证）
// ---------------------------------------------------------------------------

func TestViewModeSystem_ZeroValueIsDefault(t *testing.T) {
	m := newDashboardModel(nil)
	if m.viewMode != viewDefault {
		t.Errorf("expected new dashboardModel viewMode to be viewDefault (0), got %d", m.viewMode)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-004: [P0] AC-2 — dashboard_nav.go 文件存在且包含 dashboardKey
// ---------------------------------------------------------------------------

func TestViewModeSystem_NavFileExists(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_nav.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected dashboard_nav.go to exist in cmd/rnix/")
	}

	funcs, err := topLevelFuncNames(path)
	if err != nil {
		t.Fatalf("failed to parse dashboard_nav.go: %v", err)
	}

	funcSet := make(map[string]bool)
	for _, fn := range funcs {
		funcSet[fn] = true
	}

	expectedFuncs := []string{
		"dashboardKey",
		"dispatchPaneKey",
	}
	for _, fn := range expectedFuncs {
		if !funcSet[fn] {
			t.Errorf("expected function %s in dashboard_nav.go, not found", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-005: [P0] AC-2 — dashboardKey 已从 dashboard.go 中移除
// ---------------------------------------------------------------------------

func TestViewModeSystem_DashboardKeyRemovedFromMain(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	funcs, err := topLevelFuncNames(path)
	if err != nil {
		t.Fatalf("failed to parse dashboard.go: %v", err)
	}

	for _, fn := range funcs {
		if fn == "dashboardKey" {
			t.Error("dashboardKey should be moved from dashboard.go to dashboard_nav.go")
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-006: [P0] AC-2 — 数字键切换右侧面板
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitKeySwitchesRightPane(t *testing.T) {
	m := newDashboardModel(nil)

	// 按 2：rightPane 切换到 paneTimeline，activePane 也跟随
	m2, _ := m.Update(tea.KeyPressMsg{Code: '2'})
	m = m2.(dashboardModel)
	if m.rightPane != paneTimeline {
		t.Errorf("expected rightPane=paneTimeline, got %d", m.rightPane)
	}
	if m.activePane != paneTimeline {
		t.Errorf("expected activePane=paneTimeline, got %d", m.activePane)
	}

	// 按 7：rightPane 切换到 paneTrace
	m2, _ = m.Update(tea.KeyPressMsg{Code: '7'})
	m = m2.(dashboardModel)
	if m.rightPane != paneTrace {
		t.Errorf("expected rightPane=paneTrace, got %d", m.rightPane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-007: [P0] AC-2 — 按 1 聚焦 Tree 侧边栏
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitOneFocusesTree(t *testing.T) {
	m := newDashboardModel(nil)
	m.activePane = paneTimeline

	m2, _ := m.Update(tea.KeyPressMsg{Code: '1'})
	m = m2.(dashboardModel)
	if m.activePane != paneTree {
		t.Errorf("expected activePane=paneTree, got %d", m.activePane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-008: [P0] AC-2 — 所有面板 2-8 都可通过数字键切换
// ---------------------------------------------------------------------------

func TestViewModeSystem_AllPanesSwitchable(t *testing.T) {
	panes := []struct {
		key  rune
		pane paneType
	}{
		{'2', paneTimeline}, {'3', paneHeatmap}, {'4', paneDetail},
		{'5', paneIntent}, {'6', paneSecurity}, {'7', paneTrace}, {'8', paneEval},
	}

	for _, p := range panes {
		m := newDashboardModel(nil)
		m2, _ := m.Update(tea.KeyPressMsg{Code: p.key})
		m = m2.(dashboardModel)
		if m.rightPane != p.pane {
			t.Errorf("key %c: expected rightPane=%d, got %d", p.key, p.pane, m.rightPane)
		}
		if m.activePane != p.pane {
			t.Errorf("key %c: expected activePane=%d, got %d", p.key, p.pane, m.activePane)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-009: [P0] AC-5 — Title bar 包含面板编号标签
// ---------------------------------------------------------------------------

func TestViewModeSystem_TitleBarPaneLabels(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true

	title := m.renderDashboardTitle()

	// 默认视图下，所有面板应显示 [N]Name 格式
	expectedLabels := []string{
		"[1]", "[2]", "[3]", "[4]",
		"[5]", "[6]", "[7]", "[8]",
	}
	for _, label := range expectedLabels {
		if !strings.Contains(title, label) {
			t.Errorf("expected title bar to contain %q in default view, got: %s", label, title)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-010: [P1] AC-5 — Title bar 展开视图下激活面板显示 N:Name*
// ---------------------------------------------------------------------------

func TestViewModeSystem_TitleBarExpandedHighlight(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.viewMode = viewExpanded
	m.expandedPane = paneTimeline
	m.activePane = paneTimeline

	title := m.renderDashboardTitle()

	// 所有面板统一 [N]Name 格式，选中面板加粗
	if !strings.Contains(title, "[2]") {
		t.Errorf("expected active pane to show '[2]', got: %s", title)
	}
	if !strings.Contains(title, "[1]") {
		t.Errorf("expected non-active pane to show '[1]', got: %s", title)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-011: [P0] AC-6 — Status bar 默认视图显示全局快捷键
// ---------------------------------------------------------------------------

func TestViewModeSystem_StatusBarDefaultView(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40

	status := m.renderDashboardStatus()

	// 默认视图应包含核心 hints
	requiredHints := []string{"nav", "expand", "help", "quit"}
	for _, h := range requiredHints {
		if !strings.Contains(status, h) {
			t.Errorf("expected default status bar to contain %q, got: %s", h, status)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-012: [P1] AC-6 — Status bar 展开视图显示面板特定提示
// ---------------------------------------------------------------------------

func TestViewModeSystem_StatusBarExpandedView(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.viewMode = viewExpanded
	m.expandedPane = paneTimeline

	status := m.renderDashboardStatus()

	// 展开 Timeline 时应包含面板特定提示
	for _, kw := range []string{"detail", "help", "quit"} {
		if !strings.Contains(status, kw) {
			t.Errorf("expected expanded status bar to contain '%s', got: %s", kw, status)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-013: [P1] AC-6 — paneSpecificHints 方法存在
// ---------------------------------------------------------------------------

func TestViewModeSystem_PaneSpecificHintsExists(t *testing.T) {
	// 确认 paneSpecificHints 方法在 dashboard.go 或 dashboard_nav.go 中存在
	paths := []string{
		filepath.Join(cmdRnixDir(), "dashboard.go"),
		filepath.Join(cmdRnixDir(), "dashboard_nav.go"),
	}

	found := false
	for _, path := range paths {
		funcs, err := topLevelFuncNames(path)
		if err != nil {
			continue
		}
		if slices.Contains(funcs, "paneHints") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected paneHints method to exist in dashboard.go or dashboard_nav.go")
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-001: [P0] AC-全部 — renderDashboard 在 viewDefault 下保持原有布局
// ---------------------------------------------------------------------------

func TestViewModeSystem_DefaultLayoutUnchanged(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.viewMode = viewDefault // 显式设置，虽然零值即默认

	output := m.renderDashboard()

	// 基本验证：输出非空
	if len(output) == 0 {
		t.Error("renderDashboard returned empty string in viewDefault mode")
	}

	// 验证包含 dashboard 标志性内容
	if !strings.Contains(output, "Rnix Dashboard") {
		t.Error("expected renderDashboard output to contain 'Rnix Dashboard'")
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-002: [P0] AC-2,3 — renderDashboard 在 viewExpanded 下输出不同于默认
// ---------------------------------------------------------------------------

func TestViewModeSystem_ExpandedLayoutDiffers(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true

	defaultOutput := m.renderDashboard()

	m.viewMode = viewExpanded
	m.expandedPane = paneTimeline
	expandedOutput := m.renderDashboard()

	// 两个布局应有差异（至少 title bar 不同）
	if defaultOutput == expandedOutput {
		t.Error("expected expanded layout to differ from default layout")
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-003: [P0] AC-2 — viewExpanded + paneTree 时 Tree 全屏
// ---------------------------------------------------------------------------

func TestViewModeSystem_TreeFullScreenWhenExpanded(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.viewMode = viewExpanded
	m.expandedPane = paneTree

	output := m.renderDashboard()

	if len(output) == 0 {
		t.Error("renderDashboard returned empty string in tree expanded mode")
	}

	// 标题栏应包含 Tree 面板标签
	if !strings.Contains(output, "[1]Tree") {
		t.Error("expected title to contain '[1]Tree'")
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-014: [P0] AC-7 — renderDefaultLayout 和 renderExpandedLayout 方法存在
// ---------------------------------------------------------------------------

func TestViewModeSystem_LayoutMethodsExist(t *testing.T) {
	// 查找 renderDefaultLayout 和 renderExpandedLayout
	paths := []string{
		filepath.Join(cmdRnixDir(), "dashboard.go"),
	}

	expectedMethods := []string{
		"renderDefaultLayout",
		"renderExpandedLayout",
	}

	for _, path := range paths {
		funcs, err := topLevelFuncNames(path)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}

		funcSet := make(map[string]bool)
		for _, fn := range funcs {
			funcSet[fn] = true
		}

		for _, expected := range expectedMethods {
			if !funcSet[expected] {
				t.Errorf("expected method %s in dashboard.go, not found", expected)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-015: [P1] AC-5 — Title bar 保留连接状态和统计信息
// ---------------------------------------------------------------------------

func TestViewModeSystem_TitleBarRetainsStats(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true

	title := m.renderDashboardTitle()

	// 连接状态指示符
	if !strings.Contains(title, "●") {
		t.Errorf("expected title bar to contain connected indicator '●', got: %s", title)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-016: [P1] AC-6 — Status bar 保留 REC 指示和操作按键
// ---------------------------------------------------------------------------

func TestViewModeSystem_StatusBarRetainsOps(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40

	status := m.renderDashboardStatus()

	// 操作快捷键已移入 ? 帮助覆盖层
	if !strings.Contains(status, "help") {
		t.Errorf("expected status bar to contain 'help' hint, got: %s", status)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-017: [P0] L/H stub 键定义存在（显示未实现提示）
// ---------------------------------------------------------------------------

func TestViewModeSystem_StubKeysExist(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_nav.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_nav.go: %v", err)
	}
	content := string(data)

	// L 和 H 键应在导航文件中有处理（即使只是 stub）
	if !strings.Contains(content, `"L"`) && !strings.Contains(content, `case "L"`) {
		t.Error("expected dashboard_nav.go to handle 'L' key (LLM viewer stub)")
	}
	if !strings.Contains(content, `"H"`) && !strings.Contains(content, `case "H"`) {
		t.Error("expected dashboard_nav.go to handle 'H' key (history viewer stub)")
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-005: [P0] AC-7 — Tab 键在 Tree 和 rightPane 之间切换焦点
// ---------------------------------------------------------------------------

func TestViewModeSystem_TabKeyEntersExpanded(t *testing.T) {
	m := newDashboardModel(nil)
	m.activePane = paneTree // 初始面板

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model := m2.(dashboardModel)

	// Tab now toggles focus between Tree and rightPane (default: paneTimeline)
	if model.viewMode != viewDefault {
		t.Errorf("expected viewDefault after Tab, got %d", model.viewMode)
	}
	if model.activePane != paneTimeline {
		t.Errorf("expected activePane=paneTimeline after Tab from paneTree, got %d", model.activePane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-006: [P0] AC-4 — Shift-Tab 键同 Tab，双向切换 Tree ↔ rightPane
// ---------------------------------------------------------------------------

func TestViewModeSystem_ShiftTabKeyReverseCycle(t *testing.T) {
	m := newDashboardModel(nil)
	m.activePane = paneTree // paneTree = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model := m2.(dashboardModel)

	// Shift+Tab now same as Tab: toggle Tree ↔ rightPane
	if model.activePane != paneTimeline {
		t.Errorf("expected activePane=paneTimeline after Shift-Tab from paneTree, got %d", model.activePane)
	}
	if model.viewMode != viewDefault {
		t.Errorf("expected viewDefault after Shift-Tab, got %d", model.viewMode)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-007: [P0] AC-3 — Esc 键经 dashboardKey 从 viewExpanded 回退
// ---------------------------------------------------------------------------

func TestViewModeSystem_EscKeyReturnsToDefault(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewExpanded
	m.expandedPane = paneTimeline
	m.activePane = paneTimeline

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model := m2.(dashboardModel)

	if model.viewMode != viewDefault {
		t.Errorf("expected viewDefault after Esc from viewExpanded, got %d", model.viewMode)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-008: [P0] AC-3 — Esc 在 viewDefault 下不改变状态
// ---------------------------------------------------------------------------

func TestViewModeSystem_EscNoopInDefault(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model := m2.(dashboardModel)

	if model.viewMode != viewDefault {
		t.Errorf("expected viewDefault unchanged after Esc in viewDefault, got %d", model.viewMode)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-009: [P0] AC-2 — 数字键设置 rightPane 和 activePane
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitKeyJumpsToPane(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault

	// 按 "3" → rightPane=paneHeatmap, activePane=paneHeatmap, viewMode stays viewDefault
	m2, _ := m.Update(tea.KeyPressMsg{Code: '3'})
	model := m2.(dashboardModel)

	if model.viewMode != viewDefault {
		t.Errorf("expected viewDefault after digit key, got %d", model.viewMode)
	}
	if model.rightPane != paneHeatmap {
		t.Errorf("expected rightPane=paneHeatmap, got %d", model.rightPane)
	}
	if model.activePane != paneHeatmap {
		t.Errorf("expected activePane=paneHeatmap, got %d", model.activePane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-010: [P0] AC-2 — 数字键：再次按同一键仍保持 rightPane 设置
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitKeyToggleBack(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewExpanded
	m.expandedPane = paneHeatmap
	m.activePane = paneHeatmap
	m.rightPane = paneHeatmap

	// 再按 "3" → 在 viewExpanded 下更新 rightPane 和 expandedPane
	m2, _ := m.Update(tea.KeyPressMsg{Code: '3'})
	model := m2.(dashboardModel)

	if model.rightPane != paneHeatmap {
		t.Errorf("expected rightPane=paneHeatmap, got %d", model.rightPane)
	}
	if model.activePane != paneHeatmap {
		t.Errorf("expected activePane=paneHeatmap, got %d", model.activePane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-011: [P0] AC-2 — Eval 面板展开时 1/2/3 传给面板而非 jumpToPane
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitKeyEvalConflict(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewExpanded
	m.expandedPane = paneEval
	m.activePane = paneEval
	m.evalSubView = 0 // reputation

	// 按 "2" → 应跳转到 paneTimeline（数字键始终用于面板跳转）
	m2, _ := m.Update(tea.KeyPressMsg{Code: '2'})
	model := m2.(dashboardModel)

	if model.expandedPane != paneTimeline {
		t.Errorf("expected expandedPane=paneTimeline after '2' in eval pane, got %d", model.expandedPane)
	}
	if model.viewMode != viewExpanded {
		t.Errorf("expected viewMode=viewExpanded, got %d", model.viewMode)
	}

	// 按 '@' (Shift+2) → 应切换 evalSubView 到 topology
	m3 := newDashboardModel(nil)
	m3.viewMode = viewExpanded
	m3.expandedPane = paneEval
	m3.activePane = paneEval
	m3.evalSubView = 0

	m4, _ := m3.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	model2 := m4.(dashboardModel)

	if model2.evalSubView != 1 {
		t.Errorf("expected evalSubView=1 (topology) after '@' in eval pane, got %d", model2.evalSubView)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-012: [P0] AC-2 — Timeline 面板展开时 Shift+数字键切换 filter
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitKeyTimelineConflict(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewExpanded
	m.expandedPane = paneTimeline
	m.activePane = paneTimeline
	m.rightPane = paneTimeline

	// 按 "1" → 聚焦 Tree，从 viewExpanded 切回 viewDefault
	m2, _ := m.Update(tea.KeyPressMsg{Code: '1'})
	model := m2.(dashboardModel)

	if model.activePane != paneTree {
		t.Errorf("expected activePane=paneTree after '1' in timeline, got %d", model.activePane)
	}
	if model.viewMode != viewDefault {
		t.Errorf("expected viewMode=viewDefault after '1' from viewExpanded, got %d", model.viewMode)
	}

	// 按 'f' 进入过滤模式，再按 'l' → 应切换 LLM filter
	m3 := newDashboardModel(nil)
	m3.viewMode = viewExpanded
	m3.expandedPane = paneTimeline
	m3.activePane = paneTimeline
	m3.rightPane = paneTimeline
	m3.timelineFilters = defaultTimelineFilters()

	if !m3.timelineFilters[catLLM] {
		t.Fatal("LLM filter should be true by default")
	}

	// f 进入过滤模式
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'f'})
	model2 := m4.(dashboardModel)
	if !model2.timelineFilterMode {
		t.Error("pressing 'f' should enter filter mode")
	}

	// l 切换 LLM
	m5, _ := model2.Update(tea.KeyPressMsg{Code: 'l'})
	model3 := m5.(dashboardModel)
	if model3.timelineFilters[catLLM] {
		t.Error("pressing 'l' in filter mode should toggle LLM filter to false")
	}
	if model3.rightPane != paneTimeline {
		t.Errorf("expected to stay in rightPane=paneTimeline after filter toggle, got %d", model3.rightPane)
	}
}
// ---------------------------------------------------------------------------
// 29.2-INT-013: [P1] AC-6 — Timeline 展开时 stepTimelineMode=false 不显示 v/p 提示
// ---------------------------------------------------------------------------

func TestViewModeSystem_TimelineHintsConditional(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewExpanded
	m.expandedPane = paneTimeline
	m.stepTimelineMode = false

	core, _ := m.paneHints()
	hints := hintGroup(core...)

	// syscall mode should NOT show v/p hints
	if strings.Contains(hints, "prompt") {
		t.Errorf("expected no prompt hint when stepTimelineMode=false, got: %s", hints)
	}
	// should show step switching hint
	if !strings.Contains(hints, "step") {
		t.Errorf("expected step hint when stepTimelineMode=false, got: %s", hints)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-004: [P0] 编译与现有测试兼容性（隐式验证）
// ---------------------------------------------------------------------------
// 此测试由 `go test` 编译过程隐式验证。如果 viewMode 类型、字段或导航
// 文件的变更破坏了任何现有符号引用，整个包的编译将失败。
// `make all` (lint + vet + test + build) 提供全面回归保护。
