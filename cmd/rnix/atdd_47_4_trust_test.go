package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/skillpkg"
)

// ============================================================
// ATDD RED PHASE — Story 47.4: Trust check MVP (CLI integration)
//
// Covers AC8 / AC9 / AC10:
//   * AC8  — renderDiagnosticsToStderr renders a Trust branch (one line per
//            TrustWarning); ModeQuiet still emits to stderr; output is plain
//            text on a single un-truncated line.
//   * AC9  — runSkillInstall / runSkillUpdate run CheckProjectTrust as a
//            pre-check before the install/update for-loop; multiple args
//            emit only one trust warning per CLI invocation.
//   * AC10 — install / update JSON shapes embed `diagnostics.trust` via
//            LoadDiagnostics with omitempty; trusted-case install/update
//            JSON omits the `diagnostics` key entirely (47.3 backwards-compat);
//            list JSON always includes diagnostics (47.3 wire-shape decision).
//
// RED → GREEN: production renderDiagnosticsToStderr has no Trust branch;
// runSkillInstall / runSkillUpdate do not call CheckProjectTrust; the JSON
// data structs have no Diagnostics field. All assertions fail until the
// dev-story phase ships [Story 47.4 §Tasks 3+4].
// ============================================================

// --- helper: local trust marker writer (cmd/rnix package) ----------------
//
// Mirrors skillpkg/trust_test.go::markTrusted (skillpkg is a separate
// package; the helper cannot be reused directly across package boundaries).
func markTrustedInProject(t *testing.T, projectDir string) {
	t.Helper()
	stateDir := filepath.Join(projectDir, ".rnix", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "trusted"), nil, 0o644); err != nil {
		t.Fatalf("write trusted marker: %v", err)
	}
}

// setupUntrustedProject creates an empty .rnix/skills + fixture skill under a
// temp project directory and points $HOME / $XDG to fresh temps. Returns the
// projectDir absolute path.
func setupUntrustedProject(t *testing.T) (projectDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir = t.TempDir()
	skillDir := filepath.Join(projectDir, ".rnix", "skills", "local-skill")
	writeFixtureSkill(t, skillDir, "local-skill", "1.0.0", "trust-check fixture")
	return projectDir
}

// --- 47.4-CLI-AC8-001: [P0] renderDiagnosticsToStderr renders Trust branch ---
//
// Direct invocation of the helper with a hand-built LoadDiagnostics.Trust
// slice. Pins the AC8 string format requirements:
//   * starts with `[skill] warning:`
//   * contains `untrusted project <projectDir>`
//   * contains the literal `Policy:` keyword
//   * contains `touch ` for actionability
func TestRenderDiagnosticsToStderr_TrustWarnings_Format(t *testing.T) {
	diag := skillpkg.LoadDiagnostics{
		Trust: []skillpkg.TrustWarning{{
			ProjectDir:      "/home/decker/EchoMatrix",
			SkillsRootPaths: []string{"/home/decker/EchoMatrix/.rnix/skills"},
			Reason:          "untrusted repo can inject agent instructions",
			Policy:          "warn-only (not blocking)",
			Recommendation:  "To dismiss, run: touch /home/decker/EchoMatrix/.rnix/state/trusted",
		}},
	}

	stderr := captureStderr(t, func() {
		renderDiagnosticsToStderr(diag)
	})

	if !strings.Contains(stderr, "[skill] warning:") {
		t.Errorf("expected '[skill] warning:' prefix, got: %q", stderr)
	}
	if !strings.Contains(stderr, "untrusted project") {
		t.Errorf("expected 'untrusted project' phrase, got: %q", stderr)
	}
	if !strings.Contains(stderr, "/home/decker/EchoMatrix") {
		t.Errorf("expected projectDir path in output, got: %q", stderr)
	}
	if !strings.Contains(stderr, "Policy:") {
		t.Errorf("expected 'Policy:' keyword, got: %q", stderr)
	}
	if !strings.Contains(stderr, "touch ") {
		t.Errorf("expected 'touch ' actionable command, got: %q", stderr)
	}
}

// --- 47.4-CLI-AC8-002: [P0] skill list under untrusted project → stderr trust warning ---

func TestSkillList_UntrustedProjectScope_TrustWarningOnStderr(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	t.Chdir(projectDir)

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "list")
	})

	if !strings.Contains(stderr, "untrusted project") {
		t.Errorf("expected stderr trust warning for untrusted list, got stderr=%q", stderr)
	}
	if !strings.Contains(stderr, projectDir) {
		t.Errorf("expected stderr to mention projectDir %q, got: %q", projectDir, stderr)
	}
}

