package shell

import (
	"context"
	"strings"
	"testing"
)

// ============================================================
// Story 43.2: Trace overhead benchmark (AC#5) — green-phase
//
// AC#5: P95 single-statement overhead with hook installed must be
// ≤ 5ms above the no-hook baseline. EventWriter writes ≈ 50–200µs
// per event in production; the benchmark is mostly proving the
// hook call chain + map allocation does not balloon the cost.
//
// Re-run with:
//   go test -bench=. -benchmem ./shell/...
// ============================================================

const benchScript = `
export A0=0
export A1=1
export A2=2
export A3=3
export A4=4
export A5=5
export A6=6
export A7=7
export A8=8
export A9=9
` // 10 statements; loop the runner to amortise parse cost across N iterations.

func benchExecute(b *testing.B, attachHook bool, hookFn func(ScriptEvent)) {
	b.Helper()
	script, err := ParseScript(strings.Repeat(benchScript, 10)) // 100 statements
	if err != nil {
		b.Fatalf("ParseScript: %v", err)
	}
	spawner := &mockSpawner{}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		exec := NewScriptExecutor(spawner, NewEnvironment())
		if attachHook {
			exec.OnEvent = hookFn
		}
		if _, err := exec.Execute(ctx, script); err != nil {
			b.Fatalf("Execute: %v", err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/100.0, "ns/stmt")
}

// BenchmarkScriptExecutor_NoOnEvent is the baseline — no hook installed.
// Anything beyond Go map alloc + env.Expand is suspect.
func BenchmarkScriptExecutor_NoOnEvent(b *testing.B) {
	benchExecute(b, false, nil)
}

// BenchmarkScriptExecutor_WithOnEvent_NoWrite exercises the hook call chain
// with a no-op observer. Delta vs. baseline = pure framework overhead.
// Story AC#5 demands "OnEvent=nil zero overhead (same as baseline ±1µs)";
// this benchmark proves the inverse direction (overhead with hook attached).
func BenchmarkScriptExecutor_WithOnEvent_NoWrite(b *testing.B) {
	var sink ScriptEventKind
	benchExecute(b, true, func(ev ScriptEvent) {
		sink = ev.Kind
		_ = sink
	})
}

// BenchmarkScriptExecutor_WithOnEvent_HotPath simulates a realistic observer
// that touches Meta (the shape we'll send to kernel.EmitScriptEvent in
// production). Used to size the daemon-side cost before EventWriter writes.
func BenchmarkScriptExecutor_WithOnEvent_HotPath(b *testing.B) {
	var sink any
	benchExecute(b, true, func(ev ScriptEvent) {
		sink = ev.Meta["stmt_kind"]
		_ = sink
	})
}
