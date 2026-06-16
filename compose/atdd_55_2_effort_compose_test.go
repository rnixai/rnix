package compose

import (
	"context"
	"testing"
)

// ============================================================
// ATDD — Story 55.2: per-request reasoning_effort 外部入口（compose 层）
// AC #7 (agent 级 + spec 级 reasoning_effort) + AC #8 (透传铁律)
//
// RED 机制（[[atdd-code-story-red-mechanism-preference]]）：
// - ComposeSpec/AgentSpec/ComposeSpawnOpts.ReasoningEffort 已加骨架 yaml 字段
//   → ParseBytes 解析层会填充 AgentSpec.ReasoningEffort（解析层非红）；
// - engine.go:154-159 尚无 effort 优先级解析 + 未拷进 ComposeSpawnOpts
//   → 断言落在 ComposeSpawnOpts.ReasoningEffort（透传终点）→ 移 skip 后 FAIL。
// - CMP-004（两者皆空回归）骨架下自然 GREEN → GREEN-GUARD 不 skip。
//
// 同构模板：atdd_23_7_compose_init_config_upgrade_test.go（provider 优先级链）。
// 优先级与 Model/Provider 一致：agent 级 > spec 级 > 空。
// ============================================================

// --- 55-2-CMP-001 [P0]: agent 级 reasoning_effort → ComposeSpawnOpts 透传 (AC #7) ---

func TestEffortCompose_AgentLevel_PassedToSpawn(t *testing.T) {

	data := []byte(`
version: "1.0"
intent: "test effort passthrough"
agents:
  worker:
    intent: "run with high effort"
    reasoning_effort: high
`)
	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	// 解析层（骨架 yaml tag 已支持，非红）
	if spec.Agents["worker"].ReasoningEffort != "high" {
		t.Fatalf("AC#7: expected AgentSpec.ReasoningEffort = %q, got %q", "high", spec.Agents["worker"].ReasoningEffort)
	}

	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if _, err := engine.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// 透传终点（engine 未实现 → 红）
	if len(ks.spawned) != 1 {
		t.Fatalf("AC#7: expected 1 spawn, got %d", len(ks.spawned))
	}
	if ks.spawned[0].opts.ReasoningEffort != "high" {
		t.Errorf("AC#7: expected ComposeSpawnOpts.ReasoningEffort = %q, got %q", "high", ks.spawned[0].opts.ReasoningEffort)
	}
}

// --- 55-2-CMP-002 [P0]: spec 级回落（agent 空 + 顶层 reasoning_effort） (AC #7) ---

func TestEffortCompose_SpecLevelFallback(t *testing.T) {

	data := []byte(`
version: "1.0"
intent: "spec-level effort default"
reasoning_effort: low
agents:
  worker:
    intent: "inherit spec effort"
`)
	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if spec.ReasoningEffort != "low" {
		t.Fatalf("AC#7: expected ComposeSpec.ReasoningEffort = %q, got %q", "low", spec.ReasoningEffort)
	}

	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if _, err := engine.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if ks.spawned[0].opts.ReasoningEffort != "low" {
		t.Errorf("AC#7: spec-level fallback effort = %q, want %q", ks.spawned[0].opts.ReasoningEffort, "low")
	}
}

// --- 55-2-CMP-003 [P1]: 优先级 agent 级 > spec 级 (AC #7) ---

func TestEffortCompose_AgentOverridesSpec(t *testing.T) {

	data := []byte(`
version: "1.0"
intent: "agent overrides spec effort"
reasoning_effort: low
agents:
  worker:
    intent: "agent-level wins"
    reasoning_effort: high
`)
	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if _, err := engine.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if ks.spawned[0].opts.ReasoningEffort != "high" {
		t.Errorf("AC#7: agent-level effort must override spec-level: got %q, want %q",
			ks.spawned[0].opts.ReasoningEffort, "high")
	}
}

// --- 55-2-CMP-004 [P1]: 两者皆空 → effort 空 + provider/model 不受影响 (AC #7 零回归) ---
// GREEN-GUARD（不 skip）：未配 effort 时骨架自然为空，实时守护零回归。

func TestEffortCompose_BothEmpty_NoRegression(t *testing.T) {
	data := []byte(`
version: "1.0"
intent: "no effort configured"
agents:
  worker:
    intent: "plain"
    provider: ollama
    model: llama3
`)
	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if _, err := engine.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	opts := ks.spawned[0].opts
	if opts.ReasoningEffort != "" {
		t.Errorf("AC#7: unconfigured effort must stay empty, got %q", opts.ReasoningEffort)
	}
	if opts.Provider != "ollama" || opts.Model != "llama3" {
		t.Errorf("AC#7: provider/model regression: provider=%q model=%q", opts.Provider, opts.Model)
	}
}

// --- 55-2-CMP-005 [P1]: 透传铁律——Gemini HIGH 大写原样（不转换） (AC #8) ---

func TestEffortCompose_Passthrough_UppercaseVerbatim(t *testing.T) {

	data := []byte(`
version: "1.0"
intent: "gemini uppercase effort"
agents:
  worker:
    intent: "gemini"
    reasoning_effort: HIGH
`)
	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if _, err := engine.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// 透传铁律：大写 HIGH 原样保留，不归一化为 high
	if ks.spawned[0].opts.ReasoningEffort != "HIGH" {
		t.Errorf("AC#8: Gemini uppercase HIGH must pass through verbatim, got %q",
			ks.spawned[0].opts.ReasoningEffort)
	}
}
