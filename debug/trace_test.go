package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

func TestGenerateTraceID_Format(t *testing.T) {
	id := GenerateTraceID()
	if len(id) != 32 {
		t.Errorf("expected 32 chars, got %d", len(id))
	}
	matched, _ := regexp.MatchString("^[0-9a-f]{32}$", string(id))
	if !matched {
		t.Errorf("expected hex string, got %q", id)
	}
}

func TestGenerateTraceID_Unique(t *testing.T) {
	seen := make(map[types.TraceID]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateTraceID()
		if seen[id] {
			t.Errorf("duplicate trace ID at iteration %d: %s", i, id)
		}
		seen[id] = true
	}
}

func TestGenerateSpanID_Format(t *testing.T) {
	id := GenerateSpanID()
	if len(id) != 32 {
		t.Errorf("expected 32 chars, got %d", len(id))
	}
	matched, _ := regexp.MatchString("^[0-9a-f]{32}$", string(id))
	if !matched {
		t.Errorf("expected hex string, got %q", id)
	}
}

func TestGenerateSpanID_Unique(t *testing.T) {
	seen := make(map[types.SpanID]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateSpanID()
		if seen[id] {
			t.Errorf("duplicate span ID at iteration %d: %s", i, id)
		}
		seen[id] = true
	}
}

func TestSpanRecorder_StartSpan(t *testing.T) {
	r := NewSpanRecorder()
	pid := types.PID(1)
	traceID := types.TraceID("abc123")
	spanID := types.SpanID("def456")
	parentSpanID := types.SpanID("parent789")
	name := "test-span"

	r.StartSpan(pid, traceID, spanID, parentSpanID, name)

	s := r.GetSpan(pid)
	if s == nil {
		t.Fatal("expected span, got nil")
	}
	if s.TraceID != traceID || s.SpanID != spanID || s.ParentSpanID != parentSpanID {
		t.Errorf("trace/span IDs mismatch: trace=%q span=%q parent=%q", s.TraceID, s.SpanID, s.ParentSpanID)
	}
	if s.PID != pid || s.Name != name {
		t.Errorf("PID/Name mismatch: pid=%d name=%q", s.PID, s.Name)
	}
	if s.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}
	if s.SyscallCount != 0 || s.TokensUsed != 0 {
		t.Errorf("expected zero counts, got syscall=%d tokens=%d", s.SyscallCount, s.TokensUsed)
	}
}

func TestSpanRecorder_RecordSyscall(t *testing.T) {
	r := NewSpanRecorder()
	pid := types.PID(2)
	r.StartSpan(pid, "trace", "span", "", "span")

	r.RecordSyscall(pid)
	r.RecordSyscall(pid)
	r.RecordSyscall(pid)

	s := r.GetSpan(pid)
	if s == nil || s.SyscallCount != 3 {
		t.Errorf("expected SyscallCount=3, got %v", s)
	}
}

func TestSpanRecorder_RecordTokens(t *testing.T) {
	r := NewSpanRecorder()
	pid := types.PID(3)
	r.StartSpan(pid, "trace", "span", "", "span")

	r.RecordTokens(pid, 100)
	r.RecordTokens(pid, 50)

	s := r.GetSpan(pid)
	if s == nil || s.TokensUsed != 150 {
		t.Errorf("expected TokensUsed=150, got %v", s)
	}
}

func TestSpanRecorder_EndSpan(t *testing.T) {
	r := NewSpanRecorder()
	pid := types.PID(4)
	r.StartSpan(pid, "trace", "span", "", "span")

	r.EndSpan(pid, SpanOK)
	s := r.GetSpan(pid)
	if s == nil || s.Status != SpanOK || s.EndTime.IsZero() || s.Duration == 0 {
		t.Errorf("EndSpan OK: status=%d endTime=%v duration=%v", s.Status, s.EndTime, s.Duration)
	}

	r.StartSpan(pid+1, "trace", "span2", "", "span2")
	r.EndSpan(pid+1, SpanERROR)
	s2 := r.GetSpan(pid + 1)
	if s2 == nil || s2.Status != SpanERROR {
		t.Errorf("EndSpan ERROR: expected SpanERROR, got %d", s2.Status)
	}

	r.StartSpan(pid+2, "trace", "span3", "", "span3")
	r.EndSpan(pid+2, SpanTIMEOUT)
	s3 := r.GetSpan(pid + 2)
	if s3 == nil || s3.Status != SpanTIMEOUT {
		t.Errorf("EndSpan TIMEOUT: expected SpanTIMEOUT, got %d", s3.Status)
	}
}

