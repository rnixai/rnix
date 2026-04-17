package llm

import (
	"context"
	"sync"
	"time"
)

// IdleTimer wraps a context with an idle deadline semantics:
// the context is cancelled when no activity (Reset) is observed for
// the configured duration.
//
// This is used by streaming LLM drivers (subprocess CLI and SSE HTTP)
// where the Call() lifetime may legitimately span many minutes while
// events stream in. A wall-clock timeout would incorrectly kill a
// healthy process that is steadily emitting events. An idle timeout
// only fires when the stream truly stalls.
//
// Lifecycle:
//
//	ctx, idle, cancel := NewIdleTimer(parentCtx, 3*time.Minute)
//	defer cancel()
//	for scanner.Scan() {
//	    idle.Reset()
//	    ... process event ...
//	}
//
// If idleAfter <= 0, the timer is a no-op (ctx is returned unchanged
// aside from a nop cancel); callers retain the parent deadline only.
type IdleTimer struct {
	idleAfter time.Duration
	timer     *time.Timer
	cancel    context.CancelCauseFunc

	mu     sync.Mutex
	closed bool
}

// ErrIdleTimeout is attached as the cancellation cause when the idle
// deadline elapses without a Reset call. Callers inspecting the
// context error can use errors.Is(context.Cause(ctx), ErrIdleTimeout)
// to distinguish idle timeout from parent cancellation.
var ErrIdleTimeout = errIdleTimeout{}

type errIdleTimeout struct{}

func (errIdleTimeout) Error() string { return "idle timeout: no stream activity" }

// NewIdleTimer returns a derived context that is cancelled when Reset
// has not been called for idleAfter. The returned cancel function must
// be called to release resources; calling it also stops the timer.
//
// Reset() should be called once for every meaningful activity event
// (stdout line, SSE event, partial response chunk) from the upstream
// stream. It is safe to call Reset() concurrently and after cancel().
func NewIdleTimer(parent context.Context, idleAfter time.Duration) (context.Context, *IdleTimer, context.CancelFunc) {
	ctx, cancelCause := context.WithCancelCause(parent)
	t := &IdleTimer{
		idleAfter: idleAfter,
		cancel:    cancelCause,
	}
	if idleAfter > 0 {
		t.timer = time.AfterFunc(idleAfter, func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.closed {
				return
			}
			cancelCause(ErrIdleTimeout)
		})
	}
	cancel := func() {
		t.mu.Lock()
		t.closed = true
		if t.timer != nil {
			t.timer.Stop()
		}
		t.mu.Unlock()
		cancelCause(context.Canceled)
	}
	return ctx, t, cancel
}

// Reset postpones the idle deadline by idleAfter. Safe for concurrent
// use. A no-op if the timer has been cancelled or idleAfter <= 0.
func (t *IdleTimer) Reset() {
	if t == nil || t.timer == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.timer.Reset(t.idleAfter)
}

// IsStreamTimeout reports whether ctx.Err() reflects either a wall-clock
// deadline exceeded (context.WithTimeout) or an idle timeout from
// IdleTimer. Drivers use this to map ctx cancellation to ErrTimeout
// uniformly regardless of which timeout model is in play.
func IsStreamTimeout(ctx context.Context) bool {
	if ctx == nil || ctx.Err() == nil {
		return false
	}
	if ctx.Err() == context.DeadlineExceeded {
		return true
	}
	cause := context.Cause(ctx)
	_, ok := cause.(errIdleTimeout)
	return ok
}
