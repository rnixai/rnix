//go:build atdd_red
// +build atdd_red

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/shell"
)

// ============================================================
// ATDD RED PHASE — Story 43.2: handleExecScript script-trace integration
//
// Symbols / wiring that do NOT yet exist:
//   - handleExecScript initialising an EventWriter for the script-runner
//     PID before executor.Execute
//   - executor.OnEvent set to forward ScriptEvents into
//     s.kern.EmitScriptEvent
//   - kernel.EmitScriptEvent + (*Process).AttachEventWriter (covered by
//     atdd_43_2_emit_script_event_test.go in kernel/ package)
//
// These tests cover the IPC seam: they construct a minimal Server, drive
// the same plumbing handleExecScript would, and assert events.jsonl ends
// up populated. The full HTTP/Unix-socket round-trip is exercised
// indirectly via handleExecScript's helpers.
//
// Mapping to ACs:
//   AC#3 — events.jsonl contents follow the existing SyscallEventDisk
//          schema (round-tripped through ReadAllEvents)
//   AC#4 — EventWriter is initialised for the script-runner UUID
//   AC#6 — EventWriter init failure must NOT block execution (script
//          still runs, OnEvent is a no-op)
//   AC#7 — end-to-end while+spawn produces the expected event sequence:
//          ScriptStmtBegin(while) → ScriptCondition → ScriptWhileIter →
//          ScriptSpawn → ScriptStmtEnd(spawn) → … → ScriptStmtEnd(while)
//
// Build-tagged so the rest of the IPC suite stays green until dev-story
// wires the production path and removes the tag.
// ============================================================

// fakeEventSpawner is a shell.KernelSpawner that returns canned results
// without going through the real kernel. Each call advances an index;
// extra calls past the configured tail return ("done", 0, 0, nil).
type fakeEventSpawner struct {
	results []string
	calls   int
}

func (f *fakeEventSpawner) SpawnAndWait(_ context.Context, intent, agent, model string) (string, int, int, error) {
	out := "done"
	if f.calls < len(f.results) {
		out = f.results[f.calls]
	}
	f.calls++
	return out, 0, 0, nil
}

func (f *fakeEventSpawner) Wait(context.Context, int) (int, error) {
	return 0, errors.New("Wait not used by these tests")
}

// runScriptRunnerLikeHandleExecScript reproduces just enough of
// handleExecScript to wire EventWriter + OnEvent the way Story 43.2
// requires. Tests below call this helper instead of building a full
// Server + net.Conn, which keeps them deterministic.
//
// NOTE: This helper is intentionally a thin wrapper over the symbols
// Story 43.2 will introduce, so when dev-story implements the
// production wiring this helper collapses to a one-liner that calls
// into the real handler.
func runScriptRunnerLikeHandleExecScript(
	t *testing.T,
	scriptSrc string,
	stepBaseDir string, // pass "" to suppress EventWriter init
	spawner shell.KernelSpawner,
) (*kernel.KernelImpl, *kernel.Process, error) {
	t.Helper()

	kern := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	t.Cleanup(func() { kern.Shutdown() })

	script, err := shell.ParseScript(scriptSrc)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	scriptPID, err := kern.Spawn("run: 43-2.ash", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn(SkipReasonLoop): %v", err)
	}
	scriptProc, ok := kern.GetProcess(scriptPID)
	if !ok {
		t.Fatalf("GetProcess: vanished after Spawn")
	}

	if stepBaseDir != "" {
		ew, ewErr := kernel.NewEventWriter(stepBaseDir, scriptProc.UUID)
		if ewErr == nil {
			scriptProc.AttachEventWriter(ew)
			t.Cleanup(func() { _ = ew.Close() })
		}
		// If EventWriter init failed, Story spec says "log warn but do NOT
		// block execution" — mirror that here so the no-writer test path
		// is exercised by the same helper.
	}

	executor := shell.NewScriptExecutor(spawner, shell.NewEnvironment())
	executor.OnEvent = func(ev shell.ScriptEvent) {
		args := make(map[string]any, len(ev.Meta)+2)
		for k, v := range ev.Meta {
			args[k] = v
		}
		args["line"] = ev.Line
		if ev.Intent != "" {
			args["intent"] = ev.Intent
		}
		kern.EmitScriptEvent(scriptProc, string(ev.Kind), args)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, execErr := executor.Execute(ctx, script)
	return kern, scriptProc, execErr
}

// --- AC#3 + AC#4 + AC#7 — full while+spawn flow lands in events.jsonl ---

// TestHandleExecScript_EmitsScriptTraceEvents_E2E runs a small while-spawn
// program and asserts that events.jsonl ends up containing at least one of
// every Story-spec Kind, in the call-site order Timeline depends on.
func TestHandleExecScript_EmitsScriptTraceEvents_E2E(t *testing.T) {
	stepBaseDir := t.TempDir()
	spawner := &fakeEventSpawner{
		results: []string{"1", "2", "3"}, // increment values
	}
	src := `
export N=0
while $N != "2"
N = spawn "increment"
end
spawn "done"
`
	_, proc, err := runScriptRunnerLikeHandleExecScript(t, src, stepBaseDir, spawner)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	path := filepath.Join(stepBaseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("events.jsonl missing for script-runner UUID=%s: %v", proc.UUID, statErr)
	}
	rows, err := kernel.ReadAllEvents(path)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}

	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Syscall]++
	}

	required := []string{"ScriptStmtBegin", "ScriptStmtEnd", "ScriptSpawn", "ScriptWhileIter", "ScriptCondition"}
	for _, kind := range required {
		if counts[kind] == 0 {
			t.Errorf("missing required syscall %q in events.jsonl (counts: %v)", kind, counts)
		}
	}

	// AC#7: at least one ScriptStmtBegin must carry stmt_kind=while (the
	// outer while statement). Without this, "where did the loop stall"
	// cannot be reconstructed from events.jsonl alone.
	var sawWhileBegin bool
	for _, r := range rows {
		if r.Syscall == "ScriptStmtBegin" {
			if kind, _ := r.Args["stmt_kind"].(string); kind == "while" {
				sawWhileBegin = true
				break
			}
		}
	}
	if !sawWhileBegin {
		t.Errorf("no ScriptStmtBegin(stmt_kind=while) in events.jsonl — Timeline cannot locate the loop")
	}

	// AC#7 ordering check: a ScriptSpawn must appear after the first
	// ScriptWhileIter (we're spawning inside the loop body, not before it).
	var firstIterIdx, firstSpawnIdx = -1, -1
	for i, r := range rows {
		if firstIterIdx < 0 && r.Syscall == "ScriptWhileIter" {
			firstIterIdx = i
		}
		if firstSpawnIdx < 0 && r.Syscall == "ScriptSpawn" {
			firstSpawnIdx = i
		}
	}
	if firstIterIdx < 0 || firstSpawnIdx < 0 || firstSpawnIdx < firstIterIdx {
		t.Errorf("expected ScriptWhileIter (idx=%d) BEFORE first ScriptSpawn (idx=%d) — loop-body call ordering broken",
			firstIterIdx, firstSpawnIdx)
	}
}

