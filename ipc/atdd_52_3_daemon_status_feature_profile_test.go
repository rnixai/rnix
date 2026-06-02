package ipc

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// ATDD 52.3 AC#1: DaemonStatusResponse 包含 feature profile 信息
//
// 来源 Story：_bmad-output/implementation-artifacts/52-3-observability-cli-strace-procinfo.md
// Story Key: 52-3-observability-cli-strace-procinfo
//
// ============================== RED PHASE ==============================
// 预期失败原因：
//   1. DaemonStatusResponse 无 FeatureProfile / FeatureFlags 字段（编译错误）
//   2. Server 无 SetFeatureProfile 方法（编译错误）
//   3. handleDaemonStatus 不填充新字段（断言失败）
// ======================================================================

// ---------- AC1: DaemonStatusResponse 结构体新字段 ----------

func TestATDD_52_3_AC1_DaemonStatusResponse_HasFeatureProfileField(t *testing.T) {
	resp := DaemonStatusResponse{
		Version:        "0.9.0",
		FeatureProfile: "adaptive",
		FeatureFlags: map[string]bool{
			"planning": true,
			"replan":   true,
			"immune":   false,
		},
	}

	if resp.FeatureProfile != "adaptive" {
		t.Errorf("FeatureProfile = %q, want %q", resp.FeatureProfile, "adaptive")
	}
	if resp.FeatureFlags["planning"] != true {
		t.Errorf("FeatureFlags[planning] = %v, want true", resp.FeatureFlags["planning"])
	}
	if resp.FeatureFlags["immune"] != false {
		t.Errorf("FeatureFlags[immune] = %v, want false", resp.FeatureFlags["immune"])
	}
}

func TestATDD_52_3_AC1_DaemonStatusResponse_JSON_Roundtrip(t *testing.T) {
	original := DaemonStatusResponse{
		Version:        "0.9.0",
		DaemonCommit:   "abc1234",
		FeatureProfile: "baseline",
		FeatureFlags: map[string]bool{
			"planning":       false,
			"replan":         false,
			"specialize":     false,
			"discover_skill": false,
			"spawn":          false,
			"diff_memory":    false,
			"stem_matcher":   false,
			"immune":         false,
			"compaction":     false,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var restored DaemonStatusResponse
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if restored.FeatureProfile != "baseline" {
		t.Errorf("FeatureProfile roundtrip: got %q, want %q", restored.FeatureProfile, "baseline")
	}
	if len(restored.FeatureFlags) != 9 {
		t.Errorf("FeatureFlags roundtrip: got %d flags, want 9", len(restored.FeatureFlags))
	}
}

func TestATDD_52_3_AC1_DaemonStatusResponse_FeatureFlags_Omitempty(t *testing.T) {
	resp := DaemonStatusResponse{
		Version:        "0.9.0",
		FeatureProfile: "full",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	if _, exists := m["feature_flags"]; exists {
		t.Error("DaemonStatusResponse with nil FeatureFlags should omit feature_flags from JSON (omitempty)")
	}

	if _, exists := m["feature_profile"]; !exists {
		t.Error("DaemonStatusResponse.FeatureProfile should always be present in JSON")
	}
}

// ---------- AC1: Server.SetFeatureProfile + handleDaemonStatus 集成 ----------

func TestATDD_52_3_AC1_ServerSetFeatureProfile_Integration(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.9.0", "abc1234", "2026-06-01")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

	flags := map[string]bool{
		"planning":       true,
		"replan":         true,
		"specialize":     true,
		"discover_skill": true,
		"spawn":          true,
		"diff_memory":    true,
		"stem_matcher":   true,
		"immune":         false,
		"compaction":     true,
	}
	srv.SetFeatureProfile("adaptive", flags)

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	status, err := client.DaemonStatus()
	if err != nil {
		t.Fatalf("DaemonStatus: %v", err)
	}

	if status.FeatureProfile != "adaptive" {
		t.Errorf("DaemonStatus().FeatureProfile = %q, want %q", status.FeatureProfile, "adaptive")
	}
	if status.FeatureFlags["immune"] != false {
		t.Errorf("DaemonStatus().FeatureFlags[immune] = %v, want false", status.FeatureFlags["immune"])
	}
	if len(status.FeatureFlags) != 9 {
		t.Errorf("DaemonStatus().FeatureFlags has %d entries, want 9", len(status.FeatureFlags))
	}
}

// suppress unused import warnings — these are needed at compile time to verify
// the test can reference the right types.
var _ net.Conn
