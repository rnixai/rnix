package kernel

import (
	"context"
	"errors"
)

// =============================================================================
// Disk garbage collector for .rnix/data/steps/<uuid>/ (Story 42.5).
//
// RED PHASE — every behavior function returns errRunGcNotImplemented (or its
// helper-specific sibling). Tests reference these sentinels to detect "stub
// still in place"; dev-story replaces each function body with the real
// scanning / file deletion / ticker loop.
// =============================================================================

// GcCandidate describes one terminated process eligible for removal.
//
//   - UUID: process UUID (corresponds to .rnix/data/steps/<uuid>/).
//   - DeadAt: RFC3339Nano timestamp from proc-info.json (empty if never set).
//   - SizeBytes: total byte size of the candidate directory (best-effort sum).
//   - Reason: "age" | "count" | "age,count" — which retention rule(s) matched.
type GcCandidate struct {
	UUID      string
	DeadAt    string
	SizeBytes int64
	Reason    string
}

// GcResult is the outcome of one RunGc invocation.
//
// RemovedCount + FreedBytes describe what was actually cleaned in non-dry-run
// mode; Candidates is always populated for dry-run + as the planned list for
// regular runs (callers should not assume RemovedUUIDs == Candidates because
// individual deletion errors are tolerated).
type GcResult struct {
	RemovedCount int
	FreedBytes   int64
	RemovedUUIDs []string
	Candidates   []GcCandidate
}

// RED PHASE sentinel — re-export-friendly so package tests can reference it.
var errRunGcNotImplemented = errors.New("kernel.RunGc: not implemented (Story 42.5 dev-story)")

// errStartGcDaemonNotImplemented is a separate sentinel so a future dev-story
// implementing the scan logic before wiring the goroutine can have RunGc
// return nil while StartGcDaemon still trips this marker.
var errStartGcDaemonNotImplemented = errors.New("kernel.StartGcDaemon: not implemented (Story 42.5 dev-story)")

// RunGc scans .rnix/data/steps/ for terminated processes and removes those
// matching the active GcCfg retention policy (Story 42.5 AC#1, #2, #3, #14).
//
// dryRun=true populates result.Candidates without touching the filesystem
// (drives `rnix gc --dry-run`).
// force=true skips any future interactive confirmation hook (currently the
// confirmation lives in the CLI layer; this flag is reserved for the daemon
// background loop). Callers from the background gc daemon always pass true.
//
// RED PHASE: returns errRunGcNotImplemented and a zero-value result; tests
// that exercise live behavior wrap themselves in t.Skip.
func (k *KernelImpl) RunGc(dryRun bool, force bool) (GcResult, error) {
	_ = dryRun
	_ = force
	_ = k.stepDataDir
	return GcResult{}, errRunGcNotImplemented
}

// StartGcDaemon launches the background gc loop that periodically reaps
// terminated process directories per the active GcCfg (Story 42.5 AC#7, #8).
//
// Disabled config (both RetentionDays and MaxEntries == 0) returns immediately
// with a "[gc] disabled" log; otherwise it ticks every IntervalSeconds and
// invokes RunGc(false, true). Ctx cancellation cleanly exits the goroutine.
//
// RED PHASE: no-op return; tests wrap behavior assertions in t.Skip.
func (k *KernelImpl) StartGcDaemon(ctx context.Context) {
	_ = ctx
	_ = k.gcCfg
}

// scanGcCandidates is the (future) helper that walks .rnix/data/steps/ and
// returns GcCandidate entries for Dead/Zombie processes hit by retention rules.
// It deliberately does NOT reuse LoadProcHistory (different memory profile,
// different field needs — see Story spec "gc 不复用 LoadProcHistory").
//
// RED PHASE: returns errRunGcNotImplemented so the higher-level RunGc stub can
// surface a single canonical error code; dev-story replaces both bodies.
func scanGcCandidates(stepsDir string, cfg GcConfig) ([]GcCandidate, error) {
	_ = stepsDir
	_ = cfg
	return nil, errRunGcNotImplemented
}

// removeGcCandidate deletes the on-disk directory for one candidate UUID and
// returns the bytes freed (or an error). The caller is responsible for
// synchronizing procHistory afterwards via RemoveByUUID.
//
// RED PHASE: returns errRunGcNotImplemented.
func removeGcCandidate(stepsDir, uuid string) (int64, error) {
	_ = stepsDir
	_ = uuid
	return 0, errRunGcNotImplemented
}

// RED-phase anchors — pin sentinels and stub helpers so unusedfunc/unusedvar
// linters don't flag them while dev-story is in flight. Each `var _ = ...`
// captures one symbol; dev-story removes the anchor once the real call sites
// land.
var (
	_ = errStartGcDaemonNotImplemented
	_ = scanGcCandidates
	_ = removeGcCandidate
)
