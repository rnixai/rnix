package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 18.3: 数据结构与字符串插值
//
// Tests reference StmtArrayLit, StmtMapLit, ArrayLitStmt,
// MapLitStmt, MapEntry, Environment.SetArray, Environment.GetArray,
// Environment.SetMap, Environment.GetMap, Environment.ExpandStrict
// — which do NOT exist yet → compile failure = RED phase.
//
// mockSpawner is reused from pipe_test.go (same package).
// ============================================================

// ========================= PARSING ===========================

// --- 18.3-UNIT-001: [P0] ParseScript 数组字面量赋值（双引号元素）(AC1) ---

func TestParseScript_ArrayLitAssignment(t *testing.T) {
	input := `files = ["a.go", "b.go", "c.go"]`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtArrayLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtArrayLit)
	}
	if stmt.Assign != "files" {
		t.Errorf("assign = %q, want %q", stmt.Assign, "files")
	}
	if stmt.ArrayLit == nil {
		t.Fatal("ArrayLit should not be nil")
	}
	if len(stmt.ArrayLit.Items) != 3 {
		t.Fatalf("items len = %d, want 3", len(stmt.ArrayLit.Items))
	}
	expected := []string{"a.go", "b.go", "c.go"}
	for i, want := range expected {
		if stmt.ArrayLit.Items[i] != want {
			t.Errorf("item[%d] = %q, want %q", i, stmt.ArrayLit.Items[i], want)
		}
	}
}

// --- 18.3-UNIT-002: [P0] ParseScript 数组字面量赋值（无引号元素）(AC1) ---

func TestParseScript_ArrayLitAssignment_Unquoted(t *testing.T) {
	input := `items = [alpha, beta, gamma]`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtArrayLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtArrayLit)
	}
	if stmt.ArrayLit == nil {
		t.Fatal("ArrayLit should not be nil")
	}
	if len(stmt.ArrayLit.Items) != 3 {
		t.Fatalf("items len = %d, want 3", len(stmt.ArrayLit.Items))
	}
	if stmt.ArrayLit.Items[0] != "alpha" {
		t.Errorf("item[0] = %q, want %q", stmt.ArrayLit.Items[0], "alpha")
	}
}

// --- 18.3-UNIT-003: [P0] ParseScript 映射字面量赋值（基本）(AC2) ---

func TestParseScript_MapLitAssignment(t *testing.T) {
	input := `config = {model: "sonnet", budget: 5000}`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("statements count = %d, want 1", len(script.Statements))
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtMapLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtMapLit)
	}
	if stmt.Assign != "config" {
		t.Errorf("assign = %q, want %q", stmt.Assign, "config")
	}
	if stmt.MapLit == nil {
		t.Fatal("MapLit should not be nil")
	}
	if len(stmt.MapLit.Entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(stmt.MapLit.Entries))
	}

	entry0 := stmt.MapLit.Entries[0]
	if entry0.Key != "model" || entry0.Value != "sonnet" {
		t.Errorf("entry[0] = {%q: %q}, want {model: sonnet}", entry0.Key, entry0.Value)
	}
	entry1 := stmt.MapLit.Entries[1]
	if entry1.Key != "budget" || entry1.Value != "5000" {
		t.Errorf("entry[1] = {%q: %q}, want {budget: 5000}", entry1.Key, entry1.Value)
	}
}

// --- 18.3-UNIT-004: [P1] ParseScript 映射字面量赋值（带引号键值）(AC2) ---

func TestParseScript_MapLitAssignment_QuotedValues(t *testing.T) {
	input := `meta = {name: "hello world", path: "/tmp/out"}`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtMapLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtMapLit)
	}
	if stmt.MapLit == nil {
		t.Fatal("MapLit should not be nil")
	}
	if len(stmt.MapLit.Entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(stmt.MapLit.Entries))
	}
	if stmt.MapLit.Entries[0].Value != "hello world" {
		t.Errorf("entry[0].Value = %q, want %q", stmt.MapLit.Entries[0].Value, "hello world")
	}
}

// --- 18.3-UNIT-005: [P1] ParseScript 单元素数组 (AC1 边界) ---

func TestParseScript_ArrayLitSingleElement(t *testing.T) {
	input := `single = ["only"]`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtArrayLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtArrayLit)
	}
	if stmt.ArrayLit == nil || len(stmt.ArrayLit.Items) != 1 {
		t.Fatalf("expected single-element array")
	}
	if stmt.ArrayLit.Items[0] != "only" {
		t.Errorf("item[0] = %q, want %q", stmt.ArrayLit.Items[0], "only")
	}
}

// --- 18.3-UNIT-006: [P1] ParseScript 单键映射 (AC2 边界) ---

func TestParseScript_MapLitSingleEntry(t *testing.T) {
	input := `opts = {verbose: true}`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stmt := script.Statements[0]
	if stmt.Kind != StmtMapLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtMapLit)
	}
	if stmt.MapLit == nil || len(stmt.MapLit.Entries) != 1 {
		t.Fatalf("expected single-entry map")
	}
	if stmt.MapLit.Entries[0].Key != "verbose" || stmt.MapLit.Entries[0].Value != "true" {
		t.Errorf("entry = {%q: %q}, want {verbose: true}", stmt.MapLit.Entries[0].Key, stmt.MapLit.Entries[0].Value)
	}
}

