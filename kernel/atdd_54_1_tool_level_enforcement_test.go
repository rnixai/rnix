package kernel

// ATDD Story 54.1 — 工具级 enforcement 基础设施（proc.AllowedTools）。
//
// 覆盖 AC1（工具级判定）、AC2（设备路径展开 + spawn 派生）、AC4（向后兼容重建）、
// AC5（持久化往返）、AC6（spawn 子 ⊆ 父）、AC7（双层解耦 green-guard）。
// AC3（validateFrontmatter）见 skills/atdd_54_1_validate_frontmatter_test.go；
// AC5 的 IPC wire 见 ipc/atdd_54_1_proc_detail_wire_test.go。
//
// ── RED 形态：骨架 + t.Skip（用户 Decker 2026-06-09 拍板）──────────────────
// 本 story 刻意偏离 kernel 主流的「编译 RED」范式（37-5/37-6/44-5），改用
// 「最小骨架 + t.Skip 脚手架」以保持 ATDD 提交期间 `make all` 全绿：
//   - ATDD 阶段已加最小骨架（零业务逻辑，仅供编译）：
//       kernel/process.go         Process.AllowedTools []string
//       kernel/kernel.go          SpawnOpts.AllowedTools []string
//       kernel/devices.go         expandDevicesToTools(reg, devices) → 占位 return nil
//       kernel/checkpoint.go      CheckpointProcState.AllowedTools
//       kernel/process_history.go procInfoDisk.AllowedTools
//       vfs/proc.go               ProcInfo.AllowedTools
//       ipc/protocol.go           GetProcDetailResponse.AllowedTools
//   - 标 t.Skip 的用例是 RED 脚手架：dev-story 移除 t.Skip + 填充逻辑后由红转绿。
//   - 未标 t.Skip 的用例是 GREEN 护栏（NFR88 双层解耦 / NFR87 兼容）：当前即绿，
//     全程实时拦截回归，dev 改造不得破坏。
//
// 分级 RED→GREEN（精确映射 dev 任务）：
//   Stage 0（本 ATDD 提交）：骨架就位，包编译；green-guard 绿，RED 用例 t.Skip 跳过。
//   Stage 1（dev 移除 t.Skip）：RED 用例转为运行时失败（逻辑未实现）。
//   Stage 2（dev 落地 Task 1-7）：全绿。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// ───────────────────────────── 测试基础设施 ─────────────────────────────

// fsToolNames 是 /dev/fs 设备的语义工具名全集（PascalCase，匹配 toolgen.go:46 的
// FSOperation switch）。设备路径展开（AC2）与向后兼容重建（AC4）都以此为期望值。
var fsToolNames = []string{"Read", "Write", "Edit", "Glob", "Grep"}

// newToolLevelKernel 构造一个注册了「提供语义工具名 ToolDefs」的 /dev/fs + /dev/shell
// 的 kernel（区别于 kernel_test.go 的 registerMockTool —— 后者用 mockToolDriver，每个
// 设备只发射一个「名字=设备路径」的工具，无法覆盖 /dev/fs → [Read Write Edit Glob Grep]
// 的展开）。spawn / executeVFSTool / expandDevicesToTools 三条路径共用。
func newToolLevelKernel(t *testing.T) *KernelImpl {
	t.Helper()
	reg := newToolLevelRegistry()
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	t.Cleanup(k.Shutdown)
	return k
}

// newToolLevelRegistry 注册 LLM(parking 不需要) + /dev/fs(5 工具) + /dev/shell(Bash)。
func newToolLevelRegistry() *vfs.DeviceRegistry {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte(`{"content":"ok","tokens_used":1}`)}, nil
	})
	fsFactory := func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("data")}, nil
	}
	fsDefs := make([]vfs.ToolDef, 0, len(fsToolNames))
	for _, n := range fsToolNames {
		fsDefs = append(fsDefs, vfs.ToolDef{Name: n, Description: n})
	}
	_ = reg.RegisterWithDriver("/dev/fs", fsFactory, &mockToolDescriptor{defs: fsDefs})
	_ = reg.RegisterWithDriver("/dev/shell", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	}, &mockToolDescriptor{defs: []vfs.ToolDef{{Name: "Bash", Description: "Run command"}}})
	return reg
}

