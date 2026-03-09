package intent

import "context"

// LLMCaller abstracts LLM invocation for intent decomposition.
type LLMCaller interface {
	Call(ctx context.Context, prompt string, model string) (string, error)
}

// Decomposer breaks a high-level intent into sub-intents using an LLM.
type Decomposer struct {
	llmDriver LLMCaller
}

// NewDecomposer creates a Decomposer with the given LLM caller.
func NewDecomposer(caller LLMCaller) *Decomposer {
	return &Decomposer{llmDriver: caller}
}

// Decompose calls the LLM to break an intent into an IntentTree.
func (d *Decomposer) Decompose(ctx context.Context, intent string, model string) (*IntentTree, error) {
	// ATDD RED: stub — returns nil
	return nil, nil
}
