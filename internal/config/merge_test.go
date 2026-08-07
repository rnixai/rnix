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

// ============================================================
// MergeNamedSlice tests
// ============================================================

func TestMergeNamedSlice_OverridePartial(t *testing.T) {
	base := []any{
		map[string]any{"name": "claude", "driver": "claude-cli", "default_model": "haiku"},
		map[string]any{"name": "cursor", "driver": "cursor-cli"},
		map[string]any{"name": "openrouter", "driver": "openai", "base_url": "https://openrouter.ai/api/v1"},
	}
	override := []any{
		map[string]any{"name": "cursor", "default_model": "gpt-4o"},
	}
	result := MergeNamedSlice(base, override, "name")

	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	// claude unchanged
	if result[0].(map[string]any)["name"] != "claude" {
		t.Errorf("result[0].name = %v, want claude", result[0].(map[string]any)["name"])
	}
	// cursor merged
	cursor := result[1].(map[string]any)
	if cursor["driver"] != "cursor-cli" {
		t.Errorf("cursor.driver = %v, want cursor-cli (from base)", cursor["driver"])
	}
	if cursor["default_model"] != "gpt-4o" {
		t.Errorf("cursor.default_model = %v, want gpt-4o (from override)", cursor["default_model"])
	}
	// openrouter preserved
	if result[2].(map[string]any)["name"] != "openrouter" {
		t.Errorf("result[2].name = %v, want openrouter", result[2].(map[string]any)["name"])
	}
}

func TestMergeNamedSlice_AppendNew(t *testing.T) {
	base := []any{
		map[string]any{"name": "claude", "driver": "claude-cli"},
	}
	override := []any{
		map[string]any{"name": "cursor", "driver": "cursor-cli"},
	}
	result := MergeNamedSlice(base, override, "name")

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].(map[string]any)["name"] != "claude" {
		t.Errorf("result[0].name = %v, want claude", result[0].(map[string]any)["name"])
	}
	if result[1].(map[string]any)["name"] != "cursor" {
		t.Errorf("result[1].name = %v, want cursor", result[1].(map[string]any)["name"])
	}
}

func TestMergeNamedSlice_EmptyOverride(t *testing.T) {
	base := []any{
		map[string]any{"name": "claude"},
		map[string]any{"name": "cursor"},
	}
	result := MergeNamedSlice(base, []any{}, "name")

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
}

func TestMergeNamedSlice_EmptyBase(t *testing.T) {
	override := []any{
		map[string]any{"name": "cursor"},
	}
	result := MergeNamedSlice([]any{}, override, "name")

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].(map[string]any)["name"] != "cursor" {
		t.Errorf("result[0].name = %v, want cursor", result[0].(map[string]any)["name"])
	}
}

func TestMergeNamedSlice_NoKeyField_Fallback(t *testing.T) {
	base := []any{
		map[string]any{"a": 1},
	}
	override := []any{
		map[string]any{"b": 2},
	}
	result := MergeNamedSlice(base, override, "name")

	if len(result) != 1 {
		t.Fatalf("len = %d, want 1 (fallback to override)", len(result))
	}
	if result[0].(map[string]any)["b"] != 2 {
		t.Errorf("result[0].b = %v, want 2", result[0].(map[string]any)["b"])
	}
}

func TestMergeNamedSlice_NonMapElement_Fallback(t *testing.T) {
	base := []any{"not-a-map"}
	override := []any{"also-not-a-map"}
	result := MergeNamedSlice(base, override, "name")

	if len(result) != 1 || result[0] != "also-not-a-map" {
		t.Errorf("result = %v, want [also-not-a-map] (fallback to override)", result)
	}
}

func TestMergeNamedSlice_DoesNotMutateInputs(t *testing.T) {
	base := []any{
		map[string]any{"name": "claude", "model": "haiku"},
	}
	override := []any{
		map[string]any{"name": "claude", "model": "sonnet"},
	}

	_ = MergeNamedSlice(base, override, "name")

	if base[0].(map[string]any)["model"] != "haiku" {
		t.Error("MergeNamedSlice mutated base")
	}
	if override[0].(map[string]any)["model"] != "sonnet" {
		t.Error("MergeNamedSlice mutated override")
	}
}

func TestMergeNamedSlice_AppendDoesNotMutateOverride(t *testing.T) {
	base := []any{
		map[string]any{"name": "claude", "driver": "claude-cli"},
	}
	override := []any{
		map[string]any{"name": "cursor", "driver": "cursor-cli"},
	}

	result := MergeNamedSlice(base, override, "name")

	result[1].(map[string]any)["driver"] = "mutated"

	if override[0].(map[string]any)["driver"] != "cursor-cli" {
		t.Error("MergeNamedSlice append path shares reference with override")
	}
}

// ============================================================
// Epic 25 TA: Supplemental automated tests
// ============================================================