func TestSpanRecorder_GetSpan(t *testing.T) {
	r := NewSpanRecorder()
	pid := types.PID(5)
	r.StartSpan(pid, "trace", "span", "", "span")

	s := r.GetSpan(pid)
	if s == nil {
		t.Error("expected span for existing pid, got nil")
	}

	s2 := r.GetSpan(types.PID(999))
	if s2 != nil {
		t.Error("expected nil for non-existent pid, got span")
	}
}

func TestSpanRecorder_GetTraceSpans(t *testing.T) {
	r := NewSpanRecorder()
	traceID := types.TraceID("trace-xyz")
	r.StartSpan(1, traceID, "s1", "", "span1")
	r.StartSpan(2, traceID, "s2", "s1", "span2")
	r.StartSpan(3, types.TraceID("other"), "s3", "", "span3")

	spans := r.GetTraceSpans(traceID)
	if len(spans) != 2 {
		t.Errorf("expected 2 spans for trace, got %d", len(spans))
	}

	empty := r.GetTraceSpans(types.TraceID("nonexistent"))
	if len(empty) != 0 {
		t.Errorf("expected 0 spans for unknown trace, got %d", len(empty))
	}
}

func TestSpanRecorder_ConcurrentAccess(t *testing.T) {
	r := NewSpanRecorder()
	traceID := types.TraceID("concurrent-trace")
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		pid := types.PID(i + 1)
		go func() {
			defer wg.Done()
			spanID := GenerateSpanID()
			r.StartSpan(pid, traceID, spanID, "", "concurrent-span")
			for j := 0; j < 10; j++ {
				r.RecordSyscall(pid)
				r.RecordTokens(pid, 5)
			}
			r.EndSpan(pid, SpanOK)
			_ = r.GetSpan(pid)
		}()
	}

	wg.Wait()

	spans := r.GetTraceSpans(traceID)
	if len(spans) != 100 {
		t.Errorf("expected 100 spans, got %d", len(spans))
	}
}

func TestSpanWriter_WriteSpan(t *testing.T) {
	baseDir := t.TempDir()
	w := NewSpanWriter(baseDir)
	span := &Span{
		TraceID:      types.TraceID("trace-abc123"),
		SpanID:       types.SpanID("span-def456"),
		ParentSpanID: types.SpanID(""),
		PID:          types.PID(1),
		Name:         "test-span",
		StartTime:    time.UnixMilli(1000),
		EndTime:      time.UnixMilli(1500),
		Duration:     500 * time.Millisecond,
		SyscallCount: 3,
		TokensUsed:   100,
		Status:       SpanOK,
	}
	if err := w.WriteSpan(span); err != nil {
		t.Fatalf("WriteSpan: %v", err)
	}
	path := filepath.Join(baseDir, "trace-abc123", "spans.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed["trace_id"] != "trace-abc123" || parsed["span_id"] != "span-def456" {
		t.Errorf("unexpected trace_id/span_id: %v", parsed)
	}
	if parsed["start_time_ms"] != float64(1000) || parsed["end_time_ms"] != float64(1500) || parsed["duration_ms"] != float64(500) {
		t.Errorf("unexpected time fields: %v", parsed)
	}
	if parsed["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", parsed["status"])
	}
}

