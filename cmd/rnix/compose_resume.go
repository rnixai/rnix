package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/compose"
	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
	"github.com/spf13/cobra"
)

// composeResumeCmd implements `rnix compose resume --node <name>` (Story 42.4).
// It finds the most recent Dead/Zombie process for the named compose node,
// invokes ResumeWithOptsV2 to revive it, then re-triggers downstream nodes via
// Engine.ExecuteFromNode.
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
	flagComposeResumeFile        string
	flagComposeResumeNode        string
	flagComposeResumeFork        bool
	flagComposeResumeFromStep    int
	flagComposeResumeDryRun      bool
	flagComposeResumeWaitTimeout time.Duration
)

func init() {
	composeResumeCmd.Flags().StringVarP(&flagComposeResumeFile, "file", "f", "rnix-compose.yaml", "Compose file path")
	composeResumeCmd.Flags().StringVar(&flagComposeResumeNode, "node", "", "Compose node name to resume (required)")
	composeResumeCmd.Flags().BoolVar(&flagComposeResumeFork, "fork", false, "Fork: create new UUID instead of inheriting original")
	composeResumeCmd.Flags().IntVar(&flagComposeResumeFromStep, "from-step", 0, "Truncate history replay at this step before resuming (0 = no truncation)")
	composeResumeCmd.Flags().BoolVar(&flagComposeResumeDryRun, "dry-run", false, "Preview the resume plan without sending IPC requests")
	composeResumeCmd.Flags().DurationVar(&flagComposeResumeWaitTimeout, "wait-timeout", 0, "Abort the wait after this duration if the resumed node has not exited (0 = no timeout, rely on SIGINT)")
	_ = composeResumeCmd.MarkFlagRequired("node")
	composeCmd.AddCommand(composeResumeCmd)
}

const (
	composeResumePollInterval = 200 * time.Millisecond
	// composeResumeProgressInterval controls how often `wait` prints a
	// "still waiting" message in interactive modes. Modeled after cc-src
	// BashTool's PROGRESS_THRESHOLD_MS pattern: keep users informed but quiet
	// in JSON / quiet modes.
	composeResumeProgressInterval = 5 * time.Second
)

