package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/drivers/fs"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/drivers/lsp"
	"github.com/rnixai/rnix/drivers/mcp"
	drivershell "github.com/rnixai/rnix/drivers/shell"
	"github.com/rnixai/rnix/drivers/tty"
	"github.com/rnixai/rnix/drivers/tasks"
	"github.com/rnixai/rnix/drivers/cron"
	driversmemory "github.com/rnixai/rnix/drivers/memory"
	"github.com/rnixai/rnix/drivers/web"
	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	kernelmemory "github.com/rnixai/rnix/kernel/memory"
	agentshell "github.com/rnixai/rnix/shell"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
	"github.com/spf13/cobra"
)

var (
	version   = "0.1.0"
	gitCommit = ""
	buildDate = ""
)

func versionString() string {
	if gitCommit == "" {
		return version + "-dev"
	}
	return version
}

// Global flags
var (
	flagJSON     bool
	flagVerbose  bool
	flagQuiet    bool
	flagUUID     bool
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

func (c *cliCallbacks) OnSpawn(pid types.PID, intent, provider, model, procUUID string) {
	uuidShort := procUUID
	if len(uuidShort) > 12 {
		uuidShort = uuidShort[:12] + "..."
	}
	if provider != "" && model != "" {
		c.progress.KernelMessage("spawning PID %d (uuid: %s, %s/%s)...", pid, uuidShort, provider, model)
	} else if provider != "" {
		c.progress.KernelMessage("spawning PID %d (uuid: %s, %s)...", pid, uuidShort, provider)
	} else {
		c.progress.KernelMessage("spawning PID %d (uuid: %s)...", pid, uuidShort)
	}
}

func (c *cliCallbacks) OnStep(pid types.PID, step, total int) {
	c.progress.AgentStep(pid, step, total)
}

func (c *cliCallbacks) OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64) {
	c.progress.AgentStepComplete(pid, step, action, summary)
}

func (c *cliCallbacks) OnComplete(pid types.PID, result string, exit kernel.ExitStatus) {
	// Output handled by main flow after Done channel
}

func (c *cliCallbacks) OnError(pid types.PID, err error) {
	// Output handled by main flow after Done channel
}

func (c *cliCallbacks) OnAskUser(pid types.PID, requestID string, questions []byte) ([]byte, error) {
	// Not used in embedded mode — /dev/tty requires daemon mode with IPC
	return nil, fmt.Errorf("ask_user not supported in embedded mode")
}

