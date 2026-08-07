package kernel

import (
	gocontext "context"
	"fmt"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 76.1 — AC4/AC5/AC6: informational error classes (NOT_FOUND /
// PERMISSION; D1/D2 decisions) must never contribute to the circuit breaker —
// neither the per-fingerprint streak nor the cumulative total — while real
// driver failures (DRIVER) and the epic-73 red line (SERVICE_UNAVAILABLE) keep
// the 3-strike protection.
//
// The reproduction this file pins is the wicket incident: three real upstream
// 404s (IdentityServer4 moved orgs) surfaced as "DRIVER|/dev/web" and the step
// died with "circuit_breaker: 3 consecutive errors with fingerprint
// DRIVER|/dev/web" (proc-info 019fda02-7b77-726f-8edf-aae3e17dc8c3). The
// fingerprint does not include the URL by design (epic Non-goal), so both the
// same-URL and changed-URL sequences produce the same fingerprint — and under
// the exemption both must survive.

// atdd76ErrVFSTool is a /dev/web stand-in whose Write fails with the configured
// error — the shape executeVFSTool's default branch propagates as
// "write failed: %w", exactly the wrapping depth production sees.
type atdd76ErrVFSTool struct {
	err error
}

func (f *atdd76ErrVFSTool) Write(_ gocontext.Context, data []byte) error { return f.err }
func (f *atdd76ErrVFSTool) Read(length int) ([]byte, error)             { return nil, f.err }
func (f *atdd76ErrVFSTool) Close() error                                { return nil }
func (f *atdd76ErrVFSTool) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/web"}, nil
}

// atdd76Kernel builds a kernel whose /dev/web tool fails with whatever error
// tool currently points at (swap per step to simulate upstream statuses).
func atdd76Kernel(t *testing.T, tool **atdd76ErrVFSTool) (*KernelImpl, *Process, *errFingerprintCounter) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/web", func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return *tool, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(16)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc := NewProcess(0, "76.1 webfetch", nil)
	proc.CtxID = cid
	proc.toolMap = map[string]toolMapping{
		"WebFetch": {Type: "vfs", VFSPath: "/dev/web"},
	}
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	return k, proc, &errFingerprintCounter{}
}

// atdd76RunStep executes one WebFetch tool call through the executeToolCalls
// integration path and returns the accumulated ToolCallRecord(s).
func atdd76RunStep(k *KernelImpl, proc *Process, counter *errFingerprintCounter, step int, url string) ([]types.ToolCallRecord, bool) {
	resp := llmResponse{
		Content: "fetching",
		ToolCalls: []llmToolCall{
			{ID: fmt.Sprintf("call-%d", step), Name: "WebFetch", Input: map[string]any{"url": url}},
		},
	}
	return k.executeToolCalls(proc, resp, step, time.Now(), counter, &rnixctx.PromptResult{}, "")
}

// atdd76DrainReasonStepErrors collects the error messages of all ReasonStep
// events emitted so far (emitEvent sends synchronously, so by the time
// executeToolCalls returns everything is buffered in DebugChan).
func atdd76DrainReasonStepErrors(t *testing.T, proc *Process) []string {
	t.Helper()
	var msgs []string
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall == "ReasonStep" && ev.Err != nil {
				msgs = append(msgs, ev.Err.Error())
			}
		default:
			return msgs
		}
	}
}

// AC5: three consecutive 404s against the SAME URL must not kill the step —
// NOT_FOUND is informational (D1/D2): no streak, no total, and the agent sees
// the NOT_FOUND semantics so it can switch strategy.
func TestATDD_76_1_AC5_SameURLThreeNotFoundNotKilled(t *testing.T) {
	tool := &atdd76ErrVFSTool{}
	k, proc, counter := atdd76Kernel(t, &tool)
	const url = "https://github.com/IdentityServer/IdentityServer4"

	for step := 1; step <= 3; step++ {
		tool.err = types.NewDriverError("Fetch", "/dev/web", fmt.Errorf("HTTP 404: 404 Not Found"), types.ErrNotFound)
		calls, cont := atdd76RunStep(k, proc, counter, step, url)
		if !cont {
			t.Fatalf("step %d: circuit breaker killed the step on a 404 — wicket incident regression", step)
		}
		if len(calls) != 1 || calls[0].ErrorCode != "NOT_FOUND" {
			t.Fatalf("step %d: ToolCallRecord = %+v, want ErrorCode NOT_FOUND", step, calls)
		}
		if counter.count != 0 || counter.total != 0 {
			t.Fatalf("step %d: breaker counter count=%d total=%d, want 0/0 (informational classes never count)", step, counter.count, counter.total)
		}
	}

	msgs := atdd76DrainReasonStepErrors(t, proc)
	var sawNotFound bool
	for _, m := range msgs {
		if strings.Contains(m, "[NOT_FOUND]") {
			sawNotFound = true
		}
	}
	if !sawNotFound {
		t.Fatalf("no ReasonStep event message carries [NOT_FOUND], got %v", msgs)
	}
}

