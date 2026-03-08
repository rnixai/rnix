package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
)

func setupTraceTestDir(t *testing.T, traces map[types.TraceID][]*debug.Span) string {
	t.Helper()
	dir := t.TempDir()
	baseDir := dir + "/.rnix/traces"
	writer := debug.NewSpanWriter(baseDir)
	for _, spans := range traces {
		for _, s := range spans {
			if err := writer.WriteSpan(s); err != nil {
				t.Fatalf("WriteSpan: %v", err)
			}
		}
	}
	return dir
}

func TestTraceCmd_Registered(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(traceCmd)

	found, _, err := root.Find([]string{"trace"})
	if err != nil {
		t.Fatalf("failed to find 'trace' command: %v", err)
	}
	if found == nil {
		t.Fatal("expected 'trace' command to exist")
	}
}

func TestTraceCmd_NoArgs_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = false
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace"})
	exitCode = 0
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "No traces found") {
		t.Errorf("expected 'No traces found' message, got:\n%s", output)
	}
}

func TestTraceCmd_NoArgs_ListTraces(t *testing.T) {
	bt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	traces := map[types.TraceID][]*debug.Span{
		"trace-aaa": {
			{TraceID: "trace-aaa", SpanID: "s1", PID: 1, Name: "root-a",
				StartTime: bt, EndTime: bt.Add(5 * time.Second), Duration: 5 * time.Second, Status: debug.SpanOK},
		},
	}
	dir := setupTraceTestDir(t, traces)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = false
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace"})
	exitCode = 0
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "trace-aaa") {
		t.Errorf("expected trace ID in list output, got:\n%s", output)
	}
	if !strings.Contains(output, "root-a") {
		t.Errorf("expected root span name in list output, got:\n%s", output)
	}
}

