package shell

// ScriptEventKind enumerates the first-class control-flow events emitted by
// ScriptExecutor for observation (Story 43.2 / Epic 43 OBS-1).
//
// Stable across releases: dashboard renderers (Story 43.3) and
// kernel.EmitScriptEvent callers match on these literal values, so do not
// rename without a coordinated change in `ipc/`, `kernel/observe.go`, and
// any downstream consumer.
type ScriptEventKind string

const (
	// ScriptStmtBegin fires at the top of every statement case in
	// executeBlock — Meta carries `stmt_kind` (StatementKind string).
	ScriptStmtBegin ScriptEventKind = "ScriptStmtBegin"

	// ScriptStmtEnd fires at every statement exit (success / break / error).
	// Meta carries `stmt_kind`, `error` (string, empty on success), and
	// `exit_code` (int, optional) so Timeline can show success vs failure.
	ScriptStmtEnd ScriptEventKind = "ScriptStmtEnd"

	// ScriptSpawn fires immediately before spawner.SpawnAndWait for any
	// StmtSpawn, including spawns launched inside StmtParallel bodies (in
	// which case the OnEvent hook is invoked from a parallel goroutine —
	// implementations MUST be thread-safe). Meta carries `intent` (already
	// env.Expand'd), `agent`, `model`, `assign`.
	ScriptSpawn ScriptEventKind = "ScriptSpawn"

	// ScriptWhileIter fires once per while-loop iteration, after the
	// condition has evaluated true and before executeBlock(body). Meta
	// carries `iteration` (1-based int) and `condition` (textualised).
	ScriptWhileIter ScriptEventKind = "ScriptWhileIter"

	// ScriptCondition fires for every condition evaluation in StmtIf and
	// StmtWhile (each pass). Meta carries `condition` (textualised),
	// `left` (evaluated LHS string), and `result` (bool).
	ScriptCondition ScriptEventKind = "ScriptCondition"

	// ScriptStmtPaused fires when the executor parks at a statement boundary
	// because the injected SuspendController reports IsSuspendRequested()
	// (Story 44.2 / Ctrl+C → SIGPAUSE). Meta carries `stmt_kind` and
	// `reason` ("user_paused"). Lets dashboard Timeline render a suspend
	// anchor at the exact statement where execution halted.
	ScriptStmtPaused ScriptEventKind = "ScriptStmtPaused"

	// ScriptStmtResumed fires when ResumedCh() closes and the executor leaves
	// the suspend wait, immediately before running the parked statement. Meta
	// carries `stmt_kind`.
	ScriptStmtResumed ScriptEventKind = "ScriptStmtResumed"
)

// ScriptEvent is the payload delivered to ScriptExecutor.OnEvent. The shell
// package intentionally avoids importing kernel/internal types so the hook
// boundary stays a one-way dependency (kernel imports shell, never the
// reverse). The IPC layer translates ScriptEvent into types.SyscallEvent
// before handing it to kernel.EmitScriptEvent.
type ScriptEvent struct {
	Kind ScriptEventKind // event category — see constants above
	Line int             // 1-based source line of the statement (from Statement.Line)
	// Intent carries a short human-readable summary for events whose Meta
	// would otherwise need formatting on the consumer side (e.g. spawn
	// intent). Empty string when Meta already covers the full payload.
	Intent string
	// Meta carries structured fields keyed in snake_case. Consumers SHOULD
	// treat this map as read-only after delivery; the executor allocates a
	// fresh map per emit today, but that is not part of the contract.
	Meta map[string]any
}
