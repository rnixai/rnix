package inspector

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Note: env-mutating tests use t.Setenv (which is incompatible with t.Parallel).
// Pure tests are unaffected and continue using t.Parallel for speed.

func TestBoxWidth_Defaults(t *testing.T) {
	t.Parallel()
	if got := BoxWidth(0); got != 70 {
		t.Fatalf("BoxWidth(0) = %d, want 70", got)
	}
	if got := BoxWidth(-5); got != 70 {
		t.Fatalf("BoxWidth(-5) = %d, want 70", got)
	}
}

func TestBoxWidth_LargeTerminal(t *testing.T) {
	t.Parallel()
	// width-4 ≥ 70 → cap at 70
	if got := BoxWidth(120); got != 70 {
		t.Fatalf("BoxWidth(120) = %d, want 70 (cap)", got)
	}
	if got := BoxWidth(74); got != 70 {
		t.Fatalf("BoxWidth(74) = %d, want 70 (cap)", got)
	}
}

func TestBoxWidth_NarrowTerminal(t *testing.T) {
	t.Parallel()
	// width-4 between 20 and 70 → return width-4
	if got := BoxWidth(50); got != 46 {
		t.Fatalf("BoxWidth(50) = %d, want 46", got)
	}
	if got := BoxWidth(30); got != 26 {
		t.Fatalf("BoxWidth(30) = %d, want 26", got)
	}
}

func TestBoxWidth_VeryNarrow(t *testing.T) {
	t.Parallel()
	// width-4 < 20 → cap at min(20, mWidth)
	if got := BoxWidth(20); got != 20 { // 20-4=16 <20 → min(20, 20)=20
		t.Fatalf("BoxWidth(20) = %d, want 20", got)
	}
	if got := BoxWidth(10); got != 10 { // 10-4=6 <20 → min(20, 10)=10
		t.Fatalf("BoxWidth(10) = %d, want 10", got)
	}
	if got := BoxWidth(15); got != 15 { // 15-4=11 <20 → min(20, 15)=15
		t.Fatalf("BoxWidth(15) = %d, want 15", got)
	}
}

func TestBoxChar_Unicode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	cases := map[string]string{
		"tl": "┌", "tr": "┐", "bl": "└", "br": "┘", "h": "─", "v": "│",
	}
	for name, want := range cases {
		if got := BoxChar(name); got != want {
			t.Fatalf("BoxChar(%q) = %q, want %q", name, got, want)
		}
	}
	if got := BoxChar("unknown"); got != "?" {
		t.Fatalf("BoxChar(unknown) = %q, want '?'", got)
	}
}

func TestBoxChar_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	cornerNames := []string{"tl", "tr", "bl", "br"}
	for _, name := range cornerNames {
		if got := BoxChar(name); got != "+" {
			t.Fatalf("BoxChar(%q) ASCII = %q, want '+'", name, got)
		}
	}
	if got := BoxChar("h"); got != "-" {
		t.Fatalf("BoxChar(h) ASCII = %q, want '-'", got)
	}
	if got := BoxChar("v"); got != "|" {
		t.Fatalf("BoxChar(v) ASCII = %q, want '|'", got)
	}
	if got := BoxChar("unknown"); got != "?" {
		t.Fatalf("BoxChar(unknown) ASCII = %q, want '?'", got)
	}
}

func TestTruncateBoxContent_BelowThreshold(t *testing.T) {
	t.Parallel()
	in := strings.Repeat("a", 100)
	if got := TruncateBoxContent(in); got != in {
		t.Fatalf("short input mutated, got len %d want len %d", len(got), len(in))
	}
}

func TestTruncateBoxContent_AboveThreshold(t *testing.T) {
	in := strings.Repeat("a", TruncateThreshold+500)
	got := TruncateBoxContent(in)
	if !strings.Contains(got, "(truncated") {
		t.Fatalf("expected truncation notice, got tail %q", got[len(got)-100:])
	}
	if !strings.HasPrefix(got, strings.Repeat("a", TruncateThreshold)) {
		t.Fatalf("prefix should be truncated to threshold")
	}
}

func TestTruncateBoxContent_ExactThreshold(t *testing.T) {
	t.Parallel()
	in := strings.Repeat("a", TruncateThreshold)
	if got := TruncateBoxContent(in); got != in {
		t.Fatalf("input at exact threshold should not be truncated")
	}
}

func TestRenderTruncationNotice_Unicode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	got := RenderTruncationNotice(1500, 5000)
	if !strings.Contains(got, "1.5k") {
		t.Fatalf("expected 1.5k in shown count, got %q", got)
	}
	if !strings.Contains(got, "5.0k") {
		t.Fatalf("expected 5.0k in total count, got %q", got)
	}
	if !strings.Contains(got, " · ") {
		t.Fatalf("Unicode mode should use ' · ' separator, got %q", got)
	}
}

