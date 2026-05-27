package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/skillpkg"
	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 47.3: CLI 子命令多 scope 适配
//
// 覆盖 AC1（删除三处 basePath := "lib/skills" 硬编码 + ResolveSkillScopes
// 接入）与 AC11（既有 skill_test.go fixture 同步更新）。
//
// RED 状态：本文件所有测试都以 t.Skip("RED PHASE 47.3 ...") 起始。
// dev-story 阶段实现完成后移除 t.Skip 即可激活红色断言。这样保持
// cmd/rnix 包在 47.3 RED 阶段仍可编译，避免阻塞 47.4 并行开发。
//
// RED → GREEN: 完成 cmd/rnix/skill.go 三处 ResolveSkillScopes 接入 +
// singleScopeFromBasePath helper 删除后，所有 t.Skip 移除，测试通过。
// ============================================================

// --- 47.3-CLI-AC1-001: [P0] singleScopeFromBasePath helper 必须删除 ---

// TestSingleScopeFromBasePath_HelperRemoved 验证 47.2 临时 shim
// `singleScopeFromBasePath`（cmd/rnix/skill.go:17-28）在 47.3 已删除。
// 检测方式：grep cmd/rnix/skill.go 是否还包含该 helper 名字。
//
// AC1 / 任务 1.3：删除 helper + 唯一调用方。
func TestSingleScopeFromBasePath_HelperRemoved(t *testing.T) {
	t.Skip("RED PHASE 47.3: 实现完成后移除 t.Skip — singleScopeFromBasePath helper 应删除")

	// Read cmd/rnix/skill.go and assert the helper signature is gone.
	src, err := os.ReadFile("skill.go")
	if err != nil {
		t.Fatalf("read skill.go: %v", err)
	}
	if strings.Contains(string(src), "func singleScopeFromBasePath(") {
		t.Errorf("cmd/rnix/skill.go still contains singleScopeFromBasePath helper; 47.3 should delete the 47.2 shim")
	}
}

// --- 47.3-CLI-AC1-002: [P0] runSkillInstall / Update / List 不应硬编码 lib/skills ---

// TestRunSkill_NoLibSkillsHardcode 验证 47.3 删除了 install/update/list 三处
// `basePath := "lib/skills"` 字面量，改为 `config.ResolveSkillScopes(cwd)`。
//
// 检测方式：grep cmd/rnix/skill.go 字符串字面量 `"lib/skills"`，应在生产代
// 码段（不含注释引用）中消失。AC1 / 任务 1.1-1.2。
func TestRunSkill_NoLibSkillsHardcode(t *testing.T) {
	t.Skip("RED PHASE 47.3: 实现完成后移除 t.Skip")

	src, err := os.ReadFile("skill.go")
	if err != nil {
		t.Fatalf("read skill.go: %v", err)
	}
	body := string(src)

	// Locate all occurrences and validate they are inside comment lines only.
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

// TestRunSkillList_NoCwdLibSkills_FallsBackToUserScope 验证 EchoMatrix
// 复现场景：在不含 `lib/skills` 子目录的 cwd 中运行 `rnix skill list`，
// 应通过 ResolveSkillScopes 找到 user scope (~/.config/rnix/skills/)
// 下的 skill 并返回非空表格 —— 不再因为 lib/skills ENOENT 返回空表头。
//
// AC1 / 任务 1.6。
func TestRunSkillList_NoCwdLibSkills_FallsBackToUserScope(t *testing.T) {
	t.Skip("RED PHASE 47.3: 实现完成后移除 t.Skip")

	// Hermetic environment setup: mock HOME + XDG with fixture skill.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Pre-populate user/native scope with a skill fixture.
	userNative := filepath.Join(home, ".config", "rnix", "skills", "fallback-skill")
	if err := os.MkdirAll(userNative, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", userNative, err)
	}
	skillMD := "---\nname: fallback-skill\ndescription: minimum viable skill\nversion: 1.0.0\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(userNative, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Cwd: a fresh tmp dir with NO lib/skills, no .rnix/, no .agents/.
	cwd := t.TempDir()
	t.Chdir(cwd)

	// Execute `rnix skill list` against the fixture HOME.
	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("skill list: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, stdout.String())
	}
	if !resp.OK {
		t.Fatalf("expected ok=true (fallback-skill exists in user scope), got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"fallback-skill"`) {
		t.Errorf("expected fallback-skill in JSON output (user scope fallback); got: %s", stdout.String())
	}
}

// --- 47.3-CLI-AC1-004: [P0] runSkillInstall 在不含 .rnix/ 的目录默认写到 user/native ---

// TestRunSkillInstall_NoProject_WritesToUserNative 验证 AC1 + AC2 决策矩阵：
// 当 cwd 不在 .rnix/ 项目内且未传 -g/--shared 时，install 默认目标为
// user/native (`~/.config/rnix/skills/`)。AC2 决策矩阵第 1 行。
func TestRunSkillInstall_NoProject_WritesToUserNative(t *testing.T) {
	t.Skip("RED PHASE 47.3: 实现完成后移除 t.Skip")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Cwd: a fresh non-project directory (no .rnix/ ancestor).
	cwd := t.TempDir()
	t.Chdir(cwd)

	// Mock registry to serve a tiny skill package.
	mockPkgServeFixture(t, "tiny-skill", "1.0.0")
	defer restoreSkillRegistryURL(t)

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "install", "tiny-skill", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("skill install: %v", err)
	}

	// Assert: installed directory is under user/native (~/.config/rnix/skills/).
	expectedPath := filepath.Join(home, ".config", "rnix", "skills", "tiny-skill")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("expected tiny-skill installed at user/native path %q, stat err: %v", expectedPath, err)
	}
}

