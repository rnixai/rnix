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
