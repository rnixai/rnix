package agtest

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// --- MockExecutor for testing ---

type mockExecutorCase struct {
	result *ExecutionResult
	err    error
}

type MockExecutor struct {
	Cases map[string]mockExecutorCase // keyed by intent
}

func (m *MockExecutor) Execute(_ context.Context, tc *TestCaseSpec) (*ExecutionResult, error) {
	if c, ok := m.Cases[tc.Intent]; ok {
		return c.result, c.err
	}
	return &ExecutionResult{Output: "default output", ExitCode: 0}, nil
}

// --- 16.3-UNIT-001: RunSuite — all passed ---

func TestRunner_RunSuite_AllPassed(t *testing.T) {
	suite := &TestSuiteSpec{
		Name: "all-pass-suite",
		Tests: []TestCaseSpec{
			{Name: "test-a", Intent: "do A", Agent: AgentConfig{Name: "agent-a"}},
			{Name: "test-b", Intent: "do B", Agent: AgentConfig{Name: "agent-b"}},
		},
	}
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"do A": {result: &ExecutionResult{Output: "ok-A", ExitCode: 0}},
			"do B": {result: &ExecutionResult{Output: "ok-B", ExitCode: 0}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	sr := runner.RunSuite(context.Background(), suite)

	if sr.Total != 2 {
		t.Errorf("Total = %d, want 2", sr.Total)
	}
	if sr.Passed != 2 {
		t.Errorf("Passed = %d, want 2", sr.Passed)
	}
	if sr.Failed != 0 {
		t.Errorf("Failed = %d, want 0", sr.Failed)
	}
	if sr.Errors != 0 {
		t.Errorf("Errors = %d, want 0", sr.Errors)
	}
	if sr.Name != "all-pass-suite" {
		t.Errorf("Name = %q, want %q", sr.Name, "all-pass-suite")
	}
}

// --- 16.3-UNIT-002: RunSuite — mixed results ---

func TestRunner_RunSuite_MixedResults(t *testing.T) {
	suite := &TestSuiteSpec{
		Tests: []TestCaseSpec{
			{
				Name: "pass-test", Intent: "pass intent", Agent: AgentConfig{Name: "a"},
				Assert: &AssertConfig{Output: &OutputAssert{Contains: []string{"hello"}}},
			},
			{
				Name: "fail-test", Intent: "fail intent", Agent: AgentConfig{Name: "a"},
				Assert: &AssertConfig{Output: &OutputAssert{Contains: []string{"missing-word"}}},
			},
			{
				Name: "error-test", Intent: "error intent", Agent: AgentConfig{Name: "a"},
			},
		},
	}
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"pass intent":  {result: &ExecutionResult{Output: "hello world", ExitCode: 0}},
			"fail intent":  {result: &ExecutionResult{Output: "no match here", ExitCode: 0}},
			"error intent": {err: fmt.Errorf("spawn failed")},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	sr := runner.RunSuite(context.Background(), suite)

	if sr.Total != 3 {
		t.Errorf("Total = %d, want 3", sr.Total)
	}
	if sr.Passed != 1 {
		t.Errorf("Passed = %d, want 1", sr.Passed)
	}
	if sr.Failed != 1 {
		t.Errorf("Failed = %d, want 1", sr.Failed)
	}
	if sr.Errors != 1 {
		t.Errorf("Errors = %d, want 1", sr.Errors)
	}

	if sr.Cases[1].Status != StatusFailed {
		t.Errorf("fail-test status = %q, want %q", sr.Cases[1].Status, StatusFailed)
	}
	if len(sr.Cases[1].Assertions) == 0 {
		t.Fatal("fail-test should have assertion results")
	}
	if sr.Cases[1].Assertions[0].Passed {
		t.Error("fail-test assertion should not pass")
	}
}

// --- 16.3-UNIT-003: runCase — spawn error ---

func TestRunner_RunCase_SpawnError(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {err: fmt.Errorf("connection refused")},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{Name: "err-test", Intent: "intent", Agent: AgentConfig{Name: "a"}}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusError {
		t.Errorf("Status = %q, want %q", cr.Status, StatusError)
	}
	if cr.Error == "" {
		t.Error("Error should not be empty on spawn failure")
	}
}

// --- 16.3-UNIT-004: runCase — no assert, exit zero ---

func TestRunner_RunCase_NoAssert_ExitZero(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{Output: "done", ExitCode: 0}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{Name: "no-assert", Intent: "intent", Agent: AgentConfig{Name: "a"}}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusPassed {
		t.Errorf("Status = %q, want %q", cr.Status, StatusPassed)
	}
}

// --- 16.3-UNIT-005: runCase — no assert, exit non-zero ---

func TestRunner_RunCase_NoAssert_ExitNonZero(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{Output: "failed", ExitCode: 1}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{Name: "exit-1", Intent: "intent", Agent: AgentConfig{Name: "a"}}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusError {
		t.Errorf("Status = %q, want %q", cr.Status, StatusError)
	}
}

// --- 16.3-UNIT-006: runCase — output assert pass ---

func TestRunner_RunCase_OutputAssert_Pass(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{Output: "hello world", ExitCode: 0}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{
		Name: "output-pass", Intent: "intent", Agent: AgentConfig{Name: "a"},
		Assert: &AssertConfig{Output: &OutputAssert{Contains: []string{"hello"}}},
	}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusPassed {
		t.Errorf("Status = %q, want %q", cr.Status, StatusPassed)
	}
	if len(cr.Assertions) == 0 {
		t.Fatal("should have assertion results")
	}
	if !cr.Assertions[0].Passed {
		t.Error("assertion should pass")
	}
}

// --- 16.3-UNIT-007: runCase — output assert fail with details ---

func TestRunner_RunCase_OutputAssert_Fail(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{Output: "something else", ExitCode: 0}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{
		Name: "output-fail", Intent: "intent", Agent: AgentConfig{Name: "a"},
		Assert: &AssertConfig{Output: &OutputAssert{Contains: []string{"expected-text"}}},
	}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusFailed {
		t.Errorf("Status = %q, want %q", cr.Status, StatusFailed)
	}
	if len(cr.Assertions) == 0 {
		t.Fatal("should have assertion results")
	}
	a := cr.Assertions[0]
	if a.Passed {
		t.Error("assertion should fail")
	}
	if a.Type != "output" {
		t.Errorf("assertion Type = %q, want %q", a.Type, "output")
	}
	if a.Expected == nil {
		t.Error("assertion Expected should not be nil")
	}
	if a.Actual == nil {
		t.Error("assertion Actual should not be nil")
	}
}

// --- 16.3-UNIT-008: runCase — syscall assert pass ---

func TestRunner_RunCase_SyscallAssert_Pass(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{
				Output:   "ok",
				ExitCode: 0,
				Syscalls: []string{"Spawn", "CtxWrite", "Read"},
			}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{
		Name: "syscall-pass", Intent: "intent", Agent: AgentConfig{Name: "a"},
		Assert: &AssertConfig{Syscalls: &SyscallAssert{Includes: []string{"CtxWrite"}}},
	}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusPassed {
		t.Errorf("Status = %q, want %q", cr.Status, StatusPassed)
	}
}

// --- 16.3-UNIT-009: runCase — syscall assert fail with details ---

func TestRunner_RunCase_SyscallAssert_Fail(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{
				Output:   "ok",
				ExitCode: 0,
				Syscalls: []string{"Spawn", "Read"},
			}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{
		Name: "syscall-fail", Intent: "intent", Agent: AgentConfig{Name: "a"},
		Assert: &AssertConfig{Syscalls: &SyscallAssert{Includes: []string{"Write"}}},
	}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusFailed {
		t.Errorf("Status = %q, want %q", cr.Status, StatusFailed)
	}
	if len(cr.Assertions) == 0 {
		t.Fatal("should have assertion results")
	}
	if cr.Assertions[0].Passed {
		t.Error("syscall assertion should fail")
	}
	if cr.Assertions[0].Type != "syscalls" {
		t.Errorf("assertion Type = %q, want %q", cr.Assertions[0].Type, "syscalls")
	}
}

// --- 16.3-UNIT-010: runCase — syscall collection from ExecutionResult ---

func TestRunner_RunCase_SyscallCollection(t *testing.T) {
	wantSyscalls := []string{"Spawn", "Open", "Read", "Write", "Close"}
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{
				Output:   "ok",
				ExitCode: 0,
				Syscalls: wantSyscalls,
			}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{
		Name: "syscall-collect", Intent: "intent", Agent: AgentConfig{Name: "a"},
		Assert: &AssertConfig{Syscalls: &SyscallAssert{Includes: []string{"Open", "Write"}}},
	}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusPassed {
		t.Errorf("Status = %q, want %q", cr.Status, StatusPassed)
	}
	if len(cr.Syscalls) != len(wantSyscalls) {
		t.Errorf("Syscalls len = %d, want %d", len(cr.Syscalls), len(wantSyscalls))
	}
}

// --- 16.3-UNIT-011: runCase — timeout ---

func TestRunner_RunCase_Timeout(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {err: fmt.Errorf("context deadline exceeded")},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 100 * time.Millisecond}
	tc := &TestCaseSpec{Name: "timeout-test", Intent: "intent", Agent: AgentConfig{Name: "a"}}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusError {
		t.Errorf("Status = %q, want %q", cr.Status, StatusError)
	}
}

