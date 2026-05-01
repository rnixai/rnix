package intentdriver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

var _ vfs.ToolDescriptor = (*IntentDriver)(nil)

// IntentDriver provides VFS device access to the intent management system.
type IntentDriver struct {
	manager *intent.Manager
}

func NewDriver(mgr *intent.Manager) *IntentDriver {
	return &IntentDriver{manager: mgr}
}

func (d *IntentDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "intent_decompose",
			Description:       loadPrompt("intent_decompose"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "intent decompose plan task goal",
			MaxResultTokens:   8192,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent": map[string]any{
						"type":        "string",
						"description": "高层意图描述",
					},
					"model": map[string]any{
						"type":        "string",
						"description": "LLM 模型名称（可选）",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "LLM provider 名称（可选，覆盖默认）",
					},
				},
				"required": []string{"intent"},
			},
		},
		{
			Name:              "intent_status",
			Description:       loadPrompt("intent_status"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "intent status query",
			MaxResultTokens:   4096,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent_id": map[string]any{
						"type":        "string",
						"description": "Intent ID（如 intent-1）",
					},
				},
				"required": []string{"intent_id"},
			},
		},
		{
			Name:              "intent_confirm",
			Description:       loadPrompt("intent_confirm"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "intent confirm approve",
			MaxResultTokens:   256,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent_id": map[string]any{
						"type":        "string",
						"description": "Intent ID（如 intent-1）",
					},
				},
				"required": []string{"intent_id"},
			},
		},
		{
			Name:              "intent_execute",
			Description:       loadPrompt("intent_execute"),
			IsReadOnly:        false,
			IsConcurrencySafe: false,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "intent execute run reconcile",
			MaxResultTokens:   4096,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent_id": map[string]any{
						"type":        "string",
						"description": "Intent ID（如 intent-1）",
					},
				},
				"required": []string{"intent_id"},
			},
		},
	}
}

// IntentFile implements vfs.VFSFile for /dev/intent via write-then-read semantics.
type IntentFile struct {
	driver     *IntentDriver
	subpath    string
	devicePath string
	response   []byte
	offset     int
	closed     bool
}

func (f *IntentFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("intent file closed"), Code: types.ErrDriver}
	}

	var result any
	var opErr error

	switch f.subpath {
	case "", "/decompose":
		result, opErr = f.handleDecompose(ctx, data)
	case "/status":
		result, opErr = f.handleStatus(data)
	case "/confirm":
		result, opErr = f.handleConfirm(data)
	case "/execute":
		result, opErr = f.handleExecute(ctx, data)
	default:
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("unknown subpath %q", f.subpath), Code: types.ErrNotFound}
	}

	if opErr != nil {
		return opErr
	}

	respJSON, err := json.Marshal(result)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("marshal response: %w", err), Code: types.ErrInternal}
	}
	f.response = respJSON
	f.offset = 0
	return nil
}

func (f *IntentFile) handleDecompose(ctx context.Context, data []byte) (*intent.IntentTree, error) {
	var req struct {
		Intent   string `json:"intent"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid decompose request: %w", err), Code: types.ErrInvalid}
	}
	if req.Intent == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("intent is required"), Code: types.ErrInvalid}
	}

	tree, err := f.driver.manager.Apply(ctx, intent.ApplyRequest{
		Intent:   req.Intent,
		Model:    req.Model,
		Provider: req.Provider,
	})
	if err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrDriver}
	}
	return tree, nil
}

func (f *IntentFile) handleStatus(data []byte) (*intent.IntentTree, error) {
	var req struct {
		IntentID string `json:"intent_id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid status request: %w", err), Code: types.ErrInvalid}
	}
	if req.IntentID == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("intent_id is required"), Code: types.ErrInvalid}
	}

	tree, err := f.driver.manager.Status(intent.IntentID(req.IntentID))
	if err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrNotFound}
	}
	return tree, nil
}

func (f *IntentFile) handleConfirm(data []byte) (any, error) {
	var req struct {
		IntentID string `json:"intent_id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid confirm request: %w", err), Code: types.ErrInvalid}
	}
	if req.IntentID == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("intent_id is required"), Code: types.ErrInvalid}
	}

	if err := f.driver.manager.Confirm(intent.IntentID(req.IntentID)); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrDriver}
	}
	return map[string]bool{"ok": true}, nil
}

func (f *IntentFile) handleExecute(ctx context.Context, data []byte) (any, error) {
	var req struct {
		IntentID string `json:"intent_id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid execute request: %w", err), Code: types.ErrInvalid}
	}
	if req.IntentID == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("intent_id is required"), Code: types.ErrInvalid}
	}

	if err := f.driver.manager.Execute(ctx, intent.IntentID(req.IntentID), intent.ReconcilerCallbacks{}); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrDriver}
	}
	return map[string]bool{"ok": true}, nil
}

func (f *IntentFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("intent file closed"), Code: types.ErrDriver}
	}
	if f.response == nil {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("no data available: write a request first"), Code: types.ErrDriver}
	}

	remaining := f.response[f.offset:]
	if len(remaining) == 0 {
		return nil, nil
	}
	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}
	out := make([]byte, length)
	copy(out, remaining[:length])
	f.offset += length
	return out, nil
}

func (f *IntentFile) Close() error {
	if f.closed {
		return &types.DriverError{Op: "Close", Device: f.devicePath, Err: fmt.Errorf("intent file already closed"), Code: types.ErrDriver}
	}
	f.closed = true
	f.response = nil
	f.offset = 0
	return nil
}

func (f *IntentFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, &types.DriverError{Op: "Stat", Device: f.devicePath, Err: fmt.Errorf("intent file closed"), Code: types.ErrDriver}
	}
	return vfs.FileStat{
		Name:       f.devicePath,
		Size:       int64(len(f.response)),
		IsDevice:   true,
		DevicePath: "/dev/intent",
	}, nil
}

var validSubpaths = map[string]bool{
	"":           true,
	"/decompose": true,
	"/status":    true,
	"/confirm":   true,
	"/execute":   true,
}

// FileFactory returns a VFSFileFactory that creates IntentFile instances.
func FileFactory(driver *IntentDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		if !validSubpaths[subpath] {
			return nil, &types.DriverError{Op: "Open", Device: "/dev/intent" + subpath, Err: fmt.Errorf("unknown subpath %q", subpath), Code: types.ErrNotFound}
		}
		return &IntentFile{
			driver:     driver,
			subpath:    subpath,
			devicePath: "/dev/intent" + subpath,
		}, nil
	}
}
