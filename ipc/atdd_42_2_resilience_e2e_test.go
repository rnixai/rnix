package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// ATDD 42.2: 韧性层 — 端到端 (AC#11)
//
// 模拟流程：
//   1. Daemon 崩溃残留：写 proc-info.json (state=running) + checkpoint.json
//   2. Daemon 重启：LoadHistory + ListResumable 发现可恢复条目
//   3. 用户 resume：通过 ResumeWithOpts 走 checkpoint 优先路径
//   4. 原残留快照应在 reap 时被新进程覆盖（同 UUID 继承）
//
// RED PHASE: 全链路依赖 ListResumable + checkpoint-aware ResumeWithOpts 联动。
// =============================================================================

// writeCrashLeftover writes the disk artifacts of a process killed mid-run by
// a daemon crash (proc-info.json still says state=running because reap never
// fired). Includes checkpoint.json so 42-1's checkpoint-priority resume path
// is exercised.
func writeCrashLeftover(t *testing.T, baseDir, uuid string, lastStep int) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	info := map[string]any{
		"pid":         77,
		"uuid":        uuid,
		"state":       "running",
		"intent":      "crash leftover task",
		"provider":    "claude",
		"model":       "claude-4",
		"tokens_used": 800,
		"max_steps":   30,
		"created_at":  "2026-05-16T08:00:00Z",
	}
	infoBytes, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), infoBytes, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}

	cp := map[string]any{
		"version":          1,
		"uuid":             uuid,
		"last_step":        lastStep,
		"timestamp":        "2026-05-16T08:45:00Z",
		"context_snapshot": json.RawMessage(`{"messages":[{"role":"user","content":"resume me"}],"max_size":64,"system_prompt":"test"}`),
		"proc_state": map[string]any{
			"pid":             77,
			"provider":        "claude",
			"model":           "claude-4",
			"skills":          []string{"code-analyst"},
			"allowed_devices": []string{"/dev/fs", "/dev/shell"},
			"intent":          "crash leftover task",
			"max_steps":       30,
			"used_tokens":     800,
		},
	}
	cpBytes, _ := json.Marshal(cp)
	if err := os.WriteFile(filepath.Join(dir, "checkpoint.json"), cpBytes, 0o600); err != nil {
		t.Fatalf("write checkpoint.json: %v", err)
	}

	// Minimal process-meta.json so ResumeWithOpts history fallback can satisfy any
	// callers that touch it; checkpoint path will not need it.
	meta, _ := json.Marshal(map[string]any{"system_prompt": "test", "tool_defs": []any{}})
	if err := os.WriteFile(filepath.Join(dir, "process-meta.json"), meta, 0o600); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}
}

// --- 42.2-INT-003: 端到端 — 崩溃残留 → ListResumable → Resume → 清理 (AC#11) ---

func TestATDD_42_2_INT_003_E2E_CrashRecovery(t *testing.T) {


	client, kern, baseDir, llmFile := setupResumeIPCTest(t)
	uuid := "e2e-crash-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeCrashLeftover(t, baseDir, uuid, 12)

	// Daemon 重启模拟：触发 LoadHistory（已有）+ ListResumable（42.2 新增）。
	if err := kern.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	resp, err := client.ListResumable()
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	if len(resp.Processes) != 1 {
		t.Fatalf("ListResumable len = %d, want 1 (crash leftover)", len(resp.Processes))
	}
	if resp.Processes[0].UUID != uuid {
		t.Errorf("UUID = %q, want %q", resp.Processes[0].UUID, uuid)
	}
	if resp.Processes[0].LastStep != 12 {
		t.Errorf("LastStep = %d, want 12", resp.Processes[0].LastStep)
	}

	// 用户 resume：ResumeWithOpts(fork=false) 走 checkpoint 路径（42-1）。
	//
	// 安装握手 gate：让 resumed 进程在首次 LLM Read 处停住（此刻已 past
	// proc.Start() → Running），消除原 flaky 来源——mock 默认返回 complete 会
	// 使进程秒退被 reap，导致随后的 ListResumable 把磁盘残留（state=running）
	// 重新算作可恢复。等收到 reached 再断言，断言完 close(release) 放行。
	reached, release := llmFile.parkOnRead()
	resumeResp, err := client.ResumeWithOpts(uuid, false)
	if err != nil {
		t.Fatalf("ResumeWithOpts: %v", err)
	}
	<-reached
	defer close(release)
	if resumeResp.UUID != uuid {
		t.Errorf("resumed UUID = %q, want %q (fork=false inherits)", resumeResp.UUID, uuid)
	}
	if resumeResp.ResumedFromStep != 13 {
		t.Errorf("ResumedFromStep = %d, want 13 (last_step+1)", resumeResp.ResumedFromStep)
	}

	// 恢复后再次 ListResumable：UUID 已在 procTable → 应从 resumable 列表中过滤掉 (AC#10)。
	respAfter, err := client.ListResumable()
	if err != nil {
		t.Fatalf("ListResumable after resume: %v", err)
	}
	for _, p := range respAfter.Processes {
		if p.UUID == uuid {
			t.Errorf("resumed UUID %q must not appear in resumable list (AC#10)", uuid)
		}
	}
}