// --- 16.3-UNIT-012: runCase — tc.Timeout overrides Runner.Timeout ---

func TestRunner_RunCase_TimeoutFromSpec(t *testing.T) {
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{Output: "ok", ExitCode: 0}},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 5 * time.Second}
	tc := &TestCaseSpec{
		Name: "spec-timeout", Intent: "intent", Agent: AgentConfig{Name: "a"},
		Timeout: 500, // 500ms
	}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusPassed {
		t.Errorf("Status = %q, want %q", cr.Status, StatusPassed)
	}
	// tc.Timeout should override Runner.Timeout; as long as execution completes, it passes
}

// --- 16.3-UNIT-013: SuiteResult aggregation ---

func TestSuiteResult_Aggregation(t *testing.T) {
	suite := &TestSuiteSpec{
		Name: "agg-suite",
		Tests: []TestCaseSpec{
			{Name: "p1", Intent: "p1", Agent: AgentConfig{Name: "a"}},
			{Name: "p2", Intent: "p2", Agent: AgentConfig{Name: "a"}},
			{
				Name: "f1", Intent: "f1", Agent: AgentConfig{Name: "a"},
				Assert: &AssertConfig{Output: &OutputAssert{Contains: []string{"no-match"}}},
			},
			{Name: "e1", Intent: "e1", Agent: AgentConfig{Name: "a"}},
			{Name: "s1", Intent: "s1", Agent: AgentConfig{Name: "a"}, Skip: true},
		},
	}
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"p1": {result: &ExecutionResult{Output: "ok", ExitCode: 0}},
			"p2": {result: &ExecutionResult{Output: "ok", ExitCode: 0}},
			"f1": {result: &ExecutionResult{Output: "wrong", ExitCode: 0}},
			"e1": {err: fmt.Errorf("boom")},
		},
	}
	runner := &Runner{Executor: executor, Timeout: 30 * time.Second}
	sr := runner.RunSuite(context.Background(), suite)

	if sr.Total != 5 {
		t.Errorf("Total = %d, want 5", sr.Total)
	}
	if sr.Passed != 2 {
		t.Errorf("Passed = %d, want 2", sr.Passed)
	}
	if sr.Failed != 1 {
		t.Errorf("Failed = %d, want 1", sr.Failed)
	}
	if sr.Errors != 1 {
		t.Errorf("Errors = %d, want 1", sr.Errors)
	}
	if sr.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", sr.Skipped)
	}
	if sr.Duration == 0 {
		t.Error("Duration should be > 0")
	}
}

