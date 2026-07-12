package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/agtest"
	"github.com/rnixai/rnix/internal/types"
	"github.com/spf13/cobra"
)

// =============================================================================
// Story 68.3 Task 2 — `rnix agtest import <uuid>`.
//
// Two fixture strategies, matching the story's own split:
//   - cmd/rnix/testdata/agtest_import/ holds a STATIC, realistic recorded
//     process (steps.jsonl + proc-info.json + events.jsonl under the real
//     <dataDir>/projects/<proj>/steps/<uuid>/ layout) for end-to-end CLI
//     tests, plus a second uuid with ONLY steps.jsonl for the
//     events/proc-info-missing degradation path.
//   - Small ad-hoc t.TempDir() fixtures for the combinatorial uuid-matching
//     tiers and the StepRecord→response mapping table, where dozens of tiny
//     precise variations are far more maintainable as Go literals than as a
//     pile of one-off JSONL files.
// =============================================================================

const importFixtureUUID = "d34db33f-c0de-4b0b-8a11-0123456789ab"
const importFixtureUUIDNoMeta = "eeeeeeee-0000-4000-8000-000000000000"

func importTestdataDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("testdata/agtest_import")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func resetAgtestImportFlags(t *testing.T) {
	t.Helper()
	oldJSON, oldExitCode, oldOut := flagJSON, exitCode, flagAgtestImportOut
	flagJSON, exitCode = false, 0
	flagAgtestImportOut = "tests/agtest/imported"
	t.Cleanup(func() {
		flagJSON, exitCode, flagAgtestImportOut = oldJSON, oldExitCode, oldOut
	})
}

func newAgtestRoot() *cobra.Command {
	root := &cobra.Command{Use: "rnix"}
	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "JSON output")
	root.AddCommand(agtestCmd)
	return root
}

// -----------------------------------------------------------------------
// UUID resolution — three-level lookup (exact / suffix / prefix) + ambiguity
// + not-found + too-short, all against a shared ad-hoc data dir of empty
// uuid directories (resolveImportUUID never reads their contents).
// -----------------------------------------------------------------------

func makeUUIDDir(t *testing.T, stepsDir, uuid string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(stepsDir, uuid), 0o755); err != nil {
		t.Fatal(err)
	}
}

