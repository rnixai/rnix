package ipc

import (
	"errors"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 44.2 — AC#5 + AC#7: simplified finalizeScriptRunner.
//
// Story 44.2 redefines the helper:
//
//   func finalizeScriptRunner(scriptProc *kernel.Process,
//                             execErr error,
//                             lastResult string,
//                             lastExitCode int) scriptRunnerOutcome
//
// (The `cause` parameter is gone — three sentinel errors deleted.) The new
// case analysis is:
//
//   case-0: scriptProc.GetState() == StateSuspended
//           → returnEarly=true, reapAfter=false,
//             streamPayload SCRIPT_SUSPENDED, SuspendReason preserved.
//
//   case-1: execErr != nil (and state != Suspended)
//           → Finish("", 1, execErr), reapAfter=true, SCRIPT_ERROR.
//
//   case-2: execErr == nil
//           → Finish(lastResult, lastExitCode, nil), reapAfter=true,
//             returnEarly=false (caller streams ExecScriptResponse).
//
// RED phase: this file calls `finalizeScriptRunner(scriptProc, execErr,
// lastResult, lastExitCode)` (4 args), the current production code takes
// 5 args. Compile-fail = RED.
//
// AC#7 (the old 5 tests under server_pipeline_test.go) is handled in
// dev-story Task 5 via Edit — this file is the FORWARD-LOOKING red-phase
// scaffold that pins the new semantics.
// =============================================================================

// makeSuspendedScriptProc creates a script-runner-like process in
// StateSuspended with SuspendReason="user_paused", matching the state
// produced by SignalTree(SIGPAUSE) → SuspendSubtree in production.
func makeSuspendedScriptProc(t *testing.T) *kernel.Process {
	t.Helper()
	p := kernel.NewProcess(0, "run: test.ash", nil)
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.SetSuspendReason("user_paused")
	if err := p.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	return p
}

// --- AC#5 case-0: Suspended parent → return early, no reap, SCRIPT_SUSPENDED ---

// TestATDD_44_2_030_FinalizeScriptRunner_ParentSuspended_SkipsReapAndReturnsEarly
//
// AC#7 row 1 (rewrite of TestFinalizeScriptRunner_CliDisconnect_SuspendsAndPreserves):
// when the script-runner has been transitioned to Suspended by the
// SignalTree(SIGPAUSE) path BEFORE finalize runs, finalize must NOT reap,
// MUST returnEarly, and MUST stream a SCRIPT_SUSPENDED error event. The
// SuspendReason set by SuspendSubtree (user_paused) must be preserved.
func TestATDD_44_2_030_FinalizeScriptRunner_ParentSuspended_SkipsReapAndReturnsEarly(t *testing.T) {
	proc := makeSuspendedScriptProc(t)
	execErr := errors.New("context canceled")

	// NOTE: new 4-arg signature (no `cause`).
	outcome := finalizeScriptRunner(proc, execErr, "", 0)

	if proc.GetState() != types.StateSuspended {
		t.Errorf("State changed to %v after finalize; want StateSuspended (finalize must NOT mutate Suspended)", proc.GetState())
	}
	if reason := proc.GetSuspendReason(); reason != "user_paused" {
		t.Errorf("SuspendReason = %q after finalize, want %q (finalize must NOT overwrite Suspend reason)", reason, "user_paused")
	}
	if outcome.reapAfter {
		t.Error("reapAfter = true, want false (Suspended subtree must stay in procTable for resume)")
	}
	if !outcome.returnEarly {
		t.Error("returnEarly = false, want true (Suspended path returns early)")
	}
	if outcome.streamType != StreamError {
		t.Errorf("streamType = %v, want StreamError", outcome.streamType)
	}
	ep, ok := outcome.streamPayload.(ErrorPayload)
	if !ok {
		t.Fatalf("streamPayload type = %T, want ErrorPayload", outcome.streamPayload)
	}
	if ep.Code != "SCRIPT_SUSPENDED" {
		t.Errorf("payload.Code = %q, want SCRIPT_SUSPENDED", ep.Code)
	}
}

// TestATDD_44_2_031_FinalizeScriptRunner_ParentSuspended_NoExecErr_StillSuspendedPath
//
// Edge case: Suspended state must take precedence over execErr==nil. If
// the SignalTree(SIGPAUSE) won the race and SuspendController woke the
// executor with ctx.Done() returning nil right at completion, the parent
// must still surface SCRIPT_SUSPENDED, not a misleading success response.
func TestATDD_44_2_031_FinalizeScriptRunner_ParentSuspended_NoExecErr_StillSuspendedPath(t *testing.T) {
	proc := makeSuspendedScriptProc(t)

	outcome := finalizeScriptRunner(proc, nil, "ok", 0)

	if !outcome.returnEarly {
		t.Error("returnEarly = false on Suspended path with nil execErr")
	}
	if outcome.reapAfter {
		t.Error("reapAfter = true on Suspended path with nil execErr; want false")
	}
	ep, ok := outcome.streamPayload.(ErrorPayload)
	if !ok || ep.Code != "SCRIPT_SUSPENDED" {
		t.Errorf("streamPayload = %+v, want SCRIPT_SUSPENDED", outcome.streamPayload)
	}
}

// --- AC#5 case-1: error path (not Suspended) → Finish + Reap + SCRIPT_ERROR ---

// TestATDD_44_2_032_FinalizeScriptRunner_ExecError_FinishesAndReaps
//
// AC#7 row 2 (rewrite of TestFinalizeScriptRunner_ScriptKilled_FinishesAndReaps,
// minus the cause dependency): any non-Suspended Running parent with a
// non-nil execErr lands on Finish+Reap and streams SCRIPT_ERROR. This
// collapses 44.2's predecessor sentinels (errScriptKilled / errDaemonShutdown)
// into one path — the only signal that matters now is "did execErr happen".
func TestATDD_44_2_032_FinalizeScriptRunner_ExecError_FinishesAndReaps(t *testing.T) {
	proc := newRunningScriptProc(t)
	execErr := errors.New("context canceled")

	outcome := finalizeScriptRunner(proc, execErr, "", 0)

	if proc.GetState() != types.StateZombie {
		t.Errorf("State = %v, want Zombie (Finish on Running transitions to Zombie)", proc.GetState())
	}
	if !outcome.reapAfter {
		t.Error("reapAfter = false, want true (error path must reap)")
	}
	if !outcome.returnEarly {
		t.Error("returnEarly = false, want true (error path returns early)")
	}
	ep, ok := outcome.streamPayload.(ErrorPayload)
	if !ok || ep.Code != "SCRIPT_ERROR" {
		t.Errorf("streamPayload = %+v, want SCRIPT_ERROR", outcome.streamPayload)
	}
}

// TestATDD_44_2_033_FinalizeScriptRunner_RaceWithKill_ZombieParentStillReaps
//
// AC#7 row 6 (rewrite of TestFinalizeScriptRunner_CliDisconnect_StateNotRunning_FallsBackToFinish):
// if SIGKILL / Terminate wins the race and transitions the parent to
// Zombie before finalize runs, finalize must NOT crash on the duplicate
// state transition; the outcome still requests Reap and streams
// SCRIPT_ERROR. SuspendReason must remain empty (Zombie ≠ Suspended).
func TestATDD_44_2_033_FinalizeScriptRunner_RaceWithKill_ZombieParentStillReaps(t *testing.T) {
	proc := newRunningScriptProc(t)
	// Pre-transition to Zombie to simulate "kill won the race".
	proc.Finish("killed first", 1, errors.New("racer"))
	if proc.GetState() != types.StateZombie {
		t.Fatalf("setup: state = %v, want Zombie", proc.GetState())
	}

	execErr := errors.New("context canceled")
	outcome := finalizeScriptRunner(proc, execErr, "", 0)

	if !outcome.reapAfter {
		t.Error("reapAfter = false, want true (race-with-kill must reap)")
	}
	if !outcome.returnEarly {
		t.Error("returnEarly = false, want true")
	}
	if reason := proc.GetSuspendReason(); reason != "" {
		t.Errorf("SuspendReason = %q on Zombie path, want \"\"", reason)
	}
}

// --- AC#5 case-2: clean run → success path ---

// TestATDD_44_2_034_FinalizeScriptRunner_CleanRun_FinishesSuccessfully
//
// AC#7 row 4 (preserved, signature adjusted): execErr==nil on a Running
// parent → Finish with provided result, Reap, returnEarly=false (caller
// emits ExecScriptResponse on the success path).
func TestATDD_44_2_034_FinalizeScriptRunner_CleanRun_FinishesSuccessfully(t *testing.T) {
	proc := newRunningScriptProc(t)

	outcome := finalizeScriptRunner(proc, nil, "ok", 0)

	if proc.GetState() != types.StateZombie {
		t.Errorf("State = %v, want Zombie (success Finish also reaches Zombie)", proc.GetState())
	}
	if !outcome.reapAfter {
		t.Error("reapAfter = false, want true")
	}
	if outcome.returnEarly {
		t.Error("returnEarly = true, want false (caller emits ExecScriptResponse)")
	}
	if outcome.streamPayload != nil {
		t.Errorf("streamPayload = %+v, want nil on success", outcome.streamPayload)
	}
}

// --- AC#5: three sentinel errors deleted ---

// TestATDD_44_2_035_SentinelErrorsDeleted_GrepEquivalent
//
// AC#5 first bullet: "删除常量 errCLIDisconnected、errScriptKilled、
// errDaemonShutdown（line 24-30 全部清理）". This compile-time predicate
// makes the deletion auditable from the test suite — once dev-story
// removes the sentinels, these references compile-fail; once they're
// re-exported (or restored under a different name) they compile again,
// and this test will fail with a clear message.
//
// During RED phase the constants still exist in production, so the test
// PASSES — the failure mode flips during GREEN phase: the references
// below will compile-fail. dev-story must DELETE this whole test in the
// commit that removes the sentinels, OR convert it to a no-op (preferred:
// delete, since AC-EA4 already provides the grep-based audit).
func TestATDD_44_2_035_SentinelErrorsDeleted_GrepEquivalent(t *testing.T) {
	// Sanity touch: these references should exist BEFORE dev-story runs
	// (RED phase passes), and the whole test should be deleted once 44.2
	// implementation lands (GREEN phase = test gone, sentinels gone).
	if errCLIDisconnected == nil || errScriptKilled == nil || errDaemonShutdown == nil {
		t.Fatal("setup: legacy sentinels expected to exist during RED phase")
	}
	t.Log("RED-phase audit: sentinel constants still defined — dev-story Task 4.2 must delete them and this test together")
}
