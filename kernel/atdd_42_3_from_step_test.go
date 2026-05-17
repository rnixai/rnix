package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.3: 可观测层 — `--from-step` 截断恢复 & 越界拒绝（AC#1, AC#2）
//
// Covers 8 unit scenarios:
//   - UNIT-001: FromStep=30 加载前 30 步并写入 OriginUUID/ResumedFromStep
//   - UNIT-002: parseStepsJSONL(path, maxStep=30) 仅累计 step<=30 的记录
//   - UNIT-003: parseStepsJSONL(path, maxStep=0) 等价旧行为
//   - UNIT-004: proc-info.json 持久化 resumed_from_step / origin_uuid
//   - UNIT-005: FromStep>totalSteps 返回 ErrInvalid
//   - UNIT-006: FromStep<0 返回 ErrInvalid
//   - UNIT-007: FromStep=0 退化为默认（不截断）
//   - UNIT-008: checkpoint 存在 + FromStep<cpData.LastStep 返回 ErrInvalid
//
// RED PHASE (verifies tests fail before implementation):
//   - ResumeOpts.FromStep 字段尚不存在 → 编译失败
//   - parseStepsJSONL(path, maxStep) 扩展签名尚不存在 → 编译失败
//   - 越界校验 / checkpoint 冲突拒绝路径尚不存在 → 测试断言失败
// =============================================================================

// writeStepsJSONLOnly writes only the steps.jsonl file (no proc-info/process-meta)
// for parseStepsJSONL unit tests.
func writeStepsJSONLOnly(t *testing.T, baseDir, uuid string, totalSteps int) {
	t.Helper()
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var content []byte
	for i := 1; i <= totalSteps; i++ {
		record := map[string]any{
			"step":   i,
			"action": "tool_call",
			"messages": []map[string]string{
				{"role": "user", "content": fmt.Sprintf("step %d input", i)},
				{"role": "assistant", "content": fmt.Sprintf("step %d output", i)},
			},
		}
		line, _ := json.Marshal(record)
		content = append(content, line...)
		content = append(content, '\n')
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), content, 0o600); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
}

// --- 42.3-UNIT-001: FromStep=30 截断恢复 + Origin/ResumedFromStep 字段 (AC#1) ---

func TestATDD_42_3_001_FromStep_TruncatedResume_WritesOriginAndStep(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	originUUID := "fromstep-50-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeTestStepsAndMeta(t, baseDir, originUUID, 50, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(100), UUID: originUUID,
		State: types.StateDead, Intent: "from-step truncated test",
	})

	result, err := k.ResumeWithOpts(originUUID, ResumeOpts{Fork: true, FromStep: 30})
	if err != nil {
		t.Fatalf("ResumeWithOpts(FromStep=30) failed: %v", err)
	}

	// Fork should produce a new UUID.
	if result.UUID == originUUID {
		t.Errorf("UUID = %q, want new fork UUID (Fork=true)", result.UUID)
	}
	// ResumedFromStep = FromStep + 1 = 31.
	if result.ResumedFromStep != 31 {
		t.Errorf("ResumedFromStep = %d, want 31 (FromStep=30 + 1)", result.ResumedFromStep)
	}

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("forked process not in procTable")
	}
	if proc.OriginUUID != originUUID {
		t.Errorf("proc.OriginUUID = %q, want %q", proc.OriginUUID, originUUID)
	}
	if proc.ResumedFromStep != 31 {
		t.Errorf("proc.ResumedFromStep = %d, want 31", proc.ResumedFromStep)
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.3-UNIT-002: parseStepsJSONL(path, maxStep) 截断行为 (AC#1) ---

func TestATDD_42_3_002_ParseStepsJSONL_MaxStepTruncation(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "parse-maxstep-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeStepsJSONLOnly(t, baseDir, uuid, 50)

	stepsPath := filepath.Join(baseDir, "data", "steps", uuid, "steps.jsonl")

	// Extended signature: parseStepsJSONL(path string, maxStep int) (lastStep int, messages []Message, totalSteps int, err error)
	// NOTE: 当前签名仅返回 3 值；本测试在签名扩展后才能编译。
	lastStep, msgs, totalSteps, err := callParseStepsJSONLForTest(t, k, stepsPath, 30)
	if err != nil {
		t.Fatalf("parseStepsJSONL: %v", err)
	}
	if lastStep != 30 {
		t.Errorf("lastStep = %d, want 30 (truncated at maxStep)", lastStep)
	}
	if totalSteps != 50 {
		t.Errorf("totalSteps = %d, want 50 (full file count)", totalSteps)
	}
	if len(msgs) == 0 {
		t.Error("messages should be non-empty at step 30")
	}
}

// --- 42.3-UNIT-003: parseStepsJSONL maxStep=0 等价旧行为 (AC#1) ---

func TestATDD_42_3_003_ParseStepsJSONL_MaxStepZero_FullHistory(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "parse-zero-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeStepsJSONLOnly(t, baseDir, uuid, 20)

	stepsPath := filepath.Join(baseDir, "data", "steps", uuid, "steps.jsonl")
	lastStep, _, totalSteps, err := callParseStepsJSONLForTest(t, k, stepsPath, 0)
	if err != nil {
		t.Fatalf("parseStepsJSONL: %v", err)
	}
	if lastStep != 20 {
		t.Errorf("lastStep = %d, want 20 (maxStep=0 → full)", lastStep)
	}
	if totalSteps != 20 {
		t.Errorf("totalSteps = %d, want 20", totalSteps)
	}
}

// --- 42.3-UNIT-004: proc-info.json 持久化 origin_uuid + resumed_from_step (AC#1) ---

func TestATDD_42_3_004_ResumedProcInfo_PersistsOriginAndStep(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	originUUID := "persist-origin-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeTestStepsAndMeta(t, baseDir, originUUID, 40, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(200), UUID: originUUID,
		State: types.StateDead, Intent: "persist origin test",
	})

	result, err := k.ResumeWithOpts(originUUID, ResumeOpts{Fork: true, FromStep: 20})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	// Wait for SaveProcInfo to flush (typically synchronous in resume path).
	newProcInfoPath := filepath.Join(baseDir, "data", "steps", result.UUID, "proc-info.json")
	data, err := os.ReadFile(newProcInfoPath)
	if err != nil {
		t.Fatalf("read new proc-info.json: %v", err)
	}
	var disk map[string]any
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("parse proc-info.json: %v", err)
	}
	if got := disk["origin_uuid"]; got != originUUID {
		t.Errorf("disk.origin_uuid = %v, want %q", got, originUUID)
	}
	gotStep, _ := disk["resumed_from_step"].(float64)
	if int(gotStep) != 21 {
		t.Errorf("disk.resumed_from_step = %v, want 21", disk["resumed_from_step"])
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.3-UNIT-005: FromStep>totalSteps 返回 ErrInvalid (AC#2) ---

func TestATDD_42_3_005_FromStep_OutOfRange_ReturnsErrInvalid(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "outrange-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeTestStepsAndMeta(t, baseDir, uuid, 50, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(300), UUID: uuid,
		State: types.StateDead, Intent: "out-of-range test",
	})

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false, FromStep: 999})
	if err == nil {
		t.Fatal("Resume should reject FromStep=999 on 50-step history")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("err type = %T, want *SyscallError", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %s, want ErrInvalid", se.Code)
	}
	if !strings.Contains(err.Error(), "exceeds total steps") {
		t.Errorf("err message = %q, want substring 'exceeds total steps'", err.Error())
	}
	if !strings.Contains(err.Error(), "50") {
		t.Errorf("err message = %q, should mention actual total %d", err.Error(), 50)
	}
}

