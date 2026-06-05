package intentdriver

import (
	"regexp"
	"sort"

	"github.com/rnixai/rnix/intent"
)

// daemonRestartPattern is a best-effort, literal matcher for sub-task intents
// that name a daemon stop/restart. Only stop|restart are matched — a bare
// "rnix daemon start" does not terminate the running daemon, so it is not
// flagged. This is a literal regex over the LLM-authored intent text, not a
// guarantee: a sub-agent can still terminate the daemon by other means (e.g.
// kill/pkill/socat via /dev/shell, or phrasings this pattern does not cover).
var daemonRestartPattern = regexp.MustCompile(`(?i)\brnix\s+daemon\s+(stop|restart)\b`)

// daemonRestartHit identifies a sub-task node whose intent matched the
// daemon-restart pattern, paired with the matched substring for diagnostics.
type daemonRestartHit struct {
	NodeID  string
	Matched string
}

// daemonRestartNodes returns every node whose intent literally names a daemon
// stop/restart, in deterministic sorted-ID order, or nil if none. ALL matches
// are returned (not just the first) so the caller can report every flagged node
// in a single error and avoid a fix-one-retry-hit-the-next cycle.
//
// Best-effort, not exhaustive: auto_start runs the orchestration synchronously
// inside this daemon process, so a sub-task that stops the daemon may interrupt
// the orchestration. The match is over the node's free-text intent, which can
// diverge from what a sub-agent actually executes (e.g. via /dev/shell), so it
// can be evaded and is not a reliable barrier. The reliable fix is to keep such
// work outside the orchestration; see ADR Decision 43 — detection stays
// best-effort, the root-cause fix is eval externalization (rnix-eval).
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
