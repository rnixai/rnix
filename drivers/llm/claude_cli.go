package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	StopReason   string  `json:"stop_reason,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	Usage        struct {
		InputTokens          int `json:"input_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
		OutputTokens         int `json:"output_tokens"`
	} `json:"usage,omitzero"`
	CostUSD      float64 `json:"cost_usd"`
	DurationMS   int     `json:"duration_ms"`
	NumTurns     int     `json:"num_turns"`
	SessionID    string  `json:"session_id"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

// mergeClaudeUsage returns the effective token / cost values, preferring the
// structured Usage block (newer CLI format) over the flat legacy fields.
func (c *claudeCliResponse) mergeClaudeUsage() (input, cached, output int, cost float64) {
	input = c.Usage.InputTokens
	cached = c.Usage.CacheReadInputTokens
	output = c.Usage.OutputTokens
	if input == 0 && c.InputTokens != 0 {
		input = c.InputTokens
	}
	if output == 0 && c.OutputTokens != 0 {
		output = c.OutputTokens
	}
	cost = c.TotalCostUSD
	if cost == 0 && c.CostUSD != 0 {
		cost = c.CostUSD
	}
	return
}

// Call executes a synchronous LLM request via the Claude Code CLI.
func (d *ClaudeCliDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args, sysPromptFile, err := d.buildArgs(req, "json")
	if err != nil {
		return nil, NewLLMError("claude", 0, err)
	}
	if sysPromptFile != "" {
		defer func() { _ = os.Remove(sysPromptFile) }()
	}
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)
	cmd.Stdin = strings.NewReader(d.buildPrompt(req))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, NewLLMError("claude", 0, ErrTimeout)
	}

	// Try parsing stdout JSON even when exit code is non-zero
	var cliResp claudeCliResponse
	if parseErr := json.Unmarshal(stdout.Bytes(), &cliResp); parseErr == nil {
		// max_turns: agentic loop exhausted its budget. Return a typed sentinel so
		// callers can distinguish this from misconfiguration / true errors.
		if isMaxTurnsResult(&cliResp) {
			return nil, NewLLMError("claude", 0, ErrMaxTurns)
		}
		if cliResp.IsError {
			errMsg := cliResp.Result
			if errMsg == "" {
				errMsg = "unknown error (empty result)"
			}
			if required, loginURL := detectLoginRequired(cliResp.Result, stderr.String()); required {
				e := NewLLMError("claude", 401, ErrLoginRequired)
				if loginURL != "" {
					e.Meta = map[string]string{"login_url": loginURL}
				}
				return nil, e
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
		input, cached, output, cost := cliResp.mergeClaudeUsage()
		return &LLMResponse{
			Content:           cliResp.Result,
			TokensUsed:        input + output,
			InputTokens:       input,
			OutputTokens:      output,
			CachedInputTokens: cached,
			CostUSD:           cost,
			StopReason:        cliResp.StopReason,
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
	StopReason   string          `json:"stop_reason,omitempty"`
	TotalCostUSD float64         `json:"total_cost_usd,omitempty"`
	Usage        struct {
		InputTokens          int `json:"input_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
		OutputTokens         int `json:"output_tokens"`
	} `json:"usage,omitzero"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	DurationMS   int     `json:"duration_ms,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
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

	args, sysPromptFile, err := d.buildArgs(req, "stream-json")
	if err != nil {
		cancel()
		return nil, NewLLMError("claude", 0, err)
	}
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)
	cmd.Stdin = strings.NewReader(d.buildPrompt(req))

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		if sysPromptFile != "" {
			_ = os.Remove(sysPromptFile)
		}
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		if sysPromptFile != "" {
			_ = os.Remove(sysPromptFile)
		}
		return nil, fmt.Errorf("failed to start claude cli: %w", err)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer cancel()
		if sysPromptFile != "" {
			defer func() { _ = os.Remove(sysPromptFile) }()
		}

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
				input := evt.Usage.InputTokens
				cached := evt.Usage.CacheReadInputTokens
				output := evt.Usage.OutputTokens
				if input == 0 && evt.InputTokens != 0 {
					input = evt.InputTokens
				}
				if output == 0 && evt.OutputTokens != 0 {
					output = evt.OutputTokens
				}
				cost := evt.TotalCostUSD
				if cost == 0 && evt.CostUSD != 0 {
					cost = evt.CostUSD
				}
				se := StreamEvent{
					Type:              "done",
					Content:           evt.Result,
					TokensUsed:        input + output,
					InputTokens:       input,
					OutputTokens:      output,
					CachedInputTokens: cached,
					CostUSD:           cost,
					StopReason:        evt.StopReason,
				}
				if isMaxTurnsStreamEvent(evt) {
					se.Type = "error"
					se.Err = NewLLMError("claude", 0, ErrMaxTurns)
				} else if evt.IsError {
					se.Type = "error"
					errMsg := evt.Result
					if errMsg == "" {
						errMsg = "unknown error (empty result)"
					}
					// Stream mode: only inspect evt.Result (reading stderrBuf here
					// would race the subprocess still writing to stderr).
					if required, loginURL := detectLoginRequired(evt.Result, ""); required {
						e := NewLLMError("claude", 401, ErrLoginRequired)
						if loginURL != "" {
							e.Meta = map[string]string{"login_url": loginURL}
						}
						se.Err = e
					} else {
						code, sentinel := classifyCliError(errMsg)
						if sentinel != nil {
							se.Err = NewLLMError("claude", code, sentinel)
						} else {
							se.Err = NewLLMError("claude", 0, fmt.Errorf("%s", errMsg))
						}
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
// The prompt itself is NOT embedded in argv — callers pass it via stdin using
// the "--print -" convention. Returns the args slice, the path to a temp file
// holding the system prompt (empty if none), and any setup error.
//
// The caller MUST defer os.Remove(sysPromptFile) when it is non-empty,
// after cmd.Wait() / cmd.Run() returns.
func (d *ClaudeCliDriver) buildArgs(req LLMRequest, outputFormat string) ([]string, string, error) {
	args := []string{"--print", "-", "--output-format", outputFormat}

	if outputFormat == "stream-json" {
		args = append(args, "--verbose", "--include-partial-messages")
	}

	var sysPromptFile string
	if req.SystemPrompt != "" {
		f, err := os.CreateTemp("", "rnix-claude-sys-*.md")
		if err != nil {
			return nil, "", fmt.Errorf("create system prompt tempfile: %w", err)
		}
		if _, err := f.WriteString(req.SystemPrompt); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return nil, "", fmt.Errorf("write system prompt tempfile: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return nil, "", fmt.Errorf("close system prompt tempfile: %w", err)
		}
		sysPromptFile = f.Name()
		args = append(args, "--append-system-prompt-file", sysPromptFile)
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

	return args, sysPromptFile, nil
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

// isMaxTurnsResult reports whether a CLI result signals an agentic loop
// exhausted its turn budget. Matches either subtype=error_max_turns (older
// schema) or stop_reason=max_turns (newer schema).
func isMaxTurnsResult(r *claudeCliResponse) bool {
	if strings.EqualFold(r.Subtype, "error_max_turns") {
		return true
	}
	if strings.EqualFold(r.StopReason, "max_turns") {
		return true
	}
	return false
}

// isMaxTurnsStreamEvent applies the same logic to a stream `result` event.
func isMaxTurnsStreamEvent(e claudeStreamEvent) bool {
	if strings.EqualFold(e.Subtype, "error_max_turns") {
		return true
	}
	if strings.EqualFold(e.StopReason, "max_turns") {
		return true
	}
	return false
}

var (
	// claudeAuthRequiredRegex matches phrases the Claude CLI uses when the
	// user is not authenticated. Adapted from paperclip's claude-local
	// adapter (packages/adapters/claude-local/src/server/parse.ts).
	claudeAuthRequiredRegex = regexp.MustCompile(
		`(?i)not\s+logged\s+in|please\s+log\s+in|please\s+run\s+'?claude\s+login'?|login\s+required|requires\s+login|unauthorized|authentication\s+required`,
	)
	// claudeURLRegex extracts the first HTTP(S) URL from text; used to
	// surface a clickable login URL in error messages.
	claudeURLRegex = regexp.MustCompile(
		`https?://[^\s'"` + "`" + `<>()\[\]{};,!?]+[^\s'"` + "`" + `<>()\[\]{};,!.?:]+`,
	)
)

// detectLoginRequired scans the CLI output (result + stderr) for phrases
// indicating the user must run `claude login`. Returns (true, loginURL) on
// match; loginURL is empty when no URL is found.
func detectLoginRequired(result, stderr string) (bool, string) {
	combined := result + "\n" + stderr
	if !claudeAuthRequiredRegex.MatchString(combined) {
		return false, ""
	}
	matches := claudeURLRegex.FindAllString(combined, -1)
	for _, url := range matches {
		lower := strings.ToLower(url)
		if strings.Contains(lower, "claude") || strings.Contains(lower, "anthropic") || strings.Contains(lower, "auth") {
			return true, url
		}
	}
	if len(matches) > 0 {
		return true, matches[0]
	}
	return true, ""
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
