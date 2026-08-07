package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// ATDD Tests for Story 23.4: HTTP API Provider 的 API Key 管理
//
// Tests verify that CreateDriver reads api_key_env from environment variables
// and passes them via WithOpenAIKey() to the OpenAIDriver.
// ============================================================================

// --- AC #1: api_key_env 环境变量读取并通过 WithOpenAIKey() 传入驱动 ---

func TestATDD_23_4_AC1_CreateDriver_ReadsAPIKeyFromEnv(t *testing.T) {
	// GIVEN: rnix-providers.yaml 中 provider 配置了 api_key_env: TEST_GROQ_KEY
	// AND:   环境变量 TEST_GROQ_KEY 已设置为 "sk-test-groq-12345"
	// WHEN:  创建 OpenAIDriver 实例
	// THEN:  HTTP 请求包含 Authorization: Bearer sk-test-groq-12345

	t.Setenv("TEST_GROQ_KEY", "sk-test-groq-12345")

	// Mock HTTP server to capture Authorization header
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, okCompletion)
	}))
	defer srv.Close()

	d, err := CreateDriver(ProviderConfig{
		Name:      "groq",
		Driver:    DriverOpenAI,
		BaseURL:   srv.URL,
		APIKeyEnv: "TEST_GROQ_KEY",
	})
	if err != nil {
		t.Fatalf("CreateDriver unexpected error: %v", err)
	}

	_, _ = d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})

	if gotAuth != "Bearer sk-test-groq-12345" {
		t.Errorf("AC1 FAIL: expected Authorization header %q, got %q",
			"Bearer sk-test-groq-12345", gotAuth)
	}
}

func TestATDD_23_4_AC1_RegisterProviders_PassesAPIKey(t *testing.T) {
	// GIVEN: 完整配置含 api_key_env + 环境变量设置
	// WHEN:  RegisterProviders 执行
	// THEN:  provider 注册成功，通过 VFS 打开后 HTTP 请求带 Authorization header

	t.Setenv("TEST_REG_KEY", "sk-register-test-key")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, okCompletion)
	}))
	defer srv.Close()

	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{
				Name:      "test-api-provider",
				Driver:    DriverOpenAI,
				BaseURL:   srv.URL,
				APIKeyEnv: "TEST_REG_KEY",
			},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("RegisterProviders unexpected error: %v", err)
	}

	// Verify the driver was registered
	d, ok := driverReg.Get("test-api-provider")
	if !ok {
		t.Fatal("expected test-api-provider in driver registry")
	}

	_, _ = d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})

	if gotAuth != "Bearer sk-register-test-key" {
		t.Errorf("AC1 FAIL: RegisterProviders did not pass API key. Expected Authorization %q, got %q",
			"Bearer sk-register-test-key", gotAuth)
	}
}

// --- AC #2: api_key_env 指定的环境变量不存在时行为 ---

func TestATDD_23_4_AC2_CreateDriver_MissingEnvVar_StillCreates(t *testing.T) {
	// GIVEN: api_key_env: NONEXISTENT_API_KEY 但该环境变量未设置
	// WHEN:  创建 OpenAIDriver
	// THEN:  driver 创建成功（不报错），provider 仍然注册

	// Ensure the env var is truly unset
	// Note: We do NOT call t.Setenv here, so the var should not exist

	d, err := CreateDriver(ProviderConfig{
		Name:      "test-nokey",
		Driver:    DriverOpenAI,
		BaseURL:   "http://localhost:9999",
		APIKeyEnv: "ATDD_23_4_NONEXISTENT_KEY",
	})
	if err != nil {
		t.Fatalf("AC2 FAIL: CreateDriver should succeed even when env var is missing, got: %v", err)
	}
	if d == nil {
		t.Fatal("AC2 FAIL: CreateDriver returned nil driver")
	}
}

func TestATDD_23_4_AC2_MissingEnvVar_NoAuthHeader(t *testing.T) {
	// GIVEN: api_key_env 指定的环境变量不存在
	// WHEN:  driver 发起 HTTP 请求
	// THEN:  请求不包含 Authorization header

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, okCompletion)
	}))
	defer srv.Close()

	d, err := CreateDriver(ProviderConfig{
		Name:      "test-nokey",
		Driver:    DriverOpenAI,
		BaseURL:   srv.URL,
		APIKeyEnv: "ATDD_23_4_MISSING_VAR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})

	if gotAuth != "" {
		t.Errorf("AC2 FAIL: expected no Authorization header when env var missing, got %q", gotAuth)
	}
}

func TestATDD_23_4_AC2_MissingEnvVar_ReturnsErrAuth(t *testing.T) {
	// GIVEN: api_key_env 指定的环境变量不存在
	// WHEN:  首次调用时远程 API 返回 401
	// THEN:  返回 ErrAuth 错误

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"message":"invalid api key","type":"invalid_request_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, okCompletion)
	}))
	defer srv.Close()

	d, err := CreateDriver(ProviderConfig{
		Name:      "test-nokey",
		Driver:    DriverOpenAI,
		BaseURL:   srv.URL,
		APIKeyEnv: "ATDD_23_4_MISSING_FOR_AUTH",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, callErr := d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})
	if callErr == nil {
		t.Fatal("AC2 FAIL: expected ErrAuth error when API key missing and server returns 401, got nil")
	}
	if !errors.Is(callErr, ErrAuth) {
		t.Errorf("AC2 FAIL: expected ErrAuth, got: %v", callErr)
	}
}