var rootCmd = &cobra.Command{
	Use:   "rnix [command]",
	Short: "Rnix — Agent OS for AI agents",
	Long:  "Rnix is an operating system for AI agents.\n\nUse -i flag to spawn an agent with an intent.",
	Example: `  rnix -i "analyze ./README.md"
  rnix -i "refactor error handling in main.go"
  rnix version
  rnix -i "analyze project structure" --json`,
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
			"version":               versionString(),
			"git_commit":            gitCommit,
			"build_date":            buildDate,
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

	fmt.Fprintf(w, "rnix v%s\n", versionString())
	if gitCommit != "" {
		fmt.Fprintf(w, "commit:  %s\n", gitCommit)
	}
	if buildDate != "" {
		fmt.Fprintf(w, "built:   %s\n", buildDate)
	}
	if !claudeAvailable {
		fmt.Fprintln(w, "✗ claude-code CLI not found")
		fmt.Fprintln(w, "  → hint: npm install -g @anthropic-ai/claude-code")
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
	rootCmd.Flags().IntVar(&flagMaxSteps, "max-steps", 0, "Max reasoning steps (0 = infinite, default 0)")
	rootCmd.Flags().StringVar(&flagAgent, "agent", "", "Agent definition to use (e.g., code-analyst)")
	rootCmd.Flags().StringVarP(&flagIntent, "intent", "i", "", "Intent string to spawn an agent")
	rootCmd.Flags().Bool("dashboard", false, "Open dashboard after spawning agent")
	daemonCmd.Flags().BoolVar(&flagDaemonInternal, "internal", false, "Internal flag (not for user use)")
	_ = daemonCmd.Flags().MarkHidden("internal")
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(straceCmd)
	psCmd.Flags().BoolVar(&flagUUID, "uuid", false, "Show UUID column in process table")
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(suspendCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(heartbeatCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(composeCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(topCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(gdbCmd)
	dashboardCmd.Flags().String("load", "", "Load a recording for offline replay (path or record-id)")
	dashboardCmd.Flags().Int("pid", 0, "Focus on a specific process PID at startup")
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(intentCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(initCmd)
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

	// Discover project directory (CLI-side, passed to daemon via IPC)
	cwd, _ := os.Getwd()
	projectDir, _ := config.ProjectDir(cwd)

	client, err := ipc.EnsureDaemon()
	if err != nil {
		outputError(renderer, mode, "daemon", err.Error(), "daemon failed to start", "check that rnix is installed correctly")
		exitCode = 1
		return nil
	}
	defer client.Close()

	if isScriptSyntax(intent) {
		runScript(renderer, mode, progress, client, intent, start, projectDir)
		return nil
	}

	if isPipelineSyntax(intent) {
		runPipeline(renderer, mode, progress, client, intent, start, projectDir)
		return nil
	}

	// Single-line $VAR expansion from OS environment
	if containsVarRef(intent) {
		env := agentshell.NewEnvironmentFromOS()
		intent = env.Expand(intent)
	}

	req := ipc.SpawnRequest{
		Intent:     intent,
		Agent:      flagAgent,
		Model:      flagModel,
		Provider:   flagProvider,
		MaxSteps:   flagMaxSteps,
		ProjectDir: projectDir,
		RnixEnv:    os.Getenv("RNIX_ENV"),
	}

	dashboardFlag, _ := cmd.Flags().GetBool("dashboard")
	if dashboardFlag {
		pid, spawnErr := client.SpawnDetached(req)
		client.Close()
		if spawnErr != nil {
			outputError(renderer, mode, "/dev/llm", spawnErr.Error(), "agent failed to start", "check that LLM CLI is installed")
			exitCode = 1
			return nil
		}
		progress.KernelMessage("spawned PID %d, launching dashboard...", pid)
		exe, _ := os.Executable()
		return syscall.Exec(exe, []string{"rnix", "dashboard", fmt.Sprintf("--pid=%d", pid)}, os.Environ())
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var spawnedPID atomic.Uint64
	var spawnProvider, spawnModel string
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
		// Keep draining signals after timeout so subsequent Ctrl+C still force-exits
		<-sigCh
		forceExitFunc(130)
	}()

	pid, final, spawnErr := client.SpawnAndWatch(req, func(ev ipc.StreamEvent) {
		switch ev.Type {
		case ipc.StreamAskUser:
			handleAskUserEvent(ev.Payload, ipc.SocketPath())
			return
		case ipc.StreamProgress:
			// fall through to progress handling below
		default:
			return
		}

		var pp ipc.ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			return
		}
		switch pp.Event {
		case "spawn":
			spawnProvider = pp.Provider
			spawnModel = pp.Model
			uuidShort := pp.UUID
			if len(uuidShort) > 12 {
				uuidShort = uuidShort[:12] + "..."
			}
			if pp.Provider != "" && pp.Model != "" {
				progress.KernelMessage("spawning PID %d (uuid: %s, %s/%s)...", pp.PID, uuidShort, pp.Provider, pp.Model)
			} else if pp.Provider != "" {
				progress.KernelMessage("spawning PID %d (uuid: %s, %s)...", pp.PID, uuidShort, pp.Provider)
			} else {
				progress.KernelMessage("spawning PID %d (uuid: %s)...", pp.PID, uuidShort)
			}
		case "step":
			progress.AgentStep(pp.PID, pp.Step, pp.Total)
		case "step_complete":
			if mode == ui.ModeJSON {
				jsonLine, _ := json.Marshal(pp)
				fmt.Fprintf(os.Stdout, "%s\n", jsonLine)
			} else {
				progress.AgentStepComplete(pp.PID, pp.Step, pp.Action, pp.Summary)
			}
		case "error":
			// handled in final
		}
	})
	spawnedPID.Store(uint64(pid))

	if spawnErr != nil {
		outputError(renderer, mode, "/dev/llm", spawnErr.Error(), "agent failed to start", "check that LLM CLI is installed (claude or agent)")
		exitCode = 1
		return nil
	}

	elapsed := time.Since(start)

	if final != nil && final.ExitCode == 0 {
		outputSuccess(renderer, mode, pid, final.Result, final.TokensUsed, elapsed, spawnProvider, spawnModel)
	} else {
		reason := "unknown error"
		if final != nil {
			reason = final.ExitReason
			if final.ErrorMessage != "" {
				reason = final.ErrorMessage
			}
		}
		outputError(renderer, mode, "/dev/llm", reason, "agent execution failed", "check intent description or retry")
		tokensUsed := 0
		if final != nil {
			tokensUsed = final.TokensUsed
		}
		ui.RenderSummary(renderer, pid, 1, tokensUsed, elapsed, spawnProvider, spawnModel)
		exitCode = 1
	}

	return nil
}

func runPipeline(renderer *ui.Renderer, mode ui.OutputMode, progress *ui.ProgressReporter, client *ipc.Client, intent string, start time.Time, projectDir string) {
	pipeline, err := agentshell.ParsePipeline(intent)
	if err != nil {
		outputError(renderer, mode, "shell/parser", err.Error(), "pipeline syntax parse failed", "check syntax: spawn \"A\" | spawn \"B\"")
		exitCode = 1
		return
	}

	if len(pipeline.Commands) > agentshell.MaxRecommendedStages {
		progress.KernelMessage("warning: pipeline has %d stages (recommended ≤ %d)", len(pipeline.Commands), agentshell.MaxRecommendedStages)
	}

	req := ipc.SpawnPipelineRequest{
		Commands:   make([]ipc.SpawnPipelineCommand, len(pipeline.Commands)),
		ProjectDir: projectDir,
		RnixEnv:    os.Getenv("RNIX_ENV"),
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
		outputError(renderer, mode, "shell/pipe", pipeErr.Error(), "pipeline execution failed", "check pipeline commands or retry")
		exitCode = 1
		return
	}

	elapsed := time.Since(start)

	if pipeResp == nil || len(pipeResp.Stages) == 0 {
		outputError(renderer, mode, "shell/pipe", "no pipeline result", "pipeline returned empty result", "check pipeline commands")
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
			"pipeline execution interrupted", "check the failed stage intent")
		totalTokens := 0
		for _, s := range pipeResp.Stages {
			totalTokens += s.TokensUsed
		}
		ui.RenderSummary(renderer, lastStage.PID, lastStage.ExitCode, totalTokens, elapsed, "", "")
		exitCode = 1
		return
	}

	totalTokens := 0
	for _, s := range pipeResp.Stages {
		totalTokens += s.TokensUsed
	}
	outputSuccess(renderer, mode, lastStage.PID, lastStage.Result, totalTokens, elapsed, "", "")
}

func runScript(renderer *ui.Renderer, mode ui.OutputMode, progress *ui.ProgressReporter, client *ipc.Client, intent string, start time.Time, projectDir string) {
	req := ipc.ExecScriptRequest{
		Script:     intent,
		Env:        agentshell.NewEnvironmentFromOS().All(),
		ProjectDir: projectDir,
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
			if pp.Total > 0 {
				if pp.Intent != "" {
					progress.KernelMessage("script step %d/%d: %s", pp.Step, pp.Total, pp.Intent)
				} else {
					progress.KernelMessage("script step %d/%d...", pp.Step, pp.Total)
				}
			} else {
				if pp.Intent != "" {
					progress.KernelMessage("script step %d: %s", pp.Step, pp.Intent)
				} else {
					progress.KernelMessage("script step %d...", pp.Step)
				}
			}
		}
	})
	close(scriptDone)

	if scriptErr != nil {
		outputError(renderer, mode, "shell/script", scriptErr.Error(), "script execution failed", "check script syntax or retry")
		exitCode = 1
		return
	}

	elapsed := time.Since(start)

	if scriptResp == nil {
		outputError(renderer, mode, "shell/script", "no script result", "script returned empty result", "check script content")
		exitCode = 1
		return
	}

	if scriptResp.LastExitCode == 0 {
		outputSuccess(renderer, mode, 0, scriptResp.LastResult, scriptResp.TotalTokens, elapsed, "", "")
	} else {
		outputError(renderer, mode, "shell/script",
			fmt.Sprintf("script failed (exit %d)", scriptResp.LastExitCode),
			"script execution interrupted", "check spawn commands in script")
		ui.RenderSummary(renderer, 0, scriptResp.LastExitCode, scriptResp.TotalTokens, elapsed, "", "")
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

func outputSuccess(renderer *ui.Renderer, mode ui.OutputMode, pid types.PID, result string, tokensUsed int, elapsed time.Duration, provider, model string) {
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
	ui.RenderSummary(renderer, pid, 0, tokensUsed, elapsed, provider, model)
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

// handleAskUserEvent processes a StreamAskUser event from the daemon during spawn.
// It displays questions to the user via stdin/stdout and sends answers back.
func handleAskUserEvent(payload json.RawMessage, socketPath string) {
	var ev ipc.AskUserEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ ask_user: invalid event: %v\n", err)
		return
	}

	// Parse questions from raw JSON
	var questions []struct {
		Question    string   `json:"question"`
		Options     []string `json:"options,omitempty"`
		MultiSelect bool     `json:"multi_select,omitempty"`
	}
	if err := json.Unmarshal(ev.Questions, &questions); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ ask_user: invalid questions: %v\n", err)
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	var answers []json.RawMessage
	stdinFailed := false

	for _, q := range questions {
		fmt.Fprintf(os.Stdout, "\n❓ %s\n", q.Question)

		// Auto-append "Other" option per AC1 when options are present
		opts := q.Options
		if len(opts) > 0 {
			opts = append(opts, "Other")
			for i, opt := range opts {
				fmt.Fprintf(os.Stdout, "  %d) %s\n", i+1, opt)
			}
			if q.MultiSelect {
				fmt.Fprint(os.Stdout, "Enter choices (comma-separated numbers): ")
			} else {
				fmt.Fprint(os.Stdout, "Enter choice number (or type free text): ")
			}
		} else {
			fmt.Fprint(os.Stdout, "> ")
		}

		if !scanner.Scan() {
			// EOF or error — pad remaining with empty answers
			stdinFailed = true
			answers = append(answers, json.RawMessage(`{"answer":""}`))
			continue
		}
		if stdinFailed {
			// stdin already failed, pad with empty
			answers = append(answers, json.RawMessage(`{"answer":""}`))
			continue
		}
		input := strings.TrimSpace(scanner.Text())

		var ansJSON []byte
		if len(opts) > 0 && q.MultiSelect {
			// Parse comma-separated numbers
			var selected []string
			for part := range strings.SplitSeq(input, ",") {
				idx, err := strconv.Atoi(strings.TrimSpace(part))
				if err == nil && idx >= 1 && idx <= len(opts) {
					if opts[idx-1] == "Other" {
						// "Other" selected in multi-select — prompt for free text
						fmt.Fprint(os.Stdout, "  Enter your answer: ")
						if scanner.Scan() {
							selected = append(selected, strings.TrimSpace(scanner.Text()))
						}
					} else {
						selected = append(selected, opts[idx-1])
					}
				}
			}
			ansJSON, _ = json.Marshal(map[string]any{"answers": selected})
		} else if len(opts) > 0 {
			// Single select by number
			idx, err := strconv.Atoi(input)
			if err == nil && idx >= 1 && idx <= len(opts) {
				if opts[idx-1] == "Other" {
					// "Other" selected — prompt for free text
					fmt.Fprint(os.Stdout, "  Enter your answer: ")
					if scanner.Scan() {
						ansJSON, _ = json.Marshal(map[string]any{"answer": strings.TrimSpace(scanner.Text())})
					} else {
						ansJSON, _ = json.Marshal(map[string]any{"answer": ""})
					}
				} else {
					ansJSON, _ = json.Marshal(map[string]any{"answer": opts[idx-1]})
				}
			} else {
				// Free text fallback
				ansJSON, _ = json.Marshal(map[string]any{"answer": input})
			}
		} else {
			ansJSON, _ = json.Marshal(map[string]any{"answer": input})
		}
		answers = append(answers, ansJSON)
	}

	answersJSON, _ := json.Marshal(answers)
	if err := ipc.AnswerUser(socketPath, ev.RequestID, answersJSON); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ ask_user: failed to send answer: %v\n", err)
	}
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
		ui.RenderProcessTable(renderer, procs, true, flagUUID)
	default:
		ui.RenderProcessTable(renderer, procs, false, flagUUID)
	}

	return nil
}

// jsonProcess is the JSON representation of a single process for rnix ps --json.
type jsonProcess struct {
	PID             types.PID `json:"pid"`
	UUID            string    `json:"uuid,omitempty"`
	PPID            types.PID `json:"ppid"`
	State           string    `json:"state"`
	Intent          string    `json:"intent"`
	Skills          []string  `json:"skills"`
	TokensUsed      int       `json:"tokens_used"`
	MaxTokens       int64     `json:"max_tokens,omitempty"`
	MaxCost         float64   `json:"max_cost,omitempty"`
	UsedCost        float64   `json:"used_cost,omitempty"`
	ElapsedMs       int64     `json:"elapsed_ms"`
	Provider        string    `json:"provider,omitempty"`
	Model           string    `json:"model,omitempty"`
	LastHeartbeatMs int64     `json:"last_heartbeat_ms,omitempty"`
	StepTimeoutMs   int64     `json:"step_timeout_ms,omitempty"`
	SuspendReason   string    `json:"suspend_reason,omitempty"`
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
			PID:           p.PID,
			UUID:          p.UUID,
			PPID:          p.PPID,
			State:         p.State.String(),
			Intent:        p.Intent,
			Skills:        skills,
			TokensUsed:    p.TokensUsed,
			MaxTokens:     p.MaxTokens,
			MaxCost:       p.MaxCost,
			UsedCost:      p.UsedCost,
			ElapsedMs:     now.Sub(p.CreatedAt).Milliseconds(),
			Provider:      p.Provider,
			Model:         p.Model,
			StepTimeoutMs: p.StepTimeout.Milliseconds(),
			SuspendReason: p.SuspendReason,
		}
		if !p.LastHeartbeat.IsZero() {
			entries[i].LastHeartbeatMs = p.LastHeartbeat.UnixMilli()
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
			"rnix ps  to see active processes")
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
			"rnix ps  to see active processes")
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
			"rnix ps  to see active processes")
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
			"rnix ps  to see active processes")
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
			if p.Source == "project" {
				fmt.Fprintf(w, "  %-12s %s (%s) [project]\n", p.Name, p.Health, p.Driver)
			} else {
				fmt.Fprintf(w, "  %-12s %s (%s)\n", p.Name, p.Health, p.Driver)
			}
		}
	}

	return nil
}

