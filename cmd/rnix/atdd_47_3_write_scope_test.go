package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 47.3: install/update flag 矩阵 + resolveWriteScope
//
// 覆盖：
//   - AC2: resolveWriteScope helper 的 5 种 (global, shared, in-project) 决策
//   - AC3: skill install 注册 -g/--global 与 --shared flag + 输出 scope 元信息
//   - AC4: skill update 无新 flag + 复用 47.2 写回原 scope 行为
//
// 详见 _bmad-output/implementation-artifacts/47-3-cli-subcommand-multi-scope-flag-render-diagnostic.md
//
// RED → GREEN: 完成 cmd/rnix/skill.go resolveWriteScope + flag 注册后，
// 所有 t.Skip 移除，断言通过。
// ============================================================

// --- 47.3-CLI-AC3-001: [P0] skill install 注册 -g/--global flag ---

// TestSkillInstall_GlobalFlagRegistered 验证 `--global` (短 flag `-g`)
// 在 skillInstallCmd 上注册。AC3 / 任务 3.1。
func TestSkillInstall_GlobalFlagRegistered(t *testing.T) {
	t.Skip("RED PHASE 47.3: install 需注册 -g/--global flag")

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

// TestSkillInstall_SharedFlagRegistered 验证 `--shared` flag 在
// skillInstallCmd 上注册（无短 flag，避免与 `-s` 等冲突）。AC3。
func TestSkillInstall_SharedFlagRegistered(t *testing.T) {
	t.Skip("RED PHASE 47.3: install 需注册 --shared flag")

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

// --- 47.3-CLI-AC3-003: [P0] skill install 拒绝 -p/--project（install 无 project 过滤语义）---

// TestSkillInstall_RejectsProjectFlag 验证 `-p`/`--project` 不在 install
// 子命令注册，避免与"过滤 install 行为"误解（Cobra 默认报"unknown flag"）。
// AC3 / Dev Notes §3。
func TestSkillInstall_RejectsProjectFlag(t *testing.T) {
	t.Skip("RED PHASE 47.3: install 不应接受 -p/--project flag")

	installCmd := findSubCommand(t, "skill", "install")

	if f := installCmd.Flags().Lookup("project"); f != nil {
		t.Errorf("expected --project NOT registered on install (only on list), got %v", f)
	}
}

// --- 47.3-CLI-AC2-001: [P0] resolveWriteScope 5 种组合决策矩阵 ---

// TestResolveWriteScope_5Combinations 验证 AC2 决策表的 5 种 (global,
// shared, in-project) 组合分别返回正确的 ScopePath.{Path, Scope, Namespace}。
//
// 决策矩阵：
//   | global | shared | in-proj | 目标 Path                | Scope   | NS     |
//   |--------|--------|---------|--------------------------|---------|--------|
//   | false  | false  | yes     | <proj>/.rnix/skills/     | project | native |
//   | false  | false  | no      | <user>/skills/           | user    | native |
//   | true   | false  | -       | <user>/skills/           | user    | native |
//   | false  | true   | yes     | <proj>/.agents/skills/   | project | agents |
//   | true   | true   | -       | <home>/.agents/skills/   | user    | agents |
//
// 任务 2.5。
func TestResolveWriteScope_5Combinations(t *testing.T) {
	t.Skip("RED PHASE 47.3: resolveWriteScope helper 待实现 — 见 cmd/rnix/skill.go 任务 2")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// in-project: create cwd containing .rnix/
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir project .rnix: %v", err)
	}

	// non-project: a fresh dir without .rnix/ ancestor
	nonProject := t.TempDir()

	tests := []struct {
		name       string
		cwd        string
		global     bool
		shared     bool
		wantScope  config.SkillScope
		wantNS     config.SkillNamespace
		wantSuffix string // suffix expected on the returned ScopePath.Path
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
			wantSuffix: filepath.Join("rnix", "skills"), // ~/.config/rnix/skills
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

			// After GREEN: call resolveWriteScope(tc.cwd, scopes, tc.global, tc.shared)
			// got, err := resolveWriteScope(tc.cwd, scopes, tc.global, tc.shared)
			// if err != nil {
			//     t.Fatalf("resolveWriteScope: %v", err)
			// }
			//
			// if got.Scope != tc.wantScope {
			//     t.Errorf("Scope: got %v, want %v", got.Scope, tc.wantScope)
			// }
			// if got.Namespace != tc.wantNS {
			//     t.Errorf("Namespace: got %v, want %v", got.Namespace, tc.wantNS)
			// }
			// if !strings.HasSuffix(got.Path, tc.wantSuffix) {
			//     t.Errorf("Path: got %q, want suffix %q", got.Path, tc.wantSuffix)
			// }

			_ = scopes
			t.Fatalf("RED: resolveWriteScope helper missing; expected scope=%v ns=%v suffix=%q",
				tc.wantScope, tc.wantNS, tc.wantSuffix)
		})
	}
}

