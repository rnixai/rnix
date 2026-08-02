package jsonl

// Story 72.1 AC8-1: five cases for the unbounded scanner.
//
// The 1.5 MB and 8 MB cases are the load-bearing ones — 1.5 MB proves the old
// 1 MB / 256 KB limits are gone, and 8 MB is the only case that rules out a
// "just raise scanner.Buffer to 4 MB" pseudo-fix (F6).

import (
	"errors"
	"strings"
	"testing"
)

func TestScan_Line15MB(t *testing.T) {
	line := strings.Repeat("x", 1500*1024)
	r := strings.NewReader(line + "\n")

	var got []string
	err := Scan(r, "test", func(l []byte) error {
		got = append(got, string(l))
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	// The line includes its trailing newline.
	if len(got[0]) != len(line)+1 {
		t.Errorf("line len = %d, want %d", len(got[0]), len(line)+1)
	}
}

func TestScan_Line8MB(t *testing.T) {
	line := strings.Repeat("y", 8*1024*1024)
	r := strings.NewReader(line + "\n")

	var got []string
	err := Scan(r, "test", func(l []byte) error {
		got = append(got, string(l))
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	if len(got[0]) != len(line)+1 {
		t.Errorf("line len = %d, want %d (line must not be truncated)", len(got[0]), len(line)+1)
	}
}

func TestScan_NoTrailingNewline(t *testing.T) {
	// The last line has no trailing newline. bufio.Reader.ReadBytes returns it
	// together with io.EOF; Scan must process it before acting on the error,
	// or it is silently dropped.
	r := strings.NewReader("first\nsecond")

	var got []string
	err := Scan(r, "test", func(l []byte) error {
		got = append(got, strings.TrimRight(string(l), "\n"))
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("got %v, want [first second] (last line without newline must not be dropped)", got)
	}
}

func TestScan_AllBlankLines(t *testing.T) {
	r := strings.NewReader("\n\n   \n\t\n")

	called := 0
	err := Scan(r, "test", func(l []byte) error {
		called++
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if called != 0 {
		t.Errorf("fn called %d times, want 0 (blank lines are skipped)", called)
	}
}

func TestScan_FnErrorPropagates(t *testing.T) {
	sentinel := errors.New("stop here")
	r := strings.NewReader("a\nb\nc\n")

	var seen []string
	err := Scan(r, "test", func(l []byte) error {
		seen = append(seen, strings.TrimRight(string(l), "\n"))
		if len(seen) == 2 {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Scan error = %v, want sentinel", err)
	}
	if len(seen) != 2 {
		t.Errorf("scanned %d lines, want 2 (must stop at fn error)", len(seen))
	}
}

// TestScan_EmptyReader guards the degenerate case: no data at all.
func TestScan_EmptyReader(t *testing.T) {
	called := 0
	err := Scan(strings.NewReader(""), "test", func(l []byte) error {
		called++
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if called != 0 {
		t.Errorf("fn called %d times on empty reader, want 0", called)
	}
}
