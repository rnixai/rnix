package kernel

import (
	"fmt"
	"testing"
)

func TestLoopDetector_NoLoop(t *testing.T) {
	d := NewLoopDetector(3)
	for i := range 10 {
		status := d.Check(uint64(i)) // all different hashes
		if status != LoopNone {
			t.Errorf("step %d: expected LoopNone, got %d", i, status)
		}
	}
}

func TestLoopDetector_Warning(t *testing.T) {
	d := NewLoopDetector(3)
	hash := uint64(42)
	var gotWarning bool
	for i := range 3 {
		status := d.Check(hash)
		if status == LoopWarning {
			gotWarning = true
			if i != 2 {
				t.Errorf("warning at step %d, expected step 2", i)
			}
		}
	}
	if !gotWarning {
		t.Error("expected LoopWarning after 3 identical steps")
	}
}

func TestLoopDetector_WarningOnlyOnce(t *testing.T) {
	d := NewLoopDetector(3)
	hash := uint64(42)
	warnings := 0
	for range 5 {
		if d.Check(hash) == LoopWarning {
			warnings++
		}
	}
	if warnings != 1 {
		t.Errorf("expected exactly 1 warning, got %d", warnings)
	}
}

func TestLoopDetector_Suspend(t *testing.T) {
	d := NewLoopDetector(3)
	hash := uint64(42)
	var gotSuspend bool
	for i := range 6 {
		status := d.Check(hash)
		if status == LoopSuspend {
			gotSuspend = true
			if i != 5 {
				t.Errorf("suspend at step %d, expected step 5", i)
			}
		}
	}
	if !gotSuspend {
		t.Error("expected LoopSuspend after 6 identical steps (2*3)")
	}
}

func TestLoopDetector_ResetAfterDifferentAction(t *testing.T) {
	d := NewLoopDetector(3)
	hash := uint64(42)
	// 2 identical, then different, then 2 identical again
	d.Check(hash)
	d.Check(hash)
	d.Check(uint64(99)) // break the pattern
	d.Check(hash)
	status := d.Check(hash)
	if status != LoopNone {
		t.Errorf("expected LoopNone after pattern break, got %d", status)
	}
}

func TestLoopDetector_WarnedResetsOnPatternBreak(t *testing.T) {
	d := NewLoopDetector(3)
	hashA := uint64(42)
	hashB := uint64(77)

	// Trigger LoopWarning with pattern A
	d.Check(hashA)
	d.Check(hashA)
	status := d.Check(hashA)
	if status != LoopWarning {
		t.Fatalf("expected LoopWarning on 3rd identical step, got %d", status)
	}

	// Break pattern
	d.Check(uint64(99))

	// Start new pattern B — should get a fresh LoopWarning
	d.Check(hashB)
	d.Check(hashB)
	status = d.Check(hashB)
	if status != LoopWarning {
		t.Errorf("expected LoopWarning for new pattern after break, got %d", status)
	}
}

func TestLoopDetector_DefaultThreshold(t *testing.T) {
	d := NewLoopDetector(0)
	if d.threshold != DefaultLoopThreshold {
		t.Errorf("expected default threshold %d, got %d", DefaultLoopThreshold, d.threshold)
	}
}

func TestActionHash_Deterministic(t *testing.T) {
	h1 := ActionHash("tool_call", "/dev/shell", "ls -la")
	h2 := ActionHash("tool_call", "/dev/shell", "ls -la")
	if h1 != h2 {
		t.Error("same inputs should produce same hash")
	}
}

func TestActionHash_DifferentInputs(t *testing.T) {
	h1 := ActionHash("tool_call", "/dev/shell", "ls -la")
	h2 := ActionHash("tool_call", "/dev/shell", "cat file.txt")
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestActionHash_TruncatesInput(t *testing.T) {
	longInput := make([]byte, 500)
	for i := range longInput {
		longInput[i] = 'a'
	}
	// Same first 256 bytes, different after
	input1 := string(longInput)
	longInput[300] = 'b'
	input2 := string(longInput)
	h1 := ActionHash("tool_call", "/dev/shell", input1)
	h2 := ActionHash("tool_call", "/dev/shell", input2)
	if h1 != h2 {
		t.Error("inputs differing only after 256 bytes should produce same hash")
	}
}

func TestLoopDetector_CheckDual_CoarseWarning(t *testing.T) {
	d := NewLoopDetector(3)
	coarseHash := CoarseActionHash("tool_call", "/dev/fs")

	var gotWarning bool
	for i := range DefaultCoarseLoopThreshold {
		fineHash := ActionHash("tool_call", "/dev/fs", fmt.Sprintf("input-%d", i))
		status := d.CheckDual(fineHash, coarseHash)
		if status == LoopWarning {
			gotWarning = true
			if i != DefaultCoarseLoopThreshold-1 {
				t.Errorf("coarse warning at step %d, expected step %d", i, DefaultCoarseLoopThreshold-1)
			}
		}
	}
	if !gotWarning {
		t.Errorf("expected coarse LoopWarning after %d same-tool steps with different inputs", DefaultCoarseLoopThreshold)
	}
}

func TestLoopDetector_CheckDual_CoarseSuspend(t *testing.T) {
	d := NewLoopDetector(3)
	coarseHash := CoarseActionHash("tool_call", "/dev/fs")

	var gotSuspend bool
	for i := range 2 * DefaultCoarseLoopThreshold {
		fineHash := ActionHash("tool_call", "/dev/fs", fmt.Sprintf("input-%d", i))
		status := d.CheckDual(fineHash, coarseHash)
		if status == LoopSuspend {
			gotSuspend = true
		}
	}
	if !gotSuspend {
		t.Errorf("expected coarse LoopSuspend after %d same-tool steps", 2*DefaultCoarseLoopThreshold)
	}
}

func TestLoopDetector_CheckDual_FineStillWorks(t *testing.T) {
	d := NewLoopDetector(3)
	hash := uint64(42)

	var gotWarning bool
	for i := range 3 {
		status := d.CheckDual(hash, hash)
		if status == LoopWarning {
			gotWarning = true
			if i != 2 {
				t.Errorf("fine warning at step %d, expected step 2", i)
			}
		}
	}
	if !gotWarning {
		t.Error("expected fine-grain LoopWarning after 3 identical steps via CheckDual")
	}
}

func TestLoopDetector_CheckDual_DifferentToolsNoTrigger(t *testing.T) {
	d := NewLoopDetector(3)

	for i := range DefaultCoarseLoopThreshold {
		tool := fmt.Sprintf("/dev/tool-%d", i)
		fineHash := ActionHash("tool_call", tool, "input")
		coarseHash := CoarseActionHash("tool_call", tool)
		status := d.CheckDual(fineHash, coarseHash)
		if status != LoopNone {
			t.Errorf("step %d: expected LoopNone for different tools, got %d", i, status)
		}
	}
}

func TestCoarseActionHash_IgnoresInput(t *testing.T) {
	h1 := CoarseActionHash("tool_call", "/dev/fs")
	h2 := CoarseActionHash("tool_call", "/dev/fs")
	if h1 != h2 {
		t.Error("same (actionType, toolPath) should produce same coarse hash")
	}

	fine1 := ActionHash("tool_call", "/dev/fs", "input-a")
	fine2 := ActionHash("tool_call", "/dev/fs", "input-b")
	if fine1 == fine2 {
		t.Error("different inputs should produce different fine hashes")
	}
}

func TestLoopWarningMessage(t *testing.T) {
	msg := LoopWarningMessage(10)
	if msg == "" {
		t.Error("expected non-empty warning message")
	}
}