// --- 18.3-UNIT-007: [P1] ParseScript 数组 + 其他语句共存 (AC1) ---

func TestParseScript_ArrayLitWithOtherStatements(t *testing.T) {
	input := "files = [\"a.go\", \"b.go\"]\nspawn \"分析 ${files[0]}\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}
	if script.Statements[0].Kind != StmtArrayLit {
		t.Errorf("stmt 0 kind = %q, want %q", script.Statements[0].Kind, StmtArrayLit)
	}
	if script.Statements[1].Kind != StmtSpawn {
		t.Errorf("stmt 1 kind = %q, want %q", script.Statements[1].Kind, StmtSpawn)
	}
}

// --- 18.3-UNIT-008: [P1] ParseScript 映射 + 其他语句共存 (AC2) ---

func TestParseScript_MapLitWithOtherStatements(t *testing.T) {
	input := "config = {model: \"sonnet\"}\nspawn \"使用 ${config.model}\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statements count = %d, want 2", len(script.Statements))
	}
	if script.Statements[0].Kind != StmtMapLit {
		t.Errorf("stmt 0 kind = %q, want %q", script.Statements[0].Kind, StmtMapLit)
	}
	if script.Statements[1].Kind != StmtSpawn {
		t.Errorf("stmt 1 kind = %q, want %q", script.Statements[1].Kind, StmtSpawn)
	}
}

// --- 18.3-UNIT-009: [P1] ParseScript 错误 — 未闭合数组括号 ---

func TestParseScript_Error_UnclosedArrayBracket(t *testing.T) {
	input := `files = ["a.go", "b.go"`
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for unclosed array bracket")
	}
}

// --- 18.3-UNIT-010: [P1] ParseScript 错误 — 未闭合映射花括号 ---

func TestParseScript_Error_UnclosedMapBrace(t *testing.T) {
	input := `config = {model: "sonnet"`
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for unclosed map brace")
	}
}

// --- 18.3-UNIT-011: [P1] ParseScript 错误 — 映射缺少冒号 ---

func TestParseScript_Error_MapMissingColon(t *testing.T) {
	input := `config = {model "sonnet"}`
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for map entry missing colon separator")
	}
}

// --- 18.3-UNIT-012: [P1] ParseScript 保留关键字不能作为数组/映射变量名 ---

func TestParseScript_Error_DataLitReservedKeyword(t *testing.T) {
	tests := []string{
		`for = ["a", "b"]`,
		`spawn = {key: "val"}`,
		`if = ["x"]`,
	}
	for _, input := range tests {
		_, err := ParseScript(input)
		if err == nil {
			t.Errorf("expected error for reserved keyword as data variable: %q", input)
		}
	}
}

// ======================= EXECUTION ===========================

// --- 18.3-UNIT-013: [P0] ScriptExecutor 数组字面量 + 索引访问 (AC1) ---

func TestScriptExecutor_ArrayLit_IndexAccess(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 100},
		},
	}

	input := "files = [\"a.go\", \"b.go\", \"c.go\"]\nspawn \"分析 ${files[0]}\""
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
	if spawner.calls[0].intent != "分析 a.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "分析 a.go")
	}
}

// --- 18.3-UNIT-014: [P0] ScriptExecutor 数组不同索引访问 (AC1) ---

func TestScriptExecutor_ArrayLit_DifferentIndices(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
			{result: "ok", exitCode: 0, tokens: 50},
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"a.go\", \"b.go\", \"c.go\"]\nspawn \"${files[0]}\"\nspawn \"${files[1]}\"\nspawn \"${files[2]}\""
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

	if len(spawner.calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(spawner.calls))
	}
	if spawner.calls[0].intent != "a.go" {
		t.Errorf("call 0 intent = %q, want %q", spawner.calls[0].intent, "a.go")
	}
	if spawner.calls[1].intent != "b.go" {
		t.Errorf("call 1 intent = %q, want %q", spawner.calls[1].intent, "b.go")
	}
	if spawner.calls[2].intent != "c.go" {
		t.Errorf("call 2 intent = %q, want %q", spawner.calls[2].intent, "c.go")
	}
}

// --- 18.3-UNIT-015: [P0] ScriptExecutor 映射字面量 + 属性访问 (AC2) ---

func TestScriptExecutor_MapLit_PropertyAccess(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 100},
		},
	}

	input := "config = {model: \"sonnet\", budget: 5000}\nspawn \"使用 ${config.model}\""
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
	if spawner.calls[0].intent != "使用 sonnet" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "使用 sonnet")
	}
}

// --- 18.3-UNIT-016: [P0] ScriptExecutor 映射不同属性访问 (AC2) ---

func TestScriptExecutor_MapLit_DifferentProperties(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 100},
		},
	}

	input := "config = {model: \"sonnet\", budget: 5000}\nspawn \"模型=${config.model} 预算=${config.budget}\""
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

	if spawner.calls[0].intent != "模型=sonnet 预算=5000" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "模型=sonnet 预算=5000")
	}
}

// --- 18.3-UNIT-017: [P0] ScriptExecutor 字符串插值 spawn（已定义变量）(AC3) ---

