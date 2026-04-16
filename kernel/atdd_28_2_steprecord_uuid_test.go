package kernel

// =============================================================================
// ATDD Story 28.2: StepRecord 路径迁移到 UUID
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: StepWriter 目录使用 UUID（integration through Spawn）
//   AC-2: process-meta.json 使用 UUID 路径并包含 PID
//   AC-3: 旧 PID 目录向后兼容
//
// Priority: P0 (data integrity for persistent paths)
// Test Level: Unit + Integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// AC-1: StepWriter 目录使用 UUID
// ---------------------------------------------------------------------------

func TestATDD28_2_AC1_Integration_StepDir_UsesUUID(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeCompleteResponse("uuid path test", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("uuid dir test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// AC-1: steps.jsonl should be under data/steps/<uuid>/ NOT data/steps/<pid>/
	uuidPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "steps.jsonl")
	if _, err := os.Stat(uuidPath); err != nil {
		t.Fatalf("AC-1: UUID path %q should exist: %v", uuidPath, err)
	}

	pidPath := filepath.Join(baseDir, "data", "steps", fmt.Sprintf("%d", pid), "steps.jsonl")
	if _, err := os.Stat(pidPath); err == nil {
		t.Fatalf("AC-1: PID path %q should NOT exist after UUID migration", pidPath)
	}
}

func TestATDD28_2_AC1_StepDir_FileName_StillStepsJSONL(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/echo", map[string]any{"msg": "test"}, 50),
			makeCompleteResponse("done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echo-result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("filename test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	uuidDir := filepath.Join(baseDir, "data", "steps", proc.UUID)
	entries, err := os.ReadDir(uuidDir)
	if err != nil {
		t.Fatalf("AC-1: ReadDir UUID dir: %v", err)
	}
	hasStepsFile := false
	for _, e := range entries {
		if e.Name() == "steps.jsonl" {
			hasStepsFile = true
		}
	}
	if !hasStepsFile {
		t.Fatal("AC-1: steps.jsonl should exist in UUID directory")
	}
}

func TestATDD28_2_AC1_StepRecords_Readable_AtUUIDPath(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/echo", map[string]any{"msg": "hi"}, 50),
			makeCompleteResponse("all done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echo-result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("uuid read test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	stepsFile := filepath.Join(baseDir, "data", "steps", proc.UUID, "steps.jsonl")
	rec, err := ReadStep(stepsFile, 1)
	if err != nil {
		t.Fatalf("AC-1: ReadStep from UUID path: %v", err)
	}
	if rec.Step != 1 {
		t.Fatalf("AC-1: expected Step=1, got %d", rec.Step)
	}
	if rec.Action != "tool_call" {
		t.Fatalf("AC-1: expected Action='tool_call', got %q", rec.Action)
	}
}

// ---------------------------------------------------------------------------
// AC-2: process-meta.json 使用 UUID 路径并包含 PID
// ---------------------------------------------------------------------------

func TestATDD28_2_AC2_ProcessMeta_AtUUIDPath(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeCompleteResponse("meta uuid test", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("meta uuid path test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	exit, waitErr := k.Wait(pid)
	if waitErr != nil {
		t.Fatalf("Wait: %v", waitErr)
	}
	if exit.Code != 0 {
		t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
	}

	metaFile := filepath.Join(baseDir, "data", "steps", proc.UUID, "process-meta.json")
	data, err := os.ReadFile(metaFile)
	if err != nil {
		t.Fatalf("AC-2: process-meta.json not found at UUID path %q: %v", metaFile, err)
	}
	if len(data) == 0 {
		t.Fatal("AC-2: process-meta.json should not be empty")
	}
}

func TestATDD28_2_AC2_ProcessMeta_ContainsPIDField(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeCompleteResponse("meta pid field test", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("pid field test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	exit, waitErr := k.Wait(pid)
	if waitErr != nil {
		t.Fatalf("Wait: %v", waitErr)
	}
	if exit.Code != 0 {
		t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
	}

	// Read from UUID path (AC-1 establishes this); fallback to PID path for current code
	metaPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "process-meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		// Fallback: try PID path (current behavior) — test still fails on PID field assertion
		metaPath = filepath.Join(baseDir, "data", "steps", fmt.Sprintf("%d", pid), "process-meta.json")
		data, err = os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("AC-2: process-meta.json not found at either UUID or PID path: %v", err)
		}
	}

	var meta struct {
		PID          types.PID `json:"pid"`
		SystemPrompt string    `json:"system_prompt"`
		ToolDefs     []any     `json:"tool_defs"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("AC-2: Unmarshal meta: %v", err)
	}
	if meta.PID == 0 {
		t.Fatalf("AC-2: process-meta.json should contain non-zero pid field, got %d", meta.PID)
	}
	if meta.PID != pid {
		t.Fatalf("AC-2: process-meta.json pid=%d, want %d", meta.PID, pid)
	}
}

func TestATDD28_2_AC2_ProcessMeta_NotAtPIDPath(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeCompleteResponse("meta not at pid", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("meta not at pid path", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	exit, waitErr := k.Wait(pid)
	if waitErr != nil {
		t.Fatalf("Wait: %v", waitErr)
	}
	if exit.Code != 0 {
		t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
	}

	pidMetaFile := filepath.Join(baseDir, "data", "steps", fmt.Sprintf("%d", pid), "process-meta.json")
	if _, err := os.Stat(pidMetaFile); err == nil {
		t.Fatalf("AC-2: process-meta.json should NOT exist at PID path %q after UUID migration", pidMetaFile)
	}
}

// ---------------------------------------------------------------------------
// AC-3: 旧 PID 目录向后兼容
// ---------------------------------------------------------------------------

func TestATDD28_2_AC3_Legacy_PID_ReadStep(t *testing.T) {
	baseDir := t.TempDir()
	legacyDir := filepath.Join(baseDir, "data", "steps", "42")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("AC-3: mkdir: %v", err)
	}

	rec := types.StepRecord{
		Step:        1,
		Action:      "tool_call",
		Summary:     "legacy step",
		RawResponse: `{"content":"legacy"}`,
	}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(legacyDir, "steps.jsonl"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("AC-3: write: %v", err)
	}

	stepsPath := filepath.Join(legacyDir, "steps.jsonl")
	readRec, err := ReadStep(stepsPath, 1)
	if err != nil {
		t.Fatalf("AC-3: legacy PID directory should be readable: %v", err)
	}
	if readRec.Step != 1 {
		t.Fatalf("AC-3: expected Step=1, got %d", readRec.Step)
	}
	if readRec.Summary != "legacy step" {
		t.Fatalf("AC-3: expected Summary='legacy step', got %q", readRec.Summary)
	}
}

func TestATDD28_2_AC3_Legacy_PID_ReadAllSteps(t *testing.T) {
	baseDir := t.TempDir()
	legacyDir := filepath.Join(baseDir, "data", "steps", "99")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("AC-3: mkdir: %v", err)
	}

	f, err := os.Create(filepath.Join(legacyDir, "steps.jsonl"))
	if err != nil {
		t.Fatalf("AC-3: create: %v", err)
	}
	enc := json.NewEncoder(f)
	for i := 1; i <= 3; i++ {
		_ = enc.Encode(types.StepRecord{
			Step:    i,
			Action:  "tool_call",
			Summary: fmt.Sprintf("legacy step %d", i),
		})
	}
	f.Close()

	stepsPath := filepath.Join(legacyDir, "steps.jsonl")
	records, total, err := ReadAllSteps(stepsPath, 0)
	if err != nil {
		t.Fatalf("AC-3: ReadAllSteps from legacy dir: %v", err)
	}
	if total != 3 {
		t.Fatalf("AC-3: expected total=3, got %d", total)
	}
	if len(records) != 3 {
		t.Fatalf("AC-3: expected 3 records, got %d", len(records))
	}
}

func TestATDD28_2_AC3_Legacy_And_UUID_Coexist(t *testing.T) {
	baseDir := t.TempDir()
	stepsBase := filepath.Join(baseDir, "data", "steps")

	// Legacy PID directory
	legacyDir := filepath.Join(stepsBase, "42")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("AC-3: mkdir legacy: %v", err)
	}
	legacyRec := types.StepRecord{Step: 1, Action: "complete", Summary: "legacy"}
	data, _ := json.Marshal(legacyRec)
	_ = os.WriteFile(filepath.Join(legacyDir, "steps.jsonl"), append(data, '\n'), 0o644)

	// UUID directory
	uuidDir := filepath.Join(stepsBase, "019576f2-1234-7def-8abc-def012345678")
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-3: mkdir uuid: %v", err)
	}
	uuidRec := types.StepRecord{Step: 1, Action: "tool_call", Summary: "uuid"}
	data2, _ := json.Marshal(uuidRec)
	_ = os.WriteFile(filepath.Join(uuidDir, "steps.jsonl"), append(data2, '\n'), 0o644)

	// Both should be independently readable
	legacyResult, err := ReadStep(filepath.Join(legacyDir, "steps.jsonl"), 1)
	if err != nil {
		t.Fatalf("AC-3: legacy read: %v", err)
	}
	if legacyResult.Summary != "legacy" {
		t.Fatalf("AC-3: legacy summary=%q, want 'legacy'", legacyResult.Summary)
	}

	uuidResult, err := ReadStep(filepath.Join(uuidDir, "steps.jsonl"), 1)
	if err != nil {
		t.Fatalf("AC-3: uuid read: %v", err)
	}
	if uuidResult.Summary != "uuid" {
		t.Fatalf("AC-3: uuid summary=%q, want 'uuid'", uuidResult.Summary)
	}

	// Verify directory enumeration sees both
	entries, err := os.ReadDir(stepsBase)
	if err != nil {
		t.Fatalf("AC-3: ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("AC-3: expected 2 directories (legacy + uuid), got %d", len(entries))
	}
}