func TestSpanWriter_AppendMultiple(t *testing.T) {
	baseDir := t.TempDir()
	w := NewSpanWriter(baseDir)
	traceID := types.TraceID("trace-multi")
	for i := 0; i < 3; i++ {
		span := &Span{
			TraceID:      traceID,
			SpanID:       types.SpanID("span-" + string(rune('0'+i))),
			ParentSpanID: types.SpanID(""),
			PID:          types.PID(i + 1),
			Name:         "span",
			StartTime:    time.UnixMilli(int64(1000 + i)),
			EndTime:      time.UnixMilli(int64(1500 + i)),
			Duration:     time.Duration(500+i) * time.Millisecond,
			Status:       SpanOK,
		}
		if err := w.WriteSpan(span); err != nil {
			t.Fatalf("WriteSpan %d: %v", i, err)
		}
	}
	path := filepath.Join(baseDir, string(traceID), "spans.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d", lines)
	}
}

func TestSpanReader_ReadSpans(t *testing.T) {
	baseDir := t.TempDir()
	w := NewSpanWriter(baseDir)
	traceID := types.TraceID("trace-read")
	spans := []*Span{
		{TraceID: traceID, SpanID: "s1", PID: 1, Name: "a", StartTime: time.UnixMilli(100), EndTime: time.UnixMilli(200), Duration: 100 * time.Millisecond, Status: SpanOK},
		{TraceID: traceID, SpanID: "s2", PID: 2, Name: "b", StartTime: time.UnixMilli(150), EndTime: time.UnixMilli(300), Duration: 150 * time.Millisecond, Status: SpanERROR},
	}
	for _, s := range spans {
		if err := w.WriteSpan(s); err != nil {
			t.Fatalf("WriteSpan: %v", err)
		}
	}
	reader := NewSpanReader(baseDir)
	got, err := reader.ReadSpans(traceID)
	if err != nil {
		t.Fatalf("ReadSpans: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(got))
	}
	if got[0].SpanID != "s1" || got[0].Status != SpanOK || got[0].Duration != 100*time.Millisecond {
		t.Errorf("span 0 mismatch: %+v", got[0])
	}
	if got[1].SpanID != "s2" || got[1].Status != SpanERROR || got[1].Duration != 150*time.Millisecond {
		t.Errorf("span 1 mismatch: %+v", got[1])
	}
}

func TestSpanReader_ReadSpans_Empty(t *testing.T) {
	baseDir := t.TempDir()
	reader := NewSpanReader(baseDir)
	got, err := reader.ReadSpans(types.TraceID("nonexistent-trace"))
	if err != nil {
		t.Fatalf("ReadSpans: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d spans", len(got))
	}
}

func TestSpanRecorder_EndSpan_PersistsWhenWriterSet(t *testing.T) {
	baseDir := t.TempDir()
	w := NewSpanWriter(baseDir)
	r := NewSpanRecorder()
	r.SetWriter(w)
	traceID := types.TraceID("trace-persist")
	spanID := types.SpanID("span-1")
	r.StartSpan(1, traceID, spanID, "", "persist-test")
	r.RecordSyscall(1)
	r.RecordTokens(1, 50)
	r.EndSpan(1, SpanOK)
	reader := NewSpanReader(baseDir)
	spans, err := reader.ReadSpans(traceID)
	if err != nil {
		t.Fatalf("ReadSpans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 persisted span, got %d", len(spans))
	}
	if spans[0].SpanID != spanID || spans[0].SyscallCount != 1 || spans[0].TokensUsed != 50 || spans[0].Status != SpanOK {
		t.Errorf("persisted span mismatch: %+v", spans[0])
	}
}

func TestSpanStatus_Values(t *testing.T) {
	tests := []struct {
		s    SpanStatus
		want string
	}{
		{SpanOK, "ok"},
		{SpanERROR, "error"},
		{SpanTIMEOUT, "timeout"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("SpanStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
		data, err := json.Marshal(tt.s)
		if err != nil {
			t.Errorf("Marshal SpanStatus: %v", err)
			continue
		}
		if string(data) != `"`+tt.want+`"` {
			t.Errorf("Marshal SpanStatus = %s, want %q", data, tt.want)
		}
	}
}
