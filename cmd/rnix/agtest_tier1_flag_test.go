package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// =============================================================================
// Story 68.3 Task 1 — `rnix agtest --tier1` wires the CLI to
// agtest.ValidateTier1 (68-2 裁决 2's reserved extension point). Not bound by
// default: a plain `rnix agtest` invocation never sees this check, so
// pre-existing suites/fixtures are unaffected (see the "no flag" case below).
// =============================================================================

// tier1ViolatingSuiteYAML passes the base agtest.Validate (has version/intent/
// agent.name and a non-empty quality.criteria) but violates two ValidateTier1
// rules at once: agent.provider isn't "replay", and a quality (LLM-judge)
// assertion is present — exactly the "含 QualityAssert" fixture the story
// Task 1 subtask calls for.
const tier1ViolatingSuiteYAML = `version: "1.0"
name: "tier1-violation"
intent: "ask something"
agent:
  name: "test-agent"
assert:
  quality:
    criteria: "is it good"
`

// tier1CompliantSuiteYAML satisfies every ValidateTier1 rule: replay
// provider, a non-empty output assertion, and no absolute-path strings.
const tier1CompliantSuiteYAML = `version: "1.0"
name: "tier1-ok"
intent: "run echo"
agent:
  provider: "replay"
  model: "scripts/ok.responses.yaml"
assert:
  output:
    contains: ["hi"]
`

// setupAgtestTier1CLI writes yamlContent to a temp file and returns a fresh
// root command with agtestCmd registered, ready for SetArgs+Execute — mirrors
// the existing TestAgtestCommand_DryRun_* setup in agtest_test.go.
func setupAgtestTier1CLI(t *testing.T, yamlContent string) (root *cobra.Command, testFile string) {
	t.Helper()
	dir := t.TempDir()
	testFile = filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	root = &cobra.Command{Use: "rnix"}
	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "JSON output")
	root.AddCommand(agtestCmd)
	return root, testFile
}

// resetAgtestFlags saves the current package-level agtest flag/exitCode state
// and returns a restore func — these are cobra-bound package vars (see
// agtest.go init()) that persist across Execute() calls within a test binary,
// so every test that flips --tier1/--dry-run must restore them.
func resetAgtestFlags(t *testing.T) {
	t.Helper()
	oldJSON, oldExitCode := flagJSON, exitCode
	oldDryRun, oldTier1 := flagAgtestDryRun, flagAgtestTier1
	flagJSON, exitCode = false, 0
	flagAgtestDryRun, flagAgtestTier1 = false, false
	t.Cleanup(func() {
		flagJSON, exitCode = oldJSON, oldExitCode
		flagAgtestDryRun, flagAgtestTier1 = oldDryRun, oldTier1
	})
}

func TestAgtestTier1Flag_Registered(t *testing.T) {
	f := agtestCmd.Flags().Lookup("tier1")
	if f == nil {
		t.Fatal("--tier1 flag not found")
	}
	if f.DefValue != "false" {
		t.Errorf("default tier1 = %q, want %q", f.DefValue, "false")
	}
}

// tier1 违规 fixture（含 QualityAssert）+ --tier1 → 报错 (dry-run mode).
func TestAgtestCommand_Tier1_DryRun_RejectsViolation(t *testing.T) {
	resetAgtestFlags(t)
	root, testFile := setupAgtestTier1CLI(t, tier1ViolatingSuiteYAML)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", testFile, "--dry-run", "--tier1"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "error") {
		t.Errorf("expected tier1 violation error in output, got: %s", out)
	}
	if !strings.Contains(out, "replay") {
		t.Errorf("expected provider-must-be-replay message in output, got: %s", out)
	}
	if !strings.Contains(out, "quality") {
		t.Errorf("expected quality-forbidden message in output, got: %s", out)
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 for a Tier1 violation", exitCode)
	}
}

// A compliant suite must still pass --dry-run --tier1 (flag must not reject
// valid input) and produce the normal dry-run summary output.
func TestAgtestCommand_Tier1_DryRun_AllowsCompliantSuite(t *testing.T) {
	resetAgtestFlags(t)
	root, testFile := setupAgtestTier1CLI(t, tier1CompliantSuiteYAML)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", testFile, "--dry-run", "--tier1"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "[agtest] error") {
		t.Errorf("compliant suite should not be rejected by --tier1, got: %s", out)
	}
	if !strings.Contains(out, "1 test case") {
		t.Errorf("expected normal dry-run summary, got: %s", out)
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0 for a compliant suite", exitCode)
	}
}

// 不带 flag → 照常通过：the exact same violating suite must parse and dry-run
// cleanly when --tier1 is absent — a plain `rnix agtest` stays tier1-blind.
func TestAgtestCommand_NoTier1Flag_AllowsViolatingSuite(t *testing.T) {
	resetAgtestFlags(t)
	root, testFile := setupAgtestTier1CLI(t, tier1ViolatingSuiteYAML)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", testFile, "--dry-run"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "[agtest] error") {
		t.Errorf("without --tier1 the command must not apply Tier1 discipline, got: %s", out)
	}
	if !strings.Contains(out, "1 test case") {
		t.Errorf("expected normal dry-run summary, got: %s", out)
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0 when --tier1 is not passed", exitCode)
	}
}
