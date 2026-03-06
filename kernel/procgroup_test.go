package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/usecrux/crux/internal/types"
)

// --- Task 7: Process Group Unit Tests ---

// TestJoinGroup_Basic verifies a process can join a group and be found via GetProcGroup.
func TestJoinGroup_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var groupID types.PGID = 100
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	members, err := k.GetProcGroup(groupID)
	if err != nil {
		t.Fatalf("GetProcGroup failed: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0] != proc.PID {
		t.Errorf("member PID = %d, want %d", members[0], proc.PID)
	}
}

// TestJoinGroup_MultipleProcesses verifies multiple processes can join the same group.
func TestJoinGroup_MultipleProcesses(t *testing.T) {
	k := newSimpleKernel(t)
	p1 := newIPCTestProcess(t, k)
	p2 := newIPCTestProcess(t, k)
	p3 := newIPCTestProcess(t, k)

	var groupID types.PGID = 200
	for _, p := range []*Process{p1, p2, p3} {
		if err := k.JoinGroup(p.PID, groupID); err != nil {
			t.Fatalf("JoinGroup PID %d failed: %v", p.PID, err)
		}
	}

	members, err := k.GetProcGroup(groupID)
	if err != nil {
		t.Fatalf("GetProcGroup failed: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}

	pidSet := make(map[types.PID]bool)
	for _, pid := range members {
		pidSet[pid] = true
	}
	for _, p := range []*Process{p1, p2, p3} {
		if !pidSet[p.PID] {
			t.Errorf("PID %d not found in group members", p.PID)
		}
	}
}

// TestJoinGroup_MultipleGroups verifies a process can join multiple groups independently.
func TestJoinGroup_MultipleGroups(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var g1 types.PGID = 10
	var g2 types.PGID = 20
	if err := k.JoinGroup(proc.PID, g1); err != nil {
		t.Fatalf("JoinGroup g1 failed: %v", err)
	}
	if err := k.JoinGroup(proc.PID, g2); err != nil {
		t.Fatalf("JoinGroup g2 failed: %v", err)
	}

	m1, err := k.GetProcGroup(g1)
	if err != nil {
		t.Fatalf("GetProcGroup g1 failed: %v", err)
	}
	if len(m1) != 1 || m1[0] != proc.PID {
		t.Errorf("g1 members = %v, want [%d]", m1, proc.PID)
	}

	m2, err := k.GetProcGroup(g2)
	if err != nil {
		t.Fatalf("GetProcGroup g2 failed: %v", err)
	}
	if len(m2) != 1 || m2[0] != proc.PID {
		t.Errorf("g2 members = %v, want [%d]", m2, proc.PID)
	}

	groups := proc.GetGroups()
	if len(groups) != 2 {
		t.Errorf("process groups = %v, want 2 entries", groups)
	}
}

// TestJoinGroup_AutoCreate verifies that the first JoinGroup auto-creates the group.
func TestJoinGroup_AutoCreate(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var groupID types.PGID = 999
	// Group doesn't exist yet — GetProcGroup should fail
	_, err := k.GetProcGroup(groupID)
	if err == nil {
		t.Fatal("expected error for non-existent group")
	}

	// JoinGroup auto-creates
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	members, err := k.GetProcGroup(groupID)
	if err != nil {
		t.Fatalf("GetProcGroup failed after auto-create: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("expected 1 member after auto-create, got %d", len(members))
	}
}

// TestJoinGroup_InvalidPID verifies JoinGroup returns ErrNotFound for non-existent or dead PIDs.
func TestJoinGroup_InvalidPID(t *testing.T) {
	k := newSimpleKernel(t)

	// Non-existent PID
	err := k.JoinGroup(99999, 1)
	if err == nil {
		t.Fatal("expected error for non-existent PID")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}

	// Zombie PID
	zombie := newIPCTestProcess(t, k)
	_ = zombie.Terminate(ExitStatus{Code: 0, Reason: "done"})
	err = k.JoinGroup(zombie.PID, 1)
	if err == nil {
		t.Fatal("expected error for zombie PID")
	}
	se, ok = err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestJoinGroup_Duplicate verifies duplicate JoinGroup is idempotent (no error).
func TestJoinGroup_Duplicate(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var groupID types.PGID = 50
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("first JoinGroup failed: %v", err)
	}
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("duplicate JoinGroup failed: %v", err)
	}

	members, err := k.GetProcGroup(groupID)
	if err != nil {
		t.Fatalf("GetProcGroup failed: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("expected 1 member (idempotent), got %d", len(members))
	}
}

// TestLeaveGroup_Basic verifies LeaveGroup removes a process from the group.
func TestLeaveGroup_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	p1 := newIPCTestProcess(t, k)
	p2 := newIPCTestProcess(t, k)

	var groupID types.PGID = 300
	if err := k.JoinGroup(p1.PID, groupID); err != nil {
		t.Fatalf("JoinGroup p1 failed: %v", err)
	}
	if err := k.JoinGroup(p2.PID, groupID); err != nil {
		t.Fatalf("JoinGroup p2 failed: %v", err)
	}

	if err := k.LeaveGroup(p1.PID, groupID); err != nil {
		t.Fatalf("LeaveGroup p1 failed: %v", err)
	}

	members, err := k.GetProcGroup(groupID)
	if err != nil {
		t.Fatalf("GetProcGroup failed: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member after leave, got %d", len(members))
	}
	if members[0] != p2.PID {
		t.Errorf("remaining member = %d, want %d", members[0], p2.PID)
	}
}

// TestLeaveGroup_AutoDestroy verifies the group is auto-destroyed when the last member leaves.
func TestLeaveGroup_AutoDestroy(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var groupID types.PGID = 400
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	if err := k.LeaveGroup(proc.PID, groupID); err != nil {
		t.Fatalf("LeaveGroup failed: %v", err)
	}

	// Group should be auto-destroyed
	_, err := k.GetProcGroup(groupID)
	if err == nil {
		t.Fatal("expected error for auto-destroyed group")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestLeaveGroup_NotInGroup verifies leaving a group the process isn't in returns ErrNotFound.
func TestLeaveGroup_NotInGroup(t *testing.T) {
	k := newSimpleKernel(t)
	p1 := newIPCTestProcess(t, k)
	p2 := newIPCTestProcess(t, k)

	var groupID types.PGID = 500
	if err := k.JoinGroup(p1.PID, groupID); err != nil {
		t.Fatalf("JoinGroup p1 failed: %v", err)
	}

	// p2 never joined
	err := k.LeaveGroup(p2.PID, groupID)
	if err == nil {
		t.Fatal("expected error for process not in group")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestLeaveGroup_GroupNotFound verifies leaving a non-existent group returns ErrNotFound.
func TestLeaveGroup_GroupNotFound(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	err := k.LeaveGroup(proc.PID, 99999)
	if err == nil {
		t.Fatal("expected error for non-existent group")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
	if se.Syscall != "LeaveGroup" {
		t.Errorf("Syscall = %q, want LeaveGroup", se.Syscall)
	}
}

// TestGetProcGroup_NotFound verifies querying a non-existent group returns ErrNotFound.
func TestGetProcGroup_NotFound(t *testing.T) {
	k := newSimpleKernel(t)

	_, err := k.GetProcGroup(99999)
	if err == nil {
		t.Fatal("expected error for non-existent group")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
	if se.Syscall != "GetProcGroup" {
		t.Errorf("Syscall = %q, want GetProcGroup", se.Syscall)
	}
}

// TestSignalGroup_Basic verifies SignalGroup sends Kill to all group members.
func TestSignalGroup_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	p1 := newIPCTestProcess(t, k)
	p2 := newIPCTestProcess(t, k)
	p3 := newIPCTestProcess(t, k)

	var groupID types.PGID = 600
	for _, p := range []*Process{p1, p2, p3} {
		if err := k.JoinGroup(p.PID, groupID); err != nil {
			t.Fatalf("JoinGroup PID %d failed: %v", p.PID, err)
		}
	}

	if err := k.SignalGroup(groupID, types.SIGTERM); err != nil {
		t.Fatalf("SignalGroup failed: %v", err)
	}

	// All processes should have their context cancelled
	for _, p := range []*Process{p1, p2, p3} {
		select {
		case <-p.ctx.Done():
			// expected
		default:
			t.Errorf("PID %d context not cancelled after SignalGroup", p.PID)
		}
	}
}

// TestSignalGroup_PartialExit verifies SignalGroup succeeds even if some members already exited.
func TestSignalGroup_PartialExit(t *testing.T) {
	k := newSimpleKernel(t)
	p1 := newIPCTestProcess(t, k)
	p2 := newIPCTestProcess(t, k)

	var groupID types.PGID = 700
	if err := k.JoinGroup(p1.PID, groupID); err != nil {
		t.Fatalf("JoinGroup p1 failed: %v", err)
	}
	if err := k.JoinGroup(p2.PID, groupID); err != nil {
		t.Fatalf("JoinGroup p2 failed: %v", err)
	}

	// Terminate p1 so Kill(p1) returns noop
	_ = p1.Terminate(ExitStatus{Code: 0, Reason: "done"})

	// SignalGroup should still succeed
	if err := k.SignalGroup(groupID, types.SIGTERM); err != nil {
		t.Fatalf("SignalGroup failed: %v", err)
	}

	// p2 should have its context cancelled
	select {
	case <-p2.ctx.Done():
		// expected
	default:
		t.Error("p2 context not cancelled after SignalGroup")
	}
}

// TestSignalGroup_EmptyGroup verifies empty group returns ErrNotFound (auto-destroyed).
func TestSignalGroup_EmptyGroup(t *testing.T) {
	k := newSimpleKernel(t)

	// Group 800 never created
	err := k.SignalGroup(800, types.SIGTERM)
	if err == nil {
		t.Fatal("expected error for non-existent group")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestSignalGroup_InvalidSignal verifies invalid signal returns ErrInvalid.
func TestSignalGroup_InvalidSignal(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var groupID types.PGID = 900
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	err := k.SignalGroup(groupID, types.Signal(99))
	if err == nil {
		t.Fatal("expected error for invalid signal")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrInvalid {
		t.Errorf("Code = %v, want ErrInvalid", se.Code)
	}
}

// TestReapProcess_AutoRemove verifies that when a process is reaped, it's removed from all groups.
func TestReapProcess_AutoRemove(t *testing.T) {
	k := newSimpleKernel(t)
	p1 := newIPCTestProcess(t, k)
	p2 := newIPCTestProcess(t, k)

	var g1 types.PGID = 1000
	var g2 types.PGID = 1001
	if err := k.JoinGroup(p1.PID, g1); err != nil {
		t.Fatalf("JoinGroup g1 failed: %v", err)
	}
	if err := k.JoinGroup(p1.PID, g2); err != nil {
		t.Fatalf("JoinGroup g2 failed: %v", err)
	}
	if err := k.JoinGroup(p2.PID, g1); err != nil {
		t.Fatalf("JoinGroup p2 to g1 failed: %v", err)
	}

	// Reap p1 through the full reapProcess path
	_ = p1.Terminate(ExitStatus{Code: 0, Reason: "done"})
	k.reapProcess(p1)

	// g1 should only contain p2
	members, err := k.GetProcGroup(g1)
	if err != nil {
		t.Fatalf("GetProcGroup g1 failed: %v", err)
	}
	if len(members) != 1 || members[0] != p2.PID {
		t.Errorf("g1 members = %v, want [%d]", members, p2.PID)
	}

	// g2 should be auto-destroyed (p1 was the only member)
	_, err = k.GetProcGroup(g2)
	if err == nil {
		t.Fatal("expected error for auto-destroyed g2")
	}
}

// TestReapProcess_GroupAutoDestroy verifies that when the last member exits, the group is destroyed.
func TestReapProcess_GroupAutoDestroy(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var groupID types.PGID = 1100
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	// Reap through the full reapProcess path
	_ = proc.Terminate(ExitStatus{Code: 0, Reason: "done"})
	k.reapProcess(proc)

	_, err := k.GetProcGroup(groupID)
	if err == nil {
		t.Fatal("expected error for auto-destroyed group after last member exit")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestProcGroup_Concurrent verifies 100 goroutines can concurrently JoinGroup/LeaveGroup/GetProcGroup without race.
func TestProcGroup_Concurrent(t *testing.T) {
	k := newSimpleKernel(t)
	const n = 100
	procs := make([]*Process, n)
	for i := range n {
		procs[i] = newIPCTestProcess(t, k)
	}

	var groupID types.PGID = 1200
	var wg sync.WaitGroup

	// Concurrent JoinGroup
	for i := range n {
		wg.Go(func() {
			if err := k.JoinGroup(procs[i].PID, groupID); err != nil {
				t.Errorf("JoinGroup PID %d failed: %v", procs[i].PID, err)
			}
		})
	}
	wg.Wait()

	members, err := k.GetProcGroup(groupID)
	if err != nil {
		t.Fatalf("GetProcGroup failed: %v", err)
	}
	if len(members) != n {
		t.Errorf("expected %d members, got %d", n, len(members))
	}

	// Concurrent LeaveGroup (first half) + GetProcGroup (second half)
	for i := range n {
		if i < n/2 {
			wg.Go(func() {
				_ = k.LeaveGroup(procs[i].PID, groupID)
			})
		} else {
			wg.Go(func() {
				_, _ = k.GetProcGroup(groupID)
			})
		}
	}
	wg.Wait()

	members, err = k.GetProcGroup(groupID)
	if err != nil {
		t.Fatalf("GetProcGroup failed after concurrent ops: %v", err)
	}
	if len(members) != n/2 {
		t.Errorf("expected %d members after half leave, got %d", n/2, len(members))
	}
}

// TestSignalGroup_SyscallEvent verifies DebugChan receives JoinGroup and SignalGroup events.
func TestSignalGroup_SyscallEvent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	var groupID types.PGID = 1300
	if err := k.JoinGroup(proc.PID, groupID); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	// Drain JoinGroup event
	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "JoinGroup" {
			t.Errorf("Syscall = %q, want JoinGroup", ev.Syscall)
		}
		if ev.PID != proc.PID {
			t.Errorf("PID = %d, want %d", ev.PID, proc.PID)
		}
	case <-time.After(time.Second):
		t.Fatal("no JoinGroup SyscallEvent received")
	}

	if err := k.SignalGroup(groupID, types.SIGTERM); err != nil {
		t.Fatalf("SignalGroup failed: %v", err)
	}

	// Drain Kill event (SignalGroup calls Kill internally)
	foundKill := false
	foundSignalGroup := false
	timeout := time.After(time.Second)
	for !foundKill || !foundSignalGroup {
		select {
		case ev := <-proc.DebugChan:
			switch ev.Syscall {
			case "Kill":
				foundKill = true
			case "SignalGroup":
				foundSignalGroup = true
				if ev.Args["group_id"] != groupID {
					t.Errorf("Args[group_id] = %v, want %d", ev.Args["group_id"], groupID)
				}
			}
		case <-timeout:
			t.Fatalf("timeout waiting for events: foundKill=%v foundSignalGroup=%v", foundKill, foundSignalGroup)
		}
	}
}

// TestSignalGroup_Performance verifies SignalGroup for 10 processes completes within 2x single Kill time (NFR24).
func TestSignalGroup_Performance(t *testing.T) {
	k := newSimpleKernel(t)
	const n = 10
	procs := make([]*Process, n)
	for i := range n {
		procs[i] = newIPCTestProcess(t, k)
	}

	// Measure single Kill time
	singleProc := newIPCTestProcess(t, k)
	singleStart := time.Now()
	if err := k.Kill(singleProc.PID, types.SIGTERM); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	singleDuration := time.Since(singleStart)

	// Reset: create fresh processes for group test
	groupProcs := make([]*Process, n)
	for i := range n {
		groupProcs[i] = newIPCTestProcess(t, k)
	}

	var groupID types.PGID = 1400
	for _, p := range groupProcs {
		if err := k.JoinGroup(p.PID, groupID); err != nil {
			t.Fatalf("JoinGroup failed: %v", err)
		}
	}

	groupStart := time.Now()
	if err := k.SignalGroup(groupID, types.SIGTERM); err != nil {
		t.Fatalf("SignalGroup failed: %v", err)
	}
	groupDuration := time.Since(groupStart)

	// NFR24: group signal should be ≤ 2x single kill
	threshold := singleDuration * 2
	if threshold < time.Millisecond {
		threshold = time.Millisecond // minimum threshold for very fast operations
	}
	if groupDuration > threshold {
		t.Errorf("SignalGroup(%d procs) took %v, > 2x single Kill (%v). Threshold: %v",
			n, groupDuration, singleDuration, threshold)
	}
}
