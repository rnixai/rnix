package ipc

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func TestProcInfoWire_Heartbeat_RoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond) // Milli precision round-trip
	info := vfs.ProcInfo{
		PID:           1,
		UUID:          "test-uuid",
		State:         types.StateRunning,
		Intent:        "test",
		CreatedAt:     now.Add(-5 * time.Minute),
		LastHeartbeat: now,
		StepTimeout:   10 * time.Minute,
	}

	wire := ProcInfoToWire(info)

	if wire.LastHeartbeatMs != now.UnixMilli() {
		t.Errorf("LastHeartbeatMs = %d, want %d", wire.LastHeartbeatMs, now.UnixMilli())
	}
	if wire.StepTimeoutMs != (10 * time.Minute).Milliseconds() {
		t.Errorf("StepTimeoutMs = %d, want %d", wire.StepTimeoutMs, (10*time.Minute).Milliseconds())
	}

	// Round-trip
	back := WireToProcInfo(wire)

	if !back.LastHeartbeat.Equal(now) {
		t.Errorf("round-trip LastHeartbeat = %v, want %v", back.LastHeartbeat, now)
	}
	if back.StepTimeout != 10*time.Minute {
		t.Errorf("round-trip StepTimeout = %v, want 10m", back.StepTimeout)
	}
}

func TestProcInfoWire_Heartbeat_ZeroValues(t *testing.T) {
	// Zero heartbeat should produce 0 ms in wire (omitempty)
	info := vfs.ProcInfo{
		PID:       2,
		State:     types.StateDead,
		CreatedAt: time.Now(),
	}

	wire := ProcInfoToWire(info)

	if wire.LastHeartbeatMs != 0 {
		t.Errorf("LastHeartbeatMs should be 0 for zero heartbeat, got %d", wire.LastHeartbeatMs)
	}
	if wire.StepTimeoutMs != 0 {
		t.Errorf("StepTimeoutMs should be 0 for zero timeout, got %d", wire.StepTimeoutMs)
	}

	// Round-trip zero values
	back := WireToProcInfo(wire)

	if !back.LastHeartbeat.IsZero() {
		t.Errorf("round-trip zero LastHeartbeat should be zero, got %v", back.LastHeartbeat)
	}
	if back.StepTimeout != 0 {
		t.Errorf("round-trip zero StepTimeout should be 0, got %v", back.StepTimeout)
	}
}
