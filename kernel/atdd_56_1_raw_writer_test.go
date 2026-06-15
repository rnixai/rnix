package kernel

import (
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Story 56.1 — kernel.RawWriter NDJSON 落盘 (AC#4)
//
// 56-1-UNIT-009/010/011. RawWriter 镜像 EventWriter (kernel/event_writer.go)，
// 写 <baseDir>/steps/<uuid>/raw.jsonl。
//
// RED：源码骨架 (kernel/raw_writer.go) 全部返回 nil/no-op，三条用例全部
// t.Skip("RED")。dev-story 实现 bufio+mu+O_APPEND 后移除 skip 即转 GREEN。
// ============================================================================

// 56-1-UNIT-009: WriteRaw → ReadAllRaw / ReadRawForStep 一致 (round-trip)。
func TestATDD_56_1_009_RawWriter_RoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	uuid := "test-uuid-roundtrip"

	rw, err := NewRawWriter(baseDir, uuid)
	if err != nil {
		t.Fatalf("NewRawWriter: %v", err)
	}

	records := []vfs.RawCapture{
		{
			TsMs:    1000,
			Step:    1,
			Kind:    "api",
			Request: map[string]any{"url": "https://api.example/v1/chat"},
			Response: map[string]any{
				"status": float64(200),
				"body":   "first call",
			},
			Truncated:     false,
			OriginalBytes: 0,
		},
		{
			TsMs:          2000,
			Step:           2,
			Kind:           "cli",
			Request:        map[string]any{"argv": []any{"claude", "-p", "hello"}},
			Response:       map[string]any{"exit_code": float64(0), "stdout": "second call"},
			Truncated:      true,
			OriginalBytes:  10240,
		},
	}
	for _, rec := range records {
		if err := rw.WriteRaw(rec); err != nil {
			t.Fatalf("WriteRaw step=%d: %v", rec.Step, err)
		}
	}
	if err := rw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(baseDir, "steps", uuid, "raw.jsonl")

	// --- ReadAllRaw ---
	all, err := ReadAllRaw(path)
	if err != nil {
		t.Fatalf("ReadAllRaw: %v", err)
	}
	if len(all) != len(records) {
		t.Fatalf("ReadAllRaw len = %d, want %d", len(all), len(records))
	}
	for i, want := range records {
		got := all[i]
		if got.Step != want.Step || got.Kind != want.Kind ||
			got.Truncated != want.Truncated || got.OriginalBytes != want.OriginalBytes ||
			got.TsMs != want.TsMs {
			t.Errorf("record[%d] mismatch:\n  got  = %+v\n  want = %+v", i, got, want)
		}
	}

	// --- ReadRawForStep ---
	step2, err := ReadRawForStep(path, 2)
	if err != nil {
		t.Fatalf("ReadRawForStep(2): %v", err)
	}
	if step2 == nil {
		t.Fatal("ReadRawForStep(2) returned nil, want record")
	}
	if step2.Kind != "cli" {
		t.Errorf("step 2 Kind = %q, want %q", step2.Kind, "cli")
	}

	// 不存在的 step 返回 nil（无错误）
	stepX, err := ReadRawForStep(path, 999)
	if err != nil {
		t.Fatalf("ReadRawForStep(999): %v", err)
	}
	if stepX != nil {
		t.Errorf("ReadRawForStep(999) = %+v, want nil (no such step)", stepX)
	}
}

// 56-1-UNIT-010: Close 幂等 + Flush 不丢数据。
func TestATDD_56_1_010_RawWriter_CloseFlushIdempotent(t *testing.T) {
	baseDir := t.TempDir()
	rw, err := NewRawWriter(baseDir, "uuid-close")
	if err != nil {
		t.Fatalf("NewRawWriter: %v", err)
	}
	if err := rw.WriteRaw(vfs.RawCapture{TsMs: 1, Step: 1, Kind: "api"}); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	if err := rw.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// 二次 Close 必须幂等（仿 stepWriter / eventWriter 的 reap 路径要求）
	if err := rw.Close(); err != nil {
		t.Errorf("second Close = %v, want nil (idempotent)", err)
	}

	// Close 后 ReadAllRaw 必须看到记录（说明 Flush 正确）
	path := filepath.Join(baseDir, "steps", "uuid-close", "raw.jsonl")
	all, err := ReadAllRaw(path)
	if err != nil {
		t.Fatalf("ReadAllRaw post-close: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("post-close ReadAllRaw len = %d, want 1 (Flush should have persisted)", len(all))
	}
}

// 56-1-UNIT-011: 落点 = <baseDir>/steps/<uuid>/raw.jsonl（gc 复用前提）。
func TestATDD_56_1_011_RawWriter_PathLocation(t *testing.T) {
	baseDir := t.TempDir()
	uuid := "uuid-path-check"
	rw, err := NewRawWriter(baseDir, uuid)
	if err != nil {
		t.Fatalf("NewRawWriter: %v", err)
	}
	t.Cleanup(func() { _ = rw.Close() })

	want := filepath.Join(baseDir, "steps", uuid, "raw.jsonl")
	if got := rw.Path(); got != want {
		t.Errorf("RawWriter.Path() = %q, want %q "+
			"(gc 复用要求 raw.jsonl 落在 <uuid>/ 目录内 — AC#8)", got, want)
	}
}
