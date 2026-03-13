package agents

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/skills"
)

func TestAgentLoader_Load_Success(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	info, err := al.Load("mock-agent")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Verify manifest fields
	if info.Manifest.Name != "mock-agent" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "mock-agent")
	}
	if info.Manifest.Description != "A mock agent for testing" {
		t.Errorf("Description = %q, want %q", info.Manifest.Description, "A mock agent for testing")
	}
	if info.Manifest.Models.Provider != "claude" {
		t.Errorf("Models.Provider = %q, want %q", info.Manifest.Models.Provider, "claude")
	}
	if info.Manifest.Models.Preferred != "sonnet" {
		t.Errorf("Models.Preferred = %q, want %q", info.Manifest.Models.Preferred, "sonnet")
	}
	if info.Manifest.Models.Fallback != "haiku" {
		t.Errorf("Models.Fallback = %q, want %q", info.Manifest.Models.Fallback, "haiku")
	}
	if info.Manifest.ContextBudget != 4096 {
		t.Errorf("ContextBudget = %d, want %d", info.Manifest.ContextBudget, 4096)
	}
	if len(info.Manifest.Skills) != 1 || info.Manifest.Skills[0] != "mock-skill" {
		t.Errorf("Skills = %v, want [mock-skill]", info.Manifest.Skills)
	}

	// Verify instructions loaded
	if !strings.Contains(info.Instructions, "Mock Agent") {
		t.Errorf("Instructions does not contain 'Mock Agent', got: %q", info.Instructions)
	}

	// Verify skills loaded
	if len(info.Skills) != 1 {
		t.Fatalf("Skills count = %d, want 1", len(info.Skills))
	}
	if info.Skills[0].Manifest.Name != "mock-skill" {
		t.Errorf("Skill[0].Name = %q, want %q", info.Skills[0].Manifest.Name, "mock-skill")
	}
	if info.Skills[0].Body == "" {
		t.Error("Skill[0].Body is empty, expected content from SKILL.md")
	}
}

