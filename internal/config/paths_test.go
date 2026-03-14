package config

import (
	"os"
	"path/filepath"
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
