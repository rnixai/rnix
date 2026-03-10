package shell

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 18.5: 模块化与脚本执行
//
// Tests reference StmtSource, SourceStmt, Statement.Source,
// FileReader, NewScriptExecutorWithReader, stripShebang
// — which do NOT exist yet → compile failure = RED phase.
//
// mockSpawner is reused from pipe_test.go (same package).
// mockFileReader provides in-memory file system for testing.
// ============================================================

// mockFileReader provides an in-memory file system for source tests.
type mockFileReader struct {
	files map[string]string // absolute path → content
}

func (m *mockFileReader) ReadFile(path string) (string, error) {
	content, ok := m.files[path]
	if !ok {
		return "", &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	return content, nil
}

// ensure mockFileReader satisfies FileReader interface
var _ FileReader = (*mockFileReader)(nil)

// ========================= PARSING ===========================

// --- 18.5-UNIT-001: [P0] ParseScript source 基本解析 (AC1) ---

func TestParseScript_Source_Basic(t *testing.T) {
	input := `source ./lib/helpers.ash`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtSource {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtSource)
	}
	if stmt.Source == nil {
		t.Fatal("Source should not be nil")
	}
	if stmt.Source.Path != "./lib/helpers.ash" {
		t.Errorf("path = %q, want %q", stmt.Source.Path, "./lib/helpers.ash")
	}
}

// --- 18.5-UNIT-002: [P0] ParseScript source 带引号路径 (AC1) ---

func TestParseScript_Source_QuotedPath(t *testing.T) {
	input := `source "./lib/my helpers.ash"`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtSource {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtSource)
	}
	if stmt.Source.Path != "./lib/my helpers.ash" {
		t.Errorf("path = %q, want %q", stmt.Source.Path, "./lib/my helpers.ash")
	}
}

// --- 18.5-UNIT-003: [P0] ParseScript source 单引号路径 ---

func TestParseScript_Source_SingleQuotedPath(t *testing.T) {
	input := "source './lib/helpers.ash'"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtSource {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtSource)
	}
	if stmt.Source.Path != "./lib/helpers.ash" {
		t.Errorf("path = %q, want %q", stmt.Source.Path, "./lib/helpers.ash")
	}
}

// --- 18.5-UNIT-004: [P0] ParseScript source 无参数 → 解析错误 (AC4) ---

func TestParseScript_Source_NoPath(t *testing.T) {
	input := "source"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for source without path")
	}
	if !strings.Contains(err.Error(), "source") {
		t.Errorf("error should mention 'source', got: %q", err.Error())
	}
}

// --- 18.5-UNIT-005: [P1] ParseScript source 在函数内 ---

func TestParseScript_Source_InFunction(t *testing.T) {
	input := "fn setup()\n  source ./lib/helpers.ash\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	body := script.Statements[0].FnDef.Body
	if len(body) != 1 {
		t.Fatalf("fn body len = %d, want 1", len(body))
	}
	if body[0].Kind != StmtSource {
		t.Errorf("body[0] kind = %q, want %q", body[0].Kind, StmtSource)
	}
}

// --- 18.5-UNIT-006: [P0] ParseScript shebang 行被正确跳过 (AC3) ---

func TestParseScript_Source_Shebang(t *testing.T) {
	input := "#!/usr/bin/env rnix run\nexport KEY=val\nspawn \"test\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2 (shebang should be skipped)", len(script.Statements))
	}
	if script.Statements[0].Kind != StmtExport {
		t.Errorf("stmt 0 kind = %q, want %q", script.Statements[0].Kind, StmtExport)
	}
	if script.Statements[1].Kind != StmtSpawn {
		t.Errorf("stmt 1 kind = %q, want %q", script.Statements[1].Kind, StmtSpawn)
	}
}

// --- 18.5-UNIT-007: [P0] stripShebang 基本功能 ---

func TestStripShebang_Present(t *testing.T) {
	input := "#!/usr/bin/env rnix run\nexport A=1\nspawn \"test\""
	result := stripShebang(input)
	if strings.HasPrefix(result, "#!") {
		t.Errorf("shebang should be stripped, got: %q", result[:40])
	}
	if !strings.HasPrefix(result, "export A=1") {
		t.Errorf("content after shebang should start with 'export A=1', got: %q", result[:20])
	}
}

func TestStripShebang_Absent(t *testing.T) {
	input := "export A=1\nspawn \"test\""
	result := stripShebang(input)
	if result != input {
		t.Errorf("stripShebang should return input unchanged when no shebang, got: %q", result)
	}
}

