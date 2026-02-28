package kernel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gonewx/crux/internal/types"
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
