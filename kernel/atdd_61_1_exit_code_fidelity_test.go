package kernel

import (
	"errors"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 61.1 — exit-code tool-error fidelity (2-layer model).
//
// These tests pin the new contract beyond the phase2 tool-error翻转:
//   - failedChildren>0 drives exit 1 on BOTH completion points (ActionComplete and
//     the final-text path), proving CAP-3's symmetric verdict.
//   - intent/orchestration sub-task failures (DriverError.FailsParent) are promoted
//     to Layer-1 child failures via MarkFailedChild (C8/AC4 — the fake-success guard
//     that used to ride on the now-removed HasToolError flag).
//   - the exit code is independent of tool-error ordering (CAP-4/AC5).
//   - LLM call-level hard failures still exit non-zero via the early-exit path,
//     before reaching completion (A3 regression guard — this story does not touch
//     that path, only confirms it).

// TestActionComplete_FailedChildren_ExitOne pins CAP-2/AC3 on the ActionComplete
// path: a parent that spawned a non-zero child must NOT report success even though
// it reached `complete`. Drives executeMetaAction(ActionComplete) directly.
func TestActionComplete_FailedChildren_ExitOne(t *testing.T) {
	llm := &mockLLMFile{readData: makeLLMResponse("ok", 1)}
	k, _, _ := newTestKernel(t, llm)
	k.SetStepDataDir(t.TempDir())

	pid, err := k.Spawn("orchestrator", nil, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	proc.MarkFailedChild() // one non-zero ActionSpawn child

	mapping := toolMapping{Type: "meta", Action: ActionComplete}
	tc := llmToolCall{ID: "c1", Name: "Complete", Input: map[string]any{"result": "all done"}}
	resp := llmResponse{Content: "all done"}
	var counter errFingerprintCounter
	prompt := &rnixctx.PromptResult{}

	cont := k.executeMetaAction(proc, tc, mapping, 1, time.Now(), &counter, map[string]bool{}, prompt, "", &resp)
	if cont {
		t.Fatal("ActionComplete must terminate the process (cont=false)")
	}

	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil || exit.Code != 1 {
		t.Fatalf("exit = %+v, want Code=1 (failedChildren)", exit)
	}
	if exit.Reason != "completed_with_1_failed_children" {
		t.Errorf("reason = %q, want completed_with_1_failed_children", exit.Reason)
	}
}

// TestFinalText_FailedChildren_ExitOne pins CAP-3/AC2 on the final-text path: the
// SAME verdict as ActionComplete. This guard was previously MISSING here (the two
// completion points were asymmetric), so a process exiting via final text could
// mask failed children with exit 0. Drives reasonStep directly so a single text
// response exits at step 1 via the final-text branch.
func TestFinalText_FailedChildren_ExitOne(t *testing.T) {
	llm := &mockLLMFile{readData: makeLLMResponse("final answer", 10)}
	k, _, _ := newTestKernel(t, llm)
	k.SetStepDataDir(t.TempDir())

	pid, err := k.Spawn("orchestrator", nil, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	proc.MarkFailedChild()

	llmFD, err := k.vfs.Open(proc.PID, "/dev/llm/claude", vfs.O_RDWR)
	if err != nil {
		t.Fatalf("open llm device: %v", err)
	}
	// Synchronous drive: the single text response (no tool_calls) exits at step 1.
	k.reasonStep(proc, llmFD, SpawnOpts{})

	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil || exit.Code != 1 {
		t.Fatalf("exit = %+v, want Code=1 (failedChildren on final-text path)", exit)
	}
	if exit.Reason != "completed_with_1_failed_children" {
		t.Errorf("reason = %q, want completed_with_1_failed_children (must match ActionComplete verdict)", exit.Reason)
	}
}

// TestToolError_OrderIndependent_ExitZero pins CAP-4/AC5: with no failedChildren
// and no circuit breaker, the exit code is 0 regardless of whether a tool error
// occurred before or after a successful call (the old last-call HasToolError
// semantics are gone).
func TestToolError_OrderIndependent_ExitZero(t *testing.T) {
	cases := []struct {
		name      string
		responses [][]byte
	}{
		{"error_then_success", [][]byte{
			makeToolCallResponse("/dev/nonexistent", map[string]any{"q": "1"}, 10), // fail
			makeToolCallResponse("/dev/tools/ok", map[string]any{"q": "2"}, 10),    // ok
			makeLLMResponse("done", 5),
		}},
		{"success_then_error", [][]byte{
			makeToolCallResponse("/dev/tools/ok", map[string]any{"q": "1"}, 10),    // ok
			makeToolCallResponse("/dev/nonexistent", map[string]any{"q": "2"}, 10), // fail
			makeLLMResponse("done", 5),
		}},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			reg := vfs.NewDeviceRegistry()
			seqFile := &sequenceLLMFile{responses: tcase.responses}
			_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
				return seqFile, nil
			})
			registerMockTool(reg, "/dev/tools/ok", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
				return &mockToolFile{readData: []byte("ok")}, nil
			})
			v := vfs.NewVFS(reg)
			k := NewKernel(v, rnixctx.NewManager(), nil)
			defer k.Shutdown()

			pid, err := k.Spawn("order", nil, SpawnOpts{})
			if err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			proc, _ := k.GetProcess(pid)
			select {
			case exit := <-proc.Done:
				if exit.Code != 0 {
					t.Fatalf("exit Code = %d (reason %q), want 0 regardless of tool-error order", exit.Code, exit.Reason)
				}
				if exit.Reason != "completed" {
					t.Errorf("reason = %q, want completed", exit.Reason)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out")
			}
		})
	}
}