func TestStripShebang_OnlyShebang(t *testing.T) {
	input := "#!/usr/bin/env rnix run"
	result := stripShebang(input)
	if result != "" {
		t.Errorf("stripShebang of shebang-only content should return empty, got: %q", result)
	}
}

func TestStripShebang_EmptyInput(t *testing.T) {
	result := stripShebang("")
	if result != "" {
		t.Errorf("stripShebang of empty string should return empty, got: %q", result)
	}
}

// --- 18.5-UNIT-008: [P1] ParseScript source 大小写不敏感 ---

func TestParseScript_Source_CaseInsensitive(t *testing.T) {
	for _, kw := range []string{"SOURCE ./lib.ash", "Source ./lib.ash", "source ./lib.ash"} {
		script, err := ParseScript(kw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", kw, err)
		}
		if script.Statements[0].Kind != StmtSource {
			t.Errorf("%q: kind = %q, want %q", kw, script.Statements[0].Kind, StmtSource)
		}
	}
}

// --- 18.5-UNIT-009: [P0] ParseScript source 行号记录正确 ---

func TestParseScript_Source_LineNumber(t *testing.T) {
	input := "export A=1\nsource ./helpers.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}
	if script.Statements[1].Line != 2 {
		t.Errorf("source statement line = %d, want 2", script.Statements[1].Line)
	}
}

// ========================= EXECUTION ==========================

// --- 18.5-UNIT-010: [P0] ScriptExecutor source 后函数可调用 (AC1, AC7) ---

func TestScriptExecutor_Source_FunctionsAvailable(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/lib/helpers.ash": "fn greet(name)\n  spawn \"你好 ${name}\"\nend",
		},
	}
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "问候完成", exitCode: 0, tokens: 50},
		},
	}

	input := "source ./lib/helpers.ash\ngreet(\"世界\")"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(spawner.calls))
	}
	if spawner.calls[0].intent != "你好 世界" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "你好 世界")
	}
}

// --- 18.5-UNIT-011: [P0] ScriptExecutor source 后变量可引用 (AC7) ---

func TestScriptExecutor_Source_VariablesAvailable(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/lib/config.ash": "export VERSION=1.0.0\nexport APP_NAME=rnix",
		},
	}
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "source ./lib/config.ash\nspawn \"部署 ${APP_NAME} v${VERSION}\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if spawner.calls[0].intent != "部署 rnix v1.0.0" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "部署 rnix v1.0.0")
	}
}

// --- 18.5-UNIT-012: [P0] ScriptExecutor source 文件不存在 → 含行号错误 (AC5) ---

func TestScriptExecutor_Source_FileNotFound(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{},
	}
	spawner := &mockSpawner{}

	input := "source ./nonexistent.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for non-existent source file")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "nonexistent.ash") {
		t.Errorf("error should mention file path, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "line") || !strings.Contains(errMsg, "1") {
		t.Errorf("error should mention line number, got: %q", errMsg)
	}
}

// --- 18.5-UNIT-013: [P0] ScriptExecutor source 循环引用检测 A→B→A (AC6) ---

func TestScriptExecutor_Source_CircularDetection(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/a.ash": "source ./b.ash",
			"/project/b.ash": "source ./a.ash",
		},
	}
	spawner := &mockSpawner{}

	input := "source ./a.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for circular source reference")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "circular") {
		t.Errorf("error should mention 'circular', got: %q", errMsg)
	}
}

// --- 18.5-UNIT-014: [P0] ScriptExecutor source 自引用 A→A (AC6) ---

func TestScriptExecutor_Source_SelfReference(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/self.ash": "source ./self.ash",
		},
	}
	spawner := &mockSpawner{}

	input := "source ./self.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for self-referencing source")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error should mention 'circular', got: %q", err.Error())
	}
}

// --- 18.5-UNIT-015: [P0] ScriptExecutor source 相对路径基于 scriptDir (AC1) ---

func TestScriptExecutor_Source_RelativePath(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/lib/utils.ash": "export UTIL_VER=2.0",
		},
	}
	spawner := &mockSpawner{}

	input := "source ./lib/utils.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	val, ok := env.Get("UTIL_VER")
	if !ok || val != "2.0" {
		t.Errorf("UTIL_VER = %q, want %q", val, "2.0")
	}
}

