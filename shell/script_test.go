package shell

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 11.2: 变量与环境传递
//
// Tests reference ParseScript, Script, Statement, StatementKind,
// StmtExport, StmtSpawn, StmtPipeline, ExportStmt,
// ScriptExecutor, NewScriptExecutor, ScriptResult
// — which do NOT exist yet → compile failure = RED phase.
//
// mockSpawner is reused from pipe_test.go (same package).
// ============================================================

// --- 11.2-UNIT-011: [P0] ParseScript 单行 export (AC1) ---

func TestParseScript_SingleExport(t *testing.T) {
	script, err := ParseScript("export TARGET=./src/auth.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtExport {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtExport)
	}
	if stmt.Export == nil {
		t.Fatal("Export should not be nil")
	}
	if stmt.Export.Key != "TARGET" {
		t.Errorf("key = %q, want %q", stmt.Export.Key, "TARGET")
	}
	if stmt.Export.Value != "./src/auth.go" {
		t.Errorf("value = %q, want %q", stmt.Export.Value, "./src/auth.go")
	}
}

func TestParseScript_ExportEmptyValue(t *testing.T) {
	script, err := ParseScript("export KEY=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if script.Statements[0].Export.Value != "" {
		t.Errorf("value = %q, want empty string", script.Statements[0].Export.Value)
	}
}

func TestParseScript_ExportValueContainsEquals(t *testing.T) {
	script, err := ParseScript("export CONFIG=a=b=c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if script.Statements[0].Export.Value != "a=b=c" {
		t.Errorf("value = %q, want %q", script.Statements[0].Export.Value, "a=b=c")
	}
}

func TestParseScript_ExportCaseInsensitive(t *testing.T) {
	for _, keyword := range []string{"export", "Export", "EXPORT"} {
		script, err := ParseScript(keyword + " KEY=val")
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", keyword, err)
		}
		if script.Statements[0].Kind != StmtExport {
			t.Errorf("%q: kind = %q, want %q", keyword, script.Statements[0].Kind, StmtExport)
		}
	}
}

// --- 11.2-UNIT-012: [P0] ParseScript 带引号值 export (AC1) ---

func TestParseScript_ExportDoubleQuotedValue(t *testing.T) {
	script, err := ParseScript(`export KEY="value with spaces"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if script.Statements[0].Export.Value != "value with spaces" {
		t.Errorf("value = %q, want %q", script.Statements[0].Export.Value, "value with spaces")
	}
}

func TestParseScript_ExportSingleQuotedValue(t *testing.T) {
	script, err := ParseScript(`export KEY='value with spaces'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if script.Statements[0].Export.Value != "value with spaces" {
		t.Errorf("value = %q, want %q", script.Statements[0].Export.Value, "value with spaces")
	}
}

// --- 11.2-UNIT-013: [P0] ParseScript 多行（export + spawn）(AC1, AC2) ---

func TestParseScript_MultiLine_ExportAndSpawn(t *testing.T) {
	input := "export TARGET=./src/auth.go\nspawn \"分析 $TARGET\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}
	if script.Statements[0].Kind != StmtExport {
		t.Errorf("stmt 0 kind = %q, want %q", script.Statements[0].Kind, StmtExport)
	}
	if script.Statements[1].Kind != StmtSpawn {
		t.Errorf("stmt 1 kind = %q, want %q", script.Statements[1].Kind, StmtSpawn)
	}
	if script.Statements[1].Spawn == nil {
		t.Fatal("Spawn should not be nil")
	}
	if script.Statements[1].Spawn.Intent != "分析 $TARGET" {
		t.Errorf("intent = %q, want %q", script.Statements[1].Spawn.Intent, "分析 $TARGET")
	}
}

// --- 11.2-UNIT-014: [P0] ParseScript 多行（export + pipeline）(AC1, AC2) ---

func TestParseScript_MultiLine_ExportAndPipeline(t *testing.T) {
	input := "export OUT=./reports\nspawn \"分析\" | spawn \"保存到 $OUT\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}
	if script.Statements[0].Kind != StmtExport {
		t.Errorf("stmt 0 kind = %q, want %q", script.Statements[0].Kind, StmtExport)
	}
	if script.Statements[1].Kind != StmtPipeline {
		t.Errorf("stmt 1 kind = %q, want %q", script.Statements[1].Kind, StmtPipeline)
	}
	if script.Statements[1].Pipeline == nil {
		t.Fatal("Pipeline should not be nil")
	}
	if len(script.Statements[1].Pipeline.Commands) != 2 {
		t.Fatalf("pipeline commands = %d, want 2", len(script.Statements[1].Pipeline.Commands))
	}
}

// --- 11.2-UNIT-015: [P1] ParseScript 跳过空行和注释 ---

func TestParseScript_SkipEmptyAndComments(t *testing.T) {
	input := "# this is a comment\n\nexport KEY=val\n  \n# another comment\nspawn \"do stuff\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2 (comments and blank lines skipped)", len(script.Statements))
	}
	if script.Statements[0].Kind != StmtExport {
		t.Errorf("stmt 0 kind = %q, want %q", script.Statements[0].Kind, StmtExport)
	}
	if script.Statements[1].Kind != StmtSpawn {
		t.Errorf("stmt 1 kind = %q, want %q", script.Statements[1].Kind, StmtSpawn)
	}
}

// --- 11.2-UNIT-016: [P0] ParseScript 无效 export 格式 ---

func TestParseScript_InvalidExport_NoEquals(t *testing.T) {
	_, err := ParseScript("export KEY")
	if err == nil {
		t.Fatal("expected error for export without '='")
	}
}

func TestParseScript_InvalidExport_NoKey(t *testing.T) {
	_, err := ParseScript("export =value")
	if err == nil {
		t.Fatal("expected error for export with empty key")
	}
}

func TestParseScript_InvalidExport_SpacesAroundEquals(t *testing.T) {
	_, err := ParseScript("export KEY = value")
	if err == nil {
		t.Fatal("expected error for export with spaces around '='")
	}
}

// --- 11.2-UNIT-017: [P0] ScriptExecutor export + spawn 变量展开 (AC1, AC2) ---

func TestScriptExecutor_ExportThenSpawn(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "分析完成", exitCode: 0, tokens: 100},
		},
	}

	script, err := ParseScript("export TARGET=./src/auth.go\nspawn \"分析 $TARGET\"")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Verify variable was expanded before spawning
	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(spawner.calls))
	}
	if spawner.calls[0].intent != "分析 ./src/auth.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "分析 ./src/auth.go")
	}

	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
	if result.TotalTokens != 100 {
		t.Errorf("tokens = %d, want 100", result.TotalTokens)
	}
}

func TestScriptExecutor_ExportValueExpansion(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	script, err := ParseScript("export BASE=/home/user\nexport FULL=$BASE/file.go\nspawn \"read $FULL\"")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if spawner.calls[0].intent != "read /home/user/file.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "read /home/user/file.go")
	}
}

// --- 11.2-UNIT-018: [P0] ScriptExecutor pipeline 变量展开 (AC2) ---

func TestScriptExecutor_PipelineVarExpansion(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "分析结果", exitCode: 0, tokens: 100},
			{result: "保存完成", exitCode: 0, tokens: 50},
		},
	}

	script, err := ParseScript("export OUT=./reports\nspawn \"分析\" | spawn \"保存到 $OUT\"")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// First pipeline stage: no variable reference (intent is literal "分析")
	if spawner.calls[0].intent != "分析" {
		t.Errorf("pipeline stage 0 intent = %q, want %q", spawner.calls[0].intent, "分析")
	}

	// Second pipeline stage: variable expanded + PIPE_INPUT injected
	stage1Intent := spawner.calls[1].intent
	if !contains(stage1Intent, "保存到 ./reports") {
		t.Errorf("pipeline stage 1 should contain expanded intent, got %q", stage1Intent)
	}
	if !contains(stage1Intent, "[PIPE_INPUT]") {
		t.Errorf("pipeline stage 1 should contain [PIPE_INPUT], got %q", stage1Intent)
	}

	if result.TotalTokens != 150 {
		t.Errorf("total tokens = %d, want 150", result.TotalTokens)
	}
}

// --- 11.2-UNIT-019: [P0] ScriptExecutor export 覆盖同名变量 ---

func TestScriptExecutor_ExportOverwrite(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	script, err := ParseScript("export KEY=old\nexport KEY=new\nspawn \"$KEY\"")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if spawner.calls[0].intent != "new" {
		t.Errorf("intent = %q, want %q (overwritten value)", spawner.calls[0].intent, "new")
	}
}

// --- 11.2-UNIT-020: [P0] ScriptExecutor 非零 ExitCode 中断 ---

func TestScriptExecutor_NonZeroExitBreaks(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "error output", exitCode: 1, tokens: 50},
			{result: "should not run", exitCode: 0, tokens: 100},
		},
	}

	script, err := ParseScript("spawn \"task A\"\nspawn \"task B\"")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (second spawn should not execute)", len(spawner.calls))
	}
	if result.LastExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.LastExitCode)
	}
}

// --- 11.2-UNIT-021: [P1] ScriptExecutor context 取消 ---

func TestScriptExecutor_ContextCancelled(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	script, err := ParseScript("spawn \"task A\"\nspawn \"task B\"")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(ctx, script)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

// --- Additional: ScriptExecutor with agent and model ---

func TestScriptExecutor_SpawnWithAgentAndModel(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "分析完成", exitCode: 0, tokens: 100},
		},
	}

	script, err := ParseScript(`spawn "分析代码" --agent=code-analyst --model=opus`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if spawner.calls[0].agent != "code-analyst" {
		t.Errorf("agent = %q, want %q", spawner.calls[0].agent, "code-analyst")
	}
	if spawner.calls[0].model != "opus" {
		t.Errorf("model = %q, want %q", spawner.calls[0].model, "opus")
	}
}

// --- Additional: ScriptResult records elapsed time ---

func TestScriptExecutor_RecordsElapsed(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	script, err := ParseScript(`spawn "test"`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Elapsed <= 0 {
		t.Error("elapsed should be > 0")
	}
}

// --- Additional: ScriptExecutor OnStageStart callback ---

func TestScriptExecutor_OnStageStartCallback(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	script, err := ParseScript("export A=1\nspawn \"test\"")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)

	var callbackCalls []int
	executor.OnStageStart = func(stage, total int, intent string) {
		callbackCalls = append(callbackCalls, stage)
	}

	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(callbackCalls) == 0 {
		t.Error("OnStageStart callback should have been called")
	}
}

