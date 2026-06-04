package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// DataDir returns the global data directory for rnix session data.
// Resolution order: RNIX_DATA_DIR > XDG_DATA_HOME/rnix > ~/.local/share/rnix.
func DataDir() (string, error) {
	if v := os.Getenv("RNIX_DATA_DIR"); v != "" {
		return v, nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "rnix"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "rnix"), nil
}

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]`)
var multiDash = regexp.MustCompile(`-{2,}`)

// SanitizeBasename converts an arbitrary directory name into a safe filesystem
// component. Only ASCII alphanumeric, dot, underscore, and hyphen survive;
// everything else (spaces, CJK, symbols) is replaced with a hyphen.
func SanitizeBasename(name string) string {
	s := unsafeChars.ReplaceAllString(name, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if len(s) > 50 {
		s = s[:50]
		s = strings.TrimRight(s, "-.")
	}
	if s == "" {
		return "project"
	}
	return s
}

// ProjectDataID computes a deterministic, human-readable directory name for a
// project's data storage: <sanitized-basename>-<hash8>.
func ProjectDataID(projectPath string) string {
	base := SanitizeBasename(filepath.Base(projectPath))
	hash := sha256.Sum256([]byte(projectPath))
	return base + "-" + hex.EncodeToString(hash[:4])
}

// GlobalDataDir returns the fallback data directory for processes without a
// project context. Used when ProjectConfig is nil or ProjectDir is empty.
func GlobalDataDir(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "global")
}

// ProjectDataDir returns the per-project data directory under dataDir.
// Returns "" when projectPath is empty (no project = no data storage).
func ProjectDataDir(dataDir, projectPath string) string {
	if dataDir == "" || projectPath == "" {
		return ""
	}
	return filepath.Join(dataDir, "projects", ProjectDataID(projectPath))
}