func setupUUIDMatchFixture(t *testing.T) (stepsDir string) {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("RNIX_DATA_DIR", dataDir)
	stepsDir = filepath.Join(dataDir, "projects", "proj", "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Exact-match target.
	makeUUIDDir(t, stepsDir, "11111111-1111-4111-8111-111111111111")
	// Suffix-ambiguous pair — both end "999999".
	makeUUIDDir(t, stepsDir, "22222222-2222-4222-8222-222222999999")
	makeUUIDDir(t, stepsDir, "33333333-3333-4333-8333-333333999999")
	// Prefix-ambiguous pair — both start "888888".
	makeUUIDDir(t, stepsDir, "888888aa-4444-4444-8444-444444444444")
	makeUUIDDir(t, stepsDir, "888888bb-5555-4555-8555-555555555555")
	// Unique suffix-match target — ends "666666", shared by no one else.
	makeUUIDDir(t, stepsDir, "66666666-6666-4666-8666-666666666666")
	// Unique prefix-match target — starts "aabbcc", shared by no one else.
	makeUUIDDir(t, stepsDir, "aabbcc77-7777-4777-8777-777777777777")

	return stepsDir
}

func TestResolveImportUUID(t *testing.T) {
	setupUUIDMatchFixture(t)

	t.Run("exact match wins immediately", func(t *testing.T) {
		full := "11111111-1111-4111-8111-111111111111"
		m, err := resolveImportUUID(full)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.uuid != full {
			t.Errorf("uuid = %q, want %q", m.uuid, full)
		}
	})

	t.Run("suffix match, unique", func(t *testing.T) {
		m, err := resolveImportUUID("666666")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.uuid != "66666666-6666-4666-8666-666666666666" {
			t.Errorf("uuid = %q, want the unique 666666-suffix uuid", m.uuid)
		}
	})

	t.Run("suffix match, ambiguous", func(t *testing.T) {
		_, err := resolveImportUUID("999999")
		if err == nil {
			t.Fatal("expected ambiguous-suffix error")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("error should say ambiguous, got: %v", err)
		}
		if !strings.Contains(err.Error(), "222222999999") || !strings.Contains(err.Error(), "333333999999") {
			t.Errorf("error should list both candidate uuids, got: %v", err)
		}
	})

	t.Run("prefix match, unique", func(t *testing.T) {
		m, err := resolveImportUUID("aabbcc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.uuid != "aabbcc77-7777-4777-8777-777777777777" {
			t.Errorf("uuid = %q, want the unique aabbcc-prefix uuid", m.uuid)
		}
	})

	t.Run("prefix match, ambiguous", func(t *testing.T) {
		_, err := resolveImportUUID("888888")
		if err == nil {
			t.Fatal("expected ambiguous-prefix error")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("error should say ambiguous, got: %v", err)
		}
	})

	t.Run("no match at any level", func(t *testing.T) {
		_, err := resolveImportUUID("zzzzzz")
		if err == nil {
			t.Fatal("expected not-found error")
		}
		if !strings.Contains(err.Error(), "no process found") {
			t.Errorf("error should say no process found, got: %v", err)
		}
		if !strings.Contains(err.Error(), "rnix ps -a --uuid") {
			t.Errorf("error should hint at `rnix ps -a --uuid`, got: %v", err)
		}
	})

	t.Run("too short is rejected before scanning suffix/prefix", func(t *testing.T) {
		_, err := resolveImportUUID("zz")
		if err == nil {
			t.Fatal("expected too-short error")
		}
		if !strings.Contains(err.Error(), "6-character minimum") {
			t.Errorf("error should mention the 6-character minimum, got: %v", err)
		}
	})
}

// -----------------------------------------------------------------------
// buildImportResponses — the StepRecord -> response mapping table.
// -----------------------------------------------------------------------

func TestBuildImportResponses(t *testing.T) {
	t.Run("tool_call step with valid JSON input", func(t *testing.T) {
		steps := []types.StepRecord{{
			Step: 1, Action: "tool_call",
			ToolCalls: []types.ToolCallRecord{{Name: "Bash", Input: `{"command":"echo hi"}`}},
		}}
		responses, warnings := buildImportResponses(steps, "")
		if len(responses) != 1 {
			t.Fatalf("responses = %d, want 1", len(responses))
		}
		if len(responses[0].ToolCalls) != 1 || responses[0].ToolCalls[0].Name != "Bash" {
			t.Fatalf("tool_calls = %+v", responses[0].ToolCalls)
		}
		m, ok := responses[0].ToolCalls[0].Input.(map[string]any)
		if !ok || m["command"] != "echo hi" {
			t.Errorf("Input = %#v, want a parsed map with command=echo hi", responses[0].ToolCalls[0].Input)
		}
		// A single Bash-only step is also the LAST response, so the
		// trailing-not-Complete warning is expected here — this subtest is
		// only about input-parsing, so just assert no *parsing* warning fired.
		for _, w := range warnings {
			if strings.Contains(w, "not valid JSON") {
				t.Errorf("unexpected input-parsing warning for valid JSON input: %v", warnings)
			}
		}
	})

	t.Run("tool_call step with malformed JSON input falls back to raw string + warning", func(t *testing.T) {
		steps := []types.StepRecord{{
			Step: 1, Action: "tool_call",
			ToolCalls: []types.ToolCallRecord{{Name: "Weird", Input: "not-json-{"}},
		}}
		responses, warnings := buildImportResponses(steps, "")
		if responses[0].ToolCalls[0].Input != "not-json-{" {
			t.Errorf("Input = %#v, want the raw string fallback", responses[0].ToolCalls[0].Input)
		}
		// This single step is also the script's last response (not Complete),
		// so a second, unrelated trailing warning is expected too — just
		// check the input-parsing warning is present among them.
		found := false
		for _, w := range warnings {
			if strings.Contains(w, "not valid JSON") {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one mentioning invalid JSON", warnings)
		}
	})

	t.Run("text step uses RawResponse", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 1, Action: "text", RawResponse: "the final answer", Summary: "fallback"}}
		responses, _ := buildImportResponses(steps, "")
		if responses[0].Content != "the final answer" {
			t.Errorf("Content = %q, want RawResponse", responses[0].Content)
		}
	})

	t.Run("text step falls back to Summary when RawResponse is empty", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 1, Action: "text", Summary: "summary-only answer"}}
		responses, _ := buildImportResponses(steps, "")
		if responses[0].Content != "summary-only answer" {
			t.Errorf("Content = %q, want Summary fallback", responses[0].Content)
		}
	})

	t.Run("complete step uses proc-info result", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 2, Action: "complete", Summary: "unused"}}
		responses, _ := buildImportResponses(steps, "the proc-info result")
		if len(responses[0].ToolCalls) != 1 || responses[0].ToolCalls[0].Name != "Complete" {
			t.Fatalf("tool_calls = %+v, want a single Complete call", responses[0].ToolCalls)
		}
		input := responses[0].ToolCalls[0].Input.(map[string]any)
		if input["result"] != "the proc-info result" {
			t.Errorf("result = %v, want the proc-info result", input["result"])
		}
	})

	t.Run("complete step falls back to Summary when proc-info result is empty", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 2, Action: "complete", Summary: "fallback-result"}}
		responses, _ := buildImportResponses(steps, "")
		input := responses[0].ToolCalls[0].Input.(map[string]any)
		if input["result"] != "fallback-result" {
			t.Errorf("result = %v, want Summary fallback", input["result"])
		}
	})

	t.Run("other meta action (replan) reconstructed with a warning", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 5, Action: "replan", ToolInput: `{"reason":"try again"}`}}
		responses, warnings := buildImportResponses(steps, "")
		if len(responses[0].ToolCalls) != 1 || responses[0].ToolCalls[0].Name != "Replan" {
			t.Fatalf("tool_calls = %+v, want a single Replan call", responses[0].ToolCalls)
		}
		input := responses[0].ToolCalls[0].Input.(map[string]any)
		if input["reason"] != "try again" {
			t.Errorf("reason = %v, want 'try again'", input["reason"])
		}
		found := false
		for _, w := range warnings {
			if strings.Contains(w, "reconstructed meta action") {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one mentioning reconstructed meta action", warnings)
		}
	})

	t.Run("legacy tool_path shape reconstructed with a warning", func(t *testing.T) {
		steps := []types.StepRecord{{
			Step: 3, Action: "cli_driver_step",
			ToolPath: "/dev/fs/Read", ToolInput: `{"path":"foo.txt"}`,
		}}
		responses, warnings := buildImportResponses(steps, "")
		if len(responses[0].ToolCalls) != 1 || responses[0].ToolCalls[0].Name != "Read" {
			t.Fatalf("tool_calls = %+v, want name=Read (last path segment)", responses[0].ToolCalls)
		}
		input := responses[0].ToolCalls[0].Input.(map[string]any)
		if input["path"] != "foo.txt" {
			t.Errorf("path = %v, want foo.txt", input["path"])
		}
		found := false
		for _, w := range warnings {
			if strings.Contains(w, "legacy tool_path") {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one mentioning legacy tool_path", warnings)
		}
	})

	t.Run("unrecognized action with only RawResponse falls back to content + warning", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 4, Action: "mystery", RawResponse: "some raw text"}}
		responses, warnings := buildImportResponses(steps, "")
		if responses[0].Content != "some raw text" {
			t.Errorf("Content = %q, want the raw response", responses[0].Content)
		}
		found := false
		for _, w := range warnings {
			if strings.Contains(w, "content-only response") {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one mentioning content-only response", warnings)
		}
	})

	t.Run("fully unreconstructable step is skipped with a warning, not a panic", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 6, Action: "mystery-empty"}}
		responses, warnings := buildImportResponses(steps, "")
		if len(responses) != 0 {
			t.Errorf("responses = %+v, want zero (nothing to reconstruct)", responses)
		}
		found := false
		for _, w := range warnings {
			if strings.Contains(w, "SKIPPED") {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one mentioning SKIPPED", warnings)
		}
	})

	t.Run("last response a non-Complete tool_call warns", func(t *testing.T) {
		steps := []types.StepRecord{{
			Step: 1, Action: "tool_call",
			ToolCalls: []types.ToolCallRecord{{Name: "Bash", Input: `{"command":"echo hi"}`}},
		}}
		_, warnings := buildImportResponses(steps, "")
		found := false
		for _, w := range warnings {
			if strings.Contains(w, "is not Complete") {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one about the last response not being Complete", warnings)
		}
	})

	t.Run("last response a terminal text turn does not false-positive warn", func(t *testing.T) {
		steps := []types.StepRecord{{Step: 1, Action: "text", RawResponse: "the end"}}
		_, warnings := buildImportResponses(steps, "")
		for _, w := range warnings {
			if strings.Contains(w, "is not Complete") {
				t.Errorf("unexpected 'not Complete' warning for a text-terminal step: %v", warnings)
			}
		}
	})

	t.Run("last response ending in Complete does not warn", func(t *testing.T) {
		steps := []types.StepRecord{
			{Step: 1, Action: "tool_call", ToolCalls: []types.ToolCallRecord{{Name: "Bash", Input: `{"command":"echo hi"}`}}},
			{Step: 2, Action: "complete", Summary: "done"},
		}
		_, warnings := buildImportResponses(steps, "final result")
		for _, w := range warnings {
			if strings.Contains(w, "is not Complete") {
				t.Errorf("unexpected 'not Complete' warning when the script does end in Complete: %v", warnings)
			}
		}
	})
}

