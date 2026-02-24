package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	// Verify instructions loaded with expected content.
	if !strings.Contains(info.Instructions, "Code Analyst") {
		t.Errorf("Instructions does not contain expected content, got: %q", info.Instructions)
	}
}

func TestSkillLoader_Load_DirNotFound(t *testing.T) {
	loader := NewSkillLoader("testdata")
	_, err := loader.Load("nonexistent-skill")
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
	expected := `missing required field: name`
	if !strings.Contains(err.Error(), expected) {
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

func TestSkillLoader_Load_RealCodeAnalyst(t *testing.T) {
	loader := NewSkillLoader("../lib/skills")
	info, err := loader.Load("code-analyst")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// 3.2 验证 manifest 字段值
	if info.Manifest.Name != "code-analyst" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "code-analyst")
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
	if info.Manifest.ContextBudget != 8192 {
		t.Errorf("ContextBudget = %d, want %d", info.Manifest.ContextBudget, 8192)
	}

	// 3.3 验证 instructions 非空且包含关键指令关键词
	if info.Instructions == "" {
		t.Fatal("Instructions is empty")
	}
	keywords := []string{"Code Analyst", "/dev/fs", "/dev/shell", "Critical", "Warning", "Info"}
	for _, kw := range keywords {
		if !strings.Contains(info.Instructions, kw) {
			t.Errorf("Instructions missing keyword %q", kw)
		}
	}
}