func runDaemon(cmd *cobra.Command, args []string) error {
	if !flagDaemonInternal {
		return cmd.Help()
	}

	// Resolve global config directory
	globalDir, err := config.GlobalDir()
	if err != nil {
		return fmt.Errorf("resolve global config directory: %w", err)
	}

	// Load providers config from global directory
	var providersCfg *llm.ProvidersConfig
	var globalProvidersRaw []byte
	providersPath := filepath.Join(globalDir, "providers.yaml")
	if _, err := os.Stat(providersPath); err == nil {
		globalProvidersRaw, err = os.ReadFile(providersPath)
		if err != nil {
			return fmt.Errorf("reading providers config %s: %w", providersPath, err)
		}
		providersCfg, err = llm.ParseProvidersConfig(globalProvidersRaw)
		if err != nil {
			return fmt.Errorf("loading providers config %s: %w", providersPath, err)
		}
	} else if os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[kernel] info: %s not found, using default provider configuration\n", providersPath)
		providersCfg = llm.DefaultProvidersConfig()
	} else {
		return fmt.Errorf("checking providers config: %w", err)
	}

	driverReg := llm.NewDriverRegistry()
	devReg := vfs.NewDeviceRegistry()
	if err := llm.RegisterProviders(providersCfg, driverReg, devReg); err != nil {
		return fmt.Errorf("registering LLM providers: %w", err)
	}
	llm.RunHealthChecks(providersCfg, driverReg, 3*time.Second)
	vfsInst := vfs.NewVFS(devReg)
	fsDriver := fs.NewDriver()
	_ = devReg.RegisterWithDriver("/dev/fs", fsDriver.FileFactory(), fsDriver)
	shellDriver := drivershell.NewDriver()
	_ = devReg.RegisterWithDriver("/dev/shell", drivershell.FileFactory(shellDriver, "/dev/shell"), shellDriver)
	webDriver := web.NewDriverWithOptions(web.DriverOpts{
		Extractor: newLLMExtractor(driverReg, providersCfg),
	})
	_ = devReg.RegisterWithDriver("/dev/web", web.FileFactory(webDriver), webDriver)
	lspDriver := lsp.NewDriver()
	_ = devReg.RegisterWithDriver("/dev/lsp", lsp.FileFactory(lspDriver), lspDriver)
	defer lspDriver.CloseAll()

	// /dev/tasks (Story 33-2) — session-level task store, shared across processes
	tasksDriver := tasks.NewDriver()
	_ = devReg.RegisterWithDriver("/dev/tasks", tasks.FileFactory(tasksDriver), tasksDriver)

	// /dev/memory/commit (Story 35.2) — persistent memory read/write
	var memStore *kernelmemory.MemoryStore
	memoryCfg := kernelmemory.DefaultMemoryConfig()
	if memoryCfg.Enabled {
		globalMemDir := filepath.Join(globalDir, "memory")
		daemonCwd, _ := os.Getwd()
		projectMemDir := filepath.Join(daemonCwd, ".rnix", "memory")
		memStore = kernelmemory.NewMemoryStore(globalMemDir, projectMemDir, memoryCfg)
		if err := memStore.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "[memory] warn: failed to load memory: %v\n", err)
		}
		memDriver := driversmemory.NewDriver(memStore)
		_ = devReg.RegisterWithDriver("/dev/memory/commit", driversmemory.FileFactory(memDriver), memDriver)
	}

	ctxMgr := rnixctx.NewManager()
	skillLoader := skills.NewSkillLoader([]string{filepath.Join(globalDir, "skills")})

	// Load global MCP configuration (optional, mcp.yaml may not exist)
	var mcpCfg *mcp.MCPGlobalConfig
	mcpPath := filepath.Join(globalDir, "mcp.yaml")
	if _, err := os.Stat(mcpPath); err == nil {
		mcpCfg, err = mcp.LoadMCPConfig(mcpPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kernel] warn: failed to load %s: %v\n", mcpPath, err)
		}
	}

	agentLoader := agents.NewAgentLoader([]string{filepath.Join(globalDir, "agents")}, skillLoader, mcpCfg)

	// Load global config.yaml (optional, not critical)
	immuneCfg := kernel.DefaultImmuneConfig()
	globalConfigPath := filepath.Join(globalDir, "config.yaml")
	if _, err := os.Stat(globalConfigPath); err == nil {
		data, err := os.ReadFile(globalConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kernel] warn: failed to read %s: %v\n", globalConfigPath, err)
		} else if len(data) > 0 {
			var parsed map[string]any
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				fmt.Fprintf(os.Stderr, "[kernel] warn: failed to parse %s: %v\n", globalConfigPath, err)
			} else if immuneRaw, ok := parsed["immune"].(map[string]any); ok {
				var warnings []string
				immuneCfg, warnings = kernel.ParseImmuneConfig(immuneRaw)
				for _, w := range warnings {
					fmt.Fprintf(os.Stderr, "[kernel] warn: %s\n", w)
				}
			}
		}
	}

	// Assemble GlobalConfig
	globalConfig := &config.GlobalConfig{
		Dir:       globalDir,
		Providers: providersCfg,
		MCP:       mcpCfg,
		AgentsDir: filepath.Join(globalDir, "agents"),
		SkillsDir: filepath.Join(globalDir, "skills"),
	}

	// Create MountManager with TransportFactory for MCP server mounts
	transportFactory := func(cfg vfs.MCPConfig) (vfs.MCPTransport, error) {
		tc := mcp.TransportConfig{
			Command: cfg.Command,
			Args:    cfg.Args,
			WorkDir: cfg.WorkDir,
		}
		for k, v := range cfg.Env {
			tc.Env = append(tc.Env, k+"="+v)
		}
		return mcp.NewStdioTransport(tc), nil
	}
	mountMgr := vfs.NewMountManager(devReg, transportFactory)

	srv := ipc.NewServer(nil, agentLoader.Load, versionString())
	k := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	k.SetMountManager(mountMgr)
	k.SetProviderResolver(driverReg.Names, func(name string) bool { _, ok := driverReg.Get(name); return ok })
	k.SetDefaultProvider(providersCfg.ResolveDefaultProvider())
	k.SetCostPerToken(func(provider string) float64 {
		for _, p := range providersCfg.Providers {
			if p.Name == provider {
				return p.CostPerToken
			}
		}
		return 0
	})
	k.SetAgentLoader(agentLoader.Load)

	// Stem agent differentiation (Story 20.3)
	discovery := skills.NewSkillDiscovery(skillLoader, []string{filepath.Join(globalDir, "skills")})
	stemMatcher := kernel.NewStemMatcher(discovery)
	k.SetStemMatcher(stemMatcher)
	k.SetSkillLoader(skillLoader.LoadFull)

	// Differentiation memory (Story 20.4)
	diffMemory := kernel.NewDiffMemory(256)
	k.SetDiffMemory(diffMemory)

	// Memory store injection (Story 35.2)
	if memStore != nil {
		k.SetMemoryStore(memStore)
	}

	// Writeback async knowledge extraction (Story 35.3)
	if memStore != nil && memoryCfg.Writeback.Enabled {
		defaultProv := ""
		if providersCfg != nil {
			defaultProv = providersCfg.DefaultProvider
		}
		wbCaller := &writebackLLMAdapter{
			driverReg:       driverReg,
			model:           memoryCfg.Writeback.Model,
			defaultProvider: defaultProv,
		}
		wbWorker := kernelmemory.NewWritebackWorker(memStore, wbCaller, memoryCfg.Writeback, defaultProv)
		k.SetWritebackWorker(wbWorker)
	}

	// Initialize execution recording (Story 14.1)
	cwd, _ := os.Getwd()

	// Set step data dir and load process history from disk
	k.SetStepDataDir(filepath.Join(cwd, ".rnix"))
	if err := k.LoadHistory(); err != nil {
		fmt.Fprintf(os.Stderr, "[kernel] warn: load process history: %v\n", err)
	}

	recordBaseDir := resolveDataDir(cwd, "records")
	recordMgr := debug.NewRecordManager(recordBaseDir)
	k.SetRecordManager(recordMgr)

	// Load project-level .rnix/config.yaml immune overrides
	projectConfigPath := filepath.Join(cwd, ".rnix", "config.yaml")
	if _, err := os.Stat(projectConfigPath); err == nil {
		data, err := os.ReadFile(projectConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kernel] warn: failed to read %s: %v\n", projectConfigPath, err)
		} else if len(data) > 0 {
			var parsed map[string]any
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				fmt.Fprintf(os.Stderr, "[kernel] warn: failed to parse %s: %v\n", projectConfigPath, err)
			} else if parsed != nil {
				if immuneRaw, ok := parsed["immune"].(map[string]any); ok {
					var warnings []string
					immuneCfg, warnings = kernel.ParseImmuneConfig(immuneRaw, immuneCfg)
					for _, w := range warnings {
						fmt.Fprintf(os.Stderr, "[kernel] warn: %s\n", w)
					}
				}
			}
		}
	}

	// Initialize reputation and synergy matrix (Story 21.3, 21.5)
	reputationDir := resolveDataDir(cwd, "reputation")
	reputationStore := kernel.NewReputationStore(reputationDir)
	synergyMatrix := kernel.NewSynergyMatrix(reputationDir)

	// Initialize immune daemon (Story 22.1) — conditional on config
	var immuneDaemon *kernel.ImmuneDaemon
	if immuneCfg.Enabled {
		immuneDir := resolveDataDir(cwd, "immune")
		immuneStore := kernel.NewImmuneStore(immuneDir)
		immuneDaemon = kernel.NewImmuneDaemon(immuneStore, immuneCfg)

		if err := immuneDaemon.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[kernel] warn: immune daemon start failed: %v\n", err)
		}
		k.SetImmuneDaemon(immuneDaemon)

		// Story 22.2: set suspendFn after kernel is available (to call Kill with SIGPAUSE)
		immuneDaemon.SetSuspendFunc(func(pid types.PID) error {
			return k.Kill(pid, types.SIGPAUSE)
		})
	}

	// Initialize span persistence (Story 15.1)
	traceBaseDir := resolveDataDir(cwd, "traces")
	k.SetSpanWriter(debug.NewSpanWriter(traceBaseDir))

	// Initialize heartbeat monitor (Story 30.6)
	hm := kernel.NewHeartbeatMonitor(k, 30*time.Second)
	k.SetHeartbeatMonitor(hm)
	hm.Start()

	srv.SetKernel(k)
	srv.SetContextManager(ctxMgr)
	srv.SetSkillLoader(skillLoader)
	srv.SetReputationStore(reputationStore)
	srv.SetSynergyMatrix(synergyMatrix)
	srv.SetImmuneDaemon(immuneDaemon)
	srv.SetGlobalConfig(globalConfig)
	srv.SetGlobalProvidersRaw(globalProvidersRaw)
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

	// /dev/tty (Story 33-2) — user interaction via ask_user callback through IPC
	ttyDriver := tty.NewDriver(func(ctx context.Context, questions []tty.Question) ([]tty.Answer, error) {
		questionsJSON, err := json.Marshal(questions)
		if err != nil {
			return nil, fmt.Errorf("marshal questions: %w", err)
		}
		// Extract PID from process context (uses kernel context key)
		pid := kernel.PIDFromContext(ctx)
		requestID := fmt.Sprintf("ask-%d-%d", pid, time.Now().UnixNano())

		// Run OnAskUser in a goroutine so we can select on ctx.Done()
		type askResult struct {
			data []byte
			err  error
		}
		resultCh := make(chan askResult, 1)
		go func() {
			data, err := srv.CallbackMux().OnAskUser(pid, requestID, questionsJSON)
			resultCh <- askResult{data, err}
		}()

		select {
		case res := <-resultCh:
			if res.err != nil {
				return nil, res.err
			}
			var answers []tty.Answer
			if err := json.Unmarshal(res.data, &answers); err != nil {
				return nil, fmt.Errorf("unmarshal answers: %w", err)
			}
			return answers, nil
		case <-ctx.Done():
			return nil, fmt.Errorf("process cancelled while waiting for user response: %w", ctx.Err())
		}
	})
	_ = devReg.RegisterWithDriver("/dev/tty", tty.FileFactory(ttyDriver), ttyDriver)

	// /dev/cron (Story 33-2) — scheduled recurring jobs
	cronDataDir := resolveDataDir(cwd, "cron")
	cronDriver := cron.NewDriver(cronDataDir, func(intent, agent string) (types.PID, error) {
		agentInfo, err := agentLoader.Load(agent)
		if err != nil {
			return 0, fmt.Errorf("cron: load agent %q: %w", agent, err)
		}
		return k.Spawn(intent, agentInfo, kernel.SpawnOpts{})
	})
	if err := cronDriver.LoadAndStart(); err != nil {
		fmt.Fprintf(os.Stderr, "[kernel] warn: cron load failed: %v\n", err)
	}
	_ = devReg.RegisterWithDriver("/dev/cron", cron.FileFactory(cronDriver), cronDriver)
	defer cronDriver.Close()

	procFS := vfs.NewProcFS(k, ctxMgr)
	_ = devReg.Register("/proc", procFS.FileFactory())

	socketPath := ipc.SocketPath()
	if err := srv.ListenAndServe(socketPath); err != nil {
		return fmt.Errorf("daemon: listen failed: %w", err)
	}

	// Register signal handler early so Ctrl+C works during bootstrap and cleanup.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Force exit on second signal (covers both bootstrap phase and cleanup phase).
	go func() {
		<-sigCh // first signal — handled by main select below
		<-sigCh // second signal — force exit
		fmt.Fprintln(os.Stderr, "\n[daemon] forced exit")
		os.Exit(1)
	}()

	// Init bootstrap sequence (Story 10.5)
	// Check CWD rnix-init.yaml first (backward compat), then global init.yaml
	initCfg, err := loadInitConfigCompat(cwd, globalDir)
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

	select {
	case <-sigCh:
		fmt.Fprintln(os.Stderr, "[daemon] shutting down...")
		srv.Shutdown()
	case <-srv.Done():
	}

	srv.Wait()
	immuneDaemon.Stop()
	k.Shutdown()
	os.Remove(socketPath)

	return nil
}

