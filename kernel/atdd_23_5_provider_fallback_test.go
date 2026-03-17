package kernel

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Tests for Story 23.5: Provider Fallback 降级机制
//
// GREEN PHASE: Implementation complete. Tests verify the fallback mechanism.
//
// Implementation targets:
//   - agents/types.go: AgentModels.FallbackProvider field
//   - kernel/process.go: Process.FallbackModel, FallbackProvider, FallbackDevice fields
//   - kernel/kernel.go: Spawn fallback config parsing, attemptFallback(), reasonStep fallback path
// ============================================================================

// --- Test Helpers ---

// newFallbackTestKernel creates a kernel with two mock LLM devices for fallback testing.
// primaryDevice receives primary calls, fallbackDevice receives fallback calls.
func newFallbackTestKernel(t testing.TB, primaryFile, fallbackFile *mockLLMFile, primaryName, fallbackName string) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/"+primaryName, func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return primaryFile, nil
	})
	if fallbackName != primaryName {
		_ = reg.Register("/dev/llm/"+fallbackName, func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
			return fallbackFile, nil
		})
	}
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	// Register both providers in the resolver
	providerSet := map[string]bool{primaryName: true, fallbackName: true}
	k.SetProviderResolver(
		func() []string {
			names := make([]string, 0, len(providerSet))
			for n := range providerSet {
				names = append(names, n)
			}
			return names
		},
		func(name string) bool { return providerSet[name] },
	)

	return k
}

// newSameProviderFallbackKernel creates a kernel where the same device path returns
// different mocks on successive Opens. First Open returns primaryFile, subsequent
// Opens return fallbackFile. This simulates same-provider model downgrade.
func newSameProviderFallbackKernel(t testing.TB, primaryFile, fallbackFile *mockLLMFile, providerName string) *KernelImpl {
	var callCount atomic.Int32
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/"+providerName, func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		n := callCount.Add(1)
		if n == 1 {
			return primaryFile, nil
		}
		return fallbackFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.SetProviderResolver(
		func() []string { return []string{providerName} },
		func(name string) bool { return name == providerName },
	)
	return k
}

// fallbackAgentInfo creates an AgentInfo with fallback configuration.
func fallbackAgentInfo(provider, preferred, fallback, fallbackProvider string) *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "fallback-test-agent",
			Models: agents.AgentModels{
				Provider:         provider,
				Preferred:        preferred,
				Fallback:         fallback,
				FallbackProvider: fallbackProvider,
			},
			ContextBudget: 4096,
		},
		Instructions: "Test agent for fallback mechanism.",
	}
}

// ============================================================
// AC1: 同 provider 内模型降级
// Given Agent agent.yaml 配置了 models.preferred: sonnet + models.fallback: haiku（同 provider 内模型降级）
// When preferred 模型调用返回 ErrModelNotFound
// Then 自动使用 fallback 模型重试
// ============================================================

func TestATDD_23_5_AC1_SameProviderFallback(t *testing.T) {
	// GIVEN: Agent 配置 preferred: sonnet, fallback: haiku, same provider "claude"
	// Primary LLM device returns ErrModelNotFound
	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 0, llm.ErrModelNotFound),
	}
	// Fallback should use same device with different model, returning success
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("fallback response from haiku", 10),
	}

	k := newSameProviderFallbackKernel(t, primaryFile, fallbackFile, "claude")

	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test same-provider fallback", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC1: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC1: process not found after spawn")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("AC1: expected exit code 0 (fallback success), got %d: %s (err: %v)",
				exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AC1: timed out waiting for process completion")
	}

	if proc.Result == "" {
		t.Error("AC1: expected non-empty result from fallback model")
	}
}

func TestATDD_23_5_AC1_SameProviderModelDowngrade(t *testing.T) {
	// GIVEN: provider: claude, preferred: sonnet, fallback: haiku
	// WHEN: preferred model fails with ErrModelNotFound
	// THEN: same /dev/llm/claude device is used with model switched to "haiku"

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 0, llm.ErrModelNotFound),
	}
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("haiku response", 5),
	}

	k := newSameProviderFallbackKernel(t, primaryFile, fallbackFile, "claude")

	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test model downgrade", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "haiku response" {
		t.Errorf("expected 'haiku response', got %q", proc.Result)
	}
}

// ============================================================
// AC2: 跨 provider fallback
// Given Agent 配置了跨 provider fallback（provider: ollama + fallback 对应 claude）
// When Ollama 调用失败（HTTP 5xx、连接超时、连接拒绝、认证失败）
// Then 自动切换到 claude provider 的 fallback 模型
// And 切换延迟 <= 1 秒 (NFR33)
// ============================================================

