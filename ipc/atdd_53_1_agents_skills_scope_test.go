package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/skills"
)

// ATDD acceptance tests for Story 53.1: spawn 注入纳入 .agents/skills
// (epic-47 运行时闭环补全)。
//
// 缺陷: ipc/server_spawn.go:resolveProjectContext 的 skillDirs 硬编码
//   []string{<projectDir>/.rnix/skills, gc.SkillsDir}
// 只含 native namespace,完全不含 agents namespace (.agents/skills)。
// 修复: 改用 config.ResolveSkillScopes(projectDir)(CLI 同一解析器)+ 去重追加
// gc.SkillsDir 作最低优先级 fallback。
//
// 修复(server_spawn.go:resolveProjectContext 改用 config.ResolveSkillScopes)已
// 落地,以下测试全部激活。
//
// 测试性质分类(诚实标注):
//   - RED  : 激活后在当前(未修复)代码下必失败,实现后通过 —— 缺陷的直接证据。
//   - GUARD: 激活后在当前代码即通过 —— 锁定 AC2/AC4 实现约束(去重 / fallback /
//            不回归 / ancestor 默认关),防止修复实现引入回归。
//
// 测试隔离铁律: 本机存在 rnix init 解压的真实 ~/.config/rnix/skills 与
// ~/.agents/skills。任何对 SkillDirs 精确断言或经 user scope 的测试,必须
// t.Setenv("HOME", ...) + t.Setenv("XDG_CONFIG_HOME", ...) 隔离,否则 flaky。

// writeSkillFixture531 在 <baseDir>/<name>/SKILL.md 写一个最小合法 skill
// (agentskills.io: frontmatter 必须含 name + description)。
func writeSkillFixture531(t *testing.T, baseDir, name, description string) {
	t.Helper()
	skillDir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir %q: %v", skillDir, err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n\nATDD 53.1 fixture skill body.\n", name, description, name)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md for %q: %v", name, err)
	}
}

// newServerWithGlobal531 构造带全局 config 的测试 server。
func newServerWithGlobal531(t *testing.T, globalDir, agentsDir, skillsDir string) *Server {
	t.Helper()
	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       globalDir,
		AgentsDir: agentsDir,
		SkillsDir: skillsDir,
	})
	return srv
}

// mkProjectRnix531 创建 projectDir/.rnix —— resolveProjectContext 校验前置:
// projectDir 必须含 .rnix/ 子目录。
func mkProjectRnix531(t *testing.T, projectDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
}

// --- 53.1-INT-002 [RED] AC1/AC3: project .agents/skills/<name> 经 spawn loader 可加载 ---

func TestATDD_53_1_INT_002_ProjectAgentsSkillLoadable(t *testing.T) {
	// 修复前代码: SkillDirs 仅 [.rnix/skills, gc.SkillsDir],不含 .agents/skills →
	// LoadFull 找不到 skill → 失败(red)。修复后含 project .agents/skills → 通过。
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	srv := newServerWithGlobal531(t, t.TempDir(), t.TempDir(), t.TempDir())

	projectDir := t.TempDir()
	mkProjectRnix531(t, projectDir)
	writeSkillFixture531(t, filepath.Join(projectDir, ".agents", "skills"),
		"code-analyst", "Analyze code structure and dependencies")

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}
	if projCfg == nil {
		t.Fatal("expected non-nil ProjectConfig")
	}

	loader := skills.NewSkillLoader(projCfg.SkillDirs)
	info, err := loader.LoadFull("code-analyst")
	if err != nil {
		t.Fatalf("LoadFull(project .agents/skills skill) failed; SkillDirs=%v: %v", projCfg.SkillDirs, err)
	}
	if info.Manifest.Name != "code-analyst" {
		t.Errorf("loaded skill Name = %q, want %q", info.Manifest.Name, "code-analyst")
	}
}

// --- 53.1-INT-003 [RED] AC3: user ~/.agents/skills/<name> 经 spawn loader 可加载 ---

