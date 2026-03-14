package config

import (
	"os"
	"path/filepath"
)

// GlobalDir returns the global configuration directory path.
// It uses $XDG_CONFIG_HOME/rnix/ if set, otherwise falls back to
// ~/.config/rnix/.
func GlobalDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "rnix"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "rnix"), nil
}

// ProjectDir walks up from startDir looking for a .rnix/ subdirectory.
// If found, it returns the parent directory of .rnix/ (the project root).
// If not found (up to $HOME or filesystem root), it returns ("", nil).
func ProjectDir(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	home := os.Getenv("HOME")

	for {
		candidate := filepath.Join(dir, ".rnix")
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return dir, nil
		}

		// Stop at $HOME boundary
		if home != "" && dir == home {
			return "", nil
		}

		parent := filepath.Dir(dir)
		// Stop at filesystem root
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// ResolvePath returns the full path to a configuration file under the given scope.
// For ScopeGlobal, it resolves under the global config directory.
// For ScopeProject, it resolves under <projectDir>/.rnix/.
func ResolvePath(scope Scope, projectDir, filename string) string {
	switch scope {
	case ScopeGlobal:
		globalDir, err := GlobalDir()
		if err != nil {
			return ""
		}
		return filepath.Join(globalDir, filename)
	case ScopeProject:
		return filepath.Join(projectDir, ".rnix", filename)
	default:
		return ""
	}
}

// ResolveDir returns the full path to a configuration subdirectory under the given scope.
// For ScopeGlobal, it resolves under the global config directory.
// For ScopeProject, it resolves under <projectDir>/.rnix/.
func ResolveDir(scope Scope, projectDir, dirname string) string {
	return ResolvePath(scope, projectDir, dirname)
}
