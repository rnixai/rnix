package kernel

import (
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

func TestLoopWarningMessage(t *testing.T) {
	msg := LoopWarningMessage(10)
	if msg == "" {
		t.Error("expected non-empty warning message")
	}
}
