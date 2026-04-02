package kernel

import (
	"context"
	"testing"
)

func TestSetStepCancel_CancelStep(t *testing.T) {
	proc := NewProcess(0, "test-step-cancel", nil)

	// Initially nil — CancelStep should be safe to call
	proc.CancelStep()

	// Set a cancel func and verify it gets called
	ctx, cancel := context.WithCancel(context.Background())
	proc.SetStepCancel(cancel)

	proc.CancelStep()
	if ctx.Err() == nil {
		t.Error("expected stepCtx to be cancelled after CancelStep")
	}
}

func TestSetStepCancel_DoesNotAffectProcCtx(t *testing.T) {
	proc := NewProcess(0, "test-step-cancel-isolation", nil)

	stepCtx, stepCancel := context.WithCancel(proc.ctx)
	proc.SetStepCancel(stepCancel)

	// Cancel step context
	proc.CancelStep()

	// Step context should be cancelled
	if stepCtx.Err() == nil {
		t.Error("expected stepCtx to be cancelled")
	}

	// Process context should NOT be cancelled
	if proc.ctx.Err() != nil {
		t.Error("expected proc.ctx to remain active after step cancel")
	}
}

func TestSetStepCancel_ClearAfterUse(t *testing.T) {
	proc := NewProcess(0, "test-step-cancel-clear", nil)

	_, cancel := context.WithCancel(context.Background())
	proc.SetStepCancel(cancel)

	// Clear it
	proc.SetStepCancel(nil)

	// CancelStep on nil should be safe
	proc.CancelStep()
}

func TestStepCancel_RetryDetection(t *testing.T) {
	proc := NewProcess(0, "test-retry-detection", nil)

	stepCtx, stepCancel := context.WithCancel(proc.ctx)
	proc.SetStepCancel(stepCancel)

	// Simulate heartbeat monitor cancelling the step
	proc.CancelStep()

	// Step context should be cancelled
	if stepCtx.Err() == nil {
		t.Fatal("expected stepCtx to be cancelled")
	}

	// Process context should still be alive — this is how reason.go detects step retry
	if proc.ctx.Err() != nil {
		t.Fatal("expected proc.ctx to be alive (step retry, not process cancel)")
	}

	// The retry detection logic: stepCtx.Err() != nil && proc.ctx.Err() == nil
	isStepRetry := stepCtx.Err() != nil && proc.ctx.Err() == nil
	if !isStepRetry {
		t.Error("expected step retry condition to be true")
	}
}
