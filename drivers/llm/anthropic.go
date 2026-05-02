package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

var (
	_ LLMDriver         = (*AnthropicDriver)(nil)
	_ ToolCallingDriver = (*AnthropicDriver)(nil)
	_ HealthChecker     = (*AnthropicDriver)(nil)
)

// AnthropicDriver implements LLMDriver, ToolCallingDriver, and HealthChecker
// using the official anthropic-sdk-go SDK.
type AnthropicDriver struct {
	client           anthropic.Client
	name             string
	defaultModel     string
	defaultTimeout   time.Duration
	defaultMaxTokens int
}

// AnthropicOption configures an AnthropicDriver.
type AnthropicOption func(*anthropicDriverConfig)

type anthropicDriverConfig struct {
	model     string
	timeout   time.Duration
	sdkOpts   []option.RequestOption
	maxTokens int
}

func WithAnthropicModel(model string) AnthropicOption {
	return func(c *anthropicDriverConfig) { c.model = model }
}

func WithAnthropicTimeout(timeout time.Duration) AnthropicOption {
	return func(c *anthropicDriverConfig) { c.timeout = timeout }
}

// WithAnthropicKey sets the API key for the Anthropic client.
func WithAnthropicKey(key string) AnthropicOption {
	return func(c *anthropicDriverConfig) {
		c.sdkOpts = append(c.sdkOpts, option.WithAPIKey(key))
	}
}

// WithAnthropicBaseURL overrides the default Anthropic API base URL.
func WithAnthropicBaseURL(baseURL string) AnthropicOption {
	return func(c *anthropicDriverConfig) {
		c.sdkOpts = append(c.sdkOpts, option.WithBaseURL(baseURL))
	}
}

// WithAnthropicMaxTokens sets the default max output tokens.
func WithAnthropicMaxTokens(n int) AnthropicOption {
	return func(c *anthropicDriverConfig) { c.maxTokens = n }
}

// WithAnthropicSDKOption appends a raw SDK request option.
func WithAnthropicSDKOption(opt option.RequestOption) AnthropicOption {
	return func(c *anthropicDriverConfig) {
		c.sdkOpts = append(c.sdkOpts, opt)
	}
}

// NewAnthropicDriver creates a new driver backed by the official anthropic-sdk-go.
// By default the SDK reads ANTHROPIC_API_KEY from the env.
func NewAnthropicDriver(name string, opts ...AnthropicOption) *AnthropicDriver {
	cfg := anthropicDriverConfig{
		timeout:   DefaultTimeout,
		maxTokens: 4096,
	}
	for _, o := range opts {
		o(&cfg)
	}
	client := anthropic.NewClient(cfg.sdkOpts...)
	return &AnthropicDriver{
		client:           client,
		name:             name,
		defaultModel:     cfg.model,
		defaultTimeout:   cfg.timeout,
		defaultMaxTokens: cfg.maxTokens,
	}
}

func (d *AnthropicDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         d.name,
		Provider:     d.name,
		DefaultModel: d.defaultModel,
		DriverType:   DriverAnthropic,
	}
}

// HealthCheck performs a lightweight GET /models check.
func (d *AnthropicDriver) HealthCheck(ctx context.Context) error {
	_, err := d.client.Models.List(ctx, anthropic.ModelListParams{})
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	return nil
}

// --- Message conversion ---

func (d *AnthropicDriver) buildSDKMessages(req LLMRequest) []anthropic.MessageParam {
	if len(req.Messages) > 0 {
		return convertMessagesToAnthropic(req.Messages)
	}
	if req.Intent != "" {
		return []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Intent)),
		}
	}
	return nil
}

func convertMessagesToAnthropic(msgs []Message) []anthropic.MessageParam {
	var result []anthropic.MessageParam
	for _, m := range msgs {
		switch m.Role {
		case "system":
			// System messages are handled separately via MessageNewParams.System
			continue
		case "user":
			result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			result = append(result, buildAnthropicAssistantMsg(m))
		case "tool":
			result = append(result, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false),
			))
		default:
			result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	return result
}

