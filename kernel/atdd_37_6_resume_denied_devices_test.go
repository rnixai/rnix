package kernel

// ATDD Story 37.6 — 持久化并恢复 DeniedDevices（resume 与 AllowedDevices 对称，
// 防递归编排不变量跨 resume 存活）。
//
// 红阶段（TDD RED）。本文件在实现前刻意失败，驱动 dev-story 落地以下改动：
//   Task 1 — checkpoint 链路对称化：
//       checkpoint.go: CheckpointProcState 加 DeniedDevices []string `json:"denied_devices,omitempty"`
//       checkpoint.go: buildCheckpointData 在锁内拷贝 proc.DeniedDevices 填入 ProcState
//       resume.go:481 区: proc.DeniedDevices = append([]string(nil), cp.DeniedDevices...)
//   Task 2 — proc-info（disk）链路对称化：
//       vfs/proc.go: ProcInfo 加 DeniedDevices []string
//       process_history.go: procInfoDisk 加 DeniedDevices `json:"denied_devices,omitempty"`
//         + procInfoToDisk / procInfoFromDisk 对称映射
//       proc→ProcInfo 投影点（proc_query.go:375,451 + process.go:442）对称补字段
//       resume.go:713 区: proc.DeniedDevices = append([]string(nil), diskInfo.DeniedDevices...)
//
// RED 形态（沿用项目 ATDD 范式，Go 不用 test.skip —— 参考 atdd_37_5_*.go 头注释）：
//   - 编译 RED（最强信号）：010/020/030/050 引用尚不存在的字段
//     CheckpointProcState.DeniedDevices / vfs.ProcInfo.DeniedDevices / procInfoDisk.DeniedDevices
//     → 整个 kernel 包测试 build 失败（`unknown field DeniedDevices`）。
//   - 行为 RED：字段补齐（Task 1/2 加结构）但 resume.go:481/:713 未恢复时，
//     030/031 resume 后 proc.DeniedDevices == nil → 端到端断言失败。
//   - green-guard：040/041 守护「不破坏 legacy 反序列化」，050 守护「不改 AllowedDevices 行为」。
//
// 分级 RED→GREEN（精确映射 dev 任务）：
//   Stage 0 (impl 前)：包不编译（缺字段）—— 强 RED。
//   Stage 1 (Task 1/2 加结构+映射)：包编译；010/020/040/041/050 绿；030/031 红（resume 恢复缺失）。
//   Stage 2 (resume.go:481/:713 加恢复行)：全绿。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ───────────────────────────── 测试基础设施 ─────────────────────────────

// deniedTestDevice 是本 story 的决定性死锁设备：deny `/dev/intent` 即 37-5
// 「防递归编排」黑名单的单一真相源。
const deniedTestDevice = "/dev/intent"

// setupDeniedResumeKernel 构造一个注册 parking LLM 的 resume kernel：resumed 进程在
// 首次 LLM Read 处停住、驻留 Running（消除 CLAUDE.md「Known Test Issues」记录的
// resume-reap 竞态——complete 响应会让进程秒退被 reaper 移出 procTable），使我们能
// 稳定断言其恢复后的 DeniedDevices。
//
// t.Cleanup LIFO 顺序保证 releaseAll 在 k.Shutdown 之前执行（否则 Shutdown 的
// wg.Wait 会因 Read 永久阻塞而死锁），且 TempDir 删除在 Shutdown 之后（goroutine
// 已 join，避免 -race 下 RemoveAll 与 events 写入竞态）。
func setupDeniedResumeKernel(t *testing.T) (*KernelImpl, string, *parkingLLMFile) {
	t.Helper()
	park := newParkingLLMFile()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return park, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	_, projBaseDir := TestSetupDataDir(t, k) // 注册 TempDir 清理（最先注册 → 最后执行）
	t.Cleanup(k.Shutdown)                    // 在 releaseAll 之后、TempDir 删除之前执行
	t.Cleanup(park.releaseAll)               // 最后注册 → 最先执行：先放行再 Shutdown
	return k, projBaseDir, park
}

