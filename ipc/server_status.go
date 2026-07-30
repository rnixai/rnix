package ipc

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

func (s *Server) handleProviderStatus(conn net.Conn) {
	var wires []ProviderStatusWire
	if s.providerStatuses != nil {
		wires = s.providerStatuses()
		// Tag global providers
		for i := range wires {
			wires[i].Source = "global"
		}
	}
	if wires == nil {
		wires = []ProviderStatusWire{}
	}

	// Append project-level providers
	seen := make(map[string]bool, len(wires))
	for _, w := range wires {
		seen[w.Name] = true
	}
	s.projectProviders.Range(func(_ string, providers []ProviderStatusWire) bool {
		for _, p := range providers {
			if !seen[p.Name] {
				wires = append(wires, p)
				seen[p.Name] = true
			}
		}
		return true
	})

	payload, _ := json.Marshal(ProviderStatusResponse{Providers: wires})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleReputationStatus(conn net.Conn, rawPayload json.RawMessage) {
	var req ReputationStatusRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid reputation_status request"}})
		return
	}

	if s.reputationStore == nil {
		resp := ReputationStatusResponse{Summaries: []kernel.ReputationSummary{}}
		respPayload, _ := json.Marshal(resp)
		writeResponse(conn, Response{OK: true, Payload: respPayload})
		return
	}

	var summaries []kernel.ReputationSummary

	if req.AgentName != "" {
		summary, err := s.reputationStore.GetSummary(req.AgentName)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
			return
		}
		summaries = []kernel.ReputationSummary{*summary}
	} else {
		all, err := s.reputationStore.GetAllSummaries()
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
			return
		}
		summaries = all
	}

	resp := ReputationStatusResponse{Summaries: summaries}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleSynergyList(conn net.Conn) {
	if s.synergyMatrix == nil {
		resp := SynergyListResponse{Combos: []kernel.ComboSummary{}}
		respPayload, _ := json.Marshal(resp)
		writeResponse(conn, Response{OK: true, Payload: respPayload})
		return
	}

	summaries, err := s.synergyMatrix.GetComboSummaries()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	if summaries == nil {
		summaries = []kernel.ComboSummary{}
	}
	resp := SynergyListResponse{Combos: summaries}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleImmuneStatus(conn net.Conn) {
	if s.immuneDaemon == nil {
		resp := ImmuneStatusResponse{
			Running:        false,
			Profiles:       map[string]*kernel.NormalProfile{},
			ActivePIDs:     []uint64{},
			SuspendedPIDs:  []uint64{},
			Alerts:         []AlertWire{},
			SecurityStatus: "ok",
		}
		respPayload, _ := json.Marshal(resp)
		writeResponse(conn, Response{OK: true, Payload: respPayload})
		return
	}

	profiles := s.immuneDaemon.GetAllProfiles()
	if profiles == nil {
		profiles = map[string]*kernel.NormalProfile{}
	}
	rawPIDs := s.immuneDaemon.ActivePIDs()
	activePIDs := make([]uint64, len(rawPIDs))
	for i, p := range rawPIDs {
		activePIDs[i] = uint64(p)
	}

	// Story 22.2: include alerts and threat count
	rawAlerts := s.immuneDaemon.GetAlerts()
	alertWires := make([]AlertWire, 0, len(rawAlerts))
	for _, a := range rawAlerts {
		alertWires = append(alertWires, AlertWire{
			PID:           uint64(a.PID),
			AgentTemplate: a.AgentTemplate,
			Type:          string(a.Type),
			Detail:        a.Detail,
			Deviation:     a.Deviation,
			TimestampMs:   a.Timestamp.UnixMilli(),
		})
	}
	threats := s.immuneDaemon.GetThreats()

	// Story 22.3: uptime, suspended PIDs, security status
	uptimeMs := s.immuneDaemon.Uptime().Milliseconds()
	rawSuspended := s.immuneDaemon.SuspendedPIDs()
	suspendedPIDs := make([]uint64, len(rawSuspended))
	for i, p := range rawSuspended {
		suspendedPIDs[i] = uint64(p)
	}

	securityStatus := "ok"
	if len(alertWires) > 0 || len(suspendedPIDs) > 0 {
		securityStatus = "warning"
	}

	resp := ImmuneStatusResponse{
		Running:        s.immuneDaemon.IsRunning(),
		UptimeMs:       uptimeMs,
		ProfileCount:   len(profiles),
		Profiles:       profiles,
		ActivePIDs:     activePIDs,
		SuspendedPIDs:  suspendedPIDs,
		Alerts:         alertWires,
		ThreatCount:    len(threats),
		SecurityStatus: securityStatus,
		WarnOnly:       s.immuneDaemon.IsWarnOnly(),
	}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleImmuneResume(conn net.Conn, rawPayload json.RawMessage) {
	var req ImmuneResumeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid immune_resume request"}})
		return
	}

	pid, pidOK := s.resolvePID(types.PID(req.PID), req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Send SIGRESUME to the process. Story 66.3 review (decision A): route
	// through Signal(), NOT KillWithOrigin. The Kill path only acts on
	// IsTermination signals for a Suspended target — SIGRESUME there falls to
	// noop_suspended and never resumes. Signal()'s Suspended branch delegates to
	// ResumeSubtree and truly resumes. Tradeoff: Signal events are origin-free
	// (Kill events carry the audit trail), so "who un-paused this" is not
	// attributed here — correctness over the audit stamp.
	if s.kern != nil {
		if err := s.kern.Signal(pid, types.SIGRESUME); err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("failed to resume PID %d: %v", pid, err)}})
			return
		}
	}

	// Clear the alert in the immune daemon
	if s.immuneDaemon != nil {
		s.immuneDaemon.ClearAlert(pid)
	}

	result := ImmuneResumeResponse{
		OK:      true,
		Message: fmt.Sprintf("Process %d resumed successfully.", pid),
	}
	respPayload, _ := json.Marshal(result)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// handleImmuneForget removes threat signatures from the immune threat memory
