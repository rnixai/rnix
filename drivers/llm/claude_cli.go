package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rnixai/rnix/vfs"
)

const (
	// DefaultModel is the default Claude model to use.
	DefaultModel = "haiku"
	// DefaultTimeout is the default timeout for a single LLM CLI invocation.
	// Claude Code CLI tasks vary widely (simple: ~5s, complex with tool use: 2-3min).
	// 5 minutes provides headroom for multi-turn agentic tasks.
	DefaultTimeout = 5 * time.Minute
	// DefaultPermissionMode is the default Claude CLI --permission-mode value.
	// "bypassPermissions" matches the daemon-managed, no-TTY agent loop.
	DefaultPermissionMode = "bypassPermissions"
	// claudeCapabilityProbeTimeout caps the help-probe subprocess at 5s so an
	// uninstalled or stuck CLI cannot block the first Call/Stream invocation.
	claudeCapabilityProbeTimeout = 5 * time.Second
)

// validPermissionModes is the whitelist of accepted --permission-mode values.
var validPermissionModes = map[string]bool{
	"bypassPermissions": true,
	"acceptEdits":       true,
	"plan":              true,
	"default":           true,
}

// claudeCapabilities records which optional Claude CLI flags the locally
// installed binary understands. Older CLIs ship without these flags; emitting
// an unknown flag would crash spawn at the very first reasonStep, so we gate
// every optional flag on a positive probe result.
type claudeCapabilities struct {
	partialMessages bool
	addDir          bool
	permissionMode  bool
}

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
	graceSec       int
	cmdBuilder     CommandBuilder
	extraArgs      []string
	permissionMode string
	effort         string

	capsOnce            sync.Once
	caps                claudeCapabilities
	capsProbeDurationMs int64 // capability probe wall-clock duration (ms); set inside capsOnce
	capsWarnedOnce      sync.Once

	resolvedBin       string
	resolvedBinOnce   sync.Once
	resolvedBinErr    error
	fallbackBins      []string
	extendedBinDirsFn func() []string // injectable seam for extended bin dirs (test isolation); defaults to extendedBinDirs
	skipBinResolve    bool
	commandOverridden bool // true when `command` was set via WithCommand; makes cliCommand the sole resolution candidate (no claude/openclaude fallback)
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

// WithGrace sets the grace period (seconds) between SIGTERM and SIGKILL when
// the process is cancelled. 0 = DefaultGracePeriod.
func WithGrace(graceSec int) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.graceSec = graceSec
	}
}

// WithCommand sets the CLI binary name for the driver. Setting it marks the
// command as explicitly overridden: binary resolution will try ONLY this name
// (no fallback to the default claude/openclaude chain), and a missing binary
// surfaces as a clear error rather than silently running a different CLI.
func WithCommand(cmd string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.cliCommand = cmd
		d.commandOverridden = true
	}
}

// WithCommandBuilder sets a custom CommandBuilder for the driver.
// When a custom builder is provided, binary resolution via exec.LookPath
// is skipped — the builder is assumed to handle binary dispatch itself.
func WithCommandBuilder(cb CommandBuilder) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.cmdBuilder = cb
		d.skipBinResolve = true
	}
}

// WithExtraArgs appends additional CLI arguments to every invocation.
func WithExtraArgs(args []string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.extraArgs = args
	}
}

// WithClaudeEffort appends `--effort <value>` to every invocation when set.
// The value is passed through verbatim (no validation/mapping). Older Claude
// CLI versions that do not recognize --effort will error out themselves —
// that is acceptable MVP behavior (see CLAUDE.md "Claude CLI Driver 兼容性约定
// (Epic 40)"; this flag is appended before extraArgs and is not part of the
// default-args sequence, so it does not affect the no-effort path). Empty = no flag.
func WithClaudeEffort(effort string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.effort = effort
	}
}

// WithFallbackBins appends additional candidate binary names to the fallback
// resolution list. The default list is ["claude", "openclaude"]; custom
// candidates are appended after these defaults.
func WithFallbackBins(bins []string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		d.fallbackBins = append(d.fallbackBins, bins...)
	}
}