// runIntentDeviceTool 驱动 resumed 进程对 /dev/intent 的工具调用并返回 executeVFSTool
// 的 error（供测试体断言 deny）。纯数据提取，不含断言（test-quality §explicit-assertions
// 要求 expect 留在测试体内）。deny 检查（tool_exec.go:388）先于设备解析，故无需注册
// /dev/intent —— 参考 spawn_recursion_test.go TestSpawnRecursion_DeniedDevices。
func runIntentDeviceTool(k *KernelImpl, proc *Process) error {
	_, err := k.executeVFSTool(proc,
		llmToolCall{Name: "IntentDecompose", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/intent/decompose"})
	return err
}

// ───────────────── Layer A：序列化往返（编译 RED → 行为 RED）─────────────────

// 010 — AC1：buildCheckpointData 填充 DeniedDevices；checkpoint write/read 往返一致。
//
// 编译 RED：CheckpointProcState.DeniedDevices 尚不存在。
// 行为 RED（字段加上后）：buildCheckpointData 未填 DeniedDevices → cp.ProcState.DeniedDevices 为 nil。
func TestATDD_37_6_010_CheckpointPersistsDeniedDevices(t *testing.T) {
	proc := NewProcess(0, "denied-checkpoint-proc", nil)
	proc.AllowedDevices = []string{"/dev/fs"}
	proc.DeniedDevices = []string{deniedTestDevice, "/dev/shell"} // 编排设备 + 父原有 denied

	ctxSnap := json.RawMessage(`{"system_prompt":"x","messages":[],"max_size":64}`)
	cp := buildCheckpointData(proc, 5, ctxSnap, 0)

	// AC1 主断言：buildCheckpointData 必须把 proc.DeniedDevices 填入 ProcState。
	if !slices.Contains(cp.ProcState.DeniedDevices, deniedTestDevice) {
		t.Errorf("AC1: cp.ProcState.DeniedDevices = %v, 必须含 %s（buildCheckpointData 须填充）",
			cp.ProcState.DeniedDevices, deniedTestDevice)
	}
	if !slices.Contains(cp.ProcState.DeniedDevices, "/dev/shell") {
		t.Errorf("AC1: cp.ProcState.DeniedDevices = %v, 必须保留父原有 /dev/shell", cp.ProcState.DeniedDevices)
	}

	// write/read 往返：denied_devices JSON 持久化一致。
	dir := t.TempDir()
	if err := writeCheckpoint(dir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}
	got, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("readCheckpoint: %v", err)
	}
	if !slices.Equal(got.ProcState.DeniedDevices, cp.ProcState.DeniedDevices) {
		t.Errorf("AC1 往返: got.DeniedDevices = %v, want %v", got.ProcState.DeniedDevices, cp.ProcState.DeniedDevices)
	}
	// AC5 对称 green-guard：AllowedDevices 往返不受 denied 增补影响。
	if !slices.Equal(got.ProcState.AllowedDevices, []string{"/dev/fs"}) {
		t.Errorf("AC5: got.AllowedDevices = %v, want [/dev/fs]（AllowedDevices 往返不变）", got.ProcState.AllowedDevices)
	}
}

// 020 — AC2：vfs.ProcInfo.DeniedDevices → procInfoToDisk → JSON denied_devices → procInfoFromDisk 往返。
//
// 编译 RED：vfs.ProcInfo.DeniedDevices / procInfoDisk.DeniedDevices 尚不存在。
// 直接对标 process_history_mcp_mounts_test.go TestProcInfoDisk_MCPMounts_Roundtrip（48.1 mcp_mounts 同构）。
func TestATDD_37_6_020_ProcInfoDiskPersistsDeniedDevices(t *testing.T) {
	original := vfs.ProcInfo{
		PID: 100, UUID: "denied-procinfo-uuid", State: types.StateDead,
		AllowedDevices: []string{"/dev/fs"},
		DeniedDevices:  []string{deniedTestDevice, "/dev/shell"},
	}

	disk := procInfoToDisk(original)
	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// 字段名断言：JSON 必须含 snake_case denied_devices（项目 §输出格式）。
	if !strings.Contains(string(data), `"denied_devices"`) {
		t.Errorf("AC2: 序列化 JSON 缺 snake_case denied_devices 字段: %s", string(data))
	}

	var roundtrip procInfoDisk
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := procInfoFromDisk(roundtrip)
	if !slices.Equal(got.DeniedDevices, original.DeniedDevices) {
		t.Errorf("AC2 往返: got.DeniedDevices = %v, want %v", got.DeniedDevices, original.DeniedDevices)
	}
	// AC5 对称：AllowedDevices 往返不变。
	if !slices.Equal(got.AllowedDevices, original.AllowedDevices) {
		t.Errorf("AC5: got.AllowedDevices = %v, want %v", got.AllowedDevices, original.AllowedDevices)
	}
}

// ──────────── Layer B：两条 resume 路径恢复 + 不变量端到端（行为 RED）────────────

// 030 — AC3：checkpoint 路径 resume（resume.go:481 区）恢复 proc.DeniedDevices，
// 且 /dev/intent 跨 resume 仍被 deny 拦截（防递归编排不变量存活）。
//
// 行为 RED：当前 resume.go:481 区仅 `proc.AllowedDevices = … cp.AllowedDevices …`，
// 未恢复 DeniedDevices → resume 后 proc.DeniedDevices == nil → 两个断言均失败。
func TestATDD_37_6_030_CheckpointResumeRestoresDeniedDevices(t *testing.T) {
	k, baseDir, park := setupDeniedResumeKernel(t)
	uuid := "denied-ckpt-aaaa-bbbb-000000000001"

	stepsDir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir steps dir: %v", err)
	}
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            uuid,
		LastStep:        3,
		Timestamp:       time.Now(),
		ContextSnapshot: json.RawMessage(`{"system_prompt":"denied agent","messages":[{"role":"user","content":"hi"}],"max_size":64}`),
		ProcState: CheckpointProcState{
			PID:            types.PID(99),
			Provider:       "claude",
			Model:          "claude-4",
			AllowedDevices: []string{"/dev/fs"},
			DeniedDevices:  []string{deniedTestDevice}, // 编译 RED：字段尚不存在
			Intent:         "checkpoint denied-devices resume",
			EnvSnapshot:    map[string]string{"HOME": os.Getenv("HOME")},
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume (checkpoint path): %v", err)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed PID %d 不在 procTable", result.PID)
	}

	// AC3 字段断言：DeniedDevices 跨 checkpoint resume 存活。
	if !slices.Contains(proc.DeniedDevices, deniedTestDevice) {
		t.Errorf("AC3 (checkpoint): proc.DeniedDevices = %v, 必须含 %s（resume.go:481 须恢复 DeniedDevices）",
			proc.DeniedDevices, deniedTestDevice)
	}
	// AC3 不变量断言（核心价值）：deny 检查跨 resume 仍拦截 /dev/intent。
	if derr := runIntentDeviceTool(k, proc); derr == nil || !strings.Contains(derr.Error(), "permission denied") {
		t.Errorf("AC3 (checkpoint): resume 后 /dev/intent 必须仍被拦截（防递归编排存活），got err=%v", derr)
	}

	park.releaseAll()
	cleanupResumedProc(t, k, result.PID)
}

