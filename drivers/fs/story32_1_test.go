package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// Story 32.1: Tests for edit_file, glob, grep, ToolDef metadata, and embed templates.

// --- edit_file tests ---

func TestExecEdit_UniqueMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world\nfoo bar\n"), 0o644)

	factory := FileFactory()
	file, err := factory("/"+filepath.Base(path), vfs.O_WRONLY, dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":         "edit",
		"old_string": "foo bar",
		"new_string": "baz qux",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "replaced 1") {
		t.Errorf("expected success message, got: %s", data)
	}

	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "baz qux") {
		t.Errorf("file not updated: %s", content)
	}
	if strings.Contains(string(content), "foo bar") {
		t.Errorf("old text still present: %s", content)
	}
}

func TestExecEdit_MultipleMatchesError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("aaa\naaa\naaa\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/"+filepath.Base(path), vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":         "edit",
		"old_string": "aaa",
		"new_string": "bbb",
	})
	err := file.Write(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "3 locations") {
		t.Errorf("expected multi-match error, got: %v", err)
	}
}

func TestExecEdit_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("aaa\naaa\naaa\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/"+filepath.Base(path), vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":          "edit",
		"old_string":  "aaa",
		"new_string":  "bbb",
		"replace_all": true,
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	if !strings.Contains(string(data), "replaced 3") {
		t.Errorf("expected 3 replacements, got: %s", data)
	}

	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "aaa") {
		t.Errorf("old text still present: %s", content)
	}
}

func TestExecEdit_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/"+filepath.Base(path), vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":         "edit",
		"old_string": "nonexistent",
		"new_string": "replacement",
	})
	err := file.Write(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for old_string not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

func TestExecEdit_PreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o755)

	factory := FileFactory()
	file, _ := factory("/"+filepath.Base(path), vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":         "edit",
		"old_string": "echo old",
		"new_string": "echo new",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o755 {
		t.Errorf("permissions changed: got %o, want 755", info.Mode().Perm())
	}
}

// --- glob tests ---

func TestExecGlob_DoubleStarPattern(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "app.go"), []byte("package src"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "util.go"), []byte("package pkg"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":      "glob",
		"pattern": "**/*.go",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []globEntry `json:"matches"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Matches) != 3 {
		t.Errorf("expected 3 Go files, got %d: %v", len(result.Matches), result.Matches)
	}

	// Verify no .md files
	for _, m := range result.Matches {
		if strings.HasSuffix(m.Path, ".md") {
			t.Errorf("unexpected .md match: %s", m.Path)
		}
	}
}

func TestExecGlob_SkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "objects", "pack.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":      "glob",
		"pattern": "**/*.go",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []globEntry `json:"matches"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) != 1 {
		t.Errorf("expected 1 match (skipping .git), got %d", len(result.Matches))
	}
}

func TestExecGlob_MtimeSort(t *testing.T) {
	dir := t.TempDir()
	// Create files with different mtimes
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":      "glob",
		"pattern": "*.txt",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []globEntry `json:"matches"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result.Matches))
	}

	// Verify sorted by mtime descending
	isSorted := sort.SliceIsSorted(result.Matches, func(i, j int) bool {
		return result.Matches[i].Mtime > result.Matches[j].Mtime
	})
	if !isSorted {
		t.Error("results not sorted by mtime descending")
	}
}

func TestExecGlob_HeadLimit(t *testing.T) {
	dir := t.TempDir()
	for i := range 10 {
		os.WriteFile(filepath.Join(dir, filepath.Base(t.Name())+string(rune('a'+i))+".txt"), []byte("x"), 0o644)
	}

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":         "glob",
		"pattern":    "*.txt",
		"head_limit": 3,
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []globEntry `json:"matches"`
		Notice  string      `json:"notice"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) != 3 {
		t.Errorf("expected 3 matches (limited), got %d", len(result.Matches))
	}
	if result.Notice == "" {
		t.Error("expected truncation notice")
	}
}

// --- grep tests ---

func TestExecGrep_FilesWithMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("func main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("func helper() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("no match here\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":          "grep",
		"pattern":     "func",
		"output_mode": "files_with_matches",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []grepMatch `json:"matches"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) != 2 {
		t.Errorf("expected 2 matching files, got %d", len(result.Matches))
	}
}

func TestExecGrep_ContentMode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("line1\nfunc main() {}\nline3\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":          "grep",
		"pattern":     "func main",
		"output_mode": "content",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []grepMatch `json:"matches"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) == 0 {
		t.Fatal("expected matches")
	}
	if result.Matches[0].Line != 2 {
		t.Errorf("expected line 2, got %d", result.Matches[0].Line)
	}
}

func TestExecGrep_CountMode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("func a() {}\nfunc b() {}\nvar x\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":          "grep",
		"pattern":     "func",
		"output_mode": "count",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []grepMatch `json:"matches"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 file result, got %d", len(result.Matches))
	}
	if result.Matches[0].Count != 2 {
		t.Errorf("expected count 2, got %d", result.Matches[0].Count)
	}
}

func TestExecGrep_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("Hello World\nhello world\nHELLO WORLD\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":               "grep",
		"pattern":          "hello",
		"output_mode":      "count",
		"case_insensitive": true,
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []grepMatch `json:"matches"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Matches))
	}
	if result.Matches[0].Count != 3 {
		t.Errorf("expected count 3 (case insensitive), got %d", result.Matches[0].Count)
	}
}

