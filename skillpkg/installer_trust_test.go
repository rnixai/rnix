package skillpkg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/skills"
)

// ============================================================
// ATDD RED PHASE — Story 47.4: Installer 与 trust check 集成
//
// Covers AC3 / AC7:
//   * AC3 — warn-only: ListAll/Install/Update results MATCH the trusted-case
//           output; only LoadDiagnostics.Trust differs.
//   * AC7 — ListAll triggers CheckProjectTrust internally; the result is
//           assigned to diag.Trust AFTER the errNoScopes early-return.
//
// RED → GREEN: production ListAll currently does NOT call CheckProjectTrust,
// so diag.Trust is always nil. Tests fail until the dev-story phase wires
// `diag.Trust = CheckProjectTrust(inst.scopes)` per [Story 47.4 §Task 2].
// ============================================================

// --- 47.4-INT-AC3-001: [P0] untrusted project → entries unchanged ---
//
// Pins AC3 contract: ListAll must return the same []ListEntry shape whether
// or not the trust marker exists. Trust check is purely advisory; it does
// not skip, filter, or reorder skills.
func TestListAll_UntrustedProject_StillReturnsEntries(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "foo", "untrusted but listable")
	// Intentionally NO markTrusted(...) — this is the untrusted fixture.

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})

	// Restrict scopes to project/native so the comparison is deterministic.
	projScope := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}
	installer := NewInstaller(client, skillLoader, projScope, projScope[0])

	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (warn-only must not filter)", len(entries))
	}
	if entries[0].Name != "foo" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "foo")
	}
	if entries[0].Scope != "project" {
		t.Errorf("entries[0].Scope = %q, want %q", entries[0].Scope, "project")
	}

	_ = scopes // setupFourScopes returns the full slice; not all 4 needed here.
}

// --- 47.4-INT-AC3-002: [P0] untrusted Install still writes skill ---
//
// Pins AC3 install half: warn-only must not block Install; the *InstallResult
// fields must match the trusted-case shape.
func TestInstall_UntrustedProject_StillWritesSkill(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	// No trust marker — installer must STILL extract the skill.

	srv, _, _ := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})

	writeScope := config.ScopePath{
		Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix,
	}
	installer := NewInstaller(client, skillLoader, []config.ScopePath{writeScope}, writeScope)

	result, err := installer.Install("test-skill", InstallOpts{})
	if err != nil {
		t.Fatalf("Install must succeed under untrusted project (warn-only); got %v", err)
	}
	if result == nil {
		t.Fatal("Install returned nil *InstallResult")
	}
	if result.Name != "test-skill" {
		t.Errorf("result.Name = %q, want %q", result.Name, "test-skill")
	}
	if !result.Fresh {
		t.Errorf("result.Fresh = false, want true (first install)")
	}
	wantPath := filepath.Join(dirs.projectRnix, "test-skill")
	if result.Path != wantPath {
		t.Errorf("result.Path = %q, want %q (untrusted must still land in writeScope)",
			result.Path, wantPath)
	}
	// Skill file must be on disk.
	if _, err := os.Stat(filepath.Join(wantPath, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md must exist under writeScope after untrusted Install: %v", err)
	}

	_ = scopes
}

// --- 47.4-INT-AC7-001: [P0] ListAll populates diag.Trust on untrusted project ---
//
// Pins AC7 wiring: `diag.Trust = CheckProjectTrust(inst.scopes)` must run
// AFTER the `if len(inst.scopes) == 0` early-return but BEFORE Step 1
// candidate discovery. Test the post-condition: diag.Trust length=1.
func TestListAll_UntrustedProject_DiagTrustPopulated(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "foo", "")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})

	projScope := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}
	installer := NewInstaller(client, skillLoader, projScope, projScope[0])

	_, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(diag.Trust) != 1 {
		t.Fatalf("ListAll under untrusted project must set diag.Trust length=1, got %d: %+v",
			len(diag.Trust), diag.Trust)
	}
	tw := diag.Trust[0]
	if tw.ProjectDir != dirs.projectDir {
		t.Errorf("diag.Trust[0].ProjectDir = %q, want %q", tw.ProjectDir, dirs.projectDir)
	}
	if len(tw.SkillsRootPaths) != 1 || tw.SkillsRootPaths[0] != dirs.projectRnix {
		t.Errorf("diag.Trust[0].SkillsRootPaths = %v, want [%q]",
			tw.SkillsRootPaths, dirs.projectRnix)
	}

	_ = scopes
}

