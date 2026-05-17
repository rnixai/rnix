package main

// =============================================================================
// ATDD Story 42.4: `rnix compose resume --node <name>` CLI subcommand (UNIT)
//
// Acceptance criteria covered:
//   - AC#2  CLI-001  Cobra command shape (flags, RunE, default file)
//   - AC#4  CLI-002  Missing resumable instance → ErrNotFound (exit 1)
//   - AC#5  CLI-003  Node not in spec → ErrInvalid (exit 2)
//   - AC#5  CLI-004  Latest instance Zombie ok=true → idempotent (exit 0)
//   - AC#7  CLI-005  --fork flag plumbed through to ResumeWithOptsV2
//   - AC#8  CLI-006  --dry-run does NOT call IPC resume (plan only)
//   - AC#9  CLI-007  --json output schema matches AC#9 contract
//
// RED PHASE:
//   - composeResumeCmd is registered and accepts flags, but runComposeResume
//     returns errComposeResumeNotImplemented (stub).
//   - Helper functions (validateComposeNodeInSpec, findResumableComposeProc,
//     isComposeProcIdempotent, buildHistoricalUpstream, renderComposeResume*)
//     are stubs.
//   - All behavior tests except the stub-sanity ones are wrapped in t.Skip().
// =============================================================================

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rnixai/rnix/compose"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// fixture helpers
// ---------------------------------------------------------------------------

func newLinearComposeSpec_42_4() *compose.ComposeSpec {
	return &compose.ComposeSpec{
		Version: "1.0",
		Intent:  "linear A->B->C",
		Agents: map[string]*compose.AgentSpec{
			"node-A": {Intent: "step A"},
			"node-B": {Intent: "step B", DependsOn: map[string]string{"node-A": "completed"}},
			"node-C": {Intent: "step C", DependsOn: map[string]string{"node-B": "completed"}},
		},
	}
}

func makeProcInfo_42_4(pid types.PID, uuid string, composeNode string, state types.ProcessState, exitReason string, createdAt time.Time, result string, tokens int) vfs.ProcInfo {
	return vfs.ProcInfo{
		PID:         pid,
		UUID:        uuid,
		Intent:      composeNode + " run",
		ComposeNode: composeNode,
		State:       state,
		ExitReason:  exitReason,
		CreatedAt:   createdAt,
		Result:      result,
		TokensUsed:  tokens,
	}
}

// ---------------------------------------------------------------------------
// CLI-001 (AC#2): cobra command shape
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_001_CommandShape(t *testing.T) {
	// This sanity check is NOT skipped — it validates the cobra registration
	// from compose_resume.go:init(). Even in RED phase the command must be
	// callable from `rnix compose resume`.

	if composeResumeCmd == nil {
		t.Fatal("composeResumeCmd is nil")
	}
	if composeResumeCmd.Use != "resume" {
		t.Errorf("composeResumeCmd.Use = %q, want %q", composeResumeCmd.Use, "resume")
	}
	if composeResumeCmd.RunE == nil {
		t.Error("composeResumeCmd.RunE is nil")
	}

	// All required flags must be registered.
	required := []string{"file", "node", "fork", "from-step", "dry-run"}
	for _, name := range required {
		if composeResumeCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered on composeResumeCmd", name)
		}
	}

	// --node must be marked required.
	nodeFlag := composeResumeCmd.Flags().Lookup("node")
	if nodeFlag == nil {
		t.Fatal("flag --node missing")
	}
	requiredAnnotation := nodeFlag.Annotations["cobra_annotation_bash_completion_one_required_flag"]
	if len(requiredAnnotation) == 0 || requiredAnnotation[0] != "true" {
		t.Errorf("flag --node must be marked required; annotations=%v", nodeFlag.Annotations)
	}

	// --file default must be rnix-compose.yaml (mirrors `up`/`down`).
	fileFlag := composeResumeCmd.Flags().Lookup("file")
	if fileFlag == nil {
		t.Fatal("flag --file missing")
	}
	if fileFlag.DefValue != "rnix-compose.yaml" {
		t.Errorf("flag --file default = %q, want %q", fileFlag.DefValue, "rnix-compose.yaml")
	}

	// --fork / --from-step / --dry-run must be the right types.
	if forkFlag := composeResumeCmd.Flags().Lookup("fork"); forkFlag == nil || forkFlag.Value.Type() != "bool" {
		t.Errorf("flag --fork must be bool; got %+v", forkFlag)
	}
	if fsFlag := composeResumeCmd.Flags().Lookup("from-step"); fsFlag == nil || fsFlag.Value.Type() != "int" {
		t.Errorf("flag --from-step must be int; got %+v", fsFlag)
	}
	if drFlag := composeResumeCmd.Flags().Lookup("dry-run"); drFlag == nil || drFlag.Value.Type() != "bool" {
		t.Errorf("flag --dry-run must be bool; got %+v", drFlag)
	}

	// composeResumeCmd must be registered as a subcommand of composeCmd.
	if !slices.Contains(composeCmd.Commands(), composeResumeCmd) {
		t.Error("composeResumeCmd is not registered under composeCmd")
	}
}