// -----------------------------------------------------------------------
// Rendering — no live `assert:`, no `usage:`, warnings as comments.
// -----------------------------------------------------------------------

func TestRenderImportCase_NoLiveAssert_RejectedByValidateTier1(t *testing.T) {
	data, err := renderImportCase("import-abc123", "打个招呼并汇报结果", filepath.Join("scripts", "import-abc123.responses.yaml"), []string{"ReasonStep", "Spawn"}, "hi from the process")
	if err != nil {
		t.Fatalf("renderImportCase: %v", err)
	}
	text := string(data)

	if strings.Contains(text, "\nassert:") {
		t.Errorf("generated case must not have a live top-level assert: key, got:\n%s", text)
	}
	if !strings.Contains(text, "# assert:") {
		t.Errorf("expected a commented `# assert:` suggestion block, got:\n%s", text)
	}
	if !strings.Contains(text, "打个招呼并汇报结果") {
		t.Errorf("expected the CJK intent to survive YAML marshaling, got:\n%s", text)
	}

	suite, err := agtest.ParseBytes(data)
	if err != nil {
		t.Fatalf("generated case must still be syntactically valid agtest YAML: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected exactly one parsed case, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Assert != nil {
		t.Errorf("Assert = %+v, want nil (only commented suggestions)", suite.Tests[0].Assert)
	}
	if suite.Tests[0].Agent.Provider != "replay" {
		t.Errorf("Agent.Provider = %q, want replay", suite.Tests[0].Agent.Provider)
	}

	if err := agtest.ValidateTier1(suite); err == nil {
		t.Fatal("a freshly generated skeleton must be REJECTED by ValidateTier1 (裁决 5) until a human adds real assertions")
	}
}

func TestRenderImportScript_NoUsageField(t *testing.T) {
	responses := []importedResponseYAML{
		{ToolCalls: []importedToolCallYAML{{Name: "Bash", Input: map[string]any{"command": "echo hi"}}}},
		{ToolCalls: []importedToolCallYAML{{Name: "Complete", Input: map[string]any{"result": "done"}}}},
	}
	warnings := []string{"step 1: example warning for review"}

	data, err := renderImportScript(responses, warnings)
	if err != nil {
		t.Fatalf("renderImportScript: %v", err)
	}
	text := string(data)

	if strings.Contains(text, "usage:") {
		t.Errorf("generated response script must never contain a usage: field, got:\n%s", text)
	}
	if !strings.Contains(text, "version: \"1\"") {
		t.Errorf("expected version: \"1\" (68-1 schema), got:\n%s", text)
	}
	if !strings.Contains(text, "#   - step 1: example warning for review") {
		t.Errorf("expected the warning rendered as a comment line, got:\n%s", text)
	}
	if !strings.Contains(text, "name: Complete") {
		t.Errorf("expected the Complete tool_call to survive marshaling, got:\n%s", text)
	}
}

// -----------------------------------------------------------------------
// CLI wiring.
// -----------------------------------------------------------------------

func TestAgtestImportCmd_Registered(t *testing.T) {
	found := false
	for _, c := range agtestCmd.Commands() {
		if c.Name() == "import" {
			found = true
		}
	}
	if !found {
		t.Fatal("import subcommand not registered on agtestCmd")
	}
	if f := agtestImportCmd.Flags().Lookup("out"); f == nil {
		t.Fatal("--out flag not found on agtest import")
	} else if f.DefValue != "tests/agtest/imported" {
		t.Errorf("default --out = %q, want tests/agtest/imported", f.DefValue)
	}
}

func TestAgtestImportCmd_EndToEnd_RealisticFixture(t *testing.T) {
	resetAgtestImportFlags(t)
	t.Setenv("RNIX_DATA_DIR", importTestdataDir(t))

	outDir := t.TempDir()
	root := newAgtestRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	// Exercise the short-suffix lookup, not just the full uuid — the
	// dashboard's own convention (AC4). Last 6 chars of importFixtureUUID.
	root.SetArgs([]string{"agtest", "import", "6789ab", "--out", outDir})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0; output:\n%s", exitCode, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, importFixtureUUID) {
		t.Errorf("expected the resolved full uuid in output, got: %s", out)
	}

	casePath := filepath.Join(outDir, "import-6789ab.yaml")
	scriptPath := filepath.Join(outDir, "scripts", "import-6789ab.responses.yaml")

	caseBytes, err := os.ReadFile(casePath)
	if err != nil {
		t.Fatalf("case file not written at %s: %v", casePath, err)
	}
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("script file not written at %s: %v", scriptPath, err)
	}

	caseText := string(caseBytes)
	if !strings.Contains(caseText, "echo a greeting via shell and report it") {
		t.Errorf("expected the fixture's intent to be backfilled, got:\n%s", caseText)
	}
	if !strings.Contains(caseText, "provider: replay") {
		t.Errorf("expected agent.provider: replay, got:\n%s", caseText)
	}
	if !strings.Contains(caseText, "ReasonStep") {
		t.Errorf("expected a syscalls suggestion mentioning ReasonStep, got:\n%s", caseText)
	}

	scriptText := string(scriptBytes)
	if !strings.Contains(scriptText, "name: Bash") {
		t.Errorf("expected the recorded Bash tool_call to survive, got:\n%s", scriptText)
	}
	if !strings.Contains(scriptText, "name: Complete") {
		t.Errorf("expected the complete step to become a Complete tool_call, got:\n%s", scriptText)
	}
	if !strings.Contains(scriptText, "hi-from-import produced the greeting") {
		t.Errorf("expected the proc-info result inlined into the Complete input, got:\n%s", scriptText)
	}
	if strings.Contains(scriptText, "usage:") {
		t.Errorf("script must never contain usage:, got:\n%s", scriptText)
	}

	// The generated case must still fail Tier1 discipline until reviewed.
	suite, err := agtest.ParseFile(casePath)
	if err != nil {
		t.Fatalf("generated case must parse: %v", err)
	}
	if err := agtest.ValidateTier1(suite); err == nil {
		t.Fatal("freshly generated case must be rejected by ValidateTier1 until reviewed")
	}
}

