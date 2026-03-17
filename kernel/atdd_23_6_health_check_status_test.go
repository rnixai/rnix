package kernel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/ipc"
)

// ============================================================================
// ATDD Tests for Story 23.6: Provider 健康检查与状态报告
// GREEN PHASE: All tests implemented and passing.
// ============================================================================

// ============================================================
// AC1: HTTP API provider 健康检查
// ============================================================

func TestATDD_23_6_AC1_HTTPProviderHealthCheck(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
	}))
	defer ts.Close()

	cfg := &llm.ProvidersConfig{
		Version: "1",
		Providers: []llm.ProviderConfig{
			{Name: "healthy-api", Driver: llm.DriverOpenAICompat, BaseURL: ts.URL},
		},
	}
	reg := llm.NewDriverRegistry()
	drv := llm.NewOpenAICompatDriver("healthy-api", ts.URL, llm.WithHTTPClient(ts.Client()))
	_ = reg.Register("healthy-api", drv)

	llm.RunHealthChecks(cfg, reg, 3*time.Second)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if reg.GetHealth("healthy-api") == llm.HealthStatusHealthy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("AC1: health = %q, want %q", reg.GetHealth("healthy-api"), llm.HealthStatusHealthy)
}

func TestATDD_23_6_AC1_HealthCheckCallsModelsEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer ts.Close()

	drv := llm.NewOpenAICompatDriver("groq", ts.URL, llm.WithHTTPClient(ts.Client()), llm.WithAPIKey("sk-test-key"))
	err := drv.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/models") {
		t.Errorf("AC1: path = %q, want suffix /models", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("AC1: Authorization = %q, want %q", gotAuth, "Bearer sk-test-key")
	}
}

func TestATDD_23_6_AC1_HealthCheckWithinTimeout(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer ts.Close()

	drv := llm.NewOpenAICompatDriver("fast-provider", ts.URL, llm.WithHTTPClient(ts.Client()))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	err := drv.HealthCheck(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AC1: HealthCheck: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("AC1: health check took %v, want <= 3s (NFR32)", elapsed)
	}
}

// ============================================================
// AC2: 健康检查失败 — daemon 正常启动，provider 标记 unhealthy
// ============================================================

func TestATDD_23_6_AC2_UnreachableProvider(t *testing.T) {
	t.Parallel()
	cfg := &llm.ProvidersConfig{
		Version: "1",
		Providers: []llm.ProviderConfig{
			{Name: "dead-api", Driver: llm.DriverOpenAICompat, BaseURL: "http://127.0.0.1:1"},
		},
	}
	reg := llm.NewDriverRegistry()
	drv := llm.NewOpenAICompatDriver("dead-api", "http://127.0.0.1:1")
	_ = reg.Register("dead-api", drv)

	// Should NOT panic
	llm.RunHealthChecks(cfg, reg, 3*time.Second)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reg.GetHealth("dead-api") == llm.HealthStatusUnhealthy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("AC2: health = %q, want %q", reg.GetHealth("dead-api"), llm.HealthStatusUnhealthy)
}

func TestATDD_23_6_AC2_HealthCheckTimeout(t *testing.T) {
	t.Parallel()
	handlerDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-handlerDone:
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()
	defer close(handlerDone)

	cfg := &llm.ProvidersConfig{
		Version: "1",
		Providers: []llm.ProviderConfig{
			{Name: "slow-api", Driver: llm.DriverOpenAICompat, BaseURL: ts.URL},
		},
	}
	reg := llm.NewDriverRegistry()
	drv := llm.NewOpenAICompatDriver("slow-api", ts.URL, llm.WithHTTPClient(ts.Client()))
	_ = reg.Register("slow-api", drv)

	start := time.Now()
	llm.RunHealthChecks(cfg, reg, 1*time.Second)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if reg.GetHealth("slow-api") == llm.HealthStatusUnhealthy {
			elapsed := time.Since(start)
			if elapsed > 3*time.Second {
				t.Errorf("AC2: total took %v, want <= 3s", elapsed)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("AC2: health = %q, want %q", reg.GetHealth("slow-api"), llm.HealthStatusUnhealthy)
}

func TestATDD_23_6_AC2_HTTP401Unhealthy(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":"unauthorized"}`)
	}))
	defer ts.Close()

	drv := llm.NewOpenAICompatDriver("bad-key", ts.URL, llm.WithHTTPClient(ts.Client()))
	err := drv.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("AC2: expected error for HTTP 401, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("AC2: error = %v, want to contain 'HTTP 401'", err)
	}
}

