package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"sync/atomic"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gonewx/crux/agents"
	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/debug"
	"github.com/gonewx/crux/drivers/fs"
	"github.com/gonewx/crux/drivers/llm"
	"github.com/gonewx/crux/drivers/shell"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/ipc"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/skills"
	"github.com/gonewx/crux/vfs"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

// Global flags
var (
	flagJSON     bool
	flagVerbose  bool
	flagQuiet    bool
	flagModel    string
	flagMaxSteps int
	flagAgent    string
)

// exitCode is set by runRoot and read by main() to determine the process exit code.
var exitCode int

// forceExitFunc is called on double-SIGINT for force exit. Package-level variable for test injection.
var forceExitFunc = os.Exit

// claudeVersionChecker returns the Claude Code CLI version string, or an error if not available.
// Package-level variable to allow test injection.
var claudeVersionChecker = defaultClaudeVersionChecker

func defaultClaudeVersionChecker() (string, error) {
	out, err := exec.Command("claude", "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// JSONResponse is the standard JSON output wrapper.
type JSONResponse struct {
	OK    bool `json:"ok"`
	Data  any  `json:"data,omitempty"`
	Error any  `json:"error,omitempty"`
}

// jsonSuccessData holds success response fields.
type jsonSuccessData struct {
	PID        types.PID `json:"pid"`
	Result     string    `json:"result"`
	TokensUsed int       `json:"tokens_used"`
	ElapsedMs  int64     `json:"elapsed_ms"`
	ExitCode   int       `json:"exit_code"`
}

// jsonErrorData holds error response fields.
type jsonErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Syscall string `json:"syscall,omitempty"`
	Device  string `json:"device,omitempty"`
}

// cliCallbacks implements kernel.KernelCallbacks, forwarding events to the ProgressReporter.
type cliCallbacks struct {
	progress *ui.ProgressReporter
}

func (c *cliCallbacks) OnSpawn(pid types.PID, intent string) {
	c.progress.KernelMessage("spawning PID %d...", pid)
}

func (c *cliCallbacks) OnStep(pid types.PID, step, total int) {
	c.progress.AgentStep(pid, step, total)
}

func (c *cliCallbacks) OnComplete(pid types.PID, result string, exit kernel.ExitStatus) {
	// Output handled by main flow after Done channel
}

func (c *cliCallbacks) OnError(pid types.PID, err error) {
	// Output handled by main flow after Done channel
}

var rootCmd = &cobra.Command{
	Use:   "crux [intent]",
	Short: "Crux — Agent OS for AI agents",
	Long:  "Crux is an operating system for AI agents. Pass an intent string to spawn an agent.",
	Example: `  crux "分析 ./README.md"
  crux "重构 main.go 中的错误处理"
  crux version
  crux --json "分析项目结构"`,
	Args: cobra.ArbitraryArgs,
	RunE: runRoot,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version and dependencies",
	Run:   runVersion,
}

var astraceCmd = &cobra.Command{
	Use:   "astrace <pid>",
	Short: "Trace syscalls of an agent process in real-time",
	Long:  "Attach to a running agent process and stream its syscall events in real-time.\n\nPress Ctrl+C to detach without affecting the traced process.",
	Example: `  crux astrace 1              Trace PID 1 (default mode)
  crux astrace 1 --verbose    Show full syscall details
  crux astrace 1 --json       Output as JSON stream`,
	Args: cobra.ExactArgs(1),
	RunE: runAstrace,
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List active processes",
	Long:  "Display a table of all agent processes with their status, skills, tokens, and elapsed time.",
	Example: `  crux ps              # Show process table
  crux ps --json       # JSON output for scripting
  crux ps --quiet      # PIDs only (one per line)
  crux ps --verbose    # Full details including PPID and intent`,
	Args: cobra.NoArgs,
	RunE: runPs,
}

var killCmd = &cobra.Command{
	Use:   "kill <pid>",
	Short: "Terminate an agent process",
	Args:  cobra.ExactArgs(1),
	RunE:  runKill,
}

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Hidden: true,
	RunE:   runDaemon,
}

var flagDaemonInternal bool

