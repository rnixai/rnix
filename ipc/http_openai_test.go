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

// configurableDriver allows tests to control Call behavior.
type configurableDriver struct {
	info    llm.DriverInfo
	callFn  func(ctx context.Context, req llm.LLMRequest) (*llm.LLMResponse, error)
}

func (d *configurableDriver) Call(ctx context.Context, req llm.LLMRequest) (*llm.LLMResponse, error) {
	return d.callFn(ctx, req)
}

func (d *configurableDriver) Stream(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
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
				Delta:        ChatMessage{Content: "Hello"},
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

func TestHandleListModels_Stub(t *testing.T) {
	// Given handleListModels 为 stub（Story 24.4 实现）
	// When GET /v1/models
	// Then 返回 501 Not Implemented
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	srv.handleListModels(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d (stub should return 501)", resp.StatusCode, http.StatusNotImplemented)
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
		{http.MethodGet, "/v1/models", "", http.StatusNotImplemented},
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

func TestChatCompletions_StreamTrue_501(t *testing.T) {
	// Given stream=true in request (Story 24.3 feature)
	// When POST /v1/chat/completions
	// Then returns HTTP 501 Not Implemented
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"ollama","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
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
