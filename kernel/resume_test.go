package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// setupResumeKernel creates a kernel with stepDataDir set and a mock LLM device
// that returns a "complete" action so resumed processes terminate immediately.
func setupResumeKernel(t *testing.T) (*KernelImpl, string) {
	t.Helper()
	completeResp := makeCompleteResponse("resumed and done", 10)
	llmFile := &mockLLMFile{readData: completeResp}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	baseDir := t.TempDir()
	k.SetStepDataDir(baseDir)

	return k, baseDir
}

// writeTestCheckpoint writes a valid checkpoint to disk for the given UUID and step.
func writeTestCheckpoint(t *testing.T, baseDir, uuid string, step int) {
	t.Helper()
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir steps dir: %v", err)
	}
	ctxSnap := json.RawMessage(`{"system_prompt":"test prompt","messages":[{"role":"user","content":"hello"}],"max_size":64}`)
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            uuid,
		LastStep:        step,
		Timestamp:       time.Now(),
		ContextSnapshot: ctxSnap,
		ProcState: CheckpointProcState{
			PID:            types.PID(99),
			Provider:       "claude",
			Model:          "claude-4",
			Skills:         []string{"code-analyst"},
			AllowedDevices: []string{"/dev/fs", "/dev/shell"},
			Intent:         "resume test intent",
			MaxSteps:       0,
			UsedTokens:     2000,
			EnvSnapshot:    map[string]string{"HOME": os.Getenv("HOME")},
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}
}

// cleanupResumedProc waits for a resumed process to finish. The mock LLM
// returns a "complete" response, so the process will terminate on its own.
func cleanupResumedProc(t *testing.T, k *KernelImpl, pid types.PID) {
	t.Helper()
	proc, ok := k.GetProcess(pid)
	if !ok {
		return
	}
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Logf("warning: process %d did not terminate within 5s, killing", pid)
		_ = k.Kill(pid, types.SIGKILL)
	}
}

// --- 30.4-UNIT-001: Resume from valid checkpoint restores all fields ---

func TestResume_ValidCheckpoint_RestoresFields(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	writeTestCheckpoint(t, baseDir, uuid, 42)

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	// Verify result
	if result.UUID != uuid {
		t.Errorf("UUID = %q, want %q", result.UUID, uuid)
	}
	if result.ResumedFromStep != 43 {
		t.Errorf("ResumedFromStep = %d, want 43", result.ResumedFromStep)
	}
	if result.PID == 0 {
		t.Error("PID should be non-zero")
	}

	// Verify process in process table
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not found in process table")
	}

	if proc.UUID != uuid {
		t.Errorf("proc.UUID = %q, want %q", proc.UUID, uuid)
	}
	if proc.Intent != "resume test intent" {
		t.Errorf("proc.Intent = %q, want %q", proc.Intent, "resume test intent")
	}
	if proc.Provider != "claude" {
		t.Errorf("proc.Provider = %q, want %q", proc.Provider, "claude")
	}
	if proc.Model != "claude-4" {
		t.Errorf("proc.Model = %q, want %q", proc.Model, "claude-4")
	}
	if proc.MaxSteps != 0 {
		t.Errorf("proc.MaxSteps = %d, want 0", proc.MaxSteps)
	}
	proc.mu.Lock()
	tokens := proc.TokensUsed
	proc.mu.Unlock()
	if tokens != 2000 {
		t.Errorf("proc.TokensUsed = %d, want 2000", tokens)
	}
	if len(proc.AllowedDevices) != 2 {
		t.Errorf("AllowedDevices len = %d, want 2", len(proc.AllowedDevices))
	}

	// Verify process is Running (after goroutine starts)
	// Give it a moment to transition
	time.Sleep(50 * time.Millisecond)
	state := proc.GetState()
	if state != types.StateRunning && state != types.StateZombie {
		t.Errorf("proc.State = %s, want Running or Zombie (if already completed)", state)
	}

	// Clean up: kill the process
	_ = k.Kill(proc.PID, types.SIGKILL)
}

// --- 30.4-UNIT-002: Resume with non-existent checkpoint ---

func TestResume_CheckpointNotFound(t *testing.T) {
	k, _ := setupResumeKernel(t)
	uuid := "nonexist-0000-0000-0000-000000000000"

	_, err := k.Resume(uuid)
	if err == nil {
		t.Fatal("expected error for non-existent checkpoint")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrNotFound)
	}
}

// --- 30.4-UNIT-003: Resume with corrupted checkpoint ---

func TestResume_CheckpointCorrupted(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "corrupt0-0000-0000-0000-000000000000"

	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "checkpoint.json"), []byte("{invalid json"), 0o600); err != nil {
		t.Fatalf("write corrupt checkpoint: %v", err)
	}

	_, err := k.Resume(uuid)
	if err == nil {
		t.Fatal("expected error for corrupted checkpoint")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrInvalid)
	}
}