func TestScriptExecutor_StringInterpolation_DefinedVar(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "分析完成", exitCode: 0, tokens: 100},
		},
	}

	input := "export file_path=main.go\nspawn \"分析 ${file_path} 的代码质量\""
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

	if spawner.calls[0].intent != "分析 main.go 的代码质量" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "分析 main.go 的代码质量")
	}
}

// --- 18.3-UNIT-018: [P0] ScriptExecutor 未定义变量插值 → 错误含行号和变量名 (AC4) ---

func TestScriptExecutor_UndefinedVar_ErrorWithLineAndName(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "spawn \"分析 ${undefined_var} 的代码\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for undefined variable in string interpolation")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "undefined_var") {
		t.Errorf("error should mention variable name 'undefined_var', got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "line 1") && !strings.Contains(errMsg, "1:") {
		t.Errorf("error should mention line number, got: %q", errMsg)
	}
}

// --- 18.3-UNIT-019: [P0] ScriptExecutor 数组越界 → 错误 (AC1 边界) ---

func TestScriptExecutor_ArrayOutOfBounds_Error(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"a.go\", \"b.go\"]\nspawn \"${files[99]}\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for array index out of bounds")
	}
	if !strings.Contains(err.Error(), "files") {
		t.Errorf("error should mention array name, got: %q", err.Error())
	}
}

// --- 18.3-UNIT-020: [P0] ScriptExecutor 映射不存在的键 → 错误 (AC2 边界) ---

func TestScriptExecutor_MapUndefinedKey_Error(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "config = {model: \"sonnet\"}\nspawn \"${config.nonexistent}\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for undefined map key")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Errorf("error should mention map name, got: %q", err.Error())
	}
}

// --- 18.3-UNIT-021: [P0] ScriptExecutor 数组 + for 循环迭代 (AC1 组合) ---

func TestScriptExecutor_ArrayLit_ForLoopIteration(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok-a", exitCode: 0, tokens: 10},
			{result: "ok-b", exitCode: 0, tokens: 10},
			{result: "ok-c", exitCode: 0, tokens: 10},
		},
	}

	input := "files = [\"a.go\", \"b.go\", \"c.go\"]\nfor f in $files\n  spawn \"分析 ${f}\"\nend"
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

	if len(spawner.calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(spawner.calls))
	}
	if spawner.calls[0].intent != "分析 a.go" {
		t.Errorf("call 0 intent = %q, want %q", spawner.calls[0].intent, "分析 a.go")
	}
	if spawner.calls[1].intent != "分析 b.go" {
		t.Errorf("call 1 intent = %q, want %q", spawner.calls[1].intent, "分析 b.go")
	}
	if spawner.calls[2].intent != "分析 c.go" {
		t.Errorf("call 2 intent = %q, want %q", spawner.calls[2].intent, "分析 c.go")
	}
}

// --- 18.3-UNIT-022: [P0] ScriptExecutor 映射属性在条件中使用 (AC2 组合) ---

func TestScriptExecutor_MapLit_PropertyInCondition(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "config = {model: \"sonnet\"}\nif ${config.model} == sonnet\n  spawn \"匹配 sonnet\"\nend"
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
		t.Fatalf("calls = %d, want 1 (condition should match)", len(spawner.calls))
	}
	if spawner.calls[0].intent != "匹配 sonnet" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "匹配 sonnet")
	}
}

// ====================== COMBINATIONS =========================

// --- 18.3-CR-001: [P0] 数组 + 映射混合使用 (AC1 + AC2 + AC3) ---

func TestScriptExecutor_ArrayAndMapCombined(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 100},
		},
	}

	input := "files = [\"main.go\", \"lib.go\"]\nconfig = {model: \"sonnet\"}\nspawn \"用 ${config.model} 分析 ${files[0]}\""
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

	if spawner.calls[0].intent != "用 sonnet 分析 main.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "用 sonnet 分析 main.go")
	}
}

// --- 18.3-CR-002: [P0] 数组 + for 循环 + 字符串插值 (AC1 + AC3) ---

func TestScriptExecutor_ArrayForLoopWithInterpolation(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	input := "files = [\"main.go\", \"util.go\"]\nexport action=review\nfor f in $files\n  spawn \"${action} ${f}\"\nend"
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
	if spawner.calls[0].intent != "review main.go" {
		t.Errorf("call 0 = %q, want %q", spawner.calls[0].intent, "review main.go")
	}
	if spawner.calls[1].intent != "review util.go" {
		t.Errorf("call 1 = %q, want %q", spawner.calls[1].intent, "review util.go")
	}
}

// --- 18.3-CR-003: [P0] 映射 + 函数参数传递 (AC2 + 函数) ---

func TestScriptExecutor_MapLit_InFunction(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "config = {model: \"sonnet\"}\nfn analyze(cfg_model)\n  spawn \"用 ${cfg_model} 分析\"\nend\nanalyze(${config.model})"
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

	if spawner.calls[0].intent != "用 sonnet 分析" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "用 sonnet 分析")
	}
}

// --- 18.3-CR-004: [P1] 数组 + 赋值 spawn 结果在后续索引引用 (AC1 + 赋值) ---

func TestScriptExecutor_ArrayLit_SpawnResultInteraction(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "发现3个问题", exitCode: 0, tokens: 100},
		},
	}

	input := "files = [\"a.go\", \"b.go\"]\nresult = spawn \"分析 ${files[0]}\"\nspawn \"结果: $result\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	spawner.results = append(spawner.results, mockResult{result: "ok", exitCode: 0, tokens: 50})
	_, err = executor.Execute(context.Background(), script)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if spawner.calls[0].intent != "分析 a.go" {
		t.Errorf("call 0 = %q, want %q", spawner.calls[0].intent, "分析 a.go")
	}
}

