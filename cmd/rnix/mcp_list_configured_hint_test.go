package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// Investigation mcp-list-empty (follow-up enhancement) — `mcp list` surfaces a
// "configured but not mounted" hint when the daemon holds zero MCP mounts but
// mcp.yaml declares servers. Mounts are per-process and created at spawn
// (kernel/spawn.go `/mnt/mcp/<pid>-<name>`), so an empty list with a populated
// mcp.yaml is expected — the hint explains that instead of leaving users to
// wonder why `check mcp` / `mcp test` look healthy while `list` is empty.
// =============================================================================

// withMCPConfigFile writes a mcp.yaml with the given server names into a temp
// dir and points resolveMCPConfigPath at it via mcpConfigPathForCheck.
func withMCPConfigFile(t *testing.T, serverNames ...string) func() {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.yaml")

	var sb strings.Builder
	sb.WriteString("servers:\n")
	for _, name := range serverNames {
		sb.WriteString("  " + name + ":\n")
		sb.WriteString("    command: npx\n")
		sb.WriteString("    args: [\"-y\", \"@example/" + name + "\"]\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatalf("write mcp.yaml: %v", err)
	}

	old := mcpConfigPathForCheck
	mcpConfigPathForCheck = path
	return func() { mcpConfigPathForCheck = old }
}

// _hint_single: one configured server → singular hint naming it + a concrete probe.
func TestMcpList_EmptyMounts_SingleConfiguredHint(t *testing.T) {
	defer withMCPConfigFile(t, "playwright")()

	var buf bytes.Buffer
	renderMCPListHuman(&buf, nil)
	out := buf.String()

	if !strings.Contains(out, "No active MCP mounts.") {
		t.Errorf("missing base empty message:\n%s", out)
	}
	for _, want := range []string{
		"1 server configured in mcp.yaml (playwright)",
		"not mounted yet",
		"rnix mcp test playwright",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in hint:\n%s", want, out)
		}
	}
}

// _hint_multi: multiple servers → plural hint listing names + generic probe.
func TestMcpList_EmptyMounts_MultiConfiguredHint(t *testing.T) {
	defer withMCPConfigFile(t, "deepwiki", "playwright")()

	var buf bytes.Buffer
	renderMCPListHuman(&buf, nil)
	out := buf.String()

	for _, want := range []string{
		"2 servers configured in mcp.yaml",
		"deepwiki, playwright", // sorted
		"rnix mcp test <name>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in hint:\n%s", want, out)
		}
	}
}

// _hint_absent: no mcp.yaml → base message only, NO hint (best-effort, silent).
func TestMcpList_EmptyMounts_NoConfigNoHint(t *testing.T) {
	old := mcpConfigPathForCheck
	mcpConfigPathForCheck = filepath.Join(t.TempDir(), "does-not-exist.yaml")
	defer func() { mcpConfigPathForCheck = old }()

	var buf bytes.Buffer
	renderMCPListHuman(&buf, nil)
	out := strings.TrimSpace(buf.String())

	if out != "No active MCP mounts." {
		t.Errorf("expected bare empty message with no hint, got:\n%s", out)
	}
}

// _hint_skipped_when_mounted: non-empty mounts must NOT trigger the config hint.
func TestMcpList_WithMounts_NoConfiguredHint(t *testing.T) {
	defer withMCPConfigFile(t, "playwright")()

	mounts := []ipc.MCPMountWire{
		{Name: "playwright", Path: "/mnt/mcp/100-playwright", Transport: "stdio", Status: "connected"},
	}
	var buf bytes.Buffer
	renderMCPListHuman(&buf, mounts)
	out := buf.String()

	if strings.Contains(out, "configured in mcp.yaml") {
		t.Errorf("hint must not appear when mounts are present:\n%s", out)
	}
	if !strings.Contains(out, "playwright") || !strings.Contains(out, "connected") {
		t.Errorf("expected the mount table, got:\n%s", out)
	}
}

// formatConfiguredMCPHint pure-function unit coverage for singular/plural forms.
func TestFormatConfiguredMCPHint_SingularPlural(t *testing.T) {
	single := formatConfiguredMCPHint([]string{"playwright"})
	if !strings.HasPrefix(single, "1 server configured") {
		t.Errorf("singular form wrong: %q", single)
	}
	multi := formatConfiguredMCPHint([]string{"a", "b", "c"})
	if !strings.HasPrefix(multi, "3 servers configured") {
		t.Errorf("plural form wrong: %q", multi)
	}
}