// (IN-3 F3 correction port).
func (s *Server) handleImmuneForget(conn net.Conn, rawPayload json.RawMessage) {
	var req ImmuneForgetRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid immune_forget request"}})
		return
	}

	if !req.All && req.Template == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "immune_forget requires a template or all=true"}})
		return
	}

	removed := 0
	if s.immuneDaemon != nil {
		removed = s.immuneDaemon.ForgetThreats(req.Template, req.Metric, req.All)
	}

	respPayload, _ := json.Marshal(ImmuneForgetResponse{Removed: removed})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// handleSimilarityQuery returns similar agents for the given agent (Story 22.4).
func (s *Server) handleSimilarityQuery(conn net.Conn, rawPayload json.RawMessage) {
	var req SimilarityQueryRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid similarity_query request"}})
		return
	}

	var similarities []kernel.CapabilitySimilarity
	if s.immuneDaemon != nil {
		similarities = s.immuneDaemon.GetSimilarAgents(req.AgentName, req.MinScore)
	}
	if similarities == nil {
		similarities = []kernel.CapabilitySimilarity{}
	}

	resp := SimilarityQueryResponse{
		Agent:        req.AgentName,
		Similarities: similarities,
	}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// handleTopologyQuery returns the collaboration topology (Story 22.5).
func (s *Server) handleTopologyQuery(conn net.Conn) {
	var topo *kernel.CollaborationTopology
	if s.immuneDaemon != nil {
		topo = s.immuneDaemon.GetTopology()
	}

	resp := TopologyQueryResponse{
		Nodes:           []kernel.TopologyNode{},
		Edges:           []kernel.CooperationEdge{},
		ReinforcedPaths: []kernel.CooperationEdge{},
	}
	if topo != nil {
		if topo.Nodes != nil {
			resp.Nodes = topo.Nodes
		}
		if topo.Edges != nil {
			resp.Edges = topo.Edges
		}
		if topo.ReinforcedPaths != nil {
			resp.ReinforcedPaths = topo.ReinforcedPaths
		}
	}

	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleHeartbeatStatus(conn net.Conn) {
	hm := s.kern.GetHeartbeatMonitor()
	if hm == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "heartbeat monitor not initialized"}})
		return
	}

	status := hm.Status()
	stalled := make([]StalledProcWire, len(status.CurrentStalled))
	for i, sp := range status.CurrentStalled {
		stalled[i] = StalledProcWire{
			PID:               sp.PID,
			UUID:              sp.UUID,
			ConsecutiveStalls: sp.ConsecutiveStalls,
			StalledDurationMs: sp.StalledDuration.Milliseconds(),
			HeartbeatGapMs:    sp.HeartbeatGap.Milliseconds(),
			LastAction:        sp.LastAction,
		}
	}

	resp := HeartbeatStatusResponse{
		Running:              status.Running,
		CheckIntervalMs:      status.CheckInterval.Milliseconds(),
		TotalStalledDetected: status.TotalStalledDetected,
		CurrentStalled:       stalled,
	}

	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}
