package kernel

import (
	"testing"
	"time"
)

// Resume / supervisor / subtree paths historically reconstructed proc.ctx with
// gocontext.WithCancel(gocontext.Background()) but forgot to layer
// ContextWithPID on top. The kernel-side VFS Write then propagated a PID-less
// ctx into /dev/tty's askFunc, which read PID=0 from the ctx and tripped
// ipc/server_callback.go's `cannot send to PID 0 (no process context)`
// branch — surfacing as a confusing tool error to the LLM after any user_paused
// + resume cycle.
//
// These tests pin the invariant: every code path that replaces proc.ctx must
// also call ContextWithPID(ctx, proc.PID) so downstream ctx consumers
// (notably extractAskUserPID in cmd/rnix/main.go) can resolve the owning PID.

func TestResume_FromCheckpoint_CtxCarriesPID(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "ctxpid01-aaaa-bbbb-cccc-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 7)

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed PID %d missing from procTable", result.PID)
	}
	if got := PIDFromContext(proc.ctx); got != proc.PID {
		t.Fatalf("PIDFromContext(proc.ctx) = %d, want %d (resumeFromCheckpoint must layer ContextWithPID)", got, proc.PID)
	}

	cleanupResumedProc(t, k, result.PID)
}

func TestResume_FromHistory_CtxCarriesPID(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	childUUID := "ctxpid02-aaaa-bbbb-cccc-000000000002"
	writeTestStepsAndMetaWithParent(t, baseDir, childUUID, "" /*no parent*/, 4)

	result, err := k.ResumeWithOpts(childUUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed PID %d missing from procTable", result.PID)
	}
	if got := PIDFromContext(proc.ctx); got != proc.PID {
		t.Fatalf("PIDFromContext(proc.ctx) = %d, want %d (resumeFromHistory must layer ContextWithPID)", got, proc.PID)
	}

	cleanupResumedProc(t, k, result.PID)
}

func TestSupervisor_ProcCtxCarriesPID(t *testing.T) {
	router := &intentRouter{}
	k := newRoutedTestKernel(t, router)

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 1,
		MaxWindow:   time.Second,
		Children: []ChildSpec{
			{Name: "ctxpid-child", Intent: "ctxpid-child", Restart: RestartTemporary},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	sup, ok := k.GetProcess(supPID)
	if !ok {
		t.Fatalf("supervisor PID %d missing from procTable", supPID)
	}
	if got := PIDFromContext(sup.ctx); got != sup.PID {
		t.Fatalf("PIDFromContext(supervisor.ctx) = %d, want %d (SpawnSupervisor must layer ContextWithPID)", got, sup.PID)
	}

	// Let the supervisor finish to avoid leaking goroutines past t.Cleanup.
	_ = waitSupervisor(k, supPID)
}

// TestSubtreeResume_ProcCtxCarriesPID pins the subtree.go ctx replacement
// path. The fixture used by atdd_44_1_helpers_test.go creates fixtures with
// PrimaryDevice="" — those hit the early-return branch in resumeOneForSubtree
// (subtree.go:327-333) and never reach the ctx replacement at line 339, so
// asserting on them would prove nothing. Instead we set PrimaryDevice to the
// setupResumeKernel mock LLM device and call resumeOneForSubtree directly,
// reaching the exact line that historically forgot ContextWithPID.
func TestSubtreeResume_ProcCtxCarriesPID(t *testing.T) {
	k, _ := setupResumeKernel(t) // registers /dev/llm/claude mock so openLLMDeviceForResume succeeds

	proc := NewProcess(0, "ctxpid-subtree-root", nil)
	proc.PrimaryDevice = "/dev/llm/claude"
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	if err := proc.Suspend(); err != nil {
		t.Fatalf("proc.Suspend: %v", err)
	}
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	if err := k.resumeOneForSubtree(proc); err != nil {
		t.Fatalf("resumeOneForSubtree: %v", err)
	}

	if got := PIDFromContext(proc.ctx); got != proc.PID {
		t.Fatalf("PIDFromContext(proc.ctx) = %d, want %d (resumeOneForSubtree must layer ContextWithPID when replacing ctx)", got, proc.PID)
	}
	// Intentionally no state assertion: resumeOneForSubtree launches a
	// reasonStep goroutine that may transition Running → Zombie quickly when
	// the mock LLM completes step 1 (CtxID=0 → BuildPrompt errors immediately
	// or completeResp finishes the loop in one step). Asserting StateRunning
	// here is racy and orthogonal to the ctx-PID invariant under test.

	// resumeOneForSubtree launched a reasonStep goroutine that writes events to
	// k.stepDataDir; wait for it to terminate before t.Cleanup unbinds the
	// TempDir, otherwise RemoveAll races with the goroutine's writes and
	// surfaces as "directory not empty" under -race.
	cleanupResumedProc(t, k, proc.PID)
}
