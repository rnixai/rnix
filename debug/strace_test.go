package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

func TestFormatEvent_BasicFormat(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 12 * time.Millisecond,
		PID:       1,
		Syscall:   "Open",
		Args:      map[string]any{"flags": 2, "path": "/dev/null"},
		Result:    "FD(3)",
		Err:       nil,
		Duration:  1 * time.Millisecond,
	}
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// Verify timestamp format [  0.012s]
	if !strings.HasPrefix(got, "[  0.012s]") {
		t.Fatalf("expected timestamp prefix [  0.012s], got %q", got)
	}
	// Verify syscall name
	if !strings.Contains(got, "Open(") {
		t.Fatalf("expected 'Open(' in output, got %q", got)
	}
	// Verify args (sorted: flags before path)
	if !strings.Contains(got, "flags=2") {
		t.Fatalf("expected 'flags=2' in output, got %q", got)
	}
	if !strings.Contains(got, `path="/dev/null"`) {
		t.Fatalf("expected path=\"/dev/null\" in output, got %q", got)
	}
	// Verify result
	if !strings.Contains(got, "→ FD(3)") {
		t.Fatalf("expected '→ FD(3)' in output, got %q", got)
	}
	// Verify duration
	if !strings.Contains(got, "1ms") {
		t.Fatalf("expected '1ms' in output, got %q", got)
	}
	// Verify exact 4-space separator between result and duration (AC#2)
	if !strings.Contains(got, "→ FD(3)    1ms") {
		t.Fatalf("expected '→ FD(3)    1ms' (4-space separator), got %q", got)
	}
	// Verify no annotations
	if strings.Contains(got, "← slow op") {
		t.Fatalf("unexpected slow annotation in output: %q", got)
	}
	if strings.Contains(got, "← LLM call") {
		t.Fatalf("unexpected LLM annotation in output: %q", got)
	}
}

func TestFormatEvent_SlowOp(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 5 * time.Second,
		PID:       1,
		Syscall:   "Read",
		Args:      map[string]any{"fd": 3},
		Result:    1024,
		Err:       nil,
		Duration:  2 * time.Second,
	}

	// With color
	opts := Options{ColorEnabled: true, Verbose: false}
	got := FormatEvent(event, opts)
	if !strings.Contains(got, "← slow op") {
		t.Fatalf("expected slow op annotation with color, got %q", got)
	}
	if !strings.Contains(got, ansiGray) {
		t.Fatalf("expected gray ANSI code for slow annotation, got %q", got)
	}

	// Without color
	opts.ColorEnabled = false
	got = FormatEvent(event, opts)
	if !strings.Contains(got, "← slow op") {
		t.Fatalf("expected slow op annotation without color, got %q", got)
	}
	if strings.Contains(got, ansiGray) {
		t.Fatalf("unexpected gray ANSI code when color disabled, got %q", got)
	}
}

func TestFormatEvent_Error_WithColor(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 100 * time.Millisecond,
		PID:       1,
		Syscall:   "Write",
		Args:      map[string]any{"fd": 3},
		Result:    nil,
		Err:       errors.New("device not found"),
		Duration:  5 * time.Millisecond,
	}
	opts := Options{ColorEnabled: true, Verbose: false}

	got := FormatEvent(event, opts)

	// Verify ANSI red wrapping
	if !strings.HasPrefix(got, ansiRed) {
		t.Fatalf("expected line to start with red ANSI code, got %q", got)
	}
	if !strings.HasSuffix(got, ansiReset) {
		t.Fatalf("expected line to end with ANSI reset, got %q", got)
	}
	// Verify error result format
	if !strings.Contains(got, "err(device not found)") {
		t.Fatalf("expected 'err(device not found)' in output, got %q", got)
	}
	// No [ERR] prefix in color mode
	if strings.Contains(got, "[ERR]") {
		t.Fatalf("unexpected [ERR] prefix in color mode, got %q", got)
	}
}

