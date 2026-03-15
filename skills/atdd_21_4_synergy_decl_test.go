package skills

import (
	"testing"

	"github.com/goccy/go-yaml"
)

// ============================================================
// ATDD RED PHASE — Story 21.4: Skill Synergy 声明与自动检测
//
// SkillManifest.Synergies 字段解析测试 & SynergyDecl 类型测试。
// 测试引用 SynergyDecl 类型和 SkillManifest.Synergies 字段，
// 这些类型/字段尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 在 skills/types.go 添加 SynergyDecl 类型和
//              SkillManifest.Synergies 字段，测试编译并通过。
// ============================================================

// --- 21.4-UNIT-001: [P0] SkillManifest with synergy field parsed correctly (AC1) ---

func TestSkillManifest_Synergies_Parsed(t *testing.T) {
	// Given: YAML frontmatter with synergy field
	fm := `name: test-skill-a
description: Test skill A
allowed-tools: /dev/fs
synergy:
  - with: test-skill-b
    instruction: "Cross-reference findings from analysis in review."
  - with: test-skill-c
    instruction: "Prioritize security findings."
`
	var manifest SkillManifest
	if err := yamlUnmarshal([]byte(fm), &manifest); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Then: Synergies field is correctly parsed
	if manifest.Synergies == nil {
		t.Fatal("expected non-nil Synergies")
	}
	if len(manifest.Synergies) != 2 {
		t.Fatalf("expected 2 synergies, got %d", len(manifest.Synergies))
	}
	if manifest.Synergies[0].With != "test-skill-b" {
		t.Errorf("Synergies[0].With = %q, want %q", manifest.Synergies[0].With, "test-skill-b")
	}
	if manifest.Synergies[0].Instruction != "Cross-reference findings from analysis in review." {
		t.Errorf("Synergies[0].Instruction = %q, want expected value", manifest.Synergies[0].Instruction)
	}
	if manifest.Synergies[1].With != "test-skill-c" {
		t.Errorf("Synergies[1].With = %q, want %q", manifest.Synergies[1].With, "test-skill-c")
	}
	if manifest.Synergies[1].Instruction != "Prioritize security findings." {
		t.Errorf("Synergies[1].Instruction = %q, want expected value", manifest.Synergies[1].Instruction)
	}
}

// --- 21.4-UNIT-002: [P0] SkillManifest without synergy field - backward compat (AC5) ---

func TestSkillManifest_Synergies_Empty(t *testing.T) {
	// Given: YAML frontmatter without synergy field
	fm := `name: test-skill
description: Simple skill
allowed-tools: /dev/fs
`
	var manifest SkillManifest
	if err := yamlUnmarshal([]byte(fm), &manifest); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Then: Synergies is nil (backward compatible)
	if manifest.Synergies != nil {
		t.Fatalf("expected nil Synergies when not defined, got %v", manifest.Synergies)
	}
}

// --- 21.4-UNIT-003: [P0] SynergyDecl type exists with correct fields (AC1) ---

func TestSynergyDecl_Fields(t *testing.T) {
	// Given: a SynergyDecl literal
	decl := SynergyDecl{
		With:        "code-review",
		Instruction: "Cross-reference analysis findings in review comments.",
	}

	// Then: fields are accessible
	if decl.With != "code-review" {
		t.Errorf("With = %q, want %q", decl.With, "code-review")
	}
	if decl.Instruction != "Cross-reference analysis findings in review comments." {
		t.Errorf("Instruction = %q, want expected value", decl.Instruction)
	}
}

// --- 21.4-UNIT-004: [P1] SkillLoader LoadFull with synergy field (AC1) ---

func TestSkillLoader_LoadFull_WithSynergy(t *testing.T) {
	// Given: a SKILL.md testdata with synergy field
	loader := NewSkillLoader([]string{"testdata"})
	info, err := loader.LoadFull("with-synergy")
	if err != nil {
		t.Fatalf("LoadFull returned error: %v", err)
	}

	// Then: Synergies field is populated
	if info.Manifest.Synergies == nil {
		t.Fatal("expected non-nil Synergies from with-synergy testdata")
	}
	if len(info.Manifest.Synergies) != 1 {
		t.Fatalf("expected 1 synergy, got %d", len(info.Manifest.Synergies))
	}
	if info.Manifest.Synergies[0].With != "test-skill-b" {
		t.Errorf("Synergies[0].With = %q, want %q", info.Manifest.Synergies[0].With, "test-skill-b")
	}
	if info.Manifest.Synergies[0].Instruction == "" {
		t.Error("Synergies[0].Instruction should not be empty")
	}
}

// --- 21.4-UNIT-005: [P1] SkillLoader LoadFull without synergy - backward compat (AC5) ---

func TestSkillLoader_LoadFull_WithoutSynergy(t *testing.T) {
	// Given: existing mock-skill testdata without synergy
	loader := NewSkillLoader([]string{"testdata"})
	info, err := loader.LoadFull("mock-skill")
	if err != nil {
		t.Fatalf("LoadFull returned error: %v", err)
	}

	// Then: Synergies is nil (backward compatible)
	if info.Manifest.Synergies != nil {
		t.Fatalf("expected nil Synergies for mock-skill without synergy field, got %v", info.Manifest.Synergies)
	}

	// And: existing fields still work
	if info.Manifest.Name != "mock-skill" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "mock-skill")
	}
}

// --- 21.4-UNIT-006: [P1] Real code-analysis skill loads without synergy error (AC5) ---

func TestSkillLoader_LoadFull_RealSkill_NoSynergyField(t *testing.T) {
	// Given: real code-analysis skill without synergy field
	loader := NewSkillLoader([]string{"../lib/skills"})
	info, err := loader.LoadFull("code-analysis")
	if err != nil {
		t.Fatalf("LoadFull returned error: %v", err)
	}

	// Then: no error, Synergies is nil
	if info.Manifest.Synergies != nil {
		t.Fatalf("expected nil Synergies for real code-analysis skill, got %v", info.Manifest.Synergies)
	}
	// And: existing fields unchanged
	if info.Manifest.Name != "code-analysis" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "code-analysis")
	}
}

// yamlUnmarshal is a test helper to unmarshal YAML using go-yaml.
func yamlUnmarshal(data []byte, v any) error {
	// Use the same go-yaml library as production code
	return yaml.Unmarshal(data, v)
}
