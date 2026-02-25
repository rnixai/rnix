package skills

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestParseSKILLMD_Valid(t *testing.T) {
	content := "---\nname: test\n---\n\n# Body\n\nSome content."
	fm, body, err := parseSKILLMD(content, true)
	if err != nil {
		t.Fatalf("parseSKILLMD returned error: %v", err)
	}
	if !strings.Contains(fm, "name: test") {
		t.Errorf("frontmatter = %q, want to contain 'name: test'", fm)
	}
	if !strings.Contains(body, "# Body") {
		t.Errorf("body = %q, want to contain '# Body'", body)
	}
}

func TestParseSKILLMD_MissingOpeningSep(t *testing.T) {
	_, _, err := parseSKILLMD("name: test\n---\nbody", true)
	if err == nil {
		t.Fatal("expected error for missing opening ---")
	}
	if !strings.Contains(err.Error(), "must start with ---") {
		t.Errorf("error = %q, want 'must start with ---'", err.Error())
	}
}

func TestParseSKILLMD_MissingClosingSep(t *testing.T) {
	_, _, err := parseSKILLMD("---\nname: test\nbody without closing", true)
	if err == nil {
		t.Fatal("expected error for missing closing ---")
	}
	if !strings.Contains(err.Error(), "missing closing ---") {
		t.Errorf("error = %q, want 'missing closing ---'", err.Error())
	}
}

func TestSkillManifest_AllowedTools(t *testing.T) {
	m := SkillManifest{AllowedToolsRaw: "/dev/fs /dev/shell"}
	tools := m.AllowedTools()
	if len(tools) != 2 {
		t.Fatalf("AllowedTools length = %d, want 2", len(tools))
	}
	if tools[0] != "/dev/fs" {
		t.Errorf("AllowedTools[0] = %q, want %q", tools[0], "/dev/fs")
	}
	if tools[1] != "/dev/shell" {
		t.Errorf("AllowedTools[1] = %q, want %q", tools[1], "/dev/shell")
	}
}

func TestSkillManifest_AllowedTools_Empty(t *testing.T) {
	m := SkillManifest{AllowedToolsRaw: ""}
	tools := m.AllowedTools()
	if tools != nil {
		t.Errorf("AllowedTools = %v, want nil for empty raw", tools)
	}
}

func TestSkillLoader_LoadFull_Success(t *testing.T) {
	loader := NewSkillLoader("testdata")
	info, err := loader.LoadFull("mock-skill")
	if err != nil {
		t.Fatalf("LoadFull returned error: %v", err)
	}

	// Verify manifest fields.
	if info.Manifest.Name != "mock-skill" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "mock-skill")
	}
	if info.Manifest.Description != "A mock skill for testing" {
		t.Errorf("Description = %q, want %q", info.Manifest.Description, "A mock skill for testing")
	}
	tools := info.Manifest.AllowedTools()
	if len(tools) != 2 {
		t.Fatalf("AllowedTools length = %d, want 2", len(tools))
	}
	if tools[0] != "/dev/fs" {
		t.Errorf("AllowedTools[0] = %q, want %q", tools[0], "/dev/fs")
	}
	if tools[1] != "/dev/shell" {
		t.Errorf("AllowedTools[1] = %q, want %q", tools[1], "/dev/shell")
	}

	// Verify body loaded with expected content.
	if !strings.Contains(info.Body, "Mock Skill") {
		t.Errorf("Body does not contain expected content, got: %q", info.Body)
	}
	if !strings.Contains(info.Body, "Responsibilities") {
		t.Errorf("Body does not contain 'Responsibilities', got: %q", info.Body)
	}
}

func TestSkillLoader_LoadMetadata_Success(t *testing.T) {
	loader := NewSkillLoader("testdata")
	info, err := loader.LoadMetadata("mock-skill")
	if err != nil {
		t.Fatalf("LoadMetadata returned error: %v", err)
	}

	if info.Manifest.Name != "mock-skill" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "mock-skill")
	}
	if info.Body != "" {
		t.Errorf("Body should be empty for LoadMetadata, got: %q", info.Body)
	}
}

