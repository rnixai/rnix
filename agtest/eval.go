package agtest

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AssertionResult captures the outcome of a single assertion check.
type AssertionResult struct {
	Type     string // "output", "syscalls", "quality"
	Passed   bool
	Message  string
	Expected any
	Actual   any
}

// TestResult holds the agent execution output used as input for assertion evaluation.
type TestResult struct {
	Output   string
	Syscalls []string
	Duration time.Duration
}

// QualityResult is returned by QualityJudge after evaluating output quality.
type QualityResult struct {
	Passed bool
	Score  float64
	Reason string
}

// QualityJudge evaluates agent output quality against given criteria.
type QualityJudge interface {
	Judge(ctx context.Context, output string, criteria string) (*QualityResult, error)
}

// EvalOutput evaluates output assertions against agent output text.
// Returns nil if assert is nil.
func EvalOutput(output string, assert *OutputAssert) []AssertionResult {
	if assert == nil {
		return nil
	}
	var results []AssertionResult
	for _, want := range assert.Contains {
		if strings.Contains(output, want) {
			results = append(results, AssertionResult{
				Type:     "output",
				Passed:   true,
				Message:  fmt.Sprintf("output contains %q", want),
				Expected: want,
				Actual:   truncate(output, 200),
			})
		} else {
			results = append(results, AssertionResult{
				Type:     "output",
				Passed:   false,
				Message:  fmt.Sprintf("output does not contain %q", want),
				Expected: want,
				Actual:   truncate(output, 200),
			})
		}
	}
	for _, unwanted := range assert.NotContains {
		if strings.Contains(output, unwanted) {
			results = append(results, AssertionResult{
				Type:     "output",
				Passed:   false,
				Message:  fmt.Sprintf("output contains unwanted %q", unwanted),
				Expected: fmt.Sprintf("not contain %q", unwanted),
				Actual:   truncate(output, 200),
			})
		} else {
			results = append(results, AssertionResult{
				Type:     "output",
				Passed:   true,
				Message:  fmt.Sprintf("output does not contain unwanted %q", unwanted),
				Expected: fmt.Sprintf("not contain %q", unwanted),
				Actual:   truncate(output, 200),
			})
		}
	}
	return results
}

// EvalSyscalls evaluates syscall assertions against the recorded syscall sequence.
// Returns nil if assert is nil.
func EvalSyscalls(events []string, assert *SyscallAssert) []AssertionResult {
	if assert == nil {
		return nil
	}
	eventSet := make(map[string]bool, len(events))
	for _, e := range events {
		eventSet[e] = true
	}
	var results []AssertionResult
	for _, want := range assert.Includes {
		if eventSet[want] {
			results = append(results, AssertionResult{
				Type:     "syscalls",
				Passed:   true,
				Message:  fmt.Sprintf("syscall %q found", want),
				Expected: want,
				Actual:   events,
			})
		} else {
			results = append(results, AssertionResult{
				Type:     "syscalls",
				Passed:   false,
				Message:  fmt.Sprintf("syscall %q not found in event sequence", want),
				Expected: want,
				Actual:   events,
			})
		}
	}
	for _, unwanted := range assert.Excludes {
		if eventSet[unwanted] {
			results = append(results, AssertionResult{
				Type:     "syscalls",
				Passed:   false,
				Message:  fmt.Sprintf("syscall %q should not appear but was found", unwanted),
				Expected: fmt.Sprintf("exclude %q", unwanted),
				Actual:   events,
			})
		} else {
			results = append(results, AssertionResult{
				Type:     "syscalls",
				Passed:   true,
				Message:  fmt.Sprintf("syscall %q correctly absent", unwanted),
				Expected: fmt.Sprintf("exclude %q", unwanted),
				Actual:   events,
			})
		}
	}
	return results
}

// EvalQuality evaluates quality assertions using the provided QualityJudge.
// Returns nil if assert is nil. Returns an error result if judge returns an error.
func EvalQuality(ctx context.Context, output string, assert *QualityAssert, judge QualityJudge) []AssertionResult {
	if assert == nil {
		return nil
	}
	qr, err := judge.Judge(ctx, output, assert.Criteria)
	if err != nil {
		return []AssertionResult{{
			Type:     "quality",
			Passed:   false,
			Message:  fmt.Sprintf("quality judge error: %v", err),
			Expected: assert.Criteria,
			Actual:   nil,
		}}
	}
	if qr == nil {
		return []AssertionResult{{
			Type:     "quality",
			Passed:   false,
			Message:  "quality judge returned nil result",
			Expected: assert.Criteria,
			Actual:   nil,
		}}
	}
	return []AssertionResult{{
		Type:     "quality",
		Passed:   qr.Passed,
		Message:  qualityMessage(qr),
		Expected: assert.Criteria,
		Actual:   fmt.Sprintf("score=%.2f reason=%s", qr.Score, qr.Reason),
	}}
}

// EvalAssertions orchestrates all three assertion types against a test result.
// Returns nil slice and nil error if assert is nil (no assertions = pass).
// Returns an error if quality assertions are present but judge is nil.
func EvalAssertions(ctx context.Context, result *TestResult, assert *AssertConfig, judge QualityJudge) ([]AssertionResult, error) {
	if assert == nil {
		return nil, nil
	}
	if assert.Quality != nil && judge == nil {
		return nil, fmt.Errorf("quality assertion requires a QualityJudge, but judge is nil")
	}
	var all []AssertionResult
	all = append(all, EvalOutput(result.Output, assert.Output)...)
	all = append(all, EvalSyscalls(result.Syscalls, assert.Syscalls)...)
	if assert.Quality != nil {
		all = append(all, EvalQuality(ctx, result.Output, assert.Quality, judge)...)
	}
	return all, nil
}

// MockQualityJudge is a test double for QualityJudge.
type MockQualityJudge struct {
	Result *QualityResult
	Err    error
}

func (m *MockQualityJudge) Judge(_ context.Context, _, _ string) (*QualityResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Result, nil
}

func qualityMessage(qr *QualityResult) string {
	if qr.Passed {
		return fmt.Sprintf("quality check passed (score=%.2f): %s", qr.Score, qr.Reason)
	}
	return fmt.Sprintf("quality check failed (score=%.2f): %s", qr.Score, qr.Reason)
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
