package main

// Story 29.5 (revised): H key expands Agent Tree (viewExpanded+paneTree)
// History view removed; H no longer opens a separate history overlay.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readNavFiles concatenates dashboard_nav.go and dashboard_pane_dispatcher.go.
// Story 38.1 split the original 812-line nav.go into a 50-line entry plus the
// pane_dispatcher.go helper file; tests that grep for code patterns now look
// at both files together so the legacy assertions still hold.
func readNavFiles(t *testing.T) string {
	t.Helper()
	parts := []string{}
	for _, name := range []string{"dashboard_nav.go", "dashboard_pane_dispatcher.go", "dashboard_keylayers.go"} {
		data, err := os.ReadFile(filepath.Join(cmdRnixDir(), name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("failed to read %s: %v", name, err)
		}
		parts = append(parts, string(data))
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-001: dashboard_history.go 存在（保留 agentLabel + renderHistoryStats）
// ---------------------------------------------------------------------------

func TestHistoryView_FileExists(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected dashboard_history.go to exist (contains agentLabel and renderHistoryStats)")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-002: 辅助函数存在于正确文件中
// ---------------------------------------------------------------------------

func TestHistoryView_ExpectedFunctionsExist(t *testing.T) {
	histFuncs, err := topLevelFuncNames(filepath.Join(cmdRnixDir(), "dashboard_history.go"))
	if err != nil {
		t.Fatalf("failed to parse dashboard_history.go: %v", err)
	}
	histSet := make(map[string]bool)
	for _, fn := range histFuncs {
		histSet[fn] = true
	}
	for _, fn := range []string{"agentLabel", "renderHistoryStats"} {
		if !histSet[fn] {
			t.Errorf("expected function %s in dashboard_history.go", fn)
		}
	}

	// Story 38.1: handleExpandedTreeKey moved from dashboard_nav.go to
	// dashboard_pane_dispatcher.go as part of the 3-layer dispatcher refactor.
	navFuncs, err := topLevelFuncNames(filepath.Join(cmdRnixDir(), "dashboard_nav.go"))
	if err != nil {
		t.Fatalf("failed to parse dashboard_nav.go: %v", err)
	}
	paneFuncs, _ := topLevelFuncNames(filepath.Join(cmdRnixDir(), "dashboard_pane_dispatcher.go"))

	navSet := make(map[string]bool)
	for _, fn := range navFuncs {
		navSet[fn] = true
	}
	for _, fn := range paneFuncs {
		navSet[fn] = true
	}
	if !navSet["handleExpandedTreeKey"] {
		t.Error("expected handleExpandedTreeKey in dashboard_nav.go or dashboard_pane_dispatcher.go")
	}

	treeFuncs, err := topLevelFuncNames(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to parse dashboard_tree.go: %v", err)
	}
	treeSet := make(map[string]bool)
	for _, fn := range treeFuncs {
		treeSet[fn] = true
	}
	if !treeSet["filteredExpandedRows"] {
		t.Error("expected filteredExpandedRows in dashboard_tree.go")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-003: dashboard_types.go 不再包含 historyProcsMsg
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryProcsMsgTypeExists(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_types.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_types.go: %v", err)
	}
	if strings.Contains(string(data), "type historyProcsMsg struct") {
		t.Error("dashboard_types.go should NOT contain historyProcsMsg - history view removed")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-004: dashboardModel 包含展开树搜索字段，不包含旧历史字段
// ---------------------------------------------------------------------------

func TestHistoryView_DashboardModelHistoryFields(t *testing.T) {
	// Story 38-5 PR2: TreeState 抽离 — 4 个 treeSearch* 字段从 dashboardModel 迁到
	// internal/dashboard/tree.TreeState。本测试拆为两段：
	//   1) dashboardModel 不再含 history* 老字段（无回退）；
	//   2) TreeState 含 SearchQuery/SearchMode/SearchCursor/SearchOffset 4 字段。
	dashPath := filepath.Join(cmdRnixDir(), "dashboard.go")
	fset := token.NewFileSet()
	df, err := parser.ParseFile(fset, dashPath, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse dashboard.go: %v", err)
	}
	var dashStruct *ast.StructType
	ast.Inspect(df, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.Name == "dashboardModel" {
			if st, ok2 := ts.Type.(*ast.StructType); ok2 {
				dashStruct = st
			}
		}
		return true
	})
	if dashStruct == nil {
		t.Fatal("dashboardModel struct not found in dashboard.go")
	}
	dashFields := make(map[string]bool)
	for _, field := range dashStruct.Fields.List {
		for _, name := range field.Names {
			dashFields[name.Name] = true
		}
	}
	for _, fn := range []string{"historyProcs", "historyCursor", "historyScrollOffset", "historySortMode", "historySearchQuery", "historySearchMode"} {
		if dashFields[fn] {
			t.Errorf("dashboardModel should NOT have old history field %q", fn)
		}
	}
	// PR2 落地后 dashboardModel 必须有 tree 字段（类型 tree.TreeState）
	if !dashFields["tree"] {
		t.Error("dashboardModel should have field \"tree\" (Story 38-5 PR2: TreeState 抽离)")
	}

	// 验证 TreeState 含 4 个 search 字段
	statePath := filepath.Join(cmdRnixDir(), "..", "..", "internal", "dashboard", "tree", "state.go")
	sf, err := parser.ParseFile(fset, statePath, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse internal/dashboard/tree/state.go: %v", err)
	}
	var stateStruct *ast.StructType
	ast.Inspect(sf, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.Name == "TreeState" {
			if st, ok2 := ts.Type.(*ast.StructType); ok2 {
				stateStruct = st
			}
		}
		return true
	})
	if stateStruct == nil {
		t.Fatal("TreeState struct not found in internal/dashboard/tree/state.go")
	}
	stateFields := make(map[string]bool)
	for _, field := range stateStruct.Fields.List {
		for _, name := range field.Names {
			stateFields[name.Name] = true
		}
	}
	for _, fn := range []string{"SearchQuery", "SearchMode", "SearchCursor", "SearchOffset"} {
		if !stateFields[fn] {
			t.Errorf("expected TreeState to have field %q (Story 38-5 PR2: TreeState 抽离)", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-005: H 键设置 viewMode=viewExpanded, expandedPane=paneTree
// ---------------------------------------------------------------------------

// 29.5-UNIT-005: H key no longer exists in dashboard_nav.go (z expand is used instead)
func TestHistoryView_EnterHistoryViewSetsViewMode(t *testing.T) {
	content := readNavFiles(t)
	if strings.Contains(content, "case \"H\":") {
		t.Error("H key case should have been removed from nav files (z expand replaces it)")
	}
	// z key expands the tree pane
	if !strings.Contains(content, "viewExpanded") {
		t.Error("viewExpanded should still exist in nav files for z key")
	}
	if !strings.Contains(content, "paneTree") {
		t.Error("paneTree should still exist in nav files for z key")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-006: dashboard.go tick 调用 ListAllProcs
// ---------------------------------------------------------------------------

func TestHistoryView_FetchAllProcsCmdCallsListAllProcs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	if !strings.Contains(string(data), "ListAllProcs") {
		t.Error("dashboard.go tick should call ListAllProcs to show all processes in tree")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-007: H 键不再调用 enterHistoryView
// ---------------------------------------------------------------------------

// 29.5-UNIT-007: H key removed; z expand is used instead
func TestHistoryView_HKeyCallsEnterHistoryView(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_nav.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_nav.go: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "enterHistoryView") {
		t.Error("dashboard_nav.go should NOT call enterHistoryView (history view removed)")
	}
	if strings.Contains(content, "case \"H\":") {
		t.Error("H key case should have been removed from dashboard_nav.go (z expand replaces it)")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-008: handleExpandedTreeKey 在展开 Tree 模式下被分发
// ---------------------------------------------------------------------------

func TestHistoryView_OverlayLayerInNav(t *testing.T) {
	content := readNavFiles(t)
	if !strings.Contains(content, "handleExpandedTreeKey") {
		t.Error("nav files should call handleExpandedTreeKey in expanded tree mode")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-009: dashboard.go Update 不再处理 historyProcsMsg
// ---------------------------------------------------------------------------

func TestHistoryView_UpdateHandlesHistoryProcsMsg(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	if strings.Contains(string(data), "historyProcsMsg") {
		t.Error("dashboard.go should NOT handle historyProcsMsg (history view removed)")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-010: renderDashboard 路由 renderExpandedLayout，不路由 renderHistoryView
// ---------------------------------------------------------------------------

func TestHistoryView_ViewRoutesViewHistory(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	content := string(data)
	funcBody := extractFuncBody(content, "renderDashboard")
	if funcBody == "" {
		t.Fatal("renderDashboard function not found in dashboard.go")
	}
	if strings.Contains(funcBody, "renderHistoryView") {
		t.Error("renderDashboard should NOT call renderHistoryView (history view removed)")
	}
	if !strings.Contains(funcBody, "renderExpandedLayout") {
		t.Error("renderDashboard should call renderExpandedLayout for viewExpanded mode")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-011: renderDashboardTreePane 展开模式显示 Result（REASON 列）
// ---------------------------------------------------------------------------

func TestHistoryView_RenderContainsTableHeaders(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	funcBody := extractFuncBody(content, "renderDashboardTreePane")
	if funcBody == "" {
		t.Fatal("renderDashboardTreePane function not found in dashboard_tree.go")
	}
	if !strings.Contains(funcBody, "Result") {
		t.Error("renderDashboardTreePane should show proc.Result text in expanded mode (REASON column)")
	}
	if !strings.Contains(funcBody, "renderHistoryStats") {
		t.Error("renderDashboardTreePane should call renderHistoryStats for expanded stats bar")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-012: renderHistoryStats 显示 Running/Done/Failed 统计
// ---------------------------------------------------------------------------

func TestHistoryView_RenderContainsBottomStats(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_history.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)
	funcBody := extractFuncBody(content, "renderHistoryStats")
	if funcBody == "" {
		t.Fatal("renderHistoryStats function not found in dashboard_history.go")
	}
	if !strings.Contains(funcBody, "Running") {
		t.Error("renderHistoryStats should display Running count")
	}
	if !strings.Contains(funcBody, "Done") || !strings.Contains(funcBody, "Failed") {
		t.Error("renderHistoryStats should display Done/Failed counts")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-013: handleExpandedTreeKey 处理 j/k 导航
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesJKNavigation(t *testing.T) {
	content := readNavFiles(t)
	funcBody := extractFuncBody(content, "handleExpandedTreeKey")
	if funcBody == "" {
		t.Fatal("handleExpandedTreeKey not found in nav files")
	}
	hasJK := strings.Contains(funcBody, `"j"`) || strings.Contains(funcBody, `"k"`)
	hasUD := strings.Contains(funcBody, `"up"`) || strings.Contains(funcBody, `"down"`)
	if !hasJK && !hasUD {
		t.Error("handleExpandedTreeKey should handle j/k or up/down for cursor navigation")
	}
	if !strings.Contains(funcBody, "tree.SearchCursor") {
		t.Error("handleExpandedTreeKey should modify tree.SearchCursor for navigation (Story 38-5 PR2: TreeState 抽离)")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-014: handleExpandedTreeKey Enter 后调用 handlePIDChange
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesEnterFocus(t *testing.T) {
	content := readNavFiles(t)
	funcBody := extractFuncBody(content, "handleExpandedTreeKey")
	if funcBody == "" {
		t.Fatal("handleExpandedTreeKey not found in nav files")
	}
	if !strings.Contains(funcBody, "handlePIDChange") {
		t.Error("handleExpandedTreeKey should call handlePIDChange after Enter")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-015: L 键由全局层 enterStepInspector 处理
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesLKey(t *testing.T) {
	content := readNavFiles(t)
	if !strings.Contains(content, "enterStepInspector") {
		t.Error("nav files should call enterStepInspector for L key (global handler)")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-016: handleExpandedTreeKey 处理 / 进入搜索模式
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesSearchMode(t *testing.T) {
	content := readNavFiles(t)
	funcBody := extractFuncBody(content, "handleExpandedTreeKey")
	if funcBody == "" {
		t.Fatal("handleExpandedTreeKey not found in nav files")
	}
	if !strings.Contains(funcBody, `"/"`) {
		t.Error("handleExpandedTreeKey should handle / key to enter search mode")
	}
	if !strings.Contains(funcBody, "tree.SearchMode") {
		t.Error("handleExpandedTreeKey should reference tree.SearchMode field")
	}
	if !strings.Contains(funcBody, "tree.SearchQuery") {
		t.Error("handleExpandedTreeKey should reference tree.SearchQuery field")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-017: filteredExpandedRows 使用 agentLabel + Intent 进行过滤
// ---------------------------------------------------------------------------

func TestHistoryView_SearchFiltersByAgentName(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	funcBody := extractFuncBody(content, "filteredExpandedRows")
	if funcBody == "" {
		t.Fatal("filteredExpandedRows not found in dashboard_tree.go")
	}
	if !strings.Contains(funcBody, "ToLower") {
		t.Error("filteredExpandedRows should use strings.ToLower for case-insensitive search")
	}
	if !strings.Contains(funcBody, "agentLabel") {
		t.Error("filteredExpandedRows should filter by agentLabel")
	}
	if !strings.Contains(funcBody, "Intent") && !strings.Contains(funcBody, "intent") {
		t.Error("filteredExpandedRows should filter by intent")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-018: Agent Tree 内置排序 treeSortMode 已定义
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesSortModes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "treeSortLabels") {
		t.Error("dashboard_tree.go should define treeSortLabels for sort mode display")
	}
	if !strings.Contains(content, "treeSortMode") {
		t.Error("dashboard_tree.go should reference treeSortMode for sort mode switching")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-019: Agent Tree 排序使用 sort.SliceStable 或 slices.SortFunc
// ---------------------------------------------------------------------------

func TestHistoryView_SortUsesSlicesSortFunc(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "SliceStable") && !strings.Contains(content, "SortFunc") {
		t.Error("dashboard_tree.go should use sort.SliceStable or slices.SortFunc for sorting")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-020: handleExpandedTreeKey Esc 先清搜索词再退出展开模式
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesEscExit(t *testing.T) {
	content := readNavFiles(t)
	funcBody := extractFuncBody(content, "handleExpandedTreeKey")
	if funcBody == "" {
		t.Fatal("handleExpandedTreeKey not found in nav files")
	}
	if !strings.Contains(funcBody, `"esc"`) {
		t.Error("handleExpandedTreeKey should handle esc key")
	}
	if !strings.Contains(funcBody, "tree.SearchQuery") {
		t.Error("handleExpandedTreeKey esc should clear tree.SearchQuery")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-021: Tree 面板使用 ListAllProcs（含历史进程）
// ---------------------------------------------------------------------------

func TestHistoryView_TreePaneUsesListAllProcs(t *testing.T) {
	treeData, _ := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if strings.Contains(string(treeData), "ListAllProcs") {
		return
	}
	dashData, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	if !strings.Contains(string(dashData), "ListAllProcs") {
		t.Error("dashboard.go should use ListAllProcs to include historical processes")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-022: Tree 面板使用 StateBadge 或 StateSymbol 显示进程状态
// ---------------------------------------------------------------------------

func TestHistoryView_TreePaneUsesStateSymbol(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "StateSymbol") && !strings.Contains(content, "StateBadge") {
		t.Error("dashboard_tree.go should use ui.StateSymbol() or ui.StateBadge() for process state symbols")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-023: renderDashboardTreePane 已结束进程显示 DeadAt 存活时间
// ---------------------------------------------------------------------------

func TestHistoryView_TreePaneShowsExitCodeAndElapsed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	funcBody := extractFuncBody(content, "renderDashboardTreePane")
	if funcBody == "" {
		t.Fatal("renderDashboardTreePane not found in dashboard_tree.go")
	}
	if !strings.Contains(funcBody, "DeadAt") {
		t.Error("renderDashboardTreePane should reference DeadAt for dead process elapsed time")
	}
	if !strings.Contains(funcBody, "StateDead") && !strings.Contains(funcBody, "Dead") {
		t.Error("renderDashboardTreePane should check for Dead state to show exit info")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-024: Timeline 自动过滤 Dead 进程事件（无变化）
// ---------------------------------------------------------------------------

func TestHistoryView_TimelineAutoFilterDeadProcess(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_timeline.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_timeline.go: %v", err)
	}
	content := string(data)
	hasFilter := strings.Contains(content, "filteredStepEntries") ||
		strings.Contains(content, "FilterByPID") ||
		strings.Contains(content, "filteredEvents") ||
		strings.Contains(content, "stepFilters")
	if !hasFilter {
		t.Error("dashboard_timeline.go should contain timeline event filtering logic for Dead processes")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-025: Status bar 显示 "(PID N)" dead process 指示
// ---------------------------------------------------------------------------

func TestHistoryView_StatusBarShowsFilteredHint(t *testing.T) {
	for _, name := range []string{"dashboard.go", "dashboard_timeline.go", "dashboard_status.go"} {
		data, err := os.ReadFile(filepath.Join(cmdRnixDir(), name))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "(PID %d)") {
			return
		}
	}
	t.Error("dashboard files should contain '(PID N)' status message for Dead process timeline filtering")
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-026: dashboard_tree.go 使用 StateBadge/StateSymbol（替代旧 history.go）
// ---------------------------------------------------------------------------

func TestHistoryView_RenderUsesStateSymbol(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "StateBadge") && !strings.Contains(content, "StateSymbol") {
		t.Error("dashboard_tree.go should use ui.StateBadge or ui.StateSymbol for process state rendering")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-027: renderDashboardTreePane 方法签名正确
// ---------------------------------------------------------------------------

func TestHistoryView_RenderFuncSignature(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	if !strings.Contains(string(data), "func (m dashboardModel) renderDashboardTreePane(") {
		t.Error("renderDashboardTreePane should be a method on dashboardModel")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-028: handleExpandedTreeKey 方法签名正确
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyFuncSignature(t *testing.T) {
	content := readNavFiles(t)
	if !strings.Contains(content, "func (m dashboardModel) handleExpandedTreeKey(") {
		t.Error("handleExpandedTreeKey should be a method on dashboardModel")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-029: H 键处理中清空 tree.SearchQuery
// ---------------------------------------------------------------------------

// 29.5-UNIT-029: H key has been removed; z expand pane replaces it
func TestHistoryView_EnterHistoryViewSignature(t *testing.T) {
	content := readNavFiles(t)
	if strings.Contains(content, "case \"H\":") {
		t.Error("H key case should have been removed from nav files")
	}
	// z key expands tree and should clear search state
	if !strings.Contains(content, "tree.SearchQuery") {
		t.Error("z expand handler should reset tree.SearchQuery (Story 38-5 PR2: TreeState 抽离)")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-030: dashboard_history.go 使用 package main
// ---------------------------------------------------------------------------

func TestHistoryView_PackageMain(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	if err != nil {
		t.Fatalf("failed to parse dashboard_history.go: %v", err)
	}
	if f.Name.Name != "main" {
		t.Errorf("dashboard_history.go has package %q, expected \"main\"", f.Name.Name)
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-031: 搜索 Backspace 使用 rune-safe 截断
// ---------------------------------------------------------------------------

func TestHistoryView_SearchBackspaceRuneSafe(t *testing.T) {
	content := readNavFiles(t)
	funcBody := extractFuncBody(content, "handleExpandedTreeKey")
	if funcBody == "" {
		t.Fatal("handleExpandedTreeKey not found in nav files")
	}
	hasRuneSafe := strings.Contains(funcBody, "DecodeLastRune") ||
		strings.Contains(funcBody, "[]rune") ||
		strings.Contains(funcBody, "truncateRuneSafe") ||
		strings.Contains(funcBody, "utf8.")
	if !hasRuneSafe {
		t.Error("handleExpandedTreeKey should use rune-safe string truncation for Backspace in search mode")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-032: renderDashboardTreePane 展开模式包含搜索提示或标题
// ---------------------------------------------------------------------------

func TestHistoryView_RenderContainsTitleBar(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	funcBody := extractFuncBody(content, "renderDashboardTreePane")
	if funcBody == "" {
		t.Fatal("renderDashboardTreePane not found in dashboard_tree.go")
	}
	hasTitle := strings.Contains(funcBody, "Agent Tree") ||
		strings.Contains(funcBody, "/ to search") ||
		strings.Contains(funcBody, "tree.SearchQuery")
	if !hasTitle {
		t.Error("renderDashboardTreePane should show search hint or Agent Tree title in expanded mode")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-033: tree.SearchQuery 字段类型为 string（Story 38-5 PR2: TreeState 抽离）
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryProcsMsgHasProcsField(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "internal/dashboard/tree/state.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// Fallback: project root prefix differs in some test environments
		path = filepath.Join("..", "..", "internal", "dashboard", "tree", "state.go")
		f, err = parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("failed to parse internal/dashboard/tree/state.go: %v", err)
		}
	}
	var structType *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.Name == "TreeState" {
			if st, ok2 := ts.Type.(*ast.StructType); ok2 {
				structType = st
			}
		}
		return true
	})
	if structType == nil {
		t.Fatal("TreeState struct not found in internal/dashboard/tree/state.go")
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if name.Name == "SearchQuery" {
				if ident, ok := field.Type.(*ast.Ident); ok {
					if ident.Name != "string" {
						t.Errorf("TreeState.SearchQuery should be type string, got %s", ident.Name)
					}
				}
				return
			}
		}
	}
	t.Error("TreeState.SearchQuery field not found")
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-034: Tree 排序包含 treeSortTime/treeSortPID 及 CreatedAt
// ---------------------------------------------------------------------------

func TestHistoryView_ThreeSortModesImplemented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cmdRnixDir(), "dashboard_tree.go"))
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "CreatedAt") {
		t.Error("dashboard_tree.go should sort by CreatedAt for time-based sorting")
	}
	if !strings.Contains(content, "treeSortTime") || !strings.Contains(content, "treeSortPID") {
		t.Error("dashboard_tree.go should define treeSortTime and treeSortPID constants")
	}
	if !strings.Contains(content, "PID") {
		t.Error("dashboard_tree.go should sort by PID")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-035: handleExpandedTreeKey Enter 调用 handlePIDChange
// ---------------------------------------------------------------------------

func TestHistoryView_EnterFocusCallsHandlePIDChange(t *testing.T) {
	content := readNavFiles(t)
	funcBody := extractFuncBody(content, "handleExpandedTreeKey")
	if funcBody == "" {
		t.Fatal("handleExpandedTreeKey not found in nav files")
	}
	if !strings.Contains(funcBody, "handlePIDChange") {
		t.Error("handleExpandedTreeKey should call handlePIDChange after Enter focus")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-036: dashboard_history.go 导入 vfs 包
// ---------------------------------------------------------------------------

func TestHistoryView_ImportsVFS(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("failed to parse dashboard_history.go imports: %v", err)
	}
	for _, imp := range f.Imports {
		if strings.Contains(imp.Path.Value, "vfs") {
			return
		}
	}
	t.Error("dashboard_history.go should import vfs package (renderHistoryStats takes []vfs.ProcInfo)")
}