// --- 18.3-CR-005: [P1] 多行未定义变量 → 错误定位到正确行号 (AC4) ---

func TestScriptExecutor_UndefinedVar_CorrectLineNumber(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "export valid=ok\nspawn \"$valid\"\nspawn \"${missing_var}\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	spawner.results = append(spawner.results, mockResult{result: "ok", exitCode: 0, tokens: 50})
	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for undefined variable 'missing_var'")
	}
	if !strings.Contains(err.Error(), "missing_var") {
		t.Errorf("error should mention 'missing_var', got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "line 3") && !strings.Contains(err.Error(), "3:") {
		t.Errorf("error should mention line 3, got: %q", err.Error())
	}
}

// --- 18.3-CR-006: [P1] 映射 + 条件 + else 分支 (AC2 组合) ---

func TestScriptExecutor_MapLit_ConditionElse(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "使用了 opus", exitCode: 0, tokens: 50},
		},
	}

	input := "config = {model: \"opus\"}\nif ${config.model} == sonnet\n  spawn \"sonnet 路径\"\nelse\n  spawn \"其他模型: ${config.model}\"\nend"
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
	if spawner.calls[0].intent != "其他模型: opus" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "其他模型: opus")
	}
}

// --- 18.3-CR-007: [P1] 数组覆盖赋值 (AC1) ---

func TestScriptExecutor_ArrayLit_Overwrite(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"old.go\"]\nfiles = [\"new.go\"]\nspawn \"${files[0]}\""
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

	if spawner.calls[0].intent != "new.go" {
		t.Errorf("intent = %q, want %q (should use overwritten array)", spawner.calls[0].intent, "new.go")
	}
}

// --- 18.3-CR-008: [P1] 映射覆盖赋值 (AC2) ---

func TestScriptExecutor_MapLit_Overwrite(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "config = {model: \"old\"}\nconfig = {model: \"new\"}\nspawn \"${config.model}\""
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

	if spawner.calls[0].intent != "new" {
		t.Errorf("intent = %q, want %q (should use overwritten map)", spawner.calls[0].intent, "new")
	}
}

// ====================== PERFORMANCE =========================

// --- 18.3-PERF-001: [P0] ParseScript 复杂脚本解析时间 <= 50ms (NFR38) ---

