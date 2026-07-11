package kernel

import (
	"context"
	"log"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// OS process-group reconcile (Story 66.5, P5).
//
// The kernel's ctx-cancel → driver group-kill chain (Phase A) covers every
// death/suspend path WHILE the daemon is alive. Two residual gaps remain:
//   ① daemon crash / kill -9 → ctx cancel never runs → CLI leaders orphaned
//      (Pdeathsig reaches the leader but its subagents reparent to init);
//   ② historical residue that crosses a daemon restart.
//
// This reconcile loop is the OS-side backstop: it periodically scans the host
// for OS processes self-tagged with RNIX_PROC_UUID (injected by the LLM CLI
// drivers), matches each against the in-memory process table, and reaps those
// with no live rnix owner. Identification is env-tag ONLY — never by process
// name — so a user's own `claude` invocation is never touched.
//
// Two-round confirmation eliminates single-scan races (spawn mid-flight,
// twoPhaseShutdown grace窗口, group SIGTERM in flight): round one warns and
// records a candidate; only a process still alive AND still orphaned on the
// next round is reaped. Linux-only (needs /proc/<pid>/environ); other platforms
// no-op with a startup log.
// =============================================================================

const (
	// osReconcileInterval is how often the reconcile loop scans /proc. 60s
	// matches the two-round-confirmation cadence: an orphan is warned on one
	// scan and reaped on the next, so real reaping lags ~1 interval behind
	// detection. Kept a const (with a test seam via osReconciler.interval)
	// rather than a config knob to limit surface (Dev Notes 拍板 7).
	osReconcileInterval = 60 * time.Second

	// osReconcileGrace is the SIGTERM→SIGKILL window applied to a confirmed
	// orphan's process group. Short by design: the process has already survived
	// a full interval as a warned candidate, so it is unambiguously unwanted.
	osReconcileGrace = 5 * time.Second

	// osReconcileArgvMax caps the argv summary length logged per orphan.
	osReconcileArgvMax = 120
)

// osCliProc is one OS process discovered by the /proc scan carrying a
// RNIX_PROC_UUID marker.
type osCliProc struct {
	OSPid int
	UUID  string
	Argv  string // truncated /proc/<pid>/cmdline summary, for logs only
}

// osProcScanner returns the currently-live RNIX_PROC_UUID-tagged OS processes.
// Injectable for tests; the production implementation walks /proc (linux).
type osProcScanner func() []osCliProc

// osProcKiller reaps the process group of osPid (SIGTERM → grace → SIGKILL).
// Injectable for tests; the production implementation resolves the pgid and
// signals the group (linux). The ctx lets a daemon shutdown interrupt the
// SIGTERM→SIGKILL grace wait instead of blocking the reconcile loop (F2).
type osProcKiller func(ctx context.Context, osPid int)

// osReconciler holds the reconcile loop's cross-round candidate state.
type osReconciler struct {
	scan     osProcScanner
	kill     osProcKiller
	owned    func(uuid string) bool // true ⇔ uuid maps to a Created/Running proc
	interval time.Duration

	// candidates maps an orphan's OS pid → its UUID as seen on the PREVIOUS
	// round. An entry surviving into the next round (still live, still orphan)
	// is what triggers reaping (two-round confirmation).
	candidates map[int]string
}

// reconcileOnce performs a single scan → classify → warn/reap pass.
//
// Ownership: a UUID mapping to a Created or Running rnix process is "有主" and
// exempt (Dev Notes 拍板 5 — Suspended/Zombie/Dead and not-in-table are all
// orphan surfaces). Reaping is group-scoped via r.kill; redundant entries that
// share a pgid already killed this round are ESRCH no-ops.
func (r *osReconciler) reconcileOnce(ctx context.Context) {
	procs := r.scan()
	next := make(map[int]string, len(procs))
	reaped := make(map[string]bool) // UUID → already reaped this round (F2 dedup)

	for _, p := range procs {
		if p.UUID == "" {
			continue
		}
		if r.owned(p.UUID) {
			continue // 有主豁免
		}
		// Orphan: no live owner in {Created, Running}. Two-round confirmation
		// must match BOTH OSPid AND the UUID captured last round — otherwise a
		// pid recycled within the interval to a *different* orphan is reaped on
		// its first sighting, bypassing the warn round the confirmation exists
		// to provide (Story 66.5 code-review F1).
		if prevUUID, wasCandidate := r.candidates[p.OSPid]; wasCandidate && prevUUID == p.UUID {
			// Second consecutive round orphaned & still alive → reap. A leader
			// and any subagents it forks share one UUID (env-inherited) and one
			// process group, so reap each UUID's group at most once per round —
			// avoids re-running the killer's full SIGTERM→grace→SIGKILL (and its
			// blocking grace wait) for every member (F2 dedup).
			if reaped[p.UUID] {
				continue
			}
			reaped[p.UUID] = true
			log.Printf("[os-reconcile] reaping orphan CLI process os_pid=%d uuid=%s argv=%q",
				p.OSPid, p.UUID, p.Argv)
			r.kill(ctx, p.OSPid)
			// Not carried forward; if it survives it re-enters as a fresh
			// candidate next round.
		} else {
			// First sighting → warn + register candidate, do not kill yet.
			log.Printf("[os-reconcile] warn: orphan CLI process os_pid=%d uuid=%s argv=%q — candidate for reap",
				p.OSPid, p.UUID, p.Argv)
			next[p.OSPid] = p.UUID
		}
	}
	// Candidates whose OS pid vanished (or became owned) are naturally dropped
	// by not appearing in `next`.
	r.candidates = next
}

// run drives the reconcile loop until ctx is cancelled. First scan fires
// immediately (parity with StartGcDaemon) so a daemon restarting into a backlog
// of orphans starts warning without waiting a full interval.
func (r *osReconciler) run(ctx context.Context) {
	log.Printf("[os-reconcile] enabled: interval=%s", r.interval)
	r.reconcileOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[os-reconcile] daemon stopped")
			return
		case <-ticker.C:
			r.reconcileOnce(ctx)
		}
	}
}

// uuidOwnedByLiveProc reports whether uuid maps to an in-table process in a
// state that legitimately owns a running CLI subprocess (Created or Running).
// Suspended is intentionally NOT owned (Dev Notes 拍板 5): suspendSubtree has
// already proc.Cancel()'d the in-flight LLM call, so a surviving CLI child is a
// leak to be reaped — the exemption protects the rnix process ledger, not its
// OS children.
func (k *KernelImpl) uuidOwnedByLiveProc(uuid string) bool {
	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		return false
	}
	switch proc.GetState() {
	case types.StateCreated, types.StateRunning:
		return true
	default:
		return false
	}
}

// StartOSReconcileDaemon launches the background OS reconcile loop for the
// daemon lifetime (cancelled via ctx at shutdown). Non-Linux platforms log once
// and return — /proc/<pid>/environ is required and has no portable equivalent.
func (k *KernelImpl) StartOSReconcileDaemon(ctx context.Context) {
	if !osReconcileSupported {
		log.Printf("[os-reconcile] disabled (linux-only)")
		return
	}
	r := &osReconciler{
		scan:       defaultOSProcScanner,
		kill:       defaultOSProcKiller,
		owned:      k.uuidOwnedByLiveProc,
		interval:   osReconcileInterval,
		candidates: map[int]string{},
	}
	r.run(ctx)
}
