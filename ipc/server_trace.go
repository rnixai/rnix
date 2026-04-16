package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
)

// --- Trace Handlers (Story 27.9) ---

func (s *Server) traceBaseDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	return filepath.Join(cwd, ".rnix", "traces"), nil
}

func (s *Server) handleTraceList(conn net.Conn) {
	baseDir, err := s.traceBaseDir()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}
	reader := debug.NewSpanReader(baseDir)
	summaries, err := reader.ListTraces()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	wires := make([]TraceSummaryWire, len(summaries))
	for i, ts := range summaries {
		wires[i] = TraceSummaryWire{
			TraceID:         string(ts.TraceID),
			SpanCount:       ts.SpanCount,
			StartTimeMs:     ts.StartTime.UnixMilli(),
			TotalDurationMs: ts.TotalDuration.Milliseconds(),
			RootSpanName:    ts.RootSpanName,
		}
	}

	resp := TraceListResponse{Traces: wires}
	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleTraceTree(conn net.Conn, rawPayload json.RawMessage) {
	var req TraceTreeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid trace_tree request"}})
		return
	}

	// Validate TraceID: reject path traversal attempts
	if req.TraceID == "" || strings.Contains(req.TraceID, "/") || strings.Contains(req.TraceID, "\\") || strings.Contains(req.TraceID, "..") {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid trace ID"}})
		return
	}

	baseDir, err := s.traceBaseDir()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}
	reader := debug.NewSpanReader(baseDir)
	spans, err := reader.ReadSpans(types.TraceID(req.TraceID))
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	tree := debug.BuildSpanTree(spans)
	if tree == nil {
		// Return empty tree
		resp := TraceTreeResponse{Tree: &SpanTreeWire{TraceID: req.TraceID}}
		payload, _ := json.Marshal(resp)
		writeResponse(conn, Response{OK: true, Payload: payload})
		return
	}

	// Convert debug.SpanTree → SpanTreeWire
	wire := &SpanTreeWire{
		TraceID: tree.TraceID,
		Metadata: TraceMetaWire{
			TotalSpans:      tree.Metadata.TotalSpans,
			TotalTokens:     tree.Metadata.TotalTokens,
			TotalDurationMs: tree.Metadata.TotalDuration.Milliseconds(),
			ErrorCount:      tree.Metadata.ErrorCount,
		},
	}
	if tree.Root != nil {
		root := spanNodeToWire(tree.Root)
		wire.Root = &root
	}

	resp := TraceTreeResponse{Tree: wire}
	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func spanNodeToWire(node *debug.SpanNode) SpanNodeWire {
	if node == nil || node.Span == nil {
		return SpanNodeWire{}
	}
	w := SpanNodeWire{
		SpanID:       string(node.Span.SpanID),
		ParentSpanID: string(node.Span.ParentSpanID),
		PID:          uint64(node.Span.PID),
		Name:         node.Span.Name,
		DurationMs:   node.Span.Duration.Milliseconds(),
		TokensUsed:   node.Span.TokensUsed,
		Status:       node.Span.Status.String(),
	}
	for _, child := range node.Children {
		if child != nil {
			w.Children = append(w.Children, spanNodeToWire(child))
		}
	}
	return w
}