// --- 47.3-CLI-AC2-002: [P1] resolveWriteScope 创建目录（os.MkdirAll）---

// TestResolveWriteScope_CreatesTargetDir 验证 AC2 dev note：写入前
// `os.MkdirAll(0o755)` 创建 scope 根目录，保证首次 `--shared` 安装到
// 不存在的 ~/.agents/skills/ 时不失败。
//
// 任务 2.4。
func TestResolveWriteScope_CreatesTargetDir(t *testing.T) {
	t.Skip("RED PHASE 47.3: resolveWriteScope 应在返回前 MkdirAll target")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cwd := t.TempDir()

	// Before call: ~/.agents/skills/ does NOT exist.
	target := filepath.Join(home, ".agents", "skills")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target not to exist pre-call, got err=%v", err)
	}

	scopes := config.ResolveSkillScopes(cwd)
	_ = scopes

	// After GREEN: resolveWriteScope(cwd, scopes, true /*global*/, true /*shared*/)
	// → returns ScopePath{Path: ~/.agents/skills} and MkdirAll has been called.

	// Then: target directory exists.
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected target %q to be MkdirAll'd by resolveWriteScope, stat err: %v", target, err)
	}
}

// --- 47.3-CLI-AC3-004: [P0] -g + --shared 组合落入 user/agents ---

// TestSkillInstall_GlobalShared_TargetsUserAgents 端到端验证 AC3：
// `rnix skill install foo -g --shared` 在任意 cwd 都安装到 ~/.agents/skills/。
//
// 任务 3.4。
func TestSkillInstall_GlobalShared_TargetsUserAgents(t *testing.T) {
	t.Skip("RED PHASE 47.3: -g + --shared 组合应写到 ~/.agents/skills/")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Cwd: an in-project dir to exercise the "global overrides in-project" rule.
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	t.Chdir(projectDir)

	mockPkgServeFixture(t, "fav-skill", "1.0.0")

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "install", "fav-skill", "-g", "--shared", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	expected := filepath.Join(home, ".agents", "skills", "fav-skill")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected fav-skill at %q (user/agents per -g --shared), stat err: %v", expected, err)
	}
}

// --- 47.3-CLI-AC3-005: [P0] --shared 单独使用：in-project → project/agents ---

// TestSkillInstall_SharedOnly_InProject_TargetsProjectAgents 验证 AC3：
// `--shared` 不带 `-g` + 在 .rnix/ 项目内 → 目标为 <projectDir>/.agents/skills/。
func TestSkillInstall_SharedOnly_InProject_TargetsProjectAgents(t *testing.T) {
	t.Skip("RED PHASE 47.3: --shared 在项目内应写到 <projectDir>/.agents/skills/")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	t.Chdir(projectDir)

	mockPkgServeFixture(t, "ext-skill", "1.0.0")

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "install", "ext-skill", "--shared"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	expected := filepath.Join(projectDir, ".agents", "skills", "ext-skill")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected ext-skill at %q (project/agents), stat err: %v", expected, err)
	}
}

// --- 47.3-CLI-AC3-006: [P1] install 成功输出含 scope/namespace/path 元信息 ---

// TestSkillInstall_OutputIncludesScopeMetadata 验证 AC3 非 JSON 模式：
// install 成功后追加 `(<scope>/<ns>: <path>)`。
// 任务 3.3。
func TestSkillInstall_OutputIncludesScopeMetadata(t *testing.T) {
	t.Skip("RED PHASE 47.3: install 成功输出需含 (<scope>/<ns>: <path>) 元信息")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	mockPkgServeFixture(t, "meta-skill", "1.0.0")

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "install", "meta-skill"}) // non-JSON
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

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

