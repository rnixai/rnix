package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// AgentLoader loads agent definitions from a base directory.
type AgentLoader struct {
	basePath    string
	skillLoader *skills.SkillLoader
	mcpConfig   *mcp.MCPGlobalConfig // global MCP configuration (nil = no MCP resolution)
}

// NewAgentLoader creates a new AgentLoader rooted at basePath.
// Pass nil for mcpCfg to skip MCP resolution (backward compatible).
func NewAgentLoader(basePath string, sl *skills.SkillLoader, mcpCfg *mcp.MCPGlobalConfig) *AgentLoader {
	return &AgentLoader{basePath: basePath, skillLoader: sl, mcpConfig: mcpCfg}
}

// Load reads an agent's agent.yaml, instructions.md, and all referenced skills.
func (l *AgentLoader) Load(agentName string) (*AgentInfo, error) {
	agentDir := filepath.Join(l.basePath, agentName)

	// Path containment check: prevent directory traversal
	absBase, err := filepath.Abs(l.basePath)
	if err != nil {
		return nil, fmt.Errorf("resolve base path: %w", err)
	}
	absDir, err := filepath.Abs(agentDir)
	if err != nil {
		return nil, fmt.Errorf("resolve agent path: %w", err)
	}
	if !strings.HasPrefix(absDir, absBase+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid agent name %q: path escapes base directory", agentName)
	}

	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("agent directory not found: %s", agentDir)
	}

	// Load agent.yaml
	manifestPath := filepath.Join(agentDir, "agent.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent manifest: %w", err)
	}
	var manifest AgentManifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse agent manifest: %w", err)
	}
	if manifest.Name == "" {
		return nil, fmt.Errorf("agent manifest missing required field: name")
	}

	// Load instructions.md
	instructionsPath := filepath.Join(agentDir, "instructions.md")
	instructionsData, err := os.ReadFile(instructionsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent instructions: %w", err)
	}

	// Load referenced skills
	var loadedSkills []*skills.SkillInfo
	for _, skillName := range manifest.Skills {
		skillInfo, err := l.skillLoader.LoadFull(skillName)
		if err != nil {
			return nil, fmt.Errorf("failed to load skill %q referenced by agent %q: %w",
				skillName, agentName, err)
		}
		loadedSkills = append(loadedSkills, skillInfo)
	}

	// Resolve MCP references from global config
	var mcpConfigs []vfs.MCPConfig
	if len(manifest.MCP) > 0 && l.mcpConfig != nil {
		for _, serverName := range manifest.MCP {
			serverCfg, ok := l.mcpConfig.Servers[serverName]
			if !ok {
				return nil, fmt.Errorf("mcp server %q not found in mcp.yaml", serverName)
			}
			mcpConfigs = append(mcpConfigs, serverCfg.ToMCPConfig(serverName))
		}
	}

	return &AgentInfo{
		Manifest:     manifest,
		Instructions: string(instructionsData),
		Skills:       loadedSkills,
		MCPConfigs:   mcpConfigs,
	}, nil
}
