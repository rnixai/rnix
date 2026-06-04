package kernel

import (
	"os"
	"path/filepath"

	"github.com/rnixai/rnix/internal/config"
)

// AllProjectBaseDirs returns every per-project data directory under
// <dataDir>/projects/. Used by scanning functions that need to iterate
// all stored processes across projects.
func AllProjectBaseDirs(dataDir string) []string {
	if dataDir == "" {
		return nil
	}
	projectsDir := filepath.Join(dataDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(projectsDir, e.Name()))
		}
	}
	return dirs
}

// AllBaseDirs returns every data directory that may contain process step
// data: all per-project directories under <dataDir>/projects/ plus the
// global fallback directory <dataDir>/global/ (if it exists on disk).
func AllBaseDirs(dataDir string) []string {
	dirs := AllProjectBaseDirs(dataDir)
	if globalDir := config.GlobalDataDir(dataDir); globalDir != "" {
		if info, err := os.Stat(globalDir); err == nil && info.IsDir() {
			dirs = append(dirs, globalDir)
		}
	}
	return dirs
}

// FindBaseDirByUUID locates the data directory that contains the given
// process UUID by checking <dataDir>/projects/*/steps/<uuid>/ and the
// global fallback directory <dataDir>/global/steps/<uuid>/.
// Returns "" if no matching directory is found.
func FindBaseDirByUUID(dataDir, uuid string) string {
	if dataDir == "" || uuid == "" {
		return ""
	}
	for _, base := range AllProjectBaseDirs(dataDir) {
		if _, err := os.Stat(filepath.Join(base, "steps", uuid)); err == nil {
			return base
		}
	}
	if globalDir := config.GlobalDataDir(dataDir); globalDir != "" {
		if _, err := os.Stat(filepath.Join(globalDir, "steps", uuid)); err == nil {
			return globalDir
		}
	}
	return ""
}
