package vfs

// RawCapture is the neutral on-the-wire envelope for a single LLM call's
// raw request/response pair (Story 56.1 geological foundation for Epic 56).
//
// Drivers (56.2 API / 56.3 CLI) populate this struct inside Call/Stream and
// expose the latest one via RawCaptureProvider; the kernel pulls it after
// each successful LLM Write and appends it to <baseDir>/steps/<uuid>/raw.jsonl
// (one NDJSON line per reasonStep).
//
// 56.1 only locks the envelope shape. Concrete per-family Request/Response
// fields (HTTP headers/body vs CLI argv/stdin/stdout) are filled by 56.2/56.3
// without changing the wire layout.
//
// JSON tags are snake_case (project convention: project-context.md "输出格式").
type RawCapture struct {
	TsMs          int64          `json:"ts_ms"`
	Step          int            `json:"step"`
	Kind          string         `json:"kind"` // "api" | "cli"
	Request       map[string]any `json:"request,omitempty"`
	Response      map[string]any `json:"response,omitempty"`
	Truncated     bool           `json:"truncated"`
	OriginalBytes int64          `json:"original_bytes"`
}

// RawCaptureProvider is an optional interface for VFSFile implementations
// (Story 56.1; analogous to ReasoningEffortProvider in vfs.go). The kernel
// type-asserts the LLM file after a successful Write and pulls the most
// recent raw capture from the driver, then forwards it to RawWriter.
//
// Pull semantics: each call returns the capture for the most recent driver
// invocation, or nil if the underlying driver does not implement the
// drivers/llm internal rawCaptureDriver interface (true for *all* drivers
// in 56.1; 56.2/56.3 will populate it).
type RawCaptureProvider interface {
	LastRawCapture() *RawCapture
}
