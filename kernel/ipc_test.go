package kernel

import (
	gocontext "context"
	"sync"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// newIPCTestProcess creates a lightweight process registered in the kernel for IPC tests.
// The process is in Running state with a live context.
func newIPCTestProcess(t *testing.T, k *KernelImpl) *Process {
	t.Helper()
	proc := NewProcess(0, "ipc-test", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	_ = proc.Start() // Created → Running
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	return proc
}

// TestSend_Basic verifies A sends to B and B receives the message.
func TestSend_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	a := newIPCTestProcess(t, k)
	b := newIPCTestProcess(t, k)

	payload := []byte("hello from A")
	if err := k.Send(a.PID, b.PID, payload); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	msg, err := k.Recv(b.PID)
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if msg.FromPID != a.PID {
		t.Errorf("FromPID = %d, want %d", msg.FromPID, a.PID)
	}
	if msg.ToPID != b.PID {
		t.Errorf("ToPID = %d, want %d", msg.ToPID, b.PID)
	}
	if string(msg.Data) != string(payload) {
		t.Errorf("Data = %q, want %q", msg.Data, payload)
	}
	if msg.Seq == 0 {
		t.Error("Seq should be > 0")
	}
	if msg.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

// TestSend_TargetNotFound verifies Send returns ErrNotFound for non-existent PID.
func TestSend_TargetNotFound(t *testing.T) {
	k := newSimpleKernel(t)
	sender := newIPCTestProcess(t, k)

	err := k.Send(sender.PID, 99999, []byte("msg"))
	if err == nil {
		t.Fatal("expected error for non-existent target")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
	if se.Syscall != "Send" {
		t.Errorf("Syscall = %q, want Send", se.Syscall)
	}
}

// TestRecv_BlockUntilMessage verifies Recv blocks until a message arrives.
func TestRecv_BlockUntilMessage(t *testing.T) {
	k := newSimpleKernel(t)
	a := newIPCTestProcess(t, k)
	b := newIPCTestProcess(t, k)

	received := make(chan *Message, 1)
	go func() {
		msg, err := k.Recv(b.PID)
		if err != nil {
			t.Errorf("Recv failed: %v", err)
			return
		}
		received <- msg
	}()

	// Give goroutine time to block
	time.Sleep(20 * time.Millisecond)

	select {
	case <-received:
		t.Fatal("Recv should be blocking")
	default:
	}

	// Now send
	if err := k.Send(a.PID, b.PID, []byte("wake up")); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg.Data) != "wake up" {
			t.Errorf("Data = %q, want %q", msg.Data, "wake up")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Recv did not unblock after Send")
	}
}

// TestRecv_ContextCancel verifies Recv returns error when context is cancelled.
func TestRecv_ContextCancel(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	errCh := make(chan error, 1)
	go func() {
		_, err := k.Recv(proc.PID)
		errCh <- err
	}()

	// Give goroutine time to block
	time.Sleep(20 * time.Millisecond)

	// Cancel context (simulates Kill)
	proc.Cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after context cancel")
		}
		se, ok := err.(*SyscallError)
		if !ok {
			t.Fatalf("expected *SyscallError, got %T", err)
		}
		if se.Code != types.ErrTimeout {
			t.Errorf("Code = %v, want ErrTimeout", se.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Recv did not return after context cancel")
	}
}

// TestSend_Concurrent verifies 100 concurrent Sends all arrive without race.
func TestSend_Concurrent(t *testing.T) {
	k := newSimpleKernel(t)
	target := newIPCTestProcess(t, k)

	const n = 100
	senders := make([]*Process, n)
	for i := range n {
		senders[i] = newIPCTestProcess(t, k)
	}

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := k.Send(senders[i].PID, target.PID, []byte("msg"))
			if err != nil {
				t.Errorf("Send from PID %d failed: %v", senders[i].PID, err)
			}
		}()
	}
	wg.Wait()

	// Verify all messages arrived
	for range n {
		msg, err := k.Recv(target.PID)
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
		if string(msg.Data) != "msg" {
			t.Errorf("Data = %q, want %q", msg.Data, "msg")
		}
	}
}

