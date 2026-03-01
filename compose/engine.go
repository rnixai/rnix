package compose

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gonewx/crux/agents"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/xsync"
)

// Engine orchestrates multi-agent workflows based on a ComposeSpec.
type Engine struct {
	spec        *ComposeSpec
	dag         *DAG
	kernel      KernelSpawner
	agentLoader AgentLoaderFunc
}

// NewEngine creates a new compose engine, building the DAG and detecting cycles.
func NewEngine(spec *ComposeSpec, ks KernelSpawner, al AgentLoaderFunc) (*Engine, error) {
	dag, err := BuildDAG(spec)
	if err != nil {
		return nil, fmt.Errorf("compose engine: %w", err)
	}
	return &Engine{
		spec:        spec,
		dag:         dag,
		kernel:      ks,
		agentLoader: al,
	}, nil
}

// Execute runs all agents in topological order, parallelizing within layers.
func (e *Engine) Execute(ctx context.Context) ([]ScheduleResult, error) {
	// Check context before starting
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	layers, err := e.dag.TopologicalSort()
	if err != nil {
		return nil, err
	}

	var allResults []ScheduleResult
	resultMap := make(map[string]*ScheduleResult)
	pids := xsync.NewSyncMap[string, types.PID]()

	for _, layer := range layers {
		// Check context cancellation between layers
		if ctx.Err() != nil {
			return allResults, ctx.Err()
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
				result := e.executeNode(ctx, agentName, pids)
				layerResults[idx] = result
			}(i, name)
		}

		wg.Wait()

		// Collect results
		for _, r := range layerResults {
			if r != nil {
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
func (e *Engine) executeNode(ctx context.Context, name string, pids *xsync.SyncMap[string, types.PID]) *ScheduleResult {
	start := time.Now()

	agentSpec := e.spec.Agents[name]

	// Load agent info
	var agentInfo *agents.AgentInfo
	if agentSpec.Agent != "" && e.agentLoader != nil {
		ai, err := e.agentLoader(agentSpec.Agent)
		if err != nil {
			return &ScheduleResult{
				Name:     name,
				Err:      fmt.Errorf("load agent %q: %w", agentSpec.Agent, err),
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
	opts := ComposeSpawnOpts{
		Model: model,
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
