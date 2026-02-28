package kernel

import (
	gocontext "context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
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
		wg.Go(func() {
			err := k.Send(senders[i].PID, target.PID, []byte("msg"))
			if err != nil {
				t.Errorf("Send from PID %d failed: %v", senders[i].PID, err)
			}
		})
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

// TestSend_ToSelf verifies a process can send a message to itself.
func TestSend_ToSelf(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	payload := []byte("self-msg")
	if err := k.Send(proc.PID, proc.PID, payload); err != nil {
		t.Fatalf("Send to self failed: %v", err)
	}

	msg, err := k.Recv(proc.PID)
	if err != nil {
		t.Fatalf("Recv from self failed: %v", err)
	}
	if msg.FromPID != proc.PID || msg.ToPID != proc.PID {
		t.Errorf("FromPID=%d ToPID=%d, want both %d", msg.FromPID, msg.ToPID, proc.PID)
	}
	if string(msg.Data) != "self-msg" {
		t.Errorf("Data = %q, want %q", msg.Data, "self-msg")
	}
}

// TestSend_DataIsolation verifies Send copies data so caller mutation is safe.
func TestSend_DataIsolation(t *testing.T) {
	k := newSimpleKernel(t)
	a := newIPCTestProcess(t, k)
	b := newIPCTestProcess(t, k)

	data := []byte("original")
	if err := k.Send(a.PID, b.PID, data); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Mutate caller's slice after Send
	data[0] = 'X'

	msg, err := k.Recv(b.PID)
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if string(msg.Data) != "original" {
		t.Errorf("Data = %q, want %q (caller mutation leaked)", msg.Data, "original")
	}
}

// --- Pipe tests ---

// TestPipe_Basic verifies same-process pipe: write then read.
func TestPipe_Basic(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(proc.PID, proc.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}
	if wfd == 0 || rfd == 0 {
		t.Fatalf("expected non-zero FDs, got wfd=%d rfd=%d", wfd, rfd)
	}

	payload := []byte("hello pipe")
	if err := k.vfs.Write(proc.ctx, proc.PID, wfd, payload); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := k.vfs.Read(proc.PID, rfd, 1024)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(data) != "hello pipe" {
		t.Errorf("Data = %q, want %q", data, "hello pipe")
	}
}

// TestPipe_CrossProcess verifies data transfer between different processes.
func TestPipe_CrossProcess(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	payload := []byte("cross-process data")
	if err := k.vfs.Write(writer.ctx, writer.PID, wfd, payload); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := k.vfs.Read(reader.PID, rfd, 1024)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(data) != "cross-process data" {
		t.Errorf("Data = %q, want %q", data, "cross-process data")
	}
}

// TestPipe_WriteCloseEOF verifies closing write end causes reader to get EOF.
func TestPipe_WriteCloseEOF(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	// Write some data then close write end
	if err := k.vfs.Write(writer.ctx, writer.PID, wfd, []byte("before-close")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := k.vfs.Close(writer.PID, wfd); err != nil {
		t.Fatalf("Close write failed: %v", err)
	}

	// Read the buffered data first
	data, err := k.vfs.Read(reader.PID, rfd, 1024)
	if err != nil {
		t.Fatalf("Read buffered data failed: %v", err)
	}
	if string(data) != "before-close" {
		t.Errorf("Data = %q, want %q", data, "before-close")
	}

	// Next read should return EOF
	_, err = k.vfs.Read(reader.PID, rfd, 1024)
	if err == nil {
		t.Fatal("expected error after write end closed")
	}
	// VFS wraps io.EOF into a VFSError; verify EOF in Unwrap chain
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF in error chain, got: %v", err)
	}
}

// TestPipe_ReadCloseBrokenPipe verifies closing read end causes writer to get ErrBrokenPipe.
func TestPipe_ReadCloseBrokenPipe(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	// Close read end
	if err := k.vfs.Close(reader.PID, rfd); err != nil {
		t.Fatalf("Close read failed: %v", err)
	}

	// Write should fail with BrokenPipe
	err = k.vfs.Write(writer.ctx, writer.PID, wfd, []byte("broken"))
	if err == nil {
		t.Fatal("expected error after read end closed")
	}
	// The VFS wraps DriverError; extract the code
	var vfsErr *vfs.VFSError
	if !errors.As(err, &vfsErr) {
		t.Fatalf("expected *VFSError, got %T: %v", err, err)
	}
	if vfsErr.Code != types.ErrBrokenPipe {
		t.Errorf("Code = %v, want ErrBrokenPipe", vfsErr.Code)
	}
}

// TestPipe_BlockUntilData verifies read blocks until data is written.
func TestPipe_BlockUntilData(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	received := make(chan []byte, 1)
	go func() {
		data, err := k.vfs.Read(reader.PID, rfd, 1024)
		if err != nil {
			t.Errorf("Read failed: %v", err)
			return
		}
		received <- data
	}()

	// Give goroutine time to block
	time.Sleep(20 * time.Millisecond)

	select {
	case <-received:
		t.Fatal("Read should be blocking")
	default:
	}

	// Write data to unblock
	if err := k.vfs.Write(writer.ctx, writer.PID, wfd, []byte("unblock")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	select {
	case data := <-received:
		if string(data) != "unblock" {
			t.Errorf("Data = %q, want %q", data, "unblock")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not unblock after Write")
	}
}

// TestPipe_CancelUnblocksRead verifies process context cancel unblocks a blocked read.
func TestPipe_CancelUnblocksRead(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	_, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := k.vfs.Read(reader.PID, rfd, 1024)
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)

	// Cancel reader's context (simulates Kill)
	reader.Cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after context cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after context cancel")
	}
}

// TestPipe_Concurrent verifies multiple goroutines writing to same pipe without data loss or race.
func TestPipe_Concurrent(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	const n = 100
	msgSize := 64
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			data := make([]byte, msgSize)
			for i := range data {
				data[i] = 'A'
			}
			if err := k.vfs.Write(writer.ctx, writer.PID, wfd, data); err != nil {
				t.Errorf("Write failed: %v", err)
			}
		})
	}
	wg.Wait()

	// Close write end to signal EOF
	if err := k.vfs.Close(writer.PID, wfd); err != nil {
		t.Fatalf("Close write failed: %v", err)
	}

	// Read all data
	var totalRead int
	for {
		data, err := k.vfs.Read(reader.PID, rfd, 1024)
		if err != nil {
			break // EOF
		}
		totalRead += len(data)
	}

	expected := n * msgSize
	if totalRead != expected {
		t.Errorf("totalRead = %d, want %d", totalRead, expected)
	}
}

