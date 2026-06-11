package kernel

import (
	"slices"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// ATDD Story 54.5 — 声明值归一化（spawn / specialize 把声明的语义工具名归一化为
// proc.AllowedTools 权威判定 + proc.AllowedDevices 设备根路由）。
//
// 覆盖 AC1（顶层 spawn 归一化，本 story 最高风险核心）、AC2（父约束 / 交集）、
// AC3（specialize 归一化）、AC8（等价性 / 向后兼容 / 设备根反查坑 / 子⊆父）。
// frontmatter / docs 内容断言见 skills/atdd_54_5_builtin_frontmatter_test.go。
//
// ── RED 形态：骨架 + t.Skip（Decker 2026-06-11 拍板，混合 story 统一全绿提交）──
// 归一化 helper k.normalizeDeclaredAllowedTools 在 kernel/devices.go 是空骨架
// （return nil, nil）；spawn(spawn.go:279-435) / specialize(tool_exec.go:1000-1011)
// 尚未接线归一化。故所有 RED 用例标 t.Skip 保 ATDD 提交期 `make all` 全绿。
//   - dev-story 移除 t.Skip + 落地 Task 1（helper 实现）/ Task 2（spawn 接线）/
//     Task 3（specialize 接线）→ 用例由红转绿。
//   - 54.1 的设备路径声明向后兼容由 atdd_54_1_*.go 的 green-guard 实时拦截，本文件不重复。
//
// 设备全集声明等价性（spec owner 选定，零行为变更）：声明 [Read Write Edit Glob Grep
// Bash] 归一化得到的 tools == 旧 [/dev/fs /dev/shell] 经 expandDevicesToTools 的结果
// （逐位等价，AC8.1）。
//
// 命名权威：fs 工具 = fsToolNames（[Read Write Edit Glob Grep]，定义于 atdd_54_1_*.go）；
// shell = Bash；intent 四工具 = intentToolDefs（带 Subpath，复现设备根反查坑）。

// intentToolDefs 是 intent multiplex 设备的工具定义——每个工具带非空 Subpath，对齐
// drivers/intent/driver.go:30-116 的真实 ToolDefs。专门复现「设备根反查坑」：
// toolMap["IntentDecompose"].VFSPath == "/dev/intent" + "/decompose"，而归一化必须
// 反查到设备根 "/dev/intent"，绝非含 subpath 的 "/dev/intent/decompose"。
var intentToolDefs = []vfs.ToolDef{
	{Name: "IntentDecompose", Description: "decompose", Subpath: "/decompose"},
	{Name: "IntentConfirm", Description: "confirm", Subpath: "/confirm"},
	{Name: "IntentExecute", Description: "execute", Subpath: "/execute"},
	{Name: "IntentStatus", Description: "status", Subpath: "/status"},
}

// newNormalizeRegistry = newToolLevelRegistry（/dev/fs 5 工具 + /dev/shell Bash + LLM）
// 追加 /dev/intent multiplex 设备（4 工具带 Subpath）。归一化反查 + 设备根坑共用。
func newNormalizeRegistry() *vfs.DeviceRegistry {
	reg := newToolLevelRegistry()
	_ = reg.RegisterWithDriver("/dev/intent", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	}, &mockToolDescriptor{defs: intentToolDefs})
	return reg
}

func newNormalizeKernel(t *testing.T) *KernelImpl {
	t.Helper()
	reg := newNormalizeRegistry()
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	t.Cleanup(k.Shutdown)
	return k
}

// ───────────────── Layer A：normalizeDeclaredAllowedTools 单测（AC1 投影 / AC8）─────────────────