func TestAgentLoader_Load_DirNotFound(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	_, err := al.Load("nonexistent-agent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestAgentLoader_Load_InvalidManifest(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	_, err := al.Load("invalid-agent")
	if err == nil {
		t.Fatal("expected error for invalid agent manifest, got nil")
	}
	if !strings.Contains(err.Error(), "parse agent manifest") {
		t.Errorf("error = %q, want substring 'parse agent manifest'", err.Error())
	}
}

func TestAgentLoader_Load_MissingInstructions(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	_, err := al.Load("missing-instructions")
	if err == nil {
		t.Fatal("expected error for missing instructions.md, got nil")
	}
	if !strings.Contains(err.Error(), "instructions") {
		t.Errorf("error = %q, want substring 'instructions'", err.Error())
	}
}

func TestAgentLoader_Load_MissingName(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	_, err := al.Load("missing-name")
	if err == nil {
		t.Fatal("expected error for missing name field, got nil")
	}
	if !strings.Contains(err.Error(), "missing required field: name") {
		t.Errorf("error = %q, want substring 'missing required field: name'", err.Error())
	}
}

func TestAgentLoader_Load_BadSkillRef(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	_, err := al.Load("bad-skill-ref")
	if err == nil {
		t.Fatal("expected error for bad skill reference, got nil")
	}
	if !strings.Contains(err.Error(), "failed to load skill") {
		t.Errorf("error = %q, want substring 'failed to load skill'", err.Error())
	}
	if !strings.Contains(err.Error(), "nonexistent-skill") {
		t.Errorf("error = %q, want substring 'nonexistent-skill'", err.Error())
	}
}

func TestAgentInfo_AllowedTools(t *testing.T) {
	info := &AgentInfo{
		Skills: []*skills.SkillInfo{
			{Manifest: skills.SkillManifest{AllowedToolsRaw: "/dev/fs /dev/shell"}},
			{Manifest: skills.SkillManifest{AllowedToolsRaw: "/dev/shell /dev/net"}},
		},
	}
	tools := info.AllowedTools()

	// Should be deduplicated and sorted
	expected := []string{"/dev/fs", "/dev/net", "/dev/shell"}
	if len(tools) != len(expected) {
		t.Fatalf("AllowedTools length = %d, want %d: %v", len(tools), len(expected), tools)
	}
	for i, exp := range expected {
		if tools[i] != exp {
			t.Errorf("AllowedTools[%d] = %q, want %q", i, tools[i], exp)
		}
	}
}

func TestAgentInfo_AllowedTools_Empty(t *testing.T) {
	info := &AgentInfo{Skills: nil}
	tools := info.AllowedTools()
	if len(tools) != 0 {
		t.Errorf("AllowedTools = %v, want empty for nil skills", tools)
	}
}

func TestAgentInfo_SystemPrompt(t *testing.T) {
	info := &AgentInfo{
		Instructions: "You are an agent.",
		Skills: []*skills.SkillInfo{
			{Body: "Skill 1 content"},
			{Body: "Skill 2 content"},
		},
	}
	prompt := info.SystemPrompt()
	if !strings.Contains(prompt, "You are an agent.") {
		t.Error("SystemPrompt missing agent instructions")
	}
	if !strings.Contains(prompt, "Skill 1 content") {
		t.Error("SystemPrompt missing skill 1 body")
	}
	if !strings.Contains(prompt, "Skill 2 content") {
		t.Error("SystemPrompt missing skill 2 body")
	}
}

func TestAgentInfo_SystemPrompt_NoSkills(t *testing.T) {
	info := &AgentInfo{
		Instructions: "You are an agent.",
		Skills:       nil,
	}
	prompt := info.SystemPrompt()
	if prompt != "You are an agent." {
		t.Errorf("SystemPrompt = %q, want %q", prompt, "You are an agent.")
	}
}

func TestAgentInfo_SystemPrompt_EmptyBody(t *testing.T) {
	info := &AgentInfo{
		Instructions: "Agent instructions.",
		Skills: []*skills.SkillInfo{
			{Body: ""},
			{Body: "Non-empty body"},
		},
	}
	prompt := info.SystemPrompt()
	if strings.Count(prompt, "\n\n") != 1 {
		t.Errorf("SystemPrompt should have only one double-newline separator, got: %q", prompt)
	}
}

func TestAgentLoader_Load_RealCodeAnalyst(t *testing.T) {
	sl := skills.NewSkillLoader("../lib/skills")
	al := NewAgentLoader("../lib/agents", sl, nil)

	info, err := al.Load("code-analyst")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if info.Manifest.Name != "code-analyst" {
		t.Errorf("Name = %q, want %q", info.Manifest.Name, "code-analyst")
	}
	if info.Manifest.Models.Provider != "claude" {
		t.Errorf("Models.Provider = %q, want %q", info.Manifest.Models.Provider, "claude")
	}
	if info.Manifest.Models.Preferred != "sonnet" {
		t.Errorf("Models.Preferred = %q, want %q", info.Manifest.Models.Preferred, "sonnet")
	}
	if info.Manifest.Models.Fallback != "haiku" {
		t.Errorf("Models.Fallback = %q, want %q", info.Manifest.Models.Fallback, "haiku")
	}
	if info.Manifest.ContextBudget != 8192 {
		t.Errorf("ContextBudget = %d, want %d", info.Manifest.ContextBudget, 8192)
	}

	// Verify skills loaded
	if len(info.Skills) != 1 {
		t.Fatalf("Skills count = %d, want 1", len(info.Skills))
	}
	if info.Skills[0].Manifest.Name != "code-analysis" {
		t.Errorf("Skill[0].Name = %q, want %q", info.Skills[0].Manifest.Name, "code-analysis")
	}

	// Verify AllowedTools aggregation
	tools := info.AllowedTools()
	if len(tools) != 2 {
		t.Fatalf("AllowedTools length = %d, want 2: %v", len(tools), tools)
	}

	// Verify system prompt contains both agent and skill content
	prompt := info.SystemPrompt()
	if !strings.Contains(prompt, "Code Analyst") {
		t.Error("SystemPrompt missing 'Code Analyst' from agent instructions")
	}
	if !strings.Contains(prompt, "Code Analysis") {
		t.Error("SystemPrompt missing 'Code Analysis' from skill body")
	}
}

func TestAgentLoader_Load_PathTraversal(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	_, err := al.Load("../../../etc")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "path escapes") {
		t.Errorf("error = %q, want substring 'path escapes'", err.Error())
	}
}

// --- Story 9.2: MCP field tests ---

// testMCPGlobalConfig creates a MCPGlobalConfig for testing with github and slack servers.
func testMCPGlobalConfig() *mcp.MCPGlobalConfig {
	return &mcp.MCPGlobalConfig{
		Servers: map[string]mcp.MCPServerConfig{
			"github": {
				Command:       "npx",
				Args:          []string{"-y", "@mcp/server-github"},
				Env:           map[string]string{"GITHUB_TOKEN": "test"},
				TransportType: "stdio",
			},
			"slack": {
				Command:       "npx",
				Args:          []string{"-y", "@mcp/server-slack"},
				Env:           map[string]string{"SLACK_TOKEN": "test"},
				TransportType: "stdio",
			},
		},
	}
}

func TestAgentLoader_Load_WithMCPField(t *testing.T) {
	// Given: an agent.yaml with mcp: ["github", "slack"] and a valid MCPGlobalConfig
	sl := skills.NewSkillLoader("../skills/testdata")
	mcpCfg := testMCPGlobalConfig()
	al := NewAgentLoader("testdata", sl, mcpCfg)

	// When: loading the mcp-agent
	info, err := al.Load("mcp-agent")

	// Then: AgentManifest.MCP contains the MCP server references
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(info.Manifest.MCP) != 2 {
		t.Fatalf("Manifest.MCP length = %d, want 2", len(info.Manifest.MCP))
	}
	if info.Manifest.MCP[0] != "github" {
		t.Errorf("Manifest.MCP[0] = %q, want %q", info.Manifest.MCP[0], "github")
	}
	if info.Manifest.MCP[1] != "slack" {
		t.Errorf("Manifest.MCP[1] = %q, want %q", info.Manifest.MCP[1], "slack")
	}
}