// --- AC #3: 本地 provider 不需要 API Key ---

func TestATDD_23_4_AC3_NoAPIKeyEnv_CreatesDriverWithoutAuth(t *testing.T) {
	// GIVEN: 本地 provider（如 Ollama）不需要 API Key
	// WHEN:  api_key_env 字段为空
	// THEN:  驱动正常创建，不附带认证头

	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, okCompletion)
	}))
	defer srv.Close()

	d, err := CreateDriver(ProviderConfig{
		Name:    "ollama",
		Driver:  DriverOpenAI,
		BaseURL: srv.URL,
		// APIKeyEnv intentionally empty
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})

	if gotAuth != "" {
		t.Errorf("AC3 FAIL: expected no Authorization header for local provider, got %q", gotAuth)
	}
}

func TestATDD_23_4_AC3_EmptyAPIKeyEnv_CreatesDriverWithoutAuth(t *testing.T) {
	// GIVEN: api_key_env 字段为显式空字符串
	// WHEN:  创建 driver
	// THEN:  驱动正常创建，HTTP 请求不包含 Authorization header

	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, okCompletion)
	}))
	defer srv.Close()

	d, err := CreateDriver(ProviderConfig{
		Name:      "local-llm",
		Driver:    DriverOpenAI,
		BaseURL:   srv.URL,
		APIKeyEnv: "", // Explicitly empty
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})

	if gotAuth != "" {
		t.Errorf("AC3 FAIL: expected no Authorization header for empty api_key_env, got %q", gotAuth)
	}
}

// --- AC #4: 安全审计 - API Key 不泄露 ---

func TestATDD_23_4_AC4_APIKeyNotInDriverInfo(t *testing.T) {
	// GIVEN: provider 配置了 API Key
	// WHEN:  调用 driver.Info()
	// THEN:  返回的 DriverInfo 不包含 API Key 明文

	t.Setenv("TEST_SECRET_KEY", "sk-super-secret-key-12345")

	d, err := CreateDriver(ProviderConfig{
		Name:      "secret-provider",
		Driver:    DriverOpenAI,
		BaseURL:   "http://localhost:9999",
		APIKeyEnv: "TEST_SECRET_KEY",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := d.Info()
	infoStr := fmt.Sprintf("%+v", info)

	if strings.Contains(infoStr, "sk-super-secret-key-12345") {
		t.Errorf("AC4 FAIL: API Key leaked in DriverInfo: %s", infoStr)
	}
}

func TestATDD_23_4_AC4_APIKeyNotInErrorMessage(t *testing.T) {
	// GIVEN: provider 配置了 API Key
	// WHEN:  调用失败返回错误
	// THEN:  错误消息中不包含 API Key 明文

	t.Setenv("TEST_ERROR_KEY", "sk-error-secret-key-67890")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":{"message":"internal server error"}}`)
	}))
	defer srv.Close()

	d, err := CreateDriver(ProviderConfig{
		Name:      "error-provider",
		Driver:    DriverOpenAI,
		BaseURL:   srv.URL,
		APIKeyEnv: "TEST_ERROR_KEY",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, callErr := d.Call(context.Background(), LLMRequest{Intent: "test", TimeoutMs: 5000})
	if callErr == nil {
		t.Fatal("expected error from HTTP 500, got nil")
	}

	errStr := callErr.Error()
	if strings.Contains(errStr, "sk-error-secret-key-67890") {
		t.Errorf("AC4 FAIL: API Key leaked in error message: %s", errStr)
	}
}

func TestATDD_23_4_AC4_ProviderConfigStoresEnvVarNameNotValue(t *testing.T) {
	// GIVEN: 安全审计
	// WHEN:  检查 ProviderConfig
	// THEN:  APIKeyEnv 仅存储环境变量名称，不存储实际 key 值

	t.Parallel()

	cfg := ProviderConfig{
		Name:      "groq",
		Driver:    DriverOpenAI,
		BaseURL:   "https://api.groq.com/openai/v1",
		APIKeyEnv: "GROQ_API_KEY",
	}

	// Verify the config struct stores the env var name, not the value
	if cfg.APIKeyEnv != "GROQ_API_KEY" {
		t.Errorf("AC4 FAIL: APIKeyEnv should store env var name, got %q", cfg.APIKeyEnv)
	}

	// Verify the config struct representation doesn't contain any actual key values
	cfgStr := fmt.Sprintf("%+v", cfg)
	if strings.Contains(cfgStr, "sk-") {
		t.Errorf("AC4 FAIL: ProviderConfig representation contains key-like value: %s", cfgStr)
	}
}

// --- 边界场景: RegisterProviders 集成 ---

func TestATDD_23_4_RegisterProviders_APIKeyEnvMissing_StillRegisters(t *testing.T) {
	// GIVEN: api_key_env 设置但环境变量缺失
	// WHEN:  RegisterProviders 执行
	// THEN:  provider 仍注册成功（仅 warning 日志）

	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{
				Name:      "missing-key-provider",
				Driver:    DriverOpenAI,
				BaseURL:   "http://localhost:9999",
				APIKeyEnv: "ATDD_23_4_TOTALLY_MISSING_KEY",
			},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	err := RegisterProviders(cfg, driverReg, devReg)
	if err != nil {
		t.Fatalf("RegisterProviders should succeed even with missing env var, got: %v", err)
	}

	if _, ok := driverReg.Get("missing-key-provider"); !ok {
		t.Error("expected missing-key-provider in driver registry despite missing env var")
	}
}
