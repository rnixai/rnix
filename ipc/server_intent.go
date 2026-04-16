package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"

	"github.com/rnixai/rnix/internal/types"
)

// --- Intent Handlers ---

func (s *Server) handleApplyIntent(conn net.Conn, rawPayload json.RawMessage, connScanner *bufio.Scanner) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req ApplyIntentRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	intentID, treeJSON, err := s.intentMgr.ApplyIntent(context.Background(), req.Intent, req.Model, req.Provider, req.AutoStart)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "decompose_failed", Message: err.Error()}})
		return
	}

	var writeMu sync.Mutex
	syncWriteEvent := func(ev StreamEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		writeStreamEvent(conn, ev)
	}

	writeResponse(conn, Response{OK: true, Payload: marshalJSON(ApplyIntentResponse{IntentID: intentID, Tree: nil})})

	syncWriteEvent(StreamEvent{Type: StreamIntentDecomposed, Payload: treeJSON})

	if !req.AutoStart {
		syncWriteEvent(StreamEvent{Type: StreamIntentConfirmReq, Payload: marshalJSON(IntentNodeEventPayload{NodeID: intentID})})

		if !connScanner.Scan() {
			return
		}
		var confirmReq Request
		if err := json.Unmarshal(connScanner.Bytes(), &confirmReq); err != nil {
			return
		}
		var confirm IntentConfirmRequest
		if err := json.Unmarshal(confirmReq.Payload, &confirm); err != nil {
			return
		}
		if !confirm.Confirm {
			syncWriteEvent(StreamEvent{Type: StreamIntentComplete, Payload: marshalJSON(IntentNodeEventPayload{Error: "user cancelled"})})
			return
		}
		if err := s.intentMgr.ConfirmIntent(intentID); err != nil {
			syncWriteEvent(StreamEvent{Type: StreamError, Payload: marshalJSON(IntentNodeEventPayload{Error: err.Error()})})
			return
		}
	}

	execErr := s.intentMgr.ExecuteIntent(context.Background(), intentID,
		func(nodeID string, pid uint64) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeStart, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, PID: pid})})
		},
		func(nodeID, result string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeDone, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, Result: result})})
		},
		func(nodeID, errMsg string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeFailed, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, Error: errMsg})})
		},
		func(completed, total int) {
			syncWriteEvent(StreamEvent{Type: StreamIntentProgress, Payload: marshalJSON(IntentNodeEventPayload{Completed: completed, Total: total})})
		},
		func(nodeID string, attempt, maxRetries int) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeRetry, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, RetryAttempt: attempt, MaxRetries: maxRetries})})
		},
		func(nodeID string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeTimeout, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID})})
		},
		func(nodeID, driftType, message string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentDriftDetected, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, DriftType: driftType, Error: message})})
		},
		func(nodeID string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentDriftResolved, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID})})
		},
	)

	if execErr != nil {
		syncWriteEvent(StreamEvent{Type: StreamIntentComplete, Payload: marshalJSON(IntentNodeEventPayload{Error: execErr.Error()})})
	} else {
		syncWriteEvent(StreamEvent{Type: StreamIntentComplete, Payload: marshalJSON(IntentNodeEventPayload{})})
	}
}

func (s *Server) handleIntentStatus(conn net.Conn, rawPayload json.RawMessage) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req IntentStatusRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	var data []byte
	var err error
	if req.IntentID != "" {
		data, err = s.intentMgr.IntentStatus(req.IntentID)
	} else {
		data, err = s.intentMgr.ListActiveIntents()
	}
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true, Payload: data})
}

func (s *Server) handleIntentConfirm(conn net.Conn, rawPayload json.RawMessage) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req IntentConfirmRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	if err := s.intentMgr.ConfirmIntent(req.IntentID); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "confirm_failed", Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true})
}

func (s *Server) handleApplyIncrementalIntent(conn net.Conn, rawPayload json.RawMessage) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req ApplyIncrementalIntentRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	intentID, resultJSON, err := s.intentMgr.ApplyIncrementalIntent(context.Background(), req.IntentID, req.Intent, req.Model, req.Provider)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "incremental_failed", Message: err.Error()}})
		return
	}

	// Parse the result to build the response
	var mergeResp ApplyIncrementalIntentResponse
	if err := json.Unmarshal(resultJSON, &mergeResp); err != nil {
		// fallback: send raw
		mergeResp = ApplyIncrementalIntentResponse{IntentID: intentID}
	}

	writeResponse(conn, Response{OK: true, Payload: marshalJSON(mergeResp)})
}

func (s *Server) handleIntentList(conn net.Conn) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	data, err := s.intentMgr.ListAllIntents()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "list_failed", Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true, Payload: data})
}

func (s *Server) handleAnswerUser(conn net.Conn, rawPayload json.RawMessage) {
	var req AnswerUserRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid answer_user request"}})
		return
	}
	if req.RequestID == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "request_id is required"}})
		return
	}

	if err := s.callbackMux.deliverAnswer(req.RequestID, req.Answers); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrNotFound), Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true})
}
