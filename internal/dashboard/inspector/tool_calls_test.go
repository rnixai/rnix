// Package inspector — tool_calls_test.go (Story 38-5 PR11 Step 4(c))
package inspector

import (
	"testing"

	"github.com/rnixai/rnix/ipc"
)

func TestBuildToolCallNameMap_Empty(t *testing.T) {
	got := BuildToolCallNameMap(nil)
	if got == nil {
		t.Error("nil msgs 应返回非 nil 空 map")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestBuildToolCallNameMap_SingleMessage_MultipleToolCalls(t *testing.T) {
	msgs := []ipc.MessageWire{
		{
			ToolCalls: []ipc.ToolCallWire{
				{ID: "tc1", Name: "read_file"},
				{ID: "tc2", Name: "write_file"},
			},
		},
	}
	got := BuildToolCallNameMap(msgs)
	if got["tc1"] != "read_file" {
		t.Errorf("tc1 = %q, want read_file", got["tc1"])
	}
	if got["tc2"] != "write_file" {
		t.Errorf("tc2 = %q, want write_file", got["tc2"])
	}
}

func TestBuildToolCallNameMap_MultipleMessages(t *testing.T) {
	msgs := []ipc.MessageWire{
		{ToolCalls: []ipc.ToolCallWire{{ID: "a", Name: "Tool A"}}},
		{ToolCalls: []ipc.ToolCallWire{{ID: "b", Name: "Tool B"}}},
		{ToolCalls: []ipc.ToolCallWire{{ID: "c", Name: "Tool C"}}},
	}
	got := BuildToolCallNameMap(msgs)
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	for _, want := range []struct{ k, v string }{{"a", "Tool A"}, {"b", "Tool B"}, {"c", "Tool C"}} {
		if got[want.k] != want.v {
			t.Errorf("%s = %q, want %q", want.k, got[want.k], want.v)
		}
	}
}

func TestBuildToolCallNameMap_DuplicateID_LastWins(t *testing.T) {
	msgs := []ipc.MessageWire{
		{ToolCalls: []ipc.ToolCallWire{{ID: "x", Name: "first"}}},
		{ToolCalls: []ipc.ToolCallWire{{ID: "x", Name: "second"}}},
	}
	got := BuildToolCallNameMap(msgs)
	if got["x"] != "second" {
		t.Errorf("duplicate ID 后写覆盖: got %q, want second", got["x"])
	}
}

func TestBuildToolCallNameMap_MessageWithoutToolCalls_Skipped(t *testing.T) {
	msgs := []ipc.MessageWire{
		{ToolCalls: nil},
		{ToolCalls: []ipc.ToolCallWire{{ID: "a", Name: "A"}}},
		{ToolCalls: []ipc.ToolCallWire{}},
	}
	got := BuildToolCallNameMap(msgs)
	if len(got) != 1 {
		t.Errorf("len = %d, want 1 (跳过空 ToolCalls 的 message)", len(got))
	}
	if got["a"] != "A" {
		t.Errorf("a = %q, want A", got["a"])
	}
}
