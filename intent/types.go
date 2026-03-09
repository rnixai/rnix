package intent

import (
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
)

// IntentNode represents a single sub-intent within an IntentTree.
type IntentNode struct {
	ID        string      `json:"id" yaml:"id"`
	Intent    string      `json:"intent" yaml:"intent"`
	Agent     string      `json:"agent,omitempty" yaml:"agent,omitempty"`
	Model     string      `json:"model,omitempty" yaml:"model,omitempty"`
	DependsOn []string    `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	State     IntentState `json:"state" yaml:"state"`
	PID       types.PID   `json:"pid,omitempty" yaml:"pid,omitempty"`
	Result    string      `json:"result,omitempty" yaml:"result,omitempty"`
	Error     string      `json:"error,omitempty" yaml:"error,omitempty"`
	Children  []string    `json:"children,omitempty" yaml:"children,omitempty"`
}

// IntentTree represents the full decomposition of a high-level intent.
type IntentTree struct {
	ID          IntentID               `json:"id" yaml:"id"`
	RootIntent  string                 `json:"root_intent" yaml:"root_intent"`
	State       IntentState            `json:"state" yaml:"state"`
	Nodes       map[string]*IntentNode `json:"nodes" yaml:"nodes"`
	CreatedAt   time.Time              `json:"created_at" yaml:"created_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
}

// Progress returns the count of completed nodes and total nodes.
func (t *IntentTree) Progress() (completed, total int) {
	// ATDD RED: stub — returns zero values
	return 0, 0
}

// RunnableNodes returns all nodes whose dependencies are satisfied and state is pending.
func (t *IntentTree) RunnableNodes() []*IntentNode {
	// ATDD RED: stub — returns nil
	return nil
}

// MarkCompleted marks a node as completed with a result string and checks downstream.
func (t *IntentTree) MarkCompleted(nodeID, result string) {
	// ATDD RED: stub — no-op
}

// MarkFailed marks a node as failed and cascades failure to dependent downstream nodes.
func (t *IntentTree) MarkFailed(nodeID, errMsg string) {
	// ATDD RED: stub — no-op
}

// IsTerminal returns true when all nodes have reached a terminal state (completed or failed).
func (t *IntentTree) IsTerminal() bool {
	// ATDD RED: stub — returns false
	return false
}
