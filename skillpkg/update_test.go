package skillpkg

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rnixai/rnix/skills"
)

// --- ATDD RED Phase: Story 8.3 — skill update 更新 ---

// setupMockRegistryWithVersion creates an httptest.Server serving a skill with a specific version.
// This enables testing version comparison logic in Update().
func setupMockRegistryWithVersion(t *testing.T, skillName, version string) (*httptest.Server, []byte, string) {
	t.Helper()
	tarData := createTestTarGz(t, skillName)
	checksum := checksumOf(tarData)

	latestYAML := fmt.Sprintf(`version: "%s"
checksum: "%s"
`, version, checksum)

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/packages/%s/latest.yaml", skillName), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(latestYAML))
	})
	mux.HandleFunc(fmt.Sprintf("/packages/%s/%s.tar.gz", skillName, version), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(tarData)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, tarData, checksum
}

// TestInstaller_Update_HasUpdate verifies that Update returns Updated=true when a new version exists.
// AC #1: 检查社区仓库中的最新兼容版本，如果有更新则下载并替换本地版本，更新本地注册表
func TestInstaller_Update_HasUpdate(t *testing.T) {
	// Given: "test-skill" is installed at v1.0.0 via a mock registry
	srvV1, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	clientV1 := NewRegistryClient(srvV1.URL, srvV1.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installerV1 := NewInstaller(clientV1, registry, skillLoader, dir)

	_, err := installerV1.Install("test-skill", InstallOpts{})
	if err != nil {
		t.Fatalf("initial install failed: %v", err)
	}

	// Verify installed at v1.0.0
	entry, err := registry.Get("test-skill")
	if err != nil {
		t.Fatalf("registry Get failed: %v", err)
	}
	if entry.Version != "1.0.0" {
		t.Fatalf("expected installed version '1.0.0', got %q", entry.Version)
	}

	// When: registry now returns v2.0.0 and we call Update
	srvV2, _, _ := setupMockRegistryWithVersion(t, "test-skill", "2.0.0")
	clientV2 := NewRegistryClient(srvV2.URL, srvV2.Client())
	installerV2 := NewInstaller(clientV2, registry, skillLoader, dir)

	result, err := installerV2.Update("test-skill", UpdateOpts{})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Then: result indicates update occurred with correct version info
	if !result.Updated {
		t.Error("expected Updated=true")
	}
	if result.OldVersion != "1.0.0" {
		t.Errorf("expected OldVersion '1.0.0', got %q", result.OldVersion)
	}
	if result.NewVersion != "2.0.0" {
		t.Errorf("expected NewVersion '2.0.0', got %q", result.NewVersion)
	}
	if result.Name != "test-skill" {
		t.Errorf("expected Name 'test-skill', got %q", result.Name)
	}

	// Verify registry was updated
	updatedEntry, err := registry.Get("test-skill")
	if err != nil {
		t.Fatalf("registry Get after update failed: %v", err)
	}
	if updatedEntry.Version != "2.0.0" {
		t.Errorf("expected registry version '2.0.0' after update, got %q", updatedEntry.Version)
	}
}

// TestInstaller_Update_AlreadyUpToDate verifies that Update returns Updated=false when versions match.
// AC #3: 已是最新版本时输出 "is already up to date"
func TestInstaller_Update_AlreadyUpToDate(t *testing.T) {
	// Given: "test-skill" is installed at v1.0.0
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	_, err := installer.Install("test-skill", InstallOpts{})
	if err != nil {
		t.Fatalf("initial install failed: %v", err)
	}

	// When: registry still returns v1.0.0 and we call Update
	result, err := installer.Update("test-skill", UpdateOpts{})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Then: result indicates no update needed
	if result.Updated {
		t.Error("expected Updated=false for same version")
	}
	if result.OldVersion != "1.0.0" {
		t.Errorf("expected OldVersion '1.0.0', got %q", result.OldVersion)
	}
	if result.NewVersion != "1.0.0" {
		t.Errorf("expected NewVersion '1.0.0', got %q", result.NewVersion)
	}
}

// TestInstaller_Update_NotInstalled verifies that Update returns an error for uninstalled skills.
// AC #1: error handling when skill not installed
func TestInstaller_Update_NotInstalled(t *testing.T) {
	// Given: no skills installed
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	// When: calling Update for a skill that is not installed
	_, err := installer.Update("nonexistent", UpdateOpts{})

	// Then: should return error
	if err == nil {
		t.Fatal("expected error for updating non-installed skill")
	}
}

// TestInstaller_UpdateAll_MultipleSkills verifies batch update of multiple installed skills.
// AC #2: 检查所有已安装 Skill 的更新，批量更新
func TestInstaller_UpdateAll_MultipleSkills(t *testing.T) {
	// Given: two skills installed at v1.0.0
	srv1, _, _ := setupMockRegistry(t, "skill-a")
	dir := t.TempDir()

	client1 := NewRegistryClient(srv1.URL, srv1.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer1 := NewInstaller(client1, registry, skillLoader, dir)

	_, err := installer1.Install("skill-a", InstallOpts{})
	if err != nil {
		t.Fatalf("install skill-a failed: %v", err)
	}

	srv2, _, _ := setupMockRegistry(t, "skill-b")
	client2 := NewRegistryClient(srv2.URL, srv2.Client())
	installer2 := NewInstaller(client2, registry, skillLoader, dir)

	_, err = installer2.Install("skill-b", InstallOpts{})
	if err != nil {
		t.Fatalf("install skill-b failed: %v", err)
	}

	// When: calling UpdateAll with a registry that returns v2.0.0 for skill-a
	// Both skills are "community" source, so both should be checked
	// For simplicity, use a server that returns the same version (v1.0.0)
	// so both come back as "already up to date"
	// We'll set up a combined mock for both skills
	mux := http.NewServeMux()
	tarDataA := createTestTarGz(t, "skill-a")
	checksumA := checksumOf(tarDataA)
	mux.HandleFunc("/packages/skill-a/latest.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprintf(w, "version: \"2.0.0\"\nchecksum: \"%s\"\n", checksumA)
	})
	mux.HandleFunc("/packages/skill-a/2.0.0.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(tarDataA)
	})

	tarDataB := createTestTarGz(t, "skill-b")
	checksumB := checksumOf(tarDataB)
	mux.HandleFunc("/packages/skill-b/latest.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		fmt.Fprintf(w, "version: \"1.0.0\"\nchecksum: \"%s\"\n", checksumB)
	})

	srvAll := httptest.NewServer(mux)
	t.Cleanup(srvAll.Close)

	clientAll := NewRegistryClient(srvAll.URL, srvAll.Client())
	installerAll := NewInstaller(clientAll, registry, skillLoader, dir)

	results, err := installerAll.UpdateAll(UpdateOpts{})
	if err != nil {
		t.Fatalf("UpdateAll failed: %v", err)
	}

	// Then: should return results for both community skills
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result from UpdateAll, got %d", len(results))
	}

	// Check that at least one result has Updated=true (skill-a: v1.0.0 -> v2.0.0)
	foundUpdated := false
	foundUpToDate := false
	for _, r := range results {
		if r.Name == "skill-a" && r.Updated {
			foundUpdated = true
			if r.OldVersion != "1.0.0" {
				t.Errorf("expected skill-a OldVersion '1.0.0', got %q", r.OldVersion)
			}
			if r.NewVersion != "2.0.0" {
				t.Errorf("expected skill-a NewVersion '2.0.0', got %q", r.NewVersion)
			}
		}
		if r.Name == "skill-b" && !r.Updated {
			foundUpToDate = true
		}
	}
	if !foundUpdated {
		t.Error("expected skill-a to have Updated=true")
	}
	if !foundUpToDate {
		t.Error("expected skill-b to have Updated=false (already up to date)")
	}
}

