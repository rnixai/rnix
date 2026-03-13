package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
)

// ---------------------------------------------------------------------------
// OpenAIServer — HTTP server providing OpenAI-compatible API endpoints.
// ---------------------------------------------------------------------------

// OpenAIServer exposes an OpenAI-compatible HTTP API backed by the LLM
// DriverRegistry. It binds to a local address (default 127.0.0.1:8080) and
// routes requests to the appropriate LLM driver.
type OpenAIServer struct {
	driverReg  *llm.DriverRegistry
	listenAddr string       // default "127.0.0.1:8080"
	server     *http.Server // set by ListenAndServe
}

// NewOpenAIServer creates a new OpenAIServer holding a read-only reference to
// the given DriverRegistry and configured to listen on addr.
func NewOpenAIServer(driverReg *llm.DriverRegistry, addr string) *OpenAIServer {
	return &OpenAIServer{
		driverReg:  driverReg,
		listenAddr: addr,
	}
}

// buildMux creates the ServeMux with all registered routes.
func (s *OpenAIServer) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("GET /v1/models", s.handleListModels)
	mux.HandleFunc("GET /health", s.handleHealth)
	return mux
}

// ListenAndServe starts the HTTP server on the configured listen address.
func (s *OpenAIServer) ListenAndServe() error {
	mux := s.buildMux()
	s.server = &http.Server{
		Addr:              s.listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server.
func (s *OpenAIServer) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

// ---------------------------------------------------------------------------
// Route handlers
// ---------------------------------------------------------------------------

// handleHealth responds with server health status including provider count.
func (s *OpenAIServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{
		"status":    "ok",
		"providers": s.driverReg.Len(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleChatCompletions processes POST /v1/chat/completions requests.
// It parses the model string, finds the driver, converts the request,
// calls the LLM synchronously, and returns an OpenAI-compatible response.
func (s *OpenAIServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 4MB to prevent OOM from oversized payloads.
	const maxBodySize = 4 << 20 // 4 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid_request",
			"Invalid JSON in request body: "+err.Error())
		return
	}

	// Validate required fields
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid_request",
			"'model' is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid_request",
			"'messages' must be a non-empty array")
		return
	}

	// Parse model into provider + model name
	provider, modelName := parseModel(req.Model)

	// Look up driver
	driver, ok := s.driverReg.Get(provider)
	if !ok {
		names := s.driverReg.Names()
		writeError(w, http.StatusNotFound, "invalid_request_error", "model_not_found",
			fmt.Sprintf("Provider '%s' not found. Available providers: %s", provider, strings.Join(names, ", ")))
		return
	}

	// Convert request
	llmReq := toLLMRequest(req, modelName)

	// Branch: streaming vs synchronous
	if req.Stream {
		s.handleStreamingResponse(w, r, driver, llmReq, req.Model)
		return
	}

	// Synchronous path
	llmResp, err := driver.Call(r.Context(), llmReq)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Client disconnected — nothing useful to send back.
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "server_error", "timeout",
				"LLM request timed out")
			return
		}
		writeError(w, http.StatusBadGateway, "server_error", "upstream_error",
			"LLM driver returned an error")
		return
	}

	// Guard against buggy drivers returning (nil, nil).
	if llmResp == nil {
		writeError(w, http.StatusBadGateway, "server_error", "upstream_error",
			"LLM driver returned an empty response")
		return
	}

	// Convert and send response
	resp := toChatCompletionResponse(llmResp, req.Model)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleListModels is a stub — full implementation in Story 24.4.
func (s *OpenAIServer) handleListModels(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "server_error", "not_implemented",
		"GET /v1/models is not yet implemented")
}

