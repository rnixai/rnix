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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/drivers/fs"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/drivers/mcp"
	drivershell "github.com/rnixai/rnix/drivers/shell"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	agentshell "github.com/rnixai/rnix/shell"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
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
	flagProvider string
	flagIntent   string
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
	Use:   "rnix [command]",
	Short: "Rnix — Agent OS for AI agents",
	Long:  "Rnix is an operating system for AI agents.\n\nUse -i flag to spawn an agent with an intent.",
	Example: `  rnix -i "分析 ./README.md"
  rnix -i "重构 main.go 中的错误处理"
  rnix version
  rnix -i "分析项目结构" --json`,
	Args: rejectPositionalArgs,
	RunE: runRoot,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version and dependencies",
	Run:   runVersion,
}

var straceCmd = &cobra.Command{
	Use:   "strace <pid>",
	Short: "Trace syscalls of an agent process in real-time",
	Long:  "Attach to a running agent process and stream its syscall events in real-time.\n\nPress Ctrl+C to detach without affecting the traced process.",
	Example: `  rnix strace 1              Trace PID 1 (default mode)
  rnix strace 1 --verbose    Show full syscall details
  rnix strace 1 --json       Output as JSON stream`,
	Args: cobra.ExactArgs(1),
	RunE: runStrace,
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List active processes",
	Long:  "Display a table of all agent processes with their status, skills, tokens, and elapsed time.",
	Example: `  rnix ps              # Show process table
  rnix ps --json       # JSON output for scripting
  rnix ps --quiet      # PIDs only (one per line)
  rnix ps --verbose    # Full details including PPID and intent`,
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
	Use:   "daemon",
	Short: "Manage the rnix background daemon",
	RunE:  runDaemon,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runDaemonStatus,
}

var flagDaemonInternal bool

