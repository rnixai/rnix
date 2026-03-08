package agtest

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate_ValidSpec(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests: []TestCaseSpec{
			{Intent: "hello", Agent: AgentConfig{Name: "greeter"}},
		},
	}
	if err := Validate(suite, nil); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidate_MissingIntent(t *testing.T) {
	data := []byte(`version: "1.0"
agent:
  name: "greeter"
`)
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests:   []TestCaseSpec{{Agent: AgentConfig{Name: "greeter"}}},
	}
	err := Validate(suite, data)
	if err == nil {
		t.Fatal("expected validation error for missing intent")
	}
	assertContainsField(t, err, "intent")
}

func TestValidate_EmptyIntent(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests:   []TestCaseSpec{{Intent: "", Agent: AgentConfig{Name: "greeter"}}},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for empty intent")
	}
	assertContainsField(t, err, "intent")
}

func TestValidate_MissingAgentName(t *testing.T) {
	data := []byte(`version: "1.0"
intent: "hello"
agent:
  model: "claude"
`)
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests:   []TestCaseSpec{{Intent: "hello", Agent: AgentConfig{Model: "claude"}}},
	}
	err := Validate(suite, data)
	if err == nil {
		t.Fatal("expected validation error for missing agent.name")
	}
	assertContainsField(t, err, "agent.name")
}

func TestValidate_EmptyAgentName(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests:   []TestCaseSpec{{Intent: "hello", Agent: AgentConfig{Name: ""}}},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for empty agent.name")
	}
	assertContainsField(t, err, "agent.name")
}

func TestValidate_InvalidVersion(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "2.0",
		Tests:   []TestCaseSpec{{Intent: "hello", Agent: AgentConfig{Name: "greeter"}}},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for invalid version")
	}
	assertContainsField(t, err, "version")
}

func TestValidate_MissingVersion(t *testing.T) {
	suite := &TestSuiteSpec{
		Tests: []TestCaseSpec{{Intent: "hello", Agent: AgentConfig{Name: "greeter"}}},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for missing version")
	}
	assertContainsField(t, err, "version")
}

func TestValidate_MultipleErrors(t *testing.T) {
	data := []byte(`version: "1.0"
name: "multi-error"
agent:
  model: "claude"
`)
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests:   []TestCaseSpec{{Agent: AgentConfig{Model: "claude"}}},
	}
	err := Validate(suite, data)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(ve) < 2 {
		t.Errorf("expected at least 2 errors (intent + agent.name), got %d", len(ve))
	}
	hasIntent := false
	hasAgent := false
	for _, e := range ve {
		if strings.Contains(e.Field, "intent") {
			hasIntent = true
		}
		if strings.Contains(e.Field, "agent.name") {
			hasAgent = true
		}
	}
	if !hasIntent {
		t.Error("missing intent error")
	}
	if !hasAgent {
		t.Error("missing agent.name error")
	}
}

func TestValidate_WithLineNumbers(t *testing.T) {
	data := []byte(`version: "1.0"
name: "line-test"
agent:
  name: "greeter"
`)
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests:   []TestCaseSpec{{Agent: AgentConfig{Name: "greeter"}}},
	}
	err := Validate(suite, data)
	if err == nil {
		t.Fatal("expected validation error for missing intent")
	}
	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	// Intent is missing, so there's no line for it, but the error should still be reported.
	// The line number for a missing field is 0 (unknown) since the key doesn't exist in the YAML.
	for _, e := range ve {
		if e.Field == "intent" {
			return
		}
	}
	t.Error("expected validation error for field 'intent'")
}

func TestValidate_WithLineNumbers_ExistingField(t *testing.T) {
	data := []byte(`version: "2.0"
name: "bad-version"
intent: "hello"
agent:
  name: "greeter"
`)
	suite := &TestSuiteSpec{
		Version: "2.0",
		Tests:   []TestCaseSpec{{Version: "2.0", Intent: "hello", Agent: AgentConfig{Name: "greeter"}}},
	}
	err := Validate(suite, data)
	if err == nil {
		t.Fatal("expected validation error for bad version")
	}
	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	for _, e := range ve {
		if e.Field == "version" && e.Line > 0 {
			return
		}
	}
	t.Error("expected version error with line number > 0")
}

