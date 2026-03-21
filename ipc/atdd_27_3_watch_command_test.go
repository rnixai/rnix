package ipc

// =============================================================================
// ATDD Story 27.3: watch 命令基础 — Level 1 实时流
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-2:  Protocol wire types — MethodWatch constant + WatchRequest serialization
//   AC-3:  ProgressPayload extension — HasError + DurationMs fields
//   AC-5:  callbackMux multi-subscriber — register/unregister/broadcast
//   AC-6:  Server handleWatch — streaming connection + history replay
//   AC-10: Error — PID not found
//   AC-11: callbackMux OnStepComplete fills new fields (DurationMs, HasError)
//   AC-4:  KernelCallbacks updated interface compliance
//
// Priority: P0 (core observation infrastructure)
// Test Level: Unit (protocol, callbackMux) + Integration (IPC server+client roundtrip)

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// ---------------------------------------------------------------------------
// AC-2: Protocol wire types — MethodWatch constant
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC2_MethodWatch_Constant(t *testing.T) {
	if MethodWatch != "watch" {
		t.Fatalf("AC-2: MethodWatch = %q, want %q", MethodWatch, "watch")
	}
}

func TestATDD_27_3_AC2_WatchRequest_Serialization(t *testing.T) {
	req := WatchRequest{PID: types.PID(42)}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC-2: marshal WatchRequest: %v", err)
	}
	var decoded WatchRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-2: unmarshal WatchRequest: %v", err)
	}
	if decoded.PID != 42 {
		t.Errorf("AC-2: WatchRequest roundtrip PID = %d, want 42", decoded.PID)
	}

	// Verify JSON tag is "pid"
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("AC-2: unmarshal raw: %v", err)
	}
	if _, ok := raw["pid"]; !ok {
		t.Error("AC-2: WatchRequest JSON missing 'pid' key — check json tag")
	}
}

// ---------------------------------------------------------------------------
// AC-3: ProgressPayload extension — HasError + DurationMs fields
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC3_ProgressPayload_HasError_Serialization(t *testing.T) {
	pp := ProgressPayload{
		Event:    "step_complete",
		PID:      1,
		Step:     3,
		Action:   "tool_call",
		Summary:  "/dev/fs → read config.yaml",
		HasError: true,
	}
	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-3: marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-3: unmarshal: %v", err)
	}
	if decoded["has_error"] != true {
		t.Errorf("AC-3: has_error = %v, want true", decoded["has_error"])
	}
}

func TestATDD_27_3_AC3_ProgressPayload_DurationMs_Serialization(t *testing.T) {
	pp := ProgressPayload{
		Event:      "step_complete",
		PID:        1,
		Step:       2,
		Action:     "tool_call",
		DurationMs: 42.5,
	}
	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-3: marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-3: unmarshal: %v", err)
	}
	if decoded["duration_ms"] != 42.5 {
		t.Errorf("AC-3: duration_ms = %v, want 42.5", decoded["duration_ms"])
	}
}

func TestATDD_27_3_AC3_ProgressPayload_NewFields_OmitEmpty(t *testing.T) {
	pp := ProgressPayload{
		Event:  "step_complete",
		PID:    1,
		Step:   4,
		Action: "complete",
		// HasError = false (zero), DurationMs = 0.0 (zero) → should be omitted
	}
	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-3: marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-3: unmarshal: %v", err)
	}
	if _, ok := decoded["has_error"]; ok {
		t.Error("AC-3: has_error should be omitted when false (omitempty)")
	}
	if _, ok := decoded["duration_ms"]; ok {
		t.Error("AC-3: duration_ms should be omitted when zero (omitempty)")
	}
}