// ---------------------------------------------------------------------------
// CLI-002 (AC#4): findResumableComposeProc returns false when no Dead/Zombie
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_002_NoResumableInstance(t *testing.T) {
	t.Skip("RED phase: findResumableComposeProc not implemented (Story 42.4)")

	procs := []vfs.ProcInfo{
		makeProcInfo_42_4(1, "uuid-1", "node-A", types.StateZombie, "", time.Unix(1, 0), "A done", 50),
		// node-B has only Running instances, none Dead/Zombie
		makeProcInfo_42_4(2, "uuid-2", "node-B", types.StateRunning, "", time.Unix(2, 0), "", 0),
	}
	got, ok := findResumableComposeProc(procs, "node-B")
	if ok {
		t.Errorf("expected findResumableComposeProc to return false for node-B (only Running); got %+v", got)
	}

	// Empty list also returns false.
	if _, ok2 := findResumableComposeProc(nil, "node-B"); ok2 {
		t.Error("expected findResumableComposeProc(nil, ...) to return false")
	}
}

// ---------------------------------------------------------------------------
// CLI-002b (AC#4): findResumableComposeProc selects newest Dead/Zombie
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_002b_SelectsNewestDeadZombie(t *testing.T) {
	t.Skip("RED phase: findResumableComposeProc not implemented (Story 42.4)")

	procs := []vfs.ProcInfo{
		makeProcInfo_42_4(1, "uuid-old", "node-B", types.StateDead, "llm err", time.Unix(100, 0), "", 30),
		makeProcInfo_42_4(2, "uuid-new", "node-B", types.StateDead, "llm err", time.Unix(200, 0), "", 40),
		makeProcInfo_42_4(3, "uuid-other", "node-A", types.StateZombie, "", time.Unix(300, 0), "A", 10),
	}
	got, ok := findResumableComposeProc(procs, "node-B")
	if !ok {
		t.Fatal("expected findResumableComposeProc to find node-B")
	}
	if got.UUID != "uuid-new" {
		t.Errorf("expected newest UUID uuid-new, got %q", got.UUID)
	}
}

// ---------------------------------------------------------------------------
// CLI-003 (AC#5): validateComposeNodeInSpec rejects unknown node
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_003_NodeNotInSpec(t *testing.T) {
	t.Skip("RED phase: validateComposeNodeInSpec not implemented (Story 42.4)")

	spec := newLinearComposeSpec_42_4()

	// Known node → no error.
	if err := validateComposeNodeInSpec(spec, "node-B"); err != nil {
		t.Errorf("validateComposeNodeInSpec(node-B) = %v, want nil", err)
	}
	// Unknown node → error.
	err := validateComposeNodeInSpec(spec, "node-X")
	if err == nil {
		t.Error("validateComposeNodeInSpec(node-X) = nil, want error")
	}
}

