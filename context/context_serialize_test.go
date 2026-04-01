package context

import (
	"encoding/json"
	"testing"
)

// --- 30.2-UNIT-001: Empty context round-trip ---

func TestContext_Serialize_EmptyRoundTrip(t *testing.T) {
	ctx := &Context{
		ID:       1,
		Messages: make([]Message, 0),
		MaxSize:  100,
	}

	data, err := ctx.Serialize()
	if err != nil {
		t.Fatalf("Serialize() error: %v", err)
	}

	restored := &Context{ID: 2}
	if err := restored.Deserialize(data); err != nil {
		t.Fatalf("Deserialize() error: %v", err)
	}

	if restored.SystemPrompt != "" {
		t.Errorf("SystemPrompt = %q, want empty", restored.SystemPrompt)
	}
	if len(restored.Messages) != 0 {
		t.Errorf("Messages len = %d, want 0", len(restored.Messages))
	}
	if restored.MaxSize != 100 {
		t.Errorf("MaxSize = %d, want 100", restored.MaxSize)
	}
}

// --- 30.2-UNIT-002: Round-trip with all four roles ---

func TestContext_Serialize_AllRolesRoundTrip(t *testing.T) {
	ctx := &Context{
		ID:           1,
		SystemPrompt: "You are an assistant.",
		MaxSize:      100,
		Messages: []Message{
			{Role: RoleSystem, Content: "system init"},
			{Role: RoleUser, Content: "hello"},
			{Role: RoleAssistant, Content: "hi there"},
			{Role: RoleTool, Content: "tool result", ToolCallID: "tc-1"},
		},
	}

	data, err := ctx.Serialize()
	if err != nil {
		t.Fatalf("Serialize() error: %v", err)
	}

	restored := &Context{ID: 2}
	if err := restored.Deserialize(data); err != nil {
		t.Fatalf("Deserialize() error: %v", err)
	}

	if restored.SystemPrompt != "You are an assistant." {
		t.Errorf("SystemPrompt = %q, want %q", restored.SystemPrompt, "You are an assistant.")
	}
	if len(restored.Messages) != 4 {
		t.Fatalf("Messages len = %d, want 4", len(restored.Messages))
	}

	cases := []struct {
		idx        int
		role       Role
		content    string
		toolCallID string
	}{
		{0, RoleSystem, "system init", ""},
		{1, RoleUser, "hello", ""},
		{2, RoleAssistant, "hi there", ""},
		{3, RoleTool, "tool result", "tc-1"},
	}
	for _, c := range cases {
		m := restored.Messages[c.idx]
		if m.Role != c.role {
			t.Errorf("msg[%d].Role = %q, want %q", c.idx, m.Role, c.role)
		}
		if m.Content != c.content {
			t.Errorf("msg[%d].Content = %q, want %q", c.idx, m.Content, c.content)
		}
		if m.ToolCallID != c.toolCallID {
			t.Errorf("msg[%d].ToolCallID = %q, want %q", c.idx, m.ToolCallID, c.toolCallID)
		}
	}
}

// --- 30.2-UNIT-003: Round-trip with ToolCalls ---

func TestContext_Serialize_ToolCallsRoundTrip(t *testing.T) {
	ctx := &Context{
		ID:      1,
		MaxSize: 100,
		Messages: []Message{
			{
				Role:    RoleAssistant,
				Content: "calling tools",
				ToolCalls: []ToolCall{
					{ID: "tc-1", Name: "read_file", Input: map[string]any{"path": "/tmp/foo"}},
					{ID: "tc-2", Name: "write_file", Input: map[string]any{"path": "/tmp/bar", "data": "hello"}},
				},
			},
			{Role: RoleTool, Content: "file contents", ToolCallID: "tc-1"},
			{Role: RoleTool, Content: "ok", ToolCallID: "tc-2"},
		},
	}

	data, err := ctx.Serialize()
	if err != nil {
		t.Fatalf("Serialize() error: %v", err)
	}

	restored := &Context{ID: 2}
	if err := restored.Deserialize(data); err != nil {
		t.Fatalf("Deserialize() error: %v", err)
	}

	if len(restored.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(restored.Messages))
	}

	assistant := restored.Messages[0]
	if len(assistant.ToolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(assistant.ToolCalls))
	}
	if assistant.ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", assistant.ToolCalls[0].Name, "read_file")
	}
	if assistant.ToolCalls[1].Input["data"] != "hello" {
		t.Errorf("ToolCalls[1].Input[data] = %v, want %q", assistant.ToolCalls[1].Input["data"], "hello")
	}
}

