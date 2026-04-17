package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// QwenDefaultModel is the default Qwen model to use.
	QwenDefaultModel = "qwen3-coder"
	// QwenDefaultTimeout is the default timeout for a single Qwen CLI invocation.
	QwenDefaultTimeout = 5 * time.Minute
)

// QwenCliDriver implements LLMDriver by invoking the Qwen Code CLI.
type QwenCliDriver struct {
	cliCommand     string
	defaultModel   string
	defaultTimeout time.Duration
	graceSec       int
	cmdBuilder     CommandBuilder
	extraArgs      []string
}

// QwenCliOption configures a QwenCliDriver.
type QwenCliOption func(*QwenCliDriver)

// QwenWithModel sets the default model for the driver.
func QwenWithModel(model string) QwenCliOption {
	return func(d *QwenCliDriver) {
		d.defaultModel = model
	}
}

// QwenWithTimeout sets the default timeout for the driver.
func QwenWithTimeout(timeout time.Duration) QwenCliOption {
	return func(d *QwenCliDriver) {
		d.defaultTimeout = timeout
	}
}

// QwenWithGrace sets the grace period (seconds) between SIGTERM and SIGKILL.
// 0 = DefaultGracePeriod.
func QwenWithGrace(graceSec int) QwenCliOption {
	return func(d *QwenCliDriver) {
		d.graceSec = graceSec
	}
}

// QwenWithCommand sets the CLI binary name for the driver.
func QwenWithCommand(cmd string) QwenCliOption {
	return func(d *QwenCliDriver) {
		d.cliCommand = cmd
	}
}

// QwenWithCommandBuilder sets a custom CommandBuilder for the driver.
func QwenWithCommandBuilder(cb CommandBuilder) QwenCliOption {
	return func(d *QwenCliDriver) {
		d.cmdBuilder = cb
	}
}

// QwenWithExtraArgs appends additional CLI arguments to every invocation.
func QwenWithExtraArgs(args []string) QwenCliOption {
	return func(d *QwenCliDriver) {
		d.extraArgs = args
	}
}

// NewQwenCliDriver creates a new QwenCliDriver with the given options.
func NewQwenCliDriver(opts ...QwenCliOption) *QwenCliDriver {
	d := &QwenCliDriver{
		cliCommand:     "qwen",
		defaultModel:   QwenDefaultModel,
		defaultTimeout: QwenDefaultTimeout,
		cmdBuilder:     defaultCommandBuilder,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// qwenResultUsage is the nested usage object in Qwen CLI result messages.
type qwenResultUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

// qwenResultMessage is the result message within the Qwen CLI JSON output array.
type qwenResultMessage struct {
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype"`
	Result       string          `json:"result"`
	IsError      bool            `json:"is_error"`
	DurationMS   int             `json:"duration_ms"`
	NumTurns     int             `json:"num_turns"`
	Usage        qwenResultUsage `json:"usage"`
	Error        *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Call executes a synchronous LLM request via the Qwen Code CLI.
func (d *QwenCliDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := d.buildArgs(req, "json")
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)
	configureCommandGrace(cmd, d.graceSec)
	cmd.Stdin = strings.NewReader(d.buildPrompt(req))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, NewLLMError("qwen", 0, ErrTimeout)
	}

	// Qwen JSON output is an array of messages; find the result message.
	result, parseErr := parseQwenJsonResult(stdout.Bytes())
	if parseErr == nil {
		if result.IsError {
			errMsg := result.Result
			if errMsg == "" && result.Error != nil {
				errMsg = result.Error.Message
			}
			if errMsg == "" {
				errMsg = "unknown error (empty result)"
			}
			code, sentinel := classifyCliError(errMsg)
			if sentinel != nil {
				return nil, NewLLMError("qwen", code, sentinel)
			}
			if err != nil {
				return nil, NewLLMError("qwen", 0, fmt.Errorf("cli error (exit %d): %s", cmd.ProcessState.ExitCode(), errMsg))
			}
			return nil, NewLLMError("qwen", 0, fmt.Errorf("%s", errMsg))
		}
		if result.Result == "" {
			return nil, NewLLMError("qwen", 0, fmt.Errorf("response truncated: no result (possible max_turns limit)"))
		}
		return &LLMResponse{
			Content:      result.Result,
			TokensUsed:   result.Usage.InputTokens + result.Usage.OutputTokens,
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
		}, nil
	}

	// stdout has no valid JSON — fall back to stderr
	if err != nil {
		return nil, NewLLMError("qwen", 0, fmt.Errorf("cli failed (exit %d): %s", cmd.ProcessState.ExitCode(), stderr.String()))
	}

	return nil, NewLLMError("qwen", 0, fmt.Errorf("invalid json in stdout"))
}

// parseQwenJsonResult extracts the result message from a Qwen CLI JSON output.
// Qwen outputs a JSON array of messages; the result is the last item with type "result".
func parseQwenJsonResult(data []byte) (*qwenResultMessage, error) {
	// Try parsing as JSON array first (standard Qwen JSON output).
	var messages []json.RawMessage
	if err := json.Unmarshal(data, &messages); err == nil {
		// Scan from the end for the result message.
		for i := len(messages) - 1; i >= 0; i-- {
			var msg qwenResultMessage
			if json.Unmarshal(messages[i], &msg) == nil && msg.Type == "result" {
				return &msg, nil
			}
		}
		return nil, fmt.Errorf("no result message in json array")
	}

	// Fallback: try as single JSON object (in case Qwen outputs just the result).
	var single qwenResultMessage
	if err := json.Unmarshal(data, &single); err == nil && single.Type == "result" {
		return &single, nil
	}

	return nil, fmt.Errorf("unable to parse qwen json output")
}

// qwenStreamEvent is the JSON structure for a single Qwen CLI stream-json line.
// Qwen Code stream-json output uses the same type taxonomy as Claude Code:
// system, assistant, user, stream_event, result.
type qwenStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Message struct {
		Content []claudeContentBlock `json:"content,omitempty"`
		Role    string               `json:"role,omitempty"`
	} `json:"message,omitzero"`
	Tools   []string        `json:"tools,omitempty"` // system:init tools list
	Event   json.RawMessage `json:"event,omitempty"` // raw stream event for stream_event type
	Result  string          `json:"result,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	Usage   qwenResultUsage `json:"usage,omitzero"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	DurationMS int `json:"duration_ms,omitempty"`
	NumTurns   int `json:"num_turns,omitempty"`
}

