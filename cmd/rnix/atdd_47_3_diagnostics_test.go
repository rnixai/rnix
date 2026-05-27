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
// ATDD RED PHASE — Story 47.3: 空列表诊断 + lenient/shadow warnings 写
// stderr + JSON 模式 diagnostics 节点
//
// 覆盖：
//   - AC6: 非 JSON 空列表打印"Scanned paths"诊断行（stdout）
//   - AC8: lenient/shadow/skipped warnings 写 stderr（非 JSON 模式）
//   - AC9: JSON 模式 diagnostics 顶层节点 + ListEntry 含 Scope/Namespace/Shadowed
//
// 关键 dev 防错点（详见 story §Dev Notes #8）：
//   - AC6 "Scanned paths" → stdout（用户主动想看）
//   - AC8 warnings → stderr（伴随成功结果的警告）
//   - AC9 JSON 模式 → 全 stdout，stderr silent
//
// RED → GREEN: 完成 diagnosticScopePaths + renderDiagnosticsToStderr +
// renderSkillListJSON 三参签名 + skillListJSONData.Diagnostics 字段后，
// t.Skip 移除，断言通过。
// ============================================================

// --- 47.3-CLI-AC6-001: [P0] 全 4 路径不存在 → 打印 Scanned paths 4 行 ---

// TestSkillList_EmptyAllScopes_DiagnosticPrinted 验证 AC6：
// HOME + cwd 都不含任何 skill 目录时，list 默认模式输出表头 + 4 条
// "Scanned paths:" 诊断行（均标 "not-found"）+ tip。
//
// 任务 6.4。
func TestSkillList_EmptyAllScopes_DiagnosticPrinted(t *testing.T) {
	t.Skip("RED PHASE 47.3: 空列表应打印 Scanned paths 诊断 4 行")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cwd := t.TempDir()
	t.Chdir(cwd)

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "No skills found. Scanned paths:") {
		t.Errorf("expected diagnostic header, got:\n%s", out)
	}
	// All 4 paths should appear with not-found marker.
	if !strings.Contains(out, "not-found") {
		t.Errorf("expected at least one path marked 'not-found', got:\n%s", out)
	}
	// 4 paths × at least 1 ".rnix" / ".agents" / "config" substring each.
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

// TestSkillList_EmptyProjectExisted_EmptyDiagnostic 验证 AC6：
// project/native 路径存在但内部无 skill 子目录 → 标 "existed-but-empty"；
// 其余 3 个路径不存在 → 标 "not-found"。
func TestSkillList_EmptyProjectExisted_EmptyDiagnostic(t *testing.T) {
	t.Skip("RED PHASE 47.3: 存在但空目录应标 existed-but-empty")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cwd := t.TempDir()
	// Create project/native dir but leave it empty.
	emptyProjNative := filepath.Join(cwd, ".rnix", "skills")
	if err := os.MkdirAll(emptyProjNative, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(cwd)

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "existed-but-empty") {
		t.Errorf("expected 'existed-but-empty' marker for empty project/native, got:\n%s", out)
	}
	// project/native should appear with existed-but-empty
	wantLine := emptyProjNative + " (existed-but-empty)"
	if !strings.Contains(out, wantLine) {
		t.Errorf("expected diagnostic line %q, got:\n%s", wantLine, out)
	}
}

// --- 47.3-CLI-AC6-003: [P1] quiet 模式不打印诊断 ---

// TestSkillList_QuietMode_NoDiagnostic 验证 AC6：
// `--quiet` 与既有 runSkillSearch quiet 行为对齐 — 空列表也不输出诊断。
func TestSkillList_QuietMode_NoDiagnostic(t *testing.T) {
	t.Skip("RED PHASE 47.3: quiet 模式不打印 Scanned paths")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "--quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	if strings.Contains(out, "Scanned paths:") || strings.Contains(out, "No skills found") {
		t.Errorf("quiet mode should NOT print diagnostic, got:\n%s", out)
	}
}

// --- 47.3-CLI-AC6-004: [P1] 诊断行输出到 stdout（不是 stderr）---