func TestRenderTruncationNotice_ASCIIMode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := RenderTruncationNotice(500, 999)
	if strings.Contains(got, " · ") {
		t.Fatalf("ASCII mode should not have ' · ', got %q", got)
	}
	if !strings.Contains(got, " - ") {
		t.Fatalf("ASCII mode should use ' - ' separator, got %q", got)
	}
	if !strings.Contains(got, "500") {
		t.Fatalf("expected literal '500' (sub-1k), got %q", got)
	}
}

func TestRenderBoxedSection_BasicShape(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	got := RenderBoxedSection("Input", "hello", "#888888", false, 30)
	stripped := StripANSIApprox(got)
	lines := strings.Split(stripped, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected ≥3 lines (top + body + bottom), got %d:\n%s", len(lines), stripped)
	}
	if !strings.HasPrefix(lines[0], "┌") {
		t.Fatalf("top line should start with ┌, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("top line should end with ┐, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "Input") {
		t.Fatalf("top line should contain title 'Input', got %q", lines[0])
	}
	bottom := lines[len(lines)-1]
	if !strings.HasPrefix(bottom, "└") || !strings.HasSuffix(bottom, "┘") {
		t.Fatalf("bottom line should be └...┘, got %q", bottom)
	}
}

func TestRenderBoxedSection_MinWidth(t *testing.T) {
	t.Parallel()
	got := RenderBoxedSection("X", "y", "#888888", false, 5)
	if got == "" {
		t.Fatal("expected non-empty output even at width<8")
	}
}

func TestRenderBoxedSection_TitleTruncation(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	longTitle := strings.Repeat("X", 30)
	got := RenderBoxedSection(longTitle, "body", "#888888", false, 20)
	stripped := StripANSIApprox(got)
	lines := strings.Split(stripped, "\n")
	xCount := strings.Count(lines[0], "X")
	if xCount > 14 {
		t.Fatalf("title not truncated, found %d X's in top edge: %q", xCount, lines[0])
	}
}

func TestRenderBoxedSection_EmptyBodyLine(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	got := RenderBoxedSection("T", "line1\n\nline3", "#888888", false, 20)
	stripped := StripANSIApprox(got)
	lines := strings.Split(stripped, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d:\n%s", len(lines), stripped)
	}
	if !strings.HasPrefix(lines[2], "│") || !strings.HasSuffix(lines[2], "│") {
		t.Fatalf("empty body line should be │...│, got %q", lines[2])
	}
}

func TestRenderBoxedSection_LongLineWraps(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	body := strings.Repeat("a", 50)
	got := RenderBoxedSection("T", body, "#888888", false, 20)
	stripped := StripANSIApprox(got)
	lines := strings.Split(stripped, "\n")
	if len(lines) < 6 {
		t.Fatalf("expected ≥6 lines (top + ≥4 chunks + bottom), got %d:\n%s", len(lines), stripped)
	}
}

func TestRenderBoxedSection_ASCIIMode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := RenderBoxedSection("T", "x", "#888888", false, 20)
	stripped := StripANSIApprox(got)
	if !strings.ContainsRune(stripped, '+') {
		t.Fatalf("ASCII mode should use '+' corners, got %q", stripped)
	}
	if strings.ContainsRune(stripped, '┌') {
		t.Fatalf("ASCII mode should not contain ┌, got %q", stripped)
	}
}

func TestRenderBoxedSection_BodyColor(t *testing.T) {
	t.Parallel()
	got := RenderBoxedSection("Err", "boom", "#ff0000", true, 20)
	stripped := StripANSIApprox(got)
	if !strings.Contains(stripped, "boom") {
		t.Fatalf("body should contain 'boom', got %q", stripped)
	}
}

func TestFormatCharCount_PrivateHelper(t *testing.T) {
	t.Parallel()
	cases := map[int]string{
		0:    "0",
		999:  "999",
		1000: "1.0k",
		1500: "1.5k",
	}
	for n, want := range cases {
		if got := formatCharCount(n); got != want {
			t.Fatalf("formatCharCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestRenderBoxedSection_RuneCountStable(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	got := RenderBoxedSection("Title", "abc", "#888888", false, 30)
	stripped := StripANSIApprox(got)
	lines := strings.Split(stripped, "\n")
	wantWidth := 30
	for i, line := range lines {
		w := utf8.RuneCountInString(line)
		// Allow 1-rune jitter for trailing border alignment
		if w != wantWidth && w != wantWidth-1 && w != wantWidth+1 {
			t.Fatalf("line %d width %d != %d (line=%q)", i, w, wantWidth, line)
		}
	}
}
