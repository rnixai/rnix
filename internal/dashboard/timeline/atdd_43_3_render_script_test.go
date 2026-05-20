// ATDD Story 43.3 - Timeline Renderer for Script Trace Events
//
// Red-phase tests for internal/dashboard/timeline/render.go additions:
//   - RenderStepFilterBar 的 sysTypes 添加 [S]script 项（Row 2）
//   - RenderUnifiedStepHeader 的 allTypes 添加 EventScript → "scr" 标签
//
// Acceptance Criteria covered:
//   - AC#4: Filter bar 含 [S]script 项
//   - AC#6: Header filter indicator 关闭 EventScript 时显示 "-scr"
//
// RED 信号（dev-story 实施前 `go test -tags atdd_red ./internal/dashboard/timeline/...` 应失败）：
//   - RenderStepFilterBar 输出不含 "[S]" 或 "script" label
//   - RenderUnifiedStepHeader 输出 filter indicator 不含 "scr"（当 EventScript 关闭时）
//
// 实施完成后应能编译并通过；最后移除 build tag 让 `make all` 接管。

package timeline

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// RenderStepFilterBar — AC#4 Filter bar 含 [S]script 项
// =============================================================================

// TestRenderStepFilterBar_HasScriptKey — AC#4 / Task 4.1
// Row 2 sysTypes 含 {"S", "script", EventScript}；输出应同时包含 "[S]" 短键
// 和 "script" label（大写 S 避免与 [s]=spawn / [x]=spawn(sys) / [X]=exit 冲突）。
func TestRenderStepFilterBar_HasScriptKey(t *testing.T) {
	got := RenderStepFilterBar(nil, 300)
	if !strings.Contains(got, "[S]") {
		t.Errorf("RenderStepFilterBar missing [S] key:\n%s", got)
	}
	if !strings.Contains(got, "script") {
		t.Errorf("RenderStepFilterBar missing \"script\" label:\n%s", got)
	}
}

// TestRenderStepFilterBar_ScriptOnByDefault — AC#4 filters == nil 时
// EventScript 显示为 ✓ on（与其他 sys event 同模式 · DefaultStepFilters 含
// EventScript: true · 等价默认全开）。
func TestRenderStepFilterBar_ScriptOnByDefault(t *testing.T) {
	got := RenderStepFilterBar(nil, 300)
	// Row 2 应至少包含 1 个 ✓ mark（已有 sysTypes 全部 on）
	// 加上 [S]script ✓ 应该位于 row 2 上。
	rows := strings.Split(got, "\n")
	if len(rows) < 2 {
		t.Fatalf("expected 2 rows, got %d: %q", len(rows), got)
	}
	row2 := rows[1]
	if !strings.Contains(row2, "[S]") {
		t.Errorf("[S] should be on row 2 (Events row), got row2:\n%s", row2)
	}
	if !strings.Contains(row2, "✓") {
		t.Errorf("row 2 should contain ✓ marks (filters all on by default):\n%s", row2)
	}
}

// TestRenderStepFilterBar_ScriptOffMark — AC#6 显式关闭 EventScript filter
// 时应显示 · off mark。
func TestRenderStepFilterBar_ScriptOffMark(t *testing.T) {
	filters := map[string]bool{
		"tool_call":      true,
		"plan":           true,
		"text":           true,
		"complete":       true,
		"spawn":          true,
		"replan":         true,
		"specialize":     true,
		stepEventCompact: true,
		stepEventBudget:  true,
		"sys_spawn":      true,
		stepEventExit:    true,
		stepEventStall:   true,
		stepEventImmune:  true,
		"script":         false, // EventScript 关闭
	}
	got := RenderStepFilterBar(filters, 300)
	if !strings.Contains(got, "[S]") {
		t.Fatalf("missing [S] key:\n%s", got)
	}
	// 至少存在一个 · off mark
	if !strings.Contains(got, "·") {
		t.Errorf("expected · off mark when EventScript=false:\n%s", got)
	}
}

