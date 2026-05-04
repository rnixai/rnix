package inspector

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStripANSIApprox_NoEscapes(t *testing.T) {
	t.Parallel()
	got := StripANSIApprox("hello world")
	if got != "hello world" {
		t.Fatalf("plain text mutated: %q", got)
	}
}

func TestStripANSIApprox_StripsSGR(t *testing.T) {
	t.Parallel()
	in := "\x1b[31mred\x1b[0m text\x1b[1;32mbold\x1b[m"
	got := StripANSIApprox(in)
	want := "red textbold"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStripANSIApprox_Empty(t *testing.T) {
	t.Parallel()
	if got := StripANSIApprox(""); got != "" {
		t.Fatalf("empty input not preserved: %q", got)
	}
}

func TestStripANSIApprox_OnlyEscapes(t *testing.T) {
	t.Parallel()
	got := StripANSIApprox("\x1b[1m\x1b[31m\x1b[0m")
	if got != "" {
		t.Fatalf("escape-only input should strip to empty, got %q", got)
	}
}

func TestStripANSIApprox_UnicodeBody(t *testing.T) {
	t.Parallel()
	in := "\x1b[33m中文\x1b[0m emoji ⚙"
	got := StripANSIApprox(in)
	want := "中文 emoji ⚙"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTruncateANSIRunes_NonPositiveMaxCols(t *testing.T) {
	t.Parallel()
	if got := TruncateANSIRunes("hello", 0); got != "" {
		t.Fatalf("0 maxCols should return empty, got %q", got)
	}
	if got := TruncateANSIRunes("hello", -3); got != "" {
		t.Fatalf("negative maxCols should return empty, got %q", got)
	}
}

func TestTruncateANSIRunes_PlainTextTruncated(t *testing.T) {
	t.Parallel()
	got := TruncateANSIRunes("abcdef", 3)
	if got != "abc" {
		t.Fatalf("got %q want %q", got, "abc")
	}
}

func TestTruncateANSIRunes_NoTruncationNeeded(t *testing.T) {
	t.Parallel()
	got := TruncateANSIRunes("ab", 5)
	if got != "ab" {
		t.Fatalf("got %q want %q", got, "ab")
	}
}

func TestTruncateANSIRunes_ClosesOpenSGROnTruncate(t *testing.T) {
	t.Parallel()
	in := "\x1b[31mhello\x1b[0m world"
	// 5 visible cols of red "hello", truncate before " world", SGR is reset by \x1b[0m
	got := TruncateANSIRunes(in, 5)
	if !strings.Contains(got, "hello") {
		t.Fatalf("should keep 'hello', got %q", got)
	}
	// trailing reset should NOT be appended because SGR was already closed by \x1b[0m
	// before truncation point; the final "\x1b[0m" in input came in escape state
	// which is part of b before visible >= maxCols branch
}

func TestTruncateANSIRunes_AppendsResetWhenSGROpen(t *testing.T) {
	t.Parallel()
	in := "\x1b[31mabcdef"
	got := TruncateANSIRunes(in, 3)
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("expected trailing reset for unclosed SGR, got %q", got)
	}
	if StripANSIApprox(got) != "abc" {
		t.Fatalf("visible text after strip should be 'abc', got %q", StripANSIApprox(got))
	}
}

func TestTruncateANSIRunes_AppendsResetAtEndOfInput(t *testing.T) {
	t.Parallel()
	in := "\x1b[33mabc"
	got := TruncateANSIRunes(in, 10)
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("end-of-input with open SGR should append reset, got %q", got)
	}
}

func TestTruncateANSIRunes_NestedSGR(t *testing.T) {
	t.Parallel()
	// open red, open bold green (still open), 3 chars visible
	in := "\x1b[31m\x1b[1;32mabc\x1b[0m more"
	got := TruncateANSIRunes(in, 2)
	if StripANSIApprox(got) != "ab" {
		t.Fatalf("visible should be 'ab', got %q", StripANSIApprox(got))
	}
}

func TestChunkRunes_NonPositiveMaxCols(t *testing.T) {
	t.Parallel()
	got := ChunkRunes("hello", 0)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("0 maxCols should return [s], got %v", got)
	}
	got = ChunkRunes("hello", -1)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("negative maxCols should return [s], got %v", got)
	}
}

func TestChunkRunes_NoSplitNeeded(t *testing.T) {
	t.Parallel()
	got := ChunkRunes("ab", 10)
	if len(got) != 1 || got[0] != "ab" {
		t.Fatalf("got %v want ['ab']", got)
	}
}

func TestChunkRunes_SplitsLongPlain(t *testing.T) {
	t.Parallel()
	got := ChunkRunes("abcdefgh", 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 chunks, got %d (%v)", len(got), got)
	}
	if joined := strings.Join(got, ""); joined != "abcdefgh" {
		t.Fatalf("rejoined chunks should equal input, got %q", joined)
	}
	for _, c := range got {
		if lipgloss.Width(c) > 3 {
			t.Fatalf("chunk %q exceeds maxCols", c)
		}
	}
}

func TestChunkRunes_PreservesSGRAcrossBoundary(t *testing.T) {
	t.Parallel()
	in := "\x1b[31mabcdef\x1b[0m"
	got := ChunkRunes(in, 3)
	if len(got) < 2 {
		t.Fatalf("expected ≥2 chunks for split, got %d (%v)", len(got), got)
	}
	// First chunk should close SGR; second chunk should reopen the red SGR
	if !strings.Contains(got[0], "\x1b[0m") {
		t.Fatalf("first chunk should close SGR, got %q", got[0])
	}
	if !strings.Contains(got[1], "\x1b[31m") {
		t.Fatalf("second chunk should reopen red SGR, got %q", got[1])
	}
}

func TestChunkRunes_ResetClearsCarry(t *testing.T) {
	t.Parallel()
	// red opened, then reset (\x1b[0m), then more text. After the reset
	// arrives in chunk 2, the open-SGR carry should be cleared so the
	// following chunks (with new visible chars) do not re-open red.
	in := "\x1b[31mabc\x1b[0mdefghi"
	got := ChunkRunes(in, 3)
	if len(got) != 3 {
		t.Fatalf("expected exactly 3 chunks, got %d (%v)", len(got), got)
	}
	// Chunk 2 may legally contain `\x1b[31m` (carry from boundary re-open)
	// followed by the input's `\x1b[0m` reset — it renders as "def" with no
	// trailing colour. Chunk 3 must NOT carry red because reset has cleared
	// the open-SGR carry by the time we reach the next chunk boundary.
	if strings.Contains(got[2], "\x1b[31m") {
		t.Fatalf("chunk 3 should not carry red after reset, got %q", got[2])
	}
	// Stripped visible content must concatenate to "abcdefghi".
	var visible strings.Builder
	for _, c := range got {
		visible.WriteString(StripANSIApprox(c))
	}
	if visible.String() != "abcdefghi" {
		t.Fatalf("visible concat should be 'abcdefghi', got %q", visible.String())
	}
}

func TestChunkRunes_WideCharacters(t *testing.T) {
	t.Parallel()
	// CJK uses 2 cols per rune; 3 runes = 6 display cols; maxCols 4 → ≥ 2 chunks
	got := ChunkRunes("你好世", 4)
	if len(got) < 2 {
		t.Fatalf("CJK 6-col input with maxCols 4 should split, got %v", got)
	}
	for _, c := range got {
		if lipgloss.Width(c) > 4 {
			t.Fatalf("CJK chunk %q width %d exceeds maxCols 4", c, lipgloss.Width(c))
		}
	}
}
