package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

// Compile-time interface checks.
var (
	_ LLMDriver         = (*OpenAICompatDriver)(nil)
	_ ToolCallingDriver = (*OpenAICompatDriver)(nil)
	_ HealthChecker     = (*OpenAICompatDriver)(nil)
)

// OpenAICompatDriver implements LLMDriver and ToolCallingDriver for any
// OpenAI-compatible /v1/chat/completions endpoint (Ollama, Groq, DeepSeek, etc).
type OpenAICompatDriver struct {
	baseURL          string
	apiKey           string
	name             string
	defaultModel     string
	defaultTimeout   time.Duration
	httpClient       *http.Client
	streamUsage      bool
	defaultMaxTokens int
	thinkingBudget   int
	reasoningEffort  string
}

// CompatOption configures an OpenAICompatDriver.
type CompatOption func(*OpenAICompatDriver)

// WithCompatModel sets the default model.
func WithCompatModel(model string) CompatOption {
	return func(d *OpenAICompatDriver) { d.defaultModel = model }
}

// WithCompatTimeout sets the default timeout.
func WithCompatTimeout(timeout time.Duration) CompatOption {
	return func(d *OpenAICompatDriver) { d.defaultTimeout = timeout }
}

// WithHTTPClient sets a custom HTTP client (useful for testing with httptest).
func WithHTTPClient(c *http.Client) CompatOption {
	return func(d *OpenAICompatDriver) { d.httpClient = c }
}

// WithAPIKey sets the API key for Authorization header.
func WithAPIKey(key string) CompatOption {
	return func(d *OpenAICompatDriver) { d.apiKey = key }
}

// WithStreamUsage enables sending stream_options.include_usage in stream requests.
func WithStreamUsage(enabled bool) CompatOption {
	return func(d *OpenAICompatDriver) { d.streamUsage = enabled }
}

// WithCompatMaxTokens sets the default max output tokens for requests that don't specify one.
func WithCompatMaxTokens(n int) CompatOption {
	return func(d *OpenAICompatDriver) { d.defaultMaxTokens = n }
}

// WithCompatThinkingBudget enables thinking/reasoning mode for providers that
// support it (DeepSeek V4+). The budget is sent as budget_tokens in the request.
// DeepSeek requires this parameter for multi-turn conversations with tool calls;
// without it, the API returns HTTP 400 "reasoning_content must be passed back".
func WithCompatThinkingBudget(n int) CompatOption {
	return func(d *OpenAICompatDriver) { d.thinkingBudget = n }
}

// WithCompatReasoningEffort sets the reasoning_effort field sent in the request
// body. The value is passed through verbatim (DeepSeek V4 etc. accept it
// natively); rnix does not validate or map it. Empty = field omitted. Orthogonal
// to thinking_budget — both may be set; newest models prefer effort.
func WithCompatReasoningEffort(effort string) CompatOption {
	return func(d *OpenAICompatDriver) { d.reasoningEffort = effort }
}

// NewOpenAICompatDriver creates a new driver for an OpenAI-compatible endpoint.
func NewOpenAICompatDriver(name, baseURL string, opts ...CompatOption) *OpenAICompatDriver {
	d := &OpenAICompatDriver{
		name:           name,
		baseURL:        strings.TrimRight(baseURL, "/"),
		defaultTimeout: DefaultTimeout,
		httpClient:     &http.Client{},
		// Ask for usage on streamed responses by default, matching the official
		// OpenAI driver (openai_official.go). Without stream_options.include_usage
		// a spec-strict endpoint emits no usage chunk at all, leaving TokensUsed /
		// InputTokens at 0 for every streamed step. Endpoints that predate the
		// option ignore the field; WithStreamUsage(false) opts out.
		streamUsage: true,
	}
	for _, opt := range opts {
		opt(d)
	}
	// 56.2 raw capture: 把 httpClient 的 Transport 包成 captureRoundTripper，
	// 自动给 Call/Stream 路径产生 RawCapture。Transport 包装是透明的——保留
	// 原 client 的 Timeout/Jar/Redirect 等其它配置，且对未挂 ctx-sink 的调用
	// （HealthCheck）零开销 fallback 到 base RoundTrip。
	d.httpClient = wrapHTTPClientWithCapture(d.httpClient)
	return d
}

