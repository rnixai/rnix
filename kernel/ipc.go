package kernel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Message is a kernel-internal inter-process message.
type Message struct {
	FromPID   types.PID
	ToPID     types.PID
	Seq       types.MsgSeq
	Data      []byte
	CreatedAt time.Time
}

// IPCManager defines the kernel's inter-process communication interface.
type IPCManager interface {
	Send(senderPID, targetPID types.PID, data []byte) error
	Recv(pid types.PID) (*Message, error)
	Pipe(writerPID, readerPID types.PID) (writeFD, readFD types.FD, err error)
}

// Compile-time interface compliance check.
var _ IPCManager = (*KernelImpl)(nil)

// MessageQueue is a per-process receive queue for IPC messages.
type MessageQueue struct {
	mu       sync.Mutex
	messages []*Message
	notify   chan struct{}
	closed   bool
}

// newMessageQueue creates a new empty MessageQueue.
func newMessageQueue() *MessageQueue {
	return &MessageQueue{
		notify: make(chan struct{}, 1),
	}
}

// enqueue appends a message to the queue and signals any waiting receiver.
// Returns an error if the queue has been closed.
func (q *MessageQueue) enqueue(msg *Message) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return fmt.Errorf("message queue closed")
	}
	q.messages = append(q.messages, msg)
	q.mu.Unlock()

	// Non-blocking signal to wake up a blocked dequeue.
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

// dequeue blocks until a message is available or ctx is cancelled.
func (q *MessageQueue) dequeue(ctx context.Context) (*Message, error) {
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return nil, fmt.Errorf("message queue closed")
		}
		if len(q.messages) > 0 {
			msg := q.messages[0]
			q.messages = q.messages[1:]
			q.mu.Unlock()
			return msg, nil
		}
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-q.notify:
			// new message may be available, loop to check
		}
	}
}

