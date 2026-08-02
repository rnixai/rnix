package ipc

// Story 72.1 AC3 / AC8-3: the three observe handlers must report read failures
// explicitly instead of masquerading them as NOT_FOUND / "not yet recorded" /
// an empty-but-OK list.
//
// Failure injection: a directory standing in for the .jsonl file. os.Open on a
// directory succeeds, but the first Read returns EISDIR — so jsonl.Scan fails
// with a real I/O error. This is unaffected by the os.Stat pre-check in
// resolveStepsPath/resolveEventsPath (a directory stats fine) and, unlike
// chmod 0000, does not stop working under root.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

const testUUID72 = "019576f5-ac08-7000-8000-720100000001"

// makeDirAsFile creates<projBase>/steps/<uuid>/<name> as a DIRECTORY, so that
// os.Open succeeds but reading it fails with EISDIR.
func makeDirAsFile(t *testing.T, projBase, uuid, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(projBase, "steps", uuid, name), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
}

// --- handleListSteps ------------------------------------------------------

func TestATDD_72_1_AC3_ListSteps_ReadFailure_NotNotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	// steps.jsonl is a directory → open ok, read fails with EISDIR.
	makeDirAsFile(t, projBase, testUUID72, "steps.jsonl")

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: testUUID72})

	if resp.OK {
		t.Fatal("list_steps on unreadable steps.jsonl: want OK=false")
	}
	if resp.Error == nil {
		t.Fatal("list_steps: want error payload")
	}
	// The whole point: a read failure is NOT "process not found".
	if resp.Error.Code == "NOT_FOUND" {
		t.Errorf("list_steps read failure reported as NOT_FOUND (must be %s)", types.ErrInternal)
	}
	if resp.Error.Code != string(types.ErrInternal) {
		t.Errorf("list_steps error code = %q, want %q", resp.Error.Code, types.ErrInternal)
	}
}

// The genuine "process not found" path must be preserved: a UUID with no data
// directory at all still yields NOT_FOUND (resolveStepsPath returns "").
func TestATDD_72_1_AC3_ListSteps_MissingProcess_StillNotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	kernel.TestSetupDataDir(t, srv.kern)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: "019576f5-ac08-7000-8000-deadbeef0000"})

	if resp.OK {
		t.Fatal("list_steps for unknown UUID: want OK=false")
	}
	if resp.Error == nil || resp.Error.Code != "NOT_FOUND" {
		t.Errorf("list_steps unknown UUID: want NOT_FOUND, got %+v", resp.Error)
	}
}

// --- handleGetStepDetail --------------------------------------------------

func TestATDD_72_1_AC3_GetStepDetail_ReadFailure_NotNotRecorded(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	makeDirAsFile(t, projBase, testUUID72, "steps.jsonl")

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{UUID: testUUID72, Step: 1})

	if resp.OK {
		t.Fatal("get_step_detail on unreadable steps.jsonl: want OK=false")
	}
	if resp.Error == nil {
		t.Fatal("get_step_detail: want error payload")
	}
	// A read failure must not be reported as "step not yet recorded".
	if resp.Error.Code == "not_found" {
		t.Errorf("get_step_detail read failure reported as not_found / not-yet-recorded (must be %s)", types.ErrInternal)
	}
	if resp.Error.Code != string(types.ErrInternal) {
		t.Errorf("get_step_detail error code = %q, want %q", resp.Error.Code, types.ErrInternal)
	}
}

// A genuinely absent step (file readable, step number not present) keeps the
// original "not yet recorded" wording via the ErrStepNotFound sentinel.
func TestATDD_72_1_AC3_GetStepDetail_AbsentStep_StillNotRecorded(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	writeTestStepsUUID(t, projBase, testUUID72, []types.StepRecord{testStepRecord(1)})

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{UUID: testUUID72, Step: 99})

	if resp.OK {
		t.Fatal("get_step_detail for absent step: want OK=false")
	}
	if resp.Error == nil || resp.Error.Code != "not_found" {
		t.Errorf("get_step_detail absent step: want not_found, got %+v", resp.Error)
	}
}

// --- handleListEvents -----------------------------------------------------

func TestATDD_72_1_AC3_ListEvents_ReadFailure_OKFalse(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	// events.jsonl is a directory → open ok, read fails with EISDIR.
	makeDirAsFile(t, projBase, testUUID72, "events.jsonl")

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListEvents, ListEventsRequest{UUID: testUUID72})

	// F3: the former behavior swallowed this into OK=true + empty list.
	if resp.OK {
		t.Fatal("list_events on unreadable events.jsonl: want OK=false (was silently OK=true)")
	}
	if resp.Error == nil || resp.Error.Code != string(types.ErrInternal) {
		t.Errorf("list_events read failure: want %s, got %+v", types.ErrInternal, resp.Error)
	}
}

// Paired assertion: an absent events file (process produced no syscalls) is NOT
// a failure — it must stay OK=true with an empty list. Without this companion,
// a change that flips the empty-path branch to an error would still pass the
// read-failure test above.
func TestATDD_72_1_AC3_ListEvents_AbsentFile_StillOKEmpty(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	// A steps dir exists (so FindBaseDirByUUID resolves) but no events.jsonl.
	if err := os.MkdirAll(filepath.Join(projBase, "steps", testUUID72), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListEvents, ListEventsRequest{UUID: testUUID72})

	if !resp.OK {
		t.Fatalf("list_events with no events file: want OK=true, got error %+v", resp.Error)
	}
	var result ListEventsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Events) != 0 {
		t.Errorf("list_events with no events file: want empty list, got %d", len(result.Events))
	}
}

// --- ParseErrors on the wire (AC4 / AC8-3) --------------------------------

func TestATDD_72_1_AC4_ListSteps_ParseErrors_Reported(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	// One good line + one malformed line.
	dir := filepath.Join(projBase, "steps", testUUID72)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{"step":1,"action":"tool_call","summary":"ok"}` + "\n{malformed\n"
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: testUUID72})
	if !resp.OK {
		t.Fatalf("list_steps: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.ParseErrors != 1 {
		t.Errorf("ParseErrors = %d, want 1", result.ParseErrors)
	}
}

// omitempty regression: a clean file must not carry the parse_errors key on the
// wire, so today's byte stream is unchanged when there is nothing to report.
func TestATDD_72_1_AC4_ListSteps_ParseErrors_OmittedWhenZero(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	writeTestStepsUUID(t, projBase, testUUID72, []types.StepRecord{testStepRecord(1)})

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: testUUID72})
	if !resp.OK {
		t.Fatalf("list_steps: %+v", resp.Error)
	}

	var raw map[string]any
	if err := json.Unmarshal(resp.Payload, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, present := raw["parse_errors"]; present {
		t.Errorf("parse_errors present on wire for a clean file (omitempty regression)")
	}
}
