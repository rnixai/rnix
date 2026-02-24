package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

const (
	// DefaultModel is the default Claude model to use.
	DefaultModel = "sonnet"
	// DefaultTimeout is the default timeout for LLM calls.
	DefaultTimeout = 30 * time.Second
	// streamChanBuffer is the buffer size for stream event channels.
	streamChanBuffer = 64
)

// CommandBuilder is a function type that creates exec.Cmd instances.
// In production, this wraps exec.CommandContext; in tests, it can be replaced with a mock.
type CommandBuilder func(ctx context.Context, name string, args ...string) *exec.Cmd

// defaultCommandBuilder wraps exec.CommandContext for production use.
func defaultCommandBuilder(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// ClaudeCliDriver implements LLMDriver by invoking the Claude Code CLI.
type ClaudeCliDriver struct {
	defaultModel   string
	defaultTimeout time.Duration
	cmdBuilder     CommandBuilder
}

// ClaudeCliOption configures a ClaudeCliDriver.
type ClaudeCliOption func(*ClaudeCliDriver)

// WithModel sets the default model for the driver.
func WithModel(model string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.defaultModel = model
	}
}

// WithTimeout sets the default timeout for the driver.
func WithTimeout(timeout time.Duration) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.defaultTimeout = timeout
	}
}

// WithCommandBuilder sets a custom CommandBuilder for the driver.
func WithCommandBuilder(cb CommandBuilder) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.cmdBuilder = cb
	}
}

// NewClaudeCliDriver creates a new ClaudeCliDriver with the given options.
func NewClaudeCliDriver(opts ...ClaudeCliOption) *ClaudeCliDriver {
	d := &ClaudeCliDriver{
		defaultModel:   DefaultModel,
		defaultTimeout: DefaultTimeout,
		cmdBuilder:     defaultCommandBuilder,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// claudeCliResponse is the JSON structure returned by `claude -p ... --output-format json`.
type claudeCliResponse struct {
	Type       string  `json:"type"`
	Subtype    string  `json:"subtype"`
	Result     string  `json:"result"`
	IsError    bool    `json:"is_error"`
	CostUSD    float64 `json:"cost_usd"`
	DurationMS int     `json:"duration_ms"`
	NumTurns   int     `json:"num_turns"`
	SessionID  string  `json:"session_id"`
}

// Call executes a synchronous LLM request via the Claude Code CLI.
func (d *ClaudeCliDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := d.buildArgs(req, "json")
	cmd := d.cmdBuilder(ctx, "claude", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("llm call timed out after %v", timeout)
	}

	// Try parsing stdout JSON even when exit code is non-zero (AC #1, #2)
	var cliResp claudeCliResponse
	if parseErr := json.Unmarshal(stdout.Bytes(), &cliResp); parseErr == nil {
		if cliResp.IsError {
			errMsg := cliResp.Result
			if errMsg == "" {
				errMsg = "unknown error (empty result)"
			}
			if err != nil {
				return nil, fmt.Errorf("llm returned error (exit %d): %s", cmd.ProcessState.ExitCode(), errMsg)
			}
			return nil, fmt.Errorf("llm returned error: %s", errMsg)
		}
		if cliResp.Result == "" {
			return nil, fmt.Errorf("llm response truncated: no result (possible max_turns limit)")
		}
		return &LLMResponse{
			Content:    cliResp.Result,
			TokensUsed: cliResp.NumTurns,
		}, nil
	}

	// stdout has no valid JSON — fall back to stderr
	if err != nil {
		return nil, fmt.Errorf("claude cli failed (exit %d): %s", cmd.ProcessState.ExitCode(), stderr.String())
	}

	return nil, fmt.Errorf("failed to parse llm response: invalid json in stdout")
}

// claudeStreamEvent is the JSON structure for a single stream-json line.
type claudeStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content,omitempty"`
	} `json:"message,omitzero"`
	Result     string  `json:"result,omitempty"`
	IsError    bool    `json:"is_error,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty"`
	NumTurns   int     `json:"num_turns,omitempty"`
}

// Stream executes a streaming LLM request via the Claude Code CLI.
func (d *ClaudeCliDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	args := d.buildArgs(req, "stream-json")
	cmd := d.cmdBuilder(ctx, "claude", args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start claude cli: %w", err)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer func() { _ = cmd.Wait() }()
		defer cancel()

		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var evt claudeStreamEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				select {
				case ch <- StreamEvent{Type: "error", Err: fmt.Errorf("failed to parse stream event: %w", err)}:
				case <-ctx.Done():
				}
				continue
			}

			switch evt.Type {
			case "assistant":
				for _, c := range evt.Message.Content {
					if c.Type == "text" {
						select {
						case ch <- StreamEvent{Type: "content", Content: c.Text}:
						case <-ctx.Done():
							return
						}
					}
				}
			case "result":
				se := StreamEvent{Type: "done", Content: evt.Result, TokensUsed: evt.NumTurns}
				if evt.IsError {
					se.Type = "error"
					errMsg := evt.Result
					if errMsg == "" {
						errMsg = "unknown error (empty result)"
					}
					se.Err = fmt.Errorf("llm returned error: %s", errMsg)
				} else if evt.Result == "" {
					se.Type = "error"
					se.Err = fmt.Errorf("llm response truncated: no result (possible max_turns limit)")
				}
				select {
				case ch <- se:
				case <-ctx.Done():
				}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case ch <- StreamEvent{Type: "error", Err: fmt.Errorf("stream read error: %w", err)}:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}

// Info returns metadata about this driver.
func (d *ClaudeCliDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         "claude-cli",
		Provider:     "claude",
		DefaultModel: d.defaultModel,
	}
}

// buildArgs constructs CLI arguments for a Claude Code CLI invocation.
func (d *ClaudeCliDriver) buildArgs(req LLMRequest, outputFormat string) []string {
	args := []string{"-p", req.Intent, "--output-format", outputFormat}

	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	model := req.Model
	if model == "" {
		model = d.defaultModel
	}
	args = append(args, "--model", model)

	if req.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(req.MaxTurns))
	}

	return args
}
