package agtest

import (
	"fmt"
	"strings"
)

// ValidateTier1 enforces the Tier1 (deterministic) assertion discipline on top
// of the base Validate. It is deliberately NOT wired into the generic parser /
// `rnix agtest` command (that would make a general-purpose command hardcode
// suite-directory semantics — Story 68.2 裁决 2). Instead the repository guard
// test (tier1_guard_test.go) walks the real tests/agtest/tier1/ suite and calls
// this, so a non-conforming case can never land on main; Story 68.3 will expose
// the same function behind an opt-in `rnix agtest --tier1` flag.
//
// The four rules (裁决 2):
//  1. assert.quality present  → reject (Tier1 forbids LLM-judge nondeterminism)
//  2. assert nil, or output AND syscalls both empty → reject (an assertion-less
//     case has no regression value in Tier1 — only checking exit code is too weak)
//  3. agent.provider != "replay" → reject (Tier1 must run the deterministic
//     driver; the provider instance name is fixed to "replay" by convention)
//  4. heuristic non-deterministic value check: any output contains/not_contains
//     or syscalls includes/excludes string that starts with "/" or contains
//     "/home/" or "/tmp/" → reject (absolute paths are machine-dependent). The
//     check face is intentionally narrow (no PID/timestamp heuristics — those
//     are unreliable to detect by machine and are left to README discipline +
//     review) to avoid false-positives on legitimate relative-path assertions.
func ValidateTier1(suite *TestSuiteSpec) error {
	// Exported entry point (Story 68.3 will call it behind `rnix agtest
	// --tier1`), so guard the degenerate inputs the guard test never feeds it: a
	// nil suite would panic on the range below, and an empty suite would pass
	// vacuously — neither has any Tier1 regression value.
	if suite == nil {
		return ValidationErrors{{Field: "suite", Message: "Tier1 validation requires a non-nil suite"}}
	}
	if len(suite.Tests) == 0 {
		return ValidationErrors{{Field: "tests", Message: "Tier1 validation requires at least one test case"}}
	}

	var errs ValidationErrors

	for i := range suite.Tests {
		tc := &suite.Tests[i]
		prefix := fmt.Sprintf("tests[%d].", i)
		if len(suite.Tests) == 1 {
			prefix = ""
		}
		name := tc.Name
		if name == "" {
			name = fmt.Sprintf("test-%d", i+1)
		}

		// Rule 3: provider must be the deterministic replay driver.
		if tc.Agent.Provider != "replay" {
			errs = append(errs, ValidationError{
				Field:   prefix + "agent.provider",
				Message: fmt.Sprintf("Tier1 requires provider \"replay\", got %q (case %q)", tc.Agent.Provider, name),
			})
		}

		// Rule 1 + 2: assertion discipline.
		if tc.Assert == nil {
			errs = append(errs, ValidationError{
				Field:   prefix + "assert",
				Message: fmt.Sprintf("Tier1 requires at least one output/syscalls assertion (case %q has none)", name),
			})
			continue
		}
		if tc.Assert.Quality != nil {
			errs = append(errs, ValidationError{
				Field:   prefix + "assert.quality",
				Message: fmt.Sprintf("Tier1 forbids quality (LLM-judge) assertions (case %q)", name),
			})
		}
		hasOutput := tc.Assert.Output != nil &&
			(len(tc.Assert.Output.Contains) > 0 || len(tc.Assert.Output.NotContains) > 0)
		hasSyscalls := tc.Assert.Syscalls != nil &&
			(len(tc.Assert.Syscalls.Includes) > 0 || len(tc.Assert.Syscalls.Excludes) > 0)
		if !hasOutput && !hasSyscalls {
			errs = append(errs, ValidationError{
				Field:   prefix + "assert",
				Message: fmt.Sprintf("Tier1 requires a non-empty output or syscalls assertion (case %q)", name),
			})
		}

		// Rule 4: heuristic absolute-path / machine-dependent value check.
		if tc.Assert.Output != nil {
			errs = append(errs, checkNonDeterministic(tc.Assert.Output.Contains, prefix+"assert.output.contains", name)...)
			errs = append(errs, checkNonDeterministic(tc.Assert.Output.NotContains, prefix+"assert.output.not_contains", name)...)
		}
		if tc.Assert.Syscalls != nil {
			errs = append(errs, checkNonDeterministic(tc.Assert.Syscalls.Includes, prefix+"assert.syscalls.includes", name)...)
			errs = append(errs, checkNonDeterministic(tc.Assert.Syscalls.Excludes, prefix+"assert.syscalls.excludes", name)...)
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// checkNonDeterministic flags assertion strings that look machine-dependent.
func checkNonDeterministic(values []string, field, caseName string) ValidationErrors {
	var errs ValidationErrors
	for _, v := range values {
		if strings.HasPrefix(v, "/") || strings.Contains(v, "/home/") || strings.Contains(v, "/tmp/") {
			errs = append(errs, ValidationError{
				Field:   field,
				Message: fmt.Sprintf("Tier1 forbids absolute/machine-dependent path in assertion %q (case %q)", v, caseName),
			})
		}
	}
	return errs
}
