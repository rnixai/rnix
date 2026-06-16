package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

var (
	_ LLMDriver         = (*OpenAIDriver)(nil)
	_ ToolCallingDriver = (*OpenAIDriver)(nil)
	_ HealthChecker     = (*OpenAIDriver)(nil)
)

// OpenAIDriver implements LLMDriver and ToolCallingDriver using the official
// openai-go SDK (github.com/openai/openai-go/v3).
type OpenAIDriver struct {
	client           openai.Client
	name             string
	defaultModel     string
	defaultTimeout   time.Duration
	streamUsage      bool
	defaultMaxTokens int
	reasoningEffort  string
}

// OpenAIOption configures an OpenAIDriver.
type OpenAIOption func(*openaiDriverConfig)

type openaiDriverConfig struct {
	model           string
	timeout         time.Duration
	sdkOpts         []option.RequestOption
	streamUsage     bool
	maxTokens       int
	reasoningEffort string
}

func WithOpenAIModel(model string) OpenAIOption {
	return func(c *openaiDriverConfig) { c.model = model }
}

func WithOpenAITimeout(timeout time.Duration) OpenAIOption {
	return func(c *openaiDriverConfig) { c.timeout = timeout }
}

// WithOpenAIBaseURL overrides the default OpenAI API base URL.
func WithOpenAIBaseURL(baseURL string) OpenAIOption {
	return func(c *openaiDriverConfig) {
		c.sdkOpts = append(c.sdkOpts, option.WithBaseURL(baseURL))
	}
}

// WithOpenAIKey sets the API key for the OpenAI client.
func WithOpenAIKey(key string) OpenAIOption {
	return func(c *openaiDriverConfig) {
		c.sdkOpts = append(c.sdkOpts, option.WithAPIKey(key))
	}
}

// WithOpenAIHTTPClient sets a custom HTTP client (useful for testing).
func WithOpenAIHTTPClient(client option.HTTPClient) OpenAIOption {
	return func(c *openaiDriverConfig) {
		c.sdkOpts = append(c.sdkOpts, option.WithHTTPClient(client))
	}
}

// WithOpenAIStreamUsage enables stream_options.include_usage in stream requests.
func WithOpenAIStreamUsage(enabled bool) OpenAIOption {
	return func(c *openaiDriverConfig) { c.streamUsage = enabled }
}

// WithOpenAIMaxTokens sets the default max output tokens for requests that don't specify one.
func WithOpenAIMaxTokens(n int) OpenAIOption {
	return func(c *openaiDriverConfig) { c.maxTokens = n }
}

// WithOpenAIReasoningEffort sets the reasoning_effort sent on every request.
// The value is passed through verbatim — shared.ReasoningEffort is an open
// string type with no runtime validation, so "xhigh" (gpt-5.1-codex-max+) and
// any future vendor level work without a code change. Empty = unset.
func WithOpenAIReasoningEffort(effort string) OpenAIOption {
	return func(c *openaiDriverConfig) { c.reasoningEffort = effort }
}

// WithOpenAISDKOption appends a raw SDK request option.
func WithOpenAISDKOption(opt option.RequestOption) OpenAIOption {
	return func(c *openaiDriverConfig) {
		c.sdkOpts = append(c.sdkOpts, opt)
	}
}

// NewOpenAIDriver creates a new driver backed by the official openai-go SDK.
// By default the SDK reads OPENAI_API_KEY and OPENAI_BASE_URL from the env.
func NewOpenAIDriver(name string, opts ...OpenAIOption) *OpenAIDriver {
	cfg := openaiDriverConfig{
		timeout:     DefaultTimeout,
		streamUsage: true,
	}
	for _, o := range opts {
		o(&cfg)
	}
	// 56.2 raw capture: 把捕获 middleware 追加到 sdkOpts。option.Middleware
	// 是 type alias 到同款 func 签名（drivers/llm/raw_capture.go 共享函数），
	// 在 SDK 默认 HTTP client 之上做 tee；不覆盖 WithOpenAIHTTPClient 测试
	// 注入（middleware 与 client 注入正交）。
	cfg.sdkOpts = append(cfg.sdkOpts, option.WithMiddleware(captureMiddlewareFunc))
	client := openai.NewClient(cfg.sdkOpts...)
	return &OpenAIDriver{
		client:           client,
		name:             name,
		defaultModel:     cfg.model,
		defaultTimeout:   cfg.timeout,
		streamUsage:      cfg.streamUsage,
		defaultMaxTokens: cfg.maxTokens,
		reasoningEffort:  cfg.reasoningEffort,
	}
}