func runVersion(cmd *cobra.Command, args []string) {
	w := cmd.OutOrStdout()

	claudeVersion, err := claudeVersionChecker()
	claudeAvailable := err == nil

	if flagJSON {
		data := map[string]any{
			"version":               version,
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

	fmt.Fprintf(w, "rnix v%s\n", version)
	if !claudeAvailable {
		fmt.Fprintln(w, "✗ claude-code CLI not found")
		fmt.Fprintln(w, "  → 建议: npm install -g @anthropic-ai/claude-code")
		return
	}
	fmt.Fprintf(w, "claude-code: %s\n", claudeVersion)
}

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Quiet output")
	rootCmd.PersistentFlags().StringVarP(&flagModel, "model", "m", "", "LLM model to use (e.g. sonnet, opus, haiku)")
	rootCmd.PersistentFlags().StringVar(&flagProvider, "provider", "", "LLM provider override (see rnix-providers.yaml)")
	rootCmd.Flags().IntVar(&flagMaxSteps, "max-steps", 0, "Max reasoning steps (default 10)")
	rootCmd.Flags().StringVar(&flagAgent, "agent", "", "Agent definition to use (e.g., code-analyst)")
	rootCmd.Flags().StringVarP(&flagIntent, "intent", "i", "", "Intent string to spawn an agent")
	daemonCmd.Flags().BoolVar(&flagDaemonInternal, "internal", false, "Internal flag (not for user use)")
	_ = daemonCmd.Flags().MarkHidden("internal")
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(straceCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(composeCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(topCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(gdbCmd)
	dashboardCmd.Flags().String("load", "", "Load a recording for offline replay (path or record-id)")
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(intentCmd)
	rootCmd.AddCommand(serveCmd)
}

// levenshtein computes the standard Levenshtein distance between two strings
// using a dynamic programming approach (insert/delete/replace, no transposition).
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			curr[j] = min(ins, min(del, sub))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// suggestCommand performs fuzzy matching against all registered subcommands (including hidden).
// Priority: exact match > prefix match > Levenshtein match (skipped for len(input) <= 3).
func suggestCommand(cmd *cobra.Command, input string) string {
	var prefixCandidate string
	var levCandidate string
	var levBest int

	for _, sub := range cmd.Commands() {
		name := sub.Name()

		// Exact match: return immediately
		if input == name {
			return name
		}

		// Prefix match: record the first hit (alphabetical order from cmd.Commands())
		if prefixCandidate == "" && strings.HasPrefix(name, input) {
			prefixCandidate = name
		}

		// Levenshtein match: only for longer inputs
		if len(input) > 3 {
			d := levenshtein(input, name)
			if d <= 2 && (levCandidate == "" || d < levBest) {
				levCandidate = name
				levBest = d
			}
		}
	}

	if prefixCandidate != "" {
		return prefixCandidate
	}
	return levCandidate
}

// rejectPositionalArgs is a custom cobra.Args validator that rejects all positional arguments
// with a helpful error message, optionally suggesting the closest subcommand.
func rejectPositionalArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}

	display := strings.Join(args, " ")
	suggestion := suggestCommand(cmd, args[0])

	if suggestion != "" {
		return fmt.Errorf("unknown command %q, did you mean %q?\n\n  Usage: rnix -i <intent>\n  Run 'rnix --help' for available commands.", display, suggestion) //nolint:staticcheck // user-facing CLI message requires newlines and punctuation
	}
	return fmt.Errorf("unknown command %q\n\n  Usage: rnix -i <intent>\n  Run 'rnix --help' for available commands.", display) //nolint:staticcheck // user-facing CLI message requires newlines and punctuation
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

// isPipelineSyntax returns true if intent looks like a pipeline: contains '|' with
// 'spawn' keyword on at least two sides of the pipe character.
func isPipelineSyntax(intent string) bool {
	if !strings.Contains(intent, "|") {
		return false
	}
	parts := strings.Split(intent, "|")
	spawnCount := 0
	for _, part := range parts {
		trimmed := strings.TrimSpace(strings.ToLower(part))
		if strings.HasPrefix(trimmed, "spawn") &&
			(len(trimmed) == 5 || trimmed[5] == ' ' || trimmed[5] == '\t' || trimmed[5] == '"' || trimmed[5] == '\'') {
			spawnCount++
		}
	}
	return spawnCount >= 2
}

// isScriptSyntax returns true if intent is a multi-line script, a single-line export,
// or contains an on-error handler (single-line on-error needs script execution path).
func isScriptSyntax(intent string) bool {
	if strings.Contains(intent, "\n") {
		return true
	}
	trimmed := strings.TrimSpace(strings.ToLower(intent))
	if strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "export\t") {
		return true
	}
	_, _, hasOnError := agentshell.SplitOnError(intent)
	return hasOnError
}

// containsVarRef returns true if intent contains a $VAR reference.
func containsVarRef(intent string) bool {
	for i := 0; i < len(intent); i++ {
		if intent[i] == '\\' && i+1 < len(intent) && intent[i+1] == '$' {
			i++
			continue
		}
		if intent[i] == '$' && i+1 < len(intent) && (isVarStartByte(intent[i+1]) || intent[i+1] == '{') {
			return true
		}
	}
	return false
}

func isVarStartByte(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func runRoot(cmd *cobra.Command, args []string) error {
	if flagIntent == "" {
		return cmd.Help()
	}

	intent := flagIntent
	mode := resolveOutputMode()

	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)

	start := time.Now()

	client, err := ipc.EnsureDaemon()
	if err != nil {
		outputError(renderer, mode, "daemon", err.Error(), "daemon 启动失败", "检查 rnix 是否正确安装")
		exitCode = 1
		return nil
	}
	defer client.Close()

	if isScriptSyntax(intent) {
		runScript(renderer, mode, progress, client, intent, start)
		return nil
	}

	if isPipelineSyntax(intent) {
		runPipeline(renderer, mode, progress, client, intent, start)
		return nil
	}

	// Single-line $VAR expansion from OS environment
	if containsVarRef(intent) {
		env := agentshell.NewEnvironmentFromOS()
		intent = env.Expand(intent)
	}

	req := ipc.SpawnRequest{
		Intent:   intent,
		Agent:    flagAgent,
		Model:    flagModel,
		Provider: flagProvider,
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
		outputError(renderer, mode, "/dev/llm", spawnErr.Error(), "智能体启动失败", "检查 LLM CLI 是否已安装（claude 或 agent）")
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
		outputError(renderer, mode, "/dev/llm", reason, "智能体执行失败", "检查意图描述或重试")
		tokensUsed := 0
		if final != nil {
			tokensUsed = final.TokensUsed
		}
		ui.RenderSummary(renderer, pid, 1, tokensUsed, elapsed)
		exitCode = 1
	}

	return nil
}

func runPipeline(renderer *ui.Renderer, mode ui.OutputMode, progress *ui.ProgressReporter, client *ipc.Client, intent string, start time.Time) {
	pipeline, err := agentshell.ParsePipeline(intent)
	if err != nil {
		outputError(renderer, mode, "shell/parser", err.Error(), "管道语法解析失败", "检查语法: spawn \"A\" | spawn \"B\"")
		exitCode = 1
		return
	}

	if len(pipeline.Commands) > agentshell.MaxRecommendedStages {
		progress.KernelMessage("warning: pipeline has %d stages (recommended ≤ %d)", len(pipeline.Commands), agentshell.MaxRecommendedStages)
	}

	req := ipc.SpawnPipelineRequest{
		Commands: make([]ipc.SpawnPipelineCommand, len(pipeline.Commands)),
	}
	for i, cmd := range pipeline.Commands {
		req.Commands[i] = ipc.SpawnPipelineCommand{
			Intent: cmd.Intent,
			Agent:  cmd.Agent,
			Model:  cmd.Model,
		}
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	pipelineDone := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			progress.KernelMessage("pipeline interrupted (SIGINT)")
			client.Close()
			select {
			case <-sigCh:
				forceExitFunc(130)
			case <-time.After(2 * time.Second):
			}
		case <-pipelineDone:
		}
	}()

	pipeResp, pipeErr := client.SpawnPipelineAndWatch(req, func(ev ipc.StreamEvent) {
		if ev.Type != ipc.StreamProgress {
			return
		}
		var pp ipc.ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			return
		}
		if pp.Event == "pipeline_stage" {
			progress.KernelMessage("pipeline stage %d/%d...", pp.Step, pp.Total)
		}
	})
	close(pipelineDone)

	if pipeErr != nil {
		outputError(renderer, mode, "shell/pipe", pipeErr.Error(), "管道执行失败", "检查管道命令或重试")
		exitCode = 1
		return
	}

	elapsed := time.Since(start)

	if pipeResp == nil || len(pipeResp.Stages) == 0 {
		outputError(renderer, mode, "shell/pipe", "no pipeline result", "管道返回空结果", "检查管道命令")
		exitCode = 1
		return
	}

	lastStage := pipeResp.Stages[len(pipeResp.Stages)-1]

	if mode == ui.ModeJSON {
		outputPipelineJSON(renderer, pipeResp, elapsed)
		if lastStage.ExitCode != 0 {
			exitCode = 1
		}
		return
	}

	if lastStage.ExitCode != 0 {
		failIdx := len(pipeResp.Stages)
		total := len(pipeline.Commands)
		outputError(renderer, mode, "shell/pipe",
			fmt.Sprintf("stage %d/%d failed (exit %d): %s", failIdx, total, lastStage.ExitCode, lastStage.Intent),
			"管道执行中断", "检查失败阶段的 intent")
		totalTokens := 0
		for _, s := range pipeResp.Stages {
			totalTokens += s.TokensUsed
		}
		ui.RenderSummary(renderer, lastStage.PID, lastStage.ExitCode, totalTokens, elapsed)
		exitCode = 1
		return
	}

	totalTokens := 0
	for _, s := range pipeResp.Stages {
		totalTokens += s.TokensUsed
	}
	outputSuccess(renderer, mode, lastStage.PID, lastStage.Result, totalTokens, elapsed)
}

