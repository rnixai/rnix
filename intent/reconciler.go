package intent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// injectResultMaxRunes caps each injected upstream result (F4) so a large
// dependency output doesn't blow the dependent child's context window.
const injectResultMaxRunes = 2000

// ReconcilerConfig holds tuning parameters for the Reconciler.
type ReconcilerConfig struct {
	DefaultMaxRetries int
	DefaultTimeout    time.Duration
	ReconcileInterval time.Duration
	MaxReconcileDelay time.Duration
}

// DefaultReconcilerConfig returns production defaults.
func DefaultReconcilerConfig() ReconcilerConfig {
	return ReconcilerConfig{
		DefaultMaxRetries: 3,
		DefaultTimeout:    5 * time.Minute,
		ReconcileInterval: 1 * time.Second,
		MaxReconcileDelay: 5 * time.Second,
	}
}

// ReconcilerCallbacks provides hooks for reconciler lifecycle events.
type ReconcilerCallbacks struct {
	OnNodeRetry     func(nodeID string, attempt int, maxRetries int)
	OnNodeTimeout   func(nodeID string)
	OnDriftDetected func(drift DriftItem)
	OnDriftResolved func(nodeID string)
	OnNodeStart     func(nodeID string, pid types.PID)
	OnNodeComplete  func(nodeID string, result string)
	OnNodeFailed    func(nodeID string, err string)
	OnProgress      func(completed, total int)
}

type reconcileEventType int

const (
	evNodeCompleted reconcileEventType = iota
	evNodeFailed
	evNodeTimeout
)

type reconcileEvent struct {
	nodeID string
	evType reconcileEventType
	result string
	errMsg string
}

// Reconciler executes an IntentTree with retry, timeout, and drift management.
type Reconciler struct {
	tree      *IntentTree
	spawner   KernelSpawner
	config    ReconcilerConfig
	mu        sync.Mutex
	callbacks ReconcilerCallbacks
	eventCh   chan reconcileEvent
	active    int             // in-flight goroutines (protected by mu)
	execCtx   context.Context // stored execution context for InjectNodes scheduling
}

// NewReconciler creates a Reconciler for the given IntentTree.
func NewReconciler(tree *IntentTree, spawner KernelSpawner, config ReconcilerConfig, callbacks ReconcilerCallbacks) (*Reconciler, error) {
	dag, err := BuildIntentDAG(tree)
	if err != nil {
		return nil, fmt.Errorf("reconciler: %w", err)
	}
	_ = dag
	return &Reconciler{
		tree:      tree,
		spawner:   spawner,
		config:    config,
		callbacks: callbacks,
		eventCh:   make(chan reconcileEvent, len(tree.Nodes)*3),
	}, nil
}