// runComposeResume implements the `rnix compose resume` command (Story 42.4).
func runComposeResume(cmd *cobra.Command, args []string) error {
	_ = cmd
	_ = args

	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	// 1. Parse compose file
	spec, err := compose.ParseFile(flagComposeResumeFile)
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			"compose file parse failed", "check rnix-compose.yaml syntax")
		exitCode = 2
		return nil
	}

	// 2. Validate node exists in spec (AC#5 — ErrInvalid → exit 2)
	if vErr := validateComposeNodeInSpec(spec, flagComposeResumeNode); vErr != nil {
		outputError(renderer, mode, "compose-resume", vErr.Error(),
			fmt.Sprintf("node %q not in compose spec", flagComposeResumeNode),
			"check rnix-compose.yaml agents list")
		exitCode = 2
		return nil
	}

	// 3. Connect to daemon (no EnsureDaemon — mirrors `compose down`)
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			"no active daemon",
			"start daemon first: rnix daemon status")
		exitCode = 1
		return nil
	}
	defer client.Close()

	// 4. List all daemon processes
	procs, err := client.ListAllProcs()
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			"failed to list processes", "check daemon status")
		exitCode = 1
		return nil
	}

	// 5. Find latest Dead/Zombie process for this compose node
	target, ok := findResumableComposeProc(procs, flagComposeResumeNode)
	if !ok {
		outputError(renderer, mode, "compose-resume",
			fmt.Sprintf("ErrNotFound: no resumable process for compose node %q", flagComposeResumeNode),
			"no Dead or Zombie history found for the named compose node",
			"rnix ps --uuid  to see processes with UUIDs")
		exitCode = 1
		return nil
	}

	// 6. Idempotent: latest target is Zombie + ExitReason empty (success). AC#5.
	if isComposeProcIdempotent(target) {
		if mode == ui.ModeJSON {
			payload := map[string]any{
				"ok":           true,
				"resumed_node": flagComposeResumeNode,
				"resumed_uuid": target.UUID,
				"idempotent":   true,
				"downstream":   []dsEntry{},
			}
			data, _ := json.Marshal(payload)
			fmt.Fprintln(renderer.Writer, string(data))
		} else if mode != ui.ModeQuiet {
			prefix := ui.KernelStyle.Render("[compose-resume]")
			fmt.Fprintf(renderer.Writer, "%s Nothing to resume: latest %s completed successfully (UUID %s, exit_code=0)\n",
				prefix, flagComposeResumeNode, target.UUID)
		}
		return nil
	}

	// 7. --dry-run: print plan only (AC#8)
	downstream := downstreamComposeNodes(spec, flagComposeResumeNode)
	if flagComposeResumeDryRun {
		if mode == ui.ModeJSON {
			downstreamEntries := make([]dsEntry, 0, len(downstream))
			for _, n := range downstream {
				downstreamEntries = append(downstreamEntries, dsEntry{Name: n, ExitCode: 0, Tokens: 0, Planned: true})
			}
			payload := map[string]any{
				"ok":           true,
				"resumed_node": flagComposeResumeNode,
				"resumed_uuid": target.UUID,
				"downstream":   downstreamEntries,
				"dry_run":      true,
			}
			data, _ := json.Marshal(payload)
			fmt.Fprintln(renderer.Writer, string(data))
		} else {
			renderComposeResumePlan(renderer.Writer, flagComposeResumeNode, target.UUID, downstream)
		}
		return nil
	}

	// 8. Resume target via daemon — Epic 42 fix: pass ProjectDir + RNIX_ENV so
	// the resumed compose node inherits project-level API keys.
	cwd, _ := os.Getwd()
	projectDir, _ := config.ProjectDir(cwd)
	rnixEnv := os.Getenv("RNIX_ENV")
	resumeResp, err := client.ResumeWithOptsV3(target.UUID, flagComposeResumeFork, flagComposeResumeFromStep, projectDir, rnixEnv)
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			fmt.Sprintf("resume failed for UUID %s", target.UUID),
			"rnix ps --uuid  to see process states")
		exitCode = 1
		return nil
	}

	// 9. Set up cancellation context + signal handling.
	// Long-running resumed processes (multi-step reasoning, large generations)
	// may legitimately exceed any fixed deadline. Default to no hard timeout;
	// users press Ctrl-C (SIGINT) to abort, or set --wait-timeout for an
	// escape hatch. Cf. cc-src BashTool: long-running shell commands rely on
	// onTimeout/Ctrl-B rather than hard kills.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case <-sigCh:
			if mode != ui.ModeJSON && mode != ui.ModeQuiet {
				prefix := ui.KernelStyle.Render("[compose-resume]")
				fmt.Fprintf(renderer.Writer, "%s interrupted (SIGINT), cancelling...\n", prefix)
			}
			cancel()
			// Second signal within 5s forces immediate exit.
			select {
			case <-sigCh:
				forceExitFunc(130)
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
			}
		case <-ctx.Done():
			// Main flow returned normally — goroutine exits cleanly.
		}
	}()

	// 10. Wait for resumed process to exit (poll daemon).
	resumed, err := waitResumedExit(ctx, client, resumeResp.UUID, flagComposeResumeWaitTimeout, renderer, mode)
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			fmt.Sprintf("resumed process %s did not finish", resumeResp.UUID),
			"check daemon logs or rerun with --wait-timeout=0 (no timeout)")
		exitCode = 1
		return nil
	}
	if resumed.ExitReason != "" {
		// Resumed process itself failed — skip downstream, return failure.
		outputError(renderer, mode, "compose-resume",
			fmt.Sprintf("resumed process exited with reason: %s", resumed.ExitReason),
			fmt.Sprintf("node %s failed during resume", flagComposeResumeNode),
			"rnix proc  to inspect the failure")
		exitCode = 1
		return nil
	}

	// 11. Build historical upstream map (AC#6) — refresh procs in case state changed
	procs, err = client.ListAllProcs()
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			"failed to re-list processes for upstream", "check daemon status")
		exitCode = 1
		return nil
	}
	upstream, err := buildHistoricalUpstream(spec, procs, flagComposeResumeNode)
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			"upstream history incomplete",
			fmt.Sprintf("resume the upstream node first, then retry --node %s", flagComposeResumeNode))
		exitCode = 1
		return nil
	}

	// 12. Build engine and run ExecuteFromNode
	socketPath := ipc.SocketPath()
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		fmt.Fprintf(os.Stderr, "[compose-resume] warning: os.Getwd failed: %v (using empty path)\n", cwdErr)
	}
	projectDir, projectErr := config.ProjectDir(cwd)
	if projectErr != nil {
		fmt.Fprintf(os.Stderr, "[compose-resume] warning: config.ProjectDir failed: %v (skill/agent search confined to global dir)\n", projectErr)
	}
	spawner := newIPCKernelSpawner(socketPath, projectDir)
	globalDir, globalErr := config.GlobalDir()
	if globalErr != nil {
		fmt.Fprintf(os.Stderr, "[compose-resume] warning: config.GlobalDir failed: %v (skill/agent search will be incomplete)\n", globalErr)
	}

	var skillSearchDirs []string
	var agentSearchDirs []string
	if projectDir != "" {
		skillSearchDirs = append(skillSearchDirs, filepath.Join(projectDir, ".rnix", "skills"))
		agentSearchDirs = append(agentSearchDirs, filepath.Join(projectDir, ".rnix", "agents"))
	}
	if globalDir != "" {
		skillSearchDirs = append(skillSearchDirs, filepath.Join(globalDir, "skills"))
		agentSearchDirs = append(agentSearchDirs, filepath.Join(globalDir, "agents"))
	}

	skillLoader := skills.NewSkillLoader(skillSearchDirs)

	var mcpCfg *mcp.MCPGlobalConfig
	if globalDir != "" {
		mcpPath := filepath.Join(globalDir, "mcp.yaml")
		if _, statErr := os.Stat(mcpPath); statErr == nil {
			if cfg, loadErr := mcp.LoadMCPConfig(mcpPath); loadErr == nil {
				mcpCfg = cfg
			} else {
				fmt.Fprintf(os.Stderr, "[compose-resume] warning: mcp.LoadMCPConfig(%s) failed: %v (MCP servers will not be available)\n", mcpPath, loadErr)
			}
		}
	}
	agentLoader := agents.NewAgentLoader(agentSearchDirs, skillLoader, mcpCfg)
	agentLoaderFunc := compose.AgentLoaderFunc(agentLoader.Load)

	engine, err := compose.NewEngine(spec, spawner, agentLoaderFunc)
	if err != nil {
		outputError(renderer, mode, "compose-resume", err.Error(),
			"compose engine creation failed", "check rnix-compose.yaml for circular dependencies")
		exitCode = 2
		return nil
	}

	// 13. Execute downstream
	resumedHist := compose.HistoricalNodeResult{
		PID:      resumed.PID,
		Output:   resumed.Result,
		Tokens:   resumed.TokensUsed,
		ExitCode: 0,
	}
	results, execErr := engine.ExecuteFromNode(ctx, flagComposeResumeNode, resumedHist, upstream)
	if execErr != nil && len(results) == 0 {
		outputError(renderer, mode, "compose-resume", execErr.Error(),
			"downstream execution failed", "rnix compose down  to clean up")
		exitCode = 1
		return nil
	}

	// 14. Render results
	if mode == ui.ModeJSON {
		renderComposeResumeJSON(renderer.Writer, flagComposeResumeNode, resumeResp.UUID, results)
	} else {
		renderComposeResumeText(renderer.Writer, flagComposeResumeNode, resumeResp.PID, results)
	}

	// 15. Set exit code = max(downstream exit codes). Matches Dev Notes
	// sequence-diagram semantics; preserves the real exit code rather than
	// flattening every failure to 1.
	for _, r := range results {
		if r.Name == flagComposeResumeNode {
			continue
		}
		if r.Err != nil && exitCode == 0 {
			exitCode = 1
		}
		if r.ExitCode > exitCode {
			exitCode = r.ExitCode
		}
	}

	return nil
}