func TestFormatEvent_Error_NoColor(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 100 * time.Millisecond,
		PID:       1,
		Syscall:   "Write",
		Args:      map[string]any{"fd": 3},
		Result:    nil,
		Err:       errors.New("device not found"),
		Duration:  5 * time.Millisecond,
	}
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// Verify [ERR] prefix
	if !strings.HasPrefix(got, "[ERR] ") {
		t.Fatalf("expected [ERR] prefix, got %q", got)
	}
	// No ANSI codes
	if strings.Contains(got, ansiRed) {
		t.Fatalf("unexpected red ANSI code when color disabled, got %q", got)
	}
	// Verify error result format
	if !strings.Contains(got, "err(device not found)") {
		t.Fatalf("expected 'err(device not found)' in output, got %q", got)
	}
}

func TestFormatEvent_LLMAnnotation(t *testing.T) {
	// Test with "path" key
	event := types.SyscallEvent{
		Timestamp: 2 * time.Millisecond,
		PID:       1,
		Syscall:   "Open",
		Args:      map[string]any{"path": "/dev/llm/claude", "flags": 2},
		Result:    "FD(3)",
		Err:       nil,
		Duration:  0,
	}
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)
	if !strings.Contains(got, "← LLM call") {
		t.Fatalf("expected LLM annotation for path key, got %q", got)
	}

	// Test with "tool" key
	event2 := types.SyscallEvent{
		Timestamp: 3 * time.Millisecond,
		PID:       1,
		Syscall:   "Exec",
		Args:      map[string]any{"tool": "/dev/llm/gpt"},
		Result:    nil,
		Err:       nil,
		Duration:  0,
	}
	got2 := FormatEvent(event2, opts)
	if !strings.Contains(got2, "← LLM call") {
		t.Fatalf("expected LLM annotation for tool key, got %q", got2)
	}
}

func TestFormatEvent_SlowAndError(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 5 * time.Second,
		PID:       1,
		Syscall:   "Read",
		Args:      map[string]any{"fd": 3, "path": "/dev/llm/claude"},
		Result:    nil,
		Err:       errors.New("timeout"),
		Duration:  30 * time.Second,
	}

	// No color: both annotations + [ERR] prefix
	opts := Options{ColorEnabled: false, Verbose: false}
	got := FormatEvent(event, opts)

	if !strings.HasPrefix(got, "[ERR] ") {
		t.Fatalf("expected [ERR] prefix, got %q", got)
	}
	if !strings.Contains(got, "← slow op") {
		t.Fatalf("expected slow annotation, got %q", got)
	}
	if !strings.Contains(got, "← LLM call") {
		t.Fatalf("expected LLM annotation, got %q", got)
	}

	// With color: red wrapping + gray slow annotation + LLM annotation
	opts.ColorEnabled = true
	got = FormatEvent(event, opts)

	if !strings.HasPrefix(got, ansiRed) {
		t.Fatalf("expected red ANSI prefix, got %q", got)
	}
	if !strings.Contains(got, "← slow op") {
		t.Fatalf("expected slow annotation with color, got %q", got)
	}
	if !strings.Contains(got, "← LLM call") {
		t.Fatalf("expected LLM annotation with color, got %q", got)
	}
}

func TestFormatArgs_Truncation(t *testing.T) {
	// Use a non-string type to test the %v truncation path.
	longValue := rawValue(strings.Repeat("x", 60)) // 60 chars, exceeds 50
	args := map[string]any{"data": longValue}

	got := formatArgs(args, false)

	// Should be truncated: first 47 chars + "..."
	expected := "data=" + strings.Repeat("x", 47) + "..."
	if got != expected {
		t.Fatalf("expected truncated output %q, got %q", expected, got)
	}

	// Also test string type truncation (uses %q with quotes).
	longStr := strings.Repeat("y", 60)
	argsStr := map[string]any{"data": longStr}
	gotStr := formatArgs(argsStr, false)
	// %q of 60 y's = "yyy...60" (62 chars), truncated to v[:49] + `..."`
	expectedStr := `data="` + strings.Repeat("y", 48) + `..."`
	if gotStr != expectedStr {
		t.Fatalf("expected truncated string output %q, got %q", expectedStr, gotStr)
	}
}

