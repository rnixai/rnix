package kernel

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.1 — proc-info.json mcp_mounts schema tests
//
// Covers Story 48.1 Task 1.6:
//   - Roundtrip test: write procInfoDisk with mcp_mounts → unmarshal →
//     compare field values.
//   - Backward-compat test: old proc-info.json files without mcp_mounts
//     deserialize cleanly with the field as nil; subsequent code paths
//     that read MCPMounts MUST handle nil-as-empty.
//
// These tests live in their own file (rather than appending to an existing
// _test.go) so a dev reading kernel/process_history.go can grep for
// "process_history_mcp_mounts_test" and find the schema contract.
// =============================================================================

// TestProcInfoDisk_MCPMounts_Roundtrip — Story 48.1 Task 1.2 + 1.3.
//
// Activate by removing t.Skip after dev adds:
//   1. mcpMountDisk struct with json tags `path,config`.
//   2. procInfoDisk.MCPMounts []mcpMountDisk field with json:"mcp_mounts,omitempty".
//   3. procInfoToDisk + procInfoFromDisk handling for the new field.
//   4. vfs.MCPMountSnapshot type with {Path string, Config vfs.MCPConfig}.
//   5. vfs.ProcInfo.MCPMounts []MCPMountSnapshot field.
//
// Assertions verify that all serializable MCPConfig fields (ServerName /
// Command / Args / Env / TransportType / WorkDir / Instructions) survive the
// memory → JSON → memory round trip. JSON field naming uses snake_case per
// project-context.md §输出格式.
func TestProcInfoDisk_MCPMounts_Roundtrip(t *testing.T) {
	original := vfs.ProcInfo{
		PID: 100, UUID: "abc", State: types.StateDead,
		MCPMounts: []vfs.MCPMountSnapshot{
			{
				Path: "/mnt/mcp/100-deepwiki",
				Config: vfs.MCPConfig{
					ServerName:    "deepwiki",
					Command:       "/usr/local/bin/deepwiki",
					Args:          []string{"--mode", "stdio"},
					Env:           map[string]string{"DW_TOKEN": "xyz", "LOG_LEVEL": "info"},
					TransportType: "stdio",
					WorkDir:       "/var/run/deepwiki",
					Instructions:  "use deepwiki for documentation lookups",
				},
			},
		},
	}

	disk := procInfoToDisk(original)
	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Field name assertion: the JSON must contain snake_case mcp_mounts.
	if !strings.Contains(string(data), `"mcp_mounts"`) {
		t.Errorf("serialized JSON missing snake_case mcp_mounts field: %s", string(data))
	}

	var roundtrip procInfoDisk
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := procInfoFromDisk(roundtrip)
	if len(got.MCPMounts) != 1 {
		t.Fatalf("MCPMounts len = %d, want 1", len(got.MCPMounts))
	}
	gotMount := got.MCPMounts[0]
	wantMount := original.MCPMounts[0]
	if gotMount.Path != wantMount.Path {
		t.Errorf("Path = %q, want %q", gotMount.Path, wantMount.Path)
	}
	if !reflect.DeepEqual(gotMount.Config, wantMount.Config) {
		t.Errorf("Config differs:\n  got:  %+v\n  want: %+v", gotMount.Config, wantMount.Config)
	}
}

// TestProcInfoDisk_MCPMounts_BackwardCompat — Story 48.1 Task 1.6 (compat).
//
// Old proc-info.json files (pre-Story-48.1) have NO mcp_mounts field. The
// existing procInfoFromDisk must continue to unmarshal them without error,
// leaving downstream MCPMounts as nil. Rehydrate's mount loop then treats
// nil-MCPMounts the same as "no MCP servers" (AC6 zero-overhead path).
//
// This test is ACTIVE — it must pass NOW and continue to pass after dev
// lands the schema extension. A regression here means the new field broke
// JSON unmarshalling of legacy snapshots.
func TestProcInfoDisk_MCPMounts_BackwardCompat(t *testing.T) {
	// Hand-crafted legacy proc-info.json (intentionally NO mcp_mounts field).
	legacyJSON := []byte(`{
		"pid": 42,
		"uuid": "legacy-no-mcp-mounts-0000-000000000042",
		"ppid": 0,
		"state": "dead",
		"intent": "legacy process from pre-48.1 daemon",
		"tokens_used": 1500,
		"ctx_id": 7,
		"allowed_devices": ["/dev/fs", "/dev/shell"],
		"provider": "claude",
		"model": "claude-4",
		"pipeline_index": 0,
		"pipeline_total": 0,
		"created_at": "2026-05-01T00:00:00Z"
	}`)

	var disk procInfoDisk
	if err := json.Unmarshal(legacyJSON, &disk); err != nil {
		t.Fatalf("legacy proc-info.json failed to unmarshal: %v", err)
	}

	info := procInfoFromDisk(disk)

	// Sanity checks: known fields survived.
	if info.PID != 42 {
		t.Errorf("PID = %d, want 42", info.PID)
	}
	if info.UUID != "legacy-no-mcp-mounts-0000-000000000042" {
		t.Errorf("UUID = %q, want legacy-...", info.UUID)
	}
	if info.Intent == "" {
		t.Errorf("Intent dropped during unmarshal")
	}

	// The defining assertion — re-marshal must NOT silently inject an empty
	// mcp_mounts array. Empty/missing field stays absent (omitempty).
	roundtrip, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if strings.Contains(string(roundtrip), `"mcp_mounts":[]`) {
		t.Errorf("legacy snapshot re-marshalled with empty mcp_mounts:[] — omitempty missing on field tag: %s",
			string(roundtrip))
	}

	// Forward-compat reminder: after Task 1.1 lands, this assertion changes
	// from "string must not contain mcp_mounts:[]" to "info.MCPMounts must
	// be nil or empty". The test author intentionally encodes the strict
	// invariant ("omitempty") so a dev who forgets the tag gets a clear
	// failure message.
}
