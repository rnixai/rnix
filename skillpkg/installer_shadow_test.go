package skillpkg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/skills"
)

// ============================================================
// ATDD RED PHASE — Story 47.2: Installer 多 scope 改造
//
// 覆盖 AC3 / AC5：
//   - AC3: 同名 skill 跨 scope shadow 冲突 → 累积 ShadowWarning 到
//          LoadDiagnostics.Warnings（含 winning + shadowed 三字段）
//   - AC5: Install 写入 writeScope；Update 写回 skill 原所在 scope（不迁移）
//
// 本文件引用尚不存在的：
//   * LoadDiagnostics / ShadowWarning 类型
//   * LoadDiagnostics.Warnings 字段
//   * ShadowWarning 的 7 个字段 (SkillName / Winning*+Shadowed* 三组)
//
// RED → GREEN: 完成 AC3+AC5 实现后，本文件应编译且全部测试通过。
// ============================================================

// --- 47.2-INT-003: [P0] ShadowWarning累积含 winning + shadowed 完整元数据 (AC3) ---

func TestInstaller_ListAll_ShadowWarning(t *testing.T) {
	// Given: 4 scopes all containing skill named "shared"
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "shared", "winner")
	createTestSkillDir(t, dirs.projectAgents, "shared", "loser-p-agents")
	createTestSkillDir(t, dirs.userRnix, "shared", "loser-u-native")
	createTestSkillDir(t, dirs.userAgents, "shared", "loser-u-agents")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	// When: ListAll
	entries, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	// Then: result has exactly 1 entry — the winning shared
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (only winning skill)", len(entries))
	}

	// Diagnostics: N-1 = 3 ShadowWarning entries
	if got := len(diag.Warnings); got != 3 {
		t.Fatalf("len(diag.Warnings) = %d, want 3 (N-1 shadow events for N=4)", got)
	}

	// Each warning must reference the winning scope
	wantWinPath := filepath.Join(dirs.projectRnix, "shared")
	shadowedNS := make(map[string]bool, 3)
	for i, w := range diag.Warnings {
		if w.SkillName != "shared" {
			t.Errorf("Warnings[%d].SkillName = %q, want %q", i, w.SkillName, "shared")
		}
		if w.WinningPath != wantWinPath {
			t.Errorf("Warnings[%d].WinningPath = %q, want %q", i, w.WinningPath, wantWinPath)
		}
		if w.WinningScope != "project" || w.WinningNS != "native" {
			t.Errorf("Warnings[%d] winning scope/namespace = (%q,%q), want (project,native)",
				i, w.WinningScope, w.WinningNS)
		}
		key := w.ShadowedScope + "/" + w.ShadowedNS
		shadowedNS[key] = true
	}

	// All three shadowed scope/namespace combinations must be present exactly once
	for _, want := range []string{"project/agents", "user/native", "user/agents"} {
		if !shadowedNS[want] {
			t.Errorf("missing shadow warning for scope/namespace %q", want)
		}
	}
}

// --- 47.2-INT-004: [P1] ShadowWarning order is deterministic (AC3) ---

func TestInstaller_ListAll_ShadowWarning_Order(t *testing.T) {
	// Given: 4 scopes with "shared"
	scopes, dirs := setupFourScopes(t)
	for _, p := range dirs.allScopePaths() {
		createTestSkillDir(t, p, "shared", "any")
	}

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	_, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(diag.Warnings) != 3 {
		t.Fatalf("len(diag.Warnings) = %d, want 3", len(diag.Warnings))
	}

	// Then: warnings ordered by scope priority of the SHADOWED entry (AC3 spec)
	wantOrder := []struct{ scope, ns string }{
		{"project", "agents"},
		{"user", "native"},
		{"user", "agents"},
	}
	for i, w := range wantOrder {
		got := diag.Warnings[i]
		if got.ShadowedScope != w.scope || got.ShadowedNS != w.ns {
			t.Errorf("Warnings[%d] shadowed = (%q,%q), want (%q,%q)",
				i, got.ShadowedScope, got.ShadowedNS, w.scope, w.ns)
		}
	}
}

// --- 47.2-INT-005: [P0] Install writes to writeScope only (AC5) ---

