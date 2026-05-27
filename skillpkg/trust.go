package skillpkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rnixai/rnix/internal/config"
)

const (
	trustReason         = "untrusted repo can inject agent instructions"
	trustPolicy         = "warn-only (not blocking)"
	trustRecommendation = "To dismiss this warning, run: touch %s/.rnix/state/trusted " +
		"(a future 'rnix trust <dir>' command is planned)"
)

// CheckProjectTrust inspects the supplied scope slice for project-scope
// ScopePath entries whose parent project directory does not carry a trust
// marker at <projectDir>/.rnix/state/trusted, and returns a deduplicated
// TrustWarning per project directory.
//
// Story 47.4 contract:
//   - AC1 — each untrusted projectDir yields one TrustWarning with all five
//     fields populated (ProjectDir / SkillsRootPaths / Reason / Policy /
//     Recommendation).
//   - AC2 — marker existence is checked via os.Stat; content is ignored.
//     symlinks are followed (broken symlinks → treated as missing).
//   - AC4 — user scopes never trigger a warning; empty scopes yield nil.
//   - AC5 — multiple project-scope ScopePaths sharing a projectDir merge into
//     a single warning whose SkillsRootPaths preserves the input priority
//     order (native before agents).
//
// Output order: deterministic, by first appearance of each projectDir in the
// input scope slice.
func CheckProjectTrust(scopes []config.ScopePath) []TrustWarning {
	if len(scopes) == 0 {
		return nil
	}

	type pending struct {
		projectDir string
		paths      []string
		trusted    bool
	}

	order := make([]string, 0, len(scopes))
	byDir := make(map[string]*pending)

	for _, sp := range scopes {
		if sp.Scope != config.SkillScopeProject {
			continue
		}
		projectDir := projectDirFromScopePath(sp.Path)
		if projectDir == "" {
			continue
		}

		entry, ok := byDir[projectDir]
		if !ok {
			entry = &pending{
				projectDir: projectDir,
				trusted:    isProjectTrusted(projectDir),
			}
			byDir[projectDir] = entry
			order = append(order, projectDir)
		}
		entry.paths = append(entry.paths, sp.Path)
	}

	var warnings []TrustWarning
	for _, dir := range order {
		entry := byDir[dir]
		if entry.trusted {
			continue
		}
		warnings = append(warnings, TrustWarning{
			ProjectDir:      entry.projectDir,
			SkillsRootPaths: entry.paths,
			Reason:          trustReason,
			Policy:          trustPolicy,
			Recommendation:  fmt.Sprintf(trustRecommendation, entry.projectDir),
		})
	}
	return warnings
}

// projectDirFromScopePath strips the trailing `/.rnix/skills` or
// `/.agents/skills` two-level suffix from a project-scope path. Returns the
// empty string when the path is not at least two levels deep (safety net).
func projectDirFromScopePath(p string) string {
	if p == "" {
		return ""
	}
	parent := filepath.Dir(p)
	if parent == "" || parent == "." || parent == string(filepath.Separator) {
		return ""
	}
	projectDir := filepath.Dir(parent)
	if projectDir == "" || projectDir == "." {
		return ""
	}
	return projectDir
}

// isProjectTrusted checks for the existence (not content) of
// <projectDir>/.rnix/state/trusted. Uses os.Stat which follows symlinks; a
// broken symlink returns an error and is treated as missing (untrusted).
func isProjectTrusted(projectDir string) bool {
	marker := filepath.Join(projectDir, ".rnix", "state", "trusted")
	_, err := os.Stat(marker)
	return err == nil
}