// Execute runs the intent tree with reconciliation (retry, timeout, drift tracking).
func (r *Reconciler) Execute(ctx context.Context) error {
	r.mu.Lock()
	r.tree.State = IntentExecuting
	r.tree.InitDesired()
	r.execCtx = ctx
	r.active = 0

	for _, node := range r.tree.Nodes {
		if node.MaxRetries == 0 {
			node.MaxRetries = r.config.DefaultMaxRetries
		}
	}
	r.mu.Unlock()

	r.mu.Lock()
	r.active += r.spawnRunnable(ctx)
	if r.active == 0 {
		r.tree.State = IntentCompleted
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.tree.State = IntentFailed
			r.execCtx = nil
			r.mu.Unlock()
			return ctx.Err()

		case ev := <-r.eventCh:
			r.mu.Lock()
			r.active--

			switch ev.evType {
			case evNodeCompleted:
				r.tree.MarkCompleted(ev.nodeID, ev.result)
				r.tree.ClearDrift(ev.nodeID)

				if r.callbacks.OnNodeComplete != nil {
					r.callbacks.OnNodeComplete(ev.nodeID, ev.result)
				}
				if r.callbacks.OnDriftResolved != nil {
					node := r.tree.Nodes[ev.nodeID]
					if node != nil && node.RetryCount > 0 {
						r.callbacks.OnDriftResolved(ev.nodeID)
					}
				}

			case evNodeFailed, evNodeTimeout:
				node := r.tree.Nodes[ev.nodeID]
				if node == nil {
					r.mu.Unlock()
					continue
				}

				if ev.evType == evNodeTimeout {
					if r.callbacks.OnNodeTimeout != nil {
						r.callbacks.OnNodeTimeout(ev.nodeID)
					}
				}

				driftType := DriftNodeFailed
				if ev.evType == evNodeTimeout {
					driftType = DriftNodeTimeout
				}
				drift := DriftItem{
					NodeID:     ev.nodeID,
					Type:       driftType,
					Message:    ev.errMsg,
					DetectedAt: time.Now(),
				}
				r.tree.AddDrift(drift)
				if r.callbacks.OnDriftDetected != nil {
					r.callbacks.OnDriftDetected(drift)
				}

				if r.callbacks.OnNodeFailed != nil {
					r.callbacks.OnNodeFailed(ev.nodeID, ev.errMsg)
				}

				node.IncrRetry()
			if node.CanRetry() {
				if r.callbacks.OnNodeRetry != nil {
					r.callbacks.OnNodeRetry(ev.nodeID, node.RetryCount, node.MaxRetries)
				}
				node.State = IntentRetrying
				node.Error = ""
				node.PID = 0
				node.Result = ""
					r.tree.ClearDrift(ev.nodeID)
					r.active += r.spawnRunnable(ctx)
				} else {
					r.tree.MarkFailed(ev.nodeID, ev.errMsg)
				}
			}

			if r.callbacks.OnProgress != nil {
				completed, total := r.tree.Progress()
				r.callbacks.OnProgress(completed, total)
			}

			if r.tree.IsTerminal() {
				r.finalizeTreeState()
				r.mu.Unlock()
				return nil
			}

			if ev.evType == evNodeCompleted {
				r.active += r.spawnRunnable(ctx)
			}

			if r.active == 0 {
				r.finalizeTreeState()
				r.mu.Unlock()
				return nil
			}
			r.mu.Unlock()
		}
	}
}

// spawnRunnable launches goroutines for all currently runnable nodes. Must be called with r.mu held.
func (r *Reconciler) spawnRunnable(ctx context.Context) int {
	runnable := r.tree.RunnableNodes()
	for _, node := range runnable {
		node.State = IntentExecuting
		// F4: inject completed upstream dependency results into the node's
		// context, so strongly-coupled sequential sub-tasks (e.g. list → call →
		// write-the-result) can see prior output. Without this, decompose spawns
		// each child with an isolated context (rnix-eval mcp/hello-mcp: the
		// write-result child had no result to write).
		r.injectUpstreamContext(node)
	}
	count := len(runnable)
	for _, node := range runnable {
		go r.executeNodeWithTimeout(ctx, node)
	}
	return count
}

// injectUpstreamContext populates node.Context with the results of the node's
// completed dependencies (those listed in DependsOn). Must be called with r.mu
// held — it reads sibling node state from the tree.
func (r *Reconciler) injectUpstreamContext(node *IntentNode) {
	if len(node.DependsOn) == 0 {
		return
	}
	var b strings.Builder
	for _, depID := range node.DependsOn {
		dep, ok := r.tree.Nodes[depID]
		if !ok || dep.Result == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("Results of preceding sub-tasks (for reference):\n")
		}
		// Truncate per-dependency result (rune-safe for CJK) so injecting a
		// large upstream output doesn't blow the child's context window.
		res := dep.Result
		if rs := []rune(res); len(rs) > injectResultMaxRunes {
			res = string(rs[:injectResultMaxRunes]) + "…(truncated)"
		}
		fmt.Fprintf(&b, "\n### %s\n%s\n", dep.ID, res)
	}
	node.Context = b.String()
}

