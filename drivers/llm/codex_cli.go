package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rnixai/rnix/vfs"
)

const (
	// CodexDefaultModel is the default Codex model to use.
	CodexDefaultModel = "codex-mini-latest"
	// CodexDefaultSandboxMode preserves the old `--full-auto` exec sandbox
	// behavior without using the deprecated alias.
	CodexDefaultSandboxMode = "workspace-write"
	// CodexDefaultTimeout is the default timeout for a single Codex CLI invocation.
	// Codex exec tasks with tool use can take several minutes.
	CodexDefaultTimeout = 5 * time.Minute
)

// CodexCliDriver implements LLMDriver by invoking the Codex CLI (codex exec).
type CodexCliDriver struct {
	cliCommand     string
	defaultModel   string
	defaultTimeout time.Duration
	graceSec       int
	cmdBuilder     CommandBuilder
	extraArgs      []string
	effort         string
	sandboxMode    string
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

// CodexWithGrace sets the grace period (seconds) between SIGTERM and SIGKILL.
// 0 = DefaultGracePeriod.
func CodexWithGrace(graceSec int) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.graceSec = graceSec
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

// CodexWithReasoningEffort appends `-c model_reasoning_effort=<value>` to every
// invocation when set. The value is passed through verbatim (no validation/
// mapping); Codex itself validates. Appended before extraArgs and the prompt.
// Empty = no flag.
func CodexWithReasoningEffort(effort string) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.effort = effort
	}
}

// CodexWithSandboxMode appends `--sandbox <mode>` to every invocation when set.
// Empty = default workspace-write in buildArgs.
func CodexWithSandboxMode(mode string) CodexCliOption {
	return func(d *CodexCliDriver) {
		d.sandboxMode = mode
	}
}

// resolveEffort returns the per-request reasoning effort override, or the
// driver instance default (d.effort) when the request leaves it empty.
// Value is passed through verbatim (no validation/mapping). Computed per call
// (buildArgs already receives req).
func (d *CodexCliDriver) resolveEffort(req LLMRequest) string {
	if req.ReasoningEffort != "" {
		return req.ReasoningEffort
	}
	return d.effort
}