func TestInstaller_Install_WritesToWriteScope(t *testing.T) {
	scopes, dirs := setupFourScopes(t)

	// Use project/agents (scopes[1]) as the writeScope to confirm the choice
	// is honored (vs the default highest-priority project/native).
	writeScope := scopes[1] // project/agents

	srv, _, _ := setupMockRegistry(t, "foo")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())
	installer := NewInstaller(client, skillLoader, scopes, writeScope)

	// When: install
	if _, err := installer.Install("foo", InstallOpts{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Then: foo directory exists ONLY under project/agents
	wantPath := filepath.Join(dirs.projectAgents, "foo", "SKILL.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected SKILL.md at %q (writeScope), got error: %v", wantPath, err)
	}

	for _, p := range []string{dirs.projectRnix, dirs.userRnix, dirs.userAgents} {
		bad := filepath.Join(p, "foo")
		if _, err := os.Stat(bad); err == nil {
			t.Errorf("foo unexpectedly present at non-writeScope path %q", bad)
		}
	}

	// And: only writeScope's .registry.yaml has the entry
	writeReg := installer.registries[writeScope.Path]
	entry, err := writeReg.Get("foo")
	if err != nil {
		t.Fatalf("writeScope registry.Get(foo): %v", err)
	}
	if entry == nil {
		t.Fatal("writeScope registry missing 'foo' entry")
	}

	for _, sp := range scopes {
		if sp.Path == writeScope.Path {
			continue
		}
		other := installer.registries[sp.Path]
		if other == nil {
			continue
		}
		if e, _ := other.Get("foo"); e != nil {
			t.Errorf("non-writeScope registry at %q unexpectedly contains 'foo'", sp.Path)
		}
	}
}

// --- 47.2-INT-006: [P0] Update writes back to original scope (AC5) ---

func TestInstaller_Update_StaysInSameScope(t *testing.T) {
	scopes, dirs := setupFourScopes(t)

	// Install into user/native first.
	originalScope := scopes[2] // user/native

	srv, _, _ := setupMockRegistry(t, "foo")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())

	installer := NewInstaller(client, skillLoader, scopes, originalScope)
	if _, err := installer.Install("foo", InstallOpts{}); err != nil {
		t.Fatalf("seed install into user/native: %v", err)
	}

	// Sanity: directory in user/native
	if _, err := os.Stat(filepath.Join(dirs.userRnix, "foo")); err != nil {
		t.Fatalf("expected foo in user/native after seed install: %v", err)
	}

	// Now build a fresh installer with writeScope = project/native (different
	// from where foo currently lives). Update must still write back to user/native.
	installer2 := NewInstaller(client, skillLoader, scopes, scopes[0])

	if _, err := installer2.Update("foo", UpdateOpts{Force: true}); err != nil {
		t.Fatalf("Update(foo): %v", err)
	}

	// Then: foo still in user/native, NOT migrated to project/native
	if _, err := os.Stat(filepath.Join(dirs.userRnix, "foo", "SKILL.md")); err != nil {
		t.Errorf("Update silently moved foo out of user/native: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirs.projectRnix, "foo")); err == nil {
		t.Error("Update wrote foo into writeScope (project/native); must stay in originating scope")
	}

	// And: only user/native's registry has the entry
	userReg := installer2.registries[originalScope.Path]
	if entry, _ := userReg.Get("foo"); entry == nil {
		t.Error("user/native registry lost 'foo' after Update")
	}
	projReg := installer2.registries[scopes[0].Path]
	if entry, _ := projReg.Get("foo"); entry != nil {
		t.Error("project/native registry unexpectedly contains 'foo' after Update")
	}
}

// --- 47.2-UNIT-005: [P2] ShadowWarning omitted when no conflict (AC3 boundary) ---

func TestInstaller_ListAll_NoShadowNoWarning(t *testing.T) {
	// Given: a skill that only appears in one scope — no conflict
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "uniq", "no conflict")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	// When: ListAll
	_, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	// Then: zero shadow warnings
	if got := len(diag.Warnings); got != 0 {
		t.Errorf("len(diag.Warnings) = %d, want 0 (no conflict)", got)
	}
}

// ensure config package is imported (silence unused import on the rare path
// where every test references only the alias indirectly via setupFourScopes).
var _ = config.SkillScopeProject