func TestSkillLoader_LoadFull_DirNotFound(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.LoadFull("nonexistent-skill")
	if err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist in chain, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestSkillLoader_LoadFull_SKILLMDNotFound(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.LoadFull("no-instructions")
	if err == nil {
		t.Fatal("expected error for missing SKILL.md, got nil")
	}
	if !strings.Contains(err.Error(), "SKILL.md") {
		t.Errorf("error should mention 'SKILL.md', got: %v", err)
	}
}

func TestSkillLoader_LoadFull_InvalidFrontmatter(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.LoadFull("invalid-manifest")
	if err == nil {
		t.Fatal("expected error for invalid frontmatter YAML, got nil")
	}
}

func TestSkillLoader_LoadFull_MissingRequiredFields(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.LoadFull("missing-fields")
	if err == nil {
		t.Fatal("expected error for missing Name field, got nil")
	}
	expected := `missing required field: name`
	if !strings.Contains(err.Error(), expected) {
		t.Errorf("error = %q, want substring %q", err.Error(), expected)
	}
}

func TestSkillLoader_LoadFull_RealCodeAnalysis(t *testing.T) {
	loader := NewSkillLoader("../lib/skills")
	info, err := loader.LoadFull("code-analysis")
	if err != nil {
		t.Fatalf("LoadFull returned error: %v", err)
	}

	if info.Manifest.Name != "code-analysis" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "code-analysis")
	}

	tools := info.Manifest.AllowedTools()
	if len(tools) != 2 {
		t.Fatalf("AllowedTools length = %d, want 2", len(tools))
	}
	if tools[0] != "/dev/fs" {
		t.Errorf("AllowedTools[0] = %q, want %q", tools[0], "/dev/fs")
	}
	if tools[1] != "/dev/shell" {
		t.Errorf("AllowedTools[1] = %q, want %q", tools[1], "/dev/shell")
	}

	if info.Body == "" {
		t.Fatal("Body is empty")
	}
	keywords := []string{"Code Analysis", "/dev/fs", "/dev/shell", "Critical", "Warning", "Info"}
	for _, kw := range keywords {
		if !strings.Contains(info.Body, kw) {
			t.Errorf("Body missing keyword %q", kw)
		}
	}
}

func TestSkillLoader_LoadFull_Metadata(t *testing.T) {
	loader := NewSkillLoader("testdata")
	info, err := loader.LoadFull("mock-skill")
	if err != nil {
		t.Fatalf("LoadFull returned error: %v", err)
	}
	if info.Manifest.Metadata == nil {
		t.Fatal("Metadata is nil")
	}
	if info.Manifest.Metadata["author"] != "test" {
		t.Errorf("Metadata[author] = %q, want %q", info.Manifest.Metadata["author"], "test")
	}
	if info.Manifest.Metadata["version"] != "1.0" {
		t.Errorf("Metadata[version] = %q, want %q", info.Manifest.Metadata["version"], "1.0")
	}
}

func TestParseSKILLMD_MetadataOnly(t *testing.T) {
	content := "---\nname: test\n---\n\n# Body\n\nSome content."
	fm, body, err := parseSKILLMD(content, false)
	if err != nil {
		t.Fatalf("parseSKILLMD returned error: %v", err)
	}
	if !strings.Contains(fm, "name: test") {
		t.Errorf("frontmatter = %q, want to contain 'name: test'", fm)
	}
	if body != "" {
		t.Errorf("body should be empty for metadata-only mode, got: %q", body)
	}
}

func TestSkillLoader_LoadFull_PathTraversal(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.LoadFull("../../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "path escapes") {
		t.Errorf("error = %q, want substring 'path escapes'", err.Error())
	}
}

func TestSkillLoader_LoadMetadata_PathTraversal(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.LoadMetadata("../../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "path escapes") {
		t.Errorf("error = %q, want substring 'path escapes'", err.Error())
	}
}