// handleStreamingResponse handles the SSE streaming path for chat completions.
// It sets SSE headers, converts StreamEvents to ChatCompletionChunks, and
// writes them as SSE data lines with immediate flushing.
func (s *OpenAIServer) handleStreamingResponse(w http.ResponseWriter, r *http.Request, driver llm.LLMDriver, llmReq llm.LLMRequest, model string) {
	// Start the stream from the driver.
	eventCh, err := driver.Stream(r.Context(), llmReq)
	if err != nil {
		// SSE headers not yet sent — we can still return a normal JSON error.
		if errors.Is(err, context.Canceled) {
			// Client disconnected before stream started — nothing to send.
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "server_error", "timeout",
				"LLM stream request timed out")
			return
		}
		writeError(w, http.StatusBadGateway, "server_error", "upstream_error",
			"LLM driver failed to start stream")
		return
	}

	// Assert Flusher interface support.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "server_error", "internal_error",
			"Streaming not supported by server")
		return
	}

	// Write SSE response headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	streamID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	isFirst := true

	for event := range eventCh {
		// Check for client disconnect.
		if r.Context().Err() != nil {
			return
		}

		// Handle error events from the driver.
		if event.Type == "error" || event.Err != nil {
			errMsg := "LLM driver error during stream"
			if event.Content != "" {
				errMsg = event.Content
			}
			errResp := OpenAIErrorResponse{
				Error: OpenAIErrorDetail{
					Message: errMsg,
					Type:    "server_error",
					Code:    "stream_error",
				},
			}
			data, _ := json.Marshal(errResp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}

		chunk := toChatCompletionChunk(event, streamID, model, isFirst)
		if isFirst && event.Type == "content" {
			isFirst = false
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Write the [DONE] termination marker.
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// ---------------------------------------------------------------------------
// OpenAI-compatible request/response types
// ---------------------------------------------------------------------------

// ChatCompletionRequest mirrors the OpenAI chat completion request format.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// ChatMessage represents a single message in the conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionResponse mirrors the OpenAI chat completion response format.
type ChatCompletionResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *ChatUsage   `json:"usage,omitempty"`
}

// ChatChoice represents a single completion choice.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatCompletionChunk mirrors the OpenAI streaming chunk format.
type ChatCompletionChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ChatChunkChoice `json:"choices"`
}

// ChatDelta represents a streaming delta message with omitempty fields
// to match the OpenAI SSE format (empty fields are omitted, not "").
type ChatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ChatChunkChoice represents a single chunk choice in streaming mode.
type ChatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason,omitempty"`
}

// ChatUsage contains token usage statistics.
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Error response types (OpenAI-compatible)
// ---------------------------------------------------------------------------

// OpenAIErrorResponse wraps an error detail in the OpenAI error format.
type OpenAIErrorResponse struct {
	Error OpenAIErrorDetail `json:"error"`
}

// OpenAIErrorDetail contains the error message, type, and code.
type OpenAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// writeError writes an OpenAI-compatible JSON error response.
func writeError(w http.ResponseWriter, statusCode int, errType, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(OpenAIErrorResponse{
		Error: OpenAIErrorDetail{
			Message: message,
			Type:    errType,
			Code:    code,
		},
	})
}

// ---------------------------------------------------------------------------
// Conversion functions
// ---------------------------------------------------------------------------

// toLLMRequest converts a ChatCompletionRequest to an llm.LLMRequest.
// modelOverride is the model name extracted from parseModel; if empty the
// driver will use its configured default_model.
func toLLMRequest(req ChatCompletionRequest, modelOverride string) llm.LLMRequest {
	msgs := make([]llm.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = llm.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}
	return llm.LLMRequest{
		Model:       modelOverride,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
}

// toChatCompletionResponse converts an llm.LLMResponse to the OpenAI-compatible
// ChatCompletionResponse format.
func toChatCompletionResponse(llmResp *llm.LLMResponse, model string) ChatCompletionResponse {
	return ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatChoice{
			{
				Index:        0,
				Message:      ChatMessage{Role: "assistant", Content: llmResp.Content},
				FinishReason: "stop",
			},
		},
		Usage: &ChatUsage{
			PromptTokens:     llmResp.InputTokens,
			CompletionTokens: llmResp.OutputTokens,
			TotalTokens:      llmResp.TokensUsed,
		},
	}
}

// toChatCompletionChunk converts an llm.StreamEvent to the OpenAI-compatible
// ChatCompletionChunk format for SSE streaming. isFirst should be true for
// the first content chunk to include role:"assistant" per OpenAI spec.
func toChatCompletionChunk(event llm.StreamEvent, streamID, model string, isFirst bool) ChatCompletionChunk {
	choice := ChatChunkChoice{Index: 0}

	switch event.Type {
	case "content":
		choice.Delta = ChatDelta{Content: event.Content}
		if isFirst {
			choice.Delta.Role = "assistant"
		}
	case "done":
		choice.FinishReason = "stop"
	}

	return ChatCompletionChunk{
		ID:      streamID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatChunkChoice{choice},
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// parseModel splits a model string into provider and model name.
// Format: "provider:model" → (provider, model).
// If no colon is present, model is empty (use provider's default_model).
func parseModel(model string) (provider, modelName string) {
	if model == "" {
		return "", ""
	}
	parts := strings.SplitN(model, ":", 2)
	provider = parts[0]
	if len(parts) == 2 {
		modelName = parts[1]
	}
	return provider, modelName
}
