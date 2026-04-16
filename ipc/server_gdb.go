package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

func (s *Server) handleAttachDebug(conn net.Conn, rawPayload json.RawMessage) {
	var req AttachDebugRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid attach_debug request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	debugCh, ok := s.kern.GetDebugChan(pid)
	if !ok || debugCh == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found or no debug channel"}})
		return
	}

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)
	for event := range debugCh {
		sew := SyscallEventToWire(event)
		payload, _ := json.Marshal(sew)
		se := StreamEvent{Type: StreamSyscallEvent, Payload: payload}
		if err := enc.Encode(se); err != nil {
			return
		}
	}
	_ = enc.Encode(StreamEvent{Type: StreamEOF})
}

func (s *Server) handleAttachLog(conn net.Conn, rawPayload json.RawMessage) {
	var req AttachLogRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid attach_log request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Check process existence via both history and live channel
	history, histOK := s.kern.GetLogHistory(pid)
	logCh, logOK := s.kern.GetLogChan(pid)

	if !histOK && (!logOK || logCh == nil) {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	writeResponse(conn, Response{OK: true})
	enc := json.NewEncoder(conn)

	// Replay history logs with timestamp tracking for dedup
	var lastReplayedTs int64
	if histOK && len(history) > 0 {
		for _, entry := range history {
			lew := LogEntryToWire(entry)
			payload, _ := json.Marshal(lew)
			se := StreamEvent{Type: StreamLogEntry, Payload: payload}
			if err := enc.Encode(se); err != nil {
				return
			}
			lastReplayedTs = lew.TimestampMs
		}
	}

	// Stream live logs (if process is still alive)
	if logOK && logCh != nil {
		for entry := range logCh {
			lew := LogEntryToWire(entry)
			if lew.TimestampMs <= lastReplayedTs {
				continue // skip entries already replayed from history
			}
			payload, _ := json.Marshal(lew)
			se := StreamEvent{Type: StreamLogEntry, Payload: payload}
			if err := enc.Encode(se); err != nil {
				return
			}
		}
	}

	_ = enc.Encode(StreamEvent{Type: StreamEOF})
}

func (s *Server) handleAttachGdb(conn net.Conn, rawPayload json.RawMessage) {
	var req AttachGdbRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid attach_gdb request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Validate process exists and is Running
	info, infoErr := s.kern.GetProcInfo(pid)
	if infoErr != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}
	if info.State != types.StateRunning {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID_STATE", Message: fmt.Sprintf("process %d is %s, not running", pid, info.State)}})
		return
	}

	debugCh, debugOK := s.kern.GetDebugChan(pid)
	logCh, logOK := s.kern.GetLogChan(pid)

	if (!debugOK || debugCh == nil) && (!logOK || logCh == nil) {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found or no channels"}})
		return
	}

	// Get process for Done channel monitoring
	proc, procOK := s.kern.GetProcess(pid)
	if !procOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Check if process was cancelled (Kill called but state not yet transitioned)
	if proc.IsCancelled() {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID_STATE", Message: fmt.Sprintf("process %d has been terminated", pid)}})
		return
	}

	// Register per-PID detach channel (single-attach enforcement)
	// Must happen BEFORE sending OK response to prevent race conditions
	detachCh := make(chan struct{})
	s.gdbMu.Lock()
	if s.gdbDetachCh == nil {
		s.gdbDetachCh = make(map[types.PID]chan struct{})
	}
	if _, exists := s.gdbDetachCh[pid]; exists {
		s.gdbMu.Unlock()
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "ALREADY_ATTACHED", Message: fmt.Sprintf("process %d already has an active gdb session", pid)}})
		return
	}
	s.gdbDetachCh[pid] = detachCh
	s.gdbMu.Unlock()
	defer func() {
		s.gdbMu.Lock()
		delete(s.gdbDetachCh, pid)
		s.gdbMu.Unlock()
	}()

	// Build initial response with process metadata
	skills := info.Skills
	if skills == nil {
		skills = []string{}
	}
	gdbResp := AttachGdbResponse{
		PID:        info.PID,
		State:      info.State,
		Intent:     info.Intent,
		Skills:     skills,
		TokensUsed: info.TokensUsed,
	}
	payload, _ := json.Marshal(gdbResp)
	writeResponse(conn, Response{OK: true, Payload: payload})

	enc := json.NewEncoder(conn)

	// Get process cancellation channel for Kill detection
	cancelledCh := proc.CancelledCh()

	// Forward events from both channels using select
	for {
		select {
		case event, ok := <-debugCh:
			if !ok {
				// Debug channel closed -- send state change and EOF
				statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "exited"})
				_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
				_ = enc.Encode(GdbEvent{Type: StreamEOF})
				return
			}
			sew := SyscallEventToWire(event)
			evPayload, _ := json.Marshal(sew)
			// Route GdbPause events as gdb_prompt for breakpoint notifications
			evType := StreamGdbSyscall
			if event.Syscall == "GdbPause" {
				promptPayload, _ := json.Marshal(event.Args)
				evType = StreamGdbPrompt
				evPayload = promptPayload
			}
			if err := enc.Encode(GdbEvent{Type: evType, Payload: evPayload}); err != nil {
				return
			}
		case entry, ok := <-logCh:
			if !ok {
				// Log channel closed -- send state change and EOF
				statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "exited"})
				_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
				_ = enc.Encode(GdbEvent{Type: StreamEOF})
				return
			}
			lew := LogEntryToWire(entry)
			evPayload, _ := json.Marshal(lew)
			if err := enc.Encode(GdbEvent{Type: StreamGdbLog, Payload: evPayload}); err != nil {
				return
			}
		case <-detachCh:
			// Client sent detach via separate connection -- send EOF and return
			_ = enc.Encode(GdbEvent{Type: StreamEOF})
			return
		case <-proc.Done:
			// Process exited -- send state change and EOF
			statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "exited"})
			_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
			_ = enc.Encode(GdbEvent{Type: StreamEOF})
			return
		case <-cancelledCh:
			// Process was killed (context cancelled) -- send state change and EOF
			statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "killed"})
			_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
			_ = enc.Encode(GdbEvent{Type: StreamEOF})
			return
		case <-s.done:
			return
		}
	}
}

