package shell

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 18.4: Spawn 返回值捕获与并行执行
//
// Tests reference StmtParallel, ParallelBlock, Statement.Parallel
// — which do NOT exist yet → compile failure = RED phase.
//
// concurrentMockSpawner is a thread-safe spawner for parallel tests.
// mockSpawner (from pipe_test.go) is NOT safe for concurrent use.
// ============================================================

// concurrentMockSpawner is thread-safe for parallel test scenarios.
type concurrentMockSpawner struct {
	mu      sync.Mutex
	calls   []mockCall
	results map[string]mockResult
	// ordered results for tests that need deterministic ordering
	orderedResults []mockResult
	callIdx        int32 // atomic counter for ordered results
}

func (m *concurrentMockSpawner) SpawnAndWait(_ context.Context, intent, agent, model string) (string, int, int, error) {
	m.mu.Lock()
	m.calls = append(m.calls, mockCall{intent: intent, agent: agent, model: model})
	m.mu.Unlock()

	if m.results != nil {
		if r, ok := m.results[intent]; ok {
			return r.result, r.exitCode, r.tokens, r.err
		}
	}

	if m.orderedResults != nil {
		idx := int(atomic.AddInt32(&m.callIdx, 1)) - 1
		if idx < len(m.orderedResults) {
			r := m.orderedResults[idx]
			return r.result, r.exitCode, r.tokens, r.err
		}
	}

	return "", 0, 0, nil
}

func (m *concurrentMockSpawner) Wait(_ context.Context, _ int) (int, error) {
	return 0, fmt.Errorf("concurrentMockSpawner: Wait not implemented")
}

func (m *concurrentMockSpawner) getCalls() []mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]mockCall, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// ========================= PARSING ===========================

// --- 18.4-UNIT-001: [P0] ParseScript parallel 块基本解析（3 个 spawn）(AC2) ---

func TestParseScript_Parallel_BasicBlock(t *testing.T) {
	input := "parallel\n  spawn \"任务A\"\n  spawn \"任务B\"\n  spawn \"任务C\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtParallel {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtParallel)
	}
	if stmt.Parallel == nil {
		t.Fatal("Parallel should not be nil")
	}
	if len(stmt.Parallel.Body) != 3 {
		t.Fatalf("parallel body len = %d, want 3", len(stmt.Parallel.Body))
	}
	for i, s := range stmt.Parallel.Body {
		if s.Kind != StmtSpawn {
			t.Errorf("body[%d] kind = %q, want %q", i, s.Kind, StmtSpawn)
		}
	}
}

// --- 18.4-UNIT-002: [P0] ParseScript parallel 块带赋值 spawn (AC5) ---

func TestParseScript_Parallel_WithAssignment(t *testing.T) {
	input := "parallel\n  r1 = spawn \"分析代码\" --agent=analyst\n  r2 = spawn \"审查架构\" --agent=reviewer\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtParallel {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtParallel)
	}
	if len(stmt.Parallel.Body) != 2 {
		t.Fatalf("body len = %d, want 2", len(stmt.Parallel.Body))
	}
	if stmt.Parallel.Body[0].Assign != "r1" {
		t.Errorf("body[0].Assign = %q, want %q", stmt.Parallel.Body[0].Assign, "r1")
	}
	if stmt.Parallel.Body[1].Assign != "r2" {
		t.Errorf("body[1].Assign = %q, want %q", stmt.Parallel.Body[1].Assign, "r2")
	}
}

// --- 18.4-UNIT-003: [P1] ParseScript parallel 块带 on-error (AC6) ---

func TestParseScript_Parallel_WithOnError(t *testing.T) {
	input := "parallel\n  r1 = spawn \"分析\" on-error spawn \"回退分析\"\n  spawn \"审查\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtParallel {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtParallel)
	}
	if stmt.Parallel.Body[0].OnError == nil {
		t.Error("body[0] should have OnError handler")
	}
}

// --- 18.4-UNIT-004: [P1] ParseScript parallel 块含 pipeline (AC7) ---

func TestParseScript_Parallel_WithPipeline(t *testing.T) {
	input := "parallel\n  spawn \"任务A\"\n  spawn \"分析\" | spawn \"总结\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtParallel {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtParallel)
	}
	if len(stmt.Parallel.Body) != 2 {
		t.Fatalf("body len = %d, want 2", len(stmt.Parallel.Body))
	}
	if stmt.Parallel.Body[1].Kind != StmtPipeline {
		t.Errorf("body[1] kind = %q, want %q", stmt.Parallel.Body[1].Kind, StmtPipeline)
	}
}