// ---------------------------------------------------------------------------
// CLI-004 (AC#5): isComposeProcIdempotent detects "nothing to resume"
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_004_IdempotentSuccess(t *testing.T) {
	t.Skip("RED phase: isComposeProcIdempotent not implemented (Story 42.4)")

	successfulZombie := makeProcInfo_42_4(1, "uuid-success", "node-B", types.StateZombie, "", time.Unix(100, 0), "done", 80)
	if !isComposeProcIdempotent(successfulZombie) {
		t.Error("Zombie with empty ExitReason must be idempotent (Nothing to resume)")
	}

	failedDead := makeProcInfo_42_4(2, "uuid-fail", "node-B", types.StateDead, "llm timeout", time.Unix(100, 0), "", 80)
	if isComposeProcIdempotent(failedDead) {
		t.Error("Dead with non-empty ExitReason must NOT be idempotent")
	}

	failedZombie := makeProcInfo_42_4(3, "uuid-fail2", "node-B", types.StateZombie, "boundary err", time.Unix(100, 0), "", 80)
	if isComposeProcIdempotent(failedZombie) {
		t.Error("Zombie with non-empty ExitReason must NOT be idempotent")
	}
}

// ---------------------------------------------------------------------------
// CLI-005 (AC#7): --fork flag plumbed through (smoke — flag parsing only)
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_005_ForkFlagParsing(t *testing.T) {
	// Sanity check: flag parsing must accept --fork.

	// Reset flags to defaults (cobra commands hold global flag state).
	flagComposeResumeFork = false
	flagComposeResumeFromStep = 0
	flagComposeResumeDryRun = false

	if err := composeResumeCmd.Flags().Parse([]string{"--node=node-B", "--fork", "--from-step=5"}); err != nil {
		t.Fatalf("flag parsing: %v", err)
	}
	if !flagComposeResumeFork {
		t.Error("--fork did not set flagComposeResumeFork = true")
	}
	if flagComposeResumeFromStep != 5 {
		t.Errorf("flagComposeResumeFromStep = %d, want 5", flagComposeResumeFromStep)
	}
	if flagComposeResumeNode != "node-B" {
		t.Errorf("flagComposeResumeNode = %q, want %q", flagComposeResumeNode, "node-B")
	}

	// Clean up so other tests start from defaults.
	flagComposeResumeFork = false
	flagComposeResumeFromStep = 0
	flagComposeResumeDryRun = false
	flagComposeResumeNode = ""
}

// ---------------------------------------------------------------------------
// CLI-006 (AC#8): renderComposeResumePlan produces plan text
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_006_DryRunPlan(t *testing.T) {
	t.Skip("RED phase: renderComposeResumePlan not implemented (Story 42.4)")

	var buf bytes.Buffer
	renderComposeResumePlan(&buf, "node-B", "uuid-resume-target", []string{"node-C", "node-D"})

	out := buf.String()
	mustContain := []string{"node-B", "uuid-resume-target", "node-C", "node-D", "Would resume"}
	for _, s := range mustContain {
		if !bytesContains(buf.Bytes(), s) {
			t.Errorf("dry-run plan missing %q\noutput:\n%s", s, out)
		}
	}
}

// ---------------------------------------------------------------------------
// CLI-007 (AC#9): renderComposeResumeJSON conforms to AC#9 schema
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_007_JSONOutputSchema(t *testing.T) {
	t.Skip("RED phase: renderComposeResumeJSON not implemented (Story 42.4)")

	results := []compose.ScheduleResult{
		{Name: "node-C", PID: 3, ExitCode: 0, TokensUsed: 100, Output: "C done"},
		{Name: "node-D", PID: 4, ExitCode: 0, TokensUsed: 80, Output: "D done"},
	}

	var buf bytes.Buffer
	renderComposeResumeJSON(&buf, "node-B", "uuid-resumed", results)

	mustContain := []string{
		`"ok"`,
		`"resumed_node"`,
		`"node-B"`,
		`"resumed_uuid"`,
		`"uuid-resumed"`,
		`"downstream"`,
		`"node-C"`,
		`"node-D"`,
		`"exit_code"`,
		`"tokens"`,
	}
	for _, s := range mustContain {
		if !bytesContains(buf.Bytes(), s) {
			t.Errorf("JSON output missing %q\noutput:\n%s", s, buf.String())
		}
	}
}

