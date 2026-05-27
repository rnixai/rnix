package config

// Skill scope resolution per the agentskills.io specification.
//
// ResolveSkillScopes returns the ordered list of root directories under which
// skills can be discovered. The order follows the agentskills.io standard:
//
//	project scope > user scope        (cross-scope priority)
//	native namespace > agents namespace (same-scope priority, rnix's choice)
//
// Resulting in four root paths (when all exist):
//
//	1. <projectDir>/.rnix/skills        (project / native)
//	2. <projectDir>/.agents/skills      (project / agents)
//	3. $XDG_CONFIG_HOME/rnix/skills     (user / native)
//	4. $HOME/.agents/skills             (user / agents)
//
// The SkillScope/SkillNamespace types defined here are intentionally separate
// from the package-level Scope (types.go) — Scope describes XDG-style config
// directory layers, whereas SkillScope describes agentskills.io scope layers.
// The user-native path coincides with ScopeGlobal but the semantics differ
// and future expansion (e.g. system-level scope) may diverge.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SkillScope is an agentskills.io scope layer (project vs user). It is
// independent from the package-level Scope type, which describes XDG-style
// configuration layering.
type SkillScope int

const (
	// SkillScopeProject is the project-local skill scope. Paths resolve under
	// <projectDir>/.rnix/skills/ or <projectDir>/.agents/skills/. Highest
	// priority in cross-scope resolution.
	SkillScopeProject SkillScope = iota

	// SkillScopeUser is the user-level skill scope. Paths resolve under
	// $XDG_CONFIG_HOME/rnix/skills/ (or ~/.config/rnix/skills/) and
	// ~/.agents/skills/.
	SkillScopeUser
)

// String returns the lowercase form used in CLI rendering and JSON output.
func (s SkillScope) String() string {
	switch s {
	case SkillScopeProject:
		return "project"
	case SkillScopeUser:
		return "user"
	default:
		return "unknown"
	}
}

// SkillNamespace is the directory namespace under which skills live. Rnix
// supports both its native namespace and the cross-tool agents namespace
// defined by agentskills.io.
type SkillNamespace int

const (
	// SkillNamespaceRnix is the rnix-native namespace (.rnix/skills/ or
	// ~/.config/rnix/skills/). Highest priority within a scope.
	SkillNamespaceRnix SkillNamespace = iota

	// SkillNamespaceAgents is the cross-tool agents namespace (.agents/skills/
	// or ~/.agents/skills/), shared with the agentskills.io ecosystem
	// (vercel-labs/skills, Cursor, OpenCode, etc.).
	SkillNamespaceAgents
)

// String returns the lowercase form used in CLI rendering and JSON output.
func (s SkillNamespace) String() string {
	switch s {
	case SkillNamespaceRnix:
		return "native"
	case SkillNamespaceAgents:
		return "agents"
	default:
		return "unknown"
	}
}

// ScopePath is a resolved root path together with its scope and namespace
// classification. All paths returned by ResolveSkillScopes are absolute and
// have been stat-verified to exist as directories at the time of the call.
type ScopePath struct {
	Path      string
	Scope     SkillScope
	Namespace SkillNamespace
}

// defaultMaxAncestorDepth bounds how many directory levels ResolveSkillScopes
// will climb above cwd when ancestor traversal is enabled.
const defaultMaxAncestorDepth = 6

// defaultMaxDirs bounds the total number of os.Stat calls performed during a
// single ResolveSkillScopes invocation (including ancestor traversal).
const defaultMaxDirs = 2000

// resolveOpts is the internal options struct for ResolveSkillScopes. It is
// populated via ResolveOption functional options.
type resolveOpts struct {
	ancestorTraversal bool
	maxAncestorDepth  int
	maxDirs           int
	skipDirs          []string
	warningWriter     io.Writer
}

// defaultResolveOpts returns a fresh options struct with default values per
// the AC5 functional-option defaults.
func defaultResolveOpts() *resolveOpts {
	return &resolveOpts{
		ancestorTraversal: false,
		maxAncestorDepth:  defaultMaxAncestorDepth,
		maxDirs:           defaultMaxDirs,
		skipDirs:          []string{".git", "node_modules"},
		warningWriter:     os.Stderr,
	}
}

