package skillpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/skills"
)

// --- ATDD RED Phase: Story 8.4 — skill list 本地 Skill 注册表 ---

// createTestSkillDir creates a skill directory with a valid SKILL.md file for testing.
func createTestSkillDir(t *testing.T, basePath, name, description string) {
	t.Helper()
	dir := filepath.Join(basePath, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create skill dir %q: %v", name, err)
	}
	content := fmt.Sprintf(`---
name: %s
description: "%s"
allowed-tools: /dev/fs
metadata:
  author: test
  version: "1.0.0"
---

# %s

Test skill instructions.
`, name, description, name)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md for %q: %v", name, err)
	}
}

// TestInstaller_ListAll_BuiltinOnly verifies that ListAll returns builtin skills
// from the filesystem that are not in the registry.
// AC #1: 输出表格包含系统自带 Skill
func TestInstaller_ListAll_BuiltinOnly(t *testing.T) {
	// Given: a basePath with two skill directories but no registry entries
	dir := t.TempDir()
	createTestSkillDir(t, dir, "code-analysis", "Analyze code quality and patterns")
	createTestSkillDir(t, dir, "doc-generator", "Generate documentation from code")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: should return 2 entries, both with Source="builtin"
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Verify sorted by name
	if entries[0].Name != "code-analysis" {
		t.Errorf("expected first entry 'code-analysis', got %q", entries[0].Name)
	}
	if entries[1].Name != "doc-generator" {
		t.Errorf("expected second entry 'doc-generator', got %q", entries[1].Name)
	}

	// Verify all are builtin
	for _, e := range entries {
		if e.Source != "builtin" {
			t.Errorf("expected Source 'builtin' for %q, got %q", e.Name, e.Source)
		}
		if e.Path == "" {
			t.Errorf("expected non-empty Path for %q", e.Name)
		}
	}

	// Verify description is loaded from SKILL.md
	if entries[0].Description != "Analyze code quality and patterns" {
		t.Errorf("expected description 'Analyze code quality and patterns', got %q", entries[0].Description)
	}
}

// TestInstaller_ListAll_Mixed verifies ListAll aggregates both registry (community) and filesystem (builtin) skills.
// AC #1: 包含系统自带 Skill 和社区安装的 Skill
func TestInstaller_ListAll_Mixed(t *testing.T) {
	// Given: basePath has two skill dirs, one is registered as community
	dir := t.TempDir()
	createTestSkillDir(t, dir, "builtin-skill", "A builtin skill")
	createTestSkillDir(t, dir, "community-skill", "A community skill")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)
	registry := singleScopeRegistry(installer, dir)

	// Register the community skill in the registry
	regEntry := RegistryEntry{
		Name:    "community-skill",
		Version: "2.1.0",
		Source:  "community",
	}
	if err := registry.Add(regEntry); err != nil {
		t.Fatalf("failed to add registry entry: %v", err)
	}

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: should return 2 entries with correct sources
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Find each entry
	var builtinEntry, communityEntry *ListEntry
	for i := range entries {
		switch entries[i].Name {
		case "builtin-skill":
			builtinEntry = &entries[i]
		case "community-skill":
			communityEntry = &entries[i]
		}
	}

	if builtinEntry == nil {
		t.Fatal("expected builtin-skill in results")
	}
	if builtinEntry.Source != "builtin" {
		t.Errorf("expected builtin-skill Source 'builtin', got %q", builtinEntry.Source)
	}

	if communityEntry == nil {
		t.Fatal("expected community-skill in results")
	}
	if communityEntry.Source != "community" {
		t.Errorf("expected community-skill Source 'community', got %q", communityEntry.Source)
	}
	if communityEntry.Version != "2.1.0" {
		t.Errorf("expected community-skill Version '2.1.0', got %q", communityEntry.Version)
	}
	if communityEntry.Description != "A community skill" {
		t.Errorf("expected community-skill Description 'A community skill', got %q", communityEntry.Description)
	}
}

// TestInstaller_ListAll_Empty verifies ListAll returns an empty slice (not nil) for empty basePath.
// AC #3: JSON 输出时空 skills 数组为 [] 而非 null
func TestInstaller_ListAll_Empty(t *testing.T) {
	// Given: an empty basePath with no skill directories
	dir := t.TempDir()

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: should return non-nil empty slice
	if entries == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

// TestInstaller_ListAll_InvalidSkillSkipped verifies ListAll skips directories without valid SKILL.md.
// Edge case: 目录存在但 SKILL.md 无效时跳过（不崩溃）
func TestInstaller_ListAll_InvalidSkillSkipped(t *testing.T) {
	// Given: basePath has one valid and one invalid skill directory
	dir := t.TempDir()
	createTestSkillDir(t, dir, "valid-skill", "A valid skill")

	// Create invalid skill dir (no SKILL.md)
	invalidDir := filepath.Join(dir, "invalid-skill")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatalf("create invalid dir: %v", err)
	}

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: should return only the valid skill (invalid one skipped)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (invalid skipped), got %d", len(entries))
	}
	if entries[0].Name != "valid-skill" {
		t.Errorf("expected 'valid-skill', got %q", entries[0].Name)
	}
}

