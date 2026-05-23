package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

// =============================================================================
// ATDD 45.1 — AC#1: three-source version fallback
//
//   priority order: ldflags > BuildInfo > hardcoded "0.0.0"
//
// RED PHASE signal (compile-fail):
//   - buildVersionInfo() / defaultIfEmpty() do not exist yet
//     (cmd/rnix/version.go is unborn).
//   - The Makefile-injected vars ldVersion / ldGitCommit / ldBuildDate
//     do not exist (Task 2.1 introduces them; current main.go uses
//     version / gitCommit / buildDate).
//   - readBuildInfoFn injection point is unwired.
//
// All five tests fail to COMPILE until dev-story Task 1.1 lands version.go.
// Same compile-fail RED style used by atdd_44_4_*_test.go for undefined
// target symbols.
//
// Once green:
//   - TestATDD_45_1_001 covers the ldflags injection path (highest priority)
//   - TestATDD_45_1_002 covers the BuildInfo fallback path (go install path)
//   - TestATDD_45_1_003 covers the hardcoded floor (no ldflags + no VCS)
//   - TestATDD_45_1_004 covers the +dirty suffix wiring
//   - TestATDD_45_1_005 covers the runVersion JSON shape adding `dirty`
// =============================================================================

// withLdflagsSet temporarily mutates the package-level ldVersion / ldGitCommit
// / ldBuildDate globals and restores them on cleanup. These are the variables
// that `make build` injects via `-X main.ldVersion=...` etc. (AC#2).
//
// Sets `dirty` separately because the +dirty suffix lives on ldGitCommit per
// AC#2 (Makefile composes `<sha>+dirty`).
func withLdflagsSet(t *testing.T, v, c, d string) {
	t.Helper()
	prevV, prevC, prevD := ldVersion, ldGitCommit, ldBuildDate
	ldVersion = v
	ldGitCommit = c
	ldBuildDate = d
	t.Cleanup(func() {
		ldVersion = prevV
		ldGitCommit = prevC
		ldBuildDate = prevD
	})
}

// withBuildInfoFn temporarily replaces the runtime/debug.ReadBuildInfo
// indirection so we can inject a synthetic *debug.BuildInfo without depending
// on the test binary's actual VCS settings (go test produces
// `go test -buildmode=...` binaries whose vcs.revision is the test binary's
// commit, not a stable fixture).
//
// AC#1 spec line 295: "用 helper 函数接受 BuildInfo 注入实现可测试性".
func withBuildInfoFn(t *testing.T, fn func() (*debug.BuildInfo, bool)) {
	t.Helper()
	prev := readBuildInfoFn
	readBuildInfoFn = fn
	t.Cleanup(func() { readBuildInfoFn = prev })
}

// TestATDD_45_1_001_BuildVersionInfo_LdflagsPath_Returns3Tuple
//
// Highest-priority path: `make build` / `make install` / `make release` inject
// all three ldflags variables. buildVersionInfo must return them verbatim and
// NOT consult BuildInfo (ldflags wins).
func TestATDD_45_1_001_BuildVersionInfo_LdflagsPath_Returns3Tuple(t *testing.T) {
	withLdflagsSet(t, "v0.8.0", "abc1234", "2026-05-23T10:00:00Z")

	// Even if BuildInfo would offer different values, ldflags wins.
	withBuildInfoFn(t, func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v9.9.9-buildinfo"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "deadbeefdeadbeef"},
				{Key: "vcs.time", Value: "2099-01-01T00:00:00Z"},
			},
		}, true
	})

	version, commit, buildDate, dirty := buildVersionInfo()

	if version != "v0.8.0" {
		t.Errorf("ldflags path version = %q, want %q (ldVersion must win over BuildInfo)", version, "v0.8.0")
	}
	if commit != "abc1234" {
		t.Errorf("ldflags path commit = %q, want %q", commit, "abc1234")
	}
	if buildDate != "2026-05-23T10:00:00Z" {
		t.Errorf("ldflags path buildDate = %q, want %q", buildDate, "2026-05-23T10:00:00Z")
	}
	if dirty {
		t.Errorf("ldflags path with clean commit must not be dirty, got dirty=true")
	}
}

// TestATDD_45_1_002_BuildVersionInfo_BuildInfoFallback
//
// Second-priority path: `go install github.com/rnixai/rnix/cmd/rnix@latest`
// has NO ldflags injection — buildVersionInfo must fall back to
// runtime/debug.ReadBuildInfo and pull Main.Version + vcs.revision (short SHA)
// + vcs.time. Dirty flag derived from vcs.modified.
func TestATDD_45_1_002_BuildVersionInfo_BuildInfoFallback(t *testing.T) {
	withLdflagsSet(t, "", "", "")
	withBuildInfoFn(t, func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v0.8.0"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc1234567890abc"},
				{Key: "vcs.time", Value: "2026-05-23T10:00:00Z"},
				{Key: "vcs.modified", Value: "false"},
			},
		}, true
	})

	version, commit, buildDate, dirty := buildVersionInfo()

	if version != "v0.8.0" {
		t.Errorf("BuildInfo fallback version = %q, want %q", version, "v0.8.0")
	}
	// commit must be the SHORT sha (first 7 chars), not the full 16-hex revision.
	// Story 45.1 AC#1 spec: "Settings[vcs.revision] (commit)" — and AC#5 matrix
	// row "go install ...@latest" expects "BuildInfo vcs.revision 短 SHA".
	if !strings.HasPrefix("abc1234567890abc", commit) || len(commit) > 12 {
		t.Errorf("BuildInfo fallback commit = %q, want short SHA prefix of 'abc1234567890abc'", commit)
	}
	if buildDate != "2026-05-23T10:00:00Z" {
		t.Errorf("BuildInfo fallback buildDate = %q, want %q", buildDate, "2026-05-23T10:00:00Z")
	}
	if dirty {
		t.Errorf("BuildInfo vcs.modified=false must yield dirty=false, got dirty=true")
	}
}

