package debug

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// SpanStatus represents the outcome of a span.
type SpanStatus int

const (
	SpanOK SpanStatus = iota
	SpanERROR
	SpanTIMEOUT
)

func (s SpanStatus) String() string {
	switch s {
	case SpanOK:
		return "ok"
	case SpanERROR:
		return "error"
	case SpanTIMEOUT:
		return "timeout"
	default:
		return "unknown"
	}
}

func (s SpanStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *SpanStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "ok":
		*s = SpanOK
	case "error":
		*s = SpanERROR
	case "timeout":
		*s = SpanTIMEOUT
	default:
		*s = SpanOK
	}
	return nil
}

// SpanEvent records an event within a span.
type SpanEvent struct {
	TimestampMs int64
	Name       string
	Attrs      map[string]any
}

// Span represents a single span in a distributed trace.
type Span struct {
	TraceID       types.TraceID
	SpanID        types.SpanID
	ParentSpanID  types.SpanID
	PID           types.PID
	Name          string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	SyscallCount  int
	TokensUsed    int
	Status        SpanStatus
	Events        []SpanEvent
}

type spanJSON struct {
	TraceID       types.TraceID  `json:"trace_id"`
	SpanID        types.SpanID   `json:"span_id"`
	ParentSpanID  types.SpanID   `json:"parent_span_id"`
	PID           types.PID     `json:"pid"`
	Name          string        `json:"name"`
	StartTimeMs   int64         `json:"start_time_ms"`
	EndTimeMs     int64         `json:"end_time_ms"`
	DurationMs    int64         `json:"duration_ms"`
	SyscallCount  int           `json:"syscall_count"`
	TokensUsed    int           `json:"tokens_used"`
	Status        SpanStatus    `json:"status"`
	Events        []SpanEvent   `json:"events,omitempty"`
}

func spanToJSON(s *Span) spanJSON {
	return spanJSON{
		TraceID:      s.TraceID,
		SpanID:       s.SpanID,
		ParentSpanID: s.ParentSpanID,
		PID:          s.PID,
		Name:         s.Name,
		StartTimeMs:  s.StartTime.UnixMilli(),
		EndTimeMs:    s.EndTime.UnixMilli(),
		DurationMs:   s.Duration.Milliseconds(),
		SyscallCount: s.SyscallCount,
		TokensUsed:   s.TokensUsed,
		Status:       s.Status,
		Events:       s.Events,
	}
}

func spanFromJSON(j spanJSON) *Span {
	return &Span{
		TraceID:      j.TraceID,
		SpanID:       j.SpanID,
		ParentSpanID: j.ParentSpanID,
		PID:          j.PID,
		Name:         j.Name,
		StartTime:    time.UnixMilli(j.StartTimeMs),
		EndTime:      time.UnixMilli(j.EndTimeMs),
		Duration:     time.Duration(j.DurationMs) * time.Millisecond,
		SyscallCount: j.SyscallCount,
		TokensUsed:   j.TokensUsed,
		Status:       j.Status,
		Events:       j.Events,
	}
}

// SpanWriter writes completed spans to JSONL files.
type SpanWriter struct {
	baseDir string
}

// NewSpanWriter creates a new SpanWriter with the given base directory.
func NewSpanWriter(baseDir string) *SpanWriter {
	return &SpanWriter{baseDir: baseDir}
}

