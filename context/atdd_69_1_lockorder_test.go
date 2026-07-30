package context

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// Story 69.1 AC4: BuildPrompt / TokenUsage must not hold ctx.mu.RLock() while
// calling Sections.Build(). The backpressure section's ComputeFn calls back into
// Manager.SlotUsage(), which takes a *second* RLock on the same ctx.mu from the
// same goroutine. Under sync.RWMutex semantics a queued writer (Compact,
// IPC handleCompact, gdb AppendMessage, fork_continue) blocks that second RLock
// forever → the reasonStep goroutine deadlocks.
//
// These tests reproduce the recursive-RLock shape with a real ComputeFn that
// calls back into the Manager, plus a concurrent writer, and are bounded so a
// regression shows up as a -race/timeout failure rather than an infinite hang.

// recursiveSection registers a section whose ComputeFn re-enters the Manager for
// the same context — exactly what kernel's backpressure section does.
func recursiveSection(t *testing.T, m *Manager, cid types.CtxID) *SectionRegistry {
	t.Helper()
	reg := NewSectionRegistry()
	reg.Register("recursive_slot_reader", func() string {
		used, max, err := m.SlotUsage(cid)
		if err != nil || max == 0 {
			return ""
		}
		if float64(used)/float64(max)*100 > 70 {
			return "# Context Resource Warning\n\nslots are running low."
		}
		return ""
	}, false)
	return reg
}

func TestATDD_69_1_AC4_BuildPromptNoRecursiveRLockDeadlock(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(64)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	if err := m.SetSections(cid, recursiveSection(t, m, cid)); err != nil {
		t.Fatalf("SetSections: %v", err)
	}
	// Push slot usage above 70% so the recursive ComputeFn path is actually taken.
	for range 50 {
		_ = m.AppendMessage(cid, RoleUser, "x")
	}

	const rounds = 300
	var wg sync.WaitGroup
	wg.Add(3)

	// Bounded watchdog: a lock-order regression must fail fast and legibly
	// instead of burning the package's 10-minute test timeout (Dev Notes #6).
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Errorf("deadlock: BuildPrompt/TokenUsage still blocked after 30s with a writer contending.\n"+
				"BuildPrompt/TokenUsage must snapshot under RLock and call Sections.Build() *after* releasing it.\n"+
				"goroutine dump:\n%s", buf[:n])
			panic("story 69.1 AC4: recursive RLock deadlock detected")
		}
	}()

	// Reader A: BuildPrompt (first RLock) → Build() → SlotUsage (second RLock).
	go func() {
		defer wg.Done()
		for range rounds {
			if _, err := m.BuildPrompt(cid); err != nil {
				t.Errorf("BuildPrompt: %v", err)
				return
			}
		}
	}()

	// Reader B: TokenUsage takes the same path.
	go func() {
		defer wg.Done()
		for range rounds {
			if _, err := m.TokenUsage(cid); err != nil {
				t.Errorf("TokenUsage: %v", err)
				return
			}
		}
	}()

	// Writer: queues ctx.mu.Lock() between the two read locks (IPC compact /
	// gdb inject / fork_continue all do this from another goroutine).
	go func() {
		defer wg.Done()
		for range rounds {
			// Ignore ErrContextFull — the point is contending for the write lock.
			_ = m.AppendMessage(cid, RoleUser, "y")
			_ = m.InvalidateSections(cid)
		}
	}()

	wg.Wait()
	close(done)
}

func TestATDD_69_1_AC4_BuildPromptSemanticsUnchanged(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(16)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	if err := m.SetSystemPrompt(cid, "legacy prompt"); err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}
	if err := m.AppendMessage(cid, RoleUser, "hello"); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Without sections: the legacy SystemPrompt string is used verbatim.
	res, err := m.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if res.SystemPrompt != "legacy prompt" {
		t.Errorf("SystemPrompt = %q, want %q", res.SystemPrompt, "legacy prompt")
	}
	if len(res.Messages) != 1 || res.Messages[0].Content != "hello" {
		t.Errorf("Messages = %+v, want 1 message %q", res.Messages, "hello")
	}

	// With sections: Build() output wins over the legacy string.
	reg := NewSectionRegistry()
	reg.Register("static", func() string { return "# Section\n\nbody" }, true)
	if err := m.SetSections(cid, reg); err != nil {
		t.Fatalf("SetSections: %v", err)
	}
	res, err = m.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if res.SystemPrompt != "# Section\n\nbody" {
		t.Errorf("SystemPrompt = %q, want section build output", res.SystemPrompt)
	}

	// Returned messages must be a copy, not an alias of ctx.Messages.
	res.Messages[0].Content = "mutated"
	again, err := m.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if again.Messages[0].Content != "hello" {
		t.Errorf("BuildPrompt returned an aliased slice: content became %q", again.Messages[0].Content)
	}
}

func TestATDD_69_1_AC4_TokenUsageFieldsUnchanged(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(10)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	if err := m.SetSystemPrompt(cid, "sys"); err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}
	for range 4 {
		if err := m.AppendMessage(cid, RoleUser, "hello world"); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	stats, err := m.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	want := EstimateTokens("sys") + 4*EstimateTokens("hello world")
	if stats.Used != want {
		t.Errorf("Used = %d, want %d (accounting must be untouched by the lock-order fix)", stats.Used, want)
	}
	if stats.Limit != DefaultTokenLimit {
		t.Errorf("Limit = %d, want %d", stats.Limit, DefaultTokenLimit)
	}
	if stats.SlotUsed != 4 || stats.SlotMax != 10 {
		t.Errorf("SlotUsed/SlotMax = %d/%d, want 4/10", stats.SlotUsed, stats.SlotMax)
	}
	if stats.SlotPercentage != 40 {
		t.Errorf("SlotPercentage = %v, want 40", stats.SlotPercentage)
	}
}
