package kernel

import (
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 21.2: 合约 SLA 与评估
//
// SLA 数据模型与评估逻辑测试。
// 测试引用 SLASpec, SLACheckResult, SLAResult 等类型和方法，
// 这些类型尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 实现 kernel/sla.go 中的类型和方法，测试编译并通过。
// ============================================================

// --- 21.2-UNIT-001: [P0] SLASpec.IsEmpty returns true for zero-value struct (AC1/AC5) ---

func TestSLASpec_IsEmpty(t *testing.T) {
	// Given: a zero-value SLASpec with no constraints
	spec := SLASpec{}

	// When: checking if empty
	empty := spec.IsEmpty()

	// Then: should return true
	if !empty {
		t.Fatal("expected zero-value SLASpec to be empty")
	}
}

// --- 21.2-UNIT-002: [P0] SLASpec.IsEmpty returns false when constraints exist (AC1) ---

func TestSLASpec_IsEmpty_WithConstraints(t *testing.T) {
	tests := []struct {
		name string
		spec SLASpec
	}{
		{
			name: "max_tokens set",
			spec: SLASpec{MaxTokens: 10000},
		},
		{
			name: "max_duration_ms set",
			spec: SLASpec{MaxDurationMs: 60000},
		},
		{
			name: "output_format set",
			spec: SLASpec{OutputFormat: "json"},
		},
		{
			name: "all fields set",
			spec: SLASpec{MaxTokens: 10000, MaxDurationMs: 60000, OutputFormat: "json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.spec.IsEmpty() {
				t.Fatalf("SLASpec with %s should not be empty", tt.name)
			}
		})
	}
}

// --- 21.2-UNIT-003: [P0] SLASpec.Evaluate all constraints pass (AC2) ---

func TestSLASpec_Evaluate_AllPassed(t *testing.T) {
	// Given: SLA with all constraints, actual values within limits
	spec := SLASpec{
		MaxTokens:    15000,
		MaxDurationMs: 60000,
		OutputFormat:  "json",
	}

	validJSON := `{"result": "success", "data": [1, 2, 3]}`

	// When: evaluating with values within limits
	result := spec.Evaluate("reviewer", 10000, 30000, validJSON)

	// Then: all checks pass
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if !result.Passed {
		t.Fatal("expected SLAResult.Passed to be true when all constraints met")
	}
	if result.AgentName != "reviewer" {
		t.Fatalf("expected AgentName 'reviewer', got %q", result.AgentName)
	}
	if result.TokensUsed != 10000 {
		t.Fatalf("expected TokensUsed 10000, got %d", result.TokensUsed)
	}
	if result.DurationMs != 30000 {
		t.Fatalf("expected DurationMs 30000, got %d", result.DurationMs)
	}
	if len(result.Checks) != 3 {
		t.Fatalf("expected 3 checks, got %d", len(result.Checks))
	}
	for _, check := range result.Checks {
		if !check.Passed {
			t.Fatalf("expected check %q to pass", check.Name)
		}
	}
	if result.EvaluatedAt.IsZero() {
		t.Fatal("expected non-zero EvaluatedAt timestamp")
	}
}

// --- 21.2-UNIT-004: [P0] SLASpec.Evaluate token exceeded (AC2) ---

