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

// ============================================================
// ATDD RED PHASE — Story 13.3: 单步执行与状态检查 (gdb CLI 命令扩展)
//
// Tests reference parseStepCommand, StepCommandResult,
// parseInspectCommand, InspectCommandResult
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.3-CLI-001: [P0] parseStepCommand 解析 "step syscall" ---

func TestParseStepCommand_Syscall(t *testing.T) {
	cmd, err := parseStepCommand([]string{"syscall"})
	if err != nil {
		t.Fatalf("parseStepCommand: %v", err)
	}
	if cmd.Mode != "syscall" {
		t.Errorf("Mode = %q, want %q", cmd.Mode, "syscall")
	}
}

// --- 13.3-CLI-002: [P0] parseStepCommand 解析 "step reasoning" ---

func TestParseStepCommand_Reasoning(t *testing.T) {
	cmd, err := parseStepCommand([]string{"reasoning"})
	if err != nil {
		t.Fatalf("parseStepCommand: %v", err)
	}
	if cmd.Mode != "reasoning" {
		t.Errorf("Mode = %q, want %q", cmd.Mode, "reasoning")
	}
}

// --- 13.3-CLI-003: [P0] parseStepCommand 无参数默认 "syscall" ---

func TestParseStepCommand_DefaultSyscall(t *testing.T) {
	cmd, err := parseStepCommand([]string{})
	if err != nil {
		t.Fatalf("parseStepCommand: %v", err)
	}
	if cmd.Mode != "syscall" {
		t.Errorf("Mode = %q, want %q (default)", cmd.Mode, "syscall")
	}
}

// --- 13.3-CLI-004: [P1] parseStepCommand 未知模式返回错误 ---

