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
	// CodexDefaultModel is the default Codex model to use.
	CodexDefaultModel = "codex-mini-latest"
	// CodexDefaultTimeout is the default timeout for a single Codex CLI invocation.
	// Codex exec tasks with tool use can take several minutes.
	CodexDefaultTimeout = 5 * time.Minute
)

// CodexCliDriver implements LLMDriver by invoking the Codex CLI (codex exec).
type CodexCliDriver struct {
	cliCommand     string
	defaultModel   string
	defaultTimeout time.Duration
	cmdBuilder     CommandBuilder
	extraArgs      []string
}

// CodexCliOption configures a CodexCliDriver.
type CodexCliOption func(*CodexCliDriver)

// CodexWithModel sets the default model for the driver.
func CodexWithModel(model string) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.defaultModel = model
	}
}

// CodexWithTimeout sets the default timeout for the driver.
func CodexWithTimeout(timeout time.Duration) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.defaultTimeout = timeout
	}
}

// CodexWithCommand sets the CLI binary name for the driver.
func CodexWithCommand(cmd string) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.cliCommand = cmd
	}
}

// CodexWithCommandBuilder sets a custom CommandBuilder for the driver.
func CodexWithCommandBuilder(cb CommandBuilder) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.cmdBuilder = cb
	}
}

// CodexWithExtraArgs appends additional CLI arguments to every invocation.
func CodexWithExtraArgs(args []string) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.extraArgs = args
	}
}

// NewCodexCliDriver creates a new CodexCliDriver with the given options.
func NewCodexCliDriver(opts ...CodexCliOption) *CodexCliDriver {
	d := &CodexCliDriver{
		cliCommand:     "codex",
		defaultModel:   CodexDefaultModel,
		defaultTimeout: CodexDefaultTimeout,
		cmdBuilder:     defaultCommandBuilder,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// codexStreamEvent represents a single JSONL event from `codex exec --json`.
type codexStreamEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id,omitempty"`
	Item     *struct {
		ID      string `json:"id,omitempty"`
		Type    string `json:"type,omitempty"`
		Text    string `json:"text,omitempty"`
		Command string `json:"command,omitempty"`
		Status  string `json:"status,omitempty"`
		Output  string `json:"output,omitempty"`
	} `json:"item,omitempty"`
	Usage *struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens,omitempty"`
		OutputTokens      int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Message string `json:"message,omitempty"` // error event message
}

// Call executes a synchronous LLM request via the Codex CLI.
// In non-JSON mode, Codex streams progress to stderr and prints only the final
// agent message to stdout.
func (d *CodexCliDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := d.buildArgs(req, false)
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, NewLLMError("codex", 0, ErrTimeout)
	}

	content := strings.TrimSpace(stdout.String())

	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = content
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("codex cli exited with error: %v", err)
		}
		code, sentinel := classifyCliError(errMsg)
		if sentinel != nil {
			return nil, NewLLMError("codex", code, sentinel)
		}
		return nil, NewLLMError("codex", 0, fmt.Errorf("cli error: %s", errMsg))
	}

	if content == "" {
		return nil, NewLLMError("codex", 0, fmt.Errorf("response truncated: no output from codex exec"))
	}

	// Non-JSON mode: no token usage available.
	return &LLMResponse{
		Content: content,
	}, nil
}

