package ui

import (
	"encoding/json"
	"fmt"
	"sort"

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

	fmt.Fprintf(r.Writer, "\n[intent] %s  (state: %s)\n", tree.RootIntent, tree.State)
	fmt.Fprintf(r.Writer, "  ID: %s\n", tree.ID)

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
		fmt.Fprintf(r.Writer, "  %-12s %-40s %s %-10s %s\n", node.ID, truncate(node.Intent, 40), stateIcon, node.State, deps)
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