func TestFormatArgs_Verbose_NoTruncation(t *testing.T) {
	// Non-string type: no truncation in verbose mode.
	longValue := rawValue(strings.Repeat("x", 60))
	args := map[string]any{"data": longValue}

	got := formatArgs(args, true)

	expected := "data=" + strings.Repeat("x", 60)
	if got != expected {
		t.Fatalf("expected full output %q, got %q", expected, got)
	}

	// String type: no truncation in verbose mode (still quoted).
	longStr := strings.Repeat("y", 60)
	argsStr := map[string]any{"data": longStr}
	gotStr := formatArgs(argsStr, true)
	expectedStr := `data="` + strings.Repeat("y", 60) + `"`
	if gotStr != expectedStr {
		t.Fatalf("expected full string output %q, got %q", expectedStr, gotStr)
	}
}

// rawValue is a non-string type whose %v representation equals its underlying string.
// Used in tests to exercise the non-string truncation path in formatArgs.
type rawValue string

func TestFormatArgs_SortedKeys(t *testing.T) {
	args := map[string]any{
		"zebra":  1,
		"alpha":  2,
		"middle": 3,
	}

	got := formatArgs(args, false)

	// Keys should be alphabetically sorted
	expected := "alpha=2, middle=3, zebra=1"
	if got != expected {
		t.Fatalf("expected sorted args %q, got %q", expected, got)
	}
}

func TestAttach_ConsumesEvents(t *testing.T) {
	ch := make(chan types.SyscallEvent, 3)
	var buf bytes.Buffer
	opts := Options{ColorEnabled: false, Verbose: false}

	events := []types.SyscallEvent{
		{Timestamp: 0, PID: 1, Syscall: "Open", Args: map[string]any{"path": "/dev/null"}, Result: "FD(1)", Duration: time.Millisecond},
		{Timestamp: time.Millisecond, PID: 1, Syscall: "Read", Args: map[string]any{"fd": 1}, Result: 256, Duration: 2 * time.Millisecond},
		{Timestamp: 3 * time.Millisecond, PID: 1, Syscall: "Close", Args: map[string]any{"fd": 1}, Result: nil, Duration: 0},
	}

	for _, e := range events {
		ch <- e
	}
	close(ch)

	err := Attach(context.Background(), ch, &buf, opts)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), output)
	}

	// Verify each event produced output
	if !strings.Contains(lines[0], "Open") {
		t.Fatalf("expected 'Open' in first line, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Read") {
		t.Fatalf("expected 'Read' in second line, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "Close") {
		t.Fatalf("expected 'Close' in third line, got %q", lines[2])
	}
}

func TestAttach_ContextCancellation(t *testing.T) {
	ch := make(chan types.SyscallEvent, 1)
	var buf bytes.Buffer
	opts := Options{ColorEnabled: false, Verbose: false}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- Attach(ctx, ch, &buf, opts)
	}()

	// Cancel the context
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Attach did not return within 1s after context cancellation")
	}
}

func TestAttach_ChannelClose(t *testing.T) {
	ch := make(chan types.SyscallEvent, 1)
	var buf bytes.Buffer
	opts := Options{ColorEnabled: false, Verbose: false}

	done := make(chan error, 1)
	go func() {
		done <- Attach(context.Background(), ch, &buf, opts)
	}()

	// Close the channel
	close(ch)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error on channel close, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Attach did not return within 1s after channel close")
	}
}

