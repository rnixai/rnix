package intent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// ApplyRequest contains parameters for applying a new intent.
type ApplyRequest struct {
	Intent    string
	Model     string
	Provider  string
	AutoStart bool
}

// Manager handles intent lifecycle: creation, decomposition, execution, status.
type Manager struct {
	mu                sync.RWMutex
	intents           map[IntentID]*IntentTree
	decomposer        *Decomposer
	spawner           KernelSpawner
	reconcilerConfig  ReconcilerConfig
	nextID            atomic.Uint64
	activeReconcilers map[IntentID]*Reconciler
}

// NewManager creates a Manager with the given decomposer, spawner, and reconciler config.
func NewManager(decomposer *Decomposer, spawner KernelSpawner, config ReconcilerConfig) *Manager {
	return &Manager{
		intents:           make(map[IntentID]*IntentTree),
		decomposer:        decomposer,
		spawner:           spawner,
		reconcilerConfig:  config,
		activeReconcilers: make(map[IntentID]*Reconciler),
	}
}

// Apply decomposes a high-level intent and stores the resulting IntentTree.
func (m *Manager) Apply(ctx context.Context, req ApplyRequest) (*IntentTree, error) {
	tree, err := m.decomposer.Decompose(ctx, req.Intent, req.Model)
	if err != nil {
		return nil, fmt.Errorf("intent apply: %w", err)
	}

	if req.Provider != "" {
		for _, node := range tree.Nodes {
			if node.Provider == "" {
				node.Provider = req.Provider
			}
		}
	}

	id := IntentID(fmt.Sprintf("intent-%d", m.nextID.Add(1)))
	tree.ID = id

	m.mu.Lock()
	m.intents[id] = tree
	m.mu.Unlock()

	return tree, nil
}

// Confirm transitions an intent from await_confirm to executing.
func (m *Manager) Confirm(intentID IntentID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tree, ok := m.intents[intentID]
	if !ok {
		return fmt.Errorf("intent %s: not found", intentID)
	}
	if tree.State != IntentAwaitConfirm {
		return fmt.Errorf("intent %s: cannot confirm in state %q", intentID, tree.State)
	}
	tree.State = IntentExecuting
	return nil
}

// Execute starts execution of a confirmed intent tree using the Reconciler.
func (m *Manager) Execute(ctx context.Context, intentID IntentID, callbacks ReconcilerCallbacks) error {
	m.mu.RLock()
	tree, ok := m.intents[intentID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("intent %s: not found", intentID)
	}

	reconciler, err := NewReconciler(tree, m.spawner, m.reconcilerConfig, callbacks)
	if err != nil {
		return fmt.Errorf("intent %s: %w", intentID, err)
	}

	// Save reconciler reference for runtime injection support
	m.mu.Lock()
	m.activeReconcilers[intentID] = reconciler
	m.mu.Unlock()

	execErr := reconciler.Execute(ctx)

	// Clean up reconciler reference after execution completes
	m.mu.Lock()
	delete(m.activeReconcilers, intentID)
	m.mu.Unlock()

	return execErr
}

// Status returns the IntentTree for the given ID, or error if not found.
func (m *Manager) Status(intentID IntentID) (*IntentTree, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tree, ok := m.intents[intentID]
	if !ok {
		return nil, fmt.Errorf("intent %s: not found", intentID)
	}
	return tree, nil
}

// ListActive returns all non-terminal IntentTrees.
func (m *Manager) ListActive() []*IntentTree {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []*IntentTree
	for _, tree := range m.intents {
		if !tree.IsTerminal() {
			active = append(active, tree)
		}
	}
	return active
}

// ListAll returns all IntentTrees (active and completed).
func (m *Manager) ListAll() []*IntentTree {
	m.mu.RLock()
	defer m.mu.RUnlock()

	all := make([]*IntentTree, 0, len(m.intents))
	for _, tree := range m.intents {
		all = append(all, tree)
	}
	return all
}

// ApplyIncremental performs an incremental update on an existing intent tree.
func (m *Manager) ApplyIncremental(ctx context.Context, intentID IntentID, newIntent, model, provider string) (*IntentTree, *MergeResult, error) {
	m.mu.RLock()
	tree, ok := m.intents[intentID]
	reconciler := m.activeReconcilers[intentID]
	m.mu.RUnlock()

	if !ok {
		return nil, nil, fmt.Errorf("apply incremental: intent %s: not found", intentID)
	}
	if tree.IsTerminal() {
		return nil, nil, fmt.Errorf("apply incremental: intent %s: cannot update terminal intent", intentID)
	}

	newNodes, err := m.decomposer.DecomposeIncremental(ctx, tree, newIntent, model)
	if err != nil {
		return nil, nil, fmt.Errorf("apply incremental: intent %s: %w", intentID, err)
	}

	if provider != "" {
		for _, node := range newNodes {
			if node.Provider == "" {
				node.Provider = provider
			}
		}
	}

	var mergeResult *MergeResult
	if reconciler != nil {
		mergeResult, err = reconciler.MergeAndInject(newNodes)
	} else {
		mergeResult, err = MergeIncremental(tree, newNodes)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("apply incremental: intent %s: %w", intentID, err)
	}

	return tree, mergeResult, nil
}
