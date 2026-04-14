package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
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
}

// GeminiOption configures a GeminiDriver.
type GeminiOption func(*geminiDriverConfig)

type geminiDriverConfig struct {
	model          string
	timeout        time.Duration
	apiKey         string
	thinkingBudget int
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
	}
}

func (d *GeminiDriver) newClient(ctx context.Context) (*genai.Client, error) {
	return genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  d.apiKey,
		Backend: genai.BackendGeminiAPI,
	})
}

func (d *GeminiDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         d.name,
		Provider:     d.name,
		DefaultModel: d.defaultModel,
		DriverType:   DriverGemini,
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
	if d.thinkingBudget > 0 {
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
		if len(m.ToolCalls) > 0 {
			var parts []*genai.Part
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
		return &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: m.Content}},
		}
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
	ctx, cancel := context.WithTimeout(ctx, timeout)

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
			inputTokens  int
			outputTokens int
			totalTokens  int
			toolCalls    []ToolCall
		)

		for resp, err := range client.Models.GenerateContentStream(
			ctx,
			d.resolveModel(req),
			d.buildContents(req),
			d.buildConfig(req, tools),
		) {
			if err != nil {
				var llmErr error
				if ctx.Err() == context.DeadlineExceeded {
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
					select {
					case ch <- StreamEvent{Type: "reasoning", Content: part.Text}:
					case <-ctx.Done():
						return
					}
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
			Type:         "done",
			TokensUsed:   totalTokens,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			ToolCalls:    toolCalls,
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
			if apiErr.Code > 0 {
				return NewLLMError(d.name, apiErr.Code, fmt.Errorf("%s", msg))
			}
		}
	}
	// Log unexpected error shapes to aid debugging.
	log.Printf("[llm] gemini [%s]: unclassified error type %T: %v", d.name, err, err)
	return fmt.Errorf("gemini [%s]: %w", d.name, err)
}
