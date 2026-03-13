package agents

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/skills"
)

// ============================================================
// ATDD RED PHASE — Story 21.4: Skill Synergy 声明与自动检测
//
// AgentInfo.SystemPrompt() synergy 集成测试。
// 测试引用 skills.DetectSynergies 和 SystemPrompt() 中的
// synergy 注入逻辑，这些尚不存在。
// 测试将无法编译直到实现完成。
//
// RED → GREEN: 修改 agents/types.go 的 SystemPrompt() 方法，
//              调用 skills.DetectSynergies() 并追加结果，
//              测试编译并通过。
// ============================================================

// --- 21.4-INT-001: [P0] SystemPrompt with synergy match appends [Skill Synergy] section (AC2) ---

func TestAgentInfo_SystemPrompt_WithSynergy(t *testing.T) {
	// Given: an agent with two skills that have matching synergy
	agent := &AgentInfo{
		Instructions: "You are a code review agent.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name: "code-analysis",
					Synergies: []skills.SynergyDecl{
						{
							With:        "code-review",
							Instruction: "Cross-reference analysis findings in review comments.",
						},
					},
				},
				Body: "# Code Analysis\n\nAnalyze code quality.",
			},
			{
				Manifest: skills.SkillManifest{
					Name: "code-review",
				},
				Body: "# Code Review\n\nReview code changes.",
			},
		},
	}

	// When: building system prompt
	prompt := agent.SystemPrompt()

	// Then: prompt contains [Skill Synergy] section
	if !strings.Contains(prompt, "[Skill Synergy]") {
		t.Fatal("expected SystemPrompt to contain [Skill Synergy] section when synergy matches")
	}

	// And: prompt contains the synergy instruction
	if !strings.Contains(prompt, "Cross-reference analysis findings in review comments.") {
		t.Fatal("expected SystemPrompt to contain synergy instruction")
	}

	// And: prompt still contains original content
	if !strings.Contains(prompt, "You are a code review agent.") {
		t.Fatal("expected SystemPrompt to still contain agent instructions")
	}
	if !strings.Contains(prompt, "# Code Analysis") {
		t.Fatal("expected SystemPrompt to still contain skill body")
	}
}

// --- 21.4-INT-002: [P0] SystemPrompt without synergy - output unchanged (AC5) ---

func TestAgentInfo_SystemPrompt_NoSynergy(t *testing.T) {
	// Given: an agent with skills that have no synergy declarations
	agent := &AgentInfo{
		Instructions: "You are a helpful agent.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{Name: "basic-skill"},
				Body:     "# Basic Skill\n\nDo basic things.",
			},
		},
	}

	// When: building system prompt
	prompt := agent.SystemPrompt()

	// Then: prompt does NOT contain [Skill Synergy] section
	if strings.Contains(prompt, "[Skill Synergy]") {
		t.Fatal("expected SystemPrompt to NOT contain [Skill Synergy] when no synergies match")
	}

	// And: prompt contains normal content only
	expected := "You are a helpful agent.\n\n# Basic Skill\n\nDo basic things."
	if prompt != expected {
		t.Errorf("SystemPrompt = %q, want %q", prompt, expected)
	}
}

// --- 21.4-INT-003: [P0] SystemPrompt with synergy dedup - duplicate instruction appears once (AC3) ---

func TestAgentInfo_SystemPrompt_SynergyDedup(t *testing.T) {
	// Given: two skills with identical synergy instruction
	sameInstruction := "Both skills should cooperate on security analysis."
	agent := &AgentInfo{
		Instructions: "Agent instructions.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name: "skill-a",
					Synergies: []skills.SynergyDecl{
						{With: "skill-b", Instruction: sameInstruction},
					},
				},
				Body: "Body A.",
			},
			{
				Manifest: skills.SkillManifest{
					Name: "skill-b",
					Synergies: []skills.SynergyDecl{
						{With: "skill-a", Instruction: sameInstruction},
					},
				},
				Body: "Body B.",
			},
		},
	}

	// When: building system prompt
	prompt := agent.SystemPrompt()

	// Then: instruction appears exactly once
	count := strings.Count(prompt, sameInstruction)
	if count != 1 {
		t.Fatalf("expected synergy instruction to appear 1 time, got %d times", count)
	}
}

// --- 21.4-INT-004: [P1] SystemPrompt with no skills - no synergy section (AC5) ---

