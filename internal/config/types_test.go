package config

import (
	"testing"
)

func TestScope_Constants(t *testing.T) {
	// Verify Scope type and constants are defined and distinct
	if ScopeGlobal == ScopeProject {
		t.Error("ScopeGlobal and ScopeProject must be distinct values")
	}
	// ScopeGlobal should be 0 (iota), ScopeProject should be 1
	if ScopeGlobal != 0 {
		t.Errorf("ScopeGlobal = %d, want 0", ScopeGlobal)
	}
	if ScopeProject != 1 {
		t.Errorf("ScopeProject = %d, want 1", ScopeProject)
	}
}

func TestGlobalConfig_Fields(t *testing.T) {
	// Verify GlobalConfig struct can be instantiated with all fields
	gc := GlobalConfig{
		Dir:       "/home/user/.config/rnix",
		Providers: nil,
		Config:    nil,
		MCP:       nil,
		AgentsDir: "/home/user/.config/rnix/agents",
		SkillsDir: "/home/user/.config/rnix/skills",
	}

	if gc.Dir != "/home/user/.config/rnix" {
		t.Errorf("GlobalConfig.Dir = %q, want %q", gc.Dir, "/home/user/.config/rnix")
	}
	if gc.AgentsDir != "/home/user/.config/rnix/agents" {
		t.Errorf("GlobalConfig.AgentsDir = %q, want %q", gc.AgentsDir, "/home/user/.config/rnix/agents")
	}
	if gc.SkillsDir != "/home/user/.config/rnix/skills" {
		t.Errorf("GlobalConfig.SkillsDir = %q, want %q", gc.SkillsDir, "/home/user/.config/rnix/skills")
	}
}

func TestProjectConfig_Fields(t *testing.T) {
	// Verify ProjectConfig struct can be instantiated with all fields
	pc := ProjectConfig{
		ProjectDir:  "/some/project",
		Providers:   nil,
		Config:      nil,
		MCP:         nil,
		AgentDirs:   []string{"/some/project/.rnix/agents", "/home/user/.config/rnix/agents"},
		SkillDirs:   []string{"/some/project/.rnix/skills", "/home/user/.config/rnix/skills"},
		InitConfig:  nil,
		ComposeSpec: nil,
	}

	if pc.ProjectDir != "/some/project" {
		t.Errorf("ProjectConfig.ProjectDir = %q, want %q", pc.ProjectDir, "/some/project")
	}
	if len(pc.AgentDirs) != 2 {
		t.Errorf("ProjectConfig.AgentDirs len = %d, want 2", len(pc.AgentDirs))
	}
	if len(pc.SkillDirs) != 2 {
		t.Errorf("ProjectConfig.SkillDirs len = %d, want 2", len(pc.SkillDirs))
	}
}