// --- 47.4-INT-AC7-002: [P0] ListAll with trusted project → diag.Trust empty ---

func TestListAll_TrustedProject_NoTrustWarning(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "foo", "")
	markTrusted(t, dirs.projectDir)

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})

	projScope := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}
	installer := NewInstaller(client, skillLoader, projScope, projScope[0])

	_, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(diag.Trust) != 0 {
		t.Fatalf("trusted project must yield empty diag.Trust, got %d: %+v",
			len(diag.Trust), diag.Trust)
	}

	_ = scopes
}

// --- 47.4-INT-AC7-003: [P1] empty scopes → errNoScopes BEFORE trust check ---
//
// Pins AC7 ordering by observing the trust-check counter: when scopes is
// empty the errNoScopes early-return must fire BEFORE isProjectTrusted is
// invoked. Without the counter this test would be a tautology — both
// orderings produce the same diag.Trust=nil output (review patch P1).
func TestListAll_EmptyScopes_NoTrustCheck(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(nil)

	installer := NewInstaller(client, skillLoader, nil, config.ScopePath{})

	ResetTrustChecksForTest()
	_, diag, err := installer.ListAll()
	if err == nil {
		t.Fatal("ListAll with empty scopes: expected error, got nil")
	}
	if got := TrustChecksPerformedForTest(); got != 0 {
		t.Errorf("empty-scope ListAll must skip the trust check entirely, got %d isProjectTrusted call(s)", got)
	}
	if len(diag.Trust) != 0 {
		t.Errorf("empty scopes must yield empty diag.Trust (no trust check), got %d entries",
			len(diag.Trust))
	}
}

// --- 47.4-INT-AC7-004: [P1] non-empty scopes → trust check DOES run ---
//
// Sibling assertion to TestListAll_EmptyScopes_NoTrustCheck: confirms that
// when scopes contain a project entry, isProjectTrusted is invoked. Together
// they pin the AC7 ordering contract (errNoScopes early-return BEFORE the
// trust check, NOT after) (review patch P1).
func TestListAll_NonEmptyScopes_RunsTrustCheck(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "foo", "")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})

	projScope := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}
	installer := NewInstaller(client, skillLoader, projScope, projScope[0])

	ResetTrustChecksForTest()
	_, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if got := TrustChecksPerformedForTest(); got == 0 {
		t.Errorf("non-empty project scope must trigger at least one isProjectTrusted call, got %d", got)
	}

	_ = scopes
}

// --- 47.4-INT-AC3-003: [P1] untrusted ListAll preserves existing diagnostics ---
//
// Pins regression boundary: adding diag.Trust must NOT disturb the existing
// three channels (Warnings/Skipped/Lenient). This test loads a fixture that
// triggers a shadow warning AND has no trust marker — both must surface.
func TestListAll_UntrustedProject_ShadowWarningAlsoSurfaces(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	// Shadow setup: same skill name in project/native and user/native.
	createTestSkillDir(t, dirs.projectRnix, "dup", "winner")
	createTestSkillDir(t, dirs.userRnix, "dup", "loser")
	// Intentionally NO trust marker — trust + shadow must coexist.

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	_, diag, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(diag.Warnings) < 1 {
		t.Errorf("expected at least one ShadowWarning, got 0 (diagnostics: %+v)", diag)
	}
	if len(diag.Trust) < 1 {
		t.Errorf("expected at least one TrustWarning, got 0 (diagnostics: %+v)", diag)
	}
}