// --- 42.3-UNIT-006: FromStep<0 返回 ErrInvalid (AC#2) ---

func TestATDD_42_3_006_FromStep_Negative_ReturnsErrInvalid(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "neg-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeTestStepsAndMeta(t, baseDir, uuid, 10, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(310), UUID: uuid,
		State: types.StateDead, Intent: "neg test",
	})

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false, FromStep: -1})
	if err == nil {
		t.Fatal("Resume should reject negative FromStep")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("err type = %T, want *SyscallError", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %s, want ErrInvalid", se.Code)
	}
}

// --- 42.3-UNIT-007: FromStep=0 退化为默认 (AC#2) ---

func TestATDD_42_3_007_FromStep_Zero_DefaultsToLastStepPlusOne(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "zero-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeTestStepsAndMeta(t, baseDir, uuid, 15, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(320), UUID: uuid,
		State: types.StateDead, Intent: "zero-default test",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false, FromStep: 0})
	if err != nil {
		t.Fatalf("Resume with FromStep=0 failed: %v", err)
	}
	if result.ResumedFromStep != 16 {
		t.Errorf("ResumedFromStep = %d, want 16 (lastStep+1, default)", result.ResumedFromStep)
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.3-UNIT-008: checkpoint 路径 + FromStep<cpData.LastStep 拒绝 (AC#2) ---

func TestATDD_42_3_008_FromStep_CheckpointPath_RejectsTruncation(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "cp-conflict-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	// checkpoint 写入 step 30，proc-info state=suspended（走 checkpoint 路径）
	writeTestCheckpoint(t, baseDir, uuid, 30)
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(330), UUID: uuid,
		State: types.StateSuspended, Intent: "checkpoint+from-step conflict",
	})

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false, FromStep: 10})
	if err == nil {
		t.Fatal("Resume should reject FromStep=10 < checkpoint.LastStep=30")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("err type = %T, want *SyscallError", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %s, want ErrInvalid", se.Code)
	}
	if !strings.Contains(err.Error(), "checkpoint") || !strings.Contains(err.Error(), "history") {
		t.Errorf("err message = %q, want substring 'checkpoint'/'history'", err.Error())
	}
}

// callParseStepsJSONLForTest is a thin wrapper that exposes the parseStepsJSONL
// extended signature for unit tests. GREEN PHASE: directly delegates to
// k.parseStepsJSONL(stepsPath, maxStep) — returns (lastStep, messages,
// totalSteps, err). messages is converted to []any for stub compatibility.
func callParseStepsJSONLForTest(t *testing.T, k *KernelImpl, path string, maxStep int) (int, []any, int, error) {
	t.Helper()
	lastStep, msgs, totalSteps, err := k.parseStepsJSONL(path, maxStep)
	// Convert []rnixctx.Message → []any so test assertions remain decoupled
	// from kernel/context import in the test file (kept for compile parity
	// with the original stub signature).
	out := make([]any, len(msgs))
	for i, m := range msgs {
		out[i] = m
	}
	_ = rnixctx.Message{} // keep import alive even when msgs is empty
	return lastStep, out, totalSteps, err
}
