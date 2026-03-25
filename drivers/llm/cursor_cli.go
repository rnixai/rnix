package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// CursorDefaultTimeout is the default timeout for a single Cursor CLI invocation.
	CursorDefaultTimeout = 5 * time.Minute
)

// CursorCliDriver implements LLMDriver by invoking the Cursor CLI (agent --print).
type CursorCliDriver struct {
	defaultModel   string
	defaultTimeout time.Duration
	cmdBuilder     CommandBuilder
}

// CursorCliOption configures a CursorCliDriver.
type CursorCliOption func(*CursorCliDriver)

// CursorWithModel sets the default model for the driver.
func CursorWithModel(model string) CursorCliOption {
	return func(d *CursorCliDriver) {
		d.defaultModel = model
	}
}

// CursorWithTimeout sets the default timeout for the driver.
func CursorWithTimeout(timeout time.Duration) CursorCliOption {
	return func(d *CursorCliDriver) {
		d.defaultTimeout = timeout
	}
}

// CursorWithCommandBuilder sets a custom CommandBuilder for the driver.
func CursorWithCommandBuilder(cb CommandBuilder) CursorCliOption {
	return func(d *CursorCliDriver) {
		d.cmdBuilder = cb
	}
}

// NewCursorCliDriver creates a new CursorCliDriver with the given options.
func NewCursorCliDriver(opts ...CursorCliOption) *CursorCliDriver {
	d := &CursorCliDriver{
		defaultModel:   "",
		defaultTimeout: CursorDefaultTimeout,
		cmdBuilder:     defaultCommandBuilder,
	}
	for _, opt := range opts {
		opt(d)
	}

	return d
}

