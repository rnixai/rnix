package main

// =============================================================================
// ATDD Story 29.5: Dashboard 历史进程视图 (TDD RED PHASE)
// =============================================================================
//
// Test Strategy:
//   AC-1:  H 键进入历史视图 — viewHistory 覆盖层，fetchAllProcsCmd()
//   AC-2:  进程表渲染 — 列：PID|ST|AGENT|MODEL|TOKENS|CREATED|ELAPSED|EXIT|REASON，底部统计
//   AC-3:  光标导航 — j/k 或上/下键在进程列表中导航
//   AC-4:  Enter 聚焦 — 设置 selectedPID + selectedUUID，回到默认视图
//   AC-5:  L 键跳转 LLM 查看器 — placeholder（Story 29.6 实现）
//   AC-6:  搜索过滤 — / 进入搜索模式，按 agent 名称过滤
//   AC-7:  排序模式 — 1/2/3 切换排序模式（时间/名称/PID）
//   AC-8:  Esc 退出 — 回到之前的视图
//   AC-9:  Tree 面板已结束进程 — ListAllProcs + ✓/✕ 状态符号 + exit code + 存活时间
//   AC-10: Timeline 自动过滤 — Dead 进程时过滤只显示该 PID 事件
//
// Priority: P0 (file existence, type/field existence, key dispatch, structural)
//           P1 (rendering content, search/sort behavior, timeline filter)
// Test Level: Unit (AST structure verification, source code analysis)
// TDD Phase: RED — all tests assert EXPECTED post-implementation behavior
//
// 注意：Go 的 TDD RED phase 不能引用尚未存在的方法/类型（不会编译）。
// 因此使用 AST 解析和源码文本分析来验证结构，而非直接调用方法。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 29.5-UNIT-001: [P0] AC-1,2,3,4,5,6,7,8 — dashboard_history.go 文件存在
// ---------------------------------------------------------------------------