// Info returns metadata about this driver.
func (d *OpenAICompatDriver) Info() DriverInfo {
	return DriverInfo{
		Name:            d.name,
		Provider:        d.name,
		DefaultModel:    d.defaultModel,
		DriverType:      DriverOpenAICompat,
		ReasoningEffort: d.reasoningEffort,
	}
}

// HealthCheck performs a lightweight GET /models check against the provider endpoint.
// Returns nil if the provider is reachable and responds with HTTP 2xx.
func (d *OpenAICompatDriver) HealthCheck(ctx context.Context) error {
	url := d.baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// --- Internal OpenAI API types (unexported) ---

type oaiRequest struct {
	Model           string            `json:"model"`
	Messages        []oaiMessage      `json:"messages"`
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxTokens       *int              `json:"max_tokens,omitempty"`
	Stream          bool              `json:"stream"`
	StreamOptions   *oaiStreamOptions `json:"stream_options,omitempty"`
	Tools           []oaiTool         `json:"tools,omitempty"`
	Thinking        *oaiThinking      `json:"thinking,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
}

type oaiThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type oaiMessage struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	Name             string        `json:"name,omitempty"`
	ToolCalls        []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	Reasoning        string        `json:"reasoning,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
}

func (m oaiMessage) reasoningText() string {
	if m.Reasoning != "" {
		return m.Reasoning
	}
	return m.ReasoningContent
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
}

type oaiChoice struct {
	Index        int        `json:"index"`
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type oaiUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitzero"`
}

type oaiStreamChunk struct {
	Choices []oaiStreamChoice `json:"choices"`
	Usage   *oaiUsage         `json:"usage,omitempty"`
}

type oaiStreamChoice struct {
	Index        int        `json:"index"`
	Delta        oaiMessage `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type oaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type oaiToolCall struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function oaiToolCallFunction `json:"function"`
}

type oaiToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type oaiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// --- Request building helpers ---

// buildMessages converts LLMRequest to oaiMessage slice.
func (d *OpenAICompatDriver) buildMessages(req LLMRequest) ([]oaiMessage, error) {
	var msgs []oaiMessage

	if len(req.Messages) > 0 {
		for _, m := range req.Messages {
			om := oaiMessage{
				Role:       m.Role,
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			}
			if m.Role == "assistant" && m.Reasoning != "" {
				// Echo thinking-mode text under both protocol field names so
				// provider-agnostic round-tripping satisfies DeepSeek
				// (reasoning_content) and OpenRouter/GLM (reasoning) alike.
				// DeepSeek returns HTTP 400 if reasoning_content is dropped.
				om.Reasoning = m.Reasoning
				om.ReasoningContent = m.Reasoning
			}
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					args, err := json.Marshal(tc.Input)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal tool call input for %q: %w", tc.Name, err)
					}
					om.ToolCalls = append(om.ToolCalls, oaiToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: oaiToolCallFunction{
							Name:      tc.Name,
							Arguments: string(args),
						},
					})
				}
			}
			// Convert role=tool to role=user when the IMMEDIATELY preceding
			// message is not assistant.tool_calls. DeepSeek/OpenAI require
			// `tool` to directly follow its `assistant.tool_calls` parent;
			// any intervening user/system/assistant message breaks the
			// pairing and triggers HTTP 400 "Messages with role 'tool' must
			// be a response to a preceding message with 'tool_calls'".
			//
			// Earlier this check used "most recent assistant" which missed
			// the orphan case [assistant+tc, user, tool] — kernel ActionSpecialize
			// used to insert a skill-body user message between the assistant
			// turn and its tool result. The kernel side is now fixed but
			// this guard also catches any future kernel path that breaks
			// the invariant.
			if om.Role == "tool" && !prevMessageIsAssistantWithToolCalls(msgs) {
				om.Role = "user"
				om.Content = fmt.Sprintf("[Tool Result: %s]\n%s", om.ToolCallID, om.Content)
				om.ToolCallID = ""
			}
			msgs = append(msgs, om)
		}
	} else if req.Intent != "" {
		msgs = append(msgs, oaiMessage{Role: "user", Content: req.Intent})
	}

	if req.SystemPrompt != "" && (len(msgs) == 0 || msgs[0].Role != "system") {
		msgs = append([]oaiMessage{{Role: "system", Content: req.SystemPrompt}}, msgs...)
	}

	msgs = repairToolCallSequence(msgs)

	return msgs, nil
}

// prevMessageIsAssistantWithToolCalls reports whether the soon-to-be-appended
// `tool` message would land in a legal position per the OpenAI/DeepSeek
// protocol — i.e. the run of messages immediately before it is one or more
// `tool` messages (siblings under the same assistant.tool_calls turn) and the
// first non-tool message before them is `assistant` with non-empty
// tool_calls. Peeking at the very last entry alone is too strict (it would
// flag the second tool result of a multi-call turn) and scanning backwards
// for any assistant is too loose (the bug fixed here: an intervening user
// message between `assistant.tool_calls` and its `tool` result was missed).
func prevMessageIsAssistantWithToolCalls(msgs []oaiMessage) bool {
	for _, m := range slices.Backward(msgs) {
		if m.Role == "tool" {
			continue
		}
		return m.Role == "assistant" && len(m.ToolCalls) > 0
	}
	return false
}

// repairToolCallSequence enforces the OpenAI protocol invariant: every
// assistant message with tool_calls MUST be followed by exactly one tool
// message per tool_call_id. When upstream context bookkeeping drops a
// tool result (e.g. ctx.MaxSize hit during AppendToolResult), DeepSeek and
// most OpenAI-compat providers reject the request with HTTP 400
// "insufficient tool messages following tool_calls message".
//
// This function scans msgs and inserts stub tool messages for any
// missing tool_call_id, keeping the conversation legal at the protocol
// layer regardless of upstream bugs.
const toolResultUnavailableStub = "[Tool result unavailable: dropped due to context buffer limit]"

func repairToolCallSequence(msgs []oaiMessage) []oaiMessage {
	if len(msgs) == 0 {
		return msgs
	}
	result := make([]oaiMessage, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		result = append(result, m)
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		seen := make(map[string]bool, len(m.ToolCalls))
		j := i + 1
		for j < len(msgs) && msgs[j].Role == "tool" {
			seen[msgs[j].ToolCallID] = true
			result = append(result, msgs[j])
			j++
		}
		for _, tc := range m.ToolCalls {
			if seen[tc.ID] {
				continue
			}
			result = append(result, oaiMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolResultUnavailableStub,
			})
		}
		i = j - 1
	}
	return result
}

// resolveEffort returns the per-request reasoning effort override, or the
// driver instance default (d.reasoningEffort) when the request leaves it empty.
// Value is passed through verbatim (no validation/mapping).
func (d *OpenAICompatDriver) resolveEffort(req LLMRequest) string {
	if req.ReasoningEffort != "" {
		return req.ReasoningEffort
	}
	return d.reasoningEffort
}

// buildOAIRequest constructs the full OpenAI API request body.
func (d *OpenAICompatDriver) buildOAIRequest(req LLMRequest, stream bool, tools []ToolDef) (oaiRequest, error) {
	msgs, err := d.buildMessages(req)
	if err != nil {
		return oaiRequest{}, err
	}
	oai := oaiRequest{
		Model:    req.Model,
		Messages: msgs,
		Stream:   stream,
	}
	if oai.Model == "" {
		oai.Model = d.defaultModel
	}
	if req.Temperature != nil {
		oai.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		mt := req.MaxTokens
		oai.MaxTokens = &mt
	} else if d.defaultMaxTokens > 0 {
		mt := d.defaultMaxTokens
		oai.MaxTokens = &mt
	}
	if stream && d.streamUsage {
		oai.StreamOptions = &oaiStreamOptions{IncludeUsage: true}
	}
	if len(tools) > 0 {
		for _, td := range tools {
			oai.Tools = append(oai.Tools, oaiTool{
				Type: "function",
				Function: oaiFunction{
					Name:        td.Name,
					Description: td.Description,
					Parameters:  td.Parameters,
				},
			})
		}
	}
	if d.thinkingBudget > 0 {
		oai.Thinking = &oaiThinking{
			Type:         "enabled",
			BudgetTokens: d.thinkingBudget,
		}
	}
	// Passthrough: reasoning_effort sent verbatim when configured. Orthogonal to
	// the thinking_budget path above (both may coexist); rnix does not validate.
	if effort := d.resolveEffort(req); effort != "" {
		oai.ReasoningEffort = effort
	}
	return oai, nil
}

// doHTTP sends the request to the OpenAI-compatible endpoint.
func (d *OpenAICompatDriver) doHTTP(ctx context.Context, body oaiRequest) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}

	return d.httpClient.Do(req)
}

// classifyHTTPError maps an HTTP error response to a typed LLMError.
func (d *OpenAICompatDriver) classifyHTTPError(statusCode int, body []byte) *LLMError {
	var errResp oaiErrorResponse
	_ = json.Unmarshal(body, &errResp)
	errMsg := errResp.Error.Message
	if errMsg == "" {
		errMsg = string(body)
	}

	switch statusCode {
	case 401:
		return NewLLMError(d.name, 401, fmt.Errorf("%s: %w", errMsg, ErrAuth))
	case 429:
		// Story 73.1 / AC4: this driver has the provider's structured
		// error.type already parsed (errResp.Error.Code/.Type), so both
		// evidence channels feed the split.
		kind := classifyRateLimitBody(errResp.Error.Message, errResp.Error.Code+" "+errResp.Error.Type)
		return NewLLMError(d.name, 429, NewRateLimitError(kind, errMsg))
	case 529, 503:
		// Story 73.1 / AC3.
		return NewLLMError(d.name, statusCode, NewRateLimitError(KindOverload, errMsg))
	case 404:
		return NewLLMError(d.name, 404, fmt.Errorf("%s: %w", errMsg, ErrModelNotFound))
	case 400:
		lower := strings.ToLower(errResp.Error.Code + " " + errResp.Error.Message)
		if strings.Contains(lower, "context_length") {
			return NewLLMError(d.name, 400, fmt.Errorf("%s: %w", errMsg, ErrContextLength))
		}
	}

	return NewLLMError(d.name, statusCode, fmt.Errorf("%s", errMsg))
}

// parseToolCalls converts OpenAI tool calls to our ToolCall type.
func parseToolCalls(oaiCalls []oaiToolCall) []ToolCall {
	if len(oaiCalls) == 0 {
		return nil
	}
	result := make([]ToolCall, len(oaiCalls))
	for i, oc := range oaiCalls {
		tc := ToolCall{
			ID:   oc.ID,
			Name: oc.Function.Name,
		}
		if oc.Function.Arguments != "" {
			var input map[string]any
			if err := json.Unmarshal([]byte(oc.Function.Arguments), &input); err != nil {
				tc.ParseError = fmt.Sprintf("invalid arguments JSON: %v; raw: %.200s", err, oc.Function.Arguments)
			} else {
				tc.Input = input
			}
		}
		result[i] = tc
	}
	return result
}

// --- Call / CallWithTools ---

// callInternal is the shared implementation for Call and CallWithTools.
func (d *OpenAICompatDriver) callInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	oaiReq, err := d.buildOAIRequest(req, false, tools)
	if err != nil {
		return nil, err
	}
	resp, err := d.doHTTP(ctx, oaiReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, NewLLMError(d.name, 0, ErrTimeout)
		}
		return nil, classifyTransportError(d.name, err)
	}
	defer resp.Body.Close()

	const maxResponseBody = 10 << 20 // 10MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, d.classifyHTTPError(resp.StatusCode, body)
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, NewLLMError(d.name, 0, fmt.Errorf("failed to parse response: %w", err))
	}

	llmResp := &LLMResponse{
		TokensUsed:        oaiResp.Usage.TotalTokens,
		InputTokens:       oaiResp.Usage.PromptTokens,
		OutputTokens:      oaiResp.Usage.CompletionTokens,
		CachedInputTokens: oaiResp.Usage.PromptTokensDetails.CachedTokens,
	}
	if len(oaiResp.Choices) > 0 {
		msg := oaiResp.Choices[0].Message
		llmResp.Content = msg.Content
		llmResp.Reasoning = msg.reasoningText()
		if llmResp.Content == "" && llmResp.Reasoning != "" {
			llmResp.Content = llmResp.Reasoning
		}
		llmResp.ToolCalls = parseToolCalls(msg.ToolCalls)
	}
	return llmResp, nil
}

// Call executes a synchronous LLM request.
func (d *OpenAICompatDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	return d.callInternal(ctx, req, nil)
}

// CallWithTools executes a synchronous LLM request with tool definitions.
func (d *OpenAICompatDriver) CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	return d.callInternal(ctx, req, tools)
}

// --- Stream / StreamWithTools ---

// toolCallAccumulator accumulates streamed tool call fragments.
type toolCallAccumulator struct {
	id        string
	name      strings.Builder
	arguments strings.Builder
}

// streamInternal is the shared implementation for Stream and StreamWithTools.
func (d *OpenAICompatDriver) streamInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	// Timeout applies as an idle timeout on the SSE stream (see streamtimeout.go).
	ctx, idle, cancel := NewIdleTimer(ctx, timeout)

	oaiReq, err := d.buildOAIRequest(req, true, tools)
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := d.doHTTP(ctx, oaiReq)
	if err != nil {
		cancel()
		if IsStreamTimeout(ctx) {
			return nil, NewLLMError(d.name, 0, ErrTimeout)
		}
		return nil, classifyTransportError(d.name, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, d.classifyHTTPError(resp.StatusCode, body)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer resp.Body.Close()
		defer cancel()

		scanner := newStreamScanner(resp.Body)

		pendingToolCalls := make(map[int]*toolCallAccumulator)
		var lastUsage *oaiUsage
		// terminal records that a chunk carried a finish_reason. sawChunk records
		// that at least one well-formed chunk was parsed. Both feed the single
		// deferred `done` emission below — see the emitDone godoc.
		var terminal, sawChunk bool

		// emitDone sends the one-and-only terminal `done` event. It is deliberately
		// NOT called from the finish_reason branch: OpenAI-compatible endpoints put
		// the usage payload in a trailing `{"choices":[],"usage":{...}}` chunk that
		// arrives AFTER finish_reason, so returning at finish_reason loses usage for
		// every tool-calling step (qwen: prompt_tokens never reached the kernel →
		// proc.TokensUsed/LastInputTokens stuck at 0 → ctx%/budget% rendered 0%).
		// Emission is deferred to `[DONE]` or stream EOF, whichever comes first.
		emitDone := func() {
			evt := StreamEvent{Type: "done"}
			if lastUsage != nil {
				evt.TokensUsed = lastUsage.TotalTokens
				evt.InputTokens = lastUsage.PromptTokens
				evt.OutputTokens = lastUsage.CompletionTokens
				evt.CachedInputTokens = lastUsage.PromptTokensDetails.CachedTokens
			}
			if len(pendingToolCalls) > 0 {
				evt.ToolCalls = flushToolCalls(pendingToolCalls)
			}
			select {
			case ch <- evt:
			case <-ctx.Done():
			}
		}

		for scanner.Scan() {
			idle.Reset()
			line := scanner.Text()
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				emitDone()
				return
			}

			var chunk oaiStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				select {
				case ch <- StreamEvent{Type: "error", Err: NewLLMError(d.name, 0, fmt.Errorf("failed to parse SSE chunk: %w", err))}:
				case <-ctx.Done():
				}
				continue
			}

			if chunk.Usage != nil {
				lastUsage = chunk.Usage
			}
			sawChunk = true

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]

			// Content delta
			if choice.Delta.Content != "" {
				select {
				case ch <- StreamEvent{Type: "content", Content: choice.Delta.Content}:
				case <-ctx.Done():
					return
				}
			}

			// Reasoning delta (OpenRouter/GLM: "reasoning", DeepSeek: "reasoning_content")
			if r := choice.Delta.reasoningText(); r != "" {
				select {
				case ch <- StreamEvent{Type: "reasoning", Content: r}:
				case <-ctx.Done():
					return
				}
			}

			// Tool calls delta accumulation
			for _, tc := range choice.Delta.ToolCalls {
				acc, exists := pendingToolCalls[tc.Index]
				if !exists {
					acc = &toolCallAccumulator{id: tc.ID}
					pendingToolCalls[tc.Index] = acc
				}
				if tc.ID != "" && acc.id == "" {
					acc.id = tc.ID
				}
				acc.name.WriteString(tc.Function.Name)
				acc.arguments.WriteString(tc.Function.Arguments)
			}

			// A finish_reason marks the end of the model's turn, but NOT the end of
			// the stream: the usage chunk still trails it. Record the terminal state
			// and keep scanning so lastUsage can be picked up; emitDone runs at
			// `[DONE]` or EOF.
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				terminal = true
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError(d.name, 0, fmt.Errorf("stream read error: %w", err))}:
			case <-ctx.Done():
			}
			return
		}

		// EOF without `[DONE]`: many OpenAI-compatible endpoints (qwen among them)
		// simply close the connection after the trailing usage chunk. Emit the
		// terminal event here so usage and any accumulated tool calls still reach
		// the caller. Guarded on having seen a real chunk so a genuinely empty or
		// malformed stream keeps surfacing as ErrStreamIncomplete upstream rather
		// than as a hollow success.
		if terminal || sawChunk {
			emitDone()
		}
	}()

	return ch, nil
}

// flushToolCalls converts accumulated tool call fragments into ToolCall slice.
func flushToolCalls(pending map[int]*toolCallAccumulator) []ToolCall {
	if len(pending) == 0 {
		return nil
	}
	// Collect and sort indices to handle non-contiguous index values.
	indices := make([]int, 0, len(pending))
	for idx := range pending {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	calls := make([]ToolCall, 0, len(pending))
	for _, idx := range indices {
		acc := pending[idx]
		tc := ToolCall{
			ID:   acc.id,
			Name: acc.name.String(),
		}
		args := acc.arguments.String()
		if args != "" {
			var input map[string]any
			if err := json.Unmarshal([]byte(args), &input); err != nil {
				tc.ParseError = fmt.Sprintf("invalid arguments JSON: %v; raw: %.200s", err, args)
			} else {
				tc.Input = input
			}
		}
		calls = append(calls, tc)
	}
	return calls
}

// Stream executes a streaming LLM request.
func (d *OpenAICompatDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, nil)
}

// StreamWithTools executes a streaming LLM request with tool definitions.
func (d *OpenAICompatDriver) StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, tools)
}
