package inspector

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestIsBackScrollKey_True(t *testing.T) {
	t.Parallel()
	keys := []string{"k", "up", "pgup", "pageup", "ctrl+u", "ctrl+b", "g"}
	for _, k := range keys {
		if !IsBackScrollKey(k) {
			t.Fatalf("IsBackScrollKey(%q) = false, want true", k)
		}
	}
}

func TestIsBackScrollKey_False(t *testing.T) {
	t.Parallel()
	keys := []string{"j", "down", "pgdn", "pagedown", "ctrl+d", "ctrl+f", "G", "", "x", "enter"}
	for _, k := range keys {
		if IsBackScrollKey(k) {
			t.Fatalf("IsBackScrollKey(%q) = true, want false", k)
		}
	}
}

func TestFindInspectorMatchesByPos_EmptyQuery(t *testing.T) {
	t.Parallel()
	if got := FindInspectorMatchesByPos("hello world", ""); got != nil {
		t.Fatalf("empty query should return nil, got %v", got)
	}
}

func TestFindInspectorMatchesByPos_NoMatch(t *testing.T) {
	t.Parallel()
	got := FindInspectorMatchesByPos("hello world", "xyz")
	if len(got) != 0 {
		t.Fatalf("expected 0 matches, got %d (%v)", len(got), got)
	}
}

func TestFindInspectorMatchesByPos_SingleMatch(t *testing.T) {
	t.Parallel()
	got := FindInspectorMatchesByPos("hello world", "world")
	if len(got) != 1 {
		t.Fatalf("expected 1 match, got %d", len(got))
	}
	if got[0].LineIdx != 0 {
		t.Fatalf("expected LineIdx=0, got %d", got[0].LineIdx)
	}
	if got[0].ByteStart != 6 || got[0].ByteEnd != 11 {
		t.Fatalf("expected ByteStart=6 ByteEnd=11, got %d/%d", got[0].ByteStart, got[0].ByteEnd)
	}
}

func TestFindInspectorMatchesByPos_CaseInsensitive(t *testing.T) {
	t.Parallel()
	got := FindInspectorMatchesByPos("Hello WORLD foo", "hello")
	if len(got) != 1 {
		t.Fatalf("expected 1 match (case-insensitive), got %d", len(got))
	}
	got = FindInspectorMatchesByPos("Hello WORLD foo", "world")
	if len(got) != 1 {
		t.Fatalf("expected 1 match (case-insensitive), got %d", len(got))
	}
}

func TestFindInspectorMatchesByPos_MultilineMultipleMatches(t *testing.T) {
	t.Parallel()
	content := "first foo\nsecond foo\nfoo third"
	got := FindInspectorMatchesByPos(content, "foo")
	if len(got) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(got))
	}
	// LineIdx should be 0, 1, 2
	if got[0].LineIdx != 0 || got[1].LineIdx != 1 || got[2].LineIdx != 2 {
		t.Fatalf("LineIdx mismatch: %v", []int{got[0].LineIdx, got[1].LineIdx, got[2].LineIdx})
	}
	// ByteStart should be ascending
	for i := 1; i < len(got); i++ {
		if got[i].ByteStart <= got[i-1].ByteStart {
			t.Fatalf("matches not in ascending byte order: %v", got)
		}
	}
}

func TestFindInspectorMatchesByPos_QuotesMetaChars(t *testing.T) {
	t.Parallel()
	// query contains regex meta chars; QuoteMeta should escape
	got := FindInspectorMatchesByPos("a.b foo a.b", "a.b")
	if len(got) != 2 {
		t.Fatalf("expected 2 literal 'a.b' matches, got %d", len(got))
	}
	// Confirm "a-b" or other literals are NOT matched (i.e. '.' is not regex any-char)
	got = FindInspectorMatchesByPos("axb", "a.b")
	if len(got) != 0 {
		t.Fatalf("regex meta should be escaped — 'axb' should NOT match query 'a.b', got %d", len(got))
	}
}

func TestFindInspectorMatchesByPos_SameLineMultiple(t *testing.T) {
	t.Parallel()
	got := FindInspectorMatchesByPos("foo bar foo bar foo", "foo")
	if len(got) != 3 {
		t.Fatalf("expected 3 matches on single line, got %d", len(got))
	}
	for _, m := range got {
		if m.LineIdx != 0 {
			t.Fatalf("all matches should be on line 0, got %d", m.LineIdx)
		}
	}
}