// buildAnthropicAssistantMsg assembles an assistant turn for the SDK.
// Block order is mandated by the Anthropic API: thinking blocks must come
// first, then text, then tool_use. Without this ordering deepseek's
// anthropic-compatible endpoint rejects the request with
// "content[].thinking must be passed back to the API".
func buildAnthropicAssistantMsg(m Message) anthropic.MessageParam {
	var blocks []anthropic.ContentBlockParamUnion
	// Anthropic-style endpoints reject the next turn unless every thinking
	// block from the previous assistant turn is echoed verbatim. Pass them
	// through unconditionally — empty Thinking/Data is still a valid block
	// and trying to be clever here re-introduces the original 400 error.
	for _, rb := range m.ReasoningBlocks {
		switch rb.Type {
		case "thinking":
			blocks = append(blocks, anthropic.NewThinkingBlock(rb.Signature, rb.Thinking))
		case "redacted_thinking":
			blocks = append(blocks, anthropic.NewRedactedThinkingBlock(rb.Data))
		}
	}
	if m.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	for _, tc := range m.ToolCalls {
		inputJSON, _ := json.Marshal(tc.Input)
		blocks = append(blocks, anthropic.ContentBlockParamUnion{
			OfToolUse: &anthropic.ToolUseBlockParam{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: inputJSON,
			},
		})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(""))
	}
	return anthropic.NewAssistantMessage(blocks...)
}

// --- Tool conversion ---

func convertToolDefsToAnthropic(tools []ToolDef) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	result := make([]anthropic.ToolUnionParam, len(tools))
	for i, td := range tools {
		tp := anthropic.ToolParam{
			Name:        td.Name,
			InputSchema: convertToolSchemaToAnthropic(td.Parameters),
		}
		if td.Description != "" {
			tp.Description = anthropic.String(td.Description)
		}
		result[i] = anthropic.ToolUnionParam{OfTool: &tp}
	}
	return result
}

func convertToolSchemaToAnthropic(params map[string]any) anthropic.ToolInputSchemaParam {
	schema := anthropic.ToolInputSchemaParam{}
	if props, ok := params["properties"]; ok {
		schema.Properties = props
	}
	if req, ok := params["required"]; ok {
		if arr, ok := req.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					schema.Required = append(schema.Required, s)
				}
			}
		}
	}
	return schema
}

// --- Params building ---

func (d *AnthropicDriver) resolveModel(req LLMRequest) anthropic.Model {
	if req.Model != "" {
		return anthropic.Model(req.Model)
	}
	return anthropic.Model(d.defaultModel)
}

func (d *AnthropicDriver) buildParams(req LLMRequest, tools []ToolDef) anthropic.MessageNewParams {
	maxTokens := int64(req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = int64(d.defaultMaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     d.resolveModel(req),
		Messages:  d.buildSDKMessages(req),
		MaxTokens: maxTokens,
	}

	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: req.SystemPrompt},
		}
	}

	if req.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Temperature)
	}

	if sdkTools := convertToolDefsToAnthropic(tools); len(sdkTools) > 0 {
		params.Tools = sdkTools
	}

	return params
}

// --- Call / CallWithTools ---

func (d *AnthropicDriver) callInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	params := d.buildParams(req, tools)
	msg, err := d.client.Messages.New(ctx, params)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, NewLLMError(d.name, 0, ErrTimeout)
		}
		return nil, d.classifyError(err)
	}
	return d.convertMessage(msg), nil
}

func (d *AnthropicDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	return d.callInternal(ctx, req, nil)
}

func (d *AnthropicDriver) CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	return d.callInternal(ctx, req, tools)
}