func TestAttach_Latency(t *testing.T) {
	ch := make(chan types.SyscallEvent, 1)
	opts := Options{ColorEnabled: false, Verbose: false}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a channel-signaling writer to avoid data race on shared buffer.
	gotOutput := make(chan struct{}, 1)
	w := writerFunc(func(p []byte) (int, error) {
		select {
		case gotOutput <- struct{}{}:
		default:
		}
		return len(p), nil
	})

	done := make(chan struct{})
	go func() {
		Attach(ctx, ch, w, opts)
		close(done)
	}()

	// Send event and measure latency
	start := time.Now()
	ch <- types.SyscallEvent{
		Timestamp: 0,
		PID:       1,
		Syscall:   "Test",
		Result:    nil,
		Duration:  0,
	}

	select {
	case <-gotOutput:
		latency := time.Since(start)
		if latency > 10*time.Millisecond {
			t.Fatalf("latency %v exceeds 10ms threshold (NFR3 requires ≤500ms)", latency)
		}
		t.Logf("latency: %v", latency)
	case <-time.After(1 * time.Second):
		t.Fatal("output not received within 1s")
	}

	cancel()
	<-done
}

// writerFunc adapts a function into an io.Writer.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestDefaultOptions_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	opts := DefaultOptions()
	if opts.ColorEnabled {
		t.Fatal("expected ColorEnabled=false when NO_COLOR is set")
	}
	if opts.Verbose {
		t.Fatal("expected Verbose=false by default")
	}
}

func TestDefaultOptions_Default(t *testing.T) {
	// Ensure NO_COLOR is not set so ColorEnabled defaults to true.
	old, hadOld := os.LookupEnv("NO_COLOR")
	os.Unsetenv("NO_COLOR")
	if hadOld {
		t.Cleanup(func() { os.Setenv("NO_COLOR", old) })
	}

	opts := DefaultOptions()
	if !opts.ColorEnabled {
		t.Fatal("expected ColorEnabled=true when NO_COLOR is not set")
	}
	if opts.Verbose {
		t.Fatal("expected Verbose=false by default")
	}
}

func TestFormatEvent_ErrorSlowColor_NoGrayLeak(t *testing.T) {
	// Verify M1 fix: when error + slow + color enabled, the gray ANSI code
	// should NOT appear (entire line is red, gray would break the red wrapping).
	event := types.SyscallEvent{
		Timestamp: 5 * time.Second,
		PID:       1,
		Syscall:   "Read",
		Args:      map[string]any{"fd": 3, "path": "/dev/llm/claude"},
		Result:    nil,
		Err:       errors.New("timeout"),
		Duration:  30 * time.Second,
	}
	opts := Options{ColorEnabled: true, Verbose: false}
	got := FormatEvent(event, opts)

	// Entire line must be wrapped in red
	if !strings.HasPrefix(got, ansiRed) {
		t.Fatalf("expected red ANSI prefix, got %q", got)
	}
	if !strings.HasSuffix(got, ansiReset) {
		t.Fatalf("expected ANSI reset suffix, got %q", got)
	}
	// Gray must NOT appear (would break the red wrapping)
	if strings.Contains(got, ansiGray) {
		t.Fatalf("gray ANSI code found in error+slow line, would break red wrapping: %q", got)
	}
	// Slow and LLM annotations must still be present (as plain text within the red line)
	if !strings.Contains(got, "← slow op") {
		t.Fatalf("expected slow annotation, got %q", got)
	}
	if !strings.Contains(got, "← LLM call") {
		t.Fatalf("expected LLM annotation, got %q", got)
	}
}

