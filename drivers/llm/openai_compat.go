package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Compile-time interface checks.
var (
	_ LLMDriver        = (*OpenAICompatDriver)(nil)
	_ ToolCallingDriver = (*OpenAICompatDriver)(nil)
	_ HealthChecker     = (*OpenAICompatDriver)(nil)
)

// OpenAICompatDriver implements LLMDriver and ToolCallingDriver for any
// OpenAI-compatible /v1/chat/completions endpoint (Ollama, Groq, DeepSeek, etc).
type OpenAICompatDriver struct {
	baseURL         string
	apiKey          string
	name            string
	defaultModel    string
	defaultTimeout  time.Duration
	httpClient      *http.Client
	streamUsage     bool
	defaultMaxTokens int
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

// NewOpenAICompatDriver creates a new driver for an OpenAI-compatible endpoint.
func NewOpenAICompatDriver(name, baseURL string, opts ...CompatOption) *OpenAICompatDriver {
	d := &OpenAICompatDriver{
		name:           name,
		baseURL:        strings.TrimRight(baseURL, "/"),
		defaultTimeout: DefaultTimeout,
		httpClient:     &http.Client{},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Info returns metadata about this driver.
func (d *OpenAICompatDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         d.name,
		Provider:     d.name,
		DefaultModel: d.defaultModel,
		DriverType:   DriverOpenAICompat,
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
	Model         string            `json:"model"`
	Messages      []oaiMessage      `json:"messages"`
	Temperature   *float64          `json:"temperature,omitempty"`
	MaxTokens     *int              `json:"max_tokens,omitempty"`
	Stream        bool              `json:"stream"`
	StreamOptions *oaiStreamOptions `json:"stream_options,omitempty"`
	Tools         []oaiTool         `json:"tools,omitempty"`
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
			// Convert role=tool to role=user when the preceding assistant message
			// lacks tool_calls (rnix action protocol uses JSON-in-content, not
			// OpenAI tool_calls). Without this, DeepSeek/OpenAI APIs reject with
			// "Messages with role 'tool' must follow a 'tool_calls' message".
			if om.Role == "tool" && !prevAssistantHasToolCalls(msgs) {
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

	return msgs, nil
}

// prevAssistantHasToolCalls checks if the last assistant message in msgs has tool_calls.
func prevAssistantHasToolCalls(msgs []oaiMessage) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return len(msgs[i].ToolCalls) > 0
		}
	}
	return false
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
		return NewLLMError(d.name, 429, fmt.Errorf("%s: %w", errMsg, ErrRateLimit))
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
		return nil, fmt.Errorf("http request failed: %w", err)
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
	ctx, cancel := context.WithTimeout(ctx, timeout)

	oaiReq, err := d.buildOAIRequest(req, true, tools)
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := d.doHTTP(ctx, oaiReq)
	if err != nil {
		cancel()
		if ctx.Err() == context.DeadlineExceeded {
			return nil, NewLLMError(d.name, 0, ErrTimeout)
		}
		return nil, fmt.Errorf("http request failed: %w", err)
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

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
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

			// finish_reason == "tool_calls" triggers flush
			if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
				evt := StreamEvent{Type: "done"}
				if lastUsage != nil {
					evt.TokensUsed = lastUsage.TotalTokens
					evt.InputTokens = lastUsage.PromptTokens
					evt.OutputTokens = lastUsage.CompletionTokens
					evt.CachedInputTokens = lastUsage.PromptTokensDetails.CachedTokens
				}
				evt.ToolCalls = flushToolCalls(pendingToolCalls)
				select {
				case ch <- evt:
				case <-ctx.Done():
				}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case ch <- StreamEvent{Type: "error", Err: NewLLMError(d.name, 0, fmt.Errorf("stream read error: %w", err))}:
			case <-ctx.Done():
			}
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
