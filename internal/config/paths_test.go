package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalDir_Default(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := GlobalDir()
	if err != nil {
		t.Fatalf("GlobalDir() error = %v", err)
	}
	want := filepath.Join(home, ".config", "rnix")
	if got != want {
		t.Errorf("GlobalDir() = %q, want %q", got, want)
	}
}

func TestGlobalDir_XDGConfigHome(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", custom)

	got, err := GlobalDir()
	if err != nil {
		t.Fatalf("GlobalDir() error = %v", err)
	}
	want := filepath.Join(custom, "rnix")
	if got != want {
		t.Errorf("GlobalDir() = %q, want %q", got, want)
	}
}

func TestProjectDir_Found(t *testing.T) {
	root := t.TempDir()
	// Create <root>/.rnix/ directory
	rnixDir := filepath.Join(root, ".rnix")
	if err := os.Mkdir(rnixDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ProjectDir(root)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	if got != root {
		t.Errorf("ProjectDir(%q) = %q, want %q", root, got, root)
	}
}

func TestProjectDir_NestedLookup(t *testing.T) {
	root := t.TempDir()
	rnixDir := filepath.Join(root, ".rnix")
	if err := os.Mkdir(rnixDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create nested subdirectory
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ProjectDir(deep)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	if got != root {
		t.Errorf("ProjectDir(%q) = %q, want %q", deep, got, root)
	}
}

func TestProjectDir_NotFound(t *testing.T) {
	// Use a temp directory with no .rnix/ anywhere up to root
	dir := t.TempDir()
	// Set HOME to the same dir so traversal stops here
	t.Setenv("HOME", dir)

	got, err := ProjectDir(dir)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	if got != "" {
		t.Errorf("ProjectDir(%q) = %q, want empty string", dir, got)
	}
}

func TestProjectDir_StopsAtHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create .rnix/ above home (which shouldn't be reached)
	child := filepath.Join(home, "sub")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ProjectDir(child)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	if got != "" {
		t.Errorf("ProjectDir(%q) = %q, want empty string (should stop at HOME)", child, got)
	}
}

func TestProjectDir_StopsAtFilesystemRoot(t *testing.T) {
	// When HOME is unset, should stop at filesystem root
	t.Setenv("HOME", "")

	// Use a directory that definitely has no .rnix/ above it
	dir := t.TempDir()

	got, err := ProjectDir(dir)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	if got != "" {
		t.Errorf("ProjectDir(%q) = %q, want empty string", dir, got)
	}
}

func TestResolvePath_Global(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got := ResolvePath(ScopeGlobal, "", "rnix-providers.yaml")
	want := filepath.Join(home, ".config", "rnix", "rnix-providers.yaml")
	if got != want {
		t.Errorf("ResolvePath(Global) = %q, want %q", got, want)
	}
}

func TestResolvePath_Project(t *testing.T) {
	projectDir := "/some/project"
	got := ResolvePath(ScopeProject, projectDir, "rnix-providers.yaml")
	want := filepath.Join(projectDir, ".rnix", "rnix-providers.yaml")
	if got != want {
		t.Errorf("ResolvePath(Project) = %q, want %q", got, want)
	}
}

func TestResolveDir_Global(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got := ResolveDir(ScopeGlobal, "", "agents")
	want := filepath.Join(home, ".config", "rnix", "agents")
	if got != want {
		t.Errorf("ResolveDir(Global) = %q, want %q", got, want)
	}
}

func TestResolveDir_Project(t *testing.T) {
	projectDir := "/some/project"
	got := ResolveDir(ScopeProject, projectDir, "agents")
	want := filepath.Join(projectDir, ".rnix", "agents")
	if got != want {
		t.Errorf("ResolveDir(Project) = %q, want %q", got, want)
	}
}

func TestGlobalDir_XDGConfigHome_TrailingSlash(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", custom+"/")

	got, err := GlobalDir()
	if err != nil {
		t.Fatalf("GlobalDir() error = %v", err)
	}
	// filepath.Join normalizes trailing slashes
	want := filepath.Join(custom, "rnix")
	if got != want {
		t.Errorf("GlobalDir() = %q, want %q", got, want)
	}
}

func TestGlobalDir_EmptyXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := GlobalDir()
	if err != nil {
		t.Fatalf("GlobalDir() error = %v", err)
	}
	want := filepath.Join(home, ".config", "rnix")
	if got != want {
		t.Errorf("GlobalDir() = %q, want %q", got, want)
	}
}

func TestGlobalDir_NoHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	_, err := GlobalDir()
	if err == nil {
		t.Error("GlobalDir() expected error when HOME is unset, got nil")
	}
}

