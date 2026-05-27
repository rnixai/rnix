package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/spf13/cobra"
)

// ============================================================
// ATDD — Story 47.3: install/update flag 矩阵 + resolveWriteScope
//
// 覆盖：
//   - AC2: resolveWriteScope helper 的 5 种 (global, shared, in-project) 决策
//   - AC3: skill install 注册 -g/--global 与 --shared flag + 输出 scope 元信息
//   - AC4: skill update 无新 flag + 复用 47.2 写回原 scope 行为
// ============================================================

// --- 47.3-CLI-AC3-001: [P0] skill install 注册 -g/--global flag ---

func TestSkillInstall_GlobalFlagRegistered(t *testing.T) {
	installCmd := findSubCommand(t, "skill", "install")
	global := installCmd.Flags().Lookup("global")
	if global == nil {
		t.Fatal("expected --global flag registered on skillInstallCmd, got nil")
	}
	if global.Shorthand != "g" {
		t.Errorf("expected --global short flag 'g', got %q", global.Shorthand)
	}
	if global.DefValue != "false" {
		t.Errorf("expected --global default false, got %q", global.DefValue)
	}
}

// --- 47.3-CLI-AC3-002: [P0] skill install 注册 --shared flag ---

func TestSkillInstall_SharedFlagRegistered(t *testing.T) {
	installCmd := findSubCommand(t, "skill", "install")
	shared := installCmd.Flags().Lookup("shared")
	if shared == nil {
		t.Fatal("expected --shared flag registered on skillInstallCmd, got nil")
	}
	if shared.Shorthand != "" {
		t.Errorf("expected --shared without short flag, got -%s", shared.Shorthand)
	}
	if shared.DefValue != "false" {
		t.Errorf("expected --shared default false, got %q", shared.DefValue)
	}
}

// --- 47.3-CLI-AC3-003: [P0] skill install 拒绝 -p/--project ---

func TestSkillInstall_RejectsProjectFlag(t *testing.T) {
	installCmd := findSubCommand(t, "skill", "install")
	if f := installCmd.Flags().Lookup("project"); f != nil {
		t.Errorf("expected --project NOT registered on install (only on list), got %v", f)
	}
}

// --- 47.3-CLI-AC2-001: [P0] resolveWriteScope 5 种组合决策矩阵 ---

func TestResolveWriteScope_5Combinations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir project .rnix: %v", err)
	}

	nonProject := t.TempDir()

	tests := []struct {
		name       string
		cwd        string
		global     bool
		shared     bool
		wantScope  config.SkillScope
		wantNS     config.SkillNamespace
		wantSuffix string
	}{
		{
			name:       "in-project, no flags → project/native",
			cwd:        projectDir,
			global:     false,
			shared:     false,
			wantScope:  config.SkillScopeProject,
			wantNS:     config.SkillNamespaceRnix,
			wantSuffix: filepath.Join(".rnix", "skills"),
		},
		{
			name:       "non-project, no flags → user/native",
			cwd:        nonProject,
			global:     false,
			shared:     false,
			wantScope:  config.SkillScopeUser,
			wantNS:     config.SkillNamespaceRnix,
			wantSuffix: filepath.Join("rnix", "skills"),
		},
		{
			name:       "in-project, --global → user/native",
			cwd:        projectDir,
			global:     true,
			shared:     false,
			wantScope:  config.SkillScopeUser,
			wantNS:     config.SkillNamespaceRnix,
			wantSuffix: filepath.Join("rnix", "skills"),
		},
		{
			name:       "in-project, --shared → project/agents",
			cwd:        projectDir,
			global:     false,
			shared:     true,
			wantScope:  config.SkillScopeProject,
			wantNS:     config.SkillNamespaceAgents,
			wantSuffix: filepath.Join(".agents", "skills"),
		},
		{
			name:       "in-project, --global --shared → user/agents",
			cwd:        projectDir,
			global:     true,
			shared:     true,
			wantScope:  config.SkillScopeUser,
			wantNS:     config.SkillNamespaceAgents,
			wantSuffix: filepath.Join(".agents", "skills"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scopes := config.ResolveSkillScopes(tc.cwd)
			got, err := resolveWriteScope(tc.cwd, scopes, tc.global, tc.shared)
			if err != nil {
				t.Fatalf("resolveWriteScope: %v", err)
			}
			if got.Scope != tc.wantScope {
				t.Errorf("Scope: got %v, want %v", got.Scope, tc.wantScope)
			}
			if got.Namespace != tc.wantNS {
				t.Errorf("Namespace: got %v, want %v", got.Namespace, tc.wantNS)
			}
			if !strings.HasSuffix(got.Path, tc.wantSuffix) {
				t.Errorf("Path: got %q, want suffix %q", got.Path, tc.wantSuffix)
			}
		})
	}
}

// --- 47.3-CLI-AC2-002: [P1] resolveWriteScope 创建目录（os.MkdirAll）---

func TestResolveWriteScope_CreatesTargetDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cwd := t.TempDir()

	target := filepath.Join(home, ".agents", "skills")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target not to exist pre-call, got err=%v", err)
	}

	scopes := config.ResolveSkillScopes(cwd)
	if _, err := resolveWriteScope(cwd, scopes, true, true); err != nil {
		t.Fatalf("resolveWriteScope: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected target %q to be MkdirAll'd by resolveWriteScope, stat err: %v", target, err)
	}
}

// --- 47.3-CLI-AC3-004: [P0] -g + --shared 组合落入 user/agents ---