// TestIntentFailsParent_DrivesNonZeroExit pins AC4/C8: a DriverError carrying
// FailsParent=true (an intent orchestration sub-task tree that failed) is promoted
// to a Layer-1 child failure via MarkFailedChild, so the orchestrator exits
// non-zero even though it reached its final text. This is the fake-success guard
// that previously rode on the now-removed HasToolError flag. Plain tool errors
// (FailsParent=false) do NOT trip this — covered by the *_ExitCodeZero tests.
func TestIntentFailsParent_DrivesNonZeroExit(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{responses: [][]byte{
		// Step 1: orchestrator drives an intent sub-task tree that fails. The mock
		// device returns a FailsParent DriverError, mirroring drivers/intent's
		// "intent execution failed" terminal-state signal.
		makeToolCallResponse("/dev/intent-mock", map[string]any{"intent": "x"}, 10),
		// Step 2: orchestrator still emits a final text (the classic fake-success
		// shape) — must exit non-zero because a sub-task failed.
		makeLLMResponse("orchestration finished", 5),
	}}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/intent-mock", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{writeErr: &types.DriverError{
			Op: "Write", Device: "/dev/intent-mock", Code: types.ErrDriver, FailsParent: true,
			Err: errors.New("intent execution failed: 1 of 1 sub-task(s) failed [a: boom]"),
		}}, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	defer k.Shutdown()

	pid, err := k.Spawn("orchestrator", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code == 0 {
			t.Fatalf("orchestrator exited 0 (reason %q) — intent FailsParent guard regressed (fake success)", exit.Reason)
		}
		if exit.Reason != "completed_with_1_failed_children" {
			t.Errorf("reason = %q, want completed_with_1_failed_children", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
	proc.mu.Lock()
	fc := proc.failedChildren
	proc.mu.Unlock()
	if fc != 1 {
		t.Errorf("failedChildren = %d, want 1 (FailsParent promoted via MarkFailedChild)", fc)
	}
}

// TestPlainDriverError_DoesNotFailParent guards决策②: a device故障类 DriverError
// (DRIVER/INTERNAL) WITHOUT FailsParent stays content-layer — it must NOT mark a
// failed child, so a process that reaches its final text exits 0. This is the
// negative control proving FailsParent (not the error code) is the discriminator.
func TestPlainDriverError_DoesNotFailParent(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{responses: [][]byte{
		makeToolCallResponse("/dev/device-mock", map[string]any{"q": "1"}, 10),
		makeLLMResponse("done despite device error", 5),
	}}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/device-mock", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		// DRIVER error but FailsParent omitted (false) — observability-only.
		return &mockLLMFile{writeErr: &types.DriverError{
			Op: "Write", Device: "/dev/device-mock", Code: types.ErrDriver,
			Err: errors.New("device backend transient glitch"),
		}}, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	defer k.Shutdown()

	pid, err := k.Spawn("agent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit Code = %d (reason %q), want 0 — a plain DriverError must stay content-layer (决策②)", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
	proc.mu.Lock()
	fc := proc.failedChildren
	proc.mu.Unlock()
	if fc != 0 {
		t.Errorf("failedChildren = %d, want 0 (FailsParent=false must not mark a failed child)", fc)
	}
}

// TestLLMHardFailure_ExitNonZero is the A3 regression guard: an LLM call-level hard
// failure (driver read error, non-transient) exits non-zero via the early-exit path
// BEFORE reaching completion. This story does not touch that path; pin it so a
// future change can't let "LLM backend down" exit 0/"completed".
func TestLLMHardFailure_ExitNonZero(t *testing.T) {
	llm := &mockLLMFile{readErr: errors.New("llm backend unreachable")}
	k, _, _ := newTestKernel(t, llm)
	k.SetStepDataDir(t.TempDir())

	pid, err := k.Spawn("agent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code == 0 {
			t.Fatalf("LLM hard failure exited 0 (reason %q) — A3 regression", exit.Reason)
		}
		if exit.Reason == "completed" || strings.HasPrefix(exit.Reason, "completed_with") {
			t.Errorf("reason = %q, want an early-exit reason (llm read failed/fallback), not a completion reason", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}