func TestATDD_53_1_INT_003_UserAgentsSkillLoadable(t *testing.T) {
	// 修复前代码不含 user-level ~/.agents/skills → LoadFull 失败(red)。
	// 修复后 ResolveSkillScopes 含 user-agents(经 $HOME)→ 通过。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	srv := newServerWithGlobal531(t, t.TempDir(), t.TempDir(), t.TempDir())

	projectDir := t.TempDir()
	mkProjectRnix531(t, projectDir)
	// user scope: ~/.agents/skills/<name>
	writeSkillFixture531(t, filepath.Join(home, ".agents", "skills"),
		"doc-writer", "Write and format project documentation")

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}

	loader := skills.NewSkillLoader(projCfg.SkillDirs)
	info, err := loader.LoadFull("doc-writer")
	if err != nil {
		t.Fatalf("LoadFull(user ~/.agents/skills skill) failed; SkillDirs=%v: %v", projCfg.SkillDirs, err)
	}
	if info.Manifest.Name != "doc-writer" {
		t.Errorf("loaded skill Name = %q, want %q", info.Manifest.Name, "doc-writer")
	}
}

// --- 53.1-INT-004 [GUARD] AC2: gc.SkillsDir == GlobalDir()/skills 去重无重复 ---

func TestATDD_53_1_INT_004_GlobalSkillsDirDeduped(t *testing.T) {
	// GUARD: 生产环境 gc.SkillsDir == GlobalDir()/skills == ResolveSkillScopes 的
	// user-native 路径。修复实现必须去重(slices.Contains)避免重复条目。
	// 错误实现(无脑 append gc.SkillsDir)→ 重复 → 本测试失败。
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	globalDir, err := config.GlobalDir()
	if err != nil {
		t.Fatalf("GlobalDir: %v", err)
	}
	userNativeSkills := filepath.Join(globalDir, "skills")
	if err := os.MkdirAll(userNativeSkills, 0o755); err != nil {
		t.Fatalf("mkdir user-native skills: %v", err)
	}

	// gc.SkillsDir 故意设为与 user-native 完全相同的路径(生产语义)。
	srv := newServerWithGlobal531(t, globalDir, t.TempDir(), userNativeSkills)

	projectDir := t.TempDir()
	mkProjectRnix531(t, projectDir)

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}

	// user-native 路径必须出现且仅出现一次(去重)。
	occurrences := 0
	for _, d := range projCfg.SkillDirs {
		if d == userNativeSkills {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Errorf("user-native/gc.SkillsDir %q appears %d times in SkillDirs %v, want exactly 1 (dedup)", userNativeSkills, occurrences, projCfg.SkillDirs)
	}

	// SkillDirs 整体无任何重复条目。
	seen := make(map[string]bool, len(projCfg.SkillDirs))
	for _, d := range projCfg.SkillDirs {
		if seen[d] {
			t.Errorf("duplicate entry %q in SkillDirs %v", d, projCfg.SkillDirs)
		}
		seen[d] = true
	}
}

// --- 53.1-INT-005 [GUARD] AC2: gc.SkillsDir 独立目录时作最低优先级 fallback 仍可搜 ---

func TestATDD_53_1_INT_005_GlobalSkillsDirFallbackSearchable(t *testing.T) {
	// GUARD: 测试/自定义全局目录场景 gc.SkillsDir != user-native。修复实现必须
	// 去重追加 gc.SkillsDir,保留"全局 skill 始终可搜"语义。错误实现(只用
	// ResolveSkillScopes 不追加)→ gc.SkillsDir 里的 skill 丢失 → 本测试失败。
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// gc.SkillsDir 为独立 OS 临时目录(不在隔离 HOME/XDG 下,故 != user-native)。
	globalSkillsDir := t.TempDir()
	writeSkillFixture531(t, globalSkillsDir, "global-skill", "A globally installed skill")
	srv := newServerWithGlobal531(t, t.TempDir(), t.TempDir(), globalSkillsDir)

	projectDir := t.TempDir()
	mkProjectRnix531(t, projectDir)
	// 同时给 project .rnix/skills 一个 skill,确认两者共存。
	writeSkillFixture531(t, filepath.Join(projectDir, ".rnix", "skills"), "proj-skill", "A project-native skill")

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}

	loader := skills.NewSkillLoader(projCfg.SkillDirs)
	if _, err := loader.LoadFull("global-skill"); err != nil {
		t.Errorf("global skill not searchable via gc.SkillsDir fallback; SkillDirs=%v: %v", projCfg.SkillDirs, err)
	}
	if _, err := loader.LoadFull("proj-skill"); err != nil {
		t.Errorf("project-native skill not loadable; SkillDirs=%v: %v", projCfg.SkillDirs, err)
	}
}

