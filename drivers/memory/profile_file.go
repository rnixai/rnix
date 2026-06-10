package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/vfs"
)

// MemoryProfileFile implements vfs.VFSFile for /dev/memory/profile.
// It reuses the memoryRequest/memoryResponse JSON protocol from /dev/memory/commit,
// but forces the target to "user" for all operations.
type MemoryProfileFile struct {
	driver   *MemoryProfileDriver
	workDir  string // caller's project dir for per-project routing (user target)
	response []byte
}

// Write processes a user profile operation. The data must be valid JSON matching memoryRequest.
// The target field is always overridden to "user".
func (f *MemoryProfileFile) Write(_ context.Context, data []byte) error {
	var req memoryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		f.response = marshalResponse(memoryResponse{OK: false, Error: fmt.Sprintf("invalid JSON: %v", err)})
		return nil
	}

	// Force target to "user" — profile device always operates on USER.md
	req.Target = "user"
	store := f.driver.store

	switch req.Action {
	case "add":
		if err := store.Add(req.Target, req.Content, f.workDir); err != nil {
			f.response = marshalResponse(memoryResponse{OK: false, Error: err.Error()})
			return nil
		}
		f.response = marshalResponse(memoryResponse{OK: true, Message: "user profile entry added"})
	case "replace":
		if err := store.Replace(req.Target, req.Old, req.New, f.workDir); err != nil {
			f.response = marshalResponse(memoryResponse{OK: false, Error: err.Error()})
			return nil
		}
		f.response = marshalResponse(memoryResponse{OK: true, Message: "user profile entry replaced"})
	case "remove":
		if err := store.Remove(req.Target, req.Old, f.workDir); err != nil {
			f.response = marshalResponse(memoryResponse{OK: false, Error: err.Error()})
			return nil
		}
		f.response = marshalResponse(memoryResponse{OK: true, Message: "user profile entry removed"})
	case "snapshot":
		snap := store.Snapshot(req.Target, f.workDir)
		f.response = marshalResponse(memoryResponse{OK: true, Snapshot: snap})
	case "capacity":
		used, limit := store.Capacity(req.Target, f.workDir)
		f.response = marshalResponse(memoryResponse{OK: true, Used: used, Limit: limit})
	default:
		f.response = marshalResponse(memoryResponse{OK: false, Error: fmt.Sprintf("unknown action: %q", req.Action)})
	}
	return nil
}

// Read returns the response from the last Write operation as JSON.
func (f *MemoryProfileFile) Read(length int) ([]byte, error) {
	if f.response == nil {
		return []byte(`{"ok":false,"error":"no operation performed yet"}`), nil
	}
	resp := f.response
	if length > 0 && len(resp) > length {
		resp = resp[:length]
	}
	return resp, nil
}

// Close is a no-op for the memory profile device.
func (f *MemoryProfileFile) Close() error {
	return nil
}

// Stat returns file metadata.
func (f *MemoryProfileFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{
		Name:       "MemoryProfile",
		IsDevice:   true,
		DevicePath: "/dev/memory/profile",
	}, nil
}