func TestAgtestImportCmd_DegradedMetadata_MissingProcInfoAndEvents(t *testing.T) {
	resetAgtestImportFlags(t)
	t.Setenv("RNIX_DATA_DIR", importTestdataDir(t))

	outDir := t.TempDir()
	root := newAgtestRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", "import", importFixtureUUIDNoMeta, "--out", outDir})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("missing proc-info.json/events.jsonl must degrade, not fail; exitCode=%d output:\n%s", exitCode, buf.String())
	}

	slug := "import-" + shortUUID(importFixtureUUIDNoMeta)
	caseText, err := os.ReadFile(filepath.Join(outDir, slug+".yaml"))
	if err != nil {
		t.Fatalf("case file not written: %v", err)
	}
	text := string(caseText)
	if !strings.Contains(text, "TODO: proc-info.json had no intent recorded") {
		t.Errorf("expected the intent TODO placeholder (proc-info.json absent), got:\n%s", text)
	}
	if !strings.Contains(text, "events.jsonl was missing/empty") {
		t.Errorf("expected the syscalls TODO placeholder (events.jsonl absent), got:\n%s", text)
	}
}

func TestAgtestImportCmd_RefusesOverwrite(t *testing.T) {
	resetAgtestImportFlags(t)
	t.Setenv("RNIX_DATA_DIR", importTestdataDir(t))

	outDir := t.TempDir()

	run := func() (string, int) {
		root := newAgtestRoot()
		var buf bytes.Buffer
		root.SetOut(&buf)
		root.SetArgs([]string{"agtest", "import", importFixtureUUID, "--out", outDir})
		if err := root.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return buf.String(), exitCode
	}

	exitCode = 0
	if _, code := run(); code != 0 {
		t.Fatalf("first import should succeed, exitCode=%d", code)
	}

	exitCode = 0
	out, code := run()
	if code != 1 {
		t.Fatalf("second import into the same --out must fail, exitCode=%d output=%s", code, out)
	}
	if !strings.Contains(out, "refusing to overwrite") {
		t.Errorf("expected a refusing-to-overwrite error, got: %s", out)
	}
}

