package intentdriver

import (
	"regexp"
	"sort"

	"github.com/rnixai/rnix/intent"
)

// daemonRestartPattern matches sub-task intents that would restart the hosting
// daemon. Only stop|restart are matched — a bare "rnix daemon start" does not
// kill the running daemon, so it is not a self-reference hazard.
var daemonRestartPattern = regexp.MustCompile(`(?i)\brnix\s+daemon\s+(stop|restart)\b`)

// firstDaemonRestartNode returns the ID and matched substring of the first node
// (deterministic by sorted ID, for stable error messages) whose intent would
// restart the hosting daemon, or ("", "") if none.
//
// Rationale: auto_start runs the orchestration synchronously, bound to this
// process's (hence the daemon's) context. A sub-task that restarts the daemon
// would cancel the orchestration itself — a self-reference paradox. Architecture
// Decision 41 (2026-06-05): IntentTree orchestration does NOT span a daemon
// restart; such work must live outside the orchestration.
func firstDaemonRestartNode(tree *intent.IntentTree) (nodeID, matched string) {
	if tree == nil {
		return "", ""
	}
	ids := make([]string, 0, len(tree.Nodes))
	for id := range tree.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := tree.Nodes[id]
		if node == nil {
			continue
		}
		if m := daemonRestartPattern.FindString(node.Intent); m != "" {
			return id, m
		}
	}
	return "", ""
}
