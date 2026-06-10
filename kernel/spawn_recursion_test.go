package kernel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
)

// Tests for spec spawn-recursion-no-permission: the three-layer fix against
// infinite spawn recursion when a process lacks a required device permission.
//
//	Scheme 1 — permission-denied guidance (TestSpawnRecursion_PermissionDeniedGuidance)
//	Scheme 2 — spawn-depth hard cap        (TestSpawnRecursion_Depth*)
//	Scheme 3 — circuit-breaker semantics   (TestSpawnBreakerOutcome)

// Scheme 1: the permission-denied error must carry guidance that breaks the
// LLM's "spawn a child to acquire the device" reasoning (root cause D1).
func TestSpawnRecursion_PermissionDeniedGuidance(t *testing.T) {
	k := newSimpleKernel(t)

	proc := NewProcess(0, "restricted", nil)
	proc.AllowedDevices = []string{"/dev/fs"} // base-device whitelist active; /dev/shell is denied

	_, err := k.executeVFSTool(proc,
		llmToolCall{Name: "Bash", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/shell"})
	if err == nil {
		t.Fatal("expected permission denied for /dev/shell not in [/dev/fs]")
	}
	msg := err.Error()
	if !strings.Contains(msg, "permission denied") {
		t.Errorf("error is not a permission denial: %q", msg)
	}
	if !strings.Contains(msg, "Child processes inherit the same device restrictions") {
		t.Errorf("permission error missing spawn-inheritance guidance: %q", msg)
	}
	if !strings.Contains(msg, "complete") {
		t.Errorf("permission error should steer the LLM toward complete: %q", msg)
	}
}

// Scheme 2 (depth propagation): Spawn copies SpawnOpts.Depth onto the process,
// and the absence of Depth (resume / CLI / compose / supervisor paths) yields a
// top-level process (Depth=0) that is exempt from the guard.
func TestSpawnRecursion_DepthPropagation(t *testing.T) {
	k := newMinimalKernel(t)

	t.Run("opts_depth_propagates_to_process", func(t *testing.T) {
		pid, err := k.Spawn("child", nil, SpawnOpts{Depth: 4, SkipReasonLoop: true})
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		if proc.Depth != 4 {
			t.Errorf("proc.Depth = %d, want 4", proc.Depth)
		}
	})

	t.Run("resume_cli_spawn_defaults_to_zero", func(t *testing.T) {
		// resume.go / CLI / compose construct SpawnOpts without Depth, so the
		// spawned process is top-level (0) and never trips the guard.
		pid, err := k.Spawn("top", nil, SpawnOpts{StartStep: 7, SkipReasonLoop: true})
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		if proc.Depth != 0 {
			t.Errorf("proc.Depth = %d, want 0 (resume/CLI exempt from depth guard)", proc.Depth)
		}
	})
}

// Scheme 2 (depth rejection): an ActionSpawn whose child would exceed
// MaxSpawnDepth is rejected before any child is created, feeds the circuit
// breaker, and returns guidance to the LLM.
func TestSpawnRecursion_DepthLimitRejected(t *testing.T) {
	llm := &mockLLMFile{readData: makeLLMResponse("ok", 1)}
	k, _, ctxMgr := newTestKernel(t, llm)
	k.SetStepDataDir(t.TempDir())

	proc := NewProcess(0, "deep parent", nil)
	cid, _ := ctxMgr.CtxAlloc(64)
	proc.CtxID = cid
	proc.Depth = MaxSpawnDepth // child would be MaxSpawnDepth+1 → rejected
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	k.procTable.Store(proc.PID, proc)

	// Mimic executeToolCalls having written the assistant+tool_calls turn so the
	// rejection's tool result pairs correctly with its call.
	if err := ctxMgr.AppendAssistantWithToolCalls(cid, "spawning", "", nil, []rnixctx.ToolCall{
		{ID: "sp-1", Name: "Agent", Input: map[string]any{"intent": "x"}},
	}); err != nil {
		t.Fatalf("pre-fill assistant+tool_calls: %v", err)
	}

	mapping := toolMapping{Type: "meta", Action: ActionSpawn}
	tc := llmToolCall{ID: "sp-1", Name: "Agent", Input: map[string]any{"intent": "do something"}}
	resp := llmResponse{Content: "spawning"}
	var counter errFingerprintCounter
	seen := map[string]bool{}
	prompt := &rnixctx.PromptResult{}

	cont := k.executeMetaAction(proc, tc, mapping, 1, time.Now(), &counter, seen, prompt, "", &resp)

	if !cont {
		t.Fatal("first depth-exceeded spawn should return true (breaker not yet tripped)")
	}
	if len(proc.Children) != 0 {
		t.Errorf("depth-exceeded spawn must not create a child, got %d", len(proc.Children))
	}
	if counter.count != 1 {
		t.Errorf("counter.count = %d, want 1 (depth rejection feeds the breaker)", counter.count)
	}
	out, err := ctxMgr.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	last := out.Messages[len(out.Messages)-1]
	if !strings.Contains(last.Content, "maximum spawn depth") {
		t.Errorf("tool result missing depth-limit guidance: %q", last.Content)
	}
}

// Scheme 2 + 3 (AC3 cascade): three consecutive depth-rejected spawns must trip
// the circuit breaker and finish the parent with Code=1. This drives the real
// executeMetaAction → bumpToolError → finishProcess path (no child goroutines:
// every spawn is rejected before k.Spawn), proving the breaker fires end-to-end.
func TestSpawnRecursion_DepthLimitTripsBreakerAt3(t *testing.T) {
	llm := &mockLLMFile{readData: makeLLMResponse("ok", 1)}
	k, _, ctxMgr := newTestKernel(t, llm)
	k.SetStepDataDir(t.TempDir())

	// Real Spawn (SkipReasonLoop) gives the parent a fully initialised
	// ctx/cancel/CtxID so finishProcess works cleanly when the breaker trips.
	pid, err := k.Spawn("deep parent", nil, SpawnOpts{Depth: MaxSpawnDepth, SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}
	cid := proc.CtxID

	mapping := toolMapping{Type: "meta", Action: ActionSpawn}
	resp := llmResponse{Content: "spawning"}
	var counter errFingerprintCounter
	prompt := &rnixctx.PromptResult{}

	cont := true
	for i := 1; i <= 3; i++ {
		tcID := fmt.Sprintf("sp-%d", i)
		if err := ctxMgr.AppendAssistantWithToolCalls(cid, "spawning", "", nil, []rnixctx.ToolCall{
			{ID: tcID, Name: "Agent", Input: map[string]any{"intent": "x"}},
		}); err != nil {
			t.Fatalf("step %d pre-fill: %v", i, err)
		}
		tc := llmToolCall{ID: tcID, Name: "Agent", Input: map[string]any{"intent": "x"}}
		seen := map[string]bool{} // fresh per step, mirroring the real reason loop
		cont = k.executeMetaAction(proc, tc, mapping, i, time.Now(), &counter, seen, prompt, "", &resp)
		if i < 3 && !cont {
			t.Fatalf("step %d tripped the breaker early (cont=false, count=%d)", i, counter.count)
		}
	}

	if cont {
		t.Fatal("3rd consecutive depth-rejected spawn must trip the breaker (got cont=true)")
	}
	if counter.count != 3 {
		t.Errorf("counter.count = %d, want 3 (INTERNAL|spawn accumulated across steps)", counter.count)
	}
	if st := proc.GetState(); st != types.StateZombie && st != types.StateDead {
		t.Errorf("parent state = %s, want Zombie/Dead after breaker trip", st)
	}
	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil || exit.Code != 1 {
		t.Fatalf("parent exit = %+v, want Code=1 (circuit_breaker)", exit)
	}
	if !strings.Contains(exit.Reason, "circuit_breaker") {
		t.Errorf("exit reason = %q, want circuit_breaker", exit.Reason)
	}
	// All three spawns were rejected before k.Spawn, so no child exists.
	if len(proc.Children) != 0 {
		t.Errorf("depth-rejected spawns created %d children, want 0", len(proc.Children))
	}
}

// Scheme 3: spawnBreakerOutcome encodes the per-child-exit-code policy. Only a
// clean child resets; an abnormal child accumulates toward the breaker;
// suspended / context-full children are neutral.
func TestSpawnBreakerOutcome(t *testing.T) {
	t.Run("clean_child_resets", func(t *testing.T) {
		var c errFingerprintCounter
		bumpToolError(&c, map[string]bool{}, string(types.ErrInternal), "spawn") // count=1
		if c.count != 1 {
			t.Fatalf("setup: count=%d, want 1", c.count)
		}
		if tripped := spawnBreakerOutcome(&c, map[string]bool{}, ExitOK); tripped {
			t.Error("ExitOK must not trip the breaker")
		}
		if c.count != 0 {
			t.Errorf("ExitOK must reset the counter, got count=%d", c.count)
		}
	})

	t.Run("failing_child_trips_at_3", func(t *testing.T) {
		var c errFingerprintCounter
		for i := 1; i <= 3; i++ {
			tripped := spawnBreakerOutcome(&c, map[string]bool{}, ExitError) // fresh per-step seen
			switch {
			case i < 3 && tripped:
				t.Fatalf("attempt %d tripped early (count=%d)", i, c.count)
			case i == 3 && !tripped:
				t.Fatalf("attempt 3 should trip, count=%d", c.count)
			}
		}
	})

	t.Run("suspended_and_contextfull_are_neutral", func(t *testing.T) {
		var c errFingerprintCounter
		bumpToolError(&c, map[string]bool{}, string(types.ErrInternal), "spawn") // count=1
		for _, code := range []int{ExitSuspended, ExitContextFull} {
			if tripped := spawnBreakerOutcome(&c, map[string]bool{}, code); tripped {
				t.Errorf("exit code %d must not trip the breaker", code)
			}
			if c.count != 1 {
				t.Errorf("exit code %d must be neutral, counter changed to %d", code, c.count)
			}
		}
	})
}

// Spawn()-level depth guard: rejects spawn when opts.Depth > MaxSpawnDepth,
// covering intent / compose paths that bypass ActionSpawn in tool_exec.go.
func TestSpawn_DepthRejectInSpawn(t *testing.T) {
	k := newMinimalKernel(t)

	t.Run("at_max_depth_succeeds", func(t *testing.T) {
		pid, err := k.Spawn("at-limit", nil, SpawnOpts{Depth: MaxSpawnDepth, SkipReasonLoop: true})
		if err != nil {
			t.Fatalf("Spawn at MaxSpawnDepth should succeed: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		if proc.Depth != MaxSpawnDepth {
			t.Errorf("Depth = %d, want %d", proc.Depth, MaxSpawnDepth)
		}
	})

	t.Run("exceeding_max_depth_rejected", func(t *testing.T) {
		_, err := k.Spawn("too-deep", nil, SpawnOpts{Depth: MaxSpawnDepth + 1, SkipReasonLoop: true})
		if err == nil {
			t.Fatal("Spawn at MaxSpawnDepth+1 should fail")
		}
		if !strings.Contains(err.Error(), "spawn rejected") {
			t.Errorf("error should mention spawn rejected: %v", err)
		}
	})
}

// DeniedDevices: device blacklist blocks access regardless of AllowedDevices.
func TestSpawnRecursion_DeniedDevices(t *testing.T) {
	k := newSimpleKernel(t)

	t.Run("denied_device_blocked", func(t *testing.T) {
		proc := NewProcess(0, "intent-child", nil)
		proc.DeniedDevices = []string{"/dev/intent"}

		_, err := k.executeVFSTool(proc,
			llmToolCall{Name: "IntentDecompose", Input: map[string]any{}},
			toolMapping{Type: "vfs", VFSPath: "/dev/intent/decompose"})
		if err == nil {
			t.Fatal("expected permission denied for denied device /dev/intent")
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("error should mention permission denied: %q", err.Error())
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("error should mention device is blocked: %q", err.Error())
		}
	})

	t.Run("non_denied_device_allowed", func(t *testing.T) {
		proc := NewProcess(0, "intent-child", nil)
		proc.DeniedDevices = []string{"/dev/intent"}

		// /dev/fs should pass the DeniedDevices check (may fail later on VFS open)
		_, err := k.executeVFSTool(proc,
			llmToolCall{Name: "Read", Input: map[string]any{"path": "test.txt"}},
			toolMapping{Type: "vfs", VFSPath: "/dev/fs", FSOperation: "Read"})
		if err != nil && strings.Contains(err.Error(), "permission denied") {
			t.Errorf("non-denied device should not be blocked: %v", err)
		}
	})

	t.Run("denied_devices_inherited_by_spawn", func(t *testing.T) {
		pid, err := k.Spawn("child-with-deny", nil, SpawnOpts{
			DeniedDevices:  []string{"/dev/intent"},
			SkipReasonLoop: true,
		})
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		child, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("child process not found")
		}
		if len(child.DeniedDevices) != 1 || child.DeniedDevices[0] != "/dev/intent" {
			t.Errorf("child.DeniedDevices = %v, want [/dev/intent]", child.DeniedDevices)
		}
	})
}
