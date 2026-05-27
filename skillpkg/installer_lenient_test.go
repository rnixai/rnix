package skillpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/skills"
)

// ============================================================
// ATDD RED PHASE — Story 47.2: Installer 多 scope 改造
//
// 覆盖 AC7 / AC8：
//   - AC7: SkillLoader 返回 *LenientSkipError 时累积到 LoadDiagnostics.Skipped，
//          不进 result 数组，不让 ListAll 整体崩溃。
//   - AC8: ListAll 签名 ([]ListEntry, LoadDiagnostics, error)；LoadDiagnostics
//          含 Warnings / Skipped / Lenient 三类切片；零状态返回 zero-value 结构。
//
// 本文件引用尚不存在的：
//   * LoadDiagnostics / SkipEntry / LenientWarning 类型
//   * LoadDiagnostics.Skipped / Lenient 字段
//   * ListAll 三值返回签名
//
// RED → GREEN: 完成 AC7+AC8 实现后，本文件应编译且全部测试通过。
// ============================================================

// writeBadSKILLMD writes a SKILL.md file with arbitrary content under
// <scopeDir>/<name>/SKILL.md to simulate a corrupt or non-conforming skill.
func writeBadSKILLMD(t *testing.T, scopeDir, name, contents string) {
	t.Helper()
	dir := filepath.Join(scopeDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write SKILL.md for %q: %v", name, err)
	}
}

// --- 47.2-INT-007: [P0] ListAll lenient skip — corrupt skills don't crash list (AC7) ---

func TestInstaller_ListAll_LenientSkip(t *testing.T) {
	// Given: 3 skills in project/native: one valid + one missing description + one yaml-broken
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "good", "valid skill")

	writeBadSKILLMD(t, dirs.projectRnix, "no-desc", `---
name: no-desc
description: ""
allowed-tools: /dev/fs
---

# no-desc
`)

	writeBadSKILLMD(t, dirs.projectRnix, "yaml-broken", `---
name: : :
this is not valid yaml at all
---

body
`)

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	// When: ListAll
	entries, diag, err := installer.ListAll()

	// Then: no top-level error; only the valid skill in result
	if err != nil {
		t.Fatalf("ListAll: lenient skip must not surface as top-level error: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name)
		}
		t.Fatalf("len(entries) = %d, want 1 (only 'good'); got: %v", len(entries), names)
	}
	if entries[0].Name != "good" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "good")
	}

	// Diagnostics: 2 skipped entries (no-desc + yaml-broken)
	if got := len(diag.Skipped); got != 2 {
		t.Fatalf("len(diag.Skipped) = %d, want 2", got)
	}

	// Each Skipped entry's path must point under project/native
	skippedByReason := make(map[string]SkipEntry, 2)
	for _, sk := range diag.Skipped {
		if !strings.HasPrefix(sk.Path, dirs.projectRnix) {
			t.Errorf("Skipped.Path = %q, want prefix %q", sk.Path, dirs.projectRnix)
		}
		// Bucket by reason category for assertion convenience.
		switch {
		case strings.Contains(sk.Reason, "description"):
			skippedByReason["desc"] = sk
		case strings.Contains(sk.Reason, "yaml"):
			skippedByReason["yaml"] = sk
		}
	}

	if _, ok := skippedByReason["desc"]; !ok {
		t.Error("expected a Skipped entry with reason containing 'description'")
	}
	if _, ok := skippedByReason["yaml"]; !ok {
		t.Error("expected a Skipped entry with reason containing 'yaml'")
	}
}

// --- 47.2-UNIT-006: [P1] LoadDiagnostics zero-value when nothing flagged (AC8) ---

func TestInstaller_ListAll_DiagnosticsZeroValue(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "clean", "no issues")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	_, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	if len(diag.Warnings) != 0 {
		t.Errorf("len(diag.Warnings) = %d, want 0", len(diag.Warnings))
	}
	if len(diag.Skipped) != 0 {
		t.Errorf("len(diag.Skipped) = %d, want 0", len(diag.Skipped))
	}
	if len(diag.Lenient) != 0 {
		t.Errorf("len(diag.Lenient) = %d, want 0", len(diag.Lenient))
	}
}

