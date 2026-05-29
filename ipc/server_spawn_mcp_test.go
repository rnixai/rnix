package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/internal/config"
)

// These tests pin the fix for the daemon project-context spawn path silently
// dropping an agent's declared `mcp:` servers. resolveProjectContext used to
// build the project-aware agent loader with a nil MCP config, so an agent that
// declared e.g. `mcp: [playwright]` resolved to zero MCP devices with no error.
// See investigations/spawn-mcp-not-available-investigation.md (Finding 5).

// writeMCPAgentFixture creates .rnix/agents/<name>/{agent.yaml,instructions.md}
// under projectDir, declaring the given mcp servers.
func writeMCPAgentFixture(t *testing.T, projectDir, name string, mcpServers []string) {
	t.Helper()
	agentDir := filepath.Join(projectDir, ".rnix", "agents", name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "name: %s\ndescription: \"mcp test agent\"\nskills: []\nmcp:\n", name)
	for _, s := range mcpServers {
		fmt.Fprintf(&sb, "  - %s\n", s)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "instructions.md"), []byte("test agent"), 0o644); err != nil {
		t.Fatalf("write instructions.md: %v", err)
	}
}

func playwrightGlobalMCP() *mcp.MCPGlobalConfig {
	return &mcp.MCPGlobalConfig{
		Servers: map[string]mcp.MCPServerConfig{
			"playwright": {Command: "npx", Args: []string{"-y", "@playwright/mcp@latest"}, TransportType: "stdio"},
		},
	}
}

// Regression: global mcp.yaml has playwright, project has NO .rnix/mcp.yaml.
// The project loader must still resolve the agent's declared playwright server
// by falling back to the global MCP config (the EchoMatrix scenario).
func TestResolveProjectContext_GlobalMCP_NoProjectMCP_ResolvesAgentMCP(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       t.TempDir(),
		AgentsDir: t.TempDir(),
		SkillsDir: t.TempDir(),
		MCP:       playwrightGlobalMCP(),
	})

	projectDir := t.TempDir()
	writeMCPAgentFixture(t, projectDir, "stem", []string{"playwright"})

	_, loaderFn, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext error: %v", err)
	}
	if loaderFn == nil {
		t.Fatal("expected non-nil loader function")
	}

	info, err := loaderFn("stem")
	if err != nil {
		t.Fatalf("loading agent 'stem' returned error: %v", err)
	}
	if len(info.MCPConfigs) != 1 {
		t.Fatalf("MCPConfigs length = %d, want 1 (playwright)", len(info.MCPConfigs))
	}
	if info.MCPConfigs[0].ServerName != "playwright" {
		t.Errorf("MCPConfigs[0].ServerName = %q, want %q", info.MCPConfigs[0].ServerName, "playwright")
	}
}

// Project .rnix/mcp.yaml overrides the global definition by server name; the
// merged config (project wins) is what the loader resolves against.
func TestResolveProjectContext_ProjectMCP_OverridesGlobal(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       t.TempDir(),
		AgentsDir: t.TempDir(),
		SkillsDir: t.TempDir(),
		MCP:       playwrightGlobalMCP(),
	})

	projectDir := t.TempDir()
	writeMCPAgentFixture(t, projectDir, "stem", []string{"playwright"})

	// Project-level mcp.yaml redefines playwright with a custom command.
	projectMCP := "servers:\n  playwright:\n    command: project-npx\n    transport_type: stdio\n"
	if err := os.WriteFile(filepath.Join(projectDir, ".rnix", "mcp.yaml"), []byte(projectMCP), 0o644); err != nil {
		t.Fatalf("write project mcp.yaml: %v", err)
	}

	_, loaderFn, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext error: %v", err)
	}

	info, err := loaderFn("stem")
	if err != nil {
		t.Fatalf("loading agent 'stem' returned error: %v", err)
	}
	if len(info.MCPConfigs) != 1 {
		t.Fatalf("MCPConfigs length = %d, want 1", len(info.MCPConfigs))
	}
	if info.MCPConfigs[0].Command != "project-npx" {
		t.Errorf("MCPConfigs[0].Command = %q, want project override 'project-npx'", info.MCPConfigs[0].Command)
	}
}

// A malformed project .rnix/mcp.yaml must surface as an error, not a silent skip.
func TestResolveProjectContext_ProjectMCP_ParseError(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       t.TempDir(),
		AgentsDir: t.TempDir(),
		SkillsDir: t.TempDir(),
		MCP:       playwrightGlobalMCP(),
	})

	projectDir := t.TempDir()
	rnixDir := filepath.Join(projectDir, ".rnix")
	if err := os.MkdirAll(rnixDir, 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	// Invalid: max_output_bytes below the minimum fails MCPServerConfig.Validate.
	bad := "servers:\n  playwright:\n    command: npx\n    transport_type: stdio\n    max_output_bytes: 10\n"
	if err := os.WriteFile(filepath.Join(rnixDir, "mcp.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatalf("write bad mcp.yaml: %v", err)
	}

	_, _, err := srv.resolveProjectContext(projectDir, "")
	if err == nil {
		t.Fatal("expected error for malformed project mcp.yaml, got nil")
	}
}
