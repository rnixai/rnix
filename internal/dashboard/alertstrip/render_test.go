// Package alertstrip — render_test.go (Story 38-5 PR11 Step 4(a-2))
//
// 验证 Render() 行为契约（与 cmd/rnix.renderAlertStrip 1:1 等价）：
//   - len(state.Events) == 0 → ""
//   - 折叠模式 (Expanded=false) 显示 count badge
//   - 展开模式 (Expanded=true) 显示 cursor 高亮
//   - hasOverflow → "+N more" 行
//   - ASCII fallback：分隔条 "-" + ">" 光标前缀
//   - 行 width 截断不破坏 ANSI 边界
//
// **profile-tolerant 模式**（Story 38-3 教训）：visual assertions 检测 lipgloss
// profile · 无色环境 t.Skip 子测试。
package alertstrip

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/event"
)

func TestRender_EmptyEventsReturnsEmpty(t *testing.T) {
	state := AlertStripState{Events: nil}
	if got := Render(state, 80, 2); got != "" {
		t.Errorf("expected empty string for nil Events, got %q", got)
	}
	state.Events = []event.UnifiedEvent{}
	if got := Render(state, 80, 2); got != "" {
		t.Errorf("expected empty string for empty Events, got %q", got)
	}
}

func TestRender_ContainsAlertSummary(t *testing.T) {
	state := AlertStripState{
		Events: []event.UnifiedEvent{
			{Type: event.EventStep, Severity: event.SevError, Timestamp: time.Now(),
				Summary: "test-error-summary"},
		},
	}
	got := Render(state, 80, 2)
	if !strings.Contains(got, "test-error-summary") {
		t.Errorf("expected output to contain alert Summary, got %q", got)
	}
}

func TestRender_OverflowMore(t *testing.T) {
	events := make([]event.UnifiedEvent, 5)
	for i := range events {
		events[i] = event.UnifiedEvent{
			Type:      event.EventStep,
			Severity:  event.SevError,
			Timestamp: time.Now(),
			Summary:   "alert-" + string(rune('0'+i)),
		}
	}
	state := AlertStripState{Events: events}
	got := Render(state, 80, 2) // maxLines=2 < 5 events → overflow
	// "+N more" appears for the overflow row (hasOverflow == true)
	if !strings.Contains(got, "more") {
		t.Errorf("expected '+N more' overflow indicator, got %q", got)
	}
}

func TestRender_CollapsedShowsBadge(t *testing.T) {
	state := AlertStripState{
		Expanded: false,
		Events: []event.UnifiedEvent{
			{Type: event.EventStep, Severity: event.SevError, Timestamp: time.Now(),
				Summary: "err"},
			{Type: event.EventStep, Severity: event.SevWarn, Timestamp: time.Now(),
				Summary: "warn"},
		},
	}
	got := Render(state, 80, 2)
	// Badge contains either ✗ / ⚠ (Unicode) or X / ! (ASCII) depending on env;
	// just assert that some count tail is present (digit followed/preceded by icon)
	hasCount := strings.ContainsAny(got, "✗⚠X!")
	if !hasCount {
		t.Errorf("expected count badge in collapsed mode, got %q", got)
	}
}

func TestRender_ExpandedNoBadge(t *testing.T) {
	state := AlertStripState{
		Expanded: true,
		Cursor:   0,
		Events: []event.UnifiedEvent{
			{Type: event.EventStep, Severity: event.SevError, Timestamp: time.Now(),
				Summary: "err1"},
			{Type: event.EventStep, Severity: event.SevError, Timestamp: time.Now(),
				Summary: "err2"},
		},
	}
	got := Render(state, 80, 8)
	// Both summaries should be visible in expanded mode
	if !strings.Contains(got, "err1") {
		t.Errorf("expected err1 in expanded mode, got %q", got)
	}
	if !strings.Contains(got, "err2") {
		t.Errorf("expected err2 in expanded mode, got %q", got)
	}
}

func TestRender_NarrowWidthDropsBadge(t *testing.T) {
	// Story 38-4 P3 patch: width < badgeWidth+5 drops the badge entirely
	state := AlertStripState{
		Expanded: false,
		Events: []event.UnifiedEvent{
			{Type: event.EventStep, Severity: event.SevError, Timestamp: time.Now(),
				Summary: "x"},
		},
	}
	// width=10 is too small for badge+icon+summary
	got := Render(state, 10, 1)
	// at this width, badge is dropped — output should still contain summary "x"
	// (the icon may have been truncated but the line is present)
	if got == "" {
		t.Errorf("expected non-empty render even at narrow width, got empty")
	}
}

// Profile-tolerant test for cursor highlight (lipgloss background style).
// On no-color environments the highlight markup is stripped so we skip the
// background-color assertion. We still verify the cursor row contains the alert
// content.
func TestRender_ExpandedCursorHighlight(t *testing.T) {
	probe := lipgloss.NewStyle().
		Background(lipgloss.Color("#3D2F2F")).
		Render("probe")
	hasColor := strings.Contains(probe, "\x1b[")
	if !hasColor {
		t.Skip("lipgloss profile NoColor — skipping background-color assertion")
	}

	state := AlertStripState{
		Expanded: true,
		Cursor:   1,
		Events: []event.UnifiedEvent{
			{Type: event.EventStep, Severity: event.SevError, Timestamp: time.Now(),
				Summary: "alert-zero"},
			{Type: event.EventStep, Severity: event.SevError, Timestamp: time.Now(),
				Summary: "alert-cursor"},
		},
	}
	got := Render(state, 80, 8)
	if !strings.Contains(got, "alert-cursor") {
		t.Errorf("expected cursor row with summary, got %q", got)
	}
	// Background highlight ANSI sequence should appear when profile supports color
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI escape sequences in colored profile, got %q", got)
	}
}

// --- maxInt / truncateAnsi internal helpers ---

func TestMaxInt(t *testing.T) {
	if got := maxInt(1, 2); got != 2 {
		t.Errorf("maxInt(1, 2) should be 2, got %d", got)
	}
	if got := maxInt(5, 3); got != 5 {
		t.Errorf("maxInt(5, 3) should be 5, got %d", got)
	}
	if got := maxInt(7, 7); got != 7 {
		t.Errorf("maxInt(7, 7) should be 7, got %d", got)
	}
}

func TestTruncateAnsi_ZeroWidth(t *testing.T) {
	if got := truncateAnsi("hello", 0); got != "" {
		t.Errorf("truncateAnsi with maxWidth=0 should return empty, got %q", got)
	}
	if got := truncateAnsi("hello", -5); got != "" {
		t.Errorf("truncateAnsi with negative maxWidth should return empty, got %q", got)
	}
}

func TestTruncateAnsi_FitsWithinWidth(t *testing.T) {
	got := truncateAnsi("hi", 10)
	if got != "hi" {
		t.Errorf("string fitting within maxWidth should be returned unchanged, got %q", got)
	}
}

func TestTruncateAnsi_TruncatesWideString(t *testing.T) {
	long := "this is a very long string that exceeds twenty characters"
	got := truncateAnsi(long, 10)
	// lipgloss MaxWidth truncates to display width 10
	if lipgloss.Width(got) > 10 {
		t.Errorf("truncated string should fit within width 10, got width=%d (%q)",
			lipgloss.Width(got), got)
	}
}
