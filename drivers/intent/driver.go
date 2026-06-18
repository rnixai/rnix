package intentdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

var _ vfs.ToolDescriptor = (*IntentDriver)(nil)
var _ vfs.CallerProviderAware = (*IntentFile)(nil)
var _ vfs.LLMOpenerAware = (*IntentFile)(nil)
var _ vfs.ProjectConfigAware = (*IntentFile)(nil)
var _ vfs.CallerProcessInfoAware = (*IntentFile)(nil)

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
			Name:              "IntentDecompose",
			Description:       loadPrompt("intent_decompose"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "intent decompose plan task goal",
			MaxResultTokens:   16384,
			Subpath:           "/decompose",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent": map[string]any{
						"type":        "string",
						"description": "High-level intent description",
					},
					"model": map[string]any{
						"type":        "string",
						"description": "LLM model name (optional)",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "LLM provider name (optional, overrides default)",
					},
					"auto_start": map[string]any{
						"type":        "boolean",
						"description": "If true, automatically confirm + execute after a successful decompose, blocking until the whole intent tree finishes; suitable for [AUTO_CONFIRM] flows, avoiding subsequent manual IntentConfirm/IntentExecute calls. Note: orchestration runs synchronously inside the daemon, so sub-tasks that restart the daemon (e.g. rnix daemon stop/restart) may interrupt the orchestration itself — the system intercepts such cases on a best-effort basis by literal matching, but not exhaustively (it can be bypassed via Bash); move such work outside the orchestration",
					},
				},
				"required": []string{"intent"},
			},
		},
		{
			Name:              "IntentStatus",
			Description:       loadPrompt("intent_status"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "intent status query",
			MaxResultTokens:   4096,
			Subpath:           "/status",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent_id": map[string]any{
						"type":        "string",
						"description": "Intent ID (e.g. intent-1)",
					},
				},
				"required": []string{"intent_id"},
			},
		},
		{
			Name:              "IntentConfirm",
			Description:       loadPrompt("intent_confirm"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "intent confirm approve",
			MaxResultTokens:   256,
			Subpath:           "/confirm",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent_id": map[string]any{
						"type":        "string",
						"description": "Intent ID (e.g. intent-1)",
					},
				},
				"required": []string{"intent_id"},
			},
		},
		{
			Name:              "IntentExecute",
			Description:       loadPrompt("intent_execute"),
			IsReadOnly:        false,
			IsConcurrencySafe: false,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "intent execute run reconcile",
			MaxResultTokens:   4096,
			Subpath:           "/execute",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent_id": map[string]any{
						"type":        "string",
						"description": "Intent ID (e.g. intent-1)",
					},
				},
				"required": []string{"intent_id"},
			},
		},
	}
}

// IntentFile implements vfs.VFSFile for /dev/intent via write-then-read semantics.
type IntentFile struct {
	driver         *IntentDriver
	subpath        string
	devicePath     string
	callerProvider string                                       // injected by kernel via CallerProviderAware
	llmOpener      func(provider string) (vfs.VFSFile, error)  // injected by kernel via LLMOpenerAware
	projectConfig  any                                          // injected by kernel via ProjectConfigAware
	callerPID      types.PID                                    // injected by kernel via CallerProcessInfoAware
	callerDepth    int                                          // injected by kernel via CallerProcessInfoAware
	response       []byte
	offset         int
	closed         bool
}

// SetCallerProvider implements vfs.CallerProviderAware.
func (f *IntentFile) SetCallerProvider(provider string) {
	f.callerProvider = provider
}

// SetLLMOpener implements vfs.LLMOpenerAware.
func (f *IntentFile) SetLLMOpener(opener func(provider string) (vfs.VFSFile, error)) {
	f.llmOpener = opener
}

// SetProjectConfig implements vfs.ProjectConfigAware.
func (f *IntentFile) SetProjectConfig(cfg any) {
	f.projectConfig = cfg
}