// --- 30.2-UNIT-004: Full buffer (maxSize reached) round-trip ---

func TestContext_Serialize_FullBufferRoundTrip(t *testing.T) {
	maxSize := 3
	ctx := &Context{
		ID:      1,
		MaxSize: maxSize,
		Messages: []Message{
			{Role: RoleUser, Content: "msg1"},
			{Role: RoleAssistant, Content: "msg2"},
			{Role: RoleUser, Content: "msg3"},
		},
	}

	data, err := ctx.Serialize()
	if err != nil {
		t.Fatalf("Serialize() error: %v", err)
	}

	restored := &Context{ID: 2}
	if err := restored.Deserialize(data); err != nil {
		t.Fatalf("Deserialize() error: %v", err)
	}

	if restored.MaxSize != maxSize {
		t.Errorf("MaxSize = %d, want %d", restored.MaxSize, maxSize)
	}
	if len(restored.Messages) != maxSize {
		t.Fatalf("Messages len = %d, want %d", len(restored.Messages), maxSize)
	}
	for i, m := range restored.Messages {
		orig := ctx.Messages[i]
		if m.Role != orig.Role || m.Content != orig.Content {
			t.Errorf("msg[%d] = {%s, %q}, want {%s, %q}", i, m.Role, m.Content, orig.Role, orig.Content)
		}
	}
}

// --- 30.2-UNIT-005: Deserialize error input ---

func TestContext_Deserialize_InvalidJSON(t *testing.T) {
	ctx := &Context{ID: 1, MaxSize: 10, Messages: make([]Message, 0)}
	err := ctx.Deserialize([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestContext_Deserialize_EmptyBytes(t *testing.T) {
	ctx := &Context{ID: 1, MaxSize: 10, Messages: make([]Message, 0)}
	err := ctx.Deserialize([]byte{})
	if err == nil {
		t.Fatal("expected error for empty bytes")
	}
}

// --- 30.2-UNIT-006: Byte-level round-trip equivalence ---

func TestContext_Serialize_ByteEquivalence(t *testing.T) {
	ctx := &Context{
		ID:           1,
		SystemPrompt: "test prompt",
		MaxSize:      50,
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
			{Role: RoleAssistant, Content: "world", ToolCalls: []ToolCall{
				{ID: "t1", Name: "fn", Input: map[string]any{"k": "v"}},
			}},
		},
	}

	data1, err := ctx.Serialize()
	if err != nil {
		t.Fatalf("first Serialize() error: %v", err)
	}

	restored := &Context{ID: 2}
	if err := restored.Deserialize(data1); err != nil {
		t.Fatalf("Deserialize() error: %v", err)
	}

	data2, err := restored.Serialize()
	if err != nil {
		t.Fatalf("second Serialize() error: %v", err)
	}

	// Compare via JSON normalization (map key order may vary)
	var v1, v2 any
	if err := json.Unmarshal(data1, &v1); err != nil {
		t.Fatalf("unmarshal data1: %v", err)
	}
	if err := json.Unmarshal(data2, &v2); err != nil {
		t.Fatalf("unmarshal data2: %v", err)
	}
	norm1, _ := json.Marshal(v1)
	norm2, _ := json.Marshal(v2)
	if string(norm1) != string(norm2) {
		t.Errorf("round-trip not equivalent:\n  first:  %s\n  second: %s", norm1, norm2)
	}
}

// --- 30.2-UNIT-007: Deserialize null messages field → empty slice ---

func TestContext_Deserialize_NullMessages(t *testing.T) {
	data := []byte(`{"system_prompt":"sp","messages":null,"max_size":10}`)
	ctx := &Context{ID: 1}
	if err := ctx.Deserialize(data); err != nil {
		t.Fatalf("Deserialize() error: %v", err)
	}
	if ctx.Messages == nil {
		t.Error("Messages should be non-nil empty slice, got nil")
	}
	if len(ctx.Messages) != 0 {
		t.Errorf("Messages len = %d, want 0", len(ctx.Messages))
	}
}