// setupToolResumeKernel 构造一个用 parking LLM 的 resume kernel，额外注册提供语义
// 工具名的 /dev/fs（AC4 重建需遍历 registry 展开 /dev/fs → 工具名）。t.Cleanup LIFO
// 顺序与 atdd_37_6 的 setupDeniedResumeKernel 一致：releaseAll → Shutdown → TempDir 删除。
func setupToolResumeKernel(t *testing.T) (*KernelImpl, string, *parkingLLMFile) {
	t.Helper()
	park := newParkingLLMFile()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return park, nil
	})
	fsDefs := make([]vfs.ToolDef, 0, len(fsToolNames))
	for _, n := range fsToolNames {
		fsDefs = append(fsDefs, vfs.ToolDef{Name: n, Description: n})
	}
	_ = reg.RegisterWithDriver("/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("data")}, nil
	}, &mockToolDescriptor{defs: fsDefs})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	_, projBaseDir := TestSetupDataDir(t, k) // 最先注册 → 最后执行（TempDir 删除在 Shutdown 之后）
	t.Cleanup(k.Shutdown)                    // 在 releaseAll 之后、TempDir 删除之前
	t.Cleanup(park.releaseAll)               // 最后注册 → 最先执行：先放行再 Shutdown
	return k, projBaseDir, park
}

// ───────────────── Layer A：expandDevicesToTools 单测（AC2 helper）─────────────────

// 54.1-UNIT-001 [RED] 设备路径 → 工具名展开矩阵。
//
// expandDevicesToTools 骨架返回 nil（devices.go），故 fs_expands 子测试在移除 t.Skip
// 后失败 = RED；dev 实现遍历 DeviceRegistry 取 ToolDef.Name 后转绿。
func TestATDD_54_1_010_ExpandDevicesToTools_Matrix(t *testing.T) {
	reg := newToolLevelRegistry()

	t.Run("fs_expands_to_all_tools", func(t *testing.T) {
		got := expandDevicesToTools(reg, []string{"/dev/fs"})
		for _, want := range fsToolNames {
			if !slices.Contains(got, want) {
				t.Errorf("expandDevicesToTools([/dev/fs]) = %v, 缺工具 %q", got, want)
			}
		}
	})
	t.Run("empty_input_to_empty", func(t *testing.T) {
		if got := expandDevicesToTools(reg, nil); len(got) != 0 {
			t.Errorf("expandDevicesToTools(nil) = %v, want empty", got)
		}
	})
	t.Run("unknown_device_skipped", func(t *testing.T) {
		if got := expandDevicesToTools(reg, []string{"/dev/bogus"}); len(got) != 0 {
			t.Errorf("expandDevicesToTools([/dev/bogus]) = %v, want empty（未知设备静默跳过）", got)
		}
	})
	t.Run("mcp_path_yields_no_base_tools", func(t *testing.T) {
		got := expandDevicesToTools(reg, []string{"/mnt/mcp/100-server"})
		if slices.Contains(got, "Read") {
			t.Errorf("expandDevicesToTools(MCP 路径) = %v, 不应产出 base 工具名（MCP 工具名动态）", got)
		}
	})
}

// ───────────────── Layer B：executeVFSTool 工具级判定（AC1 核心）─────────────────

