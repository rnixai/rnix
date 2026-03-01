package compose

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Story 7.1: YAML Parsing Tests ---
// These tests verify AC #1 (YAML parsing) and AC #5 (YAML format support).
// All tests are in RED phase: they reference ParseFile/ParseBytes which do not exist yet.

func TestParseBytes_Valid(t *testing.T) {
	// Given: a valid crux-compose.yaml with multiple agents and dependencies
	data := []byte(`
version: "1.0"
intent: "PR review + code analysis"
agents:
  reviewer:
    intent: "review PR changes"
    skills: [pr-reviewer]
  analyst:
    intent: "analyze code quality"
    skills: [code-analyst]
    depends_on:
      reviewer: completed
`)

	// When: parsing the YAML
	spec, err := ParseBytes(data)

	// Then: parsing succeeds and fields are correct
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if spec.Version != "1.0" {
		t.Fatalf("expected version '1.0', got %q", spec.Version)
	}
	if spec.Intent != "PR review + code analysis" {
		t.Fatalf("expected intent 'PR review + code analysis', got %q", spec.Intent)
	}
	if len(spec.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(spec.Agents))
	}

	// Verify reviewer agent
	reviewer, ok := spec.Agents["reviewer"]
	if !ok {
		t.Fatal("expected 'reviewer' agent")
	}
	if reviewer.Intent != "review PR changes" {
		t.Fatalf("reviewer intent: expected 'review PR changes', got %q", reviewer.Intent)
	}
	if len(reviewer.Skills) != 1 || reviewer.Skills[0] != "pr-reviewer" {
		t.Fatalf("reviewer skills: expected [pr-reviewer], got %v", reviewer.Skills)
	}

	// Verify analyst agent with dependency
	analyst, ok := spec.Agents["analyst"]
	if !ok {
		t.Fatal("expected 'analyst' agent")
	}
	if analyst.Intent != "analyze code quality" {
		t.Fatalf("analyst intent: expected 'analyze code quality', got %q", analyst.Intent)
	}
	dep, ok := analyst.DependsOn["reviewer"]
	if !ok {
		t.Fatal("analyst should depend on reviewer")
	}
	if dep != "completed" {
		t.Fatalf("expected depends_on value 'completed', got %q", dep)
	}
}

func TestParseBytes_FullFormat(t *testing.T) {
	// Given: a crux-compose.yaml with all supported fields including agent reference and model
	data := []byte(`
version: "1.0"
intent: "PR review + analysis + documentation"
agents:
  reviewer:
    intent: "review PR changes"
    agent: "pr-reviewer"
    model: "opus"
    skills: [pr-reviewer]
  analyst:
    intent: "analyze code quality"
    model: "haiku"
    skills: [code-analyst]
    depends_on:
      reviewer: completed
  writer:
    intent: "write change documentation"
    skills: [doc-writer]
    depends_on:
      reviewer: completed
      analyst: completed
`)

	// When: parsing the YAML
	spec, err := ParseBytes(data)

	// Then: all fields are correctly extracted
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if len(spec.Agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(spec.Agents))
	}

	reviewer := spec.Agents["reviewer"]
	if reviewer.Agent != "pr-reviewer" {
		t.Fatalf("expected agent reference 'pr-reviewer', got %q", reviewer.Agent)
	}
	if reviewer.Model != "opus" {
		t.Fatalf("expected model 'opus', got %q", reviewer.Model)
	}

	analyst := spec.Agents["analyst"]
	if analyst.Model != "haiku" {
		t.Fatalf("expected model 'haiku', got %q", analyst.Model)
	}

	writer := spec.Agents["writer"]
	if len(writer.DependsOn) != 2 {
		t.Fatalf("expected 2 dependencies for writer, got %d", len(writer.DependsOn))
	}
	if writer.Model != "" {
		t.Fatalf("expected empty model for writer, got %q", writer.Model)
	}
}

func TestParseBytes_NoDependencies(t *testing.T) {
	// Given: a crux-compose.yaml with no dependencies (all agents independent)
	data := []byte(`
version: "1.0"
intent: "parallel analysis"
agents:
  a:
    intent: "task A"
  b:
    intent: "task B"
  c:
    intent: "task C"
`)

	// When: parsing the YAML
	spec, err := ParseBytes(data)

	// Then: parsing succeeds with empty depends_on
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if len(spec.Agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(spec.Agents))
	}
	for name, agent := range spec.Agents {
		if len(agent.DependsOn) != 0 {
			t.Fatalf("agent %q should have no dependencies, got %v", name, agent.DependsOn)
		}
	}
}

func TestParseFile_Valid(t *testing.T) {
	// Given: a valid YAML file on disk
	dir := t.TempDir()
	fpath := filepath.Join(dir, "crux-compose.yaml")
	data := []byte(`
version: "1.0"
intent: "test workflow"
agents:
  worker:
    intent: "do work"
    skills: [worker-skill]
`)
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// When: parsing the file
	spec, err := ParseFile(fpath)

	// Then: parsing succeeds
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if spec.Version != "1.0" {
		t.Fatalf("expected version '1.0', got %q", spec.Version)
	}
	if len(spec.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(spec.Agents))
	}
}

