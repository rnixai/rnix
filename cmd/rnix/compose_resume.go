package main

import (
	"fmt"
	"io"

	"github.com/rnixai/rnix/compose"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
	"github.com/spf13/cobra"
)

// composeResumeCmd implements `rnix compose resume --node <name>` (Story 42.4).
// It finds the most recent Dead/Zombie process for the named compose node,
// invokes ResumeWithOptsV2 to revive it, then re-triggers downstream nodes via
// Engine.ExecuteFromNode.
//
// RED PHASE: command is registered but runComposeResume returns the sentinel
// errComposeResumeNotImplemented. Dev-story will wire the full pipeline:
//
//  1. Parse spec via compose.ParseFile(flagComposeResumeFile)
//  2. Validate the named node exists in spec.Agents (AC#5 — ErrInvalid → 2)
//  3. ipc.Dial (no EnsureDaemon, mirroring `compose down`)
//  4. client.ListAllProcs → filter ComposeNode==name && State∈{Dead,Zombie}
//     → newest by CreatedAt
//  5. Empty filter → ErrNotFound + exit 1 (AC#4)
//  6. Newest is Zombie ok=true → idempotent "Nothing to resume" + exit 0 (AC#5)
//  7. --dry-run → print plan + return (AC#8)
//  8. client.ResumeWithOptsV2(uuid, --fork, --from-step) (AC#7)
//  9. Poll client.GetProcDetail(uuid) until Dead/Zombie (Task 4 plan B)
// 10. Build HistoricalNodeResult map from ListAllProcs for upstream nodes
// 11. engine.ExecuteFromNode(ctx, name, resumedResult, upstreamResults)
// 12. Render results: human or --json (AC#9)
var composeResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a failed compose node and re-trigger its downstream",
	Long: `Resume a single failed compose-DAG node without re-running the whole graph.

Locates the latest Dead/Zombie process whose compose_node matches --node,
calls the daemon resume IPC (history path), then triggers the downstream
layers using the standard compose engine.`,
	Example: `  rnix compose resume --node node-B                # Resume the latest failed node-B
  rnix compose resume --node node-B -f my.yaml      # Use specified compose file
  rnix compose resume --node node-B --fork          # Fork (new UUID) instead of inheriting
  rnix compose resume --node node-B --from-step 5   # Truncate history before resuming
  rnix compose resume --node node-B --dry-run       # Preview plan, do not call IPC
  rnix compose resume --node node-B --json          # Structured JSON output`,
	RunE: runComposeResume,
}

var (
	flagComposeResumeFile     string
	flagComposeResumeNode     string
	flagComposeResumeFork     bool
	flagComposeResumeFromStep int
	flagComposeResumeDryRun   bool
)

func init() {
	composeResumeCmd.Flags().StringVarP(&flagComposeResumeFile, "file", "f", "rnix-compose.yaml", "Compose file path")
	composeResumeCmd.Flags().StringVar(&flagComposeResumeNode, "node", "", "Compose node name to resume (required)")
	composeResumeCmd.Flags().BoolVar(&flagComposeResumeFork, "fork", false, "Fork: create new UUID instead of inheriting original")
	composeResumeCmd.Flags().IntVar(&flagComposeResumeFromStep, "from-step", 0, "Truncate history replay at this step before resuming (0 = no truncation)")
	composeResumeCmd.Flags().BoolVar(&flagComposeResumeDryRun, "dry-run", false, "Preview the resume plan without sending IPC requests")
	_ = composeResumeCmd.MarkFlagRequired("node")
	composeCmd.AddCommand(composeResumeCmd)
}

// errComposeResumeNotImplemented is the sentinel error returned by the RED
// PHASE stub. Dev-story replaces the body of runComposeResume with the real
// implementation described in Story 42.4 Task 2.
var errComposeResumeNotImplemented = fmt.Errorf("not implemented: rnix compose resume (Story 42.4 RED PHASE)")