// 031 — AC3：disk 路径 resume（resume.go:713 区）恢复 proc.DeniedDevices，
// 且 /dev/intent 跨 resume 仍被 deny 拦截。
//
// 纯行为 RED：用 overwriteProcInfoFields 把 denied_devices 以 raw JSON 注入 proc-info.json
// （不引用 Go 结构体字段，故本用例自身不触发编译 RED）。当前 resume.go:713 区只恢复
// AllowedDevices → resume 后 proc.DeniedDevices == nil → 断言失败。
func TestATDD_37_6_031_DiskResumeRestoresDeniedDevices(t *testing.T) {
	k, baseDir, park := setupDeniedResumeKernel(t)
	uuid := "denied-disk-aaaa-bbbb-000000000002"

	writeTestStepsAndMeta(t, baseDir, uuid, 3, "") // 写 steps.jsonl + meta + proc-info.json（provider=claude）
	overwriteProcInfoFields(t, baseDir, uuid, map[string]any{
		"denied_devices": []string{deniedTestDevice},
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume (disk path): %v", err)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed PID %d 不在 procTable", result.PID)
	}

	if !slices.Contains(proc.DeniedDevices, deniedTestDevice) {
		t.Errorf("AC3 (disk): proc.DeniedDevices = %v, 必须含 %s（resume.go:713 须恢复 DeniedDevices）",
			proc.DeniedDevices, deniedTestDevice)
	}
	if derr := runIntentDeviceTool(k, proc); derr == nil || !strings.Contains(derr.Error(), "permission denied") {
		t.Errorf("AC3 (disk): resume 后 /dev/intent 必须仍被拦截，got err=%v", derr)
	}

	park.releaseAll()
	cleanupResumedProc(t, k, result.PID)
}

