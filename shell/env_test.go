package shell

import (
	"os"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 11.2: 变量与环境传递
//
// Tests reference Environment, NewEnvironment, NewEnvironmentFromOS,
// Set, Get, Delete, All, Expand — which do NOT exist yet
// → compile failure = RED phase.
// ============================================================

// --- 11.2-UNIT-001: [P0] Environment Set/Get 基本操作 (AC1) ---

func TestEnvironment_SetGet(t *testing.T) {
	env := NewEnvironment()

	env.Set("TARGET", "./src/auth.go")
	val, ok := env.Get("TARGET")
	if !ok {
		t.Fatal("expected TARGET to exist")
	}
	if val != "./src/auth.go" {
		t.Errorf("TARGET = %q, want %q", val, "./src/auth.go")
	}

	// Non-existent key
	_, ok = env.Get("MISSING")
	if ok {
		t.Error("expected MISSING to not exist")
	}
}

func TestEnvironment_SetOverwrite(t *testing.T) {
	env := NewEnvironment()
	env.Set("KEY", "old")
	env.Set("KEY", "new")

	val, ok := env.Get("KEY")
	if !ok {
		t.Fatal("expected KEY to exist")
	}
	if val != "new" {
		t.Errorf("KEY = %q, want %q", val, "new")
	}
}

func TestEnvironment_SetEmptyValue(t *testing.T) {
	env := NewEnvironment()
	env.Set("EMPTY", "")

	val, ok := env.Get("EMPTY")
	if !ok {
		t.Fatal("expected EMPTY key to exist")
	}
	if val != "" {
		t.Errorf("EMPTY = %q, want empty string", val)
	}
}

// --- 11.2-UNIT-002: [P0] Environment Delete ---

func TestEnvironment_Delete(t *testing.T) {
	env := NewEnvironment()
	env.Set("KEY", "value")
	env.Delete("KEY")

	_, ok := env.Get("KEY")
	if ok {
		t.Error("expected KEY to be deleted")
	}
}

func TestEnvironment_DeleteNonExistent(t *testing.T) {
	env := NewEnvironment()
	env.Delete("NONEXISTENT") // should not panic
}

// --- 11.2-UNIT-003: [P0] Expand $VAR 单变量 (AC2, AC3) ---

func TestExpand_SingleVar(t *testing.T) {
	env := NewEnvironment()
	env.Set("TARGET", "./src/auth.go")

	result := env.Expand("分析 $TARGET")
	if result != "分析 ./src/auth.go" {
		t.Errorf("Expand = %q, want %q", result, "分析 ./src/auth.go")
	}
}

// --- 11.2-UNIT-004: [P0] Expand $VAR 多变量 (AC2, AC3) ---

func TestExpand_MultipleVars(t *testing.T) {
	env := NewEnvironment()
	env.Set("A", "hello")
	env.Set("B", "world")

	result := env.Expand("$A $B")
	if result != "hello world" {
		t.Errorf("Expand = %q, want %q", result, "hello world")
	}
}

func TestExpand_AdjacentVars(t *testing.T) {
	env := NewEnvironment()
	env.Set("X", "foo")
	env.Set("Y", "bar")

	result := env.Expand("$X$Y")
	if result != "foobar" {
		t.Errorf("Expand = %q, want %q", result, "foobar")
	}
}

// --- 11.2-UNIT-005: [P0] Expand ${VAR} 花括号语法 (AC3) ---

func TestExpand_BracesSyntax(t *testing.T) {
	env := NewEnvironment()
	env.Set("NAME", "crux")

	result := env.Expand("project: ${NAME}")
	if result != "project: crux" {
		t.Errorf("Expand = %q, want %q", result, "project: crux")
	}
}

// --- 11.2-UNIT-006: [P0] Expand ${VAR}suffix 带后缀 (AC3) ---

func TestExpand_BracesSuffix(t *testing.T) {
	env := NewEnvironment()
	env.Set("DIR", "/tmp")

	result := env.Expand("${DIR}/output.txt")
	if result != "/tmp/output.txt" {
		t.Errorf("Expand = %q, want %q", result, "/tmp/output.txt")
	}
}

func TestExpand_BracesInMiddle(t *testing.T) {
	env := NewEnvironment()
	env.Set("VAR", "value")

	result := env.Expand("prefix${VAR}suffix")
	if result != "prefixvaluesuffix" {
		t.Errorf("Expand = %q, want %q", result, "prefixvaluesuffix")
	}
}

// --- 11.2-UNIT-007: [P0] Expand \$ 转义 (AC3) ---

func TestExpand_EscapedDollar(t *testing.T) {
	env := NewEnvironment()
	env.Set("PRICE", "100")

	result := env.Expand(`价格是 \$100`)
	if result != "价格是 $100" {
		t.Errorf("Expand = %q, want %q", result, "价格是 $100")
	}
}

func TestExpand_EscapedDollarBeforeVar(t *testing.T) {
	env := NewEnvironment()
	env.Set("X", "val")

	result := env.Expand(`\$X is $X`)
	if result != "$X is val" {
		t.Errorf("Expand = %q, want %q", result, "$X is val")
	}
}

// --- 11.2-UNIT-008: [P1] Expand 未定义变量 → 空字符串 ---

func TestExpand_UndefinedVar(t *testing.T) {
	env := NewEnvironment()

	result := env.Expand("hello $UNDEFINED world")
	if result != "hello  world" {
		t.Errorf("Expand = %q, want %q", result, "hello  world")
	}
}

func TestExpand_UndefinedBracesVar(t *testing.T) {
	env := NewEnvironment()

	result := env.Expand("${MISSING}value")
	if result != "value" {
		t.Errorf("Expand = %q, want %q", result, "value")
	}
}

// --- 11.2-UNIT-009: [P1] Expand $ 在末尾 → 保持原样 ---

func TestExpand_DollarAtEnd(t *testing.T) {
	env := NewEnvironment()

	result := env.Expand("cost is $")
	if result != "cost is $" {
		t.Errorf("Expand = %q, want %q", result, "cost is $")
	}
}

func TestExpand_DollarFollowedByNonVar(t *testing.T) {
	env := NewEnvironment()

	result := env.Expand("$100 dollars")
	// $1 is a valid var start (digit following $), but '1' is NOT a valid var start char
	// so '$' followed by digit should keep '$' and digit literal
	if result != "$100 dollars" {
		t.Errorf("Expand = %q, want %q", result, "$100 dollars")
	}
}

func TestExpand_UnclosedBraces(t *testing.T) {
	env := NewEnvironment()

	result := env.Expand("${UNCLOSED")
	if result != "${UNCLOSED" {
		t.Errorf("Expand = %q, want %q", result, "${UNCLOSED")
	}
}

func TestExpand_NoVariables(t *testing.T) {
	env := NewEnvironment()

	result := env.Expand("plain text without variables")
	if result != "plain text without variables" {
		t.Errorf("Expand = %q, want %q", result, "plain text without variables")
	}
}

func TestExpand_EmptyInput(t *testing.T) {
	env := NewEnvironment()

	result := env.Expand("")
	if result != "" {
		t.Errorf("Expand = %q, want empty string", result)
	}
}

// --- 11.2-UNIT-010: [P1] NewEnvironmentFromOS 包含系统变量 ---

func TestNewEnvironmentFromOS_ContainsPath(t *testing.T) {
	env := NewEnvironmentFromOS()

	val, ok := env.Get("PATH")
	if !ok {
		t.Fatal("expected PATH to exist in OS environment")
	}
	if val == "" {
		t.Error("PATH should not be empty")
	}
}

func TestNewEnvironmentFromOS_ContainsHome(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set in OS environment")
	}

	env := NewEnvironmentFromOS()
	val, ok := env.Get("HOME")
	if !ok {
		t.Fatal("expected HOME to exist")
	}
	if val != home {
		t.Errorf("HOME = %q, want %q", val, home)
	}
}

