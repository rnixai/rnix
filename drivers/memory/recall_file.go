package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	kernelmemory "github.com/rnixai/rnix/kernel/memory"
	"github.com/rnixai/rnix/vfs"
)

// recallLLMTimeout is the maximum time for LLM summarization.
const recallLLMTimeout = 30 * time.Second

// MemoryRecallFile implements vfs.VFSFile for /dev/memory/recall.
// Write accepts a search query; Read returns the search results.
type MemoryRecallFile struct {
	driver   *MemoryRecallDriver
	response []byte // last Write response (JSON)
}

// recallRequest is the JSON structure expected by Write.
type recallRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Summarize  bool   `json:"summarize"`
}

// recallResponse is the JSON structure returned by Read after a Write.
type recallResponse struct {
	OK      bool                        `json:"ok"`
	Error   string                      `json:"error,omitempty"`
	Results []kernelmemory.RecallResult `json:"results,omitempty"`
	Summary string                      `json:"summary,omitempty"`
	Count   int                         `json:"count"`
}

// Write processes a recall search request.
func (f *MemoryRecallFile) Write(_ context.Context, data []byte) error {
	var req recallRequest
	if err := json.Unmarshal(data, &req); err != nil {
		f.response = marshalRecallResponse(recallResponse{OK: false, Error: fmt.Sprintf("invalid JSON: %v", err)})
		return nil
	}

	if req.Query == "" {
		f.response = marshalRecallResponse(recallResponse{OK: false, Error: "query is required"})
		return nil
	}

	if !f.driver.searcher.Ready() {
		f.response = marshalRecallResponse(recallResponse{OK: false, Error: "recall index is still building, try again shortly"})
		return nil
	}

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}

	results := f.driver.searcher.Search(req.Query, maxResults)

	resp := recallResponse{
		OK:      true,
		Results: results,
		Count:   len(results),
	}

	// Optional LLM summarization
	if req.Summarize && f.driver.caller != nil && len(results) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), recallLLMTimeout)
		defer cancel()
		summary, err := kernelmemory.SummarizeRecallResults(ctx, f.driver.caller, req.Query, results)
		if err != nil {
			log.Printf("[recall] LLM summarize failed: %v", err)
			// Continue without summary — don't fail the request
		} else {
			resp.Summary = summary
		}
	}

	f.response = marshalRecallResponse(resp)
	return nil
}

// Read returns the response from the last Write operation as JSON.
func (f *MemoryRecallFile) Read(length int) ([]byte, error) {
	if f.response == nil {
		return []byte(`{"ok":false,"error":"no search performed yet"}`), nil
	}
	resp := f.response
	if length > 0 && len(resp) > length {
		resp = resp[:length]
	}
	return resp, nil
}

// Close is a no-op for the memory recall device.
func (f *MemoryRecallFile) Close() error {
	return nil
}

// Stat returns file metadata.
func (f *MemoryRecallFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{
		Name:       "memory_recall",
		IsDevice:   true,
		DevicePath: "/dev/memory/recall",
	}, nil
}

// marshalRecallResponse encodes a recallResponse to JSON.
func marshalRecallResponse(resp recallResponse) []byte {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Appendf(nil, `{"ok":false,"error":"marshal error: %s"}`, err)
	}
	return data
}