// --- 47.2-INT-008: [P1] LoadDiagnostics accumulates all three categories (AC8) ---

func TestInstaller_ListAll_DiagnosticsAccumulate(t *testing.T) {
	// Build a fixture that triggers every diagnostic category simultaneously:
	//   - shadow: same skill in two scopes → 1 ShadowWarning
	//   - lenient skip: yaml-broken SKILL.md → 1 SkipEntry
	//   - lenient warn-but-load: SKILL.md whose name field mismatches parent dir → 1 LenientWarning
	scopes, dirs := setupFourScopes(t)

	// Shadow: "tool" in project/native (winner) + user/native (shadowed)
	createTestSkillDir(t, dirs.projectRnix, "tool", "winner")
	createTestSkillDir(t, dirs.userRnix, "tool", "loser")

	// Lenient warn-but-load: parent dir "renamed" but frontmatter name says "actual-name"
	writeBadSKILLMD(t, dirs.projectRnix, "renamed", `---
name: actual-name
description: "name mismatches parent dir"
allowed-tools: /dev/fs
---

# renamed
`)

	// Lenient skip: yaml broken
	writeBadSKILLMD(t, dirs.projectRnix, "broken-yaml", `---
name: : not yaml
---

body
`)

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	_, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	if got := len(diag.Warnings); got < 1 {
		t.Errorf("len(diag.Warnings) = %d, want ≥1 (shadow)", got)
	}
	if got := len(diag.Skipped); got < 1 {
		t.Errorf("len(diag.Skipped) = %d, want ≥1 (yaml-broken)", got)
	}
	if got := len(diag.Lenient); got < 1 {
		t.Errorf("len(diag.Lenient) = %d, want ≥1 (name mismatch warn-but-load)", got)
	}
}

// --- 47.2-INT-009: [P1] Corrupt SKILL.md with registry entry: dual-signal (AC7 §dev-notes-5) ---

// When a skill name exists in a scope's registry (community install record) but
// its on-disk SKILL.md is broken, Epic 8 behavior is to STILL list the entry
// (with empty description, version from registry). Story 47.2 adds the parallel
// requirement that this same skill ALSO surfaces in LoadDiagnostics.Skipped —
// the "dual-signal" boundary documented in story dev-notes §5.
func TestInstaller_ListAll_CorruptCommunity_DualSignal(t *testing.T) {
	scopes, dirs := setupFourScopes(t)

	// Seed the registry to simulate that "foo" was installed as community at v1.2.3
	// but its SKILL.md became corrupt afterward.
	reg := NewLocalRegistry(dirs.projectRnix)
	if err := reg.Add(RegistryEntry{
		Name:    "foo",
		Version: "1.2.3",
		Source:  "community",
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	writeBadSKILLMD(t, dirs.projectRnix, "foo", `---
not: valid: yaml: at: all
---
`)

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	entries, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	// Then: result still includes foo (community entry preserved, description empty)
	var found *ListEntry
	for i := range entries {
		if entries[i].Name == "foo" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'foo' to remain in result (Epic 8 corrupt-community behavior)")
	}
	if found.Source != "community" {
		t.Errorf("foo.Source = %q, want %q", found.Source, "community")
	}
	if found.Version != "1.2.3" {
		t.Errorf("foo.Version = %q, want %q (from registry)", found.Version, "1.2.3")
	}
	if found.Description != "" {
		t.Errorf("foo.Description = %q, want empty (SKILL.md unparsable)", found.Description)
	}

	// AND: dual signal — foo also appears in LoadDiagnostics.Skipped
	found = nil
	for _, sk := range diag.Skipped {
		if strings.Contains(sk.Path, "foo") {
			tmp := ListEntry{Name: "foo"} // marker only
			_ = tmp
			found = &ListEntry{}
			break
		}
	}
	if found == nil {
		t.Error("expected 'foo' SKILL.md path also reported in diag.Skipped (dual-signal per dev-notes §5)")
	}
}
