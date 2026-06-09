package vfs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ProcessInfoProvider provides process information for ProcFS.
// Defined in the consumer (vfs/) per Go convention; implemented by kernel.KernelImpl via duck typing.
type ProcessInfoProvider interface {
	GetProcInfo(pid types.PID) (*ProcInfo, error)
	ListProcs() []ProcInfo
}

// ContextSummaryProvider provides context summary for /proc/{pid}/context.
// Defined in the consumer (vfs/) per Go convention; implemented by context.Manager via duck typing.
type ContextSummaryProvider interface {
	GetContextSummary(ctxID types.CtxID) (string, error)
}

// MCPMountSnapshot captures one MCP mount's path + full transport config so
// proc-info.json carries everything required to re-mount on resume / daemon
// restart. Story 48.1 — Resume path must rebuild MCP transports from disk;
// before this struct existed, procInfoDisk only persisted PID/UUID/AllowedDevices,
// so the resumed process silently lost MCP tool access (Investigation Finding 9).
//
// Path is the canonical `/mnt/mcp/<original-pid>-<server-name>` string written
// at first spawn (kernel/spawn.go:619). The resume path MUST re-use this PID
// rather than the new PID assigned by NewProcess — AllowedDevices stores the
// same path string, and toolMap construction depends on three-way alignment
// (Path ↔ AllowedDevices entry ↔ DeviceRegistry key). See Story 48.1 易错点 #1.
type MCPMountSnapshot struct {
	Path   string    `json:"path"`
	Config MCPConfig `json:"config"`
}