func runVersion(cmd *cobra.Command, args []string) {
	w := cmd.OutOrStdout()

	claudeVersion, err := claudeVersionChecker()
	claudeAvailable := err == nil

	if flagJSON {
		data := map[string]any{
			"version":              version,
			"claude_code_available": claudeAvailable,
		}
		if claudeAvailable {
			data["claude_code"] = claudeVersion
		}
		resp := JSONResponse{OK: true, Data: data}
		out, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(out))
		return
	}

	fmt.Fprintf(w, "crux v%s\n", version)
	if !claudeAvailable {
		fmt.Fprintln(w, "✗ claude-code CLI not found")
		fmt.Fprintln(w, "  → 建议: npm install -g @anthropic-ai/claude-code")
		return
	}
	fmt.Fprintf(w, "claude-code: %s\n", claudeVersion)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Quiet output")
	rootCmd.Flags().StringVarP(&flagModel, "model", "m", "", "LLM model to use (e.g. sonnet, opus, haiku)")
	rootCmd.Flags().IntVar(&flagMaxSteps, "max-steps", 0, "Max reasoning steps (default 10)")
	rootCmd.Flags().StringVar(&flagAgent, "agent", "", "Agent definition to use (e.g., code-analyst)")
	daemonCmd.Flags().BoolVar(&flagDaemonInternal, "internal", false, "Internal flag (not for user use)")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(astraceCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(daemonCmd)
}

func resolveOutputMode() ui.OutputMode {
	if flagJSON {
		return ui.ModeJSON
	}
	if flagQuiet {
		return ui.ModeQuiet
	}
	if flagVerbose {
		return ui.ModeVerbose
	}
	return ui.ModeDefault
}

func runRoot(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	intent := strings.Join(args, " ")
	mode := resolveOutputMode()

	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)

	start := time.Now()

	client, err := ipc.EnsureDaemon()
	if err != nil {
		outputError(renderer, mode, "daemon", err.Error(), "daemon 启动失败", "检查 crux 是否正确安装")
		exitCode = 1
		return nil
	}
	defer client.Close()

	req := ipc.SpawnRequest{
		Intent:   intent,
		Agent:    flagAgent,
		Model:    flagModel,
		MaxSteps: flagMaxSteps,
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var spawnedPID atomic.Uint64
	cancelClient, _ := ipc.Dial(ipc.SocketPath())
	if cancelClient != nil {
		defer cancelClient.Close()
	}

	go func() {
		<-sigCh
		progress.KernelMessage("interrupted (SIGINT)")
		if pid := types.PID(spawnedPID.Load()); pid > 0 && cancelClient != nil {
			_ = cancelClient.Kill(pid, types.SIGTERM)
		}
		select {
		case <-sigCh:
			forceExitFunc(130)
		case <-time.After(2 * time.Second):
		}
	}()

	pid, final, spawnErr := client.SpawnAndWatch(req, func(ev ipc.StreamEvent) {
		if ev.Type != ipc.StreamProgress {
			return
		}
		var pp ipc.ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			return
		}
		switch pp.Event {
		case "spawn":
			progress.KernelMessage("spawning PID %d...", pp.PID)
		case "step":
			progress.AgentStep(pp.PID, pp.Step, pp.Total)
		case "error":
			// handled in final
		}
	})
	spawnedPID.Store(uint64(pid))

	if spawnErr != nil {
		outputError(renderer, mode, "/dev/llm/claude", spawnErr.Error(), "智能体启动失败", "检查 Claude Code CLI 是否已安装")
		exitCode = 1
		return nil
	}

	elapsed := time.Since(start)

	if final != nil && final.ExitCode == 0 {
		outputSuccess(renderer, mode, pid, final.Result, final.TokensUsed, elapsed)
	} else {
		reason := "unknown error"
		if final != nil {
			reason = final.ExitReason
			if final.ErrorMessage != "" {
				reason = final.ErrorMessage
			}
		}
		outputError(renderer, mode, "/dev/llm/claude", reason, "智能体执行失败", "检查意图描述或重试")
		tokensUsed := 0
		if final != nil {
			tokensUsed = final.TokensUsed
		}
		ui.RenderSummary(renderer, pid, 1, tokensUsed, elapsed)
		exitCode = 1
	}

	return nil
}

