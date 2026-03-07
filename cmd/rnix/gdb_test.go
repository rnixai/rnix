package main

import (
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 13.2: 断点系统 (gdb CLI 命令扩展)
//
// Tests reference parseBreakCommand, parseDeleteCommand,
// parseInfoBreakpointsCommand, parseContinueCommand
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.2-CLI-001: [P0] parseBreakCommand 解析 "break syscall Read" ---

func TestParseBreakCommand_Syscall(t *testing.T) {
	cmd, err := parseBreakCommand([]string{"syscall", "Read"})
	if err != nil {
		t.Fatalf("parseBreakCommand: %v", err)
	}
	if cmd.SubType != "syscall" {
		t.Errorf("SubType = %q, want %q", cmd.SubType, "syscall")
	}
	if cmd.SyscallName != "Read" {
		t.Errorf("SyscallName = %q, want %q", cmd.SyscallName, "Read")
	}
}

// --- 13.2-CLI-002: [P0] parseBreakCommand 解析 "break reasoning" ---

func TestParseBreakCommand_Reasoning(t *testing.T) {
	cmd, err := parseBreakCommand([]string{"reasoning"})
	if err != nil {
		t.Fatalf("parseBreakCommand: %v", err)
	}
	if cmd.SubType != "reasoning" {
		t.Errorf("SubType = %q, want %q", cmd.SubType, "reasoning")
	}
}

// --- 13.2-CLI-003: [P0] parseBreakCommand 解析 "break quality --pattern ..." ---

func TestParseBreakCommand_QualityPattern(t *testing.T) {
	cmd, err := parseBreakCommand([]string{"quality", "--pattern", "安全漏洞"})
	if err != nil {
		t.Fatalf("parseBreakCommand: %v", err)
	}
	if cmd.SubType != "quality" {
		t.Errorf("SubType = %q, want %q", cmd.SubType, "quality")
	}
	if cmd.QualityMode != "pattern" {
		t.Errorf("QualityMode = %q, want %q", cmd.QualityMode, "pattern")
	}
	if cmd.Pattern != "安全漏洞" {
		t.Errorf("Pattern = %q, want %q", cmd.Pattern, "安全漏洞")
	}
}

// --- 13.2-CLI-004: [P0] parseBreakCommand 解析 "break quality --eval ..." ---

func TestParseBreakCommand_QualityEval(t *testing.T) {
	cmd, err := parseBreakCommand([]string{"quality", "--eval", "输出必须包含代码示例"})
	if err != nil {
		t.Fatalf("parseBreakCommand: %v", err)
	}
	if cmd.SubType != "quality" {
		t.Errorf("SubType = %q, want %q", cmd.SubType, "quality")
	}
	if cmd.QualityMode != "eval" {
		t.Errorf("QualityMode = %q, want %q", cmd.QualityMode, "eval")
	}
	if cmd.EvalExpr != "输出必须包含代码示例" {
		t.Errorf("EvalExpr = %q, want %q", cmd.EvalExpr, "输出必须包含代码示例")
	}
}

// --- 13.2-CLI-005: [P0] parseBreakCommand 解析 "break budget 5000" ---

func TestParseBreakCommand_Budget(t *testing.T) {
	cmd, err := parseBreakCommand([]string{"budget", "5000"})
	if err != nil {
		t.Fatalf("parseBreakCommand: %v", err)
	}
	if cmd.SubType != "budget" {
		t.Errorf("SubType = %q, want %q", cmd.SubType, "budget")
	}
	if cmd.BudgetTokens != 5000 {
		t.Errorf("BudgetTokens = %d, want 5000", cmd.BudgetTokens)
	}
}

// --- 13.2-CLI-006: [P1] parseBreakCommand 无参数返回错误 ---

func TestParseBreakCommand_NoArgs(t *testing.T) {
	_, err := parseBreakCommand([]string{})
	if err == nil {
		t.Fatal("expected error for empty break command")
	}
}

// --- 13.2-CLI-007: [P1] parseBreakCommand 未知子类型返回错误 ---

func TestParseBreakCommand_UnknownSubtype(t *testing.T) {
	_, err := parseBreakCommand([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown break subtype")
	}
}

// --- 13.2-CLI-008: [P1] parseBreakCommand "break syscall" 无名称返回错误 ---

func TestParseBreakCommand_SyscallNoName(t *testing.T) {
	_, err := parseBreakCommand([]string{"syscall"})
	if err == nil {
		t.Fatal("expected error for syscall break without name")
	}
}

// --- 13.2-CLI-009: [P1] parseBreakCommand "break budget" 无值返回错误 ---

func TestParseBreakCommand_BudgetNoValue(t *testing.T) {
	_, err := parseBreakCommand([]string{"budget"})
	if err == nil {
		t.Fatal("expected error for budget break without value")
	}
}

// --- 13.2-CLI-010: [P1] parseBreakCommand "break budget abc" 无效数字返回错误 ---

func TestParseBreakCommand_BudgetInvalidNumber(t *testing.T) {
	_, err := parseBreakCommand([]string{"budget", "abc"})
	if err == nil {
		t.Fatal("expected error for budget break with non-numeric value")
	}
}

// --- 13.2-CLI-011: [P1] parseBreakCommand "break quality" 无 flag 返回错误 ---

func TestParseBreakCommand_QualityNoFlag(t *testing.T) {
	_, err := parseBreakCommand([]string{"quality"})
	if err == nil {
		t.Fatal("expected error for quality break without --pattern or --eval")
	}
}

// --- 13.2-CLI-012: [P0] parseDeleteCommand 解析 "delete 1" ---

func TestParseDeleteCommand(t *testing.T) {
	id, err := parseDeleteCommand([]string{"1"})
	if err != nil {
		t.Fatalf("parseDeleteCommand: %v", err)
	}
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}
}

// --- 13.2-CLI-013: [P1] parseDeleteCommand 无参数返回错误 ---

func TestParseDeleteCommand_NoArgs(t *testing.T) {
	_, err := parseDeleteCommand([]string{})
	if err == nil {
		t.Fatal("expected error for delete without ID")
	}
}

// --- 13.2-CLI-014: [P1] parseDeleteCommand 非数字返回错误 ---

func TestParseDeleteCommand_InvalidID(t *testing.T) {
	_, err := parseDeleteCommand([]string{"abc"})
	if err == nil {
		t.Fatal("expected error for delete with non-numeric ID")
	}
}

// --- 13.2-CLI-015: [P0] BreakCommandResult 结构体字段完整 ---

func TestBreakCommandResult_Fields(t *testing.T) {
	result := BreakCommandResult{
		SubType:      "syscall",
		SyscallName:  "Read",
		QualityMode:  "",
		Pattern:      "",
		EvalExpr:     "",
		BudgetTokens: 0,
	}
	if result.SubType != "syscall" {
		t.Errorf("SubType = %q, want %q", result.SubType, "syscall")
	}
	if result.SyscallName != "Read" {
		t.Errorf("SyscallName = %q, want %q", result.SyscallName, "Read")
	}
}