// TestInstaller_UpdateAll_SkipBuiltin verifies that UpdateAll skips non-community (builtin) skills.
// AC #2: 系统自带 Skill 不更新
func TestInstaller_UpdateAll_SkipBuiltin(t *testing.T) {
	// Given: a builtin skill is in the registry
	srv, _, _ := setupMockRegistry(t, "test-skill")
	dir := t.TempDir()

	client := NewRegistryClient(srv.URL, srv.Client())
	registry := NewLocalRegistry(dir)
	skillLoader := skills.NewSkillLoader(dir)
	installer := NewInstaller(client, registry, skillLoader, dir)

	// Manually add a builtin entry to the registry
	builtinEntry := RegistryEntry{
		Name:    "builtin-skill",
		Version: "1.0.0",
		Source:  "builtin",
	}
	if err := registry.Add(builtinEntry); err != nil {
		t.Fatalf("failed to add builtin entry: %v", err)
	}

	// When: calling UpdateAll
	results, err := installer.UpdateAll(UpdateOpts{})
	if err != nil {
		t.Fatalf("UpdateAll failed: %v", err)
	}

	// Then: builtin skill should not appear in results
	for _, r := range results {
		if r.Name == "builtin-skill" {
			t.Error("builtin skill should not be included in UpdateAll results")
		}
	}
}