func TestParseScript_Performance_NFR38(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("files = [\"a.go\", \"b.go\", \"c.go\", \"d.go\", \"e.go\"]\n")
	sb.WriteString("config = {model: \"sonnet\", budget: 5000, timeout: 30}\n")
	sb.WriteString("export base=/home/user\n")
	for range 20 {
		sb.WriteString("spawn \"分析 ${files[0]} 使用 ${config.model}\"\n")
	}
	sb.WriteString("for f in $files\n")
	sb.WriteString("  spawn \"处理 ${f}\"\n")
	sb.WriteString("end\n")
	sb.WriteString("fn analyze(target)\n")
	sb.WriteString("  spawn \"分析 ${target}\"\n")
	sb.WriteString("end\n")
	sb.WriteString("analyze(${files[0]})\n")

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

// ==================== ENVIRONMENT ============================

// --- 18.3-ENV-001: [P0] Environment SetArray/GetArray 基本操作 (AC1) ---

func TestEnvironment_SetArray_GetArray(t *testing.T) {
	env := NewEnvironment()
	env.SetArray("files", []string{"a.go", "b.go", "c.go"})

	arr, ok := env.GetArray("files")
	if !ok {
		t.Fatal("expected 'files' array to exist")
	}
	if len(arr) != 3 {
		t.Fatalf("array len = %d, want 3", len(arr))
	}
	if arr[0] != "a.go" || arr[1] != "b.go" || arr[2] != "c.go" {
		t.Errorf("array = %v, want [a.go b.go c.go]", arr)
	}
}

// --- 18.3-ENV-002: [P0] Environment SetMap/GetMap 基本操作 (AC2) ---

func TestEnvironment_SetMap_GetMap(t *testing.T) {
	env := NewEnvironment()
	env.SetMap("config", map[string]string{"model": "sonnet", "budget": "5000"})

	m, ok := env.GetMap("config")
	if !ok {
		t.Fatal("expected 'config' map to exist")
	}
	if m["model"] != "sonnet" {
		t.Errorf("model = %q, want %q", m["model"], "sonnet")
	}
	if m["budget"] != "5000" {
		t.Errorf("budget = %q, want %q", m["budget"], "5000")
	}
}

// --- 18.3-ENV-003: [P0] Environment Expand 数组索引 ${VAR[N]} (AC1) ---

func TestEnvironment_Expand_ArrayIndex(t *testing.T) {
	env := NewEnvironment()
	env.SetArray("files", []string{"a.go", "b.go", "c.go"})

	result := env.Expand("file: ${files[0]}")
	if result != "file: a.go" {
		t.Errorf("Expand = %q, want %q", result, "file: a.go")
	}

	result = env.Expand("${files[2]}")
	if result != "c.go" {
		t.Errorf("Expand = %q, want %q", result, "c.go")
	}
}

// --- 18.3-ENV-004: [P0] Environment Expand 映射属性 ${VAR.KEY} (AC2) ---

func TestEnvironment_Expand_MapField(t *testing.T) {
	env := NewEnvironment()
	env.SetMap("config", map[string]string{"model": "sonnet", "budget": "5000"})

	result := env.Expand("model: ${config.model}")
	if result != "model: sonnet" {
		t.Errorf("Expand = %q, want %q", result, "model: sonnet")
	}

	result = env.Expand("${config.budget}")
	if result != "5000" {
		t.Errorf("Expand = %q, want %q", result, "5000")
	}
}

// --- 18.3-ENV-005: [P0] Environment ExpandStrict 已定义变量正常展开 ---

func TestEnvironment_ExpandStrict_Defined(t *testing.T) {
	env := NewEnvironment()
	env.Set("NAME", "rnix")

	result, err := env.ExpandStrict("hello ${NAME}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello rnix" {
		t.Errorf("ExpandStrict = %q, want %q", result, "hello rnix")
	}
}

// --- 18.3-ENV-006: [P0] Environment ExpandStrict 未定义变量 → 错误含变量名 (AC4) ---

func TestEnvironment_ExpandStrict_Undefined_Error(t *testing.T) {
	env := NewEnvironment()

	_, err := env.ExpandStrict("hello ${undefined_var}")
	if err == nil {
		t.Fatal("expected error for undefined variable in ExpandStrict")
	}
	if !strings.Contains(err.Error(), "undefined_var") {
		t.Errorf("error should mention 'undefined_var', got: %q", err.Error())
	}
}

// --- 18.3-ENV-007: [P1] Environment ExpandStrict 数组越界 → 错误 ---

func TestEnvironment_ExpandStrict_ArrayOutOfBounds_Error(t *testing.T) {
	env := NewEnvironment()
	env.SetArray("arr", []string{"a", "b"})

	_, err := env.ExpandStrict("${arr[5]}")
	if err == nil {
		t.Fatal("expected error for array index out of bounds")
	}
}

// --- 18.3-ENV-008: [P1] Environment ExpandStrict 映射不存在的键 → 错误 ---

func TestEnvironment_ExpandStrict_MapKeyMissing_Error(t *testing.T) {
	env := NewEnvironment()
	env.SetMap("cfg", map[string]string{"a": "1"})

	_, err := env.ExpandStrict("${cfg.nonexistent}")
	if err == nil {
		t.Fatal("expected error for undefined map key")
	}
}

// --- 18.3-ENV-009: [P1] Environment GetArray 不存在的数组 → false ---

func TestEnvironment_GetArray_NotExists(t *testing.T) {
	env := NewEnvironment()
	_, ok := env.GetArray("missing")
	if ok {
		t.Error("expected missing array to return false")
	}
}

// --- 18.3-ENV-010: [P1] Environment GetMap 不存在的映射 → false ---

func TestEnvironment_GetMap_NotExists(t *testing.T) {
	env := NewEnvironment()
	_, ok := env.GetMap("missing")
	if ok {
		t.Error("expected missing map to return false")
	}
}

// --- 18.3-ENV-011: [P1] Environment SetArray 快照隔离（修改不影响原数组）---

func TestEnvironment_SetArray_SnapshotIsolation(t *testing.T) {
	env := NewEnvironment()
	original := []string{"a", "b"}
	env.SetArray("arr", original)

	original[0] = "MODIFIED"

	arr, ok := env.GetArray("arr")
	if !ok {
		t.Fatal("expected array to exist")
	}
	if arr[0] != "a" {
		t.Error("SetArray should copy the slice, not reference it")
	}
}

// --- 18.3-ENV-012: [P1] Environment SetMap 快照隔离 ---

func TestEnvironment_SetMap_SnapshotIsolation(t *testing.T) {
	env := NewEnvironment()
	original := map[string]string{"k": "v"}
	env.SetMap("m", original)

	original["k"] = "MODIFIED"

	m, ok := env.GetMap("m")
	if !ok {
		t.Fatal("expected map to exist")
	}
	if m["k"] != "v" {
		t.Error("SetMap should copy the map, not reference it")
	}
}

// ==================== TASK 9: COMPREHENSIVE TESTS ======================

// --- 18.3-UNIT-023: [P0] Environment 类型互斥 — Set 覆盖同名 array/map ---

func TestEnvironment_TypeExclusion_SetOverridesArray(t *testing.T) {
	env := NewEnvironment()
	env.SetArray("x", []string{"a", "b"})
	env.Set("x", "string_value")

	if env.GetValueKind("x") != "string" {
		t.Errorf("kind = %q, want %q", env.GetValueKind("x"), "string")
	}
	_, ok := env.GetArray("x")
	if ok {
		t.Error("array should be cleared after Set")
	}
}

func TestEnvironment_TypeExclusion_SetOverridesMap(t *testing.T) {
	env := NewEnvironment()
	env.SetMap("x", map[string]string{"k": "v"})
	env.Set("x", "string_value")

	if env.GetValueKind("x") != "string" {
		t.Errorf("kind = %q, want %q", env.GetValueKind("x"), "string")
	}
	_, ok := env.GetMap("x")
	if ok {
		t.Error("map should be cleared after Set")
	}
}

func TestEnvironment_TypeExclusion_SetArrayOverridesStringAndMap(t *testing.T) {
	env := NewEnvironment()
	env.Set("x", "hello")
	env.SetMap("y", map[string]string{"k": "v"})

	env.SetArray("x", []string{"a"})
	env.SetArray("y", []string{"b"})

	if env.GetValueKind("x") != "array" {
		t.Errorf("x kind = %q, want array", env.GetValueKind("x"))
	}
	if env.GetValueKind("y") != "array" {
		t.Errorf("y kind = %q, want array", env.GetValueKind("y"))
	}
}

func TestEnvironment_TypeExclusion_SetMapOverridesStringAndArray(t *testing.T) {
	env := NewEnvironment()
	env.Set("x", "hello")
	env.SetArray("y", []string{"a"})

	env.SetMap("x", map[string]string{"k": "v"})
	env.SetMap("y", map[string]string{"k": "v"})

	if env.GetValueKind("x") != "map" {
		t.Errorf("x kind = %q, want map", env.GetValueKind("x"))
	}
	if env.GetValueKind("y") != "map" {
		t.Errorf("y kind = %q, want map", env.GetValueKind("y"))
	}
}

// --- 18.3-UNIT-024: [P0] Environment Delete 从三个 map 中全部删除 ---

func TestEnvironment_Delete_AllTypes(t *testing.T) {
	env := NewEnvironment()
	env.Set("s", "val")
	env.SetArray("a", []string{"x"})
	env.SetMap("m", map[string]string{"k": "v"})

	env.Delete("s")
	env.Delete("a")
	env.Delete("m")

	if env.GetValueKind("s") != "" {
		t.Error("string should be deleted")
	}
	if env.GetValueKind("a") != "" {
		t.Error("array should be deleted")
	}
	if env.GetValueKind("m") != "" {
		t.Error("map should be deleted")
	}
}

// --- 18.3-UNIT-025: [P0] Environment GetValueKind ---

func TestEnvironment_GetValueKind(t *testing.T) {
	env := NewEnvironment()
	env.Set("s", "str")
	env.SetArray("a", []string{"x"})
	env.SetMap("m", map[string]string{"k": "v"})

	if k := env.GetValueKind("s"); k != "string" {
		t.Errorf("s kind = %q, want string", k)
	}
	if k := env.GetValueKind("a"); k != "array" {
		t.Errorf("a kind = %q, want array", k)
	}
	if k := env.GetValueKind("m"); k != "map" {
		t.Errorf("m kind = %q, want map", k)
	}
	if k := env.GetValueKind("missing"); k != "" {
		t.Errorf("missing kind = %q, want empty", k)
	}
}

// --- 18.3-UNIT-026: [P0] ParseScript 空数组 [] ---

func TestParseScript_EmptyArray(t *testing.T) {
	input := `empty = []`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stmt := script.Statements[0]
	if stmt.Kind != StmtArrayLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtArrayLit)
	}
	if len(stmt.ArrayLit.Items) != 0 {
		t.Errorf("items len = %d, want 0", len(stmt.ArrayLit.Items))
	}
}

