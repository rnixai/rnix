// Package tty implements the /dev/tty VFS device for interactive user Q&A.
package tty

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const (
	// askTimeout is the maximum time to wait for user response.
	askTimeout = 5 * time.Minute
)

// Question represents a single question to ask the user.
type Question struct {
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
}

// Answer represents the user's response to a question.
type Answer struct {
	Answer  string   `json:"answer,omitempty"`
	Answers []string `json:"answers,omitempty"`
}

// AskUserFunc is the callback used to forward questions to the user via IPC.
// It blocks until the user responds or the context is cancelled.
type AskUserFunc func(ctx context.Context, questions []Question) ([]Answer, error)

// TtyDriver implements the /dev/tty VFS device.
type TtyDriver struct {
	askFunc AskUserFunc
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*TtyDriver)(nil)

// NewDriver creates a TtyDriver with the given AskUserFunc callback.
func NewDriver(askFunc AskUserFunc) *TtyDriver {
	return &TtyDriver{askFunc: askFunc}
}

// ToolDefs returns the tool definitions for the tty device.
func (d *TtyDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "AskUserQuestion",
			Description:       loadPrompt("AskUserQuestion"),
			IsReadOnly:        true,
			IsConcurrencySafe: false,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "tty ask user question interact",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"questions": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"question": map[string]any{
									"type":        "string",
									"description": "The question text to display to the user",
								},
								"options": map[string]any{
									"type":        "array",
									"items":       map[string]any{"type": "string"},
									"description": "Predefined answer choices (an 'Other' option is auto-appended)",
								},
								"multi_select": map[string]any{
									"type":        "boolean",
									"description": "Allow multiple selections when options are provided",
								},
							},
							"required": []string{"question"},
						},
						"description":  "Array of 1-4 questions to ask the user",
						"minItems":     1,
						"maxItems":     4,
					},
				},
				"required": []string{"questions"},
			},
		},
	}
}

// TtyFile implements vfs.VFSFile for /dev/tty via write-then-read semantics.
type TtyFile struct {
	driver     *TtyDriver
	devicePath string
	response   []byte
	offset     int
	closed     bool
}

// Write accepts a JSON request with questions, blocks until user responds, and buffers answers.
func (f *TtyFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("tty file closed"), Code: types.ErrDriver}
	}
	if f.driver.askFunc == nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("no ask_user callback configured"), Code: types.ErrServiceUnavailable}
	}

	var req struct {
		Questions []Question `json:"questions"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid JSON input: %w", err), Code: types.ErrInvalid}
	}
	if len(req.Questions) == 0 {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("questions array is empty"), Code: types.ErrInvalid}
	}
	if len(req.Questions) > 4 {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("maximum 4 questions allowed, got %d", len(req.Questions)), Code: types.ErrInvalid}
	}

	askCtx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()

	answers, err := f.driver.askFunc(askCtx, req.Questions)
	if err != nil {
		if askCtx.Err() == context.DeadlineExceeded {
			return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("ask timeout: no user response within %v", askTimeout), Code: types.ErrTimeout}
		}
		if ctx.Err() != nil {
			return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("process cancelled while waiting for user response"), Code: types.ErrDriver}
		}
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("ask failed: %w", err), Code: types.ErrDriver}
	}

	respJSON, err := json.Marshal(answers)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("marshal answers: %w", err), Code: types.ErrInternal}
	}

	f.response = respJSON
	f.offset = 0
	return nil
}

// Read returns buffered answer data.
func (f *TtyFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("tty file closed"), Code: types.ErrDriver}
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

// Close marks the file as closed and releases buffers.
func (f *TtyFile) Close() error {
	if f.closed {
		return &types.DriverError{Op: "Close", Device: f.devicePath, Err: fmt.Errorf("tty file already closed"), Code: types.ErrDriver}
	}
	f.closed = true
	f.response = nil
	f.offset = 0
	return nil
}

// Stat returns metadata about this tty device file.
func (f *TtyFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, &types.DriverError{Op: "Stat", Device: f.devicePath, Err: fmt.Errorf("tty file closed"), Code: types.ErrDriver}
	}
	return vfs.FileStat{
		Name:       f.devicePath,
		Size:       int64(len(f.response)),
		IsDevice:   true,
		DevicePath: "/dev/tty",
	}, nil
}

// FileFactory returns a VFSFileFactory that creates TtyFile instances.
func FileFactory(driver *TtyDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &TtyFile{
			driver:     driver,
			devicePath: "/dev/tty" + subpath,
		}, nil
	}
}