func (d *OpenAIDriver) Info() DriverInfo {
	return DriverInfo{
		Name:            d.name,
		Provider:        d.name,
		DefaultModel:    d.defaultModel,
		DriverType:      DriverOpenAI,
		ReasoningEffort: d.reasoningEffort,
	}
}

// HealthCheck performs a lightweight GET /models check.
func (d *OpenAIDriver) HealthCheck(ctx context.Context) error {
	_, err := d.client.Models.List(ctx)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	return nil
}

// --- Message / param conversion ---

func (d *OpenAIDriver) buildSDKMessages(req LLMRequest) []openai.ChatCompletionMessageParamUnion {
	var msgs []openai.ChatCompletionMessageParamUnion

	if len(req.Messages) > 0 {
		for _, m := range req.Messages {
			msgs = append(msgs, convertMessageToSDK(m))
		}
	} else if req.Intent != "" {
		msgs = append(msgs, openai.UserMessage(req.Intent))
	}

	if req.SystemPrompt != "" && (len(msgs) == 0 || msgs[0].OfSystem == nil) {
		msgs = append([]openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(req.SystemPrompt),
		}, msgs...)
	}

	return msgs
}

func convertMessageToSDK(m Message) openai.ChatCompletionMessageParamUnion {
	switch m.Role {
	case "system":
		return openai.SystemMessage(m.Content)
	case "user":
		return openai.UserMessage(m.Content)
	case "assistant":
		if len(m.ToolCalls) > 0 {
			return buildAssistantToolCallMsg(m)
		}
		return openai.AssistantMessage(m.Content)
	case "tool":
		return openai.ToolMessage(m.Content, m.ToolCallID)
	default:
		return openai.UserMessage(m.Content)
	}
}

func buildAssistantToolCallMsg(m Message) openai.ChatCompletionMessageParamUnion {
	var asst openai.ChatCompletionAssistantMessageParam
	if m.Content != "" {
		asst.Content.OfString = openai.String(m.Content)
	}
	for _, tc := range m.ToolCalls {
		args, _ := json.Marshal(tc.Input)
		asst.ToolCalls = append(asst.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: tc.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      tc.Name,
					Arguments: string(args),
				},
			},
		})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &asst}
}

func convertToolDefsToSDK(tools []ToolDef) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	result := make([]openai.ChatCompletionToolUnionParam, len(tools))
	for i, td := range tools {
		fdp := shared.FunctionDefinitionParam{
			Name:       td.Name,
			Parameters: shared.FunctionParameters(td.Parameters),
		}
		if td.Description != "" {
			fdp.Description = openai.String(td.Description)
		}
		result[i] = openai.ChatCompletionFunctionTool(fdp)
	}
	return result
}

// resolveEffort returns the per-request reasoning effort override, or the
// driver instance default (d.reasoningEffort) when the request leaves it empty.
// Value is passed through verbatim (no validation/mapping).
func (d *OpenAIDriver) resolveEffort(req LLMRequest) string {
	if req.ReasoningEffort != "" {
		return req.ReasoningEffort
	}
	return d.reasoningEffort
}

func (d *OpenAIDriver) buildParams(req LLMRequest, tools []ToolDef) openai.ChatCompletionNewParams {
	model := req.Model
	if model == "" {
		model = d.defaultModel
	}

	params := openai.ChatCompletionNewParams{
		Messages: d.buildSDKMessages(req),
		Model:    shared.ChatModel(model),
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(req.MaxTokens))
	} else if d.defaultMaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(d.defaultMaxTokens))
	}
	// Passthrough: reasoning_effort sent verbatim when configured. shared.ReasoningEffort
	// is an open string type (none/minimal/low/medium/high/xhigh predefined, no validation),
	// so unknown/future levels pass through untouched. Empty = field omitted (omitzero).
	if effort := d.resolveEffort(req); effort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(effort)
	}
	if sdkTools := convertToolDefsToSDK(tools); len(sdkTools) > 0 {
		params.Tools = sdkTools
	}
	return params
}

// --- Call / CallWithTools ---

func (d *OpenAIDriver) callInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	params := d.buildParams(req, tools)
	completion, err := d.client.Chat.Completions.New(ctx, params)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, NewLLMError(d.name, 0, ErrTimeout)
		}
		return nil, d.classifyError(err)
	}
	return d.convertCompletion(completion), nil
}

