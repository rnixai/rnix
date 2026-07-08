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
	for field := range t.Fields() {
		tag := field.Tag.Get("json")
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
	ipcFields := jsonFieldSet(reflect.TypeFor[SpawnRequest]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.SpawnRequest]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("SpawnRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_SpawnResponse(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[SpawnResponse]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.SpawnResponse]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("SpawnResponse drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

// TestWireDrift_ResumeRequest verifies ipc.ResumeRequest and wire.ResumeRequest
// have identical JSON field sets (apex 10-11: resume/resume_watch + new_input
// entered the apex-consumed contract surface).
func TestWireDrift_ResumeRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[ResumeRequest]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.ResumeRequest]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ResumeRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

// TestWireDrift_ResumeResponse — PID type differs (types.PID vs uint64) but the
// wire representation must be identical.
func TestWireDrift_ResumeResponse(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[ResumeResponse]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.ResumeResponse]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ResumeResponse drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

// TestWireDrift_ResumeMethodConstants — resume/resume_watch method names are
// wire-sourced; ipc re-exports must stay identical.
func TestWireDrift_ResumeMethodConstants(t *testing.T) {
	if MethodResume != wire.MethodResume || string(wire.MethodResume) != "resume" {
		t.Fatalf("MethodResume drift: ipc=%q wire=%q", MethodResume, wire.MethodResume)
	}
	if MethodResumeWatch != wire.MethodResumeWatch || string(wire.MethodResumeWatch) != "resume_watch" {
		t.Fatalf("MethodResumeWatch drift: ipc=%q wire=%q", MethodResumeWatch, wire.MethodResumeWatch)
	}
}

func TestWireDrift_ProgressPayload(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[ProgressPayload]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.ProgressPayload]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ProgressPayload drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_BudgetStatusRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[BudgetStatusRequest]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.BudgetStatusRequest]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("BudgetStatusRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_BudgetStatusResponse(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[BudgetStatusResponse]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.BudgetStatusResponse]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("BudgetStatusResponse drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_AgentQuotaWire(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[AgentQuotaWire]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.AgentQuotaWire]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("AgentQuotaWire drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_AttachDebugRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[AttachDebugRequest]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.AttachDebugRequest]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("AttachDebugRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_ListEventsRequest(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[ListEventsRequest]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.ListEventsRequest]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ListEventsRequest drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_ListEventsResponse(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[ListEventsResponse]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.ListEventsResponse]())
	if !reflect.DeepEqual(ipcFields, wireFields) {
		t.Fatalf("ListEventsResponse drift:\nipc  = %v\nwire = %v", ipcFields, wireFields)
	}
}

func TestWireDrift_SyscallEventWire(t *testing.T) {
	ipcFields := jsonFieldSet(reflect.TypeFor[SyscallEventWire]())
	wireFields := jsonFieldSet(reflect.TypeFor[wire.SyscallEventWire]())
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
		{"Method", reflect.TypeFor[Method](), reflect.TypeFor[wire.Method]()},
		{"Request", reflect.TypeFor[Request](), reflect.TypeFor[wire.Request]()},
		{"Response", reflect.TypeFor[Response](), reflect.TypeFor[wire.Response]()},
		{"ErrorPayload", reflect.TypeFor[ErrorPayload](), reflect.TypeFor[wire.ErrorPayload]()},
		{"StreamEvent", reflect.TypeFor[StreamEvent](), reflect.TypeFor[wire.StreamEvent]()},
		{"StreamEventType", reflect.TypeFor[StreamEventType](), reflect.TypeFor[wire.StreamEventType]()},
		{"PingResponse", reflect.TypeFor[PingResponse](), reflect.TypeFor[wire.PingResponse]()},
		{"DaemonStatusResponse", reflect.TypeFor[DaemonStatusResponse](), reflect.TypeFor[wire.DaemonStatusResponse]()},
	}
	for _, c := range checks {
		if c.ipcType != c.wireType {
			t.Errorf("%s: ipc type %v != wire type %v (should be alias)", c.name, c.ipcType, c.wireType)
		}
	}
}