// resolveDataDir returns the data directory path with backward compatibility.
// Uses .rnix/data/<name> as the new path, but falls back to .rnix/<name> if the old path exists.
func resolveDataDir(cwd, name string) string {
	oldPath := filepath.Join(cwd, ".rnix", name)
	if _, err := os.Stat(oldPath); err == nil {
		return oldPath
	}
	return filepath.Join(cwd, ".rnix", "data", name)
}

// loadInitConfigCompat loads init configuration with backward compatibility.
// Checks CWD/rnix-init.yaml first, then global dir init.yaml, then defaults.
func loadInitConfigCompat(cwd, globalDir string) (*kernel.InitConfig, error) {
	// First: CWD rnix-init.yaml (backward compat)
	cwdInit := filepath.Join(cwd, "rnix-init.yaml")
	if _, err := os.Stat(cwdInit); err == nil {
		return kernel.LoadInitConfig(cwdInit)
	}

	// Second: global dir init.yaml
	globalInit := filepath.Join(globalDir, "init.yaml")
	if _, err := os.Stat(globalInit); err == nil {
		return kernel.LoadInitConfig(globalInit)
	}

	// Neither exists: use default empty config
	return kernel.DefaultInitConfig(), nil
}

// llmExtractorAdapter adapts an llm.LLMDriver to the web.ContentExtractor
// interface, enabling web_fetch to process content+prompt through a secondary
// model (following CC's queryHaiku pattern).
type llmExtractorAdapter struct {
	driver llm.LLMDriver
	model  string
}