func TestFormatEventJSON_BasicFormat(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 12 * time.Millisecond,
		PID:       1,
		Syscall:   "Open",
		Args:      map[string]any{"path": "/dev/null"},
		Result:    "FD(3)",
		Err:       nil,
		Duration:  5 * time.Millisecond,
	}

	got := FormatEventJSON(event)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("FormatEventJSON output is not valid JSON: %v\ngot: %s", err, got)
	}

	// Verify all required fields exist
	requiredFields := []string{"timestamp_ms", "pid", "syscall", "args", "result", "error", "duration_ms"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Fatalf("missing required field %q in JSON output: %s", field, got)
		}
	}

	// Verify field values
	if parsed["timestamp_ms"] != float64(12) {
		t.Fatalf("expected timestamp_ms=12, got %v", parsed["timestamp_ms"])
	}
	if parsed["pid"] != float64(1) {
		t.Fatalf("expected pid=1, got %v", parsed["pid"])
	}
	if parsed["syscall"] != "Open" {
		t.Fatalf("expected syscall=Open, got %v", parsed["syscall"])
	}
	if parsed["result"] != "FD(3)" {
		t.Fatalf("expected result=FD(3), got %v", parsed["result"])
	}
	if parsed["error"] != "" {
		t.Fatalf("expected error=\"\", got %v", parsed["error"])
	}
	if parsed["duration_ms"] != float64(5) {
		t.Fatalf("expected duration_ms=5, got %v", parsed["duration_ms"])
	}
}

func TestFormatEventJSON_Error(t *testing.T) {
	// With error
	event := types.SyscallEvent{
		Timestamp: 100 * time.Millisecond,
		PID:       2,
		Syscall:   "Write",
		Args:      map[string]any{"fd": 3},
		Result:    nil,
		Err:       errors.New("device not found"),
		Duration:  1 * time.Millisecond,
	}

	got := FormatEventJSON(event)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["error"] != "device not found" {
		t.Fatalf("expected error='device not found', got %v", parsed["error"])
	}

	// Without error — error field should be empty string
	event.Err = nil
	got = FormatEventJSON(event)

	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["error"] != "" {
		t.Fatalf("expected error=\"\" when no error, got %v", parsed["error"])
	}
}

func TestFormatEventJSON_SnakeCaseFields(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 1 * time.Second,
		PID:       1,
		Syscall:   "Read",
		Args:      map[string]any{"fd": 1},
		Result:    256,
		Err:       nil,
		Duration:  10 * time.Millisecond,
	}

	got := FormatEventJSON(event)

	// Verify snake_case field names exist in raw JSON
	snakeCaseFields := []string{
		`"timestamp_ms"`,
		`"pid"`,
		`"syscall"`,
		`"args"`,
		`"result"`,
		`"error"`,
		`"duration_ms"`,
	}
	for _, field := range snakeCaseFields {
		if !strings.Contains(got, field) {
			t.Fatalf("expected snake_case field %s in JSON output: %s", field, got)
		}
	}

	// Verify NO camelCase field names
	camelCaseFields := []string{
		`"timestampMs"`,
		`"durationMs"`,
		`"TimestampMs"`,
		`"DurationMs"`,
	}
	for _, field := range camelCaseFields {
		if strings.Contains(got, field) {
			t.Fatalf("unexpected camelCase field %s in JSON output: %s", field, got)
		}
	}
}

func TestAttach_JSONMode(t *testing.T) {
	ch := make(chan types.SyscallEvent, 2)
	var buf bytes.Buffer
	opts := Options{ColorEnabled: false, Verbose: false, JSON: true}

	events := []types.SyscallEvent{
		{Timestamp: 0, PID: 1, Syscall: "Open", Args: map[string]any{"path": "/dev/null"}, Result: "FD(1)", Duration: time.Millisecond},
		{Timestamp: time.Millisecond, PID: 1, Syscall: "Close", Args: map[string]any{"fd": 1}, Result: nil, Duration: 0},
	}

	for _, e := range events {
		ch <- e
	}
	close(ch)

	err := Attach(context.Background(), ch, &buf, opts)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d: %q", len(lines), output)
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\nline: %s", i, err, line)
		}
	}

	// Verify first line contains Open syscall
	var first map[string]any
	json.Unmarshal([]byte(lines[0]), &first)
	if first["syscall"] != "Open" {
		t.Fatalf("expected first line syscall=Open, got %v", first["syscall"])
	}

	// Verify second line contains Close syscall
	var second map[string]any
	json.Unmarshal([]byte(lines[1]), &second)
	if second["syscall"] != "Close" {
		t.Fatalf("expected second line syscall=Close, got %v", second["syscall"])
	}
}

