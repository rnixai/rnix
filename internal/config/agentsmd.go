package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// AgentsMDFilename is the exclusive filename recognized for project-root
// document injection (Story 35.7 AC3 / SPEC-agents-md-injection Non-goal).
// Only "AGENTS.md" is matched — never CLAUDE.md/RNIX.md: this repository's root
// CLAUDE.md is written for Claude Code, and reading it would pollute a Rnix
// agent's system prompt (content mismatch + dual-owner + name squatting).
const AgentsMDFilename = "AGENTS.md"

// MaxAgentsMDBytes is the soft byte ceiling for injected AGENTS.md content.
// The AGENTS.md standard sets no hard limit (plain Markdown, no schema); Rnix
// caps it only to protect the system prompt from overflow, mirroring the
// max_output_bytes truncation pattern (Story 48.6 / drivers/mcp/config.go).
// Content beyond this is truncated with a trailing marker + a logged warning.
const MaxAgentsMDBytes = 64 * 1024 // 64 KiB

// FindNearestAgentsMD walks up from startDir looking for the nearest AGENTS.md,
// stopping at — and never above — projectRoot (nearest-wins, CAP-2). It mirrors
// the upward-traversal shape of ProjectDir but matches AGENTS.md and is bounded
// by projectRoot rather than $HOME/filesystem root.
//
// Returns the file body (truncated to MaxAgentsMDBytes, UTF-8 safe) or "" when:
//   - startDir or projectRoot is empty,
//   - no AGENTS.md exists between startDir and projectRoot (inclusive),
//   - the file cannot be read (graceful degradation — explicit error handling,
//     NOT reliant on the section layer's panic recovery, AC5).
//
// Only AGENTS.md is matched (AC3); CLAUDE.md/RNIX.md are never considered.
func FindNearestAgentsMD(startDir, projectRoot string) string {
	if startDir == "" || projectRoot == "" {
		return ""
	}

	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return ""
	}

	for {
		candidate := filepath.Join(dir, AgentsMDFilename)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return readAgentsMD(candidate)
		}

		// Boundary: stop at (inclusive) projectRoot — never walk above it (CAP-2).
		if dir == root {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without crossing projectRoot — startDir was
			// not under projectRoot. Degrade rather than escape the boundary.
			return ""
		}
		dir = parent
	}
}

// readAgentsMD reads an AGENTS.md file, returning its body truncated to
// MaxAgentsMDBytes on a valid UTF-8 boundary. Read failures degrade to "".
func readAgentsMD(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "" // graceful degradation (AC5) — explicit, not via panic recovery
	}
	if len(data) <= MaxAgentsMDBytes {
		return string(data)
	}

	// Truncate on a valid UTF-8 boundary so a multi-byte rune is never split
	// (CJK content is common in project docs). ToValidUTF8 drops the trailing
	// partial rune left by the byte-level cut.
	truncated := strings.ToValidUTF8(string(data[:MaxAgentsMDBytes]), "")
	log.Printf("[project_doc] %s exceeded soft cap %d bytes (actual %d), truncated",
		path, MaxAgentsMDBytes, len(data))
	return truncated + fmt.Sprintf("\n\n[truncated: AGENTS.md exceeded %d bytes]", MaxAgentsMDBytes)
}