func TestHistoryView_FileExists(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected dashboard_history.go to exist in cmd/rnix/")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-002: [P0] AC-1,2,3,6,7,8 — dashboard_history.go 包含预期函数
// ---------------------------------------------------------------------------

func TestHistoryView_ExpectedFunctionsExist(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	funcs, err := topLevelFuncNames(path)
	if err != nil {
		t.Fatalf("failed to parse dashboard_history.go: %v", err)
	}

	funcSet := make(map[string]bool)
	for _, fn := range funcs {
		funcSet[fn] = true
	}

	expectedFuncs := []string{
		"enterHistoryView",
		"historyKey",
		"renderHistoryView",
		"fetchAllProcsCmd",
	}
	for _, fn := range expectedFuncs {
		if !funcSet[fn] {
			t.Errorf("expected function %s in dashboard_history.go, not found", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-003: [P0] AC-1 — historyProcsMsg 消息类型定义于 dashboard_types.go
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryProcsMsgTypeExists(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_types.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "type historyProcsMsg struct") {
		t.Error("expected dashboard_types.go to contain 'type historyProcsMsg struct'")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-004: [P0] AC-1 — dashboardModel 包含历史视图字段
// ---------------------------------------------------------------------------

func TestHistoryView_DashboardModelHistoryFields(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse dashboard.go: %v", err)
	}

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

	requiredFields := []string{
		"historyProcs",
		"historyCursor",
		"historyScrollOffset",
		"historySortMode",
		"historySearchQuery",
		"historySearchMode",
	}
	for _, fn := range requiredFields {
		if !fieldNames[fn] {
			t.Errorf("expected dashboardModel to have '%s' field", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-005: [P0] AC-1 — enterHistoryView 设置 viewMode = viewHistory
// ---------------------------------------------------------------------------

func TestHistoryView_EnterHistoryViewSetsViewMode(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "enterHistoryView")
	if funcBody == "" {
		t.Fatal("enterHistoryView function not found in dashboard_history.go")
	}

	if !strings.Contains(funcBody, "viewHistory") {
		t.Error("enterHistoryView should set viewMode to viewHistory")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-006: [P0] AC-1 — fetchAllProcsCmd 调用 client.ListAllProcs()
// ---------------------------------------------------------------------------

func TestHistoryView_FetchAllProcsCmdCallsListAllProcs(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "fetchAllProcsCmd")
	if funcBody == "" {
		t.Fatal("fetchAllProcsCmd function not found in dashboard_history.go")
	}

	if !strings.Contains(funcBody, "ListAllProcs") {
		t.Error("fetchAllProcsCmd should call client.ListAllProcs()")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-007: [P0] AC-1 — H 键从 placeholder 改为调用 enterHistoryView
// ---------------------------------------------------------------------------

func TestHistoryView_HKeyCallsEnterHistoryView(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_nav.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_nav.go: %v", err)
	}
	content := string(data)

	// H 键不应再是 placeholder
	if strings.Contains(content, "历史视图尚未实现") {
		t.Error("dashboard_nav.go should no longer contain H key placeholder message '历史视图尚未实现'")
	}

	// H 键应调用 enterHistoryView
	if !strings.Contains(content, "enterHistoryView") {
		t.Error("dashboard_nav.go should call enterHistoryView when H key is pressed")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-008: [P0] AC-1,8 — viewHistory 覆盖层在 Layer 1.5 处理
// ---------------------------------------------------------------------------

func TestHistoryView_OverlayLayerInNav(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_nav.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_nav.go: %v", err)
	}
	content := string(data)

	// 应有 viewHistory 检查在 Kill 确认层之前
	if !strings.Contains(content, "viewHistory") {
		t.Error("dashboard_nav.go should check for viewHistory overlay mode")
	}

	// 应调用 historyKey 处理历史视图按键
	if !strings.Contains(content, "historyKey") {
		t.Error("dashboard_nav.go should call historyKey when viewMode == viewHistory")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-009: [P0] AC-1 — dashboard.go Update 处理 historyProcsMsg
// ---------------------------------------------------------------------------

func TestHistoryView_UpdateHandlesHistoryProcsMsg(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "historyProcsMsg") {
		t.Error("dashboard.go should handle historyProcsMsg in Update method")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-010: [P0] AC-1 — dashboard.go View 方法路由 viewHistory
// ---------------------------------------------------------------------------

func TestHistoryView_ViewRoutesViewHistory(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "renderDashboard")
	if funcBody == "" {
		t.Fatal("renderDashboard function not found in dashboard.go")
	}

	if !strings.Contains(funcBody, "viewHistory") {
		t.Error("renderDashboard should route viewHistory to renderHistoryView")
	}

	if !strings.Contains(funcBody, "renderHistoryView") {
		t.Error("renderDashboard should call renderHistoryView for viewHistory mode")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-011: [P0] AC-2 — renderHistoryView 包含进程表列标题
// ---------------------------------------------------------------------------

func TestHistoryView_RenderContainsTableHeaders(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "renderHistoryView")
	if funcBody == "" {
		t.Fatal("renderHistoryView function not found in dashboard_history.go")
	}

	// 验证关键列标题
	requiredHeaders := []string{"PID", "AGENT", "MODEL", "TOKENS"}
	for _, header := range requiredHeaders {
		if !strings.Contains(funcBody, header) {
			t.Errorf("renderHistoryView should contain column header %q", header)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-012: [P1] AC-2 — renderHistoryView 包含底部统计行
// ---------------------------------------------------------------------------

func TestHistoryView_RenderContainsBottomStats(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "renderHistoryView")
	if funcBody == "" {
		t.Fatal("renderHistoryView function not found in dashboard_history.go")
	}

	// 底部统计应包含 Running/Done/Failed 计数
	if !strings.Contains(funcBody, "Running") {
		t.Error("renderHistoryView should display Running count in bottom stats")
	}
	if !strings.Contains(funcBody, "Done") || !strings.Contains(funcBody, "Failed") {
		t.Error("renderHistoryView should display Done/Failed counts in bottom stats")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-013: [P0] AC-3 — historyKey 处理 j/k 导航
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesJKNavigation(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "historyKey")
	if funcBody == "" {
		t.Fatal("historyKey function not found in dashboard_history.go")
	}

	// j/k 或 up/down 导航
	hasJK := strings.Contains(funcBody, `"j"`) || strings.Contains(funcBody, `"k"`)
	hasUpDown := strings.Contains(funcBody, `"up"`) || strings.Contains(funcBody, `"down"`)
	if !hasJK && !hasUpDown {
		t.Error("historyKey should handle j/k or up/down for cursor navigation")
	}

	// 应修改 historyCursor
	if !strings.Contains(funcBody, "historyCursor") {
		t.Error("historyKey should modify historyCursor for navigation")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-014: [P0] AC-4 — historyKey 处理 Enter 聚焦
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesEnterFocus(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "historyKey")
	if funcBody == "" {
		t.Fatal("historyKey function not found in dashboard_history.go")
	}

	// Enter 应设置 selectedPID
	if !strings.Contains(funcBody, "selectedPID") {
		t.Error("historyKey should set selectedPID on Enter")
	}

	// Enter 应设置 selectedUUID
	if !strings.Contains(funcBody, "selectedUUID") {
		t.Error("historyKey should set selectedUUID on Enter")
	}

	// Enter 应回到 viewDefault
	if !strings.Contains(funcBody, "viewDefault") {
		t.Error("historyKey should set viewMode to viewDefault on Enter")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-015: [P0] AC-5 — historyKey 处理 L 键跳转
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesLKey(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "historyKey")
	if funcBody == "" {
		t.Fatal("historyKey function not found in dashboard_history.go")
	}

	// L 键应存在于 historyKey 中
	if !strings.Contains(funcBody, `"L"`) {
		t.Error("historyKey should handle 'L' key for LLM viewer jump")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-016: [P0] AC-6 — historyKey 处理 / 搜索模式
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesSearchMode(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "historyKey")
	if funcBody == "" {
		t.Fatal("historyKey function not found in dashboard_history.go")
	}

	// / 键进入搜索模式
	if !strings.Contains(funcBody, `"/"`) {
		t.Error("historyKey should handle '/' key to enter search mode")
	}

	// 应引用 historySearchMode
	if !strings.Contains(funcBody, "historySearchMode") {
		t.Error("historyKey should reference historySearchMode field")
	}

	// 应引用 historySearchQuery
	if !strings.Contains(funcBody, "historySearchQuery") {
		t.Error("historyKey should reference historySearchQuery field")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-017: [P1] AC-6 — 搜索过滤逻辑使用 agent 名称
// ---------------------------------------------------------------------------

func TestHistoryView_SearchFiltersByAgentName(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	// 搜索过滤应使用 strings.Contains + ToLower（case-insensitive）
	if !strings.Contains(content, "ToLower") {
		t.Error("dashboard_history.go should use strings.ToLower for case-insensitive search")
	}

	// 过滤应基于 Agent 字段
	if !strings.Contains(content, "Agent") || !strings.Contains(content, "Intent") {
		t.Error("dashboard_history.go search should filter by Agent or Intent field")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-018: [P0] AC-7 — historyKey 处理 1/2/3 排序模式切换
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesSortModes(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "historyKey")
	if funcBody == "" {
		t.Fatal("historyKey function not found in dashboard_history.go")
	}

	// 应处理 1/2/3 排序键
	if !strings.Contains(funcBody, "historySortMode") {
		t.Error("historyKey should modify historySortMode for sort mode switching")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-019: [P1] AC-7 — 排序逻辑使用 slices.SortFunc
// ---------------------------------------------------------------------------

func TestHistoryView_SortUsesSlicesSortFunc(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "slices.SortFunc") && !strings.Contains(content, "sort.Slice") {
		t.Error("dashboard_history.go should use slices.SortFunc or sort.Slice for sorting")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-020: [P0] AC-8 — historyKey 处理 Esc 退出
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyHandlesEscExit(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "historyKey")
	if funcBody == "" {
		t.Fatal("historyKey function not found in dashboard_history.go")
	}

	// Esc 应退出历史视图
	if !strings.Contains(funcBody, `"esc"`) {
		t.Error("historyKey should handle 'esc' key to exit history view")
	}

	// 应回到 viewDefault
	if !strings.Contains(funcBody, "viewDefault") {
		t.Error("historyKey should set viewMode to viewDefault on Esc")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-021: [P0] AC-9 — Tree 面板使用 ListAllProcs 数据
// ---------------------------------------------------------------------------

func TestHistoryView_TreePaneUsesListAllProcs(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_tree.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)

	// Tree 面板或 dashboard.go 中应引用 ListAllProcs
	if strings.Contains(content, "ListAllProcs") {
		return // found in tree
	}

	// 也可能在 dashboard.go 的 tick 中调用
	dashPath := filepath.Join(cmdRnixDir(), "dashboard.go")
	dashData, err := os.ReadFile(dashPath)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	dashContent := string(dashData)

	if !strings.Contains(dashContent, "ListAllProcs") {
		t.Error("dashboard.go or dashboard_tree.go should use ListAllProcs to include historical processes in tree")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-022: [P0] AC-9 — Tree 面板显示已结束进程使用 StateSymbol
// ---------------------------------------------------------------------------

func TestHistoryView_TreePaneUsesStateSymbol(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_tree.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)

	// Tree 面板应使用 StateSymbol 或 StateBadge (Story 34.3 upgraded to emoji badges)
	if !strings.Contains(content, "StateSymbol") && !strings.Contains(content, "StateBadge") {
		t.Error("dashboard_tree.go should use ui.StateSymbol() or ui.StateBadge() for process state symbols")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-023: [P1] AC-9 — Tree 面板已结束进程显示 exit code 和存活时间
// ---------------------------------------------------------------------------

func TestHistoryView_TreePaneShowsExitCodeAndElapsed(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_tree.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_tree.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "renderDashboardTreePane")
	if funcBody == "" {
		t.Fatal("renderDashboardTreePane function not found in dashboard_tree.go")
	}

	// 已结束进程应显示 DeadAt（存活时间计算）
	if !strings.Contains(funcBody, "DeadAt") {
		t.Error("renderDashboardTreePane should reference DeadAt for dead process elapsed time")
	}

	// 应显示 StateDead 或状态判断
	if !strings.Contains(funcBody, "StateDead") && !strings.Contains(funcBody, "Dead") {
		t.Error("renderDashboardTreePane should check for Dead state to show exit info")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-024: [P0] AC-10 — Timeline 自动过滤 Dead 进程事件
// ---------------------------------------------------------------------------

func TestHistoryView_TimelineAutoFilterDeadProcess(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_timeline.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_timeline.go: %v", err)
	}
	content := string(data)

	// Timeline 应有 PID 过滤逻辑
	hasFilterFunc := strings.Contains(content, "filteredStepEntries") ||
		strings.Contains(content, "FilterByPID") ||
		strings.Contains(content, "filteredEvents") ||
		strings.Contains(content, "stepFilters")
	if !hasFilterFunc {
		t.Error("dashboard_timeline.go should contain timeline event filtering logic for Dead processes")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-025: [P1] AC-10 — Timeline 过滤时 Status bar 显示 filtered 提示
// ---------------------------------------------------------------------------

func TestHistoryView_StatusBarShowsFilteredHint(t *testing.T) {
	// Status bar should contain "(PID N)" indicator when dead process is selected
	paths := []string{
		filepath.Join(cmdRnixDir(), "dashboard.go"),
		filepath.Join(cmdRnixDir(), "dashboard_timeline.go"),
	}

	found := false
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, "(PID %d)") {
			found = true
			break
		}
	}

	if !found {
		t.Error("dashboard.go or dashboard_timeline.go should contain '(PID N)' status message for Dead process timeline filtering")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-026: [P0] AC-2 — renderHistoryView 使用 ui.StateSymbol
// ---------------------------------------------------------------------------

func TestHistoryView_RenderUsesStateSymbol(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "StateSymbol") {
		t.Error("dashboard_history.go should use ui.StateSymbol for process state rendering")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-027: [P0] AC-2 — renderHistoryView 方法签名正确
// ---------------------------------------------------------------------------

func TestHistoryView_RenderFuncSignature(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	// renderHistoryView 应是 dashboardModel 的方法
	if !strings.Contains(content, "func (m dashboardModel) renderHistoryView(") {
		t.Error("renderHistoryView should be a method on dashboardModel")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-028: [P0] AC-1,3,8 — historyKey 方法签名正确
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryKeyFuncSignature(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	// historyKey 应是 dashboardModel 的方法，返回 (tea.Model, tea.Cmd)
	if !strings.Contains(content, "func (m dashboardModel) historyKey(") {
		t.Error("historyKey should be a method on dashboardModel")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-029: [P0] AC-1 — enterHistoryView 方法返回正确类型
// ---------------------------------------------------------------------------

func TestHistoryView_EnterHistoryViewSignature(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	// enterHistoryView 应返回 (dashboardModel, tea.Cmd) 或 (tea.Model, tea.Cmd)
	hasCorrectSignature := strings.Contains(content, "func (m dashboardModel) enterHistoryView()") &&
		(strings.Contains(content, "(dashboardModel, tea.Cmd)") || strings.Contains(content, "(tea.Model, tea.Cmd)"))
	if !hasCorrectSignature {
		t.Error("enterHistoryView should be a method on dashboardModel returning model and cmd")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-030: [P0] AC-全部 — dashboard_history.go 使用 package main
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
// 29.5-UNIT-031: [P1] AC-6 — 搜索模式下 Backspace rune-safe 截断
// ---------------------------------------------------------------------------

func TestHistoryView_SearchBackspaceRuneSafe(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	// 搜索模式的 Backspace 应使用 rune-safe 截断
	// 可以是 utf8.DecodeLastRuneInString、[]rune 切片、或自定义 truncateRuneSafe
	hasRuneSafe := strings.Contains(content, "DecodeLastRune") ||
		strings.Contains(content, "[]rune") ||
		strings.Contains(content, "truncateRuneSafe") ||
		strings.Contains(content, "utf8.")
	if !hasRuneSafe {
		t.Error("dashboard_history.go should use rune-safe string truncation for Backspace in search mode")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-032: [P1] AC-2 — renderHistoryView 包含 Title bar
// ---------------------------------------------------------------------------

func TestHistoryView_RenderContainsTitleBar(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "renderHistoryView")
	if funcBody == "" {
		t.Fatal("renderHistoryView function not found")
	}

	// 应有标题区域
	hasTitle := strings.Contains(funcBody, "History") ||
		strings.Contains(funcBody, "历史") ||
		strings.Contains(funcBody, "Process History")
	if !hasTitle {
		t.Error("renderHistoryView should contain a title bar with 'History' or similar text")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-033: [P0] AC-2 — historyProcsMsg 包含 procs 字段
// ---------------------------------------------------------------------------

func TestHistoryView_HistoryProcsMsgHasProcsField(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse dashboard_types.go: %v", err)
	}

	var structType *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.Name == "historyProcsMsg" {
			if st, ok := ts.Type.(*ast.StructType); ok {
				structType = st
			}
		}
		return true
	})

	if structType == nil {
		t.Fatal("historyProcsMsg struct not found in dashboard_types.go")
	}

	fieldNames := make(map[string]bool)
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fieldNames[name.Name] = true
		}
	}

	// 应有 procs 字段（[]vfs.ProcInfo 类型）
	if !fieldNames["procs"] {
		t.Error("historyProcsMsg should have 'procs' field")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-034: [P1] AC-7 — 三种排序模式完整定义
// ---------------------------------------------------------------------------

func TestHistoryView_ThreeSortModesImplemented(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	// 排序应包含 CreatedAt（时间排序）
	if !strings.Contains(content, "CreatedAt") {
		t.Error("dashboard_history.go should sort by CreatedAt for time-based sorting")
	}

	// 排序应包含 Agent（名称排序）或 Intent
	if !strings.Contains(content, "Agent") && !strings.Contains(content, "Intent") {
		t.Error("dashboard_history.go should sort by Agent or Intent for name-based sorting")
	}

	// 排序应包含 PID（PID 排序）
	if !strings.Contains(content, "PID") {
		t.Error("dashboard_history.go should sort by PID")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-035: [P0] AC-4 — Enter 聚焦后调用 handlePIDChange
// ---------------------------------------------------------------------------

func TestHistoryView_EnterFocusCallsHandlePIDChange(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_history.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "historyKey")
	if funcBody == "" {
		t.Fatal("historyKey function not found in dashboard_history.go")
	}

	// Enter 聚焦后应调用 handlePIDChange 加载关联数据
	if !strings.Contains(funcBody, "handlePIDChange") {
		t.Error("historyKey should call handlePIDChange after Enter focus to reload associated data")
	}
}

// ---------------------------------------------------------------------------
// 29.5-UNIT-036: [P0] AC-全部 — dashboard_history.go 导入 vfs 包
// ---------------------------------------------------------------------------

func TestHistoryView_ImportsVFS(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_history.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("failed to parse dashboard_history.go imports: %v", err)
	}

	hasVFS := false
	for _, imp := range f.Imports {
		if strings.Contains(imp.Path.Value, "vfs") {
			hasVFS = true
			break
		}
	}

	if !hasVFS {
		// vfs 可能通过 historyProcs 字段间接使用，但如果直接操作 ProcInfo 应有导入
		// 允许不导入 vfs（如果类型在 dashboard_types.go 中定义）
		// 但至少应导入 tea 或 ui
		hasTea := false
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "bubbletea") {
				hasTea = true
				break
			}
		}
		if !hasTea {
			t.Error("dashboard_history.go should import bubbletea (tea) package")
		}
	}
}
