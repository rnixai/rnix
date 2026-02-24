package skills

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gonewx/crux/kernel"
)

func TestLoadYAML_Success(t *testing.T) {
	path := filepath.Join("testdata", "mock-skill", "manifest.yaml")
	m, err := LoadYAML[SkillManifest](path)
	if err != nil {
		t.Fatalf("LoadYAML returned error: %v", err)
	}
	if m.Name != "code-analyst" {
		t.Errorf("Name = %q, want %q", m.Name, "code-analyst")
	}
	if m.ContextBudget != 4096 {
		t.Errorf("ContextBudget = %d, want %d", m.ContextBudget, 4096)
	}
	if m.Models.Provider != "claude" {
		t.Errorf("Models.Provider = %q, want %q", m.Models.Provider, "claude")
	}
}

func TestLoadYAML_FileNotFound(t *testing.T) {
	_, err := LoadYAML[SkillManifest]("testdata/nonexistent/manifest.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist in chain, got: %v", err)
	}
}

func TestLoadYAML_InvalidYAML(t *testing.T) {
	path := filepath.Join("testdata", "invalid-manifest", "manifest.yaml")
	_, err := LoadYAML[SkillManifest](path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestSkillLoader_Load_Success(t *testing.T) {
	loader := NewSkillLoader("testdata")
	info, err := loader.Load("mock-skill")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Verify manifest fields.
	if info.Manifest.Name != "code-analyst" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "code-analyst")
	}
	if info.Manifest.Description != "分析代码质量并识别问题" {
		t.Errorf("Description = %q, want %q", info.Manifest.Description, "分析代码质量并识别问题")
	}
	if len(info.Manifest.Tools) != 2 {
		t.Fatalf("Tools length = %d, want 2", len(info.Manifest.Tools))
	}
	if info.Manifest.Tools[0] != "/dev/fs" {
		t.Errorf("Tools[0] = %q, want %q", info.Manifest.Tools[0], "/dev/fs")
	}
	if info.Manifest.Tools[1] != "/dev/shell" {
		t.Errorf("Tools[1] = %q, want %q", info.Manifest.Tools[1], "/dev/shell")
	}
	if info.Manifest.Models.Provider != "claude" {
		t.Errorf("Models.Provider = %q, want %q", info.Manifest.Models.Provider, "claude")
	}
	if info.Manifest.Models.Preferred != "sonnet" {
		t.Errorf("Models.Preferred = %q, want %q", info.Manifest.Models.Preferred, "sonnet")
	}
	if info.Manifest.Models.Fallback != "haiku" {
		t.Errorf("Models.Fallback = %q, want %q", info.Manifest.Models.Fallback, "haiku")
	}
	if info.Manifest.ContextBudget != 4096 {
		t.Errorf("ContextBudget = %d, want %d", info.Manifest.ContextBudget, 4096)
	}

	// Verify instructions loaded.
	if info.Instructions == "" {
		t.Error("Instructions is empty, expected content")
	}
}

func TestSkillLoader_Load_DirNotFound(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.Load("nonexistent-skill")
	if err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}
	var sysErr *kernel.SyscallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("expected *kernel.SyscallError, got %T: %v", err, err)
	}
	if sysErr.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want %q", sysErr.Code, "NOT_FOUND")
	}
}

func TestSkillLoader_Load_InvalidManifest(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.Load("invalid-manifest")
	if err == nil {
		t.Fatal("expected error for invalid manifest YAML, got nil")
	}
}

func TestSkillLoader_Load_MissingRequiredFields(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.Load("missing-fields")
	if err == nil {
		t.Fatal("expected error for missing Name field, got nil")
	}
	expected := `missing required field: Name`
	if !containsSubstring(err.Error(), expected) {
		t.Errorf("error = %q, want substring %q", err.Error(), expected)
	}
}

func TestSkillLoader_Load_NoInstructions(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.Load("no-instructions")
	if err == nil {
		t.Fatal("expected error for missing instructions.md, got nil")
	}
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