func runScript(renderer *ui.Renderer, mode ui.OutputMode, progress *ui.ProgressReporter, client *ipc.Client, intent string, start time.Time) {
	req := ipc.ExecScriptRequest{
		Script: intent,
		Env:    agentshell.NewEnvironmentFromOS().All(),
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	scriptDone := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			progress.KernelMessage("script interrupted (SIGINT)")
			client.Close()
			select {
			case <-sigCh:
				forceExitFunc(130)
			case <-time.After(2 * time.Second):
			}
		case <-scriptDone:
		}
	}()

	scriptResp, scriptErr := client.ExecScriptAndWatch(req, func(ev ipc.StreamEvent) {
		if ev.Type != ipc.StreamProgress {
			return
		}
		var pp ipc.ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			return
		}
		if pp.Event == "script_step" {
			if pp.Intent != "" {
				progress.KernelMessage("script step %d/%d: %s", pp.Step, pp.Total, pp.Intent)
			} else {
				progress.KernelMessage("script step %d/%d...", pp.Step, pp.Total)
			}
		}
	})
	close(scriptDone)

	if scriptErr != nil {
		outputError(renderer, mode, "shell/script", scriptErr.Error(), "脚本执行失败", "检查脚本语法或重试")
		exitCode = 1
		return
	}

	elapsed := time.Since(start)

	if scriptResp == nil {
		outputError(renderer, mode, "shell/script", "no script result", "脚本返回空结果", "检查脚本内容")
		exitCode = 1
		return
	}

	if scriptResp.LastExitCode == 0 {
		outputSuccess(renderer, mode, 0, scriptResp.LastResult, scriptResp.TotalTokens, elapsed)
	} else {
		outputError(renderer, mode, "shell/script",
			fmt.Sprintf("script failed (exit %d)", scriptResp.LastExitCode),
			"脚本执行中断", "检查脚本中的 spawn 命令")
		ui.RenderSummary(renderer, 0, scriptResp.LastExitCode, scriptResp.TotalTokens, elapsed)
		exitCode = 1
	}
}

