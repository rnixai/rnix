package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"
)

var (
	_ LLMDriver         = (*GeminiDriver)(nil)
	_ ToolCallingDriver = (*GeminiDriver)(nil)
	_ HealthChecker     = (*GeminiDriver)(nil)
)

// GeminiDriver implements LLMDriver, ToolCallingDriver, and HealthChecker
// using the official google.golang.org/genai SDK.
type GeminiDriver struct {
	name           string
	apiKey         string
	defaultModel   string
	defaultTimeout time.Duration
	thinkingBudget int
	thinkingLevel  string
	httpClient     *http.Client
	baseURL        string
}

// GeminiOption configures a GeminiDriver.
type GeminiOption func(*geminiDriverConfig)

type geminiDriverConfig struct {
	model          string
	timeout        time.Duration
	apiKey         string
	thinkingBudget int
	thinkingLevel  string
	httpClient     *http.Client
	baseURL        string
}

func WithGeminiModel(model string) GeminiOption {
	return func(c *geminiDriverConfig) { c.model = model }
}

func WithGeminiTimeout(d time.Duration) GeminiOption {
	return func(c *geminiDriverConfig) { c.timeout = d }
}

func WithGeminiAPIKey(key string) GeminiOption {
	return func(c *geminiDriverConfig) { c.apiKey = key }
}

func WithGeminiThinkingBudget(budget int) GeminiOption {
	return func(c *geminiDriverConfig) { c.thinkingBudget = budget }
}

// WithGeminiThinkingLevel sets ThinkingConfig.ThinkingLevel (migration target
// replacing thinking_budget for Gemini 3). When set it takes priority and the
// budget is NOT sent — Gemini 3 rejects requests carrying both level and budget
// (mutually exclusive). ThinkingLevel is an open string type whose Gemini enums
// are UPPERCASE (MINIMAL/LOW/MEDIUM/HIGH); the value is passed through verbatim,
// so a gemini provider must configure uppercase. Empty = unset.
func WithGeminiThinkingLevel(level string) GeminiOption {
	return func(c *geminiDriverConfig) { c.thinkingLevel = level }
}

// WithGeminiHTTPClient injects a custom *http.Client into ClientConfig.HTTPClient
// (Story 56.2 AC#10 注入口）。两个用途：
//
//  1. 测试 — httptest.NewServer 的 client 经此注入，无需起真网；
//  2. 生产 — 56.2 dev 接线后，gemini driver 用包了「捕获 RoundTripper」的
//     client 在 HTTP 层 tee 真实请求字节与原始 SSE 响应字节流（裁决 2）。
func WithGeminiHTTPClient(client *http.Client) GeminiOption {
	return func(c *geminiDriverConfig) { c.httpClient = client }
}

// WithGeminiBaseURL overrides the default Gemini base URL via
// HTTPOptions.BaseURL — primarily for testing with httptest.NewServer.
func WithGeminiBaseURL(baseURL string) GeminiOption {
	return func(c *geminiDriverConfig) { c.baseURL = baseURL }
}

// NewGeminiDriver creates a new driver backed by the official genai SDK.
func NewGeminiDriver(name string, opts ...GeminiOption) *GeminiDriver {
	cfg := geminiDriverConfig{timeout: DefaultTimeout}
	for _, o := range opts {
		o(&cfg)
	}
	return &GeminiDriver{
		name:           name,
		apiKey:         cfg.apiKey,
		defaultModel:   cfg.model,
		defaultTimeout: cfg.timeout,
		thinkingBudget: cfg.thinkingBudget,
		thinkingLevel:  cfg.thinkingLevel,
		httpClient:     cfg.httpClient,
		baseURL:        cfg.baseURL,
	}
}

func (d *GeminiDriver) newClient(ctx context.Context) (*genai.Client, error) {
	// 56.2 raw capture: 装载捕获 RoundTripper 到 ClientConfig.HTTPClient。
	// gemini SDK 是 per-Call/Stream newClient，每次构造都装上；ctx-scoped
	// sink 经 req.Context() 取，无 sink 时 RoundTrip fallback 到 base 转发。
	// nil-safe：d.httpClient 未注入也走 default transport（生产场景）。
	httpClient := wrapHTTPClientWithCapture(d.httpClient)
	cfg := &genai.ClientConfig{
		APIKey:     d.apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: httpClient,
	}
	if d.baseURL != "" {
		cfg.HTTPOptions = genai.HTTPOptions{BaseURL: d.baseURL}
	}
	return genai.NewClient(ctx, cfg)
}

