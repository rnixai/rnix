package context

import (
	"math"
)

// TokenStats holds token usage statistics for a context.
type TokenStats struct {
	Used       int     `json:"used"`
	Limit      int     `json:"limit"`
	Percentage float64 `json:"percentage"`
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
