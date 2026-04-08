package agents

import (
	"testing"

	"github.com/rnixai/rnix/skills"
)

func TestInstructionSections_SplitsCorrectly(t *testing.T) {
	agent := &AgentInfo{
		Instructions: "Agent instructions here",
		Skills: []*skills.SkillInfo{
			{Manifest: skills.SkillManifest{Name: "s1"}, Body: "Skill 1 body"},
			{Manifest: skills.SkillManifest{Name: "s2"}, Body: "Skill 2 body", Dir: "/skills/s2"},
		},
	}

	instr, bodies := agent.InstructionSections()
	if instr != "Agent instructions here" {
		t.Errorf("instructions = %q", instr)
	}
	if bodies == "" {
		t.Fatal("skill bodies should not be empty")
	}
	if !containsStr(bodies, "Skill 1 body") {
		t.Error("bodies should contain skill 1")
	}
	if !containsStr(bodies, "Skill 2 body") {
		t.Error("bodies should contain skill 2")
	}
	if !containsStr(bodies, "Base directory for this skill: /skills/s2") {
		t.Error("bodies should contain dir hint for s2")
	}
}

func TestInstructionSections_NoSkills(t *testing.T) {
	agent := &AgentInfo{
		Instructions: "Just instructions",
		Skills:       nil,
	}
	instr, bodies := agent.InstructionSections()
	if instr != "Just instructions" {
		t.Errorf("instructions = %q", instr)
	}
	if bodies != "" {
		t.Errorf("bodies should be empty, got %q", bodies)
	}
}

func TestInstructionSections_EmptyBodySkipped(t *testing.T) {
	agent := &AgentInfo{
		Instructions: "test",
		Skills: []*skills.SkillInfo{
			{Manifest: skills.SkillManifest{Name: "empty"}, Body: ""},
			{Manifest: skills.SkillManifest{Name: "real"}, Body: "content"},
		},
	}
	_, bodies := agent.InstructionSections()
	if bodies != "content" {
		t.Errorf("bodies = %q, want 'content'", bodies)
	}
}

func TestDeferredSkillsField(t *testing.T) {
	agent := &AgentInfo{
		Manifest: AgentManifest{
			Name:           "test",
			DeferredSkills: []string{"skill-a", "skill-b"},
		},
	}
	if len(agent.Manifest.DeferredSkills) != 2 {
		t.Errorf("DeferredSkills len = %d, want 2", len(agent.Manifest.DeferredSkills))
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