// ---------------------------------------------------------------------------
// AC-5: callbackMux multi-subscriber — all receive events
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC5_CallbackMux_MultiSubscriber_AllReceive(t *testing.T) {
	mux := newCallbackMux()
	pid := types.PID(1)

	ch1 := make(chan StreamEvent, 10)
	ch2 := make(chan StreamEvent, 10)
	ch3 := make(chan StreamEvent, 10)

	mux.register(pid, ch1)
	mux.register(pid, ch2)
	mux.register(pid, ch3)

	// Send an event — all 3 subscribers should receive
	pp := ProgressPayload{Event: "step_complete", PID: pid, Step: 1, Action: "tool_call"}
	payload, _ := json.Marshal(pp)
	mux.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})

	for i, ch := range []chan StreamEvent{ch1, ch2, ch3} {
		select {
		case ev := <-ch:
			if ev.Type != StreamProgress {
				t.Errorf("AC-5: subscriber %d got type %v, want StreamProgress", i, ev.Type)
			}
		default:
			t.Errorf("AC-5: subscriber %d received no event", i)
		}
	}
}

// ---------------------------------------------------------------------------
// AC-5: callbackMux unregister one subscriber — others unaffected
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC5_CallbackMux_UnregisterOne_OthersUnaffected(t *testing.T) {
	mux := newCallbackMux()
	pid := types.PID(1)

	ch1 := make(chan StreamEvent, 10)
	ch2 := make(chan StreamEvent, 10)

	mux.register(pid, ch1)
	mux.register(pid, ch2)

	// Unregister ch1 only (new signature: unregister(pid, ch))
	mux.unregister(pid, ch1)

	pp := ProgressPayload{Event: "step_complete", PID: pid, Step: 2}
	payload, _ := json.Marshal(pp)
	mux.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})

	// ch1 should NOT receive
	select {
	case <-ch1:
		t.Error("AC-5: unregistered ch1 should not receive events")
	default:
	}

	// ch2 should still receive
	select {
	case ev := <-ch2:
		if ev.Type != StreamProgress {
			t.Errorf("AC-5: ch2 got type %v, want StreamProgress", ev.Type)
		}
	default:
		t.Error("AC-5: ch2 should still receive events after ch1 unregistered")
	}
}

// ---------------------------------------------------------------------------
// AC-5: callbackMux unregister all — PID entry cleaned up
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC5_CallbackMux_UnregisterAll_Cleanup(t *testing.T) {
	mux := newCallbackMux()
	pid := types.PID(1)

	ch1 := make(chan StreamEvent, 10)
	ch2 := make(chan StreamEvent, 10)

	mux.register(pid, ch1)
	mux.register(pid, ch2)
	mux.unregister(pid, ch1)
	mux.unregister(pid, ch2)

	// After removing all subscribers, send should be a no-op (no panic)
	pp := ProgressPayload{Event: "step", PID: pid, Step: 1}
	payload, _ := json.Marshal(pp)
	mux.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})

	// Verify no lingering state by re-registering
	ch3 := make(chan StreamEvent, 10)
	mux.register(pid, ch3)

	mux.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
	select {
	case <-ch3:
		// expected
	default:
		t.Error("AC-5: re-registered subscriber should receive events")
	}
}

// ---------------------------------------------------------------------------
// AC-11: callbackMux OnStepComplete fills DurationMs and HasError
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC11_OnStepComplete_FillsDurationMs(t *testing.T) {
	mux := newCallbackMux()
	pid := types.PID(1)

	ch := make(chan StreamEvent, 10)
	mux.register(pid, ch)

	// New 6-arg signature: (pid, step, action, summary, duration, hasError)
	mux.OnStepComplete(pid, 3, "tool_call", "/dev/fs → config.yaml", 250*time.Millisecond, false)

	select {
	case ev := <-ch:
		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			t.Fatalf("AC-11: unmarshal: %v", err)
		}
		if pp.DurationMs < 249.0 || pp.DurationMs > 251.0 {
			t.Errorf("AC-11: DurationMs = %f, want ~250.0", pp.DurationMs)
		}
		if pp.HasError {
			t.Error("AC-11: HasError should be false")
		}
	default:
		t.Fatal("AC-11: no event sent by OnStepComplete")
	}
}