// --- 18.4-UNIT-005: [P1] ParseScript 空 parallel 块（合法 no-op）(AC10) ---

func TestParseScript_Parallel_Empty(t *testing.T) {
	input := "parallel\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtParallel {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtParallel)
	}
	if stmt.Parallel == nil {
		t.Fatal("Parallel should not be nil")
	}
	if len(stmt.Parallel.Body) != 0 {
		t.Errorf("body len = %d, want 0 (empty parallel is no-op)", len(stmt.Parallel.Body))
	}
}

// --- 18.4-UNIT-006: [P0] ParseScript 错误 — parallel 无 end (AC9) ---

func TestParseScript_Error_ParallelUnclosed(t *testing.T) {
	input := "parallel\n  spawn \"任务A\""
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for unclosed parallel block")
	}
	if !strings.Contains(err.Error(), "parallel") || !strings.Contains(err.Error(), "end") {
		t.Errorf("error should mention parallel/end, got: %q", err.Error())
	}
}

// --- 18.4-UNIT-007: [P0] ParseScript 错误 — parallel 内含 export (AC9) ---

func TestParseScript_Error_ParallelInvalidContent_Export(t *testing.T) {
	input := "parallel\n  export KEY=val\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for export inside parallel block")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Errorf("error should mention parallel, got: %q", err.Error())
	}
}

// --- 18.4-UNIT-008: [P0] ParseScript 错误 — parallel 内含 if (AC9) ---

func TestParseScript_Error_ParallelInvalidContent_If(t *testing.T) {
	input := "parallel\n  if $x == 1\n    spawn \"A\"\n  end\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for if inside parallel block")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Errorf("error should mention parallel, got: %q", err.Error())
	}
}

// --- 18.4-UNIT-009: [P0] ParseScript 错误 — parallel 内含 for (AC9) ---

func TestParseScript_Error_ParallelInvalidContent_For(t *testing.T) {
	input := "parallel\n  for x in [a, b]\n    spawn \"${x}\"\n  end\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for for inside parallel block")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Errorf("error should mention parallel, got: %q", err.Error())
	}
}

// --- 18.4-UNIT-010: [P0] ParseScript 错误 — parallel 内含 fn call (AC9) ---

func TestParseScript_Error_ParallelInvalidContent_FnCall(t *testing.T) {
	input := "fn myFunc()\n  spawn \"A\"\nend\nparallel\n  myFunc()\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for fn call inside parallel block")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Errorf("error should mention parallel, got: %q", err.Error())
	}
}

// --- 18.4-UNIT-011: [P1] ParseScript 错误 — 嵌套 parallel ---

func TestParseScript_Error_ParallelNested(t *testing.T) {
	input := "parallel\n  parallel\n    spawn \"A\"\n  end\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for nested parallel block")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Errorf("error should mention parallel, got: %q", err.Error())
	}
}

// ========================= EXECUTION ==========================

// --- 18.4-UNIT-012: [P0] ScriptExecutor parallel 三个 spawn 全成功 (AC2) ---

func TestScriptExecutor_Parallel_AllSucceed(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"任务A": {result: "结果A", exitCode: 0, tokens: 100},
			"任务B": {result: "结果B", exitCode: 0, tokens: 200},
			"任务C": {result: "结果C", exitCode: 0, tokens: 300},
		},
	}

	input := "parallel\n  spawn \"任务A\"\n  spawn \"任务B\"\n  spawn \"任务C\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if result.TotalTokens != 600 {
		t.Errorf("TotalTokens = %d, want 600", result.TotalTokens)
	}
}

// --- 18.4-UNIT-013: [P0] ScriptExecutor parallel 赋值 spawn — env 和 captures (AC1, AC5) ---

func TestScriptExecutor_Parallel_Assignment(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"分析代码": {result: "分析报告", exitCode: 0, tokens: 150},
			"审查架构": {result: "架构报告", exitCode: 0, tokens: 250},
		},
	}

	input := "parallel\n  r1 = spawn \"分析代码\"\n  r2 = spawn \"审查架构\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	r1, ok := env.Get("r1")
	if !ok || r1 != "分析报告" {
		t.Errorf("r1 = %q (exists=%v), want %q", r1, ok, "分析报告")
	}
	r2, ok := env.Get("r2")
	if !ok || r2 != "架构报告" {
		t.Errorf("r2 = %q (exists=%v), want %q", r2, ok, "架构报告")
	}
}

// --- 18.4-UNIT-014: [P0] ScriptExecutor parallel 一个失败不影响其他 (AC3) ---