// 54.5-UNIT-001 [RED] 归一化矩阵（AC1 投影 / AC8.2 向后兼容 / AC8.3 设备根坑 / 边界）。
//
// RED：normalizeDeclaredAllowedTools 骨架 return nil,nil → 移除 t.Skip 后全部失败。
// dev 实现「设备路径直通 + 工具名反查设备根 + 各自去重」后转绿。
func TestATDD_54_5_010_NormalizeDeclaredAllowedTools_Matrix(t *testing.T) {
	k := newNormalizeKernel(t)

	t.Run("device_fullset_tool_names", func(t *testing.T) {
		// AC1 投影：设备全集工具名 → tools=同集、devices=设备根（非工具名）。
		declared := []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash"}
		tools, devices := k.normalizeDeclaredAllowedTools(declared)
		for _, want := range declared {
			if !slices.Contains(tools, want) {
				t.Errorf("AC1: tools=%v 缺声明工具名 %q", tools, want)
			}
		}
		for _, want := range []string{"/dev/fs", "/dev/shell"} {
			if !slices.Contains(devices, want) {
				t.Errorf("AC1: devices=%v 缺设备根 %q（工具名须反查为设备根）", devices, want)
			}
		}
		if slices.Contains(devices, "Read") {
			t.Errorf("AC1: devices=%v 含工具名 Read（devices 须是设备根而非工具名）", devices)
		}
	})

	t.Run("intent_tools_resolve_to_device_root_not_subpath", func(t *testing.T) {
		// AC8.3 设备根反查坑：IntentDecompose 反查到 /dev/intent，绝非 /dev/intent/decompose。
		_, devices := k.normalizeDeclaredAllowedTools(
			[]string{"IntentDecompose", "IntentConfirm", "IntentExecute", "IntentStatus"})
		if !slices.Contains(devices, "/dev/intent") {
			t.Errorf("AC8.3: devices=%v 缺设备根 /dev/intent", devices)
		}
		if slices.Contains(devices, "/dev/intent/decompose") {
			t.Errorf("AC8.3 设备根反查坑: devices=%v 含含-subpath 路径 /dev/intent/decompose"+
				"（必须反查到设备根 /dev/intent，否则 stripOrchestrationDevices 失配）", devices)
		}
	})

	t.Run("device_path_passthrough_backward_compat", func(t *testing.T) {
		// AC8.2 向后兼容：声明设备路径直通 + 展开（与 54.1 expandDevicesToTools 一致）。
		tools, devices := k.normalizeDeclaredAllowedTools([]string{"/dev/fs"})
		if !slices.Equal(devices, []string{"/dev/fs"}) {
			t.Errorf("AC8.2: devices=%v, want [/dev/fs]（设备路径直通）", devices)
		}
		for _, want := range fsToolNames {
			if !slices.Contains(tools, want) {
				t.Errorf("AC8.2: tools=%v 缺 /dev/fs 工具 %q（设备路径须展开）", tools, want)
			}
		}
	})

	t.Run("empty_input_yields_nil", func(t *testing.T) {
		tools, devices := k.normalizeDeclaredAllowedTools(nil)
		if len(tools) != 0 || len(devices) != 0 {
			t.Errorf("空输入应得 (nil,nil)，got tools=%v devices=%v", tools, devices)
		}
	})

	t.Run("unknown_value_skipped", func(t *testing.T) {
		// 未知值（既非设备路径又非已知工具名）lenient 跳过。
		tools, devices := k.normalizeDeclaredAllowedTools([]string{"Bogus"})
		if slices.Contains(tools, "Bogus") {
			t.Errorf("未知值 Bogus 不应进 tools=%v", tools)
		}
		if len(devices) != 0 {
			t.Errorf("未知值不应产出设备根，got devices=%v", devices)
		}
	})
}

// 54.5-UNIT-002 [RED] AC8.1 等价性：设备全集工具名声明归一化的 tools，与旧设备路径声明经
// expandDevicesToTools 的结果集合相等（逐位等价 → 零行为变更）。
func TestATDD_54_5_020_NormalizeEquivalenceWithLegacyDeviceDecl(t *testing.T) {
	k := newNormalizeKernel(t)
	reg := k.deviceRegistry()

	gotTools, _ := k.normalizeDeclaredAllowedTools([]string{"Read", "Write", "Edit", "Glob", "Grep", "Bash"})
	legacyTools := expandDevicesToTools(reg, []string{"/dev/fs", "/dev/shell"})

	gotSorted := slices.Clone(gotTools)
	legacySorted := slices.Clone(legacyTools)
	slices.Sort(gotSorted)
	slices.Sort(legacySorted)
	if !slices.Equal(gotSorted, legacySorted) {
		t.Errorf("AC8.1 等价性破坏: 工具名声明归一化 tools=%v, 旧设备声明展开 tools=%v（须逐位等价 = 零行为变更）",
			gotSorted, legacySorted)
	}
}

