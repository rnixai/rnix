package main

// =============================================================================
// ATDD Story 29.3: 默认视图 Focus Card (TDD RED PHASE)
// =============================================================================
//
// Test Strategy:
//   AC-1: Focus Card 2×3 网格渲染（Tokens/Context/Status + Intent/Trace/Alerts）
//   AC-2: Running 进程实时数据（elapsed/tokens/steps 随 tick 刷新）
//   AC-3: Dead 进程快照（✓ Done / ✕ Failed 标题，Historical snapshot，Result 卡片）
//   AC-4: 默认视图布局变更（左 Tree 40% + 右上 Timeline h/2 + 右下 Focus Card h/2）
//   AC-5: lipgloss 卡片渲染（边框、标题居左、内容区 cardH-3 行、宽度=rightWidth/3）
//
// Priority: P0 (type existence, file existence, layout, grid rendering)
//           P1 (Running/Dead data rendering, tick refresh)
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
// 29.3-UNIT-001: [P0] AC-1,5 — dashboard_focus.go 文件存在且包含预期函数
// ---------------------------------------------------------------------------

func TestFocusCard_FileExistsWithExpectedFunctions(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected dashboard_focus.go to exist in cmd/rnix/")
	}

	funcs, err := topLevelFuncNames(path)
	if err != nil {
		t.Fatalf("failed to parse dashboard_focus.go: %v", err)
	}

	funcSet := make(map[string]bool)
	for _, fn := range funcs {
		funcSet[fn] = true
	}

	expectedFuncs := []string{
		"aggregateFocusCard",
		"renderFocusCard",
		"renderTokensCard",
		"renderContextCard",
		"renderStatusCard",
		"renderIntentCard",
		"renderTraceCard",
	}
	for _, fn := range expectedFuncs {
		if !funcSet[fn] {
			t.Errorf("expected function %s in dashboard_focus.go, not found", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-002: [P0] AC-1 — focusCardState 和 intentMiniTask 类型存在
// ---------------------------------------------------------------------------

func TestFocusCard_TypesExistInDashboardTypes(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_types.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "type focusCardState struct") {
		t.Error("expected dashboard_types.go to contain 'type focusCardState struct'")
	}
	if !strings.Contains(content, "type intentMiniTask struct") {
		t.Error("expected dashboard_types.go to contain 'type intentMiniTask struct'")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-003: [P0] AC-1 — dashboardModel 包含 focusCardData 字段
// ---------------------------------------------------------------------------

func TestFocusCard_DashboardModelHasFocusCardField(t *testing.T) {
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

	if !fieldNames["focusCardData"] {
		t.Error("expected dashboardModel to have 'focusCardData' field (type *focusCardState)")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-004: [P0] AC-1 — focusCardState 结构体字段完整性
// ---------------------------------------------------------------------------

func TestFocusCard_FocusCardStateFields(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse dashboard_types.go: %v", err)
	}

	var structType *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.Name == "focusCardState" {
			if st, ok := ts.Type.(*ast.StructType); ok {
				structType = st
			}
		}
		return true
	})

	if structType == nil {
		t.Fatal("focusCardState struct not found in dashboard_types.go")
	}

	fieldNames := make(map[string]bool)
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fieldNames[name.Name] = true
		}
	}

	// 必须包含核心字段
	requiredFields := []string{"pid", "isHistory"}
	for _, fn := range requiredFields {
		if !fieldNames[fn] {
			t.Errorf("focusCardState should have '%s' field", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-005: [P0] AC-1 — intentMiniTask 结构体包含 name 和 state 字段
// ---------------------------------------------------------------------------

func TestFocusCard_IntentMiniTaskFields(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse dashboard_types.go: %v", err)
	}

	var structType *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.Name == "intentMiniTask" {
			if st, ok := ts.Type.(*ast.StructType); ok {
				structType = st
			}
		}
		return true
	})

	if structType == nil {
		t.Fatal("intentMiniTask struct not found in dashboard_types.go")
	}

	fieldNames := make(map[string]bool)
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fieldNames[name.Name] = true
		}
	}

	if !fieldNames["name"] {
		t.Error("intentMiniTask should have 'name' field")
	}
	if !fieldNames["state"] {
		t.Error("intentMiniTask should have 'state' field")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-006: [P0] AC-4 — renderDefaultLayout 不再包含 activePane switch
// ---------------------------------------------------------------------------

func TestFocusCard_DefaultLayoutNoActivePaneSwitch(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}

	// 提取 renderDefaultLayout 函数体
	content := string(data)
	funcBody := extractFuncBody(content, "renderDefaultLayout")
	if funcBody == "" {
		t.Fatal("renderDefaultLayout function not found in dashboard.go")
	}

	// 验证不再包含 activePane switch（已被 Focus Card 替代）
	if strings.Contains(funcBody, "switch m.activePane") {
		t.Error("renderDefaultLayout should no longer contain 'switch m.activePane' — it should render Focus Card instead")
	}

	// 验证调用了 renderFocusCard
	if !strings.Contains(funcBody, "renderFocusCard") {
		t.Error("renderDefaultLayout should call renderFocusCard for the bottom-right area")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-007: [P0] AC-4 — viewExpanded 布局不受影响
// ---------------------------------------------------------------------------

func TestFocusCard_ExpandedLayoutUnchanged(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}

	content := string(data)
	funcBody := extractFuncBody(content, "renderExpandedLayout")
	if funcBody == "" {
		t.Fatal("renderExpandedLayout function not found in dashboard.go")
	}

	// 展开视图不应包含 renderFocusCard（Focus Card 只在默认视图）
	if strings.Contains(funcBody, "renderFocusCard") {
		t.Error("renderExpandedLayout should NOT contain renderFocusCard — Focus Card is only for default view")
	}

	// 展开视图应保留 renderHeatmapPane
	if !strings.Contains(funcBody, "renderHeatmapPane") {
		t.Error("renderExpandedLayout should still contain renderHeatmapPane")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-008: [P1] AC-2 — tick 触发 Focus Card 数据聚合
// ---------------------------------------------------------------------------

func TestFocusCard_TickTriggersAggregate(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "aggregateFocusCard") {
		t.Error("dashboard.go should call aggregateFocusCard (expected in tick handler)")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-009: [P0] AC-5 — dashboard_focus.go 包含 lipgloss 边框渲染代码
// ---------------------------------------------------------------------------

func TestFocusCard_UsesLipglossBorders(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_focus.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "lipgloss") {
		t.Error("dashboard_focus.go should import and use lipgloss for card rendering")
	}
	if !strings.Contains(content, "RoundedBorder") && !strings.Contains(content, "Border") {
		t.Error("dashboard_focus.go should use lipgloss borders for card rendering")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-010: [P0] AC-1 — renderFocusCard 接受 width, height 参数
// ---------------------------------------------------------------------------

func TestFocusCard_RenderFuncSignature(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_focus.go: %v", err)
	}
	content := string(data)

	// 验证方法签名：func (m dashboardModel) renderFocusCard(... int, ... int) string
	if !strings.Contains(content, "renderFocusCard(") {
		t.Error("dashboard_focus.go should contain renderFocusCard function")
	}
	if !strings.Contains(content, "func (m dashboardModel) renderFocusCard(") {
		t.Error("renderFocusCard should be a method on dashboardModel")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-011: [P0] AC-1 — 6 个卡片渲染辅助函数均为 dashboardModel 方法
// ---------------------------------------------------------------------------

func TestFocusCard_SixCardRenderMethodsExist(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_focus.go: %v", err)
	}
	content := string(data)

	cardMethods := []string{
		"renderTokensCard",
		"renderContextCard",
		"renderStatusCard",
		"renderIntentCard",
		"renderTraceCard",
	}
	for _, method := range cardMethods {
		if !strings.Contains(content, "func (m dashboardModel) "+method+"(") {
			t.Errorf("dashboard_focus.go should contain method: func (m dashboardModel) %s(", method)
		}
	}

	// 第 6 个卡片是 Alerts 或 AlertsOrResult
	hasAlerts := strings.Contains(content, "func (m dashboardModel) renderAlertsCard(") ||
		strings.Contains(content, "func (m dashboardModel) renderAlertsOrResultCard(")
	if !hasAlerts {
		t.Error("dashboard_focus.go should contain renderAlertsCard or renderAlertsOrResultCard method")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-012: [P0] AC-1 — renderFocusCard 使用 2×3 网格布局
// ---------------------------------------------------------------------------

func TestFocusCard_GridLayoutStructure(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_focus.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "renderFocusCard")
	if funcBody == "" {
		t.Fatal("renderFocusCard function body not found")
	}

	// 验证 2×3 布局结构：cardW = width/3, cardH = height/2
	if !strings.Contains(funcBody, "/ 3") && !strings.Contains(funcBody, "/3") {
		t.Error("renderFocusCard should divide width by 3 for card columns")
	}
	if !strings.Contains(funcBody, "/ 2") && !strings.Contains(funcBody, "/2") {
		t.Error("renderFocusCard should divide height by 2 for card rows")
	}

	// 验证使用 JoinHorizontal 组合列 + JoinVertical 组合行
	if !strings.Contains(funcBody, "JoinHorizontal") {
		t.Error("renderFocusCard should use lipgloss.JoinHorizontal for card columns")
	}
	if !strings.Contains(funcBody, "JoinVertical") {
		t.Error("renderFocusCard should use lipgloss.JoinVertical for card rows")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-013: [P1] AC-3 — dashboard_focus.go 包含 Dead 进程差异渲染逻辑
// ---------------------------------------------------------------------------

func TestFocusCard_DeadProcessDifferentialRendering(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_focus.go: %v", err)
	}
	content := string(data)

	// Dead 进程应显示 "Done" 或 "Failed" 状态
	if !strings.Contains(content, "Done") && !strings.Contains(content, "Completed") {
		t.Error("dashboard_focus.go should contain 'Done' or 'Completed' text for successful dead processes")
	}
	if !strings.Contains(content, "Failed") {
		t.Error("dashboard_focus.go should contain 'Failed' text for failed dead processes")
	}

	// Dead 进程应显示 "Historical" 或 "snapshot" 或 "final" 标记
	if !strings.Contains(content, "Historical") && !strings.Contains(content, "snapshot") && !strings.Contains(content, "final") {
		t.Error("dashboard_focus.go should contain historical snapshot indicator for dead processes")
	}

	// 应检查 isHistory 字段来区分 Running vs Dead
	if !strings.Contains(content, "isHistory") {
		t.Error("dashboard_focus.go should reference isHistory field for dead process detection")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-014: [P1] AC-3 — Dead 进程 Result 卡片替代 Alerts 卡片
// ---------------------------------------------------------------------------

func TestFocusCard_ResultCardReplacesAlertsForDead(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_focus.go: %v", err)
	}
	content := string(data)

	// 应有条件渲染 Result 卡片
	if !strings.Contains(content, "Result") {
		t.Error("dashboard_focus.go should contain 'Result' card rendering for dead processes")
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-015: [P0] AC-2 — aggregateFocusCard 从已有字段聚合数据
// ---------------------------------------------------------------------------

func TestFocusCard_AggregateUsesExistingFields(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_focus.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_focus.go: %v", err)
	}
	content := string(data)

	funcBody := extractFuncBody(content, "aggregateFocusCard")
	if funcBody == "" {
		t.Fatal("aggregateFocusCard function body not found")
	}

	// 验证从已有字段聚合，而非新增 IPC 调用
	expectedFields := []string{
		"heatmapProfile",
		"procDetail",
	}
	for _, field := range expectedFields {
		if !strings.Contains(funcBody, field) {
			t.Errorf("aggregateFocusCard should reference existing field '%s'", field)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.3-UNIT-016: [P0] AC-4 — renderDefaultLayout 布局比例符合 40%/h/2 规范
// ---------------------------------------------------------------------------

func TestFocusCard_DefaultLayoutProportions(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard.go: %v", err)
	}

	content := string(data)
	funcBody := extractFuncBody(content, "renderDefaultLayout")
	if funcBody == "" {
		t.Fatal("renderDefaultLayout function body not found")
	}

	// 验证 tree 宽度使用 40% 计算
	if !strings.Contains(funcBody, "40") {
		t.Error("renderDefaultLayout should use 40% width ratio for tree pane")
	}

	// 验证高度二等分（topRightH 和 bottomRightH）
	if !strings.Contains(funcBody, "/ 2") && !strings.Contains(funcBody, "/2") {
		t.Error("renderDefaultLayout should split height by 2 for timeline and focus card")
	}
}

// ---------------------------------------------------------------------------
// 辅助函数：从源码中提取函数体
// ---------------------------------------------------------------------------

func extractFuncBody(content string, funcName string) string {
	// 查找包含该函数名的 func 声明
	searchPatterns := []string{
		"func (m dashboardModel) " + funcName + "(",
		"func (m *dashboardModel) " + funcName + "(",
		"func " + funcName + "(",
	}

	idx := -1
	for _, pattern := range searchPatterns {
		idx = strings.Index(content, pattern)
		if idx >= 0 {
			break
		}
	}
	if idx == -1 {
		return ""
	}

	funcBody := content[idx:]
	braceCount := 0
	funcEnd := -1
	for i, ch := range funcBody {
		if ch == '{' {
			braceCount++
		} else if ch == '}' {
			braceCount--
			if braceCount == 0 {
				funcEnd = i
				break
			}
		}
	}
	if funcEnd == -1 {
		return ""
	}
	return funcBody[:funcEnd+1]
}
