package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/rnixai/rnix/ipc"
)

// RenderIntentTree renders the intent tree decomposition.
func RenderIntentTree(r *Renderer, tree *ipc.IntentTreeWire, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(tree)
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}

	// Calculate progress
	completed := 0
	total := len(tree.Nodes)
	for _, node := range tree.Nodes {
		if node.State == "completed" {
			completed++
		}
	}

	fmt.Fprintf(r.Writer, "\n[intent] %s  (state: %s)\n", tree.RootIntent, tree.State)
	fmt.Fprintf(r.Writer, "  ID: %s\n", tree.ID)
	if total > 0 {
		pct := 0
		if total > 0 {
			pct = completed * 100 / total
		}
		fmt.Fprintf(r.Writer, "  Progress: %d/%d (%d%%)\n", completed, total, pct)
	}

	ids := make([]string, 0, len(tree.Nodes))
	for id := range tree.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	fmt.Fprintf(r.Writer, "\n  %-12s %-40s %-12s %s\n", "ID", "Intent", "State", "Depends On")
	fmt.Fprintf(r.Writer, "  %-12s %-40s %-12s %s\n", "---", "---", "---", "---")

	for _, id := range ids {
		node := tree.Nodes[id]
		deps := "-"
		if len(node.DependsOn) > 0 {
			deps = fmt.Sprintf("%v", node.DependsOn)
		}
		stateIcon := intentStateIcon(node.State)
		pidInfo := ""
		if node.PID > 0 {
			pidInfo = fmt.Sprintf(" PID:%d", node.PID)
		}
		fmt.Fprintf(r.Writer, "  %-12s %-40s %s %-10s %s%s\n", node.ID, truncate(node.Intent, 40), stateIcon, node.State, deps, pidInfo)
	}
}

// RenderIntentProgress renders a progress update.
func RenderIntentProgress(r *Renderer, completed, total int, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(map[string]int{"completed": completed, "total": total})
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}
	fmt.Fprintf(r.Writer, "[intent] progress: %d/%d\n", completed, total)
}

// RenderIntentNodeEvent renders a node lifecycle event.
func RenderIntentNodeEvent(r *Renderer, eventType, nodeID, detail string, pid uint64, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(map[string]any{
			"event":   eventType,
			"node_id": nodeID,
			"detail":  detail,
			"pid":     pid,
		})
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}

	switch eventType {
	case "start":
		fmt.Fprintf(r.Writer, "[intent] ▶ %s: spawned (PID %d) — %s\n", nodeID, pid, detail)
	case "done":
		fmt.Fprintf(r.Writer, "[intent] ✓ %s: completed — %s\n", nodeID, detail)
	case "failed":
		fmt.Fprintf(r.Writer, "[intent] ✗ %s: failed — %s\n", nodeID, detail)
	case "drift":
		fmt.Fprintf(r.Writer, "[intent] ⚠ %s: drift detected — %s\n", nodeID, detail)
	case "drift_resolved":
		fmt.Fprintf(r.Writer, "[intent] ✓ %s: %s\n", nodeID, detail)
	case "error":
		fmt.Fprintf(r.Writer, "[intent] ✗ error: %s\n", detail)
	case "complete":
		fmt.Fprintf(r.Writer, "[intent] ✓ %s\n", detail)
	}
}

// RenderIntentNodeRetry renders a retry event.
func RenderIntentNodeRetry(r *Renderer, nodeID string, attempt, maxRetries int, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(map[string]any{
			"event":       "retry",
			"node_id":     nodeID,
			"attempt":     attempt,
			"max_retries": maxRetries,
		})
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}
	fmt.Fprintf(r.Writer, "[intent] ↻ %s: retrying (attempt %d/%d)\n", nodeID, attempt, maxRetries)
}

// RenderIntentNodeTimeout renders a timeout event.
func RenderIntentNodeTimeout(r *Renderer, nodeID string, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(map[string]any{
			"event":   "timeout",
			"node_id": nodeID,
		})
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}
	fmt.Fprintf(r.Writer, "[intent] ⏱ %s: timed out\n", nodeID)
}

// RenderDriftList renders the list of active drifts.
func RenderDriftList(r *Renderer, drifts []ipc.DriftItemWire, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(map[string]any{"drifts": drifts})
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}
	if len(drifts) == 0 {
		fmt.Fprintln(r.Writer, "[intent] No active drifts.")
		return
	}
	fmt.Fprintf(r.Writer, "\n  %-12s %-16s %s\n", "Node", "Type", "Message")
	fmt.Fprintf(r.Writer, "  %-12s %-16s %s\n", "---", "---", "---")
	for _, d := range drifts {
		fmt.Fprintf(r.Writer, "  %-12s %-16s %s\n", d.NodeID, d.Type, d.Message)
	}
}

