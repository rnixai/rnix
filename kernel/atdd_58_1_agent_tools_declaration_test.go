package kernel

import (
	"slices"
	"strings"
	"testing"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// ATDD Story 58.1 — Agent.yaml `tools` 字段（agent 级工具直接声明层）。
//
// 以 spawn 集成测试断言 CAP-1..CAP-5 的 proc.AllowedTools / proc.AllowedDevices 终值。
// 单一最小改动点 = agents.AgentInfo.AllowedTools()：在聚合 skill allowed-tools 之外
// 并入 a.Manifest.Tools（同 toolSet 去重 + sort）。spawn.go:286 的
// agentAllowed := agent.AllowedTools() 自动消费，归一化 + 工具级 enforcement +
// 父约束收窄全部复用 Story 54.5 收口的管线（kernel enforcement 代码零改动）。
//
// ── RED 形态：骨架 + t.Skip（Decker 2026-06-11 拍板，代码型 story 统一全绿提交）──
// ATDD 阶段已加骨架字段 agents.AgentManifest.Tools（agents/types.go），但
// AllowedTools()（agents/types.go:57-71）尚未并入 Manifest.Tools → agent.tools 当前
// 不进基线工具集。故所有 RED 用例标 t.Skip 保 ATDD 提交期 `make all` 全绿。
//   - dev-story 移除 t.Skip + 在 AllowedTools() 并入 Manifest.Tools（去重 + sort）→
//     用例由红转绿。
//   - AC6（文档正文改写 docs/skills.md + ADR 追加 Decision 46 + index.md 登记）是
//     dev-story 落地的文档动作，非可自动化的运行时断言，不在本文件覆盖（见 checklist）。
//   - AC7（缺省无 tools 字段不回归）由 58.1-INT-003 的 green-guard 实时拦截，不 skip。
//
// 单元层（AllowedTools() 纯函数并集 / 去重 / 缺省）见 agents/atdd_58_1_allowed_tools_union_test.go。

// agentWithToolsAndSkill 构造一个 agent：顶层 Manifest.Tools 声明 agentToolsCSV，
// 并引用一个 allowed-tools = skillToolsCSV 的 skill。任一参数传 "" 表示该层不声明
// （skillToolsCSV == "" → 不挂 skill；agentToolsCSV == "" → Tools 为 nil）。
//
// 复用 agentWithAllowedTools（spawn_allowed_devices_test.go:31）的 skill 构造形态，
// 仅追加 agent 顶层 Tools 字段——这是 58.1 相对 54.x 的唯一新增维度。
func agentWithToolsAndSkill(agentToolsCSV, skillToolsCSV string) *agents.AgentInfo {
	var tools []string
	if agentToolsCSV != "" {
		tools = strings.Fields(agentToolsCSV)
	}
	info := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "test-agent",
			Models: agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
			Tools:  tools,
		},
		Instructions: "test",
	}
	if skillToolsCSV != "" {
		info.Skills = []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "test-skill",
					AllowedToolsRaw: skillToolsCSV,
				},
				Body: "test",
			},
		}
	}
	return info
}

// ───────────────── CAP-1：agent 可独立声明工具（无 skill）─────────────────

// 58.1-INT-001 [RED] AC1/CAP-1：agent 不引用任何 skill、仅顶层 tools:[Read,Bash] →
// spawn 后 proc.AllowedTools 含 Read/Bash，proc.AllowedDevices 含设备根 /dev/fs /dev/shell，
// 未声明的 Write 不可见，buildToolDefs(AllowedDevices) 非空。
//
// RED：AllowedTools() 未并入 Manifest.Tools → 无 skill 的 agent.AllowedTools()=nil →
// proc.AllowedTools/AllowedDevices 均空 → 断言「含 Read」失败。移 skip 实跑验真 FAIL。
func TestATDD_58_1_001_AgentToolsOnly_NoSkill_Declared(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("Read Bash", "") // 仅 agent 声明，无 skill

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}

	for _, want := range []string{"Read", "Bash"} {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC1: proc.AllowedTools=%v 缺 agent 声明工具 %q", proc.AllowedTools, want)
		}
	}
	// 未声明的 Write 不可见。
	if slices.Contains(proc.AllowedTools, "Write") {
		t.Errorf("AC1: proc.AllowedTools=%v 含未声明的 Write（agent.tools 只授声明项）", proc.AllowedTools)
	}
	// AllowedDevices 是设备根（声明工具名归一化反查），非工具名。
	for _, want := range []string{"/dev/fs", "/dev/shell"} {
		if !slices.Contains(proc.AllowedDevices, want) {
			t.Errorf("AC1: proc.AllowedDevices=%v 缺设备根 %q", proc.AllowedDevices, want)
		}
	}
	if slices.Contains(proc.AllowedDevices, "Read") {
		t.Errorf("AC1: proc.AllowedDevices=%v 含工具名 Read（须为设备根）", proc.AllowedDevices)
	}
	// 呈现层非空：buildToolDefs 给 LLM 路由到 /dev/fs + /dev/shell。
	defs, _ := buildToolDefs(k.deviceRegistry(), proc.AllowedDevices)
	if len(defs) == 0 {
		t.Error("AC1: buildToolDefs(proc.AllowedDevices) 为空（LLM 工具列表不应为空）")
	}
}