func TestParseStepCommand_UnknownMode(t *testing.T) {
	_, err := parseStepCommand([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown step mode")
	}
}

// --- 13.3-CLI-005: [P0] parseInspectCommand 解析 "inspect context" ---

func TestParseInspectCommand_Context(t *testing.T) {
	cmd, err := parseInspectCommand([]string{"context"})
	if err != nil {
		t.Fatalf("parseInspectCommand: %v", err)
	}
	if cmd.SubCommand != "context" {
		t.Errorf("SubCommand = %q, want %q", cmd.SubCommand, "context")
	}
}

// --- 13.3-CLI-006: [P1] parseInspectCommand 无参数返回错误 ---

func TestParseInspectCommand_NoArgs(t *testing.T) {
	_, err := parseInspectCommand([]string{})
	if err == nil {
		t.Fatal("expected error for inspect without subcommand")
	}
}

// --- 13.3-CLI-007: [P1] parseInspectCommand "ctx" 是 "context" 别名 ---

func TestParseInspectCommand_CtxAlias(t *testing.T) {
	cmd, err := parseInspectCommand([]string{"ctx"})
	if err != nil {
		t.Fatalf("parseInspectCommand: %v", err)
	}
	if cmd.SubCommand != "context" {
		t.Errorf("SubCommand = %q, want %q (ctx should alias to context)", cmd.SubCommand, "context")
	}
}

// --- 13.3-CLI-008: [P0] StepCommandResult 结构体字段完整 ---

func TestStepCommandResult_Fields(t *testing.T) {
	result := StepCommandResult{
		Mode: "syscall",
	}
	if result.Mode != "syscall" {
		t.Errorf("Mode = %q, want %q", result.Mode, "syscall")
	}
}

// --- 13.3-CLI-009: [P0] InspectCommandResult 结构体字段完整 ---

func TestInspectCommandResult_Fields(t *testing.T) {
	result := InspectCommandResult{
		SubCommand: "context",
	}
	if result.SubCommand != "context" {
		t.Errorf("SubCommand = %q, want %q", result.SubCommand, "context")
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.4: 运行时参数热修改 (gdb CLI 命令扩展)
//
// Tests reference parseSetCommand, SetCommandResult
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.4-CLI-001: [P0] parseSetCommand 解析 "set model sonnet" ---

func TestParseSetCommand_Model(t *testing.T) {
	cmd, err := parseSetCommand([]string{"model", "sonnet"})
	if err != nil {
		t.Fatalf("parseSetCommand: %v", err)
	}
	if cmd.SubCommand != "model" {
		t.Errorf("SubCommand = %q, want %q", cmd.SubCommand, "model")
	}
	if cmd.Value != "sonnet" {
		t.Errorf("Value = %q, want %q", cmd.Value, "sonnet")
	}
}

// --- 13.4-CLI-002: [P0] parseSetCommand 解析 "set context append 额外分析指令" ---

func TestParseSetCommand_ContextAppend(t *testing.T) {
	cmd, err := parseSetCommand([]string{"context", "append", "额外分析指令"})
	if err != nil {
		t.Fatalf("parseSetCommand: %v", err)
	}
	if cmd.SubCommand != "context" {
		t.Errorf("SubCommand = %q, want %q", cmd.SubCommand, "context")
	}
	if cmd.Action != "append" {
		t.Errorf("Action = %q, want %q", cmd.Action, "append")
	}
	if cmd.Value != "额外分析指令" {
		t.Errorf("Value = %q, want %q", cmd.Value, "额外分析指令")
	}
}

// --- 13.4-CLI-003: [P0] parseSetCommand 解析 "set skills add code-review" ---

func TestParseSetCommand_SkillsAdd(t *testing.T) {
	cmd, err := parseSetCommand([]string{"skills", "add", "code-review"})
	if err != nil {
		t.Fatalf("parseSetCommand: %v", err)
	}
	if cmd.SubCommand != "skills" {
		t.Errorf("SubCommand = %q, want %q", cmd.SubCommand, "skills")
	}
	if cmd.Action != "add" {
		t.Errorf("Action = %q, want %q", cmd.Action, "add")
	}
	if cmd.Value != "code-review" {
		t.Errorf("Value = %q, want %q", cmd.Value, "code-review")
	}
}

// --- 13.4-CLI-004: [P0] parseSetCommand 解析 "set env DEBUG=true" ---

func TestParseSetCommand_Env(t *testing.T) {
	cmd, err := parseSetCommand([]string{"env", "DEBUG=true"})
	if err != nil {
		t.Fatalf("parseSetCommand: %v", err)
	}
	if cmd.SubCommand != "env" {
		t.Errorf("SubCommand = %q, want %q", cmd.SubCommand, "env")
	}
	if cmd.Value != "DEBUG=true" {
		t.Errorf("Value = %q, want %q", cmd.Value, "DEBUG=true")
	}
}

// --- 13.4-CLI-005: [P1] parseSetCommand 无参数返回错误 ---

func TestParseSetCommand_NoArgs(t *testing.T) {
	_, err := parseSetCommand([]string{})
	if err == nil {
		t.Fatal("expected error for empty set command")
	}
}

// --- 13.4-CLI-006: [P1] parseSetCommand 未知子命令返回错误 ---

func TestParseSetCommand_UnknownSubCommand(t *testing.T) {
	_, err := parseSetCommand([]string{"unknown", "value"})
	if err == nil {
		t.Fatal("expected error for unknown set subcommand")
	}
}

// --- 13.4-CLI-007: [P1] parseSetCommand "set model" 无值返回错误 ---

func TestParseSetCommand_ModelNoValue(t *testing.T) {
	_, err := parseSetCommand([]string{"model"})
	if err == nil {
		t.Fatal("expected error for set model without value")
	}
}

// --- 13.4-CLI-008: [P1] parseSetCommand "set context" 无 action 返回错误 ---

func TestParseSetCommand_ContextNoAction(t *testing.T) {
	_, err := parseSetCommand([]string{"context"})
	if err == nil {
		t.Fatal("expected error for set context without action")
	}
}

// --- 13.4-CLI-009: [P1] parseSetCommand "set context append" 无文本返回错误 ---

func TestParseSetCommand_ContextAppendNoText(t *testing.T) {
	_, err := parseSetCommand([]string{"context", "append"})
	if err == nil {
		t.Fatal("expected error for set context append without text")
	}
}

// --- 13.4-CLI-010: [P1] parseSetCommand "set skills" 无 action 返回错误 ---

func TestParseSetCommand_SkillsNoAction(t *testing.T) {
	_, err := parseSetCommand([]string{"skills"})
	if err == nil {
		t.Fatal("expected error for set skills without action")
	}
}

// --- 13.4-CLI-011: [P1] parseSetCommand "set skills add" 无名称返回错误 ---

func TestParseSetCommand_SkillsAddNoName(t *testing.T) {
	_, err := parseSetCommand([]string{"skills", "add"})
	if err == nil {
		t.Fatal("expected error for set skills add without name")
	}
}

// --- 13.4-CLI-012: [P1] parseSetCommand "set env" 无 KEY=VALUE 返回错误 ---

func TestParseSetCommand_EnvNoKeyValue(t *testing.T) {
	_, err := parseSetCommand([]string{"env"})
	if err == nil {
		t.Fatal("expected error for set env without KEY=VALUE")
	}
}

// --- 13.4-CLI-012b: [P1] parseSetCommand "set env INVALID" 无等号返回错误 ---

func TestParseSetCommand_EnvNoEquals(t *testing.T) {
	_, err := parseSetCommand([]string{"env", "INVALID_NO_EQUALS"})
	if err == nil {
		t.Fatal("expected error for set env without = separator")
	}
}

// --- 13.4-CLI-013: [P0] SetCommandResult 结构体字段完整 ---

func TestSetCommandResult_Fields(t *testing.T) {
	result := SetCommandResult{
		SubCommand: "model",
		Action:     "",
		Value:      "sonnet",
	}
	if result.SubCommand != "model" {
		t.Errorf("SubCommand = %q, want %q", result.SubCommand, "model")
	}
	if result.Value != "sonnet" {
		t.Errorf("Value = %q, want %q", result.Value, "sonnet")
	}
}

// --- 13.4-CLI-014: [P0] parseSetCommand "set context append" 多词文本拼接 ---

func TestParseSetCommand_ContextAppendMultipleWords(t *testing.T) {
	cmd, err := parseSetCommand([]string{"context", "append", "额外", "分析", "指令"})
	if err != nil {
		t.Fatalf("parseSetCommand: %v", err)
	}
	if cmd.SubCommand != "context" {
		t.Errorf("SubCommand = %q, want %q", cmd.SubCommand, "context")
	}
	if cmd.Action != "append" {
		t.Errorf("Action = %q, want %q", cmd.Action, "append")
	}
	// Multiple words after "append" should be joined with spaces
	if cmd.Value != "额外 分析 指令" {
		t.Errorf("Value = %q, want %q", cmd.Value, "额外 分析 指令")
	}
}