func TestScriptExecutor_Parallel_OneFails_OthersContinue(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"任务A": {result: "成功A", exitCode: 0, tokens: 100},
			"任务B": {result: "失败B", exitCode: 1, tokens: 50},
			"任务C": {result: "成功C", exitCode: 0, tokens: 100},
		},
	}

	input := "parallel\n  r1 = spawn \"任务A\"\n  r2 = spawn \"任务B\"\n  r3 = spawn \"任务C\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3 (all should execute despite failure)", len(calls))
	}

	r1, _ := env.Get("r1")
	if r1 != "成功A" {
		t.Errorf("r1 = %q, want %q", r1, "成功A")
	}
	r3, _ := env.Get("r3")
	if r3 != "成功C" {
		t.Errorf("r3 = %q, want %q", r3, "成功C")
	}

	if result.TotalTokens != 250 {
		t.Errorf("TotalTokens = %d, want 250", result.TotalTokens)
	}
}

// --- 18.4-UNIT-015: [P0] ScriptExecutor parallel on-error handler (AC6) ---

func TestScriptExecutor_Parallel_OnError(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"主分析":  {result: "分析失败", exitCode: 1, tokens: 50},
			"回退分析": {result: "回退成功", exitCode: 0, tokens: 75},
			"审查":   {result: "审查OK", exitCode: 0, tokens: 100},
		},
	}

	input := "parallel\n  r1 = spawn \"主分析\" on-error spawn \"回退分析\"\n  r2 = spawn \"审查\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	r1, _ := env.Get("r1")
	if r1 != "回退成功" {
		t.Errorf("r1 = %q, want %q (on-error result should override)", r1, "回退成功")
	}
	r2, _ := env.Get("r2")
	if r2 != "审查OK" {
		t.Errorf("r2 = %q, want %q", r2, "审查OK")
	}

	// on-error token accumulation: 主分析(50) + 回退分析(75) + 审查(100) = 225
	if result.TotalTokens != 225 {
		t.Errorf("TotalTokens = %d, want 225 (50+75 on-error + 100)", result.TotalTokens)
	}
}

// --- 18.4-UNIT-016: [P1] ScriptExecutor parallel 全部失败 ---

func TestScriptExecutor_Parallel_AllFail(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"任务A": {result: "失败A", exitCode: 1, tokens: 50},
			"任务B": {result: "失败B", exitCode: 2, tokens: 50},
		},
	}

	input := "parallel\n  r1 = spawn \"任务A\"\n  r2 = spawn \"任务B\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	r1, _ := env.Get("r1")
	if r1 != "失败A" {
		t.Errorf("r1 = %q, want %q", r1, "失败A")
	}
	r2, _ := env.Get("r2")
	if r2 != "失败B" {
		t.Errorf("r2 = %q, want %q", r2, "失败B")
	}
}

// --- 18.4-UNIT-017: [P0] ScriptExecutor parallel LastResult 为声明顺序最后一个 ---

func TestScriptExecutor_Parallel_LastResult_DeclarationOrder(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"任务A": {result: "结果A", exitCode: 0, tokens: 100},
			"任务B": {result: "结果B", exitCode: 0, tokens: 100},
			"任务C": {result: "结果C", exitCode: 0, tokens: 100},
		},
	}

	input := "parallel\n  spawn \"任务A\"\n  spawn \"任务B\"\n  spawn \"任务C\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.LastResult != "结果C" {
		t.Errorf("LastResult = %q, want %q (last in declaration order)", result.LastResult, "结果C")
	}
}

// --- 18.4-UNIT-018: [P0] ScriptExecutor parallel token 汇总 ---

func TestScriptExecutor_Parallel_TokenAggregation(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"任务A": {result: "A", exitCode: 0, tokens: 111},
			"任务B": {result: "B", exitCode: 0, tokens: 222},
			"任务C": {result: "C", exitCode: 0, tokens: 333},
		},
	}

	input := "parallel\n  spawn \"任务A\"\n  spawn \"任务B\"\n  spawn \"任务C\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.TotalTokens != 666 {
		t.Errorf("TotalTokens = %d, want 666", result.TotalTokens)
	}
}

// --- 18.4-UNIT-019: [P0] ScriptExecutor parallel 后 if $r.exitcode 条件 (AC5) ---

func TestScriptExecutor_Parallel_CapturedResult_Condition(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"分析代码":      {result: "分析完成", exitCode: 0, tokens: 100},
			"分析完成，继续处理": {result: "处理OK", exitCode: 0, tokens: 50},
		},
	}

	input := "parallel\n  r = spawn \"分析代码\"\nend\nif $r.exitcode == 0\n  spawn \"分析完成，继续处理\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2 (parallel spawn + conditional spawn)", len(calls))
	}
}