// close marks the queue as closed and wakes blocked receivers.
func (q *MessageQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()

	// Wake any blocked dequeue calls.
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Send delivers a message from senderPID to targetPID.
func (k *KernelImpl) Send(senderPID, targetPID types.PID, data []byte) error {
	start := time.Now()

	// Validate sender exists
	senderProc, ok := k.GetProcess(senderPID)
	if !ok {
		return NewSyscallError("Send", senderPID, "",
			fmt.Errorf("sender process not found"), types.ErrNotFound)
	}

	// Validate target exists and is not Dead/Zombie
	targetProc, ok := k.GetProcess(targetPID)
	if !ok {
		return NewSyscallError("Send", senderPID, "",
			fmt.Errorf("target process %d not found", targetPID), types.ErrNotFound)
	}
	if state := targetProc.GetState(); state == types.StateZombie || state == types.StateDead {
		return NewSyscallError("Send", senderPID, "",
			fmt.Errorf("target process %d is %s", targetPID, state), types.ErrNotFound)
	}

	// Build message (copy data to prevent caller mutation)
	msg := &Message{
		FromPID:   senderPID,
		ToPID:     targetPID,
		Seq:       types.MsgSeq(k.msgSeq.Add(1)),
		Data:      append([]byte(nil), data...),
		CreatedAt: time.Now(),
	}

	// Enqueue to target's message queue
	queue, _ := k.msgQueues.LoadOrStore(targetPID, newMessageQueue())
	if err := queue.enqueue(msg); err != nil {
		return NewSyscallError("Send", senderPID, "",
			fmt.Errorf("target process %d queue closed", targetPID), types.ErrNotFound)
	}

	// SyscallEvent
	k.emitEvent(senderProc, "Send", map[string]any{
		"target_pid": targetPID,
		"msg_size":   len(data),
		"msg_seq":    msg.Seq,
	}, nil, nil, time.Since(start))

	return nil
}

// Recv blocks until a message is available for the given process.
func (k *KernelImpl) Recv(pid types.PID) (*Message, error) {
	start := time.Now()

	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, NewSyscallError("Recv", pid, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	queue, _ := k.msgQueues.LoadOrStore(pid, newMessageQueue())

	msg, err := queue.dequeue(proc.ctx)
	if err != nil {
		return nil, NewSyscallError("Recv", pid, "", err, types.ErrTimeout)
	}

	k.emitEvent(proc, "Recv", map[string]any{
		"from_pid": msg.FromPID,
		"msg_size": len(msg.Data),
		"msg_seq":  msg.Seq,
	}, msg, nil, time.Since(start))

	return msg, nil
}

// --- Pipe implementation ---

// DefaultPipeBufferSize is the default capacity for pipe buffers (64KB, matches Linux pipe default).
const DefaultPipeBufferSize = 64 * 1024

// pipeBuffer is the shared bounded buffer for a pipe.
// Writers block when the buffer is full until readers consume data (backpressure).
type pipeBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	capacity int
	rdNotify chan struct{} // signals reader: data available or write closed
	wrNotify chan struct{} // signals writer: space available or read closed
	wrClosed bool
	rdClosed bool
}

func newPipeBuffer(capacity int) *pipeBuffer {
	return &pipeBuffer{
		capacity: capacity,
		rdNotify: make(chan struct{}, 1),
		wrNotify: make(chan struct{}, 1),
	}
}

func (p *pipeBuffer) write(data []byte, cancelCh <-chan struct{}) (int, error) {
	written := 0
	for written < len(data) {
		p.mu.Lock()
		if p.rdClosed {
			p.mu.Unlock()
			return written, types.NewDriverError("Write", "pipe",
				fmt.Errorf("broken pipe"), types.ErrBrokenPipe)
		}
		if p.wrClosed {
			p.mu.Unlock()
			return written, types.NewDriverError("Write", "pipe",
				fmt.Errorf("write end closed"), types.ErrInvalid)
		}

		available := p.capacity - p.buf.Len()
		if available > 0 {
			chunk := data[written:]
			if len(chunk) > available {
				chunk = chunk[:available]
			}
			n, _ := p.buf.Write(chunk)
			written += n
			p.mu.Unlock()

			// Notify reader that data is available
			select {
			case p.rdNotify <- struct{}{}:
			default:
			}
			continue
		}
		p.mu.Unlock()

		// Buffer full — wait for reader to consume or cancellation
		select {
		case <-p.wrNotify:
		case <-cancelCh:
			return written, context.Canceled
		}
	}
	return written, nil
}

func (p *pipeBuffer) read(length int, cancelCh <-chan struct{}) ([]byte, error) {
	for {
		p.mu.Lock()
		if p.buf.Len() > 0 {
			data := make([]byte, min(length, p.buf.Len()))
			n, _ := p.buf.Read(data)
			p.mu.Unlock()

			// Notify blocked writers that space is available
			select {
			case p.wrNotify <- struct{}{}:
			default:
			}

			return data[:n], nil
		}
		if p.wrClosed {
			p.mu.Unlock()
			return nil, io.EOF
		}
		p.mu.Unlock()

		select {
		case <-p.rdNotify:
		case <-cancelCh:
			return nil, context.Canceled
		}
	}
}

func (p *pipeBuffer) closeWrite() {
	p.mu.Lock()
	p.wrClosed = true
	p.mu.Unlock()

	select {
	case p.rdNotify <- struct{}{}:
	default:
	}
}

func (p *pipeBuffer) closeRead() {
	p.mu.Lock()
	p.rdClosed = true
	p.mu.Unlock()

	select {
	case p.wrNotify <- struct{}{}:
	default:
	}
}

// pipeReadEnd is the read end of a pipe, implementing vfs.VFSFile.
type pipeReadEnd struct {
	pipe     *pipeBuffer
	cancelCh <-chan struct{}
	mu       sync.Mutex
	closed   bool
}

var _ vfs.VFSFile = (*pipeReadEnd)(nil)

func (r *pipeReadEnd) Read(length int) ([]byte, error) {
	return r.pipe.read(length, r.cancelCh)
}

func (r *pipeReadEnd) Write(_ context.Context, _ []byte) error {
	return types.NewDriverError("Write", "pipe:read",
		fmt.Errorf("cannot write to read end of pipe"), types.ErrInvalid)
}

func (r *pipeReadEnd) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.pipe.closeRead()
	return nil
}