// TestRecv_MultipleMessages verifies FIFO ordering.
func TestRecv_MultipleMessages(t *testing.T) {
	k := newSimpleKernel(t)
	sender := newIPCTestProcess(t, k)
	receiver := newIPCTestProcess(t, k)

	for i := range 5 {
		data := []byte{byte(i)}
		if err := k.Send(sender.PID, receiver.PID, data); err != nil {
			t.Fatalf("Send %d failed: %v", i, err)
		}
	}

	var prevSeq types.MsgSeq
	for i := range 5 {
		msg, err := k.Recv(receiver.PID)
		if err != nil {
			t.Fatalf("Recv %d failed: %v", i, err)
		}
		if len(msg.Data) != 1 || msg.Data[0] != byte(i) {
			t.Errorf("message %d: Data = %v, want [%d]", i, msg.Data, i)
		}
		if msg.Seq <= prevSeq {
			t.Errorf("message %d: Seq %d not > prev %d (FIFO violated)", i, msg.Seq, prevSeq)
		}
		prevSeq = msg.Seq
	}
}

// TestSend_DeadProcess verifies Send to Zombie/Dead returns ErrNotFound.
func TestSend_DeadProcess(t *testing.T) {
	k := newSimpleKernel(t)
	sender := newIPCTestProcess(t, k)
	target := newIPCTestProcess(t, k)

	// Transition target to Zombie
	_ = target.Terminate(ExitStatus{Code: 0, Reason: "done"})

	err := k.Send(sender.PID, target.PID, []byte("msg"))
	if err == nil {
		t.Fatal("expected error sending to zombie process")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestMessageQueue_Close verifies that closing a queue unblocks Recv.
func TestMessageQueue_Close(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	errCh := make(chan error, 1)
	go func() {
		_, err := k.Recv(proc.PID)
		errCh <- err
	}()

	// Give goroutine time to block
	time.Sleep(20 * time.Millisecond)

	// Close the queue directly
	queue, ok := k.msgQueues.Load(proc.PID)
	if !ok {
		t.Fatal("queue not found")
	}
	queue.close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after queue close")
		}
		se, ok := err.(*SyscallError)
		if !ok {
			t.Fatalf("expected *SyscallError, got %T", err)
		}
		if se.Code != types.ErrTimeout {
			t.Errorf("Code = %v, want ErrTimeout", se.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Recv did not return after queue close")
	}
}

// TestSend_SyscallEvent verifies DebugChan receives Send/Recv events.
func TestSend_SyscallEvent(t *testing.T) {
	k := newSimpleKernel(t)
	sender := newIPCTestProcess(t, k)
	receiver := newIPCTestProcess(t, k)

	if err := k.Send(sender.PID, receiver.PID, []byte("event-test")); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Check sender's DebugChan for Send event
	select {
	case ev := <-sender.DebugChan:
		if ev.Syscall != "Send" {
			t.Errorf("Syscall = %q, want Send", ev.Syscall)
		}
		if ev.PID != sender.PID {
			t.Errorf("PID = %d, want %d", ev.PID, sender.PID)
		}
	case <-time.After(time.Second):
		t.Fatal("no Send SyscallEvent received")
	}

	// Recv should also emit an event
	msg, err := k.Recv(receiver.PID)
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if string(msg.Data) != "event-test" {
		t.Errorf("Data = %q, want %q", msg.Data, "event-test")
	}

	select {
	case ev := <-receiver.DebugChan:
		if ev.Syscall != "Recv" {
			t.Errorf("Syscall = %q, want Recv", ev.Syscall)
		}
		if ev.PID != receiver.PID {
			t.Errorf("PID = %d, want %d", ev.PID, receiver.PID)
		}
	case <-time.After(time.Second):
		t.Fatal("no Recv SyscallEvent received")
	}
}
