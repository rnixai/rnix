package kernel

// ATDD for Story 66.6 — kernel-side mid-stream usage ledger + tool-call liveness.
//
// Covered ACs:
//   AC1 中途增量: a CLI driver's mid-stream usage events grow proc TokensUsed
//     (StreamTokensUsed synthesis) while the step is still in flight — no `done`.
//   AC2 provenance: mergeProvenance keeps the lowest fidelity; cli_stream is
//     stamped by the handler and survives to proc-info.json.
//   AC4 对账不双计: a normally-completed step uses resp.TokensUsed authoritatively
//     (mid-stream accumulation discarded), NOT the sum.
//   AC4 被杀并入 / AC6: a step killed before its boundary folds StreamTokensUsed
//     into TokensUsed (finishProcess), so proc-info.json / immune Finalize see it.
//   AC5 ToolCallCount: CLI DriverToolCall flushes and kernel VFS dispatches both
//     bump the process-level tool-call liveness counter.
//
// Harness: usageLLMFile is a StreamObserver + DriverTypeProvider mock that drives
// the REAL observe.go stream handler by invoking the installed handler with usage
// / tool_call events during Write — the same path a real CLI driver takes through
// writeStream.onEvent. Reuses newInterruptKernel / interruptSpawnOpts / waitDone /
// resolveStepDir / readProcInfoField from atdd_66_2.
//
// RED proof (verified during dev): commenting out the observe.go usage branch
// zeroes AC1's mid-stream tokens; commenting out the finishProcess fold-in zeroes
// AC4-kill's proc-info tokens; commenting out the reason.go step-boundary clear
// makes AC4-normal report 450 instead of 250.

import (
	gocontext "context"
	"testing"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// usageLLMFile drives the real stream handler with a scripted sequence of driver
// events, then either parks until ctx.Done (kill scenario) or returns so Read
// serves `resp` (normal-completion scenario).
type usageLLMFile struct {
	driverType string
	handler    func(map[string]any)
	events     []map[string]any // emitted through the handler during Write
	park       bool             // park until ctx.Done after emitting events
	reached    chan struct{}    // closed when Write finished emitting (kill scenarios)
	resp       []byte           // Read return (normal completion)
}

func (f *usageLLMFile) SetStreamHandler(fn func(event map[string]any)) { f.handler = fn }
func (f *usageLLMFile) DriverType() string                            { return f.driverType }

func (f *usageLLMFile) Write(ctx gocontext.Context, _ []byte) error {
	for _, e := range f.events {
		if f.handler != nil {
			f.handler(e)
		}
	}
	if f.park {
		if f.reached != nil {
			select {
			case <-f.reached:
			default:
				close(f.reached)
			}
		}
		<-ctx.Done()
		return &llm.StreamInterruptedError{Partial: ""}
	}
	return nil
}

func (f *usageLLMFile) Read(_ int) ([]byte, error) {
	if f.resp != nil {
		return f.resp, nil
	}
	return makeLLMResponse("normal", 5), nil
}

func (f *usageLLMFile) Close() error { return nil }
func (f *usageLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *usageLLMFile) SupportsToolCalling() bool { return true }

// -----------------------------------------------------------------------------
// 66-6-UNIT-004 (AC2/AC5 persistence): usage_provenance + tool_call_count survive
// the procInfoDisk round-trip; omitempty keeps legacy snapshots clean.
// -----------------------------------------------------------------------------
func TestATDD_66_6_UNIT_004_ProcInfoDiskRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:             9,
		UUID:            "uuid-66-6-disk",
		State:           types.StateDead,
		Intent:          "x",
		TokensUsed:      777,
		UsageProvenance: vfs.UsageProvenanceCLIStream,
		ToolCallCount:   594,
	}
	back := procInfoFromDisk(procInfoToDisk(info))
	if back.UsageProvenance != vfs.UsageProvenanceCLIStream {
		t.Errorf("disk round-trip UsageProvenance=%q want cli_stream", back.UsageProvenance)
	}
	if back.ToolCallCount != 594 {
		t.Errorf("disk round-trip ToolCallCount=%d want 594", back.ToolCallCount)
	}
}

func usageEvt(tokens, input, output int) map[string]any {
	return map[string]any{
		"type":                "usage",
		"tokens_used":         tokens,
		"input_tokens":        input,
		"output_tokens":       output,
		"cached_input_tokens": 0,
	}
}

// -----------------------------------------------------------------------------
// 66-6-UNIT-001 (AC2): mergeProvenance keeps the lowest fidelity; unknown labels
// are held conservatively (never used to lower a known rank).
// -----------------------------------------------------------------------------
func TestATDD_66_6_UNIT_001_MergeProvenance(t *testing.T) {
	cases := []struct {
		cur, incoming, want string
	}{
		{"", vfs.UsageProvenanceCLIStream, vfs.UsageProvenanceCLIStream},          // empty → take incoming
		{vfs.UsageProvenanceAPIResponse, "", vfs.UsageProvenanceAPIResponse},      // empty incoming → keep cur
		{vfs.UsageProvenanceAPIResponse, vfs.UsageProvenanceCLIStream, vfs.UsageProvenanceCLIStream}, // lower wins
		{vfs.UsageProvenanceCLIStream, vfs.UsageProvenanceAPIResponse, vfs.UsageProvenanceCLIStream}, // stays lowest
		{vfs.UsageProvenanceCLIStream, vfs.UsageProvenanceEstimated, vfs.UsageProvenanceEstimated},   // estimated is lowest
		{vfs.UsageProvenanceCLIStream, vfs.UsageProvenanceCLIStream, vfs.UsageProvenanceCLIStream},   // idempotent
		{vfs.UsageProvenanceCLIStream, "weird", vfs.UsageProvenanceCLIStream},     // unknown incoming → keep known cur
		{"weird", vfs.UsageProvenanceCLIStream, vfs.UsageProvenanceCLIStream},     // unknown cur → take known incoming
	}
	for _, c := range cases {
		if got := mergeProvenance(c.cur, c.incoming); got != c.want {
			t.Errorf("mergeProvenance(%q,%q)=%q want %q", c.cur, c.incoming, got, c.want)
		}
	}
}

