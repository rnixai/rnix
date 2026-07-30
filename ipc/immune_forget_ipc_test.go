package ipc

// IN-3 F3: immune_forget IPC surface tests.
// Spec: _bmad-output/implementation-artifacts/spec-in-3-immune-false-positive-fix.md

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/kernel"
)

func TestMethodImmuneForget_Constant(t *testing.T) {
	if MethodImmuneForget != "immune_forget" {
		t.Errorf("MethodImmuneForget = %q, want %q", MethodImmuneForget, "immune_forget")
	}
}

func TestImmuneForgetRequest_JSON(t *testing.T) {
	req := ImmuneForgetRequest{Template: "sa-orchestrator", Metric: "Shutdown", All: false}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m["template"] != "sa-orchestrator" || m["metric"] != "Shutdown" {
		t.Errorf("unexpected fields: %v", m)
	}
}

func TestImmuneForgetResponse_JSON(t *testing.T) {
	resp := ImmuneForgetResponse{Removed: 3}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m["removed"] != float64(3) {
		t.Errorf("removed = %v, want 3", m["removed"])
	}
}

// handler 集成：真 server + 真 daemon，验证 forget 端到端删除 + 校验分支。
func TestHandleImmuneForget_EndToEnd(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	dir := t.TempDir()
	seed := kernel.NewImmuneStore(filepath.Join(dir, "global"))
	if err := seed.RewriteThreats([]kernel.ThreatSignature{
		{ID: "t1", Type: kernel.AnomalySyscallFreq, AgentTemplate: "a", Metric: "Open", CreatedAt: time.Now()},
		{ID: "t2", Type: kernel.AnomalySyscallFreq, AgentTemplate: "b", Metric: "Read", CreatedAt: time.Now()},
	}); err != nil {
		t.Fatalf("RewriteThreats: %v", err)
	}
	daemon := kernel.NewImmuneDaemon(kernel.NewImmuneStore(dir), kernel.DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("daemon.Start: %v", err)
	}
	t.Cleanup(daemon.Stop)
	srv.SetImmuneDaemon(daemon)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	// 指定 template：删 1
	result, err := client.ImmuneForget("a", "", false)
	if err != nil {
		t.Fatalf("ImmuneForget(a): %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1", result.Removed)
	}

	// 校验分支：无 template 且非 all → 错误
	client2, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client2.Close()
	if _, err := client2.ImmuneForget("", "", false); err == nil {
		t.Error("expected INVALID error when neither template nor all is given")
	}

	// 全量：删剩余 1
	client3, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client3.Close()
	result, err = client3.ImmuneForget("", "", true)
	if err != nil {
		t.Fatalf("ImmuneForget(all): %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1", result.Removed)
	}
}

// nil immuneDaemon：Removed=0 且不 panic。
func TestHandleImmuneForget_NoDaemon(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	result, err := client.ImmuneForget("", "", true)
	if err != nil {
		t.Fatalf("ImmuneForget: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("Removed = %d, want 0 when daemon is nil", result.Removed)
	}
}