// TestSkillUpdate_NoNewFlags 验证 AC4：update 子命令不接受 -g / --global /
// --shared / -p / --project（Update 走 47.2 "写回原 scope" 规则）。任务 4.1。
func TestSkillUpdate_NoNewFlags(t *testing.T) {
	t.Skip("RED PHASE 47.3: update 子命令不应注册新 flag")

	updateCmd := findSubCommand(t, "skill", "update")

	for _, name := range []string{"global", "shared", "project"} {
		if f := updateCmd.Flags().Lookup(name); f != nil {
			t.Errorf("expected --%s NOT registered on skill update (update writes back to origin scope), got %v", name, f)
		}
	}
}

// --- 47.3-CLI-AC4-002: [P1] update 写回原 scope（CLI wire 验证）---

// TestSkillUpdate_WritesBackToOriginScope 验证 AC4 + 47.2 Hippocratic
// 设计：user/native 装了 foo v1.0.0，update 后 foo 仍在 user/native（不
// 静默迁移到 project/native）。任务 4.4。
func TestSkillUpdate_WritesBackToOriginScope(t *testing.T) {
	t.Skip("RED PHASE 47.3: update 应写回 origin scope，CLI wire 验证")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Setup: foo v1.0.0 already installed under user/native.
	userNative := filepath.Join(home, ".config", "rnix", "skills", "foo")
	if err := os.MkdirAll(userNative, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// (Real fixture would write SKILL.md + .registry.yaml entry. RED placeholder.)

	// Cwd: in-project (to verify it does NOT migrate to project scope).
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	t.Chdir(projectDir)

	mockPkgServeFixture(t, "foo", "2.0.0")

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "update", "foo", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Then: foo still under user/native (not migrated).
	if _, err := os.Stat(userNative); err != nil {
		t.Errorf("expected foo to remain at user/native after update; stat: %v", err)
	}
	// And: NOT created under project scope.
	if _, err := os.Stat(filepath.Join(projectDir, ".rnix", "skills", "foo")); !os.IsNotExist(err) {
		t.Errorf("expected foo NOT migrated to project/native; stat: %v", err)
	}
}

// --- 47.3-CLI-AC4-003: [P2] update 成功输出含 scope 元信息 ---

// TestSkillUpdate_OutputIncludesScopeMetadata 验证 AC4 非 JSON 模式
// update 输出格式：`Updated foo v1.0.0 → v2.0.0 (user/native: <path>)`。
func TestSkillUpdate_OutputIncludesScopeMetadata(t *testing.T) {
	t.Skip("RED PHASE 47.3: update 输出需含 (<scope>/<ns>: <path>) 元信息")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	userNative := filepath.Join(home, ".config", "rnix", "skills", "bar")
	if err := os.MkdirAll(userNative, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	t.Chdir(t.TempDir())

	mockPkgServeFixture(t, "bar", "2.0.0")

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "update", "bar"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "(user/native:") {
		t.Errorf("expected update output to include '(user/native:' marker, got: %s", out)
	}
}

// --- 47.3-CLI-AC3-007: [P2] install JSON 模式输出兼容 ---

// TestSkillInstall_JSONOutput_BackwardCompat 验证 AC3 dev-decision：
// install JSON 模式 InstallResult 结构保持 47.2 既有形状（Name/Version/
// Fresh），新增的 scope/path 信息**不**修改 InstallResult，由 CLI 文本
// 自行拼接。
func TestSkillInstall_JSONOutput_BackwardCompat(t *testing.T) {
	t.Skip("RED PHASE 47.3: install JSON InstallResult 不应破坏既有形状")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())

	mockPkgServeFixture(t, "compat-skill", "1.0.0")

	var stdout bytes.Buffer
	root := newTestRootCmd(t, &stdout)
	root.SetArgs([]string{"skill", "install", "compat-skill", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v\nraw=%s", err, stdout.String())
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got: %s", stdout.String())
	}
	raw := stdout.String()
	if !strings.Contains(raw, `"installed"`) {
		t.Errorf("expected JSON key 'installed' (47.2 shape preserved), got: %s", raw)
	}
}

// ============================================================
// Local helpers (kept here to avoid duplication across files).
// ============================================================

// findSubCommand walks rootCmd.Commands() → subcommand chain and returns the
// terminal subcommand by name. Fails the test if any link is missing.
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
