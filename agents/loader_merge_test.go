package agents

import (
	"os"
	"strings"
	"testing"

	"github.com/rnixai/rnix/skills"
)

// TestAgentLoader_Merge_ProjectFieldOverride verifies F1 (rnix-eval upstream
// finding): a project agent.yaml that sets ONLY models.preferred overrides just
// that field while inheriting name, provider, fallback, skills, and
// instructions.md from the global layer — aligning agent profiles with the
// providers.yaml project-override model. Previously this failed with
// "agent manifest missing required field: name" (no field merge) and
// "no such file: instructions.md" (no file-level fallback).
func TestAgentLoader_Merge_ProjectFieldOverride(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	// Global: full agent definition + instructions.md.
	gAgent := globalDir + "/orch"
	if err := os.MkdirAll(gAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	globalYAML := "name: orch\ndescription: global orch\nmodels:\n  provider: deepseek\n  preferred: deepseek-v4-flash\n  fallback: global-fb\nskills: []\n"
	if err := os.WriteFile(gAgent+"/agent.yaml", []byte(globalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gAgent+"/instructions.md", []byte("Global instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Project: ONLY overrides models.preferred — no name/skills/instructions.md.
	pAgent := projectDir + "/orch"
	if err := os.MkdirAll(pAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pAgent+"/agent.yaml", []byte("models:\n  preferred: deepseek-v4-pro\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sl := skills.NewSkillLoader([]string{})
	al := NewAgentLoader([]string{projectDir, globalDir}, sl, nil)

	info, err := al.Load("orch")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if info.Manifest.Models.Preferred != "deepseek-v4-pro" {
		t.Errorf("Models.Preferred = %q, want deepseek-v4-pro (project override)", info.Manifest.Models.Preferred)
	}
	if info.Manifest.Name != "orch" {
		t.Errorf("Name = %q, want orch (inherited from global)", info.Manifest.Name)
	}
	if info.Manifest.Models.Provider != "deepseek" {
		t.Errorf("Models.Provider = %q, want deepseek (inherited via merge)", info.Manifest.Models.Provider)
	}
	if info.Manifest.Models.Fallback != "global-fb" {
		t.Errorf("Models.Fallback = %q, want global-fb (inherited via merge)", info.Manifest.Models.Fallback)
	}
	if !strings.Contains(info.Instructions, "Global instructions") {
		t.Errorf("Instructions = %q, want global instructions (file-level fallback)", info.Instructions)
	}
}

// TestAgentLoader_Merge_ProjectInstructionsOverride verifies file-level
// precedence: when the project layer provides instructions.md, it wins over the
// global one (project-first), while still merging agent.yaml fields.
func TestAgentLoader_Merge_ProjectInstructionsOverride(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	gAgent := globalDir + "/a2"
	if err := os.MkdirAll(gAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gAgent+"/agent.yaml", []byte("name: a2\ndescription: global-desc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gAgent+"/instructions.md", []byte("GLOBAL BODY"), 0o644); err != nil {
		t.Fatal(err)
	}

	pAgent := projectDir + "/a2"
	if err := os.MkdirAll(pAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pAgent+"/agent.yaml", []byte("description: project-desc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pAgent+"/instructions.md", []byte("PROJECT BODY"), 0o644); err != nil {
		t.Fatal(err)
	}

	sl := skills.NewSkillLoader([]string{})
	al := NewAgentLoader([]string{projectDir, globalDir}, sl, nil)
	info, err := al.Load("a2")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !strings.Contains(info.Instructions, "PROJECT BODY") {
		t.Errorf("Instructions = %q, want PROJECT BODY (project-first)", info.Instructions)
	}
	if info.Manifest.Description != "project-desc" {
		t.Errorf("Description = %q, want project-desc (project override)", info.Manifest.Description)
	}
	if info.Manifest.Name != "a2" {
		t.Errorf("Name = %q, want a2 (inherited from global)", info.Manifest.Name)
	}
}

// TestAgentLoader_Merge_ExplicitEmptyClears verifies that under field-merge a
// project can still CLEAR an inherited global field by writing an explicit empty
// value — aligning with providers.yaml / DeepMergeYAML semantics (an omitted
// field inherits; an explicit empty value overrides). This preserves the
// "project can drop a stale global provider" capability that the pre-F1
// whole-directory replacement provided via an empty models block.
func TestAgentLoader_Merge_ExplicitEmptyClears(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	gAgent := globalDir + "/a3"
	if err := os.MkdirAll(gAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gAgent+"/agent.yaml", []byte("name: a3\nmodels:\n  provider: claude\n  preferred: sonnet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gAgent+"/instructions.md", []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}

	pAgent := projectDir + "/a3"
	if err := os.MkdirAll(pAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	// Explicit empty provider clears the inherited global provider; preferred is
	// omitted, so it still inherits.
	if err := os.WriteFile(pAgent+"/agent.yaml", []byte("models:\n  provider: \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sl := skills.NewSkillLoader([]string{})
	al := NewAgentLoader([]string{projectDir, globalDir}, sl, nil)
	info, err := al.Load("a3")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if info.Manifest.Models.Provider != "" {
		t.Errorf("Models.Provider = %q, want empty (explicit empty value clears inherited global)", info.Manifest.Models.Provider)
	}
	if info.Manifest.Models.Preferred != "sonnet" {
		t.Errorf("Models.Preferred = %q, want sonnet (omitted → inherited)", info.Manifest.Models.Preferred)
	}
}
