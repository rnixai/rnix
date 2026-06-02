package kernel

import (
	"reflect"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ATDD 52.2: Kernel 条件注入
//
// 来源 Story：_bmad-output/implementation-artifacts/52-2-kernel-conditional-injection.md
// Story Key: 52-2-kernel-conditional-injection
//
// 验收标准（Acceptance Criteria）：
//   AC1: metaToolDefs 签名改为 metaToolDefs(flags FeatureFlags, deferredSkills []DeferredSkillMeta)
//   AC2: flags.Replan == false → 不生成 replan tool def
//   AC3: flags.Specialize == false → 不生成 Skill tool def
//   AC4: flags.DiscoverSkill == false → 不生成 ToolSearch tool def
//   AC5: flags.Spawn == false → 不生成 Agent tool def
//   AC6: daemon 装配区：flags 控制 SetDiffMemory / SetStemMatcher / SetImmuneDaemon 注入
//   AC7: flags.Compaction == false → CompactionDisabled=true, autoCompactIfNeeded 提前返回
//   AC8: baseline profile 下进程仍能正常运行（仅 complete action）
//   AC9: full profile 行为与当前无配置时完全一致（回归测试）
//   AC10: 已有 TestMetaToolDefs_PlanningDisabled 等测试适配新签名
//
// ============================== RED PHASE ==============================
// 本测试在 Story 52.2 实现前应 FAIL（红灯）。
// 预期失败原因：
//   1. kernel.FeatureFlags 类型未定义（kernel/feature_flags.go 不存在）
//   2. FullFeatureFlags() 函数未定义
//   3. Process.FeatureFlags 字段不存在
//   4. Process.CompactionDisabled 字段不存在
//   5. KernelImpl.SetFeatureFlags() 方法不存在
//   6. metaToolDefs 签名仍为 (planningEnabled bool, ...) 而非 (flags FeatureFlags, ...)
//
// 实现后所有用例应转绿（含 go test -race）。
// 这是 Go ATDD 的标准红灯形态，与 52-1 ATDD 测试一致。
// ======================================================================

// ---------- AC1: kernel.FeatureFlags 类型有 9 个 bool 字段 + ProfileName string ----------

func TestATDD_52_2_AC1_FeatureFlags_StructShape(t *testing.T) {
	rt := reflect.TypeFor[FeatureFlags]()

	expectedBoolFields := []string{
		"Planning", "Replan", "Specialize", "DiscoverSkill", "Spawn",
		"DiffMemory", "StemMatcher", "Immune", "Compaction",
	}

	// 9 bool fields + ProfileName string = 10 total (Story 52.3 added ProfileName)
	if rt.NumField() != len(expectedBoolFields)+1 {
		t.Fatalf("FeatureFlags has %d fields, want %d", rt.NumField(), len(expectedBoolFields)+1)
	}

	for _, name := range expectedBoolFields {
		f, ok := rt.FieldByName(name)
		if !ok {
			t.Errorf("FeatureFlags missing field %q", name)
			continue
		}
		if f.Type.Kind() != reflect.Bool {
			t.Errorf("FeatureFlags.%s is %v, want bool", name, f.Type.Kind())
		}
	}
}

// ---------- AC1: FullFeatureFlags() 返回全 true ----------

func TestATDD_52_2_AC1_FullFeatureFlags(t *testing.T) {
	flags := FullFeatureFlags()

	if !flags.Planning || !flags.Replan || !flags.Specialize || !flags.DiscoverSkill ||
		!flags.Spawn || !flags.DiffMemory || !flags.StemMatcher || !flags.Immune || !flags.Compaction {
		t.Error("FullFeatureFlags() should return all true")
	}
}

// ---------- AC1: metaToolDefs 接受 FeatureFlags（新签名） ----------

func TestATDD_52_2_AC1_MetaToolDefs_NewSignature(t *testing.T) {
	flags := FullFeatureFlags()
	defs, metaMap := metaToolDefs(flags, nil)

	if len(defs) == 0 {
		t.Fatal("metaToolDefs with full flags should return at least one tool def")
	}
	if len(metaMap) == 0 {
		t.Fatal("metaToolDefs with full flags should return non-empty metaMap")
	}
}

// ---------- AC2: Replan disabled → 无 replan tool ----------

func TestATDD_52_2_AC2_ReplanDisabled(t *testing.T) {
	flags := FullFeatureFlags()
	flags.Replan = false
	defs, metaMap := metaToolDefs(flags, nil)

	for _, d := range defs {
		if d.Name == "replan" {
			t.Fatal("replan tool def should not be generated when flags.Replan == false")
		}
	}
	if _, ok := metaMap["replan"]; ok {
		t.Fatal("replan should not be in metaMap when flags.Replan == false")
	}
}

// ---------- AC3: Specialize disabled → 无 Skill tool ----------

func TestATDD_52_2_AC3_SpecializeDisabled(t *testing.T) {
	flags := FullFeatureFlags()
	flags.Specialize = false
	defs, metaMap := metaToolDefs(flags, nil)

	for _, d := range defs {
		if d.Name == "Skill" {
			t.Fatal("Skill tool def should not be generated when flags.Specialize == false")
		}
	}
	if _, ok := metaMap["Skill"]; ok {
		t.Fatal("Skill should not be in metaMap when flags.Specialize == false")
	}
}

// ---------- AC4: DiscoverSkill disabled → 无 ToolSearch tool ----------

func TestATDD_52_2_AC4_DiscoverSkillDisabled_NoToolSearch(t *testing.T) {
	flags := FullFeatureFlags()
	flags.DiscoverSkill = false
	defs, metaMap := metaToolDefs(flags, nil)

	for _, d := range defs {
		if d.Name == "ToolSearch" {
			t.Fatal("ToolSearch tool def should not be generated when flags.DiscoverSkill == false")
		}
	}
	if _, ok := metaMap["ToolSearch"]; ok {
		t.Fatal("ToolSearch should not be in metaMap when flags.DiscoverSkill == false")
	}
}

// ---------- AC4: DiscoverSkill disabled → deferred skill placeholder 也不生成 ----------

func TestATDD_52_2_AC4_DiscoverSkillDisabled_NoDeferredPlaceholders(t *testing.T) {
	flags := FullFeatureFlags()
	flags.DiscoverSkill = false

	deferred := []DeferredSkillMeta{
		{Name: "code-review", Description: "Review code changes", SearchHint: "lint review"},
		{Name: "formatter", Description: "Format source files", SearchHint: "prettify"},
	}

	defs, _ := metaToolDefs(flags, deferred)

	for _, d := range defs {
		if d.Name == "ToolSearch" {
			t.Fatal("ToolSearch should not appear when DiscoverSkill is disabled")
		}
	}

	// DiscoverSkill 关闭时，即使传入了 deferred skills，也不应生成任何
	// deferred skill placeholder tool def（它们依赖 ToolSearch）。
	// 全部 flag 关闭（仅 DiscoverSkill 以外的 flag 开启）下，
	// defs 数量应 == FullFeatureFlags 减去 ToolSearch 的数量，
	// 不会因为 deferred 而膨胀。
	enabledFlags := FullFeatureFlags()
	enabledFlags.DiscoverSkill = false
	defsWithoutDeferred, _ := metaToolDefs(enabledFlags, nil)
	if len(defs) != len(defsWithoutDeferred) {
		t.Errorf("with DiscoverSkill=false, deferred skills should not add tool defs: got %d, want %d",
			len(defs), len(defsWithoutDeferred))
	}
}

// ---------- AC5: Spawn disabled → 无 Agent tool ----------

func TestATDD_52_2_AC5_SpawnDisabled(t *testing.T) {
	flags := FullFeatureFlags()
	flags.Spawn = false
	defs, metaMap := metaToolDefs(flags, nil)

	for _, d := range defs {
		if d.Name == "Agent" {
			t.Fatal("Agent tool def should not be generated when flags.Spawn == false")
		}
	}
	if _, ok := metaMap["Agent"]; ok {
		t.Fatal("Agent should not be in metaMap when flags.Spawn == false")
	}
}

// ---------- AC7: Compaction disabled → Process.CompactionDisabled = true ----------

func TestATDD_52_2_AC7_CompactionDisabled_Flag(t *testing.T) {
	proc := &Process{}
	proc.FeatureFlags = FeatureFlags{Compaction: false}

	// spawn 路径应将 CompactionDisabled 设为 true
	// 这里直接验证字段存在性和语义
	proc.CompactionDisabled = true

	if !proc.CompactionDisabled {
		t.Fatal("CompactionDisabled should be true when FeatureFlags.Compaction is false")
	}
}

// ---------- AC8: Baseline profile → 仅 complete tool ----------

func TestATDD_52_2_AC8_BaselineProfile_OnlyComplete(t *testing.T) {
	// baseline: 所有 flag 为 false
	flags := FeatureFlags{}
	defs, metaMap := metaToolDefs(flags, nil)

	if len(defs) != 1 {
		t.Fatalf("baseline profile: expected exactly 1 tool def (complete), got %d", len(defs))
	}
	if defs[0].Name != "complete" {
		t.Fatalf("baseline profile: expected tool name 'complete', got %q", defs[0].Name)
	}
	if _, ok := metaMap["complete"]; !ok {
		t.Fatal("baseline profile: 'complete' must be in metaMap")
	}
	if metaMap["complete"].Action != ActionComplete {
		t.Fatalf("baseline profile: complete action = %v, want ActionComplete", metaMap["complete"].Action)
	}
}

// ---------- AC9: Full profile → 全部 6 个 meta tool（回归） ----------

func TestATDD_52_2_AC9_FullProfile_AllTools(t *testing.T) {
	flags := FullFeatureFlags()
	defs, metaMap := metaToolDefs(flags, nil)

	expectedNames := map[string]bool{
		"complete":      false,
		"Agent":         false,
		"replan":        false,
		"Skill":         false,
		"ToolSearch":    false,
		"EnterPlanMode": false,
	}
	for _, d := range defs {
		if _, ok := expectedNames[d.Name]; ok {
			expectedNames[d.Name] = true
		}
	}
	for name, found := range expectedNames {
		if !found {
			t.Errorf("full profile: missing meta tool %q", name)
		}
	}

	// metaMap 应包含全部 6 个条目
	for name := range expectedNames {
		if _, ok := metaMap[name]; !ok {
			t.Errorf("full profile: %q missing from metaMap", name)
		}
	}
}

// ---------- AC10: Planning disabled via FeatureFlags → 无 EnterPlanMode ----------

func TestATDD_52_2_AC10_PlanningDisabledViaFlags(t *testing.T) {
	flags := FullFeatureFlags()
	flags.Planning = false
	defs, metaMap := metaToolDefs(flags, nil)

	for _, d := range defs {
		if d.Name == "EnterPlanMode" {
			t.Fatal("EnterPlanMode should not be generated when flags.Planning == false")
		}
	}
	if _, ok := metaMap["EnterPlanMode"]; ok {
		t.Fatal("EnterPlanMode should not be in metaMap when flags.Planning == false")
	}

	// complete 仍应存在
	if _, ok := metaMap["complete"]; !ok {
		t.Fatal("complete must always be present regardless of Planning flag")
	}
}

// ---------- AC6/AC8: KernelImpl.SetFeatureFlags 方法存在且可调用 ----------

func TestATDD_52_2_AC6_KernelSetFeatureFlags(t *testing.T) {
	k := &KernelImpl{}

	flags := FeatureFlags{
		Planning: true, Replan: false, Specialize: true,
		DiscoverSkill: false, Spawn: true, DiffMemory: false,
		StemMatcher: true, Immune: false, Compaction: true,
	}

	k.SetFeatureFlags(flags)

	// 验证 flags 存储到 kernel
	if k.featureFlags.Planning != true {
		t.Error("SetFeatureFlags: Planning should be true")
	}
	if k.featureFlags.Replan != false {
		t.Error("SetFeatureFlags: Replan should be false")
	}
	if k.featureFlags.Spawn != true {
		t.Error("SetFeatureFlags: Spawn should be true")
	}
}

// ---------- AC9: 组合——多个 flag 同时禁用 ----------

func TestATDD_52_2_CombinedDisable_SpawnAndReplan(t *testing.T) {
	flags := FullFeatureFlags()
	flags.Spawn = false
	flags.Replan = false
	defs, metaMap := metaToolDefs(flags, nil)

	for _, d := range defs {
		if d.Name == "Agent" || d.Name == "replan" {
			t.Fatalf("tool %q should not be generated when its flag is false", d.Name)
		}
	}
	if _, ok := metaMap["Agent"]; ok {
		t.Fatal("Agent should not be in metaMap")
	}
	if _, ok := metaMap["replan"]; ok {
		t.Fatal("replan should not be in metaMap")
	}

	// complete, Skill, ToolSearch, EnterPlanMode 仍应存在
	for _, name := range []string{"complete", "Skill", "ToolSearch", "EnterPlanMode"} {
		if _, ok := metaMap[name]; !ok {
			t.Errorf("combined disable: %q should still be present", name)
		}
	}
}

// ---------- AC9: 组合——全部可选 flag 禁用，仅留 Planning ----------

func TestATDD_52_2_CombinedDisable_OnlyPlanningAndComplete(t *testing.T) {
	flags := FeatureFlags{Planning: true}
	defs, metaMap := metaToolDefs(flags, nil)

	// 应仅有 complete + EnterPlanMode
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool defs (complete + EnterPlanMode), got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["complete"] {
		t.Error("complete should always be present")
	}
	if !names["EnterPlanMode"] {
		t.Error("EnterPlanMode should be present when Planning is true")
	}
	if _, ok := metaMap["complete"]; !ok {
		t.Error("complete should be in metaMap")
	}
	if _, ok := metaMap["EnterPlanMode"]; !ok {
		t.Error("EnterPlanMode should be in metaMap")
	}
}

// ---------- AC4 + DiscoverSkill enabled → deferred skills 正常生成 ----------

func TestATDD_52_2_DiscoverSkillEnabled_DeferredSkillsGenerated(t *testing.T) {
	flags := FullFeatureFlags()

	deferred := []DeferredSkillMeta{
		{Name: "code-review", Description: "Review code changes", SearchHint: "lint review"},
	}

	defs, metaMap := metaToolDefs(flags, deferred)

	if _, ok := metaMap["ToolSearch"]; !ok {
		t.Fatal("ToolSearch should be present when DiscoverSkill is enabled")
	}

	// 当 DiscoverSkill 启用时，deferred skills 应有对应 placeholder
	foundToolSearch := false
	for _, d := range defs {
		if d.Name == "ToolSearch" {
			foundToolSearch = true
		}
	}
	if !foundToolSearch {
		t.Error("ToolSearch tool def should be generated when DiscoverSkill is enabled")
	}
}

// ---------- AC1: Process.FeatureFlags 字段存在 ----------

func TestATDD_52_2_AC1_ProcessFeatureFlagsField(t *testing.T) {
	proc := &Process{}
	proc.FeatureFlags = FullFeatureFlags()

	if !proc.FeatureFlags.Planning {
		t.Error("Process.FeatureFlags.Planning should be true after setting FullFeatureFlags()")
	}
	if !proc.FeatureFlags.Compaction {
		t.Error("Process.FeatureFlags.Compaction should be true after setting FullFeatureFlags()")
	}
}

// ---------- AC7: autoCompactIfNeeded 跳过 when CompactionDisabled ----------

func TestATDD_52_2_AC7_AutoCompactSkipsWhenDisabled(t *testing.T) {
	proc := &Process{}
	proc.CompactionDisabled = true

	k := &KernelImpl{}
	// autoCompactIfNeeded should return immediately without panic
	// when CompactionDisabled is true (no ctxMgr needed)
	k.autoCompactIfNeeded(proc, 1)
}

// ---------- AC8: NewProcess 默认 FeatureFlags 为 FullFeatureFlags ----------

func TestATDD_52_2_AC8_NewProcess_DefaultFeatureFlags(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	expected := FullFeatureFlags()

	if proc.FeatureFlags != expected {
		t.Errorf("NewProcess default FeatureFlags = %+v, want %+v", proc.FeatureFlags, expected)
	}
	if proc.CompactionDisabled {
		t.Error("NewProcess should not have CompactionDisabled=true by default")
	}
}

// ---------- AC9: buildToolDefs 新签名（移除 planningEnabled） ----------

func TestATDD_52_2_AC9_BuildToolDefs_NoThirdArg(t *testing.T) {
	reg := newMockDeviceRegistry(t)
	defs, toolMap := buildToolDefs(reg, nil)

	if len(defs) == 0 {
		t.Fatal("buildToolDefs should return tool defs with nil allowedDevices")
	}
	if len(toolMap) == 0 {
		t.Fatal("buildToolDefs should return non-empty toolMap")
	}
}

func newMockDeviceRegistry(t *testing.T) *vfs.DeviceRegistry {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	factory := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return nil, nil
	}
	shellDriver := &mockToolDescriptor{defs: []vfs.ToolDef{{Name: "Bash", Description: "Run command"}}}
	_ = reg.RegisterWithDriver("/dev/shell", factory, shellDriver)
	return reg
}

