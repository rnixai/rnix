package agents

import (
	"slices"
	"testing"

	"github.com/rnixai/rnix/skills"
)

// ATDD Story 58.1 — agents 单元层：AgentInfo.AllowedTools() 并入 Manifest.Tools。
//
// 这是全链路唯一的 agent 工具聚合函数（agents/types.go:57-71），被 kernel/spawn.go:286
// 唯一消费。本文件断言纯函数语义：agent.tools 与 skill.allowed-tools 取并集、去重、sort、
// 缺省/空数组不追加。spawn 终值 + 工具级 enforcement 见
// kernel/atdd_58_1_agent_tools_declaration_test.go。
//
// ── RED 形态：骨架 + t.Skip ──────────────────────────────────────────────
// AgentManifest.Tools 骨架字段已加（types.go），但 AllowedTools() 尚未并入它 →
// 含 Tools 的断言用例标 t.Skip（dev 移除 + 落地并入逻辑后转绿）。
// 缺省/空数组 = 纯 skill 并集的用例是 GREEN 护栏（加空字段不改现有路径，dev 前已绿，
// 实时拦截「缺省=全集」回归红线），不 skip。
//
// 验证陷阱（MEMORY「dev-story 改 agent 文件验证陷阱」）：本包读 manifest，CI cache
// 不感知运行时外部文件——dev 验证须 `go test -count=1 -race ./agents/...`。

// agentInfoWithTools 构造一个 AgentInfo：顶层 Manifest.Tools = agentTools，并按
// skillToolsCSVs（每项一个 skill 的 allowed-tools raw）挂 0..N 个 skill。
func agentInfoWithTools(agentTools []string, skillToolsCSVs ...string) *AgentInfo {
	info := &AgentInfo{
		Manifest: AgentManifest{Name: "ut-agent", Tools: agentTools},
	}
	for i, csv := range skillToolsCSVs {
		info.Skills = append(info.Skills, &skills.SkillInfo{
			Manifest: skills.SkillManifest{
				Name:            "ut-skill",
				AllowedToolsRaw: csv,
			},
		})
		_ = i
	}
	return info
}

// 58.1-UNIT-001 [RED] 基础并集：agent.tools=[Bash] + skill allowed-tools=[Read] →
// AllowedTools() = [Bash, Read]（sort 后字母序）。
func TestATDD_58_1_010_AllowedTools_UnionAgentAndSkill(t *testing.T) {
	info := agentInfoWithTools([]string{"Bash"}, "Read")
	got := info.AllowedTools()
	want := []string{"Bash", "Read"}
	if !slices.Equal(got, want) {
		t.Errorf("AllowedTools() = %v, want %v（agent.tools ∪ skill.allowed-tools, sort）", got, want)
	}
}

// 58.1-UNIT-002 [RED] AC2 重叠去重：agent 与 skill 都声明 Read → 结果中 Read 只出现一次。
func TestATDD_58_1_011_AllowedTools_OverlapDedup(t *testing.T) {
	info := agentInfoWithTools([]string{"Read", "Bash"}, "Read Glob")
	got := info.AllowedTools()
	want := []string{"Bash", "Glob", "Read"} // 去重 + sort
	if !slices.Equal(got, want) {
		t.Errorf("AllowedTools() = %v, want %v（重叠 Read 去重，并集 sort）", got, want)
	}
	// 显式核验 Read 不重复。
	count := 0
	for _, v := range got {
		if v == "Read" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("AllowedTools() = %v, Read 出现 %d 次（应去重为 1 次）", got, count)
	}
}

// 58.1-UNIT-003 [RED] AC1 无 skill：agent 仅声明 tools，无 skill → AllowedTools() = agent.tools（sort）。
func TestATDD_58_1_012_AllowedTools_AgentOnly_NoSkill(t *testing.T) {
	info := agentInfoWithTools([]string{"Read", "Bash"}) // 无 skill
	got := info.AllowedTools()
	want := []string{"Bash", "Read"}
	if !slices.Equal(got, want) {
		t.Errorf("AllowedTools() = %v, want %v（无 skill 时 = agent.tools sort）", got, want)
	}
}

// 58.1-UNIT-004 AC3（green-guard）缺省：无 Manifest.Tools（nil）→ AllowedTools() = 纯 skill 并集。
//
// 非 RED：加空 Tools 骨架字段不改现有聚合路径 → dev 前已绿。green-guard 拦截
// 「缺省=全部工具」回归红线（CAP-3 不回归硬约束）：dev 并入 Manifest.Tools 时，
// nil Tools 必须不追加任何元素。
func TestATDD_58_1_013_AllowedTools_NilTools_PureSkillUnion_GreenGuard(t *testing.T) {
	info := agentInfoWithTools(nil, "Read Bash") // Manifest.Tools=nil
	got := info.AllowedTools()
	want := []string{"Bash", "Read"} // 纯 skill 并集
	if !slices.Equal(got, want) {
		t.Errorf("AC3: AllowedTools() = %v, want %v（缺省 nil Tools 应 = 纯 skill 并集，不追加额外工具）", got, want)
	}
}

// 58.1-UNIT-005 AC3（green-guard）显式空数组：Manifest.Tools=[] → 等价缺省（纯 skill 并集，
// 不清空 skill、不授全集）。
//
// 非 RED：空数组遍历零次追加，dev 前后均 = 纯 skill 并集 → green-guard 不 skip。
func TestATDD_58_1_014_AllowedTools_EmptyTools_EquivalentToDefault_GreenGuard(t *testing.T) {
	info := agentInfoWithTools([]string{}, "Read Bash") // 显式空数组
	got := info.AllowedTools()
	want := []string{"Bash", "Read"}
	if !slices.Equal(got, want) {
		t.Errorf("AC3: AllowedTools() = %v, want %v（tools:[] 显式空应等价缺省 = 纯 skill 并集）", got, want)
	}
	// 绝不因空数组清空 skill 工具或回退全集。
	if len(got) == 0 {
		t.Error("AC3: tools:[] 不得清空 skill 工具（应保留 skill 并集）")
	}
}
