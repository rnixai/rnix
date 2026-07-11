package agtest

import (
	"errors"
	"strings"
	"testing"
)

// tier1ValidCase returns a minimal Tier1-conforming case that all rules pass.
func tier1ValidCase() TestCaseSpec {
	return TestCaseSpec{
		Version: "1.0",
		Name:    "ok",
		Intent:  "run echo",
		Agent:   AgentConfig{Provider: "replay", Model: "scripts/ok.responses.yaml"},
		Assert: &AssertConfig{
			Output:   &OutputAssert{Contains: []string{"hi from replay"}},
			Syscalls: &SyscallAssert{Includes: []string{"ReasonStep"}},
		},
	}
}

func tier1Suite(tc TestCaseSpec) *TestSuiteSpec {
	return &TestSuiteSpec{Version: "1.0", Tests: []TestCaseSpec{tc}}
}

func TestValidateTier1_ValidCasePasses(t *testing.T) {
	if err := ValidateTier1(tier1Suite(tier1ValidCase())); err != nil {
		t.Fatalf("valid Tier1 case should pass, got: %v", err)
	}
}

func TestValidateTier1_RejectsQuality(t *testing.T) {
	tc := tier1ValidCase()
	tc.Assert.Quality = &QualityAssert{Criteria: "is it good"}
	err := ValidateTier1(tier1Suite(tc))
	if err == nil {
		t.Fatal("expected rejection of quality assertion")
	}
	if !strings.Contains(err.Error(), "quality") {
		t.Errorf("error should mention quality, got: %v", err)
	}
}

func TestValidateTier1_RejectsNilAssert(t *testing.T) {
	tc := tier1ValidCase()
	tc.Assert = nil
	err := ValidateTier1(tier1Suite(tc))
	if err == nil {
		t.Fatal("expected rejection of nil assert")
	}
}

func TestValidateTier1_RejectsEmptyAssert(t *testing.T) {
	tc := tier1ValidCase()
	tc.Assert = &AssertConfig{} // no output, no syscalls
	err := ValidateTier1(tier1Suite(tc))
	if err == nil {
		t.Fatal("expected rejection of empty assert (no output/syscalls)")
	}
}

func TestValidateTier1_RejectsNonReplayProvider(t *testing.T) {
	tc := tier1ValidCase()
	tc.Agent.Provider = "claude"
	err := ValidateTier1(tier1Suite(tc))
	if err == nil {
		t.Fatal("expected rejection of non-replay provider")
	}
	if !strings.Contains(err.Error(), "replay") {
		t.Errorf("error should mention replay requirement, got: %v", err)
	}
}

func TestValidateTier1_RejectsEmptyProvider(t *testing.T) {
	tc := tier1ValidCase()
	tc.Agent.Provider = ""
	err := ValidateTier1(tier1Suite(tc))
	if err == nil {
		t.Fatal("expected rejection of empty provider")
	}
}

func TestValidateTier1_RejectsAbsolutePathAssertion(t *testing.T) {
	cases := []string{"/home/decker/x", "/tmp/out.txt", "/usr/bin/echo"}
	for _, bad := range cases {
		tc := tier1ValidCase()
		tc.Assert.Output.Contains = []string{bad}
		err := ValidateTier1(tier1Suite(tc))
		if err == nil {
			t.Fatalf("expected rejection of absolute-path assertion %q", bad)
		}
	}
}

func TestValidateTier1_RejectsHomePathSubstring(t *testing.T) {
	tc := tier1ValidCase()
	tc.Assert.Syscalls.Includes = []string{"prefix/home/decker/leak"}
	err := ValidateTier1(tier1Suite(tc))
	if err == nil {
		t.Fatal("expected rejection of /home/ substring in assertion")
	}
}

func TestValidateTier1_AllowsRelativePathAssertion(t *testing.T) {
	tc := tier1ValidCase()
	// Relative-looking strings (no leading slash, no /home//tmp) are allowed.
	tc.Assert.Output.Contains = []string{"scripts/output", "relative-value"}
	if err := ValidateTier1(tier1Suite(tc)); err != nil {
		t.Fatalf("relative-path assertion should pass, got: %v", err)
	}
}

func TestValidateTier1_AggregatesMultipleErrors(t *testing.T) {
	tc := tier1ValidCase()
	tc.Agent.Provider = "claude"                          // rule 3
	tc.Assert.Quality = &QualityAssert{Criteria: "good"}  // rule 1
	tc.Assert.Output.Contains = []string{"/abs/path"}     // rule 4
	err := ValidateTier1(tier1Suite(tc))
	if err == nil {
		t.Fatal("expected aggregated errors")
	}
	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(ve) < 3 {
		t.Errorf("expected at least 3 aggregated errors, got %d: %v", len(ve), ve)
	}
}
