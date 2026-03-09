package intent

import (
	"context"
	"sync"

	"github.com/rnixai/rnix/internal/types"
)

// KernelSpawner defines the kernel operations needed by the intent engine.
type KernelSpawner interface {
	SpawnIntent(ctx context.Context, node *IntentNode) (types.PID, error)
	Wait(pid types.PID) (ExitStatus, error)
}

// ExitStatus records the exit status of an intent node execution.
type ExitStatus struct {
	Code   int
	Reason string
	Err    error
}

// EngineCallbacks provides hooks for engine lifecycle events.
type EngineCallbacks struct {
	OnNodeStart    func(nodeID string, pid types.PID)
	OnNodeComplete func(nodeID string, result string)
	OnNodeFailed   func(nodeID string, err string)
	OnProgress     func(completed, total int)
}

// Engine executes an IntentTree by scheduling nodes according to DAG order.
type Engine struct {
	tree      *IntentTree
	spawner   KernelSpawner
	mu        sync.Mutex
	callbacks EngineCallbacks
}

// NewEngine creates an Engine for the given IntentTree.
func NewEngine(tree *IntentTree, spawner KernelSpawner, callbacks EngineCallbacks) (*Engine, error) {
	// ATDD RED: stub — returns nil engine
	return nil, nil
}

// Execute runs the intent tree, scheduling nodes by DAG topology.
func (e *Engine) Execute(ctx context.Context) error {
	// ATDD RED: stub — returns nil
	return nil
}