// --- 18.5-UNIT-016: [P1] ScriptExecutor source 路径变量展开 (AC1) ---

func TestScriptExecutor_Source_VariableInPath(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/libs/helpers.ash": "export HELPER=loaded",
		},
	}
	spawner := &mockSpawner{}

	input := "export lib_dir=libs\nsource ./${lib_dir}/helpers.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	val, ok := env.Get("HELPER")
	if !ok || val != "loaded" {
		t.Errorf("HELPER = %q, want %q", val, "loaded")
	}
}

// --- 18.5-UNIT-017: [P1] ScriptExecutor sourced 文件语法错误 → 含文件名和行号 (AC4) ---

func TestScriptExecutor_Source_ParseError(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/bad.ash": "if $x == 1\n  spawn \"A\"",
		},
	}
	spawner := &mockSpawner{}

	input := "source ./bad.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for source file with syntax error")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "bad.ash") {
		t.Errorf("error should mention source file name, got: %q", errMsg)
	}
}

// --- 18.5-UNIT-018: [P0] ScriptExecutor sourced 脚本含 spawn，正确执行 ---

func TestScriptExecutor_Source_WithSpawn(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/init.ash": "spawn \"初始化环境\"",
		},
	}
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "环境就绪", exitCode: 0, tokens: 80},
			{result: "主任务完成", exitCode: 0, tokens: 120},
		},
	}

	input := "source ./init.ash\nspawn \"执行主任务\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	result, err := executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(spawner.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (sourced spawn + main spawn)", len(spawner.calls))
	}
	if spawner.calls[0].intent != "初始化环境" {
		t.Errorf("call[0] intent = %q, want %q", spawner.calls[0].intent, "初始化环境")
	}
	if spawner.calls[1].intent != "执行主任务" {
		t.Errorf("call[1] intent = %q, want %q", spawner.calls[1].intent, "执行主任务")
	}
	if result.TotalTokens != 200 {
		t.Errorf("TotalTokens = %d, want 200", result.TotalTokens)
	}
}

// --- 18.5-UNIT-019: [P1] ScriptExecutor 链式 source A→B→C（非循环）---

func TestScriptExecutor_Source_ChainedSource(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/a.ash": "source ./b.ash\nexport FROM_A=yes",
			"/project/b.ash": "source ./c.ash\nexport FROM_B=yes",
			"/project/c.ash": "export FROM_C=yes",
		},
	}
	spawner := &mockSpawner{}

	input := "source ./a.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	for _, name := range []string{"FROM_A", "FROM_B", "FROM_C"} {
		val, ok := env.Get(name)
		if !ok || val != "yes" {
			t.Errorf("%s = %q (exists=%v), want %q", name, val, ok, "yes")
		}
	}
}

// --- 18.5-UNIT-020: [P1] ScriptExecutor 后 source 的函数覆盖先前同名函数 ---

func TestScriptExecutor_Source_OverrideFunction(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/v1.ash": "fn greet()\n  spawn \"hello v1\"\nend",
			"/project/v2.ash": "fn greet()\n  spawn \"hello v2\"\nend",
		},
	}
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "source ./v1.ash\nsource ./v2.ash\ngreet()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(spawner.calls))
	}
	if spawner.calls[0].intent != "hello v2" {
		t.Errorf("intent = %q, want %q (v2 should override v1)", spawner.calls[0].intent, "hello v2")
	}
}

// --- 18.5-UNIT-021: [P1] ScriptExecutor source 空文件为 no-op ---

func TestScriptExecutor_Source_EmptyFile(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/empty.ash": "",
		},
	}
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "source ./empty.ash\nspawn \"后续任务\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(spawner.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (empty source is no-op)", len(spawner.calls))
	}
}

// --- 18.5-UNIT-022: [P0] ScriptExecutor source 文件含 shebang → 首行跳过 ---

func TestScriptExecutor_Source_FileWithShebang(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/lib.ash": "#!/usr/bin/env rnix run\nexport LIB=loaded",
		},
	}
	spawner := &mockSpawner{}

	input := "source ./lib.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	val, ok := env.Get("LIB")
	if !ok || val != "loaded" {
		t.Errorf("LIB = %q, want %q (shebang in sourced file should be skipped)", val, "loaded")
	}
}

// ======================= COMBINATIONS =========================

// --- 18.5-COMB-001: [P1] for 循环内使用 source ---

