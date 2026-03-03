package shell

import (
	"context"
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

// --- 11.3-UNIT-EXTRA-003: 空 then body ---

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
