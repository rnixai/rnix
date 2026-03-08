package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agtest"
	"github.com/spf13/cobra"
)

func TestAgtestCommand_Registered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "agtest" {
			found = true
			break
		}
	}
	if !found {
		t.Error("agtest command not registered on rootCmd")
	}
}

func TestAgtestCommand_DryRun_ValidFile(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `version: "1.0"
name: "cli-test"
intent: "hello world"
agent:
  name: "test-agent"
`
	testFile := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	root := &cobra.Command{Use: "rnix"}
	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "JSON output")
	root.AddCommand(agtestCmd)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", testFile, "--dry-run"})

	oldJSON := flagJSON
	flagJSON = false
	defer func() { flagJSON = oldJSON }()

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "1 test case") {
		t.Errorf("expected '1 test case' in output, got: %s", out)
	}
	if !strings.Contains(out, "test-agent") {
		t.Errorf("expected 'test-agent' in output, got: %s", out)
	}
}

func TestAgtestCommand_DryRun_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `version: "1.0"
agent:
  model: "claude"
`
	testFile := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	root := &cobra.Command{Use: "rnix"}
	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "JSON output")
	root.AddCommand(agtestCmd)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", testFile, "--dry-run"})

	oldJSON := flagJSON
	oldExitCode := exitCode
	flagJSON = false
	exitCode = 0
	defer func() {
		flagJSON = oldJSON
		exitCode = oldExitCode
	}()

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "error") {
		t.Errorf("expected 'error' in output for invalid file, got: %s", out)
	}
}

// --- 16.3-CLI-001: Text report — all passed ---

func TestAgtest_TextReport_AllPassed(t *testing.T) {
	sr := &agtest.SuiteResult{
		Name: "test-suite",
		Cases: []agtest.CaseResult{
			{Name: "t1", Status: agtest.StatusPassed, Duration: 1200 * time.Millisecond},
			{Name: "t2", Status: agtest.StatusPassed, Duration: 800 * time.Millisecond},
		},
		Total: 2, Passed: 2, Duration: 2 * time.Second,
	}

	var buf bytes.Buffer
	oldJSON := flagJSON
	flagJSON = false
	defer func() { flagJSON = oldJSON }()

	_ = agtestTextReport(&buf, sr)

	out := buf.String()
	if !strings.Contains(out, "\u2713 t1") {
		t.Errorf("expected checkmark for t1, got: %s", out)
	}
	if !strings.Contains(out, "\u2713 t2") {
		t.Errorf("expected checkmark for t2, got: %s", out)
	}
	if !strings.Contains(out, "2 total") {
		t.Errorf("expected '2 total' in summary, got: %s", out)
	}
	if !strings.Contains(out, "2 passed") {
		t.Errorf("expected '2 passed' in summary, got: %s", out)
	}
}

// --- 16.3-CLI-002: Text report — with failures showing assertion details ---

func TestAgtest_TextReport_WithFailures(t *testing.T) {
	sr := &agtest.SuiteResult{
		Cases: []agtest.CaseResult{
			{Name: "pass-t", Status: agtest.StatusPassed, Duration: 500 * time.Millisecond},
			{
				Name: "fail-t", Status: agtest.StatusFailed, Duration: 1 * time.Second,
				Assertions: []agtest.AssertionResult{
					{Type: "output", Passed: false, Message: "output does not contain \"hello\"", Expected: "hello", Actual: "bye"},
				},
			},
		},
		Total: 2, Passed: 1, Failed: 1, Duration: 2 * time.Second,
	}

	oldExitCode := exitCode
	exitCode = 0
	defer func() { exitCode = oldExitCode }()

	var buf bytes.Buffer
	_ = agtestTextReport(&buf, sr)

	out := buf.String()
	if !strings.Contains(out, "\u2717 fail-t") {
		t.Errorf("expected cross mark for fail-t, got: %s", out)
	}
	if !strings.Contains(out, "output: output does not contain") {
		t.Errorf("expected assertion detail, got: %s", out)
	}
	if !strings.Contains(out, "expected: hello") {
		t.Errorf("expected 'expected: hello', got: %s", out)
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 for failures", exitCode)
	}
}

// --- 16.3-CLI-003: JSON report format ---

func TestAgtest_JSONReport(t *testing.T) {
	sr := &agtest.SuiteResult{
		Cases: []agtest.CaseResult{
			{Name: "j1", Status: agtest.StatusPassed, Duration: 100 * time.Millisecond},
			{
				Name: "j2", Status: agtest.StatusFailed, Duration: 200 * time.Millisecond,
				Assertions: []agtest.AssertionResult{
					{Type: "syscalls", Passed: false, Message: "missing Write"},
				},
			},
		},
		Total: 2, Passed: 1, Failed: 1, Duration: 300 * time.Millisecond,
	}

	var buf bytes.Buffer
	_ = agtestJSONReport(&buf, sr)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.OK {
		t.Error("OK should be false when there are failures")
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("Data should be a map")
	}
	if total, ok := dataMap["total"].(float64); !ok || int(total) != 2 {
		t.Errorf("total = %v, want 2", dataMap["total"])
	}
	cases, ok := dataMap["cases"].([]any)
	if !ok {
		t.Fatal("cases should be an array")
	}
	if len(cases) != 2 {
		t.Errorf("cases len = %d, want 2", len(cases))
	}
}

// --- 16.3-CLI-004: --timeout flag ---

func TestAgtest_TimeoutFlag(t *testing.T) {
	cmd := agtestCmd
	f := cmd.Flags().Lookup("timeout")
	if f == nil {
		t.Fatal("--timeout flag not found")
	}
	if f.DefValue != "60000" {
		t.Errorf("default timeout = %q, want %q", f.DefValue, "60000")
	}
}

// --- 16.3-CLI-005: No daemon error ---

func TestAgtest_NoDaemon_Error(t *testing.T) {
	sr := &agtest.SuiteResult{
		Cases: []agtest.CaseResult{
			{Name: "err-test", Status: agtest.StatusError, Error: "daemon not available: dial unix /tmp/rnix.sock: connect: connection refused", Duration: 0},
		},
		Total: 1, Errors: 1, Duration: 0,
	}

	oldExitCode := exitCode
	exitCode = 0
	defer func() { exitCode = oldExitCode }()

	var buf bytes.Buffer
	_ = agtestTextReport(&buf, sr)

	out := buf.String()
	if !strings.Contains(out, "daemon not available") {
		t.Errorf("expected daemon error in output, got: %s", out)
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 for errors", exitCode)
	}

	// Also verify JSON report handles daemon errors
	exitCode = 0
	var jsonBuf bytes.Buffer
	_ = agtestJSONReport(&jsonBuf, sr)

	var resp JSONResponse
	if err := json.Unmarshal(jsonBuf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.OK {
		t.Error("OK should be false when there are errors")
	}
}
