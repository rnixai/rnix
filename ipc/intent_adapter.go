package ipc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
)

// IntentManagerAdapter wraps intent.Manager to implement the intentManager interface.
type IntentManagerAdapter struct {
	mgr *intent.Manager
}

// NewIntentManagerAdapter creates an adapter for the given intent.Manager.
func NewIntentManagerAdapter(mgr *intent.Manager) *IntentManagerAdapter {
	return &IntentManagerAdapter{mgr: mgr}
}

func (a *IntentManagerAdapter) ApplyIntent(ctx context.Context, intentStr, model, provider string, autoStart bool) (string, []byte, error) {
	tree, err := a.mgr.Apply(ctx, intent.ApplyRequest{
		Intent:    intentStr,
		Model:     model,
		Provider:  provider,
		AutoStart: autoStart,
	})
	if err != nil {
		return "", nil, err
	}

	treeJSON, _ := json.Marshal(intentTreeToWire(tree))
	return string(tree.ID), treeJSON, nil
}

func (a *IntentManagerAdapter) ConfirmIntent(intentID string) error {
	return a.mgr.Confirm(intent.IntentID(intentID))
}

func (a *IntentManagerAdapter) ExecuteIntent(ctx context.Context, intentID string,
	onNodeStart func(nodeID string, pid uint64),
	onNodeComplete func(nodeID, result string),
	onNodeFailed func(nodeID, errMsg string),
	onProgress func(completed, total int),
	onNodeRetry func(nodeID string, attempt, maxRetries int),
	onNodeTimeout func(nodeID string),
	onDriftDetected func(nodeID, driftType, message string),
	onDriftResolved func(nodeID string),
) error {
	callbacks := intent.ReconcilerCallbacks{
		OnNodeStart: func(nodeID string, pid types.PID) {
			if onNodeStart != nil {
				onNodeStart(nodeID, uint64(pid))
			}
		},
		OnNodeComplete: func(nodeID string, result string) {
			if onNodeComplete != nil {
				onNodeComplete(nodeID, result)
			}
		},
		OnNodeFailed: func(nodeID string, errMsg string) {
			if onNodeFailed != nil {
				onNodeFailed(nodeID, errMsg)
			}
		},
		OnProgress: func(completed, total int) {
			if onProgress != nil {
				onProgress(completed, total)
			}
		},
		OnNodeRetry: func(nodeID string, attempt int, maxRetries int) {
			if onNodeRetry != nil {
				onNodeRetry(nodeID, attempt, maxRetries)
			}
		},
		OnNodeTimeout: func(nodeID string) {
			if onNodeTimeout != nil {
				onNodeTimeout(nodeID)
			}
		},
		OnDriftDetected: func(drift intent.DriftItem) {
			if onDriftDetected != nil {
				onDriftDetected(drift.NodeID, string(drift.Type), drift.Message)
			}
		},
		OnDriftResolved: func(nodeID string) {
			if onDriftResolved != nil {
				onDriftResolved(nodeID)
			}
		},
	}
	return a.mgr.Execute(ctx, intent.IntentID(intentID), callbacks)
}

func (a *IntentManagerAdapter) IntentStatus(intentID string) ([]byte, error) {
	tree, err := a.mgr.Status(intent.IntentID(intentID))
	if err != nil {
		return nil, err
	}
	resp := IntentStatusResponse{Intents: []*IntentTreeWire{intentTreeToWire(tree)}}
	return json.Marshal(resp)
}

func (a *IntentManagerAdapter) ListActiveIntents() ([]byte, error) {
	trees := a.mgr.ListActive()
	wires := make([]*IntentTreeWire, len(trees))
	for i, tree := range trees {
		wires[i] = intentTreeToWire(tree)
	}
	resp := IntentStatusResponse{Intents: wires}
	return json.Marshal(resp)
}

func (a *IntentManagerAdapter) ApplyIncrementalIntent(ctx context.Context, intentID, intentStr, model, provider string) (string, []byte, error) {
	tree, mergeResult, err := a.mgr.ApplyIncremental(ctx, intent.IntentID(intentID), intentStr, model, provider)
	if err != nil {
		return "", nil, err
	}

	resp := ApplyIncrementalIntentResponse{
		IntentID:      intentID,
		Tree:          intentTreeToWire(tree),
		AddedNodes:    mergeResult.AddedNodes,
		ModifiedNodes: mergeResult.ModifiedNodes,
	}
	if resp.AddedNodes == nil {
		resp.AddedNodes = []string{}
	}
	if resp.ModifiedNodes == nil {
		resp.ModifiedNodes = []string{}
	}

	data, _ := json.Marshal(resp)
	return intentID, data, nil
}

func (a *IntentManagerAdapter) ListAllIntents() ([]byte, error) {
	trees := a.mgr.ListAll()
	wires := make([]*IntentTreeWire, len(trees))
	for i, tree := range trees {
		wires[i] = intentTreeToWire(tree)
	}
	resp := IntentStatusResponse{Intents: wires}
	return json.Marshal(resp)
}

func intentTreeToWire(tree *intent.IntentTree) *IntentTreeWire {
	if tree == nil {
		return nil
	}
	w := &IntentTreeWire{
		ID:          string(tree.ID),
		RootIntent:  tree.RootIntent,
		State:       string(tree.State),
		Nodes:       make(map[string]*IntentNodeWire, len(tree.Nodes)),
		CreatedAtMs: tree.CreatedAt.UnixMilli(),
	}
	if tree.CompletedAt != nil {
		w.CompletedAtMs = tree.CompletedAt.UnixMilli()
	}
	for id, node := range tree.Nodes {
		w.Nodes[id] = intentNodeToWire(node)
	}
	for _, d := range tree.Drifts {
		w.Drifts = append(w.Drifts, driftItemToWire(d))
	}
	return w
}

func driftItemToWire(d intent.DriftItem) DriftItemWire {
	return DriftItemWire{
		NodeID:       d.NodeID,
		Type:         string(d.Type),
		Message:      d.Message,
		DetectedAtMs: d.DetectedAt.UnixMilli(),
	}
}

func intentNodeToWire(node *intent.IntentNode) *IntentNodeWire {
	deps := node.DependsOn
	if deps == nil {
		deps = []string{}
	}
	w := &IntentNodeWire{
		ID:         node.ID,
		Intent:     node.Intent,
		Agent:      node.Agent,
		Model:      node.Model,
		DependsOn:  deps,
		State:      string(node.State),
		PID:        uint64(node.PID),
		Result:     node.Result,
		Error:      node.Error,
		RetryCount: node.RetryCount,
		MaxRetries: node.MaxRetries,
	}
	if node.Timeout > 0 {
		w.TimeoutMs = node.Timeout.Milliseconds()
	}
	return w
}

// IntentKernelSpawner adapts kernel spawn operations for the intent engine.
type IntentKernelSpawner struct {
	SpawnFunc func(ctx context.Context, node *intent.IntentNode) (types.PID, error)
	WaitFunc  func(pid types.PID) (intent.ExitStatus, error)
}

func (s *IntentKernelSpawner) SpawnIntent(ctx context.Context, node *intent.IntentNode) (types.PID, error) {
	if s.SpawnFunc == nil {
		return 0, fmt.Errorf("spawn not configured")
	}
	return s.SpawnFunc(ctx, node)
}

func (s *IntentKernelSpawner) Wait(pid types.PID) (intent.ExitStatus, error) {
	if s.WaitFunc == nil {
		return intent.ExitStatus{}, fmt.Errorf("wait not configured")
	}
	return s.WaitFunc(pid)
}