// --- Additional: Export-only script (no spawn) ---

func TestScriptExecutor_ExportOnly(t *testing.T) {
	spawner := &mockSpawner{}

	script, err := ParseScript("export A=1\nexport B=2")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(spawner.calls) != 0 {
		t.Errorf("calls = %d, want 0 (export-only script)", len(spawner.calls))
	}

	// Variables should be set in env
	val, ok := env.Get("A")
	if !ok || val != "1" {
		t.Errorf("env A = %q, want %q", val, "1")
	}
	val, ok = env.Get("B")
	if !ok || val != "2" {
		t.Errorf("env B = %q, want %q", val, "2")
	}

	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
}

// --- Additional: ParseScript empty input ---

func TestParseScript_EmptyInput(t *testing.T) {
	script, err := ParseScript("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 0 {
		t.Errorf("statements count = %d, want 0", len(script.Statements))
	}
}

// --- Additional: ParseScript single spawn (no export) ---

func TestParseScript_SingleSpawn(t *testing.T) {
	script, err := ParseScript(`spawn "分析代码"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}
	if script.Statements[0].Kind != StmtSpawn {
		t.Errorf("kind = %q, want %q", script.Statements[0].Kind, StmtSpawn)
	}
}

// helper
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Ensure time package is used
var _ = time.Duration(0)

// ============================================================
// ATDD RED PHASE — Story 11.3: 最小控制结构 (Minimal Control Structures)
//
// Tests reference StmtIf, IfBlock, Condition, SpawnResult,
// Statement.If, Statement.Assign, Statement.OnError
// — which do NOT exist yet → compile failure = RED phase.
//
// mockSpawner is reused from pipe_test.go (same package).
// ============================================================

// --- 11.3-UNIT-001: [P0] ParseScript if/else/end 基本解析 (AC1) ---

func TestParseScript_IfElseEnd_Basic(t *testing.T) {
	input := "result = spawn \"分析代码\"\nif $result.exitcode == 0\n  spawn \"生成报告\"\nelse\n  spawn \"记录失败\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}

	assign := script.Statements[0]
	if assign.Kind != StmtSpawn {
		t.Errorf("stmt 0 kind = %q, want %q", assign.Kind, StmtSpawn)
	}
	if assign.Assign != "result" {
		t.Errorf("stmt 0 assign = %q, want %q", assign.Assign, "result")
	}

	ifStmt := script.Statements[1]
	if ifStmt.Kind != StmtIf {
		t.Fatalf("stmt 1 kind = %q, want %q", ifStmt.Kind, StmtIf)
	}
	if ifStmt.If == nil {
		t.Fatal("If should not be nil")
	}
	if ifStmt.If.Condition.VarName != "result" {
		t.Errorf("condition var = %q, want %q", ifStmt.If.Condition.VarName, "result")
	}
	if ifStmt.If.Condition.Property != "exitcode" {
		t.Errorf("condition prop = %q, want %q", ifStmt.If.Condition.Property, "exitcode")
	}
	if ifStmt.If.Condition.Operator != "==" {
		t.Errorf("condition op = %q, want %q", ifStmt.If.Condition.Operator, "==")
	}
	if ifStmt.If.Condition.Value != "0" {
		t.Errorf("condition val = %q, want %q", ifStmt.If.Condition.Value, "0")
	}
	if len(ifStmt.If.Then) != 1 {
		t.Fatalf("then count = %d, want 1", len(ifStmt.If.Then))
	}
	if len(ifStmt.If.Else) != 1 {
		t.Fatalf("else count = %d, want 1", len(ifStmt.If.Else))
	}
}

// --- 11.3-UNIT-002: [P0] ParseScript if（无 else）解析 (AC1) ---

func TestParseScript_IfNoElse(t *testing.T) {
	input := "if $status == ok\n  spawn \"处理\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	ifStmt := script.Statements[0]
	if ifStmt.Kind != StmtIf {
		t.Fatalf("kind = %q, want %q", ifStmt.Kind, StmtIf)
	}
	if len(ifStmt.If.Then) != 1 {
		t.Errorf("then count = %d, want 1", len(ifStmt.If.Then))
	}
	if len(ifStmt.If.Else) != 0 {
		t.Errorf("else count = %d, want 0", len(ifStmt.If.Else))
	}
}

// --- 11.3-UNIT-003: [P0] ParseScript 嵌套 if 解析 (AC3) ---

func TestParseScript_NestedIf(t *testing.T) {
	input := "if $a == 1\n  if $b == 2\n    spawn \"内层\"\n  end\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	outer := script.Statements[0]
	if outer.Kind != StmtIf {
		t.Fatalf("outer kind = %q, want %q", outer.Kind, StmtIf)
	}
	if len(outer.If.Then) != 1 {
		t.Fatalf("outer then count = %d, want 1", len(outer.If.Then))
	}

	inner := outer.If.Then[0]
	if inner.Kind != StmtIf {
		t.Fatalf("inner kind = %q, want %q", inner.Kind, StmtIf)
	}
	if inner.If.Condition.VarName != "b" {
		t.Errorf("inner condition var = %q, want %q", inner.If.Condition.VarName, "b")
	}
	if len(inner.If.Then) != 1 {
		t.Errorf("inner then count = %d, want 1", len(inner.If.Then))
	}
}

// --- 11.3-UNIT-004: [P0] parseCondition $VAR.PROP == VALUE (AC1) ---

func TestParseScript_Condition_VarPropEquals(t *testing.T) {
	input := "if $result.exitcode == 0\n  spawn \"ok\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := script.Statements[0].If.Condition
	if cond.VarName != "result" {
		t.Errorf("var = %q, want %q", cond.VarName, "result")
	}
	if cond.Property != "exitcode" {
		t.Errorf("prop = %q, want %q", cond.Property, "exitcode")
	}
	if cond.Operator != "==" {
		t.Errorf("op = %q, want %q", cond.Operator, "==")
	}
	if cond.Value != "0" {
		t.Errorf("val = %q, want %q", cond.Value, "0")
	}
}

// --- 11.3-UNIT-005: [P1] parseCondition $VAR == VALUE（普通变量）(AC1) ---

func TestParseScript_Condition_PlainVar(t *testing.T) {
	input := "if $MODE == debug\n  spawn \"调试\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := script.Statements[0].If.Condition
	if cond.VarName != "MODE" {
		t.Errorf("var = %q, want %q", cond.VarName, "MODE")
	}
	if cond.Property != "" {
		t.Errorf("prop = %q, want empty", cond.Property)
	}
	if cond.Value != "debug" {
		t.Errorf("val = %q, want %q", cond.Value, "debug")
	}
}

// --- 11.3-UNIT-006: [P0] ParseScript 赋值 spawn result = spawn "..." (AC1) ---

func TestParseScript_AssignmentSpawn(t *testing.T) {
	input := `result = spawn "分析代码" --agent=code-analyst`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtSpawn {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtSpawn)
	}
	if stmt.Assign != "result" {
		t.Errorf("assign = %q, want %q", stmt.Assign, "result")
	}
	if stmt.Spawn == nil {
		t.Fatal("Spawn should not be nil")
	}
	if stmt.Spawn.Intent != "分析代码" {
		t.Errorf("intent = %q, want %q", stmt.Spawn.Intent, "分析代码")
	}
	if stmt.Spawn.Agent != "code-analyst" {
		t.Errorf("agent = %q, want %q", stmt.Spawn.Agent, "code-analyst")
	}
}

// --- 11.3-UNIT-007: [P0] ParseScript on-error spawn "A" on-error spawn "B" (AC2) ---

func TestParseScript_OnError(t *testing.T) {
	input := `spawn "危险操作" on-error spawn "回滚"`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtSpawn {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtSpawn)
	}
	if stmt.Spawn == nil || stmt.Spawn.Intent != "危险操作" {
		t.Errorf("main intent = %q, want %q", stmt.Spawn.Intent, "危险操作")
	}
	if stmt.OnError == nil {
		t.Fatal("OnError should not be nil")
	}
	if stmt.OnError.Intent != "回滚" {
		t.Errorf("handler intent = %q, want %q", stmt.OnError.Intent, "回滚")
	}
}

// --- 11.3-UNIT-008: [P0] ParseScript 赋值 + on-error 组合 (AC1, AC2) ---

func TestParseScript_AssignmentPlusOnError(t *testing.T) {
	input := `result = spawn "部署" on-error spawn "回滚"`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Assign != "result" {
		t.Errorf("assign = %q, want %q", stmt.Assign, "result")
	}
	if stmt.Spawn == nil || stmt.Spawn.Intent != "部署" {
		t.Errorf("main intent = %q, want %q", stmt.Spawn.Intent, "部署")
	}
	if stmt.OnError == nil || stmt.OnError.Intent != "回滚" {
		t.Errorf("handler intent = %q, want %q", stmt.OnError.Intent, "回滚")
	}
}

// --- 11.3-UNIT-009: [P0] ParseScript 错误——未闭合 if 块 ---

func TestParseScript_Error_UnclosedIf(t *testing.T) {
	input := "if $x == 1\n  spawn \"A\""
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for unclosed if block")
	}
}

// --- 11.3-UNIT-010: [P1] ParseScript 错误——else/end 在 if 块外 ---

func TestParseScript_Error_ElseOutsideIf(t *testing.T) {
	_, err := ParseScript("else")
	if err == nil {
		t.Fatal("expected error for else outside if block")
	}

	_, err = ParseScript("end")
	if err == nil {
		t.Fatal("expected error for end outside if block")
	}
}

// --- 11.3-UNIT-011: [P1] ParseScript 错误——无效条件 ---

func TestParseScript_Error_InvalidCondition(t *testing.T) {
	tests := []string{
		"if badcondition\n  spawn \"A\"\nend",
		"if $x\n  spawn \"A\"\nend",
		"if $x > 0\n  spawn \"A\"\nend",
		"if $x ==\n  spawn \"A\"\nend",
	}
	for _, input := range tests {
		_, err := ParseScript(input)
		if err == nil {
			t.Errorf("expected error for invalid condition: %q", input)
		}
	}
}

// --- 11.3-UNIT-012: [P0] ScriptExecutor if then 分支——exitcode == 0 走 then (AC1) ---

func TestScriptExecutor_IfThenBranch(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "分析完成", exitCode: 0, tokens: 100},
			{result: "报告生成", exitCode: 0, tokens: 200},
		},
	}

	input := "result = spawn \"分析\"\nif $result.exitcode == 0\n  spawn \"生成报告\"\nelse\n  spawn \"记录失败\"\nend"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	if !containsSubstring(spawner.calls[1].intent, "生成报告") {
		t.Errorf("expected then branch, got %q", spawner.calls[1].intent)
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
	if result.TotalTokens != 300 {
		t.Errorf("tokens = %d, want 300", result.TotalTokens)
	}
}

// --- 11.3-UNIT-013: [P0] ScriptExecutor if else 分支——exitcode != 0 走 else (AC1) ---

func TestScriptExecutor_IfElseBranch(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "失败", exitCode: 1, tokens: 50},
			{result: "已记录", exitCode: 0, tokens: 100},
		},
	}

	input := "result = spawn \"分析\"\nif $result.exitcode == 0\n  spawn \"生成报告\"\nelse\n  spawn \"记录失败\"\nend"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	if !containsSubstring(spawner.calls[1].intent, "记录失败") {
		t.Errorf("expected else branch, got %q", spawner.calls[1].intent)
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0 (else branch succeeded)", result.LastExitCode)
	}
}

// --- 11.3-UNIT-014: [P0] ScriptExecutor if（无 else）——条件不满足跳过 (AC1) ---

func TestScriptExecutor_IfNoElse_SkipWhenFalse(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "失败", exitCode: 1, tokens: 50},
		},
	}

	input := "result = spawn \"分析\"\nif $result.exitcode == 0\n  spawn \"不应执行\"\nend"
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (then branch should be skipped)", len(spawner.calls))
	}
}

// --- 11.3-UNIT-015: [P0] ScriptExecutor 嵌套 if 正确执行 (AC3) ---

func TestScriptExecutor_NestedIf(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
			{result: "深层结果", exitCode: 0, tokens: 100},
		},
	}

	input := "result = spawn \"分析\"\nif $result.exitcode == 0\n  if $result.exitcode == 0\n    spawn \"深层操作\"\n  end\nend"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	if !containsSubstring(spawner.calls[1].intent, "深层操作") {
		t.Errorf("expected nested then branch, got %q", spawner.calls[1].intent)
	}
	if result.TotalTokens != 150 {
		t.Errorf("tokens = %d, want 150", result.TotalTokens)
	}
}

// --- 11.3-UNIT-016: [P0] ScriptExecutor 赋值 spawn——captures 存储且不中断 (AC1) ---

func TestScriptExecutor_AssignSpawn_NoBreak(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "错误输出", exitCode: 1, tokens: 50},
			{result: "继续执行", exitCode: 0, tokens: 100},
		},
	}

	input := "result = spawn \"可能失败\"\nspawn \"继续\""
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (assignment spawn should not break)", len(spawner.calls))
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
	if result.TotalTokens != 150 {
		t.Errorf("tokens = %d, want 150", result.TotalTokens)
	}
}

// --- 11.3-UNIT-017: [P0] ScriptExecutor 赋值 spawn 文本输出可在后续 intent 展开 ($result) (AC1) ---

func TestScriptExecutor_AssignSpawn_VarExpansion(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "发现3个问题", exitCode: 0, tokens: 100},
			{result: "报告完成", exitCode: 0, tokens: 200},
		},
	}

	input := "result = spawn \"分析\"\nspawn \"基于 $result 生成报告\""
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	expected := "基于 发现3个问题 生成报告"
	if spawner.calls[1].intent != expected {
		t.Errorf("intent = %q, want %q", spawner.calls[1].intent, expected)
	}
}

// --- 11.3-UNIT-018: [P0] ScriptExecutor on-error——主命令失败触发 handler (AC2) ---

func TestScriptExecutor_OnError_Triggered(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "部署失败", exitCode: 1, tokens: 50},
			{result: "已回滚", exitCode: 0, tokens: 100},
		},
	}

	input := `spawn "部署" on-error spawn "回滚"`
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	if !containsSubstring(spawner.calls[1].intent, "回滚") {
		t.Errorf("expected handler execution, got %q", spawner.calls[1].intent)
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0 (handler succeeded)", result.LastExitCode)
	}
}

// --- 11.3-UNIT-019: [P0] ScriptExecutor on-error——主命令成功跳过 handler (AC2) ---

func TestScriptExecutor_OnError_NotTriggered(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "部署成功", exitCode: 0, tokens: 100},
		},
	}

	input := `spawn "部署" on-error spawn "回滚"`
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (handler should not execute)", len(spawner.calls))
	}
}

// --- 11.3-UNIT-020: [P0] ScriptExecutor on-error handler 成功→脚本继续 (AC2) ---

func TestScriptExecutor_OnError_HandlerSuccess_Continues(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "失败", exitCode: 1, tokens: 50},
			{result: "已回滚", exitCode: 0, tokens: 50},
			{result: "后续完成", exitCode: 0, tokens: 100},
		},
	}

	input := "spawn \"部署\" on-error spawn \"回滚\"\nspawn \"后续任务\""
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

	if len(spawner.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (main + handler + next)", len(spawner.calls))
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
}

// --- 11.3-UNIT-021: [P0] ScriptExecutor on-error handler 失败→脚本中断 (AC2) ---

func TestScriptExecutor_OnError_HandlerFail_Breaks(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "失败", exitCode: 1, tokens: 50},
			{result: "回滚也失败", exitCode: 2, tokens: 50},
			{result: "不应执行", exitCode: 0, tokens: 100},
		},
	}

	input := "spawn \"部署\" on-error spawn \"回滚\"\nspawn \"不应执行\""
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

	if len(spawner.calls) != 2 {
		t.Errorf("calls = %d, want 2 (main + handler, next should NOT run)", len(spawner.calls))
	}
	if result.LastExitCode != 2 {
		t.Errorf("exit code = %d, want 2", result.LastExitCode)
	}
}

// --- 11.3-UNIT-022: [P1] ScriptExecutor 条件引用 env 普通变量 (AC1) ---

func TestScriptExecutor_Condition_EnvVar(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "调试输出", exitCode: 0, tokens: 100},
		},
	}

	input := "export MODE=debug\nif $MODE == debug\n  spawn \"调试模式\"\nend"
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (condition should match)", len(spawner.calls))
	}
}

// --- 11.3-UNIT-023: [P1] ScriptExecutor 条件 != 操作符 (AC1) ---

func TestScriptExecutor_Condition_NotEquals(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "失败输出", exitCode: 1, tokens: 50},
			{result: "处理完成", exitCode: 0, tokens: 100},
		},
	}

	input := "result = spawn \"检查\"\nif $result.exitcode != 0\n  spawn \"处理错误\"\nend"
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

	if len(spawner.calls) != 2 {
		t.Errorf("calls = %d, want 2 (condition != should match)", len(spawner.calls))
	}
}

// --- 11.3-REG-001 + 11.3-REG-002: 回归验证 ---
// All existing 11.2 ParseScript and ScriptExecutor tests above
// must continue passing after 11.3 implementation.
// Verified by running: go test ./shell/...

// --- 11.3-UNIT-EXTRA-001: on-error 在引号内不触发分割 ---

func TestParseScript_OnError_InsideQuotes(t *testing.T) {
	input := `spawn "handle on-error case"`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.OnError != nil {
		t.Error("on-error inside quotes should NOT trigger split")
	}
	if stmt.Spawn.Intent != "handle on-error case" {
		t.Errorf("intent = %q, want %q", stmt.Spawn.Intent, "handle on-error case")
	}
}

// --- 11.3-UNIT-EXTRA-002: if/else/end 大小写不敏感 ---

func TestParseScript_IfCaseInsensitive(t *testing.T) {
	for _, kw := range []string{
		"IF $x == 1\n  spawn \"A\"\nEND",
		"If $x == 1\n  spawn \"A\"\nEnd",
		"if $x == 1\n  spawn \"A\"\nend",
	} {
		script, err := ParseScript(kw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", kw, err)
		}
		if len(script.Statements) != 1 || script.Statements[0].Kind != StmtIf {
			t.Errorf("failed to parse case-insensitive if: %q", kw)
		}
	}
}

// --- 11.3-UNIT-EXTRA-003: pipeline + on-error 解析 ---

func TestParseScript_PipelineOnError(t *testing.T) {
	input := `spawn "A" | spawn "B" on-error spawn "recovery"`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtPipeline {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtPipeline)
	}
	if stmt.Pipeline == nil || len(stmt.Pipeline.Commands) != 2 {
		t.Fatalf("expected 2-stage pipeline")
	}
	if stmt.OnError == nil {
		t.Fatal("OnError should not be nil")
	}
	if stmt.OnError.Intent != "recovery" {
		t.Errorf("handler intent = %q, want %q", stmt.OnError.Intent, "recovery")
	}
}

// --- 11.3-UNIT-EXTRA-004: pipeline + on-error 执行——pipeline 失败触发 handler ---

func TestScriptExecutor_PipelineOnError_Triggered(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "stage1 ok", exitCode: 0, tokens: 50},
			{result: "stage2 fail", exitCode: 1, tokens: 50},
			{result: "recovered", exitCode: 0, tokens: 100},
		},
	}

	input := `spawn "A" | spawn "B" on-error spawn "recovery"`
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

	if len(spawner.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (pipeline 2 stages + on-error handler)", len(spawner.calls))
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0 (handler recovered)", result.LastExitCode)
	}
	if result.TotalTokens != 200 {
		t.Errorf("tokens = %d, want 200", result.TotalTokens)
	}
}

// --- 11.3-UNIT-EXTRA-005: pipeline + on-error 执行——pipeline 成功跳过 handler ---

func TestScriptExecutor_PipelineOnError_NotTriggered(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "stage1 ok", exitCode: 0, tokens: 50},
			{result: "stage2 ok", exitCode: 0, tokens: 50},
		},
	}

	input := `spawn "A" | spawn "B" on-error spawn "recovery"`
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

	if len(spawner.calls) != 2 {
		t.Errorf("calls = %d, want 2 (handler should not execute)", len(spawner.calls))
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
}

// --- 11.3-UNIT-EXTRA-006: 空 then body ---

func TestParseScript_EmptyThenBody(t *testing.T) {
	input := "if $x == 1\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}
	if len(script.Statements[0].If.Then) != 0 {
		t.Errorf("then count = %d, want 0", len(script.Statements[0].If.Then))
	}
}

// ============================================================
// ATDD RED PHASE — Story 18.1: 循环结构与内置命令
//
// Tests reference StmtFor, StmtWhile, StmtBuiltin, ForBlock,
// WhileBlock, BuiltinStmt, MaxLoopIterations, ErrScriptExit
// — which do NOT exist yet → compile failure = RED phase.
//
// mockSpawner is reused from pipe_test.go (same package).
// mockWaitableSpawner extends it with Wait support (Task 5).
// ============================================================

// --- mockWaitableSpawner extends mockSpawner with Wait method (KernelSpawner Task 5) ---

type mockWaitResult struct {
	exitCode int
	err      error
}

type mockWaitableSpawner struct {
	*mockSpawner
	waitResults []mockWaitResult
	waitCalls   []int
}

func (m *mockWaitableSpawner) Wait(ctx context.Context, pid int) (int, error) {
	m.waitCalls = append(m.waitCalls, pid)
	idx := len(m.waitCalls) - 1
	if idx >= len(m.waitResults) {
		return 1, fmt.Errorf("unexpected wait call %d", idx)
	}
	r := m.waitResults[idx]
	return r.exitCode, r.err
}

// --- 18.1-UNIT-001: [P0] ParseScript for-in 基本解析（方括号列表）(AC1) ---

func TestParseScript_ForInBrackets(t *testing.T) {
	input := "for item in [a, b, c]\n  spawn \"处理 ${item}\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtFor {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtFor)
	}
	if stmt.For == nil {
		t.Fatal("For should not be nil")
	}
	if stmt.For.VarName != "item" {
		t.Errorf("var = %q, want %q", stmt.For.VarName, "item")
	}
	if len(stmt.For.List) != 3 {
		t.Fatalf("list len = %d, want 3", len(stmt.For.List))
	}
	if stmt.For.List[0] != "a" || stmt.For.List[1] != "b" || stmt.For.List[2] != "c" {
		t.Errorf("list = %v, want [a b c]", stmt.For.List)
	}
	if len(stmt.For.Body) != 1 {
		t.Fatalf("body len = %d, want 1", len(stmt.For.Body))
	}
}

// --- 18.1-UNIT-002: [P0] ParseScript for-in 基本解析（空格分隔列表）(AC1) ---

func TestParseScript_ForInSpaceSeparated(t *testing.T) {
	input := "for file in main.go utils.go config.go\n  spawn \"分析 ${file}\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtFor {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtFor)
	}
	if stmt.For.VarName != "file" {
		t.Errorf("var = %q, want %q", stmt.For.VarName, "file")
	}
	if len(stmt.For.List) != 3 {
		t.Fatalf("list len = %d, want 3", len(stmt.For.List))
	}
	if stmt.For.List[0] != "main.go" || stmt.For.List[1] != "utils.go" || stmt.For.List[2] != "config.go" {
		t.Errorf("list = %v, want [main.go utils.go config.go]", stmt.For.List)
	}
}

// --- 18.1-UNIT-003: [P0] ParseScript while 基本解析 (AC2) ---

func TestParseScript_WhileBasic(t *testing.T) {
	input := "while $counter != 0\n  spawn \"执行\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtWhile {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtWhile)
	}
	if stmt.While == nil {
		t.Fatal("While should not be nil")
	}
	if stmt.While.Condition.VarName != "counter" {
		t.Errorf("condition var = %q, want %q", stmt.While.Condition.VarName, "counter")
	}
	if stmt.While.Condition.Operator != "!=" {
		t.Errorf("condition op = %q, want %q", stmt.While.Condition.Operator, "!=")
	}
	if stmt.While.Condition.Value != "0" {
		t.Errorf("condition val = %q, want %q", stmt.While.Condition.Value, "0")
	}
	if len(stmt.While.Body) != 1 {
		t.Fatalf("body len = %d, want 1", len(stmt.While.Body))
	}
}

// --- 18.1-UNIT-004: [P0] ParseScript 内置命令 wait (AC3) ---

func TestParseScript_BuiltinWait(t *testing.T) {
	input := "wait $pid"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtBuiltin {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtBuiltin)
	}
	if stmt.Builtin == nil {
		t.Fatal("Builtin should not be nil")
	}
	if stmt.Builtin.Command != "wait" {
		t.Errorf("command = %q, want %q", stmt.Builtin.Command, "wait")
	}
	if len(stmt.Builtin.Args) != 1 || stmt.Builtin.Args[0] != "$pid" {
		t.Errorf("args = %v, want [\"$pid\"]", stmt.Builtin.Args)
	}
}

// --- 18.1-UNIT-005: [P0] ParseScript 内置命令 sleep (AC4) ---

func TestParseScript_BuiltinSleep(t *testing.T) {
	input := "sleep 5s"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtBuiltin {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtBuiltin)
	}
	if stmt.Builtin.Command != "sleep" {
		t.Errorf("command = %q, want %q", stmt.Builtin.Command, "sleep")
	}
	if len(stmt.Builtin.Args) != 1 || stmt.Builtin.Args[0] != "5s" {
		t.Errorf("args = %v, want [\"5s\"]", stmt.Builtin.Args)
	}
}

// --- 18.1-UNIT-006: [P0] ParseScript 内置命令 exit (AC5) ---

func TestParseScript_BuiltinExit(t *testing.T) {
	input := "exit 0"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtBuiltin {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtBuiltin)
	}
	if stmt.Builtin.Command != "exit" {
		t.Errorf("command = %q, want %q", stmt.Builtin.Command, "exit")
	}
	if len(stmt.Builtin.Args) != 1 || stmt.Builtin.Args[0] != "0" {
		t.Errorf("args = %v, want [\"0\"]", stmt.Builtin.Args)
	}
}

// --- 18.1-UNIT-007: [P0] ParseScript for 嵌套 if (AC6) ---

func TestParseScript_ForNestedIf(t *testing.T) {
	input := "for item in [a, b, c]\n  if $item == b\n    spawn \"匹配\"\n  end\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	forStmt := script.Statements[0]
	if forStmt.Kind != StmtFor {
		t.Fatalf("kind = %q, want %q", forStmt.Kind, StmtFor)
	}
	if len(forStmt.For.Body) != 1 {
		t.Fatalf("for body len = %d, want 1", len(forStmt.For.Body))
	}
	ifStmt := forStmt.For.Body[0]
	if ifStmt.Kind != StmtIf {
		t.Errorf("nested kind = %q, want %q", ifStmt.Kind, StmtIf)
	}
}

// --- 18.1-UNIT-008: [P1] ParseScript 未闭合 for 块报错 (AC1) ---

func TestParseScript_Error_UnclosedFor(t *testing.T) {
	input := "for item in [a, b]\n  spawn \"test\""
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for unclosed for block")
	}
}

// --- 18.1-UNIT-009: [P1] ParseScript 未闭合 while 块报错 (AC2) ---

func TestParseScript_Error_UnclosedWhile(t *testing.T) {
	input := "while $x != 0\n  spawn \"test\""
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for unclosed while block")
	}
}

// --- 18.1-UNIT-010: [P1] ParseScript sleep 非法格式报错 (AC8) ---

func TestParseScript_Error_SleepInvalidFormat(t *testing.T) {
	input := "sleep abc"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for sleep with invalid duration format")
	}
}

// --- 18.1-UNIT-011: [P1] ParseScript while 嵌套 for (AC1, AC2) ---

func TestParseScript_WhileNestedFor(t *testing.T) {
	input := "while $status != done\n  for item in [a, b]\n    spawn \"处理 ${item}\"\n  end\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	whileStmt := script.Statements[0]
	if whileStmt.Kind != StmtWhile {
		t.Fatalf("kind = %q, want %q", whileStmt.Kind, StmtWhile)
	}
	if len(whileStmt.While.Body) != 1 {
		t.Fatalf("while body len = %d, want 1", len(whileStmt.While.Body))
	}
	if whileStmt.While.Body[0].Kind != StmtFor {
		t.Errorf("nested kind = %q, want %q", whileStmt.While.Body[0].Kind, StmtFor)
	}
}

// --- 18.1-UNIT-012: [P0] ScriptExecutor for 循环变量绑定和展开 (AC1) ---

func TestScriptExecutor_ForLoop_VarBinding(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok-a", exitCode: 0, tokens: 10},
			{result: "ok-b", exitCode: 0, tokens: 10},
			{result: "ok-c", exitCode: 0, tokens: 10},
		},
	}

	input := "for item in [a, b, c]\n  spawn \"处理 ${item}\"\nend"
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

	if len(spawner.calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(spawner.calls))
	}
	if spawner.calls[0].intent != "处理 a" {
		t.Errorf("call 0 intent = %q, want %q", spawner.calls[0].intent, "处理 a")
	}
	if spawner.calls[1].intent != "处理 b" {
		t.Errorf("call 1 intent = %q, want %q", spawner.calls[1].intent, "处理 b")
	}
	if spawner.calls[2].intent != "处理 c" {
		t.Errorf("call 2 intent = %q, want %q", spawner.calls[2].intent, "处理 c")
	}
	if result.TotalTokens != 30 {
		t.Errorf("tokens = %d, want 30", result.TotalTokens)
	}
}

// --- 18.1-UNIT-013: [P0] ScriptExecutor for 循环体内 spawn 调用次数正确 (AC1) ---

func TestScriptExecutor_ForLoop_SpawnCount(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	input := "for f in main.go utils.go\n  spawn \"分析 ${f}\"\nend"
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

	if len(spawner.calls) != 2 {
		t.Errorf("calls = %d, want 2 (one per list item)", len(spawner.calls))
	}
}

// --- 18.1-UNIT-014: [P0] ScriptExecutor while 循环条件变化导致退出 (AC2) ---

func TestScriptExecutor_WhileLoop_ConditionExit(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "1", exitCode: 0, tokens: 10},
			{result: "0", exitCode: 0, tokens: 10},
		},
	}

	input := "export counter=2\nwhile $counter != 0\n  counter = spawn \"减少计数器\"\nend"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (loop should exit when counter becomes 0)", len(spawner.calls))
	}
	val, ok := env.Get("counter")
	if !ok || val != "0" {
		t.Errorf("counter = %q, want %q", val, "0")
	}
}

// --- 18.1-UNIT-015: [P0] ScriptExecutor while 无限循环保护 (AC7) ---

func TestScriptExecutor_WhileLoop_InfiniteProtection(t *testing.T) {
	results := make([]mockResult, MaxLoopIterations+1)
	for i := range results {
		results[i] = mockResult{result: "loop", exitCode: 0, tokens: 1}
	}
	spawner := &mockSpawner{results: results}

	input := "export always=true\nwhile $always != false\n  spawn \"无限\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for infinite loop protection, got nil")
	}
	if !containsSubstring(err.Error(), "maximum iterations") {
		t.Errorf("error = %q, want to contain 'maximum iterations'", err.Error())
	}
}

// --- 18.1-UNIT-016: [P0] ScriptExecutor wait 等待 mock 进程完成 (AC3) ---

func TestScriptExecutor_Wait_MockProcess(t *testing.T) {
	spawner := &mockWaitableSpawner{
		mockSpawner: &mockSpawner{
			results: []mockResult{
				{result: "42", exitCode: 0, tokens: 50},
			},
		},
		waitResults: []mockWaitResult{
			{exitCode: 0, err: nil},
		},
	}

	input := "pid = spawn \"后台任务\" --agent=worker\nwait $pid"
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

	if len(spawner.waitCalls) != 1 {
		t.Fatalf("wait calls = %d, want 1", len(spawner.waitCalls))
	}
}

// --- 18.1-UNIT-017: [P0] ScriptExecutor sleep 可被 ctx.Cancel 中断 (AC4) ---

func TestScriptExecutor_Sleep_Interruptible(t *testing.T) {
	spawner := &mockSpawner{}

	input := "sleep 10s"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	start := time.Now()
	_, err = executor.Execute(ctx, script)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("sleep should have been interrupted quickly, took %v", elapsed)
	}
}

// --- 18.1-UNIT-018: [P0] ScriptExecutor exit 立即终止脚本并返回正确退出码 (AC5) ---

func TestScriptExecutor_Exit_TerminatesWithCode(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "should not run", exitCode: 0, tokens: 100},
		},
	}

	input := "exit 1\nspawn \"不应执行\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v (exit should not be treated as error)", err)
	}

	if len(spawner.calls) != 0 {
		t.Errorf("calls = %d, want 0 (spawn after exit should not run)", len(spawner.calls))
	}
	if result.LastExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.LastExitCode)
	}
}

// --- 18.1-UNIT-019: [P0] ScriptExecutor for 循环 + if 嵌套执行 (AC6) ---

func TestScriptExecutor_ForNestedIf_Execution(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "匹配", exitCode: 0, tokens: 10},
		},
	}

	input := "for item in [a, b, c]\n  if $item == b\n    spawn \"匹配 ${item}\"\n  end\nend"
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

	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (only item 'b' matches condition)", len(spawner.calls))
	}
	if spawner.calls[0].intent != "匹配 b" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "匹配 b")
	}
}

// --- 18.1-UNIT-020: [P0] ScriptExecutor exit 在循环内终止整个脚本 (AC5) ---

func TestScriptExecutor_ExitInLoop_Terminates(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	input := "for item in [a, b, c]\n  spawn \"处理 ${item}\"\n  exit 0\nend"
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (exit should stop after first iteration)", len(spawner.calls))
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
}

// --- 18.1-UNIT-021: [P1] ScriptExecutor for 循环后变量已清除（作用域隔离）(AC1) ---

func TestScriptExecutor_ForLoop_VarCleanup(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	input := "for item in [x, y]\n  spawn \"处理 ${item}\"\nend"
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

	if _, ok := env.Get("item"); ok {
		t.Error("loop variable 'item' should be removed after for loop ends")
	}
}

// --- 18.1-UNIT-022: [P1] ScriptExecutor sleep 正常完成后继续执行 (AC4) ---

func TestScriptExecutor_Sleep_ThenContinue(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "after sleep", exitCode: 0, tokens: 50},
		},
	}

	input := "sleep 1ms\nspawn \"继续\""
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (spawn should run after sleep)", len(spawner.calls))
	}
	if result.LastResult != "after sleep" {
		t.Errorf("result = %q, want %q", result.LastResult, "after sleep")
	}
}

// --- 18.1-UNIT-023: [P0] ScriptExecutor for 循环内修改变量循环间可见 (AC1) ---

func TestScriptExecutor_ForLoop_VarModifyVisible(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "result-a", exitCode: 0, tokens: 10},
			{result: "result-b", exitCode: 0, tokens: 10},
		},
	}

	input := "export accumulated=\nfor item in [a, b]\n  accumulated = spawn \"处理 ${item} prev=${accumulated}\"\nend"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	if !containsSubstring(spawner.calls[1].intent, "prev=result-a") {
		t.Errorf("second call should see accumulated value from first, got %q", spawner.calls[1].intent)
	}
}

// --- 18.1-UNIT-024: [P0] ScriptExecutor exit 0 不作为错误处理 (AC5) ---

func TestScriptExecutor_ExitZero_NotError(t *testing.T) {
	spawner := &mockSpawner{}

	input := "exit 0"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("exit 0 should not be treated as error, got: %v", err)
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
}

// --- 18.1-UNIT-025: [P1] ParseScript for/while 大小写不敏感 ---

func TestParseScript_ForWhileCaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		input string
		kind  StatementKind
	}{
		{"FOR uppercase", "FOR item in [a]\n  spawn \"test\"\nEND", StmtFor},
		{"For mixed", "For item in [a]\n  spawn \"test\"\nEnd", StmtFor},
		{"WHILE uppercase", "WHILE $x != 0\n  spawn \"test\"\nEND", StmtWhile},
		{"While mixed", "While $x != 0\n  spawn \"test\"\nEnd", StmtWhile},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			script, err := ParseScript(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(script.Statements) != 1 || script.Statements[0].Kind != tc.kind {
				t.Errorf("expected kind %q, got %v", tc.kind, script.Statements[0].Kind)
			}
		})
	}
}

// --- 18.1-UNIT-026: [P1] ParseScript 内置命令大小写不敏感 ---

func TestParseScript_BuiltinCaseInsensitive(t *testing.T) {
	for _, kw := range []string{"SLEEP 1s", "Sleep 1s", "EXIT 0", "Exit 0", "WAIT $pid", "Wait $pid"} {
		script, err := ParseScript(kw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", kw, err)
		}
		if script.Statements[0].Kind != StmtBuiltin {
			t.Errorf("%q: kind = %q, want %q", kw, script.Statements[0].Kind, StmtBuiltin)
		}
	}
}

// --- 18.1-CR-001: [P1] for + on-error 嵌套执行（组合矩阵验证）---

func TestScriptExecutor_ForLoop_WithOnError(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "fail-a", exitCode: 1, tokens: 10},
			{result: "recovered-a", exitCode: 0, tokens: 10},
			{result: "ok-b", exitCode: 0, tokens: 10},
		},
	}

	input := "for item in [a, b]\n  spawn \"处理 ${item}\" on-error spawn \"恢复 ${item}\"\nend"
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

	if len(spawner.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (a:fail + a:recover + b:ok)", len(spawner.calls))
	}
	if spawner.calls[0].intent != "处理 a" {
		t.Errorf("call 0 intent = %q, want %q", spawner.calls[0].intent, "处理 a")
	}
	if spawner.calls[1].intent != "恢复 a" {
		t.Errorf("call 1 intent = %q, want %q", spawner.calls[1].intent, "恢复 a")
	}
	if spawner.calls[2].intent != "处理 b" {
		t.Errorf("call 2 intent = %q, want %q", spawner.calls[2].intent, "处理 b")
	}
	if result.LastExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.LastExitCode)
	}
}

// --- 18.1-CR-002: [P1] while + for 嵌套执行（组合矩阵验证）---

func TestScriptExecutor_WhileNestedFor_Execution(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
			{result: "done", exitCode: 0, tokens: 10},
		},
	}

	input := "export status=running\nwhile $status != done\n  for item in [x]\n    status = spawn \"处理 ${item} 状态=${status}\"\n  end\nend"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (while iterates twice, for has 1 item each)", len(spawner.calls))
	}
	if !containsSubstring(spawner.calls[0].intent, "状态=running") {
		t.Errorf("call 0 should see status=running, got %q", spawner.calls[0].intent)
	}
	if !containsSubstring(spawner.calls[1].intent, "状态=ok") {
		t.Errorf("call 1 should see status=ok, got %q", spawner.calls[1].intent)
	}
	val, ok := env.Get("status")
	if !ok || val != "done" {
		t.Errorf("status = %q, want %q", val, "done")
	}
}

// --- 18.1-CR-003: [P1] exit 在 while 循环内终止整个脚本（组合矩阵验证）---

func TestScriptExecutor_ExitInWhileLoop_Terminates(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	input := "export x=1\nwhile $x != 0\n  spawn \"执行\"\n  exit 42\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v (exit should not be treated as error)", err)
	}

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (exit should stop after first iteration)", len(spawner.calls))
	}
	if result.LastExitCode != 42 {
		t.Errorf("exit code = %d, want 42", result.LastExitCode)
	}
}

// --- 18.1-CR-004: [P1] exit 解析时校验——非法值拒绝 ---

func TestParseScript_Error_ExitInvalidCode(t *testing.T) {
	tests := []string{
		"exit abc",
		"exit -1",
		"exit 256",
	}
	for _, input := range tests {
		_, err := ParseScript(input)
		if err == nil {
			t.Errorf("expected error for %q", input)
		}
	}
}

// --- 18.1-CR-005: [P1] 保留关键字不能作为变量名 ---

func TestParseScript_Error_ReservedKeywordAsVarName(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"export for", "export for=value"},
		{"export while", "export while=value"},
		{"export if", "export if=value"},
		{"export exit", "export exit=value"},
		{"for var named if", "for if in [a, b]\n  spawn \"test\"\nend"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.input)
			if err == nil {
				t.Errorf("expected error for reserved keyword as variable name: %q", tc.input)
			}
		})
	}
}

// ============================================================
// ATDD RED PHASE — Story 18.2: 函数定义与调用
//
// Tests reference StmtFnDef, StmtFnCall, StmtReturn,
// FnDef, FnCallStmt, ReturnStmt, Statement.FnDef,
// Statement.FnCall, Statement.Return, Script.Functions,
// ErrFnReturn, MaxCallDepth
// — which do NOT exist yet → compile failure = RED phase.
//
// mockSpawner is reused from pipe_test.go (same package).
// ============================================================

// --- 18.2-UNIT-001: [P0] ParseScript fn 基本定义解析（带参数）(AC1) ---

func TestParseScript_FnDef_WithParams(t *testing.T) {
	input := "fn analyze(file)\n  spawn \"分析 ${file}\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtFnDef {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtFnDef)
	}
	if stmt.FnDef == nil {
		t.Fatal("FnDef should not be nil")
	}
	if stmt.FnDef.Name != "analyze" {
		t.Errorf("name = %q, want %q", stmt.FnDef.Name, "analyze")
	}
	if len(stmt.FnDef.Params) != 1 || stmt.FnDef.Params[0] != "file" {
		t.Errorf("params = %v, want [file]", stmt.FnDef.Params)
	}
	if len(stmt.FnDef.Body) != 1 {
		t.Fatalf("body len = %d, want 1", len(stmt.FnDef.Body))
	}
	if stmt.FnDef.Body[0].Kind != StmtSpawn {
		t.Errorf("body[0] kind = %q, want %q", stmt.FnDef.Body[0].Kind, StmtSpawn)
	}

	if script.Functions == nil {
		t.Fatal("Script.Functions should not be nil")
	}
	if _, ok := script.Functions["analyze"]; !ok {
		t.Error("Script.Functions should contain 'analyze'")
	}
}

// --- 18.2-UNIT-002: [P0] ParseScript fn 无参数定义 (AC4) ---

func TestParseScript_FnDef_NoParams(t *testing.T) {
	input := "fn setup()\n  export MODEL=sonnet\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtFnDef {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtFnDef)
	}
	if stmt.FnDef == nil {
		t.Fatal("FnDef should not be nil")
	}
	if stmt.FnDef.Name != "setup" {
		t.Errorf("name = %q, want %q", stmt.FnDef.Name, "setup")
	}
	if len(stmt.FnDef.Params) != 0 {
		t.Errorf("params = %v, want empty", stmt.FnDef.Params)
	}
	if len(stmt.FnDef.Body) != 1 {
		t.Fatalf("body len = %d, want 1", len(stmt.FnDef.Body))
	}
}

// --- 18.2-UNIT-003: [P0] ParseScript fn 多参数定义 (AC1) ---

func TestParseScript_FnDef_MultipleParams(t *testing.T) {
	input := "fn process(src, dst, mode)\n  spawn \"处理 ${src}\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.FnDef == nil {
		t.Fatal("FnDef should not be nil")
	}
	if len(stmt.FnDef.Params) != 3 {
		t.Fatalf("params len = %d, want 3", len(stmt.FnDef.Params))
	}
	expected := []string{"src", "dst", "mode"}
	for i, p := range expected {
		if stmt.FnDef.Params[i] != p {
			t.Errorf("param[%d] = %q, want %q", i, stmt.FnDef.Params[i], p)
		}
	}
}

// --- 18.2-UNIT-004: [P0] ParseScript fn 体内包含嵌套语句（spawn/if/for/while）(AC5) ---

func TestParseScript_FnDef_NestedStatements(t *testing.T) {
	input := "fn complex(target)\n  for item in [a, b]\n    if $item == a\n      spawn \"处理 ${target} ${item}\"\n    end\n  end\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	body := script.Statements[0].FnDef.Body
	if len(body) != 1 {
		t.Fatalf("body len = %d, want 1", len(body))
	}
	if body[0].Kind != StmtFor {
		t.Errorf("body[0] kind = %q, want %q", body[0].Kind, StmtFor)
	}
}

// --- 18.2-UNIT-005: [P0] ParseScript fn 名为保留关键字 → 错误 (AC10) ---

func TestParseScript_Error_FnNameReservedKeyword(t *testing.T) {
	keywords := []string{"if", "for", "while", "else", "end", "return", "spawn", "export", "exit", "sleep", "wait"}
	for _, kw := range keywords {
		input := fmt.Sprintf("fn %s()\n  spawn \"test\"\nend", kw)
		_, err := ParseScript(input)
		if err == nil {
			t.Errorf("expected error for fn name %q (reserved keyword)", kw)
		}
	}
}

// --- 18.2-UNIT-006: [P1] ParseScript fn 参数名重复 → 错误 ---

func TestParseScript_Error_FnDuplicateParam(t *testing.T) {
	input := "fn bad(x, x)\n  spawn \"test\"\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for duplicate parameter name 'x'")
	}
}

// --- 18.2-UNIT-007: [P1] ParseScript fn 未闭合 → 缺少 end 错误 ---

func TestParseScript_Error_FnUnclosed(t *testing.T) {
	input := "fn broken()\n  spawn \"test\""
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for unclosed fn block (missing 'end')")
	}
}

// --- 18.2-UNIT-008: [P1] ParseScript fn 嵌套定义（块内定义）→ 错误 ---

func TestParseScript_Error_FnNestedDefinition(t *testing.T) {
	input := "if $x == 1\n  fn inner()\n    spawn \"test\"\n  end\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for fn definition inside block (nested fn not allowed)")
	}
}

// --- 18.2-UNIT-009: [P1] ParseScript fn 重复定义同名函数 → 错误 ---

func TestParseScript_Error_FnDuplicateName(t *testing.T) {
	input := "fn dup()\n  spawn \"first\"\nend\nfn dup()\n  spawn \"second\"\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for duplicate function name 'dup'")
	}
}

// --- 18.2-UNIT-010: [P0] ParseScript 函数调用解析（带参数）(AC1) ---

func TestParseScript_FnCall_WithArgs(t *testing.T) {
	input := "fn analyze(file)\n  spawn \"分析 ${file}\"\nend\nanalyze(\"config.yaml\")"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}

	call := script.Statements[1]
	if call.Kind != StmtFnCall {
		t.Errorf("kind = %q, want %q", call.Kind, StmtFnCall)
	}
	if call.FnCall == nil {
		t.Fatal("FnCall should not be nil")
	}
	if call.FnCall.Name != "analyze" {
		t.Errorf("name = %q, want %q", call.FnCall.Name, "analyze")
	}
	if len(call.FnCall.Args) != 1 || call.FnCall.Args[0] != "config.yaml" {
		t.Errorf("args = %v, want [config.yaml]", call.FnCall.Args)
	}
}

// --- 18.2-UNIT-011: [P0] ParseScript 函数调用解析（无参数）(AC4) ---

func TestParseScript_FnCall_NoArgs(t *testing.T) {
	input := "fn setup()\n  export KEY=val\nend\nsetup()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}

	call := script.Statements[1]
	if call.Kind != StmtFnCall {
		t.Errorf("kind = %q, want %q", call.Kind, StmtFnCall)
	}
	if call.FnCall == nil {
		t.Fatal("FnCall should not be nil")
	}
	if call.FnCall.Name != "setup" {
		t.Errorf("name = %q, want %q", call.FnCall.Name, "setup")
	}
	if len(call.FnCall.Args) != 0 {
		t.Errorf("args = %v, want empty", call.FnCall.Args)
	}
}

// --- 18.2-UNIT-012: [P0] ParseScript 函数调用赋值形式 (AC1, AC2) ---

func TestParseScript_FnCall_Assignment(t *testing.T) {
	input := "fn analyze(file)\n  spawn \"分析 ${file}\"\n  return $result\nend\nresult = analyze(\"config.yaml\")"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the fn call statement (should be last)
	var callStmt *Statement
	for i := range script.Statements {
		if script.Statements[i].Kind == StmtFnCall {
			callStmt = &script.Statements[i]
			break
		}
	}
	if callStmt == nil {
		t.Fatal("expected a StmtFnCall statement")
	}
	if callStmt.Assign != "result" {
		t.Errorf("assign = %q, want %q", callStmt.Assign, "result")
	}
	if callStmt.FnCall.Name != "analyze" {
		t.Errorf("name = %q, want %q", callStmt.FnCall.Name, "analyze")
	}
}

// --- 18.2-UNIT-013: [P0] ParseScript 函数调用参数数量不匹配 → 错误含行号 (AC3) ---

func TestParseScript_Error_FnCallArgCountMismatch(t *testing.T) {
	input := "fn analyze(file)\n  spawn \"分析 ${file}\"\nend\nanalyze(\"a\", \"b\")"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for argument count mismatch")
	}
	errMsg := err.Error()
	if !containsSubstring(errMsg, "analyze") {
		t.Errorf("error should mention function name 'analyze', got: %q", errMsg)
	}
	if !containsSubstring(errMsg, "1") {
		t.Errorf("error should mention expected arg count '1', got: %q", errMsg)
	}
	if !containsSubstring(errMsg, "2") {
		t.Errorf("error should mention actual arg count '2', got: %q", errMsg)
	}
	if !containsSubstring(errMsg, "line 4") {
		t.Errorf("error should mention line number 'line 4', got: %q", errMsg)
	}
}

// --- 18.2-UNIT-014: [P0] ParseScript 调用未定义函数 → 错误 (AC7) ---

func TestParseScript_Error_FnCallUndefined(t *testing.T) {
	input := "nonexistent(\"arg\")"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for calling undefined function")
	}
	if !containsSubstring(err.Error(), "nonexistent") {
		t.Errorf("error should mention function name 'nonexistent', got: %q", err.Error())
	}
	if !containsSubstring(err.Error(), "line 1") {
		t.Errorf("error should mention line number 'line 1', got: %q", err.Error())
	}
}

// --- 18.2-UNIT-015: [P0] ParseScript return 带值解析 (AC2) ---

func TestParseScript_Return_WithValue(t *testing.T) {
	input := "fn get()\n  return $result\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := script.Statements[0].FnDef.Body
	if len(body) != 1 {
		t.Fatalf("body len = %d, want 1", len(body))
	}
	if body[0].Kind != StmtReturn {
		t.Errorf("kind = %q, want %q", body[0].Kind, StmtReturn)
	}
	if body[0].Return == nil {
		t.Fatal("Return should not be nil")
	}
	if body[0].Return.Value != "$result" {
		t.Errorf("value = %q, want %q", body[0].Return.Value, "$result")
	}
}

// --- 18.2-UNIT-016: [P1] ParseScript return 不带值解析 (AC9) ---

func TestParseScript_Return_NoValue(t *testing.T) {
	input := "fn noop()\n  return\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := script.Statements[0].FnDef.Body
	if len(body) != 1 {
		t.Fatalf("body len = %d, want 1", len(body))
	}
	if body[0].Kind != StmtReturn {
		t.Errorf("kind = %q, want %q", body[0].Kind, StmtReturn)
	}
	if body[0].Return.Value != "" {
		t.Errorf("value = %q, want empty string", body[0].Return.Value)
	}
}

// --- 18.2-UNIT-017: [P0] ParseScript return 带字面量值 (AC2) ---

func TestParseScript_Return_LiteralValue(t *testing.T) {
	input := "fn greet()\n  return \"hello\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ret := script.Statements[0].FnDef.Body[0]
	if ret.Kind != StmtReturn {
		t.Errorf("kind = %q, want %q", ret.Kind, StmtReturn)
	}
	if ret.Return.Value != "hello" {
		t.Errorf("value = %q, want %q", ret.Return.Value, "hello")
	}
}

// --- 18.2-UNIT-018: [P1] ParseScript return 在顶层 → 错误 ---

func TestParseScript_Error_ReturnAtTopLevel(t *testing.T) {
	input := "return $x"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for return at top level")
	}
}

// --- 18.2-UNIT-019: [P0] ParseScript fn 大小写不敏感 ---

func TestParseScript_FnCaseInsensitive(t *testing.T) {
	for _, kw := range []string{
		"FN greet()\n  spawn \"hi\"\nEND",
		"Fn greet()\n  spawn \"hi\"\nEnd",
		"fn greet()\n  spawn \"hi\"\nend",
	} {
		script, err := ParseScript(kw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", kw, err)
		}
		if len(script.Statements) != 1 || script.Statements[0].Kind != StmtFnDef {
			t.Errorf("failed to parse case-insensitive fn: %q", kw)
		}
	}
}

// --- 18.2-UNIT-020: [P0] ParseScript return 带 captures 属性值 (AC2) ---

func TestParseScript_Return_CaptureProperty(t *testing.T) {
	input := "fn get_result()\n  r = spawn \"任务\"\n  return $r.result\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := script.Statements[0].FnDef.Body
	if len(body) != 2 {
		t.Fatalf("body len = %d, want 2", len(body))
	}
	ret := body[1]
	if ret.Kind != StmtReturn {
		t.Errorf("kind = %q, want %q", ret.Kind, StmtReturn)
	}
	if ret.Return.Value != "$r.result" {
		t.Errorf("value = %q, want %q", ret.Return.Value, "$r.result")
	}
}

// --- 18.2-UNIT-021: [P0] ScriptExecutor 函数定义 + 调用 → 参数正确传递 (AC1) ---

func TestScriptExecutor_FnCallBasic(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "分析完成", exitCode: 0, tokens: 100},
		},
	}

	input := "fn analyze(file)\n  spawn \"分析 ${file}\"\nend\nanalyze(\"config.yaml\")"
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

	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(spawner.calls))
	}
	if spawner.calls[0].intent != "分析 config.yaml" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "分析 config.yaml")
	}
	if result.TotalTokens != 100 {
		t.Errorf("tokens = %d, want 100", result.TotalTokens)
	}
}

// --- 18.2-UNIT-022: [P0] ScriptExecutor 函数 return 值捕获到赋值变量 (AC2) ---

func TestScriptExecutor_FnReturnValueCapture(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "分析结果", exitCode: 0, tokens: 100},
		},
	}

	input := "fn analyze(file)\n  r = spawn \"分析 ${file}\"\n  return $r\nend\noutput = analyze(\"config.yaml\")"
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

	val, ok := env.Get("output")
	if !ok {
		t.Fatal("variable 'output' should be set")
	}
	if val != "分析结果" {
		t.Errorf("output = %q, want %q", val, "分析结果")
	}
}

// --- 18.2-UNIT-023: [P1] ScriptExecutor 无 return → 赋值变量为空字符串 (AC9) ---

func TestScriptExecutor_FnNoReturn_EmptyString(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "fn setup()\n  spawn \"初始化\"\nend\nresult = setup()"
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

	val, ok := env.Get("result")
	if !ok {
		t.Fatal("variable 'result' should be set")
	}
	if val != "" {
		t.Errorf("result = %q, want empty string (no return)", val)
	}
}

// --- 18.2-UNIT-024: [P0] ScriptExecutor 参数作用域隔离（外部变量恢复）(AC8) ---

func TestScriptExecutor_FnParamScopeIsolation(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "export file=original.go\nfn process(file)\n  spawn \"处理 ${file}\"\nend\nprocess(\"override.go\")"
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

	if spawner.calls[0].intent != "处理 override.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "处理 override.go")
	}

	val, ok := env.Get("file")
	if !ok || val != "original.go" {
		t.Errorf("file = %q, want %q (should be restored after fn return)", val, "original.go")
	}
}

// --- 18.2-UNIT-025: [P0] ScriptExecutor 嵌套函数调用（A 调 B）(AC6) ---

func TestScriptExecutor_FnNestedCalls(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "内层结果", exitCode: 0, tokens: 50},
			{result: "外层结果", exitCode: 0, tokens: 100},
		},
	}

	input := "fn inner(x)\n  spawn \"内层 ${x}\"\n  return $x\nend\nfn outer(y)\n  inner($y)\n  spawn \"外层 ${y}\"\nend\nouter(\"data\")"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	if spawner.calls[0].intent != "内层 data" {
		t.Errorf("call 0 intent = %q, want %q", spawner.calls[0].intent, "内层 data")
	}
	if spawner.calls[1].intent != "外层 data" {
		t.Errorf("call 1 intent = %q, want %q", spawner.calls[1].intent, "外层 data")
	}
}

// --- 18.2-UNIT-026: [P0] ScriptExecutor 嵌套函数调用参数独立 (AC6) ---

func TestScriptExecutor_FnNestedCalls_ParamIndependent(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok-inner", exitCode: 0, tokens: 50},
			{result: "ok-outer", exitCode: 0, tokens: 50},
		},
	}

	input := "fn inner(val)\n  spawn \"inner=${val}\"\nend\nfn outer(val)\n  inner(\"inner_data\")\n  spawn \"outer=${val}\"\nend\nouter(\"outer_data\")"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}
	if spawner.calls[0].intent != "inner=inner_data" {
		t.Errorf("call 0 intent = %q, want %q", spawner.calls[0].intent, "inner=inner_data")
	}
	if spawner.calls[1].intent != "outer=outer_data" {
		t.Errorf("call 1 intent = %q, want %q (val should be restored after inner call)", spawner.calls[1].intent, "outer=outer_data")
	}
}

// --- 18.2-UNIT-027: [P0] ScriptExecutor 调用未定义函数 → 运行时错误 (AC7) ---

func TestScriptExecutor_FnCallUndefined_RuntimeError(t *testing.T) {
	spawner := &mockSpawner{}

	input := "fn dummy()\n  spawn \"dummy\"\nend\nundefined_func()"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for calling undefined function 'undefined_func'")
	}
	if !containsSubstring(err.Error(), "undefined_func") {
		t.Errorf("error should mention 'undefined_func', got: %q", err.Error())
	}
	_ = spawner
}

// --- 18.2-UNIT-028: [P0] ScriptExecutor 零参数函数调用 (AC4) ---

func TestScriptExecutor_FnCallZeroArgs(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "initialized", exitCode: 0, tokens: 30},
		},
	}

	input := "fn init_system()\n  spawn \"初始化系统\"\nend\ninit_system()"
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

	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(spawner.calls))
	}
	if spawner.calls[0].intent != "初始化系统" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "初始化系统")
	}
}

// --- 18.2-UNIT-029: [P0] ScriptExecutor 函数体内 for/if 嵌套执行 (AC5) ---

func TestScriptExecutor_FnBodyWithForAndIf(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "match-b", exitCode: 0, tokens: 50},
		},
	}

	input := "fn scan(target)\n  for item in [a, b, c]\n    if $item == b\n      spawn \"匹配 ${target} ${item}\"\n    end\n  end\nend\nscan(\"project\")"
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

	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (only item 'b' matches)", len(spawner.calls))
	}
	if spawner.calls[0].intent != "匹配 project b" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "匹配 project b")
	}
}

// --- 18.2-UNIT-030: [P0] ScriptExecutor 函数体内 spawn on-error (AC5) ---

func TestScriptExecutor_FnBodyWithOnError(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "fail", exitCode: 1, tokens: 50},
			{result: "recovered", exitCode: 0, tokens: 50},
		},
	}

	input := "fn risky()\n  spawn \"危险操作\" on-error spawn \"恢复\"\nend\nrisky()"
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (main + on-error handler)", len(spawner.calls))
	}
}

// --- 18.2-UNIT-031: [P0] ScriptExecutor return 中途退出函数 (AC2) ---

func TestScriptExecutor_FnReturnEarlyExit(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "first", exitCode: 0, tokens: 50},
		},
	}

	input := "fn early()\n  spawn \"第一步\"\n  return \"done\"\n  spawn \"不应执行\"\nend\nresult = early()"
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (spawn after return should not execute)", len(spawner.calls))
	}

	val, ok := env.Get("result")
	if !ok || val != "done" {
		t.Errorf("result = %q, want %q", val, "done")
	}
}

// --- 18.2-UNIT-032: [P0] ScriptExecutor return 不带值 → 返回空字符串 (AC9) ---

func TestScriptExecutor_FnReturnEmpty(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 30},
		},
	}

	input := "fn void_fn()\n  spawn \"work\"\n  return\nend\nresult = void_fn()"
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

	val, ok := env.Get("result")
	if !ok {
		t.Fatal("variable 'result' should be set")
	}
	if val != "" {
		t.Errorf("result = %q, want empty string (bare return)", val)
	}
}

// --- 18.2-CR-001: [P0] return 在 for 循环内立即终止函数（组合矩阵验证）---

func TestScriptExecutor_FnReturnInForLoop(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "found-a", exitCode: 0, tokens: 50},
		},
	}

	input := "fn find_first()\n  for item in [a, b, c]\n    spawn \"检查 ${item}\"\n    return $item\n  end\nend\nfound = find_first()"
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (return should exit fn after first iteration)", len(spawner.calls))
	}
	val, ok := env.Get("found")
	if !ok || val != "a" {
		t.Errorf("found = %q, want %q", val, "a")
	}
}

// --- 18.2-CR-002: [P0] return 在 if/else 内正确退出函数（组合矩阵验证）---

func TestScriptExecutor_FnReturnInIfBranch(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "yes", exitCode: 0, tokens: 50},
		},
	}

	input := "fn check(val)\n  if $val == yes\n    spawn \"匹配\"\n    return \"matched\"\n  end\n  spawn \"不应执行\"\nend\nexport val=yes\nresult = check($val)"
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (return in if should exit fn)", len(spawner.calls))
	}
	val, ok := env.Get("result")
	if !ok || val != "matched" {
		t.Errorf("result = %q, want %q", val, "matched")
	}
}

// --- 18.2-CR-003: [P0] exit 在函数体内终止整个脚本（组合矩阵验证）---

func TestScriptExecutor_ExitInFnBody_TerminatesScript(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "before exit", exitCode: 0, tokens: 50},
		},
	}

	input := "fn fatal()\n  spawn \"致命错误\"\n  exit 42\nend\nfatal()\nspawn \"不应执行\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v (exit should not be treated as error)", err)
	}

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (spawn after exit should not run)", len(spawner.calls))
	}
	if result.LastExitCode != 42 {
		t.Errorf("exit code = %d, want 42", result.LastExitCode)
	}
}

// --- 18.2-CR-004: [P1] fn 参数与 for 循环变量同名时互不干扰（组合矩阵验证）---

func TestScriptExecutor_FnParamVsForLoopVar(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok-fn", exitCode: 0, tokens: 50},
			{result: "ok-x", exitCode: 0, tokens: 50},
			{result: "ok-fn2", exitCode: 0, tokens: 50},
			{result: "ok-y", exitCode: 0, tokens: 50},
		},
	}

	input := "fn work(item)\n  spawn \"fn-item=${item}\"\nend\nfor item in [x, y]\n  work(\"fn_value\")\n  spawn \"loop-item=${item}\"\nend"
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

	if len(spawner.calls) < 2 {
		t.Fatalf("calls = %d, want at least 2", len(spawner.calls))
	}
	if spawner.calls[0].intent != "fn-item=fn_value" {
		t.Errorf("call 0 intent = %q, want %q", spawner.calls[0].intent, "fn-item=fn_value")
	}
	if spawner.calls[1].intent != "loop-item=x" {
		t.Errorf("call 1 intent = %q, want %q (loop var should be restored after fn call)", spawner.calls[1].intent, "loop-item=x")
	}
}

// --- 18.2-CR-005: [P1] MaxCallDepth 递归深度保护（边界测试）---

func TestScriptExecutor_FnMaxCallDepth(t *testing.T) {
	spawner := &mockSpawner{
		results: make([]mockResult, MaxCallDepth+10),
	}
	for i := range spawner.results {
		spawner.results[i] = mockResult{result: "ok", exitCode: 0, tokens: 1}
	}

	input := "fn recurse(n)\n  spawn \"depth=${n}\"\n  recurse($n)\nend\nrecurse(\"start\")"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for exceeding MaxCallDepth, got nil")
	}
	if !containsSubstring(err.Error(), "call depth") && !containsSubstring(err.Error(), "recursion") {
		t.Errorf("error should mention call depth or recursion limit, got: %q", err.Error())
	}
}

// --- 18.2-CR-006: [P1] fn 定义不计入执行阶段数 ---

func TestCountStages_FnDefZero(t *testing.T) {
	input := "fn noop()\n  spawn \"a\"\nend\nspawn \"main\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	// fn def = 0 stages, fn call = 1 stage, plain spawn = 1 stage
	// Only the plain spawn "main" counts here (fn def body is counted when called)
	if total < 1 {
		t.Errorf("total stages = %d, want at least 1 (plain spawn)", total)
	}
}

// --- 18.2-CR-007: [P1] fn 调用计为一个执行阶段 ---

func TestCountStages_FnCallOne(t *testing.T) {
	input := "fn work()\n  spawn \"a\"\nend\nwork()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total < 1 {
		t.Errorf("total stages = %d, want at least 1 (fn call counts as a stage)", total)
	}
}

// --- 18.2-CR-008: [P1] fn 参数名是保留关键字 → 错误 ---

func TestParseScript_Error_FnParamReservedKeyword(t *testing.T) {
	input := "fn bad(for)\n  spawn \"test\"\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for parameter name 'for' (reserved keyword)")
	}
}

// --- 18.2-CR-014: [P1] fn 参数名非法标识符 → 错误 ---

func TestParseScript_Error_FnParamInvalidIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"numeric param", "fn bad(123)\n  spawn \"test\"\nend"},
		{"hyphenated param", "fn bad(a-b)\n  spawn \"test\"\nend"},
		{"spaced param", "fn bad(a b)\n  spawn \"test\"\nend"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.input)
			if err == nil {
				t.Errorf("expected error for invalid parameter name: %q", tc.input)
			}
		})
	}
}

// --- 18.2-CR-009: [P0] fn 定义和调用与 spawn 赋值共存 ---

func TestScriptExecutor_FnCallAndSpawnAssignment(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "fn-result", exitCode: 0, tokens: 50},
			{result: "spawn-result", exitCode: 0, tokens: 50},
		},
	}

	input := "fn helper()\n  spawn \"辅助任务\"\n  return \"helper-done\"\nend\nresult = helper()\nspawn_result = spawn \"主任务 $result\""
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

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(spawner.calls))
	}

	val, ok := env.Get("result")
	if !ok || val != "helper-done" {
		t.Errorf("result = %q, want %q", val, "helper-done")
	}

	if spawner.calls[1].intent != "主任务 helper-done" {
		t.Errorf("spawn intent = %q, want %q", spawner.calls[1].intent, "主任务 helper-done")
	}
}

// --- 18.2-CR-010: [P0] return 带 captures.result 属性返回 ---

func TestScriptExecutor_FnReturnCaptureResult(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "任务输出内容", exitCode: 0, tokens: 100},
		},
	}

	input := "fn get_output()\n  r = spawn \"执行任务\"\n  return $r.result\nend\noutput = get_output()"
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

	val, ok := env.Get("output")
	if !ok || val != "任务输出内容" {
		t.Errorf("output = %q, want %q", val, "任务输出内容")
	}
}

// --- 18.2-CR-011: [P1] ErrFnReturn 不泄漏到函数外部 ---

func TestScriptExecutor_FnReturnDoesNotLeak(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
			{result: "after", exitCode: 0, tokens: 50},
		},
	}

	input := "fn returner()\n  spawn \"work\"\n  return \"val\"\nend\nreturner()\nspawn \"后续应执行\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v (ErrFnReturn should not leak)", err)
	}

	if len(spawner.calls) != 2 {
		t.Errorf("calls = %d, want 2 (spawn after fn call should execute)", len(spawner.calls))
	}
}

// --- 18.2-CR-012: [P1] return 在 while 循环内退出函数 ---

func TestScriptExecutor_FnReturnInWhileLoop(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "found", exitCode: 0, tokens: 50},
		},
	}

	input := "fn search()\n  export counter=1\n  while $counter != 0\n    spawn \"搜索\"\n    return \"found\"\n  end\nend\nresult = search()"
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

	if len(spawner.calls) != 1 {
		t.Errorf("calls = %d, want 1 (return should exit while loop and fn)", len(spawner.calls))
	}
	val, ok := env.Get("result")
	if !ok || val != "found" {
		t.Errorf("result = %q, want %q", val, "found")
	}
}

// --- 18.2-CR-013: [P1] fn 调用参数使用变量引用 ---

func TestScriptExecutor_FnCallWithVarArgs(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "fn process(target)\n  spawn \"处理 ${target}\"\nend\nexport MY_FILE=app.go\nprocess($MY_FILE)"
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

	if spawner.calls[0].intent != "处理 app.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "处理 app.go")
	}
}

// Ensure fmt, strings, context, time packages are used.
var (
	_ = fmt.Errorf
	_ = strings.Contains
	_ = context.Background
	_ = time.Duration(0)
)

// --- ResultLastLine flag tests ---

func TestParseScript_ResultLastLine(t *testing.T) {
	input := `decision = spawn "分析状态" --agent=planner --result-last-line`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(script.Statements))
	}
	stmt := script.Statements[0]
	if stmt.Spawn == nil {
		t.Fatal("Spawn should not be nil")
	}
	if !stmt.Spawn.ResultLastLine {
		t.Error("ResultLastLine = false, want true")
	}
	if stmt.Spawn.Agent != "planner" {
		t.Errorf("agent = %q, want %q", stmt.Spawn.Agent, "planner")
	}
	if stmt.Assign != "decision" {
		t.Errorf("assign = %q, want %q", stmt.Assign, "decision")
	}
}

func TestParseScript_ResultLastLine_OnError(t *testing.T) {
	input := `spawn "test" --result-last-line on-error spawn "recover" --result-last-line`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stmt := script.Statements[0]
	if !stmt.Spawn.ResultLastLine {
		t.Error("main spawn ResultLastLine = false, want true")
	}
	if stmt.OnError == nil {
		t.Fatal("OnError should not be nil")
	}
	if !stmt.OnError.ResultLastLine {
		t.Error("on-error ResultLastLine = false, want true")
	}
}

func TestExtractLastLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"multiline", "正在分析...\n\n所有 Story 均为 backlog\n\nmanage", "manage"},
		{"single_line", "manage", "manage"},
		{"trailing_newlines", "manage\n\n\n", "manage"},
		{"empty", "", ""},
		{"whitespace_last", "hello\n  world  \n", "world"},
		{"only_newlines", "\n\n\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractLastLine(tc.input)
			if got != tc.want {
				t.Errorf("extractLastLine(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestExecute_ResultLastLine_StripsToLastLine(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "正在分析...\n\n所有 Story 均为 backlog\n\nmanage", exitCode: 0, tokens: 100},
		},
	}
	input := `decision = spawn "分析" --agent=planner --result-last-line`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	result, err := exec.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.LastResult != "manage" {
		t.Errorf("LastResult = %q, want %q", result.LastResult, "manage")
	}
	if cap, ok := exec.captures["decision"]; !ok {
		t.Error("capture 'decision' not found")
	} else if cap.Result != "manage" {
		t.Errorf("capture result = %q, want %q", cap.Result, "manage")
	}
}
