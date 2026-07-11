package agtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBytes_SingleTestCase(t *testing.T) {
	data := []byte(`
version: "1.0"
name: "basic-test"
intent: "say hello"
agent:
  name: "greeter"
`)
	suite, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(suite.Tests))
	}
	tc := suite.Tests[0]
	if tc.Intent != "say hello" {
		t.Errorf("intent = %q, want %q", tc.Intent, "say hello")
	}
	if tc.Agent.Name != "greeter" {
		t.Errorf("agent.name = %q, want %q", tc.Agent.Name, "greeter")
	}
	if tc.Name != "basic-test" {
		t.Errorf("name = %q, want %q", tc.Name, "basic-test")
	}
}

func TestParseBytes_SingleTestCase_AllFields(t *testing.T) {
	data := []byte(`
version: "1.0"
name: "full-test"
intent: "recommend a song"
agent:
  name: "recommender"
  model: "claude-sonnet-4-20250514"
  skills:
    - "music"
    - "politeness"
  context_budget: 4096
timeout: 30000
assert:
  output:
    contains:
      - "song"
    not_contains:
      - "ERROR"
  syscalls:
    includes:
      - "CtxWrite"
  quality:
    criteria: "must include a song recommendation"
`)
	suite, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tc := suite.Tests[0]
	if tc.Agent.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", tc.Agent.Model, "claude-sonnet-4-20250514")
	}
	if len(tc.Agent.Skills) != 2 {
		t.Errorf("skills count = %d, want 2", len(tc.Agent.Skills))
	}
	if tc.Agent.ContextBudget != 4096 {
		t.Errorf("context_budget = %d, want 4096", tc.Agent.ContextBudget)
	}
	if tc.Timeout != 30000 {
		t.Errorf("timeout = %d, want 30000", tc.Timeout)
	}
	if tc.Assert == nil {
		t.Fatal("assert is nil")
	}
	if tc.Assert.Output == nil || len(tc.Assert.Output.Contains) != 1 {
		t.Error("assert.output.contains not parsed correctly")
	}
	if tc.Assert.Syscalls == nil || len(tc.Assert.Syscalls.Includes) != 1 {
		t.Error("assert.syscalls.includes not parsed correctly")
	}
	if tc.Assert.Quality == nil || tc.Assert.Quality.Criteria == "" {
		t.Error("assert.quality.criteria not parsed correctly")
	}
}

func TestParseBytes_TestSuite(t *testing.T) {
	data := []byte(`
version: "1.0"
name: "my-suite"
tests:
  - name: "test-a"
    intent: "do A"
    agent:
      name: "agent-a"
  - name: "test-b"
    intent: "do B"
    agent:
      name: "agent-b"
`)
	suite, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if suite.Name != "my-suite" {
		t.Errorf("suite name = %q, want %q", suite.Name, "my-suite")
	}
	if len(suite.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Intent != "do A" {
		t.Errorf("test[0].intent = %q", suite.Tests[0].Intent)
	}
	if suite.Tests[1].Agent.Name != "agent-b" {
		t.Errorf("test[1].agent.name = %q", suite.Tests[1].Agent.Name)
	}
}

func TestParseBytes_TestSuite_Multiple(t *testing.T) {
	data := []byte(`
version: "1.0"
tests:
  - name: "t1"
    intent: "intent-1"
    agent:
      name: "a1"
  - name: "t2"
    intent: "intent-2"
    agent:
      name: "a2"
  - name: "t3"
    intent: "intent-3"
    agent:
      name: "a3"
`)
	suite, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Tests) != 3 {
		t.Fatalf("expected 3 tests, got %d", len(suite.Tests))
	}
}

func TestParseBytes_AutoDetect_SingleToSuite(t *testing.T) {
	data := []byte(`
version: "1.0"
intent: "single case"
agent:
  name: "solo"
`)
	suite, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("single case should be wrapped into suite with 1 test, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Intent != "single case" {
		t.Errorf("intent = %q", suite.Tests[0].Intent)
	}
}