func outputSuccess(renderer *ui.Renderer, mode ui.OutputMode, pid types.PID, result string, tokensUsed int, elapsed time.Duration) {
	if mode == ui.ModeJSON {
		resp := JSONResponse{
			OK: true,
			Data: jsonSuccessData{
				PID:        pid,
				Result:     result,
				TokensUsed: tokensUsed,
				ElapsedMs:  elapsed.Milliseconds(),
				ExitCode:   0,
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(renderer.Writer, string(data))
		return
	}

	ui.RenderResult(renderer, "Result", result)
	ui.RenderSummary(renderer, pid, 0, tokensUsed, elapsed)
}

func outputError(renderer *ui.Renderer, mode ui.OutputMode, device string, reason string, impact string, suggestion string) {
	if mode == ui.ModeJSON {
		resp := JSONResponse{
			OK: false,
			Error: jsonErrorData{
				Code:    "ERROR",
				Message: reason,
				Syscall: "",
				Device:  device,
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(renderer.Writer, string(data))
		return
	}

	ui.RenderError(renderer, device, reason, impact, suggestion)
}

func runPs(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if mode == ui.ModeJSON {
			resp := JSONResponse{OK: true, Data: map[string]any{"processes": []any{}}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(renderer.Writer, string(data))
		} else if mode != ui.ModeQuiet {
			fmt.Fprintln(renderer.Writer, "No active processes.")
		}
		return nil
	}
	defer client.Close()

	procs, err := client.ListProcs()
	if err != nil {
		if mode != ui.ModeQuiet {
			fmt.Fprintf(os.Stderr, "✗ failed to list processes: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	sort.Slice(procs, func(i, j int) bool {
		return procs[i].PID < procs[j].PID
	})

	switch mode {
	case ui.ModeJSON:
		renderPsJSON(renderer, procs)
	case ui.ModeQuiet:
		renderPsQuiet(renderer, procs)
	case ui.ModeVerbose:
		ui.RenderProcessTable(renderer, procs, true)
	default:
		ui.RenderProcessTable(renderer, procs, false)
	}

	return nil
}

// jsonProcess is the JSON representation of a single process for crux ps --json.
type jsonProcess struct {
	PID        types.PID `json:"pid"`
	PPID       types.PID `json:"ppid"`
	State      string    `json:"state"`
	Intent     string    `json:"intent"`
	Skills     []string  `json:"skills"`
	TokensUsed int       `json:"tokens_used"`
	ElapsedMs  int64     `json:"elapsed_ms"`
}

func renderPsJSON(r *ui.Renderer, procs []vfs.ProcInfo) {
	now := time.Now()
	entries := make([]jsonProcess, len(procs))
	for i, p := range procs {
		skills := p.Skills
		if skills == nil {
			skills = []string{}
		}
		entries[i] = jsonProcess{
			PID:        p.PID,
			PPID:       p.PPID,
			State:      p.State.String(),
			Intent:     p.Intent,
			Skills:     skills,
			TokensUsed: p.TokensUsed,
			ElapsedMs:  now.Sub(p.CreatedAt).Milliseconds(),
		}
	}
	resp := JSONResponse{
		OK:   true,
		Data: map[string]any{"processes": entries},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ json marshal failed: %v\n", err)
		return
	}
	fmt.Fprintln(r.Writer, string(data))
}

func renderPsQuiet(r *ui.Renderer, procs []vfs.ProcInfo) {
	for _, p := range procs {
		fmt.Fprintln(r.Writer, p.PID)
	}
}

func runKill(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer,
			fmt.Sprintf("PID %s", args[0]),
			"invalid PID (expected number)",
			fmt.Sprintf("PID %s: not a valid process ID", args[0]),
			"crux ps  查看活跃进程")
		exitCode = 1
		return nil
	}
	pid := types.PID(pidNum)

	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		ui.RenderError(renderer,
			fmt.Sprintf("PID %d", pid),
			"no active daemon (process not found)",
			fmt.Sprintf("PID %d: no active process", pid),
			"crux ps  查看活跃进程")
		exitCode = 1
		return nil
	}
	defer client.Close()

	if err := client.Kill(pid, types.SIGTERM); err != nil {
		reason := "process not found"
		impact := fmt.Sprintf("PID %d: no active process", pid)
		if !strings.Contains(err.Error(), "NOT_FOUND") {
			reason = err.Error()
			impact = fmt.Sprintf("PID %d: kill failed", pid)
		}
		ui.RenderError(renderer,
			fmt.Sprintf("PID %d", pid),
			reason,
			impact,
			"crux ps  查看活跃进程")
		exitCode = 1
		return nil
	}

	prefix := ui.KernelStyle.Render("[kernel]")
	fmt.Fprintf(renderer.Writer, "%s PID %d: signal sent (SIGTERM)\n", prefix, pid)
	return nil
}

func runAstrace(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("✗ crux astrace %s: invalid PID (expected number)", args[0])
	}
	pid := types.PID(pidNum)

	w := cmd.OutOrStdout()
	mode := resolveOutputMode()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		renderer := ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			"no active daemon (process not found)", "",
			"crux ps  查看活跃进程")
		return nil
	}
	defer client.Close()

	if !flagJSON {
		fmt.Fprintf(w, "[astrace] attached to PID %d\n", pid)
	}

	astraceCtx, astraceCancel := context.WithCancel(cmd.Context())
	defer astraceCancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		astraceCancel()
	}()

	var opts debug.Options
	if !flagJSON {
		renderer := ui.NewRenderer(w, resolveOutputMode())
		ui.InitStyles(renderer.Profile)
		opts.Formatter = func(event types.SyscallEvent) string {
			return ui.FormatTraceLine(renderer, event, flagVerbose)
		}
	}
	opts.Verbose = flagVerbose
	opts.JSON = flagJSON

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.AttachDebug(pid, func(sew ipc.SyscallEventWire) {
			select {
			case <-astraceCtx.Done():
				return
			default:
			}

			event := wireToSyscallEvent(sew)

			if flagJSON {
				data, _ := json.Marshal(sew)
				fmt.Fprintln(w, string(data))
			} else if opts.Formatter != nil {
				fmt.Fprintln(w, opts.Formatter(event))
			} else {
				fmt.Fprintln(w, debug.FormatEvent(event, opts))
			}
		})
	}()

	select {
	case err := <-errCh:
		if !flagJSON {
			if err == nil {
				fmt.Fprintf(w, "[astrace] detached from PID %d (process exited)\n", pid)
			} else {
				fmt.Fprintf(w, "[astrace] detached from PID %d (error: %v)\n", pid, err)
			}
		}
	case <-astraceCtx.Done():
		if !flagJSON {
			fmt.Fprintf(w, "\n[astrace] detached from PID %d (interrupted)\n", pid)
		}
	}

	return nil
}

