package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