// handleDetachGdb handles detach requests sent on a separate connection.
func (s *Server) handleDetachGdb(conn net.Conn, rawPayload json.RawMessage) {
	var req DetachGdbRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid detach_gdb request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	s.gdbMu.Lock()
	ch, ok := s.gdbDetachCh[pid]
	if ok {
		close(ch)
		delete(s.gdbDetachCh, pid)
	}
	s.gdbMu.Unlock()

	writeResponse(conn, Response{OK: true})
}

// handleGdbCommand dispatches gdb commands (break, delete, continue, info) to the kernel.
func (s *Server) handleGdbCommand(conn net.Conn, rawPayload json.RawMessage) {
	var req GdbCommandRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid gdb_command request"}})
		return
	}

	proc, ok := s.resolveProcess(req.PID, req.UUID)
	if !ok {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	var gcr GdbCommandResponse
	switch req.Command {
	case "break":
		gcr = s.handleGdbBreak(proc, req.Args)
	case "delete":
		gcr = s.handleGdbDelete(proc, req.Args)
	case "continue":
		proc.GdbResume()
		gcr = GdbCommandResponse{OK: true, Message: "resumed"}
	case "info":
		gcr = s.handleGdbInfo(proc, req.Args)
	case "step":
		gcr = s.handleGdbStep(proc, req.Args)
	case "inspect":
		gcr = s.handleGdbInspect(proc, req.Args)
	case "set":
		gcr = s.handleGdbSet(proc, req.Args)
	default:
		gcr = GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown gdb command: %s", req.Command)}
	}

	payload, _ := json.Marshal(gcr)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleGdbBreak creates a breakpoint from parsed args.
func (s *Server) handleGdbBreak(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: break <type> [args...]"}
	}

	bp, err := parseBreakpointArgs(args)
	if err != nil {
		return GdbCommandResponse{OK: false, Message: err.Error()}
	}

	id := proc.AddBreakpoint(bp)
	return GdbCommandResponse{OK: true, Message: fmt.Sprintf("breakpoint %d set", id), Data: map[string]any{"bp_id": id}}
}