func TestATDD_23_5_AC2_CrossProviderFallback(t *testing.T) {
	// GIVEN: provider: ollama, fallback: haiku, fallback_provider: claude
	// WHEN: Ollama device returns HTTP 500 error
	// THEN: automatically switch to /dev/llm/claude with model haiku

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 500, fmt.Errorf("internal server error")),
	}
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("claude fallback response", 15),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")

	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test cross-provider fallback", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC2: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC2: process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("AC2: expected exit code 0 (cross-provider fallback success), got %d: %s",
				exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AC2: timed out")
	}

	if proc.Result != "claude fallback response" {
		t.Errorf("AC2: expected 'claude fallback response', got %q", proc.Result)
	}
}

func TestATDD_23_5_AC2_ConnectionRefused(t *testing.T) {
	// GIVEN: Ollama provider with fallback to claude
	// WHEN: Ollama returns connection refused error
	// THEN: fallback to claude succeeds

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 0, fmt.Errorf("connection refused")),
	}
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("fallback after connection refused", 10),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")

	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test connection refused fallback", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected fallback success, got exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestATDD_23_5_AC2_AuthFailure(t *testing.T) {
	// GIVEN: Provider with fallback configured
	// WHEN: Primary returns ErrAuth
	// THEN: fallback provider is attempted

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 401, llm.ErrAuth),
	}
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("fallback after auth failure", 10),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")

	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test auth failure fallback", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected fallback success, got exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestATDD_23_5_AC2_FallbackLatency(t *testing.T) {
	// GIVEN: Primary provider fails immediately
	// WHEN: Fallback is triggered
	// THEN: Latency from detection to fallback call initiation <= 1 second

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 500, fmt.Errorf("server error")),
	}
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("fast fallback", 10),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")

	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	startTime := time.Now()

	pid, err := k.Spawn("test fallback latency", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		elapsed := time.Since(startTime)
		if exit.Code != 0 {
			t.Fatalf("expected fallback success, got exit code %d: %s", exit.Code, exit.Reason)
		}
		// NFR33: fallback switch latency <= 1 second
		// We measure total time which includes spawn overhead, so allow 2 seconds total
		if elapsed > 2*time.Second {
			t.Errorf("AC2 NFR33: fallback latency too high: %v (expected <= 1s switch time)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// ============================================================
// AC3: 所有 provider 均不可用 -> Zombie 状态
// Given Fallback 也失败
// When 所有配置的 provider 均不可用
// Then 进程转为 Zombie 状态，错误信息包含所有尝试过的 provider 列表和各自失败原因
// ============================================================

func TestATDD_23_5_AC3_AllProvidersExhausted(t *testing.T) {
	// GIVEN: Both primary and fallback providers fail
	// WHEN: reasonStep attempts both
	// THEN: process enters Zombie with comprehensive error containing both providers

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 0, fmt.Errorf("connection refused")),
	}
	fallbackFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 401, llm.ErrAuth),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")

	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test all providers exhausted", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC3: Spawn should succeed (failure happens during reasoning): %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC3: process not found")
	}

	select {
	case exit := <-proc.Done:
		// Should have non-zero exit code
		if exit.Code == 0 {
			t.Fatal("AC3: expected non-zero exit code when all providers fail")
		}

		// Error should contain both provider names and their failure reasons
		if exit.Err == nil {
			t.Fatal("AC3: expected error in exit status")
		}
		errMsg := exit.Err.Error()

		if !strings.Contains(errMsg, "ollama") {
			t.Errorf("AC3: error should mention primary provider 'ollama', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "claude") {
			t.Errorf("AC3: error should mention fallback provider 'claude', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "connection refused") {
			t.Errorf("AC3: error should contain primary failure reason, got: %s", errMsg)
		}

		// Verify process is in Zombie state
		state := proc.GetState()
		if state != types.StateZombie && state != types.StateDead {
			t.Errorf("AC3: expected Zombie or Dead state, got %d", state)
		}

	case <-time.After(5 * time.Second):
		t.Fatal("AC3: timed out")
	}
}

func TestATDD_23_5_AC3_ErrorContainsBothProviders(t *testing.T) {
	// GIVEN: Primary (groq) fails with timeout, fallback (claude) fails with rate limit
	// WHEN: All providers exhausted
	// THEN: Error message includes: "primary groq: ... ; fallback claude: ..."

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("groq", 0, llm.ErrTimeout),
	}
	fallbackFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 429, llm.ErrRateLimit),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "groq", "claude")

	agent := fallbackAgentInfo("groq", "mixtral", "haiku", "claude")

	pid, err := k.Spawn("test error chain", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code == 0 {
			t.Fatal("expected failure exit code")
		}
		if exit.Err == nil {
			t.Fatal("expected error in exit status")
		}
		errMsg := exit.Err.Error()

		// Both provider names present
		if !strings.Contains(errMsg, "groq") || !strings.Contains(errMsg, "claude") {
			t.Errorf("error should mention both providers, got: %s", errMsg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// ============================================================
// AC4: Strace 输出中可见 provider 切换事件
// Given Fallback 成功
// When 任务完成
// Then strace 输出中可见 provider 切换事件
// ============================================================

func TestATDD_23_5_AC4_StraceShowsFallback(t *testing.T) {
	// GIVEN: Primary fails, fallback succeeds with strace enabled
	// WHEN: Process completes via fallback
	// THEN: DebugChan contains fallback event with primary and fallback device info

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 500, fmt.Errorf("server error")),
	}
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("fallback strace test", 10),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")

	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test strace fallback event", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC4: Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC4: process not found")
	}

	// Collect debug events
	var fallbackEventFound atomic.Bool
	debugCh := proc.DebugChan
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range debugCh {
			args := evt.Args
			if args == nil {
				continue
			}
			if action, ok := args["action"]; ok && action == "fallback" {
				fallbackEventFound.Store(true)
				// Verify event contains expected fields
				if _, ok := args["primary_device"]; !ok {
					t.Error("AC4: fallback event missing 'primary_device' field")
				}
				if _, ok := args["fallback_device"]; !ok {
					t.Error("AC4: fallback event missing 'fallback_device' field")
				}
				if _, ok := args["primary_error"]; !ok {
					t.Error("AC4: fallback event missing 'primary_error' field")
				}
			}
		}
	}()

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("AC4: expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AC4: timed out")
	}

	k.Reap(pid) // trigger resource cleanup (closes DebugChan)
	<-done

	if !fallbackEventFound.Load() {
		t.Error("AC4: expected fallback event in DebugChan, but none found")
	}
}