func (d *OpenAIDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	return d.callInternal(ctx, req, nil)
}

func (d *OpenAIDriver) CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	return d.callInternal(ctx, req, tools)
}

func (d *OpenAIDriver) convertCompletion(c *openai.ChatCompletion) *LLMResponse {
	resp := &LLMResponse{
		TokensUsed:        int(c.Usage.TotalTokens),
		InputTokens:       int(c.Usage.PromptTokens),
		OutputTokens:      int(c.Usage.CompletionTokens),
		CachedInputTokens: int(c.Usage.PromptTokensDetails.CachedTokens),
	}
	if len(c.Choices) > 0 {
		msg := c.Choices[0].Message
		resp.Content = msg.Content
		resp.ToolCalls = convertSDKToolCalls(msg.ToolCalls)
	}
	return resp
}

func convertSDKToolCalls(sdkCalls []openai.ChatCompletionMessageToolCallUnion) []ToolCall {
	if len(sdkCalls) == 0 {
		return nil
	}
	result := make([]ToolCall, len(sdkCalls))
	for i, tc := range sdkCalls {
		result[i] = ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
		}
		if tc.Function.Arguments != "" {
			var input map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				result[i].ParseError = fmt.Sprintf("invalid arguments JSON: %v; raw: %.200s", err, tc.Function.Arguments)
			} else {
				result[i].Input = input
			}
		}
	}
	return result
}

// --- Stream / StreamWithTools ---

func (d *OpenAIDriver) streamInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	// Timeout applies as an idle timeout on the SSE stream (see streamtimeout.go).
	ctx, idle, cancel := NewIdleTimer(ctx, timeout)

	params := d.buildParams(req, tools)
	if d.streamUsage {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		}
	}

	stream := d.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer cancel()
		defer stream.Close()

		acc := openai.ChatCompletionAccumulator{}

		for stream.Next() {
			idle.Reset()
			chunk := stream.Current()
			acc.AddChunk(chunk)

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta
				if delta.Content != "" {
					select {
					case ch <- StreamEvent{Type: "content", Content: delta.Content}:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		if err := stream.Err(); err != nil {
			llmErr := d.classifyError(err)
			select {
			case ch <- StreamEvent{Type: "error", Err: llmErr}:
			case <-ctx.Done():
			}
			return
		}

		evt := StreamEvent{Type: "done"}
		if acc.Usage.TotalTokens > 0 {
			evt.TokensUsed = int(acc.Usage.TotalTokens)
			evt.InputTokens = int(acc.Usage.PromptTokens)
			evt.OutputTokens = int(acc.Usage.CompletionTokens)
			evt.CachedInputTokens = int(acc.Usage.PromptTokensDetails.CachedTokens)
		}
		if len(acc.Choices) > 0 {
			evt.ToolCalls = convertSDKToolCalls(acc.Choices[0].Message.ToolCalls)
		}
		select {
		case ch <- evt:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (d *OpenAIDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, nil)
}

func (d *OpenAIDriver) StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, tools)
}

// --- Error classification ---

// classifyError maps errors from the openai-go SDK into rnix LLMError types.
// The SDK's internal apierror.Error has a StatusCode field we extract via reflection.
func (d *OpenAIDriver) classifyError(err error) error {
	statusCode := extractSDKStatusCode(err)

	if statusCode > 0 {
		msg := err.Error()
		switch statusCode {
		case 401:
			return NewLLMError(d.name, 401, fmt.Errorf("%s: %w", msg, ErrAuth))
		case 429:
			return NewLLMError(d.name, 429, fmt.Errorf("%s: %w", msg, ErrRateLimit))
		case 404:
			return NewLLMError(d.name, 404, fmt.Errorf("%s: %w", msg, ErrModelNotFound))
		case 400:
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "context_length") {
				return NewLLMError(d.name, 400, fmt.Errorf("%s: %w", msg, ErrContextLength))
			}
		}
		return NewLLMError(d.name, statusCode, fmt.Errorf("%s", msg))
	}

	return fmt.Errorf("openai [%s]: %w", d.name, err)
}

// extractSDKStatusCode uses reflection to read the StatusCode field from the
// openai-go SDK's internal apierror.Error type, which we cannot import directly.
func extractSDKStatusCode(err error) int {
	if err == nil {
		return 0
	}
	v := reflect.ValueOf(err)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		f := v.FieldByName("StatusCode")
		if f.IsValid() && f.CanInt() {
			return int(f.Int())
		}
	}
	return 0
}