// WithExtendedBinDirs overrides the extended install directories searched
// during binary resolution (npm global, nvm, bun). It exists primarily as a
// test seam: injecting a function that returns nil decouples resolution from
// the host's real $HOME/$NVM_DIR so PATH-sandboxed tests are not shadowed by a
// genuine claude install. A nil fn is normalized to "return nil" to avoid a
// nil-call panic. Production code never calls this option, so the field keeps
// its constructor default (extendedBinDirs).
func WithExtendedBinDirs(fn func() []string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		if fn == nil {
			fn = func() []string { return nil }
		}
		d.extendedBinDirsFn = fn
	}
}

// WithPermissionMode sets the --permission-mode value the driver passes to
// Claude CLI when the locally installed binary supports the flag. An empty
// mode is treated as "no override" (default applies). Invalid values are
// rejected with a log warning; the default is preserved.
func WithPermissionMode(mode string) ClaudeCliOption {
	return func(d *ClaudeCliDriver) {
		if mode == "" {
			return
		}
		if !validPermissionModes[mode] {
			log.Printf("[llm] warning: invalid permission_mode %q ignored (valid: bypassPermissions, acceptEdits, plan, default)", mode)
			return
		}
		d.permissionMode = mode
	}
}

// NewClaudeCliDriver creates a new ClaudeCliDriver with the given options.
func NewClaudeCliDriver(opts ...ClaudeCliOption) *ClaudeCliDriver {
	d := &ClaudeCliDriver{
		cliCommand:        "claude",
		defaultModel:      DefaultModel,
		defaultTimeout:    DefaultTimeout,
		cmdBuilder:        defaultCommandBuilder,
		permissionMode:    DefaultPermissionMode,
		fallbackBins:      []string{"claude", "openclaude"},
		extendedBinDirsFn: extendedBinDirs,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// UsesSkillsBundle marks this driver as SkillsBundleCapable — it consumes
// req.Skills directly by materializing a content-addressed bundle and passing
// it via --add-dir. The VFS layer will NOT merge skill bodies into
// req.SystemPrompt for this driver.
func (d *ClaudeCliDriver) UsesSkillsBundle() {}

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
	if d.resolvedBinErr != nil {
		return nil, NewLLMError("claude", 0, d.resolvedBinErr)
	}
	if sysPromptFile != "" {
		defer func() { _ = os.Remove(sysPromptFile) }()
	}
	cmd := d.cmdBuilder(ctx, d.effectiveBinary(), args...)
	configureCommandGrace(cmd, d.graceSec)

	prompt := d.buildPrompt(req)
	stdinPipe, pipeErr := cmd.StdinPipe()
	if pipeErr != nil {
		return nil, NewLLMError("claude", 0, fmt.Errorf("create stdin pipe: %w", pipeErr))
	}
	go writeStdinSafe(stdinPipe, prompt)()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 56.3 raw capture：在 cmd.Run 之前填 Request（argv 用 effectiveBinary 与
	// 实际传入 cmdBuilder 的二进制一致；stdin = driver 构造的 prompt 字符串，
	// 无需 tee stdinPipe，prompt 局部变量已是同源）。Run 后填 Response。
	sink := rawSinkFromContext(ctx)
	var rawCap *vfs.RawCapture
	if sink != nil {
		rawCap = newCLIRawCapture()
		fillCLIRequest(rawCap, d.effectiveBinary(), args, prompt, nil)
		sink.set(rawCap)
	}

	err = cmd.Run()
	if sink != nil && rawCap != nil {
		fillCLIResponse(rawCap, stdout.String(), stderr.String(), processExitCode(cmd))
		sink.set(rawCap)
	}
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
		ID      string               `json:"id,omitempty"`
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
//
// Timeout semantics (see streamtimeout.go): the timeout value applies as an
// idle timeout — cancelled only when no event flows from the claude subprocess
// for `timeout`. Long agent loops with steady event output will not be killed
// by wall-clock.
func (d *ClaudeCliDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, idle, cancel := NewIdleTimer(ctx, timeout)

	args, sysPromptFile, err := d.buildArgs(req, "stream-json")
	if err != nil {
		cancel()
		return nil, NewLLMError("claude", 0, err)
	}
	if d.resolvedBinErr != nil {
		cancel()
		return nil, NewLLMError("claude", 0, d.resolvedBinErr)
	}
	cmd := d.cmdBuilder(ctx, d.effectiveBinary(), args...)
	configureCommandGrace(cmd, d.graceSec)

	prompt := d.buildPrompt(req)
	stdinPipe, pipeErr := cmd.StdinPipe()
	if pipeErr != nil {
		cancel()
		if sysPromptFile != "" {
			_ = os.Remove(sysPromptFile)
		}
		return nil, fmt.Errorf("failed to create stdin pipe: %w", pipeErr)
	}

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

	go writeStdinSafe(stdinPipe, prompt)()

	// 56.3 raw capture（Stream 路径，裁决 7）：tee stdoutPipe 累积原样
	// stream-json 字节；scanner 改读 tee reader 既不影响解析又能保留底层字节。
	sink := rawSinkFromContext(ctx)
	var rawCap *vfs.RawCapture
	var rawStdoutBuf bytes.Buffer
	scannerSrc := io.Reader(stdoutPipe)
	if sink != nil {
		rawCap = newCLIRawCapture()
		fillCLIRequest(rawCap, d.effectiveBinary(), args, prompt, nil)
		sink.set(rawCap)
		scannerSrc = io.TeeReader(stdoutPipe, &rawStdoutBuf)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	parser := newClaudeStreamParser()

	go func() {
		defer close(ch)
		defer cancel()
		if sysPromptFile != "" {
			defer func() { _ = os.Remove(sysPromptFile) }()
		}

		scanner := newStreamScanner(scannerSrc)
		for scanner.Scan() {
			idle.Reset()
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
				// Raw Claude API streaming event — extract tool_use, thinking, text_delta blocks
				if se := parser.extractStreamEvent(evt.Event); se != nil {
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
				// When the partial-messages stream already delivered text via
				// text_delta, skip the legacy block re-emit to avoid doubling
				// accumulated content. Older CLIs (no text_delta) still rely
				// on this path for any text at all.
				if !parser.sawTextDelta(evt.Message.ID) {
					for _, c := range evt.Message.Content {
						if c.Type == "text" {
							select {
							case ch <- StreamEvent{Type: "content", Content: c.Text}:
							case <-ctx.Done():
								return
							}
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
				// 56.3 raw capture：result 事件路径，scanner 已读完 stream-json，
				// rawStdoutBuf 已累积原样字节；cmd.Wait 拿到 exit code → 填 Response。
				if sink != nil && rawCap != nil {
					fillCLIResponse(rawCap, rawStdoutBuf.String(), stderrBuf.String(), processExitCode(cmd))
					sink.set(rawCap)
				}
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
		// 56.3 raw capture：no-result 路径同样填 Response（即便 stream 提前断流，
		// 已累积部分字节也归档；exit code 经 cmd.Wait 已可用）。
		if sink != nil && rawCap != nil {
			fillCLIResponse(rawCap, rawStdoutBuf.String(), stderrBuf.String(), processExitCode(cmd))
			sink.set(rawCap)
		}
	}()

	return ch, nil
}

// Info returns metadata about this driver.
func (d *ClaudeCliDriver) Info() DriverInfo {
	return DriverInfo{
		Name:            "claude-cli",
		Provider:        "claude",
		DefaultModel:    d.defaultModel,
		DriverType:      DriverClaudeCLI,
		ReasoningEffort: d.effort,
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
	caps := d.ensureCapabilities()

	args := []string{"--print", "-", "--output-format", outputFormat}

	if outputFormat == "stream-json" {
		args = append(args, "--verbose")
		if caps.partialMessages {
			args = append(args, "--include-partial-messages")
		}
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

	if caps.permissionMode && d.permissionMode != "" {
		args = append(args, "--permission-mode", d.permissionMode)
	}

	// R5: materialize a content-addressed skills bundle so Claude CLI can
	// discover them via --add-dir. Symlink to source skill dirs for stable
	// paths across reasonSteps (prompt cache prefix).
	bundleRoot, err := prepareClaudeSkillsBundle(req.ProjectDir, req.Skills)
	if err != nil {
		return nil, sysPromptFile, fmt.Errorf("prepare skills bundle: %w", err)
	}
	if bundleRoot != "" {
		if caps.addDir {
			args = append(args, "--add-dir", bundleRoot)
		} else {
			log.Printf("[llm] debug: claude-cli %s lacks --add-dir; skills bundle at %s left unannounced (skills are still injected via system prompt)", d.cliCommand, bundleRoot)
		}
	}

	// Reasoning effort: appended after built-in args and before extraArgs, only
	// when configured (passthrough, no validation). Keeps the default-args
	// sequence unchanged for the no-effort path (TestClaudeCliDriver_Call_DefaultArgs).
	if d.effort != "" {
		args = append(args, "--effort", d.effort)
	}

	args = append(args, d.extraArgs...)

	return args, sysPromptFile, nil
}

// buildPrompt constructs the prompt for a CLI Agent invocation.
// CLI Agents manage their own agent loop internally, so each Call is an
// independent task — no cross-invocation history serialization needed.
//
// When req.ProjectDir or req.Skills is non-empty, a # Instructions prefix
// is injected so the model knows its working directory and available skills
// without an extra tool call. Tools are NOT copied — Claude CLI discovers
// them via --add-dir and built-in tools.
func (d *ClaudeCliDriver) buildPrompt(req LLMRequest) string {
	if req.ProjectDir == "" && len(req.Skills) == 0 {
		return req.Intent
	}
	var sb strings.Builder
	sb.WriteString("# Instructions\n\n")
	if req.ProjectDir != "" {
		fmt.Fprintf(&sb, "Working directory: %s\n\n", req.ProjectDir)
	}
	if len(req.Skills) > 0 {
		names := make([]string, len(req.Skills))
		for i, s := range req.Skills {
			names[i] = s.Name
		}
		if req.ProjectDir != "" {
			fmt.Fprintf(&sb, "Accessible skill bundle: %s/.claude/skills/%s\n\n", req.ProjectDir, strings.Join(names, ", "))
		} else {
			fmt.Fprintf(&sb, "Accessible skill bundle: %s\n\n", strings.Join(names, ", "))
		}
	}
	sb.WriteString("# User request\n\n")
	sb.WriteString(req.Intent)
	return sb.String()
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

// ensureCapabilities lazily probes the Claude CLI binary's flag support and
// caches the result on the driver. Multiple concurrent Call/Stream invocations
// share the same probe via sync.Once. The probe runs through cmdBuilder so
// tests can mock its output.
func (d *ClaudeCliDriver) ensureCapabilities() claudeCapabilities {
	d.capsOnce.Do(func() {
		_ = d.resolveBinaryIfNeeded()
		start := time.Now()
		d.caps = d.probeCapabilities()
		d.capsProbeDurationMs = time.Since(start).Milliseconds()
	})
	return d.caps
}

// probeCapabilities executes `<cliCommand> -p --help` with a 5s deadline and
// scans stdout for known flag names. Any failure (timeout, missing binary,
// non-zero exit, parse error) collapses to a zero-value claudeCapabilities so
// downstream buildArgs falls back to the most conservative argv shape.
func (d *ClaudeCliDriver) probeCapabilities() claudeCapabilities {
	ctx, cancel := context.WithTimeout(context.Background(), claudeCapabilityProbeTimeout)
	defer cancel()

	cmd := d.cmdBuilder(ctx, d.effectiveBinary(), "-p", "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		d.warnCapabilityProbeFailure(fmt.Sprintf("probe exec failed: %v (stderr=%q)", err, strings.TrimSpace(stderr.String())))
		return claudeCapabilities{}
	}
	if stdout.Len() > scannerMaxSize {
		d.warnCapabilityProbeFailure(fmt.Sprintf("probe output exceeds %d bytes (got %d)", scannerMaxSize, stdout.Len()))
		return claudeCapabilities{}
	}

	out := stdout.String()
	return claudeCapabilities{
		partialMessages: strings.Contains(out, "--include-partial-messages"),
		addDir:          strings.Contains(out, "--add-dir"),
		permissionMode:  strings.Contains(out, "--permission-mode"),
	}
}

// warnCapabilityProbeFailure logs a single warning per driver instance when
// the capability probe fails. Keeping the warning behind sync.Once prevents
// log flooding when many sub-agents spawn concurrently.
func (d *ClaudeCliDriver) warnCapabilityProbeFailure(reason string) {
	d.capsWarnedOnce.Do(func() {
		log.Printf("[llm] warning: claude-cli capability probe (%s) failed: %s. Falling back to conservative flag set.", d.cliCommand, reason)
	})
}

// binaryCandidates returns the ordered list of binary names to try during
// resolution. An explicit `command` override (via WithCommand) is authoritative:
// it becomes the sole candidate, with no fallback to the default
// claude/openclaude chain. Without an override, the default fallbackBins apply.
// resolveClaudeBinary and DriverMeta share this single source of truth so the
// resolved binary and the reported candidate list never drift.
func (d *ClaudeCliDriver) binaryCandidates() []string {
	if d.commandOverridden {
		return []string{d.cliCommand}
	}
	return d.fallbackBins
}

// resolveClaudeBinary searches for a usable Claude-compatible CLI binary.
// It tries each candidate from binaryCandidates() via exec.LookPath first, then
// checks extended install directories (npm global, nvm, bun). The first match is
// cached in d.resolvedBin. When an explicit command override is not found, the
// error names the override and never falls back to claude/openclaude.
func (d *ClaudeCliDriver) resolveClaudeBinary() (string, error) {
	var tried []string
	for _, bin := range d.binaryCandidates() {
		tried = append(tried, bin)
		if p, err := exec.LookPath(bin); err == nil {
			return p, nil
		}
		for _, dir := range d.extendedBinDirsFn() {
			full := filepath.Join(dir, bin)
			if fi, err := os.Stat(full); err == nil && !fi.IsDir() && fi.Mode().Perm()&0o111 != 0 {
				return full, nil
			}
		}
	}
	if d.commandOverridden {
		return "", fmt.Errorf("claude-cli command override %q not found in PATH or extended install dirs", d.cliCommand)
	}
	return "", fmt.Errorf("no claude-compatible CLI found in PATH: tried %v", tried)
}

// resolveBinaryIfNeeded runs resolveClaudeBinary exactly once (via sync.Once)
// and caches the result. Returns the cached error (nil on success).
func (d *ClaudeCliDriver) resolveBinaryIfNeeded() error {
	if d.skipBinResolve {
		return nil
	}
	d.resolvedBinOnce.Do(func() {
		d.resolvedBin, d.resolvedBinErr = d.resolveClaudeBinary()
	})
	return d.resolvedBinErr
}

// extendedBinDirs returns non-standard directories where CLI tools like
// claude/openclaude might be installed (npm global, nvm, bun).
func extendedBinDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var dirs []string

	// ~/.local/bin (common for pipx, npm global on some systems)
	dirs = append(dirs, filepath.Join(home, ".local", "bin"))

	// $NVM_DIR/versions/node/*/bin — pick the newest version directory
	nvmDir := os.Getenv("NVM_DIR")
	if nvmDir == "" {
		nvmDir = filepath.Join(home, ".nvm")
	}
	if nvmDir != "" {
		pattern := filepath.Join(nvmDir, "versions", "node", "*", "bin")
		if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			dirs = append(dirs, matches[0])
		}
	}

	// ~/.bun/bin
	dirs = append(dirs, filepath.Join(home, ".bun", "bin"))

	return dirs
}

// writeStdinSafe returns a closure that writes prompt to w and closes it,
// silently swallowing EPIPE/ErrClosed (CLI fast-exit scenarios). Unexpected
// errors are logged for debugging; the CLI's stderr/exit code is the primary
// error channel.
func writeStdinSafe(w io.WriteCloser, prompt string) func() {
	return func() {
		defer w.Close()
		_, err := io.WriteString(w, prompt)
		if err != nil && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, os.ErrClosed) {
			log.Printf("[llm] warning: unexpected stdin write error (non-EPIPE): %v", err)
		}
	}
}

// effectiveBinary returns the binary name to use for exec: the resolved
// absolute path if available, otherwise the original cliCommand.
func (d *ClaudeCliDriver) effectiveBinary() string {
	if d.resolvedBin != "" {
		return d.resolvedBin
	}
	return d.cliCommand
}

// DriverMeta returns runtime metadata for observability: resolved binary path,
// permission mode, capability flags, the candidate binary list, and the
// capability-probe duration. It triggers lazy binary resolution and capability
// probing if not yet executed.
func (d *ClaudeCliDriver) DriverMeta() map[string]string {
	_ = d.resolveBinaryIfNeeded()
	caps := d.ensureCapabilities()
	return map[string]string{
		"resolved_bin":         d.resolvedBin,
		"permission_mode":      d.permissionMode,
		"cap_partial_messages": strconv.FormatBool(caps.partialMessages),
		"cap_add_dir":          strconv.FormatBool(caps.addDir),
		"cap_permission_mode":  strconv.FormatBool(caps.permissionMode),
		"fallback_candidates":  strings.Join(d.binaryCandidates(), ","),
		"probe_duration_ms":    strconv.FormatInt(d.capsProbeDurationMs, 10),
	}
}

// claudeStreamParser converts raw Claude API stream events into driver-level
// StreamEvent values while tracking which assistant message has already
// emitted text_delta increments. The Stream goroutine creates one parser per
// invocation; instances are not safe for concurrent use.
type claudeStreamParser struct {
	currentMessageID string
	hadTextDelta     bool
}

// newClaudeStreamParser returns a parser ready for a fresh stream.
func newClaudeStreamParser() *claudeStreamParser {
	return &claudeStreamParser{}
}

// sawTextDelta reports whether text_delta events have already been forwarded
// for the assistant message identified by id. The Stream goroutine consults
// this to decide whether the trailing assistant block should re-emit text
// content (legacy CLI without partial messages) or stay silent (new CLI).
func (p *claudeStreamParser) sawTextDelta(id string) bool {
	if p.currentMessageID == "" || p.currentMessageID != id {
		// Either the parser never observed message_start (older CLI / qwen)
		// or the assistant block belongs to a different message.
		return false
	}
	return p.hadTextDelta
}

// extractStreamEvent decodes a single inner Claude API stream frame and
// produces the matching driver event. It also updates parser state for
// message_start (new active message) and text_delta (mark message as having
// emitted increments).
func (p *claudeStreamParser) extractStreamEvent(raw json.RawMessage) *StreamEvent {
	if len(raw) == 0 {
		return nil
	}
	var event struct {
		Type    string `json:"type"`
		Message struct {
			ID string `json:"id"`
		} `json:"message,omitzero"`
		ContentBlock struct {
			Type string `json:"type"`
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"content_block,omitzero"`
		Delta struct {
			Type        string `json:"type"`
			PartialJSON string `json:"partial_json"`
			Thinking    string `json:"thinking"`
			Text        string `json:"text"`
		} `json:"delta,omitzero"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil
	}

	switch event.Type {
	case "message_start":
		p.currentMessageID = event.Message.ID
		p.hadTextDelta = false
		return nil
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
		case "text_delta":
			p.hadTextDelta = true
			return &StreamEvent{
				Type:    "content",
				Content: event.Delta.Text,
			}
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

// extractClaudeStreamEvent extracts tool_use and thinking blocks from a raw Claude API stream event.
// Returns nil for events that don't map to a driver-level event (e.g., ping, message_start, text_delta).
//
// Deprecated: Internal Claude streaming uses claudeStreamParser to track
// per-message text_delta state. This stateless wrapper exists for the qwen-cli
// driver, which shares the Claude stream-json schema but historically derived
// text from the trailing assistant block; it intentionally swallows text_delta
// so the qwen flow keeps a single source of text events.
func extractClaudeStreamEvent(raw json.RawMessage) *StreamEvent {
	se := newClaudeStreamParser().extractStreamEvent(raw)
	if se != nil && se.Type == "content" {
		return nil
	}
	return se
}
