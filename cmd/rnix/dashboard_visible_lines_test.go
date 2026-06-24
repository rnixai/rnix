package main

import "testing"

// Regression coverage for the Tree-pane "last row clipped" bug.
//
// dashboardVisibleLines (scroll math) and renderDashboard (layout math) both
// derive the tree's visible capacity from the title-bar height. They used to
// drift: dashboardVisibleLines hardcoded a 2-line title bar, but the bar is
// actually 1/2/3 lines depending on width and whether the Mode Strip shows.
// The Tree pane's ActiveModesFn always reports sort+dir, so at width ≥60 the
// bar is 3 lines — over-counting visible capacity by one and pushing the last
// row out of view (clipped by renderFixedPanel). These tests pin the height to
// the actual title-bar line count so the two formulas cannot drift again.
//
// Formula (viewDefault, no alert): height - titleLines - 4 - detailOffset(3),
// where 4 = statusBar(1) + panelBorder(2) + headerLine(1).

func TestDashboardVisibleLines_ModeStripThreeLineTitle(t *testing.T) {
	// Tree pane active at width ≥60 → Mode Strip shown → 3-line title.
	// This is the exact scenario the bug occurred in.
	m := newDashboardModel(nil)
	m.width = 100
	m.height = 40
	m.activePane = paneTree

	if got := m.titleBarHeight(); got != 3 {
		t.Fatalf("precondition: want 3-line title (Mode Strip), got %d", got)
	}
	// 40 - 3(title) - 4 - 3(detail) = 30. The pre-fix hardcoded-2 formula
	// returned 40 - 6 - 3 = 31, the off-by-one that clipped the last row.
	if got, want := m.dashboardVisibleLines(), 30; got != want {
		t.Errorf("dashboardVisibleLines() = %d, want %d (off-by-one if title height ignored)", got, want)
	}
}

func TestDashboardVisibleLines_TwoLineTitleNoRegression(t *testing.T) {
	// No dispatcher → Mode Strip suppressed → 2-line title (title + tabs).
	// Here the old and new formulas agree; this guards against regression.
	m := newDashboardModel(nil)
	m.width = 100
	m.height = 40
	m.activePane = paneTree
	m.dispatcher = nil

	if got := m.titleBarHeight(); got != 2 {
		t.Fatalf("precondition: want 2-line title (no Mode Strip), got %d", got)
	}
	if got, want := m.dashboardVisibleLines(), 31; got != want { // 40 - 2 - 4 - 3
		t.Errorf("dashboardVisibleLines() = %d, want %d", got, want)
	}
}

func TestDashboardVisibleLines_NarrowSingleLineTitle(t *testing.T) {
	// width <60 → only the title line renders → 1-line title.
	m := newDashboardModel(nil)
	m.width = 50
	m.height = 40

	if got := m.titleBarHeight(); got != 1 {
		t.Fatalf("precondition: want 1-line title (narrow), got %d", got)
	}
	if got, want := m.dashboardVisibleLines(), 32; got != want { // 40 - 1 - 4 - 3
		t.Errorf("dashboardVisibleLines() = %d, want %d", got, want)
	}
}

func TestDashboardVisibleLines_ExpandedTreeStatsOffset(t *testing.T) {
	// Expanded Tree mode: detailOffset=0, statsOffset=2; title still 3 lines.
	m := newDashboardModel(nil)
	m.width = 100
	m.height = 40
	m.activePane = paneTree
	m.viewMode = viewExpanded
	m.expandedPane = paneTree

	if got := m.titleBarHeight(); got != 3 {
		t.Fatalf("precondition: want 3-line title, got %d", got)
	}
	if got, want := m.dashboardVisibleLines(), 31; got != want { // 40 - 3 - 4 - 0 - 2
		t.Errorf("dashboardVisibleLines() = %d, want %d", got, want)
	}
}

func TestDashboardVisibleLines_ClampsToOne(t *testing.T) {
	// Tiny height must not yield a non-positive visible-line count.
	m := newDashboardModel(nil)
	m.width = 100
	m.height = 5
	m.activePane = paneTree

	if got := m.dashboardVisibleLines(); got != 1 {
		t.Errorf("dashboardVisibleLines() = %d, want 1 (clamped lower bound)", got)
	}
}
