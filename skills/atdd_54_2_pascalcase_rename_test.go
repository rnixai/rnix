package skills

import "testing"

// ATDD Story 54.2 — knownToolNames 镜像同步（AC3）+ validateFrontmatter 接受新名（AC4）。
//
// skills 包不能 import kernel（会成循环），故 knownToolNames 是 VFS 驱动 + kernel meta 工具
// 名的【手维护镜像】。本 story 把其中 10 个 snake_case 旧名替换为 PascalCase 新名。
// 红灯机制：RED 断言新名（改名前 isKnownToolName 返回 false → 运行时失败），t.Skip 标记；
// green-guard 立即绿，守护「已有 PascalCase 名」与「设备路径兼容期」行为不被破坏。

// story542PascalNames 是 AC1+AC2 改名后 knownToolNames 应接受的 10 个新 PascalCase 名。
var story542PascalNames = []string{
	"IntentDecompose", "IntentStatus", "IntentConfirm", "IntentExecute",
	"MemoryCommit", "MemoryRecall", "MemoryProfile",
	"SkillManage",
	"Complete", "Replan",
}

// story542SnakeNames 是改名后必须从 knownToolNames 移除的 10 个 snake_case 旧名。
var story542SnakeNames = []string{
	"intent_decompose", "intent_status", "intent_confirm", "intent_execute",
	"memory_commit", "memory_recall", "memory_profile",
	"skill_manage",
	"complete", "replan",
}

// TestATDD_54_2_400 断言 knownToolNames（skills/manager.go:148-167）替换为 PascalCase 新名，
// 且 snake_case 旧名全部移除（AC3）。
func TestATDD_54_2_400_KnownToolNames_AcceptsPascalCaseRejectsSnake(t *testing.T) {
	for _, name := range story542PascalNames {
		if !isKnownToolName(name) {
			t.Errorf("isKnownToolName(%q) = false, want true（改名后应被接受）", name)
		}
	}
	for _, name := range story542SnakeNames {
		if isKnownToolName(name) {
			t.Errorf("isKnownToolName(%q) = true，snake_case 旧名应已移除", name)
		}
	}
}

// TestATDD_54_2_410 断言幻影条目 skill_registry/skill_score 已删除（AC3 / 决策点 D1）。
// 二者非 LLM 工具（skill_registry=init 服务类型；skill_score=immune.go JSON 字段 tag），
// 无任何 ToolDef.Name 与之对应。D1 推荐删除而非创建 SkillRegistry/SkillScore 伪条目。
func TestATDD_54_2_410_KnownToolNames_PhantomEntriesRemoved(t *testing.T) {
	for _, phantom := range []string{"skill_registry", "skill_score"} {
		if isKnownToolName(phantom) {
			t.Errorf("isKnownToolName(%q) = true，应删除（非 LLM 工具，无对应 ToolDef.Name）", phantom)
		}
	}
}

// TestATDD_54_2_420 断言改名后 validateFrontmatter 接受新 PascalCase 工具名作 allowed-tools（AC4）。
func TestATDD_54_2_420_ValidateFrontmatter_AcceptsPascalCase(t *testing.T) {
	for _, name := range story542PascalNames {
		if err := validateFrontmatter("s", "d", name); err != nil {
			t.Errorf("validateFrontmatter(allowed-tools=%q) = %v, want nil", name, err)
		}
	}
}

// --- green-guard（不 skip、立即绿）---

// TestATDD_54_2_900 守护已是 PascalCase 的工具名（54.1 前已对齐 Claude 生态）本 story 不得删除。
func TestATDD_54_2_900_GreenGuard_ExistingPascalNamesIntact(t *testing.T) {
	for _, name := range []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash", "Agent", "Skill", "ToolSearch", "EnterPlanMode"} {
		if !isKnownToolName(name) {
			t.Errorf("isKnownToolName(%q) = false，已有 PascalCase 名不应被本 story 移除", name)
		}
	}
}

// TestATDD_54_2_901 守护 validateFrontmatter 校验逻辑不变：设备路径形态在兼容期仍被接受
// （54.1 行为），未知名仍被拒。本 story 只改 knownToolNames 表内容，不动校验逻辑。
func TestATDD_54_2_901_GreenGuard_ValidateFrontmatter_DevicePathAndReject(t *testing.T) {
	if err := validateFrontmatter("s", "d", "/dev/fs"); err != nil {
		t.Errorf("validateFrontmatter(\"/dev/fs\") = %v, want nil（设备路径兼容期保持）", err)
	}
	if err := validateFrontmatter("s", "d", "/mnt/mcp/1-srv/tools/x"); err != nil {
		t.Errorf("validateFrontmatter(MCP path) = %v, want nil", err)
	}
	if err := validateFrontmatter("s", "d", "Bogus"); err == nil {
		t.Error("validateFrontmatter(\"Bogus\") = nil, want error（未知名仍应拒）")
	}
}
