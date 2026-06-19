package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// writeAgentsMDFixture writes content to path, failing the test on error.
func writeAgentsMDFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// AC2: nearest-wins — startDir's own AGENTS.md wins over an ancestor's.
func TestFindNearestAgentsMD_NearestWins(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgentsMDFixture(t, filepath.Join(root, AgentsMDFilename), "ROOT DOC")
	writeAgentsMDFixture(t, filepath.Join(sub, AgentsMDFilename), "SUB DOC")

	got := FindNearestAgentsMD(sub, root)
	if !strings.Contains(got, "SUB DOC") {
		t.Errorf("expected nearest (sub) AGENTS.md, got %q", got)
	}
	if strings.Contains(got, "ROOT DOC") {
		t.Errorf("ancestor AGENTS.md must not be included (nearest-wins), got %q", got)
	}
}

// AC2: walk up to projectRoot when startDir itself has none.
func TestFindNearestAgentsMD_WalksUpToRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgentsMDFixture(t, filepath.Join(root, AgentsMDFilename), "ROOT DOC")

	got := FindNearestAgentsMD(sub, root)
	if !strings.Contains(got, "ROOT DOC") {
		t.Errorf("expected root AGENTS.md via upward walk, got %q", got)
	}
}

// AC2: boundary — never walk above projectRoot (no escape to an ancestor).
func TestFindNearestAgentsMD_DoesNotEscapeRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// AGENTS.md lives ABOVE the project root — must NOT be picked up.
	writeAgentsMDFixture(t, filepath.Join(parent, AgentsMDFilename), "ANCESTOR DOC")

	got := FindNearestAgentsMD(root, root)
	if got != "" {
		t.Errorf("must not escape projectRoot boundary, got %q", got)
	}
}

// AC5: graceful degradation — missing file / empty inputs return "".
func TestFindNearestAgentsMD_Degradation(t *testing.T) {
	root := t.TempDir() // no AGENTS.md
	if got := FindNearestAgentsMD(root, root); got != "" {
		t.Errorf("missing AGENTS.md should degrade to empty, got %q", got)
	}
	if got := FindNearestAgentsMD("", ""); got != "" {
		t.Errorf("empty inputs should return empty, got %q", got)
	}
	if got := FindNearestAgentsMD(root, ""); got != "" {
		t.Errorf("empty projectRoot should return empty, got %q", got)
	}
}

// AC3: only AGENTS.md is matched — CLAUDE.md is never read.
func TestFindNearestAgentsMD_OnlyAgentsMD(t *testing.T) {
	root := t.TempDir()
	writeAgentsMDFixture(t, filepath.Join(root, "CLAUDE.md"), "CLAUDE DOC")
	// no AGENTS.md present
	if got := FindNearestAgentsMD(root, root); got != "" {
		t.Errorf("CLAUDE.md must not be read (exclusive AGENTS.md), got %q", got)
	}
}

// AC5: byte soft-cap truncation + trailing marker + UTF-8 boundary safety.
func TestFindNearestAgentsMD_Truncation(t *testing.T) {
	root := t.TempDir()
	// Multi-byte CJK rune (3 bytes) → content ~3x the cap, and the byte-level
	// cut lands mid-rune, exercising UTF-8 boundary safety.
	big := strings.Repeat("中", MaxAgentsMDBytes)
	writeAgentsMDFixture(t, filepath.Join(root, AgentsMDFilename), big)

	got := FindNearestAgentsMD(root, root)
	if !strings.Contains(got, "[truncated: AGENTS.md exceeded") {
		t.Errorf("expected truncation marker, got len=%d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("truncated output must remain valid UTF-8 (no broken rune at cut)")
	}
	// Body (excluding the short marker) must stay near the cap, not the full 3x.
	if len(got) > MaxAgentsMDBytes+128 {
		t.Errorf("truncated body should be near cap, got %d bytes", len(got))
	}
}

// AC5 (under cap): content at/under the cap is returned verbatim, no marker.
func TestFindNearestAgentsMD_UnderCapVerbatim(t *testing.T) {
	root := t.TempDir()
	content := "# Project Conventions\n\nUse tabs. Run `make all`.\n"
	writeAgentsMDFixture(t, filepath.Join(root, AgentsMDFilename), content)

	got := FindNearestAgentsMD(root, root)
	if got != content {
		t.Errorf("under-cap content must be verbatim\n got: %q\nwant: %q", got, content)
	}
}
