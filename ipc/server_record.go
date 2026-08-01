package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// handleRecordStart starts execution recording for a process.
func (s *Server) handleRecordStart(conn net.Conn, rawPayload json.RawMessage) {
	var req RecordStartRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid record_start request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	recordID, err := s.kern.StartRecording(pid)
	if err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	payload, _ := json.Marshal(RecordStartResponse{RecordID: recordID})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleRecordStop stops execution recording for a process.
func (s *Server) handleRecordStop(conn net.Conn, rawPayload json.RawMessage) {
	var req RecordStopRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid record_stop request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Get event count before stopping
	var eventCount uint64
	if mgr := s.kern.GetRecordManager(); mgr != nil {
		eventCount = mgr.GetEventCount(pid)
	}

	if err := s.kern.StopRecording(pid); err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	payload, _ := json.Marshal(RecordStopResponse{EventCount: eventCount})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleRecordList lists all recorded sessions.
func (s *Server) handleRecordList(conn net.Conn) {
	mgr := s.kern.GetRecordManager()
	if mgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "record manager not initialized"}})
		return
	}

	records, err := mgr.ListRecords()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	wireRecords := make([]RecordMetadataWire, len(records))
	for i, r := range records {
		wireRecords[i] = RecordMetadataWire{
			RecordID:   r.RecordID,
			PID:        r.PID,
			Intent:     r.Intent,
			StartTime:  r.StartTime.UnixMilli(),
			EventCount: r.EventCount,
			Status:     string(r.Status),
		}
		if !r.EndTime.IsZero() {
			wireRecords[i].EndTime = r.EndTime.UnixMilli()
		}
	}

	payload, _ := json.Marshal(RecordListResponse{Records: wireRecords})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleReplayLoad loads a recording and returns its metadata for replay.
func (s *Server) handleReplayLoad(conn net.Conn, rawPayload json.RawMessage) {
	var req ReplayLoadRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid replay_load request"}})
		return
	}

	mgr := s.kern.GetRecordManager()
	if mgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "record manager not initialized"}})
		return
	}

	reader, err := mgr.LoadRecord(req.RecordID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: err.Error()}})
		return
	}

	meta := reader.Metadata()
	resp := ReplayLoadResponse{
		RecordID:    meta.RecordID,
		PID:         meta.PID,
		Intent:      meta.Intent,
		EventCount:  reader.EventCount(),
		StartTimeMs: meta.StartTime.UnixMilli(),
		Status:      string(meta.Status),
	}
	if !meta.EndTime.IsZero() {
		resp.EndTimeMs = meta.EndTime.UnixMilli()
	}

	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleForkContinue creates a new process from a fork context (fork-continue).
// Context is pre-allocated and populated with replayed messages, then passed to
// kernel.Spawn via PreallocatedCtxID + SkipReasonLoop to use the standard path.
func (s *Server) handleForkContinue(conn net.Conn, rawPayload json.RawMessage) {
	var req ForkContinueRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid fork_continue request"}})
		return
	}

	if req.Intent == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "intent is required"}})
		return
	}

	// Determine parent PID and validity
	parentPID := types.PID(req.OriginalPID)
	ppidValid := false
	if parentPID > 0 {
		if _, ok := s.kern.GetProcess(parentPID); ok {
			ppidValid = true
		} else {
			parentPID = 0
		}
	}

	// Allocate context and replay messages BEFORE creating the process,
	// so failures here don't leave orphaned process resources.
	var cid types.CtxID
	if s.ctxMgr != nil {
		var err error
		// 0 = no slot ceiling (Story 71.1). A replayed recording gets the same
		// unlimited context a fresh spawn does.
		cid, err = s.ctxMgr.CtxAlloc(0)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: fmt.Sprintf("CtxAlloc failed: %v", err)}})
			return
		}

		if req.SystemPrompt != "" {
			if err := s.ctxMgr.SetSystemPrompt(cid, req.SystemPrompt); err != nil {
				_ = s.ctxMgr.CtxFree(cid)
				writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: fmt.Sprintf("SetSystemPrompt failed: %v", err)}})
				return
			}
		}

		for i, msg := range req.Messages {
			if msg.Role == "system" {
				continue
			}
			if msg.ToolCallID != "" {
				if err := s.ctxMgr.AppendToolResult(cid, msg.ToolCallID, msg.Content); err != nil {
					log.Printf("[fork_continue] warning: AppendToolResult failed at message %d: %v", i, err)
				}
			} else {
				if err := s.ctxMgr.AppendMessage(cid, rnixctx.Role(msg.Role), msg.Content); err != nil {
					log.Printf("[fork_continue] warning: AppendMessage failed at message %d: %v", i, err)
				}
			}
		}
	}

	// Use standard Spawn path with pre-allocated context and no reason loop
	pid, err := s.kern.Spawn(req.Intent, nil, kernel.SpawnOpts{
		ParentPID:         parentPID,
		PreallocatedCtxID: cid,
		SkipReasonLoop:    true,
	})
	if err != nil {
		if cid != 0 {
			_ = s.ctxMgr.CtxFree(cid)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	actualPPID := parentPID
	if !ppidValid {
		actualPPID = 0
	}

	respPayload, _ := json.Marshal(ForkContinueResponse{
		PID:       pid,
		PPID:      actualPPID,
		PPIDValid: ppidValid,
	})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}
