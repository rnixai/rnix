package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rnixai/rnix/agtest"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/internal/types"
	"github.com/spf13/cobra"
)

var (
	flagAgtestDryRun bool
	flagAgtestTimeout int64
)

var agtestCmd = &cobra.Command{
	Use:   "agtest [file-or-dir]",
	Short: "Run agent behavior regression tests",
	Long: `Run declarative agent behavior tests defined in YAML files.

Use --dry-run to parse and validate test files without executing them.
Accepts a single YAML file or a directory containing *.yaml files.`,
	Example: `  rnix agtest test.yaml                Parse and run a single test file
  rnix agtest tests/                   Run all tests in a directory
  rnix agtest test.yaml --dry-run      Validate without executing
  rnix agtest tests/ --dry-run --json  Validate with JSON output`,
	Args: cobra.ExactArgs(1),
	RunE: runAgtest,
}

func init() {
	agtestCmd.Flags().BoolVar(&flagAgtestDryRun, "dry-run", false, "Parse and validate only, do not execute tests")
	agtestCmd.Flags().Int64Var(&flagAgtestTimeout, "timeout", 60000, "Global timeout per test case in milliseconds")
	rootCmd.AddCommand(agtestCmd)
}

func runAgtest(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	path := args[0]

	info, err := os.Stat(path)
	if err != nil {
		return agtestError(w, fmt.Sprintf("cannot access %s: %v", path, err))
	}

	var suite *agtest.TestSuiteSpec
	if info.IsDir() {
		suite, err = agtest.ParseDir(path)
	} else {
		suite, err = agtest.ParseFile(path)
	}

	if err != nil {
		return agtestError(w, err.Error())
	}

	if flagAgtestDryRun {
		return agtestDryRunOutput(w, suite)
	}

	return runAgtestSuite(w, suite)
}

func runAgtestSuite(w interface{ Write([]byte) (int, error) }, suite *agtest.TestSuiteSpec) error {
	dialFunc := func() (*ipc.Client, error) {
		return ipc.Dial(ipc.SocketPath())
	}

	executor := &ipcExecutor{dialFunc: dialFunc}
	var judge agtest.QualityJudge

	hasQuality := false
	for _, tc := range suite.Tests {
		if tc.Assert != nil && tc.Assert.Quality != nil {
			hasQuality = true
			break
		}
	}
	if hasQuality {
		judge = &ipcQualityJudge{dialFunc: dialFunc}
	}

	runner := &agtest.Runner{
		Executor: executor,
		Judge:    judge,
		Timeout:  time.Duration(flagAgtestTimeout) * time.Millisecond,
	}

	if !flagJSON {
		fmt.Fprintf(w, "[agtest] running %d test case(s)...\n\n", len(suite.Tests))
	}

	result := runner.RunSuite(context.Background(), suite)

	if flagJSON {
		return agtestJSONReport(w, result)
	}
	return agtestTextReport(w, result)
}

func agtestTextReport(w interface{ Write([]byte) (int, error) }, sr *agtest.SuiteResult) error {
	for _, c := range sr.Cases {
		durationStr := fmt.Sprintf("%.1fs", c.Duration.Seconds())
		switch c.Status {
		case agtest.StatusPassed:
			fmt.Fprintf(w, "  \u2713 %s (%s)\n", c.Name, durationStr)
		case agtest.StatusFailed:
			fmt.Fprintf(w, "  \u2717 %s (%s)\n", c.Name, durationStr)
			for _, a := range c.Assertions {
				if !a.Passed {
					fmt.Fprintf(w, "    %s: %s\n", a.Type, a.Message)
					fmt.Fprintf(w, "      expected: %v\n", a.Expected)
					fmt.Fprintf(w, "      actual: %v\n", a.Actual)
				}
			}
		case agtest.StatusSkipped:
			fmt.Fprintf(w, "  - %s (skipped)\n", c.Name)
		case agtest.StatusError:
			fmt.Fprintf(w, "  \u2717 %s (%s) ERROR: %s\n", c.Name, durationStr, c.Error)
		}
	}

	fmt.Fprintf(w, "\n[agtest] %d total, %d passed, %d failed, %d skipped, %d errors (%.1fs)\n",
		sr.Total, sr.Passed, sr.Failed, sr.Skipped, sr.Errors, sr.Duration.Seconds())

	if sr.Failed > 0 || sr.Errors > 0 {
		exitCode = 1
	}
	return nil
}

