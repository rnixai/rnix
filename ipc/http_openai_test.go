package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
)

// ---------------------------------------------------------------------------
// Helper: create a minimal DriverRegistry with stub drivers for testing
// ---------------------------------------------------------------------------

// stubDriver implements llm.LLMDriver for testing purposes.
type stubDriver struct {
	info llm.DriverInfo
}

func (d *stubDriver) Call(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return &llm.LLMResponse{Content: "stub"}, nil
}

func (d *stubDriver) Stream(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent)
	close(ch)
	return ch, nil
}

func (d *stubDriver) Info() llm.DriverInfo { return d.info }

// configurableDriver allows tests to control Call and Stream behavior.
type configurableDriver struct {
	info     llm.DriverInfo
	callFn   func(ctx context.Context, req llm.LLMRequest) (*llm.LLMResponse, error)
	streamFn func(ctx context.Context, req llm.LLMRequest) (<-chan llm.StreamEvent, error)
}

func (d *configurableDriver) Call(ctx context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
	return d.callFn(ctx, req)
}

func (d *configurableDriver) Stream(ctx context.Context, req llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	if d.streamFn != nil {
		return d.streamFn(ctx, req)
	}
	ch := make(chan llm.StreamEvent)
	close(ch)
	return ch, nil
}

func (d *configurableDriver) Info() llm.DriverInfo { return d.info }

func newTestRegistry(t *testing.T) *llm.DriverRegistry {
	t.Helper()
	reg := llm.NewDriverRegistry()
	_ = reg.Register("claude", &stubDriver{info: llm.DriverInfo{
		Name: "claude", Provider: "claude", DefaultModel: "claude-3.5-sonnet", DriverType: "claude-cli",
	}})
	_ = reg.Register("ollama", &stubDriver{info: llm.DriverInfo{
		Name: "ollama", Provider: "ollama", DefaultModel: "llama3", DriverType: "openai-compat",
	}})
	return reg
}

// ===========================================================================
// AC #1: OpenAIServer 结构体和构造函数
// ===========================================================================

func TestNewOpenAIServer_DefaultAddr(t *testing.T) {
	// Given NewOpenAIServer 构造函数已实现
	// When 使用默认地址创建实例
	// Then 持有 DriverRegistry 引用且 listenAddr 正确
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	if srv == nil {
		t.Fatal("NewOpenAIServer returned nil")
	}
	if srv.listenAddr != "127.0.0.1:8080" {
		t.Errorf("listenAddr = %q, want %q", srv.listenAddr, "127.0.0.1:8080")
	}
	if srv.driverReg == nil {
		t.Error("driverReg is nil, want non-nil DriverRegistry reference")
	}
}

func TestNewOpenAIServer_CustomAddr(t *testing.T) {
	// Given NewOpenAIServer 构造函数已实现
	// When 使用自定义地址创建实例
	// Then listenAddr 为自定义地址
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:9090")

	if srv.listenAddr != "127.0.0.1:9090" {
		t.Errorf("listenAddr = %q, want %q", srv.listenAddr, "127.0.0.1:9090")
	}
}

// ===========================================================================
// AC #2: OpenAI 兼容请求/响应类型的 JSON 序列化
// ===========================================================================

func TestChatCompletionRequest_JSONTags(t *testing.T) {
	// Given ChatCompletionRequest 类型已定义
	// When JSON 序列化
	// Then 字段名与 OpenAI API 规范一致（snake_case）
	temp := float64(0.7)
	req := ChatCompletionRequest{
		Model: "ollama:llama3",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Stream:      false,
		Temperature: &temp,
		MaxTokens:   1024,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	requiredFields := []string{"model", "messages", "stream", "temperature", "max_tokens"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field %q in ChatCompletionRequest", field)
		}
	}
}

func TestChatCompletionResponse_JSONTags(t *testing.T) {
	// Given ChatCompletionResponse 类型已定义
	// When JSON 序列化
	// Then 字段名与 OpenAI API 规范一致
	resp := ChatCompletionResponse{
		ID:      "chatcmpl-test123",
		Object:  "chat.completion",
		Created: 1677858242,
		Model:   "ollama:llama3",
		Choices: []ChatChoice{
			{
				Index:        0,
				Message:      ChatMessage{Role: "assistant", Content: "Hello!"},
				FinishReason: "stop",
			},
		},
		Usage: &ChatUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	requiredFields := []string{"id", "object", "created", "model", "choices", "usage"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field %q in ChatCompletionResponse", field)
		}
	}

	// Verify nested choice fields
	choices, ok := raw["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatal("choices is not a non-empty array")
	}
	choice := choices[0].(map[string]any)
	for _, field := range []string{"index", "message", "finish_reason"} {
		if _, ok := choice[field]; !ok {
			t.Errorf("missing JSON field %q in ChatChoice", field)
		}
	}
}

func TestChatCompletionChunk_JSONTags(t *testing.T) {
	// Given ChatCompletionChunk 类型已定义（用于流式响应）
	// When JSON 序列化
	// Then 字段名与 OpenAI API 规范一致
	chunk := ChatCompletionChunk{
		ID:      "chatcmpl-test123",
		Object:  "chat.completion.chunk",
		Created: 1677858242,
		Model:   "ollama:llama3",
		Choices: []ChatChunkChoice{
			{
				Index:        0,
				Delta:        ChatDelta{Content: "Hello"},
				FinishReason: "",
			},
		},
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	requiredFields := []string{"id", "object", "created", "model", "choices"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field %q in ChatCompletionChunk", field)
		}
	}
}

func TestChatMessage_JSONTags(t *testing.T) {
	// Given ChatMessage 类型已定义
	// When JSON 序列化
	// Then role 和 content 字段正确
	msg := ChatMessage{Role: "user", Content: "test message"}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if raw["role"] != "user" {
		t.Errorf("role = %v, want %q", raw["role"], "user")
	}
	if raw["content"] != "test message" {
		t.Errorf("content = %v, want %q", raw["content"], "test message")
	}
}

func TestChatUsage_JSONTags(t *testing.T) {
	// Given ChatUsage 类型已定义
	// When JSON 序列化
	// Then 字段名使用 snake_case
	usage := ChatUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}

	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	for _, field := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field %q in ChatUsage", field)
		}
	}
}

func TestChatCompletionRequest_JSONRoundTrip(t *testing.T) {
	// Given OpenAI 规范中的 JSON 请求体
	// When 反序列化到 ChatCompletionRequest
	// Then 所有字段正确映射
	input := `{
		"model": "ollama:llama3",
		"messages": [
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"}
		],
		"stream": false,
		"temperature": 0.7,
		"max_tokens": 1024
	}`

	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if req.Model != "ollama:llama3" {
		t.Errorf("Model = %q, want %q", req.Model, "ollama:llama3")
	}
	if len(req.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("Messages[0].Role = %q, want %q", req.Messages[0].Role, "system")
	}
	if req.Stream {
		t.Error("Stream = true, want false")
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", req.Temperature)
	}
	if req.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", req.MaxTokens)
	}
}