// --- 47.4-CLI-AC8-003: [P1] trusted project → no trust warning on list stderr ---

func TestSkillList_TrustedProjectScope_NoTrustWarning(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	markTrustedInProject(t, projectDir)
	t.Chdir(projectDir)

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "list")
	})

	if strings.Contains(stderr, "untrusted project") {
		t.Errorf("trusted project must NOT emit trust warning, got stderr=%q", stderr)
	}
}

// --- 47.4-CLI-AC9-001: [P0] runSkillInstall under untrusted project → stderr trust warning ---

func TestRunSkillInstall_UntrustedProject_TrustWarningOnStderr(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	t.Chdir(projectDir)
	mockPkgServeFixture(t, "tiny", "1.0.0")

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "install", "tiny")
	})

	if !strings.Contains(stderr, "untrusted project") {
		t.Errorf("expected install to emit stderr trust warning, got stderr=%q", stderr)
	}
	if !strings.Contains(stderr, projectDir) {
		t.Errorf("expected stderr to mention projectDir %q, got: %q", projectDir, stderr)
	}
}

// --- 47.4-CLI-AC9-002: [P0] install multiple args → only ONE trust warning ---
//
// Pins AC9 dedup: trust pre-check must sit OUTSIDE the install for-loop.
// Three install args means three Install calls, but only one trust warning.
func TestRunSkillInstall_MultipleArgs_TrustWarningOnlyOnce(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	t.Chdir(projectDir)
	mockPkgServeFixture(t, "foo", "1.0.0")
	mockPkgServeFixture(t, "bar", "1.0.0")
	mockPkgServeFixture(t, "baz", "1.0.0")

	stderr := captureStderr(t, func() {
		// Note: subsequent calls to mockPkgServeFixture replace the URL via
		// overrideSkillRegistryURL — only the last fixture (baz) is reachable.
		// To exercise three args under a single registry, the dev-story phase
		// will refactor the helper; for the red-phase scaffold, count occurrences
		// against the trust warning text only (the install errors are tolerable).
		_, _ = runSkillCmdForTest(t, "skill", "install", "foo", "bar", "baz")
	})

	count := strings.Count(stderr, "untrusted project")
	if count != 1 {
		t.Errorf("expected exactly 1 trust warning across 3 install args (dedup outside for-loop), got %d in stderr=%q",
			count, stderr)
	}
}

// --- 47.4-CLI-AC9-003: [P0] runSkillUpdate under untrusted project → stderr trust warning ---

func TestRunSkillUpdate_UntrustedProject_TrustWarningOnStderr(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	mockPkgServeFixture(t, "upd", "1.0.0")

	// Pre-step: install once so update has a target. Install also emits a
	// trust warning, but captureStderr only wraps the update call.
	t.Chdir(projectDir)
	_, _ = runSkillCmdForTest(t, "skill", "install", "upd")

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "update", "upd")
	})

	if !strings.Contains(stderr, "untrusted project") {
		t.Errorf("expected update to emit stderr trust warning, got stderr=%q", stderr)
	}
}

// --- 47.4-CLI-AC10-001: [P0] install JSON → diagnostics.trust populated ---

func TestSkillInstall_JSON_UntrustedProject_DiagnosticsTrustField(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	t.Chdir(projectDir)
	mockPkgServeFixture(t, "j-install", "1.0.0")

	stdout, _ := runSkillCmdForTest(t, "skill", "install", "j-install", "--json")
	raw := stdout.String()

	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("expected JSON to include `diagnostics` node under untrusted install, got: %s", raw)
	}
	if !strings.Contains(raw, `"trust"`) {
		t.Errorf("expected JSON `diagnostics.trust` node populated, got: %s", raw)
	}
	if !strings.Contains(raw, `"project_dir"`) {
		t.Errorf("expected JSON trust entry to carry `project_dir` field, got: %s", raw)
	}

	// Sanity: existing 47.3 fields must still serialize.
	if !strings.Contains(raw, `"installed"`) {
		t.Errorf("install JSON `installed` field must remain (47.3 back-compat), got: %s", raw)
	}
}

