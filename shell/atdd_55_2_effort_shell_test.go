package shell

import (
	"context"
	"strings"
	"testing"
)

// ============================================================
// ATDD — Story 55.2: per-request reasoning_effort 外部入口（AgentShell 层）
// AC #4 (--effort 字面量) + AC #5 (变量展开) + AC #6 (全站点覆盖) +
// AC #8 (透传铁律) + AC #9 (零回归)
//
// RED 机制（[[atdd-code-story-red-mechanism-preference]]）：
// - Command.ReasoningEffort / SpawnRequest.ReasoningEffort / mockCall.reasoningEffort
//   已加骨架字段（可编译）；
// - parseSpawnCommand 尚无 `--effort=` 分支 → 字面量命中 else → "unexpected token"；
// - expandSpawnProviders / script_exec 各站点尚未透传 effort；
// → 标 t.Skip 的测试移除 skip 后 FAIL（真 RED）。
// - SH-005（省略 --effort 回归）骨架下自然 GREEN → GREEN-GUARD 不 skip，
//   实时守护「不传 effort 时字段空 + agent/model/provider 零回归」。
//
// 同构模板：spawn_arg_expansion_test.go（provider flag 平行链）。
// ============================================================

// --- 55-2-SH-001 [P0]: parseSpawnCommand 解析 --effort=high（不报 unexpected token） (AC #4) ---

func TestEffortShell_ParseSpawnCommand_EffortFlag(t *testing.T) {

	pipeline, err := ParsePipeline(`spawn "x" --agent=a --effort=high`)
	if err != nil {
		t.Fatalf("AC#4: --effort must parse cleanly, got: %v", err)
	}
	cmd := pipeline.Commands[0]
	if cmd.ReasoningEffort != "high" {
		t.Errorf("AC#4: ReasoningEffort = %q, want %q", cmd.ReasoningEffort, "high")
	}
}

// --- 55-2-SH-002 [P0]: 字面量 --effort=high 透传到 SpawnRequest (AC #4) ---

func TestEffortShell_LiteralEffort_Passthrough(t *testing.T) {

	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`spawn "x" --agent=a --effort=high`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawner.calls))
	}
	if spawner.calls[0].reasoningEffort != "high" {
		t.Errorf("AC#4: reasoningEffort = %q, want %q", spawner.calls[0].reasoningEffort, "high")
	}
}

// --- 55-2-SH-003 [P0]: 变量展开 --effort=$e（export e=high）展开后透传 (AC #5) ---

func TestEffortShell_VarExpansion(t *testing.T) {

	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`
export e=high
spawn "x" --effort=$e
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if spawner.calls[0].reasoningEffort != "high" {
		t.Errorf("AC#5: expanded effort = %q, want %q", spawner.calls[0].reasoningEffort, "high")
	}
}

// --- 55-2-SH-004 [P0]: 未定义变量 --effort=$nope → 报错含 line N + --effort，spawn 不发起 (AC #5) ---

func TestEffortShell_UndefinedEffortVar_ErrorsBeforeSpawn(t *testing.T) {

	spawner := &mockSpawner{}
	script, err := ParseScript(`spawn "x" --effort=$nope`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	_, execErr := exec.Execute(context.Background(), script)
	if execErr == nil {
		t.Fatal("AC#5: expected error for undefined $nope, got nil")
	}
	if !strings.Contains(execErr.Error(), "line 1") {
		t.Errorf("AC#5: error should contain line number, got: %v", execErr)
	}
	if !strings.Contains(execErr.Error(), "--effort") {
		t.Errorf("AC#5: error should mention --effort, got: %v", execErr)
	}
	if len(spawner.calls) != 0 {
		t.Errorf("AC#5: spawn must not fire on expansion error, got %d calls", len(spawner.calls))
	}
}

// --- 55-2-SH-005 [P1]: 省略 --effort → 字段空 + agent/model/provider 零回归 (AC #9) ---
// GREEN-GUARD（不 skip）：骨架下不传 --effort 时字段自然为空，实时守护零回归。

func TestEffortShell_OmittedEffort_EmptyAndUnchanged(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`spawn "x" --agent=a --model=m --provider=deepseek`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	c := spawner.calls[0]
	if c.reasoningEffort != "" {
		t.Errorf("AC#9: omitted --effort must stay empty, got %q", c.reasoningEffort)
	}
	if c.agent != "a" || c.model != "m" || c.provider != "deepseek" {
		t.Errorf("AC#9: agent/model/provider regression: %+v", c)
	}
}

// --- 55-2-SH-006 [P1]: on-error 站点 fallback spawn 透传展开后 effort (AC #6) ---

func TestEffortShell_OnErrorSite(t *testing.T) {

	spawner := &mockSpawner{results: []mockResult{
		{result: "boom", exitCode: 1},
		{result: "recovered", exitCode: 0},
	}}
	script, err := ParseScript(`spawn "main" on-error spawn "fix" --effort=low`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("expected 2 spawn calls (main + on-error), got %d", len(spawner.calls))
	}
	if spawner.calls[1].reasoningEffort != "low" {
		t.Errorf("AC#6: on-error effort = %q, want %q", spawner.calls[1].reasoningEffort, "low")
	}
}

// --- 55-2-SH-007 [P1]: pipeline 各 stage 透传 effort (AC #6) ---

func TestEffortShell_PipelineStages(t *testing.T) {

	spawner := &mockSpawner{results: []mockResult{
		{result: "a", exitCode: 0},
		{result: "b", exitCode: 0},
	}}
	script, err := ParseScript(`spawn "s1" --effort=high | spawn "s2" --effort=low`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("expected 2 spawn calls, got %d", len(spawner.calls))
	}
	if spawner.calls[0].reasoningEffort != "high" {
		t.Errorf("AC#6: stage 1 effort = %q, want %q", spawner.calls[0].reasoningEffort, "high")
	}
	if spawner.calls[1].reasoningEffort != "low" {
		t.Errorf("AC#6: stage 2 effort = %q, want %q", spawner.calls[1].reasoningEffort, "low")
	}
}

// --- 55-2-SH-008 [P1]: parallel 块 worker 收到展开后 effort (AC #6) ---

func TestEffortShell_ParallelBlock(t *testing.T) {

	spawner := &concurrentMockSpawner{}
	script, err := ParseScript(`
export e=high
parallel
  spawn "p1" --effort=$e
  spawn "p2" --effort=low
end
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 parallel spawn calls, got %d", len(calls))
	}
	seen := map[string]bool{}
	for _, c := range calls {
		seen[c.reasoningEffort] = true
	}
	if !seen["high"] || !seen["low"] {
		t.Errorf("AC#6: parallel spawns missing expanded efforts: %+v", calls)
	}
}
