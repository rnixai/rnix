package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGlobal_CreatesDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "rnix")

	err := initGlobal(globalDir)
	if err != nil {
		t.Fatalf("initGlobal failed: %v", err)
	}

	for _, sub := range []string{"agents", "skills"} {
		dir := filepath.Join(globalDir, sub)
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", sub)
		}
	}
}

func TestInitGlobal_GeneratesProvidersYAML(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "rnix")

	err := initGlobal(globalDir)
	if err != nil {
		t.Fatalf("initGlobal failed: %v", err)
	}

	providersPath := filepath.Join(globalDir, "providers.yaml")
	data, err := os.ReadFile(providersPath)
	if err != nil {
		t.Fatalf("providers.yaml not found: %v", err)
	}
	if len(data) == 0 {
		t.Error("providers.yaml is empty")
	}

	content := string(data)
	if !strings.Contains(content, "claude") {
		t.Error("providers.yaml missing claude provider")
	}
}

func TestInitGlobal_GeneratesConfigYAML(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "rnix")

	err := initGlobal(globalDir)
	if err != nil {
		t.Fatalf("initGlobal failed: %v", err)
	}

	configPath := filepath.Join(globalDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config.yaml not found: %v", err)
	}
	if len(data) == 0 {
		t.Error("config.yaml is empty")
	}
}

func TestInitGlobal_ExtractsEmbeddedAgents(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "rnix")

	err := initGlobal(globalDir)
	if err != nil {
		t.Fatalf("initGlobal failed: %v", err)
	}

	agentsDir := filepath.Join(globalDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("failed to read agents dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("no agents extracted from embed.FS")
	}
}

func TestInitGlobal_ExtractsEmbeddedSkills(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "rnix")

	err := initGlobal(globalDir)
	if err != nil {
		t.Fatalf("initGlobal failed: %v", err)
	}

	skillsDir := filepath.Join(globalDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("failed to read skills dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("no skills extracted from embed.FS")
	}
}

func TestInitGlobal_Idempotent_NoOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "rnix")

	if err := initGlobal(globalDir); err != nil {
		t.Fatalf("first initGlobal failed: %v", err)
	}

	customContent := []byte("# user customized providers\nversion: '2'\n")
	providersPath := filepath.Join(globalDir, "providers.yaml")
	if err := os.WriteFile(providersPath, customContent, 0o644); err != nil {
		t.Fatalf("failed to write custom providers: %v", err)
	}

	if err := initGlobal(globalDir); err != nil {
		t.Fatalf("second initGlobal failed: %v", err)
	}

	data, err := os.ReadFile(providersPath)
	if err != nil {
		t.Fatalf("failed to read providers.yaml after second init: %v", err)
	}
	if string(data) != string(customContent) {
		t.Error("initGlobal overwrote user-modified providers.yaml")
	}
}

func TestInitProject_CreatesDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()

	err := initProject(tmpDir)
	if err != nil {
		t.Fatalf("initProject failed: %v", err)
	}

	for _, sub := range []string{".rnix/agents", ".rnix/skills", ".rnix/data", ".rnix/state"} {
		dir := filepath.Join(tmpDir, sub)
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", sub)
		}
	}
}

func TestInitProject_GeneratesConfigYAML(t *testing.T) {
	tmpDir := t.TempDir()

	err := initProject(tmpDir)
	if err != nil {
		t.Fatalf("initProject failed: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".rnix", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf(".rnix/config.yaml not found: %v", err)
	}
	if len(data) == 0 {
		t.Error(".rnix/config.yaml is empty")
	}
}

func TestInitProject_Idempotent_SkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()

	if err := initProject(tmpDir); err != nil {
		t.Fatalf("first initProject failed: %v", err)
	}

	customContent := []byte("# user custom config\nfoo: bar\n")
	configPath := filepath.Join(tmpDir, ".rnix", "config.yaml")
	if err := os.WriteFile(configPath, customContent, 0o644); err != nil {
		t.Fatalf("failed to write custom config: %v", err)
	}

	if err := initProject(tmpDir); err != nil {
		t.Fatalf("second initProject failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.yaml after second init: %v", err)
	}
	if string(data) != string(customContent) {
		t.Error("initProject overwrote user-modified config.yaml")
	}
}

func TestInitProject_DotRnixIsFile_NotTreatedAsDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .rnix as a FILE, not a directory
	rnixFile := filepath.Join(tmpDir, ".rnix")
	if err := os.WriteFile(rnixFile, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	// initProject should fail because .rnix is a file blocking MkdirAll
	err := initProject(tmpDir)
	if err == nil {
		t.Error("initProject should fail when .rnix is a file, got nil")
	}
}

func TestResolveDataDir_NewPath(t *testing.T) {
	tmpDir := t.TempDir()
	// No old path exists, should return new path
	got := resolveDataDir(tmpDir, "records")
	want := filepath.Join(tmpDir, ".rnix", "data", "records")
	if got != want {
		t.Errorf("resolveDataDir = %q, want %q", got, want)
	}
}

func TestResolveDataDir_OldPathFallback(t *testing.T) {
	tmpDir := t.TempDir()
	// Create old path
	oldPath := filepath.Join(tmpDir, ".rnix", "records")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveDataDir(tmpDir, "records")
	if got != oldPath {
		t.Errorf("resolveDataDir = %q, want old path %q", got, oldPath)
	}
}

func TestLoadInitConfigCompat_CWDFirst(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create CWD rnix-init.yaml
	cwdInit := filepath.Join(tmpDir, "rnix-init.yaml")
	if err := os.WriteFile(cwdInit, []byte("services: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadInitConfigCompat(tmpDir, globalDir)
	if err != nil {
		t.Fatalf("loadInitConfigCompat failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestLoadInitConfigCompat_GlobalFallback(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// No CWD init file, but create global init.yaml
	globalInit := filepath.Join(globalDir, "init.yaml")
	if err := os.WriteFile(globalInit, []byte("services: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadInitConfigCompat(tmpDir, globalDir)
	if err != nil {
		t.Fatalf("loadInitConfigCompat failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestLoadInitConfigCompat_DefaultWhenNoneExist(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadInitConfigCompat(tmpDir, globalDir)
	if err != nil {
		t.Fatalf("loadInitConfigCompat failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil default config")
	}
}

func TestWriteDefaultProviders_SkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()

	customContent := []byte("# custom\n")
	providersPath := filepath.Join(tmpDir, "providers.yaml")
	if err := os.WriteFile(providersPath, customContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeDefaultProviders(tmpDir); err != nil {
		t.Fatalf("writeDefaultProviders failed: %v", err)
	}

	data, err := os.ReadFile(providersPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(customContent) {
		t.Error("writeDefaultProviders overwrote existing file")
	}
}

func TestWriteDefaultConfig_SkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()

	customContent := []byte("# custom config\n")
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, customContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeDefaultConfig(tmpDir); err != nil {
		t.Fatalf("writeDefaultConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(customContent) {
		t.Error("writeDefaultConfig overwrote existing file")
	}
}
