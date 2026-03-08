package agtest

import (
	"encoding/json"
	"strings"
)

// qualityResponseJSON is the expected JSON structure from LLM quality evaluation.
type qualityResponseJSON struct {
	Passed bool    `json:"passed"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// ParseQualityResponse parses a quality evaluation response string into a QualityResult.
// Falls back to heuristic detection if the response is not valid JSON.
func ParseQualityResponse(raw string) *QualityResult {
	raw = strings.TrimSpace(raw)

	var resp qualityResponseJSON
	if err := json.Unmarshal([]byte(raw), &resp); err == nil {
		return &QualityResult{
			Passed: resp.Passed,
			Score:  resp.Score,
			Reason: resp.Reason,
		}
	}

	// Try extracting JSON from markdown code fences
	if idx := strings.Index(raw, "{"); idx >= 0 {
		end := strings.LastIndex(raw, "}")
		if end > idx {
			if err := json.Unmarshal([]byte(raw[idx:end+1]), &resp); err == nil {
				return &QualityResult{
					Passed: resp.Passed,
					Score:  resp.Score,
					Reason: resp.Reason,
				}
			}
		}
	}

	// Fallback heuristic: check for "passed": true in the raw text
	lower := strings.ToLower(raw)
	passed := strings.Contains(lower, `"passed": true`) || strings.Contains(lower, `"passed":true`)
	reason := raw
	if len([]rune(reason)) > 200 {
		reason = string([]rune(reason)[:200]) + "..."
	}
	return &QualityResult{
		Passed: passed,
		Score:  0,
		Reason: reason,
	}
}
