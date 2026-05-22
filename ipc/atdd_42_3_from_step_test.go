package ipc

import (
	"strings"
	"testing"
)

// =============================================================================
// ATDD 42.3: 可观测层 — IPC `--from-step` 集成测试（AC#1）
//
// Covers 1 scenario:
//   - IPC-001: ResumeRequest.FromStep 经 IPC 透传到 Kernel，响应 ResumedFromStep=N+1
//
// RED PHASE:
//   - server handleResume 尚未透传 FromStep → kernel 看到 FromStep=0（退化）
//   - 测试用 t.Skip 跳过断言；dev-story 落地透传后移除 skip
// =============================================================================

// --- 42.3-IPC-001: ResumeRequest.FromStep 透传 (AC#1) ---

func TestATDD_42_3_IPC_001_ResumeRequest_FromStep_Propagation(t *testing.T) {
	client, _, baseDir, _ := setupResumeIPCTest(t)
	uuid := "ipc-fromstep-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeIPCTestData(t, baseDir, uuid, 50)

	resp, err := client.ResumeWithOptsV2(uuid, true /*fork*/, 30 /*from_step*/)
	if err != nil {
		t.Fatalf("ResumeWithOptsV2 failed: %v", err)
	}
	if resp.PID == 0 {
		t.Error("PID should be non-zero")
	}
	// Fork must produce a new UUID.
	if resp.UUID == uuid {
		t.Errorf("UUID = %q, want new fork UUID", resp.UUID)
	}
	// FromStep=30 ⇒ ResumedFromStep = 31.
	if resp.ResumedFromStep != 31 {
		t.Errorf("ResumedFromStep = %d, want 31 (FromStep=30 + 1)", resp.ResumedFromStep)
	}
}

// --- 42.3-IPC-001b: FromStep 越界经 IPC 返回错误 (AC#2) ---

func TestATDD_42_3_IPC_001b_ResumeRequest_FromStep_OutOfRange(t *testing.T) {
	client, _, baseDir, _ := setupResumeIPCTest(t)
	uuid := "ipc-oor-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeIPCTestData(t, baseDir, uuid, 20)

	_, err := client.ResumeWithOptsV2(uuid, false, 999)
	if err == nil {
		t.Fatal("ResumeWithOptsV2 should reject FromStep=999 on 20-step history")
	}
	if !strings.Contains(err.Error(), "exceeds total steps") &&
		!strings.Contains(err.Error(), "from_step") {
		t.Errorf("err = %v, want substring 'exceeds total steps' or 'from_step'", err)
	}
}
