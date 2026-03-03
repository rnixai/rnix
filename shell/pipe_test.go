package shell

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 11.1: 管道语法 (Pipe Syntax)
//
// Tests reference PipelineExecutor, KernelSpawner, PipelineResult,
// StageResult types which do NOT exist yet → compile failure = RED phase.
// ============================================================

type mockSpawner struct {
	results []mockResult
	calls   []mockCall
}

type mockResult struct {
	result   string
	exitCode int
	tokens   int
	err      error
}

type mockCall struct {
	intent string
	agent  string
	model  string
}

func (m *mockSpawner) SpawnAndWait(ctx context.Context, intent, agent, model string) (string, int, int, error) {
	m.calls = append(m.calls, mockCall{intent: intent, agent: agent, model: model})
	idx := len(m.calls) - 1
	if idx >= len(m.results) {
		return "", 1, 0, fmt.Errorf("unexpected call %d", idx)
	}
	r := m.results[idx]
	return r.result, r.exitCode, r.tokens, r.err
}

// --- 11.1-UNIT-006: [P0] 双阶段管道执行 + PIPE_INPUT 注入 (AC1) ---

func TestPipelineExecutor_TwoStages_PipeInput(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "代码分析报告", exitCode: 0, tokens: 100},
			{result: "文档内容", exitCode: 0, tokens: 200},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "分析代码"},
			{Type: "spawn", Intent: "写文档"},
		},
	}

	result, err := executor.Execute(context.Background(), pipeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Stages) != 2 {
		t.Fatalf("stages count = %d, want 2", len(result.Stages))
	}

	// Stage 1: original intent
	if spawner.calls[0].intent != "分析代码" {
		t.Errorf("stage 0 intent = %q, want %q", spawner.calls[0].intent, "分析代码")
	}
	if result.Stages[0].Result != "代码分析报告" {
		t.Errorf("stage 0 result = %q, want %q", result.Stages[0].Result, "代码分析报告")
	}
	if result.Stages[0].ExitCode != 0 {
		t.Errorf("stage 0 exit code = %d, want 0", result.Stages[0].ExitCode)
	}

	// Stage 2: PIPE_INPUT injected
	stage1Intent := spawner.calls[1].intent
	if !strings.Contains(stage1Intent, "[PIPE_INPUT]") {
		t.Errorf("stage 1 intent should contain [PIPE_INPUT], got %q", stage1Intent)
	}
	if !strings.Contains(stage1Intent, "代码分析报告") {
		t.Errorf("stage 1 intent should contain previous result, got %q", stage1Intent)
	}
	if !strings.Contains(stage1Intent, "写文档") {
		t.Errorf("stage 1 intent should contain original intent, got %q", stage1Intent)
	}
	if result.Stages[1].Result != "文档内容" {
		t.Errorf("stage 1 result = %q, want %q", result.Stages[1].Result, "文档内容")
	}

	// Total tokens
	if result.TotalTokens != 300 {
		t.Errorf("total tokens = %d, want 300", result.TotalTokens)
	}
}

// --- 11.1-UNIT-007: [P0] 三阶段管道执行 A→B→C (AC2) ---

func TestPipelineExecutor_ThreeStages_ChainTransfer(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "Result-A", exitCode: 0, tokens: 10},
			{result: "Result-B", exitCode: 0, tokens: 20},
			{result: "Result-C", exitCode: 0, tokens: 30},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "A"},
			{Type: "spawn", Intent: "B"},
			{Type: "spawn", Intent: "C"},
		},
	}

	result, err := executor.Execute(context.Background(), pipeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Stages) != 3 {
		t.Fatalf("stages count = %d, want 3", len(result.Stages))
	}

	// Stage 0: no PIPE_INPUT
	if strings.Contains(spawner.calls[0].intent, "[PIPE_INPUT]") {
		t.Error("stage 0 should NOT contain [PIPE_INPUT]")
	}

	// Stage 1: PIPE_INPUT with Result-A
	if !strings.Contains(spawner.calls[1].intent, "Result-A") {
		t.Errorf("stage 1 should contain Result-A, got %q", spawner.calls[1].intent)
	}

	// Stage 2: PIPE_INPUT with Result-B
	if !strings.Contains(spawner.calls[2].intent, "Result-B") {
		t.Errorf("stage 2 should contain Result-B, got %q", spawner.calls[2].intent)
	}

	if result.TotalTokens != 60 {
		t.Errorf("total tokens = %d, want 60", result.TotalTokens)
	}
}

// --- 11.1-UNIT-008: [P0] 首阶段失败 → 后续不执行 (AC3) ---

func TestPipelineExecutor_FirstStageFails(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "error output", exitCode: 1, tokens: 50},
			{result: "should not run", exitCode: 0, tokens: 100},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "A"},
			{Type: "spawn", Intent: "B"},
		},
	}

	result, err := executor.Execute(context.Background(), pipeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 1 stage should have been executed
	if len(spawner.calls) != 1 {
		t.Errorf("spawner calls = %d, want 1 (second stage should not run)", len(spawner.calls))
	}

	// Result should contain the failed stage
	if len(result.Stages) != 1 {
		t.Fatalf("stages count = %d, want 1", len(result.Stages))
	}
	if result.Stages[0].ExitCode != 1 {
		t.Errorf("stage 0 exit code = %d, want 1", result.Stages[0].ExitCode)
	}
}