func TestSkillInstall_GlobalShared_TargetsUserAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	t.Chdir(projectDir)

	mockPkgServeFixture(t, "fav-skill", "1.0.0")

	stdout, _ := runSkillCmdForTest(t, "skill", "install", "fav-skill", "-g", "--shared", "--json")
	if !strings.Contains(stdout.String(), `"fav-skill"`) {
		t.Fatalf("expected install to succeed, got: %s", stdout.String())
	}

	expected := filepath.Join(home, ".agents", "skills", "fav-skill")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected fav-skill at %q (user/agents per -g --shared), stat err: %v", expected, err)
	}
}

// --- 47.3-CLI-AC3-005: [P0] --shared 单独使用：in-project → project/agents ---

func TestSkillInstall_SharedOnly_InProject_TargetsProjectAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	t.Chdir(projectDir)

	mockPkgServeFixture(t, "ext-skill", "1.0.0")

	_, _ = runSkillCmdForTest(t, "skill", "install", "ext-skill", "--shared")

	expected := filepath.Join(projectDir, ".agents", "skills", "ext-skill")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected ext-skill at %q (project/agents), stat err: %v", expected, err)
	}
}

// --- 47.3-CLI-AC3-006: [P1] install 成功输出含 scope/namespace/path 元信息 ---

func TestSkillInstall_OutputIncludesScopeMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	mockPkgServeFixture(t, "meta-skill", "1.0.0")

	stdout, _ := runSkillCmdForTest(t, "skill", "install", "meta-skill")
	out := stdout.String()
	if !strings.Contains(out, "Installed meta-skill v1.0.0") {
		t.Errorf("expected install confirmation line, got: %s", out)
	}
	if !strings.Contains(out, "(user/native:") {
		t.Errorf("expected scope/namespace marker '(user/native:' in output, got: %s", out)
	}
	if !strings.Contains(out, filepath.Join(home, ".config", "rnix", "skills")) {
		t.Errorf("expected install path under user/native in output, got: %s", out)
	}
}

// --- 47.3-CLI-AC4-001: [P1] skill update 不引入新 flag ---

func TestSkillUpdate_NoNewFlags(t *testing.T) {
	updateCmd := findSubCommand(t, "skill", "update")
	for _, name := range []string{"global", "shared", "project"} {
		if f := updateCmd.Flags().Lookup(name); f != nil {
			t.Errorf("expected --%s NOT registered on skill update (update writes back to origin scope), got %v", name, f)
		}
	}
}

// --- 47.3-CLI-AC4-002: [P1] update 写回原 scope（CLI wire 验证）---

func TestSkillUpdate_WritesBackToOriginScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	mockPkgServeFixture(t, "foo", "1.0.0")

	// First install: should land in user/native (no .rnix project).
	t.Chdir(t.TempDir())
	_, _ = runSkillCmdForTest(t, "skill", "install", "foo", "--json")
	userNative := filepath.Join(home, ".config", "rnix", "skills", "foo")
	if _, err := os.Stat(userNative); err != nil {
		t.Fatalf("install: foo not at user/native, stat err: %v", err)
	}

	// Now switch to an in-project cwd and trigger update. Expected: foo stays
	// in user/native (no silent migration).
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	t.Chdir(projectDir)

	stdout, _ := runSkillCmdForTest(t, "skill", "update", "foo", "--json")
	raw := stdout.String()
	if !strings.Contains(raw, `"foo"`) {
		t.Logf("update raw: %s", raw)
	}

	if _, err := os.Stat(userNative); err != nil {
		t.Errorf("expected foo to remain at user/native after update; stat: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".rnix", "skills", "foo")); !os.IsNotExist(err) {
		t.Errorf("expected foo NOT migrated to project/native; stat: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, raw)
	}
	if !resp.OK {
		t.Errorf("expected ok=true for update, raw=%s", raw)
	}
}

// --- 47.3-CLI-AC4-003: [P2] update 成功输出含 scope 元信息 ---

func TestSkillUpdate_OutputIncludesScopeMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	mockPkgServeFixture(t, "bar", "1.0.0")

	_, _ = runSkillCmdForTest(t, "skill", "install", "bar")
	stdout, _ := runSkillCmdForTest(t, "skill", "update", "bar")
	out := stdout.String()
	if !strings.Contains(out, "(user/native:") {
		t.Errorf("expected update output to include '(user/native:' marker, got: %s", out)
	}
}

// --- 47.3-CLI-AC3-007: [P2] install JSON 模式输出兼容 ---

func TestSkillInstall_JSONOutput_BackwardCompat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	mockPkgServeFixture(t, "compat-skill", "1.0.0")

	stdout, _ := runSkillCmdForTest(t, "skill", "install", "compat-skill", "--json")
	raw := stdout.String()
	var resp JSONResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, raw)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got: %s", raw)
	}
	if !strings.Contains(raw, `"installed"`) {
		t.Errorf("expected JSON key 'installed' (47.2 shape preserved), got: %s", raw)
	}
}

// ============================================================
// Local helpers
// ============================================================

// findSubCommand walks rootCmd → parent → sub chain and returns the leaf.
func findSubCommand(t *testing.T, parent string, sub string) *cobra.Command {
	t.Helper()
	var parentCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == parent {
			parentCmd = c
			break
		}
	}
	if parentCmd == nil {
		t.Fatalf("parent command %q not found under rootCmd", parent)
	}
	for _, c := range parentCmd.Commands() {
		if c.Name() == sub {
			return c
		}
	}
	t.Fatalf("subcommand %q not found under %q", sub, parent)
	return nil
}