// --- AC#3 — emitted rows decode cleanly under the existing wire schema ---

// TestHandleExecScript_EventRows_DecodableAsSyscallEventDisk asserts every
// row in events.jsonl satisfies the existing kernel.SyscallEventDisk schema
// so dashboard `list_events` IPC keeps working without protocol changes.
func TestHandleExecScript_EventRows_DecodableAsSyscallEventDisk(t *testing.T) {
	stepBaseDir := t.TempDir()
	spawner := &fakeEventSpawner{results: []string{"ok"}}
	src := `
export X=1
spawn "hello"
`
	_, proc, err := runScriptRunnerLikeHandleExecScript(t, src, stepBaseDir, spawner)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	path := filepath.Join(stepBaseDir, "data", "steps", proc.UUID, "events.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("events.jsonl is empty — emit path did not produce any rows")
	}

	// Each non-empty NDJSON line must unmarshal cleanly into the existing
	// schema. We additionally check pid + ts_ms shape since Timeline relies
	// on both.
	for i, line := range splitNDJSON(raw) {
		var row kernel.SyscallEventDisk
		if err := json.Unmarshal(line, &row); err != nil {
			t.Errorf("line[%d] does not decode as SyscallEventDisk: %v\nraw: %s", i, err, line)
			continue
		}
		if row.Syscall == "" {
			t.Errorf("line[%d].Syscall is empty", i)
		}
		if uint64(row.PID) != uint64(proc.PID) {
			t.Errorf("line[%d].PID = %d, want %d", i, row.PID, proc.PID)
		}
		// ts_ms must be a positive float for events emitted AFTER
		// process creation (CreatedAt is set in Spawn).
		if row.TimestampMs <= 0 {
			t.Errorf("line[%d].TimestampMs = %v, want > 0", i, row.TimestampMs)
		}
	}
}

// --- AC#6 — EventWriter init failure (or stepBaseDir="") must not block execution ---

// TestHandleExecScript_EventWriterInitFailure_DoesNotBlockExecution proves
// the spec invariant: "EventWriter init failure → log warn, but execution
// continues". We pass stepBaseDir="" to mimic the helper's no-writer path;
// the script must still complete and the process must reach Dead via the
// usual Reap. No events.jsonl is expected, no panic is acceptable.
func TestHandleExecScript_EventWriterInitFailure_DoesNotBlockExecution(t *testing.T) {
	spawner := &fakeEventSpawner{results: []string{"ok"}}
	src := `
export A=1
spawn "hello"
`
	_, proc, err := runScriptRunnerLikeHandleExecScript(t, src, "", spawner)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if spawner.calls != 1 {
		t.Errorf("spawner.calls = %d, want 1 (script must complete spawn)", spawner.calls)
	}
	if proc.GetState() != types.StateRunning {
		// After Execute returns the helper does NOT call Finish/Reap (the
		// real handleExecScript does); so the process should still be
		// Running. The key invariant: it didn't crash mid-script.
		t.Errorf("proc.State = %v, want StateRunning (helper does not finalise)", proc.GetState())
	}
}

// splitNDJSON splits raw into non-empty newline-delimited JSON rows.
func splitNDJSON(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			if i > start {
				out = append(out, raw[start:i])
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, raw[start:])
	}
	return out
}