func wireToSyscallEvent(sew ipc.SyscallEventWire) types.SyscallEvent {
	e := types.SyscallEvent{
		Timestamp: time.Duration(sew.TimestampMs) * time.Millisecond,
		PID:       sew.PID,
		Syscall:   sew.Syscall,
		Args:      sew.Args,
		Result:    sew.Result,
		Duration:  time.Duration(sew.DurationMs * float64(time.Millisecond)),
	}
	if sew.Error != "" {
		e.Err = errors.New(sew.Error)
	}
	return e
}

func runDaemon(cmd *cobra.Command, args []string) error {
	if !flagDaemonInternal {
		return fmt.Errorf("daemon command is for internal use only; use 'crux \"intent\"' to run agents")
	}

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	claudeDriver := llm.NewClaudeCliDriver()
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
	_ = devReg.Register("/dev/fs", fs.FileFactory())
	shellDriver := shell.NewDriver()
	_ = devReg.Register("/dev/shell", shell.FileFactory(shellDriver, "/dev/shell"))
	ctxMgr := cruxctx.NewManager()
	skillLoader := skills.NewSkillLoader("lib/skills")
	agentLoader := agents.NewAgentLoader("lib/agents", skillLoader)

	srv := ipc.NewServer(nil, agentLoader.Load, version)
	k := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.SetKernel(k)

	procFS := vfs.NewProcFS(k, ctxMgr)
	_ = devReg.Register("/proc", procFS.FileFactory())

	socketPath := ipc.SocketPath()
	if err := srv.ListenAndServe(socketPath); err != nil {
		return fmt.Errorf("daemon: listen failed: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		srv.Shutdown()
	case <-srv.Done():
	}

	srv.Wait()
	k.Shutdown()
	os.Remove(socketPath)

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(2)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