// ───────────────── Layer B：spawn 主路径归一化（AC1 / AC2 / AC8.4）─────────────────

// 54.5-INT-001 [RED] AC1（最高风险核心）：agent 声明设备全集工具名，顶层 spawn（无父约束）后
// proc.AllowedTools 精确含声明工具名、proc.AllowedDevices 为设备根、buildToolDefs 非空、Read 放行。
//
// RED：spawn 未接线归一化 → 声明工具名污染 AllowedDevices、AllowedTools=nil（见 story Dev Notes 推演）。
func TestATDD_54_5_100_TopLevelSpawn_ToolNameDecl_Normalized(t *testing.T) {
	k := newNormalizeKernel(t)
	agent := agentWithAllowedTools("Read Write Edit Glob Grep Bash") // 设备全集工具名（54.5 后 code-analysis 形态）

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}

	// proc.AllowedTools 精确含声明的 6 个工具名。
	for _, want := range []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash"} {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC1: proc.AllowedTools=%v 缺声明工具 %q", proc.AllowedTools, want)
		}
	}
	// proc.AllowedDevices 是设备根集合（非工具名、非含 subpath）。
	for _, want := range []string{"/dev/fs", "/dev/shell"} {
		if !slices.Contains(proc.AllowedDevices, want) {
			t.Errorf("AC1: proc.AllowedDevices=%v 缺设备根 %q", proc.AllowedDevices, want)
		}
	}
	if slices.Contains(proc.AllowedDevices, "Read") {
		t.Errorf("AC1: proc.AllowedDevices=%v 含工具名 Read（须为设备根）", proc.AllowedDevices)
	}
	// 呈现层非空：buildToolDefs(AllowedDevices) 给 LLM fs+shell 工具。
	defs, _ := buildToolDefs(k.deviceRegistry(), proc.AllowedDevices)
	if len(defs) == 0 {
		t.Error("AC1: buildToolDefs(proc.AllowedDevices) 为空（LLM 工具列表不应为空）")
	}
	// 工具级放行：Read 在 AllowedTools 内不得 permission denied（缺 path 报 missing-path 可接受）。
	_, readErr := k.executeVFSTool(proc,
		llmToolCall{Name: "Read", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/fs", FSOperation: "Read"})
	if readErr != nil && strings.Contains(readErr.Error(), "permission denied") {
		t.Errorf("AC1: Read 应放行，却被权限拒: %v", readErr)
	}
}

// 54.5-INT-002 [RED] AC2：父约束（opts.AllowedDevices=[/dev/fs]）+ agent 声明工具名时，
// 归一化后设备根与父约束求交集，不报 "no overlap" 假错；AllowedTools 收窄为 /dev/fs 工具集。
//
// RED：未归一化 → intersectDevices([/dev/fs], [Read,...]) = 空 → spawn.go:283 报 no overlap。
func TestATDD_54_5_110_ParentConstrainedSpawn_ToolNameDecl_Intersects(t *testing.T) {
	k := newNormalizeKernel(t)
	agent := agentWithAllowedTools("Read Write Edit Glob Grep Bash") // agent 想要 fs+shell

	pid, err := k.Spawn("child", agent, SpawnOpts{
		AllowedDevices: []string{"/dev/fs"}, // 父仅 /dev/fs
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("AC2: spawn 不应因「工具名 vs 设备路径」零交集报假错: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	if !slices.Equal(proc.AllowedDevices, []string{"/dev/fs"}) {
		t.Errorf("AC2: proc.AllowedDevices=%v, want [/dev/fs]（设备根交集）", proc.AllowedDevices)
	}
	// AllowedTools 收窄为 /dev/fs 工具集，不含 /dev/shell 的 Bash。
	if slices.Contains(proc.AllowedTools, "Bash") {
		t.Errorf("AC2: proc.AllowedTools=%v 含 Bash，但父仅 /dev/fs（应收窄）", proc.AllowedTools)
	}
	for _, want := range fsToolNames {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC2: proc.AllowedTools=%v 缺 /dev/fs 工具 %q", proc.AllowedTools, want)
		}
	}
}

// 54.5-INT-004 GREEN 护栏 AC8.4：归一化不破坏 54.1 的子进程工具集 ⊆ 父
// （opts.AllowedTools 工具级父约束的交集收窄）。当前即绿，实时拦截——dev 归一化接线时
// 勿破坏工具级父约束收窄。
//
// dev 前：opts.AllowedTools=[Read] 经 spawn.go 后置派生 `case len(opts.AllowedTools)>0`
// 直通 → proc.AllowedTools=[Read]。dev 后：agent 全集经后置 opts.AllowedTools 交集
// 收窄 → 仍 == [Read]。两态皆满足子⊆父。
//
// 注：本用例非 RED——ATDD RED 验证（移除 skip 跑）显示它 dev 前已 PASS（opts 直通路径），
// 故定性为 GREEN 护栏不 skip（[[atdd-code-story-red-mechanism-preference]] green-guard 不 skip）。
func TestATDD_54_5_130_NormalizedSpawn_ChildToolsSubsetOfParent(t *testing.T) {
	k := newNormalizeKernel(t)
	agent := agentWithAllowedTools("Read Write Edit Glob Grep Bash")

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
	if slices.Contains(proc.AllowedTools, "Write") || slices.Contains(proc.AllowedTools, "Bash") {
		t.Errorf("AC8.4: proc.AllowedTools=%v 超出父 opts.AllowedTools=[Read]（子不得超父）", proc.AllowedTools)
	}
	if !slices.Contains(proc.AllowedTools, "Read") {
		t.Errorf("AC8.4: proc.AllowedTools=%v 应含 Read（父约束内）", proc.AllowedTools)
	}
}

// ───────────────── Layer C：specialize 路径归一化（AC3）─────────────────

// 54.5-INT-003 [RED] AC3：specialize 加载声明语义工具名（Bash）的 skill 后，
// proc.AllowedTools 追加 Bash、proc.AllowedDevices 追加设备根 /dev/shell（非工具名污染）。
//
// RED：specialize 未接线归一化（tool_exec.go:1000 直接 append 声明值 "Bash" 到 AllowedDevices
// + expandDevicesToTools(["Bash"])=nil）→ AllowedTools 不含 Bash、AllowedDevices 含 "Bash" 污染。
func TestATDD_54_5_120_Specialize_ToolNameDecl_Normalized(t *testing.T) {
	k := newNormalizeKernel(t)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "Bash"}, // using-rnix 形态
			Body:     "skill body for " + name,
			Dir:      "/tmp/skills/" + name,
		}, nil
	})

	cid, _ := k.ctxMgr.CtxAlloc(16)
	proc := NewProcess(0, "specialize-toolname", nil)
	proc.CtxID = cid
	proc.toolMap = map[string]toolMapping{"Skill": {Type: "meta", Action: ActionSpecialize}}

	resp := llmResponse{Content: "specializing"}
	tc := llmToolCall{ID: "sp-1", Name: "Skill", Input: map[string]any{"skill": "shell-skill"}}
	var consec errFingerprintCounter
	prompt := &rnixctx.PromptResult{}
	if !k.executeMetaAction(proc, tc, toolMapping{Type: "meta", Action: ActionSpecialize}, 1, time.Now(), &consec, map[string]bool{}, prompt, "", &resp) {
		t.Fatal("expected true from successful specialize")
	}

	proc.mu.Lock()
	gotTools := slices.Clone(proc.AllowedTools)
	gotDevs := slices.Clone(proc.AllowedDevices)
	proc.mu.Unlock()

	if !slices.Contains(gotTools, "Bash") {
		t.Errorf("AC3: specialize 后 proc.AllowedTools=%v 缺 Bash", gotTools)
	}
	if !slices.Contains(gotDevs, "/dev/shell") {
		t.Errorf("AC3: specialize 后 proc.AllowedDevices=%v 缺设备根 /dev/shell", gotDevs)
	}
	if slices.Contains(gotDevs, "Bash") {
		t.Errorf("AC3: proc.AllowedDevices=%v 含工具名 Bash（应反查为设备根 /dev/shell，非污染）", gotDevs)
	}
}