func (r *pipeReadEnd) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{Name: "pipe:read", IsDevice: true, DevicePath: "pipe"}, nil
}

// pipeWriteEnd is the write end of a pipe, implementing vfs.VFSFile.
type pipeWriteEnd struct {
	pipe     *pipeBuffer
	cancelCh <-chan struct{}
	mu       sync.Mutex
	closed   bool
}

var _ vfs.VFSFile = (*pipeWriteEnd)(nil)

func (w *pipeWriteEnd) Read(_ int) ([]byte, error) {
	return nil, types.NewDriverError("Read", "pipe:write",
		fmt.Errorf("cannot read from write end of pipe"), types.ErrInvalid)
}

func (w *pipeWriteEnd) Write(ctx context.Context, data []byte) error {
	select {
	case <-ctx.Done():
		return types.NewDriverError("Write", "pipe",
			ctx.Err(), types.ErrTimeout)
	default:
	}

	_, err := w.pipe.write(data, w.cancelCh)
	return err
}

func (w *pipeWriteEnd) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	w.pipe.closeWrite()
	return nil
}

func (w *pipeWriteEnd) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{Name: "pipe:write", IsDevice: true, DevicePath: "pipe"}, nil
}

// Pipe creates a unidirectional data channel between two processes.
// The writeFD is registered in the writer's fdTable, and readFD in the reader's.
func (k *KernelImpl) Pipe(writerPID, readerPID types.PID) (writeFD, readFD types.FD, err error) {
	start := time.Now()

	// Validate writer exists and is active
	writerProc, ok := k.GetProcess(writerPID)
	if !ok {
		return 0, 0, NewSyscallError("Pipe", writerPID, "",
			fmt.Errorf("writer process not found"), types.ErrNotFound)
	}
	if state := writerProc.GetState(); state == types.StateZombie || state == types.StateDead {
		return 0, 0, NewSyscallError("Pipe", writerPID, "",
			fmt.Errorf("writer process %d is %s", writerPID, state), types.ErrNotFound)
	}

	// Validate reader exists and is active
	readerProc, ok := k.GetProcess(readerPID)
	if !ok {
		return 0, 0, NewSyscallError("Pipe", readerPID, "",
			fmt.Errorf("reader process not found"), types.ErrNotFound)
	}
	if state := readerProc.GetState(); state == types.StateZombie || state == types.StateDead {
		return 0, 0, NewSyscallError("Pipe", readerPID, "",
			fmt.Errorf("reader process %d is %s", readerPID, state), types.ErrNotFound)
	}

	// Create pipe buffer and endpoints
	pipe := newPipeBuffer(DefaultPipeBufferSize)
	writeEnd := &pipeWriteEnd{pipe: pipe, cancelCh: writerProc.ctx.Done()}
	readEnd := &pipeReadEnd{pipe: pipe, cancelCh: readerProc.ctx.Done()}

	// Register FDs via VFS
	wfd := k.vfs.RegisterFD(writerPID, writeEnd)
	rfd := k.vfs.RegisterFD(readerPID, readEnd)

	// Emit SyscallEvent
	k.emitEvent(writerProc, "Pipe", map[string]any{
		"writer_pid": writerPID,
		"reader_pid": readerPID,
		"write_fd":   wfd,
		"read_fd":    rfd,
	}, nil, nil, time.Since(start))

	return wfd, rfd, nil
}
