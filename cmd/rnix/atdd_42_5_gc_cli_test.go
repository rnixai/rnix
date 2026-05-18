package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// ATDD 42.5: 治理层 — `rnix gc` CLI 子命令 (AC#4, #5, #9, #13)
//
// Acceptance criteria covered:
//   - AC#4   CLI-001  Cobra command shape (flags, RunE, registration)
//   - AC#4   CLI-002  --dry-run table rendering schema
//   - AC#4   CLI-003  --dry-run --json schema
//   - AC#5   CLI-004  default text-mode stats formatting
//   - AC#5   CLI-005  --json gc response schema
//   - AC#5   CLI-006  formatBytesIEC matches AC#5 "M.M MB" expectations
//   - AC#9   CLI-007  Confirmation prompt fires above gcConfirmThreshold
//                     and respects --force
//   - AC#9   CLI-008  --json implies --force (no prompt in non-tty mode)
// =============================================================================

// ---------------------------------------------------------------------------
// CLI-001 (AC#4): cobra command shape
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_001_CommandShape(t *testing.T) {
	if gcCmd == nil {
		t.Fatal("gcCmd is nil")
	}
	if gcCmd.Use != "gc" {
		t.Errorf("gcCmd.Use = %q, want \"gc\"", gcCmd.Use)
	}
	if gcCmd.RunE == nil {
		t.Error("gcCmd.RunE is nil — runGc must be wired")
	}

	// Flags must be registered.
	for _, name := range []string{"dry-run", "force", "json"} {
		if gcCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered on gcCmd", name)
		}
	}

	// gcCmd must be registered as a subcommand of rootCmd (top-level command).
	if !slices.Contains(rootCmd.Commands(), gcCmd) {
		t.Error("gcCmd is not registered on rootCmd (must be top-level: `rnix gc`)")
	}
}

// ---------------------------------------------------------------------------
// CLI-002 (AC#4): --dry-run table rendering schema
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_002_DryRun_Table_Header(t *testing.T) {
	var buf bytes.Buffer
	// Use canonical 36-char UUIDs so gcTruncateUUID is a passthrough.
	uuid1 := "11111111-aaaa-bbbb-cccc-000000000001"
	uuid2 := "22222222-aaaa-bbbb-cccc-000000000002"
	renderGcDryRunTable(&buf, []ipc.GcCandidateWire{
		{UUID: uuid1, DeadAt: "2026-04-16T08:30:00Z", SizeBytes: 5 * 1024 * 1024, Reason: "age"},
		{UUID: uuid2, DeadAt: "2026-04-17T14:20:33Z", SizeBytes: 12 * 1024 * 1024, Reason: "age,count"},
	})

	out := buf.String()
	mustContain := []string{
		"UUID", "DEAD_AT", "SIZE", "REASON",
		uuid1,
		uuid2,
		"age",
		"age,count",
		"2 candidates",
		"dry-run, no changes",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("table missing %q\noutput:\n%s", s, out)
		}
	}

	// Empty candidates should render "No candidates" message.
	buf.Reset()
	renderGcDryRunTable(&buf, nil)
	if !strings.Contains(buf.String(), "No candidates") {
		t.Errorf("empty candidate list must mention \"No candidates\"; got %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// CLI-003 (AC#4): --dry-run JSON schema
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_003_DryRun_JSON_Schema(t *testing.T) {
	var buf bytes.Buffer
	renderGcDryRunJSON(&buf, []ipc.GcCandidateWire{
		{UUID: "uuid-1", DeadAt: "2026-04-16T08:30:00Z", SizeBytes: 1024, Reason: "age"},
	})

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output not valid JSON: %v\noutput=%q", err, buf.String())
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Errorf("ok = false, want true")
	}
	if dr, _ := parsed["dry_run"].(bool); !dr {
		t.Errorf("dry_run = false, want true")
	}
	cands, ok := parsed["candidates"].([]any)
	if !ok {
		t.Fatalf("candidates field missing or wrong type; parsed=%+v", parsed)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates len = %d, want 1", len(cands))
	}
	c, _ := cands[0].(map[string]any)
	for _, field := range []string{"uuid", "dead_at", "size_bytes", "reason"} {
		if _, ok := c[field]; !ok {
			t.Errorf("candidate missing field %q (AC#4 schema)", field)
		}
	}

	// Nil candidates → empty array, not null.
	buf.Reset()
	renderGcDryRunJSON(&buf, nil)
	var parsedNil map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsedNil); err != nil {
		t.Fatalf("nil candidates not valid JSON: %v", err)
	}
	cands, _ = parsedNil["candidates"].([]any)
	if cands == nil {
		t.Errorf("candidates must be [] not null when empty")
	}
}

