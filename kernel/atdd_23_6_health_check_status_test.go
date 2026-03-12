package kernel

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

// ============================================================================
// ATDD Tests for Story 23.6: Provider 健康检查与状态报告
//
// RED PHASE: All tests t.Skip until implementation is complete.
//
// Implementation targets:
//   - drivers/llm/registry.go: HealthStatus type, health SyncMap, SetHealth/GetHealth/HealthStatuses
//   - drivers/llm/driver.go: HealthChecker optional interface
//   - drivers/llm/openai_compat.go: HealthCheck(ctx) method
//   - drivers/llm/factory.go: RunHealthChecks async function
//   - ipc/protocol.go: MethodProviderStatus + wire types
//   - ipc/server.go: handleProviderStatus handler
//   - ipc/client.go: ProviderStatus() client method
// ============================================================================

// ============================================================
// AC1: HTTP API provider 健康检查
// Given HTTP API 类 provider 已注册
// When daemon 启动完成后
// Then 对每个 HTTP API provider 执行轻量健康检查
// And 单个健康检查耗时 <= 3 秒 (NFR32)
// ============================================================

func TestATDD_23_6_AC1_HTTPProviderHealthCheck(t *testing.T) {
	t.Skip("RED: HealthChecker interface, HealthCheck method, and health status storage not yet implemented")

	// GIVEN: An OpenAI-compat provider backed by a healthy httptest server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"data":[{"id":"test-model"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	drv := llm.NewOpenAICompatDriver("test-healthy", srv.URL, llm.WithHTTPClient(srv.Client()))
	reg := llm.NewDriverRegistry()
	if err := reg.Register("test-healthy", drv); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// WHEN: RunHealthChecks is called (simulating daemon startup)
	// llm.RunHealthChecks(cfg, reg, 3*time.Second)
	// time.Sleep(500 * time.Millisecond) // allow async goroutine to complete

	// THEN: provider health status is "healthy"
	// health := reg.GetHealth("test-healthy")
	// if health != llm.HealthStatusHealthy {
	//     t.Errorf("AC1: expected health status 'healthy', got %q", health)
	// }
	_ = drv
	_ = reg
}

func TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint(t *testing.T) {
	t.Skip("RED: HealthCheck method on OpenAICompatDriver not yet implemented")

	// GIVEN: An httptest server that records incoming requests
	var gotPath string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[]}`)
	}))
	defer srv.Close()

	drv := llm.NewOpenAICompatDriver("groq", srv.URL, llm.WithHTTPClient(srv.Client()), llm.WithAPIKey("sk-test-key"))

	// WHEN: HealthCheck is called
	// err := drv.(llm.HealthChecker).HealthCheck(context.Background())

	// THEN: GET /models was called with correct Authorization header
	// assert err == nil
	// assert gotPath ends with "/models"
	// assert gotAuth == "Bearer sk-test-key"
	_ = drv
	_ = gotPath
	_ = gotAuth
}

func TestATDD_23_6_AC1_HealthCheckWithinTimeout(t *testing.T) {
	t.Skip("RED: HealthCheck method and timeout enforcement not yet implemented (NFR32)")

	// GIVEN: A healthy server responding within 100ms
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[]}`)
	}))
	defer srv.Close()

	drv := llm.NewOpenAICompatDriver("fast-provider", srv.URL, llm.WithHTTPClient(srv.Client()))

	// WHEN: HealthCheck is called with 3-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	// err := drv.(llm.HealthChecker).HealthCheck(ctx)
	_ = time.Since(start)

	// THEN: check completes within 3 seconds and returns nil
	// assert err == nil
	// assert elapsed <= 3*time.Second
	_ = drv
	_ = ctx
}

// ============================================================
// AC2: 健康检查失败 — daemon 正常启动，provider 标记 unhealthy
// Given 健康检查失败
// When provider 端点不可达
// Then daemon 正常启动（不因单个 provider 失败而拒绝启动）
// And 该 provider 标记为 unhealthy
// ============================================================

func TestATDD_23_6_AC2_UnreachableProvider(t *testing.T) {
	t.Skip("RED: HealthCheck, RunHealthChecks, and health status storage not yet implemented")

	// GIVEN: An OpenAI-compat provider pointing to an unreachable address
	drv := llm.NewOpenAICompatDriver("broken-provider", "http://127.0.0.1:1")
	reg := llm.NewDriverRegistry()
	if err := reg.Register("broken-provider", drv); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// WHEN: RunHealthChecks is called (simulating daemon startup)
	// cfg := &llm.ProvidersConfig{Providers: []llm.ProviderConfig{{Name: "broken-provider", Driver: llm.DriverOpenAICompat}}}
	// llm.RunHealthChecks(cfg, reg, 3*time.Second)
	// time.Sleep(4 * time.Second) // wait for async completion (3s timeout + margin)

	// THEN: daemon did not panic (we are still running)
	// AND: provider is marked "unhealthy"
	// health := reg.GetHealth("broken-provider")
	// if health != llm.HealthStatusUnhealthy {
	//     t.Errorf("AC2: expected 'unhealthy', got %q", health)
	// }
	_ = drv
	_ = reg
}

