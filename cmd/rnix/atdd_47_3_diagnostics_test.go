package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================
// ATDD — Story 47.3: 空列表诊断 + lenient/shadow warnings 写 stderr +
// JSON 模式 diagnostics 节点
// ============================================================

// --- 47.3-CLI-AC6-001: [P0] 全 4 路径不存在 → 打印 Scanned paths 4 行 ---

func TestSkillList_EmptyAllScopes_DiagnosticPrinted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cwd := t.TempDir()
	t.Chdir(cwd)

	stdout, _ := runSkillCmdForTest(t, "skill", "list")
	out := stdout.String()
	if !strings.Contains(out, "No skills found. Scanned paths:") {
		t.Errorf("expected diagnostic header, got:\n%s", out)
	}
	if !strings.Contains(out, "not-found") {
		t.Errorf("expected at least one path marked 'not-found', got:\n%s", out)
	}
	requiredFragments := []string{
		filepath.Join(cwd, ".rnix", "skills"),
		filepath.Join(cwd, ".agents", "skills"),
		filepath.Join(home, ".config", "rnix", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	for _, frag := range requiredFragments {
		if !strings.Contains(out, frag) {
			t.Errorf("expected diagnostic to mention path %q, got:\n%s", frag, out)
		}
	}
}

// --- 47.3-CLI-AC6-002: [P0] project/native 路径存在但内部为空 → "existed-but-empty" ---

func TestSkillList_EmptyProjectExisted_EmptyDiagnostic(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cwd := t.TempDir()
	emptyProjNative := filepath.Join(cwd, ".rnix", "skills")
	if err := os.MkdirAll(emptyProjNative, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(cwd)

	stdout, _ := runSkillCmdForTest(t, "skill", "list")
	out := stdout.String()
	if !strings.Contains(out, "existed-but-empty") {
		t.Errorf("expected 'existed-but-empty' marker for empty project/native, got:\n%s", out)
	}
	wantLine := emptyProjNative + " (existed-but-empty)"
	if !strings.Contains(out, wantLine) {
		t.Errorf("expected diagnostic line %q, got:\n%s", wantLine, out)
	}
}

// --- 47.3-CLI-AC6-003: [P1] quiet 模式不打印诊断 ---

func TestSkillList_QuietMode_NoDiagnostic(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "--quiet")
	out := stdout.String()
	if strings.Contains(out, "Scanned paths:") || strings.Contains(out, "No skills found") {
		t.Errorf("quiet mode should NOT print diagnostic, got:\n%s", out)
	}
}

// --- 47.3-CLI-AC6-004: [P1] 诊断行输出到 stdout（不是 stderr）---

func TestSkillList_DiagnosticGoesToStdout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	var stdout *bytes.Buffer
	stderr := captureStderr(t, func() {
		stdout, _ = runSkillCmdForTest(t, "skill", "list")
	})

	if !strings.Contains(stdout.String(), "Scanned paths:") {
		t.Errorf("expected diagnostic in stdout, got stdout=%q", stdout.String())
	}
	if strings.Contains(stderr, "Scanned paths:") {
		t.Errorf("AC6 diagnostic should NOT be in stderr, got stderr=%q", stderr)
	}
}

// --- 47.3-CLI-AC8-001: [P0] lenient warnings → stderr ---

func TestSkillList_LenientWarnings_RenderedToStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	skillDir := filepath.Join(home, ".config", "rnix", "skills", "actual-dir-name")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: different-name\nversion: 1.0.0\ndescription: trigger lenient\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "list")
	})

	if !strings.Contains(stderr, "warning:") {
		t.Errorf("expected 'warning:' in stderr from lenient validation, got stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "mismatch") &&
		!strings.Contains(stderr, "name") {
		t.Errorf("expected lenient warning detail in stderr, got: %s", stderr)
	}
}

// --- 47.3-CLI-AC8-002: [P0] shadow warnings → stderr ---

func TestSkillList_ShadowWarnings_RenderedToStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()
	writeFixtureSkill(t,
		filepath.Join(projectDir, ".rnix", "skills", "dup-skill"),
		"dup-skill", "1.0.0", "winner")

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "dup-skill"),
		"dup-skill", "0.5.0", "shadowed")

	t.Chdir(projectDir)

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "list")
	})

	if !strings.Contains(stderr, "shadowed skill") {
		t.Errorf("expected 'shadowed skill' in stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "dup-skill") {
		t.Errorf("expected skill name 'dup-skill' in stderr warning, got: %s", stderr)
	}
}