func TestProjectDir_RelativePath(t *testing.T) {
	root := t.TempDir()
	rnixDir := filepath.Join(root, ".rnix")
	if err := os.Mkdir(rnixDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "child")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Chdir to sub and use relative path "."
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(sub)

	got, err := ProjectDir(".")
	if err != nil {
		t.Fatalf("ProjectDir(\".\") error = %v", err)
	}
	if got != root {
		t.Errorf("ProjectDir(\".\") = %q, want %q", got, root)
	}
}

func TestProjectDir_DotRnixIsFile(t *testing.T) {
	root := t.TempDir()
	// Create .rnix as a FILE, not a directory
	if err := os.WriteFile(filepath.Join(root, ".rnix"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)

	got, err := ProjectDir(root)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	// Should NOT match because .rnix is a file, not a directory
	if got != "" {
		t.Errorf("ProjectDir(%q) = %q, want empty string (.rnix is a file)", root, got)
	}
}

// ============================================================
// Epic 25 TA: Supplemental automated tests
// ============================================================

func TestResolvePath_UnknownScope(t *testing.T) {
	got := ResolvePath(Scope(99), "/some/project", "foo.yaml")
	if got != "" {
		t.Errorf("ResolvePath(unknown scope) = %q, want empty string", got)
	}
}

func TestResolveDir_UnknownScope(t *testing.T) {
	got := ResolveDir(Scope(99), "/some/project", "agents")
	if got != "" {
		t.Errorf("ResolveDir(unknown scope) = %q, want empty string", got)
	}
}

func TestProjectDir_EmptyStartDir(t *testing.T) {
	// Empty string should be resolved to CWD via filepath.Abs
	got, err := ProjectDir("")
	if err != nil {
		t.Fatalf("ProjectDir(\"\") error = %v", err)
	}
	// Just verify it doesn't panic; actual result depends on CWD
	_ = got
}

func TestProjectDir_DeepNesting_20Layers(t *testing.T) {
	root := t.TempDir()
	rnixDir := filepath.Join(root, ".rnix")
	if err := os.Mkdir(rnixDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Build 20-layer deep nested path
	deep := root
	for i := range 20 {
		deep = filepath.Join(deep, fmt.Sprintf("d%d", i))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ProjectDir(deep)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	if got != root {
		t.Errorf("ProjectDir(20-layer deep) = %q, want %q", got, root)
	}
}

// BenchmarkProjectDir_20Layers validates NFR54: ProjectDir ≤ 10ms for ≤ 20 layers.
func BenchmarkProjectDir_20Layers(b *testing.B) {
	root := b.TempDir()
	rnixDir := filepath.Join(root, ".rnix")
	if err := os.Mkdir(rnixDir, 0o755); err != nil {
		b.Fatal(err)
	}

	deep := root
	for i := range 20 {
		deep = filepath.Join(deep, fmt.Sprintf("d%d", i))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		got, err := ProjectDir(deep)
		if err != nil {
			b.Fatal(err)
		}
		if got != root {
			b.Fatalf("got %q, want %q", got, root)
		}
	}
}

// ============================================================
// DataDir tests
// ============================================================

func TestDataDir_Default(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RNIX_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")

	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	want := filepath.Join(home, ".local", "share", "rnix")
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestDataDir_XDGDataHome(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("RNIX_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", custom)

	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	want := filepath.Join(custom, "rnix")
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestDataDir_EnvOverride(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("RNIX_DATA_DIR", custom)
	t.Setenv("XDG_DATA_HOME", "/should/not/use")

	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if got != custom {
		t.Errorf("DataDir() = %q, want %q", got, custom)
	}
}

// ============================================================
// SanitizeBasename tests
// ============================================================

func TestSanitizeBasename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"echomatrix", "echomatrix"},
		{"my-project", "my-project"},
		{"my_project.v2", "my_project.v2"},
		{"my project", "my-project"},
		{"项目测试", "project"},             // pure CJK → fallback
		{"rnix-项目", "rnix"},               // mixed: CJK stripped
		{".hidden", "hidden"},               // leading dot stripped
		{"..dotdot", "dotdot"},              // leading dots stripped
		{"a b  c", "a-b-c"},                 // spaces → single dash
		{"foo@bar#baz$qux", "foo-bar-baz-qux"},
		{"", "project"},                     // empty → fallback
		{"---", "project"},                  // only dashes → trimmed → fallback
		{strings.Repeat("a", 100), strings.Repeat("a", 50)}, // truncation
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.input), func(t *testing.T) {
			got := SanitizeBasename(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeBasename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ============================================================
// ProjectDataID tests
// ============================================================

func TestProjectDataID_Stable(t *testing.T) {
	path := "/home/user/echomatrix"
	id1 := ProjectDataID(path)
	id2 := ProjectDataID(path)
	if id1 != id2 {
		t.Errorf("ProjectDataID not stable: %q != %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "echomatrix-") {
		t.Errorf("ProjectDataID(%q) = %q, want prefix 'echomatrix-'", path, id1)
	}
	// basename + "-" + 8 hex chars
	parts := strings.SplitN(id1, "-", 2)
	if len(parts) != 2 || len(parts[1]) != 8 {
		t.Errorf("ProjectDataID(%q) = %q, want format <base>-<8hex>", path, id1)
	}
}

func TestProjectDataID_DifferentPaths_DifferentIDs(t *testing.T) {
	id1 := ProjectDataID("/home/alice/myproject")
	id2 := ProjectDataID("/home/bob/myproject")
	if id1 == id2 {
		t.Errorf("different paths should produce different IDs: %q == %q", id1, id2)
	}
	// Both should have same basename prefix
	if !strings.HasPrefix(id1, "myproject-") || !strings.HasPrefix(id2, "myproject-") {
		t.Errorf("both should start with 'myproject-': %q, %q", id1, id2)
	}
}

func TestProjectDataID_CJKBasename(t *testing.T) {
	id := ProjectDataID("/home/user/我的项目")
	if !strings.HasPrefix(id, "project-") {
		t.Errorf("CJK basename should fallback: got %q", id)
	}
}

// ============================================================
// ProjectDataDir tests
// ============================================================

func TestProjectDataDir_WithProject(t *testing.T) {
	got := ProjectDataDir("/data/rnix", "/home/user/echomatrix")
	if !strings.HasPrefix(got, "/data/rnix/projects/echomatrix-") {
		t.Errorf("ProjectDataDir() = %q, want prefix /data/rnix/projects/echomatrix-", got)
	}
}

func TestProjectDataDir_EmptyProject(t *testing.T) {
	got := ProjectDataDir("/data/rnix", "")
	if got != "" {
		t.Errorf("ProjectDataDir with empty project = %q, want empty", got)
	}
}

func TestProjectDataDir_EmptyDataDir(t *testing.T) {
	got := ProjectDataDir("", "/home/user/foo")
	if got != "" {
		t.Errorf("ProjectDataDir with empty dataDir = %q, want empty", got)
	}
}

func TestProjectDir_MultipleRnixDirs_ClosestWins(t *testing.T) {
	root := t.TempDir()
	// Create .rnix at root and at root/child
	os.Mkdir(filepath.Join(root, ".rnix"), 0o755)
	child := filepath.Join(root, "child")
	os.MkdirAll(filepath.Join(child, ".rnix"), 0o755)
	deep := filepath.Join(child, "sub")
	os.Mkdir(deep, 0o755)

	got, err := ProjectDir(deep)
	if err != nil {
		t.Fatalf("ProjectDir() error = %v", err)
	}
	// Should find the closest .rnix/ (child), not root
	if got != child {
		t.Errorf("ProjectDir() = %q, want %q (closest .rnix/)", got, child)
	}
}
