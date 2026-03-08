package agtest

import (
	"testing"
)

// --- 16.3-UNIT-015: ParseQualityResponse — valid JSON ---

func TestParseQualityResponse_Valid(t *testing.T) {
	raw := `{"passed": true, "score": 0.85, "reason": "output meets criteria"}`
	qr := ParseQualityResponse(raw)
	if !qr.Passed {
		t.Error("Passed should be true")
	}
	if qr.Score != 0.85 {
		t.Errorf("Score = %f, want 0.85", qr.Score)
	}
	if qr.Reason != "output meets criteria" {
		t.Errorf("Reason = %q, want %q", qr.Reason, "output meets criteria")
	}
}

// --- 16.3-UNIT-016: ParseQualityResponse — invalid JSON fallback ---

func TestParseQualityResponse_InvalidJSON(t *testing.T) {
	raw := "this is not JSON at all, quality assessment failed"
	qr := ParseQualityResponse(raw)
	if qr.Passed {
		t.Error("Passed should be false for non-JSON without passed:true")
	}
	if qr.Reason == "" {
		t.Error("Reason should contain the raw text")
	}
}

// --- 16.3-UNIT-017: ParseQualityResponse — fallback with passed true ---

func TestParseQualityResponse_FallbackPassedTrue(t *testing.T) {
	raw := `The output is good. "passed": true and the quality is fine.`
	qr := ParseQualityResponse(raw)
	if !qr.Passed {
		t.Error("Passed should be true (fallback heuristic)")
	}
}

// --- 16.3-UNIT-017b: ParseQualityResponse — JSON in markdown fence ---

func TestParseQualityResponse_JSONInFence(t *testing.T) {
	raw := "```json\n{\"passed\": false, \"score\": 0.3, \"reason\": \"low quality\"}\n```"
	qr := ParseQualityResponse(raw)
	if qr.Passed {
		t.Error("Passed should be false")
	}
	if qr.Score != 0.3 {
		t.Errorf("Score = %f, want 0.3", qr.Score)
	}
}

// --- 16.3-UNIT-018: LLMQualityJudge interface check (compile-time) ---

func TestLLMQualityJudge_Interface(t *testing.T) {
	// ParseQualityResponse is a standalone helper used by the CLI-layer executor.
	// The QualityJudge interface is satisfied by MockQualityJudge for unit tests
	// and by ipcQualityJudge in cmd/rnix for real execution.
	var _ QualityJudge = &MockQualityJudge{}
}
