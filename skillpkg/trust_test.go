package skillpkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/config"
)

// ============================================================
// ATDD RED PHASE — Story 47.4: Trust check MVP (warn-only)
//
// Covers AC1 / AC2 / AC4 / AC5 / AC6:
//   * AC1 — project scope w/o trust marker → one TrustWarning per projectDir
//   * AC2 — trust marker existence (not content) silences the warning
//   * AC4 — user-scope-only / empty scopes produce zero warnings
//   * AC5 — both .rnix/skills + .agents/skills under same projectDir merge
//           into a single warning with priority-ordered SkillsRootPaths
//   * AC6 — TrustWarning shape + LoadDiagnostics.Trust JSON tag
//
// RED → GREEN: production `CheckProjectTrust` currently returns nil (stub).
// All assertion paths fail until the dev-story phase ships the real impl in
// `skillpkg/trust.go` per [Story 47.4 §Task 1].
// ============================================================

// --- helper: trust fixture builders --------------------------------------

// markTrusted creates <projectDir>/.rnix/state/trusted as an empty marker
// file. The body is intentionally empty — AC2 pins existence-only semantics.
// Path is sourced from TrustMarkerRelPath (review patch P4) so renaming the
// marker is a one-place change.
func markTrusted(t *testing.T, projectDir string) {
	t.Helper()
	markerPath := filepath.Join(projectDir, TrustMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(markerPath), err)
	}
	if err := os.WriteFile(markerPath, nil, 0o600); err != nil {
		t.Fatalf("write trusted marker: %v", err)
	}
}

// markTrustedWithBody writes the marker file with arbitrary content; AC2
// pins that content is ignored.
func markTrustedWithBody(t *testing.T, projectDir, body string) {
	t.Helper()
	markerPath := filepath.Join(projectDir, TrustMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(markerPath), err)
	}
	if err := os.WriteFile(markerPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write trusted marker body: %v", err)
	}
}

// --- 47.4-UNIT-AC1-001: [P0] project scope without trust marker → 1 warning ---

func TestCheckProjectTrust_UntrustedProjectScope_EmitsWarning(t *testing.T) {
	// Given: a project scope ScopePath whose parent projectDir has no trust marker.
	_, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "foo", "foo skill")

	// Only keep the project/native scope so the fixture is unambiguous.
	scopes := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}

	// When: CheckProjectTrust runs.
	warns := CheckProjectTrust(scopes)

	// Then: exactly one TrustWarning, all five fields populated per AC1.
	if len(warns) != 1 {
		t.Fatalf("len(warns) = %d, want 1", len(warns))
	}
	w := warns[0]
	if w.ProjectDir != dirs.projectDir {
		t.Errorf("ProjectDir = %q, want %q (project root, not the .rnix/skills dir)",
			w.ProjectDir, dirs.projectDir)
	}
	if len(w.SkillsRootPaths) != 1 || w.SkillsRootPaths[0] != dirs.projectRnix {
		t.Errorf("SkillsRootPaths = %v, want [%q]", w.SkillsRootPaths, dirs.projectRnix)
	}
	if w.Reason == "" {
		t.Errorf("Reason must be non-empty (AC1 spec: 'untrusted repo can inject agent instructions'); got empty")
	}
	if w.Policy == "" {
		t.Errorf("Policy must be non-empty (AC1 spec: 'warn-only (not blocking)'); got empty")
	}
	if w.Recommendation == "" {
		t.Errorf("Recommendation must be non-empty (AC1 spec: 'touch <projectDir>/.rnix/state/trusted ...'); got empty")
	}
	// Recommendation must mention the literal `touch` command for AC1 actionability.
	if !strings.Contains(w.Recommendation, "touch") {
		t.Errorf("Recommendation must include 'touch ...' actionable command; got %q", w.Recommendation)
	}
	if !strings.Contains(w.Recommendation, dirs.projectDir) {
		t.Errorf("Recommendation must include projectDir %q for copy/paste; got %q",
			dirs.projectDir, w.Recommendation)
	}
}

// --- 47.4-UNIT-AC2-001: [P0] trusted marker → no warning ---

func TestCheckProjectTrust_TrustedProjectScope_NoWarning(t *testing.T) {
	_, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "foo", "foo skill")
	markTrusted(t, dirs.projectDir)

	scopes := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}

	warns := CheckProjectTrust(scopes)
	if len(warns) != 0 {
		t.Fatalf("trusted project must yield 0 warnings, got %d: %+v", len(warns), warns)
	}
}

