package kernel

import (
	gocontext "context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
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

// resolveFallbackDevice resolves the VFS device path for a fallback provider,
// honoring project-level providers the same way the primary path does (Story 66.4).
//
// When the process carries a project config with an LLMFileOpener AND the
// fallback provider appears in the merged providers view (global + project
// .rnix/providers.yaml, already assembled by ipc/server_spawn.go's
// resolveProjectContext and attached to ProjectConfig), the device path is
// built directly — bypassing the daemon-global DriverRegistry validation that
// resolveLLMDevice applies. This is the fallback analogue of the primary
// LLMFileOpener bypass in Spawn (see the `LLMFileOpener != nil` branch).
//
// Otherwise it delegates to resolveLLMDevice(nil, fbProvider), preserving the
// original global-validation semantics: a globally-registered provider still
// resolves, and a genuinely-unknown provider still returns the original
// "unsupported LLM provider" error. The project name list only ADDS a path; it
// never narrows the global one.
//
// Unlike resolveResumeLLMDevice's unconditional bypass of the primary device,
// this validates against the merged name list rather than admitting any
// provider: fallback resolution failure is silently non-blocking (fallback
// disabled), so a typo'd fallback provider admitted to runtime would re-create
// the very "fallback silently does nothing" bug this story fixes. The kernel
// MUST NOT re-read providers.yaml — it only consumes the ProjectConfig material
// assembled by the IPC layer (epic red line: no double-read drift).
func (k *KernelImpl) resolveFallbackDevice(pc *config.ProjectConfig, fbProvider string) (string, error) {
	if pc != nil && pc.LLMFileOpener != nil && projectHasProvider(pc, fbProvider) {
		return "/dev/llm/" + fbProvider, nil
	}
	device, _, err := k.resolveLLMDevice(nil, fbProvider)
	return device, err
}

// projectHasProvider reports whether name appears in the project's merged
// providers view. ProjectConfig.Providers is typed `any` to avoid a
// config→drivers/llm import cycle; the kernel legitimately imports drivers/llm
// (action.go), so the type-assert is safe here (assert precedent:
// ipc/provider_lookup.go). A failed assert or nil Providers is treated as a
// list miss (→ global fallthrough), never a panic.
func projectHasProvider(pc *config.ProjectConfig, name string) bool {
	if pc == nil || name == "" {
		return false
	}
	pcfg, ok := pc.Providers.(*llm.ProvidersConfig)
	if !ok || pcfg == nil {
		return false
	}
	for i := range pcfg.Providers {
		if pcfg.Providers[i].Name == name {
			return true
		}
	}
	return false
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

	// Copy kernel-level feature flags to process (Story 52.2)
	proc.FeatureFlags = k.featureFlags
	if !proc.FeatureFlags.Compaction {
		proc.CompactionDisabled = true
	}

	// Spawn-recursion guard: reject if depth exceeds cap. ActionSpawn checks
	// this earlier in tool_exec.go (with LLM guidance); this is the backstop
	// for intent / compose / other spawn paths that bypass tool_exec.
	proc.Depth = opts.Depth
	if opts.Depth > MaxSpawnDepth {
		return 0, fmt.Errorf("spawn rejected: depth %d exceeds maximum %d", opts.Depth, MaxSpawnDepth)
	}

	// Device deny-list: intent-spawned children are blocked from /dev/intent
	// to prevent recursive decomposition chains.
	proc.DeniedDevices = opts.DeniedDevices

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
		proc.AgentTemplate = agent.Manifest.Name

		// Stem agent differentiation: auto-match skills based on intent (Story 20.3)
		if agent.Manifest.Name == "stem" && len(agent.Manifest.Skills) == 0 && k.stemMatcher != nil {
			diffStart := time.Now()
			k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: matching skills for intent %q", intent), "")

			// Build a project-aware stem matcher when project skill dirs are available.
			// The kernel-level stemMatcher only scans global dirs (~/.config/rnix/skills);
			// per-spawn project dirs (.rnix/skills/) require a temporary matcher.
			effectiveMatcher := k.stemMatcher
			if opts.ProjectConfig != nil && len(opts.ProjectConfig.SkillDirs) > 0 {
				projLoader := skills.NewSkillLoader(opts.ProjectConfig.SkillDirs)
				projDiscovery := skills.NewSkillDiscovery(projLoader, opts.ProjectConfig.SkillDirs)
				effectiveMatcher = NewStemMatcher(projDiscovery)
			}

			// Check differentiation memory first (Story 20.4)
			var matchResults []StemMatchResult
			var matchedNames []string
			var matchErr error
			var fromMemory bool
			var availableSkillCount int
			if k.diffMemory != nil {
				if remembered, ok := k.diffMemory.Lookup(intent, 0); ok {
					matchedNames = remembered
					for _, name := range remembered {
						matchResults = append(matchResults, StemMatchResult{Name: name, Score: -1})
					}
					fromMemory = true
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf(
						"differentiating: reusing remembered path for intent %q: %v", intent, remembered), "")
				}
			}
			// Fallback to keyword matching if no memory hit
			if !fromMemory {
				if ac, acErr := effectiveMatcher.AvailableCount(); acErr == nil {
					availableSkillCount = ac
				}
				matchResults, matchErr = effectiveMatcher.MatchWithScores(intent)
				if matchErr != nil {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: match error: %v", matchErr), "")
				}
				matchedNames = make([]string, len(matchResults))
				for i, mr := range matchResults {
					matchedNames[i] = mr.Name
				}
			}

			// Story 51.4 AR-1+EM-1: rerank matched skills using synergy/reputation data.
			if matchErr == nil && len(matchResults) > 1 && !fromMemory &&
				(k.synergyMatrixForStem != nil || k.reputationStoreForStem != nil) {
				skipReRank := k.stemEpsilon >= 1.0
				if !skipReRank && k.stemEpsilon > 0 {
					skipReRank = rand.Float64() < k.stemEpsilon
				}
				if !skipReRank {
					matchResults = reRankSkills(
						matchResults,
						agent.Manifest.Name,
						k.synergyMatrixForStem,
						k.reputationStoreForStem,
						k.stemReRankWeights,
					)
					matchedNames = make([]string, len(matchResults))
					for i, mr := range matchResults {
						matchedNames[i] = mr.Name
					}
				}
			}

			effectiveSkillLoader := k.skillLoader
			if opts.ProjectConfig != nil && opts.ProjectConfig.SkillLoader != nil {
				effectiveSkillLoader = func(name string) (*skills.SkillInfo, error) {
					result, err := opts.ProjectConfig.SkillLoader(name)
					if err != nil {
						return nil, err
					}
					si, ok := result.(*skills.SkillInfo)
					if !ok {
						return nil, fmt.Errorf("skill loader returned unexpected type")
					}
					return si, nil
				}
			}
			if matchErr == nil && len(matchedNames) > 0 && effectiveSkillLoader != nil {
				var loadedNames []string
				for _, skillName := range matchedNames {
					skillInfo, loadErr := effectiveSkillLoader(skillName)
					if loadErr == nil {
						agent.Skills = append(agent.Skills, skillInfo)
						loadedNames = append(loadedNames, skillName)
					}
				}
				if len(loadedNames) > 0 {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: loading skills %v", loadedNames), "")
					proc.Skills = loadedNames
					proc.StemMatches = matchResults
					proc.StemSelected = loadedNames
					proc.StemFromMemory = fromMemory

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

				if k.diffMemory != nil && len(loadedNames) > 0 && !fromMemory {
					k.diffMemory.Record(intent, loadedNames, availableSkillCount)
				}
			}
			diffDuration := time.Since(diffStart)
			eventArgs := map[string]any{
				"matched_skills": matchedNames,
				"duration_ms":    diffDuration.Milliseconds(),
				"from_memory":    fromMemory,
			}
			var eventErr error
			if matchErr != nil {
				eventErr = matchErr
			}
			k.emitEvent(proc, "StemDifferentiate", eventArgs, nil, eventErr, diffDuration)

			// Fire callback for CLI progress display
			if k.callbacks != nil && len(proc.StemMatches) > 0 {
				k.callbacks.OnStemDiff(proc.PID, proc.StemMatches, proc.StemSelected, fromMemory)
			}
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

		// Aggregate AllowedTools from all Skills. Story 54.5: a skill/agent may now
		// declare semantic tool names ("Read", "Bash", "IntentDecompose", …) instead
		// of device paths. Normalize the declared values into the device ROOTS they
		// resolve to (→ AllowedDevices, for routing / buildToolDefs / parent-constraint
		// intersection) and the authoritative tool names (→ AllowedTools, set just
		// below). Legacy device-path declarations still pass through unchanged.
		agentAllowed := agent.AllowedTools()
		agentTools, agentDevices := k.normalizeDeclaredAllowedTools(agentAllowed)
		if len(opts.AllowedDevices) > 0 && len(agentDevices) > 0 {
			inter := intersectDevices(opts.AllowedDevices, agentDevices)
			if len(inter) == 0 {
				return 0, fmt.Errorf("spawn: agent %q allowed devices %v have no overlap with parent constraint %v",
					agent.Manifest.Name, agentDevices, opts.AllowedDevices)
			}
			proc.AllowedDevices = inter
		} else if len(opts.AllowedDevices) > 0 {
			proc.AllowedDevices = append([]string(nil), opts.AllowedDevices...)
		} else {
			proc.AllowedDevices = agentDevices
		}
		// Story 54.5: record the declared tool names as the authoritative whitelist.
		// The post-derivation block below narrows these to the finalized
		// AllowedDevices (a parent device constraint dropping /dev/shell also drops
		// Bash — AC2) and to any opts.AllowedTools tool-level parent constraint
		// (AC8.4), while preserving a declared subset (never re-expanded to the
		// device's full tool set).
		proc.AllowedTools = agentTools

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
			proc.FeatureFlags.Planning = false
		}

		// Project-doc injection: nil (default) = enabled, explicit false = disabled (Story 35.7)
		if agent.Manifest.ProjectDoc != nil && !*agent.Manifest.ProjectDoc {
			proc.ProjectDocInjection = false
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
				// Same-provider fallback: resolve the implicit provider using the
				// SAME chain as the primary LLMFileOpener branch (the
				// `LLMFileOpener != nil` primary block below) so that when primary
				// is a project-level default_provider (case-file `reclaude`),
				// same-provider fallback resolves to it too instead of wrongly
				// defaulting to "claude" (Story 66.4 Task 2).
				p := opts.Provider
				if p == "" && agent.Manifest.Models.Provider != "" {
					p = agent.Manifest.Models.Provider
				}
				if p == "" {
					if opts.ProjectConfig != nil && opts.ProjectConfig.DefaultProvider != "" {
						p = opts.ProjectConfig.DefaultProvider
					} else if k.defaultProvider != "" {
						p = k.defaultProvider
					} else {
						p = "claude"
					}
				}
				fbProvider = p
			}
			proc.FallbackProvider = fbProvider
			// Story 66.4 Task 1: resolve through resolveFallbackDevice so a
			// project-level fallback provider (present only in the merged
			// providers view, not the daemon-global DriverRegistry) is honored,
			// matching the primary path. Falls through to global validation when
			// there is no project-list hit.
			fbDevice, fbErr := k.resolveFallbackDevice(opts.ProjectConfig, fbProvider)
			if fbErr == nil {
				proc.FallbackDevice = fbDevice
			} else {
				// fallback resolution failure is non-blocking; means fallback
				// unavailable. Store the reason (Story 66.4 Task 3) so it surfaces
				// via the spawn ProgressPayload warning + a delayed events.jsonl
				// event, and keep the existing warn log for operators.
				proc.FallbackResolveError = fbErr.Error()
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

	// Inherit parent AllowedDevices when no agent is set
	if agent == nil && len(opts.AllowedDevices) > 0 {
		proc.AllowedDevices = append([]string(nil), opts.AllowedDevices...)
	}

	// Story 54.1/54.5: finalize the authoritative tool-name whitelist. This point
	// follows both the agent-aggregation branch above (which already set
	// proc.AllowedTools from the agent's normalized declaration) and the no-agent
	// inherit branch (which leaves it nil), so it covers every spawn shape.
	//   - proc.AllowedTools == nil → no agent declaration: derive it from the
	//     finalized AllowedDevices (no-agent inherit / legacy device-path expansion),
	//     keeping the child tool set ⊆ parent automatically.
	//   - proc.AllowedTools != nil → agent declared tool names (Story 54.5): narrow
	//     them to the finalized AllowedDevices so a parent device constraint that
	//     dropped a device also drops its tools (AC2), while preserving a declared
	//     subset (intersection never re-expands to the device's full tool set).
	// An explicit opts.AllowedTools (a tool-level parent constraint, e.g. an
	// ActionSpawn child) then narrows further (AC8.4). MCP mounts appended later
	// expose dynamic tool names and contribute no base tools here, so finalizing
	// before that append is correct. Guard the DeviceRegistry lookup: SkipReasonLoop
	// script-runner spawns use a nil VFS and carry no device whitelist.
	if len(proc.AllowedDevices) > 0 || len(opts.AllowedTools) > 0 {
		var deviceTools []string
		if len(proc.AllowedDevices) > 0 && k.vfs != nil {
			deviceTools = expandDevicesToTools(k.vfs.DeviceRegistry(), proc.AllowedDevices)
		}
		switch {
		case proc.AllowedTools == nil:
			proc.AllowedTools = deviceTools
		case len(deviceTools) > 0:
			// Narrow agent-declared tools to the finalized device set. Meta tool
			// names (no device backing) are absent from deviceTools and only get
			// narrowed when the process carries a device whitelist — the
			// device-constrained case AC2 covers.
			proc.AllowedTools = intersectDevices(deviceTools, proc.AllowedTools)
		}
		if len(opts.AllowedTools) > 0 {
			if len(proc.AllowedTools) > 0 {
				proc.AllowedTools = intersectDevices(opts.AllowedTools, proc.AllowedTools)
			} else {
				proc.AllowedTools = append([]string(nil), opts.AllowedTools...)
			}
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

		// Populate DriverMeta from DriverMetaProvider if available
		if file := k.vfs.GetFile(proc.PID, llmFD); file != nil {
			type driverMetaProvider interface {
				DriverMeta() map[string]string
			}
			if dmp, ok := file.(driverMetaProvider); ok {
				proc.DriverMeta = dmp.DriverMeta()
			}
			// Snapshot ReasoningEffort. Four-tier resolution, isomorphic to Model
			// (see line ~330): a non-empty per-spawn override (opts.ReasoningEffort,
			// from CLI --effort/compose/intent) wins; else the agent manifest default
			// (agent.Manifest.Models.ReasoningEffort, agent.yaml); else the driver
			// snapshot (ProviderConfig value via ReasoningEffortProvider, providers.yaml
			// instance default, Story 55.2); else "" passed through (API/CLI native
			// default). Passed through verbatim — no validation/mapping/case-folding
			// (Gemini's ThinkingLevel is UPPERCASE; agent.yaml authors must match it).
			// The resolved value drives request construction (reason.go) and detail/
			// strace display, so both reflect the effort actually in effect.
			if opts.ReasoningEffort != "" {
				proc.ReasoningEffort = opts.ReasoningEffort
			} else if agent != nil && agent.Manifest.Models.ReasoningEffort != "" {
				proc.ReasoningEffort = agent.Manifest.Models.ReasoningEffort
			} else if rep, ok := file.(vfs.ReasoningEffortProvider); ok {
				proc.ReasoningEffort = rep.ReasoningEffort()
			}
		}

		// Story 56.5 (CAP-5): attach the EventWriter HERE — after Open succeeds
		// (proc.Model / DriverMeta / ReasoningEffort populated) but BEFORE the
		// ConfigResolve emit below — so ConfigResolve and every spawn-early event
		// after it (setupDriverStreamHandler's Mount, etc.) land in events.jsonl
		// instead of being dropped by emitEvent's `ew != nil` gate (observe.go:55).
		// The original attach point sat after AddProcess, missing every
		// pre-AddProcess event on normal agent spawns (ConfigResolve hit was 0).
		//
		// Placement = story recommended option (a): Open's own error-return
		// (above) precedes this, so an Open failure never attaches → no orphan
		// events.jsonl / FD leak. The two MCP auto-mount error-returns below sit
		// AFTER this attach but BEFORE AddProcess/reap, so they each call
		// proc.DetachAndCloseEventWriter() (release the eagerly-opened FD) +
		// k.removeOrphanStepDir(proc) (drop the orphan events.jsonl + empty
		// steps/<uuid>/ dir, which gc cannot reclaim by UUID since the process
		// never enters procHistory).
		//
		// Mirrors the RawWriter auto-attach (below) but WITHOUT an Enabled gate:
		// disk event persistence is always-on (observe.go:54). When dataDir is
		// empty (bare kernel fixtures) ResolveStepBaseDir returns "" and the
		// auto-attach is skipped — behavior unchanged from before this story.
		//
		// SkipReasonLoop script-runners never reach this block; their factory is
		// invoked at the dedicated post-AddProcess block (kept for that path), so
		// the factory fires exactly once on every spawn shape.
		if opts.EventWriterFactory != nil {
			if ew, ewErr := opts.EventWriterFactory(proc); ewErr != nil {
				log.Printf("[spawn] EventWriterFactory failed pid=%d uuid=%s: %v",
					proc.PID, proc.UUID, ewErr)
			} else if ew != nil {
				proc.AttachEventWriter(ew)
			}
		} else if baseDir := k.ResolveStepBaseDir(proc); baseDir != "" && proc.UUID != "" {
			if ew, ewErr := NewEventWriter(baseDir, proc.UUID); ewErr != nil {
				log.Printf("[spawn] auto-attach EventWriter failed pid=%d uuid=%s: %v",
					proc.PID, proc.UUID, ewErr)
			} else {
				proc.AttachEventWriter(ew)
			}
		}

		if k.contextWindowFunc != nil {
			proc.ContextWindow = k.contextWindowFunc(proc.Provider, proc.Model)
		}

		if proc.ContextBudget == 0 && proc.ContextWindow > 0 {
			proc.ContextBudget = proc.ContextWindow * 9 / 10
		}
		if proc.ContextBudget > 0 && proc.ContextWindow > 0 && proc.ContextBudget > proc.ContextWindow {
			log.Printf("[kernel] pid=%d clamped context_budget %d to context_window %d",
				proc.PID, proc.ContextBudget, proc.ContextWindow)
			proc.ContextBudget = proc.ContextWindow
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
		if proc.ReasoningEffort != "" {
			configArgs["reasoning_effort"] = proc.ReasoningEffort
		}
		projectDefault := k.defaultProvider
		if opts.ProjectConfig != nil && opts.ProjectConfig.DefaultProvider != "" {
			projectDefault = opts.ProjectConfig.DefaultProvider
		}
		if projectDefault != "" && projectDefault != proc.Provider {
			configArgs["project_default"] = projectDefault
		}
		k.emitEvent(proc, "ConfigResolve", configArgs, nil, nil, time.Since(resolveStart))

		// Story 66.4 Task 3: fallback resolution failure visibility. The fallback
		// resolve block above runs BEFORE the EventWriter attaches (a few lines
		// up), so emitting there would only reach DebugChan and never land in
		// events.jsonl (observe.go's `ew != nil` gate). Deferred to here — right
		// after ConfigResolve, past the attach — so the event is durably
		// persisted. proc.FallbackResolveError is also set unconditionally above
		// for the IPC warning backfill, which does not depend on this event; the
		// SkipReasonLoop path never reaches this block (no reasonStep consumes
		// fallback) but still carries the field for that backfill.
		if proc.FallbackResolveError != "" {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":              0,
				"action":            "fallback_resolve_failed",
				"fallback_provider": proc.FallbackProvider,
				"reason":            proc.FallbackResolveError,
			}, nil, nil, 0)
		}

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
				// Story 56.5: release the early-attached EventWriter FD before this
				// pre-AddProcess error-return (it never reaches reap's close path),
				// then remove the orphan steps/<uuid>/ dir. The process dies before
				// AddProcess — it never enters procHistory, has no proc-info.json
				// anchor, so gc cannot reclaim its dir by UUID. removeOrphanStepDir
				// drops the early-written events.jsonl + the now-empty step dir.
				proc.DetachAndCloseEventWriter()
				k.removeOrphanStepDir(proc)
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
					// Story 56.5: release the early-attached EventWriter FD before
					// this pre-AddProcess error-return, then remove the orphan
					// steps/<uuid>/ dir. The Mount-failure event was already emitted
					// above (lands on disk via the early writer); detach + remove
					// only after that emit. The process dies before AddProcess — it
					// never enters procHistory and gc cannot reclaim the dir by UUID,
					// so removeOrphanStepDir drops events.jsonl + the empty dir.
					proc.DetachAndCloseEventWriter()
					k.removeOrphanStepDir(proc)
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
	//
	// Story 56.5: scoped to the SkipReasonLoop path only. The !SkipReasonLoop
	// path already attached its EventWriter (factory OR auto-attach) inside the
	// block above, BEFORE the ConfigResolve emit (CAP-5). SkipReasonLoop
	// script-runners never enter that block, so their factory must still fire
	// here — keeping the attach exactly once on every spawn shape and avoiding a
	// redundant second factory invocation on normal agent spawns.
	if opts.SkipReasonLoop && opts.EventWriterFactory != nil {
		if ew, ewErr := opts.EventWriterFactory(proc); ewErr != nil {
			log.Printf("[spawn] EventWriterFactory failed pid=%d uuid=%s: %v",
				proc.PID, proc.UUID, ewErr)
		} else if ew != nil {
			proc.AttachEventWriter(ew)
		}
	}

	// Story 56.1: attach RawWriter early — same rationale as EventWriter so
	// the very first LLM Write capture lands on disk. Best-effort: failure
	// degrades raw observability to no-op (capture hook gates on rawWriter != nil).
	//
	// CAP-4 default-on: when no factory is supplied AND raw capture is
	// enabled, the kernel auto-attaches using ResolveStepBaseDir so callers
	// outside the IPC server (CLI direct spawn, intent decompose, compose
	// pipelines) get raw capture for free.
	if opts.RawWriterFactory != nil {
		if rw, rwErr := opts.RawWriterFactory(proc); rwErr != nil {
			log.Printf("[spawn] RawWriterFactory failed pid=%d uuid=%s: %v",
				proc.PID, proc.UUID, rwErr)
		} else if rw != nil {
			proc.AttachRawWriter(rw)
		}
	} else if k.RawCaptureCfg().Enabled {
		if baseDir := k.ResolveStepBaseDir(proc); baseDir != "" && proc.UUID != "" {
			if rw, rwErr := NewRawWriter(baseDir, proc.UUID); rwErr != nil {
				log.Printf("[spawn] auto-attach RawWriter failed pid=%d uuid=%s: %v",
					proc.PID, proc.UUID, rwErr)
			} else {
				proc.AttachRawWriter(rw)
			}
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

	// Emit driver observability events (Story 40.3)
	if proc.DriverMeta != nil {
		// claude_cli.resolve — only when a binary was actually resolved (P2:
		// an empty resolved_bin means resolution failed, so emitting a
		// "success" resolve event would be misleading).
		if rbin, ok := proc.DriverMeta["resolved_bin"]; ok && rbin != "" {
			resolveArgs := map[string]any{"resolved_path": rbin}
			if cands := proc.DriverMeta["fallback_candidates"]; cands != "" {
				resolveArgs["candidates"] = strings.Split(cands, ",")
			}
			k.emitEvent(proc, "claude_cli.resolve", resolveArgs, nil, nil, 0)
		}
		capArgs := map[string]any{
			"partial_messages": proc.DriverMeta["cap_partial_messages"] == "true",
			"add_dir":          proc.DriverMeta["cap_add_dir"] == "true",
			"permission_mode":  proc.DriverMeta["cap_permission_mode"] == "true",
		}
		if ms, err := strconv.Atoi(proc.DriverMeta["probe_duration_ms"]); err == nil {
			capArgs["probe_duration_ms"] = ms
		}
		k.emitEvent(proc, "claude_cli.capabilities", capArgs, nil, nil, 0)
	}

	k.emitEvent(proc, "feature_profile", map[string]any{
		"profile":        proc.FeatureFlags.ProfileName,
		"planning":       proc.FeatureFlags.Planning,
		"replan":         proc.FeatureFlags.Replan,
		"specialize":     proc.FeatureFlags.Specialize,
		"discover_skill": proc.FeatureFlags.DiscoverSkill,
		"spawn":          proc.FeatureFlags.Spawn,
		"diff_memory":    proc.FeatureFlags.DiffMemory,
		"stem_matcher":   proc.FeatureFlags.StemMatcher,
		"immune":         proc.FeatureFlags.Immune,
		"compaction":     proc.FeatureFlags.Compaction,
	}, nil, nil, 0)

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
	baseDir := k.ResolveStepBaseDir(proc)
	if baseDir == "" {
		return
	}
	info, err := k.GetProcInfo(proc.PID)
	if err != nil || info == nil {
		return
	}
	if saveErr := SaveProcInfo(baseDir, *info); saveErr != nil {
		log.Printf("[spawn] pid=%d persistInitialProcInfo failed: %v", proc.PID, saveErr)
	}
}

// removeOrphanStepDir best-effort removes the steps/<uuid>/ directory created
// by the Story 56.5 early EventWriter auto-attach when a spawn aborts via a
// pre-AddProcess error-return (MCP auto-mount failures). Such a process never
// enters procHistory and has no proc-info.json anchor, so gc — which prunes by
// reaped/historical UUID — can never reclaim the dir. NewEventWriter eagerly
// creates events.jsonl + the dir, so without this the early-attach leaks disk
// state on every failed MCP spawn.
//
// Must be called AFTER DetachAndCloseEventWriter (FD released) so the file can
// be removed on platforms that refuse to unlink open files. Best-effort: a
// remaining empty dir or transient error is harmless (it is just an empty dir),
// so errors are swallowed — os.Remove on a non-empty dir is a no-op-ish error
// we deliberately ignore to never delete a dir that unexpectedly holds data.
func (k *KernelImpl) removeOrphanStepDir(proc *Process) {
	baseDir := k.ResolveStepBaseDir(proc)
	if baseDir == "" || proc == nil || proc.UUID == "" {
		return
	}
	stepDir := filepath.Join(baseDir, "steps", proc.UUID)
	// Remove the eagerly-created events.jsonl first, then the now-empty dir.
	// os.Remove on the dir fails (and is ignored) if anything else is present,
	// so we never clobber a dir that holds unexpected sibling data.
	_ = os.Remove(filepath.Join(stepDir, "events.jsonl"))
	_ = os.Remove(stepDir)
}

// intersectDevices returns devices present in both parent and child lists.
func intersectDevices(parent, child []string) []string {
	set := make(map[string]struct{}, len(parent))
	for _, d := range parent {
		set[d] = struct{}{}
	}
	var result []string
	for _, d := range child {
		if _, ok := set[d]; ok {
			result = append(result, d)
		}
	}
	return result
}
