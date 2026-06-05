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

// daemonRestartHit identifies a sub-task node whose intent matched the
// daemon-restart pattern, paired with the matched substring for diagnostics.
type daemonRestartHit struct {
	NodeID  string
	Matched string
}

// daemonRestartNodes returns every node whose intent would restart the hosting
// daemon, in deterministic sorted-ID order, or nil if none. ALL matches are
// returned (not just the first) so the caller can report every offending node in
// a single error and avoid a fix-one-retry-hit-the-next cycle.
//
// Rationale: auto_start runs the orchestration synchronously, bound to this
// process's (hence the daemon's) context. A sub-task that restarts the daemon
// would cancel the orchestration itself — a self-reference paradox. Architecture
// Decision 41 (2026-06-05): IntentTree orchestration does NOT span a daemon
// restart; such work must live outside the orchestration.
func daemonRestartNodes(tree *intent.IntentTree) []daemonRestartHit {
	if tree == nil {
		return nil
	}
	ids := make([]string, 0, len(tree.Nodes))
	for id := range tree.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var hits []daemonRestartHit
	for _, id := range ids {
		node := tree.Nodes[id]
		if node == nil {
			continue
		}
		if m := daemonRestartPattern.FindString(node.Intent); m != "" {
			hits = append(hits, daemonRestartHit{NodeID: id, Matched: m})
		}
	}
	return hits
}