// TestSkillList_DiagnosticGoesToStdout 验证 AC6 dev-note #8：
// "Scanned paths" 诊断行**必须**输出到 stdout（renderer.Writer），
// 与 AC8 的 stderr warnings 严格区分。
func TestSkillList_DiagnosticGoesToStdout(t *testing.T) {
	t.Skip("RED PHASE 47.3: AC6 诊断行必须 stdout 不能 stderr")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		root := newTestRootCmd(t, &stdout)
		root.SetArgs([]string{"skill", "list"})
		_ = root.Execute()
	})

	if !strings.Contains(stdout.String(), "Scanned paths:") {
		t.Errorf("expected diagnostic in stdout, got stdout=%q", stdout.String())
	}
	if strings.Contains(stderr, "Scanned paths:") {
		t.Errorf("AC6 diagnostic should NOT be in stderr, got stderr=%q", stderr)
	}
}

// --- 47.3-CLI-AC8-001: [P0] lenient warnings → stderr ---

// TestSkillList_LenientWarnings_RenderedToStderr 验证 AC8：
// 一个 SKILL.md 触发 LenientWarning（如 name mismatch），主表格仍含该
// skill，但 stderr 多一行 `[skill] warning: <path>: <field>: ...`。
//
// 任务 8.4。
func TestSkillList_LenientWarnings_RenderedToStderr(t *testing.T) {
	t.Skip("RED PHASE 47.3: lenient warnings 应写 stderr")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Create a skill whose SKILL.md name differs from parentDir name —
	// triggers SkillLoader lenient validation "name mismatch".
	skillDir := filepath.Join(home, ".config", "rnix", "skills", "actual-dir-name")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// manifest.Name = "different-name" but parentDir = "actual-dir-name"
	md := "---\nname: different-name\nversion: 1.0.0\ndescription: trigger lenient\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		root := newTestRootCmd(t, &stdout)
		root.SetArgs([]string{"skill", "list"})
		_ = root.Execute()
	})

	// stderr should carry the lenient warning.
	if !strings.Contains(stderr, "warning:") {
		t.Errorf("expected 'warning:' in stderr from lenient validation, got stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "name mismatch") &&
		!strings.Contains(stderr, "mismatch") {
		t.Errorf("expected lenient warning detail (name mismatch) in stderr, got: %s", stderr)
	}
}

// --- 47.3-CLI-AC8-002: [P0] shadow warnings → stderr ---

// TestSkillList_ShadowWarnings_RenderedToStderr 验证 AC8：
// 项目和 user scope 同名 skill → shadow warning 写 stderr 一行；主表格
// 仍只显示 winner（project）。格式：`[skill] warning: shadowed skill
// "<name>": winner=<wpath> (<wscope>/<wns>); shadowed=<spath> ...`。
func TestSkillList_ShadowWarnings_RenderedToStderr(t *testing.T) {
	t.Skip("RED PHASE 47.3: shadow warnings 应写 stderr")

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

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		root := newTestRootCmd(t, &stdout)
		root.SetArgs([]string{"skill", "list"})
		_ = root.Execute()
	})

	if !strings.Contains(stderr, "shadowed skill") {
		t.Errorf("expected 'shadowed skill' in stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "dup-skill") {
		t.Errorf("expected skill name 'dup-skill' in stderr warning, got: %s", stderr)
	}
}

// --- 47.3-CLI-AC8-003: [P1] skipped warnings → stderr ---

// TestSkillList_SkippedWarnings_RenderedToStderr 验证 AC8：
// SKILL.md 缺 description 等致命缺陷 → SkipEntry → stderr 一行
// `[skill] warning: skipped skill at <path>: <reason>`。
func TestSkillList_SkippedWarnings_RenderedToStderr(t *testing.T) {
	t.Skip("RED PHASE 47.3: skipped warnings 应写 stderr")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// SKILL.md missing description field → SkipEntry.
	skillDir := filepath.Join(home, ".config", "rnix", "skills", "no-desc")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: no-desc\nversion: 1.0.0\n---\nbody\n" // no description
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		root := newTestRootCmd(t, &stdout)
		root.SetArgs([]string{"skill", "list"})
		_ = root.Execute()
	})

	if !strings.Contains(stderr, "skipped skill") {
		t.Errorf("expected 'skipped skill' in stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, skillDir) {
		t.Errorf("expected skipped skill path %q in stderr, got: %s", skillDir, stderr)
	}
}

