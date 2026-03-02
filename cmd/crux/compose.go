package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gonewx/crux/agents"
	"github.com/gonewx/crux/compose"
	"github.com/gonewx/crux/drivers/mcp"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/internal/xsync"
	"github.com/gonewx/crux/ipc"
	"github.com/gonewx/crux/skills"
	"github.com/gonewx/crux/vfs"
	"github.com/spf13/cobra"
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Multi-agent orchestration",
	Long:  "Manage multi-agent workflows defined in crux-compose.yaml.",
}

var composeUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Start all agents defined in compose file",
	Long:  "Parse crux-compose.yaml, resolve dependencies, and spawn all agents in DAG order.",
	Example: `  crux compose up                      # Use crux-compose.yaml in current directory
  crux compose up -f my-workflow.yaml   # Use specified file
  crux compose up --json                # JSON output mode`,
	RunE: runComposeUp,
}

var flagComposeFile string

var composeDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop all agents defined in compose file",
	Long:  "Stop all running agents from the compose orchestration and release resources.",
	Example: `  crux compose down                      # Stop agents from crux-compose.yaml
  crux compose down -f my-workflow.yaml   # Stop agents from specified file
  crux compose down --json                # JSON output mode`,
	RunE: runComposeDown,
}

var flagComposeDownFile string

func init() {
	composeUpCmd.Flags().StringVarP(&flagComposeFile, "file", "f", "crux-compose.yaml", "Compose file path")
	composeCmd.AddCommand(composeUpCmd)
	composeDownCmd.Flags().StringVarP(&flagComposeDownFile, "file", "f", "crux-compose.yaml", "Compose file path")
	composeCmd.AddCommand(composeDownCmd)
}

// ipcKernelSpawner adapts IPC Client to compose.KernelSpawner interface.
type ipcKernelSpawner struct {
	socketPath string
	waitChans  *xsync.SyncMap[types.PID, chan waitResult]
	results    *xsync.SyncMap[types.PID, string]
	tokens     *xsync.SyncMap[types.PID, int]
}

type waitResult struct {
	status compose.ComposeExitStatus
	err    error
}

func newIPCKernelSpawner(socketPath string) *ipcKernelSpawner {
	return &ipcKernelSpawner{
		socketPath: socketPath,
		waitChans:  xsync.NewSyncMap[types.PID, chan waitResult](),
		results:    xsync.NewSyncMap[types.PID, string](),
		tokens:     xsync.NewSyncMap[types.PID, int](),
	}
}

func (s *ipcKernelSpawner) Spawn(intent string, agent *agents.AgentInfo, opts compose.ComposeSpawnOpts) (types.PID, error) {
	client, err := ipc.Dial(s.socketPath)
	if err != nil {
		return 0, fmt.Errorf("compose: ipc dial: %w", err)
	}

	req := ipc.SpawnRequest{
		Intent: intent,
		Model:  opts.Model,
	}
	if agent != nil {
		req.Agent = agent.Manifest.Name
	}

	waitCh := make(chan waitResult, 1)

	pidCh := make(chan types.PID, 1)
	errCh := make(chan error, 1)

	go func() {
		defer client.Close()
		pid, final, spawnErr := client.SpawnAndWatch(req, func(ev ipc.StreamEvent) {
			// Progress events could be forwarded to UI here
		})
		if spawnErr != nil {
			errCh <- spawnErr
			return
		}
		pidCh <- pid

		status := compose.ComposeExitStatus{}
		if final != nil {
			status.Code = final.ExitCode
			status.Reason = final.ExitReason
			if final.ErrorMessage != "" {
				status.Reason = final.ErrorMessage
			}
			if final.Result != "" {
				s.results.Store(pid, final.Result)
			}
			if final.TokensUsed > 0 {
				s.tokens.Store(pid, final.TokensUsed)
			}
		}
		waitCh <- waitResult{status: status}
	}()

	select {
	case pid := <-pidCh:
		s.waitChans.Store(pid, waitCh)
		return pid, nil
	case err := <-errCh:
		return 0, err
	}
}

