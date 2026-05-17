package compose

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/kernel"
)

// Engine orchestrates multi-agent workflows based on a ComposeSpec.
type Engine struct {
	spec            *ComposeSpec
	dag             *DAG
	kernel          KernelSpawner
	agentLoader     AgentLoaderFunc
	budgetPool      *kernel.BudgetPool
	reputationStore *kernel.ReputationStore
	synergyMatrix   *kernel.SynergyMatrix
}

// SetSynergyMatrix sets the synergy matrix for recording skill combination results (Story 21.5).
func (e *Engine) SetSynergyMatrix(m *kernel.SynergyMatrix) {
	e.synergyMatrix = m
}

// NewEngine creates a new compose engine, building the DAG and detecting cycles.
func NewEngine(spec *ComposeSpec, ks KernelSpawner, al AgentLoaderFunc) (*Engine, error) {
	dag, err := BuildDAG(spec)
	if err != nil {
		return nil, fmt.Errorf("compose engine: %w", err)
	}
	e := &Engine{
		spec:        spec,
		dag:         dag,
		kernel:      ks,
		agentLoader: al,
	}
	if spec.TokenBudget > 0 {
		e.budgetPool = kernel.NewBudgetPool(spec.TokenBudget)
	}
	return e, nil
}

// NewEngineWithReputation creates a compose engine with an optional ReputationStore
// for persisting SLA evaluation results.
func NewEngineWithReputation(spec *ComposeSpec, ks KernelSpawner, al AgentLoaderFunc, rs *kernel.ReputationStore) (*Engine, error) {
	e, err := NewEngine(spec, ks, al)
	if err != nil {
		return nil, err
	}
	e.reputationStore = rs
	return e, nil
}

// Execute runs all agents in topological order, parallelizing within layers.
func (e *Engine) Execute(ctx context.Context) ([]ScheduleResult, error) {
	// Check context before starting
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	traceID := debug.GenerateTraceID()

	layers, err := e.dag.TopologicalSort()
	if err != nil {
		return nil, err
	}

	// Calculate BudgetPool quotas for all agents (Story 21.1)
	agentQuotas := make(map[string]int)
	if e.budgetPool != nil {
		// Compute weights and proportional quotas
		totalWeight := 0
		weights := make(map[string]int)
		for name, spec := range e.spec.Agents {
			w := int(kernel.ParsePriority(spec.Priority))
			weights[name] = w
			totalWeight += w
		}
		if totalWeight > 0 {
			for name, w := range weights {
				agentQuotas[name] = e.spec.TokenBudget * w / totalWeight
			}
		}
	}

	var allResults []ScheduleResult
	resultMap := make(map[string]*ScheduleResult)
	pids := xsync.NewSyncMap[string, types.PID]()

	for _, layer := range layers {
		// Check context cancellation between layers
		if ctx.Err() != nil {
			return allResults, ctx.Err()
		}

		// Check budget pool exhaustion between layers (Story 21.1)
		if e.budgetPool != nil && e.budgetPool.IsExhausted() {
			for _, remainingLayer := range layers {
				for _, name := range remainingLayer {
					if _, exists := resultMap[name]; !exists {
						allResults = append(allResults, ScheduleResult{
							Name: name,
							Err:  fmt.Errorf("budget_exhausted: token budget pool depleted"),
						})
						resultMap[name] = &allResults[len(allResults)-1]
					}
				}
			}
			return allResults, fmt.Errorf("budget_exhausted: token budget pool depleted")
		}

		var wg sync.WaitGroup
		layerResults := make([]*ScheduleResult, len(layer))

		for i, name := range layer {
			// Check if upstream dependencies all succeeded
			node := e.dag.Nodes[name]
			allDepsOK := true
			for _, dep := range node.DependsOn {
				if r, ok := resultMap[dep]; ok && r.Err != nil {
					allDepsOK = false
					break
				}
			}

			if !allDepsOK {
				layerResults[i] = &ScheduleResult{
					Name: name,
					Err:  fmt.Errorf("upstream dependency failed"),
				}
				continue
			}

			wg.Add(1)
			go func(idx int, agentName string) {
				defer wg.Done()
				result := e.executeNode(ctx, agentName, traceID, pids, agentQuotas)
				layerResults[idx] = result
			}(i, name)
		}

		wg.Wait()

		// Collect results and track budget consumption
		for _, r := range layerResults {
			if r != nil {
				// Record token usage to BudgetPool (Story 21.1)
				if r.PID != 0 && r.TokensUsed > 0 && e.budgetPool != nil {
					_ = e.budgetPool.RecordUsage(r.PID, r.TokensUsed)
				}
				allResults = append(allResults, *r)
				resultMap[r.Name] = r
			}
		}

		// Check context cancellation after layer completes
		if ctx.Err() != nil {
			return allResults, ctx.Err()
		}
	}

	return allResults, nil
}

