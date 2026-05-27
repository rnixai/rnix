package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/config"
)

// ============================================================
// ATDD RED PHASE — Story 47.3: skill list -g/-p 过滤 + SCOPE/NAMESPACE 列
//
// 覆盖：
//   - AC5: list 注册 -g/--global + -p/--project + 互斥 + filterScopes helper
//   - AC7: list 表格新增 SCOPE / NAMESPACE 两列
//
// RED → GREEN: 完成 cmd/rnix/skill.go list flag 注册 + filterScopes helper
// + 表头/行格式串后，t.Skip 移除，断言通过。
// ============================================================

// --- 47.3-CLI-AC5-001: [P0] skill list 注册 -g/--global flag ---

// TestSkillList_GlobalFlagRegistered 验证 `--global` (短 flag `-g`)
// 在 skillListCmd 注册。注意：与 install 的语义不同 — list 的 -g 是
// "只显示 user scope 的 skill" 过滤语义。
//
// AC5 / 任务 5.1。
func TestSkillList_GlobalFlagRegistered(t *testing.T) {
	t.Skip("RED PHASE 47.3: list 需注册 -g/--global flag")

	listCmd := findSubCommand(t, "skill", "list")

	global := listCmd.Flags().Lookup("global")
	if global == nil {
		t.Fatal("expected --global flag registered on skillListCmd, got nil")
	}
	if global.Shorthand != "g" {
		t.Errorf("expected --global short flag 'g', got %q", global.Shorthand)
	}
}

// --- 47.3-CLI-AC5-002: [P0] skill list 注册 -p/--project flag ---

// TestSkillList_ProjectFlagRegistered 验证 `--project` (短 flag `-p`)
// 在 skillListCmd 注册。"只显示 project scope 下的 skill" 过滤语义。
func TestSkillList_ProjectFlagRegistered(t *testing.T) {
	t.Skip("RED PHASE 47.3: list 需注册 -p/--project flag")

	listCmd := findSubCommand(t, "skill", "list")

	project := listCmd.Flags().Lookup("project")
	if project == nil {
		t.Fatal("expected --project flag registered on skillListCmd, got nil")
	}
	if project.Shorthand != "p" {
		t.Errorf("expected --project short flag 'p', got %q", project.Shorthand)
	}
}

// --- 47.3-CLI-AC5-003: [P0] -g 与 -p 互斥 ---

// TestSkillList_GlobalAndProject_MutuallyExclusive 验证 AC5：
// `rnix skill list -g -p` 触发 Cobra 的 MarkFlagsMutuallyExclusive 检查
// 报错，非静默通过。
//
// 任务 5.2。
func TestSkillList_GlobalAndProject_MutuallyExclusive(t *testing.T) {
	t.Skip("RED PHASE 47.3: -g 与 -p 应互斥")

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "-g", "-p"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when both -g and -p set, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") &&
		!strings.Contains(stdout.String(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error or output; got err=%v out=%q", err, stdout.String())
	}
}

// --- 47.3-CLI-AC5-004: [P0] -g 过滤：只显示 user scope skill ---

// TestSkillList_GlobalFlag_OnlyUserScope 端到端验证 AC5：
// 项目内 (.rnix/skills/) 有 skill A，user (.config/rnix/skills/) 有
// skill B。`rnix skill list -g` 应只显示 B（user scope），不显示 A。
//
// 任务 5.5。
func TestSkillList_GlobalFlag_OnlyUserScope(t *testing.T) {
	t.Skip("RED PHASE 47.3: -g 过滤应只显示 user scope")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()

	// Project scope skill A.
	projA := filepath.Join(projectDir, ".rnix", "skills", "alpha-skill")
	writeFixtureSkill(t, projA, "alpha-skill", "1.0.0", "project-scope skill")

	// User scope skill B.
	userB := filepath.Join(home, ".config", "rnix", "skills", "beta-skill")
	writeFixtureSkill(t, userB, "beta-skill", "1.0.0", "user-scope skill")

	t.Chdir(projectDir)

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "-g", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, stdout.String())
	}
	raw := stdout.String()
	if strings.Contains(raw, `"alpha-skill"`) {
		t.Errorf("alpha-skill should be filtered out by -g (project scope), got: %s", raw)
	}
	if !strings.Contains(raw, `"beta-skill"`) {
		t.Errorf("beta-skill should be present (user scope), got: %s", raw)
	}
}

// --- 47.3-CLI-AC5-005: [P0] -p 过滤：只显示 project scope skill ---

// TestSkillList_ProjectFlag_OnlyProjectScope 验证 AC5：
// `rnix skill list -p` 镜像测试 — 只显示 alpha-skill (project)，不显
// 示 beta-skill (user)。
func TestSkillList_ProjectFlag_OnlyProjectScope(t *testing.T) {
	t.Skip("RED PHASE 47.3: -p 过滤应只显示 project scope")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()

	writeFixtureSkill(t,
		filepath.Join(projectDir, ".rnix", "skills", "alpha-skill"),
		"alpha-skill", "1.0.0", "project-scope skill")

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "beta-skill"),
		"beta-skill", "1.0.0", "user-scope skill")

	t.Chdir(projectDir)

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "-p", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	raw := stdout.String()
	if !strings.Contains(raw, `"alpha-skill"`) {
		t.Errorf("alpha-skill should be present (project scope), got: %s", raw)
	}
	if strings.Contains(raw, `"beta-skill"`) {
		t.Errorf("beta-skill should be filtered out by -p (user scope), got: %s", raw)
	}
}