// WriteSpan appends a span as a single JSON line to baseDir/<trace-id>/spans.jsonl.
func (w *SpanWriter) WriteSpan(span *Span) error {
	dir := filepath.Join(w.baseDir, string(span.TraceID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("trace: create dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "spans.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("trace: open %s: %w", path, err)
	}
	defer f.Close()
	data, err := json.Marshal(spanToJSON(span))
	if err != nil {
		return fmt.Errorf("trace: marshal span: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("trace: write span: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("trace: write newline: %w", err)
	}
	return nil
}

// SpanReader reads spans from JSONL files.
type SpanReader struct {
	baseDir string
}

// NewSpanReader creates a new SpanReader with the given base directory.
func NewSpanReader(baseDir string) *SpanReader {
	return &SpanReader{baseDir: baseDir}
}

// ReadSpans reads all spans for the given trace ID from baseDir/<trace-id>/spans.jsonl.
func (r *SpanReader) ReadSpans(traceID types.TraceID) ([]*Span, error) {
	path := filepath.Join(r.baseDir, string(traceID), "spans.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("trace: open %s: %w", path, err)
	}
	defer f.Close()
	var spans []*Span
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var j spanJSON
		if err := json.Unmarshal(line, &j); err != nil {
			return nil, fmt.Errorf("trace: unmarshal span: %w", err)
		}
		spans = append(spans, spanFromJSON(j))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("trace: read spans: %w", err)
	}
	return spans, nil
}

// GenerateTraceID returns a new 32-character hex trace ID.
func GenerateTraceID() types.TraceID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return types.TraceID(hex.EncodeToString(b))
}

// GenerateSpanID returns a new 32-character hex span ID.
func GenerateSpanID() types.SpanID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return types.SpanID(hex.EncodeToString(b))
}

// SpanRecorder records spans for distributed tracing.
type SpanRecorder struct {
	mu     sync.Mutex
	spans  map[types.PID]*Span
	writer *SpanWriter
}

// NewSpanRecorder creates a new SpanRecorder.
func NewSpanRecorder() *SpanRecorder {
	return &SpanRecorder{
		spans: make(map[types.PID]*Span),
	}
}

// SetWriter sets the optional SpanWriter for persisting completed spans.
func (r *SpanRecorder) SetWriter(w *SpanWriter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writer = w
}

// StartSpan creates a new span for the given process.
func (r *SpanRecorder) StartSpan(pid types.PID, traceID types.TraceID, spanID types.SpanID, parentSpanID types.SpanID, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans[pid] = &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		PID:          pid,
		Name:         name,
		StartTime:    time.Now(),
	}
}

// RecordSyscall increments the syscall count for the given process.
func (r *SpanRecorder) RecordSyscall(pid types.PID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.spans[pid]; ok {
		s.SyscallCount++
	}
}

// RecordTokens adds tokens to the span's TokensUsed.
func (r *SpanRecorder) RecordTokens(pid types.PID, tokens int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.spans[pid]; ok {
		s.TokensUsed += tokens
	}
}

// EndSpan sets EndTime, Duration, and Status for the span.
// If a SpanWriter is set, the completed span is persisted.
func (r *SpanRecorder) EndSpan(pid types.PID, status SpanStatus) {
	r.mu.Lock()
	var toWrite *Span
	if s, ok := r.spans[pid]; ok {
		s.EndTime = time.Now()
		s.Duration = s.EndTime.Sub(s.StartTime)
		s.Status = status
		if r.writer != nil {
			toWrite = copySpan(s)
		}
	}
	writer := r.writer
	r.mu.Unlock()
	if writer != nil && toWrite != nil {
		if err := writer.WriteSpan(toWrite); err != nil {
			log.Printf("[trace] write span pid=%d trace=%s: %v", toWrite.PID, toWrite.TraceID, err)
		}
	}
}

// GetSpan returns a copy of the span for the given PID, or nil if not found.
func (r *SpanRecorder) GetSpan(pid types.PID) *Span {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.spans[pid]
	if !ok {
		return nil
	}
	return copySpan(s)
}

// GetTraceSpans returns all spans for the given trace ID.
func (r *SpanRecorder) GetTraceSpans(traceID types.TraceID) []*Span {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*Span
	for _, s := range r.spans {
		if s.TraceID == traceID {
			result = append(result, copySpan(s))
		}
	}
	return result
}

func copySpan(s *Span) *Span {
	cp := &Span{
		TraceID:      s.TraceID,
		SpanID:       s.SpanID,
		ParentSpanID: s.ParentSpanID,
		PID:          s.PID,
		Name:         s.Name,
		StartTime:    s.StartTime,
		EndTime:      s.EndTime,
		Duration:     s.Duration,
		SyscallCount: s.SyscallCount,
		TokensUsed:   s.TokensUsed,
		Status:       s.Status,
	}
	if len(s.Events) > 0 {
		cp.Events = make([]SpanEvent, len(s.Events))
		copy(cp.Events, s.Events)
	}
	return cp
}
