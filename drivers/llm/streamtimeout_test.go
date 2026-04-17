package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIdleTimer_FiresWhenNoReset(t *testing.T) {
	ctx, _, cancel := NewIdleTimer(context.Background(), 50*time.Millisecond)
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrIdleTimeout) {
			t.Errorf("cause = %v, want ErrIdleTimeout", context.Cause(ctx))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("idle timer did not fire")
	}
}

func TestIdleTimer_ResetPostponesDeadline(t *testing.T) {
	ctx, idle, cancel := NewIdleTimer(context.Background(), 80*time.Millisecond)
	defer cancel()

	// Reset every 40ms for 240ms — idle must NOT fire.
	stop := time.After(240 * time.Millisecond)
loop:
	for {
		select {
		case <-stop:
			break loop
		case <-ctx.Done():
			t.Fatalf("ctx cancelled prematurely: %v", context.Cause(ctx))
		case <-time.After(40 * time.Millisecond):
			idle.Reset()
		}
	}

	// Stop resetting — idle must fire within ~80ms.
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrIdleTimeout) {
			t.Errorf("cause = %v, want ErrIdleTimeout", context.Cause(ctx))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("idle timer did not fire after reset stopped")
	}
}

func TestIdleTimer_ZeroDisablesTimer(t *testing.T) {
	ctx, _, cancel := NewIdleTimer(context.Background(), 0)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("ctx cancelled unexpectedly: %v", context.Cause(ctx))
	case <-time.After(100 * time.Millisecond):
	}
}

func TestIdleTimer_CancelStopsTimer(t *testing.T) {
	ctx, _, cancel := NewIdleTimer(context.Background(), 50*time.Millisecond)
	cancel()
	<-ctx.Done()
	if !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Errorf("cause = %v, want context.Canceled", context.Cause(ctx))
	}
}

func TestIdleTimer_ParentCancelPropagates(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	ctx, _, cancel := NewIdleTimer(parent, 5*time.Second)
	defer cancel()
	parentCancel()
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("parent cancel did not propagate")
	}
}

func TestIdleTimer_ResetAfterCancelIsNoop(t *testing.T) {
	_, idle, cancel := NewIdleTimer(context.Background(), 50*time.Millisecond)
	cancel()
	idle.Reset() // must not panic
}