// ---------------------------------------------------------------------------
// CLI-004 (AC#5): text-mode stats formatting
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_004_Gc_Stats_Formatting(t *testing.T) {
	var buf bytes.Buffer
	renderGcStats(&buf, &ipc.GcResponse{
		OK:           true,
		RemovedCount: 3,
		FreedBytes:   20 * 1024 * 1024,
		RemovedUUIDs: []string{"a", "b", "c"},
	})
	out := buf.String()
	if !strings.Contains(out, "Removed 3 processes") {
		t.Errorf("missing \"Removed 3 processes\"; got %q", out)
	}
	if !strings.Contains(out, "20.00 MiB") {
		t.Errorf("expected \"20.00 MiB\" freed marker; got %q", out)
	}
}

// ---------------------------------------------------------------------------
// CLI-005 (AC#5): --json gc response schema
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_005_Gc_JSON_Schema(t *testing.T) {
	var buf bytes.Buffer
	renderGcStatsJSON(&buf, &ipc.GcResponse{
		OK:           true,
		RemovedCount: 3,
		FreedBytes:   20 * 1024 * 1024,
		RemovedUUIDs: []string{"a", "b", "c"},
	})

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\noutput=%q", err, buf.String())
	}
	for _, field := range []string{"ok", "removed_count", "freed_bytes", "removed_uuids"} {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing field %q (AC#5 schema)", field)
		}
	}

	// nil RemovedUUIDs must still serialize as [] not null.
	buf.Reset()
	renderGcStatsJSON(&buf, &ipc.GcResponse{OK: true, RemovedCount: 0, FreedBytes: 0})
	var parsedNil map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsedNil); err != nil {
		t.Fatalf("nil uuids not valid JSON: %v", err)
	}
	uuids, _ := parsedNil["removed_uuids"].([]any)
	if uuids == nil {
		t.Errorf("removed_uuids must be [] not null when nil")
	}
}

