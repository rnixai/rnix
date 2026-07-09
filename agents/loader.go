package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

	// Resolve ALL directories containing this agent across searchDirs (project
	// first). Project layers override global on a per-field basis — mirroring
	// the providers.yaml project-override model (ipc/server_spawn.go) so a
	// project .rnix/agents/<name>/agent.yaml can override just `models` while
	// inheriting name/skills/instructions.md from the global definition.
	agentDirs := config.ShadowResolveAll(agentName, l.searchDirs...)
	if len(agentDirs) == 0 {
		return nil, fmt.Errorf("agent directory not found: %s (searched %v)", agentName, l.searchDirs)
	}

	// Path containment check: every resolved layer must be under a searchDir.
	for _, d := range agentDirs {
		absDir, err := filepath.Abs(d)
		if err != nil {
			return nil, fmt.Errorf("resolve agent path: %w", err)
		}
		if !isUnderAnyDir(absDir, l.searchDirs) {
			return nil, fmt.Errorf("invalid agent name %q: resolved path escapes search directories", agentName)
		}
	}

	// Merge agent.yaml across layers. agentDirs is project-first; DeepMergeYAML
	// treats its second arg as the override, so fold from the lowest-priority
	// layer (last/global) up to the highest-priority (first/project). A layer
	// without agent.yaml contributes nothing — file-level fallback to a lower
	// layer that does have it.
	mergedManifest := map[string]any{}
	foundManifest := false
	for i := range slices.Backward(agentDirs) {
		manifestData, err := os.ReadFile(filepath.Join(agentDirs[i], "agent.yaml"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read agent manifest: %w", err)
		}
		var layer map[string]any
		if err := yaml.Unmarshal(manifestData, &layer); err != nil {
			return nil, fmt.Errorf("failed to parse agent manifest %s: %w", filepath.Join(agentDirs[i], "agent.yaml"), err)
		}
		mergedManifest = config.DeepMergeYAML(mergedManifest, layer)
		foundManifest = true
	}
	if !foundManifest {
		return nil, fmt.Errorf("failed to read agent manifest: no agent.yaml found for %q in %v", agentName, agentDirs)
	}

	mergedBytes, err := yaml.Marshal(mergedManifest)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal merged agent manifest: %w", err)
	}
	var manifest AgentManifest
	if err := yaml.Unmarshal(mergedBytes, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse agent manifest: %w", err)
	}
	if manifest.Name == "" {
		return nil, fmt.Errorf("agent manifest missing required field: name")
	}

	// Load instructions.md with file-level fallback: first existing across
	// agentDirs in project-first order (project overrides, else inherit global).
	var instructionsData []byte
	instructionsFound := false
	for _, d := range agentDirs {
		b, rerr := os.ReadFile(filepath.Join(d, "instructions.md"))
		if rerr == nil {
			instructionsData = b
			instructionsFound = true
			break
		}
	}
	if !instructionsFound {
		return nil, fmt.Errorf("failed to read agent instructions: instructions.md not found for %q in %v", agentName, agentDirs)
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

	// Load deferred skills (metadata-only, body loaded on discover_skill)
	var deferredSkills []*skills.SkillInfo
	for _, skillName := range manifest.DeferredSkills {
		if l.skillLoader == nil {
			break
		}
		skillInfo, err := l.skillLoader.LoadMetadata(skillName)
		if err != nil {
			return nil, fmt.Errorf("failed to load deferred skill metadata %q referenced by agent %q: %w",
				skillName, agentName, err)
		}
		deferredSkills = append(deferredSkills, skillInfo)
	}

	// Resolve MCP references from global config. An agent that declares `mcp:`
	// servers but whose loader has no mcp.yaml configuration is a misconfiguration
	// (or a wiring bug, see ipc/server_spawn.go:resolveProjectContext) — fail loudly
	// rather than silently dropping the MCP devices, which would leave the spawned
	// process without any MCP tool and no error to explain why.
	var mcpConfigs []vfs.MCPConfig
	if len(manifest.MCP) > 0 {
		if l.mcpConfig == nil {
			return nil, fmt.Errorf("agent %q declares mcp servers %q but no mcp.yaml configuration was loaded", agentName, manifest.MCP)
		}
		for _, serverName := range manifest.MCP {
			serverCfg, ok := l.mcpConfig.Servers[serverName]
			if !ok {
				return nil, fmt.Errorf("mcp server %q not found in mcp.yaml", serverName)
			}
			mcpConfigs = append(mcpConfigs, serverCfg.ToMCPConfig(serverName))
		}
	}

	return &AgentInfo{
		Manifest:       manifest,
		Instructions:   string(instructionsData),
		Skills:         loadedSkills,
		DeferredSkills: deferredSkills,
		MCPConfigs:     mcpConfigs,
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
