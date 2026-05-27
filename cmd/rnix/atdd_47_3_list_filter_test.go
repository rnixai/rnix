package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/config"
)

// ============================================================
// ATDD — Story 47.3: skill list -g/-p 过滤 + SCOPE/NAMESPACE 列
//
// 覆盖：
//   - AC5: list 注册 -g/--global + -p/--project + 互斥 + filterScopes helper
//   - AC7: list 表格新增 SCOPE / NAMESPACE 两列
// ============================================================

// --- 47.3-CLI-AC5-001: [P0] skill list 注册 -g/--global flag ---

func TestSkillList_GlobalFlagRegistered(t *testing.T) {
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

func TestSkillList_ProjectFlagRegistered(t *testing.T) {
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

func TestSkillList_GlobalAndProject_MutuallyExclusive(t *testing.T) {
	stdout, err := runSkillCmdForTest(t, "skill", "list", "-g", "-p")
	if err == nil {
		t.Fatal("expected error when both -g and -p set, got nil")
	}
	// Cobra v1.10's MarkFlagsMutuallyExclusive emits:
	// "if any flags in the group [global project] are set none of the others can be"
	combined := err.Error() + " " + stdout.String()
	if !strings.Contains(combined, "[global project]") {
		t.Errorf("expected '[global project]' in error/output; got err=%v out=%q", err, stdout.String())
	}
	if !strings.Contains(combined, "none of the others can be") &&
		!strings.Contains(combined, "mutually exclusive") {
		t.Errorf("expected mutually-exclusive language; got err=%v out=%q", err, stdout.String())
	}
}

// --- 47.3-CLI-AC5-004: [P0] -g 过滤：只显示 user scope skill ---

func TestSkillList_GlobalFlag_OnlyUserScope(t *testing.T) {
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

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "-g", "--json")
	raw := stdout.String()
	var resp JSONResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, raw)
	}
	if strings.Contains(raw, `"alpha-skill"`) {
		t.Errorf("alpha-skill should be filtered out by -g (project scope), got: %s", raw)
	}
	if !strings.Contains(raw, `"beta-skill"`) {
		t.Errorf("beta-skill should be present (user scope), got: %s", raw)
	}
}

// --- 47.3-CLI-AC5-005: [P0] -p 过滤：只显示 project scope skill ---

func TestSkillList_ProjectFlag_OnlyProjectScope(t *testing.T) {
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

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "-p", "--json")
	raw := stdout.String()
	if !strings.Contains(raw, `"alpha-skill"`) {
		t.Errorf("alpha-skill should be present (project scope), got: %s", raw)
	}
	if strings.Contains(raw, `"beta-skill"`) {
		t.Errorf("beta-skill should be filtered out by -p (user scope), got: %s", raw)
	}
}

// --- 47.3-CLI-AC5-006: [P1] filterScopes 内部 helper 行为 ---

func TestFilterScopes_PreservesPriorityOrder(t *testing.T) {
	scopes := []config.ScopePath{
		{Path: "/p/.rnix/skills", Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
		{Path: "/p/.agents/skills", Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceAgents},
		{Path: "/h/.config/rnix/skills", Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceRnix},
		{Path: "/h/.agents/skills", Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceAgents},
	}

	gotUser := filterScopes(scopes, config.SkillScopeUser)
	if len(gotUser) != 2 {
		t.Fatalf("expected 2 user scopes, got %d", len(gotUser))
	}
	if gotUser[0].Path != "/h/.config/rnix/skills" {
		t.Errorf("user[0] priority preserved: want '/h/.config/rnix/skills', got %q", gotUser[0].Path)
	}
	if gotUser[1].Path != "/h/.agents/skills" {
		t.Errorf("user[1] priority preserved: want '/h/.agents/skills', got %q", gotUser[1].Path)
	}

	gotProj := filterScopes(scopes, config.SkillScopeProject)
	if len(gotProj) != 2 {
		t.Fatalf("expected 2 project scopes, got %d", len(gotProj))
	}
}

// --- 47.3-CLI-AC7-001: [P0] list 表格新增 SCOPE 与 NAMESPACE 列 ---

func TestSkillList_DefaultMode_RendersScopeNamespaceColumns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()

	writeFixtureSkill(t,
		filepath.Join(projectDir, ".rnix", "skills", "skill-a"),
		"skill-a", "1.0.0", "first")

	writeFixtureSkill(t,
		filepath.Join(home, ".agents", "skills", "skill-b"),
		"skill-b", "1.0.0", "second")

	t.Chdir(projectDir)

	stdout, _ := runSkillCmdForTest(t, "skill", "list")
	out := stdout.String()
	if !strings.Contains(out, "SCOPE") {
		t.Errorf("expected SCOPE column header in default-mode output, got:\n%s", out)
	}
	if !strings.Contains(out, "NAMESPACE") {
		t.Errorf("expected NAMESPACE column header in default-mode output, got:\n%s", out)
	}
	if !strings.Contains(out, "project") || !strings.Contains(out, "native") {
		t.Errorf("expected 'project' + 'native' for skill-a row, got:\n%s", out)
	}
	if !strings.Contains(out, "user") || !strings.Contains(out, "agents") {
		t.Errorf("expected 'user' + 'agents' for skill-b row, got:\n%s", out)
	}
}

// --- 47.3-CLI-AC7-002: [P2] quiet 模式不渲染新列 ---

func TestSkillList_QuietMode_NoNewColumns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	writeFixtureSkill(t,
		filepath.Join(home, ".config", "rnix", "skills", "quiet-skill"),
		"quiet-skill", "1.0.0", "quiet mode test")

	t.Chdir(t.TempDir())

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "--quiet")
	out := stdout.String()
	if strings.Contains(out, "SCOPE") || strings.Contains(out, "NAMESPACE") {
		t.Errorf("quiet mode should NOT render new columns, got:\n%s", out)
	}
	if !strings.Contains(out, "quiet-skill") {
		t.Errorf("quiet mode should still output skill name, got:\n%s", out)
	}
}

// --- 47.3-CLI-AC5-007: [P1] -g 后过滤空 → 走 AC6 空诊断分支 ---

func TestSkillList_GlobalFlag_EmptyUserScope_DiagnosticPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()
	writeFixtureSkill(t,
		filepath.Join(projectDir, ".rnix", "skills", "in-project"),
		"in-project", "1.0.0", "project only")

	t.Chdir(projectDir)

	stdout, _ := runSkillCmdForTest(t, "skill", "list", "-g")
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

// writeFixtureSkill creates a minimum-viable SKILL.md under skillDir.
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
