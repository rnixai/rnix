package intent

import (
	"context"
	"fmt"
	"sync"

	"github.com/rnixai/rnix/internal/types"
)

// KernelSpawner defines the kernel operations needed by the intent engine.
type KernelSpawner interface {
	SpawnIntent(ctx context.Context, node *IntentNode) (types.PID, error)
	Wait(pid types.PID) (ExitStatus, error)
	Kill(pid types.PID) error
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
	dag       *DAG
	spawner   KernelSpawner
	mu        sync.Mutex
	callbacks EngineCallbacks
}

// NewEngine creates an Engine for the given IntentTree.
func NewEngine(tree *IntentTree, spawner KernelSpawner, callbacks EngineCallbacks) (*Engine, error) {
	dag, err := BuildIntentDAG(tree)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	return &Engine{
		tree:      tree,
		dag:       dag,
		spawner:   spawner,
		callbacks: callbacks,
	}, nil
}

type nodeEvent struct {
	nodeID string
	result string
	errMsg string
}

// Execute runs the intent tree using an event-driven loop.
func (e *Engine) Execute(ctx context.Context) error {
	e.mu.Lock()
	e.tree.State = IntentExecuting
	e.mu.Unlock()

	eventCh := make(chan nodeEvent, len(e.tree.Nodes))
	active := 0

	// spawnRunnable launches goroutines for all currently runnable nodes.
	// Must be called with e.mu held. Returns the number of newly spawned goroutines.
	spawnRunnable := func() int {
		runnable := e.tree.RunnableNodes()
		for _, node := range runnable {
			node.State = IntentExecuting
		}
		count := len(runnable)
		for _, node := range runnable {
			go e.executeNode(ctx, node, eventCh)
		}
		return count
	}

	e.mu.Lock()
	active += spawnRunnable()
	if active == 0 {
		e.tree.State = IntentCompleted
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			e.mu.Lock()
			e.tree.State = IntentFailed
			e.mu.Unlock()
			return ctx.Err()

		case ev := <-eventCh:
			active--

			e.mu.Lock()
			if ev.errMsg != "" {
				e.tree.MarkFailed(ev.nodeID, ev.errMsg)
				if e.callbacks.OnNodeFailed != nil {
					e.callbacks.OnNodeFailed(ev.nodeID, ev.errMsg)
				}
			} else {
				e.tree.MarkCompleted(ev.nodeID, ev.result)
				if e.callbacks.OnNodeComplete != nil {
					e.callbacks.OnNodeComplete(ev.nodeID, ev.result)
				}
			}

			if e.callbacks.OnProgress != nil {
				completed, total := e.tree.Progress()
				e.callbacks.OnProgress(completed, total)
			}

			if e.tree.IsTerminal() {
				e.finalizeTreeState()
				e.mu.Unlock()
				return nil
			}

			active += spawnRunnable()

			if active == 0 {
				e.finalizeTreeState()
				e.mu.Unlock()
				return nil
			}
			e.mu.Unlock()
		}
	}
}

func (e *Engine) executeNode(ctx context.Context, node *IntentNode, eventCh chan<- nodeEvent) {
	pid, err := e.spawner.SpawnIntent(ctx, node)
	if err != nil {
		eventCh <- nodeEvent{nodeID: node.ID, errMsg: fmt.Sprintf("spawn failed: %v", err)}
		return
	}

	e.mu.Lock()
	node.PID = pid
	e.mu.Unlock()

	if e.callbacks.OnNodeStart != nil {
		e.callbacks.OnNodeStart(node.ID, pid)
	}

	status, waitErr := e.spawner.Wait(pid)
	if waitErr != nil || status.Code != 0 {
		errMsg := status.Reason
		if waitErr != nil {
			errMsg = waitErr.Error()
		}
		eventCh <- nodeEvent{nodeID: node.ID, errMsg: errMsg}
		return
	}

	eventCh <- nodeEvent{nodeID: node.ID, result: status.Reason}
}

// finalizeTreeState sets the tree's final state. Must be called with e.mu held.
func (e *Engine) finalizeTreeState() {
	hasFailed := false
	for _, node := range e.tree.Nodes {
		if node.State == IntentFailed {
			hasFailed = true
			break
		}
	}
	if hasFailed {
		e.tree.State = IntentFailed
	} else {
		e.tree.State = IntentCompleted
	}
}
