package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agtest"
)

// =============================================================================
// E2E — Story 68.3 Task 7: 失败转用例回路闭环 ("每次 agent 出丑，测试集就增长").
//
// The unit tests in agtest_import_test.go stop at "the generated case/script is
// syntactically valid agtest YAML and is (correctly) rejected by ValidateTier1
// until reviewed". They never exercise the OTHER half of the loop's contract:
// that `rnix agtest import`'s generated response script actually loads under
// the replay driver's STRICT parser (drivers/llm loadReplayScript, yaml.Strict)
// and, once a human fills in the suggested assertions, reproduces the recorded
// behavior when really replayed. That cross-module producer→consumer contract
// (cmd/rnix import  →  drivers/llm replay) is exactly what Story 68.3 Task 7's
// *manual* "整环反证" checked by hand. This file automates it so a schema or
// behavior regression is caught by `make test` / CI instead of the next manual
// run.
//
// The strict-parse contract is verified authentically rather than by mirroring
// the (unexported) replay schema struct here: setupAgtestE2E mounts a real
// *llm.ReplayDriver, which strict-loads the import-generated script when the
// process runs. If import ever emits a field the driver rejects, result.Error
// is a parse failure and these tests fail loudly — no drift-prone schema copy.
//
// Setup reuses setupAgtestE2E / agtestE2EExecutor (atdd_68_2_agtest_e2e_test.go)
// and the import CLI helpers (agtest_import_test.go). setupAgtestE2E t.Chdir's
// into an empty temp dir; every path used here is absolute so that is harmless.
// =============================================================================

// importFixtureCase drives the real `rnix agtest import <fixtureUUID> --out
// outDir` CLI against the static testdata recording (the same shape the Story
// 68.3 Task 2 tests use) and returns the path of the generated case skeleton.
func importFixtureCase(t *testing.T, outDir string) (casePath string) {
	t.Helper()
	resetAgtestImportFlags(t)
	t.Setenv("RNIX_DATA_DIR", importTestdataDir(t))

	root := newAgtestRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", "import", importFixtureUUID, "--out", outDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("import command errored: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("import exitCode = %d, want 0; output:\n%s", exitCode, buf.String())
	}
	return filepath.Join(outDir, "import-"+shortUUID(importFixtureUUID)+".yaml")
}

// TestE2E_68_3_ImportedCase_RunsGreenAfterReview is the centerpiece: the full
// failure-to-case loop, green half. It records nothing new — it takes the
// existing static recording, imports it into a skeleton, performs the human
// review step (filling in the assertions the tool suggested as comments), and
// runs the now-complete case against a real replay daemon + real shell. Passing
// proves both that the generated script strict-loads under the replay driver
// AND that replaying it reproduces the recorded behavior.
func TestE2E_68_3_ImportedCase_RunsGreenAfterReview(t *testing.T) {
	outDir := t.TempDir()
	casePath := importFixtureCase(t, outDir)

	// Parse the freshly generated skeleton exactly as a maintainer would after
	// `rnix agtest import`. Per 裁决 5 it must have NO live assertions yet —
	// only commented suggestions — so it stays out of the suite until reviewed.
	suite, err := agtest.ParseFile(casePath)
	if err != nil {
		t.Fatalf("generated case must parse: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("generated case has %d tests, want 1", len(suite.Tests))
	}
	tc := &suite.Tests[0]
	if tc.Assert != nil {
		t.Fatalf("freshly imported skeleton must have no live assertions (only commented suggestions), got %+v", tc.Assert)
	}
	if tc.Agent.Provider != "replay" {
		t.Fatalf("Agent.Provider = %q, want replay", tc.Agent.Provider)
	}

	// --- human review step: promote the tool's commented suggestions to real
	// assertions. The recorded process echoed "hi-from-import" via shell and
	// completed with result "hi-from-import produced the greeting".
	tc.Assert = &agtest.AssertConfig{
		Output:   &agtest.OutputAssert{Contains: []string{"hi-from-import produced the greeting"}},
		Syscalls: &agtest.SyscallAssert{Includes: []string{"ReasonStep"}},
	}

	// --- run the now-complete case against a real replay daemon + real shell.
	sockPath := setupAgtestE2E(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := agtestE2EExecutor(sockPath).Execute(ctx, tc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// A non-empty result.Error here is most likely the replay driver rejecting
	// the import-generated script under strict parsing — the exact producer→
	// consumer schema break this test exists to catch.
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty (import-generated script must strict-load and replay cleanly)", result.Error)
	}
	if !strings.Contains(result.Output, "hi-from-import produced the greeting") {
		t.Errorf("Output = %q, want the recorded Complete result reproduced by replay", result.Output)
	}
	if len(result.Syscalls) == 0 {
		t.Fatal("Syscalls empty — a completed replay run recorded no events")
	}
	for _, r := range agtest.EvalOutput(result.Output, tc.Assert.Output) {
		if !r.Passed {
			t.Errorf("output assertion failed against real replayed data: %s", r.Message)
		}
	}
	for _, r := range agtest.EvalSyscalls(result.Syscalls, tc.Assert.Syscalls) {
		if !r.Passed {
			t.Errorf("syscall assertion failed against real replayed data: %s (syscalls=%v)", r.Message, result.Syscalls)
		}
	}
}

// TestE2E_68_3_ImportedCase_RegressionHasTeeth is the red half of Task 7's 整环
// 反证: a regression suite is only worth anything if a *changed behavior* turns
// it red. We corrupt one recorded behavior in the generated script (the Complete
// result the process replays), then assert the ORIGINAL recorded contract — which
// the mutated behavior no longer satisfies — and require the case to fail. If it
// still passed, the imported case would have no teeth.
func TestE2E_68_3_ImportedCase_RegressionHasTeeth(t *testing.T) {
	outDir := t.TempDir()
	casePath := importFixtureCase(t, outDir)

	scriptPath := filepath.Join(outDir, "scripts", "import-"+shortUUID(importFixtureUUID)+".responses.yaml")
	orig, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read generated script: %v", err)
	}
	// Change only a string value, not a field name, so the script still
	// strict-loads — the run must succeed and merely produce different output.
	mutated := strings.Replace(string(orig), "hi-from-import produced the greeting", "MUTATED-behavior-diverged", 1)
	if mutated == string(orig) {
		t.Fatal("expected to find the recorded Complete result in the generated script to mutate; fixture or import output changed?")
	}
	if err := os.WriteFile(scriptPath, []byte(mutated), 0o644); err != nil {
		t.Fatalf("rewrite mutated script: %v", err)
	}

	suite, err := agtest.ParseFile(casePath)
	if err != nil {
		t.Fatalf("generated case must parse: %v", err)
	}
	tc := &suite.Tests[0]
	// Assert the ORIGINAL recorded contract, which the mutated script violates.
	tc.Assert = &agtest.AssertConfig{
		Output: &agtest.OutputAssert{Contains: []string{"hi-from-import produced the greeting"}},
	}

	sockPath := setupAgtestE2E(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := agtestE2EExecutor(sockPath).Execute(ctx, tc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q — mutating a string value must still strict-load and run, want empty", result.Error)
	}

	// The output assertion must now FAIL — that failing signal is the whole
	// point: a changed behavior is caught by the imported regression case.
	failed := false
	for _, r := range agtest.EvalOutput(result.Output, tc.Assert.Output) {
		if !r.Passed {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("expected the output assertion to FAIL after mutating the recorded behavior, but it passed (Output=%q) — the imported regression case would have no teeth", result.Output)
	}
}
