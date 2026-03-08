package agtest

import (
	"context"
	"time"
)

// CaseStatus represents the outcome of a single test case.
type CaseStatus string

const (
	StatusPassed  CaseStatus = "passed"
	StatusFailed  CaseStatus = "failed"
	StatusSkipped CaseStatus = "skipped"
	StatusError   CaseStatus = "error"
)

// ExecutionResult holds the raw output from running an agent.
type ExecutionResult struct {
	Output     string
	Syscalls   []string
	ExitCode   int
	TokensUsed int
	Error      string
}

// CaseResult captures the complete outcome of a single test case run.
type CaseResult struct {
	Name       string
	Intent     string
	Agent      string
	Status     CaseStatus
	Duration   time.Duration
	Output     string
	Syscalls   []string
	Assertions []AssertionResult
	Error      string
	TokensUsed int
}

// SuiteResult aggregates outcomes for an entire test suite.
type SuiteResult struct {
	Name     string
	Cases    []CaseResult
	Total    int
	Passed   int
	Failed   int
	Skipped  int
	Errors   int
	Duration time.Duration
}

// TestExecutor abstracts agent execution for testability.
// The real implementation uses IPC; tests use a mock.
type TestExecutor interface {
	Execute(ctx context.Context, tc *TestCaseSpec) (*ExecutionResult, error)
}

// Runner orchestrates batch test execution and assertion evaluation.
type Runner struct {
	Executor TestExecutor
	Judge    QualityJudge
	Timeout  time.Duration
}

// RunSuite runs all test cases in the suite sequentially and aggregates results.
func (r *Runner) RunSuite(ctx context.Context, suite *TestSuiteSpec) *SuiteResult {
	start := time.Now()
	sr := &SuiteResult{
		Name:  suite.Name,
		Cases: make([]CaseResult, 0, len(suite.Tests)),
	}

	for i := range suite.Tests {
		cr := r.runCase(ctx, &suite.Tests[i])
		sr.Cases = append(sr.Cases, cr)
	}

	for _, c := range sr.Cases {
		sr.Total++
		switch c.Status {
		case StatusPassed:
			sr.Passed++
		case StatusFailed:
			sr.Failed++
		case StatusSkipped:
			sr.Skipped++
		case StatusError:
			sr.Errors++
		}
	}
	sr.Duration = time.Since(start)
	return sr
}

func (r *Runner) runCase(ctx context.Context, tc *TestCaseSpec) CaseResult {
	name := tc.Name
	if name == "" {
		name = tc.Intent
	}
	cr := CaseResult{
		Name:   name,
		Intent: tc.Intent,
		Agent:  tc.Agent.Name,
	}

	if tc.Skip {
		cr.Status = StatusSkipped
		return cr
	}

	timeout := r.Timeout
	if tc.Timeout > 0 {
		timeout = time.Duration(tc.Timeout) * time.Millisecond
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	execResult, err := r.Executor.Execute(ctx, tc)
	cr.Duration = time.Since(start)

	if err != nil {
		cr.Status = StatusError
		cr.Error = err.Error()
		return cr
	}
	if execResult == nil {
		cr.Status = StatusError
		cr.Error = "executor returned nil result"
		return cr
	}

	cr.Output = execResult.Output
	cr.Syscalls = execResult.Syscalls
	cr.TokensUsed = execResult.TokensUsed

	if execResult.Error != "" {
		cr.Status = StatusError
		cr.Error = execResult.Error
		return cr
	}

	if tc.Assert == nil {
		if execResult.ExitCode == 0 {
			cr.Status = StatusPassed
		} else {
			cr.Status = StatusError
			cr.Error = "non-zero exit code"
		}
		return cr
	}

	tr := &TestResult{
		Output:   execResult.Output,
		Syscalls: execResult.Syscalls,
		Duration: cr.Duration,
	}
	assertions, evalErr := EvalAssertions(ctx, tr, tc.Assert, r.Judge)
	if evalErr != nil {
		cr.Status = StatusError
		cr.Error = evalErr.Error()
		return cr
	}
	cr.Assertions = assertions

	allPassed := true
	for _, a := range assertions {
		if !a.Passed {
			allPassed = false
			break
		}
	}
	if allPassed {
		cr.Status = StatusPassed
	} else {
		cr.Status = StatusFailed
	}
	return cr
}
