package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeepMergeYAML_BothEmpty(t *testing.T) {
	result := DeepMergeYAML(map[string]any{}, map[string]any{})
	if len(result) != 0 {
		t.Errorf("DeepMergeYAML(empty, empty) = %v, want empty map", result)
	}
}

func TestDeepMergeYAML_OnlyBase(t *testing.T) {
	base := map[string]any{"a": 1}
	result := DeepMergeYAML(base, map[string]any{})
	want := map[string]any{"a": 1}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML(base, empty) = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_OnlyOverride(t *testing.T) {
	override := map[string]any{"b": 2}
	result := DeepMergeYAML(map[string]any{}, override)
	want := map[string]any{"b": 2}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML(empty, override) = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_NestedRecursive(t *testing.T) {
	base := map[string]any{
		"a": map[string]any{"x": 1},
	}
	override := map[string]any{
		"a": map[string]any{"y": 2},
	}
	result := DeepMergeYAML(base, override)
	want := map[string]any{
		"a": map[string]any{"x": 1, "y": 2},
	}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML nested = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_TypeConflict_OverrideWins(t *testing.T) {
	base := map[string]any{
		"a": map[string]any{"x": 1},
	}
	override := map[string]any{
		"a": "scalar",
	}
	result := DeepMergeYAML(base, override)
	want := map[string]any{
		"a": "scalar",
	}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML type conflict = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_SliceReplace(t *testing.T) {
	base := map[string]any{
		"list": []any{1, 2, 3},
	}
	override := map[string]any{
		"list": []any{4, 5},
	}
	result := DeepMergeYAML(base, override)
	want := map[string]any{
		"list": []any{4, 5},
	}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML slice = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_ThreeLevelDeep(t *testing.T) {
	base := map[string]any{
		"l1": map[string]any{
			"l2": map[string]any{
				"l3": "base",
				"b":  true,
			},
		},
	}
	override := map[string]any{
		"l1": map[string]any{
			"l2": map[string]any{
				"l3": "override",
				"c":  42,
			},
		},
	}
	result := DeepMergeYAML(base, override)
	want := map[string]any{
		"l1": map[string]any{
			"l2": map[string]any{
				"l3": "override",
				"b":  true,
				"c":  42,
			},
		},
	}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML 3-level = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_NilValues(t *testing.T) {
	base := map[string]any{
		"a": 1,
		"b": nil,
	}
	override := map[string]any{
		"b": 2,
		"c": nil,
	}
	result := DeepMergeYAML(base, override)
	want := map[string]any{
		"a": 1,
		"b": 2,
		"c": nil,
	}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML nil = %v, want %v", result, want)
	}
}

func TestShadowResolve_ProjectExists(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	// Create "coder" in both
	os.Mkdir(filepath.Join(projectDir, "coder"), 0o755)
	os.Mkdir(filepath.Join(globalDir, "coder"), 0o755)

	got := ShadowResolve("coder", projectDir, globalDir)
	want := filepath.Join(projectDir, "coder")
	if got != want {
		t.Errorf("ShadowResolve(project exists) = %q, want %q", got, want)
	}
}

func TestShadowResolve_OnlyGlobal(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	// Create "coder" only in global
	os.Mkdir(filepath.Join(globalDir, "coder"), 0o755)

	got := ShadowResolve("coder", projectDir, globalDir)
	want := filepath.Join(globalDir, "coder")
	if got != want {
		t.Errorf("ShadowResolve(global only) = %q, want %q", got, want)
	}
}

func TestShadowResolve_NotFound(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	got := ShadowResolve("coder", projectDir, globalDir)
	if got != "" {
		t.Errorf("ShadowResolve(not found) = %q, want empty string", got)
	}
}

func TestShadowResolve_FileNotDir(t *testing.T) {
	dir := t.TempDir()
	// Create a file (not a directory) named "coder"
	os.WriteFile(filepath.Join(dir, "coder"), []byte("file"), 0o644)

	got := ShadowResolve("coder", dir)
	if got != "" {
		t.Errorf("ShadowResolve(file not dir) = %q, want empty string", got)
	}
}

func TestListMerged_Dedup_Sorted(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	// Project has "coder"
	os.Mkdir(filepath.Join(projectDir, "coder"), 0o755)
	// Global has "coder" and "planner"
	os.Mkdir(filepath.Join(globalDir, "coder"), 0o755)
	os.Mkdir(filepath.Join(globalDir, "planner"), 0o755)

	got, err := ListMerged(projectDir, globalDir)
	if err != nil {
		t.Fatalf("ListMerged() error = %v", err)
	}
	want := []string{"coder", "planner"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListMerged() = %v, want %v", got, want)
	}
}

func TestListMerged_EmptyDirs(t *testing.T) {
	dir := t.TempDir()

	got, err := ListMerged(dir)
	if err != nil {
		t.Fatalf("ListMerged() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListMerged(empty) = %v, want empty", got)
	}
}

func TestListMerged_NonexistentDir(t *testing.T) {
	got, err := ListMerged("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("ListMerged() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListMerged(nonexistent) = %v, want empty", got)
	}
}

func TestListMerged_SkipsFiles(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "agent-a"), 0o755)
	os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("hi"), 0o644)

	got, err := ListMerged(dir)
	if err != nil {
		t.Fatalf("ListMerged() error = %v", err)
	}
	want := []string{"agent-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListMerged() = %v, want %v", got, want)
	}
}