func (a *llmExtractorAdapter) Extract(ctx context.Context, content, prompt string) (string, error) {
	modelPrompt := web.MakeSecondaryModelPrompt(content, prompt)
	resp, err := a.driver.Call(ctx, llm.LLMRequest{
		Intent:    modelPrompt,
		Model:     a.model,
		MaxTokens: 4000,
	})
	if err != nil {
		return "", err
	}
	if resp.Content == "" {
		return "", fmt.Errorf("empty response from secondary model")
	}
	return resp.Content, nil
}

// newLLMExtractor creates a ContentExtractor from the default LLM provider.
// Returns nil if no suitable provider is available.
func newLLMExtractor(driverReg *llm.DriverRegistry, cfg *llm.ProvidersConfig) web.ContentExtractor {
	providerName := cfg.ResolveDefaultProvider()
	driver, ok := driverReg.Get(providerName)
	if !ok {
		return nil
	}
	// Use the provider's default model — the provider config should point to
	// a fast model suitable for utility calls.
	return &llmExtractorAdapter{driver: driver, model: driver.Info().DefaultModel}
}

// writebackLLMAdapter adapts an llm.DriverRegistry to the kernelmemory.LLMCaller
// interface for async writeback knowledge extraction (Story 35.3).
type writebackLLMAdapter struct {
	driverReg       *llm.DriverRegistry
	model           string
	defaultProvider string
}

func (a *writebackLLMAdapter) Call(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	providerName := a.model
	if providerName == "" {
		providerName = a.defaultProvider
	}
	driver, ok := a.driverReg.Get(providerName)
	if !ok {
		return "", fmt.Errorf("LLM provider %q not found for writeback", providerName)
	}
	resp, err := driver.Call(ctx, llm.LLMRequest{
		Intent:       userPrompt,
		SystemPrompt: systemPrompt,
		MaxTokens:    maxTokens,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(2)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
