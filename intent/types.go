package intent

import (
	"sort"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// IntentID uniquely identifies an intent tree.
type IntentID string

// IntentState represents the lifecycle state of an intent node or tree.
type IntentState string

const (
	IntentPending      IntentState = "pending"
	IntentDecomposing  IntentState = "decomposing"
	IntentAwaitConfirm IntentState = "await_confirm"
	IntentExecuting    IntentState = "executing"
	IntentCompleted    IntentState = "completed"
	IntentFailed       IntentState = "failed"
	IntentRetrying     IntentState = "retrying"
)

// IntentNode represents a single sub-intent within an IntentTree.
type IntentNode struct {
	ID           string        `json:"id" yaml:"id"`
	Intent       string        `json:"intent" yaml:"intent"`
	Agent        string        `json:"agent,omitempty" yaml:"agent,omitempty"`
	Model        string        `json:"model,omitempty" yaml:"model,omitempty"`
	DependsOn    []string      `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	State        IntentState   `json:"state" yaml:"state"`
	PID          types.PID     `json:"pid,omitempty" yaml:"pid,omitempty"`
	Result       string        `json:"result,omitempty" yaml:"result,omitempty"`
	Error        string        `json:"error,omitempty" yaml:"error,omitempty"`
	Children     []string      `json:"children,omitempty" yaml:"children,omitempty"`
	RetryCount   int           `json:"retry_count" yaml:"retry_count"`
	MaxRetries   int           `json:"max_retries" yaml:"max_retries"`
	Timeout      time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	LastFailedAt *time.Time    `json:"last_failed_at,omitempty" yaml:"last_failed_at,omitempty"`
}

// CanRetry returns true if the node has remaining retry attempts.
func (n *IntentNode) CanRetry() bool {
	return n.RetryCount < n.MaxRetries
}

// IncrRetry increments the retry counter and records the failure time.
func (n *IntentNode) IncrRetry() {
	n.RetryCount++
	now := time.Now()
	n.LastFailedAt = &now
}

// DriftType classifies the kind of drift between desired and current state.
type DriftType string

const (
	DriftNodeFailed  DriftType = "node_failed"
	DriftNodeTimeout DriftType = "node_timeout"
)

// DriftItem records a single divergence between desired and current state.
type DriftItem struct {
	NodeID     string    `json:"node_id"`
	Type       DriftType `json:"type"`
	Message    string    `json:"message"`
	DetectedAt time.Time `json:"detected_at"`
}

// IntentTree represents the full decomposition of a high-level intent.
type IntentTree struct {
	ID           IntentID               `json:"id" yaml:"id"`
	RootIntent   string                 `json:"root_intent" yaml:"root_intent"`
	State        IntentState            `json:"state" yaml:"state"`
	Nodes        map[string]*IntentNode `json:"nodes" yaml:"nodes"`
	DesiredNodes map[string]IntentState `json:"desired_nodes,omitempty" yaml:"desired_nodes,omitempty"`
	Drifts       []DriftItem            `json:"drifts,omitempty" yaml:"drifts,omitempty"`
	CreatedAt    time.Time              `json:"created_at" yaml:"created_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
}

// InitDesired sets the desired state for all nodes to IntentCompleted.
func (t *IntentTree) InitDesired() {
	t.DesiredNodes = make(map[string]IntentState, len(t.Nodes))
	for id := range t.Nodes {
		t.DesiredNodes[id] = IntentCompleted
	}
}

// ComputeDrifts scans all nodes and returns items where current != desired.
func (t *IntentTree) ComputeDrifts() []DriftItem {
	var drifts []DriftItem
	for id, node := range t.Nodes {
		desired, ok := t.DesiredNodes[id]
		if !ok {
			continue
		}
		if node.State == desired {
			continue
		}
		if node.State != IntentFailed {
			continue
		}
		drifts = append(drifts, DriftItem{
			NodeID:     id,
			Type:       DriftNodeFailed,
			Message:    node.Error,
			DetectedAt: time.Now(),
		})
	}
	return drifts
}

// AddDrift appends a drift record.
func (t *IntentTree) AddDrift(item DriftItem) {
	t.Drifts = append(t.Drifts, item)
}

// ClearDrift removes drift records for the given node.
func (t *IntentTree) ClearDrift(nodeID string) {
	filtered := t.Drifts[:0]
	for _, d := range t.Drifts {
		if d.NodeID != nodeID {
			filtered = append(filtered, d)
		}
	}
	t.Drifts = filtered
}

// ActiveDrifts returns unresolved drift items.
func (t *IntentTree) ActiveDrifts() []DriftItem {
	result := make([]DriftItem, len(t.Drifts))
	copy(result, t.Drifts)
	return result
}

// Progress returns the count of completed nodes and total nodes.
func (t *IntentTree) Progress() (completed, total int) {
	total = len(t.Nodes)
	for _, node := range t.Nodes {
		if node.State == IntentCompleted {
			completed++
		}
	}
	return completed, total
}

// RunnableNodes returns all nodes whose dependencies are satisfied and state is pending or retrying.
func (t *IntentTree) RunnableNodes() []*IntentNode {
	var runnable []*IntentNode
	for _, node := range t.Nodes {
		if node.State != IntentPending && node.State != IntentRetrying {
			continue
		}
		allDepsSatisfied := true
		for _, dep := range node.DependsOn {
			depNode, ok := t.Nodes[dep]
			if !ok || depNode.State != IntentCompleted {
				allDepsSatisfied = false
				break
			}
		}
		if allDepsSatisfied {
			runnable = append(runnable, node)
		}
	}
	sort.Slice(runnable, func(i, j int) bool {
		return runnable[i].ID < runnable[j].ID
	})
	return runnable
}

// MarkCompleted marks a node as completed with a result string.
func (t *IntentTree) MarkCompleted(nodeID, result string) {
	node, ok := t.Nodes[nodeID]
	if !ok {
		return
	}
	node.State = IntentCompleted
	node.Result = result
}

// MarkFailed marks a node as failed and cascades failure to dependent downstream nodes.
func (t *IntentTree) MarkFailed(nodeID, errMsg string) {
	node, ok := t.Nodes[nodeID]
	if !ok {
		return
	}
	node.State = IntentFailed
	node.Error = errMsg

	// Cascade failure to all downstream nodes that depend (directly or transitively) on this node
	t.cascadeFailure(nodeID)
}

func (t *IntentTree) cascadeFailure(failedID string) {
	for _, node := range t.Nodes {
		if node.State == IntentFailed || node.State == IntentCompleted {
			continue
		}
		for _, dep := range node.DependsOn {
			if dep == failedID {
				node.State = IntentFailed
				node.Error = "upstream dependency failed: " + failedID
				t.cascadeFailure(node.ID)
				break
			}
		}
	}
}

// IsTerminal returns true when all nodes have reached a terminal state (completed or failed).
func (t *IntentTree) IsTerminal() bool {
	for _, node := range t.Nodes {
		if node.State != IntentCompleted && node.State != IntentFailed {
			return false
		}
	}
	return true
}