// --- 18.3-UNIT-027: [P0] ParseScript 空映射 {} ---

func TestParseScript_EmptyMap(t *testing.T) {
	input := `empty = {}`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stmt := script.Statements[0]
	if stmt.Kind != StmtMapLit {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtMapLit)
	}
	if len(stmt.MapLit.Entries) != 0 {
		t.Errorf("entries len = %d, want 0", len(stmt.MapLit.Entries))
	}
}

// --- 18.3-UNIT-028: [P0] ParseScript 映射重复 key → 错误 ---

func TestParseScript_Error_MapDuplicateKey(t *testing.T) {
	input := `config = {model: "a", model: "b"}`
	_, err := ParseScript(input)
	if err == nil {
		t.Fatal("expected error for duplicate map key")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention 'duplicate', got: %q", err.Error())
	}
}

// --- 18.3-UNIT-029: [P0] ParseScript 索引赋值 files[0] = "new.go" ---

func TestParseScript_IndexAssignment(t *testing.T) {
	input := `files[0] = "new.go"`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stmt := script.Statements[0]
	if stmt.Kind != StmtAssignIndex {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtAssignIndex)
	}
	if stmt.IndexAssign.VarName != "files" {
		t.Errorf("varName = %q, want %q", stmt.IndexAssign.VarName, "files")
	}
	if stmt.IndexAssign.Index != "0" {
		t.Errorf("index = %q, want %q", stmt.IndexAssign.Index, "0")
	}
	if stmt.IndexAssign.Value != "new.go" {
		t.Errorf("value = %q, want %q", stmt.IndexAssign.Value, "new.go")
	}
}

// --- 18.3-UNIT-030: [P0] ParseScript 属性赋值 config.model = "opus" ---

func TestParseScript_PropAssignment(t *testing.T) {
	input := `config.model = "opus"`
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stmt := script.Statements[0]
	if stmt.Kind != StmtAssignProp {
		t.Errorf("kind = %q, want %q", stmt.Kind, StmtAssignProp)
	}
	if stmt.PropAssign.VarName != "config" {
		t.Errorf("varName = %q, want %q", stmt.PropAssign.VarName, "config")
	}
	if stmt.PropAssign.Property != "model" {
		t.Errorf("property = %q, want %q", stmt.PropAssign.Property, "model")
	}
	if stmt.PropAssign.Value != "opus" {
		t.Errorf("value = %q, want %q", stmt.PropAssign.Value, "opus")
	}
}