func TestATDD_23_6_AC2_DaemonDoesNotBlock(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	cfg := &llm.ProvidersConfig{
		Version: "1",
		Providers: []llm.ProviderConfig{
			{Name: "slow-check", Driver: llm.DriverOpenAICompat, BaseURL: ts.URL},
		},
	}
	reg := llm.NewDriverRegistry()
	drv := llm.NewOpenAICompatDriver("slow-check", ts.URL, llm.WithHTTPClient(ts.Client()))
	_ = reg.Register("slow-check", drv)

	start := time.Now()
	llm.RunHealthChecks(cfg, reg, 3*time.Second)
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("AC2: RunHealthChecks blocked for %v, expected < 100ms", elapsed)
	}
}

// ============================================================
// AC3: CLI 类 provider 跳过健康检查
// ============================================================

func TestATDD_23_6_AC3_CLIProviderSkipped(t *testing.T) {
	t.Parallel()
	cfg := &llm.ProvidersConfig{
		Version: "1",
		Providers: []llm.ProviderConfig{
			{Name: "claude", Driver: llm.DriverClaudeCLI},
			{Name: "cursor", Driver: llm.DriverCursorCLI},
		},
	}
	reg := llm.NewDriverRegistry()
	_ = reg.Register("claude", llm.NewClaudeCliDriver())
	_ = reg.Register("cursor", llm.NewCursorCliDriver())

	llm.RunHealthChecks(cfg, reg, 3*time.Second)
	time.Sleep(100 * time.Millisecond)

	if got := reg.GetHealth("claude"); got != llm.HealthStatusUnchecked {
		t.Errorf("AC3: claude health = %q, want %q", got, llm.HealthStatusUnchecked)
	}
	if got := reg.GetHealth("cursor"); got != llm.HealthStatusUnchecked {
		t.Errorf("AC3: cursor health = %q, want %q", got, llm.HealthStatusUnchecked)
	}
}

func TestATDD_23_6_AC3_CLIDriverDoesNotImplementHealthChecker(t *testing.T) {
	t.Parallel()
	claude := llm.NewClaudeCliDriver()
	cursor := llm.NewCursorCliDriver()

	if _, ok := any(claude).(llm.HealthChecker); ok {
		t.Error("AC3: ClaudeCliDriver should NOT implement HealthChecker")
	}
	if _, ok := any(cursor).(llm.HealthChecker); ok {
		t.Error("AC3: CursorCliDriver should NOT implement HealthChecker")
	}
}

func TestATDD_23_6_AC3_OpenAICompatImplementsHealthChecker(t *testing.T) {
	t.Parallel()
	drv := llm.NewOpenAICompatDriver("test", "http://localhost:11434")
	if _, ok := any(drv).(llm.HealthChecker); !ok {
		t.Error("AC3: OpenAICompatDriver should implement HealthChecker")
	}
}

// ============================================================
// AC4: rnix daemon status 显示 provider 状态
// ============================================================