func TestAgtestImportCmd_StepsJSONLMissing_HardFail(t *testing.T) {
	resetAgtestImportFlags(t)
	dataDir := t.TempDir()
	t.Setenv("RNIX_DATA_DIR", dataDir)
	stepsDir := filepath.Join(dataDir, "projects", "proj", "steps")
	noStepsUUID := "44444444-4444-4444-8444-444444444444"
	makeUUIDDir(t, stepsDir, noStepsUUID)

	root := newAgtestRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", "import", noStepsUUID, "--out", t.TempDir()})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 1 {
		t.Fatalf("missing steps.jsonl must hard-fail, exitCode=%d output:\n%s", exitCode, buf.String())
	}
	if !strings.Contains(buf.String(), "read steps for") {
		t.Errorf("expected a steps-read error, got: %s", buf.String())
	}
}

func TestAgtestImportCmd_UnknownUUID_JSONError(t *testing.T) {
	resetAgtestImportFlags(t)
	t.Setenv("RNIX_DATA_DIR", t.TempDir())

	root := newAgtestRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", "import", "nosuchuuid", "--json", "--out", t.TempDir()})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v (output: %s)", err, buf.String())
	}
	if resp.OK {
		t.Error("OK should be false for an unresolvable uuid")
	}
}

// runAgtest's own directory-argument path must still work with the import
// subcommand registered — cobra resolves "import" as a subcommand name, but
// an ordinary directory path is never mistaken for it (Story 68.3 组合矩阵).
func TestAgtestCommand_DirectoryArg_StillRoutesToParentRunE(t *testing.T) {
	resetAgtestImportFlags(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte("version: \"1.0\"\nintent: \"hi\"\nagent:\n  name: a\n"), 0644); err != nil {
		t.Fatal(err)
	}

	root := newAgtestRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"agtest", dir, "--dry-run"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "1 test case") {
		t.Errorf("expected the parent RunE's normal dry-run output, got: %s", buf.String())
	}
}
