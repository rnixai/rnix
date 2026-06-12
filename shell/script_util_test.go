package shell

import "testing"

// 覆盖 spec-fix-count-stages-fncall-source 的 I/O 矩阵：
// fn 调用按定义递归展开计数；source / 缺失定义 / 递归环 → total 不可信 → 0。

// fn 定义可解析：fn f 体含 2 spawn；主流程 f()×2 + 1 spawn → total = 5
func TestCountStages_FnCallExpandsBody(t *testing.T) {
	input := "fn f()\n  spawn \"a\"\n  spawn \"b\"\nend\nf()\nf()\nspawn \"main\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 5 {
		t.Errorf("stage count = %d, want 5 (2 fn calls × 2 spawns + 1 plain spawn)", total)
	}
}

// 嵌套 fn 调用：fn a 调 fn b（b 含 1 spawn），主流程调 a() → total = 1
func TestCountStages_NestedFnCall(t *testing.T) {
	input := "fn b()\n  spawn \"inner\"\nend\nfn a()\n  b()\nend\na()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 1 {
		t.Errorf("stage count = %d, want 1 (a() → b() → 1 spawn)", total)
	}
}

// 递归 fn（直接环）：展开次数静态不可知 → total = 0，且不死循环
func TestCountStages_RecursiveFnZero(t *testing.T) {
	input := "fn a()\n  spawn \"x\"\n  a()\nend\na()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 0 {
		t.Errorf("stage count = %d, want 0 (recursive cycle → untrusted)", total)
	}
}

// 递归 fn（间接环 a→b→a）：→ total = 0
func TestCountStages_MutualRecursionZero(t *testing.T) {
	input := "fn a()\n  b()\nend\nfn b()\n  a()\nend\na()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 0 {
		t.Errorf("stage count = %d, want 0 (mutual recursion → untrusted)", total)
	}
}

// 含 source（嵌在 if 体内）：→ total = 0
func TestCountStages_SourceInNestedBlockZero(t *testing.T) {
	input := "if $X == \"1\"\n  source ./lib.ash\nend\nspawn \"main\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 0 {
		t.Errorf("stage count = %d, want 0 (source anywhere → untrusted)", total)
	}
}

// FnCall 无定义（与 source 共存时 validateFnCalls 容忍）：→ total = 0
func TestCountStages_UndefinedFnCallZero(t *testing.T) {
	input := "source ./lib.ash\nhelper()\nspawn \"main\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 0 {
		t.Errorf("stage count = %d, want 0 (undefined fn / source → untrusted)", total)
	}
}

// 无 fn/source 的普通脚本：计数与现状完全一致（回归不变）
func TestCountStages_PlainScriptUnchanged(t *testing.T) {
	input := "spawn \"a\"\nfor x in [\"1\", \"2\", \"3\"]\n  spawn \"item ${x}\"\nend\nif $X == \"1\"\n  spawn \"then\"\n  spawn \"then2\"\nelse\n  spawn \"else\"\nend"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	// 1 spawn + for 3×1 + if max(2,1)=2 → 6
	if total != 6 {
		t.Errorf("stage count = %d, want 6 (regression: plain script semantics unchanged)", total)
	}
}

// fn 体内 for 循环：fn f 体 for x in [a,b] 含 1 spawn，调 f() → total = 2
func TestCountStages_FnBodyWithForLoop(t *testing.T) {
	input := "fn f()\n  for x in [\"a\", \"b\"]\n    spawn \"item ${x}\"\n  end\nend\nf()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 2 {
		t.Errorf("stage count = %d, want 2 (fn body for loop: 2 iterations × 1 spawn)", total)
	}
}

// --- patch 级补充：builtin fn / return 语义 / 菱形调用链 ---

// builtin fn（len/append/keys）计 0 且不触发 untrusted
func TestCountStages_BuiltinFnZeroTrusted(t *testing.T) {
	input := "arr = [\"a\", \"b\"]\nn = len($arr)\nspawn \"main\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 1 {
		t.Errorf("stage count = %d, want 1 (builtin len() = 0 stages, stays trusted)", total)
	}
}

// fn 体内条件 return（if 内 return，之后还有 spawn）：执行性不可知 → 0
func TestCountStages_ConditionalReturnUntrusted(t *testing.T) {
	input := "fn f(x)\n  if $x == \"1\"\n    return\n  end\n  spawn \"after\"\nend\nf(\"1\")"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 0 {
		t.Errorf("stage count = %d, want 0 (conditional return → untrusted)", total)
	}
}

// fn 体末尾顶层 return：计数精确（return 前的 spawn 计入，不降级为 0）
func TestCountStages_TrailingReturnExact(t *testing.T) {
	input := "fn f()\n  spawn \"a\"\n  spawn \"b\"\n  return\nend\nf()\nspawn \"main\""
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 3 {
		t.Errorf("stage count = %d, want 3 (fn body 2 spawns + plain spawn; trailing return is exact)", total)
	}
}

// 菱形调用链：a 调 b ×2、b 调 c ×2、c 含 1 spawn → total = 4（非指数、计数正确）
func TestCountStages_DiamondCallChain(t *testing.T) {
	input := "fn c()\n  spawn \"leaf\"\nend\nfn b()\n  c()\n  c()\nend\nfn a()\n  b()\n  b()\nend\na()"
	script, err := ParseScript(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	total := countExecutableStages(script)
	if total != 4 {
		t.Errorf("stage count = %d, want 4 (diamond chain: 2×2×1 spawn)", total)
	}
}