func runComposeResume(cmd *cobra.Command, args []string) error {
	_ = cmd
	_ = args
	// RED PHASE call-graph anchor: keep all helper functions referenced so
	// `unusedfunc` lint stays green until dev-story wires the real pipeline.
	// The `if false` branch is statically unreachable and adds zero runtime
	// cost; dev-story will replace the entire body with the implementation
	// described in the file-header comment.
	if false {
		var spec *compose.ComposeSpec
		var procs []vfs.ProcInfo
		_ = validateComposeNodeInSpec(spec, flagComposeResumeNode)
		_, _ = findResumableComposeProc(procs, flagComposeResumeNode)
		_ = isComposeProcIdempotent(vfs.ProcInfo{})
		_, _ = buildHistoricalUpstream(spec, procs, flagComposeResumeNode)
		renderComposeResumePlan(nil, flagComposeResumeNode, "", nil)
		renderComposeResumeText(nil, flagComposeResumeNode, 0, nil)
		renderComposeResumeJSON(nil, flagComposeResumeNode, "", nil)
	}
	return errComposeResumeNotImplemented
}

// ---------------------------------------------------------------------------
// Story 42.4 helper stubs (RED PHASE)
//
// These helpers split runComposeResume into testable pieces. The current
// implementations are stubs returning sentinel errors / zero values so the
// ATDD test scaffold compiles. Dev-story replaces each body with the real
// behavior described in Task 2 / Task 3 of the implementation artifact.
// ---------------------------------------------------------------------------

// validateComposeNodeInSpec returns nil iff nodeName exists in spec.Agents.
// AC#5: unknown node → returns a non-nil error that the CLI maps to exit code 2.
//
// RED PHASE: returns a sentinel error unconditionally to flag "not implemented".
func validateComposeNodeInSpec(spec *compose.ComposeSpec, nodeName string) error {
	_ = spec
	_ = nodeName
	return errComposeResumeNotImplemented
}

// findResumableComposeProc scans procs for the most recent Dead or Zombie
// process whose ComposeNode == nodeName, returning it and true. If none match,
// it returns the zero ProcInfo and false (CLI maps to ErrNotFound, exit 1 —
// AC#4).
//
// RED PHASE: stub — always returns (zero, false).
func findResumableComposeProc(procs []vfs.ProcInfo, nodeName string) (vfs.ProcInfo, bool) {
	_ = procs
	_ = nodeName
	return vfs.ProcInfo{}, false
}

// isComposeProcIdempotent reports whether a proc is a "nothing to resume"
// success case — Zombie state with ExitCode 0 and an empty ExitReason. AC#5.
//
// RED PHASE: stub — always returns false (no idempotent detection yet).
func isComposeProcIdempotent(p vfs.ProcInfo) bool {
	_ = p
	return false
}

// buildHistoricalUpstream constructs a map[string]HistoricalNodeResult for the
// resumed node's upstream dependencies, sourcing from procs. The keys are
// compose node names (e.g. "node-A") and values are the latest successful
// Zombie proc's persisted Result/TokensUsed/PID/SpanID.
//
// Returns an error if any required upstream has no successful run (AC#6 hard
// boundary: cannot resume from a node whose upstream never finished).
//
// RED PHASE: stub — returns errComposeResumeNotImplemented.
func buildHistoricalUpstream(spec *compose.ComposeSpec, procs []vfs.ProcInfo, resumedNode string) (map[string]compose.HistoricalNodeResult, error) {
	_ = spec
	_ = procs
	_ = resumedNode
	return nil, errComposeResumeNotImplemented
}

// renderComposeResumePlan prints a human-readable preview of the resume plan
// when --dry-run is set (AC#8). It does NOT call any IPC method.
//
// RED PHASE: stub — no-op.
func renderComposeResumePlan(w io.Writer, nodeName string, targetUUID string, downstream []string) {
	_ = w
	_ = nodeName
	_ = targetUUID
	_ = downstream
}

// renderComposeResumeText prints the human-readable summary of a completed
// resume operation: resumed node + downstream results.
//
// RED PHASE: stub — no-op.
func renderComposeResumeText(w io.Writer, resumedNode string, resumedPID types.PID, results []compose.ScheduleResult) {
	_ = w
	_ = resumedNode
	_ = resumedPID
	_ = results
}

// renderComposeResumeJSON prints the structured JSON output for --json mode.
// Schema (AC#9):
//
//	{"ok": true, "resumed_node": "node-B", "resumed_uuid": "...",
//	 "downstream": [{"name":"node-C", "exit_code":0, "tokens":N}, ...]}
//
// RED PHASE: stub — no-op.
func renderComposeResumeJSON(w io.Writer, resumedNode string, resumedUUID string, results []compose.ScheduleResult) {
	_ = w
	_ = resumedNode
	_ = resumedUUID
	_ = results
}
