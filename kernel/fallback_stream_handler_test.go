package kernel

import (
	gocontext "context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// Regression: attemptFallback must wire the driver stream handler onto the
// fallback FD, so that a long-running fallback call's stream events refresh
// proc.LastHeartbeat (via TouchHeartbeat) and do NOT trigger spurious
// HeartbeatMonitor stall warnings.
//
// Root cause (investigation apex-pid517-llm-gateway-stall): setupDriverStreamHandler
// was wired only on the primary llmFD at spawn/subtree/resume time. A fallback
// call that streamed events for ~27min left LastHeartbeat frozen and was falsely
// reported stalled 39 times, even though it eventually succeeded.
//
// Spec: spec-fallback-fd-stream-handler-heartbeat.md
// ============================================================================

// streamingMockLLMFile is a fallback LLM device that implements vfs.StreamObserver.
// When the kernel registers a stream handler on it and then issues a Write, the
// file invokes the handler with a driver event — mirroring how a real streaming
// CLI/API driver emits events during a call. This lets the test observe whether
// the fallback FD received a handler and whether that handler refreshes the
// process heartbeat.
type streamingMockLLMFile struct {
	mu          sync.Mutex
	readData    []byte
	closed      bool
	handler     func(event map[string]any)
	handlerSet  atomic.Bool
	emitOnWrite bool   // when true, Write fires the handler once (simulates a stream event)
	toolName    string // when non-empty, the Write-fired event is a tool_call for this tool
}

func (f *streamingMockLLMFile) Write(_ gocontext.Context, data []byte) error {
	f.mu.Lock()
	h := f.handler
	emit := f.emitOnWrite
	tool := f.toolName
	f.mu.Unlock()
	if emit && h != nil {
		// Simulate a driver stream event flowing during the call. When toolName
		// is set, emit a tool_call "started" event — the shape the full
		// setupDriverStreamHandler would turn into a driver step-record and a
		// nativeToolDefs merge. A heartbeat-only handler must ignore all of that.
		if tool != "" {
			h(map[string]any{"type": "tool_call", "content": "started", "tool": tool})
		} else {
			h(map[string]any{"type": "assistant", "content": "streaming..."})
		}
	}
	return nil
}

func (f *streamingMockLLMFile) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readData, nil
}

func (f *streamingMockLLMFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *streamingMockLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *streamingMockLLMFile) SupportsToolCalling() bool { return true }

// SetStreamHandler implements vfs.StreamObserver. Records that a handler was
// attached so the test can assert the fallback FD was wired.
func (f *streamingMockLLMFile) SetStreamHandler(fn func(event map[string]any)) {
	f.mu.Lock()
	f.handler = fn
	f.mu.Unlock()
	f.handlerSet.Store(true)
}

// TestFallbackFD_StreamHandlerRefreshesHeartbeat is the core regression test:
// the fallback FD must receive a stream handler whose events refresh the
// process heartbeat. (Spec AC1)
func TestFallbackFD_StreamHandlerRefreshesHeartbeat(t *testing.T) {
	// Primary fails transiently-but-non-retryably so reasonStep enters fallback.
	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 500, fmt.Errorf("server error")),
	}
	// Fallback is a streaming device: on Write it fires the registered handler,
	// then returns a completing response on Read.
	fallbackFile := &streamingMockLLMFile{
		readData:    makeLLMResponse("fallback ok", 10),
		emitOnWrite: true,
	}

	k := newStreamingFallbackKernel(t, primaryFile, fallbackFile, "ollama", "claude")
	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test fallback fd stream handler", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected fallback success (exit 0), got %d: %s (err: %v)",
				exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion")
	}

	// AC1 core: the fallback FD must have had a stream handler attached.
	if !fallbackFile.handlerSet.Load() {
		t.Fatal("fallback FD did not receive a stream handler — attemptFallback " +
			"is missing setupDriverStreamHandler (regression)")
	}

	// AC1 behavior: the handler fired during the fallback Write must have
	// advanced LastHeartbeat past the zero/spawn value (proving TouchHeartbeat
	// is reachable through the fallback FD's handler).
	proc.mu.Lock()
	lastHB := proc.LastHeartbeat
	proc.mu.Unlock()
	if lastHB.IsZero() {
		t.Error("LastHeartbeat is zero — fallback stream event did not refresh heartbeat")
	}
}

