package kernel

import (
	"fmt"
	"testing"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
)

func TestLoopDetector_NoLoop(t *testing.T) {
	d := NewLoopDetector(3, 5)
	for i := range 10 {
		status := d.Check(uint64(i)) // all different hashes
		if status != LoopNone {
			t.Errorf("step %d: expected LoopNone, got %d", i, status)
		}
	}
}

func TestLoopDetector_Warning(t *testing.T) {
	d := NewLoopDetector(3, 5)
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
	d := NewLoopDetector(3, 5)
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
	d := NewLoopDetector(3, 5)
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
	d := NewLoopDetector(3, 5)
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
	d := NewLoopDetector(3, 5)
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
	d := NewLoopDetector(0, 0)
	if d.threshold != DefaultLoopThreshold {
		t.Errorf("expected default threshold %d, got %d", DefaultLoopThreshold, d.threshold)
	}
	if d.coarseThreshold != DefaultCoarseLoopThreshold {
		t.Errorf("expected default coarse threshold %d, got %d", DefaultCoarseLoopThreshold, d.coarseThreshold)
	}
	if d.fineDisabled || d.coarseDisabled {
		t.Error("zero thresholds must mean default, not disabled")
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

// TestActionHash_HashesFullInput is the inverted form of the old
// TestActionHash_TruncatesInput (Story 70.1 AC8/AC9-⑤). The truncation it used to
// assert WAS the defect: two different commands sharing a long prefix collided
// into one hash, which the detector then read as a repeated action. The observed
// incident had 24 bytes of headroom below the old 256-byte cutoff.
func TestActionHash_HashesFullInput(t *testing.T) {
	longInput := make([]byte, 500)
	for i := range longInput {
		longInput[i] = 'a'
	}
	// Identical for the first 300 bytes, different only after the old cutoff.
	input1 := string(longInput)
	longInput[300] = 'b'
	input2 := string(longInput)
	h1 := ActionHash("tool_call", "/dev/shell", input1)
	h2 := ActionHash("tool_call", "/dev/shell", input2)
	if h1 == h2 {
		t.Error("inputs differing after byte 256 must produce DIFFERENT hashes (no truncation)")
	}
}

func TestLoopDetector_CheckDual_CoarseWarning(t *testing.T) {
	d := NewLoopDetector(3, 5)
	coarseHash := CoarseActionHash("tool_call", "/dev/fs")
	// The CONSTANT result hash is this case's premise, not incidental setup.
	// Since Story 70.1 the result is part of both tracks' criteria, so varying it
	// here would (correctly) suppress detection and the case would test nothing.
	const constResult = uint64(0xC0FFEE)

	var gotWarning bool
	for i := range d.coarseThreshold {
		fineHash := ActionHash("tool_call", "/dev/fs", fmt.Sprintf("input-%d", i))
		status := d.CheckDual(fineHash, coarseHash, constResult)
		if status == LoopWarning {
			gotWarning = true
			if i != d.coarseThreshold-1 {
				t.Errorf("coarse warning at step %d, expected step %d", i, d.coarseThreshold-1)
			}
		}
	}
	if !gotWarning {
		t.Errorf("expected coarse LoopWarning after %d same-tool same-result steps with different inputs", d.coarseThreshold)
	}
}

func TestLoopDetector_CheckDual_CoarseSuspend(t *testing.T) {
	d := NewLoopDetector(3, 5)
	coarseHash := CoarseActionHash("tool_call", "/dev/fs")
	// Constant result hash — see the note in the CoarseWarning case above.
	const constResult = uint64(0xC0FFEE)

	var gotSuspend bool
	for i := range 2 * d.coarseThreshold {
		fineHash := ActionHash("tool_call", "/dev/fs", fmt.Sprintf("input-%d", i))
		status := d.CheckDual(fineHash, coarseHash, constResult)
		if status == LoopSuspend {
			gotSuspend = true
		}
	}
	if !gotSuspend {
		t.Errorf("expected coarse LoopSuspend after %d same-tool same-result steps", 2*d.coarseThreshold)
	}
}

func TestLoopDetector_CheckDual_FineStillWorks(t *testing.T) {
	d := NewLoopDetector(3, 5)
	hash := uint64(42)
	const constResult = uint64(7)

	var gotWarning bool
	for i := range 3 {
		status := d.CheckDual(hash, hash, constResult)
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
	d := NewLoopDetector(3, 5)

	for i := range 2 * d.coarseThreshold {
		tool := fmt.Sprintf("/dev/tool-%d", i)
		fineHash := ActionHash("tool_call", tool, "input")
		coarseHash := CoarseActionHash("tool_call", tool)
		status := d.CheckDual(fineHash, coarseHash, uint64(i))
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

// ============================================================================
// Story 70.1 New Test Cases — Five Groups
// ============================================================================

// TestLoopDetector_FalsePositive_40DifferentSteps verifies AC9-① (误判反证):
// 40 consecutive tool_call steps with distinct (action, result) pairs must never
// trigger either track, even though the old粗粒度 hash was a constant for every
// `Bash` call and would have suspended at step 30 with the old ceiling.
//
// RED form: Under the old code (粗粒度 hash = actionType+toolPath only, no result),
// this case red-lights at step 15 (coarse warning with the old DefaultCoarse=15),
// then at step 30 (coarse suspend with the old DefaultCoarse=15).
//
// GREEN form: With the result criterion added, the coarse track sees 40 *different*
// mixed hashes (different results), so neither track fires.
func TestLoopDetector_FalsePositive_40DifferentSteps(t *testing.T) {
	// Use explicit small thresholds so the test runs fast and doesn't rely on
	// the new raised defaults (陷阱一：真空 PASS).
	d := NewLoopDetector(3, 5)
	coarseHash := CoarseActionHash("tool_call", "Bash")

	for i := range 40 {
		fineHash := ActionHash("tool_call", "Bash", fmt.Sprintf("echo step-%d", i))
		// Each step produces a DIFFERENT result hash — the key fix.
		resultHash := uint64(0xBADA550 + uint64(i))
		status := d.CheckDual(fineHash, coarseHash, resultHash)
		if status != LoopNone {
			t.Errorf("step %d: expected LoopNone for 40 different (action,result) pairs, got %v", i, status)
		}
	}
}

// TestLoopDetector_RealLoop_StillCaught verifies AC9-② (真循环仍被捕获):
// The result criterion must not disable detection when a genuine loop exists.
// Two sub-cases: fine-grain (same tool+input+result) and coarse-grain (same
// tool+result, input varies).
func TestLoopDetector_RealLoop_Fine(t *testing.T) {
	d := NewLoopDetector(3, 5)
	hash := uint64(42)
	const constResult = uint64(0xDEADBEEF)

	for i := range 10 {
		status := d.CheckDual(hash, hash, constResult)
		if i == 2 && status != LoopWarning {
			t.Errorf("fine: expected LoopWarning at step %d (threshold=3), got %v", i, status)
		}
		if i == 5 && status != LoopSuspend {
			t.Errorf("fine: expected LoopSuspend at step %d (2*3), got %v", i, status)
			return
		}
	}
}

func TestLoopDetector_RealLoop_Coarse(t *testing.T) {
	d := NewLoopDetector(3, 5)
	coarseHash := CoarseActionHash("tool_call", "Bash")
	const constResult = uint64(0xC0FFEE)

	for i := range 12 {
		// Input varies, but result is CONSTANT — the LLM is thrashing.
		fineHash := ActionHash("tool_call", "Bash", fmt.Sprintf("attempt-%d", i))
		status := d.CheckDual(fineHash, coarseHash, constResult)
		if i == 4 && status != LoopWarning {
			t.Errorf("coarse: expected LoopWarning at step %d (coarse threshold=5), got %v", i, status)
		}
		if i >= 9 && status == LoopSuspend {
			// 2*5 = 10, so suspend at step 9 (0-indexed) or 10 (1-indexed).
			return
		}
	}
	t.Error("coarse: expected LoopSuspend after 10 same-tool same-result steps with varying input")
}

// TestLoopDetector_PollProgression_NoFalseKill verifies AC9-③ (poll 递进不误杀):
// 20 consecutive calls to the same command, each result ≥300 bytes with an
// identical prefix but a PROGRESSIVELY CHANGING tail (e.g., elapsed time counter),
// must not trigger either track. The old 256-byte truncation would have made
// every result hash identical, firing the coarse track at step 5 with coarse=5.
//
// RED form: If ActionHash or ToolResultHash truncates at 256 bytes, every result
// hashes identically → coarse track fires at step 5.
//
// GREEN form: Full hashing sees the tail differences → LoopNone throughout.
func TestLoopDetector_PollProgression_NoFalseKill(t *testing.T) {
	d := NewLoopDetector(3, 5)
	actionHash := ActionHash("tool_call", "Bash", "rnix ps 12345")
	coarseHash := CoarseActionHash("tool_call", "Bash")

	// Build 20 results with identical 300-byte prefix + unique tail.
	prefix := make([]byte, 300)
	for i := range prefix {
		prefix[i] = 'X'
	}

	for i := range 20 {
		// Result = 300-byte prefix + tail showing progression.
		var tail string
		if i < 19 {
			tail = fmt.Sprintf("RUNNING (elapsed=%ds)", i*30)
		} else {
			tail = "EXITED exit=0"
		}
		result := string(prefix) + tail

		// Compute result hash from the simulated ToolCallRecord.
		rec := []types.ToolCallRecord{{Name: "Bash", Result: result}}
		resultHash := ToolResultHash(rec)

		status := d.CheckDual(actionHash, coarseHash, resultHash)
		if status != LoopNone {
			t.Errorf("poll step %d: expected LoopNone for progressing result (tail=%q), got %v", i, tail, status)
		}
	}
}

// TestLoopDetector_DisableSemantics verifies AC9-④ (禁用/默认 green-guard):
// A negative threshold must disable that track without panic, and a zero threshold
// must fall back to the default.
func TestLoopDetector_DisableBothTracks(t *testing.T) {
	d := NewLoopDetector(-1, -1)
	hash := uint64(42)
	const constResult = uint64(7)

	// 100 identical steps must remain LoopNone when both tracks disabled.
	for i := range 100 {
		status := d.CheckDual(hash, hash, constResult)
		if status != LoopNone {
			t.Errorf("disabled: step %d expected LoopNone, got %v", i, status)
		}
	}
}

func TestLoopDetector_ZeroMeansDefault(t *testing.T) {
	d := NewLoopDetector(0, 0)
	if d.threshold != DefaultLoopThreshold {
		t.Errorf("0 threshold must fall back to DefaultLoopThreshold (%d), got %d", DefaultLoopThreshold, d.threshold)
	}
	if d.coarseThreshold != DefaultCoarseLoopThreshold {
		t.Errorf("0 coarse must fall back to DefaultCoarseLoopThreshold (%d), got %d", DefaultCoarseLoopThreshold, d.coarseThreshold)
	}
	if d.fineDisabled || d.coarseDisabled {
		t.Error("0 threshold must mean default, not disabled")
	}
}

// TestToolResultHash_EmptyBatch verifies that an empty batch hashes to the
// sentinel 0, and a non-empty batch incorporates Name, Result, and Error.
func TestToolResultHash_EmptyBatch(t *testing.T) {
	h := ToolResultHash(nil)
	if h != 0 {
		t.Errorf("empty batch must hash to 0, got %d", h)
	}
	h = ToolResultHash([]types.ToolCallRecord{})
	if h != 0 {
		t.Errorf("empty slice must hash to 0, got %d", h)
	}
}

func TestToolResultHash_IncorporatesError(t *testing.T) {
	rec1 := []types.ToolCallRecord{{Name: "Bash", Result: "ok", Error: ""}}
	rec2 := []types.ToolCallRecord{{Name: "Bash", Result: "ok", Error: "timeout"}}
	h1 := ToolResultHash(rec1)
	h2 := ToolResultHash(rec2)
	if h1 == h2 {
		t.Error("result hash must differ when Error differs (same command, same error = loop)")
	}
}

// TestApplyLoopThresholds_ThreeTierResolution verifies AC5 / 陷阱四: the
// SpawnOpts > manifest > default priority, and critically that the resolution
// uses `!= 0` (NOT `> 0`). Under `> 0`, a -1 ("disable this track") at the opts
// or manifest tier would be misread as "unset" and silently fall through to the
// next tier, re-enabling detection with no error. These cases pin that behaviour.
func TestApplyLoopThresholds_ThreeTierResolution(t *testing.T) {
	agentWith := func(loop, coarse int) *agents.AgentInfo {
		return &agents.AgentInfo{Manifest: agents.AgentManifest{
			LoopThreshold:       loop,
			CoarseLoopThreshold: coarse,
		}}
	}

	cases := []struct {
		name                 string
		optsLoop, optsCoarse int
		agent                *agents.AgentInfo
		wantLoop, wantCoarse int
	}{
		{"opts wins over manifest", 7, 9, agentWith(3, 5), 7, 9},
		{"manifest wins when opts zero", 0, 0, agentWith(3, 5), 3, 5},
		{"zero everywhere stays zero (default resolved at read time)", 0, 0, agentWith(0, 0), 0, 0},
		{"negative opts passthrough — the != 0 trap", -1, -1, agentWith(3, 5), -1, -1},
		{"negative manifest passthrough", 0, 0, agentWith(-1, -1), -1, -1},
		{"nil agent, opts set", 7, 9, nil, 7, 9},
		{"nil agent, opts zero stays zero", 0, 0, nil, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proc := &Process{}
			applyLoopThresholds(proc, tc.agent, SpawnOpts{
				LoopThreshold:       tc.optsLoop,
				CoarseLoopThreshold: tc.optsCoarse,
			})
			if proc.LoopThreshold != tc.wantLoop {
				t.Errorf("LoopThreshold = %d, want %d", proc.LoopThreshold, tc.wantLoop)
			}
			if proc.CoarseLoopThreshold != tc.wantCoarse {
				t.Errorf("CoarseLoopThreshold = %d, want %d", proc.CoarseLoopThreshold, tc.wantCoarse)
			}
		})
	}
}

// TestEffectiveLoopThreshold_NegativePassthrough verifies the Process-level
// helpers: 0 maps to the default, but a negative value is returned AS-IS (it is
// the "disable" signal NewLoopDetector owns). A `> 0` gate here would swallow -1
// and return the default, silently re-enabling the track — the last line of
// defense for the disable semantics.
func TestEffectiveLoopThreshold_NegativePassthrough(t *testing.T) {
	cases := []struct {
		name         string
		loop, coarse int
		wantLoop     int
		wantCoarse   int
	}{
		{"zero falls back to defaults", 0, 0, DefaultLoopThreshold, DefaultCoarseLoopThreshold},
		{"positive passes through", 7, 9, 7, 9},
		{"negative passes through (disable signal)", -1, -1, -1, -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Process{LoopThreshold: tc.loop, CoarseLoopThreshold: tc.coarse}
			if got := p.effectiveLoopThreshold(); got != tc.wantLoop {
				t.Errorf("effectiveLoopThreshold() = %d, want %d", got, tc.wantLoop)
			}
			if got := p.effectiveCoarseLoopThreshold(); got != tc.wantCoarse {
				t.Errorf("effectiveCoarseLoopThreshold() = %d, want %d", got, tc.wantCoarse)
			}
		})
	}
}