// Stream executes a streaming LLM request via the Qwen Code CLI.
func (d *QwenCliDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	args := d.buildArgs(req, "stream-json")
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)
	configureCommandGrace(cmd, d.graceSec)
	cmd.Stdin = strings.NewReader(d.buildPrompt(req))

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start qwen cli: %w", err)
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

			var evt qwenStreamEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				select {
				case ch <- StreamEvent{Type: "error", Err: NewLLMError("qwen", 0, fmt.Errorf("failed to parse stream event: %w", err))}:
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
				// Raw streaming event — uses the same content_block format as Claude API.
				if se := extractClaudeStreamEvent(evt.Event); se != nil {
					select {
					case ch <- *se:
					case <-ctx.Done():
						return
					}
				}
			case "assistant":
				// Forward full message content for message history.
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
				// Also emit text content events for existing consumers.
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
					TokensUsed:   evt.Usage.InputTokens + evt.Usage.OutputTokens,
					InputTokens:  evt.Usage.InputTokens,
					OutputTokens: evt.Usage.OutputTokens,
				}
				if evt.IsError {
					se.Type = "error"
					errMsg := evt.Result
					if errMsg == "" && evt.Error != nil {
						errMsg = evt.Error.Message
					}
					if errMsg == "" {
						errMsg = "unknown error (empty result)"
					}
					code, sentinel := classifyCliError(errMsg)
					if sentinel != nil {
						se.Err = NewLLMError("qwen", code, sentinel)
					} else {
						se.Err = NewLLMError("qwen", 0, fmt.Errorf("%s", errMsg))
					}
				} else if evt.Result == "" {
					se.Type = "error"
					se.Err = NewLLMError("qwen", 0, fmt.Errorf("response truncated: no result (possible max_turns limit)"))
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
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("qwen", 0, fmt.Errorf("stream read error: %w", err))}:
			case <-ctx.Done():
			}
		}

		// No result event received — check exit code and stderr.
		waitErr := cmd.Wait()
		if waitErr != nil {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = fmt.Sprintf("qwen cli exited with error: %v", waitErr)
			}
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("qwen", 0, fmt.Errorf("%s", errMsg))}:
			case <-ctx.Done():
			}
		} else if stderrBuf.Len() > 0 {
			errMsg := strings.TrimSpace(stderrBuf.String())
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError("qwen", 0, fmt.Errorf("qwen cli stderr: %s", errMsg))}:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}

// Info returns metadata about this driver.
func (d *QwenCliDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         "qwen-cli",
		Provider:     "qwen",
		DefaultModel: d.defaultModel,
		DriverType:   DriverQwenCLI,
	}
}

// buildArgs constructs CLI arguments for a Qwen Code CLI invocation.
// The prompt itself is delivered via stdin (qwen --prompt is deprecated in
// favor of positional arg / stdin). System prompt is still passed via argv
// because qwen CLI has no --system-prompt-file equivalent.
func (d *QwenCliDriver) buildArgs(req LLMRequest, outputFormat string) []string {
	args := []string{"--output-format", outputFormat, "--yolo"}

	if outputFormat == "stream-json" {
		args = append(args, "--include-partial-messages")
	}

	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	model := req.Model
	if model == "" {
		model = d.defaultModel
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	if req.MaxTurns > 0 {
		args = append(args, "--max-session-turns", strconv.Itoa(req.MaxTurns))
	}

	args = append(args, d.extraArgs...)

	return args
}

// buildPrompt constructs the prompt for a CLI Agent invocation.
// CLI Agents manage their own agent loop internally, so each Call is an
// independent task — no cross-invocation history serialization needed.
func (d *QwenCliDriver) buildPrompt(req LLMRequest) string {
	return req.Intent
}
