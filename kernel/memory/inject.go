package memory

import (
	"strings"
)

// BuildMemoryBlock builds the memory section content for system prompt injection.
// Returns empty string if no memory entries exist in either scope.
// Output order: global memory first, then project memory (project is closer to conversation).
func BuildMemoryBlock(store *MemoryStore, projectDir string) string {
	globalSnap := store.Snapshot("global_memory", "")
	projectSnap := store.Snapshot("memory", projectDir)
	if globalSnap == "" && projectSnap == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Memory\n\nKnowledge accumulated from previous sessions. Use this context but verify against current code state.\n\n")

	if globalSnap != "" {
		b.WriteString("## Global Memory\n\n")
		b.WriteString(globalSnap)
		b.WriteString("\n")
	}
	if globalSnap != "" && projectSnap != "" {
		b.WriteString("\n---\n\n")
	}
	if projectSnap != "" {
		b.WriteString("## Project Memory\n\n")
		b.WriteString(projectSnap)
		b.WriteString("\n")
	}

	return b.String()
}

// BuildUserProfileBlock builds the user profile section for system prompt injection.
// Returns empty string if no user profile entries exist.
func BuildUserProfileBlock(store *MemoryStore, projectDir string) string {
	userSnap := store.Snapshot("user", projectDir)
	if userSnap == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("# User Profile\n\nPersonalized context about the current user. Use this to tailor your responses.\n\n")
	b.WriteString(userSnap)
	b.WriteString("\n")
	return b.String()
}
