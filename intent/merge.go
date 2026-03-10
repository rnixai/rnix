package intent

import (
	"fmt"
	"sort"

	"github.com/rnixai/rnix/internal/types"
)

// MergeResult describes the outcome of an incremental merge operation.
type MergeResult struct {
	AddedNodes     []string // New node IDs added to the tree
	ModifiedNodes  []string // Existing node IDs whose intent changed (reset to pending)
	UnchangedNodes []string // Existing node IDs unchanged by the merge
}

// MergeIncremental merges newNodes into an existing IntentTree.
//
// For each node in newNodes:
//   - ID not in existing → add as new node with IntentPending state
//   - ID exists and Intent unchanged → unchanged (preserve current state)
//   - ID exists and Intent changed → modified (reset to IntentPending)
//
// After merging, validates all dependencies and checks for cycles.
// Updates DesiredNodes for any newly added nodes.
// nodeSnapshot captures the pre-modification state of a node for rollback.
type nodeSnapshot struct {
	intent     string
	dependsOn  []string
	state      IntentState
	errorMsg   string
	pid        types.PID
	result     string
	retryCount int
}

func MergeIncremental(existing *IntentTree, newNodes []*IntentNode) (*MergeResult, error) {
	result := &MergeResult{}

	// Track modified node snapshots for complete rollback on validation failure (H2).
	var modifiedSnapshots []struct {
		id   string
		snap nodeSnapshot
	}

	for _, newNode := range newNodes {
		existingNode, exists := existing.Nodes[newNode.ID]
		if !exists {
			// New node: add to tree with pending state
			deps := newNode.DependsOn
			if deps == nil {
				deps = []string{}
			}
			existing.Nodes[newNode.ID] = &IntentNode{
				ID:        newNode.ID,
				Intent:    newNode.Intent,
				Agent:     newNode.Agent,
				Model:     newNode.Model,
				DependsOn: deps,
				State:     IntentPending,
			}
			result.AddedNodes = append(result.AddedNodes, newNode.ID)
		} else if existingNode.Intent != newNode.Intent {
			// Save snapshot before modifying for rollback
			depsCopy := make([]string, len(existingNode.DependsOn))
			copy(depsCopy, existingNode.DependsOn)
			modifiedSnapshots = append(modifiedSnapshots, struct {
				id   string
				snap nodeSnapshot
			}{
				id: newNode.ID,
				snap: nodeSnapshot{
					intent:     existingNode.Intent,
					dependsOn:  depsCopy,
					state:      existingNode.State,
					errorMsg:   existingNode.Error,
					pid:        existingNode.PID,
					result:     existingNode.Result,
					retryCount: existingNode.RetryCount,
				},
			})

			// Modified node: intent changed, reset to pending
			existingNode.Intent = newNode.Intent
			existingNode.DependsOn = newNode.DependsOn
			if existingNode.DependsOn == nil {
				existingNode.DependsOn = []string{}
			}
			existing.ResetNode(newNode.ID)
			result.ModifiedNodes = append(result.ModifiedNodes, newNode.ID)
		} else {
			// Unchanged node: preserve current state
			// Update DependsOn in case it changed (same intent but different deps)
			existingNode.DependsOn = newNode.DependsOn
			if existingNode.DependsOn == nil {
				existingNode.DependsOn = []string{}
			}
			result.UnchangedNodes = append(result.UnchangedNodes, newNode.ID)
		}
	}

	sort.Strings(result.AddedNodes)
	sort.Strings(result.ModifiedNodes)
	sort.Strings(result.UnchangedNodes)

	// rollback reverts both added nodes and modified nodes to their original state.
	rollback := func() {
		for _, addedID := range result.AddedNodes {
			delete(existing.Nodes, addedID)
		}
		for _, ms := range modifiedSnapshots {
			node := existing.Nodes[ms.id]
			node.Intent = ms.snap.intent
			node.DependsOn = ms.snap.dependsOn
			node.State = ms.snap.state
			node.Error = ms.snap.errorMsg
			node.PID = ms.snap.pid
			node.Result = ms.snap.result
			node.RetryCount = ms.snap.retryCount
		}
	}

	// Validate all dependencies exist in the merged tree
	for _, node := range existing.Nodes {
		for _, dep := range node.DependsOn {
			if _, ok := existing.Nodes[dep]; !ok {
				rollback()
				return nil, fmt.Errorf("merge: node %q depends on unknown node %q", node.ID, dep)
			}
		}
	}

	// Validate no cycles in the merged DAG
	if _, err := BuildIntentDAG(existing); err != nil {
		rollback()
		return nil, fmt.Errorf("merge: %w", err)
	}

	// Update DesiredNodes for new nodes
	if existing.DesiredNodes == nil {
		existing.DesiredNodes = make(map[string]IntentState)
	}
	for _, id := range result.AddedNodes {
		existing.DesiredNodes[id] = IntentCompleted
	}

	return result, nil
}
