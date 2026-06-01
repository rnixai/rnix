package ipc

// =============================================================================
// ATDD Story 41.2 AC#3: GetStepDetailResponse 含 Provider / DriverType
// =============================================================================
//
// 测试矩阵：
//   - Live process (in process table)：Provider 来自 proc.Provider；
//     DriverType 来自 driverTypeForProvider 查询 GlobalConfig.Providers
//   - Reaped process (only in history)：Provider 来自 vfs.ProcInfo.Provider；
//     DriverType 同上
//   - 未配置 GlobalConfig.Providers 时 DriverType 留空（dashboard 走 fallback）
//
// 触发：Story 41.2 修复 Cache Hit Rate dashboard 一等指标 · driver 语义分支需要
//       step-level driver type 信息。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// setProviderConfigForTest 给 srv 注入一个最小 providers.yaml 等价的 GlobalConfig，
// 让 driverTypeForProvider 能查到。
func setProviderConfigForTest(srv *Server) {
	cfg := &llm.ProvidersConfig{
		Providers: []llm.ProviderConfig{
			{Name: "anthropic-prod", Driver: llm.DriverAnthropic, DefaultModel: "claude-sonnet-4-6"},
			{Name: "deepseek", Driver: llm.DriverOpenAICompat, DefaultModel: "deepseek-chat"},
			{Name: "openai", Driver: llm.DriverOpenAI, DefaultModel: "gpt-5"},
		},
	}
	srv.SetGlobalConfig(&config.GlobalConfig{Providers: cfg})
	// reset cache so test injection takes effect
	srv.providerDriverCache = nil
}

func TestHandleGetStepDetail_ContainsProviderAndDriverType_LiveProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	setProviderConfigForTest(srv)

	// Create a running process with Provider = "anthropic-prod"
	proc := kernel.NewProcess(0, "anthropic test", nil)
	_ = proc.Start()
	proc.Provider = "anthropic-prod"
	proc.Model = "claude-sonnet-4-6"
	proc.SetFinalSystemPrompt("You are an Anthropic agent.")

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestStepsUUID(t, projBase, proc.UUID, []types.StepRecord{testStepRecord(1)})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 1})
	if !resp.OK {
		t.Fatalf("Live process get_step_detail failed: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if detail.Provider != "anthropic-prod" {
		t.Errorf("Live: Provider = %q, want %q", detail.Provider, "anthropic-prod")
	}
	if detail.DriverType != "anthropic" {
		t.Errorf("Live: DriverType = %q, want %q (resolved via provider config)", detail.DriverType, "anthropic")
	}
}

func TestHandleGetStepDetail_ContainsProviderAndDriverType_ReapedProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	setProviderConfigForTest(srv)

	// Create a reaped process: history entry only, not in proc table.
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	reapedUUID := "00000000-0000-7000-0000-deadbeef0042"
	pid := types.PID(54321)
	writeTestStepsUUID(t, projBase, reapedUUID, []types.StepRecord{testStepRecord(1)})

	// Persist process-meta.json so handleGetStepDetail can read SystemPrompt.
	metaDir := filepath.Join(projBase, "steps", reapedUUID)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	metaPath := filepath.Join(metaDir, "process-meta.json")
	metaJSON := `{"system_prompt":"reaped agent","tool_defs":[]}`
	if err := os.WriteFile(metaPath, []byte(metaJSON), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	// Persist proc-info.json with Provider = "deepseek" then reload history
	// (this models the reaped-process flow: ProcInfo is on disk, daemon loads
	// via LoadHistory).
	procInfo := vfs.ProcInfo{
		PID:       pid,
		UUID:      reapedUUID,
		State:     types.StateDead,
		Provider:  "deepseek",
		Model:     "deepseek-chat",
		CreatedAt: time.Now().Add(-1 * time.Minute),
		DeadAt:    time.Now(),
	}
	if err := kernel.SaveProcInfo(projBase, procInfo); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}
	if err := srv.kern.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{UUID: reapedUUID, Step: 1})
	if !resp.OK {
		t.Fatalf("Reaped process get_step_detail failed: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if detail.Provider != "deepseek" {
		t.Errorf("Reaped: Provider = %q, want %q (from proc-info history)", detail.Provider, "deepseek")
	}
	if detail.DriverType != "openai-compat" {
		t.Errorf("Reaped: DriverType = %q, want %q (deepseek → openai-compat driver)", detail.DriverType, "openai-compat")
	}
}

func TestHandleGetStepDetail_ProviderEmpty_WhenNotConfigured(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	// 故意不调用 setProviderConfigForTest：dashboard 应走 fallback 公式
	// （这是极旧进程数据或 daemon 不知道 provider 配置时的 case）。

	proc := kernel.NewProcess(0, "no-provider test", nil)
	_ = proc.Start()
	// Provider 留空模拟极旧进程数据
	proc.SetFinalSystemPrompt("hi")

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestStepsUUID(t, projBase, proc.UUID, []types.StepRecord{testStepRecord(1)})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 1})
	if !resp.OK {
		t.Fatalf("get_step_detail failed: %+v", resp.Error)
	}
	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.Provider != "" {
		t.Errorf("expected empty Provider for legacy data, got %q", detail.Provider)
	}
	if detail.DriverType != "" {
		t.Errorf("expected empty DriverType when Provider empty, got %q", detail.DriverType)
	}
}