// SetCallerProcessInfo implements vfs.CallerProcessInfoAware.
func (f *IntentFile) SetCallerProcessInfo(pid types.PID, depth int) {
	f.callerPID = pid
	f.callerDepth = depth
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
		Intent    string `json:"intent"`
		Model     string `json:"model"`
		Provider  string `json:"provider"`
		AutoStart bool   `json:"auto_start"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid decompose request: %w", err), Code: types.ErrInvalid}
	}
	if req.Intent == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("intent is required"), Code: types.ErrInvalid}
	}

	provider := req.Provider
	if provider == "" {
		provider = f.callerProvider
	}
	if f.llmOpener != nil {
		ctx = WithLLMOpener(ctx, f.llmOpener)
	}
	if f.projectConfig != nil {
		ctx = WithProjectConfig(ctx, f.projectConfig)
	}
	if f.callerPID > 0 {
		ctx = WithCallerProcessInfo(ctx, CallerProcessInfo{PID: f.callerPID, Depth: f.callerDepth})
	}
	tree, err := f.driver.manager.Apply(ctx, intent.ApplyRequest{
		Intent:   req.Intent,
		Model:    req.Model,
		Provider: provider,
	})
	if err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrDriver}
	}

	// Auto-start chain: when [AUTO_CONFIRM] flow sets auto_start=true, confirm
	// and execute synchronously inside this single tool call. This collapses
	// the LLM's three-step protocol (decompose → confirm → execute) into one
	// reliable call — LLMs (especially DeepSeek/weak models) frequently drop
	// the intent_id parameter on follow-up calls, leaving the reconciler
	// idle. The synchronous Execute waits for all child processes to finish,
	// so the returned tree reflects terminal state (completed or failed).
	if req.AutoStart {
		// Best-effort guard: auto_start runs the orchestration synchronously
		// inside this daemon process, so a sub-task that stops the daemon may
		// interrupt the orchestration. This is a literal-match reminder over the
		// node's free-text intent, not exhaustive enforcement — it can be evaded
		// (e.g. a sub-agent running kill/pkill/socat via Bash, or phrasings
		// the pattern misses). The reliable fix is eval externalization
		// (rnix-eval); see ADR Decision 43.
		if hits := daemonRestartNodes(tree); len(hits) > 0 {
			parts := make([]string, len(hits))
			for i, h := range hits {
				parts[i] = fmt.Sprintf("node %s: %q", h.NodeID, h.Matched)
			}
			return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Code: types.ErrInvalid,
				Err: fmt.Errorf("auto_start intercepted, on a best-effort basis, sub-tasks that restart the daemon (%s): orchestration runs synchronously inside the daemon, and such sub-tasks may interrupt the orchestration itself. Note this is a literal, text-based reminder, not exhaustive interception — sub-tasks can still bypass it via Bash (kill/pkill/socat, etc.); the reliable approach is to move daemon restarts outside the orchestration, or switch to a step-by-step decompose→confirm→execute", strings.Join(parts, "; "))}
		}
		if err := f.driver.manager.Confirm(tree.ID); err != nil {
			return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("auto_start confirm: %w", err), Code: types.ErrDriver}
		}
		if execErr := f.driver.manager.Execute(ctx, tree.ID, intent.ReconcilerCallbacks{}); execErr != nil {
			// Return error but keep the tree state; caller can inspect.
			return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("auto_start execute: %w", execErr), Code: types.ErrDriver}
		}
		// Re-fetch the tree to capture post-execution state.
		latest, statusErr := f.driver.manager.Status(tree.ID)
		if statusErr == nil {
			tree = latest
		}

		// F2 (assessment integrity): reconciler.Execute returns nil on ANY
		// terminal state — including IntentFailed — so a failed tree would
		// otherwise surface as a *successful* tool result. The caller
		// (orchestrator) then emits `complete` with exit 0, masking child
		// failures (the rnix-eval mcp/hello-mcp "fake success"). Reflect the
		// real terminal state: a failed tree returns a DriverError listing the
		// failed sub-tasks, so the caller's HasToolError trips and `rnix apply`
		// exits non-zero.
		if tree.State == intent.IntentFailed {
			failed := make([]string, 0, len(tree.Nodes))
			for _, node := range tree.Nodes {
				if node.State == intent.IntentFailed {
					msg := node.Error
					if msg == "" {
						msg = "failed"
					}
					failed = append(failed, fmt.Sprintf("%s: %s", node.ID, msg))
				}
			}
			sort.Strings(failed)
			return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Code: types.ErrDriver,
				Err: fmt.Errorf("intent execution failed: %d of %d sub-task(s) failed [%s]", len(failed), len(tree.Nodes), strings.Join(failed, "; "))}
		}
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