// TestFallbackFD_NonStreamObserver_NoPanic covers the edge case where the
// fallback device does not implement vfs.StreamObserver: setupDriverStreamHandler
// must silently skip and the fallback must still succeed. (Spec AC2 / I/O matrix #2)
func TestFallbackFD_NonStreamObserver_NoPanic(t *testing.T) {
	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 500, fmt.Errorf("server error")),
	}
	// Plain mockLLMFile does NOT implement SetStreamHandler.
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("plain fallback ok", 10),
	}

	k := newFallbackTestKernel(t, primaryFile, fallbackFile, "ollama", "claude")
	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test fallback fd non-stream-observer", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected fallback success even without StreamObserver, got %d: %s",
				exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.Result != "plain fallback ok" {
		t.Errorf("expected 'plain fallback ok', got %q", proc.Result)
	}
}

// TestFallbackFD_HeartbeatOnly_NoToolDefMerge verifies the fallback handler is
// heartbeat-only: a tool_call event streamed during the fallback must NOT be
// merged into proc.nativeToolDefs (that is a side effect of the full
// setupDriverStreamHandler, which we deliberately do not reuse — it would also
// collide driver step-record numbering in steps.jsonl). (Spec AC3 / I/O matrix #4)
func TestFallbackFD_HeartbeatOnly_NoToolDefMerge(t *testing.T) {
	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("ollama", 500, fmt.Errorf("server error")),
	}
	// Fallback streams a tool_call "started" event for a uniquely-named tool,
	// then returns a completing response.
	const fallbackToolName = "fallback-only-phantom-tool"
	fallbackFile := &streamingMockLLMFile{
		readData:    makeLLMResponse("fallback ok", 10),
		emitOnWrite: true,
		toolName:    fallbackToolName,
	}

	k := newStreamingFallbackKernel(t, primaryFile, fallbackFile, "ollama", "claude")
	agent := fallbackAgentInfo("ollama", "llama3", "haiku", "claude")

	pid, err := k.Spawn("test fallback heartbeat-only no tooldef merge", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected fallback success, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// The heartbeat-only handler must still have fired (heartbeat refreshed)...
	if !fallbackFile.handlerSet.Load() {
		t.Fatal("fallback FD did not receive a stream handler (regression)")
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	// ...but the fallback's tool_call must NOT have leaked into nativeToolDefs.
	for _, td := range proc.nativeToolDefs {
		if td.Name == fallbackToolName {
			t.Errorf("fallback tool %q leaked into proc.nativeToolDefs — handler is "+
				"not heartbeat-only (regression: full setupDriverStreamHandler reused)", fallbackToolName)
		}
	}
}

// newStreamingFallbackKernel mirrors newFallbackTestKernel but registers a
// streamingMockLLMFile (vfs.StreamObserver) as the fallback device.
func newStreamingFallbackKernel(t testing.TB, primaryFile *mockLLMFile, fallbackFile *streamingMockLLMFile, primaryName, fallbackName string) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/"+primaryName, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return primaryFile, nil
	})
	_ = reg.Register("/dev/llm/"+fallbackName, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return fallbackFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	providerSet := map[string]bool{primaryName: true, fallbackName: true}
	k.SetProviderResolver(
		func() []string {
			names := make([]string, 0, len(providerSet))
			for n := range providerSet {
				names = append(names, n)
			}
			return names
		},
		func(name string) bool { return providerSet[name] },
	)
	return k
}