func TestAgentInfo_SystemPrompt_NoSkills_NoSynergySection(t *testing.T) {
	// Given: an agent with no skills
	agent := &AgentInfo{
		Instructions: "Bare agent.",
		Skills:       nil,
	}

	// When: building system prompt
	prompt := agent.SystemPrompt()

	// Then: no [Skill Synergy] section
	if strings.Contains(prompt, "[Skill Synergy]") {
		t.Fatal("expected no [Skill Synergy] section when no skills loaded")
	}
	if prompt != "Bare agent." {
		t.Errorf("SystemPrompt = %q, want %q", prompt, "Bare agent.")
	}
}

// --- 21.4-INT-005: [P1] SystemPrompt synergy does not affect AllowedTools (AC2) ---

func TestAgentInfo_AllowedTools_UnaffectedBySynergy(t *testing.T) {
	// Given: an agent with synergy-enabled skills
	agent := &AgentInfo{
		Instructions: "Instructions.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "skill-a",
					AllowedToolsRaw: "/dev/fs",
					Synergies: []skills.SynergyDecl{
						{With: "skill-b", Instruction: "Synergy instruction."},
					},
				},
			},
			{
				Manifest: skills.SkillManifest{
					Name:            "skill-b",
					AllowedToolsRaw: "/dev/shell",
				},
			},
		},
	}

	// When: getting allowed tools
	tools := agent.AllowedTools()

	// Then: tools are from skill manifests only (synergy does not add tools)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(tools), tools)
	}
	// Tools are sorted
	if tools[0] != "/dev/fs" {
		t.Errorf("tools[0] = %q, want %q", tools[0], "/dev/fs")
	}
	if tools[1] != "/dev/shell" {
		t.Errorf("tools[1] = %q, want %q", tools[1], "/dev/shell")
	}
}

// --- 21.4-INT-006: [P0] SystemPrompt with multiple synergy matches - all appended (AC3) ---

func TestAgentInfo_SystemPrompt_MultipleSynergyMatches(t *testing.T) {
	// Given: three skills with cross-synergies
	agent := &AgentInfo{
		Instructions: "Agent.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name: "analysis",
					Synergies: []skills.SynergyDecl{
						{With: "review", Instruction: "Analysis+Review: cross-reference."},
						{With: "audit", Instruction: "Analysis+Audit: security focus."},
					},
				},
				Body: "Analysis body.",
			},
			{
				Manifest: skills.SkillManifest{
					Name: "review",
					Synergies: []skills.SynergyDecl{
						{With: "analysis", Instruction: "Review+Analysis: prioritize flagged."},
					},
				},
				Body: "Review body.",
			},
			{
				Manifest: skills.SkillManifest{Name: "audit"},
				Body:     "Audit body.",
			},
		},
	}

	// When: building system prompt
	prompt := agent.SystemPrompt()

	// Then: all 3 synergy instructions appear
	if !strings.Contains(prompt, "[Skill Synergy]") {
		t.Fatal("expected [Skill Synergy] section")
	}
	if !strings.Contains(prompt, "Analysis+Review: cross-reference.") {
		t.Error("missing instruction: Analysis+Review")
	}
	if !strings.Contains(prompt, "Analysis+Audit: security focus.") {
		t.Error("missing instruction: Analysis+Audit")
	}
	if !strings.Contains(prompt, "Review+Analysis: prioritize flagged.") {
		t.Error("missing instruction: Review+Analysis")
	}
}

// --- 21.4-INT-007: [P1] SystemPrompt synergy section appears after skill bodies (AC2) ---

func TestAgentInfo_SystemPrompt_SynergyAfterBodies(t *testing.T) {
	// Given: agent with synergy-matched skills
	agent := &AgentInfo{
		Instructions: "Base instructions.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name: "skill-a",
					Synergies: []skills.SynergyDecl{
						{With: "skill-b", Instruction: "Synergy instruction."},
					},
				},
				Body: "Skill A body content.",
			},
			{
				Manifest: skills.SkillManifest{Name: "skill-b"},
				Body:     "Skill B body content.",
			},
		},
	}

	// When: building system prompt
	prompt := agent.SystemPrompt()

	// Then: [Skill Synergy] appears after all skill bodies
	synergyIdx := strings.Index(prompt, "[Skill Synergy]")
	lastBodyIdx := strings.LastIndex(prompt, "Skill B body content.")
	if synergyIdx < 0 {
		t.Fatal("expected [Skill Synergy] in prompt")
	}
	if lastBodyIdx < 0 {
		t.Fatal("expected skill body in prompt")
	}
	if synergyIdx < lastBodyIdx {
		t.Fatalf("expected [Skill Synergy] (at %d) after last skill body (at %d)", synergyIdx, lastBodyIdx)
	}
}
