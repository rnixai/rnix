package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/vfs"
)

// MemoryCommitFile implements vfs.VFSFile for /dev/memory/commit.
// Each Write dispatches to the MemoryStore based on the action field.
// The response from the last Write is available via Read.
type MemoryCommitFile struct {
	driver   *MemoryCommitDriver
	workDir  string // caller's project dir (ProjectConfig.ProjectDir) for per-project routing
	response []byte // last Write response (JSON)
}

// memoryRequest is the JSON structure expected by Write.
type memoryRequest struct {
	Action  string `json:"action"`
	Target  string `json:"target"`
	Content string `json:"content,omitempty"`
	Old     string `json:"old,omitempty"`
	New     string `json:"new,omitempty"`
}

// memoryResponse is the JSON structure returned by Read after a Write.
type memoryResponse struct {
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
	Used     int    `json:"used,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// Write processes a memory operation. The data must be valid JSON matching memoryRequest.
func (f *MemoryCommitFile) Write(_ context.Context, data []byte) error {
	var req memoryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		f.response = marshalResponse(memoryResponse{OK: false, Error: fmt.Sprintf("invalid JSON: %v", err)})
		return nil // error is in the response, consistent with store error handling
	}

	if req.Target == "" {
		req.Target = "memory"
	}

	store := f.driver.store

	switch req.Action {
	case "add":
		if err := store.Add(req.Target, req.Content, f.workDir); err != nil {
			f.response = marshalResponse(memoryResponse{OK: false, Error: err.Error()})
			return nil // error is in the response, not a VFS-level error
		}
		f.response = marshalResponse(memoryResponse{OK: true, Message: fmt.Sprintf("entry added to %s", req.Target)})

	case "replace":
		if err := store.Replace(req.Target, req.Old, req.New, f.workDir); err != nil {
			f.response = marshalResponse(memoryResponse{OK: false, Error: err.Error()})
			return nil
		}
		f.response = marshalResponse(memoryResponse{OK: true, Message: fmt.Sprintf("entry replaced in %s", req.Target)})

	case "remove":
		if err := store.Remove(req.Target, req.Old, f.workDir); err != nil {
			f.response = marshalResponse(memoryResponse{OK: false, Error: err.Error()})
			return nil
		}
		f.response = marshalResponse(memoryResponse{OK: true, Message: fmt.Sprintf("entry removed from %s", req.Target)})

	case "snapshot":
		snap := store.Snapshot(req.Target, f.workDir)
		f.response = marshalResponse(memoryResponse{OK: true, Snapshot: snap})

	case "capacity":
		used, limit := store.Capacity(req.Target, f.workDir)
		f.response = marshalResponse(memoryResponse{OK: true, Used: used, Limit: limit})

	default:
		f.response = marshalResponse(memoryResponse{OK: false, Error: fmt.Sprintf("unknown action: %q", req.Action)})
		return nil // error is in the response
	}

	return nil
}

// Read returns the response from the last Write operation as JSON.
func (f *MemoryCommitFile) Read(length int) ([]byte, error) {
	if f.response == nil {
		return []byte(`{"ok":false,"error":"no operation performed yet"}`), nil
	}
	resp := f.response
	if length > 0 && len(resp) > length {
		resp = resp[:length]
	}
	return resp, nil
}

// Close is a no-op for the memory commit device.
func (f *MemoryCommitFile) Close() error {
	return nil
}

// Stat returns file metadata.
func (f *MemoryCommitFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{
		Name:       "memory_commit",
		IsDevice:   true,
		DevicePath: "/dev/memory/commit",
	}, nil
}

// marshalResponse encodes a memoryResponse to JSON, falling back to an error string.
func marshalResponse(resp memoryResponse) []byte {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Appendf(nil, `{"ok":false,"error":"marshal error: %s"}`, err)
	}
	return data
}
