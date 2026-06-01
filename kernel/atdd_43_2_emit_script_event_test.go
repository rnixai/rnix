package kernel

import (
	"os"
	"path/filepath"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
)

// ============================================================
// Story 43.2: kernel.EmitScriptEvent + AttachEventWriter — green-phase
//
// Mapping to ACs:
//   AC#3 — emit path serialises into the existing SyscallEventDisk schema
//          and lands in events.jsonl
//   AC#4 — AttachEventWriter setter exists, mu-protected
//   AC#6 — nil-guard: EmitScriptEvent on a process without an attached
//          EventWriter is silent (no panic, no error surfaced)
// ============================================================

// newSkipReasonLoopKernelProc spawns a SkipReasonLoop process via the real
// kernel (mirrors how handleExecScript builds the script-runner). No VFS
// devices / no LLM driver are required because SkipReasonLoop short-circuits
// the reasonStep pipeline.
func newSkipReasonLoopKernelProc(t *testing.T) (*KernelImpl, *Process) {
	t.Helper()
	kern := NewKernel(nil, rnixctx.NewManager(), nil)
	t.Cleanup(func() { kern.Shutdown() })

	pid, err := kern.Spawn("run: atdd-43-2.ash", nil, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn(SkipReasonLoop): %v", err)
	}
	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("GetProcess(%d): process vanished after Spawn", pid)
	}
	return kern, proc
}

// --- AC#3 + AC#4 — Attach + Emit round-trips through events.jsonl ---

// TestKernel_EmitScriptEvent_WritesToDisk verifies the end-to-end seam:
//
//  1. AttachEventWriter installs an EventWriter under proc.mu (no direct
//     field access, no race).
//  2. EmitScriptEvent forwards through the existing emitEvent path so the
//     same code that owns DebugChan + recordMgr + immune monitoring also
//     persists this event to disk.
//  3. ReadAllEvents returns the row with: syscall == kind we passed,
//     args == args we passed (snake_case key fidelity), and pid == proc.PID.
func TestKernel_EmitScriptEvent_WritesToDisk(t *testing.T) {
	kern, proc := newSkipReasonLoopKernelProc(t)

	baseDir := t.TempDir()
	ew, err := NewEventWriter(baseDir, proc.UUID)
	if err != nil {
		t.Fatalf("NewEventWriter: %v", err)
	}
	t.Cleanup(func() { _ = ew.Close() })

	proc.AttachEventWriter(ew)

	kern.EmitScriptEvent(proc, "ScriptStmtBegin", map[string]any{
		"line":      42,
		"stmt_kind": "spawn",
		"intent":    "say hi",
	})
	kern.EmitScriptEvent(proc, "ScriptSpawn", map[string]any{
		"line":   42,
		"intent": "say hi",
		"agent":  "",
		"model":  "",
	})

	// Force buffer flush so the read below sees the bytes — Close also
	// flushes, but in production EventWriter.WriteEvent already calls
	// Flush per write. Either way we want the on-disk state.
	if err := ew.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	path := filepath.Join(baseDir, "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("events.jsonl not created at %s: %v", path, err)
	}

	rows, err := ReadAllEvents(path)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	first := rows[0]
	if first.Syscall != "ScriptStmtBegin" {
		t.Errorf("rows[0].Syscall = %q, want ScriptStmtBegin", first.Syscall)
	}
	if uint64(first.PID) != uint64(proc.PID) {
		t.Errorf("rows[0].PID = %d, want %d", first.PID, proc.PID)
	}
	if got, _ := first.Args["stmt_kind"].(string); got != "spawn" {
		t.Errorf("rows[0].Args[stmt_kind] = %q, want \"spawn\"", got)
	}
	// "line" is the canonical key per spec — JSON numbers decode as float64.
	if line, ok := first.Args["line"].(float64); !ok || int(line) != 42 {
		t.Errorf("rows[0].Args[line] = %v (type %T), want 42", first.Args["line"], first.Args["line"])
	}

	second := rows[1]
	if second.Syscall != "ScriptSpawn" {
		t.Errorf("rows[1].Syscall = %q, want ScriptSpawn", second.Syscall)
	}
	if got, _ := second.Args["intent"].(string); got != "say hi" {
		t.Errorf("rows[1].Args[intent] = %q, want \"say hi\"", got)
	}
}

// --- AC#6 — proc without an EventWriter is silent (no panic, no error) ---

// TestKernel_EmitScriptEvent_NoEventWriter_Silent asserts the spec's
// "EventWriter init failure must not block script execution" guarantee.
// The emit path already gates on `ew != nil` (observe.go:55); this test
// pins that contract so a future refactor cannot remove the guard
// without failing CI.
func TestKernel_EmitScriptEvent_NoEventWriter_Silent(t *testing.T) {
	kern, proc := newSkipReasonLoopKernelProc(t)

	// Intentionally DO NOT AttachEventWriter — simulate the
	// "EventWriter init failed; log warn and keep going" branch.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("EmitScriptEvent panicked with no EventWriter attached: %v", r)
		}
	}()

	kern.EmitScriptEvent(proc, "ScriptStmtBegin", map[string]any{
		"line":      1,
		"stmt_kind": "export",
	})
	// Two emits in a row — first proved no panic, second pins the
	// invariant that the no-writer branch isn't a one-time bypass.
	kern.EmitScriptEvent(proc, "ScriptStmtEnd", map[string]any{
		"line":      1,
		"stmt_kind": "export",
		"error":     "",
	})
}

// --- AC#6 (defensive) — proc=nil is a no-op, not a panic ---

// TestKernel_EmitScriptEvent_NilProc_NoPanic locks down the spec note
// (43-2 Task 3.3: "EmitScriptEvent must `proc != nil` guard"). Without
// this guard, a caller that races with Reap would dereference a nil
// pointer inside emitEvent's proc.CheckBreakpoint call.
func TestKernel_EmitScriptEvent_NilProc_NoPanic(t *testing.T) {
	kern, _ := newSkipReasonLoopKernelProc(t)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("EmitScriptEvent(nil, …) panicked: %v", r)
		}
	}()
	kern.EmitScriptEvent(nil, "ScriptStmtBegin", map[string]any{"line": 1})
}