// ResolveOption configures ResolveSkillScopes behavior.
type ResolveOption func(*resolveOpts)

// WithAncestorTraversal enables walking ancestor directories from cwd toward
// the git repository root (or filesystem boundary). Disabled by default to
// preserve v1 behavior — see Story 47.1 AC7 for rationale.
func WithAncestorTraversal(enabled bool) ResolveOption {
	return func(o *resolveOpts) { o.ancestorTraversal = enabled }
}

// WithMaxAncestorDepth overrides the maximum number of ancestor directories
// climbed during ancestor traversal. Default is 6 layers.
func WithMaxAncestorDepth(n int) ResolveOption {
	return func(o *resolveOpts) {
		if n > 0 {
			o.maxAncestorDepth = n
		}
	}
}

// WithMaxDirs overrides the cap on total stat calls performed during a single
// ResolveSkillScopes invocation. Default is 2000.
func WithMaxDirs(n int) ResolveOption {
	return func(o *resolveOpts) {
		if n > 0 {
			o.maxDirs = n
		}
	}
}

// WithSkipDirs appends directory base names to the ancestor traversal skip
// list (without checking for skill subdirectories within them).
// Default list is [".git", "node_modules"]; this option extends it.
func WithSkipDirs(names ...string) ResolveOption {
	return func(o *resolveOpts) {
		o.skipDirs = append(o.skipDirs, names...)
	}
}

// WithWarningWriter overrides where truncation warnings are written. Default
// is os.Stderr. Tests typically inject a *bytes.Buffer to capture output.
func WithWarningWriter(w io.Writer) ResolveOption {
	return func(o *resolveOpts) {
		if w != nil {
			o.warningWriter = w
		}
	}
}

// ResolveSkillScopes returns the ordered list of skill root directories per
// the agentskills.io specification. Paths that do not exist (or are not
// directories) are silently skipped. The caller should check len(result) to
// detect "no scopes found".
//
// Default behavior (no options): four stat calls — two on cwd
// (.rnix/skills, .agents/skills) and two on the home directory
// (~/.config/rnix/skills via GlobalDir, ~/.agents/skills).
//
// With WithAncestorTraversal(true): additionally climbs from cwd toward the
// containing git repository root (stopping at .git/ or the configured limits)
// checking each layer's .rnix/skills and .agents/skills directories.
func ResolveSkillScopes(cwd string, opts ...ResolveOption) []ScopePath {
	o := defaultResolveOpts()
	for _, opt := range opts {
		opt(o)
	}

	var (
		result        []ScopePath
		dirCounter    int
		truncated     bool
		truncWarnSent bool
	)

	// noteTruncation emits the warning at most once and flips the truncated
	// flag so callers stop further work. Returns true after truncation so
	// the caller can early-return.
	noteTruncation := func() {
		truncated = true
		if !truncWarnSent && o.warningWriter != nil {
			fmt.Fprintf(o.warningWriter,
				"[skillscope] ResolveSkillScopes truncated: scanned %d dirs, returning partial results. Set WithMaxDirs(...) to override.\n",
				dirCounter)
			truncWarnSent = true
		}
	}

	// checkAndAppend stats path; on success and IsDir(), appends a ScopePath.
	// Always increments dirCounter (each stat counts toward the cap).
	// Returns true = "keep going", false = "truncation hit, stop now".
	checkAndAppend := func(path string, scope SkillScope, namespace SkillNamespace) bool {
		if truncated {
			return false
		}
		dirCounter++
		if dirCounter > o.maxDirs {
			noteTruncation()
			return false
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return true // path absent or not a dir — not an error, just skip
		}
		result = append(result, ScopePath{
			Path:      path,
			Scope:     scope,
			Namespace: namespace,
		})
		return true
	}

	// --- Project scope (current cwd layer) ---
	// AC2 ordering: project/native, project/agents, user/native, user/agents.
	if cwd != "" {
		absCwd, err := filepath.Abs(cwd)
		if err != nil {
			if o.warningWriter != nil {
				fmt.Fprintf(o.warningWriter, "[skillscope] ResolveSkillScopes: could not resolve cwd %q: %v; project scope skipped.\n", cwd, err)
			}
		} else {
			if !checkAndAppend(filepath.Join(absCwd, ".rnix", "skills"), SkillScopeProject, SkillNamespaceRnix) {
				return result
			}
			if !checkAndAppend(filepath.Join(absCwd, ".agents", "skills"), SkillScopeProject, SkillNamespaceAgents) {
				return result
			}

			// --- Ancestor traversal (AC6) ---
			if o.ancestorTraversal {
				if !walkAncestors(absCwd, o, &result, &dirCounter, &truncated, noteTruncation) {
					return result
				}
			}
		}
	}

	// --- User scope ---
	if globalDir, err := GlobalDir(); err != nil {
		if o.warningWriter != nil {
			fmt.Fprintf(o.warningWriter, "[skillscope] ResolveSkillScopes: could not resolve user config dir: %v; user-native scope skipped.\n", err)
		}
	} else {
		if !checkAndAppend(filepath.Join(globalDir, "skills"), SkillScopeUser, SkillNamespaceRnix) {
			return result
		}
	}
	if home, err := userHomeDir(); err == nil {
		if !checkAndAppend(filepath.Join(home, ".agents", "skills"), SkillScopeUser, SkillNamespaceAgents) {
			return result
		}
	}

	return result
}