func TestATDD_23_5_AC4_StraceShowsExhausted(t *testing.T) {
	// GIVEN: Both primary and fallback fail
	// WHEN: Process enters zombie
	// THEN: DebugChan contains fallback_exhausted event

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 0, fmt.Errorf("connection refused")),
	}
	fallbackFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 500, fmt.Errorf("server error")),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")

	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test strace exhausted", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	var exhaustedEventFound atomic.Bool
	debugCh := proc.DebugChan
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range debugCh {
			args := evt.Args
			if args == nil {
				continue
			}
			if action, ok := args["action"]; ok && action == "fallback_exhausted" {
				exhaustedEventFound.Store(true)
				// Verify both errors present
				if _, ok := args["primary_error"]; !ok {
					t.Error("exhausted event missing 'primary_error'")
				}
				if _, ok := args["fallback_error"]; !ok {
					t.Error("exhausted event missing 'fallback_error'")
				}
			}
		}
	}()

	select {
	case <-proc.Done:
		// Expected to fail
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	k.Reap(pid) // trigger resource cleanup (closes DebugChan)
	<-done

	if !exhaustedEventFound.Load() {
		t.Error("expected fallback_exhausted event in DebugChan, but none found")
	}
}

// ============================================================
// AC5: Agent 未配置 fallback 时直接报错
// Given Agent 未配置 fallback（models.fallback 为空）
// When 首选 provider 调用失败
// Then 直接报错，不尝试 fallback
// ============================================================