// --- 18.4-UNIT-020: [P0] ScriptExecutor parallel intent 中 ${var} 展开 ---

func TestScriptExecutor_Parallel_IntentExpansion(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"分析 main.go": {result: "OK", exitCode: 0, tokens: 100},
			"审查 main.go": {result: "OK", exitCode: 0, tokens: 100},
		},
	}

	input := "export target=main.go\nparallel\n  spawn \"分析 ${target}\"\n  spawn \"审查 ${target}\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}

	foundAnalyze := false
	foundReview := false
	for _, c := range calls {
		if c.intent == "分析 main.go" {
			foundAnalyze = true
		}
		if c.intent == "审查 main.go" {
			foundReview = true
		}
	}
	if !foundAnalyze {
		t.Error("expected call with intent '分析 main.go'")
	}
	if !foundReview {
		t.Error("expected call with intent '审查 main.go'")
	}
}

// --- 18.4-UNIT-021: [P0] ScriptExecutor parallel ExpandStrict 未定义变量 → 含行号错误 (AC8) ---

func TestScriptExecutor_Parallel_UndefinedVar_Error(t *testing.T) {
	spawner := &concurrentMockSpawner{}

	input := "parallel\n  spawn \"${undefined_var} 分析\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for undefined variable in parallel intent")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "undefined_var") {
		t.Errorf("error should mention 'undefined_var', got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "line") || !strings.Contains(errMsg, "2") {
		t.Errorf("error should mention line number 2, got: %q", errMsg)
	}
}

// --- 18.4-UNIT-022: [P0] ScriptExecutor parallel 空块 no-op (AC10) ---

func TestScriptExecutor_Parallel_Empty(t *testing.T) {
	spawner := &concurrentMockSpawner{}

	input := "parallel\nend\nspawn \"后续任务\""
	spawner.results = map[string]mockResult{
		"后续任务": {result: "OK", exitCode: 0, tokens: 50},
	}
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1 (empty parallel no-op, then subsequent spawn)", len(calls))
	}
	if calls[0].intent != "后续任务" {
		t.Errorf("call[0].intent = %q, want %q", calls[0].intent, "后续任务")
	}
}

// --- 18.4-UNIT-023: [P0] ScriptExecutor parallel context 取消传播 ---

func TestScriptExecutor_Parallel_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"任务A": {result: "A", exitCode: 0, tokens: 100},
		},
	}

	input := "parallel\n  spawn \"任务A\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(ctx, script)
	if err == nil {
		t.Fatal("expected error due to cancelled context")
	}
}

// --- 18.4-UNIT-024: [P0] ScriptExecutor parallel 含 pipeline (AC7) ---

func TestScriptExecutor_Parallel_Pipeline(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"独立任务": {result: "独立OK", exitCode: 0, tokens: 100},
			"分析":   {result: "分析报告", exitCode: 0, tokens: 150},
		},
	}

	input := "parallel\n  spawn \"独立任务\"\n  spawn \"分析\" | spawn \"总结\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) < 2 {
		t.Errorf("calls = %d, want >= 2 (spawn + pipeline stages)", len(calls))
	}
}

// --- 18.4-UNIT-025: [P0] countStagesInBlock parallel 内 spawn 独立计数 ---

func TestScriptExecutor_Parallel_StageCount(t *testing.T) {
	input := "parallel\n  spawn \"A\"\n  spawn \"B\"\n  spawn \"C\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	count := countExecutableStages(script)
	if count != 3 {
		t.Errorf("stage count = %d, want 3 (each spawn in parallel counts independently)", count)
	}
}

// ======================= COMBINATIONS =========================

// --- 18.4-COMB-001: [P0] ScriptExecutor for 循环内使用 parallel 块 ---

func TestScriptExecutor_Parallel_InForLoop(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"分析 a.go": {result: "A-OK", exitCode: 0, tokens: 50},
			"审查 a.go": {result: "A-审查OK", exitCode: 0, tokens: 50},
			"分析 b.go": {result: "B-OK", exitCode: 0, tokens: 50},
			"审查 b.go": {result: "B-审查OK", exitCode: 0, tokens: 50},
		},
	}

	input := "for f in [a.go, b.go]\n  parallel\n    spawn \"分析 ${f}\"\n    spawn \"审查 ${f}\"\n  end\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 4 {
		t.Fatalf("calls = %d, want 4 (2 files * 2 parallel spawns)", len(calls))
	}
}

// --- 18.4-COMB-002: [P0] ScriptExecutor 函数内使用 parallel 块 ---

