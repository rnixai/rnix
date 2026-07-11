package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SLASpec declares post-hoc contract constraints for an agent run. These are
// NOT runtime guards: they neither terminate execution nor gate scheduling.
// After an agent completes, compose calls Evaluate (compose/engine.go) to score
// the run against these limits and feeds the result to the ReputationStore for
// reputation scoring. Runtime token enforcement is a separate mechanism —
// proc.Budget.MaxTokens (over-limit → selfSuspend), distinct from SLASpec.MaxTokens.
type SLASpec struct {
	MaxTokens     int    `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	MaxDurationMs int64  `yaml:"max_duration_ms,omitempty" json:"max_duration_ms,omitempty"`
	OutputFormat  string `yaml:"output_format,omitempty" json:"output_format,omitempty"` // "json" | "markdown" | "" (any)
}

// SLACheckResult records the result of a single SLA constraint check.
type SLACheckResult struct {
	Name   string `json:"name"`   // check name ("max_tokens", "max_duration_ms", "output_format")
	Passed bool   `json:"passed"`
	Actual string `json:"actual"` // actual value as string
	Limit  string `json:"limit"`  // SLA limit value as string
}

// SLAResult records the complete SLA evaluation result.
type SLAResult struct {
	AgentName   string           `json:"agent_name"`
	Passed      bool             `json:"passed"` // true if all checks passed
	Checks      []SLACheckResult `json:"checks"`
	EvaluatedAt time.Time        `json:"evaluated_at"`
	TokensUsed  int              `json:"tokens_used"`
	DurationMs  int64            `json:"duration_ms"`
}

// IsEmpty returns true if no SLA constraints are defined.
func (s *SLASpec) IsEmpty() bool {
	return s.MaxTokens == 0 && s.MaxDurationMs == 0 && s.OutputFormat == ""
}

// Evaluate scores a completed run against the SLA constraints. This is a
// post-hoc contract check, not a runtime guard — it never terminates the
// process or gates scheduling; the returned SLAResult is fed to the
// ReputationStore as a reputation input. Only non-zero constraints are
// evaluated. Returns an SLAResult with individual check results and an
// overall Passed flag.
func (s *SLASpec) Evaluate(agentName string, tokensUsed int, durationMs int64, output string) *SLAResult {
	result := &SLAResult{
		AgentName:   agentName,
		Passed:      true,
		EvaluatedAt: time.Now(),
		TokensUsed:  tokensUsed,
		DurationMs:  durationMs,
	}

	if s.MaxTokens > 0 {
		passed := tokensUsed <= s.MaxTokens
		result.Checks = append(result.Checks, SLACheckResult{
			Name:   "max_tokens",
			Passed: passed,
			Actual: fmt.Sprintf("%d", tokensUsed),
			Limit:  fmt.Sprintf("%d", s.MaxTokens),
		})
		if !passed {
			result.Passed = false
		}
	}

	if s.MaxDurationMs > 0 {
		passed := durationMs <= s.MaxDurationMs
		result.Checks = append(result.Checks, SLACheckResult{
			Name:   "max_duration_ms",
			Passed: passed,
			Actual: fmt.Sprintf("%d", durationMs),
			Limit:  fmt.Sprintf("%d", s.MaxDurationMs),
		})
		if !passed {
			result.Passed = false
		}
	}

	if s.OutputFormat != "" {
		passed := checkOutputFormat(s.OutputFormat, output)
		actual := s.OutputFormat
		if !passed {
			actual = fmt.Sprintf("invalid_%s", s.OutputFormat)
		}
		result.Checks = append(result.Checks, SLACheckResult{
			Name:   "output_format",
			Passed: passed,
			Actual: actual,
			Limit:  s.OutputFormat,
		})
		if !passed {
			result.Passed = false
		}
	}

	return result
}

// checkOutputFormat validates output against the expected format.
func checkOutputFormat(format, output string) bool {
	switch strings.ToLower(format) {
	case "json":
		return json.Valid([]byte(output))
	case "markdown":
		for line := range strings.SplitSeq(output, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				return true
			}
		}
		return false
	default:
		return true
	}
}
