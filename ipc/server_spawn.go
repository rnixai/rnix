package ipc

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"

	"github.com/goccy/go-yaml"
)

func (s *Server) handleSpawn(conn net.Conn, rawPayload json.RawMessage) {
	var req SpawnRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid spawn request"}})
		return
	}

	// Build project config and project-aware loaders (Story 25.3)
	projectCfg, agentLoaderFn, err := s.resolveProjectContext(req.ProjectDir, req.RnixEnv)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "CONFIG_ERROR", Message: err.Error()}})
		return
	}

	var agentInfo *agents.AgentInfo
	if req.Agent != "" && agentLoaderFn != nil {
		var err error
		agentInfo, err = agentLoaderFn(req.Agent)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("agent %q not found: %v", req.Agent, err)}})
			return
		}
	}

	eventCh := make(chan StreamEvent, 64)

	pid, err := s.kern.Spawn(req.Intent, agentInfo, kernel.SpawnOpts{
		Model:            req.Model,
		Provider:         req.Provider,
		FallbackModel:    req.FallbackModel,
		FallbackProvider: req.FallbackProvider,
		MaxTurns:         req.MaxSteps,
		ContextBudget:    req.ContextBudget,
		TimeoutMs:        req.TimeoutMs,
		TraceID:          types.TraceID(req.TraceID),
		ParentSpanID:     types.SpanID(req.ParentSpanID),
		ProjectConfig:    projectCfg,
		ComposeNode:      req.ComposeNode,
		ComposeDeps:      req.ComposeDeps,
		PipelineIndex:    req.PipelineIndex,
		PipelineTotal:    req.PipelineTotal,
	})
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	s.callbackMux.register(pid, eventCh)
	defer s.callbackMux.unregister(pid)
	defer s.kern.Reap(pid) // Reap top-level process after stream ends (no CLI Wait in daemon mode)

	// Compensate for OnSpawn event lost during kern.Spawn (fires before register)
	// Retrieve process to access resolved provider/model
	proc, ok := s.kern.GetProcess(pid)
	if !ok {
		writeStreamEvent(conn, StreamEvent{Type: StreamError, Payload: marshalJSON(ErrorPayload{Code: "INTERNAL", Message: "process vanished after spawn"})})
		return
	}
	spawnPP := ProgressPayload{Event: "spawn", PID: pid, Intent: req.Intent, Provider: proc.Provider, Model: proc.Model, UUID: proc.UUID}
	spawnPayload, _ := json.Marshal(spawnPP)
	select {
	case eventCh <- StreamEvent{Type: StreamProgress, Payload: spawnPayload}:
	default:
	}

	payload, _ := json.Marshal(SpawnResponse{PID: pid, UUID: proc.UUID})
	writeResponse(conn, Response{OK: true, Payload: payload})

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		exit := <-proc.Done
		pp := ProgressPayload{
			Event:      "complete",
			PID:        pid,
			ExitCode:   exit.Code,
			ExitReason: exit.Reason,
		}
		if info, err := s.kern.GetProcInfo(pid); err == nil {
			pp.Result = info.Result
			pp.TokensUsed = info.TokensUsed
		}
		if exit.Err != nil {
			pp.ErrorMessage = exit.Err.Error()
		}
		// Include SpanID for compose parent-child trace (Story 15.1)
		if spanID, ok := s.kern.GetSpanID(pid); ok {
			pp.SpanID = string(spanID)
		}

		payload, _ := json.Marshal(pp)
		select {
		case eventCh <- StreamEvent{Type: StreamComplete, Payload: payload}:
		default:
		}
	}()

	enc := json.NewEncoder(conn)
	for {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				return
			}
			if err := enc.Encode(ev); err != nil {
				return
			}
			if ev.Type == StreamComplete || ev.Type == StreamError {
				return
			}
		case <-doneCh:
			for {
				select {
				case ev := <-eventCh:
					_ = enc.Encode(ev)
					if ev.Type == StreamComplete || ev.Type == StreamError {
						return
					}
				default:
					return
				}
			}
		case <-s.done:
			return
		}
	}
}

