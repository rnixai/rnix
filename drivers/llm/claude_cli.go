package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultModel is the default Claude model to use.
	DefaultModel = "haiku"
	// DefaultTimeout is the default timeout for a single LLM CLI invocation.
	// Claude Code CLI tasks vary widely (simple: ~5s, complex with tool use: 2-3min).
	// 5 minutes provides headroom for multi-turn agentic tasks.
	DefaultTimeout = 5 * time.Minute
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
	cliCommand     string
	defaultModel   string
	defaultTimeout time.Duration
	cmdBuilder     CommandBuilder
	extraArgs      []string
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

// WithCommand sets the CLI binary name for the driver.
func WithCommand(cmd string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.cliCommand = cmd
	}
}

// WithCommandBuilder sets a custom CommandBuilder for the driver.
func WithCommandBuilder(cb CommandBuilder) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.cmdBuilder = cb
	}
}

// WithExtraArgs appends additional CLI arguments to every invocation.
func WithExtraArgs(args []string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.extraArgs = args
	}
}

// NewClaudeCliDriver creates a new ClaudeCliDriver with the given options.
func NewClaudeCliDriver(opts ...ClaudeCliOption) *ClaudeCliDriver {
	d := &ClaudeCliDriver{
		cliCommand:     "claude",
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
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	Result       string  `json:"result"`
	IsError      bool    `json:"is_error"`
	CostUSD      float64 `json:"cost_usd"`
	DurationMS   int     `json:"duration_ms"`
	NumTurns     int     `json:"num_turns"`
	SessionID    string  `json:"session_id"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
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
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, NewLLMError("claude", 0, ErrTimeout)
	}

	// Try parsing stdout JSON even when exit code is non-zero
	var cliResp claudeCliResponse
	if parseErr := json.Unmarshal(stdout.Bytes(), &cliResp); parseErr == nil {
		if cliResp.IsError {
			errMsg := cliResp.Result
			if errMsg == "" {
				errMsg = "unknown error (empty result)"
			}
			code, sentinel := classifyCliError(errMsg)
			if sentinel != nil {
				return nil, NewLLMError("claude", code, sentinel)
			}
			if err != nil {
				return nil, NewLLMError("claude", 0, fmt.Errorf("cli error (exit %d): %s", cmd.ProcessState.ExitCode(), errMsg))
			}
			return nil, NewLLMError("claude", 0, fmt.Errorf("%s", errMsg))
		}
		if cliResp.Result == "" {
			return nil, NewLLMError("claude", 0, fmt.Errorf("response truncated: no result (possible max_turns limit)"))
		}
		return &LLMResponse{
			Content:      cliResp.Result,
			TokensUsed:   cliResp.InputTokens + cliResp.OutputTokens,
			InputTokens:  cliResp.InputTokens,
			OutputTokens: cliResp.OutputTokens,
		}, nil
	}

	// stdout has no valid JSON — fall back to stderr
	if err != nil {
		return nil, NewLLMError("claude", 0, fmt.Errorf("cli failed (exit %d): %s", cmd.ProcessState.ExitCode(), stderr.String()))
	}

	return nil, NewLLMError("claude", 0, fmt.Errorf("invalid json in stdout"))
}

// claudeStreamEvent is the JSON structure for a single stream-json line.
type claudeStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Message struct {
		Content []claudeContentBlock `json:"content,omitempty"`
		Role    string               `json:"role,omitempty"`
	} `json:"message,omitzero"`
	Tools        []string        `json:"tools,omitempty"` // system:init tools list
	Event        json.RawMessage `json:"event,omitempty"` // raw API event for stream_event type
	Result       string          `json:"result,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	DurationMS   int             `json:"duration_ms,omitempty"`
	NumTurns     int             `json:"num_turns,omitempty"`
	InputTokens  int             `json:"input_tokens,omitempty"`
	OutputTokens int             `json:"output_tokens,omitempty"`
}

// claudeContentBlock represents a content block in an assistant/user message.
type claudeContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`          // tool_use block id
	Name      string `json:"name,omitempty"`        // tool_use tool name
	Input     any    `json:"input,omitempty"`       // tool_use input
	ToolUseID string `json:"tool_use_id,omitempty"` // tool_result reference
	Content   any    `json:"content,omitempty"`     // tool_result content (string or array)
}

// Stream executes a streaming LLM request via the Claude Code CLI.
func (d *ClaudeCliDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	args := d.buildArgs(req, "stream-json")
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start claude cli: %w", err)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer cancel()

		scanner := newStreamScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var evt claudeStreamEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				select {
				case ch <- StreamEvent{Type: "error", Err: NewLLMError("claude", 0, fmt.Errorf("failed to parse stream event: %w", err))}:
				case <-ctx.Done():
				}
				continue
			}

			switch evt.Type {
			case "system":
				data := map[string]any{"subtype": evt.Subtype}
				if len(evt.Tools) > 0 {
					data["tools"] = evt.Tools
				}
				se := StreamEvent{
					Type:    "system",
					Content: evt.Subtype,
					Data:    data,
				}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}
			case "user":
				// User message (includes tool_result for tool execution results)
				data := map[string]any{"role": "user"}
				if len(evt.Message.Content) > 0 {
					data["content"] = contentBlocksToAny(evt.Message.Content)
				}
				se := StreamEvent{Type: "user", Data: data}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}
			case "stream_event":
				// Raw Claude API streaming event — extract tool_use and thinking blocks
				if se := extractClaudeStreamEvent(evt.Event); se != nil {
					select {
					case ch <- *se:
					case <-ctx.Done():
						return
					}
				}
			case "assistant":
				// Forward full message content (text + tool_use blocks) for message history
				data := map[string]any{"role": "assistant"}
				if len(evt.Message.Content) > 0 {
					data["content"] = contentBlocksToAny(evt.Message.Content)
				}
				se := StreamEvent{Type: "assistant", Data: data}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}
				// Also emit text content events for existing consumers
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
				se := StreamEvent{Type: "done", Content: evt.Result, TokensUsed: evt.InputTokens + evt.OutputTokens, InputTokens: evt.InputTokens, OutputTokens: evt.OutputTokens}
				if evt.IsError {
					se.Type = "error"
					errMsg := evt.Result
					if errMsg == "" {
						errMsg = "unknown error (empty result)"
					}
					code, sentinel := classifyCliError(errMsg)
					if sentinel != nil {
						se.Err = NewLLMError("claude", code, sentinel)
					} else {
						se.Err = NewLLMError("claude", 0, fmt.Errorf("%s", errMsg))
					}
				} else if evt.Result == "" {
					se.Type = "error"
					se.Err = NewLLMError("claude", 0, fmt.Errorf("response truncated: no result (possible max_turns limit)"))
				}
				select {
				case ch <- se:
				case <-ctx.Done():
				}
				_ = cmd.Wait()
				return
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("claude", 0, fmt.Errorf("stream read error: %w", err))}:
			case <-ctx.Done():
			}
		}

		// No result event received — check exit code and stderr for error details
		waitErr := cmd.Wait()
		if waitErr != nil {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = fmt.Sprintf("claude cli exited with error: %v", waitErr)
			}
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("claude", 0, fmt.Errorf("%s", errMsg))}:
			case <-ctx.Done():
			}
		} else if stderrBuf.Len() > 0 {
			errMsg := strings.TrimSpace(stderrBuf.String())
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("claude", 0, fmt.Errorf("claude cli stderr: %s", errMsg))}:
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
		DriverType:   DriverClaudeCLI,
	}
}

// buildArgs constructs CLI arguments for a Claude Code CLI invocation.
func (d *ClaudeCliDriver) buildArgs(req LLMRequest, outputFormat string) []string {
	prompt := d.buildPrompt(req)
	args := []string{"-p", prompt, "--output-format", outputFormat}

	if outputFormat == "stream-json" {
		args = append(args, "--verbose", "--include-partial-messages")
	}

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

	args = append(args, d.extraArgs...)

	return args
}

// buildPrompt constructs the prompt for a CLI Agent invocation.
// CLI Agents manage their own agent loop internally, so each Call is an
// independent task — no cross-invocation history serialization needed.
func (d *ClaudeCliDriver) buildPrompt(req LLMRequest) string {
	return req.Intent
}

// contentBlocksToAny converts content blocks to a generic slice for StreamEvent.Data.
func contentBlocksToAny(blocks []claudeContentBlock) []map[string]any {
	result := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		m := map[string]any{"type": b.Type}
		if b.Text != "" {
			m["text"] = b.Text
		}
		if b.ID != "" {
			m["id"] = b.ID
		}
		if b.Name != "" {
			m["name"] = b.Name
		}
		if b.Input != nil {
			m["input"] = b.Input
		}
		if b.ToolUseID != "" {
			m["tool_use_id"] = b.ToolUseID
		}
		if b.Content != nil {
			m["content"] = b.Content
		}
		result = append(result, m)
	}
	return result
}

// classifyCliError attempts to categorize an error message from the CLI
// into a sentinel error and HTTP status code. Returns (0, nil) if no match.
func classifyCliError(msg string) (int, error) {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "rate limit"):
		return 429, ErrRateLimit
	case strings.Contains(lower, "overloaded") || strings.Contains(lower, "529"):
		return 529, ErrTransient
	case strings.Contains(lower, "socket") || strings.Contains(lower, "connection"):
		return 0, ErrTransient
	case strings.Contains(lower, "auth") || strings.Contains(lower, "key"):
		return 401, ErrAuth
	case strings.Contains(lower, "too long") || strings.Contains(lower, "context"):
		return 400, ErrContextLength
	default:
		return 0, nil
	}
}

// extractClaudeStreamEvent extracts tool_use and thinking blocks from a raw Claude API stream event.
// Returns nil for events that don't map to a driver-level event (e.g., text_delta, ping, message_start).
func extractClaudeStreamEvent(raw json.RawMessage) *StreamEvent {
	if len(raw) == 0 {
		return nil
	}
	var event struct {
		Type         string `json:"type"`
		ContentBlock struct {
			Type string `json:"type"`
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"content_block,omitzero"`
		Delta struct {
			Type        string `json:"type"`
			PartialJSON string `json:"partial_json"`
			Thinking    string `json:"thinking"`
		} `json:"delta,omitzero"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil
	}

	switch event.Type {
	case "content_block_start":
		switch event.ContentBlock.Type {
		case "tool_use":
			return &StreamEvent{
				Type:    "tool_call",
				Content: "started",
				Data: map[string]any{
					"tool":    event.ContentBlock.Name,
					"call_id": event.ContentBlock.ID,
					"subtype": "started",
				},
			}
		case "thinking":
			return &StreamEvent{
				Type:    "thinking",
				Content: "started",
				Data:    map[string]any{"subtype": "started"},
			}
		}
	case "content_block_stop":
		// Signals end of a content block; kernel uses next event to finalize pending tool steps
		return nil
	case "content_block_delta":
		switch event.Delta.Type {
		case "thinking_delta":
			return &StreamEvent{
				Type:    "thinking",
				Content: event.Delta.Thinking,
				Data:    map[string]any{"subtype": "delta"},
			}
		case "input_json_delta":
			// Forward tool input fragments for step record accumulation
			return &StreamEvent{
				Type:    "tool_call",
				Content: "input_delta",
				Data:    map[string]any{"partial_json": event.Delta.PartialJSON},
			}
		}
	}
	return nil
}