func (s *ipcKernelSpawner) Wait(pid types.PID) (compose.ComposeExitStatus, error) {
	ch, ok := s.waitChans.Load(pid)
	if !ok {
		return compose.ComposeExitStatus{}, fmt.Errorf("compose: no wait channel for PID %d", pid)
	}

	wr := <-ch

	// Clean up
	s.waitChans.Delete(pid)

	return wr.status, wr.err
}

func (s *ipcKernelSpawner) GetProcessResult(pid types.PID) (string, bool) {
	return s.results.Load(pid)
}

// runComposeUp implements the `crux compose up` command.
func runComposeUp(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	// 1. Parse compose file
	spec, err := compose.ParseFile(flagComposeFile)
	if err != nil {
		outputError(renderer, mode, "compose", err.Error(),
			"compose file parse failed", "check crux-compose.yaml syntax")
		exitCode = 2
		return nil
	}

	// 2. Connect to daemon
	daemonClient, err := ipc.EnsureDaemon()
	if err != nil {
		outputError(renderer, mode, "daemon", err.Error(),
			"daemon startup failed", "check if crux is installed correctly")
		exitCode = 2
		return nil
	}
	defer daemonClient.Close()

	socketPath := ipc.SocketPath()

	// 3. Create IPC KernelSpawner adapter
	spawner := newIPCKernelSpawner(socketPath)

	// 4. Create AgentLoaderFunc (local loading)
	skillLoader := skills.NewSkillLoader("lib/skills")

	// Load global MCP configuration (consistent with daemon behavior)
	var mcpCfg *mcp.MCPGlobalConfig
	if _, err := os.Stat("mcp.yaml"); err == nil {
		mcpCfg, err = mcp.LoadMCPConfig("mcp.yaml")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[compose] warn: failed to load mcp.yaml: %v\n", err)
		}
	}

	agentLoader := agents.NewAgentLoader("lib/agents", skillLoader, mcpCfg)
	agentLoaderFunc := compose.AgentLoaderFunc(agentLoader.Load)

	// 5. Create Engine
	engine, err := compose.NewEngine(spec, spawner, agentLoaderFunc)
	if err != nil {
		outputError(renderer, mode, "compose", err.Error(),
			"compose engine creation failed", "check crux-compose.yaml for circular dependencies")
		exitCode = 2
		return nil
	}

	// 6. Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		if mode != ui.ModeJSON && mode != ui.ModeQuiet {
			prefix := ui.KernelStyle.Render("[compose]")
			fmt.Fprintf(renderer.Writer, "%s interrupted (SIGINT), cancelling...\n", prefix)
		}
		cancel()

		// Second SIGINT: force exit
		select {
		case <-sigCh:
			forceExitFunc(130)
		case <-time.After(5 * time.Second):
		}
	}()

	// 7. Output header
	ui.RenderComposeHeader(renderer, spec.Intent)

	// 8. Execute orchestration
	results, execErr := engine.Execute(ctx)

	// 9. Output summary
	if mode == ui.ModeJSON {
		renderComposeJSON(renderer, results, execErr, spawner)
	} else {
		renderComposeResults(renderer, results, spawner)
	}

	// 10. Set exit code
	if execErr != nil {
		exitCode = 1
		return nil
	}
	for _, r := range results {
		if r.Err != nil {
			exitCode = 1
			return nil
		}
	}

	return nil
}

// renderComposeResults outputs per-agent results and the summary table.
func renderComposeResults(r *ui.Renderer, results []compose.ScheduleResult, spawner *ipcKernelSpawner) {
	total := len(results)
	for i, res := range results {
		idx := i + 1
		if res.Err != nil {
			if res.PID == 0 {
				ui.RenderComposeProgress(r, res.Name, "skipped", idx, total, -1)
			} else {
				ui.RenderComposeProgress(r, res.Name, "failed", idx, total, res.ExitCode)
			}
		} else {
			ui.RenderComposeProgress(r, res.Name, "done", idx, total, res.ExitCode)
		}
	}

	// Enrich results with token data from the IPC spawner
	tokenMap := make(map[string]int)
	if spawner != nil {
		for _, res := range results {
			if res.PID > 0 {
				if tokens, ok := spawner.tokens.Load(res.PID); ok {
					tokenMap[res.Name] = tokens
				}
			}
		}
	}

	ui.RenderComposeSummary(r, results, tokenMap)
}