// --- 30.4-UNIT-004: Resume preserves UUID, assigns new PID ---

func TestResume_NewPID_PreservedUUID(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "preserve-uuid-0000-0000-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 10)

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if result.UUID != uuid {
		t.Errorf("UUID changed: got %q, want %q", result.UUID, uuid)
	}
	// Original PID in checkpoint was 99, new PID should differ
	if result.PID == 99 {
		t.Error("PID should be newly allocated, not the old value 99")
	}
	if result.PID == 0 {
		t.Error("PID should be non-zero")
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- 30.4-UNIT-005: Resume when LLM provider not available ---

func TestResume_ProviderNotAvailable(t *testing.T) {
	// Create kernel without the "claude" device registered
	reg := vfs.NewDeviceRegistry()
	// Register a different provider
	_ = reg.Register("/dev/llm/openai", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockClosableVFSFile{path: "/dev/llm/openai"}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	baseDir := t.TempDir()
	k.SetStepDataDir(baseDir)

	// Set up provider validation that rejects "claude"
	k.SetProviderResolver(
		func() []string { return []string{"openai"} },
		func(name string) bool { return name == "openai" },
	)

	uuid := "provider-fail-0000-0000-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 5) // checkpoint has provider="claude"

	_, err := k.Resume(uuid)
	if err == nil {
		t.Fatal("expected error when LLM provider not available")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrDriver {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrDriver)
	}
}

// --- 30.4-UNIT-006: Resume removes Suspended process from procTable ---

func TestResume_RemovesSuspendedEntry(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "suspended-0000-0000-0000-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 20)

	// Create a Suspended process in the process table with this UUID
	oldProc := NewProcess(0, "old process", nil)
	oldProc.UUID = uuid
	_ = oldProc.Start()
	_ = oldProc.Suspend()
	k.AddProcess(oldProc)
	oldPID := oldProc.PID

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	// Old PID should no longer be in the table
	_, oldExists := k.GetProcess(oldPID)
	if oldExists {
		t.Error("old Suspended process should have been removed from procTable")
	}

	// New process should be registered
	_, newExists := k.GetProcess(result.PID)
	if !newExists {
		t.Error("new resumed process should be in procTable")
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- 30.4-UNIT-007: Resume clears procHistory entry ---

func TestResume_ClearsProcHistory(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "history-0000-0000-0000-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 15)

	// Add entry to procHistory
	k.procHistory.Add(vfs.ProcInfo{
		PID:       types.PID(77),
		UUID:      uuid,
		State:     types.StateDead,
		Intent:    "old dead process",
		CreatedAt: time.Now().Add(-time.Hour),
	})

	if k.procHistory.FindByUUID(uuid) == nil {
		t.Fatal("history entry should exist before resume")
	}

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if k.procHistory.FindByUUID(uuid) != nil {
		t.Error("history entry should have been removed after resume")
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- 30.4-UNIT-008: Resume with empty UUID ---

func TestResume_EmptyUUID(t *testing.T) {
	k, _ := setupResumeKernel(t)

	_, err := k.Resume("")
	if err == nil {
		t.Fatal("expected error for empty UUID")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrInvalid)
	}
}

// --- 30.4-UNIT-009: Resume UUID mismatch ---

func TestResume_UUIDMismatch(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	// Write checkpoint with UUID "aaa" but try to resume with "bbb"
	checkpointUUID := "mismatch-real-0000-0000-000000000001"
	queryUUID := "mismatch-fake-0000-0000-000000000002"

	stepsDir := filepath.Join(baseDir, "data", "steps", queryUUID)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ctxSnap := json.RawMessage(`{"system_prompt":"","messages":[],"max_size":64}`)
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            checkpointUUID, // Different UUID
		LastStep:        5,
		Timestamp:       time.Now(),
		ContextSnapshot: ctxSnap,
		ProcState: CheckpointProcState{
			Provider: "claude",
			Intent:   "test",
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	_, err := k.Resume(queryUUID)
	if err == nil {
		t.Fatal("expected error for UUID mismatch")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrInvalid)
	}
}

// --- 30.4-UNIT-010: Resume from daemon restart (no procTable entry) ---

func TestResume_DaemonRestart_NoProcTableEntry(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "restart-0000-0000-0000-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 30)

	// No process in procTable — simulating daemon restart
	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if result.UUID != uuid {
		t.Errorf("UUID = %q, want %q", result.UUID, uuid)
	}
	if result.ResumedFromStep != 31 {
		t.Errorf("ResumedFromStep = %d, want 31", result.ResumedFromStep)
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- 30.4-UNIT-011: Resume non-Suspended process is rejected ---

func TestResume_RunningProcess_Rejected(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "running-0000-0000-0000-000000000001"
	writeTestCheckpoint(t, baseDir, uuid, 5)

	// Create a Running process with this UUID
	proc := NewProcess(0, "running process", nil)
	proc.UUID = uuid
	_ = proc.Start()
	k.AddProcess(proc)

	_, err := k.Resume(uuid)
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

// --- 30.4-UNIT-012: Resume without stepDataDir configured ---

func TestResume_NoStepDataDir(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	// stepDataDir not set

	_, err := k.Resume("some-uuid-0000-0000-000000000001")
	if err == nil {
		t.Fatal("expected error when stepDataDir not configured")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInternal {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrInternal)
	}
}

// --- 30.4-UNIT-013: ProcessHistory.RemoveByUUID ---

func TestProcessHistory_RemoveByUUID(t *testing.T) {
	h := NewProcessHistory(100)

	h.Add(vfs.ProcInfo{PID: 1, UUID: "aaa", Intent: "first"})
	h.Add(vfs.ProcInfo{PID: 2, UUID: "bbb", Intent: "second"})
	h.Add(vfs.ProcInfo{PID: 3, UUID: "aaa", Intent: "third"}) // duplicate UUID

	if h.Len() != 3 {
		t.Fatalf("Len = %d, want 3", h.Len())
	}

	removed := h.RemoveByUUID("aaa")
	if !removed {
		t.Error("RemoveByUUID should return true when entries removed")
	}
	if h.Len() != 1 {
		t.Fatalf("Len after remove = %d, want 1", h.Len())
	}
	if h.FindByUUID("aaa") != nil {
		t.Error("FindByUUID should return nil after removal")
	}
	if h.FindByUUID("bbb") == nil {
		t.Error("FindByUUID('bbb') should still exist")
	}

	// Remove non-existent
	removed = h.RemoveByUUID("zzz")
	if removed {
		t.Error("RemoveByUUID should return false for non-existent UUID")
	}

	// Remove empty string
	removed = h.RemoveByUUID("")
	if removed {
		t.Error("RemoveByUUID should return false for empty UUID")
	}
}

// --- 30.4-UNIT-014: Resume context deserialization ---

func TestResume_ContextDeserialized(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "ctxdeser-0000-0000-0000-000000000001"

	// Write checkpoint with specific context content
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ctxSnap := json.RawMessage(`{"system_prompt":"You are a test agent","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"world"}],"max_size":64}`)
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            uuid,
		LastStep:        3,
		Timestamp:       time.Now(),
		ContextSnapshot: ctxSnap,
		ProcState: CheckpointProcState{
			Provider:       "claude",
			Model:          "claude-4",
			Intent:         "context test",
			AllowedDevices: []string{"/dev/fs"},
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("process not found")
	}

	// Verify context was deserialized
	ctx, ctxErr := k.ctxMgr.GetContext(proc.CtxID)
	if ctxErr != nil {
		t.Fatalf("GetContext: %v", ctxErr)
	}

	promptResult, buildErr := k.ctxMgr.BuildPrompt(proc.CtxID)
	if buildErr != nil {
		t.Fatalf("BuildPrompt: %v", buildErr)
	}

	if promptResult.SystemPrompt != "You are a test agent" {
		t.Errorf("SystemPrompt = %q, want %q", promptResult.SystemPrompt, "You are a test agent")
	}
	_ = ctx // used above via GetContext

	if len(promptResult.Messages) != 2 {
		t.Errorf("Messages count = %d, want 2", len(promptResult.Messages))
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- 30.4-UNIT-015: Resume rejects path-traversal UUID ---

func TestResume_InvalidUUIDFormat(t *testing.T) {
	k, _ := setupResumeKernel(t)

	cases := []string{
		"../../etc/passwd",
		"../secret",
		"valid-but/has-slash",
		"has\\backslash",
		"has.dot.in.it",
	}
	for _, bad := range cases {
		_, err := k.Resume(bad)
		if err == nil {
			t.Errorf("expected error for UUID %q", bad)
			continue
		}
		se, ok := err.(*SyscallError)
		if !ok {
			t.Errorf("expected *SyscallError for UUID %q, got %T", bad, err)
			continue
		}
		if se.Code != types.ErrInvalid {
			t.Errorf("UUID %q: code = %q, want %q", bad, se.Code, types.ErrInvalid)
		}
	}
}

// --- 30.4-UNIT-016: Resume rejects checkpoint at max step ---

func TestResume_MaxStepsBoundary(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "aabb0000-0000-0000-0000-000000000001"

	// Write checkpoint where LastStep == MaxSteps (budget exhausted)
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ctxSnap := json.RawMessage(`{"system_prompt":"","messages":[],"max_size":64}`)
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            uuid,
		LastStep:        50,
		Timestamp:       time.Now(),
		ContextSnapshot: ctxSnap,
		ProcState: CheckpointProcState{
			Provider: "claude",
			Model:    "claude-4",
			Intent:   "max steps test",
			MaxSteps: 50, // LastStep == MaxSteps
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	_, err := k.Resume(uuid)
	if err == nil {
		t.Fatal("expected error when checkpoint is at max step")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrInvalid)
	}
}