func (d *GeminiDriver) Info() DriverInfo {
	return DriverInfo{
		Name:            d.name,
		Provider:        d.name,
		DefaultModel:    d.defaultModel,
		DriverType:      DriverGemini,
		ReasoningEffort: d.thinkingLevel,
	}
}

// HealthCheck performs a lightweight model lookup.
func (d *GeminiDriver) HealthCheck(ctx context.Context) error {
	client, err := d.newClient(ctx)
	if err != nil {
		return fmt.Errorf("health check: create client: %w", err)
	}
	_, err = client.Models.Get(ctx, "models/gemini-2.0-flash", nil)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	return nil
}

// resolveModel returns the request model or falls back to the driver default.
func (d *GeminiDriver) resolveModel(req LLMRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return d.defaultModel
}

// resolveThinkingLevel returns the per-request reasoning effort override, or the
// driver instance default (d.thinkingLevel) when the request leaves it empty.
// Value is passed through verbatim (note: Gemini enums are UPPERCASE; no
// validation/mapping/case normalization). Mirrors resolveModel.
func (d *GeminiDriver) resolveThinkingLevel(req LLMRequest) string {
	if req.ReasoningEffort != "" {
		return req.ReasoningEffort
	}
	return d.thinkingLevel
}

// buildConfig builds a GenerateContentConfig from the request.
func (d *GeminiDriver) buildConfig(req LLMRequest, tools []ToolDef) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{}

	if req.SystemPrompt != "" {
		cfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.SystemPrompt}},
		}
	}
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = int32(req.MaxTokens)
	}
	if req.Temperature != nil {
		t := float32(*req.Temperature)
		cfg.Temperature = &t
	}
	// Reasoning effort (migration target): ThinkingLevel takes priority when set.
	// Gemini 3 rejects requests carrying BOTH thinking_level and thinking_budget
	// (mutually exclusive) → when level is set the budget is intentionally NOT
	// sent. The budget path is RETAINED for Gemini ≤2.5. ThinkingLevel is an open
	// string type → passed through verbatim (note: Gemini enums are UPPERCASE).
	switch level := d.resolveThinkingLevel(req); {
	case level != "":
		cfg.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingLevel:   genai.ThinkingLevel(level),
		}
	case d.thinkingBudget > 0:
		budget := int32(d.thinkingBudget)
		cfg.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  &budget,
		}
	}
	if len(tools) > 0 {
		cfg.Tools = []*genai.Tool{{
			FunctionDeclarations: convertToolDefsToGenai(tools),
		}}
	}
	return cfg
}

func convertToolDefsToGenai(tools []ToolDef) []*genai.FunctionDeclaration {
	decls := make([]*genai.FunctionDeclaration, len(tools))
	for i, td := range tools {
		decl := &genai.FunctionDeclaration{
			Name:        td.Name,
			Description: td.Description,
		}
		// Only set ParametersJsonSchema when Parameters is non-nil and non-empty.
		// A nil map assigned to `any` creates a non-nil interface that serializes as
		// JSON null rather than being omitted, causing Gemini API to return HTTP 400.
		if len(td.Parameters) > 0 {
			decl.ParametersJsonSchema = td.Parameters
		}
		decls[i] = decl
	}
	return decls
}

// buildContents converts LLMRequest messages into []*genai.Content.
func (d *GeminiDriver) buildContents(req LLMRequest) []*genai.Content {
	var contents []*genai.Content

	if len(req.Messages) == 0 {
		if req.Intent != "" {
			contents = append(contents, &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: req.Intent}},
			})
		}
		return contents
	}

	// Pre-scan: build ToolCallID → function name map from assistant messages.
	nameByID := make(map[string]string)
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				nameByID[tc.ID] = tc.Name
			}
		}
	}

	for _, m := range req.Messages {
		if c := convertMsgToGenai(m, nameByID); c != nil {
			contents = append(contents, c)
		}
	}
	return contents
}