// RenderIntentMergeResult renders the result of an incremental merge operation.
func RenderIntentMergeResult(r *Renderer, added, modified []string, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(map[string]any{
			"added_nodes":    added,
			"modified_nodes": modified,
		})
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}

	fmt.Fprintln(r.Writer, "\n[intent] Incremental merge result:")
	if len(added) > 0 {
		fmt.Fprintf(r.Writer, "  Added nodes:    %v\n", added)
	}
	if len(modified) > 0 {
		fmt.Fprintf(r.Writer, "  Modified nodes: %v\n", modified)
	}
	if len(added) == 0 && len(modified) == 0 {
		fmt.Fprintln(r.Writer, "  No changes.")
	}
}

// RenderIntentStatusDetail renders an enhanced status view for a single intent.
func RenderIntentStatusDetail(r *Renderer, tree *ipc.IntentTreeWire, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(tree)
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}

	// Calculate progress
	completed := 0
	total := len(tree.Nodes)
	for _, node := range tree.Nodes {
		if node.State == "completed" {
			completed++
		}
	}

	pct := 0
	if total > 0 {
		pct = completed * 100 / total
	}

	fmt.Fprintf(r.Writer, "\nIntent: %s\n", tree.ID)
	fmt.Fprintf(r.Writer, "Root: %q\n", tree.RootIntent)
	fmt.Fprintf(r.Writer, "State: %s\n", tree.State)
	fmt.Fprintf(r.Writer, "Progress: %d/%d (%d%%)\n", completed, total, pct)

	// Group nodes by state
	stateOrder := []string{"executing", "retrying", "pending", "completed", "failed"}
	stateGroups := make(map[string][]*ipc.IntentNodeWire)
	for _, node := range tree.Nodes {
		stateGroups[node.State] = append(stateGroups[node.State], node)
	}

	fmt.Fprintln(r.Writer, "\nNodes:")
	for _, state := range stateOrder {
		nodes, ok := stateGroups[state]
		if !ok {
			continue
		}
		// Sort nodes by ID within each group
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].ID < nodes[j].ID
		})
		for _, node := range nodes {
			icon := intentStateIcon(node.State)
			pidInfo := ""
			if node.PID > 0 {
				pidInfo = fmt.Sprintf("  PID: %d", node.PID)
			}
			fmt.Fprintf(r.Writer, "  [%s %s] %-12s - %s%s\n", icon, node.State, node.ID, truncate(node.Intent, 50), pidInfo)
		}
	}

	// Active agents section
	var activeAgents []*ipc.IntentNodeWire
	for _, node := range tree.Nodes {
		if node.State == "executing" && node.PID > 0 {
			activeAgents = append(activeAgents, node)
		}
	}
	if len(activeAgents) > 0 {
		sort.Slice(activeAgents, func(i, j int) bool {
			return activeAgents[i].ID < activeAgents[j].ID
		})
		fmt.Fprintln(r.Writer, "\nActive Agents:")
		for _, agent := range activeAgents {
			fmt.Fprintf(r.Writer, "  %s (PID %d) - %s\n", agent.ID, agent.PID, truncate(agent.Intent, 60))
		}
	}

	// Drifts section
	if len(tree.Drifts) > 0 {
		fmt.Fprintf(r.Writer, "\nDrifts: %d\n", len(tree.Drifts))
	} else {
		fmt.Fprintln(r.Writer, "\nDrifts: (none)")
	}
}

// RenderIntentList renders a table of all intents.
func RenderIntentList(r *Renderer, trees []*ipc.IntentTreeWire, mode OutputMode) {
	if mode == ModeJSON {
		data, _ := json.Marshal(map[string]any{"intents": trees})
		fmt.Fprintln(r.Writer, string(data))
		return
	}
	if mode == ModeQuiet {
		return
	}

	if len(trees) == 0 {
		fmt.Fprintln(r.Writer, "No intents found.")
		return
	}

	fmt.Fprintf(r.Writer, "\n%-12s %-40s %-12s %-12s %s\n", "ID", "Root Intent", "State", "Progress", "Created")
	fmt.Fprintf(r.Writer, "%-12s %-40s %-12s %-12s %s\n", "---", "---", "---", "---", "---")

	for _, tree := range trees {
		completed := 0
		total := len(tree.Nodes)
		for _, node := range tree.Nodes {
			if node.State == "completed" {
				completed++
			}
		}
		progress := fmt.Sprintf("%d/%d", completed, total)
		created := time.UnixMilli(tree.CreatedAtMs).Format("2006-01-02 15:04")
		icon := intentStateIcon(tree.State)
		fmt.Fprintf(r.Writer, "%-12s %-40s %s %-10s %-12s %s\n", tree.ID, truncate(tree.RootIntent, 40), icon, tree.State, progress, created)
	}
}

func intentStateIcon(state string) string {
	switch state {
	case "completed":
		return "✓"
	case "failed":
		return "✗"
	case "executing":
		return "▶"
	case "pending":
		return "○"
	case "retrying":
		return "↻"
	default:
		return "·"
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
