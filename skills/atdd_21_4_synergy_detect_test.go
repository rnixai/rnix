package skills

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 21.4: Skill Synergy 声明与自动检测
//
// DetectSynergies 函数测试。
// 测试引用 DetectSynergies 函数（skills/synergy.go），
// 该函数尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 新建 skills/synergy.go 并实现 DetectSynergies，
//              测试编译并通过。
// ============================================================

// --- 21.4-UNIT-007: [P0] DetectSynergies - no synergies declared (AC2) ---

func TestDetectSynergies_NoSynergies(t *testing.T) {
	// Given: skills without synergy declarations
	skills := []*SkillInfo{
		{Manifest: SkillManifest{Name: "skill-a"}},
		{Manifest: SkillManifest{Name: "skill-b"}},
	}

	// When: detecting synergies
	result := DetectSynergies(skills)

	// Then: empty result
	if len(result) != 0 {
		t.Fatalf("expected 0 synergy instructions, got %d: %v", len(result), result)
	}
}

// --- 21.4-UNIT-008: [P0] DetectSynergies - single match (AC2) ---

func TestDetectSynergies_SingleMatch(t *testing.T) {
	// Given: skill A declares synergy with skill B, both loaded
	skills := []*SkillInfo{
		{
			Manifest: SkillManifest{
				Name: "skill-a",
				Synergies: []SynergyDecl{
					{With: "skill-b", Instruction: "When both A and B are active, combine their outputs."},
				},
			},
		},
		{Manifest: SkillManifest{Name: "skill-b"}},
	}

	// When: detecting synergies
	result := DetectSynergies(skills)

	// Then: 1 instruction returned
	if len(result) != 1 {
		t.Fatalf("expected 1 synergy instruction, got %d: %v", len(result), result)
	}
	if result[0] != "When both A and B are active, combine their outputs." {
		t.Errorf("result[0] = %q, want expected instruction", result[0])
	}
}

// --- 21.4-UNIT-009: [P0] DetectSynergies - bidirectional match (AC2/AC3) ---

func TestDetectSynergies_BidirectionalMatch(t *testing.T) {
	// Given: A→B and B→A synergy declarations, both loaded
	skills := []*SkillInfo{
		{
			Manifest: SkillManifest{
				Name: "skill-a",
				Synergies: []SynergyDecl{
					{With: "skill-b", Instruction: "A sees B: cross-reference findings."},
				},
			},
		},
		{
			Manifest: SkillManifest{
				Name: "skill-b",
				Synergies: []SynergyDecl{
					{With: "skill-a", Instruction: "B sees A: prioritize flagged files."},
				},
			},
		},
	}

	// When: detecting synergies
	result := DetectSynergies(skills)

	// Then: both instructions returned
	if len(result) != 2 {
		t.Fatalf("expected 2 synergy instructions, got %d: %v", len(result), result)
	}
}

// --- 21.4-UNIT-010: [P0] DetectSynergies - partial load, target not present (AC2) ---

func TestDetectSynergies_PartialLoad(t *testing.T) {
	// Given: skill A declares synergy with skill B, but B is NOT loaded
	skills := []*SkillInfo{
		{
			Manifest: SkillManifest{
				Name: "skill-a",
				Synergies: []SynergyDecl{
					{With: "skill-b", Instruction: "This should not match."},
				},
			},
		},
		{Manifest: SkillManifest{Name: "skill-c"}}, // not skill-b
	}

	// When: detecting synergies
	result := DetectSynergies(skills)

	// Then: no match (target not loaded)
	if len(result) != 0 {
		t.Fatalf("expected 0 synergy instructions (target not loaded), got %d: %v", len(result), result)
	}
}

// --- 21.4-UNIT-011: [P0] DetectSynergies - multiple matches across skills (AC3) ---

func TestDetectSynergies_MultipleMatches(t *testing.T) {
	// Given: multiple cross-referencing synergies
	skills := []*SkillInfo{
		{
			Manifest: SkillManifest{
				Name: "code-analysis",
				Synergies: []SynergyDecl{
					{With: "code-review", Instruction: "Instruction A-to-Review."},
					{With: "security-audit", Instruction: "Instruction A-to-Security."},
				},
			},
		},
		{
			Manifest: SkillManifest{
				Name: "code-review",
				Synergies: []SynergyDecl{
					{With: "code-analysis", Instruction: "Instruction Review-to-A."},
				},
			},
		},
		{Manifest: SkillManifest{Name: "security-audit"}},
	}

	// When: detecting synergies
	result := DetectSynergies(skills)

	// Then: all 3 instructions matched
	if len(result) != 3 {
		t.Fatalf("expected 3 synergy instructions, got %d: %v", len(result), result)
	}
}