func TestScriptExecutor_Parallel_InFunction(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"分析 target.go": {result: "分析OK", exitCode: 0, tokens: 100},
			"审查 target.go": {result: "审查OK", exitCode: 0, tokens: 100},
		},
	}

	input := "fn analyze(file)\n  parallel\n    spawn \"分析 ${file}\"\n    spawn \"审查 ${file}\"\n  end\nend\nanalyze(target.go)"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
}

// --- 18.4-COMB-003: [P1] ScriptExecutor parallel 前数组/映射赋值 + intent 展开 ---

func TestScriptExecutor_Parallel_AfterDataStructures(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"分析 main.go": {result: "OK", exitCode: 0, tokens: 100},
			"使用 sonnet":  {result: "OK", exitCode: 0, tokens: 100},
		},
	}

	input := "files = [\"main.go\", \"lib.go\"]\nconfig = {model: \"sonnet\"}\nparallel\n  spawn \"分析 ${files[0]}\"\n  spawn \"使用 ${config.model}\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}

	foundFile := false
	foundModel := false
	for _, c := range calls {
		if c.intent == "分析 main.go" {
			foundFile = true
		}
		if c.intent == "使用 sonnet" {
			foundModel = true
		}
	}
	if !foundFile {
		t.Error("expected call with intent '分析 main.go' (array index expanded)")
	}
	if !foundModel {
		t.Error("expected call with intent '使用 sonnet' (map property expanded)")
	}
}

// --- 18.4-COMB-004: [P1] ScriptExecutor parallel 结果用于 while 条件 ---

func TestScriptExecutor_Parallel_ResultInWhileCondition(t *testing.T) {
	// Use a spawner that returns exitcode=1 on first call, then exitcode=0 on second.
	// This validates while loop re-evaluates the parallel result each iteration.
	callNum := int32(0)
	spawner := &whileTestSpawner{callNum: &callNum}

	// while $status.exitcode != 0: re-run parallel until spawn succeeds
	input := "parallel\n  status = spawn \"检查状态\"\nend\nwhile $status.exitcode != 0\n  parallel\n    status = spawn \"检查状态\"\n  end\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	got := atomic.LoadInt32(&callNum)
	if got != 2 {
		t.Fatalf("calls = %d, want 2 (first parallel fail + while-body parallel succeed)", got)
	}
}

// whileTestSpawner returns exitcode=1 on first call, exitcode=0 on second+.
type whileTestSpawner struct {
	callNum *int32
}

func (s *whileTestSpawner) SpawnAndWait(_ context.Context, _, _, _ string) (string, int, int, error) {
	n := atomic.AddInt32(s.callNum, 1)
	if n == 1 {
		return "pending", 1, 50, nil
	}
	return "done", 0, 50, nil
}

func (s *whileTestSpawner) Wait(_ context.Context, _ int) (int, error) {
	return 0, fmt.Errorf("whileTestSpawner: Wait not implemented")
}

// ======================== RACE TEST ===========================

// --- 18.4-RACE-001: [P0] parallel 执行无数据竞争 (go test -race) ---

func TestScriptExecutor_Parallel_NoRace(t *testing.T) {
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"A": {result: "rA", exitCode: 0, tokens: 10},
			"B": {result: "rB", exitCode: 0, tokens: 20},
			"C": {result: "rC", exitCode: 0, tokens: 30},
			"D": {result: "rD", exitCode: 0, tokens: 40},
			"E": {result: "rE", exitCode: 0, tokens: 50},
		},
	}

	input := "parallel\n  r1 = spawn \"A\"\n  r2 = spawn \"B\"\n  r3 = spawn \"C\"\n  r4 = spawn \"D\"\n  r5 = spawn \"E\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", result.TotalTokens)
	}

	for _, name := range []string{"r1", "r2", "r3", "r4", "r5"} {
		if _, ok := env.Get(name); !ok {
			t.Errorf("expected variable %q to be set", name)
		}
	}
}

// ======================= PERFORMANCE ==========================

// --- 18.4-PERF-001: [P0] parallel 解析性能 <= 50ms (NFR38) ---

func TestParseScript_Parallel_Performance_NFR38(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("export base=project\n")
	for i := range 10 {
		sb.WriteString("parallel\n")
		for j := range 5 {
			fmt.Fprintf(&sb, "  spawn \"任务%d-%d\"\n", i, j)
		}
		sb.WriteString("end\n")
	}

	input := sb.String()
	start := time.Now()
	_, err := ParseScript(input)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("parse took %v, want <= 50ms (NFR38)", elapsed)
	}
}

// Ensure sync and time packages are used
var (
	_ = sync.Mutex{}
	_ = time.Duration(0)
	_ = atomic.Int32{}
)
