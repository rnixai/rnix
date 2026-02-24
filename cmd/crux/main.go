package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/drivers/fs"
	"github.com/gonewx/crux/drivers/llm"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/vfs"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

// Global flags
var (
	flagJSON    bool
	flagVerbose bool
	flagQuiet   bool
	flagModel    string
	flagMaxSteps int
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
	rootCmd.AddCommand(versionCmd)
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
	ctxMgr := cruxctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

	start := time.Now()

	pid, err := kern.Spawn(intent, nil, kernel.SpawnOpts{Model: flagModel, MaxTurns: flagMaxSteps})
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(2)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