// ---------------------------------------------------------------------------
// CLI-006 (AC#5): formatBytesIEC unit ladder
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_006_FormatBytesIEC(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KiB"},
		{1536, "1.50 KiB"},
		{5 * 1024 * 1024, "5.00 MiB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.50 GiB"},
	}
	for _, tc := range cases {
		got := formatBytesIEC(tc.in)
		if got != tc.want {
			t.Errorf("formatBytesIEC(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CLI-007 (AC#9): gcConfirm — declines on EOF / blank / non-yes
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_007_Confirm_DefaultDeny(t *testing.T) {
	cases := []struct {
		name  string
		stdin string
		want  bool
	}{
		{"empty input", "", false},
		{"blank line", "\n", false},
		{"explicit N", "n\n", false},
		{"random text", "maybe\n", false},
		{"explicit y", "y\n", true},
		{"explicit Y", "Y\n", true},
		{"yes", "yes\n", true},
	}
	cand := []ipc.GcCandidateWire{{UUID: "u1", SizeBytes: 1024, Reason: "age"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got := gcConfirm(&out, strings.NewReader(tc.stdin), cand)
			if got != tc.want {
				t.Errorf("gcConfirm(stdin=%q) = %v, want %v", tc.stdin, got, tc.want)
			}
			if !strings.Contains(out.String(), "Proceed? [y/N]") {
				t.Errorf("missing prompt; got %q", out.String())
			}
		})
	}

	// Empty candidate list short-circuits to false (caller does not show prompt).
	var out bytes.Buffer
	if gcConfirm(&out, strings.NewReader("y\n"), nil) {
		t.Error("gcConfirm with empty candidates must return false")
	}
}

// ---------------------------------------------------------------------------
// CLI-008 (AC#9): --json + --force semantics (declarative test of the seam)
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_008_JSON_Implies_Force(t *testing.T) {
	// Build a mock client returning more than gcConfirmThreshold candidates so
	// the prompt branch would normally fire. With jsonOut=true the prompt MUST
	// be skipped — runGcWithClient must not even attempt to read from stdin.
	mock := &mockGcClient{
		dryRun: &ipc.GcDryRunResponse{
			OK:         true,
			DryRun:     true,
			Candidates: makeMockCandidates(gcConfirmThreshold + 5),
		},
		gc: &ipc.GcResponse{
			OK:           true,
			RemovedCount: gcConfirmThreshold + 5,
			FreedBytes:   1024,
			RemovedUUIDs: []string{"u1", "u2"},
		},
	}
	var out bytes.Buffer
	in := &panicReader{t: t} // any read = test failure
	err := runGcWithClient(mock, &out, in, false, false, true)
	if err != nil {
		t.Fatalf("runGcWithClient err: %v", err)
	}
	if mock.gcCalls != 1 {
		t.Errorf("Gc called %d times, want 1", mock.gcCalls)
	}
	if in.touched {
		t.Error("stdin was read in --json mode (AC#9 last clause violation)")
	}
	// JSON output present?
	if !strings.Contains(out.String(), `"removed_count":`) {
		t.Errorf("expected JSON stats; got %q", out.String())
	}
}

// mockGcClient implements the gcClient interface for CLI-008.
type mockGcClient struct {
	dryRun  *ipc.GcDryRunResponse
	gc      *ipc.GcResponse
	dryErr  error
	gcErr   error
	dryCalls int
	gcCalls  int
}

func (m *mockGcClient) Gc() (*ipc.GcResponse, error) {
	m.gcCalls++
	if m.gcErr != nil {
		return nil, m.gcErr
	}
	return m.gc, nil
}

func (m *mockGcClient) GcDryRun() (*ipc.GcDryRunResponse, error) {
	m.dryCalls++
	if m.dryErr != nil {
		return nil, m.dryErr
	}
	return m.dryRun, nil
}

// panicReader fails the test if read.
type panicReader struct {
	t       *testing.T
	touched bool
}

func (p *panicReader) Read(_ []byte) (int, error) {
	p.touched = true
	p.t.Fatal("stdin must not be read in --json or --force mode")
	return 0, nil
}

func makeMockCandidates(n int) []ipc.GcCandidateWire {
	out := make([]ipc.GcCandidateWire, n)
	for i := range n {
		out[i] = ipc.GcCandidateWire{
			UUID:      "mock-uuid-" + string(rune('a'+(i%26))),
			DeadAt:    "2026-04-16T08:30:00Z",
			SizeBytes: 1024,
			Reason:    "age",
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// CLI-009 (AC#9 boundary): gcConfirmThreshold equals 100 (boundary 100 is below)
// ---------------------------------------------------------------------------

func TestATDD_42_5_CLI_009_ConfirmThreshold_Boundary(t *testing.T) {
	if gcConfirmThreshold != 100 {
		t.Errorf("gcConfirmThreshold = %d, want 100 (AC#9: \"超过 100\" = strictly greater than 100)", gcConfirmThreshold)
	}
}

// ---------------------------------------------------------------------------
// Stub sanity checks (post-GREEN: become natural no-ops)
// ---------------------------------------------------------------------------

// TestATDD_42_5_CLI_StubSanity_RunGc verifies that runGc no longer panics and
// is wired to the cobra command. After GREEN-phase, calling it from the test
// environment is expected to set exitCode (via outputError) and return nil —
// NOT to return a sentinel error.
//
// Safety: dryRun is forced true so that even if a daemon happens to be running
// on the dev box, no .rnix/data/steps/ deletion occurs.
func TestATDD_42_5_CLI_StubSanity_RunGc(t *testing.T) {
	prevExit := exitCode
	prevJSON := flagGcJSON
	prevDryRun := flagGcDryRun
	defer func() {
		exitCode = prevExit
		flagGcJSON = prevJSON
		flagGcDryRun = prevDryRun
	}()
	flagGcJSON = false   // do not pollute stdout in this stub-sanity check
	flagGcDryRun = true  // prevent destructive client.Gc() if daemon is alive

	err := runGc(gcCmd, nil)
	// runGc returns nil even on error (it sets exitCode and lets cobra root
	// handle exit code). The key contract: no panic, no sentinel returned.
	if err != nil {
		t.Errorf("runGc should return nil (exitCode handles failures); got %v", err)
	}
}

// TestATDD_42_5_CLI_StubSanity_RunGcWithClient confirms that the testable
// seam runGcWithClient now refuses nil client (GREEN-phase guard) — previously
// it returned the RED-phase sentinel. Real IPC-driven coverage lives behind
// the daemon-backed e2e tests; this only asserts the nil-guard.
func TestATDD_42_5_CLI_StubSanity_RunGcWithClient(t *testing.T) {
	err := runGcWithClient(nil, nil, nil, false, false, false)
	if err == nil {
		t.Error("runGcWithClient(nil client) must return error")
		return
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("runGcWithClient(nil client) err must mention nil; got %v", err)
	}
}
