package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Resume parent-linkage regression — "resume detaches child from process tree"
//
// Bug: resumeFromCheckpoint never restored ParentUUID; resumeFromHistory
// restored ParentUUID but left PPID = 0 and never called parent.AddChild.
// Effect: BuildProcessTree rendered the resumed child as a sibling of its
// (dead or alive) parent instead of nesting under it. See user repro:
//   pid=14 ppid=1 parent_uuid=019e3f6c..cd33 state=dead   ← original child
//   pid=1  ppid=0 parent_uuid=-              state=running ← resume detached
//
// Fix: restoreParentLinkage in resume.go runs on both paths. These tests pin
// the new behavior so the bug cannot regress silently.
// =============================================================================

// writeTestStepsAndMetaWithParent extends writeTestStepsAndMeta with parent_uuid
// in proc-info.json so the history path has a parent to restore from.
func writeTestStepsAndMetaWithParent(t *testing.T, baseDir, uuid, parentUUID string, lastStep int) {
	t.Helper()
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir steps dir: %v", err)
	}

	var stepsContent []byte
	for i := 1; i <= lastStep; i++ {
		record := map[string]any{
			"step":   i,
			"action": "tool_call",
			"request": map[string]any{
				"messages": []map[string]string{{"role": "user", "content": "x"}},
			},
			"response": map[string]any{"content": "y"},
		}
		line, _ := json.Marshal(record)
		stepsContent = append(stepsContent, line...)
		stepsContent = append(stepsContent, '\n')
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), stepsContent, 0o600); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}

	meta := map[string]any{"system_prompt": "test", "tool_defs": []any{}}
	mb, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(stepsDir, "process-meta.json"), mb, 0o600); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}

	procInfo := map[string]any{
		"pid":             99,
		"uuid":            uuid,
		"parent_uuid":     parentUUID,
		"state":           "dead",
		"intent":          "child intent",
		"provider":        "claude",
		"model":           "claude-4",
		"allowed_devices": []string{"/dev/fs"},
		"exit_reason":     "complete",
		"last_step":       lastStep,
	}
	pi, _ := json.Marshal(procInfo)
	if err := os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), pi, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}

// --- ResumeFromHistory: parent alive in procTable → PPID + Children restored ---