// --- 16.3-UNIT-015: runCase — skip flag ---

func TestRunner_RunCase_Skip(t *testing.T) {
	callCount := 0
	executor := &MockExecutor{
		Cases: map[string]mockExecutorCase{
			"intent": {result: &ExecutionResult{Output: "should not execute", ExitCode: 0}},
		},
	}
	origExecute := executor.Execute
	wrappedExecutor := &countingExecutor{inner: executor, count: &callCount, orig: origExecute}

	runner := &Runner{Executor: wrappedExecutor, Timeout: 30 * time.Second}
	tc := &TestCaseSpec{
		Name: "skip-test", Intent: "intent", Agent: AgentConfig{Name: "a"},
		Skip: true,
	}
	cr := runner.runCase(context.Background(), tc)

	if cr.Status != StatusSkipped {
		t.Errorf("Status = %q, want %q", cr.Status, StatusSkipped)
	}
	if callCount != 0 {
		t.Errorf("executor was called %d times, want 0 for skipped test", callCount)
	}
}

type countingExecutor struct {
	inner *MockExecutor
	count *int
	orig  func(context.Context, *TestCaseSpec) (*ExecutionResult, error)
}

func (c *countingExecutor) Execute(ctx context.Context, tc *TestCaseSpec) (*ExecutionResult, error) {
	*c.count++
	return c.inner.Execute(ctx, tc)
}

// --- 16.3-UNIT-014: CaseStatus constants ---

func TestCaseStatus_Constants(t *testing.T) {
	statuses := []CaseStatus{StatusPassed, StatusFailed, StatusSkipped, StatusError}
	seen := make(map[CaseStatus]bool)
	for _, s := range statuses {
		if s == "" {
			t.Error("status constant should not be empty")
		}
		if seen[s] {
			t.Errorf("duplicate status: %q", s)
		}
		seen[s] = true
	}
}