// ProcInfo is a snapshot of process information at a point in time.
// Value type (not a reference to *Process) to avoid concurrency issues and cross-package dependencies.
type ProcInfo struct {
	PID            types.PID
	UUID           string
	OriginUUID     string // non-empty when forked from another process (resume --fork)
	ResumedFromStep int   // step number from which this process was resumed
	ExitReason     string // exit reason (e.g. "context_full", "complete", ...) — used by resume to detect context_full
	CtxSize        int    // context message slot limit at allocation (preserves original size on resume)
	PPID           types.PID
	ParentUUID     string // UUID of parent process — used for accurate tree building across PID reuse
	State          types.ProcessState
	Intent         string
	Skills         []string
	TokensUsed      int
	LastInputTokens int
	ContextBudget   int
	MaxTokens      int64
	MaxCost        float64
	UsedCost       float64
	MaxSteps       int
	CreatedAt      time.Time
	DeadAt         time.Time
	CtxID          types.CtxID
	Result         string
	AllowedDevices []string
	// DeniedDevices is the process device blocklist, checked before AllowedDevices
	// in kernel tool dispatch. Persisted alongside AllowedDevices (Story 37.6) so
	// resume / daemon-restart revival keeps the anti-recursive-orchestration guard
	// (deny /dev/intent) that Story 37-5 installs on spawned children — without
	// this the blocklist resumes as nil and the guard silently lapses.
	DeniedDevices  []string
	// AllowedTools is the process authoritative tool-name whitelist (Story 54.1),
	// persisted alongside AllowedDevices so resume / daemon-restart revival keeps
	// tool-level enforcement. Empty on legacy snapshots → rebuilt from AllowedDevices.
	AllowedTools   []string
	Provider       string
	Model          string
	// PrimaryDevice is the LLM VFS device path (e.g. "/dev/llm/claude") this
	// process was wired to at spawn / resume time. Persisted to proc-info.json so
	// daemon-restart placeholder revival (kernel/load_suspended.go) can route
	// reasonStep-driven processes back through openLLMDeviceForResume instead of
	// being misclassified as script-runners (PrimaryDevice == "" branch in
	// kernel/subtree.go:resumeOneForSubtree).
	PrimaryDevice  string
	// ProjectDir is the absolute path to the project root the process was
	// spawned from (parent of .rnix/). Persisted so LoadSuspendedFromDisk can
	// invoke the kernel's projectConfigLoader to re-attach a full ProjectConfig
	// — without this, dashboard `r` against a placeholder using a project-only
	// LLM provider (e.g. EchoMatrix's opencodego) fails with `device not
	// found: /dev/llm/<provider>` because the global VFS has no such device.
	ProjectDir     string
	ContextWindow  int
	LastHeartbeat  time.Time
	StepTimeout    time.Duration
	SuspendReason  string
	IsPaused       bool          // true when SIGPAUSE is active (reasoning loop blocked)
	PausedAt       time.Time     // when pause started; zero if not paused
	PausedTotal    time.Duration // accumulated paused duration across all pause/resume cycles

	// Orchestration metadata (Story 34.7)
	ComposeNode   string   // compose node name (e.g. "summarizer"), empty = not compose
	ComposeDeps   []string // depends_on node names (e.g. ["researcher", "analyst"])
	PipelineIndex int      // 0-based stage index in pipeline (only meaningful when PipelineTotal > 0)
	PipelineTotal int      // total stages in pipeline, 0 = not pipeline

	// Exit code authoritative signal: 0 = success, non-zero = failure.
	// Only meaningful when ExitCodeSet=true; otherwise unknown (legacy data or
	// process not yet exited) and dashboard falls back to result-text heuristic.
	ExitCode    int
	ExitCodeSet bool

	// MCPMounts is the persisted snapshot of every MCP server mounted onto this
	// process at its most recent live observation (Story 48.1 Task 1.1). Empty /
	// nil for processes that did not reference MCP servers (the AC6 zero-overhead
	// path). The resume path consumes this slice in rehydrate to re-call
	// MountManager.Mount with the original PID + complete vfs.MCPConfig, so
	// dashboard `r` after daemon restart or `rnix resume <uuid>` after reap no
	// longer silently strips MCP tool access.
	//
	// json tag uses snake_case to match the on-disk schema (procInfoDisk
	// stores the same data under `mcp_mounts`) and the rest of ProcInfo's
	// effective IPC contract — Story 48.1 code-review P11 fixed the prior
	// silent PascalCase/snake_case split.
	MCPMounts []MCPMountSnapshot `json:"mcp_mounts,omitempty"`

	// DriverMeta holds optional runtime metadata from the LLM driver (e.g.
	// resolved binary path, permission mode, capability flags). Populated at
	// spawn time via DriverMetaProvider interface; nil for drivers that don't
	// implement it.
	DriverMeta      map[string]string `json:"driver_meta,omitempty"`
	FeatureProfile  string            `json:"feature_profile,omitempty"`
}

// ProcFS implements a read-only /proc filesystem that exposes process runtime state.
type ProcFS struct {
	provider    ProcessInfoProvider
	ctxProvider ContextSummaryProvider
}

// NewProcFS creates a new ProcFS with the given providers.
func NewProcFS(provider ProcessInfoProvider, ctxProvider ContextSummaryProvider) *ProcFS {
	return &ProcFS{
		provider:    provider,
		ctxProvider: ctxProvider,
	}
}

// FileFactory returns a VFSFileFactory that creates procFile instances for /proc subpaths.
func (p *ProcFS) FileFactory() VFSFileFactory {
	return func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
		pid, file, err := parseProcPath(subpath)
		if err != nil {
			return nil, &VFSError{
				Op:     "Open",
				Device: "/proc",
				Err:    err,
				Code:   types.ErrNotFound,
			}
		}

		info, err := p.provider.GetProcInfo(pid)
		if err != nil {
			return nil, err
		}

		var content []byte
		switch file {
		case "status":
			content, err = buildStatusJSON(info)
		case "intent":
			content = buildIntent(info)
		case "context":
			content, err = buildContext(p.ctxProvider, info.CtxID)
		}
		if err != nil {
			return nil, &VFSError{
				Op:     "Open",
				Device: "/proc",
				Err:    err,
				Code:   types.ErrInternal,
			}
		}

		return &procFile{
			content: content,
			path:    fmt.Sprintf("/proc/%d/%s", pid, file),
		}, nil
	}
}