// --- 21.4-UNIT-012: [P0] DetectSynergies - deduplication (AC3) ---

func TestDetectSynergies_Dedup(t *testing.T) {
	// Given: two skills declare the SAME instruction
	sameInstruction := "Shared synergy instruction for both."
	skills := []*SkillInfo{
		{
			Manifest: SkillManifest{
				Name: "skill-a",
				Synergies: []SynergyDecl{
					{With: "skill-b", Instruction: sameInstruction},
				},
			},
		},
		{
			Manifest: SkillManifest{
				Name: "skill-b",
				Synergies: []SynergyDecl{
					{With: "skill-a", Instruction: sameInstruction},
				},
			},
		},
	}

	// When: detecting synergies
	result := DetectSynergies(skills)

	// Then: instruction appears only once (deduped)
	if len(result) != 1 {
		t.Fatalf("expected 1 deduped instruction, got %d: %v", len(result), result)
	}
	if result[0] != sameInstruction {
		t.Errorf("result[0] = %q, want %q", result[0], sameInstruction)
	}
}

// --- 21.4-UNIT-013: [P0] DetectSynergies - deterministic order (AC3) ---

func TestDetectSynergies_DeterministicOrder(t *testing.T) {
	// Given: multiple synergy instructions
	skills := []*SkillInfo{
		{
			Manifest: SkillManifest{
				Name: "skill-a",
				Synergies: []SynergyDecl{
					{With: "skill-b", Instruction: "Zebra instruction."},
				},
			},
		},
		{
			Manifest: SkillManifest{
				Name: "skill-b",
				Synergies: []SynergyDecl{
					{With: "skill-a", Instruction: "Alpha instruction."},
				},
			},
		},
	}

	// When: detecting synergies multiple times
	result1 := DetectSynergies(skills)
	result2 := DetectSynergies(skills)

	// Then: results are sorted alphabetically and deterministic
	if len(result1) != 2 {
		t.Fatalf("expected 2 instructions, got %d", len(result1))
	}
	if result1[0] != "Alpha instruction." {
		t.Errorf("result[0] = %q, want %q (alphabetical order)", result1[0], "Alpha instruction.")
	}
	if result1[1] != "Zebra instruction." {
		t.Errorf("result[1] = %q, want %q (alphabetical order)", result1[1], "Zebra instruction.")
	}
	// Deterministic: same result on repeated calls
	for i := range result1 {
		if result1[i] != result2[i] {
			t.Errorf("non-deterministic: result1[%d]=%q != result2[%d]=%q", i, result1[i], i, result2[i])
		}
	}
}

// --- 21.4-UNIT-014: [P1] DetectSynergies - nil input returns empty (AC5) ---

func TestDetectSynergies_NilInput(t *testing.T) {
	// Given: nil skills slice
	result := DetectSynergies(nil)

	// Then: empty result, no panic
	if len(result) != 0 {
		t.Fatalf("expected 0 instructions for nil input, got %d", len(result))
	}
}

// --- 21.4-UNIT-015: [P1] DetectSynergies - empty skills slice (AC5) ---

func TestDetectSynergies_EmptySlice(t *testing.T) {
	// Given: empty skills slice
	result := DetectSynergies([]*SkillInfo{})

	// Then: empty result
	if len(result) != 0 {
		t.Fatalf("expected 0 instructions for empty slice, got %d", len(result))
	}
}

// --- 21.4-PERF-001: [P1] DetectSynergies performance - 100 skills x 10 synergies < 100ms (AC4) ---

func TestDetectSynergies_Performance(t *testing.T) {
	// Given: 100 skills each with 10 synergy declarations
	allSkills := make([]*SkillInfo, 100)
	for i := range 100 {
		synergies := make([]SynergyDecl, 10)
		for j := range 10 {
			targetIdx := (i + j + 1) % 100
			synergies[j] = SynergyDecl{
				With:        fmt.Sprintf("skill-%03d", targetIdx),
				Instruction: fmt.Sprintf("Instruction from skill-%03d to skill-%03d.", i, targetIdx),
			}
		}
		allSkills[i] = &SkillInfo{
			Manifest: SkillManifest{
				Name:      fmt.Sprintf("skill-%03d", i),
				Synergies: synergies,
			},
		}
	}

	// When: measuring detection time
	start := time.Now()
	result := DetectSynergies(allSkills)
	elapsed := time.Since(start)

	// Then: completes within 100ms (NFR46)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("DetectSynergies took %v, want < 100ms", elapsed)
	}

	// And: result is non-empty (sanity check)
	if len(result) == 0 {
		t.Fatal("expected non-empty result for 100 skills with synergies")
	}
}