// --- 47.3-CLI-AC9-001: [P0] JSON 模式：diagnostics 顶层节点 ---

// TestSkillList_JSON_DiagnosticsField 验证 AC9：
// JSON 模式输出含 `"diagnostics"` 顶层节点，含 warnings/skipped/lenient
// 三个子数组（omitempty）。
//
// 任务 9.5。
func TestSkillList_JSON_DiagnosticsField(t *testing.T) {
	t.Skip("RED PHASE 47.3: JSON 模式需含 diagnostics 顶层节点")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// One lenient + one valid skill.
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

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

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

// TestSkillList_JSON_ListEntryHasScopeFields 验证 AC9：
// JSON 输出每个 skill 含 `"scope"` + `"namespace"` + `"shadowed"` 字段
// （ListEntry 47.2 新增字段透传）。
func TestSkillList_JSON_ListEntryHasScopeFields(t *testing.T) {
	t.Skip("RED PHASE 47.3: JSON ListEntry 需含 scope/namespace/shadowed")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "test-skill"),
		"test-skill", "1.0.0", "test")

	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

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

// TestSkillList_JSON_NoStderrOutput 验证 AC9：
// JSON 模式下 stderr 不应输出文本诊断（避免双重信号 + 让 JSON 消费者
// 只解析 stdout JSON）。
//
// 任务 9.4。
func TestSkillList_JSON_NoStderrOutput(t *testing.T) {
	t.Skip("RED PHASE 47.3: JSON 模式 stderr 应 silent")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Lenient skill to trigger diagnostics that would otherwise go to stderr.
	leDir := filepath.Join(home, ".config", "rnix", "skills", "lenient-dir")
	if err := os.MkdirAll(leDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: different-name\nversion: 1.0.0\ndescription: test\n---\n"
	if err := os.WriteFile(filepath.Join(leDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		root := newTestRootCmd(t, &stdout)
		root.SetArgs([]string{"skill", "list", "--json"})
		_ = root.Execute()
	})

	// JSON output present on stdout
	if !strings.Contains(stdout.String(), `"diagnostics"`) {
		t.Errorf("expected JSON diagnostics on stdout, got stdout=%s", stdout.String())
	}
	// stderr should be empty in JSON mode
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("expected stderr to be empty in JSON mode, got: %q", stderr)
	}
}

// --- 47.3-CLI-AC9-004: [P1] JSON 模式空列表 — diagnostics 仍存在 ---

// TestSkillList_JSON_EmptyDiagnosticsAlwaysPresent 验证 AC9：
// `diagnostics` 字段始终存在于 JSON（即便所有 channel 为空，输出
// `"diagnostics":{}`）。这与 ListEntry 数组 `[]` vs `null` 的处理对齐。
func TestSkillList_JSON_EmptyDiagnosticsAlwaysPresent(t *testing.T) {
	t.Skip("RED PHASE 47.3: 空 diagnostics 仍应序列化为 {}")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "clean-skill"),
		"clean-skill", "1.0.0", "no diag")

	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	raw := stdout.String()
	if !strings.Contains(raw, `"diagnostics"`) {
		t.Errorf("expected 'diagnostics' key in JSON even when empty, got: %s", raw)
	}
}

// --- 47.3-CLI-AC8-004: [P2] stderr 输出无 lipgloss 样式（简化处理）---

// TestSkillList_StderrWarnings_PlainText 验证 AC8 dev-note：
// stderr 输出**不**用 KernelStyle / lipgloss 颜色渲染（stderr 通常无色
// 终端），简化测试断言。
func TestSkillList_StderrWarnings_PlainText(t *testing.T) {
	t.Skip("RED PHASE 47.3: stderr 警告应纯文本不带 ANSI 转义")

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

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		root := newTestRootCmd(t, &stdout)
		root.SetArgs([]string{"skill", "list"})
		_ = root.Execute()
	})

	// No ANSI escape sequences in stderr.
	if strings.Contains(stderr, "\x1b[") {
		t.Errorf("expected stderr to be plain text (no ANSI escapes), got: %q", stderr)
	}
}

// ============================================================
// Test helpers
// ============================================================

// captureStderr redirects os.Stderr to a pipe during fn() and returns the
// captured output. Tests must not be t.Parallel() because os.Stderr is
// process-global; -race detector will surface any accidental concurrency.
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
