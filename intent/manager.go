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
	AutoStart bool
}

// Manager handles intent lifecycle: creation, decomposition, execution, status.
type Manager struct {
	mu               sync.RWMutex
	intents          map[IntentID]*IntentTree
	decomposer       *Decomposer
	spawner          KernelSpawner
	reconcilerConfig ReconcilerConfig
	nextID           atomic.Uint64
}

// NewManager creates a Manager with the given decomposer, spawner, and reconciler config.
func NewManager(decomposer *Decomposer, spawner KernelSpawner, config ReconcilerConfig) *Manager {
	return &Manager{
		intents:          make(map[IntentID]*IntentTree),
		decomposer:       decomposer,
		spawner:          spawner,
		reconcilerConfig: config,
	}
}

// Apply decomposes a high-level intent and stores the resulting IntentTree.
func (m *Manager) Apply(ctx context.Context, req ApplyRequest) (*IntentTree, error) {
	tree, err := m.decomposer.Decompose(ctx, req.Intent, req.Model)
	if err != nil {
		return nil, fmt.Errorf("intent apply: %w", err)
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

	return reconciler.Execute(ctx)
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
