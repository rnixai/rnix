package kernel

import (
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.5: resumeFromHistory 错误消息区分 (AC#6)
//
// Acceptance criteria covered:
//   - AC#6  UNIT-040  Resume after gc returns "garbage collected" error
//   - AC#6  UNIT-041  Resume for never-seen UUID returns "never persisted"
//
// RED PHASE: resumeFromHistory currently returns the legacy
//
//   "no data found for UUID %s: proc-info.json missing"
//
// for both cases. dev-story will branch on procHistory.HasEverSeen(uuid):
//   - HasEverSeen → "garbage collected"
//   - !HasEverSeen → "never spawned or never persisted"
// =============================================================================

// newResumeAfterGcKernel builds a kernel with stepDataDir set + procHistory
// pre-populated so we can simulate the "gc'd" state by Add()+RemoveByUUID().
func newResumeAfterGcKernel(t *testing.T) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	k.SetStepDataDir(t.TempDir())
	return k
}

// --- UNIT-040 (AC#6): Resume after gc returns "garbage collected" ---

func TestATDD_42_5_040_Resume_AfterGc_ReturnsGarbageCollectedError(t *testing.T) {
	t.Skip("RED phase: 42.5 resumeFromHistory error branching not implemented")

	k := newResumeAfterGcKernel(t)
	uuid := "gced-aaaaaaaa-bbbb-cccc-dddd-000000000001"

	// Simulate the lifecycle: process existed once (Add'd to history), then
	// gc removed both the on-disk directory AND the procHistory entry.
	k.procHistory.Add(vfs.ProcInfo{PID: types.PID(1), UUID: uuid, State: types.StateDead})
	k.procHistory.RemoveByUUID(uuid)
	// On disk: no .rnix/data/steps/<uuid>/ ever existed (or was removed by gc).

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err == nil {
		t.Fatal("Resume after gc must return an error")
	}

	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "garbage collected") {
		t.Errorf("err message must contain \"garbage collected\"; got %q", msg)
	}

	// Should still surface ErrNotFound (no new error code introduced — AC#6
	// 错误码均为 types.ErrNotFound).
	if !isErrCode42_5(err, types.ErrNotFound) {
		t.Errorf("err must have code ErrNotFound; got %v", err)
	}
}

// --- UNIT-041 (AC#6): Resume for never-seen UUID returns "never persisted" ---

func TestATDD_42_5_041_Resume_NeverSeen_ReturnsNotFoundError(t *testing.T) {
	t.Skip("RED phase: 42.5 resumeFromHistory error branching not implemented")

	k := newResumeAfterGcKernel(t)
	uuid := "neverexists-aaaaaaaa-bbbb-cccc-dddd-000000000099"
	// Do NOT Add() to procHistory; do NOT write to disk.

	_, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err == nil {
		t.Fatal("Resume for never-seen UUID must return an error")
	}

	msg := strings.ToLower(err.Error())
	// New message must clearly say it was never spawned / never persisted.
	if !strings.Contains(msg, "never") {
		t.Errorf("err message for never-seen UUID must contain \"never\"; got %q", msg)
	}
	// Must NOT mistakenly say "garbage collected" — that's the gc'd case.
	if strings.Contains(msg, "garbage collected") {
		t.Errorf("err message must NOT contain \"garbage collected\" for never-seen UUID; got %q", msg)
	}

	if !isErrCode42_5(err, types.ErrNotFound) {
		t.Errorf("err must have code ErrNotFound; got %v", err)
	}
}

// isErrCode42_5 reports whether err's underlying SyscallError has the given
// code. Suffixed _42_5 to avoid clashing with other helper names in this
// package.
func isErrCode42_5(err error, want types.ErrCode) bool {
	if err == nil {
		return false
	}
	if se, ok := err.(*SyscallError); ok && se.Code == want {
		return true
	}
	return false
}
