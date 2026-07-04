package llm

import (
	"context"
	"testing"
)

// Story 62.2 AC5: cursor/qwen message-level content events must carry the
// subtype marker consumed by kernel/observe.go to persist DriverMessage events.
func TestATDD_62_2_AC5_CursorContentEventsCarryAgentMessageSubtype(t *testing.T) {
	t.Parallel()

	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_stream_success")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	assertAgentMessageSubtypeForContent(t, ch, "hello ", "world")
}

// Story 62.2 AC5: qwen follows the same message-level content contract as
// codex/cursor so token-level deltas can remain unmarked and non-persisted.
func TestATDD_62_2_AC5_QwenContentEventsCarryAgentMessageSubtype(t *testing.T) {
	t.Parallel()

	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_success")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	assertAgentMessageSubtypeForContent(t, ch, "hello ", "world")
}

func assertAgentMessageSubtypeForContent(t *testing.T, events <-chan StreamEvent, expectedContent ...string) {
	t.Helper()

	seen := make(map[string]bool, len(expectedContent))
	for _, content := range expectedContent {
		seen[content] = false
	}

	for evt := range events {
		if evt.Type != "content" {
			continue
		}
		if _, ok := seen[evt.Content]; !ok {
			continue
		}
		if got := evt.Data["subtype"]; got != "agent_message" {
			t.Errorf("content %q subtype = %v, want agent_message", evt.Content, got)
		}
		seen[evt.Content] = true
	}

	for content, ok := range seen {
		if !ok {
			t.Errorf("missing content event %q", content)
		}
	}
}
