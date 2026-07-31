package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// Story 69.3 AC6 (preventive half) — a revived process must have headroom
// BEFORE its first reasonStep. rehydrateRuntimeStateFromDisk is the shared
// funnel for both disk-backed paths (resumeFromHistory and
// LoadSuspendedFromDisk), so exercising it covers two callers at once.

// writeLeakySteps writes a steps.jsonl whose cumulative history fills the
// context to its ceiling with leaked tool results, i.e. exactly the shape a
// process suspended on context_full leaves behind.
func writeLeakySteps(t *testing.T, baseDir, uuid string, rounds int) {
	t.Helper()
	dir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	meta := map[string]any{
		"system_prompt": "you are a resumed test agent",
		"tools":         []any{},
	}
	mb, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "process-meta.json"), mb, 0o644); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}

	payload := strings.Repeat("tool payload ", 250) // > context.LeakedThreshold
	var msgs []map[string]any
	for i := range rounds {
		id := "call-" + strconv.Itoa(i)
		msgs = append(msgs,
			map[string]any{"role": "user", "content": "please run step " + strconv.Itoa(i)},
			map[string]any{
				"role":       "assistant",
				"content":    "running",
				"tool_calls": []map[string]any{{"id": id, "name": "Bash"}},
			},
			map[string]any{"role": "tool", "content": payload, "tool_call_id": id},
		)
	}

	// One record carrying the full cumulative history — parseStepsJSONL takes
	// the messages of the highest step.
	msgsRaw, _ := json.Marshal(msgs)
	rec, _ := json.Marshal(map[string]any{
		"step":     1,
		"messages": json.RawMessage(msgsRaw),
	})
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), append(rec, '\n'), 0o644); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
}

func setupRehydrateKernel(t *testing.T) (*KernelImpl, *rnixctx.Manager, string) {
	t.Helper()
	// t.TempDir() BEFORE t.Cleanup(k.Shutdown): cleanups run LIFO, so a TempDir
	// registered later would be removed while the kernel still holds files in it
	// ("directory not empty" flake).
	dataDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	k.SetStepDataDir(dataDir)
	t.Cleanup(k.Shutdown)

	return k, ctxMgr, dataDir
}

func TestATDD_69_3_AC6_RehydrateUnloadsWhenAtCeiling(t *testing.T) {
	k, ctxMgr, dataDir := setupRehydrateKernel(t)

	const uuid = "69-3-unload-ceiling"
	const ctxSize = 40
	writeLeakySteps(t, dataDir, uuid, 13) // 39 messages into 40 slots = 97.5%

	proc := NewProcess(0, "resumed at ceiling", nil)
	proc.UUID = uuid
	proc.PrimaryDevice = "/dev/llm/mock"

	stepsDir := filepath.Join(dataDir, "steps", uuid)
	if _, _, err := k.rehydrateRuntimeStateFromDisk(proc, stepsDir, ctxSize, 0); err != nil {
		t.Fatalf("rehydrateRuntimeStateFromDisk: %v", err)
	}

	avail, err := ctxMgr.AvailableSlots(proc.CtxID)
	if err != nil {
		t.Fatalf("AvailableSlots: %v", err)
	}
	if avail <= 0 {
		t.Fatalf("available slots = %d — a revived process with no headroom hits precompact on step one", avail)
	}
	// AC9 ③: enough room for the first assistant + its tool results.
	if avail < resumeFallbackHeadroom {
		t.Errorf("available slots = %d, want >= %d", avail, resumeFallbackHeadroom)
	}

	used, _, _ := ctxMgr.SlotUsage(proc.CtxID)
	if used >= ctxSize-1 {
		t.Errorf("slot usage %d/%d, want meaningful reclamation", used, ctxSize)
	}

	// The reclamation must not have broken tool_use ↔ tool_result pairing.
	assertKernelPairing(t, ctxMgr, proc.CtxID)
}

// TestATDD_69_3_AC6_RehydrateLeavesRoomySnapshotAlone: a snapshot well under the
// threshold must be restored verbatim — the unload is a fault remedy, not a
// routine trim.
func TestATDD_69_3_AC6_RehydrateLeavesRoomySnapshotAlone(t *testing.T) {
	k, ctxMgr, dataDir := setupRehydrateKernel(t)

	const uuid = "69-3-unload-roomy"
	const ctxSize = 200
	writeLeakySteps(t, dataDir, uuid, 5) // 15 of 200 slots

	proc := NewProcess(0, "resumed with room", nil)
	proc.UUID = uuid
	proc.PrimaryDevice = "/dev/llm/mock"

	stepsDir := filepath.Join(dataDir, "steps", uuid)
	if _, _, err := k.rehydrateRuntimeStateFromDisk(proc, stepsDir, ctxSize, 0); err != nil {
		t.Fatalf("rehydrateRuntimeStateFromDisk: %v", err)
	}

	used, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage: %v", err)
	}
	if used != 15 {
		t.Errorf("restored messages = %d, want 15 (below-threshold snapshot must be untouched)", used)
	}

	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for i, msg := range prompt.Messages {
		if msg.Role == rnixctx.RoleTool && msg.Content == rnixctx.DefaultPrunePlaceholder {
			t.Errorf("msg[%d] was pruned although the snapshot had plenty of room", i)
		}
	}
	assertKernelPairing(t, ctxMgr, proc.CtxID)
}
