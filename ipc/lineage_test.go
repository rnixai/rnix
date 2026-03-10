package ipc

// =============================================================================
// ATDD Story 20.5: Differentiation Lineage Graph -- IPC Tests
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
// - Task 4: IPC lineage handler (protocol, server dispatch, client method)
//
// Priority: P0 (IPC is the only way CLI can query lineage)
// Test Level: Integration (IPC server+client roundtrip)

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rnixai/rnix/kernel"
)

func TestServer_Lineage_Success(t *testing.T) {
	// Given: a server with a process that has lineage events
	// When: sending a lineage request for that PID
	// Then: response contains the lineage events
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "analyze code", nil)
	_ = proc.Start()
	lineage := kernel.NewLineage()
	lineage.Record(kernel.LineageEvent{
		Timestamp:  time.Now(),
		Phase:      "initial",
		Skills:     []string{"code-analysis", "git-tools"},
		Trigger:    "analyze code quality",
		FromMemory: false,
	})
	lineage.Record(kernel.LineageEvent{
		Timestamp:  time.Now().Add(time.Second),
		Phase:      "progressive",
		Skills:     []string{"test-runner"},
		Trigger:    "need test execution",
		FromMemory: false,
	})
	proc.SetLineage(lineage)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodLineage, LineageRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("lineage request failed: %+v", resp.Error)
	}

	var lr LineageResponse
	if err := json.Unmarshal(resp.Payload, &lr); err != nil {
		t.Fatalf("unmarshal lineage response: %v", err)
	}
	if lr.PID != proc.PID {
		t.Errorf("PID = %d, want %d", lr.PID, proc.PID)
	}
	if len(lr.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(lr.Events))
	}
	if lr.Events[0].Phase != "initial" {
		t.Errorf("first event phase = %q, want 'initial'", lr.Events[0].Phase)
	}
	if lr.Events[1].Phase != "progressive" {
		t.Errorf("second event phase = %q, want 'progressive'", lr.Events[1].Phase)
	}
	if len(lr.Events[0].Skills) != 2 {
		t.Errorf("first event skills count = %d, want 2", len(lr.Events[0].Skills))
	}
	if lr.Events[0].TimestampMs <= 0 {
		t.Errorf("first event timestamp_ms = %d, want positive", lr.Events[0].TimestampMs)
	}
}

func TestServer_Lineage_NotFound(t *testing.T) {
	// Given: a server with no process for PID 99999
	// When: sending a lineage request for that PID
	// Then: response has OK=false with NOT_FOUND error
	_, sockPath, _ := setupTestServer(t)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodLineage, LineageRequest{PID: 99999})
	if resp.OK {
		t.Fatal("lineage should fail for nonexistent PID")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestServer_Lineage_NoDifferentiation(t *testing.T) {
	// Given: a server with a non-stem process (no lineage)
	// When: sending a lineage request for that PID
	// Then: response is OK with empty events list
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "regular task", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodLineage, LineageRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("lineage request failed: %+v", resp.Error)
	}

	var lr LineageResponse
	if err := json.Unmarshal(resp.Payload, &lr); err != nil {
		t.Fatalf("unmarshal lineage response: %v", err)
	}
	if len(lr.Events) != 0 {
		t.Errorf("expected 0 events for non-differentiated process, got %d", len(lr.Events))
	}
}
