package intent

import (
	"context"
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
	mu         sync.RWMutex
	intents    map[IntentID]*IntentTree
	decomposer *Decomposer
	spawner    KernelSpawner
	nextID     atomic.Uint64
}

// NewManager creates a Manager with the given decomposer and spawner.
func NewManager(decomposer *Decomposer, spawner KernelSpawner) *Manager {
	return &Manager{
		intents:    make(map[IntentID]*IntentTree),
		decomposer: decomposer,
		spawner:    spawner,
	}
}

// Apply decomposes a high-level intent and stores the resulting IntentTree.
func (m *Manager) Apply(ctx context.Context, req ApplyRequest) (*IntentTree, error) {
	// ATDD RED: stub — returns nil
	return nil, nil
}

// Confirm transitions an intent from await_confirm to executing.
func (m *Manager) Confirm(intentID IntentID) error {
	// ATDD RED: stub — returns nil
	return nil
}

// Execute starts execution of a confirmed intent tree.
func (m *Manager) Execute(ctx context.Context, intentID IntentID, callbacks EngineCallbacks) error {
	// ATDD RED: stub — returns nil
	return nil
}

// Status returns the IntentTree for the given ID, or error if not found.
func (m *Manager) Status(intentID IntentID) (*IntentTree, error) {
	// ATDD RED: stub — returns nil
	return nil, nil
}

// ListActive returns all non-terminal IntentTrees.
func (m *Manager) ListActive() []*IntentTree {
	// ATDD RED: stub — returns nil
	return nil
}