// --- 47.4-CLI-AC10-002: [P0] install JSON trusted → diagnostics OMITTED ---
//
// Pins the omitempty back-compat decision (Story 47.4 §AC10): a trusted
// install must produce a JSON payload identical in shape to the 47.3 wire
// — no `diagnostics` key.
func TestSkillInstall_JSON_TrustedProject_NoDiagnosticsField(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	markTrustedInProject(t, projectDir)
	t.Chdir(projectDir)
	mockPkgServeFixture(t, "j-trusted", "1.0.0")

	stdout, _ := runSkillCmdForTest(t, "skill", "install", "j-trusted", "--json")
	raw := stdout.String()

	if strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("trusted install JSON must OMIT `diagnostics` (omitempty), got: %s", raw)
	}
	// Parse to be sure the JSON is still well-formed.
	var resp JSONResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, raw)
	}
	if !resp.OK {
		t.Errorf("trusted install JSON ok=false unexpected: %s", raw)
	}
}

// --- 47.4-CLI-AC10-003: [P0] update JSON → diagnostics.trust populated ---

func TestSkillUpdate_JSON_UntrustedProject_DiagnosticsTrustField(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	mockPkgServeFixture(t, "upd-json", "1.0.0")

	// Install first (target must exist for Update to find it).
	t.Chdir(projectDir)
	_, _ = runSkillCmdForTest(t, "skill", "install", "upd-json")

	stdout, _ := runSkillCmdForTest(t, "skill", "update", "upd-json", "--json")
	raw := stdout.String()

	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("expected update JSON to include `diagnostics`, got: %s", raw)
	}
	if !strings.Contains(raw, `"trust"`) {
		t.Errorf("expected update JSON `diagnostics.trust` populated, got: %s", raw)
	}
}

// --- 47.4-CLI-AC10-004: [P1] list JSON → diagnostics.trust populated ---
//
// list-JSON shape ALWAYS contains `diagnostics` (47.3 decision); 47.4 adds
// the trust subkey when project scope is untrusted.
func TestSkillList_JSON_UntrustedProject_DiagnosticsTrustField(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	t.Chdir(projectDir)

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "--json")
	raw := stdout.String()

	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("list JSON must always include `diagnostics` (47.3 contract), got: %s", raw)
	}
	if !strings.Contains(raw, `"trust"`) {
		t.Errorf("list JSON `diagnostics.trust` must be populated under untrusted project, got: %s", raw)
	}
}

// --- 47.4-CLI-AC8-004: [P2] quiet mode still emits trust warning on stderr ---
//
// Pins 47.3 decision-point #8: ModeQuiet only affects stdout. Stderr
// advisories (lenient/shadow/skipped/trust) are still emitted.
func TestSkillList_QuietMode_StillEmitsTrustWarning(t *testing.T) {
	projectDir := setupUntrustedProject(t)
	t.Chdir(projectDir)

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "list", "--quiet")
	})

	if !strings.Contains(stderr, "untrusted project") {
		t.Errorf("quiet mode must STILL emit trust warning on stderr (47.3 decision #8), got: %q",
			stderr)
	}
}

// --- 47.4-CLI-AC8-005: [P2] trust warning is NOT truncated ---
//
// Pins AC8 dev-note: long projectDir paths must be fully rendered so users
// can copy the `touch ...` command. Construct a deliberately long path.
func TestRenderDiagnosticsToStderr_TrustWarnings_NoTruncation(t *testing.T) {
	longPath := "/very/long/absolute/path/to/some/deeply/nested/project/directory/used-by-acceptance-test"
	diag := skillpkg.LoadDiagnostics{
		Trust: []skillpkg.TrustWarning{{
			ProjectDir:      longPath,
			SkillsRootPaths: []string{longPath + "/.rnix/skills"},
			Reason:          "untrusted repo can inject agent instructions",
			Policy:          "warn-only (not blocking)",
			Recommendation:  "touch " + longPath + "/.rnix/state/trusted",
		}},
	}

	stderr := captureStderr(t, func() {
		renderDiagnosticsToStderr(diag)
	})

	if !strings.Contains(stderr, longPath) {
		t.Errorf("long projectDir must NOT be truncated; missing %q in stderr=%q",
			longPath, stderr)
	}
	if !strings.Contains(stderr, "touch "+longPath) {
		t.Errorf("full `touch <long path>` recommendation must render verbatim; got stderr=%q",
			stderr)
	}
}

// --- Helper compile-time guard: ensure bytes import is exercised ----------
// captureStderr already covers bytes; this guard remains so future test
// additions that need a buffer can import freely.
var _ = bytes.NewBuffer