// jsonPipelineData holds pipeline JSON output fields.
type jsonPipelineData struct {
	Stages      []jsonPipelineStage `json:"stages"`
	TotalTokens int                 `json:"total_tokens"`
	ElapsedMs   int64               `json:"elapsed_ms"`
}

type jsonPipelineStage struct {
	PID        types.PID `json:"pid"`
	Intent     string    `json:"intent"`
	Result     string    `json:"result"`
	ExitCode   int       `json:"exit_code"`
	TokensUsed int       `json:"tokens_used"`
	ElapsedMs  int64     `json:"elapsed_ms"`
}

func outputPipelineJSON(renderer *ui.Renderer, resp *ipc.SpawnPipelineResponse, elapsed time.Duration) {
	stages := make([]jsonPipelineStage, len(resp.Stages))
	totalTokens := 0
	for i, s := range resp.Stages {
		stages[i] = jsonPipelineStage{
			PID:        s.PID,
			Intent:     s.Intent,
			Result:     s.Result,
			ExitCode:   s.ExitCode,
			TokensUsed: s.TokensUsed,
			ElapsedMs:  s.ElapsedMs,
		}
		totalTokens += s.TokensUsed
	}

	lastStage := resp.Stages[len(resp.Stages)-1]
	resp2 := JSONResponse{
		OK: lastStage.ExitCode == 0,
		Data: jsonPipelineData{
			Stages:      stages,
			TotalTokens: totalTokens,
			ElapsedMs:   elapsed.Milliseconds(),
		},
	}
	data, _ := json.Marshal(resp2)
	fmt.Fprintln(renderer.Writer, string(data))
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

// jsonProcess is the JSON representation of a single process for rnix ps --json.
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
			"rnix ps  查看活跃进程")
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
			"rnix ps  查看活跃进程")
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
			"rnix ps  查看活跃进程")
		exitCode = 1
		return nil
	}

	prefix := ui.KernelStyle.Render("[kernel]")
	fmt.Fprintf(renderer.Writer, "%s PID %d: signal sent (SIGTERM)\n", prefix, pid)
	return nil
}

