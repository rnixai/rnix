package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// ATDD Tests for Story 3.6: Step Output Streaming — AgentStepComplete UI 渲染
//
// RED PHASE: These tests reference ProgressReporter.AgentStepComplete which
// does not exist yet. They will not compile until the method is implemented.
//
// Tests verify:
// AC1: default 模式输出 "[agent/{pid}] step {step}: {summary}" (summary 非空时直接显示)
// AC2: plan 步骤输出 "[agent/{pid}] step {step}: plan (3 steps)"
// AC3: spawn 步骤输出格式正确
// AC4: quiet 模式不输出任何步骤摘要
// AC5: JSON 模式不输出（JSON 由 cmd 层处理）

// ============================================================
// AC1: tool_call default 模式渲染
// ============================================================

func TestATDD_3_6_AC1_AgentStepComplete_ToolCall_DefaultMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStepComplete(types.PID(1), 2, "tool_call", "/dev/fs → read sprint-status.yaml")

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("AC1: expected '[agent/1]' prefix, got %q", output)
	}
	if !strings.Contains(output, "step 2:") {
		t.Errorf("AC1: expected 'step 2:' in output, got %q", output)
	}
	if !strings.Contains(output, "/dev/fs") {
		t.Errorf("AC1: expected '/dev/fs' in summary, got %q", output)
	}
	if !strings.Contains(output, "\n") {
		t.Error("AC1: expected output to contain newline")
	}
}

// ============================================================
// AC2: plan default 模式渲染
// ============================================================

func TestATDD_3_6_AC2_AgentStepComplete_Plan_DefaultMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStepComplete(types.PID(1), 1, "plan", "plan (3 steps)")

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("AC2: expected '[agent/1]' prefix, got %q", output)
	}
	if !strings.Contains(output, "step 1:") {
		t.Errorf("AC2: expected 'step 1:' in output, got %q", output)
	}
	if !strings.Contains(output, "plan (3 steps)") {
		t.Errorf("AC2: expected 'plan (3 steps)' in output, got %q", output)
	}
}

// ============================================================
// AC3: spawn default 模式渲染
// ============================================================

func TestATDD_3_6_AC3_AgentStepComplete_Spawn_DefaultMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStepComplete(types.PID(1), 3, "spawn", `spawn PID 2 "分析代码"`)

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("AC3: expected '[agent/1]' prefix, got %q", output)
	}
	if !strings.Contains(output, "step 3:") {
		t.Errorf("AC3: expected 'step 3:' in output, got %q", output)
	}
	if !strings.Contains(output, "spawn PID 2") {
		t.Errorf("AC3: expected 'spawn PID 2' in output, got %q", output)
	}
}

// ============================================================
// AC1 补充: action 没有 summary 时仅显示 action 类型
// ============================================================

func TestATDD_3_6_AC1_AgentStepComplete_EmptySummary(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStepComplete(types.PID(1), 4, "complete", "")

	output := buf.String()
	if !strings.Contains(output, "step 4:") {
		t.Errorf("AC1-empty: expected 'step 4:' in output, got %q", output)
	}
	if !strings.Contains(output, "complete") {
		t.Errorf("AC1-empty: expected 'complete' action in output, got %q", output)
	}
	// Should NOT contain " → " when summary is empty
	if strings.Contains(output, " → ") {
		t.Errorf("AC1-empty: should not contain ' → ' when summary is empty, got %q", output)
	}
}

// ============================================================
// AC4: quiet 模式静默
// ============================================================

func TestATDD_3_6_AC4_AgentStepComplete_QuietMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStepComplete(types.PID(1), 1, "tool_call", "/dev/fs → read file")
	p.AgentStepComplete(types.PID(1), 2, "plan", "plan (5 steps)")
	p.AgentStepComplete(types.PID(1), 3, "spawn", `spawn PID 3 "task"`)

	if buf.Len() != 0 {
		t.Errorf("AC4: expected no output in quiet mode, got %q", buf.String())
	}
}

// ============================================================
// AC5: JSON 模式静默（JSON 输出由 cmd 层处理）
// ============================================================

func TestATDD_3_6_AC5_AgentStepComplete_JSONMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStepComplete(types.PID(1), 1, "tool_call", "/dev/fs → read file")
	p.AgentStepComplete(types.PID(1), 2, "plan", "plan (3 steps)")

	if buf.Len() != 0 {
		t.Errorf("AC5: expected no output in JSON mode (handled by cmd layer), got %q", buf.String())
	}
}

// ============================================================
// AC1 补充: verbose 模式正常输出
// ============================================================

func TestATDD_3_6_AC1_AgentStepComplete_VerboseMode(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeVerbose, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStepComplete(types.PID(1), 2, "tool_call", "/dev/shell → ls -la")

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("AC1-verbose: expected output in verbose mode, got %q", output)
	}
	if !strings.Contains(output, "step 2:") {
		t.Errorf("AC1-verbose: expected 'step 2:' in output, got %q", output)
	}
}

// ============================================================
// Story 3.6 附加: AgentStep 默认模式静默（改为仅 verbose 输出）
// ============================================================

func TestATDD_3_6_AgentStep_DefaultMode_Silent(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStep(types.PID(1), 2, 5)

	// After Story 3.6, AgentStep should be silent in default mode
	// (only verbose mode outputs "reasoning step N...")
	if buf.Len() != 0 {
		t.Errorf("AC-AgentStep: expected no output in default mode after 3.6, got %q", buf.String())
	}
}

func TestATDD_3_6_AgentStep_VerboseMode_StillOutputs(t *testing.T) {
	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeVerbose, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentStep(types.PID(1), 2, 5)

	output := buf.String()
	if !strings.Contains(output, "reasoning step 2") {
		t.Errorf("AC-AgentStep-verbose: expected 'reasoning step 2' in verbose mode, got %q", output)
	}
}