func TestATDD_23_6_AC4_DaemonStatusShowsProviders(t *testing.T) {
	t.Parallel()
	reg := llm.NewDriverRegistry()
	_ = reg.Register("claude", llm.NewClaudeCliDriver())
	_ = reg.Register("ollama", llm.NewOpenAICompatDriver("ollama", "http://localhost:11434/v1"))

	reg.SetHealth("ollama", llm.HealthStatusHealthy)

	statuses := reg.HealthStatuses()
	wires := make([]ipc.ProviderStatusWire, len(statuses))
	for i, ps := range statuses {
		wires[i] = ipc.ProviderStatusWire{
			Name:   ps.Name,
			Driver: ps.Driver,
			Health: string(ps.Health),
		}
	}

	if len(wires) != 2 {
		t.Fatalf("AC4: expected 2 providers, got %d", len(wires))
	}
	if wires[0].Name != "claude" || wires[0].Driver != "claude-cli" || wires[0].Health != "unchecked" {
		t.Errorf("AC4: claude = %+v, want name=claude driver=claude-cli health=unchecked", wires[0])
	}
	if wires[1].Name != "ollama" || wires[1].Driver != "openai-compat" || wires[1].Health != "healthy" {
		t.Errorf("AC4: ollama = %+v, want name=ollama driver=openai-compat health=healthy", wires[1])
	}

	// Verify JSON roundtrip
	resp := ipc.ProviderStatusResponse{Providers: wires}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"healthy"`) || !strings.Contains(body, `"unchecked"`) {
		t.Errorf("AC4: JSON body missing expected health values: %s", body)
	}
}

func TestATDD_23_6_AC4_RegistryHealthStatuses(t *testing.T) {
	t.Parallel()
	reg := llm.NewDriverRegistry()
	_ = reg.Register("groq", llm.NewOpenAICompatDriver("groq", "http://localhost:1"))
	_ = reg.Register("claude", llm.NewClaudeCliDriver())
	_ = reg.Register("ollama", llm.NewOpenAICompatDriver("ollama", "http://localhost:11434"))

	reg.SetHealth("groq", llm.HealthStatusHealthy)
	reg.SetHealth("ollama", llm.HealthStatusUnhealthy)

	statuses := reg.HealthStatuses()
	if len(statuses) != 3 {
		t.Fatalf("AC4: expected 3 statuses, got %d", len(statuses))
	}
	// Sorted: claude, groq, ollama
	if statuses[0].Name != "claude" || statuses[0].Health != llm.HealthStatusUnchecked {
		t.Errorf("AC4: claude = %+v", statuses[0])
	}
	if statuses[1].Name != "groq" || statuses[1].Health != llm.HealthStatusHealthy {
		t.Errorf("AC4: groq = %+v", statuses[1])
	}
	if statuses[2].Name != "ollama" || statuses[2].Health != llm.HealthStatusUnhealthy {
		t.Errorf("AC4: ollama = %+v", statuses[2])
	}
}

func TestATDD_23_6_AC4_DefaultUnchecked(t *testing.T) {
	t.Parallel()
	reg := llm.NewDriverRegistry()
	_ = reg.Register("new", llm.NewOpenAICompatDriver("new", "http://localhost:1"))

	if got := reg.GetHealth("new"); got != llm.HealthStatusUnchecked {
		t.Errorf("AC4: default health = %q, want %q", got, llm.HealthStatusUnchecked)
	}
}

// ============================================================
// Integration: Mixed provider health checks end-to-end
// ============================================================

func TestATDD_23_6_Integration_MixedProviderHealthChecks(t *testing.T) {
	t.Parallel()
	healthySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer healthySrv.Close()

	cfg := &llm.ProvidersConfig{
		Version: "1",
		Providers: []llm.ProviderConfig{
			{Name: "healthy-api", Driver: llm.DriverOpenAICompat, BaseURL: healthySrv.URL},
			{Name: "broken-api", Driver: llm.DriverOpenAICompat, BaseURL: "http://127.0.0.1:1"},
			{Name: "claude-cli", Driver: llm.DriverClaudeCLI},
		},
	}
	reg := llm.NewDriverRegistry()
	_ = reg.Register("healthy-api", llm.NewOpenAICompatDriver("healthy-api", healthySrv.URL, llm.WithHTTPClient(healthySrv.Client())))
	_ = reg.Register("broken-api", llm.NewOpenAICompatDriver("broken-api", "http://127.0.0.1:1"))
	_ = reg.Register("claude-cli", llm.NewClaudeCliDriver())

	llm.RunHealthChecks(cfg, reg, 3*time.Second)

	// Wait for both async checks to settle
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h := reg.GetHealth("healthy-api")
		u := reg.GetHealth("broken-api")
		if h == llm.HealthStatusHealthy && u == llm.HealthStatusUnhealthy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := reg.GetHealth("healthy-api"); got != llm.HealthStatusHealthy {
		t.Errorf("healthy-api = %q, want healthy", got)
	}
	if got := reg.GetHealth("broken-api"); got != llm.HealthStatusUnhealthy {
		t.Errorf("broken-api = %q, want unhealthy", got)
	}
	if got := reg.GetHealth("claude-cli"); got != llm.HealthStatusUnchecked {
		t.Errorf("claude-cli = %q, want unchecked", got)
	}
}