// executeNodeWithTimeout runs a single node with timeout and reports events.
func (r *Reconciler) executeNodeWithTimeout(ctx context.Context, node *IntentNode) {
	timeout := node.Timeout
	if timeout == 0 {
		timeout = r.config.DefaultTimeout
	}

	nodeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pid, err := r.spawner.SpawnIntent(nodeCtx, node)
	if err != nil {
		if nodeCtx.Err() != nil && ctx.Err() == nil {
			r.eventCh <- reconcileEvent{nodeID: node.ID, evType: evNodeTimeout, errMsg: fmt.Sprintf("spawn timeout: %v", err)}
		} else {
			r.eventCh <- reconcileEvent{nodeID: node.ID, evType: evNodeFailed, errMsg: fmt.Sprintf("spawn failed: %v", err)}
		}
		return
	}

	r.mu.Lock()
	node.PID = pid
	r.mu.Unlock()

	if r.callbacks.OnNodeStart != nil {
		r.callbacks.OnNodeStart(node.ID, pid)
	}

	type waitResult struct {
		status ExitStatus
		err    error
	}
	waitCh := make(chan waitResult, 1)
	go func() {
		status, waitErr := r.spawner.Wait(pid)
		waitCh <- waitResult{status: status, err: waitErr}
	}()

	select {
	case <-nodeCtx.Done():
		_ = r.spawner.Kill(pid)
		if ctx.Err() == nil {
			r.eventCh <- reconcileEvent{nodeID: node.ID, evType: evNodeTimeout, errMsg: fmt.Sprintf("node timeout after %v", timeout)}
		} else {
			r.eventCh <- reconcileEvent{nodeID: node.ID, evType: evNodeFailed, errMsg: "context cancelled"}
		}
		return
	case wr := <-waitCh:
		if wr.err != nil || wr.status.Code != 0 {
			errMsg := wr.status.Reason
			if wr.err != nil {
				errMsg = wr.err.Error()
			}
			r.eventCh <- reconcileEvent{nodeID: node.ID, evType: evNodeFailed, errMsg: errMsg}
			return
		}
		// F4: prefer the child's real output (Result) over the exit reason, so a
		// dependent node's injected context shows actual upstream product. Fall
		// back to Reason for spawners that don't populate Result.
		result := wr.status.Result
		if result == "" {
			result = wr.status.Reason
		}
		r.eventCh <- reconcileEvent{nodeID: node.ID, evType: evNodeCompleted, result: result}
	}
}

// finalizeTreeState sets the tree's final state. Must be called with r.mu held.
func (r *Reconciler) finalizeTreeState() {
	hasFailed := false
	for _, node := range r.tree.Nodes {
		if node.State == IntentFailed {
			hasFailed = true
			break
		}
	}
	now := time.Now()
	r.tree.CompletedAt = &now
	if hasFailed {
		r.tree.State = IntentFailed
	} else {
		r.tree.State = IntentCompleted
	}
}

// MergeAndInject atomically merges new nodes into the tree and schedules runnable nodes.
// This performs the merge under the reconciler's lock to prevent data races with the
// event loop (H3 fix), and calls spawnRunnable to schedule new nodes (H1 fix).
// Used by Manager.ApplyIncremental when the reconciler is actively executing.
func (r *Reconciler) MergeAndInject(newNodes []*IntentNode) (*MergeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result, err := MergeIncremental(r.tree, newNodes)
	if err != nil {
		return nil, err
	}

	// Set default MaxRetries for newly added nodes
	for _, id := range result.AddedNodes {
		if node, ok := r.tree.Nodes[id]; ok && node.MaxRetries == 0 {
			node.MaxRetries = r.config.DefaultMaxRetries
		}
	}

	// Schedule any newly runnable nodes
	if r.execCtx != nil {
		r.active += r.spawnRunnable(r.execCtx)
	}

	return result, nil
}