func TestATDD_23_6_AC2_HealthCheckTimeout(t *testing.T) {
	t.Skip("RED: HealthCheck timeout handling not yet implemented")

	// GIVEN: A server that delays 5 seconds (longer than 3s timeout)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	drv := llm.NewOpenAICompatDriver("slow-provider", srv.URL, llm.WithHTTPClient(srv.Client()))

	// WHEN: HealthCheck is called with 1-second context deadline
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	// err := drv.(llm.HealthChecker).HealthCheck(ctx)
	elapsed := time.Since(start)

	// THEN: error is deadline exceeded AND total time <= 3 seconds
	// assert err != nil (context.DeadlineExceeded)
	// assert elapsed <= 3*time.Second
	_ = drv
	_ = ctx
	_ = elapsed
	_ = start
}

func TestATDD_23_6_AC2_HTTP401Unhealthy(t *testing.T) {
	t.Skip("RED: HealthCheck HTTP error classification not yet implemented")

	// GIVEN: A server returning 401 Unauthorized
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	drv := llm.NewOpenAICompatDriver("bad-key-provider", srv.URL, llm.WithHTTPClient(srv.Client()))

	// WHEN: HealthCheck is called
	// err := drv.(llm.HealthChecker).HealthCheck(context.Background())

	// THEN: error contains "HTTP 401"
	// assert err != nil
	// assert strings.Contains(err.Error(), "HTTP 401")
	_ = drv
}

func TestATDD_23_6_AC2_DaemonDoesNotBlock(t *testing.T) {
	t.Skip("RED: RunHealthChecks non-blocking behavior not yet implemented")

	// GIVEN: A provider that takes 5 seconds to respond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	drv := llm.NewOpenAICompatDriver("slow-check", srv.URL, llm.WithHTTPClient(srv.Client()))
	reg := llm.NewDriverRegistry()
	_ = reg.Register("slow-check", drv)

	// WHEN: RunHealthChecks is called
	start := time.Now()
	// cfg := &llm.ProvidersConfig{Providers: []llm.ProviderConfig{{Name: "slow-check", Driver: llm.DriverOpenAICompat}}}
	// llm.RunHealthChecks(cfg, reg, 3*time.Second)
	elapsed := time.Since(start)

	// THEN: function returns immediately (< 100ms), not blocking on health check
	if elapsed > 100*time.Millisecond {
		t.Errorf("AC2: RunHealthChecks blocked for %v, expected < 100ms (non-blocking)", elapsed)
	}
	_ = drv
	_ = reg
}

// ============================================================
// AC3: CLI 类 provider 跳过健康检查
// Given CLI 类 provider（claude、cursor）
// When daemon 启动
// Then 跳过健康检查（CLI 可用性在首次调用时验证）
// ============================================================

func TestATDD_23_6_AC3_CLIProviderSkipped(t *testing.T) {
	t.Skip("RED: HealthChecker optional interface and CLI driver type assertion not yet implemented")

	// GIVEN: A Claude CLI driver registered
	drv := llm.NewClaudeCliDriver()
	reg := llm.NewDriverRegistry()
	if err := reg.Register("claude", drv); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// WHEN: RunHealthChecks processes all providers
	// cfg := &llm.ProvidersConfig{Providers: []llm.ProviderConfig{{Name: "claude", Driver: llm.DriverClaudeCLI}}}
	// llm.RunHealthChecks(cfg, reg, 3*time.Second)
	// time.Sleep(500 * time.Millisecond)

	// THEN: Claude CLI driver does NOT implement HealthChecker
	// _, isHealthChecker := drv.(llm.HealthChecker)
	// if isHealthChecker {
	//     t.Error("AC3: ClaudeCliDriver should NOT implement HealthChecker")
	// }

	// AND: health status remains "unchecked"
	// health := reg.GetHealth("claude")
	// if health != llm.HealthStatusUnchecked {
	//     t.Errorf("AC3: expected 'unchecked' for CLI provider, got %q", health)
	// }
	_ = drv
	_ = reg
}

func TestATDD_23_6_AC3_CursorCLIProviderSkipped(t *testing.T) {
	t.Skip("RED: HealthChecker optional interface not yet defined")

	// GIVEN: A Cursor CLI driver registered
	drv := llm.NewCursorCliDriver()

	// WHEN: Type assertion for HealthChecker is attempted
	// _, isHealthChecker := drv.(llm.HealthChecker)

	// THEN: Cursor CLI driver does NOT implement HealthChecker
	// if isHealthChecker {
	//     t.Error("AC3: CursorCliDriver should NOT implement HealthChecker")
	// }
	_ = drv
}

func TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker(t *testing.T) {
	t.Skip("RED: HealthChecker interface not yet defined on OpenAICompatDriver")

	// GIVEN: An OpenAI-compat driver
	drv := llm.NewOpenAICompatDriver("test", "http://localhost:11434")

	// WHEN: Type assertion for HealthChecker is attempted
	// _, ok := drv.(llm.HealthChecker)

	// THEN: OpenAICompatDriver DOES implement HealthChecker
	// if !ok {
	//     t.Error("AC3: OpenAICompatDriver should implement HealthChecker")
	// }
	_ = drv
}