// resolveProjectContext builds a ProjectConfig and project-aware agent loader for a spawn request.
// If projectDir is empty, returns (nil, s.agentLoader, nil) — pure global mode.
// If projectDir is non-empty, loads .env files, merges project providers with global,
// creates project-level LLM drivers, and builds project-aware loaders.
func (s *Server) resolveProjectContext(projectDir, rnixEnv string) (*config.ProjectConfig, AgentLoaderFunc, error) {
	if projectDir == "" {
		return nil, s.agentLoader, nil
	}

	gc := s.globalConfig
	if gc == nil {
		return nil, s.agentLoader, nil
	}

	// F-01: Sanitize projectDir — clean path and require absolute
	projectDir = filepath.Clean(projectDir)
	if !filepath.IsAbs(projectDir) {
		return nil, s.agentLoader, nil
	}

	// Safety: verify projectDir contains a .rnix/ subdirectory
	if _, err := os.Stat(filepath.Join(projectDir, ".rnix")); err != nil {
		// No .rnix/ directory — fall back to global mode
		return nil, s.agentLoader, nil
	}

	// F-02: Validate rnixEnv early (also validated in LoadDotenvDir, but defense-in-depth)
	if rnixEnv != "" && !config.ValidateEnvName(rnixEnv) {
		return nil, nil, fmt.Errorf("invalid RNIX_ENV value: %q", rnixEnv)
	}

	// Build search directories (project first, global second)
	projectAgentsDir := filepath.Join(projectDir, ".rnix", "agents")
	projectSkillsDir := filepath.Join(projectDir, ".rnix", "skills")
	agentDirs := []string{projectAgentsDir, gc.AgentsDir}
	skillDirs := []string{projectSkillsDir, gc.SkillsDir}

	// Load .env files from project directory
	dotenvVars, dotenvErr := config.LoadDotenvDir(projectDir, rnixEnv)
	if dotenvErr != nil {
		return nil, nil, fmt.Errorf("loading .env files from %s: %w", projectDir, dotenvErr)
	}
	envLookup := config.NewEnvLookup(dotenvVars)

	// Merge project providers.yaml with global (raw YAML bytes → DeepMergeYAML)
	projectProvidersPath := filepath.Join(projectDir, ".rnix", "providers.yaml")
	var mergedProvidersCfg *llm.ProvidersConfig

	if _, statErr := os.Stat(projectProvidersPath); statErr == nil {
		projectRaw, readErr := os.ReadFile(projectProvidersPath)
		if readErr != nil {
			return nil, nil, fmt.Errorf("reading project providers %s: %w", projectProvidersPath, readErr)
		}

		if len(s.globalProvidersRaw) > 0 && len(projectRaw) > 0 {
			var globalMap, projectMap map[string]any
			if unmarshalErr := yaml.Unmarshal(s.globalProvidersRaw, &globalMap); unmarshalErr != nil {
				return nil, nil, fmt.Errorf("parsing global providers YAML: %w", unmarshalErr)
			}
			if unmarshalErr := yaml.Unmarshal(projectRaw, &projectMap); unmarshalErr != nil {
				return nil, nil, fmt.Errorf("parsing project providers YAML: %w", unmarshalErr)
			}

			merged := config.DeepMergeYAML(globalMap, projectMap)

			if baseSlice, ok := globalMap["providers"].([]any); ok {
				if overSlice, ok := projectMap["providers"].([]any); ok {
					merged["providers"] = config.MergeNamedSlice(baseSlice, overSlice, "name")
				}
			}

			mergedBytes, marshalErr := yaml.Marshal(merged)
			if marshalErr != nil {
				return nil, nil, fmt.Errorf("marshaling merged providers: %w", marshalErr)
			}

			var parseErr error
			mergedProvidersCfg, parseErr = llm.ParseProvidersConfig(mergedBytes)
			if parseErr != nil {
				return nil, nil, fmt.Errorf("validating merged providers config: %w", parseErr)
			}
		} else {
			// No global raw bytes — just load project config directly
			var loadErr error
			mergedProvidersCfg, loadErr = llm.LoadProvidersConfig(projectProvidersPath)
			if loadErr != nil {
				return nil, nil, fmt.Errorf("project providers config %s: %w", projectProvidersPath, loadErr)
			}
		}
	}

	// F-12: If no project providers.yaml, shallow-copy global config to preserve immutability
	if mergedProvidersCfg == nil {
		if gcProviders, ok := gc.Providers.(*llm.ProvidersConfig); ok && gcProviders != nil {
			copied := *gcProviders
			mergedProvidersCfg = &copied
		}
	}

	// Build ProjectConfig snapshot
	projCfg := &config.ProjectConfig{
		ProjectDir:  projectDir,
		Providers:   mergedProvidersCfg,
		AgentDirs:   agentDirs,
		SkillDirs:   skillDirs,
		EnvSnapshot: dotenvVars,
	}

	// Create project-level LLM drivers and LLMFileOpener if we have merged providers
	if mergedProvidersCfg != nil {
		factories := make(map[string]vfs.VFSFileFactory)
		var projectOnlyProviders []ProviderStatusWire
		for _, pc := range mergedProvidersCfg.Providers {
			driver, driverErr := llm.CreateDriverWithEnv(pc, envLookup)
			if driverErr != nil {
				log.Printf("[ipc] warning: project provider %q: %v", pc.Name, driverErr)
				continue
			}
			factories[pc.Name] = llm.FileFactory(driver, "/dev/llm/"+pc.Name, pc.Mode)
		}

		// Track project-only providers (defined in project providers.yaml but not in global)
		if projectRawProviders := mergedProvidersCfg.Providers; len(projectRawProviders) > 0 {
			globalNames := s.globalProviderNames()
			for _, pc := range projectRawProviders {
				if _, isGlobal := globalNames[pc.Name]; !isGlobal {
					projectOnlyProviders = append(projectOnlyProviders, ProviderStatusWire{
						Name:   pc.Name,
						Driver: pc.Driver,
						Health: "unchecked",
						Source: "project",
					})
				}
			}
			if len(projectOnlyProviders) > 0 {
				s.projectProviders.Store(projectDir, projectOnlyProviders)
			}
		}

		if len(factories) > 0 {
			projCfg.LLMFileOpener = func(provider string, flags int) (any, error) {
				factory, ok := factories[provider]
				if !ok {
					return nil, fmt.Errorf("project provider %q not found", provider)
				}
				return factory("", vfs.OpenFlag(flags), projectDir)
			}
		}

		projCfg.DefaultProvider = mergedProvidersCfg.ResolveDefaultProvider()
	}

	// Resolve MCP config for the project-aware agent loader: the daemon's global
	// mcp.yaml (cached on globalConfig.MCP) merged with the project's optional
	// .rnix/mcp.yaml (project overrides global). Passing this — instead of nil —
	// is what lets an agent's declared `mcp:` servers (e.g. stem's playwright)
	// actually resolve and mount when spawned in a project context. A project with
	// no .rnix/mcp.yaml falls back to the global config (the EchoMatrix case).
	// See investigations/spawn-mcp-not-available-investigation.md.
	var globalMCP *mcp.MCPGlobalConfig
	if gc.MCP != nil {
		if gm, ok := gc.MCP.(*mcp.MCPGlobalConfig); ok {
			globalMCP = gm
		} else {
			// Defensive: globalConfig.MCP should always be *mcp.MCPGlobalConfig
			// (set in cmd/rnix/main.go). A type mismatch would otherwise silently
			// drop global MCP servers — the exact failure class this fix removes —
			// so surface it rather than swallow it.
			log.Printf("[ipc] warning: globalConfig.MCP has unexpected type %T; ignoring global MCP servers", gc.MCP)
		}
	}
	var projectMCP *mcp.MCPGlobalConfig
	projectMCPPath := filepath.Join(projectDir, ".rnix", "mcp.yaml")
	if _, statErr := os.Stat(projectMCPPath); statErr == nil {
		loaded, loadErr := mcp.LoadMCPConfig(projectMCPPath)
		if loadErr != nil {
			return nil, nil, fmt.Errorf("loading project mcp config %s: %w", projectMCPPath, loadErr)
		}
		projectMCP = loaded
	}
	mergedMCP := mcp.MergeGlobalConfig(globalMCP, projectMCP)

	// Create project-aware skill and agent loaders
	projectSkillLoader := skills.NewSkillLoader(skillDirs)
	projectAgentLoader := agents.NewAgentLoader(agentDirs, projectSkillLoader, mergedMCP)

	// Wrap both loaders into opaque types.AgentLoaderFunc / SkillLoaderFunc and
	// stash on ProjectConfig so kernel/tool_exec.go ActionSpawn / ActionSpecialize
	// can prefer the project-aware lookup over k.agentLoader / k.skillLoader.
	// Without this, sub-agent spawn from a project-context process would silently
	// fall back to ~/.config/rnix/agents/ and ignore .rnix/agents/ overrides.
	projCfg.AgentLoader = func(name string) (any, error) {
		return projectAgentLoader.Load(name)
	}
	projCfg.SkillLoader = func(name string) (any, error) {
		return projectSkillLoader.LoadFull(name)
	}

	return projCfg, projectAgentLoader.Load, nil
}