func TestValidate_SuiteMultipleTests(t *testing.T) {
	data := []byte(`version: "1.0"
tests:
  - name: "ok-test"
    intent: "hello"
    agent:
      name: "greeter"
  - name: "bad-test"
    agent:
      name: "greeter"
`)
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests: []TestCaseSpec{
			{Intent: "hello", Agent: AgentConfig{Name: "greeter"}},
			{Agent: AgentConfig{Name: "greeter"}},
		},
	}
	err := Validate(suite, data)
	if err == nil {
		t.Fatal("expected validation error for second test missing intent")
	}
	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(ve) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(ve), ve)
	}
	if !strings.Contains(ve[0].Field, "tests[1]") {
		t.Errorf("expected tests[1] prefix, got field = %q", ve[0].Field)
	}
}

func TestValidationError_ErrorString(t *testing.T) {
	e := &ValidationError{Field: "intent", Message: "required", Line: 5}
	s := e.Error()
	if !strings.Contains(s, "line 5") {
		t.Errorf("expected 'line 5' in %q", s)
	}
	if !strings.Contains(s, "intent") {
		t.Errorf("expected 'intent' in %q", s)
	}

	e2 := &ValidationError{Field: "agent.name", Message: "required", Line: 0}
	s2 := e2.Error()
	if strings.Contains(s2, "line") {
		t.Errorf("should not contain 'line' when Line=0: %q", s2)
	}
}

func TestValidationErrors_ErrorString(t *testing.T) {
	errs := ValidationErrors{
		{Field: "intent", Message: "required", Line: 3},
		{Field: "agent.name", Message: "required", Line: 5},
	}
	s := errs.Error()
	if !strings.Contains(s, "2 errors") {
		t.Errorf("expected '2 errors' in %q", s)
	}
	if !strings.Contains(s, "intent") || !strings.Contains(s, "agent.name") {
		t.Errorf("expected both field names in %q", s)
	}
}

func TestValidate_EmptyTestsArray(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests:   []TestCaseSpec{},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for empty tests array")
	}
	assertContainsField(t, err, "tests")
}

func TestValidate_AssertOutputEmptyBoth_Fail(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests: []TestCaseSpec{{
			Intent: "hello",
			Agent:  AgentConfig{Name: "greeter"},
			Assert: &AssertConfig{Output: &OutputAssert{}},
		}},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for empty output assert")
	}
	assertContainsField(t, err, "assert.output")
}

func TestValidate_AssertSyscallsEmptyBoth_Fail(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests: []TestCaseSpec{{
			Intent: "hello",
			Agent:  AgentConfig{Name: "greeter"},
			Assert: &AssertConfig{Syscalls: &SyscallAssert{}},
		}},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for empty syscalls assert")
	}
	assertContainsField(t, err, "assert.syscalls")
}

func TestValidate_AssertQualityEmptyCriteria_Fail(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests: []TestCaseSpec{{
			Intent: "hello",
			Agent:  AgentConfig{Name: "greeter"},
			Assert: &AssertConfig{Quality: &QualityAssert{Criteria: ""}},
		}},
	}
	err := Validate(suite, nil)
	if err == nil {
		t.Fatal("expected validation error for empty quality criteria")
	}
	assertContainsField(t, err, "assert.quality.criteria")
}

func TestValidate_ValidAssert_Pass(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests: []TestCaseSpec{{
			Intent: "hello",
			Agent:  AgentConfig{Name: "greeter"},
			Assert: &AssertConfig{
				Output:   &OutputAssert{Contains: []string{"hi"}},
				Syscalls: &SyscallAssert{Includes: []string{"Read"}},
				Quality:  &QualityAssert{Criteria: "must greet"},
			},
		}},
	}
	if err := Validate(suite, nil); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidate_AssertMixed_Pass(t *testing.T) {
	suite := &TestSuiteSpec{
		Version: "1.0",
		Tests: []TestCaseSpec{{
			Intent: "hello",
			Agent:  AgentConfig{Name: "greeter"},
			Assert: &AssertConfig{
				Output: &OutputAssert{NotContains: []string{"ERROR"}},
			},
		}},
	}
	if err := Validate(suite, nil); err != nil {
		t.Fatalf("expected no error for valid partial assert, got: %v", err)
	}
}

func assertContainsField(t *testing.T, err error, field string) {
	t.Helper()
	var ve ValidationErrors
	if errors.As(err, &ve) {
		for _, e := range ve {
			if strings.Contains(e.Field, field) {
				return
			}
		}
	}
	t.Errorf("expected error containing field %q, got: %v", field, err)
}