func TestScriptExecutor_Source_InForLoop(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/mod_a.ash": "export LOADED_A=yes",
			"/project/mod_b.ash": "export LOADED_B=yes",
		},
	}
	spawner := &mockSpawner{}

	input := "for mod in [mod_a.ash, mod_b.ash]\n  source ./${mod}\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	a, _ := env.Get("LOADED_A")
	b, _ := env.Get("LOADED_B")
	if a != "yes" || b != "yes" {
		t.Errorf("LOADED_A=%q, LOADED_B=%q, want both 'yes'", a, b)
	}
}

// --- 18.5-COMB-002: [P1] if 块内条件 source ---

func TestScriptExecutor_Source_InIfBlock(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/debug.ash": "export DEBUG_MODE=on",
		},
	}
	spawner := &mockSpawner{}

	input := "export env=debug\nif $env == debug\n  source ./debug.ash\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	val, ok := env.Get("DEBUG_MODE")
	if !ok || val != "on" {
		t.Errorf("DEBUG_MODE = %q, want %q", val, "on")
	}
}

// --- 18.5-COMB-003: [P0] source 函数后在 parallel 中引用 ---

func TestScriptExecutor_Source_BeforeParallel(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/tasks.ash": "fn analyze(f)\n  spawn \"分析 ${f}\"\nend",
		},
	}
	spawner := &concurrentMockSpawner{
		results: map[string]mockResult{
			"分析 main.go": {result: "OK", exitCode: 0, tokens: 100},
			"分析 lib.go":  {result: "OK", exitCode: 0, tokens: 100},
		},
	}

	input := "source ./tasks.ash\nparallel\n  spawn \"分析 main.go\"\n  spawn \"分析 lib.go\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
}

// --- 18.5-COMB-004: [P1] source 后使用数组/映射 ---

func TestScriptExecutor_Source_WithDataStructures(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/project/data.ash": "files = [\"a.go\", \"b.go\"]\nconfig = {model: \"sonnet\"}",
		},
	}
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "source ./data.ash\nspawn \"使用 ${config.model} 分析 ${files[0]}\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if spawner.calls[0].intent != "使用 sonnet 分析 a.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "使用 sonnet 分析 a.go")
	}
}

// --- 18.5-COMB-005: [P0] source 在 parallel 块内被拒绝 ---

func TestParseScript_Source_RejectedInParallel(t *testing.T) {
	input := "parallel\n  source ./lib.ash\nend"
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for source inside parallel block")
	}
	if !strings.Contains(err.Error(), "parallel") {
		t.Errorf("error should mention 'parallel', got: %q", err.Error())
	}
}

// --- 18.5-COMB-007: [P0] countStagesInBlock source 本身 count 为 0 ---

func TestCountStages_SourceZero(t *testing.T) {
	input := "source ./lib.ash\nspawn \"main\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 1 {
		t.Errorf("stage count = %d, want 1 (source counts as 0, spawn counts as 1)", total)
	}
}

// --- 18.5-COMB-008: [P0] validateFnCalls 对 source 不报错 ---

func TestValidateFnCalls_SourceSkipped(t *testing.T) {
	input := "source ./lib.ash\nspawn \"test\""
	_, err := ParseScript(input)
	if err != nil {
		t.Fatalf("ParseScript should not error on source (fn validation should skip it): %v", err)
	}
}

// --- 18.5-UNIT-023: [P0] ScriptExecutor source 绝对路径直接使用 ---

func TestScriptExecutor_Source_AbsolutePath(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/absolute/path/lib.ash": "export ABS=loaded",
		},
	}
	spawner := &mockSpawner{}

	input := "source /absolute/path/lib.ash"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutorWithReader(spawner, env, reader)
	executor.SetScriptDir("/project")
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	val, ok := env.Get("ABS")
	if !ok || val != "loaded" {
		t.Errorf("ABS = %q, want %q", val, "loaded")
	}
}

// ======================== PERFORMANCE ==========================

// --- 18.5-PERF-001: [P0] source 解析性能 <= 50ms (NFR38) ---

func TestParseScript_Source_Performance_NFR38(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("export base=project\n")
	for i := range 50 {
		fmt.Fprintf(&sb, "source ./lib/mod_%d.ash\n", i)
	}
	for i := range 20 {
		fmt.Fprintf(&sb, "spawn \"任务%d\"\n", i)
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

// Ensure packages are used
var (
	_ = os.ErrNotExist
	_ = fmt.Errorf
	_ = time.Duration(0)
)
