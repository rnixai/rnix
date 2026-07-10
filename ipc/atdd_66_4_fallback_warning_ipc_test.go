package ipc

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD — Story 66.4 (IPC 面): fallback 解析失败经真 socket 落到 spawn
// StreamProgress payload 的 `warning` 字段, 供 CLI 渲染。
//
// kernel/atdd_66_4_* 已覆盖 proc.FallbackResolveError 的内核语义 + events.jsonl
// 落盘。本文件焊死另一条链: handleSpawn 把 proc.FallbackResolveError 回填到
// spawn ProgressPayload.Warning → 真 Unix socket → SpawnAndWatch 消费。这是
// CLI 可见 warning 的唯一可靠通道 (kernel 在 kern.Spawn 期间 emit 的事件在
// callbackMux 注册前丢失, server_spawn.go:110 注释)。
//
// 两个假绿陷阱 (Dev Notes 测试标准):
//   ①hasProvider nil 放行: 不 SetProviderResolver 则 resolveLLMDevice 放行任何
//     provider → "ghost" 假性解析成功 → warning 恒空。fixture 注册只含 "claude"
//     的全局名单。
//   ②agent 必须非 nil: fallback 块在 `agent != nil` 内; NewServer(nil, nil, …) 的
//     agentLoader==nil → handleSpawn 裸 spawn → fallback 块整体跳过 → warning 恒空。
//     fixture 注入一个返回 minimal agent 的 loader。
// =============================================================================

// setupFallbackWarningE2E starts a real socket server whose /dev/llm/claude is a
// real LLMFile wrapping driver, whose agent loader always yields a minimal agent
// (陷阱 ②), and whose global provider resolver knows only globalProviders (陷阱 ①).
func setupFallbackWarningE2E(t *testing.T, driver llm.LLMDriver, globalProviders []string) string {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	if err := devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude", "")); err != nil {
		t.Fatalf("register /dev/llm/claude: %v", err)
	}
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	// 陷阱 ②: minimal agent so handleSpawn's fallback block (agent != nil) runs.
	agentLoader := func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{
			Manifest: agents.AgentManifest{
				Name:   name,
				Models: agents.AgentModels{Provider: "claude"},
			},
			Instructions: "fixture agent for 66.4 ipc",
		}, nil
	}

	srv := NewServer(nil, agentLoader, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

	// Must precede the kern.Shutdown cleanup below (LIFO) — same order as
	// setupInterruptE2E, avoids "TempDir RemoveAll: directory not empty".
	kernel.TestSetupDataDir(t, kern)

	// 陷阱 ①: register only globalProviders so an unknown fallback provider fails.
	set := make(map[string]bool, len(globalProviders))
	for _, n := range globalProviders {
		set[n] = true
	}
	kern.SetProviderResolver(
		func() []string { return append([]string(nil), globalProviders...) },
		func(name string) bool { return set[name] },
	)

	sockPath := filepath.Join(t.TempDir(), "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})
	return sockPath
}

// spawnAndCaptureWarning spawns across the real socket and returns the Warning
// field of the "spawn" StreamProgress payload. The driver sends `done` immediately
// so the process completes and SpawnAndWatch returns.
func spawnAndCaptureWarning(t *testing.T, sockPath, fallbackProvider string) (warning string, sawSpawn bool) {
	t.Helper()
	_, _, err := dialClient(t, sockPath).SpawnAndWatch(SpawnRequest{
		Intent:           "66.4 fallback warning wire",
		FallbackModel:    "some-fallback-model",
		FallbackProvider: fallbackProvider,
	}, func(ev StreamEvent) {
		if ev.Type != StreamProgress {
			return
		}
		var pp ProgressPayload
		if json.Unmarshal(ev.Payload, &pp) != nil {
			return
		}
		if pp.Event == "spawn" {
			sawSpawn = true
			warning = pp.Warning
		}
	})
	if err != nil {
		t.Fatalf("SpawnAndWatch: %v", err)
	}
	return warning, sawSpawn
}

// doneDriver builds an LLM driver that immediately streams a `done` event so the
// spawned process completes with exit 0.
func doneDriver() *e2eStreamDriver {
	return &e2eStreamDriver{sendDone: true, doneContent: "done"}
}

// AC3/AC5: unresolvable fallback provider surfaces a non-empty warning over the wire.
func TestATDD_66_4_IPC_FallbackResolveFailure_WarningAcrossWire(t *testing.T) {
	sockPath := setupFallbackWarningE2E(t, doneDriver(), []string{"claude"})

	warning, sawSpawn := spawnAndCaptureWarning(t, sockPath, "ghost")
	if !sawSpawn {
		t.Fatal("no spawn StreamProgress event received")
	}
	if warning == "" {
		t.Error("spawn ProgressPayload.Warning should be non-empty (fallback provider ghost unresolvable)")
	}
	if !strings.Contains(warning, "ghost") {
		t.Errorf("warning = %q, should mention the unresolvable provider", warning)
	}
}

// AC5: a resolvable fallback provider leaves warning empty (omitempty → absent).
func TestATDD_66_4_IPC_FallbackResolveOK_NoWarning(t *testing.T) {
	sockPath := setupFallbackWarningE2E(t, doneDriver(), []string{"claude"})

	// fallback provider "claude" is globally registered → resolves fine.
	warning, sawSpawn := spawnAndCaptureWarning(t, sockPath, "claude")
	if !sawSpawn {
		t.Fatal("no spawn StreamProgress event received")
	}
	if warning != "" {
		t.Errorf("warning = %q, want empty (fallback resolved OK; omitempty)", warning)
	}
}