// TestRenderStepFilterBar_ScriptKeyDoesNotCollideWithExisting — AC#4 关键约束：
// 短键 "S" 必须大写（与已有 s=spawn / x=spawn(sys) / X=exit / T=stall 不冲突）。
func TestRenderStepFilterBar_ScriptKeyDoesNotCollideWithExisting(t *testing.T) {
	got := RenderStepFilterBar(nil, 300)
	// Row 1 含 [t/p/a/c/s/r/z]；Row 2 含 [C/b/x/X/T/i/S]
	rows := strings.Split(got, "\n")
	if len(rows) < 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	row1 := rows[0]
	// [S] 必须在 row 2 而不在 row 1；row 1 用的是小写 s
	if strings.Contains(row1, "[S]") {
		t.Errorf("[S] should NOT be on row 1 (Step actions row): %s", row1)
	}
	// row 2 必须含 [S]
	row2 := rows[1]
	if !strings.Contains(row2, "[S]") {
		t.Errorf("[S] should be on row 2 (Events row): %s", row2)
	}
	// 同时 row 1 应保留原 [s] = spawn step-action
	if !strings.Contains(row1, "[s]") {
		t.Errorf("row 1 should still contain [s] (spawn step-action): %s", row1)
	}
}

// =============================================================================
// RenderUnifiedStepHeader — AC#6 filter indicator 含 "scr"
// =============================================================================

// TestRenderUnifiedStepHeader_HasScriptInFilterIndicator — AC#6 / Task 4.2
// allTypes 含 {EventScript, "scr"}。当 EventScript filter 关闭时，header
// 末尾 filter indicator 应显示 "scr" 标签。
func TestRenderUnifiedStepHeader_HasScriptInFilterIndicator(t *testing.T) {
	filters := defaultFiltersAllOn(t)
	filters["script"] = false

	ctx := HeaderContext{
		State: TimelineState{
			StepFilters: filters,
			StepCursor:  0,
		},
		Processes:    []vfs.ProcInfo{{PID: types.PID(101), UUID: "uuid-101"}},
		SelectedPID:  types.PID(101),
		SelectedUUID: "uuid-101",
		TotalEvents:  10, // 模拟 10 个总事件中 2 个被 script filter 过滤
	}
	// totalSteps, filteredCount=8 (10-2 script events), sysCount=2
	got := RenderUnifiedStepHeader(ctx, 300, 8, 8, 2)
	if !strings.Contains(got, "scr") {
		t.Errorf("header filter indicator missing \"scr\" label when script filter is off:\n%s", got)
	}
}

// TestRenderUnifiedStepHeader_NoScriptLabelWhenFilterOn — AC#6 默认全开时不应
// 出现 "-scr" 字样（避免误展示）。
func TestRenderUnifiedStepHeader_NoScriptLabelWhenFilterOn(t *testing.T) {
	filters := defaultFiltersAllOn(t)

	ctx := HeaderContext{
		State: TimelineState{
			StepFilters: filters,
			StepCursor:  0,
		},
		Processes:    []vfs.ProcInfo{{PID: types.PID(101), UUID: "uuid-101"}},
		SelectedPID:  types.PID(101),
		SelectedUUID: "uuid-101",
		TotalEvents:  5,
	}
	// filteredCount == TotalEvents → filter indicator 不渲染
	got := RenderUnifiedStepHeader(ctx, 300, 5, 5, 0)
	if strings.Contains(got, "-scr") {
		t.Errorf("unexpected \"-scr\" in header when all filters on (filteredCount==TotalEvents):\n%s", got)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

// defaultFiltersAllOn 构造一份全开 filter map（含 EventScript 的 "script" key）·
// 字符串字面量与 event.DefaultStepFilters 等价 · 避免循环导入 event 包.
func defaultFiltersAllOn(t *testing.T) map[string]bool {
	t.Helper()
	return map[string]bool{
		"tool_call":      true,
		"plan":           true,
		"text":           true,
		"complete":       true,
		"spawn":          true,
		"replan":         true,
		"specialize":     true,
		stepEventCompact: true,
		stepEventBudget:  true,
		"sys_spawn":      true,
		stepEventExit:    true,
		stepEventStall:   true,
		stepEventImmune:  true,
		"syscall":        true,
		"script":         true,
	}
}
