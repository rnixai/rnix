package config

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func newTestFS() fstest.MapFS {
	return fstest.MapFS{
		"lib/agents/coder/agent.yaml":          {Data: []byte("name: coder\n")},
		"lib/agents/coder/instructions.md":     {Data: []byte("# Coder\n")},
		"lib/agents/planner/agent.yaml":        {Data: []byte("name: planner\n")},
		"lib/agents/planner/instructions.md":   {Data: []byte("# Planner\n")},
		"lib/agents/planner/sub/nested.txt":    {Data: []byte("nested content\n")},
	}
}

func TestExtractEmbedded_EmptyTarget(t *testing.T) {
	fsys := newTestFS()
	target := t.TempDir()

	err := ExtractEmbedded(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbedded() error = %v", err)
	}

	// Check coder/agent.yaml was created
	content, err := os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if err != nil {
		t.Fatalf("read coder/agent.yaml: %v", err)
	}
	if string(content) != "name: coder\n" {
		t.Errorf("coder/agent.yaml = %q, want %q", content, "name: coder\n")
	}

	// Check planner/agent.yaml was created
	content, err = os.ReadFile(filepath.Join(target, "planner", "agent.yaml"))
	if err != nil {
		t.Fatalf("read planner/agent.yaml: %v", err)
	}
	if string(content) != "name: planner\n" {
		t.Errorf("planner/agent.yaml = %q, want %q", content, "name: planner\n")
	}
}

func TestExtractEmbedded_SkipExisting(t *testing.T) {
	fsys := newTestFS()
	target := t.TempDir()

	// Pre-create coder/agent.yaml with different content
	coderDir := filepath.Join(target, "coder")
	os.MkdirAll(coderDir, 0o755)
	os.WriteFile(filepath.Join(coderDir, "agent.yaml"), []byte("custom content"), 0o644)

	err := ExtractEmbedded(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbedded() error = %v", err)
	}

	// Existing file should NOT be overwritten
	content, err := os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "custom content" {
		t.Errorf("existing file was overwritten: got %q, want %q", content, "custom content")
	}

	// But non-existing files should be created
	content, err = os.ReadFile(filepath.Join(target, "coder", "instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Coder\n" {
		t.Errorf("coder/instructions.md = %q, want %q", content, "# Coder\n")
	}
}

func TestExtractEmbedded_SkipExistingDir(t *testing.T) {
	fsys := newTestFS()
	target := t.TempDir()

	// Pre-create coder directory
	os.MkdirAll(filepath.Join(target, "coder"), 0o755)

	err := ExtractEmbedded(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbedded() error = %v", err)
	}

	// coder directory should still exist
	info, err := os.Stat(filepath.Join(target, "coder"))
	if err != nil || !info.IsDir() {
		t.Error("coder directory should exist")
	}
}

func TestExtractEmbedded_NestedStructure(t *testing.T) {
	fsys := newTestFS()
	target := t.TempDir()

	err := ExtractEmbedded(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbedded() error = %v", err)
	}

	// Check nested file
	content, err := os.ReadFile(filepath.Join(target, "planner", "sub", "nested.txt"))
	if err != nil {
		t.Fatalf("read planner/sub/nested.txt: %v", err)
	}
	if string(content) != "nested content\n" {
		t.Errorf("nested.txt = %q, want %q", content, "nested content\n")
	}
}

func TestExtractEmbedded_PathPrefix(t *testing.T) {
	// Verify that the srcRoot prefix (e.g. "lib/agents") is correctly stripped
	// so files appear directly under targetDir, not under targetDir/lib/agents/
	fsys := newTestFS()
	target := t.TempDir()

	err := ExtractEmbedded(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbedded() error = %v", err)
	}

	// The file should be at target/coder/agent.yaml, NOT target/lib/agents/coder/agent.yaml
	correctPath := filepath.Join(target, "coder", "agent.yaml")
	if _, err := os.Stat(correctPath); err != nil {
		t.Errorf("expected file at %q, got error: %v", correctPath, err)
	}

	wrongPath := filepath.Join(target, "lib", "agents", "coder", "agent.yaml")
	if _, err := os.Stat(wrongPath); err == nil {
		t.Errorf("file should NOT exist at %q (prefix not stripped)", wrongPath)
	}
}

func TestExtractEmbeddedForce_Overwrite(t *testing.T) {
	fsys := newTestFS()
	target := t.TempDir()

	// Pre-create coder/agent.yaml with different content
	coderDir := filepath.Join(target, "coder")
	os.MkdirAll(coderDir, 0o755)
	os.WriteFile(filepath.Join(coderDir, "agent.yaml"), []byte("old content"), 0o644)

	err := ExtractEmbeddedForce(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbeddedForce() error = %v", err)
	}

	// File should be overwritten with embedded content
	content, err := os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "name: coder\n" {
		t.Errorf("coder/agent.yaml = %q, want overwritten %q", content, "name: coder\n")
	}
}
