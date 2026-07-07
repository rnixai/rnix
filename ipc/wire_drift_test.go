package ipc

import (
	"reflect"
	"sort"
	"testing"

	"github.com/rnixai/rnix/ipc/wire"
)

// jsonFieldSet extracts the set of json tag names from a struct type.
func jsonFieldSet(t reflect.Type) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		if idx := len(tag); idx > 0 {
			for j := 0; j < len(tag); j++ {
				if tag[j] == ',' {
					name = tag[:j]
					break
				}
			}
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

// TestWireDrift_SpawnRequest verifies ipc.SpawnRequest and wire.SpawnRequest
// have identical JSON field sets. The PID field type differs (types.PID vs
// uint64) but the wire representation is the same.
func TestWireDrift_SpawnRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(SpawnRequest{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.SpawnRequest{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("SpawnRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_SpawnResponse(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(SpawnResponse{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.SpawnResponse{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("SpawnResponse drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_ProgressPayload(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(ProgressPayload{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.ProgressPayload{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ProgressPayload drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_BudgetStatusRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(BudgetStatusRequest{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.BudgetStatusRequest{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("BudgetStatusRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_BudgetStatusResponse(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(BudgetStatusResponse{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.BudgetStatusResponse{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("BudgetStatusResponse drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_AgentQuotaWire(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(AgentQuotaWire{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.AgentQuotaWire{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("AgentQuotaWire drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_AttachDebugRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(AttachDebugRequest{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.AttachDebugRequest{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("AttachDebugRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_ListEventsRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(ListEventsRequest{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.ListEventsRequest{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ListEventsRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_ListEventsResponse(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(ListEventsResponse{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.ListEventsResponse{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ListEventsResponse drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_SyscallEventWire(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeOf(SyscallEventWire{}))
	wireFields := jsonFieldSet(reflect.TypeOf(wire.SyscallEventWire{}))
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("SyscallEventWire drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

// TestWireAliasesAreAliases confirms aliased types are the same type (not
// copies). A Go type alias makes the two identical at the reflect level.
func TestWireAliasesAreAliases(t *testing.T) {
	checks := []struct {
		name     string
		ipcType  reflect.Type
		wireType reflect.Type
	}{
		{"Method", reflect.TypeOf(Method("")), reflect.TypeOf(wire.Method(""))},
		{"Request", reflect.TypeOf(Request{}), reflect.TypeOf(wire.Request{})},
		{"Response", reflect.TypeOf(Response{}), reflect.TypeOf(wire.Response{})},
		{"ErrorPayload", reflect.TypeOf(ErrorPayload{}), reflect.TypeOf(wire.ErrorPayload{})},
		{"StreamEvent", reflect.TypeOf(StreamEvent{}), reflect.TypeOf(wire.StreamEvent{})},
		{"StreamEventType", reflect.TypeOf(StreamEventType("")), reflect.TypeOf(wire.StreamEventType(""))},
		{"PingResponse", reflect.TypeOf(PingResponse{}), reflect.TypeOf(wire.PingResponse{})},
		{"DaemonStatusResponse", reflect.TypeOf(DaemonStatusResponse{}), reflect.TypeOf(wire.DaemonStatusResponse{})},
	}
	for _, c := range checks {
		if c.ipcType != c.wireType {
			t.Errorf("%s: ipc type %v != wire type %v (should be alias)", c.name, c.ipcType, c.wireType)
		}
	}
}
