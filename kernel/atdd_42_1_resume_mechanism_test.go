package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.1: Resume 机制层 — 验收测试
//
// Tests cover all 7 ACs from the story spec across 9 unit + 3 integration scenarios.
// Helpers (setupResumeKernel, writeTestCheckpoint, cleanupResumedProc) are shared
// with the 30-4 series in resume_test.go; 42-1-specific helpers live in
// atdd_42_1_helpers_test.go.
// =============================================================================

// --- 42.1-UNIT-001: Dead 进程通过 ResumeFromHistory 恢复 (AC#1) ---

func TestATDD_42_1_001_DeadProcess_ResumeFromHistory(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "dead-resume-0000-0000-000000000001"

	writeTestStepsAndMeta(t, baseDir, uuid, 10, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(50), UUID: uuid,
		State: types.StateDead, Intent: "dead process intent",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume Dead process failed: %v", err)
	}
	if result.UUID != uuid {
		t.Errorf("UUID = %q, want %q", result.UUID, uuid)
	}
	if result.PID == 0 {
		t.Error("PID should be non-zero")
	}
	if result.ResumedFromStep != 11 {
		t.Errorf("ResumedFromStep = %d, want 11", result.ResumedFromStep)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	// Poll for state transition rather than blanket sleep (resilient on busy CI).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		s := proc.GetState()
		if s == types.StateRunning || s == types.StateZombie {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s := proc.GetState(); s != types.StateRunning && s != types.StateZombie {
		t.Errorf("state = %s, want Running or Zombie", s)
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.1-UNIT-002: Zombie 进程通过 ResumeFromHistory 恢复 (AC#2) ---

func TestATDD_42_1_002_ZombieProcess_ResumeFromHistory(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "zombie-resume-0000-0000-000000000001"
	writeTestStepsAndMeta(t, baseDir, uuid, 25, "complete")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(60), UUID: uuid,
		State: types.StateZombie, Intent: "zombie process intent",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume Zombie process failed: %v", err)
	}
	if result.UUID != uuid {
		t.Errorf("UUID = %q, want %q", result.UUID, uuid)
	}
	if result.ResumedFromStep != 26 {
		t.Errorf("ResumedFromStep = %d, want 26", result.ResumedFromStep)
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.1-UNIT-003: context_full 退出后 resume 触发 Compact (AC#3) ---

func TestATDD_42_1_003_ContextFull_ResumeTriggersCompact(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "ctxfull-resume-0000-0000-000000000001"
	writeTestStepsAndMeta(t, baseDir, uuid, 50, "context_full")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(70), UUID: uuid,
		State: types.StateDead, Intent: "context full process",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume context_full process failed: %v", err)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}

	// AC#3 verification: Compact must be triggered for context_full resume.
	// Poll DebugChan for Compact event with trigger "context_full_resume" up to 1s.
	// Without this assertion, the Compact path could be silently broken (the
	// original UNIT-003 only checked state, masking the isContextFull field bug).
	deadline := time.Now().Add(1 * time.Second)
	foundCompact := false
	for time.Now().Before(deadline) && !foundCompact {
		select {
		case evt := <-proc.DebugChan:
			if evt.Syscall == "Compact" {
				if trigger, _ := evt.Args["trigger"].(string); trigger == "context_full_resume" {
					foundCompact = true
				}
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !foundCompact {
		t.Errorf("AC#3 violated: no Compact event with trigger=context_full_resume observed within 1s — isContextFull detection or Compact dispatch is broken")
	}

	if s := proc.GetState(); s != types.StateRunning && s != types.StateZombie {
		t.Errorf("state = %s, want Running or Zombie", s)
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.1-UNIT-004: --fork 生成新 UUID + origin_uuid 记录 (AC#4) ---

func TestATDD_42_1_004_ForkMode_NewUUID_OriginTracked(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	originalUUID := "fork-origin-0000-0000-000000000001"
	writeTestStepsAndMeta(t, baseDir, originalUUID, 15, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(80), UUID: originalUUID,
		State: types.StateDead, Intent: "fork origin process",
	})

	result, err := k.ResumeWithOpts(originalUUID, ResumeOpts{Fork: true})
	if err != nil {
		t.Fatalf("Resume with fork failed: %v", err)
	}
	if result.UUID == originalUUID {
		t.Errorf("Fork should generate new UUID, got original %q", originalUUID)
	}
	if result.UUID == "" {
		t.Error("Fork UUID should not be empty")
	}

	newStepsDir := filepath.Join(baseDir, "data", "steps", result.UUID)
	infoPath := filepath.Join(newStepsDir, "proc-info.json")

	// AC#4 verification: origin_uuid must be set in memory on the new process.
	// proc-info.json is only written at reap time, so disk verification is
	// best-effort here — but the in-memory field MUST be correct unconditionally
	// (the original test silently passed when readFile failed, masking bugs).
	newProc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("forked process %d not found in procTable", result.PID)
	}
	if newProc.OriginUUID != originalUUID {
		t.Errorf("proc.OriginUUID = %q, want %q (AC#4)", newProc.OriginUUID, originalUUID)
	}

	// Best-effort disk verification (only if proc-info.json was written).
	if infoData, readErr := os.ReadFile(infoPath); readErr == nil {
		var info struct {
			OriginUUID string `json:"origin_uuid"`
		}
		if err := json.Unmarshal(infoData, &info); err != nil {
			t.Fatalf("parse proc-info.json: %v", err)
		}
		if info.OriginUUID != originalUUID {
			t.Errorf("disk origin_uuid = %q, want %q", info.OriginUUID, originalUUID)
		}
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.1-UNIT-005: Suspended 进程走原 30-4 路径 (AC#5, 回归保护) ---

func TestATDD_42_1_005_Suspended_OriginalPath(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "suspended-compat-0000-0000-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 20)

	oldProc := NewProcess(0, "suspended process", nil)
	oldProc.UUID = uuid
	_ = oldProc.Start()
	_ = oldProc.Suspend()
	k.AddProcess(oldProc)

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume Suspended failed: %v", err)
	}
	if result.UUID != uuid {
		t.Errorf("UUID = %q, want %q", result.UUID, uuid)
	}
	if result.ResumedFromStep != 21 {
		t.Errorf("ResumedFromStep = %d, want 21", result.ResumedFromStep)
	}
	cleanupResumedProc(t, k, result.PID)
}

// --- 42.1-UNIT-006: 数据不存在返回 ErrNotFound (AC#6) ---

func TestATDD_42_1_006_DataNotFound_ReturnsError(t *testing.T) {
	k, _ := setupResumeKernel(t)
	uuid := "nodata-resume-0000-0000-000000000001"

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err == nil {
		t.Fatal("expected error for non-existent data")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrNotFound)
	}
}

// --- 42.1-UNIT-007: steps.jsonl 损坏返回 ErrInternal (AC#6) ---

func TestATDD_42_1_007_CorruptedSteps_ReturnsError(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "corrupt-steps-0000-0000-000000000001"

	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), []byte("{invalid\n"), 0o600)
	_ = os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), []byte("{invalid"), 0o600)

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err == nil {
		t.Fatal("expected error for corrupted steps data")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInternal {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrInternal)
	}
}

// --- 42.1-UNIT-008: Running 进程拒绝 resume (回归保护) ---

func TestATDD_42_1_008_RunningProcess_Rejected(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "running-reject-0000-0000-000000000001"
	writeTestStepsAndMeta(t, baseDir, uuid, 5, "running")

	proc := NewProcess(0, "running process", nil)
	proc.UUID = uuid
	_ = proc.Start()
	k.AddProcess(proc)

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err == nil {
		t.Fatal("expected error when resuming Running process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrInvalid)
	}
}

// --- 42.1-UNIT-009: Fork=false 继承原 UUID 并清理 procHistory ---

func TestATDD_42_1_009_NoFork_InheritsUUID_ClearsHistory(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "inherit-uuid-0000-0000-000000000001"
	writeTestStepsAndMeta(t, baseDir, uuid, 8, "killed")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(90), UUID: uuid,
		State: types.StateDead, Intent: "inherit test",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	if result.UUID != uuid {
		t.Errorf("UUID = %q, want original %q", result.UUID, uuid)
	}
	if k.procHistory.FindByUUID(uuid) != nil {
		t.Error("procHistory should be cleared after resume")
	}
	cleanupResumedProc(t, k, result.PID)
}