// waitResumedExit polls the daemon for the resumed UUID until it reaches Dead
// or Zombie state, returning the final ProcInfo. Story 42.4 Task 4 method B.
//
// timeout == 0 disables the hard deadline — callers rely on ctx cancellation
// (typically SIGINT) to bail out of unbounded waits. This mirrors cc-src
// BashTool's long-running model: no surprise timeouts kill genuine work.
// When timeout > 0, the function also returns ErrTimeout after the deadline.
func waitResumedExit(ctx context.Context, client *ipc.Client, uuid string, timeout time.Duration, renderer *ui.Renderer, mode ui.OutputMode) (vfs.ProcInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	var deadline time.Time
	if timeout > 0 {
		deadline = start.Add(timeout)
	}
	progressTicker := time.NewTicker(composeResumeProgressInterval)
	defer progressTicker.Stop()
	pollTimer := time.NewTimer(0) // fire immediately on first iteration
	defer pollTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return vfs.ProcInfo{}, ctx.Err()
		case <-progressTicker.C:
			if renderer != nil && mode != ui.ModeJSON && mode != ui.ModeQuiet {
				prefix := ui.KernelStyle.Render("[compose-resume]")
				elapsed := time.Since(start).Round(time.Second)
				fmt.Fprintf(renderer.Writer, "%s still waiting for resumed UUID %s (%s elapsed)\n",
					prefix, uuid, elapsed)
			}
			continue
		case <-pollTimer.C:
			procs, err := client.ListAllProcs()
			if err != nil {
				return vfs.ProcInfo{}, fmt.Errorf("ipc list_procs: %w", err)
			}
			for _, p := range procs {
				if p.UUID == uuid && (p.State == types.StateDead || p.State == types.StateZombie) {
					return p, nil
				}
			}
			if timeout > 0 && time.Now().After(deadline) {
				return vfs.ProcInfo{}, fmt.Errorf("ErrTimeout: resumed process %s did not finish in %s", uuid, timeout)
			}
			pollTimer.Reset(composeResumePollInterval)
		}
	}
}

