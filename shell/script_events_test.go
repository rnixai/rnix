package shell

import (
	"context"
	"maps"
	"strings"
	"sync/atomic"
	"testing"
)

// ============================================================
// Story 43.2: ScriptExecutor Trace Events (OBS-1) — green-phase tests
//
// Mapping to Story 43.2 ACs:
//   AC#1 — ScriptExecutor exposes OnEvent hook + ScriptEvent type
//   AC#2 — Five first-class event Kinds emitted at the right call sites
//   AC#5 — Hook nil path is zero-overhead (see *_bench_test.go)
// ============================================================

// onEventRecorder captures every ScriptEvent the executor emits.
// Safe for the single-goroutine ScriptExecutor.Execute path; concurrent
// emits inside StmtParallel are guarded by sync/atomic semantics in
// production hook callers and aren't exercised here.
type onEventRecorder struct {
	events []ScriptEvent
}

func (r *onEventRecorder) record(ev ScriptEvent) {
	// Defensive copy of Meta so later mutations (if any) don't bleed into
	// the recorded slice — the production hook contract does NOT promise
	// Meta is immutable after delivery.
	meta := make(map[string]any, len(ev.Meta))
	maps.Copy(meta, ev.Meta)
	ev.Meta = meta
	r.events = append(r.events, ev)
}

func (r *onEventRecorder) filterByKind(k ScriptEventKind) []ScriptEvent {
	out := make([]ScriptEvent, 0, len(r.events))
	for _, ev := range r.events {
		if ev.Kind == k {
			out = append(out, ev)
		}
	}
	return out
}

// --- AC#2 — every statement emits a matched begin/end pair, spawn emits ScriptSpawn ---

// TestScriptExecutor_OnEvent_EmitsBeginEndPerStatement: 3 exports + 1 spawn
// must yield 4× ScriptStmtBegin paired with 4× ScriptStmtEnd, plus 1×
// ScriptSpawn emitted BEFORE the spawn-end pair (call-site ordering).
func TestScriptExecutor_OnEvent_EmitsBeginEndPerStatement(t *testing.T) {
	script, err := ParseScript(`
export A=1
export B=2
export C=3
spawn "hello"
`)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	spawner := &mockSpawner{
		results: []mockResult{{result: "ok", exitCode: 0, tokens: 0}},
	}
	rec := &onEventRecorder{}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	exec.OnEvent = rec.record

	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	begins := rec.filterByKind(ScriptStmtBegin)
	ends := rec.filterByKind(ScriptStmtEnd)
	spawns := rec.filterByKind(ScriptSpawn)

	if len(begins) != 4 {
		t.Errorf("ScriptStmtBegin count = %d, want 4 (3 exports + 1 spawn)", len(begins))
	}
	if len(ends) != 4 {
		t.Errorf("ScriptStmtEnd count = %d, want 4 (one per statement, error path included)", len(ends))
	}
	if len(spawns) != 1 {
		t.Fatalf("ScriptSpawn count = %d, want exactly 1", len(spawns))
	}

	sp := spawns[0]
	if got, _ := sp.Meta["intent"].(string); got != "hello" {
		t.Errorf("ScriptSpawn.Meta[intent] = %q, want \"hello\"", got)
	}

	// ScriptSpawn must precede the corresponding ScriptStmtEnd of the spawn
	// statement so Timeline can show "we entered spawn, then waited."
	var sawSpawn, sawSpawnEnd bool
	for _, ev := range rec.events {
		switch ev.Kind {
		case ScriptSpawn:
			sawSpawn = true
		case ScriptStmtEnd:
			if kind, _ := ev.Meta["stmt_kind"].(string); kind == string(StmtSpawn) {
				if !sawSpawn {
					t.Errorf("ScriptStmtEnd(stmt_kind=spawn) emitted before ScriptSpawn — call-site order violated")
				}
				sawSpawnEnd = true
			}
		}
	}
	if !sawSpawnEnd {
		t.Errorf("no ScriptStmtEnd carrying stmt_kind=spawn was emitted")
	}
}

// --- AC#1 — OnEvent=nil is zero-overhead (no side effects, no panic) ---

// TestScriptExecutor_OnEvent_NilHookProducesNoEvents asserts the fast-path
// guard exists: with OnEvent==nil the executor must not crash and must not
// (visibly) call into any hook. Companion benchmark in script_events_bench_test.go
// validates the perf side of the same contract.
func TestScriptExecutor_OnEvent_NilHookProducesNoEvents(t *testing.T) {
	script, err := ParseScript(`
export A=1
export B=2
`)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	spawner := &mockSpawner{}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	// OnEvent intentionally left nil — must not panic when emit helpers fire.

	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("Execute(nil OnEvent): %v", err)
	}
}

// --- AC#2 — while loop emits ScriptWhileIter per iteration with 1-based index ---