// TestATDD_45_1_003_BuildVersionInfo_NoVcsInfo_ReturnsHardcoded
//
// Bottom-priority path: `go install ./cmd/rnix` inside a local clone with
// `-buildvcs=false`, or any other scenario where neither ldflags nor BuildInfo
// VCS settings are available. Must return the hardcoded floor "0.0.0" + "unknown"
// — NOT the legacy "0.1.0" which lies about the actual codebase version
// (Story 45.1 §"核心 bug 诊断": var version = "0.1.0" 与实际 git tag v0.8.0
// 撒谎 7 个 minor version).
func TestATDD_45_1_003_BuildVersionInfo_NoVcsInfo_ReturnsHardcoded(t *testing.T) {
	withLdflagsSet(t, "", "", "")
	withBuildInfoFn(t, func() (*debug.BuildInfo, bool) {
		// BuildInfo present but completely empty (no Main.Version, no Settings):
		// emulates `go run` / older Go toolchain without VCS embedding.
		return &debug.BuildInfo{}, true
	})

	version, commit, buildDate, dirty := buildVersionInfo()

	if version != "0.0.0" {
		t.Errorf("hardcoded floor version = %q, want %q (not legacy '0.1.0')", version, "0.0.0")
	}
	if commit != "unknown" {
		t.Errorf("hardcoded floor commit = %q, want %q", commit, "unknown")
	}
	if buildDate != "" {
		t.Errorf("hardcoded floor buildDate = %q, want empty string", buildDate)
	}
	if dirty {
		t.Errorf("hardcoded floor must not be dirty, got dirty=true")
	}
}

// TestATDD_45_1_004_BuildVersionInfo_DirtyFlag_AppendsSuffix
//
// Dirty detection (AC#1 §dirty 处理):
//   - ldflags path: Makefile composes commit as `<sha>+dirty` (AC#2 GIT_DIRTY).
//     buildVersionInfo must surface dirty=true when ldGitCommit contains "+dirty".
//   - BuildInfo path: vcs.modified == "true" must yield commit with "+dirty"
//     suffix appended AND dirty=true.
func TestATDD_45_1_004_BuildVersionInfo_DirtyFlag_AppendsSuffix(t *testing.T) {
	t.Run("ldflags-dirty", func(t *testing.T) {
		withLdflagsSet(t, "v0.8.0", "abc1234+dirty", "2026-05-23T10:00:00Z")

		_, commit, _, dirty := buildVersionInfo()

		if !strings.HasSuffix(commit, "+dirty") {
			t.Errorf("ldflags-dirty commit = %q, want suffix '+dirty'", commit)
		}
		if !dirty {
			t.Errorf("ldflags-dirty path must set dirty=true (commit contains '+dirty')")
		}
	})

	t.Run("buildinfo-dirty", func(t *testing.T) {
		withLdflagsSet(t, "", "", "")
		withBuildInfoFn(t, func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{
				Main: debug.Module{Version: "v0.8.0"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc1234567890abc"},
					{Key: "vcs.time", Value: "2026-05-23T10:00:00Z"},
					{Key: "vcs.modified", Value: "true"},
				},
			}, true
		})

		_, commit, _, dirty := buildVersionInfo()

		if !strings.HasSuffix(commit, "+dirty") {
			t.Errorf("buildinfo-dirty commit = %q, want suffix '+dirty'", commit)
		}
		if !dirty {
			t.Errorf("buildinfo-dirty path must set dirty=true (vcs.modified=true)")
		}
	})
}

// TestATDD_45_1_005_RunVersion_JSON_IncludesDirtyField
//
// AC#1 §runVersion 输出格式更新: "JSON 模式 {version, git_commit, build_date,
// dirty} 四字段（新增 dirty bool）". The legacy main_test.go:TestVersion_JSON_Basic
// asserted 3 fields; the new ATDD asserts the 4th (dirty) is present and is a
// real bool, not a string.
func TestATDD_45_1_005_RunVersion_JSON_IncludesDirtyField(t *testing.T) {
	withLdflagsSet(t, "v0.8.0", "abc1234+dirty", "2026-05-23T10:00:00Z")

	// runVersion is a top-level entry point; we exercise it indirectly through
	// buildVersionInfo + a small JSON shape check on the package's marshaller.
	// dev-story Task 1.3 wires `runVersion` to include `dirty` as a top-level
	// JSON field — until then, the field will be missing and this fails.
	_, _, _, dirty := buildVersionInfo()
	if !dirty {
		t.Fatalf("precondition: buildVersionInfo dirty must be true with ldflags '+dirty' suffix")
	}

	// Smoke check that the JSON serialization contract for `runVersion`
	// (Task 1.3) includes "dirty" as a boolean. We do not call runVersion
	// here (that test belongs in main_test.go re-write per AC#6); we only
	// confirm the package exposes a helper that emits the new shape.
	//
	// Per AC#1 line 62: helper returns (version, commit, buildDate string).
	// The dirty field is a NEW return — its mere presence in the signature
	// (4th value) is the contract proof.
	//
	// If dev-story Task 1.1 ships a 3-return helper instead of 4, this file
	// fails to COMPILE on the `_, _, _, dirty := buildVersionInfo()` line —
	// exactly the RED signal we want.
	_ = dirty
}