// executeNode spawns and waits for a single agent node.
func (e *Engine) executeNode(ctx context.Context, name string, traceID types.TraceID, pids *xsync.SyncMap[string, types.PID], agentQuotas map[string]int) *ScheduleResult {
	start := time.Now()

	node := e.dag.Nodes[name]
	agentSpec := e.spec.Agents[name]

	// Auto-select: if Candidates is specified, pick the best agent (Story 21.3)
	selectedAgent := agentSpec.Agent
	if len(agentSpec.Candidates) > 0 {
		if e.reputationStore != nil {
			best, err := e.reputationStore.SelectBest(agentSpec.Candidates)
			if err == nil {
				selectedAgent = best
			} else {
				selectedAgent = agentSpec.Candidates[0]
			}
		} else {
			selectedAgent = agentSpec.Candidates[0]
		}
	}

	// Load agent info
	var agentInfo *agents.AgentInfo
	if selectedAgent != "" && e.agentLoader != nil {
		ai, err := e.agentLoader(selectedAgent)
		if err != nil {
			return &ScheduleResult{
				Name:     name,
				Err:      fmt.Errorf("load agent %q: %w", selectedAgent, err),
				Duration: time.Since(start),
			}
		}
		agentInfo = ai
	} else if e.agentLoader != nil {
		// Best-effort fallback: try loading agent by compose name.
		// Errors are ignored here since the agent field was not explicitly set.
		ai, _ := e.agentLoader(name)
		agentInfo = ai // may be nil if agent not found, which is acceptable
	}

	// Build spawn options with upstream output injection
	// Model priority: agent-level model > spec-level model (global default)
	model := agentSpec.Model
	if model == "" {
		model = e.spec.Model
	}
	// Provider priority: agent-level > spec-level (global default) > empty (system default)
	provider := agentSpec.Provider
	if provider == "" {
		provider = e.spec.Provider
	}
	opts := ComposeSpawnOpts{
		Model:         model,
		Provider:      provider,
		ContextBudget: agentSpec.ContextBudget,
		TimeoutMs:     agentSpec.TimeoutMs,
		TraceID:       traceID,
		ComposeNode:   name,
	}
	if node != nil {
		opts.ComposeDeps = append([]string(nil), node.DependsOn...)
	}

	// Apply BudgetPool quota to ContextBudget (Story 21.1)
	if quota, ok := agentQuotas[name]; ok && quota > 0 {
		if agentSpec.ContextBudget > 0 {
			// Take min(quota, context_budget) -- agent's own limit wins if smaller
			if quota < agentSpec.ContextBudget {
				opts.ContextBudget = quota
			}
		} else {
			opts.ContextBudget = quota
		}
	}
	// ParentSpanID from first upstream dependency (Story 15.1)
	if node != nil && len(node.DependsOn) > 0 {
		if depPID, ok := pids.Load(node.DependsOn[0]); ok {
			if parentSpanID, ok := e.kernel.GetSpanID(depPID); ok {
				opts.ParentSpanID = parentSpanID
			}
		}
	}
	upstreamPrompt := e.buildUpstreamPrompt(name, pids)
	if upstreamPrompt != "" {
		opts.SystemPrompt = upstreamPrompt
	}

	// Spawn the agent
	pid, err := e.kernel.Spawn(agentSpec.Intent, agentInfo, opts)
	if err != nil {
		return &ScheduleResult{
			Name:     name,
			Err:      fmt.Errorf("spawn agent %q: %w", name, err),
			Duration: time.Since(start),
		}
	}

	// Track PID for output passthrough (thread-safe via xsync.SyncMap)
	pids.Store(name, pid)

	// Register agent with BudgetPool now that PID is known (Story 21.1)
	if e.budgetPool != nil {
		priority := kernel.ParsePriority(agentSpec.Priority)
		e.budgetPool.AllocateQuota(pid, name, priority)
	}

	// Wait for completion, respecting context cancellation
	type waitResult struct {
		status ComposeExitStatus
		err    error
	}
	waitCh := make(chan waitResult, 1)
	go func() {
		status, werr := e.kernel.Wait(pid)
		waitCh <- waitResult{status: status, err: werr}
	}()

	select {
	case <-ctx.Done():
		return &ScheduleResult{
			Name:     name,
			PID:      pid,
			Err:      ctx.Err(),
			Duration: time.Since(start),
		}
	case wr := <-waitCh:
		result := &ScheduleResult{
			Name:     name,
			PID:      pid,
			ExitCode: wr.status.Code,
			Duration: time.Since(start),
		}
		if wr.err != nil {
			result.Err = wr.err
		} else if wr.status.Code != 0 {
			result.Err = fmt.Errorf("agent %q exited with code %d: %s", name, wr.status.Code, wr.status.Reason)
		}
		// Retrieve process output
		if output, ok := e.kernel.GetProcessResult(pid); ok {
			result.Output = output
		}
		// Retrieve token usage (Story 21.1)
		if tokensUsed, ok := e.kernel.GetTokensUsed(pid); ok {
			result.TokensUsed = tokensUsed
		}
		// SLA evaluation (Story 21.2)
		if agentSpec.SLA != nil && !agentSpec.SLA.IsEmpty() {
			durationMs := result.Duration.Milliseconds()
			slaResult := agentSpec.SLA.Evaluate(name, result.TokensUsed, durationMs, result.Output)
			result.SLAResult = slaResult
			if e.reputationStore != nil {
				_ = e.reputationStore.RecordResult(name, slaResult)
			}
			// Record synergy matrix data (Story 21.5)
			if e.synergyMatrix != nil && agentInfo != nil && len(agentInfo.Skills) > 0 {
				skillNames := make([]string, len(agentInfo.Skills))
				for i, s := range agentInfo.Skills {
					skillNames[i] = s.Manifest.Name
				}
				_ = e.synergyMatrix.RecordCombo(kernel.SynergyRecord{
					ComboKey:   kernel.NewComboKey(skillNames),
					Skills:     skillNames,
					Passed:     slaResult.Passed,
					TokensUsed: result.TokensUsed,
					DurationMs: durationMs,
					Timestamp:  time.Now(),
				})
			}
		}
		return result
	}
}