// --- 18.3-UNIT-031: [P0] ScriptExecutor 索引赋值修改元素 (AC11) ---

func TestScriptExecutor_IndexAssign_ModifyElement(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"a.go\", \"b.go\", \"c.go\"]\nfiles[0] = \"z.go\"\nspawn \"${files[0]}\""
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

	if spawner.calls[0].intent != "z.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "z.go")
	}
}

// --- 18.3-UNIT-032: [P0] ScriptExecutor 属性赋值修改属性 (AC12) ---

func TestScriptExecutor_PropAssign_ModifyProperty(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "config = {model: \"sonnet\", budget: 5000}\nconfig.model = \"opus\"\nspawn \"${config.model}\""
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

	if spawner.calls[0].intent != "opus" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "opus")
	}
}

// --- 18.3-UNIT-033: [P0] ScriptExecutor len(array) 返回 "3" (AC7) ---

func TestScriptExecutor_Builtin_LenArray(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"a.go\", \"b.go\", \"c.go\"]\nl = len(files)\nspawn \"长度 $l\""
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

	if spawner.calls[0].intent != "长度 3" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "长度 3")
	}
}

// --- 18.3-UNIT-034: [P0] ScriptExecutor len(map) 返回正确数量 ---

func TestScriptExecutor_Builtin_LenMap(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "config = {model: \"sonnet\", budget: 5000}\nl = len(config)\nspawn \"键数 $l\""
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

	if spawner.calls[0].intent != "键数 2" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "键数 2")
	}
}

// --- 18.3-UNIT-035: [P0] ScriptExecutor len(string) 按 rune 计 ---

func TestScriptExecutor_Builtin_LenString(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "export name=你好世界\nl = len(name)\nspawn \"长度 $l\""
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

	if spawner.calls[0].intent != "长度 4" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "长度 4")
	}
}

// --- 18.3-UNIT-036: [P0] ScriptExecutor append 追加元素 (AC8) ---

func TestScriptExecutor_Builtin_Append(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"a.go\", \"b.go\", \"c.go\"]\nappend(files, \"d.go\")\nspawn \"${files[3]}\""
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

	if spawner.calls[0].intent != "d.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "d.go")
	}
}

// --- 18.3-UNIT-037: [P0] ScriptExecutor keys(map) 结果为排序后的数组 (AC9) ---

func TestScriptExecutor_Builtin_Keys(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	input := "config = {model: \"sonnet\", budget: 5000}\nk = keys(config)\nfor key in $k\n  spawn \"key: ${key}\"\nend"
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
	// keys are sorted: budget, model
	if spawner.calls[0].intent != "key: budget" {
		t.Errorf("call 0 = %q, want %q", spawner.calls[0].intent, "key: budget")
	}
	if spawner.calls[1].intent != "key: model" {
		t.Errorf("call 1 = %q, want %q", spawner.calls[1].intent, "key: model")
	}
}

// --- 18.3-UNIT-038: [P1] ScriptExecutor append 非数组变量 → 错误 ---

func TestScriptExecutor_Builtin_Append_NotArray_Error(t *testing.T) {
	spawner := &mockSpawner{}

	input := "export x=hello\nappend(x, \"world\")"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for append on non-array")
	}
}

// --- 18.3-UNIT-039: [P1] ScriptExecutor keys 非映射变量 → 错误 ---

func TestScriptExecutor_Builtin_Keys_NotMap_Error(t *testing.T) {
	spawner := &mockSpawner{}

	input := "files = [\"a.go\"]\nk = keys(files)"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for keys on non-map")
	}
}

// --- 18.3-UNIT-040: [P0] ParseScript 内置函数参数数量不匹配 → 错误 ---

func TestParseScript_Error_BuiltinArgCount(t *testing.T) {
	tests := []string{
		`len()`,
		`len(a, b)`,
		`append(a)`,
		`append(a, b, c)`,
		`keys()`,
		`keys(a, b)`,
	}
	for _, input := range tests {
		_, err := ParseScript(input)
		if err == nil {
			t.Errorf("expected error for wrong arg count: %q", input)
		}
	}
}

// --- 18.3-UNIT-041: [P0] ScriptExecutor 索引赋值越界 → 运行时错误 (AC10) ---

func TestScriptExecutor_IndexAssign_OutOfBounds_Error(t *testing.T) {
	spawner := &mockSpawner{}

	input := "files = [\"a.go\", \"b.go\"]\nfiles[99] = \"z.go\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for index assignment out of bounds")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention 'out of range', got: %q", err.Error())
	}
}

// --- 18.3-UNIT-042: [P0] ScriptExecutor 属性赋值不存在的映射 → 运行时错误 ---

func TestScriptExecutor_PropAssign_NotMap_Error(t *testing.T) {
	spawner := &mockSpawner{}

	input := "export x=hello\nx.key = \"value\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for property assignment on non-map")
	}
}

// --- 18.3-UNIT-043: [P0] ScriptExecutor 索引赋值非数组 → 运行时错误 ---

