package intent

import (
	"fmt"
	"sort"
	"strings"
)

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
	dag := &DAG{
		Nodes: make(map[string]*DAGNode, len(tree.Nodes)),
	}

	for id, node := range tree.Nodes {
		dag.Nodes[id] = &DAGNode{
			Name: id,
			Node: node,
		}
	}

	for id, node := range tree.Nodes {
		dagNode := dag.Nodes[id]
		for _, dep := range node.DependsOn {
			if _, ok := dag.Nodes[dep]; !ok {
				return nil, fmt.Errorf("intent dag: node %q depends on unknown node %q", id, dep)
			}
			dagNode.DependsOn = append(dagNode.DependsOn, dep)
			dag.Nodes[dep].DependedBy = append(dag.Nodes[dep].DependedBy, id)
		}
		sort.Strings(dagNode.DependsOn)
	}
	for _, dagNode := range dag.Nodes {
		sort.Strings(dagNode.DependedBy)
	}

	cyclePath, err := dag.DetectCycle()
	if err != nil {
		return nil, fmt.Errorf("intent dag: %w (path: %v)", err, cyclePath)
	}

	return dag, nil
}

// DetectCycle uses DFS three-color marking to detect cycles.
func (d *DAG) DetectCycle() ([]string, error) {
	const (
		white = 0
		gray  = 1
		black = 2
	)

	color := make(map[string]int, len(d.Nodes))
	parent := make(map[string]string)

	var cyclePath []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		for _, dep := range d.Nodes[node].DependedBy {
			if color[dep] == gray {
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

// TopologicalSort returns layers of node IDs that can be executed in parallel.
func (d *DAG) TopologicalSort() ([][]string, error) {
	inDegree := make(map[string]int, len(d.Nodes))
	for name := range d.Nodes {
		inDegree[name] = len(d.Nodes[name].DependsOn)
	}

	var currentLayer []string
	for name, deg := range inDegree {
		if deg == 0 {
			currentLayer = append(currentLayer, name)
		}
	}
	sort.Strings(currentLayer)

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
		sort.Strings(nextLayer)
		currentLayer = nextLayer
	}

	if processed != len(d.Nodes) {
		return nil, fmt.Errorf("topological sort failed: graph contains a cycle")
	}

	return layers, nil
}
