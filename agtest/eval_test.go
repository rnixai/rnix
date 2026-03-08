package agtest

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEvalOutput_ContainsAll_Pass(t *testing.T) {
	results := EvalOutput("hello world, 代码示例 included", &OutputAssert{
		Contains: []string{"hello", "代码示例"},
	})
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected pass for %q, got fail: %s", r.Expected, r.Message)
		}
		if r.Type != "output" {
			t.Errorf("type = %q, want output", r.Type)
		}
	}
}

func TestEvalOutput_ContainsMissing_Fail(t *testing.T) {
	results := EvalOutput("hello world", &OutputAssert{
		Contains: []string{"hello", "missing-text"},
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("first check (hello) should pass")
	}
	if results[1].Passed {
		t.Error("second check (missing-text) should fail")
	}
	if results[1].Message == "" {
		t.Error("failure message should not be empty")
	}
}

func TestEvalOutput_NotContainsFound_Fail(t *testing.T) {
	results := EvalOutput("ERROR: something broke", &OutputAssert{
		NotContains: []string{"ERROR"},
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("should fail when unwanted text is found")
	}
}

func TestEvalOutput_Mixed(t *testing.T) {
	output := "hello world, success"
	results := EvalOutput(output, &OutputAssert{
		Contains:    []string{"hello", "success"},
		NotContains: []string{"ERROR", "failure"},
	})
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("all checks should pass, but %q failed: %s", r.Expected, r.Message)
		}
	}
}

func TestEvalOutput_NilAssert(t *testing.T) {
	results := EvalOutput("any output", nil)
	if results != nil {
		t.Errorf("expected nil for nil assert, got %v", results)
	}
}

func TestEvalSyscalls_IncludesAll_Pass(t *testing.T) {
	events := []string{"Spawn", "CtxWrite", "Read", "Write", "Kill"}
	results := EvalSyscalls(events, &SyscallAssert{
		Includes: []string{"Read", "Write"},
	})
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected pass for %q, got fail: %s", r.Expected, r.Message)
		}
		if r.Type != "syscalls" {
			t.Errorf("type = %q, want syscalls", r.Type)
		}
	}
}

func TestEvalSyscalls_IncludesMissing_Fail(t *testing.T) {
	events := []string{"Spawn", "CtxWrite"}
	results := EvalSyscalls(events, &SyscallAssert{
		Includes: []string{"Read", "Write"},
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Passed {
			t.Errorf("should fail for missing syscall %q", r.Expected)
		}
	}
}

func TestEvalSyscalls_ExcludesFound_Fail(t *testing.T) {
	events := []string{"Spawn", "Kill", "Read"}
	results := EvalSyscalls(events, &SyscallAssert{
		Excludes: []string{"Kill"},
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("should fail when excluded syscall is found")
	}
}

func TestEvalSyscalls_Partial(t *testing.T) {
	events := []string{"Spawn", "Read"}
	results := EvalSyscalls(events, &SyscallAssert{
		Includes: []string{"Read", "Write"},
		Excludes: []string{"Kill"},
	})
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Read found -> pass, Write missing -> fail, Kill absent -> pass
	passCount := 0
	for _, r := range results {
		if r.Passed {
			passCount++
		}
	}
	if passCount != 2 {
		t.Errorf("expected 2 passes, got %d", passCount)
	}
}

func TestEvalSyscalls_NilAssert(t *testing.T) {
	results := EvalSyscalls([]string{"Spawn"}, nil)
	if results != nil {
		t.Errorf("expected nil for nil assert, got %v", results)
	}
}

func TestEvalQuality_Pass(t *testing.T) {
	judge := &MockQualityJudge{
		Result: &QualityResult{Passed: true, Score: 0.95, Reason: "well structured"},
	}
	results := EvalQuality(context.Background(), "some output", &QualityAssert{Criteria: "must be good"}, judge)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("expected pass")
	}
	if results[0].Type != "quality" {
		t.Errorf("type = %q, want quality", results[0].Type)
	}
}

func TestEvalQuality_Fail(t *testing.T) {
	judge := &MockQualityJudge{
		Result: &QualityResult{Passed: false, Score: 0.3, Reason: "lacks detail"},
	}
	results := EvalQuality(context.Background(), "poor output", &QualityAssert{Criteria: "must be detailed"}, judge)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected fail")
	}
	if results[0].Message == "" {
		t.Error("failure message should contain reason")
	}
}