// ───────────────── CAP-2：agent.tools × skill.allowed-tools 取并集 ─────────────────

// 58.1-INT-002 [RED] AC2/CAP-2：agent tools:[Bash] + skill allowed-tools:[Read] →
// 基线 {Read, Bash}（并集，非覆盖、非仅收窄）。
//
// RED：未并入 → proc.AllowedTools 仅含 skill 的 Read，不含 agent 的 Bash → 断言含 Bash 失败。
func TestATDD_58_1_002_AgentToolsUnionSkillTools(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("Bash", "Read") // agent 声明 Bash，skill 携带 Read

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	for _, want := range []string{"Read", "Bash"} {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC2: proc.AllowedTools=%v 缺并集工具 %q（agent.tools ∪ skill.allowed-tools）", proc.AllowedTools, want)
		}
	}
	for _, want := range []string{"/dev/fs", "/dev/shell"} {
		if !slices.Contains(proc.AllowedDevices, want) {
			t.Errorf("AC2: proc.AllowedDevices=%v 缺设备根 %q", proc.AllowedDevices, want)
		}
	}
}

// ───────────────── CAP-3：缺省 / 空数组 / 通配 fail-closed ─────────────────

// 58.1-INT-003 AC3/CAP-3 + AC7（green-guard）：不含 tools 字段的 agent → proc.AllowedTools
// 与纯 skill 并集逐项一致（无任何行为变化）。
//
// 非 RED：当前 AllowedTools() 仅聚合 skill，加空 Tools 骨架字段不改此路径 → dev 前已 PASS。
// 定性为 GREEN 护栏（GREEN-stays-GREEN 不 skip，[[atdd-code-story-red-mechanism-preference]]）：
// dev 在 AllowedTools() 并入 Manifest.Tools 时，nil/空 Tools 必须不追加任何元素（禁止
// 「缺省=全部工具」分支），本用例实时拦截该回归红线（CAP-3 不回归是硬约束）。
func TestATDD_58_1_003_NoToolsField_PureSkillUnion_GreenGuard(t *testing.T) {
	k := newToolLevelKernel(t)
	// 缺省 agent：无 Manifest.Tools，仅 skill 携带 /dev/fs 全集工具名。
	agent := agentWithToolsAndSkill("", "Read Write Edit Glob Grep")

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	// 基线 = 纯 skill 并集：含 fs 工具全集，不含任何「凭空多出」的工具（如 Bash）。
	for _, want := range fsToolNames {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC3/AC7: proc.AllowedTools=%v 缺 skill 工具 %q（缺省 agent 应 = 纯 skill 并集）", proc.AllowedTools, want)
		}
	}
	if slices.Contains(proc.AllowedTools, "Bash") {
		t.Errorf("AC3/AC7: proc.AllowedTools=%v 含 Bash（缺省无 tools 字段不应授予额外工具）", proc.AllowedTools)
	}
	if !slices.Equal(proc.AllowedDevices, []string{"/dev/fs"}) {
		t.Errorf("AC3/AC7: proc.AllowedDevices=%v, want [/dev/fs]（纯 skill /dev/fs 设备根）", proc.AllowedDevices)
	}
}