func convertMsgToGenai(m Message, nameByID map[string]string) *genai.Content {
	switch m.Role {
	case "user":
		return &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: m.Content}},
		}
	case "assistant":
		var parts []*genai.Part
		// Echo thought parts first so Gemini receives them in their
		// original order (thought → text → function_call). Round-tripping
		// the opaque ThoughtSignature is required for thinking + function
		// calling to keep prior reasoning context across turns.
		for _, rb := range m.ReasoningBlocks {
			if rb.Type != "thought" {
				continue // skip Anthropic-style blocks; not consumed by Gemini
			}
			if rb.Thinking == "" && len(rb.ThoughtSignature) == 0 {
				continue
			}
			parts = append(parts, &genai.Part{
				Text:             rb.Thinking,
				Thought:          true,
				ThoughtSignature: rb.ThoughtSignature,
			})
		}
		if len(m.ToolCalls) > 0 {
			if m.Content != "" {
				parts = append(parts, &genai.Part{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   tc.ID,
						Name: tc.Name,
						Args: tc.Input,
					},
				})
			}
			return &genai.Content{Role: "model", Parts: parts}
		}
		if m.Content != "" {
			parts = append(parts, &genai.Part{Text: m.Content})
		}
		if len(parts) == 0 {
			parts = []*genai.Part{{Text: ""}}
		}
		return &genai.Content{Role: "model", Parts: parts}
	case "tool":
		name := nameByID[m.ToolCallID]
		if name == "" {
			name = m.ToolCallID // fallback when name is unavailable
		}
		return &genai.Content{
			Role: "user",
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					ID:       m.ToolCallID,
					Name:     name,
					Response: map[string]any{"result": m.Content},
				},
			}},
		}
	default:
		return &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: m.Content}},
		}
	}
}

// extractResponse converts a GenerateContentResponse into an LLMResponse.
func extractResponse(resp *genai.GenerateContentResponse) *LLMResponse {
	out := &LLMResponse{}
	if resp.UsageMetadata != nil {
		out.InputTokens = int(resp.UsageMetadata.PromptTokenCount)
		out.OutputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
		out.CachedInputTokens = int(resp.UsageMetadata.CachedContentTokenCount)
		out.TokensUsed = int(resp.UsageMetadata.TotalTokenCount)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return out
	}

	var textParts, thoughtParts []string
	for _, part := range resp.Candidates[0].Content.Parts {
		switch {
		case part.FunctionCall != nil:
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:    part.FunctionCall.ID,
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			})
		case part.Thought:
			thoughtParts = append(thoughtParts, part.Text)
			// Persist the thought block verbatim including its opaque
			// signature: Gemini 2.5+ requires echoing thoughtSignature on
			// subsequent turns when function calling is involved or the
			// model loses prior reasoning context.
			out.ReasoningBlocks = append(out.ReasoningBlocks, ReasoningBlock{
				Type:             "thought",
				Thinking:         part.Text,
				ThoughtSignature: part.ThoughtSignature,
			})
		case part.Text != "":
			textParts = append(textParts, part.Text)
		}
	}
	out.Content = strings.Join(textParts, "")
	out.Reasoning = strings.Join(thoughtParts, "")
	return out
}

// --- Call / CallWithTools ---

func (d *GeminiDriver) callInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := d.newClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gemini [%s]: create client: %w", d.name, err)
	}

	resp, err := client.Models.GenerateContent(
		ctx,
		d.resolveModel(req),
		d.buildContents(req),
		d.buildConfig(req, tools),
	)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, NewLLMError(d.name, 0, ErrTimeout)
		}
		return nil, d.classifyError(err)
	}
	return extractResponse(resp), nil
}

func (d *GeminiDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	return d.callInternal(ctx, req, nil)
}

func (d *GeminiDriver) CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error) {
	return d.callInternal(ctx, req, tools)
}

// --- Stream / StreamWithTools ---

