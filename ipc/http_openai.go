package ipc

import (
	"context"
	"encoding/json"
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

// handleChatCompletions is a stub — full implementation in Story 24.2.
func (s *OpenAIServer) handleChatCompletions(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "server_error", "not_implemented",
		"POST /v1/chat/completions is not yet implemented")
}

// handleListModels is a stub — full implementation in Story 24.4.
func (s *OpenAIServer) handleListModels(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "server_error", "not_implemented",
		"GET /v1/models is not yet implemented")
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

// ChatChunkChoice represents a single chunk choice in streaming mode.
type ChatChunkChoice struct {
	Index        int         `json:"index"`
	Delta        ChatMessage `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
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
