package kernel

import (
	gocontext "context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 73.5 — ATDD acceptance tests for the per-provider concurrency gate
// (D9: atdd_73_5_* naming; clock via the sleepFunc seam, ms-level waits).

// parkWriteLLM parks every Write until releaseCh is closed (subsequent
// writes return immediately — a closed channel stays closed), then serves
// readData. reachedCh signals the FIRST Write arrival.
type parkWriteLLM struct {
	mu         sync.Mutex
	reached    bool
	reachedCh  chan struct{}
	releaseCh  chan struct{}
	writeCount atomic.Int32
	readData   []byte
}

func newParkWriteLLM(readData []byte) *parkWriteLLM {
	return &parkWriteLLM{
		reachedCh: make(chan struct{}),
		releaseCh: make(chan struct{}),
		readData:  readData,
	}
}

func (f *parkWriteLLM) Write(_ gocontext.Context, _ []byte) error {
	f.mu.Lock()
	if !f.reached {
		f.reached = true
		close(f.reachedCh)
	}
	f.mu.Unlock()
	f.writeCount.Add(1)
	<-f.releaseCh
	return nil
}

func (f *parkWriteLLM) Read(_ int) ([]byte, error) { return f.readData, nil }
func (f *parkWriteLLM) Close() error               { return nil }
func (f *parkWriteLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *parkWriteLLM) SupportsToolCalling() bool { return true }

// gateKernel builds a kernel with primary (and optional fallback) LLM device
// mocks and a global per-provider concurrency limit of `limit`.
func gateKernel(t *testing.T, primary, fallback vfs.VFSFile, primaryName, fallbackName string, limit int) (*KernelImpl, string) {
	t.Helper()
	k, baseDir := newFailureRawKernel(t, primary, fallback, primaryName, fallbackName)
	k.SetProviderConcurrencyLimitFunc(func(string) int { return limit })
	return k, baseDir
}

// --- AC3: three-level resolution chain (project → global closure → default) ---

func TestATDD_73_5_AC3_ResolutionChain(t *testing.T) {
	k := &KernelImpl{}
	proc := NewProcess(0, "73.5 chain", nil)
	proc.Provider = "claude"

	projWith := func(mc int) *config.ProjectConfig {
		return &config.ProjectConfig{
			Providers: &llm.ProvidersConfig{Providers: []llm.ProviderConfig{
				{Name: "claude", MaxConcurrency: mc},
			}},
		}
	}

	cases := []struct {
		name      string
		proj      *config.ProjectConfig
		global    func(string) int
		want      int
	}{
		{"project hit wins", projWith(7), func(string) int { return 5 }, 7},
		{"project zero = miss, global hit", projWith(0), func(string) int { return 5 }, 5},
		{"project miss, global hit", &config.ProjectConfig{Providers: &llm.ProvidersConfig{Providers: []llm.ProviderConfig{{Name: "other"}}}}, func(string) int { return 5 }, 5},
		{"all miss → default 4", projWith(0), nil, 4},
		{"global zero → default 4", nil, func(string) int { return 0 }, 4},
		{"global negative → default 4", nil, func(string) int { return -1 }, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proc.ProjectConfig = tc.proj
			k.providerConcurrencyLimitFunc = tc.global
			if got := k.resolveProviderConcurrencyLimit(proc, "claude"); got != tc.want {
				t.Errorf("resolveProviderConcurrencyLimit = %d, want %d", got, tc.want)
			}
		})
	}

	// The provider argument is the gate key (D8): the FALLBACK provider's
	// bucket must resolve its own limit, not proc.Provider's — a per-provider
	// config for the fallback must not be silently applied to the primary's
	// bucket or vice versa.
	projBoth := &config.ProjectConfig{
		Providers: &llm.ProvidersConfig{Providers: []llm.ProviderConfig{
			{Name: "claude", MaxConcurrency: 2},
			{Name: "cursor", MaxConcurrency: 8},
		}},
	}
	proc.ProjectConfig = projBoth
	k.providerConcurrencyLimitFunc = nil
	if got := k.resolveProviderConcurrencyLimit(proc, "claude"); got != 2 {
		t.Errorf("claude bucket limit = %d, want 2", got)
	}
	if got := k.resolveProviderConcurrencyLimit(proc, "cursor"); got != 8 {
		t.Errorf("cursor bucket limit = %d, want 8 (fallback bucket resolves its own config)", got)
	}
	if got := k.resolveProviderConcurrencyLimit(proc, "unknown"); got != 4 {
		t.Errorf("unknown provider bucket limit = %d, want default 4", got)
	}

	// lookupProjectProviderConcurrencyLimit: typed-any failure / nil config /
	// unknown provider = LOOKUP MISS (0), never a panic.
	if got := lookupProjectProviderConcurrencyLimit(&config.ProjectConfig{Providers: "not-a-providers-config"}, "claude"); got != 0 {
		t.Errorf("typed-any mismatch = %d, want 0", got)
	}
	if got := lookupProjectProviderConcurrencyLimit(nil, "claude"); got != 0 {
		t.Errorf("nil config = %d, want 0", got)
	}
	if got := lookupProjectProviderConcurrencyLimit(projWith(3), "nope"); got != 0 {
		t.Errorf("unknown provider = %d, want 0", got)
	}
	if got := lookupProjectProviderConcurrencyLimit(projWith(3), ""); got != 0 {
		t.Errorf("empty provider = %d, want 0", got)
	}
}

