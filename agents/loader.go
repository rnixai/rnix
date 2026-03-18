package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// AgentLoader loads agent definitions from multiple search directories.
type AgentLoader struct {
	searchDirs  []string
	skillLoader *skills.SkillLoader
	mcpConfig   *mcp.MCPGlobalConfig // global MCP configuration (nil = no MCP resolution)
}

// NewAgentLoader creates a new AgentLoader that searches directories in order.
// The first directory containing the agent wins (shadow resolution).
// Pass nil for mcpCfg to skip MCP resolution (backward compatible).
func NewAgentLoader(searchDirs []string, sl *skills.SkillLoader, mcpCfg *mcp.MCPGlobalConfig) *AgentLoader {
	return &AgentLoader{searchDirs: searchDirs, skillLoader: sl, mcpConfig: mcpCfg}
}

// Load reads an agent's agent.yaml, instructions.md, and all referenced skills.
func (l *AgentLoader) Load(agentName string) (*AgentInfo, error) {
	// Path traversal check: reject names with path separators
	if strings.ContainsAny(agentName, `/\`) || agentName == ".." || strings.Contains(agentName, "..") {
		return nil, fmt.Errorf("invalid agent name %q: path traversal not allowed", agentName)
	}

	// Use ShadowResolve to find the agent directory in searchDirs (project-first)
	agentDir := config.ShadowResolve(agentName, l.searchDirs...)
	if agentDir == "" {
		return nil, fmt.Errorf("agent directory not found: %s (searched %v)", agentName, l.searchDirs)
	}

	// Path containment check: ensure resolved path is under one of the searchDirs
	absDir, err := filepath.Abs(agentDir)
	if err != nil {
		return nil, fmt.Errorf("resolve agent path: %w", err)
	}
	if !isUnderAnyDir(absDir, l.searchDirs) {
		return nil, fmt.Errorf("invalid agent name %q: resolved path escapes search directories", agentName)
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
		if l.skillLoader == nil {
			break
		}
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

// isUnderAnyDir checks if absPath is under at least one of the given directories.
func isUnderAnyDir(absPath string, dirs []string) bool {
	for _, dir := range dirs {
		absBase, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, absBase+string(filepath.Separator)) || absPath == absBase {
			return true
		}
	}
	return false
}
