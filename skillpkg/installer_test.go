package skillpkg

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gonewx/crux/skills"
)

func TestInstaller_Install_Fresh(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	result, err := installer.Install("test-skill", InstallOpts{})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if result.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", result.Name)
	}
	if result.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", result.Version)
	}
	if !result.Fresh {
		t.Error("expected fresh install")
	}

	// Verify SKILL.md exists on disk
	skillPath := filepath.Join(dir, "test-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("SKILL.md not found after install: %v", err)
	}

	// Verify registry entry
	entry, err := registry.Get("test-skill")
	if err != nil {
		t.Fatalf("registry Get failed: %v", err)
	}
	if entry == nil {
		t.Fatal("expected registry entry")
	}
	if entry.Version != "1.0.0" {
		t.Errorf("expected registry version '1.0.0', got %q", entry.Version)
	}
	if entry.Source != "community" {
		t.Errorf("expected source 'community', got %q", entry.Source)
	}
}

func TestInstaller_Install_AlreadyInstalled(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	// First install
	_, err := installer.Install("test-skill", InstallOpts{})
	if err != nil {
		t.Fatalf("First install failed: %v", err)
	}

	// Second install without force
	_, err = installer.Install("test-skill", InstallOpts{})
	if err == nil {
		t.Fatal("expected AlreadyInstalledError")
	}
	if _, ok := err.(*AlreadyInstalledError); !ok {
		t.Fatalf("expected *AlreadyInstalledError, got %T: %v", err, err)
	}
}

func TestInstaller_Install_ForceOverwrite(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	// First install
	_, err := installer.Install("test-skill", InstallOpts{})
	if err != nil {
		t.Fatalf("First install failed: %v", err)
	}

	// Force install
	result, err := installer.Install("test-skill", InstallOpts{Force: true})
	if err != nil {
		t.Fatalf("Force install failed: %v", err)
	}
	if result.Fresh {
		t.Error("expected not fresh (overwrite)")
	}
}

func TestInstaller_Install_VerificationFailure(t *testing.T) {
	tarData := createTestTarGz(t, "bad-skill")

	mux := http.NewServeMux()
	mux.HandleFunc("/packages/bad-skill/latest.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprint(w, `version: "1.0.0"
checksum: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
`)
	})
	mux.HandleFunc("/packages/bad-skill/1.0.0.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(tarData)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	_, err := installer.Install("bad-skill", InstallOpts{})
	if err == nil {
		t.Fatal("expected verification error")
	}

	// Verify the skill directory was cleaned up (rollback)
	skillDir := filepath.Join(dir, "bad-skill")
	if _, statErr := os.Stat(skillDir); !os.IsNotExist(statErr) {
		t.Error("expected skill directory to be removed after verification failure")
	}
}

func TestInstaller_Install_SkillLoaderValidation(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	// Use a different base path for skill loader that won't have our files
	badLoader := skills.NewSkillLoader(t.TempDir())
	installer := NewInstaller(client, registry, badLoader, dir)

	_, err := installer.Install("test-skill", InstallOpts{})
	if err == nil {
		t.Fatal("expected validation error from SkillLoader")
	}
}

func TestInstaller_Install_NotFoundInRegistry(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	_, err := installer.Install("nonexistent-skill", InstallOpts{})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}