type agtestCaseJSON struct {
	Name       string                  `json:"name"`
	Status     agtest.CaseStatus       `json:"status"`
	DurationMs int64                   `json:"duration_ms"`
	Error      string                  `json:"error,omitempty"`
	Assertions []agtestAssertionJSON   `json:"assertions,omitempty"`
}

type agtestAssertionJSON struct {
	Type     string `json:"type"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
}

func agtestJSONReport(w interface{ Write([]byte) (int, error) }, sr *agtest.SuiteResult) error {
	cases := make([]agtestCaseJSON, len(sr.Cases))
	for i, c := range sr.Cases {
		cj := agtestCaseJSON{
			Name:       c.Name,
			Status:     c.Status,
			DurationMs: c.Duration.Milliseconds(),
			Error:      c.Error,
		}
		if len(c.Assertions) > 0 {
			cj.Assertions = make([]agtestAssertionJSON, len(c.Assertions))
			for j, a := range c.Assertions {
				cj.Assertions[j] = agtestAssertionJSON{
					Type:     a.Type,
					Passed:   a.Passed,
					Message:  a.Message,
					Expected: a.Expected,
					Actual:   a.Actual,
				}
			}
		}
		cases[i] = cj
	}

	ok := sr.Failed == 0 && sr.Errors == 0
	resp := JSONResponse{
		OK: ok,
		Data: map[string]any{
			"total":       sr.Total,
			"passed":      sr.Passed,
			"failed":      sr.Failed,
			"skipped":     sr.Skipped,
			"errors":      sr.Errors,
			"duration_ms": sr.Duration.Milliseconds(),
			"cases":       cases,
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(w, `{"ok":false,"error":{"message":"marshal error: %s"}}`, err)
		fmt.Fprintln(w)
		exitCode = 1
		return nil
	}
	fmt.Fprintln(w, string(data))

	if !ok {
		exitCode = 1
	}
	return nil
}

// --- IPC TestExecutor ---

type ipcExecutor struct {
	dialFunc func() (*ipc.Client, error)
}

func (e *ipcExecutor) Execute(ctx context.Context, tc *agtest.TestCaseSpec) (*agtest.ExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	client, err := e.dialFunc()
	if err != nil {
		return nil, fmt.Errorf("daemon not available: %w", err)
	}
	defer client.Close()

	req := ipc.SpawnRequest{
		Intent:        tc.Intent,
		Agent:         tc.Agent.Name,
		Model:         tc.Agent.Model,
		ContextBudget: tc.Agent.ContextBudget,
	}
	if tc.Timeout > 0 {
		req.TimeoutMs = int64(tc.Timeout)
	} else if deadline, ok := ctx.Deadline(); ok {
		req.TimeoutMs = time.Until(deadline).Milliseconds()
	}

	hasSyscallAssert := tc.Assert != nil && tc.Assert.Syscalls != nil
	var syscalls []string
	var debugDone chan struct{}

	type spawnResult struct {
		pid  types.PID
		pp   *ipc.ProgressPayload
		err  error
	}
	resultCh := make(chan spawnResult, 1)

	go func() {
		pid, final, spawnErr := client.SpawnAndWatch(req, func(ev ipc.StreamEvent) {
			if hasSyscallAssert && ev.Type == ipc.StreamProgress {
				var pp ipc.ProgressPayload
				if err := json.Unmarshal(ev.Payload, &pp); err == nil && pp.Event == "spawn" && debugDone == nil {
					debugDone = make(chan struct{})
					go func() {
						defer close(debugDone)
						debugClient, dialErr := e.dialFunc()
						if dialErr != nil {
							return
						}
						defer debugClient.Close()
						_ = debugClient.AttachDebug(pp.PID, func(sew ipc.SyscallEventWire) {
							syscalls = append(syscalls, sew.Syscall)
						})
					}()
				}
			}
		})
		resultCh <- spawnResult{pid, final, spawnErr}
	}()

	var sr spawnResult
	select {
	case sr = <-resultCh:
	case <-ctx.Done():
		client.Close()
		return nil, ctx.Err()
	}

	if debugDone != nil {
		<-debugDone
	}

	result := &agtest.ExecutionResult{
		Syscalls: syscalls,
	}

	if sr.err != nil {
		result.Error = sr.err.Error()
		return result, nil
	}

	if sr.pp != nil {
		result.Output = sr.pp.Result
		result.ExitCode = sr.pp.ExitCode
		result.TokensUsed = sr.pp.TokensUsed
		if sr.pp.ErrorMessage != "" && result.Error == "" {
			result.Error = sr.pp.ErrorMessage
		}
	}
	return result, nil
}

// --- IPC QualityJudge ---

type ipcQualityJudge struct {
	dialFunc func() (*ipc.Client, error)
	model    string
}

func (j *ipcQualityJudge) Judge(ctx context.Context, output, criteria string) (*agtest.QualityResult, error) {
	client, err := j.dialFunc()
	if err != nil {
		return nil, fmt.Errorf("daemon not available for quality judge: %w", err)
	}
	defer client.Close()

	intent := fmt.Sprintf(
		"Evaluate whether the following output meets the criteria.\n\nOutput:\n%s\n\nCriteria:\n%s\n\nRespond in JSON: {\"passed\": bool, \"score\": 0.0-1.0, \"reason\": \"...\"}",
		truncateForJudge(output, 4000),
		criteria,
	)

	model := j.model
	if model == "" {
		model = "haiku"
	}

	req := ipc.SpawnRequest{
		Intent:    intent,
		Model:     model,
		TimeoutMs: 30000,
	}

	_, final, spawnErr := client.SpawnAndWatch(req, func(_ ipc.StreamEvent) {})
	if spawnErr != nil {
		return nil, fmt.Errorf("quality judge spawn failed: %w", spawnErr)
	}
	if final == nil {
		return nil, fmt.Errorf("quality judge returned no result")
	}

	return agtest.ParseQualityResponse(final.Result), nil
}

func truncateForJudge(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// Ensure ipcExecutor and ipcQualityJudge implement their interfaces.
var (
	_ agtest.TestExecutor = (*ipcExecutor)(nil)
	_ agtest.QualityJudge = (*ipcQualityJudge)(nil)
)

func agtestDryRunOutput(w interface{ Write([]byte) (int, error) }, suite *agtest.TestSuiteSpec) error {
	if flagJSON {
		type testSummary struct {
			Name   string `json:"name,omitempty"`
			Intent string `json:"intent"`
			Agent  string `json:"agent"`
		}
		summaries := make([]testSummary, len(suite.Tests))
		for i, tc := range suite.Tests {
			summaries[i] = testSummary{Name: tc.Name, Intent: tc.Intent, Agent: tc.Agent.Name}
		}
		resp := JSONResponse{
			OK: true,
			Data: map[string]any{
				"test_count": len(suite.Tests),
				"tests":      summaries,
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprintf(w, "[agtest] parsed %d test case(s)\n", len(suite.Tests))
	for i, tc := range suite.Tests {
		name := tc.Name
		if name == "" {
			name = fmt.Sprintf("test-%d", i+1)
		}
		fmt.Fprintf(w, "  %d. %s (agent: %s, intent: %q)\n", i+1, name, tc.Agent.Name, tc.Intent)
	}
	return nil
}

func agtestError(w interface{ Write([]byte) (int, error) }, msg string) error {
	if flagJSON {
		resp := JSONResponse{OK: false, Error: map[string]string{"message": msg}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
	} else {
		fmt.Fprintf(w, "[agtest] error: %s\n", msg)
	}
	exitCode = 1
	return nil
}