func TestExecGrep_ContextLines(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("1\n2\n3\nMATCH\n5\n6\n7\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":          "grep",
		"pattern":     "MATCH",
		"output_mode": "content",
		"context":     2,
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []grepMatch `json:"matches"`
	}
	json.Unmarshal(data, &result)

	// Should include lines 2,3,MATCH,5,6 (context=2 around line 4)
	if len(result.Matches) != 5 {
		t.Errorf("expected 5 lines (match + 2 context each side), got %d", len(result.Matches))
	}
}

func TestExecGrep_GlobFilter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("func main\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.txt"), []byte("func main\n"), 0o644)

	factory := FileFactory()
	file, _ := factory("/.", vfs.O_WRONLY, dir)
	defer file.Close()

	req, _ := json.Marshal(map[string]any{
		"op":          "grep",
		"pattern":     "func",
		"output_mode": "files_with_matches",
		"glob":        "*.go",
	})
	if err := file.Write(context.Background(), req); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := file.Read(0)
	var result struct {
		Matches []grepMatch `json:"matches"`
	}
	json.Unmarshal(data, &result)

	if len(result.Matches) != 1 {
		t.Errorf("expected 1 match (only .go), got %d", len(result.Matches))
	}
}

// --- ToolDef metadata tests ---

func TestToolDefs_MetadataAnnotations(t *testing.T) {
	driver := NewDriver()
	defs := driver.ToolDefs()

	defMap := make(map[string]vfs.ToolDef)
	for _, d := range defs {
		defMap[d.Name] = d
	}

	// read_file: read-only, concurrency safe
	if rd, ok := defMap["read_file"]; ok {
		if !rd.IsReadOnly {
			t.Error("read_file should be IsReadOnly")
		}
		if !rd.IsConcurrencySafe {
			t.Error("read_file should be IsConcurrencySafe")
		}
		if rd.IsDestructive {
			t.Error("read_file should not be IsDestructive")
		}
	} else {
		t.Error("read_file ToolDef not found")
	}

	// write_file: concurrency safe, not destructive (per spec table)
	if wr, ok := defMap["write_file"]; ok {
		if !wr.IsConcurrencySafe {
			t.Error("write_file should be IsConcurrencySafe")
		}
		if wr.IsReadOnly {
			t.Error("write_file should not be IsReadOnly")
		}
	} else {
		t.Error("write_file ToolDef not found")
	}

	// edit_file: not explicitly destructive (surgical edit)
	if _, ok := defMap["edit_file"]; !ok {
		t.Error("edit_file ToolDef not found")
	}

	// glob: read-only, concurrency safe
	if g, ok := defMap["glob"]; ok {
		if !g.IsReadOnly {
			t.Error("glob should be IsReadOnly")
		}
		if !g.IsConcurrencySafe {
			t.Error("glob should be IsConcurrencySafe")
		}
	} else {
		t.Error("glob ToolDef not found")
	}

	// grep: read-only, concurrency safe
	if g, ok := defMap["grep"]; ok {
		if !g.IsReadOnly {
			t.Error("grep should be IsReadOnly")
		}
		if !g.IsConcurrencySafe {
			t.Error("grep should be IsConcurrencySafe")
		}
	} else {
		t.Error("grep ToolDef not found")
	}
}

// --- Embed template tests ---

func TestToolDefs_EmbedDescriptions(t *testing.T) {
	driver := NewDriver()
	defs := driver.ToolDefs()

	for _, d := range defs {
		if d.Description == "" {
			t.Errorf("ToolDef %q has empty description (embed failed?)", d.Name)
		}
		// Descriptions from embed should be multi-line (not one-liners)
		if !strings.Contains(d.Description, "\n") {
			t.Errorf("ToolDef %q description looks like it's not from embed template: %q", d.Name, d.Description[:min(50, len(d.Description))])
		}
	}
}

func TestLoadPrompt_KnownTemplates(t *testing.T) {
	for _, name := range []string{"read_file", "write_file", "list_dir", "edit_file", "glob", "grep"} {
		content := loadPrompt(name)
		if content == "" {
			t.Errorf("loadPrompt(%q) returned empty", name)
		}
	}
}

func TestLoadPrompt_UnknownReturnsEmpty(t *testing.T) {
	content := loadPrompt("nonexistent_tool")
	if content != "" {
		t.Errorf("loadPrompt for unknown tool should return empty, got: %q", content)
	}
}

// --- matchGlob tests ---

func TestMatchGlob_Patterns(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.txt", false},
		{"**/*.go", "src/main.go", true},
		{"**/*.go", "src/pkg/util.go", true},
		{"**/*.go", "main.go", true},
		{"src/**/*.go", "src/main.go", true},
		{"src/**/*.go", "src/pkg/util.go", true},
		{"src/**/*.go", "lib/main.go", false},
	}

	for _, tt := range tests {
		got := matchGlob(tt.pattern, tt.name)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}