func runStrace(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("✗ rnix strace %s: invalid PID (expected number)", args[0])
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
			"rnix ps  查看活跃进程")
		return nil
	}
	defer client.Close()

	if !flagJSON {
		fmt.Fprintf(w, "[strace] attached to PID %d\n", pid)
	}

	straceCtx, straceCancel := context.WithCancel(cmd.Context())
	defer straceCancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		straceCancel()
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
			case <-straceCtx.Done():
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
				fmt.Fprintf(w, "[strace] detached from PID %d (process exited)\n", pid)
			} else {
				fmt.Fprintf(w, "[strace] detached from PID %d (error: %v)\n", pid, err)
			}
		}
	case <-straceCtx.Done():
		if !flagJSON {
			fmt.Fprintf(w, "\n[strace] detached from PID %d (interrupted)\n", pid)
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
		TraceID:   types.TraceID(sew.TraceID),
		SpanID:    types.SpanID(sew.SpanID),
	}
	if sew.Error != "" {
		e.Err = errors.New(sew.Error)
	}
	return e
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	sockPath := ipc.SocketPath()
	client, err := ipc.Dial(sockPath)
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "daemon is not running")
		return nil
	}
	defer client.Close()

	if err := client.Shutdown(); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "daemon stopped")
	return nil
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	sockPath := ipc.SocketPath()
	client, err := ipc.Dial(sockPath)
	if err != nil {
		fmt.Fprintf(w, "status: stopped\nsocket: %s\n", sockPath)
		return nil
	}
	defer client.Close()

	version, err := client.Ping()
	if err != nil {
		fmt.Fprintf(w, "status: unreachable\nsocket: %s\nerror:  %v\n", sockPath, err)
		return nil
	}

	procs, _ := client.ListProcs()
	active := 0
	for _, p := range procs {
		if p.State == types.StateRunning {
			active++
		}
	}

	fmt.Fprintf(w, "status:  running\nversion: %s\nsocket:  %s\nprocs:   %d active / %d total\n",
		version, sockPath, active, len(procs))

	// Provider health status
	providers, err := client.ProviderStatus()
	if err == nil && len(providers) > 0 {
		fmt.Fprintf(w, "providers:\n")
		for _, p := range providers {
			fmt.Fprintf(w, "  %-12s %s (%s)\n", p.Name, p.Health, p.Driver)
		}
	}

	return nil
}