// Stream executes a streaming LLM request via the Codex CLI with --json output.
func (d *CodexCliDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	args := d.buildArgs(req, true)
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
		return nil, fmt.Errorf("failed to start codex cli: %w", err)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer cancel()

		var lastUsage struct {
			inputTokens  int
			outputTokens int
		}
		var lastAgentMessage string

		scanner := newStreamScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var evt codexStreamEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				select {
				case ch <- StreamEvent{Type: "error", Err: NewLLMError("codex", 0, fmt.Errorf("failed to parse stream event: %w", err))}:
				case <-ctx.Done():
				}
				continue
			}

			switch evt.Type {
			case "thread.started":
				data := map[string]any{"subtype": "init"}
				if evt.ThreadID != "" {
					data["thread_id"] = evt.ThreadID
				}
				se := StreamEvent{
					Type:    "system",
					Content: "init",
					Data:    data,
				}
				select {
				case ch <- se:
				case <-ctx.Done():
					return
				}

			case "item.started":
				if evt.Item != nil && evt.Item.Type == "command_execution" {
					se := StreamEvent{
						Type:    "tool_call",
						Content: "started",
						Data: map[string]any{
							"tool":    "shell",
							"command": evt.Item.Command,
							"call_id": evt.Item.ID,
							"subtype": "started",
						},
					}
					select {
					case ch <- se:
					case <-ctx.Done():
						return
					}
				}

			case "item.completed":
				if evt.Item == nil {
					continue
				}
				switch evt.Item.Type {
				case "agent_message":
					lastAgentMessage = evt.Item.Text
					select {
					case ch <- StreamEvent{Type: "content", Content: evt.Item.Text}:
					case <-ctx.Done():
						return
					}
				case "command_execution":
					se := StreamEvent{
						Type:    "tool_call",
						Content: "completed",
						Data: map[string]any{
							"tool":    "shell",
							"command": evt.Item.Command,
							"call_id": evt.Item.ID,
							"output":  evt.Item.Output,
							"subtype": "completed",
						},
					}
					select {
					case ch <- se:
					case <-ctx.Done():
						return
					}
				case "reasoning":
					select {
					case ch <- StreamEvent{Type: "thinking", Content: evt.Item.Text}:
					case <-ctx.Done():
						return
					}
				}

			case "turn.completed":
				if evt.Usage != nil {
					lastUsage.inputTokens += evt.Usage.InputTokens
					lastUsage.outputTokens += evt.Usage.OutputTokens
				}

			case "turn.failed":
				errMsg := "turn failed"
				if evt.Message != "" {
					errMsg = evt.Message
				}
				select {
				case ch <- StreamEvent{Type: "error", Err: NewLLMError("codex", 0, fmt.Errorf("%s", errMsg))}:
				case <-ctx.Done():
				}

			case "error":
				errMsg := "unknown error"
				if evt.Message != "" {
					errMsg = evt.Message
				}
				code, sentinel := classifyCliError(errMsg)
				var llmErr error
				if sentinel != nil {
					llmErr = NewLLMError("codex", code, sentinel)
				} else {
					llmErr = NewLLMError("codex", 0, fmt.Errorf("%s", errMsg))
				}
				select {
				case ch <- StreamEvent{Type: "error", Err: llmErr}:
				case <-ctx.Done():
				}
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("codex", 0, fmt.Errorf("stream read error: %w", err))}:
			case <-ctx.Done():
			}
		}

		// Emit done event with the last agent message and accumulated usage.
		se := StreamEvent{
			Type:         "done",
			Content:      lastAgentMessage,
			TokensUsed:   lastUsage.inputTokens + lastUsage.outputTokens,
			InputTokens:  lastUsage.inputTokens,
			OutputTokens: lastUsage.outputTokens,
		}
		if lastAgentMessage == "" {
			se.Type = "error"
			se.Err = NewLLMError("codex", 0, fmt.Errorf("response truncated: no agent message received"))
		}
		select {
		case ch <- se:
		case <-ctx.Done():
		}

		_ = cmd.Wait()
	}()

	return ch, nil
}

// Info returns metadata about this driver.
func (d *CodexCliDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         "codex-cli",
		Provider:     "codex",
		DefaultModel: d.defaultModel,
		DriverType:   DriverCodexCLI,
	}
}

// buildArgs constructs CLI arguments for a Codex exec invocation.
func (d *CodexCliDriver) buildArgs(req LLMRequest, jsonMode bool) []string {
	prompt := d.buildPrompt(req)
	args := []string{"exec", "--full-auto"}

	if jsonMode {
		args = append(args, "--json")
	}

	model := req.Model
	if model == "" {
		model = d.defaultModel
	}
	if model != "" {
		args = append(args, "-m", model)
	}

	args = append(args, d.extraArgs...)

	// Prompt must be the last argument per `codex exec [OPTIONS] [PROMPT]`.
	args = append(args, prompt)

	return args
}

// buildPrompt constructs the prompt from intent, system prompt, and conversation history.
// Codex CLI has no --system-prompt flag, so system instructions are prepended to the prompt.
func (d *CodexCliDriver) buildPrompt(req LLMRequest) string {
	var sb strings.Builder

	// Prepend system prompt if present.
	if req.SystemPrompt != "" {
		fmt.Fprintf(&sb, "[System Instructions]\n%s\n\n[Task]\n", req.SystemPrompt)
	}

	if len(req.Messages) <= 1 {
		sb.WriteString(req.Intent)
		return sb.String()
	}

	// Multi-turn: serialize conversation history.
	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			fmt.Fprintf(&sb, "Human: %s\n\n", msg.Content)
		case "assistant":
			fmt.Fprintf(&sb, "Assistant: %s\n\n", msg.Content)
		case "tool":
			fmt.Fprintf(&sb, "[Tool Result (%s)]\n%s\n\n", msg.ToolCallID, msg.Content)
		}
	}
	fmt.Fprintf(&sb, "Continue. Your original task: %s", req.Intent)
	return sb.String()
}
