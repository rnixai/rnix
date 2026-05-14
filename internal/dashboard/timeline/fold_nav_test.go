package timeline

import (
	"testing"
)

// --- BuildVisibleIndices ---

func TestBuildVisibleIndices_NoGroups(t *testing.T) {
	vis := BuildVisibleIndices(5, nil, nil)
	want := []int{0, 1, 2, 3, 4}
	assertIntSlice(t, vis, want)
}

func TestBuildVisibleIndices_SingleGroupCollapsed(t *testing.T) {
	groups := []FoldGroup{{StartIdx: 2, EndIdx: 5, Key: 10}}
	vis := BuildVisibleIndices(7, nil, groups)
	// indices 3, 4 are hidden (inside collapsed group), 2 (start) is visible
	want := []int{0, 1, 2, 5, 6}
	assertIntSlice(t, vis, want)
}

func TestBuildVisibleIndices_SingleGroupExpanded(t *testing.T) {
	groups := []FoldGroup{{StartIdx: 2, EndIdx: 5, Key: 10}}
	expanded := map[int]bool{10: true}
	vis := BuildVisibleIndices(7, expanded, groups)
	want := []int{0, 1, 2, 3, 4, 5, 6}
	assertIntSlice(t, vis, want)
}

func TestBuildVisibleIndices_MultipleGroups_AllCollapsed(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 0, EndIdx: 3, Key: 1},
		{StartIdx: 5, EndIdx: 8, Key: 5},
	}
	vis := BuildVisibleIndices(10, nil, groups)
	// group1: 0 visible, 1-2 hidden; group2: 5 visible, 6-7 hidden
	want := []int{0, 3, 4, 5, 8, 9}
	assertIntSlice(t, vis, want)
}

func TestBuildVisibleIndices_MixedFoldState(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 0, EndIdx: 4, Key: 1},  // collapsed
		{StartIdx: 6, EndIdx: 9, Key: 10}, // expanded
	}
	expanded := map[int]bool{10: true}
	vis := BuildVisibleIndices(10, expanded, groups)
	// group1 (collapsed): 0 visible, 1-3 hidden; group2 (expanded): all visible
	want := []int{0, 4, 5, 6, 7, 8, 9}
	assertIntSlice(t, vis, want)
}

func TestBuildVisibleIndices_Empty(t *testing.T) {
	vis := BuildVisibleIndices(0, nil, nil)
	if len(vis) != 0 {
		t.Errorf("expected empty, got %v", vis)
	}
}

// --- NextVisibleIndex ---

func TestNextVisibleIndex_Empty(t *testing.T) {
	got := NextVisibleIndex(3, nil, 1)
	if got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestNextVisibleIndex_ForwardOneStep(t *testing.T) {
	vis := []int{0, 2, 5, 8}
	got := NextVisibleIndex(2, vis, 1)
	if got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestNextVisibleIndex_BackwardOneStep(t *testing.T) {
	vis := []int{0, 2, 5, 8}
	got := NextVisibleIndex(5, vis, -1)
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestNextVisibleIndex_ForwardAtEnd(t *testing.T) {
	vis := []int{0, 2, 5, 8}
	got := NextVisibleIndex(8, vis, 1)
	if got != 8 {
		t.Errorf("expected 8 (clamped), got %d", got)
	}
}

func TestNextVisibleIndex_BackwardAtStart(t *testing.T) {
	vis := []int{0, 2, 5, 8}
	got := NextVisibleIndex(0, vis, -1)
	if got != 0 {
		t.Errorf("expected 0 (clamped), got %d", got)
	}
}

func TestNextVisibleIndex_PageJump(t *testing.T) {
	vis := []int{0, 2, 5, 8, 10, 15, 20}
	got := NextVisibleIndex(2, vis, 3)
	if got != 10 {
		t.Errorf("expected 10 (vis[4]), got %d", got)
	}
}

func TestNextVisibleIndex_PageJumpClamped(t *testing.T) {
	vis := []int{0, 2, 5, 8}
	got := NextVisibleIndex(2, vis, 10)
	if got != 8 {
		t.Errorf("expected 8 (clamped to last), got %d", got)
	}
}

func TestNextVisibleIndex_SnapForward(t *testing.T) {
	// cursor is at 3, which is inside collapsed group (vis: 0, 2, 5, 8)
	vis := []int{0, 2, 5, 8}
	got := NextVisibleIndex(3, vis, 1)
	// 3 is not in vis; snaps forward to 5 (next visible), no additional delta
	if got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestNextVisibleIndex_SnapBackward(t *testing.T) {
	vis := []int{0, 2, 5, 8}
	got := NextVisibleIndex(3, vis, -1)
	// 3 is not in vis; snaps backward to 2
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

// --- VisiblePosition ---

func TestVisiblePosition_ExactMatch(t *testing.T) {
	vis := []int{0, 3, 7, 10}
	got := VisiblePosition(7, vis)
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestVisiblePosition_BetweenVisible(t *testing.T) {
	vis := []int{0, 3, 7, 10}
	got := VisiblePosition(5, vis)
	// 5 is between vis[1]=3 and vis[2]=7, should return 1 (preceding)
	if got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestVisiblePosition_BeforeFirst(t *testing.T) {
	vis := []int{2, 5, 8}
	got := VisiblePosition(0, vis)
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestVisiblePosition_AfterLast(t *testing.T) {
	vis := []int{0, 3, 7}
	got := VisiblePosition(10, vis)
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestVisiblePosition_Empty(t *testing.T) {
	got := VisiblePosition(5, nil)
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// --- FindGroupBoundary ---

func TestFindGroupBoundary_ForwardFound(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 2, EndIdx: 5, Key: 1},
		{StartIdx: 8, EndIdx: 11, Key: 2},
	}
	got := FindGroupBoundary(3, groups, 1)
	if got != 8 {
		t.Errorf("expected 8, got %d", got)
	}
}

func TestFindGroupBoundary_ForwardFromBeforeAll(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 5, EndIdx: 8, Key: 1},
	}
	got := FindGroupBoundary(0, groups, 1)
	if got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestFindGroupBoundary_ForwardNoneFound(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 2, EndIdx: 5, Key: 1},
	}
	got := FindGroupBoundary(10, groups, 1)
	if got != 10 {
		t.Errorf("expected 10 (unchanged), got %d", got)
	}
}

func TestFindGroupBoundary_BackwardFound(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 2, EndIdx: 5, Key: 1},
		{StartIdx: 8, EndIdx: 11, Key: 2},
	}
	got := FindGroupBoundary(10, groups, -1)
	if got != 8 {
		t.Errorf("expected 8, got %d", got)
	}
}

func TestFindGroupBoundary_BackwardNoneFound(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 5, EndIdx: 8, Key: 1},
	}
	got := FindGroupBoundary(3, groups, -1)
	if got != 3 {
		t.Errorf("expected 3 (unchanged), got %d", got)
	}
}

func TestFindGroupBoundary_NoGroups(t *testing.T) {
	got := FindGroupBoundary(5, nil, 1)
	if got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestFindGroupBoundary_AtGroupStart(t *testing.T) {
	groups := []FoldGroup{
		{StartIdx: 2, EndIdx: 5, Key: 1},
		{StartIdx: 8, EndIdx: 11, Key: 2},
	}
	// At group 1 start, forward should find group 2
	got := FindGroupBoundary(2, groups, 1)
	if got != 8 {
		t.Errorf("expected 8, got %d", got)
	}
}

// --- helpers ---

func assertIntSlice(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %d, want %d", i, got[i], want[i])
		}
	}
}
