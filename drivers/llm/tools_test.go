package llm

import (
	"context"
	"encoding/json"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
)

func TestToolDef_JSONRoundTrip(t *testing.T) {
	original := ToolDef{
		Name:        "get_weather",
		Description: "Get current weather for a location",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string"},
			},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ToolDef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Description != original.Description {
		t.Errorf("Description = %q, want %q", decoded.Description, original.Description)
	}
	if decoded.Parameters["type"] != "object" {
		t.Errorf("Parameters[type] = %v, want %q", decoded.Parameters["type"], "object")
	}
}

func TestToolCall_JSONRoundTrip(t *testing.T) {
	original := ToolCall{
		ID:   "call_123",
		Name: "get_weather",
		Input: map[string]any{
			"location": "Tokyo",
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ToolCall
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Input["location"] != "Tokyo" {
		t.Errorf("Input[location] = %v, want %q", decoded.Input["location"], "Tokyo")
	}
}

func TestToolResult_JSONRoundTrip(t *testing.T) {
	original := ToolResult{
		ToolCallID: "call_123",
		Content:    "25°C, sunny",
		IsError:    false,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ToolCallID != original.ToolCallID {
		t.Errorf("ToolCallID = %q, want %q", decoded.ToolCallID, original.ToolCallID)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content = %q, want %q", decoded.Content, original.Content)
	}
}

func TestToolCallingDriver_InterfaceEmbedding(t *testing.T) {
	// Compile-time check: ToolCallingDriver embeds LLMDriver.
	// CallWithTools and StreamWithTools are declared on ToolCallingDriver,
	// while Call/Stream/Info come from the embedded LLMDriver.
	t.Log("ToolCallingDriver interface compiles correctly")
}

// Compile-time interface satisfaction check.
var _ ToolCallingDriver = (*toolCallingStub)(nil)

type toolCallingStub struct{}

func (toolCallingStub) Call(context.Context, LLMRequest) (*LLMResponse, error) { return nil, nil }
func (toolCallingStub) Stream(context.Context, LLMRequest) (<-chan StreamEvent, error) {
	return nil, nil
}
func (toolCallingStub) Info() DriverInfo { return DriverInfo{} }
func (toolCallingStub) CallWithTools(context.Context, LLMRequest, []ToolDef) (*LLMResponse, error) {
	return nil, nil
}
func (toolCallingStub) StreamWithTools(context.Context, LLMRequest, []ToolDef) (<-chan StreamEvent, error) {
	return nil, nil
}

func TestMessage_JSONRoundTrip(t *testing.T) {
	original := Message{
		Role:       "tool",
		Content:    "result data",
		ToolCallID: "call_456",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Role != original.Role {
		t.Errorf("Role = %q, want %q", decoded.Role, original.Role)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content = %q, want %q", decoded.Content, original.Content)
	}
	if decoded.ToolCallID != original.ToolCallID {
		t.Errorf("ToolCallID = %q, want %q", decoded.ToolCallID, original.ToolCallID)
	}
}

func TestMessage_JSONCompatWithContextMessage(t *testing.T) {
	// Serialize llm.Message, deserialize as context.Message — fields must match.
	llmMsg := Message{
		Role:       "tool",
		Content:    "result",
		ToolCallID: "call_123",
	}
	data, err := json.Marshal(llmMsg)
	if err != nil {
		t.Fatalf("Marshal llm.Message: %v", err)
	}

	var ctxMsg rnixctx.Message
	if err := json.Unmarshal(data, &ctxMsg); err != nil {
		t.Fatalf("Unmarshal to context.Message: %v", err)
	}
	if string(ctxMsg.Role) != llmMsg.Role {
		t.Errorf("Role = %q, want %q", ctxMsg.Role, llmMsg.Role)
	}
	if ctxMsg.Content != llmMsg.Content {
		t.Errorf("Content = %q, want %q", ctxMsg.Content, llmMsg.Content)
	}
	if ctxMsg.ToolCallID != llmMsg.ToolCallID {
		t.Errorf("ToolCallID = %q, want %q", ctxMsg.ToolCallID, llmMsg.ToolCallID)
	}
}

func TestStreamEvent_ToolCalls_JSONRoundTrip(t *testing.T) {
	original := StreamEvent{
		Type: "done",
		ToolCalls: []ToolCall{
			{ID: "call_1", Name: "search", Input: map[string]any{"q": "test"}},
			{ID: "call_2", Name: "read"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded StreamEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.ToolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(decoded.ToolCalls))
	}
	if decoded.ToolCalls[0].ID != "call_1" || decoded.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls[0] = %+v, want id=call_1 name=search", decoded.ToolCalls[0])
	}
	if decoded.ToolCalls[1].ID != "call_2" || decoded.ToolCalls[1].Name != "read" {
		t.Errorf("ToolCalls[1] = %+v, want id=call_2 name=read", decoded.ToolCalls[1])
	}
}
