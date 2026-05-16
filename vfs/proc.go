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
	TokensUsed     int
	ContextBudget  int
	MaxTokens      int64
	MaxCost        float64
	UsedCost       float64
	MaxSteps       int
	CreatedAt      time.Time
	DeadAt         time.Time
	CtxID          types.CtxID
	Result         string
	AllowedDevices []string
	Provider       string
	Model          string
	ContextWindow  int
	LastHeartbeat  time.Time
	StepTimeout    time.Duration
	SuspendReason  string
	IsPaused       bool      // true when SIGPAUSE is active (reasoning loop blocked)
	PausedAt       time.Time // when pause started; zero if not paused

	// Orchestration metadata (Story 34.7)
	ComposeNode   string   // compose node name (e.g. "summarizer"), empty = not compose
	ComposeDeps   []string // depends_on node names (e.g. ["researcher", "analyst"])
	PipelineIndex int      // 0-based stage index in pipeline (only meaningful when PipelineTotal > 0)
	PipelineTotal int      // total stages in pipeline, 0 = not pipeline
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
