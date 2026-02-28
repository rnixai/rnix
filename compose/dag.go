package compose

import (
	"fmt"
	"sort"
	"strings"
)

// BuildDAG constructs a DAG from a ComposeSpec, detecting cycles.
func BuildDAG(spec *ComposeSpec) (*DAG, error) {
	dag := &DAG{
		Nodes: make(map[string]*DAGNode, len(spec.Agents)),
	}

	// Create nodes
	for name, agentSpec := range spec.Agents {
		dag.Nodes[name] = &DAGNode{
			Name: name,
			Spec: agentSpec,
		}
	}

	// Build edges
	for name, agentSpec := range spec.Agents {
		node := dag.Nodes[name]
		for dep := range agentSpec.DependsOn {
			node.DependsOn = append(node.DependsOn, dep)
			dag.Nodes[dep].DependedBy = append(dag.Nodes[dep].DependedBy, name)
		}
		// Sort for deterministic ordering
		sort.Strings(node.DependsOn)
	}
	// Sort DependedBy for determinism
	for _, node := range dag.Nodes {
		sort.Strings(node.DependedBy)
	}

	// Detect cycles
	cyclePath, err := dag.DetectCycle()
	if err != nil {
		return nil, fmt.Errorf("compose dag: %w (path: %v)", err, cyclePath)
	}

	return dag, nil
}

// DetectCycle uses DFS three-color marking to detect cycles.
// Returns the cycle path and error if a cycle is found, or (nil, nil) if acyclic.
func (d *DAG) DetectCycle() ([]string, error) {
	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully processed
	)

	color := make(map[string]int, len(d.Nodes))
	parent := make(map[string]string)

	var cyclePath []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		for _, dep := range d.Nodes[node].DependedBy {
			if color[dep] == gray {
				// Found cycle — reconstruct path
				cyclePath = reconstructCycle(parent, node, dep)
				return true
			}
			if color[dep] == white {
				parent[dep] = node
				if dfs(dep) {
					return true
				}
			}
		}
		color[node] = black
		return false
	}

	// Sort node names for deterministic traversal order
	names := make([]string, 0, len(d.Nodes))
	for name := range d.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if color[name] == white {
			if dfs(name) {
				return cyclePath, fmt.Errorf("cycle detected: %s", strings.Join(cyclePath, " -> "))
			}
		}
	}
	return nil, nil
}

// reconstructCycle traces back through parent map to reconstruct the cycle path.
func reconstructCycle(parent map[string]string, from, to string) []string {
	var path []string
	path = append(path, to)
	cur := from
	for cur != to {
		path = append([]string{cur}, path...)
		cur = parent[cur]
	}
	path = append([]string{to}, path...)
	return path
}

// TopologicalSort returns layers of nodes that can be executed in parallel.
// Uses Kahn's algorithm (BFS with in-degree counting).
func (d *DAG) TopologicalSort() ([][]string, error) {
	// Compute in-degrees
	inDegree := make(map[string]int, len(d.Nodes))
	for name := range d.Nodes {
		inDegree[name] = len(d.Nodes[name].DependsOn)
	}

	// Find initial layer (nodes with in-degree 0)
	var currentLayer []string
	for name, deg := range inDegree {
		if deg == 0 {
			currentLayer = append(currentLayer, name)
		}
	}
	sort.Strings(currentLayer) // deterministic ordering

	var layers [][]string
	processed := 0

	for len(currentLayer) > 0 {
		layers = append(layers, currentLayer)
		processed += len(currentLayer)

		var nextLayer []string
		for _, name := range currentLayer {
			for _, downstream := range d.Nodes[name].DependedBy {
				inDegree[downstream]--
				if inDegree[downstream] == 0 {
					nextLayer = append(nextLayer, downstream)
				}
			}
		}
		sort.Strings(nextLayer) // deterministic ordering
		currentLayer = nextLayer
	}

	if processed != len(d.Nodes) {
		return nil, fmt.Errorf("topological sort failed: graph contains a cycle")
	}

	return layers, nil
}