func TestEnvironment_All_Snapshot(t *testing.T) {
	env := NewEnvironment()
	env.Set("A", "1")
	env.Set("B", "2")

	all := env.All()
	if len(all) != 2 {
		t.Fatalf("All() len = %d, want 2", len(all))
	}
	if all["A"] != "1" {
		t.Errorf("A = %q, want %q", all["A"], "1")
	}
	if all["B"] != "2" {
		t.Errorf("B = %q, want %q", all["B"], "2")
	}

	// Mutating the snapshot should not affect the original
	all["A"] = "modified"
	val, _ := env.Get("A")
	if val != "1" {
		t.Error("All() should return a snapshot copy, not a reference")
	}
}

// --- Variable name case sensitivity (boundary case) ---

func TestExpand_CaseSensitive(t *testing.T) {
	env := NewEnvironment()
	env.Set("target", "lowercase")
	env.Set("TARGET", "uppercase")

	if result := env.Expand("$target"); result != "lowercase" {
		t.Errorf("$target = %q, want %q", result, "lowercase")
	}
	if result := env.Expand("$TARGET"); result != "uppercase" {
		t.Errorf("$TARGET = %q, want %q", result, "uppercase")
	}
}

// --- Circular reference (boundary case, bash behavior) ---

func TestExpand_CircularReference(t *testing.T) {
	env := NewEnvironment()
	env.Set("A", "$B")
	env.Set("B", "$A")

	// Expand takes current literal value, no recursive expansion
	result := env.Expand("$A")
	if result != "$B" {
		t.Errorf("Expand($A) = %q, want %q (no recursive expansion)", result, "$B")
	}
}
