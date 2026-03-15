package agents

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 21.3: 声誉系统与自动选择
//
// AgentManifest.Alternatives 字段测试。
// 测试引用 AgentManifest.Alternatives 字段，该字段尚不存在。
// 测试将无法编译直到实现完成。
//
// RED → GREEN: 在 agents/types.go 添加 Alternatives 字段，测试编译并通过。
// ============================================================

// --- 21.3-AGENT-001: [P0] AgentManifest parses alternatives field (AC4) ---

func TestAgentManifest_Alternatives(t *testing.T) {
	// Given: an agent.yaml with alternatives field
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "lib", "agents", "code-reviewer-v2")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	agentYAML := `name: code-reviewer-v2
description: "Code review v2"
models:
  provider: claude
  preferred: claude-sonnet-4-6
skills:
  - code-analysis
alternatives:
  - code-reviewer-v1
  - code-reviewer-legacy
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "instructions.md"), []byte("Review code"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loader := NewAgentLoader([]string{filepath.Join(dir, "lib", "agents")}, nil, nil)
	info, err := loader.Load("code-reviewer-v2")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Then: Alternatives field is parsed correctly
	if info.Manifest.Alternatives == nil {
		t.Fatal("expected non-nil Alternatives")
	}
	if len(info.Manifest.Alternatives) != 2 {
		t.Fatalf("expected 2 alternatives, got %d", len(info.Manifest.Alternatives))
	}
	if info.Manifest.Alternatives[0] != "code-reviewer-v1" {
		t.Fatalf("expected first alternative 'code-reviewer-v1', got %q", info.Manifest.Alternatives[0])
	}
	if info.Manifest.Alternatives[1] != "code-reviewer-legacy" {
		t.Fatalf("expected second alternative 'code-reviewer-legacy', got %q", info.Manifest.Alternatives[1])
	}
}

// --- 21.3-AGENT-002: [P0] AgentManifest without alternatives - backward compat (AC5) ---

func TestAgentManifest_NoAlternatives(t *testing.T) {
	// Given: an agent.yaml without alternatives field
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "lib", "agents", "simple-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	agentYAML := `name: simple-agent
description: "Simple agent"
models:
  provider: claude
  preferred: claude-sonnet-4-6
skills:
  - basic-skill
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "instructions.md"), []byte("Instructions"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loader := NewAgentLoader([]string{filepath.Join(dir, "lib", "agents")}, nil, nil)
	info, err := loader.Load("simple-agent")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Then: Alternatives is nil (backward compatible)
	if len(info.Manifest.Alternatives) > 0 {
		t.Fatalf("expected nil/empty Alternatives when not defined, got %v", info.Manifest.Alternatives)
	}
}