// --- 47.3-CLI-AC8-003: [P1] skipped warnings → stderr ---

func TestSkillList_SkippedWarnings_RenderedToStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	skillDir := filepath.Join(home, ".config", "rnix", "skills", "no-desc")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: no-desc\nversion: 1.0.0\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "list")
	})

	if !strings.Contains(stderr, "skipped skill") {
		t.Errorf("expected 'skipped skill' in stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, skillDir) {
		t.Errorf("expected skipped skill path %q in stderr, got: %s", skillDir, stderr)
	}
}

// --- 47.3-CLI-AC9-001: [P0] JSON 模式：diagnostics 顶层节点 ---

func TestSkillList_JSON_DiagnosticsField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "valid-skill"),
		"valid-skill", "1.0.0", "good")

	leDir := filepath.Join(home, ".config", "rnix", "skills", "actual-name")
	if err := os.MkdirAll(leDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: wrong-name\nversion: 1.0.0\ndescription: lenient trigger\n---\n"
	if err := os.WriteFile(filepath.Join(leDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "--json")
	raw := stdout.String()

	var resp JSONResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, raw)
	}

	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("expected 'diagnostics' top-level key in JSON, got: %s", raw)
	}
	if !strings.Contains(raw, `"lenient"`) {
		t.Errorf("expected 'lenient' subkey under diagnostics (1 entry), got: %s", raw)
	}
}

// --- 47.3-CLI-AC9-002: [P0] JSON 模式 ListEntry 含 scope/namespace/shadowed ---

func TestSkillList_JSON_ListEntryHasScopeFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "test-skill"),
		"test-skill", "1.0.0", "test")

	t.Chdir(t.TempDir())

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "--json")
	raw := stdout.String()
	for _, key := range []string{`"scope"`, `"namespace"`, `"shadowed"`} {
		if !strings.Contains(raw, key) {
			t.Errorf("expected ListEntry to expose %s in JSON, got: %s", key, raw)
		}
	}
	if !strings.Contains(raw, `"user"`) {
		t.Errorf("expected scope value 'user', got: %s", raw)
	}
	if !strings.Contains(raw, `"native"`) {
		t.Errorf("expected namespace value 'native', got: %s", raw)
	}
}

// --- 47.3-CLI-AC9-003: [P0] JSON 模式 stderr silent ---

func TestSkillList_JSON_NoStderrOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	leDir := filepath.Join(home, ".config", "rnix", "skills", "lenient-dir")
	if err := os.MkdirAll(leDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: different-name\nversion: 1.0.0\ndescription: test\n---\n"
	if err := os.WriteFile(filepath.Join(leDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	var stdout *bytes.Buffer
	stderr := captureStderr(t, func() {
		stdout, _ = runSkillCmdForTest(t, "skill", "list", "--json")
	})

	if !strings.Contains(stdout.String(), `"diagnostics"`) {
		t.Errorf("expected JSON diagnostics on stdout, got stdout=%s", stdout.String())
	}
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("expected stderr to be empty in JSON mode, got: %q", stderr)
	}
}

// --- 47.3-CLI-AC9-004: [P1] JSON 模式空列表 — diagnostics 仍存在 ---

func TestSkillList_JSON_EmptyDiagnosticsAlwaysPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "clean-skill"),
		"clean-skill", "1.0.0", "no diag")

	t.Chdir(t.TempDir())

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "--json")
	raw := stdout.String()
	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("expected 'diagnostics' key in JSON even when empty, got: %s", raw)
	}
}

// --- 47.3-CLI-AC8-004: [P2] stderr 输出无 lipgloss 样式（简化处理）---

func TestSkillList_StderrWarnings_PlainText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	leDir := filepath.Join(home, ".config", "rnix", "skills", "le-skill")
	if err := os.MkdirAll(leDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: different\nversion: 1.0.0\ndescription: test\n---\n"
	if err := os.WriteFile(filepath.Join(leDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	stderr := captureStderr(t, func() {
		_, _ = runSkillCmdForTest(t, "skill", "list")
	})

	if strings.Contains(stderr, "\x1b[") {
		t.Errorf("expected stderr to be plain text (no ANSI escapes), got: %q", stderr)
	}
}

// ============================================================
// Test helpers
// ============================================================

// captureStderr redirects os.Stderr through a pipe during fn() and returns the
// captured output. Tests must not be t.Parallel() because os.Stderr is
// process-global.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	_ = w.Close()
	<-done
	return buf.String()
}
