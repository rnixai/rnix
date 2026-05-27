package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/skillpkg"
)

// ============================================================
// ATDD — Story 47.3: CLI 子命令多 scope 适配
//
// 覆盖 AC1（删除三处 basePath := "lib/skills" 硬编码 + ResolveSkillScopes
// 接入）与 AC11（既有 skill_test.go fixture 同步更新）。
// ============================================================

// --- 47.3-CLI-AC1-001: [P0] singleScopeFromBasePath helper 必须删除 ---

func TestSingleScopeFromBasePath_HelperRemoved(t *testing.T) {
	src, err := os.ReadFile("skill.go")
	if err != nil {
		t.Fatalf("read skill.go: %v", err)
	}
	if strings.Contains(string(src), "func singleScopeFromBasePath(") {
		t.Errorf("cmd/rnix/skill.go still contains singleScopeFromBasePath helper; 47.3 should delete the 47.2 shim")
	}
}

// --- 47.3-CLI-AC1-002: [P0] runSkillInstall / Update / List 不应硬编码 lib/skills ---

func TestRunSkill_NoLibSkillsHardcode(t *testing.T) {
	src, err := os.ReadFile("skill.go")
	if err != nil {
		t.Fatalf("read skill.go: %v", err)
	}
	body := string(src)

	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if strings.Contains(line, `"lib/skills"`) {
			t.Errorf("cmd/rnix/skill.go non-comment line still hardcodes \"lib/skills\": %s", line)
		}
	}
}

// --- 47.3-CLI-AC1-003: [P0] runSkillList 在 /tmp 这类无 lib/skills 目录运行时回退到 user scope ---

func TestRunSkillList_NoCwdLibSkills_FallsBackToUserScope(t *testing.T) {
	resetSkillFlagsForTest(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Pre-populate user/native scope with a skill fixture.
	userNative := filepath.Join(home, ".config", "rnix", "skills", "fallback-skill")
	writeFixtureSkill(t, userNative, "fallback-skill", "1.0.0", "minimum viable skill")

	// Cwd: a fresh tmp dir with NO lib/skills, no .rnix/, no .agents/.
	cwd := t.TempDir()
	t.Chdir(cwd)

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "--json")
	raw := stdout.String()

	var resp JSONResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, raw)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true (fallback-skill exists in user scope), got %s", raw)
	}
	if !strings.Contains(raw, `"fallback-skill"`) {
		t.Errorf("expected fallback-skill in JSON output (user scope fallback); got: %s", raw)
	}
}

// --- 47.3-CLI-AC1-004: [P0] runSkillInstall 在不含 .rnix/ 的目录默认写到 user/native ---

func TestRunSkillInstall_NoProject_WritesToUserNative(t *testing.T) {
	resetSkillFlagsForTest(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cwd := t.TempDir()
	t.Chdir(cwd)

	srv := startMockSkillRegistry(t, "tiny-skill", "1.0.0")
	overrideSkillRegistryURL(t, srv.URL)

	stdout, _ := runSkillCmdForTest(t, "skill", "install", "tiny-skill", "--json")
	raw := stdout.String()
	if !strings.Contains(raw, `"tiny-skill"`) {
		t.Fatalf("expected install to succeed, got: %s", raw)
	}

	expectedPath := filepath.Join(home, ".config", "rnix", "skills", "tiny-skill")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("expected tiny-skill installed at user/native path %q, stat err: %v", expectedPath, err)
	}
}

// --- 47.3-CLI-AC11-001: [P1] renderSkillListJSON 签名追加 diag 第三参数 ---

func TestRenderSkillListJSON_NewThreeArgSignature(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	entries := []skillpkg.ListEntry{
		{
			Name: "code-analysis", Version: "1.0.0",
			Path: "lib/skills/code-analysis/", Description: "Analyze code",
			Source: "builtin", Scope: "project", Namespace: "native",
		},
	}
	diag := skillpkg.LoadDiagnostics{
		Lenient: []skillpkg.LenientWarning{
			{Path: "/x", Field: "name", Reason: "mismatch", Detail: "x"},
		},
	}

	renderSkillListJSON(r, entries, diag)

	raw := buf.String()
	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("expected 'diagnostics' key in JSON, got: %s", raw)
	}
	if !strings.Contains(raw, `"lenient"`) {
		t.Errorf("expected 'lenient' subkey in JSON, got: %s", raw)
	}
}

// --- 47.3-CLI-AC11-002: [P1] 既有 JSON fixture 必须含 Scope / Namespace ---

func TestSkillList_JSONOutput_FixtureHasScopeNamespace(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	entries := []skillpkg.ListEntry{
		{
			Name: "code-analysis", Version: "1.0.0",
			Path: "lib/skills/code-analysis/", Description: "Analyze code",
			Source: "builtin", Scope: "project", Namespace: "native",
		},
		{
			Name: "pr-reviewer", Version: "2.1.0",
			Path: "lib/skills/pr-reviewer/", Description: "Review PRs",
			Source: "community", Scope: "user", Namespace: "native",
		},
	}

	renderSkillListJSON(r, entries, skillpkg.LoadDiagnostics{})

	raw := buf.String()
	if !strings.Contains(raw, `"scope":"project"`) {
		t.Errorf("expected \"scope\":\"project\" field in JSON; got: %s", raw)
	}
	if !strings.Contains(raw, `"namespace":"native"`) {
		t.Errorf("expected \"namespace\":\"native\" field in JSON; got: %s", raw)
	}
	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("expected \"diagnostics\" key in JSON; got: %s", raw)
	}
}