// --- 47.4-UNIT-AC2-002: [P1] trusted marker content is ignored (existence-only) ---

func TestCheckProjectTrust_TrustedFile_AnyContent_NoWarning(t *testing.T) {
	_, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "foo", "foo skill")
	markTrustedWithBody(t, dirs.projectDir, "trusted by decker 2026-05-27\n# arbitrary content\n")

	scopes := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}

	warns := CheckProjectTrust(scopes)
	if len(warns) != 0 {
		t.Fatalf("trusted marker content must be ignored; got %d warnings: %+v", len(warns), warns)
	}
}

// --- 47.4-UNIT-AC2-003: [P0] trust marker as symlink → STILL warns ---
//
// Review decision DN1: symbolic links are rejected so a malicious repo
// cannot commit `.rnix/state/trusted -> /etc/hostname` (or any other
// commonly-present file) to silently mark itself trusted. isProjectTrusted
// uses os.Lstat + mode.IsRegular() to enforce the regular-file requirement.
// This test exercises BOTH the symlink-to-valid-target branch (must NOT be
// trusted) and the broken-symlink branch (must NOT be trusted).
func TestCheckProjectTrust_TrustedFile_AsSymlink_StillWarns(t *testing.T) {
	t.Run("symlink_to_valid_target", func(t *testing.T) {
		_, dirs := setupFourScopes(t)
		createTestSkillDir(t, dirs.projectRnix, "foo", "foo skill")

		markerPath := filepath.Join(dirs.projectDir, TrustMarkerRelPath)
		if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
			t.Fatalf("mkdir state dir: %v", err)
		}
		target := filepath.Join(dirs.projectDir, "elsewhere-trusted")
		if err := os.WriteFile(target, []byte("ok"), 0o600); err != nil {
			t.Fatalf("write target: %v", err)
		}
		if err := os.Symlink(target, markerPath); err != nil {
			t.Skipf("symlink unsupported on this platform: %v", err)
		}

		scopes := []config.ScopePath{
			{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
		}

		warns := CheckProjectTrust(scopes)
		if len(warns) != 1 {
			t.Fatalf("symlink trust marker must STILL trigger 1 warning (DN1), got %d", len(warns))
		}
	})

	t.Run("broken_symlink", func(t *testing.T) {
		_, dirs := setupFourScopes(t)
		createTestSkillDir(t, dirs.projectRnix, "foo", "foo skill")

		markerPath := filepath.Join(dirs.projectDir, TrustMarkerRelPath)
		if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
			t.Fatalf("mkdir state dir: %v", err)
		}
		if err := os.Symlink("/nonexistent-trust-target", markerPath); err != nil {
			t.Skipf("symlink unsupported on this platform: %v", err)
		}

		scopes := []config.ScopePath{
			{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
		}

		warns := CheckProjectTrust(scopes)
		if len(warns) != 1 {
			t.Fatalf("broken symlink marker must trigger 1 warning, got %d", len(warns))
		}
	})
}

// --- 47.4-UNIT-AC4-001: [P0] user scope only → no warning ---

func TestCheckProjectTrust_OnlyUserScope_NoWarning(t *testing.T) {
	_, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.userRnix, "user-only", "lives in user scope")
	// IMPORTANT: deliberately omit any .rnix/state/trusted marker; AC4 pins
	// "user scope is always trusted, marker irrelevant".

	scopes := []config.ScopePath{
		{Path: dirs.userRnix, Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceRnix},
		{Path: dirs.userAgents, Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceAgents},
	}

	warns := CheckProjectTrust(scopes)
	if len(warns) != 0 {
		t.Fatalf("user-only scopes must produce 0 trust warnings, got %d: %+v", len(warns), warns)
	}
}

// --- 47.4-UNIT-AC4-002: [P0] empty scopes → no warning ---

func TestCheckProjectTrust_EmptyScopes_NoWarning(t *testing.T) {
	var scopes []config.ScopePath
	warns := CheckProjectTrust(scopes)
	if len(warns) != 0 {
		t.Fatalf("empty scopes must yield 0 warnings, got %d", len(warns))
	}
}

// --- 47.4-UNIT-AC5-001: [P0] same project, both namespaces → 1 merged warning ---