func TestParseFile_NotFound(t *testing.T) {
	// Given: a non-existent file path
	// When: parsing the file
	_, err := ParseFile("/nonexistent/crux-compose.yaml")

	// Then: an error is returned
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestParseBytes_InvalidYAML(t *testing.T) {
	// Given: invalid YAML syntax
	data := []byte(`
version: "1.0"
intent: "bad yaml
  this is broken: [
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: an error is returned
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestParseBytes_InvalidVersion(t *testing.T) {
	// Given: a YAML with unsupported version
	data := []byte(`
version: "2.0"
intent: "test"
agents:
  a:
    intent: "task"
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: version validation error
	if err == nil {
		t.Fatal("expected error for invalid version, got nil")
	}
}

func TestParseBytes_MissingVersion(t *testing.T) {
	// Given: a YAML with no version field
	data := []byte(`
intent: "test"
agents:
  a:
    intent: "task"
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: validation error for missing version
	if err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
}

func TestParseBytes_EmptyAgents(t *testing.T) {
	// Given: a YAML with empty agents map
	data := []byte(`
version: "1.0"
intent: "empty workflow"
agents: {}
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: validation error
	if err == nil {
		t.Fatal("expected error for empty agents, got nil")
	}
}

func TestParseBytes_MissingAgents(t *testing.T) {
	// Given: a YAML with no agents field
	data := []byte(`
version: "1.0"
intent: "no agents"
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: validation error
	if err == nil {
		t.Fatal("expected error for missing agents, got nil")
	}
}

func TestParseBytes_AgentMissingIntent(t *testing.T) {
	// Given: an agent without an intent field
	data := []byte(`
version: "1.0"
intent: "test"
agents:
  worker:
    skills: [some-skill]
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: validation error for missing agent intent
	if err == nil {
		t.Fatal("expected error for agent missing intent, got nil")
	}
}

func TestParseBytes_DependsOnInvalidRef(t *testing.T) {
	// Given: depends_on references a non-existent agent
	data := []byte(`
version: "1.0"
intent: "test"
agents:
  a:
    intent: "task A"
    depends_on:
      nonexistent: completed
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: validation error for invalid dependency reference
	if err == nil {
		t.Fatal("expected error for depends_on referencing non-existent agent, got nil")
	}
}

func TestParseBytes_DependsOnInvalidCondition(t *testing.T) {
	// Given: depends_on uses unsupported condition (only "completed" is valid)
	data := []byte(`
version: "1.0"
intent: "test"
agents:
  a:
    intent: "task A"
  b:
    intent: "task B"
    depends_on:
      a: started
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: validation error for unsupported condition
	if err == nil {
		t.Fatal("expected error for unsupported depends_on condition, got nil")
	}
}

func TestParseBytes_MissingTopLevelIntent(t *testing.T) {
	// Given: a YAML missing the top-level intent
	data := []byte(`
version: "1.0"
agents:
  a:
    intent: "task"
`)

	// When: parsing
	_, err := ParseBytes(data)

	// Then: validation error for missing intent
	if err == nil {
		t.Fatal("expected error for missing top-level intent, got nil")
	}
}

func TestParseBytes_SingleAgent(t *testing.T) {
	// Given: a valid YAML with a single agent (no dependencies possible)
	data := []byte(`
version: "1.0"
intent: "single task"
agents:
  solo:
    intent: "do everything"
    skills: [multi-tool]
`)

	// When: parsing
	spec, err := ParseBytes(data)

	// Then: single agent parsed correctly
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if len(spec.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(spec.Agents))
	}
	solo := spec.Agents["solo"]
	if solo == nil {
		t.Fatal("expected 'solo' agent")
	}
	if solo.Intent != "do everything" {
		t.Fatalf("expected intent 'do everything', got %q", solo.Intent)
	}
}

func TestParseBytes_GlobalModel(t *testing.T) {
	// Given: a crux-compose.yaml with a top-level model field
	data := []byte(`
version: "1.0"
intent: "cost-optimized workflow"
model: "haiku"
agents:
  fast:
    intent: "quick task"
  precise:
    intent: "deep analysis"
    model: "opus"
`)

	// When: parsing the YAML
	spec, err := ParseBytes(data)

	// Then: global model is parsed correctly
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if spec.Model != "haiku" {
		t.Fatalf("expected global model 'haiku', got %q", spec.Model)
	}

	// Agent without model inherits nothing at parse level (engine handles fallback)
	if spec.Agents["fast"].Model != "" {
		t.Fatalf("expected empty model for 'fast', got %q", spec.Agents["fast"].Model)
	}

	// Agent with explicit model retains it
	if spec.Agents["precise"].Model != "opus" {
		t.Fatalf("expected model 'opus' for 'precise', got %q", spec.Agents["precise"].Model)
	}
}