func TestParseBytes_InvalidYAML(t *testing.T) {
	data := []byte(`version: "1.0"
name: "broken
intent: [
  this is: not valid yaml`)
	_, err := ParseBytes(data)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestParseBytes_EmptyInput(t *testing.T) {
	_, err := ParseBytes([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}

	_, err = ParseBytes([]byte("   \n\t  \n"))
	if err == nil {
		t.Fatal("expected error for whitespace-only input, got nil")
	}
}

func TestParseFile_ValidFile(t *testing.T) {
	suite, err := ParseFile("testdata/valid-single.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Intent != "向用户打招呼" {
		t.Errorf("intent = %q", suite.Tests[0].Intent)
	}
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := ParseFile("testdata/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestParseFile_AssertOutputOnlyFixture(t *testing.T) {
	suite, err := ParseFile("testdata/assert-output-only.yaml")
	if err != nil {
		t.Fatalf("expected valid parse, got: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(suite.Tests))
	}
	tc := suite.Tests[0]
	if tc.Assert == nil || tc.Assert.Output == nil {
		t.Fatal("assert.output should be present")
	}
	if len(tc.Assert.Output.Contains) != 1 || tc.Assert.Output.Contains[0] != "代码示例" {
		t.Errorf("assert.output.contains = %v, want [代码示例]", tc.Assert.Output.Contains)
	}
	if len(tc.Assert.Output.NotContains) != 1 || tc.Assert.Output.NotContains[0] != "ERROR" {
		t.Errorf("assert.output.not_contains = %v, want [ERROR]", tc.Assert.Output.NotContains)
	}
}

func TestParseFile_AssertInvalidEmptyFixture(t *testing.T) {
	_, err := ParseFile("testdata/assert-invalid-empty.yaml")
	if err == nil {
		t.Fatal("expected validation error for empty output assert, got nil")
	}
	if !strings.Contains(err.Error(), "assert.output") {
		t.Errorf("error should mention assert.output, got: %v", err)
	}
}

func TestParseDir_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "a.yaml", `
version: "1.0"
intent: "test A"
agent:
  name: "agent-a"
`)
	writeTestYAML(t, dir, "b.yaml", `
version: "1.0"
intent: "test B"
agent:
  name: "agent-b"
`)

	suite, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(suite.Tests))
	}
}

func TestParseDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := ParseDir(dir)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

func TestParseDir_IgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "test.yaml", `
version: "1.0"
intent: "real test"
agent:
  name: "agent"
`)
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# not a test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	suite, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test (ignoring non-yaml), got %d", len(suite.Tests))
	}
}

func writeTestYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// --- Story 68.2: Provider field + SourceDir ---

func TestParseBytes_ProviderField(t *testing.T) {
	data := []byte(`
version: "1.0"
name: "replay-case"
intent: "run echo"
agent:
  provider: replay
  model: scripts/01-echo.responses.yaml
`)
	suite, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tc := suite.Tests[0]
	if tc.Agent.Provider != "replay" {
		t.Errorf("agent.provider = %q, want %q", tc.Agent.Provider, "replay")
	}
	if tc.Agent.Model != "scripts/01-echo.responses.yaml" {
		t.Errorf("agent.model = %q, want script path", tc.Agent.Model)
	}
	// ParseBytes has no file origin — SourceDir must remain empty.
	if tc.SourceDir != "" {
		t.Errorf("SourceDir = %q, want empty for ParseBytes", tc.SourceDir)
	}
}

func TestParseFile_SourceDirFilled(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "case.yaml", `
version: "1.0"
name: "sd-case"
intent: "hi"
agent:
  provider: replay
  model: scripts/x.responses.yaml
`)
	suite, err := ParseFile(filepath.Join(dir, "case.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	absDir, _ := filepath.Abs(dir)
	if suite.Tests[0].SourceDir != absDir {
		t.Errorf("SourceDir = %q, want %q", suite.Tests[0].SourceDir, absDir)
	}
}

func TestParseDir_SourceDirFilled(t *testing.T) {
	dir := t.TempDir()
	writeTestYAML(t, dir, "a.yaml", `
version: "1.0"
name: "a"
intent: "hi"
agent:
  provider: replay
  model: scripts/a.responses.yaml
`)
	suite, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	absDir, _ := filepath.Abs(dir)
	if suite.Tests[0].SourceDir != absDir {
		t.Errorf("SourceDir = %q, want %q", suite.Tests[0].SourceDir, absDir)
	}
}
