package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	twoStepScript   = "testdata/replay/two-step.responses.yaml"
	reasoningScript = "testdata/replay/reasoning.responses.yaml"
)

// ---------------------------------------------------------------------------
// Sequential replay + model/default_model two-tier resolution (AC 2, 3)
// ---------------------------------------------------------------------------

func TestReplayDriver_SequentialReplay(t *testing.T) {
	d := NewReplayDriver("replay-test")
	req := LLMRequest{Model: twoStepScript, CallerUUID: "proc-1"}

	resp0, err := d.Call(t.Context(), req)
	if err != nil {
		t.Fatalf("response #1: unexpected error: %v", err)
	}
	if len(resp0.ToolCalls) != 1 || resp0.ToolCalls[0].Name != "Bash" {
		t.Fatalf("response #1: ToolCalls = %+v, want one Bash call", resp0.ToolCalls)
	}
	if got, want := resp0.ToolCalls[0].ID, "replay-s1-c1"; got != want {
		t.Errorf("response #1: tool_call id = %q, want deterministic %q", got, want)
	}
	if cmd, _ := resp0.ToolCalls[0].Input["command"].(string); cmd != "echo hi" {
		t.Errorf("response #1: input.command = %q, want %q", cmd, "echo hi")
	}
	// No usage declared on response #1 — must default to the deterministic zero.
	if resp0.TokensUsed != 0 || resp0.InputTokens != 0 || resp0.OutputTokens != 0 {
		t.Errorf("response #1: usage = %+v, want all zero (undeclared)", resp0)
	}

	resp1, err := d.Call(t.Context(), req)
	if err != nil {
		t.Fatalf("response #2: unexpected error: %v", err)
	}
	if resp1.Content != "all done" {
		t.Errorf("response #2: Content = %q, want %q", resp1.Content, "all done")
	}
	if len(resp1.ToolCalls) != 1 || resp1.ToolCalls[0].Name != "Complete" {
		t.Fatalf("response #2: ToolCalls = %+v, want one Complete call", resp1.ToolCalls)
	}
	if got, want := resp1.ToolCalls[0].ID, "replay-s2-c1"; got != want {
		t.Errorf("response #2: tool_call id = %q, want deterministic %q", got, want)
	}
	if resp1.StopReason != "tool_use" {
		t.Errorf("response #2: StopReason = %q, want %q", resp1.StopReason, "tool_use")
	}
	if resp1.InputTokens != 12 || resp1.OutputTokens != 8 || resp1.TokensUsed != 20 {
		t.Errorf("response #2: usage = {in:%d out:%d total:%d}, want {12 8 20}",
			resp1.InputTokens, resp1.OutputTokens, resp1.TokensUsed)
	}
}