func TestATDD_27_3_AC11_OnStepComplete_FillsHasError(t *testing.T) {
	mux := newCallbackMux()
	pid := types.PID(1)

	ch := make(chan StreamEvent, 10)
	mux.register(pid, ch)

	mux.OnStepComplete(pid, 5, "tool_call", "/dev/shell → exit 1", 1200*time.Millisecond, true)

	select {
	case ev := <-ch:
		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			t.Fatalf("AC-11: unmarshal: %v", err)
		}
		if !pp.HasError {
			t.Error("AC-11: HasError should be true for error step")
		}
		if pp.DurationMs < 1199.0 || pp.DurationMs > 1201.0 {
			t.Errorf("AC-11: DurationMs = %f, want ~1200.0", pp.DurationMs)
		}
	default:
		t.Fatal("AC-11: no event sent by OnStepComplete")
	}
}

// ---------------------------------------------------------------------------
// AC-4: KernelCallbacks interface — updated OnStepComplete signature
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC4_CallbackMux_ImplementsUpdatedKernelCallbacks(t *testing.T) {
	mux := newCallbackMux()
	// Compile-time check: callbackMux must satisfy KernelCallbacks
	// with the new 6-arg OnStepComplete(pid, step, action, summary, duration, hasError)
	var _ kernel.KernelCallbacks = mux
}

// ---------------------------------------------------------------------------
// AC-10: handleWatch — PID not found
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC10_HandleWatch_PIDNotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodWatch, WatchRequest{PID: types.PID(99999)})

	if resp.OK {
		t.Fatal("AC-10: expected error response for non-existent PID")
	}
	if resp.Error == nil {
		t.Fatal("AC-10: expected error payload")
	}
	if resp.Error.Code != "not_found" {
		t.Errorf("AC-10: error code = %q, want %q", resp.Error.Code, "not_found")
	}
}

// ---------------------------------------------------------------------------
// AC-6: handleWatch — stream events for running process
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC6_HandleWatch_StreamEvents(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "watch test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// Write initial steps to disk for replay
	writeTestSteps(t, tmpDir, proc.PID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})

	// Connect and send watch request
	conn := dial(t, sockPath)
	rawPayload, _ := json.Marshal(WatchRequest{PID: proc.PID})
	req := Request{Method: MethodWatch, Payload: rawPayload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("AC-6: encode request: %v", err)
	}

	scanner := bufio.NewScanner(conn)

	// First line should be OK response
	if !scanner.Scan() {
		t.Fatal("AC-6: no initial response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("AC-6: unmarshal response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("AC-6: watch request failed: %+v", resp.Error)
	}

	// Should receive replayed history events (steps 1 and 2)
	replayed := 0
	for replayed < 2 {
		if !scanner.Scan() {
			t.Fatalf("AC-6: expected replayed event %d, got EOF", replayed+1)
		}
		var ev StreamEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("AC-6: unmarshal event: %v", err)
		}
		if ev.Type != StreamProgress {
			t.Fatalf("AC-6: expected StreamProgress, got %v", ev.Type)
		}
		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			t.Fatalf("AC-6: unmarshal payload: %v", err)
		}
		if pp.Event != "step_complete" {
			t.Errorf("AC-6: replayed event %d: event = %q, want step_complete", replayed+1, pp.Event)
		}
		if pp.Step != replayed+1 {
			t.Errorf("AC-6: replayed event %d: step = %d, want %d", replayed+1, pp.Step, replayed+1)
		}
		replayed++
	}

	// Now trigger a live event via the callbackMux
	srv.CallbackMux().OnStepComplete(proc.PID, 3, "tool_call", "live event", 100*time.Millisecond, false)

	// Should receive the live event
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if !scanner.Scan() {
		t.Fatal("AC-6: no live event received")
	}
	var liveEv StreamEvent
	if err := json.Unmarshal(scanner.Bytes(), &liveEv); err != nil {
		t.Fatalf("AC-6: unmarshal live event: %v", err)
	}
	var livePP ProgressPayload
	if err := json.Unmarshal(liveEv.Payload, &livePP); err != nil {
		t.Fatalf("AC-6: unmarshal live payload: %v", err)
	}
	if livePP.Step != 3 || livePP.Action != "tool_call" || livePP.Summary != "live event" {
		t.Errorf("AC-6: live event mismatch: step=%d action=%q summary=%q", livePP.Step, livePP.Action, livePP.Summary)
	}
}

