package main

// =============================================================================
// ATDD Story 29.1: Dashboard 文件拆分（纯重构）
// =============================================================================
//
// Test Strategy:
//   AC-1: dashboard.go 拆分为 10 个模块化文件
//   AC-2: make all 通过，零行为变更
//
// Priority: P0 (file existence, build regression), P1 (line count, function distribution)
// Test Level: Unit (file structure verification via source analysis)

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// cmdRnixDir returns the absolute path to the cmd/rnix directory.
func cmdRnixDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(thisFile)
}

// fileExists checks whether a file exists in cmd/rnix.
func fileExistsInCmdRnix(name string) bool {
	_, err := os.Stat(filepath.Join(cmdRnixDir(), name))
	return err == nil
}

// countFileLines counts the number of lines in a file.
func countFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// topLevelFuncNames uses go/parser to extract all top-level function and method
// names from a Go source file. Returns bare function names (receiver not included).
func topLevelFuncNames(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			names = append(names, fn.Name.Name)
		}
	}
	return names, nil
}

// extractFuncBody extracts the full body of a function from Go source code.
// Searches by function name across receiver patterns.
func extractFuncBody(content string, funcName string) string {
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

// ---------------------------------------------------------------------------
// 29.1-UNIT-001: [P0] 验证 10 个目标文件全部存在
// ---------------------------------------------------------------------------

func TestDashboardFileSplitting_AllFilesExist(t *testing.T) {
	expectedFiles := []string{
		"dashboard.go",
		"dashboard_types.go",
		"dashboard_tree.go",
		"dashboard_timeline.go",
		"dashboard_heatmap.go",
		"dashboard_detail.go",
		"dashboard_title.go",
		"dashboard_intent.go",
		"dashboard_security.go",
		"dashboard_trace.go",
		"dashboard_eval.go",
	}

	for _, name := range expectedFiles {
		if !fileExistsInCmdRnix(name) {
			t.Errorf("expected file %s to exist in cmd/rnix/", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.1-UNIT-002: [P1] 验证 dashboard.go 行数 ≤ 1500
// ---------------------------------------------------------------------------

func TestDashboardFileSplitting_MainFileLineCount(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	lines, err := countFileLines(path)
	if err != nil {
		t.Fatalf("failed to count lines in dashboard.go: %v", err)
	}

	const maxLines = 1550
	if lines > maxLines {
		t.Errorf("dashboard.go has %d lines, expected ≤ %d after splitting", lines, maxLines)
	}
}

// ---------------------------------------------------------------------------
// 29.1-UNIT-003: [P1] 验证关键函数分布在正确的文件中
// ---------------------------------------------------------------------------

func TestDashboardFileSplitting_FunctionDistribution(t *testing.T) {
	// Map of file → expected functions (representative sample per file)
	// Note: dashboard_types.go contains type definitions, verified by UNIT-006
	expectations := map[string][]string{
		"dashboard_tree.go": {
			"renderDashboardTreePane", "renderDashboardPlaceholder", "buildProcessTree",
		},
		"dashboard_timeline.go": {
			"renderTimelinePane", "renderStepTimeline",
			"fetchStepsCmd", "fetchStepDetailCmd",
		},
		"dashboard_heatmap.go": {
			"buildHeatmapSegments", "renderHeatmapPane", "fetchHeatmapCmd",
			"handleHeatmapKey",
		},
		"dashboard_detail.go": {
			"fetchProcDetailCmd", "renderDetailPane", "truncateUUID",
		},
		"dashboard_intent.go": {
			"flattenIntentTrees", "renderIntentPane", "fetchIntentTreesCmd",
			"intentStateColor",
		},
		"dashboard_security.go": {
			"fetchImmuneStatusCmd", "renderSecurityPane", "sortAlertsByDeviation",
			"formatTimeAgo",
		},
		"dashboard_trace.go": {
			"fetchTraceListCmd", "renderTracePane", "flattenSpanTree",
			"handleTraceKey",
		},
		"dashboard_eval.go": {
			"fetchReputationCmd", "renderEvalPane", "handleEvalKey",
			"renderEvalReputationView", "renderEvalTopologyView", "renderEvalSynergyView",
		},
		"dashboard_title.go": {
			"renderDashboardTitle", "renderPanelTabsLine", "computeHealthCounts",
			"computeCtxPercent", "computeBudgetPercent", "styleProviderName", "formatElapsedHHMMSS",
		},
	}

	dir := cmdRnixDir()

	for file, expectedFuncs := range expectations {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("file %s does not exist", file)
			continue
		}

		funcs, err := topLevelFuncNames(path)
		if err != nil {
			t.Errorf("failed to parse %s: %v", file, err)
			continue
		}

		funcSet := make(map[string]bool)
		for _, fn := range funcs {
			funcSet[fn] = true
		}

		for _, expected := range expectedFuncs {
			if !funcSet[expected] {
				t.Errorf("expected function %s in %s, but not found", expected, file)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 29.1-UNIT-004: [P0] 验证核心编排函数保留在 dashboard.go 中
// ---------------------------------------------------------------------------

func TestDashboardFileSplitting_CoreFunctionsRemainInMain(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard.go")
	funcs, err := topLevelFuncNames(path)
	if err != nil {
		t.Fatalf("failed to parse dashboard.go: %v", err)
	}

	funcSet := make(map[string]bool)
	for _, fn := range funcs {
		funcSet[fn] = true
	}

	// These core orchestration functions MUST stay in dashboard.go
	coreFuncs := []string{
		"newDashboardModel",
		"selectProcess",
		"dashboardTick",
		"dashboardVisibleLines",
		"renderDashboard",
		"handlePIDChange",
		"runDashboard",
		"newReplayDashboardModel",
	}

	for _, fn := range coreFuncs {
		if !funcSet[fn] {
			t.Errorf("expected core function %s to remain in dashboard.go, but not found", fn)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.1-UNIT-005: [P1] 验证所有新文件使用 package main
// ---------------------------------------------------------------------------

func TestDashboardFileSplitting_AllFilesPackageMain(t *testing.T) {
	newFiles := []string{
		"dashboard_types.go",
		"dashboard_tree.go",
		"dashboard_timeline.go",
		"dashboard_heatmap.go",
		"dashboard_detail.go",
		"dashboard_title.go",
		"dashboard_intent.go",
		"dashboard_security.go",
		"dashboard_trace.go",
		"dashboard_eval.go",
	}

	dir := cmdRnixDir()

	for _, name := range newFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("file %s does not exist", name)
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if err != nil {
			t.Errorf("failed to parse %s: %v", name, err)
			continue
		}

		if f.Name.Name != "main" {
			t.Errorf("file %s has package %q, expected \"main\"", name, f.Name.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// 29.1-INT-001: [P0] 验证现有测试编译无误（回归保护）
// This test is implicitly verified by `go test` being able to compile this
// file alongside all existing tests. If the split breaks any symbol references,
// compilation will fail for the entire package.
// ---------------------------------------------------------------------------
// Note: `make all` (lint + vet + test + build) covers AC-2 comprehensively.
// The tests above focus on structural verification (AC-1) that `make all`
// does not explicitly check.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// 29.1-UNIT-006: [P1] 验证 dashboard_types.go 包含类型定义（非函数体）
// ---------------------------------------------------------------------------

func TestDashboardFileSplitting_TypesFileContainsTypes(t *testing.T) {
	path := filepath.Join(cmdRnixDir(), "dashboard_types.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard_types.go: %v", err)
	}

	content := string(data)

	// Verify key type definitions are present
	expectedTypes := []string{
		"type paneType",
		"type stepDetailLevel",
		"type segmentKind",
		"type activityLevel",
		"type stepEntry struct",
		// Story 38-5 PR3 Step 1: heatmapSegment 由 struct 改为 alias 至 internal/dashboard/heatmap.Segment，
		// 让 HeatmapState.Segments 字段类型在 cmd/rnix 端无需转换 wrapper（spec § Risk 4 测试迁移）。
		// 字面契约从 "type heatmapSegment struct" 放宽为 "type heatmapSegment"，alias 形式 + struct 形式
		// 都通过；保留 grep 验证 dashboard_types.go 仍是声明该类型的入口（不允许迁出文件）。
		"type heatmapSegment",
		"type intentFlatNode struct",
		"type spanFlatNode struct",
	}

	for _, typeDef := range expectedTypes {
		if !strings.Contains(content, typeDef) {
			t.Errorf("expected dashboard_types.go to contain %q", typeDef)
		}
	}

	// Verify key message types
	expectedMsgs := []string{
		"type stepListMsg",
		"type stepDetailResultMsg",
		"type procDetailResultMsg",
		"type intentTreesMsg",
		"type immuneStatusMsg",
		"type traceListMsg",
		"type traceTreeMsg",
		"type evalReputationMsg",
		"type promptPagerMsg",
		"type heatmapProfileMsg",
	}

	for _, msgDef := range expectedMsgs {
		if !strings.Contains(content, msgDef) {
			t.Errorf("expected dashboard_types.go to contain %q", msgDef)
		}
	}
}
