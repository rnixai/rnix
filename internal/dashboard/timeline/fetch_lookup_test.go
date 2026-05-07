// Package timeline — fetch_lookup_test.go (Story 38-5 PR11 Step 4(c))
package timeline

import (
	"testing"

	"github.com/rnixai/rnix/ipc"
)

func mkEntry(step int, level StepDetailLevel) StepEntry {
	return StepEntry{
		Summary: ipc.StepSummaryWire{Step: step},
		Level:   level,
	}
}

func TestFindNextDetailToFetch_Priority1_ExpandedNoCache(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			mkEntry(1, LevelSummary),
			mkEntry(2, LevelExpanded), // 应被选中（Priority 1）
			mkEntry(3, LevelExpanded), // 也是候选但被前者优先
		},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	got := FindNextDetailToFetch(state, nil, 0, 0)
	if got != 2 {
		t.Errorf("got step %d, want 2 (Priority 1 first expanded)", got)
	}
}

func TestFindNextDetailToFetch_Priority1_SkipsCached(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			mkEntry(1, LevelExpanded),
			mkEntry(2, LevelExpanded),
		},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{
			1: {}, // step 1 已缓存
		},
	}
	got := FindNextDetailToFetch(state, nil, 0, 0)
	if got != 2 {
		t.Errorf("got step %d, want 2 (跳过已缓存的 step 1)", got)
	}
}

func TestFindNextDetailToFetch_Priority2_VisibleCollapsed(t *testing.T) {
	e1 := mkEntry(1, LevelSummary)
	e2 := mkEntry(2, LevelSummary)
	e3 := mkEntry(3, LevelSummary)
	state := TimelineState{
		StepEntries:     []StepEntry{e1, e2, e3},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	filtered := []*StepEntry{&e1, &e2, &e3}
	// 可视窗口 = [1, 3) → e2, e3 中首个未缓存的（e2）
	got := FindNextDetailToFetch(state, filtered, 1, 3)
	if got != 2 {
		t.Errorf("got step %d, want 2 (Priority 2 visStart=1 first uncached)", got)
	}
}

func TestFindNextDetailToFetch_Priority2_RespectsVisRange(t *testing.T) {
	e1 := mkEntry(1, LevelSummary)
	e2 := mkEntry(2, LevelSummary)
	e3 := mkEntry(3, LevelSummary)
	state := TimelineState{
		StepEntries:     []StepEntry{e1, e2, e3},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	filtered := []*StepEntry{&e1, &e2, &e3}
	// visStart=0 visEnd=1 → 仅 e1
	got := FindNextDetailToFetch(state, filtered, 0, 1)
	if got != 1 {
		t.Errorf("got step %d, want 1 (vis range 限制为单个 entry)", got)
	}
}

func TestFindNextDetailToFetch_Priority2_SkipsNilEntry(t *testing.T) {
	e1 := mkEntry(1, LevelSummary)
	state := TimelineState{
		StepEntries:     []StepEntry{e1},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	// filtered[0] 为 nil（system event 解开后 StepEntry 为 nil 的情况）
	filtered := []*StepEntry{nil, &e1}
	got := FindNextDetailToFetch(state, filtered, 0, 2)
	if got != 1 {
		t.Errorf("got step %d, want 1 (跳过 nil entry)", got)
	}
}

func TestFindNextDetailToFetch_Priority2_SkipsExpanded(t *testing.T) {
	// Priority 1 已经处理了 Expanded · Priority 2 不应再次匹配
	e1 := mkEntry(1, LevelExpanded)
	state := TimelineState{
		StepEntries:     []StepEntry{e1},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{1: {}}, // 已缓存
	}
	filtered := []*StepEntry{&e1}
	got := FindNextDetailToFetch(state, filtered, 0, 1)
	if got != -1 {
		t.Errorf("got step %d, want -1 (已缓存 + Priority 2 跳过 Expanded)", got)
	}
}

func TestFindNextDetailToFetch_AllCached_ReturnsMinusOne(t *testing.T) {
	e1 := mkEntry(1, LevelSummary)
	e2 := mkEntry(2, LevelExpanded)
	state := TimelineState{
		StepEntries: []StepEntry{e1, e2},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{
			1: {},
			2: {},
		},
	}
	filtered := []*StepEntry{&e1, &e2}
	got := FindNextDetailToFetch(state, filtered, 0, 2)
	if got != -1 {
		t.Errorf("got step %d, want -1", got)
	}
}

func TestFindNextDetailToFetch_EmptyState_ReturnsMinusOne(t *testing.T) {
	got := FindNextDetailToFetch(TimelineState{}, nil, 0, 0)
	if got != -1 {
		t.Errorf("got step %d, want -1", got)
	}
}

func TestFindNextDetailToFetch_NegativeVisStart_ClampedToZero(t *testing.T) {
	e1 := mkEntry(1, LevelSummary)
	state := TimelineState{
		StepEntries:     []StepEntry{e1},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	filtered := []*StepEntry{&e1}
	got := FindNextDetailToFetch(state, filtered, -5, 1)
	if got != 1 {
		t.Errorf("negative visStart should clamp to 0; got %d, want 1", got)
	}
}

func TestFindNextDetailToFetch_VisEndBeyondLen_Clamped(t *testing.T) {
	e1 := mkEntry(1, LevelSummary)
	state := TimelineState{
		StepEntries:     []StepEntry{e1},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	filtered := []*StepEntry{&e1}
	got := FindNextDetailToFetch(state, filtered, 0, 999)
	if got != 1 {
		t.Errorf("visEnd 越界应 clamp; got %d, want 1", got)
	}
}

func TestFindNextDetailToFetch_PriorityOrder_ExpandedBeforeVisible(t *testing.T) {
	// 同时存在 Priority 1 + Priority 2 候选 · Priority 1 优先
	e1 := mkEntry(1, LevelSummary)  // Priority 2 候选
	e2 := mkEntry(2, LevelExpanded) // Priority 1 候选
	state := TimelineState{
		StepEntries:     []StepEntry{e1, e2},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	filtered := []*StepEntry{&e1, &e2}
	got := FindNextDetailToFetch(state, filtered, 0, 2)
	if got != 2 {
		t.Errorf("Priority order: got %d, want 2 (Expanded before visible collapsed)", got)
	}
}
