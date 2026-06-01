package kernel

import (
	"os"
	"path/filepath"
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

// FindBaseDirByUUID locates the per-project data directory that contains
// the given process UUID by checking <dataDir>/projects/*/steps/<uuid>/.
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
	return ""
}
