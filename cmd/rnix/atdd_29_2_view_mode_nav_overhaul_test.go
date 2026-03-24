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
		"jumpToPane",
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
// 29.2-UNIT-006: [P0] AC-2 — jumpToPane toggle 行为：第一次展开，再次回默认
// ---------------------------------------------------------------------------

func TestViewModeSystem_JumpToPaneToggle(t *testing.T) {
	m := newDashboardModel(nil)

	// 首次按：从 viewDefault 跳转到 viewExpanded + paneTimeline
	m2, _ := m.jumpToPane(paneTimeline)
	m = m2.(dashboardModel)
	if m.viewMode != viewExpanded {
		t.Errorf("expected viewExpanded after first jump, got %d", m.viewMode)
	}
	if m.expandedPane != paneTimeline {
		t.Errorf("expected expandedPane=paneTimeline, got %d", m.expandedPane)
	}
	if m.activePane != paneTimeline {
		t.Errorf("expected activePane=paneTimeline, got %d", m.activePane)
	}

	// 再次按同一面板：toggle 回 viewDefault
	m2, _ = m.jumpToPane(paneTimeline)
	m = m2.(dashboardModel)
	if m.viewMode != viewDefault {
		t.Errorf("expected viewDefault after toggle, got %d", m.viewMode)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-007: [P0] AC-2 — jumpToPane 切换到不同面板时保持 viewExpanded
// ---------------------------------------------------------------------------

func TestViewModeSystem_JumpToDifferentPane(t *testing.T) {
	m := newDashboardModel(nil)

	// 先跳到 paneTimeline
	m2, _ := m.jumpToPane(paneTimeline)
	m = m2.(dashboardModel)

	// 再跳到 paneTrace（不同面板 → 不 toggle，而是切换）
	m2, _ = m.jumpToPane(paneTrace)
	m = m2.(dashboardModel)
	if m.viewMode != viewExpanded {
		t.Errorf("expected viewExpanded when jumping to different pane, got %d", m.viewMode)
	}
	if m.expandedPane != paneTrace {
		t.Errorf("expected expandedPane=paneTrace, got %d", m.expandedPane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-UNIT-008: [P0] AC-2 — 所有 8 个面板都可通过 jumpToPane 展开
// ---------------------------------------------------------------------------

func TestViewModeSystem_AllPanesJumpable(t *testing.T) {
	panes := []paneType{
		paneTree, paneTimeline, paneHeatmap, paneDetail,
		paneIntent, paneSecurity, paneTrace, paneEval,
	}

	for _, p := range panes {
		m := newDashboardModel(nil)
		m2, _ := m.jumpToPane(p)
		m = m2.(dashboardModel)

		if m.viewMode != viewExpanded {
			t.Errorf("pane %d: expected viewExpanded, got %d", p, m.viewMode)
		}
		if m.expandedPane != p {
			t.Errorf("pane %d: expected expandedPane=%d, got %d", p, p, m.expandedPane)
		}
		if m.activePane != p {
			t.Errorf("pane %d: expected activePane=%d, got %d", p, p, m.activePane)
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

	title := m.renderDashboardTitle()

	// 展开的面板应显示 2:Time* 而不是 [2]Time
	if !strings.Contains(title, "2:") {
		t.Errorf("expected expanded pane to show '2:' prefix, got: %s", title)
	}
	if !strings.Contains(title, "*") {
		t.Errorf("expected expanded pane to show '*' suffix, got: %s", title)
	}

	// 其他面板仍显示 [N]Name 格式
	if !strings.Contains(title, "[1]") {
		t.Errorf("expected non-expanded pane to show '[1]', got: %s", title)
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

	// 默认视图应包含 1-8 jump 提示
	requiredHints := []string{"1-8", "Tab", "q"}
	for _, hint := range requiredHints {
		if !strings.Contains(status, hint) {
			t.Errorf("expected default status bar to contain %q, got: %s", hint, status)
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
	if !strings.Contains(status, "Esc") {
		t.Errorf("expected expanded status bar to contain 'Esc' hint, got: %s", status)
	}
	if !strings.Contains(status, "1-8") {
		t.Errorf("expected expanded status bar to contain '1-8' hint, got: %s", status)
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
		if slices.Contains(funcs, "paneSpecificHints") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected paneSpecificHints method to exist in dashboard.go or dashboard_nav.go")
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

	// 标题应反映树面板展开状态
	if !strings.Contains(output, "1:") && !strings.Contains(output, "Tree*") {
		t.Error("expected title to indicate tree pane is expanded")
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

	// 操作快捷键保留
	if !strings.Contains(status, "Kill") {
		t.Errorf("expected status bar to contain 'Kill' operation, got: %s", status)
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
// 29.2-INT-005: [P0] AC-7 — Tab 键经 dashboardKey 进入 viewExpanded
// ---------------------------------------------------------------------------

func TestViewModeSystem_TabKeyEntersExpanded(t *testing.T) {
	m := newDashboardModel(nil)
	m.activePane = paneTree // 初始面板

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model := m2.(dashboardModel)

	if model.viewMode != viewExpanded {
		t.Errorf("expected viewExpanded after Tab, got %d", model.viewMode)
	}
	if model.activePane != paneTimeline {
		t.Errorf("expected activePane=paneTimeline after Tab from paneTree, got %d", model.activePane)
	}
	if model.expandedPane != paneTimeline {
		t.Errorf("expected expandedPane=paneTimeline after Tab, got %d", model.expandedPane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-006: [P0] AC-4 — Shift-Tab 键经 dashboardKey 反向导航
// ---------------------------------------------------------------------------

func TestViewModeSystem_ShiftTabKeyReverseCycle(t *testing.T) {
	m := newDashboardModel(nil)
	m.activePane = paneTree // paneTree = 0

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model := m2.(dashboardModel)

	// (0 + 7) % 8 = 7 = paneEval
	if model.activePane != paneEval {
		t.Errorf("expected activePane=paneEval after Shift-Tab from paneTree, got %d", model.activePane)
	}
	if model.viewMode != viewExpanded {
		t.Errorf("expected viewExpanded after Shift-Tab, got %d", model.viewMode)
	}
	if model.expandedPane != paneEval {
		t.Errorf("expected expandedPane=paneEval after Shift-Tab, got %d", model.expandedPane)
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
// 29.2-INT-009: [P0] AC-2 — 数字键经 dashboardKey 跳转到面板
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitKeyJumpsToPane(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewDefault

	// 按 "3" → paneHeatmap (index 2)
	m2, _ := m.Update(tea.KeyPressMsg{Code: '3'})
	model := m2.(dashboardModel)

	if model.viewMode != viewExpanded {
		t.Errorf("expected viewExpanded after digit key, got %d", model.viewMode)
	}
	if model.expandedPane != paneHeatmap {
		t.Errorf("expected expandedPane=paneHeatmap, got %d", model.expandedPane)
	}
	if model.activePane != paneHeatmap {
		t.Errorf("expected activePane=paneHeatmap, got %d", model.activePane)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-010: [P0] AC-2 — 数字键 toggle：再次按同一键回到 viewDefault
// ---------------------------------------------------------------------------

func TestViewModeSystem_DigitKeyToggleBack(t *testing.T) {
	m := newDashboardModel(nil)
	m.viewMode = viewExpanded
	m.expandedPane = paneHeatmap
	m.activePane = paneHeatmap

	// 再按 "3" → toggle 回 viewDefault
	m2, _ := m.Update(tea.KeyPressMsg{Code: '3'})
	model := m2.(dashboardModel)

	if model.viewMode != viewDefault {
		t.Errorf("expected viewDefault after toggle, got %d", model.viewMode)
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

	// 按 "1" → 应跳转到 paneTree（数字键始终用于面板跳转）
	m2, _ := m.Update(tea.KeyPressMsg{Code: '1'})
	model := m2.(dashboardModel)

	if model.expandedPane != paneTree {
		t.Errorf("expected expandedPane=paneTree after '1' in timeline, got %d", model.expandedPane)
	}
	if model.viewMode != viewExpanded {
		t.Errorf("expected viewMode=viewExpanded, got %d", model.viewMode)
	}

	// 按 '!' (Shift+1) → 应切换 timeline filter
	m3 := newDashboardModel(nil)
	m3.viewMode = viewExpanded
	m3.expandedPane = paneTimeline
	m3.activePane = paneTimeline
	m3.timelineFilters = defaultTimelineFilters()

	if !m3.timelineFilters[catLLM] {
		t.Fatal("LLM filter should be true by default")
	}

	m4, _ := m3.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	model2 := m4.(dashboardModel)

	if model2.timelineFilters[catLLM] {
		t.Error("pressing '!' should toggle LLM filter to false")
	}
	if model2.expandedPane != paneTimeline {
		t.Errorf("expected to stay in paneTimeline after '!', got %d", model2.expandedPane)
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

	hints := m.paneSpecificHints()

	if strings.Contains(hints, "v:expand") || strings.Contains(hints, "p:prompt") {
		t.Errorf("expected no v/p hints when stepTimelineMode=false, got: %s", hints)
	}
	if !strings.Contains(hints, "h/l:scroll") {
		t.Errorf("expected h/l:scroll hint regardless of stepTimelineMode, got: %s", hints)
	}
}

// ---------------------------------------------------------------------------
// 29.2-INT-004: [P0] 编译与现有测试兼容性（隐式验证）
// ---------------------------------------------------------------------------
// 此测试由 `go test` 编译过程隐式验证。如果 viewMode 类型、字段或导航
// 文件的变更破坏了任何现有符号引用，整个包的编译将失败。
// `make all` (lint + vet + test + build) 提供全面回归保护。
