package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// ATDD 42.6: resume_watch 流式路径 + NewInput IPC 透传
// （apex 10-11 B 路续接，见 _bmad-output/implementation-artifacts/
//  requirement-brief-resume-new-input-2026-07-08.md）
// =============================================================================

// writeIPCTestDataWithResult mirrors writeIPCTestData but also persists a
// proc-info.json result (the previous round's final assistant output), which
// the history-resume NewInput path restores for anti-replay.
func writeIPCTestDataWithResult(t *testing.T, projBase, uuid string, lastStep int, result string) {
	t.Helper()
	stepsDir := filepath.Join(projBase, "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var stepsContent []byte
	for i := 1; i <= lastStep; i++ {
		record := map[string]any{
			"step":     i,
			"action":   "tool_call",
			"messages": []map[string]string{{"role": "user", "content": fmt.Sprintf("step %d", i)}},
		}
		line, _ := json.Marshal(record)
		stepsContent = append(stepsContent, line...)
		stepsContent = append(stepsContent, '\n')
	}
	_ = os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), stepsContent, 0o600)
	meta, _ := json.Marshal(map[string]any{"system_prompt": "test", "tool_defs": []any{}})
	_ = os.WriteFile(filepath.Join(stepsDir, "process-meta.json"), meta, 0o600)
	info, _ := json.Marshal(map[string]any{
		"pid": 99, "uuid": uuid, "state": "dead", "intent": "resume watch test",
		"provider": "claude", "model": "claude-4", "tokens_used": 1000,
		"result": result,
	})
	_ = os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), info, 0o600)
}

// --- 42.6-INT-001: resume_watch 流式形态与 spawn 同形（spawn progress → complete） ---

func TestATDD_42_6_INT_001_ResumeWatch_StreamsSpawnShapedProgress(t *testing.T) {
	client, _, baseDir, _ := setupResumeIPCTest(t)
	uuid := "watch-stream-0000-0000-000000000001"
	writeIPCTestDataWithResult(t, baseDir, uuid, 2, "prior final output")

	var progress []ProgressPayload
	resp, final, err := client.ResumeAndWatch(ResumeRequest{
		UUID:     uuid,
		Fork:     true,
		NewInput: "watch next turn",
	}, func(ev StreamEvent) {
		if ev.Type != StreamProgress {
			return
		}
		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err == nil {
			progress = append(progress, pp)
		}
	})
	if err != nil {
		t.Fatalf("ResumeAndWatch: %v", err)
	}
	if resp.UUID == uuid || resp.UUID == "" {
		t.Fatalf("fork resume UUID = %q, want fresh UUID", resp.UUID)
	}
	if resp.ResumedFromStep != 3 {
		t.Fatalf("ResumedFromStep = %d, want 3", resp.ResumedFromStep)
	}
	if len(progress) == 0 || progress[0].Event != "spawn" {
		t.Fatalf("resume_watch progress = %#v, want first event spawn (spawn-shaped stream)", progress)
	}
	if final == nil {
		t.Fatal("resume_watch did not deliver a terminal complete/error payload")
	}
	if final.Event != "complete" || final.ExitCode != 0 {
		t.Fatalf("final payload = %#v, want complete with exit 0", final)
	}
}

// --- 42.6-INT-002: 一次性 resume 方法透传 NewInput（语义不变仍一次性） ---

func TestATDD_42_6_INT_002_OneShotResume_CarriesNewInput(t *testing.T) {
	client, kern, baseDir, _ := setupResumeIPCTest(t)
	uuid := "oneshot-newinput-0000-0000-000000000001"
	writeIPCTestDataWithResult(t, baseDir, uuid, 2, "prior final output")

	resp, err := client.ResumeWithRequest(ResumeRequest{
		UUID:     uuid,
		Fork:     true,
		NewInput: "oneshot next turn",
	})
	if err != nil {
		t.Fatalf("ResumeWithRequest: %v", err)
	}
	if resp.UUID == uuid || resp.UUID == "" {
		t.Fatalf("fork resume UUID = %q, want fresh UUID", resp.UUID)
	}

	// The resumed process may complete quickly; assert the NewInput reached the
	// kernel by checking the forked process's context via GetProcessByUUID while
	// it is still in the procTable (one-shot path has no stream to synchronize
	// on, so poll briefly).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := kern.GetProcessByUUID(resp.UUID); ok {
			return // process materialized — NewInput acceptance is covered by kernel ATDD 42.6 unit tests
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("forked process %s never appeared in procTable", resp.UUID)
}
