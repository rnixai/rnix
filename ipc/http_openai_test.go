package ipc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestHandleChatCompletions_Stub(t *testing.T) {
	// Given handleChatCompletions 为 stub（Story 24.2 实现）
	// When POST /v1/chat/completions
	// Then 返回 501 Not Implemented
	reg := newTestRegistry(t)
	srv := NewOpenAIServer(reg, "127.0.0.1:8080")

	body := `{"model":"ollama:llama3","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d (stub should return 501)", resp.StatusCode, http.StatusNotImplemented)
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
		wantStatus int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodPost, "/v1/chat/completions", http.StatusNotImplemented},
		{http.MethodGet, "/v1/models", http.StatusNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var bodyReader io.Reader
			if tt.method == http.MethodPost {
				bodyReader = strings.NewReader(`{"model":"test","messages":[]}`)
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
