package context

import (
	"encoding/json"
	"math"
)

// TokenStats holds token usage statistics for a context.
type TokenStats struct {
	Used           int     `json:"used"`
	Limit          int     `json:"limit"`
	Percentage     float64 `json:"percentage"`
	SlotUsed       int     `json:"slot_used"`
	SlotMax        int     `json:"slot_max"`
	SlotPercentage float64 `json:"slot_percentage"`
}

// DefaultTokenLimit is the default context window size in tokens.
const DefaultTokenLimit = 200_000

// EstimateTokens estimates the number of tokens in a text string.
// ASCII text uses ~3.5 characters per token, CJK/non-ASCII uses ~1.5 runes per token.
// For mixed text, characters are classified per-rune and accumulated separately.
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}

	var asciiBytes, nonASCIIRunes int
	for _, r := range text {
		if r < 128 {
			asciiBytes++
		} else {
			nonASCIIRunes++
		}
	}

	tokens := float64(asciiBytes)/3.5 + float64(nonASCIIRunes)/1.5
	return int(math.Ceil(tokens))
}

// EstimateMessageTokens estimates the tokens a single Message contributes to a
// request. It is the SINGLE source of truth for per-message accounting — three
// call sites depend on it (Manager.TokenUsage, Manager.estimateMessagesTokens,
// and Compact's postTokens loop). Do not inline a Content-only loop anywhere:
// before Story 69.2 all three sites counted Content alone, which under-reported
// tool-heavy workloads by up to 58% and kept the token axis from ever firing.
//
// Counted payloads:
//   - Content and the flat Reasoning field (the unified openai driver echoes
//     reasoning_content back via the dual-spelling round-trip; dropping it
//     previously surfaced as HTTP 400 on DeepSeek — disproven by a 2026-08-04
//     probe, 4×200 — and the compat driver that prompted that caveat is gone).
//   - ToolCalls[].Name (the tool name goes on the wire) and ToolCalls[].Input.
//     Input is map[string]any, not a pre-serialized arguments string, so it is
//     marshalled first. json.Marshal orders keys deterministically; a
//     fmt.Sprintf("%v", …) would inherit Go's randomized map iteration and make
//     the estimate jitter between calls. Marshal failure counts 0 for that call
//     and is otherwise ignored — this is a best-effort statistic, not a
//     validator, so it must never fail or panic a TokenUsage query.
//   - ReasoningBlocks[].Thinking and .Data. Thinking is replayed verbatim by
//     the Anthropic and Gemini drivers (drivers/llm/anthropic.go,
//     drivers/llm/gemini.go), and Data is the redacted_thinking ciphertext,
//     which is likewise echoed as request body text.
//   - The correlation IDs and the reasoning-block type selector: ToolCalls[].ID,
//     the message-level ToolCallID, and ReasoningBlocks[].Type. All three are
//     serialized onto the wire as model-visible text (json tags id /
//     tool_call_id / type), unlike Signature / ThoughtSignature below; leaving
//     them out would keep a third, undocumented category of payload invisible
//     (Story 69.2 Review P2).
//
// Deliberately NOT counted: ReasoningBlock.Signature and .ThoughtSignature.
// Both are opaque round-trip credentials rather than model-visible text, and
// whether a provider bills them as tokens cannot be established from inside
// this repo. Counting an opaque blob's byte length as "tokens" would silently
// mix a guess into a number the compact threshold acts on, which
// [[observability-data-provenance-principle]] forbids; their omission is a
// bounded, documented residual instead.
//
// Story 69.2 AC3 constraint: this function only adds previously-missing fields.
// It must NOT compensate for tokenizer inaccuracy — EstimateTokens' 3.5 / 1.5
// ratios stay untouched and no magic multiplier may be introduced here. Ratio
// calibration would need a provider-feedback loop (raw.jsonl prompt_tokens) and
// is a separate concern; residual drift is recorded, not papered over.
func EstimateMessageTokens(msg Message) int {
	total := EstimateTokens(msg.Content)
	total += EstimateTokens(msg.Reasoning)
	total += EstimateTokens(msg.ToolCallID)

	for _, tc := range msg.ToolCalls {
		total += EstimateTokens(tc.ID)
		total += EstimateTokens(tc.Name)
		if len(tc.Input) == 0 {
			continue
		}
		if b, err := json.Marshal(tc.Input); err == nil {
			total += EstimateTokens(string(b))
		}
	}

	for _, rb := range msg.ReasoningBlocks {
		total += EstimateTokens(rb.Type)
		total += EstimateTokens(rb.Thinking)
		total += EstimateTokens(rb.Data)
	}

	return total
}
