package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

// ============================================================
// Epic 25 TA: Supplemental automated tests
// ============================================================

func TestExtractEmbedded_EmptyFS(t *testing.T) {
	fsys := fstest.MapFS{}
	target := t.TempDir()

	// Extracting from an empty FS with a nonexistent srcRoot should return an error
	err := ExtractEmbedded(fsys, "nonexistent", target)
	if err == nil {
		t.Error("ExtractEmbedded(empty FS, nonexistent root) expected error, got nil")
	}
}

func TestExtractEmbedded_EmptySrcDir(t *testing.T) {
	// FS has the directory but no files inside
	fsys := fstest.MapFS{
		"lib/agents": {Mode: os.ModeDir | 0o755},
	}
	target := t.TempDir()

	err := ExtractEmbedded(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbedded(empty dir) error = %v", err)
	}

	// Target should exist but be empty
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty target, got %d entries", len(entries))
	}
}

func TestExtractEmbedded_BinaryContent(t *testing.T) {
	// Verify binary files are extracted correctly
	binaryData := []byte{0x00, 0x01, 0xFF, 0xFE, 0x89, 0x50, 0x4E, 0x47}
	fsys := fstest.MapFS{
		"data/icon.png": {Data: binaryData},
	}
	target := t.TempDir()

	err := ExtractEmbedded(fsys, "data", target)
	if err != nil {
		t.Fatalf("ExtractEmbedded(binary) error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(target, "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(content, binaryData) {
		t.Errorf("binary content mismatch: got %v, want %v", content, binaryData)
	}
}

// ============================================================
// ExtractEmbeddedSmart tests
// ============================================================

func TestExtractEmbeddedSmart_FirstRun(t *testing.T) {
	fsys := newTestFS()
	target := t.TempDir()

	// Pre-create a file with the SAME content as embedded (simulates old init)
	coderDir := filepath.Join(target, "coder")
	os.MkdirAll(coderDir, 0o755)
	os.WriteFile(filepath.Join(coderDir, "agent.yaml"), []byte("name: coder\n"), 0o644)

	// Pre-create a file with DIFFERENT content (user modified)
	os.WriteFile(filepath.Join(coderDir, "instructions.md"), []byte("my custom instructions\n"), 0o644)

	ExtractEmbeddedSmart(fsys, "lib/agents", target)

	// Unchanged file: should NOT be overwritten (same content), but hash should be recorded
	content, _ := os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if string(content) != "name: coder\n" {
		t.Errorf("coder/agent.yaml = %q, want %q", content, "name: coder\n")
	}

	// User-modified file: should be preserved
	content, _ = os.ReadFile(filepath.Join(target, "coder", "instructions.md"))
	if string(content) != "my custom instructions\n" {
		t.Errorf("instructions.md was overwritten: got %q", content)
	}

	// New file (planner) should be extracted
	content, _ = os.ReadFile(filepath.Join(target, "planner", "agent.yaml"))
	if string(content) != "name: planner\n" {
		t.Errorf("planner/agent.yaml = %q, want %q", content, "name: planner\n")
	}

	// Hash file should exist
	hashData, err := os.ReadFile(filepath.Join(target, builtinHashesFile))
	if err != nil {
		t.Fatalf("hash file not created: %v", err)
	}
	var hashes map[string]string
	if err := json.Unmarshal(hashData, &hashes); err != nil {
		t.Fatalf("invalid hash file: %v", err)
	}
	if len(hashes) == 0 {
		t.Error("hash file is empty")
	}
}

func TestExtractEmbeddedSmart_UpgradeUnmodified(t *testing.T) {
	target := t.TempDir()

	// Phase 1: initial extraction with v1 content
	v1FS := fstest.MapFS{
		"lib/agents/coder/agent.yaml":      {Data: []byte("v1 content\n")},
		"lib/agents/coder/instructions.md": {Data: []byte("v1 instructions\n")},
	}
	ExtractEmbeddedSmart(v1FS, "lib/agents", target)

	content, _ := os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if string(content) != "v1 content\n" {
		t.Fatalf("phase 1: agent.yaml = %q, want %q", content, "v1 content\n")
	}

	// Phase 2: upgrade with v2 content — files untouched by user
	v2FS := fstest.MapFS{
		"lib/agents/coder/agent.yaml":      {Data: []byte("v2 content\n")},
		"lib/agents/coder/instructions.md": {Data: []byte("v2 instructions\n")},
	}
	ExtractEmbeddedSmart(v2FS, "lib/agents", target)

	content, _ = os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if string(content) != "v2 content\n" {
		t.Errorf("phase 2: agent.yaml = %q, want %q (should be upgraded)", content, "v2 content\n")
	}
	content, _ = os.ReadFile(filepath.Join(target, "coder", "instructions.md"))
	if string(content) != "v2 instructions\n" {
		t.Errorf("phase 2: instructions.md = %q, want %q", content, "v2 instructions\n")
	}
}

func TestExtractEmbeddedSmart_PreserveUserModified(t *testing.T) {
	target := t.TempDir()

	// Phase 1: initial extraction
	v1FS := fstest.MapFS{
		"lib/agents/coder/agent.yaml": {Data: []byte("v1 content\n")},
	}
	ExtractEmbeddedSmart(v1FS, "lib/agents", target)

	// User modifies the file
	os.WriteFile(filepath.Join(target, "coder", "agent.yaml"), []byte("user customized\n"), 0o644)

	// Phase 2: upgrade — user-modified file should be preserved
	v2FS := fstest.MapFS{
		"lib/agents/coder/agent.yaml": {Data: []byte("v2 content\n")},
	}
	ExtractEmbeddedSmart(v2FS, "lib/agents", target)

	content, _ := os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if string(content) != "user customized\n" {
		t.Errorf("user-modified file was overwritten: got %q, want %q", content, "user customized\n")
	}
}

func TestExtractEmbeddedSmart_NewFile(t *testing.T) {
	target := t.TempDir()

	// Phase 1: extract v1 with only one file
	v1FS := fstest.MapFS{
		"lib/agents/coder/agent.yaml": {Data: []byte("v1\n")},
	}
	ExtractEmbeddedSmart(v1FS, "lib/agents", target)

	// Phase 2: v2 adds a new file
	v2FS := fstest.MapFS{
		"lib/agents/coder/agent.yaml":      {Data: []byte("v1\n")},
		"lib/agents/coder/instructions.md": {Data: []byte("new file\n")},
	}
	ExtractEmbeddedSmart(v2FS, "lib/agents", target)

	content, err := os.ReadFile(filepath.Join(target, "coder", "instructions.md"))
	if err != nil {
		t.Fatalf("new file not created: %v", err)
	}
	if string(content) != "new file\n" {
		t.Errorf("new file content = %q, want %q", content, "new file\n")
	}
}

func TestExtractEmbeddedForce_CreatesNewFiles(t *testing.T) {
	fsys := newTestFS()
	target := t.TempDir()

	// Force extract to empty target should work the same as normal extract
	err := ExtractEmbeddedForce(fsys, "lib/agents", target)
	if err != nil {
		t.Fatalf("ExtractEmbeddedForce() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(target, "coder", "agent.yaml"))
	if err != nil {
		t.Fatalf("read coder/agent.yaml: %v", err)
	}
	if string(content) != "name: coder\n" {
		t.Errorf("coder/agent.yaml = %q, want %q", content, "name: coder\n")
	}
}