// --- 47.3-CLI-AC5-006: [P1] filterScopes 内部 helper 行为 ---

// TestFilterScopes_PreservesPriorityOrder 验证 AC5 dev-note：filterScopes
// 只过滤 .Scope 字段，保留 priority 顺序（project/native > project/agents
// > user/native > user/agents）。
//
// 任务 5.3。
func TestFilterScopes_PreservesPriorityOrder(t *testing.T) {
	t.Skip("RED PHASE 47.3: filterScopes helper 待实现")

	scopes := []config.ScopePath{
		{Path: "/p/.rnix/skills", Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
		{Path: "/p/.agents/skills", Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceAgents},
		{Path: "/h/.config/rnix/skills", Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceRnix},
		{Path: "/h/.agents/skills", Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceAgents},
	}

	// After GREEN:
	// gotUser := filterScopes(scopes, config.SkillScopeUser)
	// if len(gotUser) != 2 { ... }
	// if gotUser[0].Path != "/h/.config/rnix/skills" { ... }  // priority preserved
	// if gotUser[1].Path != "/h/.agents/skills" { ... }
	//
	// gotProj := filterScopes(scopes, config.SkillScopeProject)
	// if len(gotProj) != 2 { ... }

	_ = scopes
	t.Fatal("RED PHASE: filterScopes helper missing")
}

// --- 47.3-CLI-AC7-001: [P0] list 表格新增 SCOPE 与 NAMESPACE 列 ---

// TestSkillList_DefaultMode_RendersScopeNamespaceColumns 验证 AC7：
// 默认（非 JSON / 非 quiet）模式表头从
// `NAME VERSION SOURCE DESCRIPTION` 改为
// `NAME VERSION SOURCE SCOPE NAMESPACE DESCRIPTION`。
//
// 任务 7.3。
func TestSkillList_DefaultMode_RendersScopeNamespaceColumns(t *testing.T) {
	t.Skip("RED PHASE 47.3: list 默认表头需含 SCOPE / NAMESPACE 列")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()

	// project/native skill
	writeFixtureSkill(t,
		filepath.Join(projectDir, ".rnix", "skills", "skill-a"),
		"skill-a", "1.0.0", "first")

	// user/agents skill
	writeFixtureSkill(t,
		filepath.Join(home, ".agents", "skills", "skill-b"),
		"skill-b", "1.0.0", "second")

	t.Chdir(projectDir)

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	// Header row contains both new column labels.
	if !strings.Contains(out, "SCOPE") {
		t.Errorf("expected SCOPE column header in default-mode output, got:\n%s", out)
	}
	if !strings.Contains(out, "NAMESPACE") {
		t.Errorf("expected NAMESPACE column header in default-mode output, got:\n%s", out)
	}
	// Skill rows render the actual scope/namespace value.
	if !strings.Contains(out, "project") || !strings.Contains(out, "native") {
		t.Errorf("expected 'project' + 'native' for skill-a row, got:\n%s", out)
	}
	if !strings.Contains(out, "user") || !strings.Contains(out, "agents") {
		t.Errorf("expected 'user' + 'agents' for skill-b row, got:\n%s", out)
	}
}

// --- 47.3-CLI-AC7-002: [P2] quiet 模式不渲染新列 ---

// TestSkillList_QuietMode_NoNewColumns 验证 AC7：quiet 模式（既有行为：
// 只输出 name）不渲染 SCOPE / NAMESPACE 列。
func TestSkillList_QuietMode_NoNewColumns(t *testing.T) {
	t.Skip("RED PHASE 47.3: quiet 模式不渲染新列")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "quiet-skill"),
		"quiet-skill", "1.0.0", "quiet mode test")

	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "--quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	if strings.Contains(out, "SCOPE") || strings.Contains(out, "NAMESPACE") {
		t.Errorf("quiet mode should NOT render new columns, got:\n%s", out)
	}
	if !strings.Contains(out, "quiet-skill") {
		t.Errorf("quiet mode should still output skill name, got:\n%s", out)
	}
}

// --- 47.3-CLI-AC5-007: [P1] -g 后过滤空 → 走 AC6 空诊断分支 ---

// TestSkillList_GlobalFlag_EmptyUserScope_DiagnosticPath 验证 AC5 + AC6
// 桥接：用户传 -g 但 user scope 全空，应进入 AC6 "No skills found.
// Scanned paths:" 诊断分支（覆盖 user/native + user/agents 两条路径）。
func TestSkillList_GlobalFlag_EmptyUserScope_DiagnosticPath(t *testing.T) {
	t.Skip("RED PHASE 47.3: -g 过滤后空列表应进入诊断分支")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()
	writeFixtureSkill(t,
		filepath.Join(projectDir, ".rnix", "skills", "in-project"),
		"in-project", "1.0.0", "project only")

	t.Chdir(projectDir)

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "list", "-g"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "No skills found") {
		t.Errorf("expected empty-state diagnostic, got:\n%s", out)
	}
	if strings.Contains(out, "in-project") {
		t.Errorf("in-project skill should be filtered out by -g, got:\n%s", out)
	}
}

// ============================================================
// Local fixture helpers
// ============================================================

// writeFixtureSkill creates a minimum-viable SKILL.md under skillDir for
// list/install tests. The skill has name + description + version frontmatter.
func writeFixtureSkill(t *testing.T, skillDir, name, version, description string) {
	t.Helper()
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", skillDir, err)
	}
	content := strings.Join([]string{
		"---",
		"name: " + name,
		"version: " + version,
		"description: " + description,
		"---",
		"# " + name,
		"",
		"body",
	}, "\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}