// 58.1-INT-004 [RED] AC3/CAP-3：tools:["*"] 通配 → fail-closed 静默丢弃（* 非已知工具名），
// 绝不回退为全集。skill 携带 Read → proc.AllowedTools 恰为 {Read}，不含 Write/Bash 等未声明工具。
//
// 58.1-INT-004 AC3/CAP-3（green-guard）：tools:["*"] 通配 → fail-closed 静默丢弃（* 非已知
// 工具名），绝不回退为全集。skill 携带 Read → proc.AllowedTools 恰含 Read，不含 Write/Bash 等。
//
// 非 RED：dev 前 agent.tools 整体未消费、基线 = skill 的 {Read}（断言成立）；dev 后 agent.tools
// 经管线消费、"*" 作为未知工具名被 normalizeDeclaredAllowedTools 静默丢弃、基线仍 = {Read}
// （断言仍成立）。两态皆 PASS → 定性 GREEN 护栏（GREEN-stays-GREEN 不 skip）：实时拦截
// 「* 回退全集」回归红线（CAP-3 硬约束）。RED 有效性验证（移 skip 实跑）确认本用例 dev 前已 PASS。
func TestATDD_58_1_004_WildcardToolsFailClosed_GreenGuard(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("*", "Read") // 通配 + skill 携带 Read

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	// "*" 是未知工具名 → 静默丢弃 → 基线 = skill 并集 {Read}，绝不回退全集。
	if !slices.Contains(proc.AllowedTools, "Read") {
		t.Errorf("AC3: proc.AllowedTools=%v 缺 skill 工具 Read", proc.AllowedTools)
	}
	if slices.Contains(proc.AllowedTools, "*") {
		t.Errorf("AC3: proc.AllowedTools=%v 含 *（通配须 fail-closed 静默丢弃，非已知工具名）", proc.AllowedTools)
	}
	for _, forbidden := range []string{"Write", "Edit", "Bash"} {
		if slices.Contains(proc.AllowedTools, forbidden) {
			t.Errorf("AC3: proc.AllowedTools=%v 含 %q —— tools:[\"*\"] 绝不回退为全集", proc.AllowedTools, forbidden)
		}
	}
}

// ───────────────── CAP-4：复用归一化 + 工具级 enforcement 零旁路 ─────────────────

