package kernel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/config"
)

const testProjectDir = "/test/project"

// TestSetupDataDir creates a global data dir with a test project subdirectory
// and configures the kernel to use it. Returns (dataDir, projectBaseDir).
// projectBaseDir is the per-project data directory under dataDir/projects/.
func TestSetupDataDir(t *testing.T, k *KernelImpl) (string, string) {
	t.Helper()
	dataDir := t.TempDir()
	k.SetDataDir(dataDir)
	projBaseDir := config.ProjectDataDir(dataDir, testProjectDir)
	if err := os.MkdirAll(projBaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dataDir, projBaseDir
}

// TestSetProjectConfig sets a test ProjectConfig on the process so that
// ResolveStepBaseDir can resolve the per-project data directory.
func TestSetProjectConfig(proc *Process) {
	proc.ProjectConfig = &config.ProjectConfig{ProjectDir: testProjectDir}
}

// TestProjectConfig returns a ProjectConfig pointing at the test project dir,
// for use in SpawnOpts so spawned processes resolve to the test project's
// per-project data directory.
func TestProjectConfig() *config.ProjectConfig {
	return &config.ProjectConfig{ProjectDir: testProjectDir}
}

// TestProjectBaseDir returns the per-project base directory for the test
// project within the given dataDir.
func TestProjectBaseDir(dataDir string) string {
	return config.ProjectDataDir(dataDir, testProjectDir)
}

// TestStepsDir returns the steps directory for a UUID within the test project.
func TestStepsDir(dataDir, uuid string) string {
	return filepath.Join(TestProjectBaseDir(dataDir), "steps", uuid)
}