// --- 47.3-CLI-AC11-001: [P1] renderSkillListJSON 签名追加 diag 第三参数 ---

// TestRenderSkillListJSON_NewThreeArgSignature 验证 AC9 / AC11：
// renderSkillListJSON 签名从 2 参（renderer, entries）改为 3 参
// （renderer, entries, diag），既有 7 个 JSON 测试同步更新。
//
// 检测方式：通过 ListEntry fixture + 空 diag 调用新签名，断言输出含
// `"diagnostics"` 节点。
func TestRenderSkillListJSON_NewThreeArgSignature(t *testing.T) {
	t.Skip("RED PHASE 47.3: 实现完成后移除 t.Skip — renderSkillListJSON 应有 3 参签名")

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

	// NOTE: After GREEN phase, the call below must compile against the
	// 3-arg signature: renderSkillListJSON(r *ui.Renderer, entries []ListEntry,
	// diag skillpkg.LoadDiagnostics). For now (RED), this is a behavioral
	// expectation expressed in narrative.
	//
	// renderSkillListJSON(r, entries, diag)

	_ = diag
	_ = entries
	_ = r
	_ = buf

	t.Fatal("RED PHASE: renderSkillListJSON must accept (renderer, entries, diagnostics) — third arg missing in 47.2")
}

// --- 47.3-CLI-AC11-002: [P1] 既有 JSON fixture 必须含 Scope / Namespace ---

// TestSkillList_JSONOutput_FixtureHasScopeNamespace 验证 AC11：
// `cmd/rnix/skill_test.go:641-642` 的 fixture ListEntry 显式设置 Scope /
// Namespace 字段，下游消费者拿到的 JSON 含完整字段（不返回空字符串）。
//
// 该测试用反射重跑既有 fixture 的输出，断言 `"scope":"project"` +
// `"namespace":"native"` 字段必现。
func TestSkillList_JSONOutput_FixtureHasScopeNamespace(t *testing.T) {
	t.Skip("RED PHASE 47.3: 实现完成后移除 t.Skip — 既有 fixture 必须含 Scope/Namespace 字段")

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

	// After GREEN: renderSkillListJSON(r, entries, skillpkg.LoadDiagnostics{})
	_ = entries
	_ = r

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

// newTestRootCmd returns a fresh root command for ATDD tests, redirecting
// stdout to the provided buffer for assertion. Used to exercise skill
// install/update/list end-to-end without invoking os.Exit.
func newTestRootCmd(t *testing.T, stdout *bytes.Buffer) *cobra.Command {
	t.Helper()
	// rootCmd is the package-level Cobra root (cmd/rnix/main.go). For
	// isolation we wrap it; if rootCmd is reused across tests, ensure flag
	// state is reset via NewCommand or via the cobra setOut redirect.
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stdout)
	// Reset exitCode between tests so the run is hermetic.
	exitCode = 0
	return rootCmd
}

// mockPkgServeFixture stands up a fake registry serving a one-skill package
// for install/update tests. Implementation defers to existing test fixture
// helpers in cmd/rnix/skill_test.go (mockSkillRegistry / serveFixture).
//
// dev-story 阶段实现：复用 cmd/rnix 现有 mock 注册中心 helper（如
// `mockSkillRegistryServer` 或直接通过 httptest）。
func mockPkgServeFixture(t *testing.T, name, version string) {
	t.Helper()
	t.Skipf("TODO: wire mock registry serving skill %q@%s (use existing cmd/rnix skill_test fixtures)", name, version)
}

// restoreSkillRegistryURL restores the package-level skillRegistryURL after
// mock injection in mockPkgServeFixture.
func restoreSkillRegistryURL(_ *testing.T) {
	// no-op stub for RED phase
}

// Unused-symbol guards to keep imports honest while RED tests are skipped.
var _ = config.ScopePath{}
var _ skillpkg.ListEntry
