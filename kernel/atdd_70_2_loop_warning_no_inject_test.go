package kernel

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
)

// Story 70.2 code-review guard: handleLoopDetection's LoopWarning branch must
// emit a LoopDetected event and must NOT append anything to the process context.
//
// Why this file exists: the story deleted the injection, but nothing in the
// suite asserted the resulting behaviour — grep for handleLoopDetection or
// LoopDetected across *_test.go returned nothing, so re-adding the
// AppendMessage call (or renaming the event payload keys that dashboard/strace
// consume) would have kept `make all` green. The prose warning in CLAUDE.md was
// the only thing standing in the way. Now this test is.

// newLoopDetectionFixture builds the minimal kernel + process + context needed
// to drive handleLoopDetection directly. It deliberately skips reasonStep: the
// unit under test is the consumer side of the detector, not the loop that calls
// it.
func newLoopDetectionFixture(t *testing.T) (*KernelImpl, *rnixctx.Manager, *Process) {
	t.Helper()

	llmFile := &mockLLMFile{}
	k, _, ctxMgr := newTestKernel(t, llmFile)

	cid, err := ctxMgr.CtxAlloc(64)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}

	proc := NewProcess(0, "loop-detection-guard", nil)
	proc.CtxID = cid
	return k, ctxMgr, proc
}

// awaitSyscall drains proc.DebugChan looking for the named syscall. Returns the
// event args, or nil if the event never arrived within the timeout.
func awaitSyscall(t *testing.T, proc *Process, syscall string, timeout time.Duration) map[string]any {
	t.Helper()

	deadline := time.After(timeout)
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == syscall {
				return ev.Args
			}
		case <-deadline:
			return nil
		}
	}
}

func TestATDD_70_2_LoopWarning_EmitsEventWithoutContextAppend(t *testing.T) {
	k, ctxMgr, proc := newLoopDetectionFixture(t)

	// Seed some history so "no new message" is a meaningful assertion rather
	// than "the context was empty and stayed empty".
	for range 4 {
		if err := ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, "prior turn"); err != nil {
			t.Fatalf("seed AppendMessage: %v", err)
		}
	}
	before, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage before: %v", err)
	}

	stopped := k.handleLoopDetection(proc, LoopWarning, 30, "coarse", 30, time.Now())
	if stopped {
		t.Error("LoopWarning must not stop the process; only LoopSuspend does")
	}

	// The warning must remain observable to operators via the event stream.
	args := awaitSyscall(t, proc, "LoopDetected", time.Second)
	if args == nil {
		t.Fatal("LoopWarning did not emit a LoopDetected event")
	}
	// payload keys are the dashboard / strace contract (AC5)
	for _, key := range []string{"step", "threshold"} {
		if _, ok := args[key]; !ok {
			t.Errorf("LoopDetected payload missing %q key; args=%v", key, args)
		}
	}

	// The core invariant of Story 70.2: nothing was written into the context.
	after, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage after: %v", err)
	}
	if after != before {
		t.Errorf("LoopWarning appended %d message(s) to the context (%d → %d); Story 70.2 requires the warning to stay out of the prompt entirely",
			after-before, before, after)
	}
}

// LoopSuspend is the branch that DOES act on the process. Guarding it here
// keeps the two branches from being conflated by a future edit: the event must
// carry the doubled threshold, and the process must be stopped.
func TestATDD_70_2_LoopSuspend_EmitsDoubledThresholdAndStops(t *testing.T) {
	k, ctxMgr, proc := newLoopDetectionFixture(t)

	before, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage before: %v", err)
	}

	stopped := k.handleLoopDetection(proc, LoopSuspend, 30, "coarse", 61, time.Now())
	if !stopped {
		t.Error("LoopSuspend must report the process as stopped")
	}

	args := awaitSyscall(t, proc, "LoopSuspend", time.Second)
	if args == nil {
		t.Fatal("LoopSuspend did not emit a LoopSuspend event")
	}
	if got, ok := args["threshold"].(int); !ok || got != 60 {
		t.Errorf("LoopSuspend threshold = %v, want 60 (2 × 30)", args["threshold"])
	}

	// Suspension is also a silent-to-the-LLM path: the context must not gain a
	// message here either. This is the accepted trade-off recorded in CLAUDE.md
	// — if someone later decides the LLM should learn why it was suspended,
	// this assertion is the one to revisit deliberately.
	after, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage after: %v", err)
	}
	if after != before {
		t.Errorf("LoopSuspend appended %d message(s) to the context; suspension is expected to be silent to the LLM", after-before)
	}
}

// LoopNone must be inert: no event, no context write, no stop.
func TestATDD_70_2_LoopNone_IsInert(t *testing.T) {
	k, ctxMgr, proc := newLoopDetectionFixture(t)

	before, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage before: %v", err)
	}

	if stopped := k.handleLoopDetection(proc, LoopNone, 30, "", 1, time.Now()); stopped {
		t.Error("LoopNone must not stop the process")
	}

	select {
	case ev := <-proc.DebugChan:
		t.Errorf("LoopNone emitted an unexpected event: %s", ev.Syscall)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing emitted
	}

	after, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage after: %v", err)
	}
	if after != before {
		t.Errorf("LoopNone wrote %d message(s) to the context", after-before)
	}
}