// --- 11.1-UNIT-009: [P0] 中间阶段失败 → 后续不执行，前置保留 (AC3) ---

func TestPipelineExecutor_MiddleStageFails(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "Result-A", exitCode: 0, tokens: 10},
			{result: "error at B", exitCode: 2, tokens: 20},
			{result: "should not run", exitCode: 0, tokens: 30},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "A"},
			{Type: "spawn", Intent: "B"},
			{Type: "spawn", Intent: "C"},
		},
	}

	result, err := executor.Execute(context.Background(), pipeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 2 stages executed (A success, B fail, C skipped)
	if len(spawner.calls) != 2 {
		t.Errorf("spawner calls = %d, want 2", len(spawner.calls))
	}
	if len(result.Stages) != 2 {
		t.Fatalf("stages count = %d, want 2", len(result.Stages))
	}

	// Stage 0 succeeded
	if result.Stages[0].ExitCode != 0 {
		t.Errorf("stage 0 exit code = %d, want 0", result.Stages[0].ExitCode)
	}
	if result.Stages[0].Result != "Result-A" {
		t.Errorf("stage 0 result = %q, want %q", result.Stages[0].Result, "Result-A")
	}

	// Stage 1 failed
	if result.Stages[1].ExitCode != 2 {
		t.Errorf("stage 1 exit code = %d, want 2", result.Stages[1].ExitCode)
	}

	// Total tokens = only executed stages
	if result.TotalTokens != 30 {
		t.Errorf("total tokens = %d, want 30", result.TotalTokens)
	}
}

// --- 11.1-UNIT-010: [P1] context 取消 → 执行中断 ---

func TestPipelineExecutor_ContextCancelled(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "Result-A", exitCode: 0, tokens: 10},
			{result: "should not complete", exitCode: 0, tokens: 20},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	cancellingSpawner := &contextCancellingSpawner{
		inner:       spawner,
		cancelAfter: 1,
		cancel:      cancel,
	}

	executor := NewPipelineExecutor(cancellingSpawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "A"},
			{Type: "spawn", Intent: "B"},
		},
	}

	_, err := executor.Execute(ctx, pipeline)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

type contextCancellingSpawner struct {
	inner       *mockSpawner
	cancelAfter int
	cancel      context.CancelFunc
	callCount   int
}

func (c *contextCancellingSpawner) SpawnAndWait(ctx context.Context, intent, agent, model string) (string, int, int, error) {
	c.callCount++
	if c.callCount > c.cancelAfter {
		c.cancel()
		return "", 1, 0, ctx.Err()
	}
	return c.inner.SpawnAndWait(ctx, intent, agent, model)
}

// --- 11.1-UNIT-011: [P1] PipelineResult.Elapsed 记录总耗时 ---

func TestPipelineExecutor_RecordsElapsed(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "test"},
		},
	}

	result, err := executor.Execute(context.Background(), pipeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Elapsed <= 0 {
		t.Error("elapsed should be > 0")
	}
}

// --- 11.1-UNIT-012: [P1] StageResult.Elapsed 每阶段独立计时 ---

func TestPipelineExecutor_StageElapsed(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok", exitCode: 0, tokens: 10},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "test"},
		},
	}

	result, err := executor.Execute(context.Background(), pipeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Stages) != 1 {
		t.Fatalf("stages count = %d, want 1", len(result.Stages))
	}
	if result.Stages[0].Elapsed <= 0 {
		t.Error("stage elapsed should be > 0")
	}
}

// --- 11.1-UNIT-013: [P1] 带 Agent/Model 参数传递到 spawner ---

func TestPipelineExecutor_PassesAgentAndModel(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "ok1", exitCode: 0, tokens: 10},
			{result: "ok2", exitCode: 0, tokens: 20},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "分析", Agent: "analyst", Model: "sonnet"},
			{Type: "spawn", Intent: "写文档", Agent: "writer", Model: "opus"},
		},
	}

	_, err := executor.Execute(context.Background(), pipeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spawner.calls[0].agent != "analyst" {
		t.Errorf("stage 0 agent = %q, want %q", spawner.calls[0].agent, "analyst")
	}
	if spawner.calls[0].model != "sonnet" {
		t.Errorf("stage 0 model = %q, want %q", spawner.calls[0].model, "sonnet")
	}
	if spawner.calls[1].agent != "writer" {
		t.Errorf("stage 1 agent = %q, want %q", spawner.calls[1].agent, "writer")
	}
	if spawner.calls[1].model != "opus" {
		t.Errorf("stage 1 model = %q, want %q", spawner.calls[1].model, "opus")
	}
}

// --- 11.1-UNIT-014: [P2] Spawner 返回 error → 管道中断 ---

func TestPipelineExecutor_SpawnerError(t *testing.T) {
	spawner := &mockSpawner{
		results: []mockResult{
			{result: "", exitCode: 0, tokens: 0, err: fmt.Errorf("driver unavailable")},
		},
	}

	executor := NewPipelineExecutor(spawner)
	pipeline := &Pipeline{
		Commands: []Command{
			{Type: "spawn", Intent: "test"},
		},
	}

	_, err := executor.Execute(context.Background(), pipeline)
	if err == nil {
		t.Fatal("expected error when spawner returns error, got nil")
	}
}

// Ensure time package is used (referenced in StageResult.Elapsed, PipelineResult.Elapsed)
var _ = time.Duration(0)