// 54.1-INT-001 [RED] proc.AllowedTools=[Read] → Read 放行 / Write 拒（同属 /dev/fs）。
//
// RED 由 Write 断言驱动：当前 executeVFSTool 只按设备路径判定（tool_exec.go:397），
// /dev/fs 在 AllowedDevices 即对 Read/Write 一律放行 → Write 不会 permission denied →
// 移除 t.Skip 后失败。dev 改为 base 工具按 tc.Name 比对 proc.AllowedTools 后转绿。
// 两个工具调用都故意缺 path，使权限闸放行后在 FSOperation 分支因缺参数提前返回（非
// permission denied），从而不触碰真实文件系统。
func TestATDD_54_1_020_ToolLevelEnforcement_ReadAllowedWriteDenied(t *testing.T) {
	k := newToolLevelKernel(t)
	proc := NewProcess(0, "tool-level-enforce", nil)
	proc.AllowedDevices = []string{"/dev/fs"} // 设备级两者都允许
	proc.AllowedTools = []string{"Read"}      // 工具级仅放行 Read

	// Read：工具级放行——不得是 permission denied（缺 path 会报 missing-path，可接受）。
	_, readErr := k.executeVFSTool(proc,
		llmToolCall{Name: "Read", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/fs", FSOperation: "Read"})
	if readErr != nil && strings.Contains(readErr.Error(), "permission denied") {
		t.Errorf("AC1: Read 在 AllowedTools=[Read] 下应放行，却被权限拒: %v", readErr)
	}

	// Write：工具级拒——即使 Write 与 Read 同属 /dev/fs 设备。
	_, writeErr := k.executeVFSTool(proc,
		llmToolCall{Name: "Write", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/fs", FSOperation: "Write"})
	if writeErr == nil || !strings.Contains(writeErr.Error(), "permission denied") {
		t.Errorf("AC1: Write 在 AllowedTools=[Read] 下必须被工具级拒（同属 /dev/fs 也不放行），got err=%v", writeErr)
	}
}

// ───────────────── Layer C：spawn 链工具级派生（AC2 spawn + AC6）─────────────────

// 54.1-INT-002 [RED] AC2：skill 声明设备路径 /dev/fs → spawn 后 proc.AllowedTools 展开为设备工具集。
//
// RED：当前 spawn 只维护 proc.AllowedDevices（spawn.go:286-290），不派生 AllowedTools →
// proc.AllowedTools == nil → 移除 t.Skip 后失败。
func TestATDD_54_1_030_SpawnExpandsDeviceDeclToAllowedTools(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithAllowedTools("/dev/fs") // 旧式设备路径声明（54.5 前内置 skill 仍如此）

	pid, err := k.Spawn("child", agent, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	for _, want := range fsToolNames {
		if !slices.Contains(proc.AllowedTools, want) {
			t.Errorf("AC2: spawn 后 proc.AllowedTools = %v, 缺 /dev/fs 的工具 %q（设备声明须展开为工具名）",
				proc.AllowedTools, want)
		}
	}
}

// 54.1-INT-003 [RED] AC6：spawn 子进程 AllowedTools ⊆ 父（设备交集收窄同步收窄工具集）。
//
// 父仅 /dev/fs，agent 想要 /dev/fs + /dev/shell → 设备交集 = /dev/fs → 子工具集应仅含
// fs 工具，不含 /dev/shell 的 Bash。RED：当前不派生 AllowedTools → nil → 「应含 Read」失败。
func TestATDD_54_1_031_SpawnChildToolsSubsetOfParent(t *testing.T) {
	k := newToolLevelKernel(t)
	agent := agentWithAllowedTools("/dev/fs /dev/shell") // agent 想要更宽

	pid, err := k.Spawn("child", agent, SpawnOpts{
		AllowedDevices: []string{"/dev/fs"}, // 父仅 /dev/fs
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("spawned process not found")
	}
	if slices.Contains(proc.AllowedTools, "Bash") {
		t.Errorf("AC6: 子 proc.AllowedTools = %v 含 Bash，但父仅 /dev/fs（子不得超父）", proc.AllowedTools)
	}
	if !slices.Contains(proc.AllowedTools, "Read") {
		t.Errorf("AC6: 子 proc.AllowedTools = %v 应含 Read（父 /dev/fs 交集内）", proc.AllowedTools)
	}
}

// 54.1-INT-011 AC6（specialize 追加）：specialize 加载声明 /dev/fs 的 skill 后，
// proc.AllowedTools 展开追加该设备工具名（与 AllowedDevices 同步）。
func TestATDD_54_1_080_SpecializeAppendsAllowedTools(t *testing.T) {
	k := newToolLevelKernel(t)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "skill body for " + name,
			Dir:      "/tmp/skills/" + name,
		}, nil
	})

	cid, _ := k.ctxMgr.CtxAlloc(16)
	proc := NewProcess(0, "specialize-append", nil)
	proc.CtxID = cid
	proc.toolMap = map[string]toolMapping{"Skill": {Type: "meta", Action: ActionSpecialize}}

	resp := llmResponse{Content: "specializing"}
	tc := llmToolCall{ID: "sp-1", Name: "Skill", Input: map[string]any{"skill": "fs-skill"}}
	var consec errFingerprintCounter
	prompt := &rnixctx.PromptResult{}
	if !k.executeMetaAction(proc, tc, toolMapping{Type: "meta", Action: ActionSpecialize}, 1, time.Now(), &consec, map[string]bool{}, prompt, "", &resp) {
		t.Fatal("expected true from successful specialize")
	}

	proc.mu.Lock()
	got := append([]string(nil), proc.AllowedTools...)
	proc.mu.Unlock()
	for _, want := range fsToolNames {
		if !slices.Contains(got, want) {
			t.Errorf("AC6: specialize 后 proc.AllowedTools = %v, 缺 /dev/fs 工具 %q（应与 AllowedDevices 同步展开）", got, want)
		}
	}
}

// 54.1-INT-012 AC6（specialize 回滚）：context-full 触发回滚时，proc.AllowedTools
// 对称移除该 skill 追加的工具（与 AllowedDevices 回滚同步）。
func TestATDD_54_1_081_SpecializeRollbackRemovesAllowedTools(t *testing.T) {
	k := newToolLevelKernel(t)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "skill body content for " + name,
			Dir:      "/tmp/skills/" + name,
		}, nil
	})

	// 4 槽位填 3 + 1 个 assistant → skill body 的 AppendMessage 会因 context full 失败 → 回滚。
	cid, _ := k.ctxMgr.CtxAlloc(4)
	for i := range 3 {
		if err := k.ctxMgr.AppendMessage(cid, rnixctx.RoleUser, fmt.Sprintf("fill-%d", i)); err != nil {
			t.Fatalf("fill: %v", err)
		}
	}
	proc := NewProcess(0, "specialize-rollback", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/claude"
	proc.toolMap = map[string]toolMapping{"Skill": {Type: "meta", Action: ActionSpecialize}}
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := k.ctxMgr.AppendMessage(cid, rnixctx.RoleAssistant, "I will specialize"); err != nil {
		t.Fatalf("pre-fill: %v", err)
	}

	resp := llmResponse{Content: "specializing", ToolCalls: []llmToolCall{{ID: "sp-1", Name: "Skill", Input: map[string]any{"skill": "fs-skill"}}}}
	var consec errFingerprintCounter
	prompt := &rnixctx.PromptResult{}
	if !k.executeMetaAction(proc, resp.ToolCalls[0], toolMapping{Type: "meta", Action: ActionSpecialize}, 1, time.Now(), &consec, map[string]bool{}, prompt, "", &resp) {
		t.Fatal("expected true after specialize rollback")
	}

	proc.mu.Lock()
	got := append([]string(nil), proc.AllowedTools...)
	proc.mu.Unlock()
	if slices.Contains(got, "Read") {
		t.Errorf("AC6: 回滚后 proc.AllowedTools = %v 仍含 Read（应对称移除 skill 追加的工具）", got)
	}
}

// ───────────────── Layer D：持久化往返 + 向后兼容重建（AC5 + AC4）─────────────────

// 54.1-INT-004 [RED] AC5：buildCheckpointData 填充 AllowedTools；checkpoint write/read 往返一致。
//
// RED：当前 buildCheckpointData 不拷贝 proc.AllowedTools → cp.ProcState.AllowedTools == nil。
func TestATDD_54_1_040_CheckpointPersistsAllowedTools(t *testing.T) {
	proc := NewProcess(0, "allowed-tools-checkpoint", nil)
	proc.AllowedDevices = []string{"/dev/fs"}
	proc.AllowedTools = []string{"Read", "Write"}

	ctxSnap := json.RawMessage(`{"system_prompt":"x","messages":[],"max_size":64}`)
	cp := buildCheckpointData(proc, 5, ctxSnap, 0)

	if !slices.Contains(cp.ProcState.AllowedTools, "Read") {
		t.Errorf("AC5: cp.ProcState.AllowedTools = %v, 必须含 Read（buildCheckpointData 须填充）", cp.ProcState.AllowedTools)
	}

	dir := t.TempDir()
	if err := writeCheckpoint(dir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}
	got, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("readCheckpoint: %v", err)
	}
	if !slices.Equal(got.ProcState.AllowedTools, cp.ProcState.AllowedTools) {
		t.Errorf("AC5 往返: got.AllowedTools = %v, want %v", got.ProcState.AllowedTools, cp.ProcState.AllowedTools)
	}
}

