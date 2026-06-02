package ipc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/kernel"
)

// callbackMux routes KernelCallbacks events to per-PID channels for streaming to clients.
type callbackMux struct {
	handlers    *xsync.SyncMap[types.PID, chan<- StreamEvent]
	pendingAsks *xsync.SyncMap[string, chan []byte]
	done        chan struct{}
}

func newCallbackMux() *callbackMux {
	return &callbackMux{
		handlers:    xsync.NewSyncMap[types.PID, chan<- StreamEvent](),
		pendingAsks: xsync.NewSyncMap[string, chan []byte](),
		done:        make(chan struct{}),
	}
}

func (m *callbackMux) register(pid types.PID, ch chan<- StreamEvent) {
	m.handlers.Store(pid, ch)
}

func (m *callbackMux) unregister(pid types.PID) {
	m.handlers.Delete(pid)
}

func (m *callbackMux) send(pid types.PID, ev StreamEvent) {
	ch, ok := m.handlers.Load(pid)
	if ok {
		select {
		case ch <- ev:
		default:
		}
	}
}

// KernelCallbacks interface implementation for the server's callbackMux.

func (m *callbackMux) OnSpawn(pid types.PID, intent, provider, model, procUUID string) {
	pp := ProgressPayload{Event: "spawn", PID: pid, Intent: intent, Provider: provider, Model: model, UUID: procUUID}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnStep(pid types.PID, step int, total int) {
	pp := ProgressPayload{Event: "step", PID: pid, Step: step, Total: total}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64) {
	pp := ProgressPayload{Event: "step_complete", PID: pid, Step: step, Action: action, Summary: summary, HasError: hasError, DurationMs: durationMs}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnComplete(pid types.PID, result string, exit kernel.ExitStatus) {
	// Completion is handled by the spawn handler goroutine reading from proc.Done.
}

func (m *callbackMux) OnError(pid types.PID, err error) {
	pp := ProgressPayload{Event: "error", PID: pid, ErrorMessage: err.Error()}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnAskUser(pid types.PID, requestID string, questions []byte) ([]byte, error) {
	if pid == 0 {
		return nil, fmt.Errorf("ask_user: cannot send to PID 0 (no process context)")
	}

	// Create answer channel before sending event
	answerCh := make(chan []byte, 1)
	m.pendingAsks.Store(requestID, answerCh)
	defer m.pendingAsks.Delete(requestID)

	// Send ask_user event to CLI via the spawn stream.
	// Use blocking send for ask_user events (critical, must not be dropped).
	askEv := AskUserEvent{PID: pid, RequestID: requestID, Questions: questions}
	payload, _ := json.Marshal(askEv)
	ev := StreamEvent{Type: StreamAskUser, Payload: payload}

	ch, ok := m.handlers.Load(pid)
	if !ok {
		return nil, fmt.Errorf("ask_user: no handler registered for PID %d", pid)
	}
	select {
	case ch <- ev:
	case <-m.done:
		return nil, fmt.Errorf("ask_user: server shutting down")
	}

	// Block waiting for answer (with timeout and shutdown awareness)
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()

	select {
	case answer := <-answerCh:
		return answer, nil
	case <-timer.C:
		return nil, fmt.Errorf("ask_user timeout: no answer received within 5 minutes")
	case <-m.done:
		return nil, fmt.Errorf("ask_user: server shutting down while waiting for answer")
	}
}

// deliverAnswer routes an answer to the pending ask_user request.
func (m *callbackMux) deliverAnswer(requestID string, answers []byte) error {
	ch, ok := m.pendingAsks.Load(requestID)
	if !ok {
		return fmt.Errorf("no pending ask for request_id %q", requestID)
	}
	select {
	case ch <- answers:
		return nil
	default:
		return fmt.Errorf("answer channel full for request_id %q", requestID)
	}
}

func (m *callbackMux) OnStemDiff(pid types.PID, matches []kernel.StemMatchResult, selected []string, fromMemory bool) {
	wireMatches := make([]StemMatchWire, len(matches))
	for i, mr := range matches {
		wireMatches[i] = StemMatchWire{Name: mr.Name, Score: mr.Score}
	}
	pp := ProgressPayload{Event: "stem_diff", PID: pid, StemMatches: wireMatches, StemSelected: selected, FromMemory: fromMemory}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

// Compile-time check that callbackMux implements KernelCallbacks.
var _ kernel.KernelCallbacks = (*callbackMux)(nil)
