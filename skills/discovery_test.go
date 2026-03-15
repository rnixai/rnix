package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// ATDD Story 20.3: Stem Agent & Auto-Differentiation
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================

// --- Task 1: Skill Discovery & Metadata Scanning (AC: #1) ---

func TestSkillDiscovery_DiscoverAll(t *testing.T) {
	// Given a testdata directory with valid skills
	loader := NewSkillLoader([]string{"testdata"})
	discovery := NewSkillDiscovery(loader, []string{"testdata"})

	// When DiscoverAll is called
	skills, err := discovery.DiscoverAll()

	// Then it returns valid skill list without error
	if err != nil {
		t.Fatalf("DiscoverAll returned error: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("DiscoverAll returned empty list, expected at least one valid skill")
	}

	// Verify mock-skill is discovered
	found := false
	for _, s := range skills {
		if s.Manifest.Name == "mock-skill" {
			found = true
			if s.Manifest.Description != "A mock skill for testing" {
				t.Errorf("Description = %q, want %q", s.Manifest.Description, "A mock skill for testing")
			}
			// DiscoverAll returns metadata only, body should be empty
			if s.Body != "" {
				t.Errorf("Body should be empty for metadata-only scan, got: %q", s.Body)
			}
			break
		}
	}
	if !found {
		t.Error("mock-skill not found in DiscoverAll results")
	}
}

func TestSkillDiscovery_SkipsInvalidSkills(t *testing.T) {
	// Given a testdata directory that contains invalid SKILL.md entries
	loader := NewSkillLoader([]string{"testdata"})
	discovery := NewSkillDiscovery(loader, []string{"testdata"})

	// When DiscoverAll is called
	skills, err := discovery.DiscoverAll()

	// Then invalid skills are silently skipped (no error)
	if err != nil {
		t.Fatalf("DiscoverAll should not error on invalid skills: %v", err)
	}

	// Verify invalid-manifest and missing-fields are NOT in the results
	for _, s := range skills {
		if s.Manifest.Name == "invalid-manifest" {
			t.Error("invalid-manifest should have been skipped")
		}
		// missing-fields has empty name, so it would fail validation
	}
}

func TestSkillDiscovery_EmptyDirectory(t *testing.T) {
	// Given an empty directory
	tmpDir := t.TempDir()
	loader := NewSkillLoader([]string{tmpDir})
	discovery := NewSkillDiscovery(loader, []string{tmpDir})

	// When DiscoverAll is called
	skills, err := discovery.DiscoverAll()

	// Then it returns empty list without error
	if err != nil {
		t.Fatalf("DiscoverAll on empty dir returned error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty list for empty directory, got %d items", len(skills))
	}
}

func TestSkillDiscovery_NonexistentDirectory(t *testing.T) {
	// Given a nonexistent directory path
	loader := NewSkillLoader([]string{"/nonexistent/path"})
	discovery := NewSkillDiscovery(loader, []string{"/nonexistent/path"})

	// When DiscoverAll is called
	skills, err := discovery.DiscoverAll()

	// Then it returns empty list without error (graceful degradation)
	if err != nil {
		t.Fatalf("DiscoverAll on nonexistent dir returned error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty list for nonexistent directory, got %d items", len(skills))
	}
}

func TestSkillDiscovery_SkipsNonDirectories(t *testing.T) {
	// Given a directory that contains regular files alongside skill dirs
	tmpDir := t.TempDir()

	// Create a regular file (should be skipped)
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Skills"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a valid skill directory
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: test-skill\ndescription: \"Test skill\"\nallowed-tools: /dev/fs\n---\n\n# Test Skill Body"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewSkillLoader([]string{tmpDir})
	discovery := NewSkillDiscovery(loader, []string{tmpDir})

	// When DiscoverAll is called
	skills, err := discovery.DiscoverAll()

	// Then only the skill directory is discovered
	if err != nil {
		t.Fatalf("DiscoverAll returned error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Manifest.Name != "test-skill" {
		t.Errorf("Name = %q, want %q", skills[0].Manifest.Name, "test-skill")
	}
}

func TestSkillDiscovery_SkipsHiddenDirectories(t *testing.T) {
	// Given a directory that contains hidden directories
	tmpDir := t.TempDir()

	// Create a hidden directory (should be skipped)
	hiddenDir := filepath.Join(tmpDir, ".hidden-skill")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hiddenContent := "---\nname: hidden\ndescription: \"Hidden\"\n---\n\n# Hidden"
	if err := os.WriteFile(filepath.Join(hiddenDir, "SKILL.md"), []byte(hiddenContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewSkillLoader([]string{tmpDir})
	discovery := NewSkillDiscovery(loader, []string{tmpDir})

	// When DiscoverAll is called
	skills, err := discovery.DiscoverAll()

	// Then hidden directories are skipped
	if err != nil {
		t.Fatalf("DiscoverAll returned error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills (hidden dirs skipped), got %d", len(skills))
	}
}