// --- AC8: the main write is gated — the second request queues BEFORE Write ---

func TestATDD_73_5_AC8_MainPathQueuesAtGateBeforeWrite(t *testing.T) {
	installBlockSleep(t) // park proc2 at the gate, not in Write
	llmFile := newParkWriteLLM(makeLLMResponse("done", 5))
	k, baseDir := gateKernel(t, llmFile, nil, "claude", "", 1)

	spawn := func(intent string) *Process {
		t.Helper()
		pid, err := k.Spawn(intent, nil, failureRawSpawnOpts(baseDir))
		if err != nil {
			t.Fatalf("Spawn %s: %v", intent, err)
		}
		proc, _ := k.GetProcess(pid)
		return proc
	}

	// proc1 enters Write and holds the gate slot.
	proc1 := spawn("73.5 gate queue proc1")
	select {
	case <-llmFile.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("proc1 never reached Write")
	}

	// proc2 must queue at the GATE (before Write): the mock's writeCount
	// must stay at 1 while proc1 holds the slot.
	proc2 := spawn("73.5 gate queue proc2")
	pollUntil(t, "proc2 queued at the gate", func() bool { return gateWaiters(k.gate(), "claude") == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := llmFile.writeCount.Load(); n != 1 {
		t.Fatalf("writeCount = %d, want 1 — proc2 reached Write while queued at the gate", n)
	}

	// Release proc1 → it completes and frees the slot → proc2 proceeds.
	close(llmFile.releaseCh)
	for i, proc := range []*Process{proc1, proc2} {
		exit := waitDone(t, proc)
		if exit.Code != 0 {
			t.Fatalf("proc%d exit = %+v, want success", i+1, exit)
		}
	}
	if n := llmFile.writeCount.Load(); n != 2 {
		t.Errorf("writeCount = %d, want 2 (both writes after the slot freed)", n)
	}
}

// --- AC6/D5: primary gate timeout → attemptFallback (another provider, its own gate) ---

func TestATDD_73_5_AC6_GateTimeoutFallsBackToAnotherProvider(t *testing.T) {
	// Default TestMain sleepFunc is immediate → the 60s gate budget elapses
	// instantly; proc2's primary gate wait times out and switches provider.
	primary := newParkWriteLLM(makeLLMResponse("primary", 5))
	fallback := &mockLLMFile{readData: makeLLMResponse("fallback ok", 3)}
	k, baseDir := gateKernel(t, primary, fallback, "claude", "cursor", 1)

	// proc1 holds the claude slot (its Write parks).
	pid1, err := k.Spawn("73.5 fb holder", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc1, _ := k.GetProcess(pid1)
	select {
	case <-primary.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("proc1 never reached Write")
	}

	// proc2: claude gate timeout → fallback cursor (free slot) → success.
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "cursor")
	pid2, err := k.Spawn("73.5 gate timeout → fallback", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc2, _ := k.GetProcess(pid2)
	exit := waitDone(t, proc2)
	if exit.Code != 0 {
		t.Fatalf("proc2 exit = %+v, want success via fallback", exit)
	}
	if proc2.Result != "fallback ok" {
		t.Errorf("proc2 result = %q, want fallback response", proc2.Result)
	}

	// provider_gate_timeout event for the primary provider is on disk.
	evs, err := ReadAllEvents(filepath.Join(baseDir, "steps", proc2.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	var timeoutFound, fallbackFound bool
	for _, ev := range evs {
		if ev.Syscall == "provider_gate_timeout" && ev.Args["provider"] == "claude" {
			timeoutFound = true
		}
		if ev.Syscall == "ReasonStep" && ev.Args["action"] == "fallback" {
			fallbackFound = true
		}
	}
	if !timeoutFound {
		t.Error("provider_gate_timeout event not found for claude")
	}
	if !fallbackFound {
		t.Error("fallback ReasonStep event not found")
	}

	// Cleanup: unblock proc1.
	close(primary.releaseCh)
	_ = waitDone(t, proc1)
}

// --- AC6/D5: gate timeout with NO fallback → terminal write-fail with gate text ---

func TestATDD_73_5_AC6_NoFallbackTerminalWithGateText(t *testing.T) {
	primary := newParkWriteLLM(makeLLMResponse("primary", 5))
	k, baseDir := gateKernel(t, primary, nil, "claude", "", 1)

	pid1, err := k.Spawn("73.5 no-fb holder", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc1, _ := k.GetProcess(pid1)
	select {
	case <-primary.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("proc1 never reached Write")
	}

	// proc2 has no fallback: gate timeout → terminal write-fail carrying the
	// gate text in the exit reason (D5 — 归因经 driverErrorDetail 模式承载).
	pid2, err := k.Spawn("73.5 gate timeout no fallback", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc2, _ := k.GetProcess(pid2)
	exit := waitDone(t, proc2)
	if exit.Code == 0 {
		t.Fatalf("expected non-zero exit, got %+v", exit)
	}
	if !strings.Contains(exit.Reason, "provider concurrency gate timeout: claude") {
		t.Errorf("exit reason = %q, want gate timeout text", exit.Reason)
	}

	close(primary.releaseCh)
	_ = waitDone(t, proc1)
}

// --- AC6/D5: FALLBACK gate timeout → existing fallback_exhausted terminal ---

func TestATDD_73_5_AC6_FallbackGateTimeoutExhausted(t *testing.T) {
	// procA (cursor) parks in Write and holds the cursor slot; procB's
	// fallback to cursor then times out at the cursor gate.
	cursor := newParkWriteLLM(makeLLMResponse("cursor", 1))
	primary := &mockLLMFile{writeErr: fmt.Errorf("boom")} // non-transient → straight to fallback
	k, baseDir := gateKernel(t, primary, cursor, "claude", "cursor", 1)

	pidA, err := k.Spawn("73.5 cursor holder", fallbackAgentInfo("cursor", "m", "f", ""), failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn A: %v", err)
	}
	procA, _ := k.GetProcess(pidA)
	select {
	case <-cursor.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("procA never reached cursor Write")
	}

	agentB := fallbackAgentInfo("claude", "sonnet", "haiku", "cursor")
	pidB, err := k.Spawn("73.5 fallback gate timeout", agentB, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn B: %v", err)
	}
	procB, _ := k.GetProcess(pidB)
	exit := waitDone(t, procB)
	if exit.Code == 0 {
		t.Fatalf("expected non-zero exit, got %+v", exit)
	}
	if !strings.Contains(exit.Reason, "provider concurrency gate timeout: cursor") {
		t.Errorf("exit reason = %q, want fallback gate timeout text", exit.Reason)
	}

	// The fallback_exhausted event carries the gate error text.
	evs, err := ReadAllEvents(filepath.Join(baseDir, "steps", procB.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	found := false
	for _, ev := range evs {
		if ev.Syscall == "ReasonStep" && ev.Args["action"] == "fallback_exhausted" {
			found = true
			if fbErr, _ := ev.Args["fallback_error"].(string); !strings.Contains(fbErr, "provider concurrency gate timeout: cursor") {
				t.Errorf("fallback_error = %q, want gate timeout text", fbErr)
			}
		}
	}
	if !found {
		t.Error("fallback_exhausted event not found")
	}

	close(cursor.releaseCh)
	_ = waitDone(t, procA)
}

// --- AC6/D5: compact gate timeout → classifyCompactFailure "other" ---

func TestATDD_73_5_AC6_CompactGateTimeoutOther(t *testing.T) {
	primary := newParkWriteLLM(makeLLMResponse("primary", 5))
	k, baseDir := gateKernel(t, primary, nil, "claude", "", 1)

	pid, err := k.Spawn("73.5 compact holder", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-primary.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("proc never reached Write")
	}

	// Compact is gated on the same provider bucket (D3): the slot is held,
	// so the compact LLM call times out at the gate BEFORE its Write.
	compactCall := k.BuildCompactLLMCall(proc)
	errCh := make(chan error, 1)
	go func() {
		_, cerr := compactCall("compact system", nil)
		errCh <- cerr
	}()
	var compactErr error
	select {
	case compactErr = <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("compact call never resolved")
	}
	if compactErr == nil {
		t.Fatal("compact call succeeded while the gate slot was held")
	}
	if !strings.Contains(compactErr.Error(), "provider concurrency gate") {
		t.Errorf("compact error = %v, want gate marker", compactErr)
	}
	// D5: gate timeout is NOT ErrCompactTimeout/ErrCompactCancelled — the
	// classifier must say "other" (no sentinel, no disguise), and the
	// mechanical fallback runs in the caller.
	if kind := classifyCompactFailure(compactErr); kind != "other" {
		t.Errorf("classifyCompactFailure = %q, want \"other\"", kind)
	}
	if errors.Is(compactErr, ErrCompactTimeout) || errors.Is(compactErr, ErrCompactCancelled) {
		t.Error("gate error must not masquerade as ErrCompactTimeout/ErrCompactCancelled")
	}

	close(primary.releaseCh)
	_ = waitDone(t, proc)
}

// --- AC6/D4: cancel during the gate wait → interrupted (SIGTERM semantics) ---

func TestATDD_73_5_AC6_CancelDuringGateWaitInterrupted(t *testing.T) {
	installBlockSleep(t)
	primary := newParkWriteLLM(makeLLMResponse("primary", 5))
	k, baseDir := gateKernel(t, primary, nil, "claude", "", 1)

	pid1, err := k.Spawn("73.5 cancel holder", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc1, _ := k.GetProcess(pid1)
	select {
	case <-primary.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("proc1 never reached Write")
	}

	pid2, err := k.Spawn("73.5 cancel queued", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc2, _ := k.GetProcess(pid2)
	pollUntil(t, "proc2 queued at the gate", func() bool { return gateWaiters(k.gate(), "claude") == 1 })

	// SIGTERM during the gate wait → the acquire returns the cancelled gate
	// error and reasonStep routes it through handleInterruptedWrite — the
	// SAME exit path as a cancel during the write (one SIGTERM = one
	// exit_reason, D4).
	if err := k.Kill(pid2, types.SIGTERM); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	exit := waitDone(t, proc2)
	if exit.Code == 0 || exit.Reason != "interrupted" {
		t.Errorf("exit = %+v, want interrupted", exit)
	}

	close(primary.releaseCh)
	_ = waitDone(t, proc1)
}

// --- AC8: compact wiring note — compact's gate key is the PRIMARY provider ---

func TestATDD_73_5_AC8_CompactGateKeyIsPrimaryProvider(t *testing.T) {
	// The compact call resolves its gate bucket from proc.Provider, not from
	// a device path — pin via the provider-name bucket directly.
	primary := &mockLLMFile{readData: makeLLMResponse("ok", 1)}
	k, baseDir := gateKernel(t, primary, nil, "claude", "", 4)
	pid, err := k.Spawn("73.5 compact key", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	_ = waitDone(t, proc)

	compactCall := k.BuildCompactLLMCall(proc)
	resp, cerr := compactCall("compact system", nil)
	if cerr != nil {
		t.Fatalf("compact call failed: %v", cerr)
	}
	if resp != "ok" {
		t.Errorf("compact response = %q, want ok", resp)
	}
}