// --- 53.1-INT-006 [GUARD] AC4: 仅 .rnix/skills 时既有行为不回归 + agentDirs 不动 ---

func TestATDD_53_1_INT_006_NoRegressionRnixOnly(t *testing.T) {
	// GUARD: 项目仅有 .rnix/skills(无 .agents/skills)时,.rnix/skills 仍最高优先级,
	// agentDirs 保持 [.rnix/agents, global] 不变(范围护栏: 不得给 agentDirs 加 .agents)。
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	globalAgentsDir := t.TempDir()
	globalSkillsDir := t.TempDir()
	srv := newServerWithGlobal531(t, t.TempDir(), globalAgentsDir, globalSkillsDir)

	projectDir := t.TempDir()
	rnixSkills := filepath.Join(projectDir, ".rnix", "skills")
	if err := os.MkdirAll(rnixSkills, 0o755); err != nil {
		t.Fatalf("mkdir .rnix/skills: %v", err)
	}

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}

	if len(projCfg.SkillDirs) == 0 {
		t.Fatalf("SkillDirs empty, want at least project .rnix/skills + global fallback")
	}
	if projCfg.SkillDirs[0] != rnixSkills {
		t.Errorf("SkillDirs[0] = %q, want %q (project .rnix/skills highest priority); full=%v", projCfg.SkillDirs[0], rnixSkills, projCfg.SkillDirs)
	}
	// 全局仍可搜(去重追加的 gc.SkillsDir)。
	if !slices.Contains(projCfg.SkillDirs, globalSkillsDir) {
		t.Errorf("global skills dir %q missing from SkillDirs %v", globalSkillsDir, projCfg.SkillDirs)
	}
	// agentDirs 范围护栏: 仍为 2 项,不含任何 .agents 路径。
	if len(projCfg.AgentDirs) != 2 {
		t.Fatalf("AgentDirs length = %d, want 2 (unchanged by 53.1)", len(projCfg.AgentDirs))
	}
	expectedAgentDir := filepath.Join(projectDir, ".rnix", "agents")
	if projCfg.AgentDirs[0] != expectedAgentDir || projCfg.AgentDirs[1] != globalAgentsDir {
		t.Errorf("AgentDirs = %v, want [%q, %q] (no .agents creep)", projCfg.AgentDirs, expectedAgentDir, globalAgentsDir)
	}
}

// --- 53.1-INT-007 [GUARD] AC4: ancestor traversal 默认关,父目录 .agents/skills 不纳入 ---

func TestATDD_53_1_INT_007_AncestorTraversalOffByDefault(t *testing.T) {
	// GUARD: resolveProjectContext 必须用默认 opts(不传 WithAncestorTraversal),
	// projectDir 已是解析好的项目根。父目录的 .agents/skills 不得被纳入。
	// 错误实现(误开 ancestor)→ 父目录 skill 泄漏进 SkillDirs → 本测试失败。
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	srv := newServerWithGlobal531(t, t.TempDir(), t.TempDir(), t.TempDir())

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	projectDir := filepath.Join(workspace, "project")
	// 父层(workspace)的 .agents/skills —— 不应被纳入。
	parentAgentsSkills := filepath.Join(workspace, ".agents", "skills")
	writeSkillFixture531(t, parentAgentsSkills, "ancestor-skill", "Should not be visible without ancestor traversal")
	// projectDir 自身含 .rnix(校验前置)。
	mkProjectRnix531(t, projectDir)

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}

	if slices.Contains(projCfg.SkillDirs, parentAgentsSkills) {
		t.Errorf("ancestor .agents/skills %q leaked into SkillDirs %v (ancestor traversal must stay OFF by default)", parentAgentsSkills, projCfg.SkillDirs)
	}
}
