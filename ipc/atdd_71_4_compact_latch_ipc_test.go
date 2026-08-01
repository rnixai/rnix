package ipc

import (
	"encoding/json"
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// Story 71.4 — IPC-layer coverage: the manual compact success path clears the
// latch (AC3), and the latch is visible to consumers over the wire (AC4-②).
// Kernel-side behaviour (classification, pairing, latch set/short-circuit) lives
// in kernel/atdd_71_4_failure_observability_latch_test.go.

// setupCompactIPCTest builds a server whose /dev/llm/claude device returns a
// canned compact summary, so handleCompact's ctxMgr.Compact succeeds. Returns
// the ctxMgr (setupResumeIPCTest does not) so tests can seed the process context.
func setupCompactIPCTest(t *testing.T) (*Client, *kernel.KernelImpl, *rnixctx.Manager) {
	t.Helper()
	summaryResp := `{"content":"<summary>manual compaction ok</summary>"}`
	llmFile := &mockLLMFile{readData: []byte(summaryResp)}
	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern
	srv.SetContextManager(ctxMgr)

	sockDir := t.TempDir()
	sockPath := sockDir + "/test.sock"
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	return client, kern, ctxMgr
}

// seedCompactableContext fills enough messages for ctxMgr.Compact to admit the
// call (minMessagesForCompact = 2).
func seedCompactableContext(t *testing.T, ctxMgr *rnixctx.Manager) types.CtxID {
	t.Helper()
	cid, err := ctxMgr.CtxAlloc(0)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	for i := range 4 {
		role := rnixctx.RoleUser
		if i%2 == 1 {
			role = rnixctx.RoleAssistant
		}
		if err := ctxMgr.AppendMessage(cid, role, strings.Repeat("compactable payload ", 50)); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	return cid
}

// TestATDD_71_4_AC3_ManualCompactBypassesLatch (F4): with the latch set, a
// manual IPC compact still runs and succeeds — the handler calls ctxMgr.Compact
// directly, never autoCompactIfNeeded, so the latch is structurally invisible to
// it. No `force` parameter was added to Compact() to achieve this.
func TestATDD_71_4_AC3_ManualCompactBypassesLatch(t *testing.T) {
	client, kern, ctxMgr := setupCompactIPCTest(t)
	cid := seedCompactableContext(t, ctxMgr)

	proc := kernel.NewProcess(0, "manual bypass", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/claude"
	_ = proc.Start()
	kern.AddProcess(proc)
	proc.SetCompactLatched(true)

	preTokens, _ := ctxMgr.TokenUsage(cid)
	resp, err := client.Compact(proc.PID, "")
	if err != nil {
		t.Fatalf("manual compact must succeed despite the latch (F4 bypass): %v", err)
	}
	if resp.PostTokens >= preTokens.Used {
		t.Errorf("manual compact did not reduce tokens: %d → %d", preTokens.Used, resp.PostTokens)
	}
}

// TestATDD_71_4_AC3_ManualCompactSuccessClearsLatch: a successful manual
// compaction proves the operation works again, so the handler clears the latch —
// leaving it set would keep automatic compaction permanently disabled after one
// repaired failure.
func TestATDD_71_4_AC3_ManualCompactSuccessClearsLatch(t *testing.T) {
	client, kern, ctxMgr := setupCompactIPCTest(t)
	cid := seedCompactableContext(t, ctxMgr)

	proc := kernel.NewProcess(0, "manual clear", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/claude"
	_ = proc.Start()
	kern.AddProcess(proc)
	proc.SetCompactLatched(true)

	if _, err := client.Compact(proc.PID, ""); err != nil {
		t.Fatalf("manual compact: %v", err)
	}

	if proc.GetCompactLatched() {
		t.Error("latch still set after a successful manual compaction — the escape hatch must " +
			"re-enable automatic compaction once an operator proves it works again")
	}
}

// TestATDD_71_4_AC4_CompactLatched_WireRoundTrip: the latch survives
// ProcInfo → wire → ProcInfo, and omitempty keeps it off the wire when unset.
func TestATDD_71_4_AC4_CompactLatched_WireRoundTrip(t *testing.T) {
	latched := vfs.ProcInfo{PID: 7, State: types.StateRunning, Intent: "latched", CompactLatched: true}
	wire := ProcInfoToWire(latched)
	if !wire.CompactLatched {
		t.Fatal("ProcInfoToWire dropped CompactLatched")
	}
	if rt := WireToProcInfo(wire); !rt.CompactLatched {
		t.Error("round-trip lost CompactLatched")
	}

	// Unlatched processes (the overwhelming majority) must not carry the field.
	blob, err := json.Marshal(ProcInfoToWire(vfs.ProcInfo{PID: 8, State: types.StateRunning}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), "compact_latched") {
		t.Error("omitempty violated: unlatched ProcInfoWire still carries compact_latched")
	}
}

// TestATDD_71_4_AC4_CompactLatched_VisibleOverIPC: end-to-end visibility —
// ListProcs (kernel → ProcInfoToWire → NDJSON → WireToProcInfo) reports the
// latch to an IPC consumer. This is AC4-②'s "process-level visible state".
func TestATDD_71_4_AC4_CompactLatched_VisibleOverIPC(t *testing.T) {
	client, kern, _ := setupCompactIPCTest(t)

	proc := kernel.NewProcess(0, "latch wire", nil)
	_ = proc.Start()
	kern.AddProcess(proc)
	proc.SetCompactLatched(true)

	procs, err := client.ListProcs()
	if err != nil {
		t.Fatalf("ListProcs: %v", err)
	}
	for _, p := range procs {
		if p.PID == proc.PID {
			if !p.CompactLatched {
				t.Error("CompactLatched not visible over the IPC wire (AC4-②)")
			}
			if p.SuspendReason != "" {
				t.Errorf("SuspendReason = %q on a Running process — the latch must not reuse it (F1)",
					p.SuspendReason)
			}
			return
		}
	}
	t.Fatalf("process %d not found in ListProcs", proc.PID)
}
