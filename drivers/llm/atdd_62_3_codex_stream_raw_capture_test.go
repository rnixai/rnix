package llm

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Story 62.3 — VFS/raw-capture boundary guards. Direct Stream tests prove the
// codex driver emits the right terminal errors; these tests prove the user-facing
// LLMFile.Write path still drains the stream, returns that error, and preserves
// the terminal raw capture for diagnostics.
func TestATDD_62_3_AC2_TruncatedAfterAgentMessage_RawCapturePreserved(t *testing.T) {
	t.Parallel()

	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_truncated")))
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()

	err := f.Write(t.Context(), []byte(`{"intent":"test truncated codex stream"}`))
	if err == nil {
		t.Fatal("expected truncated stream error")
	}
	if !strings.Contains(err.Error(), "stream ended before turn.completed") {
		t.Fatalf("expected 62.3 truncation detail, got: %v", err)
	}

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil on codex truncated stream")
	}
	if cap.Response == nil {
		t.Fatalf("raw capture Response is nil, want terminal capture: %+v", cap)
	}
	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"thread_id":"tid-truncated"`) {
		t.Errorf("raw stdout missing truncated stream events: %q", stdout)
	}
	if !strings.Contains(stdout, "Waiting for worker to finish.") {
		t.Errorf("raw stdout missing last agent_message before truncation: %q", stdout)
	}
	if exit, ok := cap.Response["exit_code"].(int); !ok || exit != 0 {
		t.Errorf("exit_code = %v (%T), want clean EOF exit 0 recorded", cap.Response["exit_code"], cap.Response["exit_code"])
	}
}

func TestATDD_62_3_AC1_IdleTimeoutAfterAgentMessage_RawCapturePreserved(t *testing.T) {
	t.Parallel()

	d := NewCodexCliDriver(
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_idle_timeout")),
		CodexWithTimeout(100*time.Millisecond),
	)
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()

	err := f.Write(t.Context(), []byte(`{"intent":"test idle timeout codex stream"}`))
	if err == nil {
		t.Fatal("expected timeout stream error")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout sentinel, got: %v", err)
	}

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil on codex idle timeout")
	}
	if cap.Response == nil {
		t.Fatalf("raw capture Response is nil, want terminal capture: %+v", cap)
	}
	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"thread_id":"tid-timeout"`) {
		t.Errorf("raw stdout missing timeout stream events: %q", stdout)
	}
	if !strings.Contains(stdout, "Waiting for worker to finish.") {
		t.Errorf("raw stdout missing last agent_message before idle timeout: %q", stdout)
	}
}