// ============================================================
// Test helpers — shared across 47-3 ATDD files.
// ============================================================

// resetSkillFlagsForTest clears global Cobra flag state and exitCode so each
// test starts from a known baseline. rootCmd is a package-global Cobra.Command
// whose flag values persist across SetArgs invocations; flags must be reset
// explicitly between tests.
func resetSkillFlagsForTest(t *testing.T) {
	t.Helper()
	exitCode = 0
	flagSkillForce = false
	flagSkillInstallGlobal = false
	flagSkillInstallShared = false
	flagSkillListGlobal = false
	flagSkillListProject = false
	flagJSON = false
	flagQuiet = false
	flagVerbose = false
	// Restore Cobra-managed flag string values so MarkFlagsMutuallyExclusive
	// works correctly on consecutive runs.
	if f := skillInstallCmd.Flags().Lookup("global"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := skillInstallCmd.Flags().Lookup("shared"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := skillInstallCmd.Flags().Lookup("force"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := skillListCmd.Flags().Lookup("global"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := skillListCmd.Flags().Lookup("project"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	t.Cleanup(func() {
		exitCode = 0
		flagSkillForce = false
		flagSkillInstallGlobal = false
		flagSkillInstallShared = false
		flagSkillListGlobal = false
		flagSkillListProject = false
		flagJSON = false
		flagQuiet = false
		flagVerbose = false
	})
}

// runSkillCmdForTest invokes rootCmd with the given args, capturing both
// stdout (which holds renderer output) and Cobra's own err writer. Since the
// production code writes to os.Stdout via ui.Renderer directly, we redirect
// os.Stdout through a pipe and replay the captured bytes into the returned
// buffer. stderr is not redirected here (use captureStderr for that).
func runSkillCmdForTest(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	resetSkillFlagsForTest(t)

	oldStdout := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = pw

	// Mirror cobra writers to discard so usage strings don't pollute stdout.
	rootCmd.SetOut(pw)
	rootCmd.SetErr(pw)
	rootCmd.SetArgs(args)

	errCh := make(chan error, 1)
	go func() {
		errCh <- rootCmd.Execute()
		_ = pw.Close()
	}()

	var stdout bytes.Buffer
	if _, copyErr := stdout.ReadFrom(pr); copyErr != nil {
		t.Logf("read pipe: %v", copyErr)
	}
	execErr := <-errCh
	os.Stdout = oldStdout
	return &stdout, execErr
}

// overrideSkillRegistryURL points the package-level skillRegistryURL at a test
// server for the lifetime of the test.
func overrideSkillRegistryURL(t *testing.T, url string) {
	t.Helper()
	old := skillRegistryURL
	skillRegistryURL = url
	t.Cleanup(func() { skillRegistryURL = old })
}

// startMockSkillRegistry stands up an httptest.Server that serves a single
// skill package at the given name/version. Mirrors skillpkg.setupMockRegistry
// but lives in cmd/rnix package for ATDD-level tests.
func startMockSkillRegistry(t *testing.T, name, version string) *httptest.Server {
	t.Helper()
	tarData := buildSkillTarball(t, name)
	checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(tarData))

	indexYAML := fmt.Sprintf("skills:\n  - name: %s\n    description: \"A test skill\"\n    latest: \"%s\"\n", name, version)
	latestYAML := fmt.Sprintf("version: \"%s\"\nchecksum: \"%s\"\n", version, checksum)

	mux := http.NewServeMux()
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(indexYAML))
	})
	mux.HandleFunc(fmt.Sprintf("/packages/%s/latest.yaml", name), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(latestYAML))
	})
	mux.HandleFunc(fmt.Sprintf("/packages/%s/%s.tar.gz", name, version), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(tarData)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// buildSkillTarball constructs an in-memory .tar.gz containing a valid
// SKILL.md for the given name. Used by the install/update ATDD tests.
func buildSkillTarball(t *testing.T, name string) []byte {
	t.Helper()
	content := fmt.Sprintf(`---
name: %s
description: "A test skill"
allowed-tools: /dev/fs
metadata:
  author: atdd
  version: "1.0.0"
---

# %s

Mock skill body.
`, name, name)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte(content)
	if err := tw.WriteHeader(&tar.Header{Name: "SKILL.md", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// mockPkgServeFixture wires a mock registry into the test for install/update
// scenarios. Replaces the original placeholder helper.
func mockPkgServeFixture(t *testing.T, name, version string) {
	t.Helper()
	srv := startMockSkillRegistry(t, name, version)
	overrideSkillRegistryURL(t, srv.URL)
}