// handleGdbDelete removes a breakpoint by ID.
func (s *Server) handleGdbDelete(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: delete <bp_id>"}
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("invalid breakpoint ID: %s", args[0])}
	}
	if proc.RemoveBreakpoint(id) {
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("breakpoint %d deleted", id)}
	}
	return GdbCommandResponse{OK: false, Message: fmt.Sprintf("breakpoint %d not found", id)}
}

// handleGdbInfo returns breakpoint information.
func (s *Server) handleGdbInfo(proc *kernel.Process, args []string) GdbCommandResponse {
	// Default to "breakpoints" if no args or explicit "breakpoints"/"bp"
	if len(args) > 0 && args[0] != "breakpoints" && args[0] != "bp" {
		return GdbCommandResponse{OK: false, Message: "usage: info breakpoints|bp"}
	}
	bps := proc.ListBreakpoints()
	bpList := make([]map[string]any, len(bps))
	for i, bp := range bps {
		bpList[i] = map[string]any{
			"id":        bp.ID,
			"type":      bpTypeString(bp.Type),
			"enabled":   bp.Enabled,
			"hit_count": bp.HitCount,
			"condition": bpConditionString(bp),
		}
	}
	return GdbCommandResponse{OK: true, Data: bpList}
}

// handleGdbStep sets step mode and resumes the process.
func (s *Server) handleGdbStep(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: step <syscall|reasoning>"}
	}
	mode := args[0]

	switch mode {
	case "syscall":
		proc.SetStepMode(kernel.StepSyscall)
	case "reasoning":
		proc.SetStepMode(kernel.StepReasoning)
	default:
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown step mode: %s (valid: syscall, reasoning)", mode)}
	}

	proc.GdbResume()
	return GdbCommandResponse{OK: true, Message: fmt.Sprintf("stepping %s", mode)}
}

// handleGdbInspect handles the inspect command to examine process state.
func (s *Server) handleGdbInspect(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: inspect <context|ctx>"}
	}

	subCmd := args[0]
	switch subCmd {
	case "context", "ctx":
		if s.ctxMgr == nil {
			return GdbCommandResponse{OK: false, Message: "context manager not available"}
		}
		info, err := s.ctxMgr.GetContextInfo(proc.CtxID)
		if err != nil {
			return GdbCommandResponse{OK: false, Message: fmt.Sprintf("inspect context failed: %v", err)}
		}
		info["pid"] = proc.PID
		info["ctx_id"] = proc.CtxID
		totalMsgs, _ := info["total_messages"].(int)
		totalTokens, _ := info["total_tokens"].(int)
		return GdbCommandResponse{
			OK:      true,
			Message: fmt.Sprintf("context: %d messages, ~%d tokens", totalMsgs, totalTokens),
			Data:    info,
		}
	default:
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown inspect target: %s (valid: context, ctx)", subCmd)}
	}
}

// handleGdbSet handles the set command for runtime parameter hot modification.
func (s *Server) handleGdbSet(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) < 2 {
		return GdbCommandResponse{OK: false, Message: "usage: set <model|context|skills|env> <args...>"}
	}
	subCmd := args[0]
	switch subCmd {
	case "model":
		proc.SetGdbModelOverride(args[1])
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("model set to %s", args[1])}
	case "context":
		if len(args) < 3 {
			return GdbCommandResponse{OK: false, Message: "usage: set context append <text>"}
		}
		if args[1] != "append" {
			return GdbCommandResponse{OK: false, Message: "usage: set context append <text>"}
		}
		content := strings.Join(args[2:], " ")
		if s.ctxMgr == nil {
			return GdbCommandResponse{OK: false, Message: "context manager not available"}
		}
		if err := s.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, content); err != nil {
			return GdbCommandResponse{OK: false, Message: fmt.Sprintf("context append failed: %v", err)}
		}
		return GdbCommandResponse{OK: true, Message: "context updated"}
	case "skills":
		if len(args) < 3 || args[1] != "add" {
			return GdbCommandResponse{OK: false, Message: "usage: set skills add <name>"}
		}
		skillName := args[2]
		proc.AddGdbSkill(skillName)
		// Hot-load skill body into context if skill loader and context manager are available
		if s.skillLoader != nil && s.ctxMgr != nil {
			info, err := s.skillLoader.LoadFull(skillName)
			if err != nil {
				return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s recorded (load failed: %v)", skillName, err)}
			}
			if info.Body != "" {
				if err := s.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, fmt.Sprintf("[Skill: %s]\n%s", skillName, info.Body)); err != nil {
					return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s recorded (context append failed: %v)", skillName, err)}
				}
				return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s loaded and injected into context", skillName)}
			}
		}
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s added", skillName)}
	case "env":
		kv := args[1]
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			return GdbCommandResponse{OK: false, Message: "usage: set env KEY=VALUE"}
		}
		proc.SetGdbEnv(kv[:idx], kv[idx+1:])
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("env %s set", kv[:idx])}
	default:
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown set target: %s", subCmd)}
	}
}