func TestReplayDriver_PerSpawnModel_OverridesDefault(t *testing.T) {
	// Default script points at a single reasoning-only response; the
	// per-spawn req.Model should win and serve the two-step script instead
	// (裁决 2: req.Model non-empty always wins over the instance default).
	d := NewReplayDriver("replay-test", WithReplayDefaultScript(reasoningScript))
	req := LLMRequest{Model: twoStepScript, CallerUUID: "proc-override"}

	resp, err := d.Call(t.Context(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "Bash" {
		t.Errorf("got %+v, want the two-step script's first (Bash) response — "+
			"per-spawn model did not override default_model", resp)
	}
}

func TestReplayDriver_DefaultModel_UsedWhenRequestModelEmpty(t *testing.T) {
	d := NewReplayDriver("replay-test", WithReplayDefaultScript(twoStepScript))
	req := LLMRequest{CallerUUID: "proc-default"} // Model left empty

	resp, err := d.Call(t.Context(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "Bash" {
		t.Errorf("got %+v, want the default script's first (Bash) response", resp)
	}
}

func TestReplayDriver_NoModelNoDefault_Errors(t *testing.T) {
	d := NewReplayDriver("replay-test") // no default script configured
	req := LLMRequest{CallerUUID: "proc-empty"}

	_, err := d.Call(t.Context(), req)
	if err == nil {
		t.Fatal("expected an error when both req.Model and default_model are empty")
	}
	if IsTransient(err) {
		t.Errorf("no-script-path error must not be transient: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exhaustion — fail-loud, non-transient, never silently restarts (裁决 3)
// ---------------------------------------------------------------------------

func TestReplayDriver_ScriptExhausted_FailsLoudAndStaysExhausted(t *testing.T) {
	d := NewReplayDriver("replay-test")
	req := LLMRequest{Model: twoStepScript, CallerUUID: "proc-exhaust"}

	if _, err := d.Call(t.Context(), req); err != nil {
		t.Fatalf("response #1: unexpected error: %v", err)
	}
	if _, err := d.Call(t.Context(), req); err != nil {
		t.Fatalf("response #2: unexpected error: %v", err)
	}

	// Script only has 2 responses — the 3rd call must fail loud, not repeat
	// response #2 or silently restart from response #1.
	_, err := d.Call(t.Context(), req)
	if err == nil {
		t.Fatal("call #3: expected exhaustion error, got nil")
	}
	if IsTransient(err) {
		t.Errorf("exhaustion error must not be transient (would trigger pointless kernel step_retry): %v", err)
	}
	if !strings.Contains(err.Error(), "exhausted") || !strings.Contains(err.Error(), twoStepScript) {
		t.Errorf("exhaustion error = %q, want it to mention \"exhausted\" and the script path", err.Error())
	}

	// A 4th call must keep failing — never resurrect/restart from response #1
	// (would happen if the session were deleted from the map on exhaustion).
	resp4, err4 := d.Call(t.Context(), req)
	if err4 == nil {
		t.Fatalf("call #4: expected exhaustion error to persist, got response %+v", resp4)
	}
	if IsTransient(err4) {
		t.Errorf("call #4 exhaustion error must not be transient: %v", err4)
	}
}

func TestReplayDriver_ConcurrentDefaultKey_ExactlyOnceDelivery(t *testing.T) {
	d := NewReplayDriver("replay-test")
	const n = 8 // > len(responses)=2, so most callers race into exhaustion

	var wg sync.WaitGroup
	var successCount, errCount atomic.Int64
	var seenBash, seenComplete atomic.Bool

	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			// CallerUUID intentionally empty on every goroutine: all of them
			// race on the shared "default" fallback key (裁决 4 defensive lock).
			resp, err := d.Call(t.Context(), LLMRequest{Model: twoStepScript})
			if err != nil {
				if IsTransient(err) {
					t.Errorf("exhaustion error must not be transient: %v", err)
				}
				errCount.Add(1)
				return
			}
			successCount.Add(1)
			switch {
			case len(resp.ToolCalls) == 1 && resp.ToolCalls[0].Name == "Bash":
				if seenBash.Swap(true) {
					t.Error("Bash response delivered more than once — exactly-once violated")
				}
			case len(resp.ToolCalls) == 1 && resp.ToolCalls[0].Name == "Complete":
				if seenComplete.Swap(true) {
					t.Error("Complete response delivered more than once — exactly-once violated")
				}
			default:
				t.Errorf("unexpected response: %+v", resp)
			}
		}()
	}
	wg.Wait()

	if got := successCount.Load(); got != 2 {
		t.Errorf("successCount = %d, want 2 (script has exactly 2 responses)", got)
	}
	if got, want := errCount.Load(), int64(n-2); got != want {
		t.Errorf("errCount = %d, want %d", got, want)
	}
}

// ---------------------------------------------------------------------------
// Concurrency isolation across distinct CallerUUIDs (裁决 4, -race)
// ---------------------------------------------------------------------------

func TestReplayDriver_ConcurrentUUIDs_NoCrossTalk(t *testing.T) {
	d := NewReplayDriver("replay-test")

	run := func(uuid string) error {
		req := LLMRequest{Model: twoStepScript, CallerUUID: uuid}
		resp0, err := d.Call(t.Context(), req)
		if err != nil {
			return err
		}
		if len(resp0.ToolCalls) != 1 || resp0.ToolCalls[0].Name != "Bash" {
			t.Errorf("uuid=%s call#1: ToolCalls = %+v, want Bash", uuid, resp0.ToolCalls)
		}
		resp1, err := d.Call(t.Context(), req)
		if err != nil {
			return err
		}
		if len(resp1.ToolCalls) != 1 || resp1.ToolCalls[0].Name != "Complete" {
			t.Errorf("uuid=%s call#2: ToolCalls = %+v, want Complete", uuid, resp1.ToolCalls)
		}
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for _, uuid := range []string{"uuid-a", "uuid-b"} {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			if err := run(u); err != nil {
				errCh <- err
			}
		}(uuid)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Stream event ordering: reasoning → content → done (no Content on done)
// ---------------------------------------------------------------------------

func TestReplayDriver_Stream_EventOrder(t *testing.T) {
	d := NewReplayDriver("replay-test")
	req := LLMRequest{Model: reasoningScript, CallerUUID: "proc-stream"}

	ch, err := d.Stream(t.Context(), req)
	if err != nil {
		t.Fatalf("Stream: unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (reasoning, content, done): %+v", len(events), events)
	}
	if events[0].Type != "reasoning" || events[0].Content != "thinking about the problem" {
		t.Errorf("event[0] = %+v, want reasoning event", events[0])
	}
	if events[1].Type != "content" || events[1].Content != "here is the answer" {
		t.Errorf("event[1] = %+v, want content event", events[1])
	}
	done := events[2]
	if done.Type != "done" {
		t.Fatalf("event[2].Type = %q, want done", done.Type)
	}
	if done.Content != "" {
		t.Errorf("done.Content = %q, want empty (vfsfile.go Resets content.Builder on non-empty done.Content)", done.Content)
	}
	if done.StopReason != "end_turn" {
		t.Errorf("done.StopReason = %q, want %q", done.StopReason, "end_turn")
	}
	if done.TokensUsed != 0 {
		t.Errorf("done.TokensUsed = %d, want 0 (undeclared usage)", done.TokensUsed)
	}
}

// ---------------------------------------------------------------------------
// Deterministic tool_call id generation (explicit id preserved, missing id filled)
// ---------------------------------------------------------------------------

func TestLoadReplayScript_ToolCallID_ExplicitPreservedMissingGenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ids.responses.yaml")
	content := `version: "1"
responses:
  - tool_calls:
      - id: "custom-id-1"
        name: Bash
        input:
          command: "ls"
      - name: Read
        input:
          path: "/tmp/x"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	script, err := loadReplayScript(path)
	if err != nil {
		t.Fatalf("loadReplayScript: %v", err)
	}
	calls := script.Responses[0].ToolCalls
	if len(calls) != 2 {
		t.Fatalf("got %d tool_calls, want 2", len(calls))
	}
	if calls[0].ID != "custom-id-1" {
		t.Errorf("calls[0].ID = %q, want explicit %q preserved", calls[0].ID, "custom-id-1")
	}
	if calls[1].ID != "replay-s1-c2" {
		t.Errorf("calls[1].ID = %q, want deterministic %q", calls[1].ID, "replay-s1-c2")
	}
}

// ---------------------------------------------------------------------------
// Script validation failures (load-time fail-fast; error names path + entry)
// ---------------------------------------------------------------------------

func TestLoadReplayScript_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantSub []string
	}{
		{
			name:    "empty responses list",
			content: "version: \"1\"\nresponses: []\n",
			wantSub: []string{"responses list is empty"},
		},
		{
			name: "entry with neither content nor tool_calls",
			content: `version: "1"
responses:
  - stop_reason: "end_turn"
`,
			wantSub: []string{"entry #1", "content and/or tool_calls"},
		},
		{
			name: "tool_call with empty name",
			content: `version: "1"
responses:
  - tool_calls:
      - input:
          command: "ls"
`,
			wantSub: []string{"entry #1", "tool_call #1", "name is required"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.responses.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			_, err := loadReplayScript(path)
			if err == nil {
				t.Fatal("expected a validation error, got nil")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error = %q, want it to mention the script path %q", err.Error(), path)
			}
			for _, sub := range tt.wantSub {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), sub)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Raw capture (Epic 56 9/9) — Kind="replay", Request/Response field shape
// ---------------------------------------------------------------------------

func TestReplayDriver_RawCapture_Success(t *testing.T) {
	d := NewReplayDriver("replay-test")
	sink := &rawCaptureSink{}
	ctx := withRawSink(t.Context(), sink)
	req := LLMRequest{Model: twoStepScript, CallerUUID: "proc-raw", Intent: "test intent"}

	if _, err := d.Call(ctx, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sink.get()
	if got == nil {
		t.Fatal("sink.get() = nil, want a populated RawCapture")
	}
	if got.Kind != "replay" {
		t.Errorf("Kind = %q, want %q", got.Kind, "replay")
	}
	if got.Request["script_path"] != twoStepScript {
		t.Errorf("Request[script_path] = %v, want %q", got.Request["script_path"], twoStepScript)
	}
	if idx, _ := got.Request["response_index"].(int); idx != 0 {
		t.Errorf("Request[response_index] = %v, want 0", got.Request["response_index"])
	}
	if got.Request["intent"] != "test intent" {
		t.Errorf("Request[intent] = %v, want %q", got.Request["intent"], "test intent")
	}
	body, _ := got.Response["body"].(string)
	if body == "" {
		t.Fatal("Response[body] is empty, want the JSON-serialized LLMResponse")
	}
	var decoded LLMResponse
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Response[body] does not round-trip as LLMResponse JSON: %v", err)
	}
	if len(decoded.ToolCalls) != 1 || decoded.ToolCalls[0].Name != "Bash" {
		t.Errorf("decoded Response body ToolCalls = %+v, want one Bash call", decoded.ToolCalls)
	}
}

func TestReplayDriver_RawCapture_ExhaustionKeepsRequestNoResponse(t *testing.T) {
	d := NewReplayDriver("replay-test")
	req := LLMRequest{Model: twoStepScript, CallerUUID: "proc-raw-exhaust"}

	// Drain the 2-response script (no sink needed for these two).
	if _, err := d.Call(t.Context(), req); err != nil {
		t.Fatalf("response #1: %v", err)
	}
	if _, err := d.Call(t.Context(), req); err != nil {
		t.Fatalf("response #2: %v", err)
	}

	sink := &rawCaptureSink{}
	ctx := withRawSink(t.Context(), sink)
	if _, err := d.Call(ctx, req); err == nil {
		t.Fatal("expected exhaustion error on call #3")
	}

	got := sink.get()
	if got == nil {
		t.Fatal("sink.get() = nil, want Request form preserved even on error (writeCall parity)")
	}
	if got.Kind != "replay" {
		t.Errorf("Kind = %q, want %q", got.Kind, "replay")
	}
	if got.Request["script_path"] != twoStepScript {
		t.Errorf("Request[script_path] = %v, want %q", got.Request["script_path"], twoStepScript)
	}
	if got.Response != nil {
		t.Errorf("Response = %+v, want nil on an errored call", got.Response)
	}
}

// ---------------------------------------------------------------------------
// Factory + config wiring (AC 1, 6)
// ---------------------------------------------------------------------------

func TestCreateDriver_Replay(t *testing.T) {
	drv, err := CreateDriver(ProviderConfig{
		Name:         "r1",
		Driver:       DriverReplay,
		DefaultModel: "/abs/case-01.responses.yaml",
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	rd, ok := drv.(*ReplayDriver)
	if !ok {
		t.Fatalf("CreateDriver returned %T, want *ReplayDriver", drv)
	}
	info := rd.Info()
	if info.Name != "r1" || info.Provider != "r1" {
		t.Errorf("Info() Name/Provider = %q/%q, want %q/%q", info.Name, info.Provider, "r1", "r1")
	}
	if info.DriverType != DriverReplay {
		t.Errorf("Info().DriverType = %q, want %q", info.DriverType, DriverReplay)
	}
	if info.DefaultModel != "/abs/case-01.responses.yaml" {
		t.Errorf("Info().DefaultModel = %q, want the configured default script path", info.DefaultModel)
	}
	// Compile-time-shaped runtime check: replay must support native tool
	// calling or the kernel will never assemble ToolDef/toolMap for it
	// (observe.go SupportsToolCalling gate) and meta actions (Complete) could
	// never be replayed.
	if _, ok := drv.(ToolCallingDriver); !ok {
		t.Fatal("CreateDriver(replay) must return a ToolCallingDriver")
	}
}

func TestCreateDriver_Replay_ReasoningEffortIsNoOp(t *testing.T) {
	// replay has no reasoning-effort concept — factory logs a warning and
	// still returns a working driver (mirrors the cursor-cli/qwen-cli path).
	drv, err := CreateDriver(ProviderConfig{
		Name:            "r1",
		Driver:          DriverReplay,
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("CreateDriver with reasoning_effort set: unexpected error: %v", err)
	}
	if _, ok := drv.(*ReplayDriver); !ok {
		t.Fatalf("CreateDriver returned %T, want *ReplayDriver", drv)
	}
}

func TestProvidersConfig_Validate_AcceptsReplayDriver(t *testing.T) {
	cfg := ProvidersConfig{
		Version:   "1",
		Providers: []ProviderConfig{{Name: "r1", Driver: DriverReplay}},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with driver=replay: unexpected error: %v", err)
	}
}