func TestScriptExecutor_IndexAssign_NotArray_Error(t *testing.T) {
	spawner := &mockSpawner{}

	input := "export x=hello\nx[0] = \"value\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for index assignment on non-array")
	}
}

// --- 18.3-UNIT-044: [P0] ScriptExecutor for 循环内修改数组元素 ---

func TestScriptExecutor_ForLoop_ModifyArray(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	input := "files = [\"a.go\", \"b.go\"]\nfiles[0] = \"z.go\"\nfor f in $files\n  spawn \"${f}\"\nend"
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

	if spawner.calls[0].intent != "z.go" {
		t.Errorf("call 0 = %q, want %q", spawner.calls[0].intent, "z.go")
	}
	if spawner.calls[1].intent != "b.go" {
		t.Errorf("call 1 = %q, want %q", spawner.calls[1].intent, "b.go")
	}
}

// --- 18.3-UNIT-045: [P0] ScriptExecutor export 中数组/映射插值 ---

func TestScriptExecutor_ExportWithDataStructInterpolation(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"main.go\"]\nconfig = {model: \"sonnet\"}\nexport summary=${files[0]}-${config.model}\nspawn \"$summary\""
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

	if spawner.calls[0].intent != "main.go-sonnet" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "main.go-sonnet")
	}
}

// --- 18.3-UNIT-046: [P0] ScriptExecutor 数组 + 函数调用 —— 函数参数传递数组元素 ---

func TestScriptExecutor_FnCallWithArrayElement(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "files = [\"main.go\", \"lib.go\"]\nfn analyze(file)\n  spawn \"分析 ${file}\"\nend\nanalyze(${files[1]})"
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

	if spawner.calls[0].intent != "分析 lib.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "分析 lib.go")
	}
}

// --- 18.3-UNIT-047: [P1] ScriptExecutor 数组变量引用 $VAR 短格式不展开 ---

func TestScriptExecutor_ArrayVarShortForm_NoExpand(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	// $files[0] should expand $files (as string, empty since it's array) then literal [0]
	input := "files = [\"a.go\"]\nspawn \"test $files end\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	// ExpandStrict: $files is recognized as array variable, not undefined
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// $files short form expands to empty string (it's an array, not in vars map)
	if spawner.calls[0].intent != "test  end" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "test  end")
	}
}

// --- 18.3-UNIT-048: [P1] ScriptExecutor 数组带变量引用元素 ---

func TestScriptExecutor_ArrayLit_WithVarRef(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "export prefix=src\nfiles = [\"$prefix/a.go\", \"$prefix/b.go\"]\nspawn \"${files[0]}\""
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

	if spawner.calls[0].intent != "src/a.go" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "src/a.go")
	}
}

// --- 18.3-UNIT-049: [P1] ScriptExecutor 映射带变量引用值 ---

func TestScriptExecutor_MapLit_WithVarRef(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "export m=sonnet\nconfig = {model: $m, budget: 5000}\nspawn \"${config.model}\""
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

	if spawner.calls[0].intent != "sonnet" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "sonnet")
	}
}

// --- 18.3-UNIT-050: [P1] ScriptExecutor ExpandStrict export 值不报错 ---

func TestScriptExecutor_Export_UndefinedVar_NoError(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "export val=${undefined}\nspawn \"result: $val\""
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

	// export uses Expand (not strict), undefined → empty; then spawn uses ExpandStrict but val is defined
	if spawner.calls[0].intent != "result: " {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "result: ")
	}
}

// --- 18.3-CR-009: [P0] ScriptExecutor len($undefined) → 错误 (AC7 边界) ---

func TestScriptExecutor_Builtin_LenUndefined_Error(t *testing.T) {
	spawner := &mockSpawner{}

	input := "l = len($undefined)"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for len on undefined variable")
	}
	if !strings.Contains(err.Error(), "undefined") {
		t.Errorf("error should mention 'undefined', got: %q", err.Error())
	}
}

// --- 18.3-CR-010: [P0] ScriptExecutor len(undefined_bare) → 错误 (AC7 边界) ---

func TestScriptExecutor_Builtin_LenUndefinedBare_Error(t *testing.T) {
	spawner := &mockSpawner{}

	input := "l = len(nonexistent)"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	_, err = executor.Execute(context.Background(), script)
	if err == nil {
		t.Fatal("expected error for len on undefined bare variable")
	}
}

// --- 18.3-CR-011: [P0] ParseScript 映射非法 key → 错误 ---

func TestParseScript_Error_MapInvalidKey(t *testing.T) {
	tests := []string{
		`config = {123: "value"}`,
		`config = {a-b: "value"}`,
	}
	for _, input := range tests {
		_, err := ParseScript(input)
		if err == nil {
			t.Errorf("expected error for invalid map key: %q", input)
		}
	}
}

// --- 18.3-UNIT-051: [P0] ScriptExecutor for-in 非数组 $VAR 回退到字符串 (AC6 边界) ---

func TestScriptExecutor_ForIn_NonArrayVar_Fallback(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 50},
		},
	}

	input := "export items=hello\nfor x in $items\n  spawn \"${x}\"\nend"
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
		t.Fatalf("calls = %d, want 1 (non-array fallback to single string)", len(spawner.calls))
	}
	if spawner.calls[0].intent != "hello" {
		t.Errorf("intent = %q, want %q", spawner.calls[0].intent, "hello")
	}
}
