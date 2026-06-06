package kernel

import "testing"

// TestProcess_MarkFailedChild verifies F2 (rnix-eval upstream finding): a parent
// process counts children that exited non-zero so ActionComplete can reflect
// child failures in the parent's own exit code, rather than reporting success
// (exit 0) while spawned sub-tasks failed — the "fake success" terminal state.
func TestProcess_MarkFailedChild(t *testing.T) {
	p := NewProcess(0, "parent", nil)
	if p.failedChildren != 0 {
		t.Fatalf("new process failedChildren = %d, want 0", p.failedChildren)
	}
	p.MarkFailedChild()
	p.MarkFailedChild()
	if p.failedChildren != 2 {
		t.Errorf("failedChildren = %d, want 2 after two MarkFailedChild calls", p.failedChildren)
	}
}