func TestSLASpec_Evaluate_TokenExceeded(t *testing.T) {
	// Given: SLA with max_tokens constraint
	spec := SLASpec{MaxTokens: 10000}

	// When: actual tokens exceed limit
	result := spec.Evaluate("agent", 15000, 1000, "output")

	// Then: overall result fails, token check fails
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if result.Passed {
		t.Fatal("expected SLAResult.Passed to be false when tokens exceeded")
	}

	// Find the max_tokens check
	found := false
	for _, check := range result.Checks {
		if check.Name == "max_tokens" {
			found = true
			if check.Passed {
				t.Fatal("expected max_tokens check to fail")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected max_tokens check in results")
	}
}

// --- 21.2-UNIT-005: [P0] SLASpec.Evaluate duration exceeded (AC2) ---

func TestSLASpec_Evaluate_DurationExceeded(t *testing.T) {
	// Given: SLA with max_duration_ms constraint
	spec := SLASpec{MaxDurationMs: 30000}

	// When: actual duration exceeds limit
	result := spec.Evaluate("agent", 1000, 60000, "output")

	// Then: overall result fails, duration check fails
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if result.Passed {
		t.Fatal("expected SLAResult.Passed to be false when duration exceeded")
	}

	found := false
	for _, check := range result.Checks {
		if check.Name == "max_duration_ms" {
			found = true
			if check.Passed {
				t.Fatal("expected max_duration_ms check to fail")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected max_duration_ms check in results")
	}
}

// --- 21.2-UNIT-006: [P0] SLASpec.Evaluate output format JSON valid (AC2) ---

func TestSLASpec_Evaluate_OutputFormatJSON_Valid(t *testing.T) {
	// Given: SLA requiring JSON output format
	spec := SLASpec{OutputFormat: "json"}
	validJSON := `{"key": "value", "num": 42}`

	// When: evaluating with valid JSON output
	result := spec.Evaluate("agent", 0, 0, validJSON)

	// Then: output_format check passes
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if !result.Passed {
		t.Fatal("expected SLAResult.Passed to be true for valid JSON")
	}
	for _, check := range result.Checks {
		if check.Name == "output_format" && !check.Passed {
			t.Fatal("expected output_format check to pass for valid JSON")
		}
	}
}

// --- 21.2-UNIT-007: [P1] SLASpec.Evaluate output format JSON invalid (AC2) ---

func TestSLASpec_Evaluate_OutputFormatJSON_Invalid(t *testing.T) {
	// Given: SLA requiring JSON output format
	spec := SLASpec{OutputFormat: "json"}
	invalidJSON := "this is not json at all"

	// When: evaluating with non-JSON output
	result := spec.Evaluate("agent", 0, 0, invalidJSON)

	// Then: output_format check fails
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if result.Passed {
		t.Fatal("expected SLAResult.Passed to be false for invalid JSON")
	}
	found := false
	for _, check := range result.Checks {
		if check.Name == "output_format" {
			found = true
			if check.Passed {
				t.Fatal("expected output_format check to fail for invalid JSON")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected output_format check in results")
	}
}

// --- 21.2-UNIT-008: [P1] SLASpec.Evaluate output format markdown valid (AC2) ---

func TestSLASpec_Evaluate_OutputFormatMarkdown_Valid(t *testing.T) {
	// Given: SLA requiring markdown output format
	spec := SLASpec{OutputFormat: "markdown"}
	validMarkdown := "# Review Summary\n\nCode looks good.\n\n## Details\n\nNo issues found."

	// When: evaluating with valid markdown (contains # heading)
	result := spec.Evaluate("agent", 0, 0, validMarkdown)

	// Then: output_format check passes
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if !result.Passed {
		t.Fatal("expected SLAResult.Passed to be true for valid markdown")
	}
}

// --- 21.2-UNIT-009: [P1] SLASpec.Evaluate output format markdown invalid (AC2) ---

func TestSLASpec_Evaluate_OutputFormatMarkdown_Invalid(t *testing.T) {
	// Given: SLA requiring markdown output format
	spec := SLASpec{OutputFormat: "markdown"}
	noMarkdown := "plain text without any markdown headings"

	// When: evaluating with non-markdown output
	result := spec.Evaluate("agent", 0, 0, noMarkdown)

	// Then: output_format check fails
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if result.Passed {
		t.Fatal("expected SLAResult.Passed to be false for non-markdown output")
	}
}

// --- 21.2-UNIT-010: [P0] SLASpec.Evaluate with no constraints passes (AC5) ---

func TestSLASpec_Evaluate_NoConstraints(t *testing.T) {
	// Given: SLASpec with no constraints (all zero values)
	spec := SLASpec{}

	// When: evaluating with any values
	result := spec.Evaluate("agent", 99999, 99999, "anything")

	// Then: result passes (no constraints to violate)
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if !result.Passed {
		t.Fatal("expected SLAResult.Passed to be true when no constraints defined")
	}
	if len(result.Checks) != 0 {
		t.Fatalf("expected 0 checks for no constraints, got %d", len(result.Checks))
	}
}

// --- 21.2-UNIT-011: [P1] SLASpec.Evaluate multiple failures simultaneously (AC2) ---

func TestSLASpec_Evaluate_MultipleFailures(t *testing.T) {
	// Given: SLA with all constraints
	spec := SLASpec{
		MaxTokens:    5000,
		MaxDurationMs: 10000,
		OutputFormat:  "json",
	}

	// When: all constraints are violated
	result := spec.Evaluate("agent", 20000, 50000, "not json")

	// Then: overall fails, multiple individual checks fail
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if result.Passed {
		t.Fatal("expected SLAResult.Passed to be false when multiple constraints violated")
	}

	failCount := 0
	for _, check := range result.Checks {
		if !check.Passed {
			failCount++
		}
	}
	if failCount < 3 {
		t.Fatalf("expected at least 3 failed checks, got %d", failCount)
	}
}

// --- 21.2-UNIT-012: [P1] SLASpec.Evaluate boundary - tokens exactly at limit (AC2) ---

func TestSLASpec_Evaluate_TokenExactBoundary(t *testing.T) {
	// Given: SLA with max_tokens constraint
	spec := SLASpec{MaxTokens: 10000}

	// When: tokens exactly equal the limit
	result := spec.Evaluate("agent", 10000, 0, "")

	// Then: should pass (<=)
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if !result.Passed {
		t.Fatal("expected SLAResult.Passed to be true when tokens == limit (boundary)")
	}
}

// --- 21.2-UNIT-013: [P1] SLASpec.Evaluate boundary - duration exactly at limit (AC2) ---

func TestSLASpec_Evaluate_DurationExactBoundary(t *testing.T) {
	// Given: SLA with max_duration_ms constraint
	spec := SLASpec{MaxDurationMs: 30000}

	// When: duration exactly equals the limit
	result := spec.Evaluate("agent", 0, 30000, "")

	// Then: should pass (<=)
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if !result.Passed {
		t.Fatal("expected SLAResult.Passed to be true when duration == limit (boundary)")
	}
}

// --- 21.2-UNIT-014: [P2] SLASpec.Evaluate records timestamp (AC2/AC3) ---

func TestSLASpec_Evaluate_RecordsTimestamp(t *testing.T) {
	spec := SLASpec{MaxTokens: 10000}

	before := time.Now()
	result := spec.Evaluate("agent", 5000, 1000, "")
	after := time.Now()

	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if result.EvaluatedAt.Before(before) || result.EvaluatedAt.After(after) {
		t.Fatalf("EvaluatedAt %v should be between %v and %v",
			result.EvaluatedAt, before, after)
	}
}

// --- 21.2-UNIT-015: [P1] SLASpec.Evaluate check results contain actual and limit values (AC2) ---

func TestSLASpec_Evaluate_CheckResultValues(t *testing.T) {
	spec := SLASpec{MaxTokens: 15000, MaxDurationMs: 60000}

	result := spec.Evaluate("agent", 10000, 30000, "")

	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	for _, check := range result.Checks {
		if check.Actual == "" {
			t.Fatalf("expected non-empty Actual for check %q", check.Name)
		}
		if check.Limit == "" {
			t.Fatalf("expected non-empty Limit for check %q", check.Name)
		}
	}
}

// --- 21.2-UNIT-016: [P2] SLASpec.Evaluate skips zero-value constraints (AC5) ---

func TestSLASpec_Evaluate_SkipsZeroConstraints(t *testing.T) {
	// Given: SLA with only max_tokens set (duration and format are zero)
	spec := SLASpec{MaxTokens: 10000}

	// When: evaluating
	result := spec.Evaluate("agent", 5000, 999999, "anything")

	// Then: only max_tokens check is present, no duration/format checks
	if result == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if !result.Passed {
		t.Fatal("expected pass - only max_tokens constraint and value is within limit")
	}
	if len(result.Checks) != 1 {
		t.Fatalf("expected 1 check (max_tokens only), got %d", len(result.Checks))
	}
	if result.Checks[0].Name != "max_tokens" {
		t.Fatalf("expected check name 'max_tokens', got %q", result.Checks[0].Name)
	}
}
