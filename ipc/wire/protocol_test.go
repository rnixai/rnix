package wire

import (
	"encoding/json"
	"testing"
)

func TestRequestEnvelopeUsesPayloadOnly(t *testing.T) {
	payload, err := json.Marshal(SpawnRequest{Intent: "hello", Agent: "apex-planner-pm"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	frame, err := json.Marshal(Request{Method: MethodSpawn, Payload: payload})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(frame, &got); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if got["method"] != "spawn" {
		t.Fatalf("method = %v, want spawn", got["method"])
	}
	if _, ok := got["payload"]; !ok {
		t.Fatalf("payload missing from request: %s", frame)
	}
	if _, ok := got["params"]; ok {
		t.Fatalf("params must not appear in wire request: %s", frame)
	}
}

func TestProtocolVersionIsDeclared(t *testing.T) {
	if ProtocolVersion == "" {
		t.Fatal("ProtocolVersion must be declared")
	}
}