func (d *AnthropicDriver) convertMessage(msg *anthropic.Message) *LLMResponse {
	resp := &LLMResponse{
		InputTokens:       int(msg.Usage.InputTokens),
		OutputTokens:      int(msg.Usage.OutputTokens),
		CachedInputTokens: int(msg.Usage.CacheReadInputTokens),
		TokensUsed:        int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
	}

	var textParts, thinkingParts []string
	for _, block := range msg.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			clean, reasoning, hadSep := extractDeepSeekThinking(variant.Text)
			textParts = append(textParts, clean)
			if hadSep && reasoning != "" {
				thinkingParts = append(thinkingParts, reasoning)
				resp.ReasoningBlocks = append(resp.ReasoningBlocks, ReasoningBlock{
					Type:     "thinking",
					Thinking: reasoning,
				})
			}
		case anthropic.ThinkingBlock:
			thinkingParts = append(thinkingParts, variant.Thinking)
			resp.ReasoningBlocks = append(resp.ReasoningBlocks, ReasoningBlock{
				Type:      "thinking",
				Thinking:  variant.Thinking,
				Signature: variant.Signature,
			})
		case anthropic.RedactedThinkingBlock:
			resp.ReasoningBlocks = append(resp.ReasoningBlocks, ReasoningBlock{
				Type: "redacted_thinking",
				Data: variant.Data,
			})
		case anthropic.ToolUseBlock:
			tc := ToolCall{
				ID:   variant.ID,
				Name: variant.Name,
			}
			if len(variant.Input) > 0 {
				var input map[string]any
				if err := json.Unmarshal(variant.Input, &input); err != nil {
					tc.ParseError = fmt.Sprintf("invalid input JSON: %v; raw: %.200s", err, variant.Input)
				} else {
					tc.Input = input
				}
			}
			resp.ToolCalls = append(resp.ToolCalls, tc)
		}
	}
	resp.Content = strings.Join(textParts, "")
	resp.Reasoning = strings.Join(thinkingParts, "")
	return resp
}

// --- Stream / StreamWithTools ---

func (d *AnthropicDriver) streamInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	// Timeout applies as an idle timeout on the SSE event stream (see streamtimeout.go).
	ctx, idle, cancel := NewIdleTimer(ctx, timeout)

	params := d.buildParams(req, tools)
	stream := d.client.Messages.NewStreaming(ctx, params)

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer cancel()
		defer stream.Close()

		acc := anthropic.Message{}

		for stream.Next() {
			idle.Reset()
			event := stream.Current()
			if err := acc.Accumulate(event); err != nil {
				log.Printf("[llm] anthropic [%s]: accumulate error: %v", d.name, err)
			}

			switch variant := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				switch delta := variant.Delta.AsAny().(type) {
				case anthropic.TextDelta:
					if delta.Text != "" {
						select {
						case ch <- StreamEvent{Type: "content", Content: delta.Text}:
						case <-ctx.Done():
							return
						}
					}
				case anthropic.ThinkingDelta:
					if delta.Thinking != "" {
						select {
						case ch <- StreamEvent{Type: "reasoning", Content: delta.Thinking}:
						case <-ctx.Done():
							return
						}
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

		// Build done event from accumulated message
		evt := StreamEvent{
			Type:              "done",
			InputTokens:       int(acc.Usage.InputTokens),
			OutputTokens:      int(acc.Usage.OutputTokens),
			CachedInputTokens: int(acc.Usage.CacheReadInputTokens),
			TokensUsed:        int(acc.Usage.InputTokens + acc.Usage.OutputTokens),
		}

		// Extract reasoning blocks and tool calls from accumulated content.
		// populateDoneEventFromAcc also re-processes TextBlocks through
		// extractDeepSeekThinking so any DeepSeek-style textual thinking
		// separators in acc.Content are scrubbed from the final evt.Content
		// (vfsfile.go's done branch resets the streamed accumulator with
		// the cleaned version when evt.Content != "").
		populateDoneEventFromAcc(&acc, &evt)

		select {
		case ch <- evt:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (d *AnthropicDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, nil)
}

func (d *AnthropicDriver) StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, tools)
}

// --- Error classification ---

func (d *AnthropicDriver) classifyError(err error) error {
	if apiErr, ok := errors.AsType[*anthropic.Error](err); ok {
		msg := err.Error()
		switch apiErr.StatusCode {
		case 401:
			return NewLLMError(d.name, 401, fmt.Errorf("%s: %w", msg, ErrAuth))
		case 429:
			return NewLLMError(d.name, 429, fmt.Errorf("%s: %w", msg, ErrRateLimit))
		case 404:
			return NewLLMError(d.name, 404, fmt.Errorf("%s: %w", msg, ErrModelNotFound))
		case 400:
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "context") || strings.Contains(lower, "token") {
				return NewLLMError(d.name, 400, fmt.Errorf("%s: %w", msg, ErrContextLength))
			}
			return NewLLMError(d.name, 400, fmt.Errorf("%s", msg))
		default:
			if apiErr.StatusCode > 0 {
				return NewLLMError(d.name, apiErr.StatusCode, fmt.Errorf("%s", msg))
			}
		}
	}
	log.Printf("[llm] anthropic [%s]: unclassified error type %T: %v", d.name, err, err)
	return fmt.Errorf("anthropic [%s]: %w", d.name, err)
}

