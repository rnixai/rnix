package kernel

// ATDD for Story 74.2 — process-level four-way cumulative token spend.
//
// Covered ACs:
//   AC1-1 多步累加正确: 权威点累计四维, 进程退出后 = Σ 各步对应字段
//   AC1-2 mid-stream 不进四维 (NFR4 核心负断言): AddStreamUsage delta 只进
//     StreamTokensUsed 预览, 四维仍 0
//   AC1-3 kill 路径降级: step 中途 kill → finishProcess 折叠 StreamTokensUsed
//     → TokensUsed, 四维停在最后权威步 (不造数)
//   AC1-4 逐字段独立累加 + 与 TokensUsed 不闭合: CumInput + CumOutput 不强等于
//     TokensUsed (OpenAI 系 input 含 cached / Anthropic 系不含)
//   AC2-1 procInfoDisk round-trip: 四维逐字段一致 + 旧 JSON 无字段 → 0
//   AC2-2 checkpoint round-trip: buildCheckpointData 四维正确 + 旧 checkpoint → 0
//   AC2-3 resumeFromHistory 装配: disk resume 后四维 = 落盘值
//   AC2-4 LoadSuspendedFromDisk 装配: 第三条恢复路径 (epic 漏列, create-story 修正 3)
//   AC2-5 SaveProcInfo 全链路: 真实进程落盘 JSON 含 cum_*_tokens
//
// Harness: reuses newInterruptKernel / interruptSpawnOpts / usageLLMFile /
// waitDone / resolveStepDir / readProcInfoField from atdd_66_2 / atdd_66_6 /
// atdd_56_7, and registerMockTool / sequenceLLMFile from kernel_test.go /
// atdd_26_4.
//
// RED proof: this file references vfs.ProcInfo.Cum*Tokens / procInfoDisk /
// CheckpointProcState four fields which do not exist yet — fails to compile
// until T1/T2 land.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// makeTokenSpendToolCallResponse is a token-spend-carrying response + a native
// tool call so the process keeps looping (text is a terminal action in
// reasonStep). TokensUsed = Input + Output (Story 74-1 AC1-5 discipline).
func makeTokenSpendToolCallResponse(toolName string, toolInput map[string]any, input, cached, creation, output int) []byte {
	resp := llmResponse{
		TokensUsed:               input + output,
		InputTokens:              input,
		CachedInputTokens:        cached,
		CacheCreationInputTokens: creation,
		OutputTokens:             output,
		ToolCalls: []llmToolCall{{
			ID:    "call_" + toolName,
			Name:  toolName,
			Input: toolInput,
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

// tokenSpendSeqKernel is replaced by inline setup in each test (registerMockTool
// needs the local reg); kept out to avoid an unused-helper lint.

// -----------------------------------------------------------------------------
// AC1-1 / AC1-4: three tool-call steps accumulate {input, cached, creation,
// output} per field; TokensUsed = Σ(input+output) + complete's standalone
// tokens — CumInput + CumOutput must NOT be forced to close with TokensUsed.
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC1_001_MultiStepAccumulation(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeTokenSpendToolCallResponse("/dev/tools/echo", map[string]any{"msg": "a"}, 100, 50, 20, 30),
			makeTokenSpendToolCallResponse("/dev/tools/echo", map[string]any{"msg": "b"}, 200, 100, 40, 60),
			makeTokenSpendToolCallResponse("/dev/tools/echo", map[string]any{"msg": "c"}, 300, 150, 60, 90),
			makeCompleteResponse("done", 5),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echo-result")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("74.2 accumulate", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for completion")
	}

	proc.mu.Lock()
	in, cached, creation, output := proc.CumInputTokens, proc.CumCachedInputTokens, proc.CumCacheCreationInputTokens, proc.CumOutputTokens
	tokens := proc.TokensUsed
	proc.mu.Unlock()

	if in != 600 {
		t.Errorf("AC1 FAIL: CumInputTokens=%d want 600 (Σ 100+200+300)", in)
	}
	if cached != 300 {
		t.Errorf("AC1 FAIL: CumCachedInputTokens=%d want 300 (Σ 50+100+150)", cached)
	}
	if creation != 120 {
		t.Errorf("AC1 FAIL: CumCacheCreationInputTokens=%d want 120 (Σ 20+40+60)", creation)
	}
	if output != 180 {
		t.Errorf("AC1 FAIL: CumOutputTokens=%d want 180 (Σ 30+60+90)", output)
	}
	// AC1-4 逐字段独立累加: 每维 = Σ 各步对应字段 — the per-field asserts above
	// already pin this (not derived from a merged value).
	//
	// AC1-4 不闭合: complete step carries TokensUsed=5 with zero input/output,
	// so CumInput+CumOutput (780) must NOT be forced to equal TokensUsed (785).
	if tokens != 785 {
		t.Errorf("AC1 FAIL: TokensUsed=%d want 785 (130+260+390+5)", tokens)
	}
	if in+output == tokens {
		t.Errorf("AC1 FAIL: CumInput+CumOutput=%d accidentally closes with TokensUsed=%d; "+
			"跨 driver 不要求闭合, 只做忠实累加", in+output, tokens)
	}
}

// -----------------------------------------------------------------------------
// AC1-2 (NFR4 核心负断言): CLI mid-stream deltas feed StreamTokensUsed preview
// only — the four cumulatives stay 0 while the step is in flight.
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC1_002_MidStreamNotInCumulative(t *testing.T) {
	f := &usageLLMFile{
		driverType: llm.DriverClaudeCLI,
		park:       true,
		reached:    make(chan struct{}),
		events: []map[string]any{
			usageEvt(100, 80, 20),
			usageEvt(100, 80, 20),
		},
	}
	k, baseDir := newInterruptKernel(t, f)

	pid, err := k.Spawn("74.2 mid-stream", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	<-f.reached

	info, err := k.GetProcInfo(pid)
	if err != nil {
		t.Fatalf("GetProcInfo: %v", err)
	}
	// 66.6 预览语义: TokensUsed 合成 mid-stream 增长…
	if info.TokensUsed != 200 {
		t.Errorf("preview FAIL: TokensUsed=%d want 200 (StreamTokensUsed 合成)", info.TokensUsed)
	}
	// …但四维如实停在最后权威步 (= 0, 尚无权威 resp)。
	if info.CumInputTokens != 0 || info.CumCachedInputTokens != 0 ||
		info.CumCacheCreationInputTokens != 0 || info.CumOutputTokens != 0 {
		t.Errorf("AC1-2 FAIL: mid-stream delta leaked into cumulatives: %+v", info)
	}
	_ = k.Kill(pid, types.SIGTERM)
	waitDone(t, proc)
}

// -----------------------------------------------------------------------------
// AC1-3 (kill 降级): finishProcess folds StreamTokensUsed into TokensUsed but
// has no mid-stream decomposition to add to the cumulatives — they stay at the
// last authoritative step (here: 0, no step ever completed). No fabrication.
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC1_003_KillStopsAtLastAuthoritativeStep(t *testing.T) {
	f := &usageLLMFile{
		driverType: llm.DriverClaudeCLI,
		park:       true,
		reached:    make(chan struct{}),
		events: []map[string]any{
			usageEvt(100, 80, 20),
			usageEvt(100, 80, 20),
		},
	}
	k, baseDir := newInterruptKernel(t, f)

	pid, err := k.Spawn("74.2 kill", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	<-f.reached

	if err := k.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitDone(t, proc)

	proc.mu.Lock()
	tokens, in, cached, creation, output := proc.TokensUsed, proc.CumInputTokens, proc.CumCachedInputTokens, proc.CumCacheCreationInputTokens, proc.CumOutputTokens
	stream := proc.StreamTokensUsed
	proc.mu.Unlock()
	if tokens != 200 {
		t.Errorf("AC1-3 FAIL: TokensUsed=%d want 200 (kill 折叠 StreamTokensUsed)", tokens)
	}
	if stream != 0 {
		t.Errorf("AC1-3 FAIL: StreamTokensUsed=%d want 0 (折叠后清零)", stream)
	}
	if in != 0 || cached != 0 || creation != 0 || output != 0 {
		t.Errorf("AC1-3 FAIL: cumulatives fabricated mid-stream data: %d/%d/%d/%d", in, cached, creation, output)
	}
	// 落盘侧同形: tokens_used=200, cum_* 字段不存在 (0 → omitempty)。
	stepDir := resolveStepDir(k, proc)
	if tokens, ok := readProcInfoField(t, stepDir, "tokens_used").(float64); !ok || int(tokens) != 200 {
		t.Errorf("AC1-3 FAIL: proc-info tokens_used=%v want 200", readProcInfoField(t, stepDir, "tokens_used"))
	}
	for _, field := range []string{"cum_input_tokens", "cum_cached_input_tokens", "cum_cache_creation_input_tokens", "cum_output_tokens"} {
		if v := readProcInfoField(t, stepDir, field); v != nil {
			t.Errorf("AC1-3 FAIL: proc-info %s present (%v); kill 路径不得造数", field, v)
		}
	}
}

// -----------------------------------------------------------------------------
// AC2-1: procInfoDisk round-trip carries the four dimensions; a legacy JSON
// without them reads back as 0 (omitempty, NFR5).
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC2_001_ProcInfoDiskRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:                         41,
		UUID:                        "uuid-74-2-disk",
		State:                       types.StateDead,
		Intent:                      "x",
		TokensUsed:                  777,
		CumInputTokens:              600,
		CumCachedInputTokens:        300,
		CumCacheCreationInputTokens: 120,
		CumOutputTokens:             180,
	}
	back := procInfoFromDisk(procInfoToDisk(info))
	if back.CumInputTokens != 600 || back.CumCachedInputTokens != 300 ||
		back.CumCacheCreationInputTokens != 120 || back.CumOutputTokens != 180 {
		t.Errorf("AC2-1 FAIL: disk round-trip = %d/%d/%d/%d want 600/300/120/180",
			back.CumInputTokens, back.CumCachedInputTokens, back.CumCacheCreationInputTokens, back.CumOutputTokens)
	}

	// Legacy JSON without the four keys → 0.
	legacy := `{"pid":42,"uuid":"legacy","state":"dead","intent":"y","tokens_used":10}`
	var d procInfoDisk
	if err := json.Unmarshal([]byte(legacy), &d); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	backLegacy := procInfoFromDisk(d)
	if backLegacy.CumInputTokens != 0 || backLegacy.CumCachedInputTokens != 0 ||
		backLegacy.CumCacheCreationInputTokens != 0 || backLegacy.CumOutputTokens != 0 {
		t.Errorf("AC2-1 FAIL: legacy disk → cumulatives %d/%d/%d/%d, want 0/0/0/0",
			backLegacy.CumInputTokens, backLegacy.CumCachedInputTokens, backLegacy.CumCacheCreationInputTokens, backLegacy.CumOutputTokens)
	}
}

// -----------------------------------------------------------------------------
// AC2-2: checkpoint snapshot carries the four dimensions; a legacy checkpoint
// JSON without them resumes with 0.
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC2_002_CheckpointRoundTrip(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	proc := &Process{PID: 7, TokensUsed: 500}
	proc.CumInputTokens = 400
	proc.CumCachedInputTokens = 200
	proc.CumCacheCreationInputTokens = 80
	proc.CumOutputTokens = 100
	cp := buildCheckpointData(proc, 5, json.RawMessage(`{"system_prompt":"p","messages":[],"max_size":0}`), 0)
	if cp.ProcState.CumInputTokens != 400 || cp.ProcState.CumCachedInputTokens != 200 ||
		cp.ProcState.CumCacheCreationInputTokens != 80 || cp.ProcState.CumOutputTokens != 100 {
		t.Fatalf("AC2-2 FAIL: buildCheckpointData = %d/%d/%d/%d want 400/200/80/100",
			cp.ProcState.CumInputTokens, cp.ProcState.CumCachedInputTokens,
			cp.ProcState.CumCacheCreationInputTokens, cp.ProcState.CumOutputTokens)
	}

	// Legacy checkpoint without the four keys → resumed process reads 0.
	uuid := uuidForTest("ckpt742")
	stepsDir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := `{"version":` + versionLiteral() + `,"uuid":"` + uuid + `","last_step":3,` +
		`"timestamp":"2026-08-07T00:00:00Z","context_snapshot":{"system_prompt":"p","messages":[],"max_size":0},` +
		`"proc_state":{"pid":99,"provider":"claude","model":"claude-4","skills":[],"allowed_devices":[],"intent":"legacy","max_steps":0,"used_tokens":50,"env_snapshot":{}}}`
	if err := os.WriteFile(filepath.Join(stepsDir, "checkpoint.json"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy checkpoint: %v", err)
	}

	// steps.jsonl so the disk path can be skipped (checkpoint exists → routed).
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), []byte(`{"step":1,"messages":[]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}

	// buildCheckpointData above already proves snapshotting; the resume side is
	// covered by AC2-3 (disk) + the checkpoint branch asserts restore below via
	// a fresh kernel.Resume. Mock LLM completes immediately → process dies fast,
	// so grab the Process before it reaps and read fields under mu.
	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	proc2, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	proc2.mu.Lock()
	in, cached, creation, output := proc2.CumInputTokens, proc2.CumCachedInputTokens, proc2.CumCacheCreationInputTokens, proc2.CumOutputTokens
	proc2.mu.Unlock()
	if in != 0 || cached != 0 || creation != 0 || output != 0 {
		t.Errorf("AC2-2 FAIL: legacy checkpoint resumed cumulatives %d/%d/%d/%d, want 0/0/0/0",
			in, cached, creation, output)
	}
	// The resumed process exits immediately (mock LLM completes); wait for its
	// proc-info.json so the reaper is done before TempDir cleanup runs.
	waitForProcInfoWrite(t, k, proc2)
}

// waitForProcInfoWrite polls for the process's proc-info.json (written by
// finishProcess / reapProcess asynchronously after Done) so tests do not race
// the reaper during TempDir cleanup.
func waitForProcInfoWrite(t *testing.T, k *KernelImpl, proc *Process) {
	t.Helper()
	path := filepath.Join(k.ResolveStepBaseDir(proc), "steps", proc.UUID, "proc-info.json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("proc-info.json not written within 5s: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// versionLiteral renders the current CheckpointVersion as a Go string literal
// for hand-built legacy JSON fixtures.
func versionLiteral() string {
	return "1"
}

// -----------------------------------------------------------------------------
// AC2-3: resumeFromHistory (disk path) restores the four dimensions from
// proc-info.json — a real装配 assertion, not just serialization.
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC2_003_ResumeFromHistoryRestoresFour(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := uuidForTest("res742")

	info := vfs.ProcInfo{
		PID:                         12,
		UUID:                        uuid,
		PPID:                        0,
		State:                       types.StateSuspended,
		Intent:                      "74.2 disk resume",
		Provider:                    "claude",
		Model:                       "claude-4",
		CreatedAt:                   time.Now().Add(-time.Hour),
		CtxID:                       1,
		TokensUsed:                  500,
		CumInputTokens:              400,
		CumCachedInputTokens:        200,
		CumCacheCreationInputTokens: 80,
		CumOutputTokens:             100,
		SuspendReason:               "user_paused",
	}
	writeProcInfoFixture74_2(t, baseDir, uuid, info)

	result, err := k.Resume(uuid)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatal("resumed process not in procTable")
	}
	proc.mu.Lock()
	in, cached, creation, output := proc.CumInputTokens, proc.CumCachedInputTokens, proc.CumCacheCreationInputTokens, proc.CumOutputTokens
	tokens := proc.TokensUsed
	proc.mu.Unlock()
	if tokens != 500 {
		t.Errorf("AC2-3 FAIL: TokensUsed=%d want 500 (disk 恢复)", tokens)
	}
	if in != 400 || cached != 200 || creation != 80 || output != 100 {
		t.Errorf("AC2-3 FAIL: disk resume cumulatives %d/%d/%d/%d want 400/200/80/100",
			in, cached, creation, output)
	}
	// Resumed process exits immediately (mock LLM completes); let the reaper
	// finish before TempDir cleanup.
	waitForProcInfoWrite(t, k, proc)
}

// -----------------------------------------------------------------------------
// AC2-4: LoadSuspendedFromDisk — the daemon-restart placeholder path (epic 漏列,
// create-story 修正 3) must restore the four dimensions too, or a
// suspend → daemon restart → resume sequence zeroes them.
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC2_004_LoadSuspendedRestoresFour(t *testing.T) {
	k, baseDir := newReloadKernel(t)
	uuid := uuidForTest("susp742")

	info := vfs.ProcInfo{
		PID:                         21,
		UUID:                        uuid,
		PPID:                        0,
		State:                       types.StateSuspended,
		Intent:                      "74.2 suspended placeholder",
		Provider:                    "claude",
		Model:                       "claude-4",
		CreatedAt:                   time.Now().Add(-time.Hour),
		CtxID:                       1,
		TokensUsed:                  250,
		CumInputTokens:              200,
		CumCachedInputTokens:        100,
		CumCacheCreationInputTokens: 40,
		CumOutputTokens:             50,
		SuspendReason:               "user_paused",
		IsPaused:                    true,
	}
	writeProcInfoFixture74_2(t, baseDir, uuid, info)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded < 1 {
		t.Fatalf("loaded = %d, want >= 1", loaded)
	}
	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID %s not loaded", uuid)
	}
	proc.mu.Lock()
	in, cached, creation, output := proc.CumInputTokens, proc.CumCachedInputTokens, proc.CumCacheCreationInputTokens, proc.CumOutputTokens
	proc.mu.Unlock()
	if in != 200 || cached != 100 || creation != 40 || output != 50 {
		t.Errorf("AC2-4 FAIL: LoadSuspended cumulatives %d/%d/%d/%d want 200/100/40/50",
			in, cached, creation, output)
	}
}

// writeProcInfoFixture74_2 writes a proc-info.json (via the real
// procInfoToDisk, so the four fields ride along) + minimal companion files so
// Resume / LoadSuspendedFromDisk can parse the entry.
func writeProcInfoFixture74_2(t *testing.T, baseDir, uuid string, info vfs.ProcInfo) {
	t.Helper()
	dir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.MarshalIndent(procInfoToDisk(info), "", "  ")
	if err != nil {
		t.Fatalf("marshal procInfoDisk: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), data, 0o644); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), []byte(`{"step":1,"messages":[]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
	meta := map[string]any{"system_prompt": "You are a resumed test agent.", "tools": []any{}}
	mb, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "process-meta.json"), mb, 0o644); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}
}

// -----------------------------------------------------------------------------
// AC2-5: SaveProcInfo full chain — a real process that completed steps lands
// cum_*_tokens on disk with the authoritative values.
// -----------------------------------------------------------------------------
func TestATDD_74_2_AC2_005_SaveProcInfoFullChain(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeTokenSpendToolCallResponse("/dev/tools/echo", map[string]any{"msg": "a"}, 100, 50, 20, 30),
			makeCompleteResponse("done", 5),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echo-result")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()
	// Set a dataDir (no ProjectConfig on this Spawn → the "global" bucket) so
	// finishProcess's SaveProcInfo actually lands on disk.
	k.dataDir = t.TempDir()

	pid, err := k.Spawn("74.2 persist", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}

	// proc-info.json lands under ResolveStepBaseDir (no ProjectConfig on this
	// Spawn → the "global" project bucket), written asynchronously by
	// finishProcess / the reaper — poll instead of racing them.
	waitForProcInfoWrite(t, k, proc)
	raw := readProcInfoFromDisk44_3(t, k.ResolveStepBaseDir(proc), proc.UUID)
	checks := []struct {
		key  string
		want float64
	}{
		{"cum_input_tokens", 100},
		{"cum_cached_input_tokens", 50},
		{"cum_cache_creation_input_tokens", 20},
		{"cum_output_tokens", 30},
	}
	for _, c := range checks {
		got, ok := raw[c.key].(float64)
		if !ok || got != c.want {
			t.Errorf("AC2-5 FAIL: proc-info %s = %v, want %v", c.key, raw[c.key], c.want)
		}
	}
}