// ===========================================================================
// AC #3: parseModel 路由函数
// ===========================================================================

func TestParseModel(t *testing.T) {
	// Given parseModel 函数已实现
	// When 输入不同格式的 model 字符串
	// Then 正确解析 provider 和 model 名

	tests := []struct {
		input        string
		wantProvider string
		wantModel    string
	}{
		// provider:model 格式
		{input: "ollama:llama3", wantProvider: "ollama", wantModel: "llama3"},
		{input: "cursor:claude-3.5-sonnet", wantProvider: "cursor", wantModel: "claude-3.5-sonnet"},
		{input: "claude:claude-3-opus", wantProvider: "claude", wantModel: "claude-3-opus"},
		// 仅 provider 名（model 为空）
		{input: "ollama", wantProvider: "ollama", wantModel: ""},
		{input: "claude", wantProvider: "claude", wantModel: ""},
		// model 名中含 : 的情况（SplitN 限制分割次数为 2）
		{input: "provider:model:with:colons", wantProvider: "provider", wantModel: "model:with:colons"},
		// 空字符串
		{input: "", wantProvider: "", wantModel: ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			provider, model := parseModel(tt.input)
			if provider != tt.wantProvider {
				t.Errorf("parseModel(%q) provider = %q, want %q", tt.input, provider, tt.wantProvider)
			}
			if model != tt.wantModel {
				t.Errorf("parseModel(%q) model = %q, want %q", tt.input, model, tt.wantModel)
			}
		})
	}
}

// ===========================================================================
// AC #4: OpenAI 兼容错误响应
// ===========================================================================

func TestWriteError_ModelNotFound(t *testing.T) {
	// Given writeError 辅助函数已实现
	// When provider 不存在
	// Then 返回 HTTP 404 + OpenAI 标准错误格式
	w := httptest.NewRecorder()
	writeError(w, http.StatusNotFound, "invalid_request_error", "model_not_found",
		"Provider 'nonexistent' not found")

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	body, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("error.type = %q, want %q", errResp.Error.Type, "invalid_request_error")
	}
	if errResp.Error.Code != "model_not_found" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "model_not_found")
	}
	if errResp.Error.Message == "" {
		t.Error("error.message is empty, want non-empty")
	}
}

func TestWriteError_InvalidRequest(t *testing.T) {
	// Given writeError 辅助函数已实现
	// When 请求体格式错误
	// Then 返回 HTTP 400 + error.code: "invalid_request"
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid_request",
		"Invalid JSON in request body")

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	body, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if errResp.Error.Code != "invalid_request" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "invalid_request")
	}
}

func TestWriteError_ContentType(t *testing.T) {
	// Given writeError 辅助函数已实现
	// When 写入错误响应
	// Then Content-Type 为 application/json
	w := httptest.NewRecorder()
	writeError(w, http.StatusInternalServerError, "server_error", "upstream_error", "test")

	resp := w.Result()
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want to contain %q", ct, "application/json")
	}
}

func TestOpenAIErrorResponse_JSONFormat(t *testing.T) {
	// Given OpenAIErrorResponse 结构体已定义
	// When JSON 序列化
	// Then 输出格式为 {"error": {"message": ..., "type": ..., "code": ...}}
	errResp := OpenAIErrorResponse{
		Error: OpenAIErrorDetail{
			Message: "test error",
			Type:    "invalid_request_error",
			Code:    "model_not_found",
		},
	}

	data, err := json.Marshal(errResp)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	errObj, ok := raw["error"].(map[string]any)
	if !ok {
		t.Fatal("missing or invalid 'error' field in JSON output")
	}
	for _, field := range []string{"message", "type", "code"} {
		if _, ok := errObj[field]; !ok {
			t.Errorf("missing JSON field %q in error object", field)
		}
	}
}

// ===========================================================================
// AC #5: /health 端点
// ===========================================================================

