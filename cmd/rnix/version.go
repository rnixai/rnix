package main

import (
	"runtime/debug"
	"strings"
)

// Story 45.1 — Cross-repo version observability.
//
// Three-source version fallback priority:
//   1. ldflags injection (make build / make install / make release):
//      ldVersion / ldGitCommit / ldBuildDate are populated via
//      `-X main.ldVersion=... -X main.ldGitCommit=... -X main.ldBuildDate=...`.
//   2. runtime/debug.ReadBuildInfo (go install ...@latest / go install ...@<sha>):
//      BuildInfo.Main.Version + Settings["vcs.revision"|"vcs.time"|"vcs.modified"].
//   3. Hardcoded floor (go install ./cmd/rnix without VCS info, or older toolchain):
//      "0.0.0" / "unknown" / "" — explicitly NOT the legacy lying "0.1.0".
//
// The legacy var block (version / gitCommit / buildDate in main.go:51-55) is
// retained as runtime placeholders but is no longer fed by the Makefile; it
// only serves as the hardcoded floor source.
var (
	// ldVersion / ldGitCommit / ldBuildDate are injected by the Makefile via
	// -ldflags "-X main.ldVersion=..." etc. The "ld" prefix prevents the
	// "no ldflags injection" detection from confusing them with the legacy
	// placeholder vars.
	ldVersion   = ""
	ldGitCommit = ""
	ldBuildDate = ""
)

// readBuildInfoFn is an indirection over runtime/debug.ReadBuildInfo so tests
// can inject a synthetic *debug.BuildInfo without depending on the test
// binary's actual VCS settings.
var readBuildInfoFn = debug.ReadBuildInfo

// buildVersionInfo resolves the daemon's version provenance through the
// three-source fallback chain. The dirty flag is true when the commit contains
// the "+dirty" suffix (ldflags path, Makefile-composed) or when BuildInfo's
// vcs.modified setting reports "true" (BuildInfo path).
func buildVersionInfo() (version, commit, buildDate string, dirty bool) {
	// Priority 1: ldflags injection. We treat ldGitCommit != "" as the
	// authoritative "ldflags path active" signal — Makefile always injects
	// all three together, and an empty commit defeats the whole purpose of
	// the daemon-version verification flow Story 45.1 is solving.
	if ldGitCommit != "" {
		return ldVersion, ldGitCommit, ldBuildDate, strings.HasSuffix(ldGitCommit, "+dirty")
	}

	// Priority 2: runtime/debug.ReadBuildInfo for go install paths.
	if info, ok := readBuildInfoFn(); ok && info != nil {
		var bv, bc, bd string
		var bdirty bool

		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			bv = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				bc = shortSHA(s.Value)
			case "vcs.time":
				bd = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					bdirty = true
				}
			}
		}
		if bdirty && bc != "" {
			bc += "+dirty"
		}

		if bv != "" || bc != "" || bd != "" {
			// Invariant: v must never leave buildVersionInfo as "".
			// When Main.Version is "(devel)" or empty but vcs.* settings
			// are present, fill bv with the floor so all downstream
			// renderers (runVersion / runDaemonStatus / startup banner)
			// have a SemVer-shape value to print.
			if bv == "" {
				bv = "0.0.0"
			}
			return bv, bc, bd, bdirty
		}
	}

	// Priority 3: hardcoded floor. Story 45.1 §"核心 bug 诊断" — explicitly
	// return "0.0.0" rather than the legacy "0.1.0" which lies about the
	// actual codebase version.
	return "0.0.0", "unknown", "", false
}

// shortSHA truncates a full SHA to the conventional 7-character prefix used by
// `git rev-parse --short HEAD`. Values shorter than 7 chars are returned as-is.
func shortSHA(sha string) string {
	const shortLen = 7
	if len(sha) <= shortLen {
		return sha
	}
	return sha[:shortLen]
}

// versionString returns the resolved daemon version string. Retained as a
// public helper for callers that only need the version portion
// (cmd/rnix/main.go:1488 ipc.NewServer kept the single-value signature for
// some test fixtures, and main.go:222 runVersion still prints it as the
// primary identifier).
func versionString() string {
	v, _, _, _ := buildVersionInfo()
	// Defense in depth: buildVersionInfo already enforces a non-empty v
	// (Priority 3 returns "0.0.0"; Priority 2 fills bv when empty). The
	// defaultIfEmpty here protects against a Priority-1 ldflags injection
	// that populates ldGitCommit but leaves ldVersion empty (partial CI).
	return defaultIfEmpty(v, "0.0.0")
}

// defaultIfEmpty returns fallback if s is empty, otherwise s. Used by
// runVersion / runDaemonStatus / the daemon startup banner so the rendered
// output is never a bare colon followed by nothing.
func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