// -----------------------------------------------------------------------------
// 66-6-UNIT-002 (AC1/AC5/AC4-kill/AC6): a long CLI step accumulates mid-stream
// usage + tool calls WHILE running (no done), and a kill folds the accumulation
// into TokensUsed on disk.
// -----------------------------------------------------------------------------
func TestATDD_66_6_UNIT_002_MidStreamVisibleThenKillFolds(t *testing.T) {
	reached := make(chan struct{})
	f := &usageLLMFile{
		driverType: llm.DriverClaudeCLI,
		reached:    reached,
		park:       true,
		events: []map[string]any{
			usageEvt(100, 80, 20),
			usageEvt(100, 80, 20),
			// one CLI tool lifecycle → DriverToolCall flush → ToolCallCount++
			{"type": "tool_call", "content": "started", "tool": "Bash", "call_id": "c1", "command": "ls"},
			{"type": "tool_call", "content": "completed", "call_id": "c1", "result": "files"},
		},
	}
	k, baseDir := newInterruptKernel(t, f)

	pid, err := k.Spawn("66.6 mid-stream", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	<-reached

	// AC1 + AC2 + AC5: mid-flight preview via GetProcInfo (no done reached yet).
	info, err := k.GetProcInfo(pid)
	if err != nil {
		t.Fatalf("GetProcInfo: %v", err)
	}
	if info.TokensUsed != 200 {
		t.Errorf("AC1 FAIL: 中途 TokensUsed=%d want 200 (StreamTokensUsed 合成失败)", info.TokensUsed)
	}
	if info.UsageProvenance != vfs.UsageProvenanceCLIStream {
		t.Errorf("AC2 FAIL: UsageProvenance=%q want cli_stream", info.UsageProvenance)
	}
	if info.ToolCallCount != 1 {
		t.Errorf("AC5 FAIL: ToolCallCount=%d want 1 (CLI DriverToolCall flush 未计数)", info.ToolCallCount)
	}

	// AC4-kill + AC6: kill mid-step → finishProcess folds StreamTokensUsed into
	// TokensUsed; proc-info.json (the immune / audit source) shows the real value.
	if err := k.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitDone(t, proc)

	stepDir := resolveStepDir(k, proc)
	if tokens, ok := readProcInfoField(t, stepDir, "tokens_used").(float64); !ok || int(tokens) != 200 {
		t.Errorf("AC4-kill FAIL: proc-info tokens_used=%v want 200 (被杀并入未生效)", readProcInfoField(t, stepDir, "tokens_used"))
	}
	if prov := readProcInfoField(t, stepDir, "usage_provenance"); prov != vfs.UsageProvenanceCLIStream {
		t.Errorf("AC2 FAIL: proc-info usage_provenance=%v want cli_stream", prov)
	}

	// AC6: immune Finalize is fed proc.TokensUsed at OnProcessExit; assert the
	// in-memory value the immune daemon would have received is the folded 200.
	proc.mu.Lock()
	finalTokens := proc.TokensUsed
	streamRemainder := proc.StreamTokensUsed
	proc.mu.Unlock()
	if finalTokens != 200 {
		t.Errorf("AC6 FAIL: proc.TokensUsed=%d want 200 (immune Finalize 输入失真)", finalTokens)
	}
	if streamRemainder != 0 {
		t.Errorf("AC4 FAIL: StreamTokensUsed=%d want 0 (并入后未清零)", streamRemainder)
	}
}

// -----------------------------------------------------------------------------
// 66-6-UNIT-003 (AC4 对账不双计): a normally-completed step uses the final
// response as authoritative; the mid-stream 2×100 is DISCARDED, not added.
// -----------------------------------------------------------------------------
func TestATDD_66_6_UNIT_003_NoDoubleCountOnNormalCompletion(t *testing.T) {
	f := &usageLLMFile{
		driverType: llm.DriverClaudeCLI,
		events: []map[string]any{
			usageEvt(100, 80, 20),
			usageEvt(100, 80, 20),
		},
		resp: makeCompleteResponse("done", 250),
	}
	k, baseDir := newInterruptKernel(t, f)

	pid, err := k.Spawn("66.6 reconcile", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code != 0 {
		t.Fatalf("expected clean completion, got code=%d reason=%q", exit.Code, exit.Reason)
	}

	proc.mu.Lock()
	tokens := proc.TokensUsed
	stream := proc.StreamTokensUsed
	prov := proc.UsageProvenance
	proc.mu.Unlock()

	if tokens != 250 {
		t.Errorf("AC4 FAIL: TokensUsed=%d want 250 (中途 200 应被权威覆盖丢弃，非 450 双计)", tokens)
	}
	if stream != 0 {
		t.Errorf("AC4 FAIL: StreamTokensUsed=%d want 0 (step 边界未清零)", stream)
	}
	if prov != vfs.UsageProvenanceCLIStream {
		t.Errorf("AC2 FAIL: UsageProvenance=%q want cli_stream", prov)
	}
}