// ---------------------------------------------------------------------------
// CLI-008 (AC#6): buildHistoricalUpstream constructs full upstream map
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_008_BuildHistoricalUpstream(t *testing.T) {
	t.Skip("RED phase: buildHistoricalUpstream not implemented (Story 42.4)")

	spec := newLinearComposeSpec_42_4()
	procs := []vfs.ProcInfo{
		makeProcInfo_42_4(1, "uuid-A", "node-A", types.StateZombie, "", time.Unix(100, 0), "A output", 50),
		makeProcInfo_42_4(2, "uuid-B-failed", "node-B", types.StateDead, "err", time.Unix(200, 0), "", 30),
	}

	upstream, err := buildHistoricalUpstream(spec, procs, "node-B")
	if err != nil {
		t.Fatalf("buildHistoricalUpstream: %v", err)
	}
	if len(upstream) != 1 {
		t.Fatalf("expected 1 upstream entry, got %d", len(upstream))
	}
	a, ok := upstream["node-A"]
	if !ok {
		t.Fatal("upstream map missing node-A")
	}
	if a.Output != "A output" {
		t.Errorf("upstream[node-A].Output = %q, want %q", a.Output, "A output")
	}
	if a.Tokens != 50 {
		t.Errorf("upstream[node-A].Tokens = %d, want 50", a.Tokens)
	}
	if a.PID != types.PID(1) {
		t.Errorf("upstream[node-A].PID = %d, want 1", a.PID)
	}
}

// ---------------------------------------------------------------------------
// CLI-008b (AC#6): buildHistoricalUpstream errors when upstream never succeeded
// ---------------------------------------------------------------------------

func TestATDD_42_4_CLI_008b_BuildHistoricalUpstream_MissingSuccess(t *testing.T) {
	t.Skip("RED phase: buildHistoricalUpstream not implemented (Story 42.4)")

	spec := newLinearComposeSpec_42_4()
	procs := []vfs.ProcInfo{
		// node-A only has a Dead instance — no successful Zombie.
		makeProcInfo_42_4(1, "uuid-A-fail", "node-A", types.StateDead, "err", time.Unix(100, 0), "", 30),
	}
	_, err := buildHistoricalUpstream(spec, procs, "node-B")
	if err == nil {
		t.Error("buildHistoricalUpstream should error when upstream node-A has no successful run")
	}
}

// ---------------------------------------------------------------------------
// Stub sanity checks (RED PHASE compile-time only; not skipped)
// ---------------------------------------------------------------------------

// TestATDD_42_4_CLI_StubSanity_RunComposeResume verifies that the cobra RunE
// stub returns the sentinel error in red phase.
func TestATDD_42_4_CLI_StubSanity_RunComposeResume(t *testing.T) {
	err := runComposeResume(composeResumeCmd, nil)
	if err == nil {
		t.Log("runComposeResume returned nil — implementation likely live; remove t.Skip from sibling CLI tests")
		return
	}
	if !errors.Is(err, errComposeResumeNotImplemented) {
		t.Errorf("runComposeResume err = %v, want errComposeResumeNotImplemented", err)
	}
}

// TestATDD_42_4_CLI_StubSanity_SeedHistorical_NoOp verifies that the RED
// PHASE ipcKernelSpawner.SeedHistorical method exists and does not panic.
func TestATDD_42_4_CLI_StubSanity_SeedHistorical_NoOp(t *testing.T) {
	spawner := newIPCKernelSpawner("/tmp/no-socket", "/tmp")
	// Must not panic.
	spawner.SeedHistorical("node-A", types.PID(99), "seeded", 42, types.SpanID("span-x"))

	// Verify it satisfies the compose.HistoricalSeeder interface at compile time.
	var _ compose.HistoricalSeeder = spawner
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func bytesContains(b []byte, s string) bool {
	if len(s) == 0 {
		return true
	}
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