// TestScriptExecutor_OnEvent_WhileIterEmitsCorrectIteration runs a while loop
// 5 times by mutating a counter via assignment spawns, then asserts iteration
// values 1..5 surface in ScriptWhileIter.Meta["iteration"].
//
// Loop shape (note: unquoted RHS — the script parser does NOT strip quotes
// from string literals, so `!= "5"` would compare against the literal three
// characters `"5"` and never break):
//
//	export N=0
//	while $N != 5
//	  N = spawn "increment"       # mock returns 1, 2, 3, 4, 5 on each call
//	end
func TestScriptExecutor_OnEvent_WhileIterEmitsCorrectIteration(t *testing.T) {
	script, err := ParseScript(`
export N=0
while $N != 5
N = spawn "increment"
end
`)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	spawner := &mockSpawner{
		results: []mockResult{
			{result: "1", exitCode: 0},
			{result: "2", exitCode: 0},
			{result: "3", exitCode: 0},
			{result: "4", exitCode: 0},
			{result: "5", exitCode: 0},
		},
	}
	rec := &onEventRecorder{}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	exec.OnEvent = rec.record

	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	iters := rec.filterByKind(ScriptWhileIter)
	if len(iters) != 5 {
		t.Fatalf("ScriptWhileIter count = %d, want 5", len(iters))
	}
	for i, ev := range iters {
		got, ok := ev.Meta["iteration"]
		if !ok {
			t.Errorf("iter[%d]: Meta[iteration] missing", i)
			continue
		}
		// Accept any numeric encoding (int/int64/float64) — Story spec only
		// requires "1-based" semantics, not a specific Go integer type.
		want := i + 1
		switch v := got.(type) {
		case int:
			if v != want {
				t.Errorf("iter[%d].iteration = %d, want %d", i, v, want)
			}
		case int64:
			if int(v) != want {
				t.Errorf("iter[%d].iteration = %d, want %d", i, v, want)
			}
		case float64:
			if int(v) != want {
				t.Errorf("iter[%d].iteration = %v, want %d", i, v, want)
			}
		default:
			t.Errorf("iter[%d].iteration has unexpected type %T", i, got)
		}
	}
}

// --- AC#1 — hook panic must NOT break execution (recover semantics) ---

// TestScriptExecutor_OnEvent_PanicInHookDoesNotBreakExecution asserts the
// emit helper installs a defer-recover so a faulty observer cannot brick
// the script-runner. The script must still complete (1 spawn → exit 0).
func TestScriptExecutor_OnEvent_PanicInHookDoesNotBreakExecution(t *testing.T) {
	script, err := ParseScript(`
spawn "hello"
`)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	spawner := &mockSpawner{
		results: []mockResult{{result: "ok", exitCode: 0}},
	}
	var hookCalls atomic.Int32
	exec := NewScriptExecutor(spawner, NewEnvironment())
	exec.OnEvent = func(ev ScriptEvent) {
		hookCalls.Add(1)
		panic("intentional hook panic — observer should be isolated")
	}

	result, err := exec.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("Execute returned error after hook panic: %v", err)
	}
	if result == nil || result.LastResult != "ok" {
		t.Errorf("script result corrupted by hook panic: %+v", result)
	}
	if hookCalls.Load() == 0 {
		t.Errorf("hook was never called — emit path may be wired incorrectly")
	}
}

// --- AC#2 — ScriptCondition meta carries condition / left / result fields ---

// TestScriptExecutor_OnEvent_ConditionMetaShape exercises both an `if` and a
// `while` condition and asserts both routes emit ScriptCondition with the
// trio of fields required by Story spec AC#2 table row 5.
//
// RHS values are unquoted — see TestScriptExecutor_OnEvent_WhileIterEmitsCorrectIteration
// for the parser quirk this avoids.
func TestScriptExecutor_OnEvent_ConditionMetaShape(t *testing.T) {
	script, err := ParseScript(`
export X=ready
if $X == ready
export Y=go
end
export N=0
while $N != 1
N = spawn "once"
end
`)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	spawner := &mockSpawner{
		results: []mockResult{
			{result: "1", exitCode: 0},
		},
	}
	rec := &onEventRecorder{}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	exec.OnEvent = rec.record

	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	conds := rec.filterByKind(ScriptCondition)
	if len(conds) < 2 {
		// >=2 because while emits one Condition per pass (true on iter 1, false on iter 2).
		t.Fatalf("ScriptCondition count = %d, want >= 2 (1 if + at least 1 while)", len(conds))
	}
	for i, ev := range conds {
		required := []string{"condition", "left", "result"}
		for _, key := range required {
			if _, ok := ev.Meta[key]; !ok {
				t.Errorf("cond[%d].Meta[%s] missing (have keys: %v)", i, key, mapKeys(ev.Meta))
			}
		}
		if _, ok := ev.Meta["result"].(bool); !ok {
			t.Errorf("cond[%d].Meta[result] type = %T, want bool", i, ev.Meta["result"])
		}
		// Sanity: "condition" should at least contain the operator text so
		// Timeline can render "$X == \"ready\"" instead of a blob.
		if condStr, _ := ev.Meta["condition"].(string); !strings.ContainsAny(condStr, "=!") {
			t.Errorf("cond[%d].Meta[condition] = %q does not look textualised", i, condStr)
		}
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