func (d *GeminiDriver) streamInternal(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = d.defaultTimeout
	}
	// Timeout applies as an idle timeout on the stream iterator (see streamtimeout.go).
	ctx, idle, cancel := NewIdleTimer(ctx, timeout)

	client, err := d.newClient(ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gemini [%s]: create client: %w", d.name, err)
	}

	ch := make(chan StreamEvent, streamChanBuffer)

	go func() {
		defer close(ch)
		defer cancel()

		var (
			inputTokens       int
			outputTokens      int
			cachedInputTokens int
			totalTokens       int
			toolCalls         []ToolCall
			reasoningBlocks   []ReasoningBlock
		)

		for resp, err := range client.Models.GenerateContentStream(
			ctx,
			d.resolveModel(req),
			d.buildContents(req),
			d.buildConfig(req, tools),
		) {
			idle.Reset()
			if err != nil {
				var llmErr error
				if IsStreamTimeout(ctx) {
					llmErr = NewLLMError(d.name, 0, ErrTimeout)
				} else {
					llmErr = d.classifyError(err)
				}
				select {
				case ch <- StreamEvent{Type: "error", Err: llmErr}:
				case <-ctx.Done():
				}
				return
			}

			if resp.UsageMetadata != nil {
				inputTokens = int(resp.UsageMetadata.PromptTokenCount)
				outputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
				cachedInputTokens = int(resp.UsageMetadata.CachedContentTokenCount)
				totalTokens = int(resp.UsageMetadata.TotalTokenCount)
			}

			if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
				continue
			}

			for _, part := range resp.Candidates[0].Content.Parts {
				switch {
				case part.FunctionCall != nil:
					toolCalls = append(toolCalls, ToolCall{
						ID:    part.FunctionCall.ID,
						Name:  part.FunctionCall.Name,
						Input: part.FunctionCall.Args,
					})
				case part.Thought && part.Text != "":
					// Buffer the thought block (text + opaque signature)
					// so the done event can carry it for round-trip — the
					// streaming "reasoning" event is best-effort UI text
					// and cannot transport []byte signatures.
					reasoningBlocks = append(reasoningBlocks, ReasoningBlock{
						Type:             "thought",
						Thinking:         part.Text,
						ThoughtSignature: part.ThoughtSignature,
					})
					select {
					case ch <- StreamEvent{Type: "reasoning", Content: part.Text}:
					case <-ctx.Done():
						return
					}
				case part.Thought && len(part.ThoughtSignature) > 0:
					// Signature-only thought parts (no text) still need
					// to round-trip — Gemini sometimes emits the signature
					// on a separate part from the thought text.
					reasoningBlocks = append(reasoningBlocks, ReasoningBlock{
						Type:             "thought",
						ThoughtSignature: part.ThoughtSignature,
					})
				case !part.Thought && part.Text != "":
					select {
					case ch <- StreamEvent{Type: "content", Content: part.Text}:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		evt := StreamEvent{
			Type:              "done",
			TokensUsed:        totalTokens,
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			CachedInputTokens: cachedInputTokens,
			ToolCalls:         toolCalls,
			ReasoningBlocks:   reasoningBlocks,
		}
		select {
		case ch <- evt:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (d *GeminiDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, nil)
}

func (d *GeminiDriver) StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error) {
	return d.streamInternal(ctx, req, tools)
}

// --- Error classification ---

// classifyError maps genai SDK errors into rnix LLMError types.
// The SDK returns genai.APIError (value type) with a Code field.
func (d *GeminiDriver) classifyError(err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.Message
		if msg == "" {
			msg = err.Error()
		}
		switch apiErr.Code {
		case 401:
			return NewLLMError(d.name, 401, fmt.Errorf("%s: %w", msg, ErrAuth))
		case 429:
			// Story 73.1 / AC4: body-evidence split (genai.APIError carries no
			// structured error.type; the message is the only evidence).
			return NewLLMError(d.name, 429, NewRateLimitError(classifyRateLimitBody(msg, ""), msg))
		case 529, 503:
			// Story 73.1 / AC3.
			return NewLLMError(d.name, apiErr.Code, NewRateLimitError(KindOverload, msg))
		case 404:
			return NewLLMError(d.name, 404, fmt.Errorf("%s: %w", msg, ErrModelNotFound))
		case 400:
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "context") || strings.Contains(lower, "token") {
				return NewLLMError(d.name, 400, fmt.Errorf("%s: %w", msg, ErrContextLength))
			}
			return NewLLMError(d.name, 400, fmt.Errorf("%s", msg))
		default:
			if apiErr.Code > 0 {
				return NewLLMError(d.name, apiErr.Code, fmt.Errorf("%s", msg))
			}
		}
	}
	// Log unexpected error shapes to aid debugging.
	log.Printf("[llm] gemini [%s]: unclassified error type %T: %v", d.name, err, err)
	return fmt.Errorf("gemini [%s]: %w", d.name, err)
}
