package skillpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gonewx/crux/skills"
)

// --- BUG-005: Skill Install Local Filesystem Check Tests ---

// writeValidSkillMD creates a valid SKILL.md file in the given directory.
func writeValidSkillMD(t *testing.T, dir, name string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", skillDir, err)
	}
	content := fmt.Sprintf(`---
name: %s
description: "A local skill"
allowed-tools: /dev/fs
metadata:
  author: local
  version: "0.0.0"
---

# %s

Local skill instructions.
`, name, name)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestInstaller_Install_LocalDirExists_NotInRegistry(t *testing.T) {
	// Given: a local skill directory with valid SKILL.md exists on filesystem
	// but is NOT registered in the registry (e.g., builtin skill).
	// When: Install is called without --force
	// Then: returns AlreadyInstalledError with version "local"
	srv, _, _ := setupMockRegistry(t, "other-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	// Create a local skill directory with valid SKILL.md (but not in registry)
	writeValidSkillMD(t, dir, "local-skill")

	_, err := installer.Install("local-skill", InstallOpts{})
	if err == nil {
		t.Fatal("expected AlreadyInstalledError for local skill")
	}

	aie, ok := err.(*AlreadyInstalledError)
	if !ok {
		t.Fatalf("expected *AlreadyInstalledError, got %T: %v", err, err)
	}
	if aie.Version != "local" {
		t.Errorf("expected version 'local', got %q", aie.Version)
	}
	if aie.Name != "local-skill" {
		t.Errorf("expected name 'local-skill', got %q", aie.Name)
	}
}

func TestInstaller_Install_LocalDirExists_ForceBypass(t *testing.T) {
	// Given: a local skill directory exists with valid SKILL.md
	// When: Install is called with --force
	// Then: bypasses local check and proceeds to network install
	srv, _, _ := setupMockRegistry(t, "local-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	// Create local skill directory
	writeValidSkillMD(t, dir, "local-skill")

	// Force install should bypass local check and succeed
	result, err := installer.Install("local-skill", InstallOpts{Force: true})
	if err != nil {
		t.Fatalf("Force install should bypass local check: %v", err)
	}
	if result.Name != "local-skill" {
		t.Errorf("expected name 'local-skill', got %q", result.Name)
	}
}

func TestInstaller_Install_LocalDirExists_InvalidSkill(t *testing.T) {
	// Given: a local directory exists for the skill name, but it does NOT
	// contain a valid SKILL.md (e.g., empty dir or corrupted file).
	// When: Install is called
	// Then: does NOT return AlreadyInstalledError, proceeds to network install
	srv, _, _ := setupMockRegistry(t, "invalid-local")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	// Create directory but WITHOUT a valid SKILL.md
	invalidDir := filepath.Join(dir, "invalid-local")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write an invalid SKILL.md (no frontmatter)
	if err := os.WriteFile(filepath.Join(invalidDir, "SKILL.md"), []byte("no frontmatter"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Should proceed to network install (not return AlreadyInstalledError)
	result, err := installer.Install("invalid-local", InstallOpts{})
	if err != nil {
		// If it fails, it should NOT be AlreadyInstalledError
		if _, ok := err.(*AlreadyInstalledError); ok {
			t.Fatal("should not return AlreadyInstalledError for invalid local skill")
		}
		// Other errors (e.g., validation failure from network-installed version) are acceptable
		t.Logf("got expected non-AlreadyInstalled error: %v", err)
		return
	}
	if result.Name != "invalid-local" {
		t.Errorf("expected name 'invalid-local', got %q", result.Name)
	}
}
