package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// handleMCPList implements MethodMCPList (Story 48.3 §AC1 / §AC5). Projects
// MountManager.ListMounts via the kernel surface into MCPMountWire entries.
// Server names are recovered from the mount path's "/mnt/mcp/<pid>-<name>"
// suffix; mounts whose path doesn't match are still emitted (Name=path) so
// observability doesn't drop legitimate state due to a parse miss.
//
// Tools / Resources / LastCheckMs are placeholders pre-48.5 — the IPC
// contract pins their position so 48.5's registry cache can fill them
// without bumping the wire.
func (s *Server) handleMCPList(conn net.Conn) {
	wires := make([]MCPMountWire, 0)

	if s.kern != nil {
		for _, m := range s.kern.ListMounts() {
			w := MCPMountWire{
				Name:      mcpMountNameFromPath(m.Path),
				Path:      m.Path,
				Transport: mcpTransportLabel(m.Config),
				Status:    vfs.MCPStatusString(m.Status),
			}
			// Story 48.5 AC5 — backfill from the live transport: Status becomes
			// the authoritative transport state (MCPMount.Status is a stale
			// Mount-time projection), and Tools/Resources/LastCheckMs are filled
			// from the cached counters. LastCheckMs is the age of the last L2
			// ping in ms (0 = never checked → cmd renders the placeholder).
			if tr := m.Transport(); tr != nil {
				w.Status = vfs.MCPStatusString(tr.Status())
				w.Tools = tr.ToolCount()
				w.Resources = tr.ResourceCount()
				if lc := tr.LastCheck(); !lc.IsZero() {
					w.LastCheckMs = time.Since(lc).Milliseconds()
				}
			}
			wires = append(wires, w)
		}
	}

	payload, err := json.Marshal(MCPListResponse{Mounts: wires})
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal mcp_list: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleMCPTest implements MethodMCPTest (Story 48.3 §AC2 / §AC5). Validates
// payload, looks the server up in kernel.MCPRegistry, then dispatches to
// kernel.RunMCPProbe with a request-scoped 12s ctx (probe enforces its own
// 10s internal deadline; the extra 2s absorbs request handling overhead).
//
// Business failures (connect / list failures, timeout) stay inside the
// payload so cmd-side renderers can show stage-by-stage detail. The IPC
// envelope reports OK=false only for routing/validation errors (empty name,
// unknown server, marshal failure) — see Story §易错点 4.
func (s *Server) handleMCPTest(conn net.Conn, payload json.RawMessage) {
	var req MCPTestRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: fmt.Sprintf("parse mcp_test request: %v", err)}})
			return
		}
	}
	if strings.TrimSpace(req.Name) == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "name required"}})
		return
	}

	if s.kern == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: "kernel not wired"}})
		return
	}

	registry := s.kern.MCPRegistry()
	cfg, ok := registry[req.Name]
	if !ok {
		available := availableMCPServerNames(registry)
		msg := fmt.Sprintf("server %q not found in mcp.yaml", req.Name)
		if len(available) > 0 {
			msg = fmt.Sprintf("%s. Available: %s", msg, strings.Join(available, ", "))
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: msg}})
		return
	}

	// Story §AC2 — total probe budget ≤ 10s + small handling slack.
	probeCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	result := s.kern.RunMCPProbe(probeCtx, cfg)
	resp := MCPTestResponse{
		Server:     req.Name,
		OK:         result.OK,
		Stages:     stagesToWire(result.Stages),
		Tools:      result.Tools,
		Resources:  result.Resources,
		Prompts:    result.Prompts,
		ServerInfo: result.ServerInfo,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal mcp_test: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: body})
}

// handleMCPLogs implements MethodMCPLogs (Story 48.5 AC4). Resolves the
// server's live transport via the kernel surface and returns its stderr ring
// buffer. An unknown server returns NOT_FOUND with the available (mounted)
// server list as a copy-paste hint — mirroring handleMCPTest's NOT_FOUND.
func (s *Server) handleMCPLogs(conn net.Conn, payload json.RawMessage) {
	var req MCPLogsRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: fmt.Sprintf("parse mcp_logs request: %v", err)}})
			return
		}
	}
	if strings.TrimSpace(req.Name) == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "name required"}})
		return
	}
	if s.kern == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: "kernel not wired"}})
		return
	}

	tr, ok := s.kern.MCPTransportByServer(req.Name)
	if !ok {
		available := s.mountedMCPServerNames()
		msg := fmt.Sprintf("mcp server %q not found", req.Name)
		if len(available) > 0 {
			msg = fmt.Sprintf("%s. Available: %s", msg, strings.Join(available, ", "))
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: msg}})
		return
	}

	body, err := json.Marshal(MCPLogsResponse{Lines: tr.StderrTail()})
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal mcp_logs: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: body})
}

// mountedMCPServerNames returns the server names currently held in the mount
// table, sorted. Unlike availableMCPServerNames (which reads the mcp.yaml
// registry), this reflects what is actually MOUNTED — the right hint for a
// `mcp logs` NOT_FOUND, since logs only exist for live mounts.
func (s *Server) mountedMCPServerNames() []string {
	if s.kern == nil {
		return nil
	}
	var out []string
	for _, m := range s.kern.ListMounts() {
		out = append(out, mcpMountNameFromPath(m.Path))
	}
	sort.Strings(out)
	return out
}

// mcpMountNameFromPath parses the server name from a `/mnt/mcp/<pid>-<name>`
// path. Returns the raw path when the format does not match (no panic, no
// silent drop in the wire — operator can still see something to debug).
func mcpMountNameFromPath(path string) string {
	const prefix = "/mnt/mcp/"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	leaf := strings.TrimPrefix(path, prefix)
	if dash := strings.Index(leaf, "-"); dash >= 0 && dash+1 < len(leaf) {
		return leaf[dash+1:]
	}
	return leaf
}

// mcpTransportLabel returns the user-facing transport label for a config.
// Currently only stdio is supported; empty maps to the default rather than
// confusing the operator with a blank cell.
func mcpTransportLabel(cfg vfs.MCPConfig) string {
	if cfg.TransportType == "" {
		return "stdio"
	}
	return cfg.TransportType
}

// availableMCPServerNames returns the registry keys sorted alphabetically.
// Code review patch P2 — earlier "sorted-ish" map iteration produced
// non-deterministic NOT_FOUND error messages flickering across daemon restarts
// and ATDD asserting substring match still passed; sort.Strings makes the
// "Available: ..." hint copy-paste reproducible.
func availableMCPServerNames(registry map[string]vfs.MCPConfig) []string {
	if len(registry) == 0 {
		return nil
	}
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// stagesToWire converts kernel.MCPProbeStage entries to wire form, preserving
// nil-as-empty-slice for deterministic JSON output.
func stagesToWire(in []kernel.MCPProbeStage) []MCPTestStageWire {
	out := make([]MCPTestStageWire, 0, len(in))
	for _, s := range in {
		out = append(out, MCPTestStageWire{
			Name:       s.Name,
			OK:         s.OK,
			DurationMs: s.DurationMs,
			Error:      s.Error,
		})
	}
	return out
}