// 58.1-INT-005 [RED] AC4/CAP-4：agent 仅声明 tools:[Read]（无 skill）→ 同设备 /dev/fs 的
// Write 不被放行（工具级 enforcement 不被 agent.tools 绕过，与 Decision 45 一致）。
//
// RED：未并入 → agent.AllowedTools()=nil → proc.AllowedTools=nil → 工具白名单未激活 →
// executeVFSTool 退化为设备级判定，但 proc.AllowedDevices 也空 → Read 会 device-level 拒；
// 断言「Read 放行」失败。dev 后 Read 进白名单放行、Write 工具级拒 → 转绿。
func TestATDD_58_1_005_ToolLevelEnforcement_NoBypass(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("Read", "") // 仅声明 Read

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}

	// Read：工具级放行——不得 permission denied（缺 path 报 missing-path 可接受）。
	_, readErr := k.executeVFSTool(proc,
		llmToolCall{Name: "Read", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/fs", FSOperation: "Read"})
	if readErr != nil && strings.Contains(readErr.Error(), "permission denied") {
		t.Errorf("AC4: 声明 Read 应放行，却被权限拒: %v", readErr)
	}
	// Write：工具级拒——即使 Write 与 Read 同属 /dev/fs（agent.tools 不绕过 enforcement）。
	_, writeErr := k.executeVFSTool(proc,
		llmToolCall{Name: "Write", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/fs", FSOperation: "Write"})
	if writeErr == nil || !strings.Contains(writeErr.Error(), "permission denied") {
		t.Errorf("AC4: 声明 Read 不得放行同设备 Write（工具级 enforcement 零旁路），got err=%v", writeErr)
	}
}

// 58.1-INT-008 AC4/CAP-4：agent tools 含 legacy 设备路径值（/dev/fs）→ 经 isDevicePathValue
// 分支向后兼容展开为该设备全部工具名（与 skill 设备路径值同规则，Story 54.1 expandDevicesToTools）。
//
// 与 INT-005（工具级 enforcement 零旁路）配对，补齐 CAP-4 的「设备路径向后兼容展开」子句断言。
func TestATDD_58_1_008_AgentToolsDevicePath_BackwardCompatExpand(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("/dev/fs", "") // agent 直接声明设备路径值（向后兼容形态）

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	// 设备路径值展开为 /dev/fs 全部工具名（Read/Write/Edit/Glob/Grep）。
	for _, want := range fsToolNames {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC4: proc.AllowedTools=%v 缺设备路径展开工具 %q（/dev/fs 向后兼容展开）", proc.AllowedTools, want)
		}
	}
	// 设备根落入 AllowedDevices，不串入其它设备（无 skill、无 /dev/shell）。
	if !slices.Equal(proc.AllowedDevices, []string{"/dev/fs"}) {
		t.Errorf("AC4: proc.AllowedDevices=%v, want [/dev/fs]（设备路径值展开后的设备根）", proc.AllowedDevices)
	}
}

// 58.1-INT-009 AC4/CAP-4：agent tools 含任意非法/未知工具名（非 "*"）→ 静默丢弃，
// 绝不报错、绝不回退全集；同声明里的合法工具名仍正常生效。
//
// 与 INT-004（"*" 通配丢弃）互补：INT-004 证通配，本用例证一般未知名（typo 等）。
func TestATDD_58_1_009_AgentToolsUnknownName_SilentlyDropped(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("Read BogusToolXyz", "") // 合法 Read + 垃圾名

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err) // 未知名不得致 spawn 失败
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	// 合法 Read 生效。
	if !slices.Contains(proc.AllowedTools, "Read") {
		t.Errorf("AC4: proc.AllowedTools=%v 缺合法工具 Read", proc.AllowedTools)
	}
	// 未知名静默丢弃，不入白名单。
	if slices.Contains(proc.AllowedTools, "BogusToolXyz") {
		t.Errorf("AC4: proc.AllowedTools=%v 含未知名 BogusToolXyz（应静默丢弃）", proc.AllowedTools)
	}
	// 未知名不得触发全集回退（除 Read 设备根 /dev/fs 外无其它设备）。
	if !slices.Equal(proc.AllowedDevices, []string{"/dev/fs"}) {
		t.Errorf("AC4: proc.AllowedDevices=%v, want [/dev/fs]（未知名丢弃，不回退全集）", proc.AllowedDevices)
	}
}

// ───────────────── CAP-5：父约束对「agent ∪ skill」基线只能交集收窄 ─────────────────

// 58.1-INT-006 AC5/CAP-5（green-guard）：父约束 opts.AllowedTools=[Read] 作用于基线
// {Read, Bash}（agent tools:[Bash] + skill [Read]）→ proc.AllowedTools 不含 Bash。
//
// 非 RED：dev 前基线仅 skill 的 {Read}、父约束 [Read] 交集后 {Read}（不含 Bash，断言成立）；
// dev 后基线 {Read, Bash} 被父约束 [Read] 交集收窄掉 Bash（仍不含 Bash，断言成立）。两态皆 PASS
// → GREEN 护栏：实时拦截「父约束未能收窄 agent.tools」回归。本用例的有效 RED 配对是 INT-007
// （证基线确实含 Bash），两者共同锁死 CAP-5「父约束只减不增、无法超出基线」时序。
func TestATDD_58_1_006_ParentConstraintNarrowsBaseline_GreenGuard(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("Bash", "Read") // 基线 {Read, Bash}

	pid, err := k.Spawn("child", agent, SpawnOpts{
		AllowedTools:   []string{"Read"}, // 工具级父约束：仅 Read
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	if !slices.Contains(proc.AllowedTools, "Read") {
		t.Errorf("AC5: proc.AllowedTools=%v 应含 Read（父约束内）", proc.AllowedTools)
	}
	if slices.Contains(proc.AllowedTools, "Bash") {
		t.Errorf("AC5: proc.AllowedTools=%v 含 Bash，但父约束 opts.AllowedTools=[Read]（应交集收窄掉）", proc.AllowedTools)
	}
}

// 58.1-INT-007 [RED] AC5/CAP-5（基线锚点）：无父约束时基线 = agent.tools ∪ skill = {Read, Bash}；
// 证明 Bash 确实在基线内（与 INT-006 配对：INT-007 证基线含 Bash，INT-006 证父约束能收窄掉它，
// 共同锁死「父约束只减不增、无法超出基线」时序）。
//
// RED：未并入 → 基线仅 skill 的 {Read}，缺 agent 的 Bash → 断言含 Bash 失败。
func TestATDD_58_1_007_BaselineUnionWithoutParentConstraint(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithToolsAndSkill("Bash", "Read") // agent.tools=[Bash] + skill=[Read]

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true}) // 无父约束
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	// 无父约束 → 基线 = 完整并集 {Read, Bash}，父约束无法让 proc 超出此基线（只减不增）。
	for _, want := range []string{"Read", "Bash"} {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC5: 无父约束时 proc.AllowedTools=%v 缺基线工具 %q（agent.tools ∪ skill）", proc.AllowedTools, want)
		}
	}
}

// 编译期 sanity：确保 newToolLevelKernel 注册的设备齐全（fs+shell），与 54.1 共用。
var _ = vfs.NewDeviceRegistry
var _ = rnixctx.NewManager