// TestPipe_LargeData verifies pipe can transfer ≥1MB of data (NFR23).
func TestPipe_LargeData(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	// Write 1MB
	dataSize := 1 << 20 // 1MB
	payload := make([]byte, dataSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	start := time.Now()
	go func() {
		if err := k.vfs.Write(writer.ctx, writer.PID, wfd, payload); err != nil {
			t.Errorf("Write failed: %v", err)
		}
		_ = k.vfs.Close(writer.PID, wfd)
	}()

	// Read all data
	var result []byte
	for {
		data, err := k.vfs.Read(reader.PID, rfd, 64*1024)
		if err != nil {
			break
		}
		result = append(result, data...)
	}
	elapsed := time.Since(start)

	if len(result) != dataSize {
		t.Fatalf("read %d bytes, want %d", len(result), dataSize)
	}

	// Verify content
	for i, b := range result {
		if b != byte(i%256) {
			t.Fatalf("byte %d: got %d, want %d", i, b, i%256)
		}
	}

	// NFR23: throughput ≥ 1MB/s (should be >> 100MB/s for in-memory)
	throughputMBps := float64(dataSize) / elapsed.Seconds() / (1 << 20)
	if throughputMBps < 1.0 {
		t.Errorf("throughput %.2f MB/s < 1 MB/s (NFR23)", throughputMBps)
	}
}

// TestPipe_InvalidPID verifies Pipe returns ErrNotFound for non-existent PIDs.
func TestPipe_InvalidPID(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newIPCTestProcess(t, k)

	// Invalid writer PID
	_, _, err := k.Pipe(99999, proc.PID)
	if err == nil {
		t.Fatal("expected error for invalid writer PID")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
	if se.Syscall != "Pipe" {
		t.Errorf("Syscall = %q, want Pipe", se.Syscall)
	}

	// Invalid reader PID
	_, _, err = k.Pipe(proc.PID, 99999)
	if err == nil {
		t.Fatal("expected error for invalid reader PID")
	}
	se, ok = err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestPipe_DeadProcess verifies Pipe returns ErrNotFound for Zombie/Dead processes.
func TestPipe_DeadProcess(t *testing.T) {
	k := newSimpleKernel(t)
	alive := newIPCTestProcess(t, k)
	zombie := newIPCTestProcess(t, k)

	// Transition to Zombie
	_ = zombie.Terminate(ExitStatus{Code: 0, Reason: "done"})

	// Zombie as writer
	_, _, err := k.Pipe(zombie.PID, alive.PID)
	if err == nil {
		t.Fatal("expected error for zombie writer")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}

	// Zombie as reader
	_, _, err = k.Pipe(alive.PID, zombie.PID)
	if err == nil {
		t.Fatal("expected error for zombie reader")
	}
	se, ok = err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound", se.Code)
	}
}

// TestPipe_SyscallEvent verifies DebugChan receives Pipe SyscallEvent.
func TestPipe_SyscallEvent(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	_, _, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	// Check writer's DebugChan for Pipe event
	select {
	case ev := <-writer.DebugChan:
		if ev.Syscall != "Pipe" {
			t.Errorf("Syscall = %q, want Pipe", ev.Syscall)
		}
		if ev.PID != writer.PID {
			t.Errorf("PID = %d, want %d", ev.PID, writer.PID)
		}
		if ev.Args["writer_pid"] != writer.PID {
			t.Errorf("Args[writer_pid] = %v, want %d", ev.Args["writer_pid"], writer.PID)
		}
		if ev.Args["reader_pid"] != reader.PID {
			t.Errorf("Args[reader_pid] = %v, want %d", ev.Args["reader_pid"], reader.PID)
		}
	case <-time.After(time.Second):
		t.Fatal("no Pipe SyscallEvent received")
	}
}

// TestPipe_DoubleClose verifies double-closing a pipe end doesn't panic (idempotent).
func TestPipe_DoubleClose(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	// Close write end twice via VFS
	if err := k.vfs.Close(writer.PID, wfd); err != nil {
		t.Fatalf("First close write failed: %v", err)
	}
	// Second close via VFS will fail (FD removed from table), but should not panic
	_ = k.vfs.Close(writer.PID, wfd)

	// Close read end twice via VFS
	if err := k.vfs.Close(reader.PID, rfd); err != nil {
		t.Fatalf("First close read failed: %v", err)
	}
	_ = k.vfs.Close(reader.PID, rfd)
}

// TestPipe_WrongDirection verifies writing to read-end and reading from write-end return ErrInvalid.
func TestPipe_WrongDirection(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}

	// Write to read-end should fail with ErrInvalid
	err = k.vfs.Write(reader.ctx, reader.PID, rfd, []byte("wrong"))
	if err == nil {
		t.Fatal("expected error writing to read end")
	}
	var writeErr *vfs.VFSError
	if !errors.As(err, &writeErr) {
		t.Fatalf("expected *VFSError, got %T: %v", err, err)
	}
	if writeErr.Code != types.ErrInvalid {
		t.Errorf("Code = %v, want ErrInvalid", writeErr.Code)
	}

	// Read from write-end should fail with ErrInvalid
	_, err = k.vfs.Read(writer.PID, wfd, 1024)
	if err == nil {
		t.Fatal("expected error reading from write end")
	}
	var readErr *vfs.VFSError
	if !errors.As(err, &readErr) {
		t.Fatalf("expected *VFSError, got %T: %v", err, err)
	}
	if readErr.Code != types.ErrInvalid {
		t.Errorf("Code = %v, want ErrInvalid", readErr.Code)
	}
}

// TestPipe_WriteAfterWriteClose verifies writing after write-end is closed returns error.
func TestPipe_WriteAfterWriteClose(t *testing.T) {
	k := newSimpleKernel(t)
	writer := newIPCTestProcess(t, k)
	reader := newIPCTestProcess(t, k)

	wfd, rfd, err := k.Pipe(writer.PID, reader.PID)
	if err != nil {
		t.Fatalf("Pipe failed: %v", err)
	}
	_ = rfd // keep reader side open

	// Close write end
	if err := k.vfs.Close(writer.PID, wfd); err != nil {
		t.Fatalf("Close write failed: %v", err)
	}

	// Write via VFS after close → FD removed from table → ErrNotFound
	err = k.vfs.Write(writer.ctx, writer.PID, wfd, []byte("after-close"))
	if err == nil {
		t.Fatal("expected error writing after write-end closed")
	}
	var vfsErr *vfs.VFSError
	if !errors.As(err, &vfsErr) {
		t.Fatalf("expected *VFSError, got %T: %v", err, err)
	}
	if vfsErr.Code != types.ErrNotFound {
		t.Errorf("Code = %v, want ErrNotFound (FD removed)", vfsErr.Code)
	}
}