// ============================================================
// AC4: rnix daemon status 显示 provider 状态
// Given 用户执行 rnix daemon status
// When 查看输出
// Then 显示所有已注册 provider 的状态 (healthy / unhealthy / unchecked)
// ============================================================

func TestATDD_23_6_AC4_RegistryHealthStatuses(t *testing.T) {
	t.Skip("RED: HealthStatuses() method and ProviderStatus type not yet implemented")

	// GIVEN: Multiple providers registered with different health states
	reg := llm.NewDriverRegistry()
	_ = reg.Register("groq", llm.NewOpenAICompatDriver("groq", "http://localhost:1"))
	_ = reg.Register("claude", llm.NewClaudeCliDriver())
	_ = reg.Register("ollama", llm.NewOpenAICompatDriver("ollama", "http://localhost:11434"))

	// WHEN: SetHealth is called for some providers
	// reg.SetHealth("groq", llm.HealthStatusHealthy)
	// reg.SetHealth("ollama", llm.HealthStatusUnhealthy)
	// claude remains "unchecked" (CLI, no health check)

	// THEN: HealthStatuses returns sorted list with correct status for each
	// statuses := reg.HealthStatuses()
	// assert len(statuses) == 3
	// assert statuses[0].Name == "claude" && statuses[0].Health == llm.HealthStatusUnchecked
	// assert statuses[1].Name == "groq" && statuses[1].Health == llm.HealthStatusHealthy
	// assert statuses[2].Name == "ollama" && statuses[2].Health == llm.HealthStatusUnhealthy
	_ = reg
}

func TestATDD_23_6_AC4_RegistryDefaultUnchecked(t *testing.T) {
	t.Skip("RED: GetHealth method and HealthStatusUnchecked constant not yet implemented")

	// GIVEN: A provider is registered but no health check has run
	reg := llm.NewDriverRegistry()
	_ = reg.Register("new-provider", llm.NewOpenAICompatDriver("new-provider", "http://localhost:1"))

	// WHEN: GetHealth is called
	// health := reg.GetHealth("new-provider")

	// THEN: Default status is "unchecked"
	// if health != llm.HealthStatusUnchecked {
	//     t.Errorf("AC4: expected default 'unchecked', got %q", health)
	// }
	_ = reg
}

func TestATDD_23_6_AC4_IPCProviderStatusResponse(t *testing.T) {
	t.Skip("RED: MethodProviderStatus IPC method, ProviderStatusResponse, and client.ProviderStatus() not yet implemented")

	// GIVEN: A daemon with registered providers of mixed health states
	// (This test requires the full IPC server setup)
	// server has providerStatuses func injected via SetProviderStatusFunc

	// WHEN: Client calls ProviderStatus() via IPC
	// providers, err := client.ProviderStatus()

	// THEN: Response contains all providers with correct name/driver/health
	// assert err == nil
	// assert len(providers) > 0
	// each provider has Name, Driver, Health fields populated
}

// ============================================================
// Integration: RunHealthChecks end-to-end with mixed providers
// ============================================================

func TestATDD_23_6_Integration_MixedProviderHealthChecks(t *testing.T) {
	t.Skip("RED: RunHealthChecks function and health status storage not yet implemented")

	// GIVEN: A mix of healthy HTTP, unhealthy HTTP, and CLI providers
	healthySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[]}`)
	}))
	defer healthySrv.Close()

	reg := llm.NewDriverRegistry()
	_ = reg.Register("healthy-api", llm.NewOpenAICompatDriver("healthy-api", healthySrv.URL, llm.WithHTTPClient(healthySrv.Client())))
	_ = reg.Register("broken-api", llm.NewOpenAICompatDriver("broken-api", "http://127.0.0.1:1"))
	_ = reg.Register("claude-cli", llm.NewClaudeCliDriver())

	// WHEN: RunHealthChecks is called
	// cfg := &llm.ProvidersConfig{Providers: []llm.ProviderConfig{
	//     {Name: "healthy-api", Driver: llm.DriverOpenAICompat},
	//     {Name: "broken-api", Driver: llm.DriverOpenAICompat},
	//     {Name: "claude-cli", Driver: llm.DriverClaudeCLI},
	// }}
	// llm.RunHealthChecks(cfg, reg, 3*time.Second)
	// time.Sleep(4 * time.Second)

	// THEN: healthy-api => "healthy", broken-api => "unhealthy", claude-cli => "unchecked"
	// assert reg.GetHealth("healthy-api") == llm.HealthStatusHealthy
	// assert reg.GetHealth("broken-api") == llm.HealthStatusUnhealthy
	// assert reg.GetHealth("claude-cli") == llm.HealthStatusUnchecked
	_ = reg
}

// Suppress unused import warnings from t.Skip'd tests.
var (
	_ = context.Background
	_ = json.Marshal
	_ = fmt.Sprintf
	_ = io.Discard
	_ = http.StatusOK
	_ = httptest.NewServer
	_ = strings.Contains
	_ = time.Second
)
