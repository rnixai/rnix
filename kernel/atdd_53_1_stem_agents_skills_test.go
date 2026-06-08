package kernel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/skills"
)

// ATDD acceptance test for Story 53.1 (AC3 消费方③: stem discovery)。
//
// 验证 kernel 侧 project-aware stem matcher(kernel/spawn.go:136-139 从
// ProjectConfig.SkillDirs 重建)遍历 SkillDirs 时对 .agents/skills 路径无歧视。
//
// 性质: GUARD —— 这是 AC3"单点修复 server_spawn 即三消费方全链路打通"中
// 消费方③的 kernel 侧契约前提。一旦 ipc 包的 resolveProjectContext(由
// 53.1-INT-001/002/003 验证)把 .agents/skills 纳入 SkillDirs,本路径即生效。
// 激活后在当前代码即通过(kernel 侧无需改动),非 server_spawn 修复的直接 red 证据;
// 它锁定 kernel 契约,防止误判"消费方③需单独改动"。
func TestATDD_53_1_INT_008_StemMatcherDiscoversAgentsSkills(t *testing.T) {
	// 模拟 resolveProjectContext 修复后产出的、含 .agents/skills 的 SkillDirs。
	projectDir := t.TempDir()
	agentsSkillsDir := filepath.Join(projectDir, ".agents", "skills")
	skillDir := filepath.Join(agentsSkillsDir, "code-analyst")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	content := "---\nname: code-analyst\ndescription: Analyze code structure and dependencies\n---\n\n# code-analyst\n\nATDD 53.1 stem fixture.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// 复刻 kernel/spawn.go:136-139 的 project-aware stem matcher 重建路径。
	skillDirs := []string{agentsSkillsDir}
	projLoader := skills.NewSkillLoader(skillDirs)
	projDiscovery := skills.NewSkillDiscovery(projLoader, skillDirs)
	matcher := NewStemMatcher(projDiscovery)

	count, err := matcher.AvailableCount()
	if err != nil {
		t.Fatalf("AvailableCount: %v", err)
	}
	if count < 1 {
		t.Fatalf("AvailableCount = %d, want >= 1 (stem must discover .agents/skills skill)", count)
	}

	results, err := matcher.MatchWithScores("analyze code structure")
	if err != nil {
		t.Fatalf("MatchWithScores: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Name == "code-analyst" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("stem matcher did not surface .agents/skills skill %q for matching intent; got %v", "code-analyst", results)
	}
}
