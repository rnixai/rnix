package main

import (
	"os"
	"path/filepath"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/kernel"
)

// allStepsDirs returns the steps directories for every project under the
// global data directory: <dataDir>/projects/<project-id>/steps/. CLI commands
// that scan historical session data (record, replay) iterate these instead of
// a single cwd-relative .rnix/data/steps/ directory.
func allStepsDirs() []string {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil
	}
	var dirs []string
	for _, base := range kernel.AllBaseDirs(dataDir) {
		stepsDir := filepath.Join(base, "steps")
		if info, statErr := os.Stat(stepsDir); statErr == nil && info.IsDir() {
			dirs = append(dirs, stepsDir)
		}
	}
	return dirs
}