// 54.1-INT-005 [RED] AC5：vfs.ProcInfo.AllowedTools → procInfoToDisk → JSON allowed_tools →
// procInfoFromDisk 往返。对标 atdd_37_6 的 020（DeniedDevices 同构）。
//
// RED：当前 procInfoToDisk/FromDisk 不映射 AllowedTools → JSON 无 allowed_tools 字段。
func TestATDD_54_1_041_ProcInfoDiskPersistsAllowedTools(t *testing.T) {
	original := vfs.ProcInfo{
		PID: 100, UUID: "allowed-tools-procinfo", State: types.StateDead,
		AllowedDevices: []string{"/dev/fs"},
		AllowedTools:   []string{"Read", "Write"},
	}

	disk := procInfoToDisk(original)
	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"allowed_tools"`) {
		t.Errorf("AC5: 序列化 JSON 缺 snake_case allowed_tools 字段: %s", string(data))
	}

	var roundtrip procInfoDisk
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := procInfoFromDisk(roundtrip)
	if !slices.Equal(got.AllowedTools, original.AllowedTools) {
		t.Errorf("AC5 往返: got.AllowedTools = %v, want %v", got.AllowedTools, original.AllowedTools)
	}
}

// 54.1-INT-006 [RED] AC4（NFR87 命门）：仅含 AllowedDevices 的 checkpoint，resume 后
// proc.AllowedTools 从 AllowedDevices 展开重建。
//
// RED：当前 resume.go:481 区只恢复 AllowedDevices，不重建 AllowedTools → resume 后 nil。
func TestATDD_54_1_050_CheckpointResumeRebuildsAllowedTools(t *testing.T) {
	k, baseDir, park := setupToolResumeKernel(t)
	uuid := "allowed-tools-rebuild-aaaa-000000000001"

	stepsDir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir steps dir: %v", err)
	}
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            uuid,
		LastStep:        3,
		Timestamp:       time.Now(),
		ContextSnapshot: json.RawMessage(`{"system_prompt":"legacy","messages":[{"role":"user","content":"hi"}],"max_size":64}`),
		ProcState: CheckpointProcState{
			PID:            types.PID(99),
			Provider:       "claude",
			Model:          "claude-4",
			AllowedDevices: []string{"/dev/fs"}, // 旧进程：仅 AllowedDevices，无 AllowedTools
			Intent:         "legacy allowed-devices-only process",
			EnvSnapshot:    map[string]string{"HOME": os.Getenv("HOME")},
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed PID %d 不在 procTable", result.PID)
	}
	if !slices.Contains(proc.AllowedTools, "Read") {
		t.Errorf("AC4: resume 后 proc.AllowedTools = %v, 应从 /dev/fs 展开重建（含 Read，NFR87 命门）", proc.AllowedTools)
	}

	park.releaseAll()
	cleanupResumedProc(t, k, result.PID)
}

// ───────────────── Layer E：green-guard（AC4 兼容 + AC7 双层解耦）─────────────────

// 54.1-INT-007 AC4 green-guard：旧 checkpoint.json（无 allowed_tools）→ readCheckpoint →
// AllowedTools == nil，不报错。骨架就位后即应通过（字段 omitempty + 无字段安全退化）。
func TestATDD_54_1_060_LegacyCheckpointAllowedToolsNil_GreenGuard(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{
		"version": 1,
		"uuid": "legacy-ckpt-no-tools-0001",
		"last_step": 3,
		"timestamp": "2026-05-01T00:00:00Z",
		"context_snapshot": {"system_prompt":"legacy","messages":[],"max_size":64},
		"proc_state": {
			"pid": 42,
			"provider": "claude",
			"model": "claude-4",
			"allowed_devices": ["/dev/fs"],
			"intent": "legacy process pre-54.1",
			"env_snapshot": {}
		}
	}`)
	if err := os.WriteFile(filepath.Join(dir, "checkpoint.json"), legacy, 0o600); err != nil {
		t.Fatalf("write legacy checkpoint: %v", err)
	}

	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("AC4: legacy checkpoint.json 应能反序列化, got: %v", err)
	}
	if cp.ProcState.AllowedTools != nil {
		t.Errorf("AC4: legacy AllowedTools = %v, want nil（无字段安全退化）", cp.ProcState.AllowedTools)
	}
	if !slices.Equal(cp.ProcState.AllowedDevices, []string{"/dev/fs"}) {
		t.Errorf("AC4 sanity: legacy AllowedDevices = %v, want [/dev/fs]", cp.ProcState.AllowedDevices)
	}
}