// userHomeDir returns the user's home directory, preferring $HOME when set so
// tests can override it via t.Setenv. Falls back to os.UserHomeDir() when
// $HOME is empty.
func userHomeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// walkAncestors climbs from the parent of cwd upward, checking each layer for
// .rnix/skills and .agents/skills directories. It stops on the first of:
//
//   - reaching maxAncestorDepth layers above cwd,
//   - reaching the .git/ root (the layer containing .git/ is checked and
//     then traversal stops),
//   - reaching $HOME or the filesystem root,
//   - hitting the dirCounter cap (truncation warning).
//
// Returns false when truncation has occurred and the outer function should
// stop adding entries (user scope paths are skipped in that case so the
// caller observes a consistent partial result).
func walkAncestors(absCwd string, o *resolveOpts, result *[]ScopePath, dirCounter *int, truncated *bool, noteTruncation func()) bool {
	skipSet := make(map[string]struct{}, len(o.skipDirs))
	for _, name := range o.skipDirs {
		skipSet[name] = struct{}{}
	}
	home := os.Getenv("HOME")

	// If cwd itself is $HOME, don't scan into its parents at all.
	if home != "" && absCwd == home {
		return true
	}

	dir := absCwd
	for depth := 1; depth <= o.maxAncestorDepth; depth++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			return true // filesystem root
		}
		dir = parent

		// Stop at $HOME boundary — ancestor scan must not leak into the user's
		// home directory and accidentally re-enter ~/.agents/skills via the
		// project path.
		if home != "" && dir == home {
			return true
		}

		// Skip blacklisted directory levels (.git, node_modules) — do not
		// check skill paths within them. Continue climbing.
		base := filepath.Base(dir)
		if _, skip := skipSet[base]; skip {
			continue
		}

		// Check .rnix/skills first (native > agents).
		if *truncated {
			return false
		}
		*dirCounter++
		if *dirCounter > o.maxDirs {
			noteTruncation()
			return false
		}
		rnixPath := filepath.Join(dir, ".rnix", "skills")
		if info, err := os.Stat(rnixPath); err == nil && info.IsDir() {
			*result = append(*result, ScopePath{
				Path:      rnixPath,
				Scope:     SkillScopeProject,
				Namespace: SkillNamespaceRnix,
			})
		}

		// Check .agents/skills.
		if *truncated {
			return false
		}
		*dirCounter++
		if *dirCounter > o.maxDirs {
			noteTruncation()
			return false
		}
		agentsPath := filepath.Join(dir, ".agents", "skills")
		if info, err := os.Stat(agentsPath); err == nil && info.IsDir() {
			*result = append(*result, ScopePath{
				Path:      agentsPath,
				Scope:     SkillScopeProject,
				Namespace: SkillNamespaceAgents,
			})
		}

		// Detect repository root — any .git entry (directory or file, covering
		// worktrees and submodules where .git is a file) marks the boundary.
		// Stop after processing this layer's skill paths.
		if *truncated {
			return false
		}
		*dirCounter++
		if *dirCounter > o.maxDirs {
			noteTruncation()
			return false
		}
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return true
		}
	}

	return true
}
