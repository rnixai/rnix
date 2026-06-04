package kernel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/config"
)

func TestResolveStepBaseDir_NilProjectConfig_ReturnsGlobalDir(t *testing.T) {
	k := &KernelImpl{}
	k.dataDir = t.TempDir()

	proc := &Process{UUID: "test-uuid-1"}

	got := k.ResolveStepBaseDir(proc)
	want := config.GlobalDataDir(k.dataDir)
	if got != want {
		t.Errorf("ResolveStepBaseDir(nil ProjectConfig) = %q, want %q", got, want)
	}
}

func TestResolveStepBaseDir_EmptyProjectDir_ReturnsGlobalDir(t *testing.T) {
	k := &KernelImpl{}
	k.dataDir = t.TempDir()

	proc := &Process{
		UUID:          "test-uuid-2",
		ProjectConfig: &config.ProjectConfig{ProjectDir: ""},
	}

	got := k.ResolveStepBaseDir(proc)
	want := config.GlobalDataDir(k.dataDir)
	if got != want {
		t.Errorf("ResolveStepBaseDir(empty ProjectDir) = %q, want %q", got, want)
	}
}

func TestResolveStepBaseDir_ValidProjectConfig_ReturnsProjectDir(t *testing.T) {
	k := &KernelImpl{}
	k.dataDir = t.TempDir()

	projectPath := "/home/user/myproject"
	proc := &Process{
		UUID:          "test-uuid-3",
		ProjectConfig: &config.ProjectConfig{ProjectDir: projectPath},
	}

	got := k.ResolveStepBaseDir(proc)
	want := config.ProjectDataDir(k.dataDir, projectPath)
	if got != want {
		t.Errorf("ResolveStepBaseDir(valid ProjectConfig) = %q, want %q", got, want)
	}
}

func TestResolveStepBaseDir_EmptyDataDir_ReturnsEmpty(t *testing.T) {
	k := &KernelImpl{}

	proc := &Process{UUID: "test-uuid-4"}

	got := k.ResolveStepBaseDir(proc)
	if got != "" {
		t.Errorf("ResolveStepBaseDir(empty dataDir) = %q, want empty", got)
	}
}

func TestResolveStepBaseDir_NilProc_ReturnsEmpty(t *testing.T) {
	k := &KernelImpl{}
	k.dataDir = t.TempDir()

	got := k.ResolveStepBaseDir(nil)
	if got != "" {
		t.Errorf("ResolveStepBaseDir(nil proc) = %q, want empty", got)
	}
}

func TestFindBaseDirByUUID_GlobalDir(t *testing.T) {
	dataDir := t.TempDir()
	uuid := "test-uuid-global"

	globalStepsDir := filepath.Join(dataDir, "global", "steps", uuid)
	if err := os.MkdirAll(globalStepsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindBaseDirByUUID(dataDir, uuid)
	want := filepath.Join(dataDir, "global")
	if got != want {
		t.Errorf("FindBaseDirByUUID(global) = %q, want %q", got, want)
	}
}

func TestFindBaseDirByUUID_ProjectDirPreferred(t *testing.T) {
	dataDir := t.TempDir()
	uuid := "test-uuid-project"

	projectDir := filepath.Join(dataDir, "projects", "myproj-abc12345")
	projectStepsDir := filepath.Join(projectDir, "steps", uuid)
	if err := os.MkdirAll(projectStepsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	globalStepsDir := filepath.Join(dataDir, "global", "steps", uuid)
	if err := os.MkdirAll(globalStepsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindBaseDirByUUID(dataDir, uuid)
	if got != projectDir {
		t.Errorf("FindBaseDirByUUID(both exist) = %q, want project dir %q", got, projectDir)
	}
}

func TestAllBaseDirs_IncludesGlobalAndProjects(t *testing.T) {
	dataDir := t.TempDir()

	projDir := filepath.Join(dataDir, "projects", "myproj-abc12345")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	globalDir := filepath.Join(dataDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := AllBaseDirs(dataDir)
	if len(dirs) != 2 {
		t.Fatalf("AllBaseDirs = %v, want 2 entries (1 project + 1 global)", dirs)
	}
	if dirs[0] != projDir {
		t.Errorf("AllBaseDirs[0] = %q, want project dir %q", dirs[0], projDir)
	}
	if dirs[1] != globalDir {
		t.Errorf("AllBaseDirs[1] = %q, want global dir %q", dirs[1], globalDir)
	}
}

func TestAllBaseDirs_NoGlobalDir(t *testing.T) {
	dataDir := t.TempDir()

	projDir := filepath.Join(dataDir, "projects", "myproj-abc12345")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := AllBaseDirs(dataDir)
	if len(dirs) != 1 {
		t.Fatalf("AllBaseDirs (no global) = %v, want 1 entry", dirs)
	}
	if dirs[0] != projDir {
		t.Errorf("AllBaseDirs[0] = %q, want project dir %q", dirs[0], projDir)
	}
}