func TestATDD_23_5_AC5_NoFallbackConfigured(t *testing.T) {
	// GIVEN: Agent has no fallback model configured
	// WHEN: Primary provider call fails
	// THEN: Process fails immediately without attempting fallback

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 500, fmt.Errorf("server error")),
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return primaryFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	// Agent with NO fallback configured
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "no-fallback-agent",
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
				Fallback:  "", // No fallback
			},
			ContextBudget: 4096,
		},
		Instructions: "Test agent without fallback.",
	}

	pid, err := k.Spawn("test no fallback", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC5: Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC5: process not found")
	}

	startTime := time.Now()

	select {
	case exit := <-proc.Done:
		elapsed := time.Since(startTime)
		// Should fail, not succeed
		if exit.Code == 0 {
			t.Fatal("AC5: expected failure when no fallback configured and primary fails")
		}
		// Should fail quickly (no fallback delay)
		if elapsed > 2*time.Second {
			t.Errorf("AC5: should fail immediately without fallback delay, took %v", elapsed)
		}
		// Error should be from primary only
		if exit.Err == nil {
			t.Fatal("AC5: expected error in exit status")
		}
		errMsg := exit.Err.Error()
		if !strings.Contains(errMsg, "server error") {
			t.Errorf("AC5: error should contain primary failure reason, got: %s", errMsg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AC5: timed out")
	}
}

func TestATDD_23_5_AC5_EmptyFallbackNoRetry(t *testing.T) {
	// GIVEN: Agent with empty fallback fields
	// WHEN: Primary fails
	// THEN: No fallback event emitted in strace (only primary error event)

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 0, llm.ErrTimeout),
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return primaryFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "no-fallback-agent",
			Models: agents.AgentModels{
				Provider: "claude",
				Fallback: "",
			},
			ContextBudget: 4096,
		},
		Instructions: "No fallback test.",
	}

	pid, err := k.Spawn("test no fallback no retry", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Drain debug events and verify no fallback event
	var fallbackSeen atomic.Bool
	debugCh := proc.DebugChan
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range debugCh {
			args := evt.Args
			if args == nil {
				continue
			}
			if action, ok := args["action"]; ok {
				if action == "fallback" || action == "fallback_exhausted" {
					fallbackSeen.Store(true)
				}
			}
		}
	}()

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	k.Reap(pid) // trigger resource cleanup (closes DebugChan)
	<-done

	if fallbackSeen.Load() {
		t.Error("AC5: should NOT emit any fallback events when no fallback configured")
	}
}

// ============================================================
// Edge Cases: Fallback Provider Not Registered
// ============================================================

func TestATDD_23_5_FallbackProviderNotRegistered(t *testing.T) {
	// GIVEN: Agent configures fallback_provider: "nonexist" (not registered)
	// WHEN: Primary fails
	// THEN: Fallback is not available, behaves like no-fallback (AC5 path)

	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 500, fmt.Errorf("server error")),
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return primaryFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.SetProviderResolver(
		func() []string { return []string{"claude"} },
		func(name string) bool { return name == "claude" },
	)

	// Agent references nonexistent fallback provider
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "bad-fallback-agent",
			Models: agents.AgentModels{
				Provider:         "claude",
				Preferred:        "sonnet",
				Fallback:         "haiku",
				FallbackProvider: "nonexist",
			},
			ContextBudget: 4096,
		},
		Instructions: "Agent with unresolvable fallback provider.",
	}

	pid, err := k.Spawn("test unregistered fallback", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed (fallback resolution failure is non-blocking): %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code == 0 {
			t.Fatal("expected failure when primary fails and fallback provider unregistered")
		}
		if exit.Err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// ============================================================
// Edge Cases: AgentModels FallbackProvider field
// ============================================================

func TestATDD_23_5_AgentModels_FallbackProvider_YAMLParsing(t *testing.T) {
	// GIVEN: AgentModels struct should have FallbackProvider field
	// WHEN: Setting FallbackProvider
	// THEN: Field is populated correctly

	models := agents.AgentModels{
		Provider:         "ollama",
		Preferred:        "llama3",
		Fallback:         "haiku",
		FallbackProvider: "claude",
	}

	if models.FallbackProvider != "claude" {
		t.Errorf("expected FallbackProvider 'claude', got %q", models.FallbackProvider)
	}
}

func TestATDD_23_5_Process_FallbackFields(t *testing.T) {
	// GIVEN: Process struct should have FallbackModel, FallbackProvider, FallbackDevice fields
	// WHEN: Process is created and fields are set
	// THEN: Fallback fields are accessible

	proc := NewProcess(0, "test", nil)

	proc.FallbackModel = "haiku"
	proc.FallbackProvider = "claude"
	proc.FallbackDevice = "/dev/llm/claude"

	if proc.FallbackModel != "haiku" {
		t.Errorf("expected FallbackModel 'haiku', got %q", proc.FallbackModel)
	}
	if proc.FallbackProvider != "claude" {
		t.Errorf("expected FallbackProvider 'claude', got %q", proc.FallbackProvider)
	}
	if proc.FallbackDevice != "/dev/llm/claude" {
		t.Errorf("expected FallbackDevice '/dev/llm/claude', got %q", proc.FallbackDevice)
	}
}