func TestApplyWordLevelHighlight_NoPositions(t *testing.T) {
	t.Parallel()
	curStyle := lipgloss.NewStyle().Reverse(true)
	other := lipgloss.NewStyle()
	got := ApplyWordLevelHighlight("hello", nil, nil, 0, curStyle, other)
	if got != "hello" {
		t.Fatalf("no positions should return content unchanged, got %q", got)
	}
}

func TestApplyWordLevelHighlight_AllUseOtherStyle(t *testing.T) {
	t.Parallel()
	curStyle := lipgloss.NewStyle().Reverse(true)
	other := lipgloss.NewStyle().Bold(true)
	positions := []SearchMatchPos{
		{LineIdx: 0, ByteStart: 0, ByteEnd: 3},
		{LineIdx: 1, ByteStart: 6, ByteEnd: 9},
	}
	// matchIdx out of range → currentLine = -1 → all positions get otherStyle
	got := ApplyWordLevelHighlight("foo\nbarbaz", positions, []int{}, -1, curStyle, other)
	stripped := stripAnsi(got)
	if !strings.Contains(stripped, "foo") {
		t.Fatalf("content should still contain 'foo', got %q", stripped)
	}
}

func TestApplyWordLevelHighlight_CurrentLineDistinct(t *testing.T) {
	t.Parallel()
	curStyle := lipgloss.NewStyle().Reverse(true)
	other := lipgloss.NewStyle()
	positions := []SearchMatchPos{
		{LineIdx: 0, ByteStart: 0, ByteEnd: 3},
		{LineIdx: 1, ByteStart: 4, ByteEnd: 7},
	}
	// searchMatches[matchIdx=0] = 1 → current line is line 1 → second position gets curStyle
	got := ApplyWordLevelHighlight("foo\nbar", positions, []int{1}, 0, curStyle, other)
	// We can't easily distinguish styles in plaintext, but we can verify content
	// preservation and that the function doesn't panic.
	stripped := stripAnsi(got)
	if !strings.Contains(stripped, "foo") || !strings.Contains(stripped, "bar") {
		t.Fatalf("highlights should not lose content, got %q", stripped)
	}
}

func TestApplyWordLevelHighlight_OutOfRangePositions(t *testing.T) {
	t.Parallel()
	curStyle := lipgloss.NewStyle()
	other := lipgloss.NewStyle()
	positions := []SearchMatchPos{
		{LineIdx: 0, ByteStart: -1, ByteEnd: 5},  // negative start → skip
		{LineIdx: 0, ByteStart: 0, ByteEnd: 100}, // end > len → skip
		{LineIdx: 0, ByteStart: 5, ByteEnd: 5},   // start >= end → skip
		{LineIdx: 0, ByteStart: 0, ByteEnd: 3},   // valid
	}
	got := ApplyWordLevelHighlight("hello", positions, []int{0}, 0, curStyle, other)
	if !strings.Contains(stripAnsi(got), "hello") {
		t.Fatalf("content should be preserved despite invalid positions, got %q", got)
	}
}

func TestApplyWordLevelHighlight_ReverseOrderInsertion(t *testing.T) {
	t.Parallel()
	// Positions in ascending order; function processes them in reverse so
	// inserting style strings at later byte offsets first does not shift
	// earlier offsets.
	curStyle := lipgloss.NewStyle()
	other := lipgloss.NewStyle()
	positions := []SearchMatchPos{
		{LineIdx: 0, ByteStart: 0, ByteEnd: 3}, // "foo"
		{LineIdx: 0, ByteStart: 4, ByteEnd: 7}, // "bar"
	}
	got := ApplyWordLevelHighlight("foo bar", positions, nil, -1, curStyle, other)
	stripped := stripAnsi(got)
	if stripped != "foo bar" {
		t.Fatalf("stripped content should equal input 'foo bar', got %q", stripped)
	}
}

// stripAnsi is a local helper for search_test (renames StripANSIApprox to avoid
// over-asserting on internal API surface from tests).
func stripAnsi(s string) string {
	return StripANSIApprox(s)
}