func TestCheckProjectTrust_BothNamespaces_SameProject_OneMergedWarning(t *testing.T) {
	_, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "native-skill", "")
	createTestSkillDir(t, dirs.projectAgents, "agents-skill", "")

	scopes := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
		{Path: dirs.projectAgents, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceAgents},
	}

	warns := CheckProjectTrust(scopes)
	if len(warns) != 1 {
		t.Fatalf("two ScopePaths under one projectDir must dedup to 1 warning, got %d: %+v",
			len(warns), warns)
	}
	w := warns[0]
	if w.ProjectDir != dirs.projectDir {
		t.Errorf("ProjectDir = %q, want %q", w.ProjectDir, dirs.projectDir)
	}
	if len(w.SkillsRootPaths) != 2 {
		t.Fatalf("SkillsRootPaths length = %d, want 2 (both namespaces): %v",
			len(w.SkillsRootPaths), w.SkillsRootPaths)
	}
	// Priority order: project/native before project/agents (input scope order).
	if w.SkillsRootPaths[0] != dirs.projectRnix {
		t.Errorf("SkillsRootPaths[0] = %q, want %q (native first)",
			w.SkillsRootPaths[0], dirs.projectRnix)
	}
	if w.SkillsRootPaths[1] != dirs.projectAgents {
		t.Errorf("SkillsRootPaths[1] = %q, want %q (agents second)",
			w.SkillsRootPaths[1], dirs.projectAgents)
	}
}

// --- 47.4-UNIT-AC5-002: [P1] distinct projectDirs → distinct warnings ---
//
// Each projectDir gets its own TrustWarning entry; cross-project dedup is
// explicitly forbidden by AC5 dev-notes ("WithAncestorTraversal edge case").
func TestCheckProjectTrust_DistinctProjectDirs_TwoWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projA := t.TempDir()
	projB := t.TempDir()
	dirA := filepath.Join(projA, ".rnix", "skills")
	dirB := filepath.Join(projB, ".rnix", "skills")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatalf("mkdir A: %v", err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatalf("mkdir B: %v", err)
	}

	scopes := []config.ScopePath{
		{Path: dirA, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
		{Path: dirB, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
	}

	warns := CheckProjectTrust(scopes)
	if len(warns) != 2 {
		t.Fatalf("two distinct projectDirs must produce 2 warnings, got %d: %+v",
			len(warns), warns)
	}
	gotDirs := map[string]bool{warns[0].ProjectDir: true, warns[1].ProjectDir: true}
	if !gotDirs[projA] {
		t.Errorf("missing TrustWarning for projectA %q", projA)
	}
	if !gotDirs[projB] {
		t.Errorf("missing TrustWarning for projectB %q", projB)
	}
}

// --- 47.4-UNIT-AC6-001: [P0] LoadDiagnostics.Trust JSON tag + omitempty ---
//
// Pins the four-channel wire shape:
//   * non-empty Trust → JSON contains `"trust":[...]`
//   * empty Trust + empty other channels → JSON is `{}` (omitempty all-around)
func TestLoadDiagnostics_TrustFieldExists_JSON(t *testing.T) {
	// Populated case.
	diag := LoadDiagnostics{
		Trust: []TrustWarning{{
			ProjectDir:      "/abs/proj",
			SkillsRootPaths: []string{"/abs/proj/.rnix/skills"},
			Reason:          "test",
			Policy:          "test",
			Recommendation:  "test",
		}},
	}
	raw, err := json.Marshal(diag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"trust":[`) {
		t.Errorf("expected `trust` JSON key when populated, got %s", string(raw))
	}
	if !strings.Contains(string(raw), `"project_dir":"/abs/proj"`) {
		t.Errorf("expected snake_case `project_dir` JSON tag, got %s", string(raw))
	}
	if !strings.Contains(string(raw), `"skills_root_paths":`) {
		t.Errorf("expected snake_case `skills_root_paths` JSON tag, got %s", string(raw))
	}

	// Empty case — omitempty must hide the field.
	emptyRaw, err := json.Marshal(LoadDiagnostics{})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(emptyRaw), `"trust"`) {
		t.Errorf("empty LoadDiagnostics must omit `trust` (omitempty), got %s", string(emptyRaw))
	}
}

// --- 47.4-UNIT-AC6-002: [P1] TrustWarning struct field tags compile ---
//
// Compile-time check that the production struct has the exact five fields
// the CLI renderer and JSON consumers will read.
func TestTrustWarning_StructShape_CompileGuard(t *testing.T) {
	var tw TrustWarning
	tw.ProjectDir = "/x"
	tw.SkillsRootPaths = []string{"/x/.rnix/skills"}
	tw.Reason = "r"
	tw.Policy = "p"
	tw.Recommendation = "rec"
	_ = tw
}
