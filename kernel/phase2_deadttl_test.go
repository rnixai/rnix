package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// --- BUG-002: Dead Process TTL Cleanup Tests ---

func TestCleanupExpiredDead_RemovesExpired(t *testing.T) {
	k := newSimpleKernel(t)

	// Create a Dead process with DeadAt well past TTL
	p := NewProcess(0, "expired", nil)
	p.State = types.StateDead
	p.DeadAt = time.Now().Add(-2 * time.Minute)
	k.AddProcess(p)

	if _, ok := k.GetProcess(p.PID); !ok {
		t.Fatal("process should exist before cleanup")
	}

	k.cleanupExpiredDead(DeadProcessTTL)

	if _, ok := k.GetProcess(p.PID); ok {
		t.Error("expired dead process should have been removed")
	}
}

func TestCleanupExpiredDead_KeepsRecentDead(t *testing.T) {
	k := newSimpleKernel(t)

	// Create a recently Dead process (within TTL)
	p := NewProcess(0, "recent", nil)
	p.State = types.StateDead
	p.DeadAt = time.Now().Add(-5 * time.Second)
	k.AddProcess(p)

	k.cleanupExpiredDead(DeadProcessTTL)

	if _, ok := k.GetProcess(p.PID); !ok {
		t.Error("recent dead process should NOT have been removed")
	}
}

func TestCleanupExpiredDead_IgnoresNonDead(t *testing.T) {
	k := newSimpleKernel(t)

	// Add processes in non-Dead states
	running := NewProcess(0, "running", nil)
	running.State = types.StateRunning
	k.AddProcess(running)

	created := NewProcess(0, "created", nil)
	// State is Created (default)
	k.AddProcess(created)

	zombie := NewProcess(0, "zombie", nil)
	zombie.State = types.StateZombie
	k.AddProcess(zombie)

	// Use TTL=0 so everything "expired" would be removed if it matched
	k.cleanupExpiredDead(0)

	if _, ok := k.GetProcess(running.PID); !ok {
		t.Error("running process should not have been removed")
	}
	if _, ok := k.GetProcess(created.PID); !ok {
		t.Error("created process should not have been removed")
	}
	if _, ok := k.GetProcess(zombie.PID); !ok {
		t.Error("zombie process should not have been removed")
	}
}

func TestCleanupExpiredDead_IgnoresDeadWithZeroDeadAt(t *testing.T) {
	k := newSimpleKernel(t)

	// Dead process with zero DeadAt (defensive: should not happen in normal flow)
	p := NewProcess(0, "dead-no-deadat", nil)
	p.State = types.StateDead
	// p.DeadAt is zero value
	k.AddProcess(p)

	k.cleanupExpiredDead(0) // TTL=0 means everything older than now

	// Should NOT be removed because DeadAt.IsZero() check prevents it
	if _, ok := k.GetProcess(p.PID); !ok {
		t.Error("dead process with zero DeadAt should not have been removed")
	}
}

func TestCleanupExpiredDead_MixedStates(t *testing.T) {
	k := newSimpleKernel(t)

	// Mix of expired-dead, recent-dead, running, and zombie
	expiredDead := NewProcess(0, "expired-dead", nil)
	expiredDead.State = types.StateDead
	expiredDead.DeadAt = time.Now().Add(-2 * time.Minute)
	k.AddProcess(expiredDead)

	recentDead := NewProcess(0, "recent-dead", nil)
	recentDead.State = types.StateDead
	recentDead.DeadAt = time.Now().Add(-1 * time.Second)
	k.AddProcess(recentDead)

	running := NewProcess(0, "running", nil)
	running.State = types.StateRunning
	k.AddProcess(running)

	k.cleanupExpiredDead(DeadProcessTTL)

	// Only the expired dead should be removed
	if _, ok := k.GetProcess(expiredDead.PID); ok {
		t.Error("expired dead process should have been removed")
	}
	if _, ok := k.GetProcess(recentDead.PID); !ok {
		t.Error("recent dead process should still exist")
	}
	if _, ok := k.GetProcess(running.PID); !ok {
		t.Error("running process should still exist")
	}
}