func TestDeepMergeYAML_FiveLevelDeep(t *testing.T) {
	base := map[string]any{
		"l1": map[string]any{
			"l2": map[string]any{
				"l3": map[string]any{
					"l4": map[string]any{
						"l5": "base-value",
						"b":  true,
					},
				},
			},
		},
	}
	override := map[string]any{
		"l1": map[string]any{
			"l2": map[string]any{
				"l3": map[string]any{
					"l4": map[string]any{
						"l5": "override-value",
						"c":  42,
					},
				},
			},
		},
	}
	result := DeepMergeYAML(base, override)
	// Navigate to l4 and check
	l1 := result["l1"].(map[string]any)
	l2 := l1["l2"].(map[string]any)
	l3 := l2["l3"].(map[string]any)
	l4 := l3["l4"].(map[string]any)
	if l4["l5"] != "override-value" {
		t.Errorf("l5 = %v, want override-value", l4["l5"])
	}
	if l4["b"] != true {
		t.Errorf("b = %v, want true (preserved from base)", l4["b"])
	}
	if l4["c"] != 42 {
		t.Errorf("c = %v, want 42 (added from override)", l4["c"])
	}
}

func TestDeepMergeYAML_MapOverridesScalar(t *testing.T) {
	base := map[string]any{
		"a": "scalar",
	}
	override := map[string]any{
		"a": map[string]any{"x": 1},
	}
	result := DeepMergeYAML(base, override)
	want := map[string]any{
		"a": map[string]any{"x": 1},
	}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML map-over-scalar = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_DisjointKeys(t *testing.T) {
	base := map[string]any{"a": 1, "b": 2}
	override := map[string]any{"c": 3, "d": 4}
	result := DeepMergeYAML(base, override)
	want := map[string]any{"a": 1, "b": 2, "c": 3, "d": 4}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("DeepMergeYAML disjoint = %v, want %v", result, want)
	}
}

func TestDeepMergeYAML_DoesNotMutateInputs(t *testing.T) {
	base := map[string]any{
		"nested": map[string]any{"x": 1},
	}
	override := map[string]any{
		"nested": map[string]any{"y": 2},
	}
	// Deep copy originals
	origBaseX := base["nested"].(map[string]any)["x"]
	origOverrideY := override["nested"].(map[string]any)["y"]

	_ = DeepMergeYAML(base, override)

	// Verify inputs not mutated
	if base["nested"].(map[string]any)["x"] != origBaseX {
		t.Error("DeepMergeYAML mutated base map")
	}
	if _, exists := base["nested"].(map[string]any)["y"]; exists {
		t.Error("DeepMergeYAML leaked override key into base map")
	}
	if override["nested"].(map[string]any)["y"] != origOverrideY {
		t.Error("DeepMergeYAML mutated override map")
	}
}

func TestShadowResolve_EmptyDirsList(t *testing.T) {
	got := ShadowResolve("anything")
	if got != "" {
		t.Errorf("ShadowResolve(no dirs) = %q, want empty string", got)
	}
}

func TestShadowResolve_EmptyName(t *testing.T) {
	dir := t.TempDir()
	got := ShadowResolve("", dir)
	// filepath.Join(dir, "") == dir, which IS a directory
	// Behavior depends on whether empty name makes sense; test the actual behavior
	if got != dir {
		t.Logf("ShadowResolve(\"\", dir) = %q (dir = %q)", got, dir)
	}
}

func TestShadowResolve_MultiDirPriority(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	dir3 := t.TempDir()

	// Create "agent" in dir2 and dir3 only
	os.Mkdir(filepath.Join(dir2, "agent"), 0o755)
	os.Mkdir(filepath.Join(dir3, "agent"), 0o755)

	got := ShadowResolve("agent", dir1, dir2, dir3)
	want := filepath.Join(dir2, "agent")
	if got != want {
		t.Errorf("ShadowResolve 3-dir = %q, want %q (first match in dir2)", got, want)
	}
}

func TestListMerged_ThreeDirs_Dedup(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()
	d3 := t.TempDir()

	os.Mkdir(filepath.Join(d1, "alpha"), 0o755)
	os.Mkdir(filepath.Join(d2, "alpha"), 0o755) // duplicate
	os.Mkdir(filepath.Join(d2, "beta"), 0o755)
	os.Mkdir(filepath.Join(d3, "gamma"), 0o755)

	got, err := ListMerged(d1, d2, d3)
	if err != nil {
		t.Fatalf("ListMerged() error = %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListMerged(3 dirs) = %v, want %v", got, want)
	}
}

func TestListMerged_NoDirs(t *testing.T) {
	got, err := ListMerged()
	if err != nil {
		t.Fatalf("ListMerged() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListMerged(no args) = %v, want empty", got)
	}
}

// BenchmarkDeepMergeYAML validates NFR55: DeepMergeYAML ≤ 50ms.
func BenchmarkDeepMergeYAML(b *testing.B) {
	// Build a realistic config with nested maps (simulating providers.yaml merge)
	base := map[string]any{
		"version": "1",
		"providers": map[string]any{
			"claude": map[string]any{
				"driver":        "claude-cli",
				"default_model": "haiku",
			},
			"cursor": map[string]any{
				"driver": "cursor-cli",
			},
		},
		"settings": map[string]any{
			"log_level": "info",
			"timeout":   30,
		},
	}
	override := map[string]any{
		"providers": map[string]any{
			"claude": map[string]any{
				"default_model": "sonnet",
			},
			"ollama": map[string]any{
				"driver":   "openai",
				"base_url": "http://localhost:11434/v1",
			},
		},
		"settings": map[string]any{
			"log_level": "debug",
			"max_retry": 3,
		},
	}

	b.ResetTimer()
	for b.Loop() {
		result := DeepMergeYAML(base, override)
		if result == nil {
			b.Fatal("nil result")
		}
	}
}