func TestResume_FromHistory_ParentAlive_RestoresPPIDAndChildren(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	parent := NewProcess(0, "parent intent", nil)
	parent.UUID = "parent-alive-aaaaaaaa-aaaa-aaaa-aaaaaaaaaaaa"
	k.AddProcess(parent)

	childUUID := "child-of-alive-bbbbbbbb-bbbb-bbbb-bbbbbbbbbbbb"
	writeTestStepsAndMetaWithParent(t, baseDir, childUUID, parent.UUID, 5)

	result, err := k.ResumeWithOpts(childUUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	resumed, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	if resumed.ParentUUID != parent.UUID {
		t.Errorf("ParentUUID = %q, want %q", resumed.ParentUUID, parent.UUID)
	}
	if resumed.PPID != parent.PID {
		t.Errorf("PPID = %d, want %d (parent PID)", resumed.PPID, parent.PID)
	}

	parent.mu.Lock()
	children := slices.Clone(parent.Children)
	parent.mu.Unlock()
	if !slices.Contains(children, resumed.PID) {
		t.Errorf("parent.Children = %v, missing resumed PID %d", children, resumed.PID)
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- ResumeFromHistory: parent dead → ParentUUID kept, PPID stays 0 ---
//
// BuildProcessTree uses ParentUUID-keyed lookup (UUID-first), so a node whose
// parent is dead-but-present-in-the-list still nests correctly. The contract
// here is "preserve lineage on disk", not "fabricate live PPID".

func TestResume_FromHistory_ParentGone_KeepsParentUUID(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	deadParentUUID := "dead-parent-cccccccc-cccc-cccc-cccccccccccc"
	childUUID := "child-of-dead-dddddddd-dddd-dddd-dddddddddddd"
	writeTestStepsAndMetaWithParent(t, baseDir, childUUID, deadParentUUID, 3)

	result, err := k.ResumeWithOpts(childUUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	resumed, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	if resumed.ParentUUID != deadParentUUID {
		t.Errorf("ParentUUID = %q, want %q (BuildProcessTree needs this even with dead parent)",
			resumed.ParentUUID, deadParentUUID)
	}
	if resumed.PPID != 0 {
		t.Errorf("PPID = %d, want 0 (parent not in procTable)", resumed.PPID)
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- ResumeFromCheckpoint: ParentUUID persisted + restored, links to live parent ---

func TestResume_FromCheckpoint_RestoresParentLinkage(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	parent := NewProcess(0, "parent intent", nil)
	parent.UUID = "cp-parent-eeeeeeee-eeee-eeee-eeeeeeeeeeee"
	k.AddProcess(parent)

	childUUID := "cp-child-ffffffff-ffff-ffff-ffffffffffff"
	stepsDir := filepath.Join(baseDir, "data", "steps", childUUID)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            childUUID,
		LastStep:        7,
		ContextSnapshot: json.RawMessage(`{"system_prompt":"test","messages":[],"max_size":64}`),
		ProcState: CheckpointProcState{
			PID:            types.PID(99),
			Provider:       "claude",
			Model:          "claude-4",
			AllowedDevices: []string{"/dev/fs"},
			Intent:         "child intent",
			ParentUUID:     parent.UUID,
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}
	// resumeFromCheckpoint also stat's proc-info.json on its disk-fallback branch
	// for non-procTable UUIDs; not needed here because we go through the "not in
	// procTable" path which prefers checkpoint.json once present.

	result, err := k.ResumeWithOpts(childUUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	resumed, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	if resumed.ParentUUID != parent.UUID {
		t.Errorf("ParentUUID = %q, want %q", resumed.ParentUUID, parent.UUID)
	}
	if resumed.PPID != parent.PID {
		t.Errorf("PPID = %d, want %d", resumed.PPID, parent.PID)
	}
	parent.mu.Lock()
	children := slices.Clone(parent.Children)
	parent.mu.Unlock()
	if !slices.Contains(children, resumed.PID) {
		t.Errorf("parent.Children = %v, missing resumed PID %d", children, resumed.PID)
	}

	cleanupResumedProc(t, k, result.PID)
}

// --- buildCheckpointData persists ParentUUID so future checkpoints carry lineage ---

func TestBuildCheckpointData_PersistsParentUUID(t *testing.T) {
	proc := NewProcess(7, "child", nil)
	proc.ParentUUID = "buildcp-parent-1111-2222-333333333333"

	data := buildCheckpointData(proc, 12, json.RawMessage(`{}`), 0)
	if data.ProcState.ParentUUID != proc.ParentUUID {
		t.Errorf("ProcState.ParentUUID = %q, want %q",
			data.ProcState.ParentUUID, proc.ParentUUID)
	}

	// Round-trip via JSON to guard against accidental json:"-" tag.
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTrip CheckpointData
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip.ProcState.ParentUUID != proc.ParentUUID {
		t.Errorf("round-trip ParentUUID = %q, want %q",
			roundTrip.ProcState.ParentUUID, proc.ParentUUID)
	}
}

// --- ListAllProcs reflects restored linkage (smoke test for tree-builder input) ---
//
// This is the closest unit-level proxy for "Dashboard tree shows resumed child
// under its parent." We assert ProcInfo carries PPID and ParentUUID consistent
// with what BuildProcessTree consumes; the renderer is tested separately in
// internal/dashboard/tree/builder_test.go.

// =============================================================================
// CLI-disconnect ancestor wakeup — `restoreParentLinkage` must walk the
// ParentUUID chain and Unsuspend procTable-resident ancestors whose
// SuspendReason == "cli_disconnected". Covers single-level and multi-level
// trees plus the documented break-on-Reaped-ancestor edge case.
// =============================================================================

// Single level: direct parent Suspended+cli_disconnected → Resume child → parent
// returns to Running.
func TestResume_FromHistory_UnsuspendsParentOnCliDisconnect(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	parent := NewProcess(0, "script runner", nil)
	parent.UUID = "cli-parent-1111-2222-3333-444444444444"
	if err := parent.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	parent.SetSuspendReason(SuspendReasonCLIDisconnected)
	if err := parent.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	k.AddProcess(parent)

	childUUID := "cli-child-5555-6666-7777-888888888888"
	writeTestStepsAndMetaWithParent(t, baseDir, childUUID, parent.UUID, 3)

	result, err := k.ResumeWithOpts(childUUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if got := parent.GetState(); got != types.StateRunning {
		t.Errorf("parent.State = %v, want Running (should be unsuspended)", got)
	}
	if reason := parent.GetSuspendReason(); reason != "" {
		t.Errorf("parent.SuspendReason = %q, want empty after wakeup", reason)
	}

	cleanupResumedProc(t, k, result.PID)
}

// Multi-level: P1(Suspended+cli) → P2(Running, procTable) → P3(disk).
// Resume P3 → P3 Running, P2 unchanged, P1 unsuspended via chain walk.
func TestResume_MultiLevel_UnsuspendsRootAncestor(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	p1 := NewProcess(0, "P1 script", nil)
	p1.UUID = "ml-p1-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	if err := p1.Start(); err != nil {
		t.Fatalf("p1.Start: %v", err)
	}
	p1.SetSuspendReason(SuspendReasonCLIDisconnected)
	if err := p1.Suspend(); err != nil {
		t.Fatalf("p1.Suspend: %v", err)
	}
	k.AddProcess(p1)

	p2 := NewProcess(p1.PID, "P2 agent", nil)
	p2.UUID = "ml-p2-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	p2.ParentUUID = p1.UUID
	if err := p2.Start(); err != nil {
		t.Fatalf("p2.Start: %v", err)
	}
	k.AddProcess(p2)

	p3UUID := "ml-p3-cccc-cccc-cccc-cccccccccccc"
	writeTestStepsAndMetaWithParent(t, baseDir, p3UUID, p2.UUID, 4)

	result, err := k.ResumeWithOpts(p3UUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if got := p1.GetState(); got != types.StateRunning {
		t.Errorf("p1.State = %v, want Running (chain walk should reach root)", got)
	}
	if reason := p1.GetSuspendReason(); reason != "" {
		t.Errorf("p1.SuspendReason = %q, want empty after wakeup", reason)
	}
	if got := p2.GetState(); got != types.StateRunning {
		t.Errorf("p2.State = %v, want Running (middle node unchanged)", got)
	}

	cleanupResumedProc(t, k, result.PID)
}

// Break on Reaped ancestor: P1(Suspended+cli) → P2(missing from procTable, only
// in procHistory) → P3(disk). Resume P3 → P3 Running, but P1 stays Suspended
// because chain walk halts at P2. This is the documented edge case from the
// spec's I/O matrix.
func TestResume_MultiLevel_BreakOnReapedAncestor(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	p1 := NewProcess(0, "P1 script", nil)
	p1.UUID = "br-p1-dddd-dddd-dddd-dddddddddddd"
	if err := p1.Start(); err != nil {
		t.Fatalf("p1.Start: %v", err)
	}
	p1.SetSuspendReason(SuspendReasonCLIDisconnected)
	if err := p1.Suspend(); err != nil {
		t.Fatalf("p1.Suspend: %v", err)
	}
	k.AddProcess(p1)

	// P2 only exists as a UUID string in P3's proc-info.json — it's NOT added
	// to procTable, simulating a reap.
	p2UUID := "br-p2-eeee-eeee-eeee-eeeeeeeeeeee"
	p3UUID := "br-p3-ffff-ffff-ffff-ffffffffffff"
	writeTestStepsAndMetaWithParent(t, baseDir, p3UUID, p2UUID, 4)

	result, err := k.ResumeWithOpts(p3UUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if got := p1.GetState(); got != types.StateSuspended {
		t.Errorf("p1.State = %v, want Suspended (chain breaks at P2; see spec edge case)", got)
	}
	if reason := p1.GetSuspendReason(); reason != SuspendReasonCLIDisconnected {
		t.Errorf("p1.SuspendReason = %q, want %q (unchanged because chain broke)",
			reason, SuspendReasonCLIDisconnected)
	}

	cleanupResumedProc(t, k, result.PID)
}

// Negative: non-cli SuspendReason must NOT be cleared by a descendant resume.
// Guards against "any resume unsuspends any Suspended ancestor".
func TestResume_DoesNotUnsuspendNonCliAncestor(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	parent := NewProcess(0, "user-suspended parent", nil)
	parent.UUID = "neg-parent-9999-8888-7777-666666666666"
	if err := parent.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	parent.SetSuspendReason("user_suspended")
	if err := parent.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	k.AddProcess(parent)

	childUUID := "neg-child-1010-2020-3030-404040404040"
	writeTestStepsAndMetaWithParent(t, baseDir, childUUID, parent.UUID, 2)

	result, err := k.ResumeWithOpts(childUUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if got := parent.GetState(); got != types.StateSuspended {
		t.Errorf("parent.State = %v, want Suspended (non-cli reason must not be cleared)", got)
	}
	if reason := parent.GetSuspendReason(); reason != "user_suspended" {
		t.Errorf("parent.SuspendReason = %q, want \"user_suspended\"", reason)
	}

	cleanupResumedProc(t, k, result.PID)
}

func TestResume_ProcInfo_ReflectsRestoredLinkage(t *testing.T) {
	k, baseDir := setupResumeKernel(t)

	parent := NewProcess(0, "parent", nil)
	parent.UUID = "procinfo-parent-aaaa-bbbb-cccccccccccc"
	k.AddProcess(parent)

	childUUID := "procinfo-child-dddd-eeee-ffffffffffff"
	writeTestStepsAndMetaWithParent(t, baseDir, childUUID, parent.UUID, 4)

	result, err := k.ResumeWithOpts(childUUID, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	procs := k.ListAllProcs()
	var found *vfs.ProcInfo
	for i := range procs {
		if procs[i].PID == result.PID {
			found = &procs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("resumed PID %d missing from ListAllProcs", result.PID)
	}
	if found.PPID != parent.PID || found.ParentUUID != parent.UUID {
		t.Errorf("ProcInfo: PPID=%d ParentUUID=%q, want PPID=%d ParentUUID=%q",
			found.PPID, found.ParentUUID, parent.PID, parent.UUID)
	}

	cleanupResumedProc(t, k, result.PID)
}