// ───────────────── Layer C：向后兼容 green-guard（AC4）─────────────────

// 040 — AC4：旧 checkpoint.json（无 denied_devices 字段）→ readCheckpoint → DeniedDevices == nil，不报错。
//
// green-guard：字段加上后此用例必须仍通过（无字段安全退化为 nil，与 legacy 进程当前行为一致）。
func TestATDD_37_6_040_LegacyCheckpointDeniedDevicesNil(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{
		"version": 1,
		"uuid": "legacy-ckpt-no-denied-0001",
		"last_step": 3,
		"timestamp": "2026-05-01T00:00:00Z",
		"context_snapshot": {"system_prompt":"legacy","messages":[],"max_size":64},
		"proc_state": {
			"pid": 42,
			"provider": "claude",
			"model": "claude-4",
			"allowed_devices": ["/dev/fs", "/dev/shell"],
			"intent": "legacy process pre-37.6",
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
	if cp.ProcState.DeniedDevices != nil {
		t.Errorf("AC4: legacy DeniedDevices = %v, want nil（无字段安全退化）", cp.ProcState.DeniedDevices)
	}
	// sanity：AllowedDevices 仍正常恢复。
	if !slices.Equal(cp.ProcState.AllowedDevices, []string{"/dev/fs", "/dev/shell"}) {
		t.Errorf("AC4: legacy AllowedDevices = %v, want [/dev/fs /dev/shell]", cp.ProcState.AllowedDevices)
	}
}

// 041 — AC4：旧 proc-info.json（无 denied_devices）→ procInfoFromDisk → nil；
// re-marshal 不得注入空 denied_devices:[]（omitempty 严格守护）。
//
// 直接对标 process_history_mcp_mounts_test.go TestProcInfoDisk_MCPMounts_BackwardCompat。
func TestATDD_37_6_041_LegacyProcInfoDeniedDevicesNil(t *testing.T) {
	legacyJSON := []byte(`{
		"pid": 42,
		"uuid": "legacy-no-denied-devices-0042",
		"ppid": 0,
		"state": "dead",
		"intent": "legacy process pre-37.6",
		"tokens_used": 1500,
		"ctx_id": 7,
		"allowed_devices": ["/dev/fs", "/dev/shell"],
		"provider": "claude",
		"model": "claude-4",
		"pipeline_index": 0,
		"pipeline_total": 0,
		"created_at": "2026-05-01T00:00:00Z"
	}`)

	var disk procInfoDisk
	if err := json.Unmarshal(legacyJSON, &disk); err != nil {
		t.Fatalf("AC4: legacy proc-info.json 反序列化失败: %v", err)
	}

	info := procInfoFromDisk(disk)
	if info.DeniedDevices != nil {
		t.Errorf("AC4: legacy info.DeniedDevices = %v, want nil", info.DeniedDevices)
	}
	// sanity：已知字段存活。
	if !slices.Equal(info.AllowedDevices, []string{"/dev/fs", "/dev/shell"}) {
		t.Errorf("AC4: legacy AllowedDevices = %v, want [/dev/fs /dev/shell]", info.AllowedDevices)
	}

	// omitempty 严格守护：re-marshal 不得注入空 denied_devices:[]（dev 忘记 omitempty 会在此明确失败）。
	remarshal, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if strings.Contains(string(remarshal), `"denied_devices":[]`) {
		t.Errorf("AC4: legacy 快照 re-marshal 注入了空 denied_devices:[] —— 字段缺 omitempty: %s", string(remarshal))
	}
}

// ───────────────── Layer D：对称防误伤 green-guard（AC5）─────────────────

// 050 — AC5：DeniedDevices 空值语义 = nil（不强制 nil→[]），与 disk 层 AllowedDevices 直传一致；
// 多元素 denied 与 allowed 共存往返；denied 增补不改 AllowedDevices 既有行为。ATDD 固化所选语义。
//
// 编译 RED：vfs.ProcInfo.DeniedDevices 尚不存在。
func TestATDD_37_6_050_SymmetryAndEmptyNilSemantics(t *testing.T) {
	t.Run("empty_denied_round_trips_to_nil", func(t *testing.T) {
		original := vfs.ProcInfo{
			PID: 7, UUID: "empty-denied", State: types.StateDead,
			AllowedDevices: []string{"/dev/fs"},
			DeniedDevices:  nil, // 空黑名单
		}
		disk := procInfoToDisk(original)
		data, err := json.Marshal(disk)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// omitempty：空 denied 不应出现在 JSON。
		if strings.Contains(string(data), `"denied_devices"`) {
			t.Errorf("AC5: 空 DeniedDevices 不应序列化出 denied_devices 字段（omitempty）: %s", string(data))
		}
		var roundtrip procInfoDisk
		if err := json.Unmarshal(data, &roundtrip); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		got := procInfoFromDisk(roundtrip)
		if got.DeniedDevices != nil {
			t.Errorf("AC5: 空 DeniedDevices 往返后 = %v, want nil（不强制 nil→[]）", got.DeniedDevices)
		}
		// AllowedDevices 往返不受 denied 增补影响。
		if !slices.Equal(got.AllowedDevices, []string{"/dev/fs"}) {
			t.Errorf("AC5: AllowedDevices = %v, want [/dev/fs]（denied 增补不得改动 AllowedDevices）", got.AllowedDevices)
		}
	})

	t.Run("multi_denied_preserves_allowed", func(t *testing.T) {
		original := vfs.ProcInfo{
			PID: 8, UUID: "multi-denied", State: types.StateDead,
			AllowedDevices: []string{"/dev/fs", "/dev/shell"},
			DeniedDevices:  []string{deniedTestDevice, "/dev/mcp/github"},
		}
		disk := procInfoToDisk(original)
		data, err := json.Marshal(disk)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var roundtrip procInfoDisk
		if err := json.Unmarshal(data, &roundtrip); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		got := procInfoFromDisk(roundtrip)
		if !slices.Equal(got.DeniedDevices, original.DeniedDevices) {
			t.Errorf("AC5: DeniedDevices 往返 = %v, want %v", got.DeniedDevices, original.DeniedDevices)
		}
		if !slices.Equal(got.AllowedDevices, original.AllowedDevices) {
			t.Errorf("AC5: AllowedDevices 往返 = %v, want %v", got.AllowedDevices, original.AllowedDevices)
		}
	})
}