func TestHandleHealth_Returns200(t *testing.T) {
	// Given /health 端点已实现
	// When GET /health
	// Then 返回 HTTP 200
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleHealth_ReturnsJSON(t *testing.T) {
	// Given /health 端点已实现
	// When GET /health
	// Then 返回 JSON 格式，Content-Type 为 application/json
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	resp := w.Result()
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want to contain %q", ct, "application/json")
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

func TestHandleHealth_ContainsStatus(t *testing.T) {
	// Given /health 端点已实现
	// When GET /health
	// Then 响应体包含 status 字段
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if _, ok := result["status"]; !ok {
		t.Error("response missing 'status' field")
	}
}

func TestHandleHealth_ContainsProviders(t *testing.T) {
	// Given /health 端点已实现且 registry 有 2 个 provider
	// When GET /health
	// Then 响应体包含 providers 数量信息
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	providers, ok := result["providers"]
	if !ok {
		t.Fatal("response missing 'providers' field")
	}
	// providers should be a number equal to 2 (claude + ollama)
	count, ok := providers.(float64)
	if !ok {
		t.Fatalf("providers is not a number: %T", providers)
	}
	if int(count) != 2 {
		t.Errorf("providers = %d, want 2", int(count))
	}
}

// ===========================================================================
// AC #6: 安全绑定配置
// ===========================================================================

func TestNewOpenAIServer_DefaultBindsLocalhost(t *testing.T) {
	// Given 安全绑定配置
	// When 使用默认地址创建 OpenAIServer
	// Then 绑定 127.0.0.1，不暴露到外部网络接口
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	if !strings.HasPrefix(srv.listenAddr, "127.0.0.1") {
		t.Errorf("listenAddr = %q, want to start with %q (security: NFR52)", srv.listenAddr, "127.0.0.1")
	}
}

func TestNewOpenAIServer_WildcardAddrNotDefault(t *testing.T) {
	// Given 安全绑定配置
	// When 使用默认 127.0.0.1 地址构造
	// Then listenAddr 不包含 0.0.0.0（NFR52: 默认不暴露外部接口）
	reg := newTestRegistry(t)
	defaultSrv := NewOpenAIServer(reg, "127.0.0.1:8080")

	if strings.HasPrefix(defaultSrv.listenAddr, "0.0.0.0") {
		t.Error("default listenAddr should not bind to 0.0.0.0 (security: NFR52)")
	}
}

// ===========================================================================
// AC #1: 路由注册验证 — stub 端点返回 501
// ===========================================================================

func TestHandleChatCompletions_Success(t *testing.T) {
	// Given handleChatCompletions is now fully implemented (Story 24.2)
	// When POST /v1/chat/completions with valid request
	// Then returns 200 with ChatCompletionResponse
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"ollama:llama3","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if chatResp.Object != "chat.completion" {
		t.Errorf("object = %q, want %q", chatResp.Object, "chat.completion")
	}
	if !strings.HasPrefix(chatResp.ID, "chatcmpl-") {
		t.Errorf("id = %q, want prefix %q", chatResp.ID, "chatcmpl-")
	}
	if chatResp.Model != "ollama:llama3" {
		t.Errorf("model = %q, want %q", chatResp.Model, "ollama:llama3")
	}
	if len(chatResp.Choices) != 1 {
		t.Fatalf("len(choices) = %d, want 1", len(chatResp.Choices))
	}
	if chatResp.Choices[0].Message.Role != "assistant" {
		t.Errorf("choices[0].message.role = %q, want %q", chatResp.Choices[0].Message.Role, "assistant")
	}
	if chatResp.Choices[0].FinishReason != "stop" {
		t.Errorf("choices[0].finish_reason = %q, want %q", chatResp.Choices[0].FinishReason, "stop")
	}
}

func TestHandleListModels_ReturnsModelList(t *testing.T) {
	// Given handleListModels is implemented (Story 24.4)
	// When GET /v1/models
	// Then returns 200 with model list
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	srv.handleListModels(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body ModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body.Object != "list" {
		t.Errorf("object = %q, want %q", body.Object, "list")
	}
}

// ===========================================================================
// AC #1: ServeMux 路由集成测试
// ===========================================================================

func TestOpenAIServer_RoutesRegistered(t *testing.T) {
	// Given OpenAIServer 路由注册已实现
	// When 通过 ServeMux 发送请求
	// Then 各端点正确路由

	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")
	mux := srv.buildMux()

	tests := []struct {
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{http.MethodGet, "/health", "", http.StatusOK},
		{http.MethodPost, "/v1/chat/completions", `{"model":"ollama:llama3","messages":[{"role":"user","content":"hi"}]}`, http.StatusOK},
		{http.MethodGet, "/v1/models", "", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var bodyReader io.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Result().StatusCode != tt.wantStatus {
				t.Errorf("%s %s: status = %d, want %d", tt.method, tt.path, w.Result().StatusCode, tt.wantStatus)
			}
		})
	}
}

// ===========================================================================
// 补充: Shutdown 方法存在性验证
// ===========================================================================

func TestOpenAIServer_ShutdownExists(t *testing.T) {
	// Given OpenAIServer.Shutdown 方法已实现
	// When 未启动 ListenAndServe 时调用 Shutdown
	// Then 不应 panic
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	// Shutdown on an unstarted server should be safe
	if err := srv.Shutdown(context.Background()); err != nil {
		// Allow error (server not started), but should not panic
		t.Logf("Shutdown returned error (expected for unstarted server): %v", err)
	}
}

// ===========================================================================
// Story 24.2: /v1/chat/completions 同步模式
// ===========================================================================

// --- AC #1: 成功同步请求 ---

func TestChatCompletions_SyncSuccess_FullResponse(t *testing.T) {
	// Given mock driver returns a fixed response with token usage
	// When POST /v1/chat/completions with valid request
	// Then returns complete ChatCompletionResponse with correct format
	reg := llm.NewDriverRegistry()
	_ = reg.Register("ollama", &configurableDriver{
		info: llm.DriverInfo{Name: "ollama", Provider: "ollama", DefaultModel: "llama3", DriverType: "openai-compat"},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			return &llm.LLMResponse{
				Content:      "Hello! How can I help you?",
				TokensUsed:   25,
				InputTokens:  10,
				OutputTokens: 15,
			}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"ollama:llama3","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Verify response structure
	if !strings.HasPrefix(chatResp.ID, "chatcmpl-") {
		t.Errorf("id = %q, want prefix %q", chatResp.ID, "chatcmpl-")
	}
	if chatResp.Object != "chat.completion" {
		t.Errorf("object = %q, want %q", chatResp.Object, "chat.completion")
	}
	if chatResp.Created == 0 {
		t.Error("created = 0, want non-zero timestamp")
	}
	if chatResp.Model != "ollama:llama3" {
		t.Errorf("model = %q, want %q", chatResp.Model, "ollama:llama3")
	}

	// Verify choices
	if len(chatResp.Choices) != 1 {
		t.Fatalf("len(choices) = %d, want 1", len(chatResp.Choices))
	}
	choice := chatResp.Choices[0]
	if choice.Index != 0 {
		t.Errorf("choices[0].index = %d, want 0", choice.Index)
	}
	if choice.Message.Role != "assistant" {
		t.Errorf("choices[0].message.role = %q, want %q", choice.Message.Role, "assistant")
	}
	if choice.Message.Content != "Hello! How can I help you?" {
		t.Errorf("choices[0].message.content = %q, want %q", choice.Message.Content, "Hello! How can I help you?")
	}
	if choice.FinishReason != "stop" {
		t.Errorf("choices[0].finish_reason = %q, want %q", choice.FinishReason, "stop")
	}

	// Verify usage
	if chatResp.Usage == nil {
		t.Fatal("usage is nil, want non-nil")
	}
	if chatResp.Usage.PromptTokens != 10 {
		t.Errorf("usage.prompt_tokens = %d, want 10", chatResp.Usage.PromptTokens)
	}
	if chatResp.Usage.CompletionTokens != 15 {
		t.Errorf("usage.completion_tokens = %d, want 15", chatResp.Usage.CompletionTokens)
	}
	if chatResp.Usage.TotalTokens != 25 {
		t.Errorf("usage.total_tokens = %d, want 25", chatResp.Usage.TotalTokens)
	}

	// Verify Content-Type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want to contain %q", ct, "application/json")
	}
}

// --- AC #2: 仅 provider 名（使用 default_model）---

func TestChatCompletions_ProviderOnly_UsesDefaultModel(t *testing.T) {
	// Given model parameter is just provider name (no colon)
	// When POST /v1/chat/completions with model="ollama"
	// Then request is processed using provider's default_model
	var capturedReq llm.LLMRequest
	reg := llm.NewDriverRegistry()
	_ = reg.Register("ollama", &configurableDriver{
		info: llm.DriverInfo{Name: "ollama", Provider: "ollama", DefaultModel: "llama3", DriverType: "openai-compat"},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			capturedReq = req
			return &llm.LLMResponse{Content: "ok"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"ollama","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Result().StatusCode)
	}
	// When no model specified after colon, LLMRequest.Model should be empty
	// allowing the driver to use its default_model
	if capturedReq.Model != "" {
		t.Errorf("LLMRequest.Model = %q, want empty (driver uses default_model)", capturedReq.Model)
	}
}

// --- AC #3: provider:model 复合格式 ---

func TestChatCompletions_ProviderModel_CompoundFormat(t *testing.T) {
	// Given model parameter uses provider:model format
	// When POST /v1/chat/completions with model="cursor:claude-3.5-sonnet"
	// Then uses specified model override
	var capturedReq llm.LLMRequest
	reg := llm.NewDriverRegistry()
	_ = reg.Register("cursor", &configurableDriver{
		info: llm.DriverInfo{Name: "cursor", Provider: "cursor", DefaultModel: "default-model", DriverType: "cursor-cli"},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			capturedReq = req
			return &llm.LLMResponse{Content: "ok"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"cursor:claude-3.5-sonnet","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Result().StatusCode)
	}
	if capturedReq.Model != "claude-3.5-sonnet" {
		t.Errorf("LLMRequest.Model = %q, want %q", capturedReq.Model, "claude-3.5-sonnet")
	}
}

// --- AC #4: provider 不存在 → 404 ---

func TestChatCompletions_ProviderNotFound_404(t *testing.T) {
	// Given request is sent to nonexistent provider
	// When POST /v1/chat/completions with model="nonexistent"
	// Then returns HTTP 404 + model_not_found + available provider list
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"nonexistent","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if errResp.Error.Code != "model_not_found" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "model_not_found")
	}
	if !strings.Contains(errResp.Error.Message, "nonexistent") {
		t.Errorf("error.message should contain provider name, got %q", errResp.Error.Message)
	}
	// Verify available providers are listed
	if !strings.Contains(errResp.Error.Message, "claude") || !strings.Contains(errResp.Error.Message, "ollama") {
		t.Errorf("error.message should list available providers, got %q", errResp.Error.Message)
	}
}

// --- AC #5: JSON 解析失败 → 400 ---

func TestChatCompletions_InvalidJSON_400(t *testing.T) {
	// Given request body contains invalid JSON
	// When POST /v1/chat/completions
	// Then returns HTTP 400 + invalid_request
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if errResp.Error.Code != "invalid_request" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "invalid_request")
	}
}

// --- messages 为空 → 400 ---

func TestChatCompletions_EmptyMessages_400(t *testing.T) {
	// Given messages array is empty
	// When POST /v1/chat/completions
	// Then returns HTTP 400
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"ollama","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if errResp.Error.Code != "invalid_request" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "invalid_request")
	}
}

// --- model 为空 → 400 ---

func TestChatCompletions_EmptyModel_400(t *testing.T) {
	// Given model field is empty
	// When POST /v1/chat/completions
	// Then returns HTTP 400
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// --- 请求体过大 → 400 ---

func TestChatCompletions_OversizedBody_400(t *testing.T) {
	// Given request body exceeds 4MB limit
	// When POST /v1/chat/completions
	// Then returns HTTP 400 (MaxBytesReader triggers error)
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	// Create a body just over 4MB
	bigContent := strings.Repeat("x", 5<<20) // 5 MB
	body := fmt.Sprintf(`{"model":"ollama","messages":[{"role":"user","content":"%s"}]}`, bigContent)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (oversized body)", resp.StatusCode, http.StatusBadRequest)
	}
}

// --- AC #5: driver.Call 超时 → 504 ---

func TestChatCompletions_DriverTimeout_504(t *testing.T) {
	// Given driver.Call returns context.DeadlineExceeded
	// When POST /v1/chat/completions
	// Then returns HTTP 504 + timeout
	reg := llm.NewDriverRegistry()
	_ = reg.Register("slow", &configurableDriver{
		info: llm.DriverInfo{Name: "slow", Provider: "slow", DefaultModel: "model", DriverType: "test"},
		callFn: func(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
			return nil, context.DeadlineExceeded
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"slow","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusGatewayTimeout)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if errResp.Error.Code != "timeout" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "timeout")
	}
}

// --- driver.Call 内部错误 → 502 (no error detail leak) ---

func TestChatCompletions_DriverError_502(t *testing.T) {
	// Given driver.Call returns a non-timeout error
	// When POST /v1/chat/completions
	// Then returns HTTP 502 + upstream_error (without leaking internal error details)
	reg := llm.NewDriverRegistry()
	_ = reg.Register("broken", &configurableDriver{
		info: llm.DriverInfo{Name: "broken", Provider: "broken", DefaultModel: "model", DriverType: "test"},
		callFn: func(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
			return nil, fmt.Errorf("internal driver failure: secret_api_key=abc123")
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"broken","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if errResp.Error.Code != "upstream_error" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "upstream_error")
	}
	// Verify internal error details are NOT leaked to the client
	if strings.Contains(errResp.Error.Message, "secret_api_key") {
		t.Errorf("error.message leaks internal details: %q", errResp.Error.Message)
	}
	if strings.Contains(errResp.Error.Message, "internal driver failure") {
		t.Errorf("error.message leaks internal error text: %q", errResp.Error.Message)
	}
}

// --- context.Canceled (client disconnect) → silent return ---

func TestChatCompletions_ClientDisconnect_ContextCanceled(t *testing.T) {
	// Given driver.Call returns context.Canceled (client disconnected)
	// When POST /v1/chat/completions
	// Then handler returns silently (no error body written)
	reg := llm.NewDriverRegistry()
	_ = reg.Register("slow", &configurableDriver{
		info: llm.DriverInfo{Name: "slow", Provider: "slow", DefaultModel: "model", DriverType: "test"},
		callFn: func(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
			return nil, context.Canceled
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"slow","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	// When client disconnects, handler returns silently; default status is 200
	// (no explicit WriteHeader was called, which is the correct Go behavior
	// for "connection gone, nothing to write")
	respBody, _ := io.ReadAll(resp.Body)
	if len(respBody) > 0 {
		// If body is written, it should NOT be an error response
		var errResp OpenAIErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Code != "" {
			t.Errorf("should not write error response on client disconnect, got code=%q", errResp.Error.Code)
		}
	}
}

// --- nil LLMResponse (buggy driver) → 502 ---

func TestChatCompletions_NilResponse_502(t *testing.T) {
	// Given driver.Call returns (nil, nil) — a buggy driver
	// When POST /v1/chat/completions
	// Then returns HTTP 502 instead of panicking
	reg := llm.NewDriverRegistry()
	_ = reg.Register("buggy", &configurableDriver{
		info: llm.DriverInfo{Name: "buggy", Provider: "buggy", DefaultModel: "model", DriverType: "test"},
		callFn: func(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
			return nil, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"buggy","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if errResp.Error.Code != "upstream_error" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "upstream_error")
	}
}

// --- stream:true → 501 ---

// --- Story 24.3: SSE Streaming Response Tests ---
// (Replaces TestChatCompletions_StreamTrue_501 which tested the 501 stub)

// makeStreamEvents creates a channel that emits the given content strings as
// StreamEvents, followed by a "done" event, then closes.
func makeStreamEvents(contents ...string) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent, len(contents)+1)
	for _, c := range contents {
		ch <- llm.StreamEvent{Type: "content", Content: c}
	}
	ch <- llm.StreamEvent{Type: "done"}
	close(ch)
	return ch
}

// parseSSELines extracts all "data: " lines from an SSE response body.
func parseSSELines(body string) []string {
	var lines []string
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			lines = append(lines, line)
		}
	}
	return lines
}

// parseSSEChunks parses SSE data lines (excluding [DONE]) into ChatCompletionChunk slices.
func parseSSEChunks(t *testing.T, body string) []ChatCompletionChunk {
	t.Helper()
	var chunks []ChatCompletionChunk
	for _, line := range parseSSELines(body) {
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			continue
		}
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("failed to parse SSE chunk JSON %q: %v", payload, err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func newStreamingTestServer(t *testing.T, streamFn func(ctx context.Context, req llm.LLMRequest) (<-chan llm.StreamEvent, error)) *OpenAIServer {
	t.Helper()
	reg := llm.NewDriverRegistry()
	_ = reg.Register("ollama", &configurableDriver{
		info: llm.DriverInfo{Name: "ollama", Provider: "ollama", DefaultModel: "llama3", DriverType: "openai-compat"},
		callFn: func(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
			return &llm.LLMResponse{Content: "sync-response"}, nil
		},
		streamFn: streamFn,
	})
	return NewOpenAIServer(reg, "127.0.0.1:8080")
}

func TestStreamingResponse_SSEHeaders(t *testing.T) {
	// AC1: SSE response headers are correctly set
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("hello"), nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want %q", conn, "keep-alive")
	}
}

func TestStreamingResponse_ChunkFormat(t *testing.T) {
	// AC2: Each chunk written as "data: {json}\n\n" format
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("Hello", " world", "!"), nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	respBody := w.Body.String()
	sseLines := parseSSELines(respBody)

	// 3 content chunks + 1 done chunk + 1 [DONE] marker = 5 data lines
	if len(sseLines) != 5 {
		t.Fatalf("got %d SSE data lines, want 5; body:\n%s", len(sseLines), respBody)
	}

	// Verify each non-DONE line is valid JSON
	chunks := parseSSEChunks(t, respBody)
	if len(chunks) != 4 { // 3 content + 1 done
		t.Errorf("got %d parsed chunks, want 4", len(chunks))
	}
}

func TestStreamingResponse_ChunkFields(t *testing.T) {
	// AC2: ChatCompletionChunk fields are correct
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("hi"), nil
	})

	body := `{"model":"ollama:llama3","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	chunks := parseSSEChunks(t, w.Body.String())
	if len(chunks) < 1 {
		t.Fatal("expected at least 1 chunk")
	}

	first := chunks[0]
	if first.Object != "chat.completion.chunk" {
		t.Errorf("object = %q, want %q", first.Object, "chat.completion.chunk")
	}
	if first.Model != "ollama:llama3" {
		t.Errorf("model = %q, want %q", first.Model, "ollama:llama3")
	}
	if first.Created == 0 {
		t.Error("created = 0, want valid Unix timestamp")
	}
	if len(first.Choices) != 1 {
		t.Fatalf("choices length = %d, want 1", len(first.Choices))
	}
	if first.Choices[0].Delta.Content != "hi" {
		t.Errorf("delta.content = %q, want %q", first.Choices[0].Delta.Content, "hi")
	}
	// OpenAI spec: first content chunk includes role:"assistant"
	if first.Choices[0].Delta.Role != "assistant" {
		t.Errorf("delta.role = %q, want %q (first chunk should include role)", first.Choices[0].Delta.Role, "assistant")
	}
}

func TestStreamingResponse_ConsistentChunkID(t *testing.T) {
	// AC2: All chunks in the same stream share the same ID
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("a", "b", "c"), nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	chunks := parseSSEChunks(t, w.Body.String())
	if len(chunks) < 2 {
		t.Fatal("expected at least 2 chunks")
	}

	firstID := chunks[0].ID
	if !strings.HasPrefix(firstID, "chatcmpl-") {
		t.Errorf("chunk ID = %q, want prefix %q", firstID, "chatcmpl-")
	}
	for i, chunk := range chunks[1:] {
		if chunk.ID != firstID {
			t.Errorf("chunk[%d].ID = %q, want %q (same as first)", i+1, chunk.ID, firstID)
		}
	}
}

func TestStreamingResponse_FinishReason(t *testing.T) {
	// AC2: content chunks have empty finish_reason, done chunk has "stop"
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("hello"), nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	chunks := parseSSEChunks(t, w.Body.String())
	if len(chunks) != 2 { // 1 content + 1 done
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}

	// Content chunk: finish_reason should be empty (omitempty)
	if chunks[0].Choices[0].FinishReason != "" {
		t.Errorf("content chunk finish_reason = %q, want empty", chunks[0].Choices[0].FinishReason)
	}
	// Done chunk: finish_reason should be "stop"
	if chunks[1].Choices[0].FinishReason != "stop" {
		t.Errorf("done chunk finish_reason = %q, want %q", chunks[1].Choices[0].FinishReason, "stop")
	}
}

func TestStreamingResponse_DeltaRoleOnlyOnFirst(t *testing.T) {
	// OpenAI spec: first content chunk has role:"assistant", subsequent do not
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("Hello", " world"), nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	chunks := parseSSEChunks(t, w.Body.String())
	if len(chunks) < 3 { // 2 content + 1 done
		t.Fatalf("got %d chunks, want at least 3", len(chunks))
	}

	// First content chunk: role="assistant"
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk delta.role = %q, want %q", chunks[0].Choices[0].Delta.Role, "assistant")
	}
	// Second content chunk: role should be empty (omitted in JSON)
	if chunks[1].Choices[0].Delta.Role != "" {
		t.Errorf("second chunk delta.role = %q, want empty", chunks[1].Choices[0].Delta.Role)
	}
	// Done chunk: delta should be empty (both role and content empty)
	if chunks[2].Choices[0].Delta.Role != "" || chunks[2].Choices[0].Delta.Content != "" {
		t.Errorf("done chunk delta = {role:%q, content:%q}, want empty delta",
			chunks[2].Choices[0].Delta.Role, chunks[2].Choices[0].Delta.Content)
	}
}

func TestStreamingResponse_StreamInitCanceled(t *testing.T) {
	// H1 fix: context.Canceled during stream init returns silently (no error body)
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return nil, context.Canceled
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)
	// Should return silently without writing an error body
	if len(respBody) > 0 {
		var errResp OpenAIErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Code != "" {
			t.Errorf("should not write error response on context.Canceled, got code=%q", errResp.Error.Code)
		}
	}
}

func TestStreamingResponse_MidStreamErrorContent(t *testing.T) {
	// M3 fix: error event content is propagated to SSE error response
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 2)
		ch <- llm.StreamEvent{Type: "error", Content: "rate limit exceeded"}
		close(ch)
		return ch, nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	sseLines := parseSSELines(w.Body.String())
	if len(sseLines) != 1 {
		t.Fatalf("got %d SSE lines, want 1; body:\n%s", len(sseLines), w.Body.String())
	}

	errPayload := strings.TrimPrefix(sseLines[0], "data: ")
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal([]byte(errPayload), &errResp); err != nil {
		t.Fatalf("failed to parse error SSE: %v", err)
	}
	if errResp.Error.Message != "rate limit exceeded" {
		t.Errorf("error.message = %q, want %q", errResp.Error.Message, "rate limit exceeded")
	}
}

func TestStreamingResponse_DoneMarker(t *testing.T) {
	// AC3: [DONE] termination marker is present as last SSE line
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("hi"), nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	sseLines := parseSSELines(w.Body.String())
	if len(sseLines) == 0 {
		t.Fatal("no SSE data lines found")
	}
	last := sseLines[len(sseLines)-1]
	if last != "data: [DONE]" {
		t.Errorf("last SSE line = %q, want %q", last, "data: [DONE]")
	}
}

func TestStreamingResponse_ClientDisconnect(t *testing.T) {
	// AC4: Client disconnect propagates context cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create a slow stream that blocks until context is cancelled
	srv := newStreamingTestServer(t, func(streamCtx context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent)
		go func() {
			defer close(ch)
			// Send one event
			select {
			case ch <- llm.StreamEvent{Type: "content", Content: "first"}:
			case <-streamCtx.Done():
				return
			}
			// Wait for context cancellation
			<-streamCtx.Done()
		}()
		return ch, nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.handleChatCompletions(w, req)
		close(done)
	}()

	// Cancel context to simulate client disconnect
	cancel()

	// Handler should finish without blocking
	select {
	case <-done:
		// Success: handler returned after context cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancellation (deadlock?)")
	}
}

func TestStreamingResponse_StreamInitError(t *testing.T) {
	// AC5: Stream initialization error returns JSON error (not SSE)
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return nil, fmt.Errorf("driver initialization failed")
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var errResp OpenAIErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != "upstream_error" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "upstream_error")
	}
}

func TestStreamingResponse_StreamInitTimeout(t *testing.T) {
	// AC5: Stream initialization timeout returns 504
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return nil, context.DeadlineExceeded
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusGatewayTimeout)
	}

	var errResp OpenAIErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != "timeout" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "timeout")
	}
}

func TestStreamingResponse_MidStreamError(t *testing.T) {
	// AC5: Error event during stream writes SSE error event
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 3)
		ch <- llm.StreamEvent{Type: "content", Content: "partial"}
		ch <- llm.StreamEvent{Type: "error", Err: fmt.Errorf("driver crashed")}
		close(ch)
		return ch, nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	respBody := w.Body.String()
	sseLines := parseSSELines(respBody)

	// Should have: 1 content chunk + 1 error event (no [DONE])
	if len(sseLines) != 2 {
		t.Fatalf("got %d SSE lines, want 2; body:\n%s", len(sseLines), respBody)
	}

	// First line should be a content chunk
	chunks := parseSSEChunks(t, "data: "+strings.TrimPrefix(sseLines[0], "data: ")+"\n\n")
	if len(chunks) != 1 || chunks[0].Choices[0].Delta.Content != "partial" {
		t.Errorf("first SSE line should be content chunk with 'partial'")
	}

	// Second line should be an error
	errPayload := strings.TrimPrefix(sseLines[1], "data: ")
	var errResp OpenAIErrorResponse
	if err := json.Unmarshal([]byte(errPayload), &errResp); err != nil {
		t.Fatalf("failed to parse error SSE: %v", err)
	}
	if errResp.Error.Code != "stream_error" {
		t.Errorf("error.code = %q, want %q", errResp.Error.Code, "stream_error")
	}

	// No [DONE] marker after error
	last := sseLines[len(sseLines)-1]
	if strings.Contains(last, "[DONE]") {
		t.Error("found [DONE] after error event, should not be present")
	}
}

func TestStreamingResponse_EmptyStream(t *testing.T) {
	// Edge case: channel closed immediately (no events)
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent)
		close(ch)
		return ch, nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	sseLines := parseSSELines(w.Body.String())
	// Only [DONE] marker expected
	if len(sseLines) != 1 {
		t.Fatalf("got %d SSE lines, want 1 (only [DONE]); body:\n%s", len(sseLines), w.Body.String())
	}
	if sseLines[0] != "data: [DONE]" {
		t.Errorf("SSE line = %q, want %q", sseLines[0], "data: [DONE]")
	}
}

func TestStreamingResponse_SyncModeRegression(t *testing.T) {
	// Regression: sync mode (stream:false) still works after streaming implementation
	srv := newStreamingTestServer(t, func(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
		return makeStreamEvents("should not be called"), nil
	})

	body := `{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var chatResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode sync response: %v", err)
	}
	if chatResp.Object != "chat.completion" {
		t.Errorf("object = %q, want %q", chatResp.Object, "chat.completion")
	}
	if chatResp.Choices[0].Message.Content != "sync-response" {
		t.Errorf("content = %q, want %q", chatResp.Choices[0].Message.Content, "sync-response")
	}
}

// --- AC #6: HTTP 处理开销 <= 50ms ---

func TestChatCompletions_OverheadUnder50ms(t *testing.T) {
	// Given a driver that returns instantly
	// When measuring HTTP handler overhead (excluding LLM inference)
	// Then overhead is <= 50ms (NFR50)
	reg := llm.NewDriverRegistry()
	_ = reg.Register("fast", &configurableDriver{
		info: llm.DriverInfo{Name: "fast", Provider: "fast", DefaultModel: "m", DriverType: "test"},
		callFn: func(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
			return &llm.LLMResponse{Content: "fast"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"fast","messages":[{"role":"user","content":"test"}]}`

	// Warm up
	warmReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	warmW := httptest.NewRecorder()
	srv.handleChatCompletions(warmW, warmReq)

	// Measure
	iterations := 100
	start := time.Now()
	for range iterations {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		w := httptest.NewRecorder()
		srv.handleChatCompletions(w, req)
		if w.Result().StatusCode != http.StatusOK {
			t.Fatalf("unexpected status %d during overhead test", w.Result().StatusCode)
		}
	}
	elapsed := time.Since(start)
	avgOverhead := elapsed / time.Duration(iterations)

	if avgOverhead > 50*time.Millisecond {
		t.Errorf("average HTTP overhead = %v, want <= 50ms (NFR50)", avgOverhead)
	}
	t.Logf("average HTTP overhead: %v (iterations=%d)", avgOverhead, iterations)
}

// --- toLLMRequest 转换测试 ---

func TestToLLMRequest_MessageMapping(t *testing.T) {
	// Given ChatCompletionRequest with multiple messages
	// When converting to LLMRequest
	// Then messages are correctly mapped
	temp := float64(0.8)
	req := ChatCompletionRequest{
		Model: "ollama:llama3",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hello"},
		},
		Temperature: &temp,
		MaxTokens:   2048,
	}

	llmReq := toLLMRequest(req, "llama3")

	if llmReq.Model != "llama3" {
		t.Errorf("Model = %q, want %q", llmReq.Model, "llama3")
	}
	if len(llmReq.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(llmReq.Messages))
	}
	if llmReq.Messages[0].Role != "system" {
		t.Errorf("Messages[0].Role = %q, want %q", llmReq.Messages[0].Role, "system")
	}
	if llmReq.Messages[0].Content != "You are helpful" {
		t.Errorf("Messages[0].Content = %q, want %q", llmReq.Messages[0].Content, "You are helpful")
	}
	if llmReq.Messages[1].Role != "user" {
		t.Errorf("Messages[1].Role = %q, want %q", llmReq.Messages[1].Role, "user")
	}
	if llmReq.Temperature == nil || *llmReq.Temperature != 0.8 {
		t.Errorf("Temperature = %v, want 0.8", llmReq.Temperature)
	}
	if llmReq.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048", llmReq.MaxTokens)
	}
}

func TestToLLMRequest_EmptyModel(t *testing.T) {
	// Given modelOverride is empty (provider-only model string)
	// When converting to LLMRequest
	// Then LLMRequest.Model is empty (driver uses default)
	req := ChatCompletionRequest{
		Model:    "ollama",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}

	llmReq := toLLMRequest(req, "")

	if llmReq.Model != "" {
		t.Errorf("Model = %q, want empty", llmReq.Model)
	}
}

// --- toChatCompletionResponse 转换测试 ---

func TestToChatCompletionResponse_Format(t *testing.T) {
	// Given LLMResponse with content and token usage
	// When converting to ChatCompletionResponse
	// Then all fields are correctly set
	llmResp := &llm.LLMResponse{
		Content:      "test response",
		TokensUsed:   30,
		InputTokens:  12,
		OutputTokens: 18,
	}

	resp := toChatCompletionResponse(llmResp, "ollama:llama3")

	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Errorf("ID = %q, want prefix %q", resp.ID, "chatcmpl-")
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", resp.Object, "chat.completion")
	}
	if resp.Created == 0 {
		t.Error("Created = 0, want non-zero")
	}
	if resp.Model != "ollama:llama3" {
		t.Errorf("Model = %q, want %q", resp.Model, "ollama:llama3")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "test response" {
		t.Errorf("Choices[0].Message.Content = %q, want %q", resp.Choices[0].Message.Content, "test response")
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("Choices[0].Message.Role = %q, want %q", resp.Choices[0].Message.Role, "assistant")
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("Choices[0].FinishReason = %q, want %q", resp.Choices[0].FinishReason, "stop")
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("Usage.PromptTokens = %d, want 12", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 18 {
		t.Errorf("Usage.CompletionTokens = %d, want 18", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("Usage.TotalTokens = %d, want 30", resp.Usage.TotalTokens)
	}
}

// --- 可选参数（Temperature, MaxTokens）透传测试 ---

func TestChatCompletions_OptionalFields_PassThrough(t *testing.T) {
	// Given request with temperature and max_tokens
	// When POST /v1/chat/completions
	// Then optional fields are passed to the driver
	var capturedReq llm.LLMRequest
	reg := llm.NewDriverRegistry()
	_ = reg.Register("test", &configurableDriver{
		info: llm.DriverInfo{Name: "test", Provider: "test", DefaultModel: "m", DriverType: "test"},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			capturedReq = req
			return &llm.LLMResponse{Content: "ok"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"test","messages":[{"role":"user","content":"hi"}],"temperature":0.5,"max_tokens":512}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Result().StatusCode)
	}
	if capturedReq.Temperature == nil || *capturedReq.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", capturedReq.Temperature)
	}
	if capturedReq.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d, want 512", capturedReq.MaxTokens)
	}
}

// ===========================================================================
// Story 24-4: /v1/models Provider Discovery
// ===========================================================================

func TestListModels_Basic(t *testing.T) {
	// AC #1,#3: GET /v1/models returns 200 with object:"list" and data array containing all providers
	reg := newTestRegistry(t) // claude + ollama
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body ModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Object != "list" {
		t.Errorf("object = %q, want %q", body.Object, "list")
	}
	// 2 providers * 2 entries each (provider + provider:model) = 4
	if len(body.Data) != 4 {
		t.Errorf("data length = %d, want 4", len(body.Data))
	}

	// Verify both provider names appear
	ids := make(map[string]bool)
	for _, entry := range body.Data {
		ids[entry.ID] = true
	}
	if !ids["claude"] {
		t.Error("missing model entry for 'claude'")
	}
	if !ids["ollama"] {
		t.Error("missing model entry for 'ollama'")
	}
}

func TestListModels_ResponseFormat(t *testing.T) {
	// AC #2: Each entry has correct id, object, created, owned_by fields
	reg := llm.NewDriverRegistry()
	_ = reg.Register("testprov", &stubDriver{info: llm.DriverInfo{
		Name: "testprov", Provider: "testprov", DefaultModel: "mymodel", DriverType: "test",
	}})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	var body ModelListResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(body.Data) < 1 {
		t.Fatal("expected at least 1 entry")
	}

	for _, entry := range body.Data {
		if entry.Object != "model" {
			t.Errorf("entry %q: object = %q, want %q", entry.ID, entry.Object, "model")
		}
		if entry.Created == 0 {
			t.Errorf("entry %q: created = 0, want non-zero", entry.ID)
		}
		if entry.OwnedBy != "testprov" {
			t.Errorf("entry %q: owned_by = %q, want %q", entry.ID, entry.OwnedBy, "testprov")
		}
	}
}

func TestListModels_ModelID(t *testing.T) {
	// AC #2: Provider name as model ID; if default_model exists, extra "provider:model" entry
	reg := llm.NewDriverRegistry()
	_ = reg.Register("ollama", &stubDriver{info: llm.DriverInfo{
		Name: "ollama", Provider: "ollama", DefaultModel: "llama3", DriverType: "openai-compat",
	}})
	_ = reg.Register("bare", &stubDriver{info: llm.DriverInfo{
		Name: "bare", Provider: "bare", DefaultModel: "", DriverType: "test",
	}})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	var body ModelListResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	ids := make(map[string]bool)
	for _, entry := range body.Data {
		ids[entry.ID] = true
	}

	// "ollama" should have both provider entry and provider:model entry
	if !ids["ollama"] {
		t.Error("missing 'ollama' entry")
	}
	if !ids["ollama:llama3"] {
		t.Error("missing 'ollama:llama3' entry")
	}

	// "bare" should have only provider entry (no default_model)
	if !ids["bare"] {
		t.Error("missing 'bare' entry")
	}
	if ids["bare:"] {
		t.Error("unexpected 'bare:' entry for provider with empty default_model")
	}

	// Total: ollama(2) + bare(1) = 3
	if len(body.Data) != 3 {
		t.Errorf("data length = %d, want 3", len(body.Data))
	}
}

func TestListModels_HealthFiltering(t *testing.T) {
	// AC #4: Unhealthy provider excluded from results
	reg := llm.NewDriverRegistry()
	_ = reg.Register("healthy", &stubDriver{info: llm.DriverInfo{
		Name: "healthy", Provider: "healthy", DefaultModel: "m1", DriverType: "test",
	}})
	_ = reg.Register("sick", &stubDriver{info: llm.DriverInfo{
		Name: "sick", Provider: "sick", DefaultModel: "m2", DriverType: "test",
	}})
	reg.SetHealth("healthy", llm.HealthStatusHealthy)
	reg.SetHealth("sick", llm.HealthStatusUnhealthy)

	srv := NewOpenAIServer(reg, "127.0.0.1:8080")
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	var body ModelListResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	for _, entry := range body.Data {
		if entry.OwnedBy == "sick" {
			t.Errorf("unhealthy provider 'sick' should not appear; got entry id=%q", entry.ID)
		}
	}
	// Only "healthy" + "healthy:m1" = 2
	if len(body.Data) != 2 {
		t.Errorf("data length = %d, want 2", len(body.Data))
	}
}

func TestListModels_UncheckedHealth(t *testing.T) {
	// AC #4: Unchecked health status providers are included
	reg := llm.NewDriverRegistry()
	_ = reg.Register("unchecked", &stubDriver{info: llm.DriverInfo{
		Name: "unchecked", Provider: "unchecked", DefaultModel: "m1", DriverType: "test",
	}})
	// Default health after Register is HealthStatusUnchecked — no SetHealth call

	srv := NewOpenAIServer(reg, "127.0.0.1:8080")
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	var body ModelListResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(body.Data) != 2 { // "unchecked" + "unchecked:m1"
		t.Errorf("data length = %d, want 2 (unchecked provider should be included)", len(body.Data))
	}
}

func TestListModels_EmptyRegistry(t *testing.T) {
	// Edge case: empty registry returns {"object":"list","data":[]}
	reg := llm.NewDriverRegistry()
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body ModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body.Object != "list" {
		t.Errorf("object = %q, want %q", body.Object, "list")
	}
	if body.Data == nil || len(body.Data) != 0 {
		t.Errorf("data = %v, want empty non-nil array", body.Data)
	}
}

func TestListModels_ContentType(t *testing.T) {
	// Verify Content-Type is application/json
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	ct := w.Result().Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want to contain %q", ct, "application/json")
	}
}

func TestListModels_Sorted(t *testing.T) {
	// Verify model entries follow provider-sorted order (grouped by provider name).
	// Within each provider: base entry first, then "provider:model" if DefaultModel set.
	reg := llm.NewDriverRegistry()
	_ = reg.Register("zebra", &stubDriver{info: llm.DriverInfo{
		Name: "zebra", Provider: "zebra", DefaultModel: "z-model", DriverType: "test",
	}})
	_ = reg.Register("alpha", &stubDriver{info: llm.DriverInfo{
		Name: "alpha", Provider: "alpha", DefaultModel: "a-model", DriverType: "test",
	}})
	_ = reg.Register("mid", &stubDriver{info: llm.DriverInfo{
		Name: "mid", Provider: "mid", DefaultModel: "", DriverType: "test",
	}})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	var body ModelListResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// alpha(2) + mid(1) + zebra(2) = 5 entries
	if len(body.Data) != 5 {
		t.Fatalf("data length = %d, want 5", len(body.Data))
	}
	expected := []string{"alpha", "alpha:a-model", "mid", "zebra", "zebra:z-model"}
	for i, want := range expected {
		if body.Data[i].ID != want {
			t.Errorf("data[%d].id = %q, want %q", i, body.Data[i].ID, want)
		}
	}
}

// ===========================================================================
// Model fallback resolution — bare model names without "provider:" prefix
// ===========================================================================

func TestChatCompletions_BareModelName_ResolvedByDefaultModel(t *testing.T) {
	// Given provider "deepseek" registered with default_model "deepseek-chat"
	// When request uses model="deepseek-chat" (bare model name, no colon)
	// Then it should resolve to the deepseek provider
	reg := llm.NewDriverRegistry()
	_ = reg.Register("deepseek", &configurableDriver{
		info: llm.DriverInfo{
			Name: "deepseek", Provider: "deepseek", DefaultModel: "deepseek-chat", DriverType: "openai-compat",
		},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			if req.Model != "deepseek-chat" {
				return nil, fmt.Errorf("expected model deepseek-chat, got %s", req.Model)
			}
			return &llm.LLMResponse{Content: "hello"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp ChatCompletionResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Errorf("content = %q, want %q", resp.Choices[0].Message.Content, "hello")
	}
}

func TestChatCompletions_BareModelName_FallbackToDefaultProvider(t *testing.T) {
	// Given defaultProvider="deepseek" and request model="some-custom-model"
	// that doesn't match any provider's default_model
	// When request is sent
	// Then it should use the defaultProvider and pass model name through
	reg := llm.NewDriverRegistry()
	_ = reg.Register("deepseek", &configurableDriver{
		info: llm.DriverInfo{
			Name: "deepseek", Provider: "deepseek", DefaultModel: "deepseek-chat", DriverType: "openai-compat",
		},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			if req.Model != "deepseek-r1" {
				return nil, fmt.Errorf("expected model deepseek-r1, got %s", req.Model)
			}
			return &llm.LLMResponse{Content: "ok"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")
	srv.SetDefaultProvider("deepseek")

	body := `{"model":"deepseek-r1","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_BareModelName_DefaultModelTakesPrecedenceOverDefaultProvider(t *testing.T) {
	// Given defaultProvider="claude" but model "llama3" matches ollama's default_model
	// When request uses model="llama3"
	// Then it should resolve to ollama (default_model match), not claude (defaultProvider)
	reg := llm.NewDriverRegistry()
	_ = reg.Register("claude", &stubDriver{info: llm.DriverInfo{
		Name: "claude", Provider: "claude", DefaultModel: "claude-3.5-sonnet", DriverType: "claude-cli",
	}})
	_ = reg.Register("ollama", &configurableDriver{
		info: llm.DriverInfo{
			Name: "ollama", Provider: "ollama", DefaultModel: "llama3", DriverType: "openai-compat",
		},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			return &llm.LLMResponse{Content: "from-ollama"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")
	srv.SetDefaultProvider("claude")

	body := `{"model":"llama3","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp ChatCompletionResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Choices[0].Message.Content != "from-ollama" {
		t.Errorf("content = %q, want %q", resp.Choices[0].Message.Content, "from-ollama")
	}
}

func TestChatCompletions_ProviderColonModel_StillWorks(t *testing.T) {
	// Given the provider:model format "deepseek:deepseek-chat"
	// When request uses this format
	// Then it should continue to work as before (no regression)
	reg := llm.NewDriverRegistry()
	_ = reg.Register("deepseek", &configurableDriver{
		info: llm.DriverInfo{
			Name: "deepseek", Provider: "deepseek", DefaultModel: "deepseek-chat", DriverType: "openai-compat",
		},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			if req.Model != "deepseek-chat" {
				return nil, fmt.Errorf("expected model deepseek-chat, got %s", req.Model)
			}
			return &llm.LLMResponse{Content: "ok"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"deepseek:deepseek-chat","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_BareProviderName_StillWorks(t *testing.T) {
	// Given request uses model="deepseek" (exact provider name, no colon)
	// When request is sent
	// Then it should match directly as provider name (existing behavior)
	reg := llm.NewDriverRegistry()
	_ = reg.Register("deepseek", &configurableDriver{
		info: llm.DriverInfo{
			Name: "deepseek", Provider: "deepseek", DefaultModel: "deepseek-chat", DriverType: "openai-compat",
		},
		callFn: func(_ context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
			if req.Model != "" {
				return nil, fmt.Errorf("expected empty model (use default), got %s", req.Model)
			}
			return &llm.LLMResponse{Content: "ok"}, nil
		},
	})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"deepseek","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_UnknownModel_NoDefaultProvider_Returns404(t *testing.T) {
	// Given no defaultProvider is set and model doesn't match any provider
	// When request uses model="unknown-model"
	// Then it should return 404
	reg := llm.NewDriverRegistry()
	_ = reg.Register("claude", &stubDriver{info: llm.DriverInfo{
		Name: "claude", Provider: "claude", DefaultModel: "claude-3.5-sonnet", DriverType: "claude-cli",
	}})
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.buildMux().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
