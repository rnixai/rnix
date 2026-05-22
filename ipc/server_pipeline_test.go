package ipc

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/kernel"
)

// newRunningScriptProc returns a script-runner-like Process in State=Running,
// matching how handleExecScript creates it via Spawn(SkipReasonLoop:true).
// Seeds LastHeartbeat + StepTimeout the same way Spawn does (kernel/spawn.go:363-366),
// so HeartbeatMonitor.scan does not silently short-circuit on uninitialised
// fields (see kernel/heartbeat_monitor.go:115-129).
//
// Shared by the heartbeat tests and the Story 44.2 finalize/cancel-watcher
// ATDD suites (atdd_44_2_*_test.go).
func newRunningScriptProc(t *testing.T) *kernel.Process {
	t.Helper()
	p := kernel.NewProcess(0, "run: test.ash", nil)
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.TouchHeartbeat()
	p.StepTimeout = 5 * time.Minute
	return p
}

// Story 44.2 (AC#7) deleted the three sentinel-cause-driven
// TestFinalizeScriptRunner_* tests that used to live here. CLI disconnect no
// longer cancels scriptCtx (it emits SignalTree(SIGPAUSE)), so the
// (cause, execErr) decision matrix collapsed to a (state, execErr) one. The
// rewritten coverage now lives in atdd_44_2_finalize_test.go:
//
//   - TestATDD_44_2_030_FinalizeScriptRunner_ParentSuspended_SkipsReapAndReturnsEarly
//     (rewrite of CliDisconnect_SuspendsAndPreserves — SCRIPT_SUSPENDED, no reap)
//   - TestATDD_44_2_032_FinalizeScriptRunner_ExecError_FinishesAndReaps
//     (rewrite of ScriptKilled_FinishesAndReaps, sentinel-free)
//   - TestATDD_44_2_033_FinalizeScriptRunner_RaceWithKill_ZombieParentStillReaps
//     (rewrite of CliDisconnect_StateNotRunning_FallsBackToFinish)
//   - TestATDD_44_2_034_FinalizeScriptRunner_CleanRun_FinishesSuccessfully
//     (preserved, 4-arg signature)
//
// DaemonShutdown_FinishesAndReaps + CliDisconnect_NoExecErr_StillFinishes +
// SuspendFails_ClearsSuspendReason were deleted as no-longer-reachable
// scenarios under the new Ctrl+C semantics.