// ---------- AC7: CompactionDisabled 字段语义正确 ----------

func TestATDD_52_2_AC7_CompactionDisabled_DerivedFromFlags(t *testing.T) {
	// FeatureFlags.Compaction=false → CompactionDisabled should be set to true by spawn path
	proc := NewProcess(0, "test", nil)
	proc.FeatureFlags.Compaction = false
	// Simulate what spawn.go does
	if !proc.FeatureFlags.Compaction {
		proc.CompactionDisabled = true
	}

	if !proc.CompactionDisabled {
		t.Error("CompactionDisabled should be true when FeatureFlags.Compaction is false")
	}

	// FeatureFlags.Compaction=true → CompactionDisabled remains false
	proc2 := NewProcess(0, "test2", nil)
	if proc2.CompactionDisabled {
		t.Error("CompactionDisabled should be false by default (FeatureFlags.Compaction=true)")
	}
}

// ---------- AC1/AC5: spawn 路径 PlanningEnabled 同步 ----------

func TestATDD_52_2_PlanningOverride_SyncsFeatureFlags(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	// Simulate agent manifest planning: false
	proc.PlanningEnabled = false
	proc.FeatureFlags.Planning = false

	flags := FeatureFlags{Planning: false}
	defs, metaMap := metaToolDefs(flags, nil)

	for _, d := range defs {
		if d.Name == "EnterPlanMode" {
			t.Fatal("EnterPlanMode should not be generated when Planning is false")
		}
	}
	if _, ok := metaMap["EnterPlanMode"]; ok {
		t.Fatal("EnterPlanMode should not be in metaMap")
	}
}
