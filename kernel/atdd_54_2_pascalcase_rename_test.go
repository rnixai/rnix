package kernel

import (
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ATDD Story 54.2 — Rnix 独有工具统一 PascalCase（kernel meta 工具 complete/replan + toolMap 派生机制）。
//
// 红灯机制（Decker 偏好，atdd-code-story-red-mechanism-preference）：本 story 是「改名」，
// 涉及的符号都【已存在】 → RED 测试断言【新名】，在改名前【运行时失败】（非编译失败）。
// 为保 ATDD 提交期 `make all` 全绿，RED 脚手架用 t.Skip 标记；dev-story 移除 t.Skip + 落地
// 改名后验证 RED→GREEN。green-guard（不 skip、立即绿）断言本 story 全程不变的解耦不变量，实时拦回归。
//
// 一句话心智模型：工具有两个名字活在两个世界——①呈现名（ToolDef.Name / toolMap・metaMap key，
// LLM 看见）②内部标识符（ActionType 字符串 / 持久化 action / VFSPath 路由）。54.2 只改①，
// 两个世界靠 metaMap・toolMap 的 key→value 映射解耦，value（Action/VFSPath）不动。

// --- AC2：meta 工具 complete→Complete / replan→Replan（RED 脚手架）---

// TestATDD_54_2_010 断言 complete 呈现名改为 Complete（ToolDef.Name + metaMap key 都改）。
func TestATDD_54_2_010_MetaComplete_RenamedToPascalCase(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——complete→Complete（toolgen.go:118 Name + :134 metaMap key）")

	defs, metaMap := metaToolDefs(FullFeatureFlags(), nil)

	if !hasToolDefName(defs, "Complete") {
		t.Errorf("期望 meta tool def 名为 %q（改名后）", "Complete")
	}
	if hasToolDefName(defs, "complete") {
		t.Errorf("snake_case 旧名 %q 不应残留", "complete")
	}
	if _, ok := metaMap["Complete"]; !ok {
		t.Errorf("期望 metaMap 含 key %q（meta 工具 key 是硬编码，须显式改）", "Complete")
	}
	if _, ok := metaMap["complete"]; ok {
		t.Errorf("snake_case metaMap key %q 不应残留", "complete")
	}
}

// TestATDD_54_2_011 断言 replan 呈现名改为 Replan（受 flags.Replan 门控）。
func TestATDD_54_2_011_MetaReplan_RenamedToPascalCase(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——replan→Replan（toolgen.go:166 Name + :180 metaMap key）")

	defs, metaMap := metaToolDefs(FullFeatureFlags(), nil)

	if !hasToolDefName(defs, "Replan") {
		t.Errorf("期望 meta tool def 名为 %q（改名后）", "Replan")
	}
	if hasToolDefName(defs, "replan") {
		t.Errorf("snake_case 旧名 %q 不应残留", "replan")
	}
	if _, ok := metaMap["Replan"]; !ok {
		t.Errorf("期望 metaMap 含 key %q", "Replan")
	}
	if _, ok := metaMap["replan"]; ok {
		t.Errorf("snake_case metaMap key %q 不应残留", "replan")
	}
}

// TestATDD_54_2_012 解耦核心：呈现名改 PascalCase 后，metaMap 新 key 映射的 Action
// （内部 ActionType）必须仍为 ActionComplete/ActionReplan——即「名变、value 不变」。
func TestATDD_54_2_012_MetaRename_PreservesActionMapping(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——新 key 改名后才存在；验证 value.Action 解耦不变")

	_, metaMap := metaToolDefs(FullFeatureFlags(), nil)

	if got := metaMap["Complete"].Action; got != ActionComplete {
		t.Errorf("metaMap[\"Complete\"].Action = %q, want %q（呈现名变、ActionType 不变）", got, ActionComplete)
	}
	if got := metaMap["Replan"].Action; got != ActionReplan {
		t.Errorf("metaMap[\"Replan\"].Action = %q, want %q", got, ActionReplan)
	}
}

// --- green-guard（不 skip、立即绿、实时拦回归）---

// TestATDD_54_2_900 守护内部 ActionType 字符串值零修改——这些是落盘 steps.jsonl 的持久化
// action 标识符（tool_exec.go 硬编码写入），改名破坏历史数据与 writeback/dashboard 读取（AC5 铁律）。
func TestATDD_54_2_900_GreenGuard_ActionTypeValuesUnchanged(t *testing.T) {
	if ActionComplete != "complete" {
		t.Errorf("ActionComplete = %q, want %q（持久化 action 标识符不可随呈现名改）", ActionComplete, "complete")
	}
	if ActionReplan != "replan" {
		t.Errorf("ActionReplan = %q, want %q", ActionReplan, "replan")
	}
}

// TestATDD_54_2_901 守护 toolMap key→VFSPath 派生机制（toolgen.go:49）对 PascalCase 新名照常
// 工作——既覆盖 intent 风格（subpath 多路复用），也覆盖 memory 风格（无 subpath、注册于完整路径）。
// 用 mock descriptor 直接喂新名，不依赖真实 driver 改名，故立即绿；它保证「AC1 toolMap key 自动
// 派生」这一机制不被破坏，是路由锚点（VFSPath）不变的机制级护栏。
func TestATDD_54_2_901_GreenGuard_ToolMapDerivesVFSPathForPascalCase(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	stub := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) { return nil, nil }

	intentDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "IntentDecompose", Subpath: "/decompose"},
		{Name: "IntentConfirm", Subpath: "/confirm"},
	}}
	memoryDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "MemoryCommit"}, // 无 subpath → VFSPath = 注册路径
	}}
	_ = reg.RegisterWithDriver("/dev/intent", stub, intentDriver)
	_ = reg.RegisterWithDriver("/dev/memory/commit", stub, memoryDriver)

	_, toolMap := buildToolDefs(reg, nil)

	wantPaths := map[string]string{
		"IntentDecompose": "/dev/intent/decompose",
		"IntentConfirm":   "/dev/intent/confirm",
		"MemoryCommit":    "/dev/memory/commit",
	}
	for name, want := range wantPaths {
		got, ok := toolMap[name]
		if !ok {
			t.Errorf("toolMap 缺少 %q（派生机制应支持 PascalCase 名）", name)
			continue
		}
		if got.VFSPath != want {
			t.Errorf("toolMap[%q].VFSPath = %q, want %q（路由锚点）", name, got.VFSPath, want)
		}
	}
}