// 54.1-INT-008 AC4 green-guard：旧 proc-info.json（无 allowed_tools）→ procInfoFromDisk →
// nil；re-marshal 不得注入空 allowed_tools:[]（omitempty 严格守护）。对标 atdd_37_6 的 041。
func TestATDD_54_1_061_LegacyProcInfoAllowedToolsNil_GreenGuard(t *testing.T) {
	legacyJSON := []byte(`{
		"pid": 42,
		"uuid": "legacy-no-allowed-tools-0042",
		"ppid": 0,
		"state": "dead",
		"intent": "legacy process pre-54.1",
		"ctx_id": 7,
		"allowed_devices": ["/dev/fs"],
		"provider": "claude",
		"model": "claude-4",
		"created_at": "2026-05-01T00:00:00Z"
	}`)

	var disk procInfoDisk
	if err := json.Unmarshal(legacyJSON, &disk); err != nil {
		t.Fatalf("AC4: legacy proc-info.json 反序列化失败: %v", err)
	}
	info := procInfoFromDisk(disk)
	if info.AllowedTools != nil {
		t.Errorf("AC4: legacy info.AllowedTools = %v, want nil", info.AllowedTools)
	}

	remarshal, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if strings.Contains(string(remarshal), `"allowed_tools":[]`) {
		t.Errorf("AC4: legacy 快照 re-marshal 注入了空 allowed_tools:[] —— 字段缺 omitempty: %s", string(remarshal))
	}
}

