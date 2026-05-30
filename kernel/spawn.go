package kernel

import (
	gocontext "context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// resolveLLMDevice returns the VFS device path for the LLM provider and its source.
// providerOverride takes precedence over the agent manifest's provider field.
// Returns "/dev/llm/claude" by default when both are empty.
// When hasProvider is non-nil, validates against the DriverRegistry; when nil,
// allows any provider (backward compatibility for tests without resolver injection).
// The source return value indicates where the provider was resolved from:
// "cli", "agent", "project", or "default".
func (k *KernelImpl) resolveLLMDevice(agent *agents.AgentInfo, providerOverride string) (string, string, error) {
	provider := providerOverride
	source := "cli"
	if provider == "" {
		if agent != nil && agent.Manifest.Models.Provider != "" {
			provider = agent.Manifest.Models.Provider
			source = "agent"
		}
	}
	if provider == "" {
		if k.defaultProvider != "" {
			provider = k.defaultProvider
			source = "project"
		} else {
			provider = "claude"
			source = "default"
		}
	}

	if k.hasProvider != nil && !k.hasProvider(provider) {
		available := "none"
		if k.providerNames != nil {
			names := k.providerNames()
			if len(names) > 0 {
				available = strings.Join(names, ", ")
			}
		}
		return "", "", fmt.Errorf("unsupported LLM provider: %q (available: %s)", provider, available)
	}

	return "/dev/llm/" + provider, source, nil
}

// Spawn creates a new agent process that automatically executes the reasonStep loop.
func (k *KernelImpl) Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (types.PID, error) {
	start := time.Now()

	var skillNames []string
	if agent != nil {
		for _, s := range agent.Skills {
			skillNames = append(skillNames, s.Manifest.Name)
		}
	}
	proc := NewProcess(opts.ParentPID, intent, skillNames)
	// Note: proc.Skills may be updated below after stem differentiation (Story 20.3)

	// Set project config snapshot (Story 25.3) — immutable after spawn
	proc.ProjectConfig = opts.ProjectConfig

	// Register per-process WorkDir for VFS path resolution
	if opts.ProjectConfig != nil && opts.ProjectConfig.ProjectDir != "" {
		k.vfs.SetWorkDir(proc.PID, opts.ProjectConfig.ProjectDir)
	}

	// Maintain parent-child tracking
	if opts.ParentPID > 0 {
		parent, ok := k.GetProcess(opts.ParentPID)
		if !ok {
			return 0, NewSyscallError("Spawn", opts.ParentPID, "", fmt.Errorf("parent process %d not found", opts.ParentPID), types.ErrNotFound)
		}
		proc.ParentUUID = parent.UUID
		parent.AddChild(proc.PID)
	}

	// Normalize negative budget to 0 (no limit) before priority resolution
	if opts.ContextBudget < 0 {
		opts.ContextBudget = 0
	}

	// Save original CLI model before agent manifest may override it (P1: model_source tracking)
	cliModel := opts.Model

	// Extract agent instructions (empty if no agent)
	agentInstructions := ""
	if agent != nil {
		agentInstructions, _ = agent.InstructionSections()
	}

	// Load Agent information if specified
	if agent != nil {
		// Stem agent differentiation: auto-match skills based on intent (Story 20.3)
		if agent.Manifest.Name == "stem" && len(agent.Manifest.Skills) == 0 && k.stemMatcher != nil {
			diffStart := time.Now()
			k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: matching skills for intent %q", intent), "")

			// Check differentiation memory first (Story 20.4)
			var matchedSkills []string
			var matchErr error
			var fromMemory bool
			if k.diffMemory != nil {
				if remembered, ok := k.diffMemory.Lookup(intent); ok {
					matchedSkills = remembered
					fromMemory = true
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf(
						"differentiating: reusing remembered path for intent %q: %v", intent, remembered), "")
				}
			}
			// Fallback to keyword matching if no memory hit
			if !fromMemory {
				matchedSkills, matchErr = k.stemMatcher.Match(intent)
				if matchErr != nil {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: match error: %v", matchErr), "")
				}
			}

			if matchErr == nil && len(matchedSkills) > 0 && k.skillLoader != nil {
				var loadedNames []string
				for _, skillName := range matchedSkills {
					skillInfo, loadErr := k.skillLoader(skillName)
					if loadErr == nil {
						agent.Skills = append(agent.Skills, skillInfo)
						loadedNames = append(loadedNames, skillName)
					}
				}
				if len(loadedNames) > 0 {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: loading skills %v", loadedNames), "")
					// Update proc.Skills so ps/ProcInfo reflects differentiated skills
					proc.Skills = loadedNames

					// Record initial differentiation lineage (Story 20.5)
					if proc.lineage == nil {
						proc.lineage = NewLineage()
					}
					proc.lineage.Record(LineageEvent{
						Timestamp:  time.Now(),
						Phase:      "initial",
						Skills:     loadedNames,
						Trigger:    intent,
						FromMemory: fromMemory,
					})
				}

				// Record differentiation path to memory (Story 20.4)
				if k.diffMemory != nil && len(loadedNames) > 0 {
					k.diffMemory.Record(intent, loadedNames)
				}
			}
			diffDuration := time.Since(diffStart)
			eventArgs := map[string]any{
				"matched_skills": matchedSkills,
				"duration_ms":    diffDuration.Milliseconds(),
				"from_memory":    fromMemory,
			}
			var eventErr error
			if matchErr != nil {
				eventErr = matchErr
			}
			k.emitEvent(proc, "StemDifferentiate", eventArgs, nil, eventErr, diffDuration)
		}

		// Populate skill body + dir maps for loaded_skills section and bundle drivers
		skillBodies := make(map[string]string)
		skillDirs := make(map[string]string)
		for _, s := range agent.Skills {
			if s.Body != "" {
				body := s.Body
				if s.Dir != "" {
					body = "Base directory for this skill: " + s.Dir + "\n\n" + body
				}
				skillBodies[s.Manifest.Name] = body
				if s.Dir != "" {
					skillDirs[s.Manifest.Name] = s.Dir
				}
			}
		}
		proc.SkillBodies = skillBodies
		proc.SkillDirs = skillDirs

		// Aggregate AllowedTools from all Skills
		proc.AllowedDevices = agent.AllowedTools()

		// Populate deferred skill metadata for discover_skill scoring
		if len(agent.DeferredSkills) > 0 {
			for _, ds := range agent.DeferredSkills {
				proc.DeferredSkills = append(proc.DeferredSkills, DeferredSkillMeta{
					Name:        ds.Manifest.Name,
					Description: ds.Manifest.Description,
					SearchHint:  ds.Manifest.SearchHint,
				})
			}
		}

		// Planning configuration: nil (default) = true, explicit false = disabled
		if agent.Manifest.Planning != nil && !*agent.Manifest.Planning {
			proc.PlanningEnabled = false
		}

		// Language preference from agent manifest
		if agent.Manifest.Language != "" {
			proc.Language = agent.Manifest.Language
		}

		// Model selection priority: CLI --model > Agent manifest > driver default
		if opts.Model == "" && agent.Manifest.Models.Preferred != "" {
			opts.Model = agent.Manifest.Models.Preferred
		}

		// Budget priority: opts (CLI/Compose) > agent manifest > 0 (no limit)
		if opts.ContextBudget == 0 && agent.Manifest.ContextBudget > 0 {
			opts.ContextBudget = agent.Manifest.ContextBudget
		}

		// CtxSize priority: opts (CLI/Compose) > agent manifest > DefaultCtxSize
		if opts.CtxSize == 0 && agent.Manifest.CtxSize > 0 {
			opts.CtxSize = agent.Manifest.CtxSize
		}

		// Fallback configuration (Story 23.5; CLI override per spec
		// `anthropic-thinking-and-cli-fallback`):
		//
		// Priority for the resolved fallback model/provider:
		//   1. opts.FallbackModel / opts.FallbackProvider (CLI flags)
		//   2. cross-provider auto-disable: when CLI overrides --provider
		//      to something different from the agent manifest's primary
		//      provider AND no CLI fallback flags are given, suppress the
		//      manifest fallback entirely (its model almost certainly
		//      doesn't exist on the new provider — see rnix-eval feedback).
		//   3. agent manifest fallback
		fbModel := agent.Manifest.Models.Fallback
		fbProvider := agent.Manifest.Models.FallbackProvider

		// CLI flags override agent manifest, independently for each.
		if opts.FallbackModel != "" {
			fbModel = opts.FallbackModel
		}
		if opts.FallbackProvider != "" {
			fbProvider = opts.FallbackProvider
		}

		// Cross-provider auto-disable: when CLI overrides --provider AND
		// the user gave NEITHER fallback flag AND the agent has a manifest
		// fallback configured, suppress the manifest fallback entirely
		// (its model almost certainly doesn't exist on the new provider —
		// see rnix-eval feedback).
		//
		// We do NOT condition on agent.Manifest.Models.Provider being set:
		// if the manifest has only `fallback:` (no explicit primary), the
		// fallback model is still tied to its natural home provider, which
		// is unlikely to match a CLI --provider override.
		autoDisabled := opts.FallbackModel == "" && opts.FallbackProvider == "" &&
			opts.Provider != "" && agent.Manifest.Models.Fallback != "" &&
			opts.Provider != agent.Manifest.Models.Provider
		if autoDisabled {
			fbModel = ""
			fbProvider = ""
		}

		if fbModel != "" {
			proc.FallbackModel = fbModel
			if fbProvider == "" {
				// Same-provider fallback: resolve using main provider
				p := opts.Provider
				if p == "" {
					p = agent.Manifest.Models.Provider
				}
				if p == "" {
					p = "claude"
				}
				fbProvider = p
			}
			proc.FallbackProvider = fbProvider
			fbDevice, _, fbErr := k.resolveLLMDevice(nil, fbProvider)
			if fbErr == nil {
				proc.FallbackDevice = fbDevice
			} else {
				// fallback resolution failure is non-blocking; means fallback
				// unavailable. Log a warn so operators can see why fallback
				// silently does nothing on the next reasonStep.
				log.Printf("[kernel] spawn: fallback provider %q resolve failed: %v (fallback disabled for this process)", fbProvider, fbErr)
			}
		}

		if autoDisabled {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":             0,
				"action":           "fallback_auto_disabled",
				"cli_provider":     opts.Provider,
				"agent_provider":   agent.Manifest.Models.Provider,
				"agent_fallback":   agent.Manifest.Models.Fallback,
				"agent_fallback_p": agent.Manifest.Models.FallbackProvider,
			}, nil, nil, 0)
		}
	}

	// System prompt = caller-provided prompt + agent instructions (skill bodies handled by loaded_skills section per AC4)
	if agentInstructions != "" {
		if opts.SystemPrompt == "" {
			opts.SystemPrompt = agentInstructions
		} else {
			opts.SystemPrompt = opts.SystemPrompt + "\n\n" + agentInstructions
		}
	}

	// Register section-based prompt assembly (all processes, regardless of agent)
	proc.HasSections = true
	proc.sections = registerSections(proc, k, opts.SystemPrompt)

	proc.ContextBudget = opts.ContextBudget

	// ProcessBudget priority: opts (CLI/Compose) > agent manifest > 0 (unlimited)
	if opts.MaxTokens < 0 {
		opts.MaxTokens = 0
	}
	if opts.MaxCost < 0 {
		opts.MaxCost = 0
	}
	if opts.MaxTokens == 0 && agent != nil && agent.Manifest.MaxTokens > 0 {
		opts.MaxTokens = agent.Manifest.MaxTokens
	}
	if opts.MaxCost == 0 && agent != nil && agent.Manifest.MaxCost > 0 {
		opts.MaxCost = agent.Manifest.MaxCost
	}
	proc.mu.Lock()
	proc.Budget = ProcessBudget{
		MaxTokens: opts.MaxTokens,
		MaxCost:   opts.MaxCost,
	}
	proc.mu.Unlock()

	// MaxSteps priority: CLI --max-steps > agent manifest max_steps > DefaultMaxSteps
	maxStepsForProc := DefaultMaxSteps
	if agent != nil && agent.Manifest.MaxSteps > 0 {
		maxStepsForProc = agent.Manifest.MaxSteps
	}
	if opts.MaxTurns > 0 {
		maxStepsForProc = opts.MaxTurns
	}
	proc.MaxSteps = maxStepsForProc

	// StepTimeout priority: CLI opts > agent manifest step_timeout > default 5 minutes
	// 0 explicitly disables timeout detection
	stepTimeout := 5 * time.Minute
	if opts.StepTimeout > 0 {
		stepTimeout = opts.StepTimeout
	} else if agent != nil && agent.Manifest.StepTimeout != "" {
		if d, err := time.ParseDuration(agent.Manifest.StepTimeout); err == nil {
			stepTimeout = d // includes 0 = disabled
		}
	}
	proc.mu.Lock()
	proc.StepTimeout = stepTimeout
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()

	// CompactThreshold: opts > default (80%)
	if opts.CompactThreshold > 0 {
		proc.CompactThreshold = opts.CompactThreshold
	}

	// GracePeriod: opts > default (10s via effectiveGracePeriod)
	if opts.GracePeriod > 0 {
		proc.GracePeriod = opts.GracePeriod
	}

	// Orchestration metadata (Story 34.7)
	proc.ComposeNode = opts.ComposeNode
	proc.ComposeDeps = opts.ComposeDeps
	proc.PipelineIndex = opts.PipelineIndex
	proc.PipelineTotal = opts.PipelineTotal

	if opts.TraceID != "" {
		proc.TraceID = opts.TraceID
		proc.ParentSpanID = opts.ParentSpanID
	} else if opts.ParentPID == 0 {
		proc.TraceID = debug.GenerateTraceID()
	}
	if proc.TraceID != "" && proc.SpanID == "" {
		proc.SpanID = debug.GenerateSpanID()
		if k.spanRecorder != nil {
			k.spanRecorder.StartSpan(proc.PID, proc.TraceID, proc.SpanID, proc.ParentSpanID, intent)
		}
	}

	var cid types.CtxID
	var llmFD types.FD

	if opts.PreallocatedCtxID != 0 {
		// Use pre-allocated context (fork-continue path)
		cid = opts.PreallocatedCtxID
		proc.CtxID = cid
		proc.CtxSize = DefaultCtxSize
	} else {
		// Allocate context
		effectiveCtxSize := DefaultCtxSize
		if opts.CtxSize > 0 {
			effectiveCtxSize = opts.CtxSize
		}
		proc.CtxSize = effectiveCtxSize
		ctxAllocStart := time.Now()
		var err error
		cid, err = k.ctxMgr.CtxAlloc(effectiveCtxSize)
		k.emitEvent(proc, "CtxAlloc", map[string]any{
			"size": effectiveCtxSize,
		}, cid, err, time.Since(ctxAllocStart))
		if err != nil {
			return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
		}
		proc.CtxID = cid

		// Set system prompt if provided
		if opts.SystemPrompt != "" {
			setPromptStart := time.Now()
			if err := k.ctxMgr.SetSystemPrompt(cid, opts.SystemPrompt); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid": cid,
					"op":  "SetSystemPrompt",
				}, nil, err, time.Since(setPromptStart))
				_ = k.ctxMgr.CtxFree(cid)
				return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid": cid,
				"op":  "SetSystemPrompt",
			}, nil, nil, time.Since(setPromptStart))
		}

		// Attach section registry to context when available
		if proc.HasSections && proc.sections != nil {
			if err := k.ctxMgr.SetSections(cid, proc.sections); err != nil {
				_ = k.ctxMgr.CtxFree(cid)
				return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
			}
		}

		// Append initial intent as user message
		appendStart := time.Now()
		if err := k.ctxMgr.AppendMessage(cid, rnixctx.RoleUser, intent); err != nil {
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  cid,
				"op":   "AppendMessage",
				"role": string(rnixctx.RoleUser),
			}, nil, err, time.Since(appendStart))
			_ = k.ctxMgr.CtxFree(cid)
			return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
		}
		k.emitEvent(proc, "CtxWrite", map[string]any{
			"cid":  cid,
			"op":   "AppendMessage",
			"role": string(rnixctx.RoleUser),
		}, nil, nil, time.Since(appendStart))
	}

	if !opts.SkipReasonLoop {
		resolveStart := time.Now()

		// Use project-level default provider if available and no explicit override
		providerOverride := opts.Provider
		providerOverrideFromProject := false
		if providerOverride == "" && agent != nil && agent.Manifest.Models.Provider == "" &&
			opts.ProjectConfig != nil && opts.ProjectConfig.DefaultProvider != "" {
			providerOverride = opts.ProjectConfig.DefaultProvider
			providerOverrideFromProject = true
		}

		// Resolve LLM device path based on agent provider
		// When project config provides an LLMFileOpener, skip global provider validation
		// since the provider may only exist at project level
		var llmDevice string
		var providerSource string
		var resolveErr error
		if opts.ProjectConfig != nil && opts.ProjectConfig.LLMFileOpener != nil {
			// Build device path without global validation
			// Use opts.Provider (actual CLI value), not providerOverride which may
			// have been pre-resolved from ProjectConfig.DefaultProvider
			provider := opts.Provider
			providerSource = "cli"
			if provider == "" {
				if agent != nil && agent.Manifest.Models.Provider != "" {
					provider = agent.Manifest.Models.Provider
					providerSource = "agent"
				}
			}
			if provider == "" {
				if opts.ProjectConfig.DefaultProvider != "" {
					provider = opts.ProjectConfig.DefaultProvider
					providerSource = "project"
				} else if k.defaultProvider != "" {
					provider = k.defaultProvider
					providerSource = "project"
				} else {
					provider = "claude"
					providerSource = "default"
				}
			}
			llmDevice = "/dev/llm/" + provider
		} else {
			llmDevice, providerSource, resolveErr = k.resolveLLMDevice(agent, providerOverride)
			if providerOverrideFromProject && providerSource == "cli" {
				providerSource = "project"
			}
		}
		if resolveErr != nil {
			if opts.PreallocatedCtxID == 0 {
				_ = k.ctxMgr.CtxFree(cid)
			}
			return 0, NewSyscallError("Spawn", proc.PID, "", resolveErr, types.ErrDriver)
		}
		proc.PrimaryDevice = llmDevice // Store for fallback reference (Story 23.5)
		proc.Provider = strings.TrimPrefix(llmDevice, "/dev/llm/")
		proc.Model = opts.Model

		// Open LLM device via VFS (or project-level override)
		openStart := time.Now()
		var err error
		if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
			// Try project-level LLM device first
			providerName := strings.TrimPrefix(llmDevice, "/dev/llm/")
			file, openErr := proc.ProjectConfig.LLMFileOpener(providerName, int(vfs.O_RDWR))
			if openErr == nil {
				if vfsFile, ok := file.(vfs.VFSFile); ok {
					llmFD = k.vfs.RegisterFD(proc.PID, vfsFile)
				} else {
					log.Printf("[kernel] warning: LLMFileOpener returned non-VFSFile type %T for provider %q, falling back to global VFS", file, providerName)
					llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
				}
			} else {
				// Project opener failed — fallback to global VFS
				llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
			}
		} else {
			llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
		}
		k.emitEvent(proc, "Open", map[string]any{
			"path":  llmDevice,
			"flags": vfs.O_RDWR,
		}, llmFD, err, time.Since(openStart))
		if err != nil {
			if opts.PreallocatedCtxID == 0 {
				_ = k.ctxMgr.CtxFree(cid)
			}
			return 0, NewSyscallError("Spawn", proc.PID, llmDevice, err, types.ErrDriver)
		}
		proc.FDTable[llmFD] = nil // VFS manages actual file internally; tracks FD existence for process inspection

		// Backfill Process.Model from driver's default when no explicit model was specified.
		// This ensures proc-info.json and dashboard always show the actual model used.
		if proc.Model == "" {
			if file := k.vfs.GetFile(proc.PID, llmFD); file != nil {
				if mip, ok := file.(vfs.ModelInfoProvider); ok {
					proc.Model = mip.DefaultModel()
				}
			}
		}

		if k.contextWindowFunc != nil {
			proc.ContextWindow = k.contextWindowFunc(proc.Provider, proc.Model)
		}

		// Determine model source: CLI --model > agent manifest preferred > driver default
		modelSource := "driver"
		if cliModel != "" {
			modelSource = "cli"
		} else if opts.Model != "" {
			modelSource = "agent"
		}

		// Emit ConfigResolve strace event (after Open so proc.Model is resolved)
		configArgs := map[string]any{
			"provider":        proc.Provider,
			"provider_source": providerSource,
			"model":           proc.Model,
			"model_source":    modelSource,
		}
		projectDefault := k.defaultProvider
		if opts.ProjectConfig != nil && opts.ProjectConfig.DefaultProvider != "" {
			projectDefault = opts.ProjectConfig.DefaultProvider
		}
		if projectDefault != "" && projectDefault != proc.Provider {
			configArgs["project_default"] = projectDefault
		}
		k.emitEvent(proc, "ConfigResolve", configArgs, nil, nil, time.Since(resolveStart))

		if providerSource == "agent" && opts.ProjectConfig != nil &&
			opts.ProjectConfig.DefaultProvider != "" &&
			opts.ProjectConfig.DefaultProvider != proc.Provider {
			k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf(
				"warning: agent %q manifest provider %q overrides project default_provider %q (pass --provider to choose explicitly, or clear models.provider in the agent manifest)",
				agent.Manifest.Name, proc.Provider, opts.ProjectConfig.DefaultProvider), "")
		}

		// Set up stream handler for driver events (tool_call, thinking, system, etc.)
		k.setupDriverStreamHandler(proc, llmFD)

		// Auto-mount MCP servers referenced in agent.yaml (Story 9.2)
		if agent != nil && len(agent.MCPConfigs) > 0 {
			if k.mountMgr == nil {
				_ = k.vfs.CloseAll(proc.PID)
				if opts.PreallocatedCtxID == 0 {
					_ = k.ctxMgr.CtxFree(cid)
				}
				return 0, NewSyscallError("Spawn", proc.PID, "",
					fmt.Errorf("mount manager not initialized but agent requires MCP servers"), types.ErrInternal)
			}
			var mountedPaths []string
			for _, mcpCfg := range agent.MCPConfigs {
				mountPath := fmt.Sprintf("/mnt/mcp/%d-%s", proc.PID, mcpCfg.ServerName)
				// mcpCfg is a value copy from the slice — safe to mutate without affecting agent.MCPConfigs
				if opts.ProjectConfig != nil && opts.ProjectConfig.ProjectDir != "" {
					mcpCfg.WorkDir = opts.ProjectConfig.ProjectDir
				}
				mountStart := time.Now()
				if err := k.mountMgr.Mount(mountPath, mcpCfg); err != nil {
					k.emitEvent(proc, "Mount", map[string]any{
						"path": mountPath,
						"auto": true,
					}, nil, err, time.Since(mountStart))
					for _, p := range mountedPaths {
						_ = k.mountMgr.Unmount(p)
					}
					_ = k.vfs.CloseAll(proc.PID)
					if opts.PreallocatedCtxID == 0 {
						_ = k.ctxMgr.CtxFree(cid)
					}
					return 0, NewSyscallError("Spawn", proc.PID, mountPath,
						fmt.Errorf("auto-mount mcp %q failed: %w", mcpCfg.ServerName, err), types.ErrDriver)
				}
				k.emitEvent(proc, "Mount", map[string]any{
					"path": mountPath,
					"auto": true,
				}, nil, nil, time.Since(mountStart))
				mountedPaths = append(mountedPaths, mountPath)
			}
			proc.mu.Lock()
			proc.MCPMounts = mountedPaths
			proc.AllowedDevices = append(proc.AllowedDevices, mountedPaths...)
			proc.mcpConfigs = agent.MCPConfigs
			proc.mu.Unlock()

			// 路线 B: now that the MCP transports are mounted and live, expose
			// each server's tools as native ToolDefs. buildToolDefs (run inside
			// setupDriverStreamHandler above, BEFORE this mount loop) skips MCP
			// devices, so this is where the LLM gains the mcp__<server>__<tool>
			// tools + toolMap routing.
			k.attachMCPToolDefs(proc)
		}
	}

	// Set up goroutine context for cancellation
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	ctx = ContextWithPID(ctx, proc.PID)
	proc.cancel = cancel
	proc.ctx = ctx

	// Register process in table
	k.AddProcess(proc)

	// Initialize IPC message queue for the new process (Story 6.1)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	// D4 (Story 43.2): invoke EventWriterFactory BEFORE the Spawn event
	// emit so the Spawn event itself lands in the process's events.jsonl.
	// Factory failure is logged but non-fatal — Spawn proceeds without a
	// writer (degrades trace events to no-ops via the `ew != nil` gate in
	// emitEvent).
	if opts.EventWriterFactory != nil {
		if ew, ewErr := opts.EventWriterFactory(proc); ewErr != nil {
			log.Printf("[spawn] EventWriterFactory failed pid=%d uuid=%s: %v",
				proc.PID, proc.UUID, ewErr)
		} else if ew != nil {
			proc.AttachEventWriter(ew)
		}
	}

	// Emit Spawn syscall event
	spawnArgs := map[string]any{
		"intent": intent,
	}
	if agent != nil {
		spawnArgs["agent"] = agent.Manifest.Name
		spawnArgs["skills"] = skillNames
		spawnArgs["allowed_devices"] = proc.AllowedDevices
		if len(proc.MCPMounts) > 0 {
			spawnArgs["mcp_mounts"] = proc.MCPMounts
		}
	}
	if opts.SkipReasonLoop {
		spawnArgs["skip_reason_loop"] = true
	}
	k.emitEvent(proc, "Spawn", spawnArgs, proc.PID, nil, time.Since(start))

	// Capture FinalSystemPrompt eagerly at spawn time so process-meta.json
	// (written at reap) always has a non-empty `system_prompt` field —
	// independent of whether the reasonStep goroutine reaches the
	// first-step assignment at kernel/reason.go:368-372. The reasonStep-
	// side guard (`if FinalSystemPrompt == ""`) keeps this value pinned.
	if proc.HasSections && proc.sections != nil {
		built := proc.sections.Build()
		proc.mu.Lock()
		if proc.FinalSystemPrompt == "" {
			proc.FinalSystemPrompt = built
		}
		proc.mu.Unlock()
	} else if opts.SystemPrompt != "" {
		proc.mu.Lock()
		if proc.FinalSystemPrompt == "" {
			proc.FinalSystemPrompt = opts.SystemPrompt
		}
		proc.mu.Unlock()
	}

	if opts.SkipReasonLoop {
		// No reasoning goroutine — process starts in Running state immediately
		_ = proc.Start() // Created → Running
		// Epic 42 fix: persist proc-info.json immediately so daemon crash
		// recovery / `rnix ps --resumable` sees the process from step 0.
		k.persistInitialProcInfo(proc)
	} else {
		// Launch reasoning goroutine
		// Note: CtxFree deferred to Wait/Reap (Story 4.1) per resource release order
		proc.wg.Go(func() {
			defer func() { _ = k.vfs.CloseAll(proc.PID) }()
			_ = proc.Start() // Created → Running
			// Epic 42 fix: persist proc-info.json once on Running transition so
			// short processes (< 5 steps / < 30 s) that die before the first
			// checkpoint are still recoverable from `rnix ps --resumable`.
			k.persistInitialProcInfo(proc)
			k.reasonStep(proc, llmFD, opts)
		})
	}

	// Notify callback after process is registered and goroutine launched
	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, intent, proc.Provider, proc.Model, proc.UUID)
	}

	// Notify immune daemon about new process (Story 22.1)
	if k.immuneDaemon != nil {
		agentName := ""
		if agent != nil {
			agentName = agent.Manifest.Name
		}
		k.immuneDaemon.OnProcessStart(proc.PID, agentName)

		// Cooperation topology feed: record parent→child spawn edge (Story 51.2 / EM-3).
		// Parent collector was registered at the parent's earlier OnProcessStart.
		// Skip if either template resolves to "" (ad-hoc intent spawn / unregistered PID).
		if opts.ParentPID > 0 {
			parentTemplate := k.immuneDaemon.AgentTemplateForPID(opts.ParentPID)
			if parentTemplate != "" && agentName != "" {
				k.immuneDaemon.RecordCooperationTyped(parentTemplate, agentName, "spawn")
			}
		}

		// Similarity matrix feed: record agent template→skills mapping (Story 51.3 / EM-2).
		// Accumulates templates and triggers full matrix recomputation on each spawn.
		// Empty agentName / empty skills skip (ad-hoc spawn guard).
		if agentName != "" && len(proc.Skills) > 0 {
			k.immuneDaemon.RecordAgentSkills(agentName, proc.Skills)
		}
	}

	return proc.PID, nil
}

// persistInitialProcInfo writes a best-effort proc-info.json snapshot the
// instant a freshly spawned process enters Running state. Without this, short
// processes that die before the first asyncWriteCheckpoint (5 steps / 30 s)
// leave no on-disk snapshot, defeating daemon-crash recovery and `rnix ps
// --resumable` (Epic 42 fix).
//
// Errors are logged but never propagated — spawn must not fail due to a stale
// disk snapshot, and the next checkpoint / reap will rewrite the file anyway.
func (k *KernelImpl) persistInitialProcInfo(proc *Process) {
	if k.stepDataDir == "" {
		return
	}
	info, err := k.GetProcInfo(proc.PID)
	if err != nil || info == nil {
		return
	}
	if saveErr := SaveProcInfo(k.stepDataDir, *info); saveErr != nil {
		log.Printf("[spawn] pid=%d persistInitialProcInfo failed: %v", proc.PID, saveErr)
	}
}