func (d *CodexCliDriver) resolveSandboxMode() string {
	if d.sandboxMode != "" {
		return d.sandboxMode
	}
	return CodexDefaultSandboxMode
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
		// Codex 0.14x 实测 schema（调查 codex-cli-observability-parity）：
		// command_execution 的输出字段是 aggregated_output（非 output），
		// 另携带 exit_code；collab_tool_call（multi_agent_v2）携带 tool
		// （wait/spawn_agent/...）；error item 携带 message。
		AggregatedOutput string `json:"aggregated_output,omitempty"`
		ExitCode         *int   `json:"exit_code,omitempty"`
		Tool             string `json:"tool,omitempty"`
		Message          string `json:"message,omitempty"`
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
	configureCommandGrace(cmd, d.graceSec)
	configureCommandDir(cmd, req.ProjectDir)
	configureCommandRnixParentEnv(cmd, req)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 56.3 raw capture: 在 buildArgs 之后、cmd.Run() 之前填 Request（保留请求形态
	// 即便后续执行失败）；cmd.Run 返回后再填 Response（stdout/stderr 此时已为终态）。
	// codex-cli 的 prompt 在 argv 里传（buildArgs 末尾 append），故 stdin="" 。
	sink := rawSinkFromContext(ctx)
	var rawCap *vfs.RawCapture
	if sink != nil {
		rawCap = newCLIRawCapture()
		fillCLIRequest(rawCap, d.cliCommand, args, "", nil)
		sink.set(rawCap)
	}

	err := cmd.Run()
	if sink != nil && rawCap != nil {
		fillCLIResponse(rawCap, stdout.String(), stderr.String(), processExitCode(cmd))
		sink.set(rawCap)
	}
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
//
// Timeout semantics (see streamtimeout.go): the timeout value applies as an
// idle timeout — the context is cancelled only when no event flows from the
// codex subprocess for `timeout`. Long agent loops that continuously emit
// tool calls and messages will NOT be killed by wall-clock elapsed time.
func (d *CodexCliDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, idle, cancel := NewIdleTimer(ctx, timeout)

	args := d.buildArgs(req, true)
	cmd := d.cmdBuilder(ctx, d.cliCommand, args...)
	configureCommandGrace(cmd, d.graceSec)
	configureCommandDir(cmd, req.ProjectDir)
	configureCommandRnixParentEnv(cmd, req)

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

	// 56.3 raw capture（Stream 路径，裁决 7 时序铁律）：
	//   1. 先填 Request（buildArgs 之后即可，prompt 在 argv 里 codex stdin="" ）
	//   2. tee stdoutPipe → rawStdoutBuf 累积原样 stream-json 字节
	//   3. goroutine 末尾（scanner 循环之后、close(ch) 触发之前）显式 cmd.Wait
	//      取 exit code → fillCLIResponse → sink.set；不塞 defer（LIFO 易错）
	sink := rawSinkFromContext(ctx)
	var rawCap *vfs.RawCapture
	var rawStdoutBuf bytes.Buffer
	scannerSrc := io.Reader(stdoutPipe)
	if sink != nil {
		rawCap = newCLIRawCapture()
		fillCLIRequest(rawCap, d.cliCommand, args, "", nil)
		sink.set(rawCap)
		scannerSrc = io.TeeReader(stdoutPipe, &rawStdoutBuf)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer cancel()

		var lastUsage struct {
			inputTokens       int
			outputTokens      int
			cachedInputTokens int
		}
		var lastAgentMessage string
		var sawTurnCompleted bool
		var scanErr error

		scanner := newStreamScanner(scannerSrc)
		for scanner.Scan() {
			idle.Reset()
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
				if evt.Item == nil {
					continue
				}
				switch evt.Item.Type {
				case "command_execution":
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
				case "collab_tool_call":
					// multi_agent_v2 协作工具（wait/spawn_agent/close_agent/...）。
					// 作为 tool_call 接入使子代理编排在 steps/events 可见，且
					// started 帧刷新 kernel heartbeat，避免长 wait 期间的 STALL
					// 误报（调查 codex-cli-observability-parity R2/R3）。
					tool := evt.Item.Tool
					if tool == "" {
						tool = "collab"
					}
					se := StreamEvent{
						Type:    "tool_call",
						Content: "started",
						Data: map[string]any{
							"tool":    tool,
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
					// subtype 标记消息级 content：kernel 据此落 events.jsonl，
					// 而 API driver 的 token 级 content delta 仅刷 heartbeat。
					select {
					case ch <- StreamEvent{Type: "content", Content: evt.Item.Text, Data: map[string]any{"subtype": "agent_message"}}:
					case <-ctx.Done():
						return
					}
				case "command_execution":
					// "result" 键对齐 kernel 的回填白名单（observe.go completed
					// 分支读 result/tool_result）；输出取实测 schema 的
					// aggregated_output。
					data := map[string]any{
						"tool":    "shell",
						"command": evt.Item.Command,
						"call_id": evt.Item.ID,
						"result":  evt.Item.AggregatedOutput,
						"subtype": "completed",
					}
					if evt.Item.ExitCode != nil {
						data["exit_code"] = *evt.Item.ExitCode
					}
					se := StreamEvent{
						Type:    "tool_call",
						Content: "completed",
						Data:    data,
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
				case "collab_tool_call":
					tool := evt.Item.Tool
					if tool == "" {
						tool = "collab"
					}
					se := StreamEvent{
						Type:    "tool_call",
						Content: "completed",
						Data: map[string]any{
							"tool":    tool,
							"call_id": evt.Item.ID,
							"result":  evt.Item.Status,
							"subtype": "completed",
						},
					}
					select {
					case ch <- se:
					case <-ctx.Done():
						return
					}
				default:
					// 未知 item 类型不再静默丢弃：以通用 "item" 事件透传，
					// kernel 落 events.jsonl 留痕（如 error item 承载的
					// multi_agent_v2 警告此前完全不可见）。
					data := map[string]any{
						"item_type": evt.Item.Type,
						"call_id":   evt.Item.ID,
					}
					if evt.Item.Text != "" {
						data["text"] = evt.Item.Text
					}
					if evt.Item.Message != "" {
						data["message"] = evt.Item.Message
					}
					select {
					case ch <- StreamEvent{Type: "item", Content: evt.Item.Type, Data: data}:
					case <-ctx.Done():
						return
					}
				}

			case "turn.completed":
				sawTurnCompleted = true
				if evt.Usage != nil {
					lastUsage.inputTokens += evt.Usage.InputTokens
					lastUsage.outputTokens += evt.Usage.OutputTokens
					lastUsage.cachedInputTokens += evt.Usage.CachedInputTokens
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
			scanErr = err
		}

		// Emit done event with the last agent message and accumulated usage.
		se := StreamEvent{
			Type:              "done",
			Content:           lastAgentMessage,
			TokensUsed:        lastUsage.inputTokens + lastUsage.outputTokens,
			InputTokens:       lastUsage.inputTokens,
			OutputTokens:      lastUsage.outputTokens,
			CachedInputTokens: lastUsage.cachedInputTokens,
		}
		if IsStreamTimeout(ctx) {
			_ = cmd.Wait()
			se.Type = "error"
			se.Err = NewLLMError("codex", 0, fmt.Errorf("%w%s",
				ErrTimeout, cliFailureDetail(stderrBuf.String(), processExitCode(cmd))))
		} else if scanErr != nil {
			_ = cmd.Wait()
			se.Type = "error"
			se.Err = NewLLMError("codex", 0, fmt.Errorf("stream read error: %w%s",
				scanErr, cliFailureDetail(stderrBuf.String(), processExitCode(cmd))))
		} else if lastAgentMessage == "" {
			// 56.7 (G5): Wait BEFORE composing the error — the scanner loop is
			// done so stdout is drained and Wait is safe (claude no-result 路径
			// 范本); reading stderrBuf without Wait would race the subprocess.
			// The done path below keeps its original send-then-Wait order so
			// normal completion is never delayed. Double Wait is harmless
			// ("Wait was already called" is discarded via _).
			_ = cmd.Wait()
			se.Type = "error"
			se.Err = NewLLMError("codex", 0, fmt.Errorf("response truncated: no agent message received%s",
				cliFailureDetail(stderrBuf.String(), processExitCode(cmd))))
		} else if !sawTurnCompleted {
			_ = cmd.Wait()
			se.Type = "error"
			se.Err = NewLLMError("codex", 0, fmt.Errorf("response truncated: stream ended before turn.completed%s",
				cliFailureDetail(stderrBuf.String(), processExitCode(cmd))))
		}
		select {
		case ch <- se:
		default:
			select {
			case ch <- se:
			case <-ctx.Done():
			}
		}

		_ = cmd.Wait()

		// 56.3 raw capture（Stream 路径裁决 7）：scanner 循环 + cmd.Wait 完成后
		// 才填 Response——此刻 stream-json 已被 tee 累积到 rawStdoutBuf，stderr
		// 已收尾，exit code 经 cmd.ProcessState 可用。set 必须在 close(ch)
		// （defer LIFO 最先执行）触发前完成，否则 LLMFile.range ch 退出后
		// sink.get() 取到的可能是仅含 Request 的过早态。
		if sink != nil && rawCap != nil {
			fillCLIResponse(rawCap, rawStdoutBuf.String(), stderrBuf.String(), processExitCode(cmd))
			sink.set(rawCap)
		}
	}()

	return ch, nil
}

// Info returns metadata about this driver.
func (d *CodexCliDriver) Info() DriverInfo {
	return DriverInfo{
		Name:            "codex-cli",
		Provider:        "codex",
		DefaultModel:    d.defaultModel,
		DriverType:      DriverCodexCLI,
		ReasoningEffort: d.effort,
	}
}

// buildArgs constructs CLI arguments for a Codex exec invocation.
func (d *CodexCliDriver) buildArgs(req LLMRequest, jsonMode bool) []string {
	prompt := d.buildPrompt(req)
	args := []string{"exec", "--sandbox", d.resolveSandboxMode()}

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

	// Reasoning effort: passthrough via `-c model_reasoning_effort=<value>`,
	// only when configured (no validation). Before extraArgs and prompt.
	if effort := d.resolveEffort(req); effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+effort)
	}

	args = append(args, d.extraArgs...)

	// Prompt must be the last argument per `codex exec [OPTIONS] [PROMPT]`.
	args = append(args, prompt)

	return args
}

// buildPrompt constructs the prompt for a CLI Agent invocation.
// Codex CLI has no --system-prompt flag, so system instructions are prepended to the prompt.
// CLI Agents manage their own agent loop internally, so each Call is an
// independent task — no cross-invocation history serialization needed.
func (d *CodexCliDriver) buildPrompt(req LLMRequest) string {
	if req.SystemPrompt != "" {
		return fmt.Sprintf("[System Instructions]\n%s\n\n[Task]\n%s", req.SystemPrompt, req.Intent)
	}
	return req.Intent
}