func TestEvalQuality_JudgeError(t *testing.T) {
	judge := &MockQualityJudge{
		Err: errors.New("LLM unavailable"),
	}
	results := EvalQuality(context.Background(), "output", &QualityAssert{Criteria: "criteria"}, judge)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("should fail on judge error")
	}
}

func TestEvalQuality_NilAssert(t *testing.T) {
	judge := &MockQualityJudge{Result: &QualityResult{Passed: true}}
	results := EvalQuality(context.Background(), "output", nil, judge)
	if results != nil {
		t.Errorf("expected nil for nil assert, got %v", results)
	}
}

func TestEvalQuality_NilResult(t *testing.T) {
	judge := &MockQualityJudge{Result: nil, Err: nil}
	results := EvalQuality(context.Background(), "output", &QualityAssert{Criteria: "check"}, judge)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("should fail when judge returns nil result")
	}
	if !strings.Contains(results[0].Message, "nil result") {
		t.Errorf("message should mention nil result, got %q", results[0].Message)
	}
}

func TestEvalAssertions_NilAssert(t *testing.T) {
	tr := &TestResult{Output: "hello", Syscalls: []string{"Spawn"}}
	results, err := EvalAssertions(context.Background(), tr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for nil assert, got %v", results)
	}
}

func TestEvalAssertions_OutputOnly(t *testing.T) {
	tr := &TestResult{Output: "hello world"}
	ac := &AssertConfig{Output: &OutputAssert{Contains: []string{"hello"}}}
	results, err := EvalAssertions(context.Background(), tr, ac, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("expected pass")
	}
}

func TestEvalAssertions_SyscallsOnly(t *testing.T) {
	tr := &TestResult{Syscalls: []string{"Spawn", "Read"}}
	ac := &AssertConfig{Syscalls: &SyscallAssert{Includes: []string{"Read"}}}
	results, err := EvalAssertions(context.Background(), tr, ac, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || !results[0].Passed {
		t.Error("expected single pass result")
	}
}

func TestEvalAssertions_QualityOnly(t *testing.T) {
	judge := &MockQualityJudge{
		Result: &QualityResult{Passed: true, Score: 0.9, Reason: "good"},
	}
	tr := &TestResult{Output: "detailed output"}
	ac := &AssertConfig{Quality: &QualityAssert{Criteria: "must be detailed"}}
	results, err := EvalAssertions(context.Background(), tr, ac, judge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || !results[0].Passed {
		t.Error("expected single pass result")
	}
}

func TestEvalAssertions_AllThree(t *testing.T) {
	judge := &MockQualityJudge{
		Result: &QualityResult{Passed: true, Score: 0.85, Reason: "acceptable"},
	}
	tr := &TestResult{
		Output:   "hello world with details",
		Syscalls: []string{"Spawn", "Read", "CtxWrite"},
	}
	ac := &AssertConfig{
		Output:   &OutputAssert{Contains: []string{"hello"}, NotContains: []string{"ERROR"}},
		Syscalls: &SyscallAssert{Includes: []string{"Read"}, Excludes: []string{"Kill"}},
		Quality:  &QualityAssert{Criteria: "should be acceptable"},
	}
	results, err := EvalAssertions(context.Background(), tr, ac, judge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results (2 output + 2 syscall + 1 quality), got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected all pass, got fail: type=%s msg=%s", r.Type, r.Message)
		}
	}
}

func TestEvalAssertions_NilJudgeWithQuality(t *testing.T) {
	tr := &TestResult{Output: "output"}
	ac := &AssertConfig{Quality: &QualityAssert{Criteria: "check quality"}}
	_, err := EvalAssertions(context.Background(), tr, ac, nil)
	if err == nil {
		t.Fatal("expected error when judge is nil but quality assertion present")
	}
}
