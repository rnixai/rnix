package kernel

import (
	"os"
	"path/filepath"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// exitReasonInterrupted is the synthetic exit reason stamped onto an on-disk
// history snapshot that was left in a non-terminal state (created/running) by
// a daemon that was force-killed before it could reap the process. It is the
// only exit_reason value this reconciler ever writes — Story 64.1 裁决 2 red
// line: normalization MUST NOT fabricate "completed" (that would misreport a
// process that never finished as successful). "interrupted" carries no special
// meaning on the resume side (resume.go only branches on "context_full"), so a
// reconciled entry resumes as an ordinary historical process — same treatment
// as an existing "killed" snapshot.
const exitReasonInterrupted = "interrupted"

// reconcileStaleHistoryEntry normalizes a non-terminal on-disk history snapshot
// (created/running/zombie) to Dead, so that a process which already died with a
// previous daemon is no longer rendered as 🟢 running / counted as Running.
//
// It returns the (possibly rewritten) info and whether it was changed. When
// changed, the caller writes the result back to disk (Story 64.1 裁决 5), so the
// cure is permanent — a second LoadHistory finds a terminal snapshot and skips
// normalization entirely (幂等).
//
// Exemptions (裁决 1 第 2 点 — 豁免序靠状态集合无交集论证，而非调用顺序):
//
//   - Suspended entries are a legitimate frozen state and are never normalized.
//     The daemon revival paths (LoadSuspendedFromDisk / AutoResumeDaemonShutdown)
//     only ever revive disk snapshots whose state is Suspended (a graceful
//     shutdown always stamps `suspended: daemon_shutdown`; a force-kill leaves a
//     bare running snapshot with no daemon_shutdown marker, which auto-resume
//     never picks up). The normalization target set {created, running, zombie}
//     and the revival target set {suspended} are therefore disjoint — running
//     before the revival sequence introduces no race.
//
//   - A UUID currently live in procTable is skipped. At daemon startup the
//     procTable is empty so this guard never fires, but LoadHistory is a public
//     method; the guard makes it re-entrant safe at any call time (epic AC3 的
//     两种判定之一，选此种).
//
// Known limitation (Story 64.1 review D1, 裁决归 Epic 64 R4): liveness is
// judged only against THIS daemon's procTable. If two daemons coexist (the
// EnsureDaemon socket-takeover pathology), daemon B's startup will rewrite
// daemon A's genuinely-running snapshots to dead/interrupted; A's next
// checkpoint write restores them, so the damage is transient view skew, not
// data loss. Cross-daemon liveness probing (pid check / snapshot freshness
// threshold) is deliberately out of scope here and owned by the Epic 64 R4
// double-daemon governance story.
//
// ExitReason handling (裁决 2): a created/running snapshot has an empty
// ExitReason per the invariant matrix → we stamp "interrupted". A zombie
// snapshot already carries a real ExitReason recorded by Terminate (e.g.
// "context_full" / "killed" / "completed") → we PRESERVE it verbatim. This is
// load-bearing: kernel/resume.go's compaction-recovery check (isContextFull,
// keyed on `diskInfo.ExitReason` containing "context_full") decides whether a
// resume triggers compact-after-start, so overwriting it would silently
// change resume behavior and violate AC4. As a defensive floor, if a zombie
// snapshot's ExitReason is unexpectedly empty (invariant-violating dirty data)
// we also stamp "interrupted" so the reconciled Dead entry always satisfies
// ValidateProcInfoInvariant (Dead requires non-empty ExitReason).
func (k *KernelImpl) reconcileStaleHistoryEntry(baseDir string, info vfs.ProcInfo) (vfs.ProcInfo, bool) {
	// 豁免 1：suspended 是合法冻结态，永不归一化（裁决 6 附带观察同样不动 suspended）。
	// 归一化目标集只含 {created, running, zombie}；其余状态（含已终态 dead 与
	// parseProcessState 兜底的未知态）原样返回。
	switch info.State {
	case types.StateCreated, types.StateRunning, types.StateZombie:
		// 需要归一化，继续
	default:
		return info, false
	}

	// 豁免 2：UUID 已在 procTable（this-startup 复活条目）→ 跳过，re-entrant safe。
	if _, ok := k.GetProcessByUUID(info.UUID); ok {
		return info, false
	}

	// 归一化到 Dead（冻结态；ADR Decision 40：Dead 不损失可恢复性）。
	info.State = types.StateDead

	// dead_at 补时戳：仅当为空。取 proc-info.json 的 mtime（≈ 进程死亡前最后一次
	// 快照写入时刻，误差分钟级）；os.Stat 失败（罕见竞态）fallback 装载时刻，保证
	// dead_at 必非空。磁盘已有非空 dead_at 的脏数据保留原值（裁决 3）。
	// clamp：mtime 被外部工具/备份还原篡改为早于 CreatedAt 时，钳到 CreatedAt，
	// 避免产出负时长条目（review P1）。
	if info.DeadAt.IsZero() {
		path := filepath.Join(baseDir, "steps", info.UUID, procInfoFilename)
		if fi, err := os.Stat(path); err == nil {
			info.DeadAt = fi.ModTime()
		} else {
			info.DeadAt = time.Now()
		}
		if !info.CreatedAt.IsZero() && info.DeadAt.Before(info.CreatedAt) {
			info.DeadAt = info.CreatedAt
		}
	}

	// exit_reason：空 → "interrupted"；非空保留原值（裁决 2）。任何分支都不写
	// "completed" 字面量。
	if info.ExitReason == "" {
		info.ExitReason = exitReasonInterrupted
	}

	// 防御清理：Dead × SuspendReason 非空 = Story 44.5 修过的 EchoMatrix 矛盾；
	// IsPaused / PausedAt 一并清零（裁决 4 + review P2——留 PausedAt 会产出
	// "未暂停但有暂停时刻" 的自相矛盾字段组合）。保证归一化产物必过
	// ValidateProcInfoInvariant。
	info.SuspendReason = ""
	info.IsPaused = false
	info.PausedAt = time.Time{}

	return info, true
}
