package kernel

import (
	gocontext "context"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/kernel/memory"
	"github.com/rnixai/rnix/vfs"
)

// recordingCaller is a mock memory.LLMCaller that signals when invoked.
type recordingCaller struct {
	called chan struct{}
}

func (c *recordingCaller) Call(_ gocontext.Context, _, _ string, _ int) (string, error) {
	select {
	case c.called <- struct{}{}:
	default:
	}
	return `{"entries": []}`, nil
}

// TestSetWritebackWorker_StartsConsumer guards the production wiring order:
// NewKernel runs startReaper before any worker is injected, so the
// startReaper-side Start() never fires and SetWritebackWorker itself must
// start the consumer goroutine. Without it, submitted jobs queue forever and
// drop once the channel fills (Story 35.3 wiring defect, found in the 35-8
// code review). The unit tests in kernel/memory call Start() directly and
// cannot catch this — only the kernel-level injection path exercises it.
func TestSetWritebackWorker_StartsConsumer(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	k.dataDir = t.TempDir()
	t.Cleanup(k.Shutdown)

	dir := t.TempDir()
	globalMemDir := filepath.Join(dir, "global", "memory")
	if err := os.MkdirAll(globalMemDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := memory.NewMemoryStore(globalMemDir, dir, memory.DefaultMemoryConfig())
	caller := &recordingCaller{called: make(chan struct{}, 1)}
	worker := memory.NewWritebackWorker(store, caller, memory.DefaultMemoryConfig().Writeback, "")

	// Production order: the worker is injected AFTER NewKernel already ran
	// startReaper. SetWritebackWorker must start the consumer itself.
	k.SetWritebackWorker(worker)

	// Submit a job whose steps.jsonl is readable so processJob reaches the
	// LLM call — the observable proof that the consumer goroutine is alive.
	stepsDir := filepath.Join(dir, "steps", "wiring-test-uuid")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stepsLine := `{"step":1,"action":"tool_call","summary":"wiring probe"}` + "\n"
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), []byte(stepsLine), 0o644); err != nil {
		t.Fatal(err)
	}
	worker.Submit(memory.NewWritebackJob("wiring-test-uuid", stepsDir, "", 0, "completed"))

	select {
	case <-caller.called:
		// Consumer goroutine is alive and processed the job.
	case <-time.After(5 * time.Second):
		t.Fatal("writeback job was never consumed — SetWritebackWorker did not start the worker goroutine")
	}
}