// cursorCliResponse is the JSON structure returned by `agent --print ... --output-format json`.
// [SPIKE] Fields are assumed based on Cursor CLI docs; verify with Task 0 spike.
type cursorCliResponse struct {
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

// Call executes a synchronous LLM request via the Cursor CLI.
func (d *CursorCliDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := d.buildArgs(req, "json")
	cmd := d.cmdBuilder(ctx, "agent", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, NewLLMError("cursor", 0, ErrTimeout)
	}

	var cliResp cursorCliResponse
	if parseErr := json.Unmarshal(stdout.Bytes(), &cliResp); parseErr == nil {
		if cliResp.IsError {
			errMsg := cliResp.Result
			if errMsg == "" {
				errMsg = "unknown error (empty result)"
			}
			code, sentinel := classifyCliError(errMsg)
			if sentinel != nil {
				return nil, NewLLMError("cursor", code, sentinel)
			}
			if err != nil {
				return nil, NewLLMError("cursor", 0, fmt.Errorf("cli error (exit %d): %s", cmd.ProcessState.ExitCode(), errMsg))
			}
			return nil, NewLLMError("cursor", 0, fmt.Errorf("%s", errMsg))
		}
		if cliResp.Result == "" {
			return nil, NewLLMError("cursor", 0, fmt.Errorf("response truncated: no result"))
		}
		return &LLMResponse{
			Content:      cliResp.Result,
			TokensUsed:   cliResp.InputTokens + cliResp.OutputTokens,
			InputTokens:  cliResp.InputTokens,
			OutputTokens: cliResp.OutputTokens,
		}, nil
	}

	if err != nil {
		return nil, NewLLMError("cursor", 0, fmt.Errorf("cli failed (exit %d): %s", cmd.ProcessState.ExitCode(), stderr.String()))
	}

	return nil, NewLLMError("cursor", 0, fmt.Errorf("invalid json in stdout"))
}

// cursorStreamEvent is the JSON structure for a single Cursor CLI stream-json line.
type cursorStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Model   string `json:"model,omitempty"`
	CallID  string `json:"call_id,omitempty"`
	Text    string `json:"text,omitempty"` // thinking delta text
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content,omitempty"`
	} `json:"message,omitzero"`
	ToolCall     json.RawMessage `json:"tool_call,omitempty"` // raw JSON for tool_call details
	Result       string          `json:"result,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	DurationMS   int             `json:"duration_ms,omitempty"`
	NumTurns     int             `json:"num_turns,omitempty"`
	InputTokens  int             `json:"input_tokens,omitempty"`
	OutputTokens int             `json:"output_tokens,omitempty"`
}

// Stream executes a streaming LLM request via the Cursor CLI.
func (d *CursorCliDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	args := d.buildArgs(req, "stream-json")
	cmd := d.cmdBuilder(ctx, "agent", args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start cursor cli: %w", err)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer func() { _ = cmd.Wait() }()
		defer cancel()

		scanner := newStreamScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var evt cursorStreamEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				select {
				case ch <- StreamEvent{Type: "error", Err: NewLLMError("cursor", 0, fmt.Errorf("failed to parse stream event: %w", err))}:
				case <-ctx.Done():
				}
				continue
			}

			switch evt.Type {
			case "system":
				se := StreamEvent{
					Type:    "system",
					Content: evt.Subtype,
					Data:    map[string]any{"subtype": evt.Subtype},
				}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}
			case "user":
				se := StreamEvent{Type: "user"}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}
			case "thinking":
				se := StreamEvent{
					Type:    "thinking",
					Content: evt.Text,
					Data:    map[string]any{"subtype": evt.Subtype},
				}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}
			case "tool_call":
				se := StreamEvent{
					Type:    "tool_call",
					Content: evt.Subtype,
					Data:    extractToolCallInfo(evt.ToolCall),
				}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}
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
				se := StreamEvent{
					Type:         "done",
					Content:      evt.Result,
					TokensUsed:   evt.InputTokens + evt.OutputTokens,
					InputTokens:  evt.InputTokens,
					OutputTokens: evt.OutputTokens,
				}
				if evt.IsError {
					se.Type = "error"
					errMsg := evt.Result
					if errMsg == "" {
						errMsg = "unknown error (empty result)"
					}
					code, sentinel := classifyCliError(errMsg)
					if sentinel != nil {
						se.Err = NewLLMError("cursor", code, sentinel)
					} else {
						se.Err = NewLLMError("cursor", 0, fmt.Errorf("%s", errMsg))
					}
				} else if evt.Result == "" {
					se.Type = "error"
					se.Err = NewLLMError("cursor", 0, fmt.Errorf("response truncated: no result"))
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
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("cursor", 0, fmt.Errorf("stream read error: %w", err))}:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}

// Info returns metadata about this driver.
func (d *CursorCliDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         "cursor-cli",
		Provider:     "cursor",
		DefaultModel: d.defaultModel,
		DriverType:   DriverCursorCLI,
	}
}

// buildArgs constructs CLI arguments for a Cursor CLI invocation.
// Cursor CLI uses `--print` as a boolean flag; prompt is the last positional argument.
func (d *CursorCliDriver) buildArgs(req LLMRequest, outputFormat string) []string {
	args := []string{"--print", "--output-format", outputFormat, "--force", "--trust", "--approve-mcps"}

	model := req.Model
	if model == "" {
		model = d.defaultModel
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	prompt := d.buildPrompt(req)
	args = append(args, prompt)
	return args
}

// extractToolCallInfo extracts tool name and description from Cursor CLI tool_call JSON.
// The tool_call object has varying structures: shellToolCall, readFileToolCall, etc.
// Each has a "description" field and type-specific args.
func extractToolCallInfo(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var tc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil
	}
	info := make(map[string]any)
	// Find the first *ToolCall key (e.g., "shellToolCall", "readFileToolCall")
	for key, val := range tc {
		if key == "description" {
			var desc string
			if json.Unmarshal(val, &desc) == nil {
				info["description"] = desc
			}
			continue
		}
		// Extract tool type from key (e.g., "shellToolCall" → "shell")
		toolType := strings.TrimSuffix(key, "ToolCall")
		if toolType != key { // it was a *ToolCall key
			info["tool"] = toolType
			// Try to extract description and command from the nested object
			var inner struct {
				Description string `json:"description"`
				Args        struct {
					Command string `json:"command"`
					Path    string `json:"path"`
				} `json:"args"`
			}
			if json.Unmarshal(val, &inner) == nil {
				if inner.Description != "" {
					info["description"] = inner.Description
				}
				if inner.Args.Command != "" {
					info["command"] = inner.Args.Command
				}
				if inner.Args.Path != "" {
					info["path"] = inner.Args.Path
				}
			}
		}
	}
	if len(info) == 0 {
		return nil
	}
	return info
}

// buildPrompt constructs the prompt with optional system prompt prefix.
func (d *CursorCliDriver) buildPrompt(req LLMRequest) string {
	if req.SystemPrompt != "" {
		return "[System Instructions]\n" + req.SystemPrompt + "\n[End System Instructions]\n\n" + req.Intent
	}
	return req.Intent
}
