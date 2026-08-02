package jsonl

// Story 72.1 AC8-1: five cases for the unbounded scanner.
//
// The 1.5 MB and 8 MB cases are the load-bearing ones — 1.5 MB proves the old
// 1 MB / 256 KB limits are gone, and 8 MB is the only case that rules out a
// "just raise scanner.Buffer to 4 MB" pseudo-fix (F6).

import (
	"encoding/json"
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

// --- code-review 2026-08-02: ReadSlice 不变量护栏 ---------------------------
//
// Scan 的底座是 bufio.Reader.ReadSlice（复用内部 buffer，零分配），不是
// ReadBytes（每行新分配）。这带来两条必须被测试钉死的不变量，否则一次
// 「看起来更简单」的重写就会静默破坏正确性：
//   1. fn 拿到的 line 在 fn 返回后即失效 —— 但 json.Unmarshal 的产物必须完好；
//   2. 真实 io 错误时不得把半行当完整行交给 fn（否则 parse 错误掩盖 io 错因）。

// TestScan_NoAliasingAcrossLines 守不变量 1：buffer 复用不得污染已 unmarshal
// 的数据。若哪天有人让 fn 保留 line 引用做缓存，或改用别名式解码，此测试转红。
func TestScan_NoAliasingAcrossLines(t *testing.T) {
	type rec struct {
		Raw json.RawMessage `json:"raw"`
		Str string          `json:"str"`
	}

	const n = 40
	var sb strings.Builder
	for i := range n {
		ch := string(rune('a' + i%26))
		sb.WriteString(`{"raw":{"k":"` + strings.Repeat(ch, 3000) + `"},"str":"` + strings.Repeat(ch, 3000) + `"}` + "\n")
	}

	var got []rec
	err := Scan(strings.NewReader(sb.String()), "alias", func(line []byte) error {
		var r rec
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		got = append(got, r) // 合法：unmarshal 后的结构体自持有
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != n {
		t.Fatalf("got %d records, want %d", len(got), n)
	}

	// 扫描早已结束、buffer 被复用多轮：内容必须仍与写入时一致。
	for i, r := range got {
		want := strings.Repeat(string(rune('a'+i%26)), 3000)
		if r.Str != want {
			t.Fatalf("record %d: string corrupted by buffer reuse", i)
		}
		var raw struct{ K string }
		if err := json.Unmarshal(r.Raw, &raw); err != nil {
			t.Fatalf("record %d: RawMessage no longer valid JSON (buffer overwritten): %v", i, err)
		}
		if raw.K != want {
			t.Fatalf("record %d: RawMessage content corrupted by buffer reuse", i)
		}
	}
}

// 超长行走 scratch 拼接路径（唯一分配路径），同样不得别名污染。
func TestScan_NoAliasingOversizedLines(t *testing.T) {
	var sb strings.Builder
	for _, ch := range []string{"x", "y", "z"} {
		sb.WriteString(`{"v":"` + strings.Repeat(ch, 200*1024) + `"}` + "\n")
	}

	var vals []string
	err := Scan(strings.NewReader(sb.String()), "alias-big", func(line []byte) error {
		var r struct{ V string }
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		vals = append(vals, r.V)
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(vals) != 3 {
		t.Fatalf("got %d lines, want 3", len(vals))
	}
	for i, ch := range []string{"x", "y", "z"} {
		if want := strings.Repeat(ch, 200*1024); vals[i] != want {
			t.Errorf("oversized line %d corrupted (len %d, want %d)", i, len(vals[i]), len(want))
		}
	}
}

// errAfterData 读完 data 后返回一个真实（非 EOF）io 错误。
type errAfterData struct {
	data []byte
	pos  int
	fail error
}

func (e *errAfterData) Read(p []byte) (int, error) {
	if e.pos >= len(e.data) {
		return 0, e.fail
	}
	n := copy(p, e.data[e.pos:])
	e.pos += n
	return n, nil
}

// TestScan_PartialLineNotDeliveredOnRealError 守不变量 2。
//
// 撕裂读（错误发生在一行中途）时，手上的字节只是某行的前半段。把它当完整行
// 交给 fn 会导致 unmarshal 失败，调用方于是报 PARSE 错误 —— 真正的 io 错因就此
// 消失。这正是本 story AC3 要消灭的「失败被伪装成别的东西」，只是换了一层。
// 被替换掉的 bufio.Scanner 在读错误时也从不产出 token，此处保持同一语义。
func TestScan_PartialLineNotDeliveredOnRealError(t *testing.T) {
	errDisk := errors.New("simulated disk failure")
	// 一行完整 + 一行中途失败（无换行）。
	r := &errAfterData{data: []byte(`{"step":1}` + "\n" + `{"step":2,"partial"`), fail: errDisk}

	var seen []string
	err := Scan(r, "torn", func(line []byte) error {
		seen = append(seen, strings.TrimRight(string(line), "\n"))
		return nil
	})

	if !errors.Is(err, errDisk) {
		t.Errorf("Scan error = %v, want the real io error verbatim", err)
	}
	if len(seen) != 1 {
		t.Errorf("fn saw %d lines (%q), want 1 — the torn fragment must not be delivered", len(seen), seen)
	}
}