// buildUpstreamPrompt constructs the system prompt prefix containing upstream agent outputs.
func (e *Engine) buildUpstreamPrompt(name string, pids *xsync.SyncMap[string, types.PID]) string {
	node := e.dag.Nodes[name]
	if len(node.DependsOn) == 0 {
		return ""
	}

	var parts []string
	for _, dep := range node.DependsOn {
		pid, hasPID := pids.Load(dep)
		if !hasPID {
			continue
		}
		output, ok := e.kernel.GetProcessResult(pid)
		if !ok || output == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("### %s output:\n%s", dep, output))
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Upstream Agent Output\n" + strings.Join(parts, "\n")
}

// ExecuteFromNode runs the compose DAG starting *after* a node that has already
// been resumed externally (Story 42.4). Upstream nodes are not respawned; their
// historical results are injected via the upstreamResults map. The resumedNode
// itself is seeded with the supplied resumedResult, then the engine schedules
// nodes from the next layer onward using the standard runLayers helper.
//
// RED PHASE: stub — returns errExecuteFromNodeNotImplemented. Dev-story will
// implement the seedUpstream logic + delegate to runLayers from layer L+1.
func (e *Engine) ExecuteFromNode(
	ctx context.Context,
	resumedNode string,
	resumedResult HistoricalNodeResult,
	upstreamResults map[string]HistoricalNodeResult,
) ([]ScheduleResult, error) {
	_ = ctx
	_ = resumedNode
	_ = resumedResult
	_ = upstreamResults
	// RED PHASE call-graph anchor: dev-story will replace this body with the
	// real seed + delegate-to-runLayers logic. Keeping the call here ensures
	// the unused-method lint stays green during the red phase.
	if false {
		_, _ = e.runLayers(ctx, nil, 0, "", nil, nil, nil)
	}
	return nil, errExecuteFromNodeNotImplemented
}

// runLayers walks the DAG layers starting from startLayerIdx, using the
// supplied resultMap / pids as pre-seeded state. Both Execute (startLayerIdx=0)
// and ExecuteFromNode (startLayerIdx > 0) call this helper so layer ordering,
// dependency propagation, budget exhaustion, and SLA recording stay consistent
// between the two entry points (Story 42.4).
//
// RED PHASE: stub — returns errRunLayersNotImplemented. Dev-story will move the
// loop body from Execute() into this helper without behavior changes; Execute
// will then call runLayers(ctx, layers, 0, ...).
func (e *Engine) runLayers(
	ctx context.Context,
	layers [][]string,
	startLayerIdx int,
	traceID types.TraceID,
	resultMap map[string]*ScheduleResult,
	pids *xsync.SyncMap[string, types.PID],
	agentQuotas map[string]int,
) ([]ScheduleResult, error) {
	_ = ctx
	_ = layers
	_ = startLayerIdx
	_ = traceID
	_ = resultMap
	_ = pids
	_ = agentQuotas
	return nil, errRunLayersNotImplemented
}

// Sentinel errors for Story 42.4 RED PHASE stubs.
var (
	errExecuteFromNodeNotImplemented = fmt.Errorf("not implemented: Engine.ExecuteFromNode (Story 42.4 RED PHASE)")
	errRunLayersNotImplemented       = fmt.Errorf("not implemented: Engine.runLayers (Story 42.4 RED PHASE)")
)