// ---------------------------------------------------------------------------
// AC-6: handleWatch — history replay from steps.jsonl
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC6_HandleWatch_HistoryReplay(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "replay test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// Write 5 historical steps
	steps := make([]types.StepRecord, 5)
	for i := range steps {
		steps[i] = testStepRecord(i + 1)
	}
	writeTestSteps(t, tmpDir, proc.PID, steps)

	conn := dial(t, sockPath)
	rawPayload, _ := json.Marshal(WatchRequest{PID: proc.PID})
	req := Request{Method: MethodWatch, Payload: rawPayload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("AC-6: encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)

	// Read OK response
	if !scanner.Scan() {
		t.Fatal("AC-6: no response")
	}
	var resp Response
	json.Unmarshal(scanner.Bytes(), &resp)
	if !resp.OK {
		t.Fatalf("AC-6: request failed: %+v", resp.Error)
	}

	// Read 5 replayed events
	for i := 1; i <= 5; i++ {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if !scanner.Scan() {
			t.Fatalf("AC-6: expected replay event %d, got EOF", i)
		}
		var ev StreamEvent
		json.Unmarshal(scanner.Bytes(), &ev)
		var pp ProgressPayload
		json.Unmarshal(ev.Payload, &pp)
		if pp.Step != i {
			t.Errorf("AC-6: replay event %d: step = %d", i, pp.Step)
		}
		if pp.Event != "step_complete" {
			t.Errorf("AC-6: replay event %d: event = %q, want step_complete", i, pp.Event)
		}
		// Verify DurationMs is populated from StepRecord timestamps
		if pp.DurationMs <= 0 {
			t.Errorf("AC-6: replay event %d: DurationMs = %f, want > 0", i, pp.DurationMs)
		}
	}
}

// ---------------------------------------------------------------------------
// AC-6: Client WatchProcess roundtrip
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC6_Client_WatchProcess_Roundtrip(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client watch", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)
	writeTestSteps(t, tmpDir, proc.PID, []types.StepRecord{
		testStepRecord(1),
	})

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-6: dial: %v", err)
	}
	defer client.Close()

	var events []ProgressPayload
	done := make(chan struct{})

	go func() {
		defer close(done)
		client.WatchProcess(proc.PID, func(ev StreamEvent) {
			var pp ProgressPayload
			if err := json.Unmarshal(ev.Payload, &pp); err == nil {
				events = append(events, pp)
			}
		})
	}()

	// Give the watch time to connect and receive replay
	time.Sleep(200 * time.Millisecond)

	// Send a live event then complete
	srv.CallbackMux().OnStepComplete(proc.PID, 2, "plan", "created plan", 500*time.Millisecond, false)

	// Trigger process completion via callbackMux.OnComplete (mirrors kernel behavior)
	time.Sleep(100 * time.Millisecond)
	srv.CallbackMux().OnComplete(proc.PID, "done", kernel.ExitStatus{Code: 0, Reason: "completed"})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AC-6: WatchProcess did not complete within timeout")
	}

	if len(events) < 2 {
		t.Fatalf("AC-6: got %d events, want >= 2 (replay + live)", len(events))
	}

	// First event should be the replayed step 1
	if events[0].Step != 1 || events[0].Event != "step_complete" {
		t.Errorf("AC-6: first event: step=%d event=%q, want step=1 event=step_complete", events[0].Step, events[0].Event)
	}
}