// parseProcPath parses a subpath like "/1/status" into (pid, filename, error).
func parseProcPath(subpath string) (types.PID, string, error) {
	parts := strings.SplitN(strings.TrimPrefix(subpath, "/"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, "", fmt.Errorf("invalid proc path: %s", subpath)
	}
	pid, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid PID: %s", parts[0])
	}
	file := parts[1]
	if file != "status" && file != "intent" && file != "context" {
		return 0, "", fmt.Errorf("unknown proc file: %s", file)
	}
	return types.PID(pid), file, nil
}

// procFile is a read-only VFSFile backed by pre-computed content (snapshot at Open time).
type procFile struct {
	content []byte
	offset  int
	path    string
}

// Read returns content from the current offset, advancing the offset.
func (f *procFile) Read(length int) ([]byte, error) {
	if f.offset >= len(f.content) {
		return nil, nil // EOF
	}
	end := min(f.offset+length, len(f.content))
	data := f.content[f.offset:end]
	f.offset = end
	return data, nil
}

// Write returns an error because /proc is read-only.
func (f *procFile) Write(_ context.Context, _ []byte) error {
	return &VFSError{
		Op:     "Write",
		Device: "/proc",
		Err:    fmt.Errorf("/proc is read-only"),
		Code:   types.ErrPermission,
	}
}

// Close is a no-op for procFile (no resources to release).
func (f *procFile) Close() error {
	return nil
}

// Stat returns file metadata for this procFile.
func (f *procFile) Stat() (FileStat, error) {
	return FileStat{
		Name: f.path,
		Size: int64(len(f.content)),
	}, nil
}

// stateToString maps ProcessState constants to human-readable JSON strings.
func stateToString(s types.ProcessState) string {
	return s.String()
}

// statusJSON is the JSON structure for /proc/{pid}/status.
type statusJSON struct {
	PID            types.PID `json:"pid"`
	PPID           types.PID `json:"ppid"`
	State          string    `json:"state"`
	Intent         string    `json:"intent"`
	Skills         []string  `json:"skills"`
	TokensUsed     int       `json:"tokens_used"`
	ContextBudget  int       `json:"context_budget,omitempty"`
	ElapsedMs      int64     `json:"elapsed_ms"`
	AllowedDevices []string  `json:"allowed_devices"`
	DeniedDevices  []string  `json:"denied_devices,omitempty"`
}

// buildStatusJSON generates the JSON content for /proc/{pid}/status.
func buildStatusJSON(info *ProcInfo) ([]byte, error) {
	s := statusJSON{
		PID:            info.PID,
		PPID:           info.PPID,
		State:          stateToString(info.State),
		Intent:         info.Intent,
		Skills:         info.Skills,
		TokensUsed:     info.TokensUsed,
		ContextBudget:  info.ContextBudget,
		ElapsedMs:      time.Since(info.CreatedAt).Milliseconds(),
		AllowedDevices: info.AllowedDevices,
		DeniedDevices:  info.DeniedDevices,
	}
	if s.Skills == nil {
		s.Skills = []string{}
	}
	if s.AllowedDevices == nil {
		s.AllowedDevices = []string{}
	}
	return json.MarshalIndent(s, "", "    ")
}

// buildIntent returns the raw intent text for /proc/{pid}/intent.
func buildIntent(info *ProcInfo) []byte {
	return []byte(info.Intent)
}

// buildContext calls ContextSummaryProvider to get the context summary for /proc/{pid}/context.
func buildContext(ctxProvider ContextSummaryProvider, ctxID types.CtxID) ([]byte, error) {
	summary, err := ctxProvider.GetContextSummary(ctxID)
	if err != nil {
		return nil, err
	}
	return []byte(summary), nil
}