// TestInstaller_ListAll_RegisteredSkillWithCorruptedSKILLMD verifies that a registered (community) skill
// whose directory exists but SKILL.md is corrupted/missing is still listed from registry data.
// Bug fix: previously, seen[name] was set before LoadMetadata error check, hiding registry entries.
func TestInstaller_ListAll_RegisteredSkillWithCorruptedSKILLMD(t *testing.T) {
	// Given: basePath has a registered community skill directory without valid SKILL.md
	dir := t.TempDir()

	// Create a directory for the community skill but with no SKILL.md (corrupted state)
	corruptDir := filepath.Join(dir, "corrupt-community")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("create corrupt dir: %v", err)
	}

	// Also create a valid builtin skill for comparison
	createTestSkillDir(t, dir, "valid-builtin", "A valid builtin skill")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)
	registry := singleScopeRegistry(installer, dir)

	// Register the community skill
	if err := registry.Add(RegistryEntry{
		Name:    "corrupt-community",
		Version: "1.5.0",
		Source:  "community",
	}); err != nil {
		t.Fatalf("add registry entry: %v", err)
	}

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: should return 2 entries (valid builtin + corrupt community from registry)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}

	// Find the corrupt-community entry
	var corruptEntry *ListEntry
	for i := range entries {
		if entries[i].Name == "corrupt-community" {
			corruptEntry = &entries[i]
			break
		}
	}
	if corruptEntry == nil {
		t.Fatal("expected corrupt-community to still appear in list (from registry data)")
	}
	if corruptEntry.Version != "1.5.0" {
		t.Errorf("expected version '1.5.0', got %q", corruptEntry.Version)
	}
	if corruptEntry.Source != "community" {
		t.Errorf("expected source 'community', got %q", corruptEntry.Source)
	}
	if corruptEntry.Description != "" {
		t.Errorf("expected empty description for corrupted skill, got %q", corruptEntry.Description)
	}
}

// TestInstaller_ListAll_SortedByName verifies that ListAll returns entries sorted alphabetically by name.
// AC #1: 输出表格按名称排序
func TestInstaller_ListAll_SortedByName(t *testing.T) {
	// Given: skills in non-alphabetical directory order
	dir := t.TempDir()
	createTestSkillDir(t, dir, "zebra-skill", "Z skill")
	createTestSkillDir(t, dir, "alpha-skill", "A skill")
	createTestSkillDir(t, dir, "middle-skill", "M skill")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: entries should be sorted alphabetically
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	expected := []string{"alpha-skill", "middle-skill", "zebra-skill"}
	for i, name := range expected {
		if entries[i].Name != name {
			t.Errorf("entry[%d] expected %q, got %q", i, name, entries[i].Name)
		}
	}
}

// TestInstaller_ListAll_PathField verifies the Path field is set to the relative path.
// AC #1: PATH 列包含 skill 路径
func TestInstaller_ListAll_PathField(t *testing.T) {
	// Given: a skill in basePath
	dir := t.TempDir()
	createTestSkillDir(t, dir, "test-skill", "A test skill")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: Path should contain the skill name
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Path == "" {
		t.Error("expected non-empty Path")
	}
}

// TestInstaller_ListAll_RegistrySkipsDotFiles verifies that .registry.yaml and hidden files are not listed.
// Edge case: 不列出 .registry.yaml 等非 skill 目录
func TestInstaller_ListAll_RegistrySkipsDotFiles(t *testing.T) {
	// Given: basePath has a skill dir and .registry.yaml will exist after Add()
	dir := t.TempDir()
	createTestSkillDir(t, dir, "real-skill", "A real skill")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dir})
	installer := newSingleScopeInstaller(client, skillLoader, dir)
	registry := singleScopeRegistry(installer, dir)

	// Create a registry entry to ensure .registry.yaml exists
	if err := registry.Add(RegistryEntry{Name: "real-skill", Version: "1.0.0", Source: "community"}); err != nil {
		t.Fatalf("add registry entry: %v", err)
	}

	// When: calling ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: only the real skill should be listed
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "real-skill" {
		t.Errorf("expected 'real-skill', got %q", entries[0].Name)
	}
}