// renderComposeJSON outputs the compose results as JSON.
func renderComposeJSON(r *ui.Renderer, results []compose.ScheduleResult, execErr error, spawner *ipcKernelSpawner) {
	if execErr != nil && len(results) == 0 {
		resp := JSONResponse{
			OK:    false,
			Error: jsonErrorData{Code: "COMPOSE_ERROR", Message: execErr.Error()},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(r.Writer, string(data))
		return
	}

	// Collect token data from the IPC spawner
	tokenMap := make(map[string]int)
	if spawner != nil {
		for _, res := range results {
			if res.PID > 0 {
				if tokens, ok := spawner.tokens.Load(res.PID); ok {
					tokenMap[res.Name] = tokens
				}
			}
		}
	}

	ui.RenderComposeSummaryJSON(r, results, tokenMap)
}

// matchComposeProcesses filters processes that match a compose spec's agent intents.
func matchComposeProcesses(procs []vfs.ProcInfo, spec *compose.ComposeSpec) (running []vfs.ProcInfo, completed []vfs.ProcInfo) {
	// Build a set of intents from the compose spec
	intentSet := make(map[string]bool)
	for _, agent := range spec.Agents {
		intentSet[agent.Intent] = true
	}

	for _, p := range procs {
		if !intentSet[p.Intent] {
			continue
		}
		if p.State == types.StateRunning || p.State == types.StateCreated {
			running = append(running, p)
		} else {
			completed = append(completed, p)
		}
	}
	return running, completed
}

// runComposeDown implements the `crux compose down` command.
func runComposeDown(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	// 1. Parse compose file
	spec, err := compose.ParseFile(flagComposeDownFile)
	if err != nil {
		outputError(renderer, mode, "compose", err.Error(),
			"compose file parse failed", "check crux-compose.yaml syntax")
		exitCode = 2
		return nil
	}

	// 2. Connect to daemon (do NOT use EnsureDaemon — compose down should not start a daemon)
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		// Daemon not running: nothing to stop
		if mode == ui.ModeJSON {
			ui.RenderComposeDownSummaryJSON(renderer, nil, nil, nil)
		} else if mode != ui.ModeQuiet {
			prefix := ui.KernelStyle.Render("[compose]")
			fmt.Fprintf(renderer.Writer, "%s No daemon running, nothing to stop\n", prefix)
		}
		return nil
	}
	defer client.Close()

	// 3. List all daemon processes
	procs, err := client.ListProcs()
	if err != nil {
		outputError(renderer, mode, "compose", err.Error(),
			"failed to list processes", "check daemon status")
		exitCode = 1
		return nil
	}

	// 4. Match compose processes
	running, completed := matchComposeProcesses(procs, spec)

	// 5. No matching processes
	if len(running) == 0 && len(completed) == 0 {
		if mode == ui.ModeJSON {
			ui.RenderComposeDownSummaryJSON(renderer, nil, nil, nil)
		} else if mode != ui.ModeQuiet {
			prefix := ui.KernelStyle.Render("[compose]")
			fmt.Fprintf(renderer.Writer, "%s No matching processes found for compose file\n", prefix)
		}
		return nil
	}

	// 5.5 Output header
	ui.RenderComposeDownHeader(renderer, flagComposeDownFile)

	// 6. Kill running processes (best-effort)
	var killed []ui.ComposeDownEntry
	var killErrors []string
	for _, p := range running {
		killErr := client.Kill(p.PID, types.SIGTERM)
		if killErr != nil {
			killErrors = append(killErrors, fmt.Sprintf("PID %d: %v", p.PID, killErr))
		} else {
			killed = append(killed, ui.ComposeDownEntry{PID: p.PID, Intent: p.Intent})
		}
	}

	// 7. Build skipped list
	var skipped []ui.ComposeDownEntry
	for _, p := range completed {
		skipped = append(skipped, ui.ComposeDownEntry{PID: p.PID, Intent: p.Intent, State: p.State.String()})
	}

	// 8. Output summary
	if mode == ui.ModeJSON {
		ui.RenderComposeDownSummaryJSON(renderer, killed, skipped, killErrors)
	} else {
		ui.RenderComposeDownSummary(renderer, killed, skipped, killErrors)
	}

	// 9. Set exit code
	if len(killErrors) > 0 {
		exitCode = 1
	}

	return nil
}
