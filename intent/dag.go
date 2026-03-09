package intent

// DAG represents the directed acyclic graph of intent node dependencies.
type DAG struct {
	Nodes map[string]*DAGNode
}

// DAGNode represents a single node in the intent DAG.
type DAGNode struct {
	Name       string
	Node       *IntentNode
	DependsOn  []string
	DependedBy []string
}

// BuildIntentDAG constructs a DAG from an IntentTree, detecting cycles.
func BuildIntentDAG(tree *IntentTree) (*DAG, error) {
	// ATDD RED: stub — returns nil DAG with nil error
	return nil, nil
}

// DetectCycle uses DFS to detect cycles in the DAG.
// Returns the cycle path and error if found, or (nil, nil) if acyclic.
func (d *DAG) DetectCycle() ([]string, error) {
	// ATDD RED: stub — returns no cycle
	return nil, nil
}

// TopologicalSort returns layers of node IDs that can be executed in parallel.
func (d *DAG) TopologicalSort() ([][]string, error) {
	// ATDD RED: stub — returns nil
	return nil, nil
}