// parseBreakpointArgs parses "break" command arguments into a Breakpoint.
func parseBreakpointArgs(args []string) (*kernel.Breakpoint, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing breakpoint type")
	}
	bpType := args[0]
	switch bpType {
	case "syscall":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: break syscall <name>")
		}
		return &kernel.Breakpoint{
			Type:      kernel.BPSyscall,
			Enabled:   true,
			Condition: &kernel.SyscallCondition{Name: args[1]},
		}, nil
	case "reasoning":
		return &kernel.Breakpoint{
			Type:      kernel.BPReasoning,
			Enabled:   true,
			Condition: &kernel.ReasoningCondition{},
		}, nil
	case "quality":
		return parseQualityBreakpoint(args[1:])
	case "budget":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: break budget <tokens>")
		}
		threshold, err := strconv.Atoi(args[1])
		if err != nil {
			return nil, fmt.Errorf("invalid budget threshold: %s", args[1])
		}
		return &kernel.Breakpoint{
			Type:      kernel.BPBudget,
			Enabled:   true,
			Condition: &kernel.BudgetCondition{Threshold: threshold},
		}, nil
	default:
		return nil, fmt.Errorf("unknown breakpoint type: %s (valid: syscall, reasoning, quality, budget)", bpType)
	}
}

// parseQualityBreakpoint parses quality breakpoint flags.
func parseQualityBreakpoint(args []string) (*kernel.Breakpoint, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: break quality --pattern <pattern> | --eval <criteria>")
	}
	switch args[0] {
	case "--pattern":
		re, err := regexp.Compile(args[1])
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %v", err)
		}
		return &kernel.Breakpoint{
			Type:    kernel.BPQuality,
			Enabled: true,
			Condition: &kernel.QualityCondition{
				Mode:    kernel.QualityModePattern,
				Pattern: re,
			},
		}, nil
	case "--eval":
		return &kernel.Breakpoint{
			Type:    kernel.BPQuality,
			Enabled: true,
			Condition: &kernel.QualityCondition{
				Mode:     kernel.QualityModeEval,
				EvalExpr: args[1],
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown quality flag: %s (valid: --pattern, --eval)", args[0])
	}
}

// bpTypeString returns a human-readable name for a breakpoint type.
func bpTypeString(t kernel.BreakpointType) string {
	switch t {
	case kernel.BPSyscall:
		return "syscall"
	case kernel.BPReasoning:
		return "reasoning"
	case kernel.BPQuality:
		return "quality"
	case kernel.BPBudget:
		return "budget"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// bpConditionString returns a human-readable description of a breakpoint's condition.
func bpConditionString(bp *kernel.Breakpoint) string {
	if bp.Condition == nil {
		return ""
	}
	switch c := bp.Condition.(type) {
	case *kernel.SyscallCondition:
		return c.Name
	case *kernel.ReasoningCondition:
		return "every step"
	case *kernel.QualityCondition:
		if c.Mode == kernel.QualityModePattern && c.Pattern != nil {
			return fmt.Sprintf("pattern: %s", c.Pattern.String())
		}
		return fmt.Sprintf("eval: %s", c.EvalExpr)
	case *kernel.BudgetCondition:
		return fmt.Sprintf(">= %d tokens", c.Threshold)
	default:
		return "custom"
	}
}