// validateComposeNodeInSpec returns nil iff nodeName exists in spec.Agents.
// AC#5: unknown node → returns an error the CLI maps to exit code 2.
func validateComposeNodeInSpec(spec *compose.ComposeSpec, nodeName string) error {
	if spec == nil {
		return fmt.Errorf("ErrInvalid: compose spec is nil")
	}
	if nodeName == "" {
		return fmt.Errorf("ErrInvalid: --node is required")
	}
	if _, ok := spec.Agents[nodeName]; !ok {
		return fmt.Errorf("ErrInvalid: node %q not declared in compose spec", nodeName)
	}
	return nil
}

// findResumableComposeProc scans procs for the most recent Dead or Zombie
// process whose ComposeNode == nodeName, returning it and true. If none match,
// it returns the zero ProcInfo and false (CLI maps to ErrNotFound, exit 1 —
// AC#4).
func findResumableComposeProc(procs []vfs.ProcInfo, nodeName string) (vfs.ProcInfo, bool) {
	var candidates []vfs.ProcInfo
	for _, p := range procs {
		if p.ComposeNode != nodeName {
			continue
		}
		if p.State != types.StateDead && p.State != types.StateZombie {
			continue
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return vfs.ProcInfo{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})
	return candidates[0], true
}

// isComposeProcIdempotent reports whether a proc is a "nothing to resume"
// success case — Zombie state with an empty ExitReason. AC#5.
func isComposeProcIdempotent(p vfs.ProcInfo) bool {
	return p.State == types.StateZombie && p.ExitReason == ""
}

// buildHistoricalUpstream constructs a map[string]HistoricalNodeResult for the
// resumed node's transitive upstream dependencies, sourcing from procs. Keys
// are compose node names; values are the latest successful Zombie proc's
// persisted Result/TokensUsed/PID.
//
// Returns an error if any required upstream has no successful run (AC#6).
func buildHistoricalUpstream(spec *compose.ComposeSpec, procs []vfs.ProcInfo, resumedNode string) (map[string]compose.HistoricalNodeResult, error) {
	dag, err := compose.BuildDAG(spec)
	if err != nil {
		return nil, fmt.Errorf("build DAG: %w", err)
	}
	required := compose.TransitiveUpstream(dag, resumedNode)

	out := make(map[string]compose.HistoricalNodeResult, len(required))
	for name := range required {
		var best *vfs.ProcInfo
		for i := range procs {
			p := procs[i]
			if p.ComposeNode != name {
				continue
			}
			if p.State != types.StateZombie || p.ExitReason != "" {
				continue
			}
			if best == nil || p.CreatedAt.After(best.CreatedAt) {
				best = &procs[i]
			}
		}
		if best == nil {
			return nil, fmt.Errorf("ErrInvalid: upstream node %q has no successful run, cannot resume from %q", name, resumedNode)
		}
		out[name] = compose.HistoricalNodeResult{
			PID:      best.PID,
			Output:   best.Result,
			Tokens:   best.TokensUsed,
			ExitCode: 0,
		}
	}
	return out, nil
}

// downstreamComposeNodes returns the node names that transitively depend on
// resumedNode (used by --dry-run for plan rendering).
func downstreamComposeNodes(spec *compose.ComposeSpec, resumedNode string) []string {
	dag, err := compose.BuildDAG(spec)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var walk func(name string)
	walk = func(name string) {
		node, ok := dag.Nodes[name]
		if !ok {
			return
		}
		for _, child := range node.DependedBy {
			if _, ok := seen[child]; ok {
				continue
			}
			seen[child] = struct{}{}
			walk(child)
		}
	}
	walk(resumedNode)
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// renderComposeResumePlan prints a human-readable preview of the resume plan
// when --dry-run is set (AC#8). It does NOT call any IPC method.
func renderComposeResumePlan(w io.Writer, nodeName string, targetUUID string, downstream []string) {
	if w == nil {
		return
	}
	if len(downstream) == 0 {
		fmt.Fprintf(w, "Would resume node %q (UUID %s) — no downstream nodes to trigger.\n", nodeName, targetUUID)
		return
	}
	fmt.Fprintf(w, "Would resume node %q (UUID %s) and trigger downstream: %s\n",
		nodeName, targetUUID, strings.Join(downstream, ", "))
}

// renderComposeResumeText prints the human-readable summary of a completed
// resume operation: resumed node + downstream results.
func renderComposeResumeText(w io.Writer, resumedNode string, resumedPID types.PID, results []compose.ScheduleResult) {
	if w == nil {
		return
	}
	prefix := ui.KernelStyle.Render("[compose-resume]")
	fmt.Fprintf(w, "%s resumed node %q (PID %d)\n", prefix, resumedNode, resumedPID)
	for _, r := range results {
		if r.Name == resumedNode {
			continue
		}
		status := "done"
		if r.Err != nil {
			status = "failed"
		} else if r.ExitCode != 0 {
			status = fmt.Sprintf("exit %d", r.ExitCode)
		}
		fmt.Fprintf(w, "  - %s: %s (tokens=%d)\n", r.Name, status, r.TokensUsed)
	}
}

// renderComposeResumeJSON prints the structured JSON output for --json mode.
// Schema (AC#9):
//
//	{"ok": true, "resumed_node": "node-B", "resumed_uuid": "...",
//	 "downstream": [{"name":"node-C", "exit_code":0, "tokens":N}, ...]}
//
// The same dsEntry schema is reused by the idempotent and dry-run paths so
// consumers can parse a single shape regardless of which branch ran.
func renderComposeResumeJSON(w io.Writer, resumedNode string, resumedUUID string, results []compose.ScheduleResult) {
	if w == nil {
		return
	}
	allOK := true
	downstream := make([]dsEntry, 0, len(results))
	for _, r := range results {
		if r.Name == resumedNode {
			continue
		}
		if r.Err != nil || r.ExitCode != 0 {
			allOK = false
		}
		downstream = append(downstream, dsEntry{
			Name:     r.Name,
			ExitCode: r.ExitCode,
			Tokens:   r.TokensUsed,
		})
	}
	payload := map[string]any{
		"ok":           allOK,
		"resumed_node": resumedNode,
		"resumed_uuid": resumedUUID,
		"downstream":   downstream,
	}
	data, _ := json.Marshal(payload)
	fmt.Fprintln(w, string(data))
}

// dsEntry is the JSON shape for one downstream node entry. Shared by the
// resumed / dry-run / idempotent renderers so all three branches emit the same
// downstream schema. `Planned` is only set in dry-run mode.
type dsEntry struct {
	Name     string `json:"name"`
	ExitCode int    `json:"exit_code"`
	Tokens   int    `json:"tokens"`
	Planned  bool   `json:"planned,omitempty"`
}
