package shell

import (
	"context"
	"fmt"
	"time"
)

// MaxRecommendedStages is the soft limit on pipeline depth. Exceeding it produces a
// warning but does not block execution (as specified in Story 11.1 boundary cases).
const MaxRecommendedStages = 10

// SpawnRequest carries all per-spawn parameters to KernelSpawner.SpawnAndWait.
// Using a struct (rather than positional args) keeps call sites readable as the
// parameter set grows — provider/model/fallback values are easy to transpose
// when passed as a string of bare positional arguments.
type SpawnRequest struct {
	Intent           string
	Agent            string
	Model            string
	Provider         string // --provider override; "" = inherit agent manifest / kernel default
	FallbackProvider string // --fallback-provider override; "" = same-provider fallback
	FallbackModel    string // --fallback-model override; "" = use agent manifest fallback
	ReasoningEffort  string // --effort override; "" = inherit driver/provider default (passthrough, Story 55.2)
}

// KernelSpawner abstracts the kernel's spawn-and-wait operation for pipeline execution.
// Implementations bridge to the real kernel (via IPC) or mock (for testing).
type KernelSpawner interface {
	SpawnAndWait(ctx context.Context, req SpawnRequest) (result string, exitCode int, tokensUsed int, err error)
	Wait(ctx context.Context, pid int) (exitCode int, err error)
}

// StageCallback is invoked when a pipeline stage begins execution.
type StageCallback func(stage, total int, intent string)

// PipelineExecutor runs a parsed Pipeline sequentially, injecting each stage's
// output into the next stage's intent via [PIPE_INPUT] markers.
type PipelineExecutor struct {
	spawner      KernelSpawner
	OnStageStart StageCallback
}

// NewPipelineExecutor creates a PipelineExecutor backed by the given spawner.
func NewPipelineExecutor(spawner KernelSpawner) *PipelineExecutor {
	return &PipelineExecutor{spawner: spawner}
}

// PipelineResult holds the outcome of a pipeline execution.
type PipelineResult struct {
	Stages      []StageResult
	TotalTokens int
	Elapsed     time.Duration
}

// StageResult holds the outcome of a single pipeline stage.
type StageResult struct {
	PID        int
	Intent     string
	Result     string
	ExitCode   int
	TokensUsed int
	Elapsed    time.Duration
}

// Execute runs each command in the pipeline sequentially. The previous stage's
// Result is injected as [PIPE_INPUT] into the next stage's intent.
// Returns an error if the spawner itself errors or context is cancelled.
// A non-zero ExitCode stops the pipeline but is NOT treated as an error;
// the partial PipelineResult is returned with stages completed so far.
func (e *PipelineExecutor) Execute(ctx context.Context, pipeline *Pipeline) (*PipelineResult, error) {
	start := time.Now()
	result := &PipelineResult{}
	var prevResult string

	for i, cmd := range pipeline.Commands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if e.OnStageStart != nil {
			e.OnStageStart(i+1, len(pipeline.Commands), cmd.Intent)
		}

		intent := cmd.Intent
		if i > 0 {
			intent = fmt.Sprintf("[PIPE_INPUT]\n%s\n[/PIPE_INPUT]\n\n%s", prevResult, cmd.Intent)
		}

		stageStart := time.Now()
		res, exitCode, tokens, err := e.spawner.SpawnAndWait(ctx, SpawnRequest{
			Intent:           intent,
			Agent:            cmd.Agent,
			Model:            cmd.Model,
			Provider:         cmd.Provider,
			FallbackProvider: cmd.FallbackProvider,
			FallbackModel:    cmd.FallbackModel,
		})
		stageElapsed := time.Since(stageStart)

		if err != nil {
			return nil, fmt.Errorf("stage %d: %w", i+1, err)
		}

		stage := StageResult{
			Intent:     cmd.Intent,
			Result:     res,
			ExitCode:   exitCode,
			TokensUsed: tokens,
			Elapsed:    stageElapsed,
		}
		result.Stages = append(result.Stages, stage)
		result.TotalTokens += tokens

		if exitCode != 0 {
			break
		}

		prevResult = res
	}

	result.Elapsed = time.Since(start)
	return result, nil
}
