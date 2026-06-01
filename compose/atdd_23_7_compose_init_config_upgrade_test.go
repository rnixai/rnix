package compose

import (
	"context"
	"testing"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
)

// ============================================================================
// ATDD Tests for Story 23.7: rnix-compose/init 配置格式升级
// Validates Provider field on ComposeSpec, AgentSpec, and ComposeSpawnOpts.
// ============================================================================

// ============================================================
// AC1: Compose YAML agent 指定 provider + model → Spawn 传递正确
// ============================================================

// TestATDD_23_7_AC1_ComposeProviderPassedToSpawn verifies that when an agent
// in rnix-compose.yaml specifies provider: ollama + model: llama3, the compose
// engine correctly passes both provider and model to the spawner.
func TestATDD_23_7_AC1_ComposeProviderPassedToSpawn(t *testing.T) {
	t.Parallel()

	// Given: a compose YAML with agent-level provider + model
	data := []byte(`
version: "1.0"
intent: "test provider passthrough"
agents:
  worker:
    intent: "run with ollama"
    provider: ollama
    model: llama3
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	// Verify parse-level fields
	if spec.Agents["worker"].Provider != "ollama" {
		t.Fatalf("AC1: expected AgentSpec.Provider = %q, got %q", "ollama", spec.Agents["worker"].Provider)
	}
	if spec.Agents["worker"].Model != "llama3" {
		t.Fatalf("AC1: expected AgentSpec.Model = %q, got %q", "llama3", spec.Agents["worker"].Model)
	}

	// When: executing through the engine
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Then: spawn opts contain the correct provider and model
	if len(ks.spawned) != 1 {
		t.Fatalf("AC1: expected 1 spawn, got %d", len(ks.spawned))
	}
	rec := ks.spawned[0]
	if rec.opts.Provider != "ollama" {
		t.Errorf("AC1: expected ComposeSpawnOpts.Provider = %q, got %q", "ollama", rec.opts.Provider)
	}
	if rec.opts.Model != "llama3" {
		t.Errorf("AC1: expected ComposeSpawnOpts.Model = %q, got %q", "llama3", rec.opts.Model)
	}
}

// ============================================================
// AC2: 向后兼容——旧格式仅指定 model，无 provider
// ============================================================

// TestATDD_23_7_AC2_ComposeBackwardCompat verifies backward compatibility:
// old format with only model: haiku (no provider field) defaults to empty string.
func TestATDD_23_7_AC2_ComposeBackwardCompat(t *testing.T) {
	t.Parallel()

	// Given: a compose YAML with only model, no provider (old format)
	data := []byte(`
version: "1.0"
intent: "backward compat test"
agents:
  worker:
    intent: "run with default provider"
    model: haiku
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	// Verify parse-level: Provider should be empty
	if spec.Agents["worker"].Provider != "" {
		t.Fatalf("AC2: expected empty AgentSpec.Provider for old format, got %q", spec.Agents["worker"].Provider)
	}

	// When: executing through the engine
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Then: spawn opts have empty provider (system default = claude)
	if len(ks.spawned) != 1 {
		t.Fatalf("AC2: expected 1 spawn, got %d", len(ks.spawned))
	}
	rec := ks.spawned[0]
	if rec.opts.Provider != "" {
		t.Errorf("AC2: expected empty Provider for backward compat, got %q", rec.opts.Provider)
	}
	if rec.opts.Model != "haiku" {
		t.Errorf("AC2: expected Model = %q, got %q", "haiku", rec.opts.Model)
	}
}

// TestATDD_23_7_AC2_ComposeGlobalProviderFallback verifies that when the
// top-level spec has a provider and the agent does not, the global provider
// is used as fallback.
func TestATDD_23_7_AC2_ComposeGlobalProviderFallback(t *testing.T) {
	t.Parallel()

	// Given: a compose YAML with global provider, agent without provider
	data := []byte(`
version: "1.0"
intent: "global provider fallback"
provider: groq
agents:
  worker:
    intent: "inherit global provider"
    model: llama3
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	// Verify parse-level: ComposeSpec.Provider = "groq", AgentSpec.Provider = ""
	if spec.Provider != "groq" {
		t.Fatalf("AC2: expected ComposeSpec.Provider = %q, got %q", "groq", spec.Provider)
	}
	if spec.Agents["worker"].Provider != "" {
		t.Fatalf("AC2: expected empty AgentSpec.Provider, got %q", spec.Agents["worker"].Provider)
	}

	// When: executing through the engine
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Then: spawn opts use global provider as fallback
	if len(ks.spawned) != 1 {
		t.Fatalf("AC2: expected 1 spawn, got %d", len(ks.spawned))
	}
	rec := ks.spawned[0]
	if rec.opts.Provider != "groq" {
		t.Errorf("AC2: expected Provider = %q (global fallback), got %q", "groq", rec.opts.Provider)
	}
}

// TestATDD_23_7_AC2_AgentProviderOverridesGlobal verifies that agent-level
// provider takes priority over the global spec-level provider.
func TestATDD_23_7_AC2_AgentProviderOverridesGlobal(t *testing.T) {
	t.Parallel()

	// Given: spec has global provider "groq", agent has provider "ollama"
	data := []byte(`
version: "1.0"
intent: "agent overrides global"
provider: groq
agents:
  worker:
    intent: "use ollama not groq"
    provider: ollama
    model: llama3
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	// When: executing through the engine
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Then: agent-level provider wins
	if len(ks.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(ks.spawned))
	}
	rec := ks.spawned[0]
	if rec.opts.Provider != "ollama" {
		t.Errorf("AC2: expected Provider = %q (agent overrides global), got %q", "ollama", rec.opts.Provider)
	}
}

// ============================================================
// Unit Tests: Compose Parser — Provider field parsing
// ============================================================

// TestParseBytes_AgentProvider verifies AgentSpec.Provider is parsed from YAML.
func TestParseBytes_AgentProvider(t *testing.T) {
	t.Parallel()

	data := []byte(`
version: "1.0"
intent: "provider parse test"
agents:
  worker:
    intent: "task"
    provider: ollama
    model: llama3
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if spec.Agents["worker"].Provider != "ollama" {
		t.Errorf("expected AgentSpec.Provider = %q, got %q", "ollama", spec.Agents["worker"].Provider)
	}
}

// TestParseBytes_GlobalProvider verifies ComposeSpec.Provider is parsed from YAML.
func TestParseBytes_GlobalProvider(t *testing.T) {
	t.Parallel()

	data := []byte(`
version: "1.0"
intent: "global provider test"
provider: groq
agents:
  worker:
    intent: "task"
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if spec.Provider != "groq" {
		t.Errorf("expected ComposeSpec.Provider = %q, got %q", "groq", spec.Provider)
	}
}

// TestParseBytes_NoProvider_BackwardCompat verifies that YAML without provider
// fields results in empty string (backward compatible).
func TestParseBytes_NoProvider_BackwardCompat(t *testing.T) {
	t.Parallel()

	data := []byte(`
version: "1.0"
intent: "no provider test"
agents:
  worker:
    intent: "task"
    model: haiku
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if spec.Provider != "" {
		t.Errorf("expected empty ComposeSpec.Provider, got %q", spec.Provider)
	}
	if spec.Agents["worker"].Provider != "" {
		t.Errorf("expected empty AgentSpec.Provider, got %q", spec.Agents["worker"].Provider)
	}
}

// ============================================================
// Unit Tests: Engine — Provider passthrough
// ============================================================

// TestEngine_Execute_AgentProviderPassedToSpawn verifies engine passes
// agent-level provider to ComposeSpawnOpts.
func TestEngine_Execute_AgentProviderPassedToSpawn(t *testing.T) {
	t.Parallel()

	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "provider passthrough",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "task", Provider: "ollama", Model: "llama3"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(ks.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(ks.spawned))
	}
	if ks.spawned[0].opts.Provider != "ollama" {
		t.Errorf("expected Provider = %q, got %q", "ollama", ks.spawned[0].opts.Provider)
	}
}

// TestEngine_Execute_GlobalProviderFallback verifies engine falls back to
// spec-level provider when agent has none.
func TestEngine_Execute_GlobalProviderFallback(t *testing.T) {
	t.Parallel()

	spec := &ComposeSpec{
		Version:  "1.0",
		Intent:   "global provider fallback",
		Provider: "groq",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "task", Model: "llama3"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(ks.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(ks.spawned))
	}
	if ks.spawned[0].opts.Provider != "groq" {
		t.Errorf("expected Provider = %q (global fallback), got %q", "groq", ks.spawned[0].opts.Provider)
	}
}

// TestEngine_Execute_AgentProviderOverridesGlobal verifies agent-level
// provider overrides spec-level.
func TestEngine_Execute_AgentProviderOverridesGlobal(t *testing.T) {
	t.Parallel()

	spec := &ComposeSpec{
		Version:  "1.0",
		Intent:   "agent overrides global",
		Provider: "groq",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "task", Provider: "ollama", Model: "llama3"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(ks.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(ks.spawned))
	}
	if ks.spawned[0].opts.Provider != "ollama" {
		t.Errorf("expected Provider = %q (agent overrides global), got %q", "ollama", ks.spawned[0].opts.Provider)
	}
}

// TestEngine_Execute_NoProvider_EmptyString verifies no provider config
// results in empty string.
func TestEngine_Execute_NoProvider_EmptyString(t *testing.T) {
	t.Parallel()

	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "no provider",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "task"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(ks.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(ks.spawned))
	}
	if ks.spawned[0].opts.Provider != "" {
		t.Errorf("expected empty Provider, got %q", ks.spawned[0].opts.Provider)
	}
}

// ============================================================
// Compile-time type existence checks
// ============================================================

// TestATDD_23_7_TypesExist verifies that new Provider fields compile.
func TestATDD_23_7_TypesExist(t *testing.T) {
	t.Parallel()

	// ComposeSpec.Provider must exist
	var cs ComposeSpec
	_ = cs.Provider

	// AgentSpec.Provider must exist
	var as AgentSpec
	_ = as.Provider

	// ComposeSpawnOpts.Provider must exist
	var so ComposeSpawnOpts
	_ = so.Provider

	// Verify mockKernelSpawner still works (interface compat)
	var _ KernelSpawner = newMockKernelSpawner()

	// Verify existing types are unaffected
	_ = ComposeSpec{Version: "1.0", Intent: "test", Model: "haiku", Provider: "groq", Agents: map[string]*AgentSpec{}}
	_ = AgentSpec{Intent: "task", Provider: "ollama", Model: "llama3"}
	_ = ComposeSpawnOpts{Model: "llama3", Provider: "ollama", MaxTokens: 1000}

	// Verify mock records Provider
	ks := newMockKernelSpawner()
	_, _ = ks.Spawn("test", &agents.AgentInfo{}, ComposeSpawnOpts{Provider: "ollama"})
	if len(ks.spawned) != 1 || ks.spawned[0].opts.Provider != "ollama" {
		t.Fatalf("mock spawner should record Provider")
	}

	_ = types.PID(0) // ensure types import is used
}