// --- DeepSeek thinking-block textual leak handling ---

// DeepSeek's anthropic-compat endpoint occasionally embeds thinking content
// inside TextBlock.Text using these literal separators instead of using the
// SDK's structured ThinkingBlock. They contain U+FF5C (FULLWIDTH VERTICAL
// LINE) and U+2581 (LOWER ONE EIGHTH BLOCK), so byte-level matching on the
// UTF-8 form is sufficient.
const (
	deepSeekThinkBegin = "<｜begin▁of▁thinking｜>"
	deepSeekThinkEnd   = "<｜end▁of▁thinking｜>"
)

// populateDoneEventFromAcc fills the reasoning/tool/leak-related fields of
// a "done" StreamEvent from the SDK's accumulated message content. It is
// extracted from streamInternal so leak handling can be unit-tested
// without a real SSE round-trip.
func populateDoneEventFromAcc(acc *anthropic.Message, evt *StreamEvent) {
	var cleanedTextParts []string
	var anyTextLeaked bool
	for _, block := range acc.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			clean, reasoning, hadSep := extractDeepSeekThinking(variant.Text)
			cleanedTextParts = append(cleanedTextParts, clean)
			if hadSep {
				anyTextLeaked = true
				if reasoning != "" {
					evt.ReasoningBlocks = append(evt.ReasoningBlocks, ReasoningBlock{
						Type:     "thinking",
						Thinking: reasoning,
					})
				}
			}
		case anthropic.ThinkingBlock:
			evt.ReasoningBlocks = append(evt.ReasoningBlocks, ReasoningBlock{
				Type:      "thinking",
				Thinking:  variant.Thinking,
				Signature: variant.Signature,
			})
		case anthropic.RedactedThinkingBlock:
			evt.ReasoningBlocks = append(evt.ReasoningBlocks, ReasoningBlock{
				Type: "redacted_thinking",
				Data: variant.Data,
			})
		case anthropic.ToolUseBlock:
			tc := ToolCall{
				ID:   variant.ID,
				Name: variant.Name,
			}
			if len(variant.Input) > 0 {
				var input map[string]any
				if err := json.Unmarshal(variant.Input, &input); err != nil {
					tc.ParseError = fmt.Sprintf("invalid input JSON: %v", err)
				} else {
					tc.Input = input
				}
			}
			evt.ToolCalls = append(evt.ToolCalls, tc)
		}
	}
	if anyTextLeaked {
		evt.Content = strings.Join(cleanedTextParts, "")
	}
}

// extractDeepSeekThinking splits a TextBlock body into clean content and
// thinking text whenever DeepSeek-style separators are present. When no
// separator appears, it returns the original text on a zero-allocation
// fast path. See spec-anthropic-deepseek-thinking-leak-fix for the full
// boundary rules.
func extractDeepSeekThinking(text string) (clean, reasoning string, hadSeparator bool) {
	if !strings.Contains(text, deepSeekThinkBegin) && !strings.Contains(text, deepSeekThinkEnd) {
		return text, "", false
	}

	var cleanB, reasoningB strings.Builder
	remaining := text
	for {
		begin := strings.Index(remaining, deepSeekThinkBegin)
		end := strings.Index(remaining, deepSeekThinkEnd)
		switch {
		case begin >= 0 && (end < 0 || begin < end):
			cleanB.WriteString(remaining[:begin])
			rest := remaining[begin+len(deepSeekThinkBegin):]
			inner, after, ok := strings.Cut(rest, deepSeekThinkEnd)
			reasoningB.WriteString(inner)
			if !ok {
				return cleanB.String(), reasoningB.String(), true
			}
			remaining = after
		case end >= 0:
			reasoningB.WriteString(remaining[:end])
			remaining = remaining[end+len(deepSeekThinkEnd):]
		default:
			cleanB.WriteString(remaining)
			return cleanB.String(), reasoningB.String(), true
		}
	}
}