func TestTraceCmd_ValidTraceID_TreeOutput(t *testing.T) {
	bt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	traces := map[types.TraceID][]*debug.Span{
		"trace-bbb": {
			{TraceID: "trace-bbb", SpanID: "s1", PID: 1, Name: "orchestrator",
				StartTime: bt, EndTime: bt.Add(10 * time.Second), Duration: 10 * time.Second, TokensUsed: 800, Status: debug.SpanOK},
			{TraceID: "trace-bbb", SpanID: "s2", ParentSpanID: "s1", PID: 2, Name: "analyst",
				StartTime: bt.Add(time.Second), EndTime: bt.Add(4 * time.Second), Duration: 3 * time.Second, TokensUsed: 500, Status: debug.SpanOK},
		},
	}
	dir := setupTraceTestDir(t, traces)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = false
	flagVerbose = false
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace", "trace-bbb"})
	exitCode = 0
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "trace-bbb") {
		t.Errorf("expected trace ID in output, got:\n%s", output)
	}
	if !strings.Contains(output, "orchestrator") {
		t.Errorf("expected 'orchestrator' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "analyst") {
		t.Errorf("expected 'analyst' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "2 spans") {
		t.Errorf("expected '2 spans' in output, got:\n%s", output)
	}
}

func TestTraceCmd_InvalidTraceID_Error(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = false
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace", "nonexistent-trace-id"})
	exitCode = 0
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "not found") {
		t.Errorf("expected 'not found' error, got:\n%s", output)
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestTraceCmd_ValidTraceID_JSON(t *testing.T) {
	bt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	traces := map[types.TraceID][]*debug.Span{
		"trace-ccc": {
			{TraceID: "trace-ccc", SpanID: "s1", PID: 1, Name: "root",
				StartTime: bt, EndTime: bt.Add(5 * time.Second), Duration: 5 * time.Second, TokensUsed: 300, Status: debug.SpanOK},
		},
	}
	dir := setupTraceTestDir(t, traces)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = true
	defer func() { flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace", "trace-ccc"})
	exitCode = 0
	_ = cmd.Execute()

	output := strings.TrimSpace(buf.String())
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v\noutput: %s", err, output)
	}
	if !resp.OK {
		t.Errorf("expected OK=true, got false")
	}
	if resp.Data == nil {
		t.Error("expected non-nil Data")
	}
}

// --- Blame subcommand tests ---

func TestBlameCmd_ValidTraceID(t *testing.T) {
	bt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	traces := map[types.TraceID][]*debug.Span{
		"trace-blame1": {
			{TraceID: "trace-blame1", SpanID: "s1", PID: 1, Name: "orchestrator",
				StartTime: bt, EndTime: bt.Add(10 * time.Second), Duration: 10 * time.Second, TokensUsed: 800, Status: debug.SpanOK},
			{TraceID: "trace-blame1", SpanID: "s2", ParentSpanID: "s1", PID: 2, Name: "analyst",
				StartTime: bt.Add(time.Second), EndTime: bt.Add(4 * time.Second), Duration: 3 * time.Second, TokensUsed: 500, Status: debug.SpanOK},
			{TraceID: "trace-blame1", SpanID: "s3", ParentSpanID: "s1", PID: 3, Name: "reviewer",
				StartTime: bt.Add(2 * time.Second), EndTime: bt.Add(6 * time.Second), Duration: 4 * time.Second, TokensUsed: 200, Status: debug.SpanERROR},
		},
	}
	dir := setupTraceTestDir(t, traces)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = false
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace", "blame", "trace-blame1"})
	exitCode = 0
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "Critical Path") {
		t.Errorf("expected 'Critical Path' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Duration Hotspots") {
		t.Errorf("expected 'Duration Hotspots' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Error Chains") {
		t.Errorf("expected 'Error Chains' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "[ROOT CAUSE]") {
		t.Errorf("expected '[ROOT CAUSE]' in output, got:\n%s", output)
	}
}

func TestBlameCmd_InvalidTraceID(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = false
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace", "blame", "nonexistent-id"})
	exitCode = 0
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "not found") {
		t.Errorf("expected 'not found' error, got:\n%s", output)
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestBlameCmd_JSON(t *testing.T) {
	bt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	traces := map[types.TraceID][]*debug.Span{
		"trace-blame2": {
			{TraceID: "trace-blame2", SpanID: "s1", PID: 1, Name: "root",
				StartTime: bt, EndTime: bt.Add(5 * time.Second), Duration: 5 * time.Second, TokensUsed: 300, Status: debug.SpanOK},
		},
	}
	dir := setupTraceTestDir(t, traces)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = true
	defer func() { flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace", "blame", "trace-blame2"})
	exitCode = 0
	_ = cmd.Execute()

	output := strings.TrimSpace(buf.String())
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v\noutput: %s", err, output)
	}
	if !resp.OK {
		t.Errorf("expected OK=true, got false")
	}
	if resp.Data == nil {
		t.Error("expected non-nil Data")
	}
}

func TestBlameCmd_AllOK(t *testing.T) {
	bt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	traces := map[types.TraceID][]*debug.Span{
		"trace-blame3": {
			{TraceID: "trace-blame3", SpanID: "s1", PID: 1, Name: "root",
				StartTime: bt, EndTime: bt.Add(5 * time.Second), Duration: 5 * time.Second, TokensUsed: 500, Status: debug.SpanOK},
			{TraceID: "trace-blame3", SpanID: "s2", ParentSpanID: "s1", PID: 2, Name: "child",
				StartTime: bt.Add(time.Second), EndTime: bt.Add(3 * time.Second), Duration: 2 * time.Second, TokensUsed: 300, Status: debug.SpanOK},
		},
	}
	dir := setupTraceTestDir(t, traces)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = false
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace", "blame", "trace-blame3"})
	exitCode = 0
	_ = cmd.Execute()

	output := buf.String()
	if strings.Contains(output, "Error Chains") {
		t.Errorf("expected NO 'Error Chains' for all-OK trace, got:\n%s", output)
	}
	if !strings.Contains(output, "Duration Hotspots") {
		t.Errorf("expected 'Duration Hotspots' even for all-OK trace, got:\n%s", output)
	}
}

func TestTraceCmd_NoArgs_JSON(t *testing.T) {
	bt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	traces := map[types.TraceID][]*debug.Span{
		"trace-ddd": {
			{TraceID: "trace-ddd", SpanID: "s1", PID: 1, Name: "root-d",
				StartTime: bt, EndTime: bt.Add(3 * time.Second), Duration: 3 * time.Second, Status: debug.SpanOK},
		},
	}
	dir := setupTraceTestDir(t, traces)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	flagJSON = true
	defer func() { flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(traceCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"trace"})
	exitCode = 0
	_ = cmd.Execute()

	output := strings.TrimSpace(buf.String())
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}
	if !resp.OK {
		t.Errorf("expected OK=true")
	}
}
