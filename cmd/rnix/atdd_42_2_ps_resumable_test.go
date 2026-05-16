package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// ATDD 42.2: 韧性层 — CLI `rnix ps --resumable` 渲染（AC#6）
//
// RED PHASE: render functions are stubs (see ps_resumable.go); tests below are
// skipped until dev-story implements:
//   - psCmd.Flags().BoolVar(&flagResumable, "resumable", false, "...")
//   - runPs branch on flagResumable
//   - real renderResumable{Table,JSON,Quiet}
//   - formatRelativeTimeForPs duration math
// =============================================================================

// --- 42.2-CLI-001: --resumable 空列表渲染 (AC#6) ---

func TestATDD_42_2_CLI_001_Resumable_EmptyList(t *testing.T) {
	t.Skip("RED PHASE: dev-story replaces renderResumableTable with real implementation")

	var buf bytes.Buffer
	renderResumableTable(&buf, nil)
	out := buf.String()
	if !strings.Contains(out, "No resumable processes.") {
		t.Errorf("empty list output = %q, want substring %q", out, "No resumable processes.")
	}
}

// --- 42.2-CLI-001b: --resumable table 模式渲染 ---

func TestATDD_42_2_CLI_001b_Resumable_TableMode(t *testing.T) {
	t.Skip("RED PHASE: pending dev-story implementation")

	procs := []ipc.ResumableProcessWire{
		{
			UUID:       "abcdef12-3456-7890-abcd-ef1234567890",
			Intent:     "long task one",
			Agent:      "code-analyst",
			LastStep:   12,
			LastActive: 1747000000000, // arbitrary ms timestamp
			Provider:   "claude",
			Model:      "claude-4",
		},
		{
			UUID:       "fedcba98-7654-3210-fedc-ba9876543210",
			Intent:     "long task two",
			Agent:      "researcher",
			LastStep:   7,
			LastActive: 1747000600000,
			Provider:   "claude",
			Model:      "claude-4",
		},
	}

	var buf bytes.Buffer
	renderResumableTable(&buf, procs)
	out := buf.String()

	for _, want := range []string{"abcdef12", "fedcba98", "code-analyst", "researcher", "12", "7"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q\noutput:\n%s", want, out)
		}
	}

	// AC#6: LastActive should render as a human-readable relative time, not raw ms.
	if strings.Contains(out, "1747000000000") {
		t.Errorf("table output should not show raw ms timestamp; got:\n%s", out)
	}
}

// --- 42.2-CLI-001c: --resumable JSON 模式渲染 ---

func TestATDD_42_2_CLI_001c_Resumable_JSONMode(t *testing.T) {
	t.Skip("RED PHASE: pending dev-story implementation")

	procs := []ipc.ResumableProcessWire{
		{
			UUID:       "uuid-json-test-aaaaaaaaaaaaaaaaaaaa",
			Intent:     "json test",
			Agent:      "code-analyst",
			LastStep:   3,
			LastActive: 1747001234000,
			Provider:   "claude",
			Model:      "claude-4",
		},
	}

	var buf bytes.Buffer
	renderResumableJSON(&buf, procs)

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Processes []ipc.ResumableProcessWire `json:"processes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if !resp.OK {
		t.Error("response OK should be true")
	}
	if len(resp.Data.Processes) != 1 {
		t.Fatalf("processes len = %d, want 1", len(resp.Data.Processes))
	}
	if got := resp.Data.Processes[0]; got.UUID != "uuid-json-test-aaaaaaaaaaaaaaaaaaaa" || got.LastStep != 3 {
		t.Errorf("JSON roundtrip mismatch: %+v", got)
	}
}

// --- 42.2-CLI-001d: --resumable quiet 模式渲染 ---

func TestATDD_42_2_CLI_001d_Resumable_QuietMode(t *testing.T) {
	t.Skip("RED PHASE: pending dev-story implementation")

	procs := []ipc.ResumableProcessWire{
		{UUID: "u1-quiet-test-aaaaaaaaaaaaaaaaaaaa", LastStep: 1},
		{UUID: "u2-quiet-test-bbbbbbbbbbbbbbbbbbbb", LastStep: 2},
	}

	var buf bytes.Buffer
	renderResumableQuiet(&buf, procs)
	got := strings.TrimRight(buf.String(), "\n")
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("quiet output lines = %d, want 2\n%s", len(lines), buf.String())
	}
	if lines[0] != procs[0].UUID || lines[1] != procs[1].UUID {
		t.Errorf("quiet output lines:\n%v\nwant:\n%s\n%s", lines, procs[0].UUID, procs[1].UUID)
	}
}