// ---------------------------------------------------------------------------
// AC-5 + AC-6: Multiple watchers on same PID both receive events
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC5_MultipleWatchers_SamePID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "multi watch", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// No history steps — test live events only
	stepsDir := filepath.Join(tmpDir, "data", "steps", fmt.Sprintf("%d", proc.PID))
	os.MkdirAll(stepsDir, 0o755)
	os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), []byte{}, 0o644)

	// Connect two watchers
	conn1 := dial(t, sockPath)
	conn2 := dial(t, sockPath)

	sendWatchRequest := func(conn *bufio.Scanner, connW *json.Encoder) {
		t.Helper()
		rawPayload, _ := json.Marshal(WatchRequest{PID: proc.PID})
		connW.Encode(Request{Method: MethodWatch, Payload: rawPayload})
	}

	enc1 := json.NewEncoder(conn1)
	enc2 := json.NewEncoder(conn2)
	scan1 := bufio.NewScanner(conn1)
	scan2 := bufio.NewScanner(conn2)

	rawPayload, _ := json.Marshal(WatchRequest{PID: proc.PID})
	enc1.Encode(Request{Method: MethodWatch, Payload: rawPayload})
	enc2.Encode(Request{Method: MethodWatch, Payload: rawPayload})

	// Both should get OK responses
	for i, s := range []*bufio.Scanner{scan1, scan2} {
		if !s.Scan() {
			t.Fatalf("AC-5: watcher %d: no response", i)
		}
		var resp Response
		json.Unmarshal(s.Bytes(), &resp)
		if !resp.OK {
			t.Fatalf("AC-5: watcher %d: request failed: %+v", i, resp.Error)
		}
	}

	// Give the handlers time to register
	time.Sleep(100 * time.Millisecond)

	// Trigger a live event
	srv.CallbackMux().OnStepComplete(proc.PID, 1, "tool_call", "multi event", 300*time.Millisecond, false)

	// Both watchers should receive the event
	for i, s := range []*bufio.Scanner{scan1, scan2} {
		conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
		if !s.Scan() {
			t.Fatalf("AC-5: watcher %d: no live event", i)
		}
		var ev StreamEvent
		json.Unmarshal(s.Bytes(), &ev)
		var pp ProgressPayload
		json.Unmarshal(ev.Payload, &pp)
		if pp.Step != 1 || pp.Summary != "multi event" {
			t.Errorf("AC-5: watcher %d: step=%d summary=%q", i, pp.Step, pp.Summary)
		}
	}

	// Suppress unused variable warnings
	_ = sendWatchRequest
}

// ---------------------------------------------------------------------------
// AC-3 + AC-11: ProgressPayload HasError + DurationMs in step_complete context
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC11_ProgressPayload_FullStepComplete(t *testing.T) {
	pp := ProgressPayload{
		Event:      "step_complete",
		PID:        42,
		Step:       7,
		Action:     "tool_call",
		Summary:    "/dev/shell → exit 1",
		HasError:   true,
		DurationMs: 1500.5,
	}
	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-11: marshal: %v", err)
	}

	var decoded ProgressPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-11: unmarshal: %v", err)
	}

	if decoded.HasError != true {
		t.Error("AC-11: HasError roundtrip failed")
	}
	if decoded.DurationMs != 1500.5 {
		t.Errorf("AC-11: DurationMs = %f, want 1500.5", decoded.DurationMs)
	}
	if decoded.Action != "tool_call" {
		t.Errorf("AC-11: Action = %q, want tool_call", decoded.Action)
	}
	if decoded.Summary != "/dev/shell → exit 1" {
		t.Errorf("AC-11: Summary = %q", decoded.Summary)
	}
}