// TestATDD_54_2_902 守护 deferred skill 占位形态——AC6/决策点 D4 定调：嵌用户技能名的占位
// 保持 `skill_<name>`（ds.Name 是含连字符的用户技能名、属动态用户数据而非固定 Rnix 工具，
// 不套用 R2′ PascalCase 约束）。立即绿，确认本 story 改工具名时未误改 toolgen.go:230 占位形态。
func TestATDD_54_2_902_GreenGuard_DeferredSkillPlaceholderFormat(t *testing.T) {
	defs, metaMap := metaToolDefs(FullFeatureFlags(), []DeferredSkillMeta{
		{Name: "code-analysis", Description: "analyze code"},
	})
	if !hasToolDefName(defs, "skill_code-analysis") {
		t.Errorf("deferred 占位应保持 skill_<name> 形态（AC6/D4 定调），缺 %q", "skill_code-analysis")
	}
	if got := metaMap["skill_code-analysis"].Action; got != ActionDeferredSkillPlaceholder {
		t.Errorf("deferred 占位 Action = %q, want %q（占位仍走 deferred 派发）", got, ActionDeferredSkillPlaceholder)
	}
}

// hasToolDefName 报告 defs 中是否存在指定 Name 的 tool def（Story 54.2 测试本地 helper）。
func hasToolDefName(defs []vfs.ToolDef, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}
