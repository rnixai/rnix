package skillpkg

import "github.com/rnixai/rnix/internal/config"

// CheckProjectTrust inspects the supplied scope slice for project-scope
// ScopePath entries whose parent project directory does not carry a trust
// marker at <projectDir>/.rnix/state/trusted, and returns a deduplicated
// TrustWarning per project directory.
//
// Story 47.4 ATDD RED PHASE — this is a STUB.
//
//	Current behaviour: always returns nil so callers compile cleanly while
//	the red-phase tests pin the production contract:
//	  * AC1  — untrusted project scope emits one warning per projectDir
//	  * AC2  — trust marker existence (not content) silences the warning
//	  * AC4  — user-only scopes / empty scopes produce zero warnings
//	  * AC5  — same projectDir across .rnix/skills + .agents/skills merges
//	           into a single warning whose SkillsRootPaths preserves scope
//	           priority order
//	  * AC6  — TrustWarning struct is defined in types.go alongside the
//	           existing diagnostic types so callers import a single package
//
// Dev-story phase replaces this body with the real implementation outlined
// in [Story 47.4 §Tasks/Subtasks §Task 1.4].
func CheckProjectTrust(scopes []config.ScopePath) []TrustWarning {
	_ = scopes
	return nil
}
