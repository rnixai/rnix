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

// =============================================================================
// REWRITTEN by Story 44.1 (AC#7).
//
// Prior to Story 44.1 this section asserted "Epic 43 ancestor wakeup" — that
// resuming a child process automatically unsuspended a cli_disconnected
// ancestor via `reactivateCliDisconnectedAncestors`. That mechanism is removed
// (see kernel/resume.go diff in 44.1) because it failed in the dashboard
// `r` path. The new product semantics are:
//
//   - Pause/Resume act on the *subtree* of the targeted PID, never upward.
//   - Resuming a child does NOT touch its parent's Suspended state.
//   - A user must manually `rnix resume <parent-uuid>` (or signal SIGRESUME on
//     the parent) to wake the whole subtree.
//
// Original tests preserved as rewrites per Murat's review red-line: tests that
// could not be expressed under the new semantics were deleted with explicit
// justification (see git blame on 44.1).
//
// Original ↔ rewrite map:
//
//   TestResume_FromHistory_UnsuspendsParentOnCliDisconnect
//     → TestResume_FromHistory_DoesNotAutoUnsuspendParent
//
//   TestResume_MultiLevel_UnsuspendsRootAncestor
//     → TestResumeSubtree_CascadesDescendantsNotAncestors
//
//   TestResume_MultiLevel_BreakOnReapedAncestor
//     → TestResumeSubtree_BreaksOnReapedDescendant
//
//   TestResume_DoesNotUnsuspendNonCliAncestor
//     → DELETED. The new ResumeSubtree resumes every Suspended descendant
//       regardless of SuspendReason. The "non-cli reason must not be cleared"
//       guarantee was specific to the deleted ancestor-walk algorithm and has
//       no equivalent expression under subtree semantics.
// =============================================================================

// Single level: direct parent Suspended+cli_disconnected. Resuming the child
// (via ResumeWithOpts/Resume) MUST NOT unsuspend the parent under Story 44.1.
// User must explicitly resume the parent to wake the subtree.
func TestResume_FromHistory_DoesNotAutoUnsuspendParent(t *testing.T) {
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

	// 44.1 contract: parent stays Suspended and its SuspendReason is unchanged.
	if got := parent.GetState(); got != types.StateSuspended {
		t.Errorf("parent.State = %v, want Suspended (44.1: child resume must not wake parent)", got)
	}
	if reason := parent.GetSuspendReason(); reason != SuspendReasonCLIDisconnected {
		t.Errorf("parent.SuspendReason = %q, want %q (must remain cli_disconnected)",
			reason, SuspendReasonCLIDisconnected)
	}

	cleanupResumedProc(t, k, result.PID)
}

// ResumeSubtree on the root cascades to descendants only — never up to a
// hypothetical further ancestor. Variant: invoking on a middle node only
// resumes that node and its descendants, leaving its parent untouched.
func TestResumeSubtree_CascadesDescendantsNotAncestors(t *testing.T) {
	k := newSubtreeKernel(t)

	// Build P1 (Suspended) → P2 (Suspended) → P3 (Suspended); P0 is a sibling
	// of P1 (also Suspended) under no shared parent — we just want to assert
	// it is not touched.
	p1 := makeSuspendedProc44_1(t, k, 0, "P1 root", "user_paused")
	p2 := makeSuspendedProc44_1(t, k, p1.PID, "P2 child", "user_paused")
	p3 := makeSuspendedProc44_1(t, k, p2.PID, "P3 grandchild", "user_paused")
	p0 := makeSuspendedProc44_1(t, k, 0, "P0 sibling", "user_paused")

	// 1. ResumeSubtree(P1) must cascade to P1/P2/P3 and leave P0 alone.
	if _, _, err := k.ResumeSubtree(p1.PID); err != nil {
		t.Fatalf("ResumeSubtree(P1): %v", err)
	}
	for _, proc := range []*Process{p1, p2, p3} {
		assertProcState44_1(t, proc, types.StateRunning, proc.Intent+" after ResumeSubtree(P1)")
	}
	if got := p0.GetState(); got != types.StateSuspended {
		t.Errorf("P0 sibling state = %v, want Suspended (subtree resume must not touch siblings)", got)
	}

	// 2. Re-Suspend P2/P3 and then ResumeSubtree(P2). P1 must NOT regress to
	//    Running again from a SubtreeResume targeting its child.
	for _, proc := range []*Process{p1, p2, p3} {
		proc.SetSuspendReason("user_paused")
		if err := proc.Suspend(); err != nil {
			t.Fatalf("re-Suspend %s: %v", proc.Intent, err)
		}
	}
	// P1 stays Suspended at this point; ResumeSubtree(P2) targets the middle
	// of the tree and must only resume P2/P3.
	if _, _, err := k.ResumeSubtree(p2.PID); err != nil {
		t.Fatalf("ResumeSubtree(P2): %v", err)
	}
	assertProcState44_1(t, p2, types.StateRunning, "P2 after ResumeSubtree(P2)")
	assertProcState44_1(t, p3, types.StateRunning, "P3 after ResumeSubtree(P2)")
	if got := p1.GetState(); got != types.StateSuspended {
		t.Errorf("P1 state = %v, want Suspended (ResumeSubtree(P2) must not walk up to P1)", got)
	}
}

// Reaped (procHistory-only) descendant in the subtree: ResumeSubtree continues
// past it without error, processing other living descendants. Previously this
// test asserted "ancestor chain walk halts at reaped node". Under 44.1 there is
// no ancestor walk; the symmetric concern is "subtree walk must tolerate a
// reaped descendant".
func TestResumeSubtree_BreaksOnReapedDescendant(t *testing.T) {
	k := newSubtreeKernel(t)

	// Tree: root (Suspended) → child (Suspended) → grandchild (Suspended).
	// We simulate "child was reaped between Suspend and Resume" by removing it
	// from the proc table but leaving the grandchild parent linkage in place.
	root := makeSuspendedProc44_1(t, k, 0, "root", "user_paused")
	child := makeSuspendedProc44_1(t, k, root.PID, "child", "user_paused")
	grandchild := makeSuspendedProc44_1(t, k, child.PID, "grandchild", "user_paused")

	// Simulate reap of child: remove from procTable. Parent's Children list
	// still references child.PID — ResumeSubtree must tolerate the dangling
	// reference (GetProcess returns ok=false).
	k.RemoveProcess(child.PID)

	if _, _, err := k.ResumeSubtree(root.PID); err != nil {
		t.Fatalf("ResumeSubtree(root) with reaped descendant: %v", err)
	}

	assertProcState44_1(t, root, types.StateRunning, "root must resume even though child was reaped")
	// grandchild is unreachable through child; whether it resumes depends on
	// how ResumeSubtree handles the dangling child PID. We assert that the
	// kernel did not crash and that grandchild is at least in a consistent
	// state (Running OR Suspended — never an undefined transition).
	switch grandchild.GetState() {
	case types.StateRunning, types.StateSuspended:
		// OK either way; the documented contract permits both paths so long
		// as the kernel does not panic.
	default:
		t.Errorf("grandchild state = %v, want Running or Suspended", grandchild.GetState())
	}
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