func TestAgentLoader_Load_WithoutMCPField(t *testing.T) {
	// Given: a standard agent.yaml without mcp field
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	// When: loading the mock-agent (no mcp field)
	info, err := al.Load("mock-agent")

	// Then: AgentManifest.MCP is nil/empty (backward compatible)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(info.Manifest.MCP) != 0 {
		t.Errorf("Manifest.MCP = %v, want empty for agent without mcp field", info.Manifest.MCP)
	}
	if len(info.MCPConfigs) != 0 {
		t.Errorf("MCPConfigs = %v, want empty for agent without mcp field", info.MCPConfigs)
	}
}

func TestAgentLoader_Load_MCPServerNotFound(t *testing.T) {
	// Given: an agent.yaml referencing MCP servers not in the global config
	sl := skills.NewSkillLoader("../skills/testdata")
	// MCPGlobalConfig only has "github", not "slack"
	mcpCfg := &mcp.MCPGlobalConfig{
		Servers: map[string]mcp.MCPServerConfig{
			"github": {
				Command:       "npx",
				TransportType: "stdio",
			},
		},
	}
	al := NewAgentLoader("testdata", sl, mcpCfg)

	// When: loading the mcp-agent which references ["github", "slack"]
	_, err := al.Load("mcp-agent")

	// Then: error is returned indicating the missing server
	if err == nil {
		t.Fatal("expected error for MCP server not found, got nil")
	}
	if !strings.Contains(err.Error(), "slack") {
		t.Errorf("error = %q, want substring 'slack'", err.Error())
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want substring 'not found'", err.Error())
	}
}

func TestAgentLoader_Load_MCPResolvesToAgentInfo(t *testing.T) {
	// Given: an agent.yaml with mcp field and matching global config
	sl := skills.NewSkillLoader("../skills/testdata")
	mcpCfg := testMCPGlobalConfig()
	al := NewAgentLoader("testdata", sl, mcpCfg)

	// When: loading the mcp-agent
	info, err := al.Load("mcp-agent")

	// Then: AgentInfo.MCPConfigs is populated with resolved vfs.MCPConfig entries
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(info.MCPConfigs) != 2 {
		t.Fatalf("MCPConfigs length = %d, want 2", len(info.MCPConfigs))
	}

	// Verify first config (github)
	if info.MCPConfigs[0].ServerName != "github" {
		t.Errorf("MCPConfigs[0].ServerName = %q, want %q", info.MCPConfigs[0].ServerName, "github")
	}
	if info.MCPConfigs[0].Command != "npx" {
		t.Errorf("MCPConfigs[0].Command = %q, want %q", info.MCPConfigs[0].Command, "npx")
	}
	if info.MCPConfigs[0].TransportType != "stdio" {
		t.Errorf("MCPConfigs[0].TransportType = %q, want %q", info.MCPConfigs[0].TransportType, "stdio")
	}

	// Verify second config (slack)
	if info.MCPConfigs[1].ServerName != "slack" {
		t.Errorf("MCPConfigs[1].ServerName = %q, want %q", info.MCPConfigs[1].ServerName, "slack")
	}
}

func TestAgentLoader_Load_NilMCPConfig_SkipsMCPResolution(t *testing.T) {
	// Given: an agent.yaml with mcp field but mcpConfig is nil (no mcp.yaml)
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	// When: loading the mcp-agent
	info, err := al.Load("mcp-agent")

	// Then: MCP field is parsed but MCPConfigs is empty (no resolution)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(info.Manifest.MCP) != 2 {
		t.Fatalf("Manifest.MCP length = %d, want 2", len(info.Manifest.MCP))
	}
	if len(info.MCPConfigs) != 0 {
		t.Errorf("MCPConfigs = %v, want empty when mcpConfig is nil", info.MCPConfigs)
	}
}

// --- Story 21.2: SLA parsing tests ---

func TestAgentLoader_Load_WithSLA(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	info, err := al.Load("sla-agent")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if info.Manifest.SLA == nil {
		t.Fatal("expected SLA to be non-nil")
	}
	if info.Manifest.SLA.MaxTokens != 15000 {
		t.Errorf("SLA.MaxTokens = %d, want 15000", info.Manifest.SLA.MaxTokens)
	}
	if info.Manifest.SLA.MaxDurationMs != 60000 {
		t.Errorf("SLA.MaxDurationMs = %d, want 60000", info.Manifest.SLA.MaxDurationMs)
	}
	if info.Manifest.SLA.OutputFormat != "json" {
		t.Errorf("SLA.OutputFormat = %q, want %q", info.Manifest.SLA.OutputFormat, "json")
	}
}

func TestAgentLoader_Load_WithoutSLA(t *testing.T) {
	sl := skills.NewSkillLoader("../skills/testdata")
	al := NewAgentLoader("testdata", sl, nil)

	info, err := al.Load("mock-agent")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if info.Manifest.SLA != nil {
		t.Errorf("expected SLA to be nil for agent without SLA, got %+v", info.Manifest.SLA)
	}
}
