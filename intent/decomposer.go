package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LLMCaller abstracts LLM invocation for intent decomposition.
type LLMCaller interface {
	Call(ctx context.Context, prompt string, model string, provider string) (string, error)
}

// Decomposer breaks a high-level intent into sub-intents using an LLM.
type Decomposer struct {
	llmDriver         LLMCaller
	decomposePrompt   string
	incrementalPrompt string
}

// NewDecomposer creates a Decomposer with the given LLM caller.
// Prompt templates are loaded from embedded files.
func NewDecomposer(caller LLMCaller) *Decomposer {
	return &Decomposer{
		llmDriver:         caller,
		decomposePrompt:   loadPrompt("decompose"),
		incrementalPrompt: loadPrompt("incremental_decompose"),
	}
}

type llmDecomposeNode struct {
	ID        string   `json:"id"`
	Intent    string   `json:"intent"`
	Agent     string   `json:"agent,omitempty"`
	DependsOn []string `json:"depends_on"`
}

// Decompose calls the LLM to break an intent into an IntentTree.
func (d *Decomposer) Decompose(ctx context.Context, intent string, model string, provider string) (*IntentTree, error) {
	prompt := fmt.Sprintf(d.decomposePrompt, intent)

	response, err := d.llmDriver.Call(ctx, prompt, model, provider)
	if err != nil {
		return nil, fmt.Errorf("decompose: LLM call failed: %w", err)
	}

	var nodes []llmDecomposeNode
	if err := json.Unmarshal([]byte(response), &nodes); err != nil {
		return nil, fmt.Errorf("decompose: invalid JSON from LLM: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("decompose: LLM returned empty task list")
	}

	tree := &IntentTree{
		RootIntent: intent,
		State:      IntentAwaitConfirm,
		Nodes:      make(map[string]*IntentNode, len(nodes)),
		CreatedAt:  time.Now(),
	}

	for _, n := range nodes {
		deps := n.DependsOn
		if deps == nil {
			deps = []string{}
		}
		tree.Nodes[n.ID] = &IntentNode{
			ID:        n.ID,
			Intent:    n.Intent,
			Agent:     n.Agent,
			DependsOn: deps,
			State:     IntentPending,
		}
	}

	if _, err := BuildIntentDAG(tree); err != nil {
		return nil, fmt.Errorf("decompose: %w", err)
	}

	tree.InitDesired()

	return tree, nil
}

// DecomposeIncremental calls the LLM to produce an updated node list for an existing tree.
func (d *Decomposer) DecomposeIncremental(ctx context.Context, tree *IntentTree, newIntent string, model string, provider string) ([]*IntentNode, error) {
	// Build current tasks summary with deterministic order
	ids := make([]string, 0, len(tree.Nodes))
	for id := range tree.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var tasksSummary strings.Builder
	for _, id := range ids {
		node := tree.Nodes[id]
		fmt.Fprintf(&tasksSummary, "- id: %s, intent: %s, state: %s\n", id, node.Intent, node.State)
	}

	prompt := fmt.Sprintf(d.incrementalPrompt, tree.RootIntent, tasksSummary.String(), newIntent)

	response, err := d.llmDriver.Call(ctx, prompt, model, provider)
	if err != nil {
		return nil, fmt.Errorf("decompose incremental: LLM call failed: %w", err)
	}

	var llmNodes []llmDecomposeNode
	if err := json.Unmarshal([]byte(response), &llmNodes); err != nil {
		return nil, fmt.Errorf("decompose incremental: invalid JSON from LLM: %w", err)
	}

	nodes := make([]*IntentNode, len(llmNodes))
	for i, n := range llmNodes {
		deps := n.DependsOn
		if deps == nil {
			deps = []string{}
		}
		nodes[i] = &IntentNode{
			ID:        n.ID,
			Intent:    n.Intent,
			Agent:     n.Agent,
			DependsOn: deps,
			State:     IntentPending,
		}
	}

	return nodes, nil
}