// --- ConfigResolve formatting tests (Story 3.5) ---

func TestFormatEvent_ConfigResolve_Basic(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "openrouter",
			"provider_source": "agent",
			"model":           "hunter-alpha",
			"model_source":    "cli",
		},
		Result:   nil,
		Err:      nil,
		Duration: 1 * time.Millisecond,
	}
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// Verify timestamp
	if !strings.HasPrefix(got, "[  0.001s]") {
		t.Fatalf("expected timestamp prefix [  0.001s], got %q", got)
	}
	// Verify ConfigResolve syscall name
	if !strings.Contains(got, "ConfigResolve(") {
		t.Fatalf("expected 'ConfigResolve(' in output, got %q", got)
	}
	// Verify provider with source
	if !strings.Contains(got, "provider=openrouter [agent]") {
		t.Fatalf("expected 'provider=openrouter [agent]' in output, got %q", got)
	}
	// Verify model with source
	if !strings.Contains(got, "model=hunter-alpha [cli]") {
		t.Fatalf("expected 'model=hunter-alpha [cli]' in output, got %q", got)
	}
	// Verify duration
	if !strings.Contains(got, "1ms") {
		t.Fatalf("expected '1ms' in output, got %q", got)
	}
}

func TestFormatEvent_ConfigResolve_WithProjectDefault(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "openrouter",
			"provider_source": "agent",
			"model":           "",
			"model_source":    "driver",
			"project_default": "cursor",
		},
		Result:   nil,
		Err:      nil,
		Duration: 500 * time.Microsecond,
	}
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// Verify project_default is shown
	if !strings.Contains(got, "project_default=cursor") {
		t.Fatalf("expected 'project_default=cursor' in output, got %q", got)
	}
	// Verify empty model shows source
	if !strings.Contains(got, "model= [driver]") {
		t.Fatalf("expected 'model= [driver]' for empty model, got %q", got)
	}
}

func TestFormatEvent_ConfigResolve_WithColor(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "claude",
			"provider_source": "default",
			"model":           "",
			"model_source":    "driver",
		},
		Result:   nil,
		Err:      nil,
		Duration: 100 * time.Microsecond,
	}
	opts := Options{ColorEnabled: true, Verbose: false}

	got := FormatEvent(event, opts)

	// Verify gray ANSI codes are present for source labels
	if !strings.Contains(got, ansiGray) {
		t.Fatalf("expected gray ANSI code for source labels, got %q", got)
	}
	// Verify it still contains the content
	if !strings.Contains(got, "provider=claude") {
		t.Fatalf("expected 'provider=claude' in colored output, got %q", got)
	}
	if !strings.Contains(got, "[default]") {
		t.Fatalf("expected '[default]' in colored output, got %q", got)
	}
}

func TestFormatEvent_ConfigResolve_NoProjectDefault_WhenNotOverridden(t *testing.T) {
	event := types.SyscallEvent{
		Timestamp: 1 * time.Millisecond,
		PID:       1,
		Syscall:   "ConfigResolve",
		Args: map[string]any{
			"provider":        "claude",
			"provider_source": "default",
			"model":           "",
			"model_source":    "driver",
		},
		Result:   nil,
		Err:      nil,
		Duration: 100 * time.Microsecond,
	}
	opts := Options{ColorEnabled: false, Verbose: false}

	got := FormatEvent(event, opts)

	// project_default should NOT appear
	if strings.Contains(got, "project_default") {
		t.Fatalf("expected no 'project_default' in output when not overridden, got %q", got)
	}
}
