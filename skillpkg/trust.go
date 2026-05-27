package skillpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/rnixai/rnix/internal/config"
)

// trustChecksPerformed counts how many times isProjectTrusted has been
// invoked. Tests use this to verify the AC7 ordering contract in
// Installer.ListAll (the errNoScopes early-return must happen BEFORE any
// trust check). Production code never reads this counter; the atomic.Int64
// is paid one increment per stat call, negligible vs the syscall.
var trustChecksPerformed atomic.Int64

// TrustChecksPerformedForTest returns the running tally of isProjectTrusted
// invocations. Test-only helper.
func TrustChecksPerformedForTest() int64 {
	return trustChecksPerformed.Load()
}

// ResetTrustChecksForTest zeroes the trust-check counter. Test-only helper.
func ResetTrustChecksForTest() {
	trustChecksPerformed.Store(0)
}

// TrustMarkerRelPath is the project-relative path of the trust marker. Used
// by CheckProjectTrust and surfaced in the user-facing recommendation so the
// path is sourced from one place (Story 47.4 review patch P4).
const TrustMarkerRelPath = ".rnix/state/trusted"

const (
	trustReason         = "untrusted repo can inject agent instructions"
	trustPolicy         = "warn-only (not blocking)"
	trustRecommendation = "To dismiss this warning, run: touch %s/" + TrustMarkerRelPath + " " +
		"(a future 'rnix trust <dir>' command is planned)"
)

// CheckProjectTrust inspects the supplied scope slice for project-scope
// ScopePath entries whose parent project directory does not carry a regular
// trust marker file at <projectDir>/.rnix/state/trusted, and returns a
// deduplicated TrustWarning per project directory.
//
// Story 47.4 contract:
//   - AC1 — each untrusted projectDir yields one TrustWarning with all five
//     fields populated (ProjectDir / SkillsRootPaths / Reason / Policy /
//     Recommendation).
//   - AC2 — marker existence is checked via os.Lstat with a regular-file
//     guard; content is never read. Symlinks are NOT trusted (review patch
//     DN1: a malicious repo could otherwise point the marker at any common
//     filesystem entry to bypass the trust check).
//   - AC4 — user scopes never trigger a warning; empty scopes yield nil.
//   - AC5 — multiple project-scope ScopePaths sharing a projectDir merge into
//     a single warning whose SkillsRootPaths preserves the input priority
//     order (native before agents). projectDir is normalized with
//     filepath.Clean so equivalent path spellings dedupe correctly (review
//     patch P7).
//
// TOCTOU note (review decision DN2): runSkillInstall / runSkillUpdate call
// CheckProjectTrust once for stderr advisories, and Installer.ListAll calls
// it once more internally for diag.Trust. Both calls happen within the same
// CLI invocation milliseconds apart; the race window for an external writer
// flipping the marker is negligible in practice. Consumers that need
// strict consistency should call CheckProjectTrust exactly once and pass
// the result to all sinks.
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
		projectDir := projectDirFromScopePath(sp)
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
		// Story 47.4 review patch P11: dedup identical ScopePath.Path entries
		// so SkillsRootPaths never double-counts under unusual upstream
		// configurations.
		if !slices.Contains(entry.paths, sp.Path) {
			entry.paths = append(entry.paths, sp.Path)
		}
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

// projectDirFromScopePath strips the trailing `/.rnix/skills` (rnix
// namespace) or `/.agents/skills` (agents namespace) two-level suffix from a
// project-scope ScopePath. The expected suffix is selected by sp.Namespace
// so a malformed path that happens to be two levels deep does not silently
// resolve to a random parent directory (review patch P5).
//
// Returns the empty string when:
//   - the path is empty, "." or the filesystem root (review patch P14);
//   - the actual suffix does not match the namespace's expected suffix.
//
// The returned projectDir is normalized via filepath.Clean so callers can
// use it as a stable map key (review patch P7).
func projectDirFromScopePath(sp config.ScopePath) string {
	if sp.Path == "" {
		return ""
	}
	cleaned := filepath.Clean(sp.Path)
	if cleaned == "" || cleaned == "." || cleaned == string(filepath.Separator) {
		return ""
	}

	var expectedSuffix string
	switch sp.Namespace {
	case config.SkillNamespaceRnix:
		expectedSuffix = filepath.Join(".rnix", "skills")
	case config.SkillNamespaceAgents:
		expectedSuffix = filepath.Join(".agents", "skills")
	default:
		return ""
	}
	if !strings.HasSuffix(cleaned, string(filepath.Separator)+expectedSuffix) {
		return ""
	}

	projectDir := strings.TrimSuffix(cleaned, string(filepath.Separator)+expectedSuffix)
	if projectDir == "" || projectDir == "." || projectDir == string(filepath.Separator) {
		return ""
	}
	return projectDir
}

// isProjectTrusted checks for a *regular file* at
// <projectDir>/.rnix/state/trusted. Uses os.Lstat with a regular-file guard
// so symbolic links are NEVER trusted — a malicious repo could otherwise
// commit `.rnix/state/trusted -> /etc/hostname` (or any common file on the
// user's machine) to bypass the trust check entirely. Story 47.4 review
// decision DN1.
//
// EACCES on the marker itself is treated as missing (untrusted) by design:
// if the trust marker is unreadable, the user can't have positively asserted
// trust. Stricter EACCES semantics is deferred (Story 47.4 review defer W4).
func isProjectTrusted(projectDir string) bool {
	trustChecksPerformed.Add(1)
	marker := filepath.Join(projectDir, ".rnix", "state", "trusted")
	info, err := os.Lstat(marker)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