// 54.1-INT-009 AC7 green-guard（NFR88）：MCP 工具仍按设备路径 additive-permit 判定，
// 不受 base 工具白名单影响。进程仅持 MCP mount，其 MCP 工具调用不得被 permission denied。
func TestATDD_54_1_070_MCPAdditivePermitByPath_GreenGuard(t *testing.T) {
	k := newToolLevelKernel(t)
	proc := NewProcess(0, "mcp-additive", nil)
	proc.AllowedDevices = []string{"/mnt/mcp/100-server"} // 仅 MCP mount

	_, err := k.executeVFSTool(proc,
		llmToolCall{Name: "some_mcp_tool", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/mnt/mcp/100-server/tools/foo"})
	// 该 MCP 设备未注册 → 可能因 device-not-found 失败，但绝不能是 permission denied。
	if err != nil && strings.Contains(err.Error(), "permission denied") {
		t.Errorf("NFR88: MCP 工具在其 mount 的 AllowedDevices 下应放行（additive permit），got %v", err)
	}
}

// 54.1-INT-010 AC7 green-guard（NFR88）：DeniedDevices 黑名单先行拦截照旧（intent 子进程
// 递归阻断）。deny 检查（tool_exec.go:388）先于设备解析，无需注册 /dev/intent。
func TestATDD_54_1_071_DeniedDevicesStillBlocks_GreenGuard(t *testing.T) {
	k := newToolLevelKernel(t)
	proc := NewProcess(0, "denied-guard", nil)
	proc.DeniedDevices = []string{"/dev/intent"}

	_, err := k.executeVFSTool(proc,
		llmToolCall{Name: "IntentDecompose", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/intent/decompose"})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("NFR88: DeniedDevices 必须先行拦截 /dev/intent（防递归编排），got err=%v", err)
	}
}
