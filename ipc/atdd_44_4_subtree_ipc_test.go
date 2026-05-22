package ipc

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 44.4 — AC#1: new IPC `pause_subtree` / `resume_subtree` methods
// (IPC 4-step extension spec) + their client wrappers.
//
// RED PHASE signal: `client.PauseSubtree` / `client.ResumeSubtree` do not
// exist yet (ipc/client.go has only Suspend / Resume* / SignalTree). Both
// tests fail to COMPILE until dev-story Task 1.3 adds the wrappers — the same
// compile-fail RED style 44.1/44.2/44.3 used for undefined target symbols.
//
// Once the wrappers + server handlers land (Task 1.1-1.3), these become
// behavioural assertions on the affected / skipped counts that mirror
// kernel.SuspendSubtree / kernel.ResumeSubtree's (int, error) and
// (affected, skipped, error) returns.
//
// Construction: real Running processes via srv.kern.Spawn(SkipReasonLoop=true)
// — no LLM I/O, no reasonStep goroutine — linked parent→child via
// SpawnOpts.ParentPID (kernel/spawn.go:85 does parent.AddChild on spawn).
// =============================================================================

// TestATDD_44_4_010_PauseSubtree_SuspendsWholeSubtree_AffectedCount
//
// AC#1 pause leg: client.PauseSubtree(parentPID) suspends the parent and every
// living descendant, returning Affected == number of Running nodes transitioned
// to Suspended.
func TestATDD_44_4_010_PauseSubtree_SuspendsWholeSubtree_AffectedCount(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	parentPID, err := srv.kern.Spawn("P_script parent", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	childPID, err := srv.kern.Spawn("P_child", nil, kernel.SpawnOpts{SkipReasonLoop: true, ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}

	cli, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	resp, err := cli.PauseSubtree(parentPID)
	if err != nil {
		t.Fatalf("PauseSubtree: %v", err)
	}
	if resp.Affected != 2 {
		t.Errorf("PauseSubtree Affected = %d, want 2 (parent + child)", resp.Affected)
	}

	for label, pid := range map[string]types.PID{"parent": parentPID, "child": childPID} {
		proc, ok := srv.kern.GetProcess(pid)
		if !ok {
			t.Fatalf("%s process vanished after PauseSubtree", label)
		}
		if got := proc.GetState(); got != types.StateSuspended {
			t.Errorf("%s state = %s, want Suspended", label, got)
		}
	}
}

// TestATDD_44_4_011_ResumeSubtree_ResumesSuspended_SkipsDead
//
// AC#1 resume leg: client.ResumeSubtree(parentPID) resumes every Suspended node
// in the subtree to Running and reports Skipped for Dead/Failed descendants
// (Decker step-5 "父恢复时已 Dead 的子被跳过, 不报错").
func TestATDD_44_4_011_ResumeSubtree_ResumesSuspended_SkipsDead(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	parentPID, err := srv.kern.Spawn("P_script parent", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	alivePID, err := srv.kern.Spawn("P_child alive", nil, kernel.SpawnOpts{SkipReasonLoop: true, ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn alive child: %v", err)
	}
	deadPID, err := srv.kern.Spawn("P_child dead", nil, kernel.SpawnOpts{SkipReasonLoop: true, ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn dead child: %v", err)
	}

	// Walk the dead child through the production Running→Zombie→Dead path so
	// ResumeSubtree counts it as skipped (it is no longer Running, so the
	// PauseSubtree leg below leaves it Dead untouched).
	deadProc, ok := srv.kern.GetProcess(deadPID)
	if !ok {
		t.Fatalf("dead child vanished")
	}
	if err := deadProc.Terminate(kernel.ExitStatus{Code: 0, Reason: "completed"}); err != nil {
		t.Fatalf("Terminate dead child: %v", err)
	}
	if err := deadProc.Transition(types.StateDead); err != nil {
		t.Fatalf("Transition dead child to Dead: %v", err)
	}

	cli, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	// Pause first: parent + alive child → Suspended (dead child skipped).
	pauseResp, err := cli.PauseSubtree(parentPID)
	if err != nil {
		t.Fatalf("PauseSubtree: %v", err)
	}
	if pauseResp.Affected != 2 {
		t.Errorf("PauseSubtree Affected = %d, want 2 (only Running parent + alive child)", pauseResp.Affected)
	}

	// Resume: parent + alive child → Running; dead child → Skipped.
	resumeResp, err := cli.ResumeSubtree(parentPID)
	if err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}
	if resumeResp.Affected != 2 {
		t.Errorf("ResumeSubtree Affected = %d, want 2 (parent + alive child resumed)", resumeResp.Affected)
	}
	if resumeResp.Skipped < 1 {
		t.Errorf("ResumeSubtree Skipped = %d, want >=1 (dead child must be counted skipped)", resumeResp.Skipped)
	}

	for label, pid := range map[string]types.PID{"parent": parentPID, "alive": alivePID} {
		proc, _ := srv.kern.GetProcess(pid)
		if proc == nil || proc.GetState() != types.StateRunning {
			t.Errorf("%s state = %v, want Running after ResumeSubtree", label, proc)
		}
	}
	if deadProc.GetState() != types.StateDead {
		t.Errorf("dead child state = %s, want Dead (skipped, not resurrected)", deadProc.GetState())
	}
}
