// Tests for ClampPercent + FormatElapsedHHMMSS + PctColorStyle (Story 38-5 PR11 Step 4(c)).
//
// 行为契约覆盖：
//   1. ClampPercent: 负 → 0 / 0-999 passthrough / >999 → 999
//   2. FormatElapsedHHMMSS: HH:MM:SS 零填充 / 负值 → 00:00:00 / 大值 25h+
//   3. PctColorStyle: < 60 Muted / 60-79 Warning / ≥80 Error+Bold (Story 38.2 AC#1)

package title

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// --- ClampPercent ---

func TestClampPercent_NegativeFloorsToZero(t *testing.T) {
	cases := []int{-1, -100, -999, -1000000}
	for _, v := range cases {
		if got := ClampPercent(v); got != 0 {
			t.Errorf("ClampPercent(%d): want 0, got %d", v, got)
		}
	}
}

func TestClampPercent_InRangePassthrough(t *testing.T) {
	cases := []int{0, 1, 50, 99, 100, 500, 999}
	for _, v := range cases {
		if got := ClampPercent(v); got != v {
			t.Errorf("ClampPercent(%d) passthrough: want %d, got %d", v, v, got)
		}
	}
}

func TestClampPercent_AboveCeilingClampsTo999(t *testing.T) {
	cases := []int{1000, 1001, 9999, 1000000}
	for _, v := range cases {
		if got := ClampPercent(v); got != 999 {
			t.Errorf("ClampPercent(%d): want 999 (ceiling), got %d", v, got)
		}
	}
}

// --- FormatElapsedHHMMSS ---

func TestFormatElapsedHHMMSS_Zero(t *testing.T) {
	if got := FormatElapsedHHMMSS(0); got != "00:00:00" {
		t.Errorf("FormatElapsedHHMMSS(0): want 00:00:00, got %q", got)
	}
}

func TestFormatElapsedHHMMSS_Negative(t *testing.T) {
	if got := FormatElapsedHHMMSS(-time.Second); got != "00:00:00" {
		t.Errorf("FormatElapsedHHMMSS(-1s): want 00:00:00, got %q", got)
	}
	if got := FormatElapsedHHMMSS(-time.Hour); got != "00:00:00" {
		t.Errorf("FormatElapsedHHMMSS(-1h): want 00:00:00, got %q", got)
	}
}

func TestFormatElapsedHHMMSS_Seconds(t *testing.T) {
	if got := FormatElapsedHHMMSS(5 * time.Second); got != "00:00:05" {
		t.Errorf("FormatElapsedHHMMSS(5s): want 00:00:05, got %q", got)
	}
	if got := FormatElapsedHHMMSS(45 * time.Second); got != "00:00:45" {
		t.Errorf("FormatElapsedHHMMSS(45s): want 00:00:45, got %q", got)
	}
}

func TestFormatElapsedHHMMSS_MinutesAndSeconds(t *testing.T) {
	got := FormatElapsedHHMMSS(2*time.Minute + 30*time.Second)
	if got != "00:02:30" {
		t.Errorf("FormatElapsedHHMMSS(2m30s): want 00:02:30, got %q", got)
	}
}

func TestFormatElapsedHHMMSS_Hours(t *testing.T) {
	got := FormatElapsedHHMMSS(1*time.Hour + 23*time.Minute + 45*time.Second)
	if got != "01:23:45" {
		t.Errorf("FormatElapsedHHMMSS(1h23m45s): want 01:23:45, got %q", got)
	}
}

func TestFormatElapsedHHMMSS_LongDuration(t *testing.T) {
	// 25:30:15 - 跨天但不显示天
	got := FormatElapsedHHMMSS(25*time.Hour + 30*time.Minute + 15*time.Second)
	if got != "25:30:15" {
		t.Errorf("FormatElapsedHHMMSS(25h30m15s): want 25:30:15, got %q", got)
	}
}

func TestFormatElapsedHHMMSS_ZeroPadding(t *testing.T) {
	// 9 小时 5 分 1 秒 → "09:05:01" (零填充)
	got := FormatElapsedHHMMSS(9*time.Hour + 5*time.Minute + 1*time.Second)
	if got != "09:05:01" {
		t.Errorf("FormatElapsedHHMMSS zero padding: want 09:05:01, got %q", got)
	}
}

// --- PctColorStyle ---

func TestPctColorStyle_BelowSixty_Muted(t *testing.T) {
	cases := []int{0, 1, 30, 59}
	want := lipgloss.Color(ui.ColorMuted)
	for _, p := range cases {
		st := PctColorStyle(p)
		if st.GetForeground() != want {
			t.Errorf("PctColorStyle(%d): want fg %v (Muted), got %v", p, want, st.GetForeground())
		}
		if st.GetBold() {
			t.Errorf("PctColorStyle(%d) below 60: should not be bold", p)
		}
	}
}

func TestPctColorStyle_SixtyToSeventyNine_Warning(t *testing.T) {
	cases := []int{60, 70, 79}
	want := lipgloss.Color(ui.ColorWarning)
	for _, p := range cases {
		st := PctColorStyle(p)
		if st.GetForeground() != want {
			t.Errorf("PctColorStyle(%d): want fg %v (Warning), got %v", p, want, st.GetForeground())
		}
		if st.GetBold() {
			t.Errorf("PctColorStyle(%d) 60-79: should not be bold", p)
		}
	}
}

func TestPctColorStyle_EightyOrAbove_ErrorBold(t *testing.T) {
	cases := []int{80, 100, 200, 999}
	want := lipgloss.Color(ui.ColorError)
	for _, p := range cases {
		st := PctColorStyle(p)
		if st.GetForeground() != want {
			t.Errorf("PctColorStyle(%d): want fg %v (Error), got %v", p, want, st.GetForeground())
		}
		if !st.GetBold() {
			t.Errorf("PctColorStyle(%d) ≥80: should be bold (Story 38.2 AC#1)", p)
		}
	}
}

func TestPctColorStyle_BoundaryValues(t *testing.T) {
	// 59 → Muted · 60 → Warning · 79 → Warning · 80 → Error+Bold
	if PctColorStyle(59).GetForeground() != lipgloss.Color(ui.ColorMuted) {
		t.Error("59 should be Muted")
	}
	if PctColorStyle(60).GetForeground() != lipgloss.Color(ui.ColorWarning) {
		t.Error("60 should be Warning (boundary)")
	}
	if PctColorStyle(79).GetForeground() != lipgloss.Color(ui.ColorWarning) {
		t.Error("79 should still be Warning")
	}
	if PctColorStyle(80).GetForeground() != lipgloss.Color(ui.ColorError) {
		t.Error("80 should be Error (boundary)")
	}
	if !PctColorStyle(80).GetBold() {
		t.Error("80 should be Bold (Story 38.2 AC#1)")
	}
}