// AC5: three consecutive 404s while CHANGING the URL must also survive — the
// exemption is class-based, not URL-based (fingerprint excludes the URL by
// design, epic Non-goal), so retry-with-new-URL is exactly the behaviour the
// wicket agent was punished for.
func TestATDD_76_1_AC5_ChangedURLThreeNotFoundNotKilled(t *testing.T) {
	tool := &atdd76ErrVFSTool{}
	k, proc, counter := atdd76Kernel(t, &tool)
	urls := []string{
		"https://api.github.com/orgs/IdentityServer",
		"https://api.github.com/orgs/IdentityServer4",
		"https://github.com/IdentityServer/IdentityServer4",
	}

	for step := 1; step <= 3; step++ {
		tool.err = types.NewDriverError("Fetch", "/dev/web", fmt.Errorf("HTTP 404: 404 Not Found"), types.ErrNotFound)
		calls, cont := atdd76RunStep(k, proc, counter, step, urls[step-1])
		if !cont {
			t.Fatalf("step %d (URL %s): circuit breaker killed the step — changed-URL retry must survive", step, urls[step-1])
		}
		if len(calls) != 1 || calls[0].ErrorCode != "NOT_FOUND" {
			t.Fatalf("step %d: ToolCallRecord = %+v, want ErrorCode NOT_FOUND", step, calls)
		}
		if counter.count != 0 || counter.total != 0 {
			t.Fatalf("step %d: breaker counter count=%d total=%d, want 0/0", step, counter.count, counter.total)
		}
	}
}

// AC4: three consecutive DRIVER errors on /dev/web must STILL kill the step and
// the exit reason must attribute the trip to the streak breaker.
func TestATDD_76_1_AC4_ThreeDriverErrorsStillKill(t *testing.T) {
	tool := &atdd76ErrVFSTool{}
	k, proc, counter := atdd76Kernel(t, &tool)
	const url = "https://example.com/boom"

	for step := 1; step <= 3; step++ {
		tool.err = types.NewDriverError("Fetch", "/dev/web", fmt.Errorf("HTTP 500: upstream exploded"), types.ErrDriver)
		_, cont := atdd76RunStep(k, proc, counter, step, url)
		if step < 3 && !cont {
			t.Fatalf("step %d: tripped early (count=%d)", step, counter.count)
		}
		if step == 3 {
			if cont {
				t.Fatal("step 3: DRIVER errors must still trip the circuit breaker (AC4)")
			}
			proc.mu.Lock()
			exit := proc.Exit
			proc.mu.Unlock()
			if exit == nil || !strings.Contains(exit.Reason, "consecutive") {
				t.Fatalf("exit = %+v, want circuit_breaker reason with 'consecutive'", exit)
			}
		}
	}
}

// AC6 (epic-73 red line): SERVICE_UNAVAILABLE must never be exempt — it is the
// classification input of the 429 disposition chain, and exempting it would
// nullify that work. It keeps the 3-strike protection.
func TestATDD_76_1_AC6_ServiceUnavailableStillKills(t *testing.T) {
	tool := &atdd76ErrVFSTool{}
	k, proc, counter := atdd76Kernel(t, &tool)
	const url = "https://example.com/ratelimited"

	for step := 1; step <= 3; step++ {
		tool.err = types.NewDriverError("Fetch", "/dev/web", fmt.Errorf("HTTP 429: Too Many Requests"), types.ErrServiceUnavailable)
		_, cont := atdd76RunStep(k, proc, counter, step, url)
		if step < 3 && !cont {
			t.Fatalf("step %d: tripped early (count=%d)", step, counter.count)
		}
		if step == 3 && cont {
			t.Fatal("step 3: SERVICE_UNAVAILABLE must still trip (epic-73 boundary)")
		}
	}
}
