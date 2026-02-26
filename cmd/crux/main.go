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

// kern is the global kernel instance, initialized by runRoot or runAstrace.
var kern *kernel.KernelImpl

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
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(astraceCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(killCmd)
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
	cb := &cliCallbacks{progress: progress}

	// Dependency injection chain
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
	kern = kernel.NewKernel(vfsInst, ctxMgr, cb)

	// Register ProcFS (requires kernel as ProcessInfoProvider)
	procFS := vfs.NewProcFS(kern, ctxMgr)
	_ = devReg.Register("/proc", procFS.FileFactory())

	start := time.Now()

	var agentInfo *agents.AgentInfo
	if flagAgent != "" {
		var err error
		agentInfo, err = agentLoader.Load(flagAgent)
		if err != nil {
			outputError(renderer, mode, "agent", err.Error(), "智能体加载失败", "检查 --agent 参数")
			exitCode = 1
			return nil
		}
	}
	pid, err := kern.Spawn(intent, agentInfo, kernel.SpawnOpts{Model: flagModel, MaxTurns: flagMaxSteps})
	if err != nil {
		outputError(renderer, mode, "/dev/llm/claude", err.Error(), "智能体启动失败", "检查 Claude Code CLI 是否已安装")
		exitCode = 1
		return nil
	}

	proc, ok := kern.GetProcess(pid)
	if !ok {
		outputError(renderer, mode, "kernel", "process not found after spawn", "内部错误", "请报告此问题")
		exitCode = 1
		return nil
	}

	// Signal handling
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh // First signal
		progress.KernelMessage("PID %d interrupted (SIGINT)", pid)
		proc.Cancel()
		select {
		case <-sigCh: // Second signal within timeout
			forceExitFunc(130)
		case <-time.After(2 * time.Second):
			// Timeout elapsed, let normal exit flow handle it
		}
	}()

	// Wait for process completion
	var exit kernel.ExitStatus
	exit = <-proc.Done

	elapsed := time.Since(start)

	if exit.Code == 0 {
		outputSuccess(renderer, mode, pid, proc, elapsed)
	} else {
		reason := exit.Reason
		if exit.Err != nil {
			reason = exit.Err.Error()
		}
		outputError(renderer, mode, "/dev/llm/claude", reason, "智能体执行失败", "检查意图描述或重试")
		ui.RenderSummary(renderer, pid, exit.Code, proc.TokensUsed, elapsed)
		exitCode = 1
	}

	return nil
}

func outputSuccess(renderer *ui.Renderer, mode ui.OutputMode, pid types.PID, proc *kernel.Process, elapsed time.Duration) {
	if mode == ui.ModeJSON {
		resp := JSONResponse{
			OK: true,
			Data: jsonSuccessData{
				PID:        pid,
				Result:     proc.Result,
				TokensUsed: proc.TokensUsed,
				ElapsedMs:  elapsed.Milliseconds(),
				ExitCode:   0,
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(renderer.Writer, string(data))
		return
	}

	ui.RenderResult(renderer, "Result", proc.Result)
	ui.RenderSummary(renderer, pid, 0, proc.TokensUsed, elapsed)
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

// initKernel initializes a kernel instance for subcommands (e.g., astrace).
// TODO: astrace needs IPC to attach to a running crux instance's process table.
// Currently creates a standalone kernel, which means astrace only works within
// the same process (test injection). Device drivers registered here are unused
// by astrace but required by the kernel constructor.
func initKernel() {
	if kern != nil {
		return
	}
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	claudeDriver := llm.NewClaudeCliDriver()
	_ = devReg.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
	_ = devReg.Register("/dev/fs", fs.FileFactory())
	shellDriver := shell.NewDriver()
	_ = devReg.Register("/dev/shell", shell.FileFactory(shellDriver, "/dev/shell"))
	ctxMgr := cruxctx.NewManager()

	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	cb := &cliCallbacks{progress: progress}

	kern = kernel.NewKernel(vfsInst, ctxMgr, cb)

	// Register ProcFS (requires kernel as ProcessInfoProvider)
	procFS := vfs.NewProcFS(kern, ctxMgr)
	_ = devReg.Register("/proc", procFS.FileFactory())
}

func processStateName(s types.ProcessState) string {
	return s.String()
}

func runPs(cmd *cobra.Command, args []string) error {
	initKernel()
	if kern == nil {
		fmt.Fprintln(os.Stderr, "✗ kernel initialization failed")
		exitCode = 1
		return nil
	}

	procs := kern.ListProcs()
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].PID < procs[j].PID
	})

	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

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

	initKernel()
	if kern == nil {
		fmt.Fprintln(os.Stderr, "✗ kernel initialization failed")
		exitCode = 1
		return nil
	}

	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	if err := kern.Kill(pid, types.SIGTERM); err != nil {
		reason := "process not found"
		impact := fmt.Sprintf("PID %d: no active process", pid)
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) && sysErr.Code != types.ErrNotFound {
			reason = sysErr.Err.Error()
			impact = fmt.Sprintf("PID %d: %s", pid, sysErr.Code)
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
	// 1. Parse PID
	pidNum, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("✗ crux astrace %s: invalid PID (expected number)", args[0])
	}
	pid := types.PID(pidNum)

	// 2. Initialize kernel if needed
	initKernel()

	// 3. Find process
	proc, ok := kern.GetProcess(pid)
	if !ok {
		w := cmd.OutOrStdout()
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			"process not found", "",
			"crux ps  查看活跃进程")
		return nil
	}

	// 4. Build Options
	opts := debug.DefaultOptions()
	opts.Verbose = flagVerbose
	opts.JSON = flagJSON

	// 5. Output writer and UI formatter injection
	w := cmd.OutOrStdout()
	if !flagJSON {
		renderer := ui.NewRenderer(w, resolveOutputMode())
		ui.InitStyles(renderer.Profile)
		opts.Formatter = func(event types.SyscallEvent) string {
			return ui.FormatTraceLine(renderer, event, flagVerbose)
		}
	}

	// 6. Output attach confirmation (non-JSON mode)
	if !flagJSON {
		state := proc.GetState()
		fmt.Fprintf(w, "[astrace] attached to PID %d (state: %s)\n", pid, processStateName(state))
	}

	// 7. Set up astrace-specific context (SIGINT only detaches, never kills process)
	astraceCtx, astraceCancel := context.WithCancel(cmd.Context())
	defer astraceCancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		astraceCancel() // Only cancel astrace, do not affect traced process
	}()

	// 8. Execute Attach
	err = debug.Attach(astraceCtx, proc.DebugChan, w, opts)

	// 9. Output detach summary (non-JSON mode)
	if !flagJSON {
		if err == nil {
			fmt.Fprintf(w, "[astrace] detached from PID %d (process exited)\n", pid)
		} else if errors.Is(err, context.Canceled) {
			fmt.Fprintf(w, "\n[astrace] detached from PID %d (interrupted)\n", pid)
		} else {
			fmt.Fprintf(w, "[astrace] detached from PID %d (error: %v)\n", pid, err)
		}
	}

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