func runDaemon(cmd *cobra.Command, args []string) error {
	if !flagDaemonInternal {
		return cmd.Help()
	}

	providersCfg, err := llm.LoadOrDefaultProvidersConfig()
	if err != nil {
		return fmt.Errorf("loading providers config: %w", err)
	}
	driverReg := llm.NewDriverRegistry()
	devReg := vfs.NewDeviceRegistry()
	if err := llm.RegisterProviders(providersCfg, driverReg, devReg); err != nil {
		return fmt.Errorf("registering LLM providers: %w", err)
	}
	llm.RunHealthChecks(providersCfg, driverReg, 3*time.Second)
	vfsInst := vfs.NewVFS(devReg)
	_ = devReg.Register("/dev/fs", fs.FileFactory())
	shellDriver := drivershell.NewDriver()
	_ = devReg.Register("/dev/shell", drivershell.FileFactory(shellDriver, "/dev/shell"))
	ctxMgr := rnixctx.NewManager()
	skillLoader := skills.NewSkillLoader("lib/skills")

	// Load global MCP configuration (optional, mcp.yaml may not exist)
	var mcpCfg *mcp.MCPGlobalConfig
	if _, err := os.Stat("mcp.yaml"); err == nil {
		mcpCfg, err = mcp.LoadMCPConfig("mcp.yaml")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kernel] warn: failed to load mcp.yaml: %v\n", err)
		}
	}

	agentLoader := agents.NewAgentLoader("lib/agents", skillLoader, mcpCfg)

	// Create MountManager with TransportFactory for MCP server mounts
	transportFactory := func(config vfs.MCPConfig) (vfs.MCPTransport, error) {
		tc := mcp.TransportConfig{
			Command: config.Command,
			Args:    config.Args,
		}
		for k, v := range config.Env {
			tc.Env = append(tc.Env, k+"="+v)
		}
		return mcp.NewStdioTransport(tc), nil
	}
	mountMgr := vfs.NewMountManager(devReg, transportFactory)

	srv := ipc.NewServer(nil, agentLoader.Load, version)
	k := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	k.SetMountManager(mountMgr)
	k.SetProviderResolver(driverReg.Names, func(name string) bool { _, ok := driverReg.Get(name); return ok })
	k.SetDefaultProvider(providersCfg.ResolveDefaultProvider())
	k.SetAgentLoader(agentLoader.Load) // Inject for OODA autonomous spawn (Story 20.2)

	// Stem agent differentiation (Story 20.3)
	discovery := skills.NewSkillDiscovery(skillLoader, "lib/skills")
	stemMatcher := kernel.NewStemMatcher(discovery)
	k.SetStemMatcher(stemMatcher)
	k.SetSkillLoader(skillLoader.LoadFull)

	// Differentiation memory (Story 20.4)
	diffMemory := kernel.NewDiffMemory(256)
	k.SetDiffMemory(diffMemory)

	// Initialize execution recording (Story 14.1)
	cwd, _ := os.Getwd()
	recordBaseDir := cwd + "/.rnix/records"
	recordMgr := debug.NewRecordManager(recordBaseDir)
	k.SetRecordManager(recordMgr)

	// Initialize span persistence (Story 15.1)
	traceBaseDir := cwd + "/.rnix/traces"
	k.SetSpanWriter(debug.NewSpanWriter(traceBaseDir))

	srv.SetKernel(k)
	srv.SetContextManager(ctxMgr)
	srv.SetSkillLoader(skillLoader)
	srv.SetProviderStatusFunc(func() []ipc.ProviderStatusWire {
		statuses := driverReg.HealthStatuses()
		wires := make([]ipc.ProviderStatusWire, len(statuses))
		for i, ps := range statuses {
			wires[i] = ipc.ProviderStatusWire{
				Name:   ps.Name,
				Driver: ps.Driver,
				Health: string(ps.Health),
			}
		}
		return wires
	})

	// Intent manager initialization (Story 19.1)
	intentDecomposer := intent.NewDecomposer(&intent.CLICaller{})
	intentSpawner := &ipc.IntentKernelSpawner{
		SpawnFunc: func(ctx context.Context, node *intent.IntentNode) (types.PID, error) {
			agentInfo, _ := agentLoader.Load(node.Agent)
			pid, err := k.Spawn(node.Intent, agentInfo, kernel.SpawnOpts{Model: node.Model, Provider: node.Provider})
			return pid, err
		},
		WaitFunc: func(pid types.PID) (intent.ExitStatus, error) {
			proc, ok := k.GetProcess(pid)
			if !ok {
				return intent.ExitStatus{Code: 1, Reason: "process not found"}, fmt.Errorf("process %d not found", pid)
			}
			es := <-proc.Done
			return intent.ExitStatus{Code: es.Code, Reason: es.Reason, Err: es.Err}, nil
		},
	}
	intentMgr := intent.NewManager(intentDecomposer, intentSpawner, intent.DefaultReconcilerConfig())
	srv.SetIntentManager(ipc.NewIntentManagerAdapter(intentMgr))

	procFS := vfs.NewProcFS(k, ctxMgr)
	_ = devReg.Register("/proc", procFS.FileFactory())

	socketPath := ipc.SocketPath()
	if err := srv.ListenAndServe(socketPath); err != nil {
		return fmt.Errorf("daemon: listen failed: %w", err)
	}

	// Init bootstrap sequence (Story 10.5)
	initCfg, err := kernel.LoadInitConfig("rnix-init.yaml")
	if err != nil {
		srv.Shutdown()
		srv.Wait()
		k.Shutdown()
		os.Remove(socketPath)
		return fmt.Errorf("daemon: init config error: %w", err)
	}
	initResult, err := kernel.Bootstrap(k, initCfg, agentLoader.Load)
	if err != nil {
		srv.Shutdown()
		srv.Wait()
		k.Shutdown()
		os.Remove(socketPath)
		return fmt.Errorf("daemon: bootstrap failed: %w", err)
	}
	for _, svc := range initResult.Started {
		fmt.Fprintf(os.Stderr, "[init] \u2713 %s\n", svc)
	}
	for _, warn := range initResult.Warnings {
		fmt.Fprintf(os.Stderr, "[init] \u26a0 %s\n", warn)
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
