// Package llm implements the LLM driver layer for Rnix.
package llm

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"syscall"
	"time"
)

const (
	// streamChanBuffer is the buffer size for stream event channels.
	streamChanBuffer = 64

	// scannerMaxSize is the maximum token size for bufio.Scanner instances
	// that read LLM streaming output. LLM responses can produce very long
	// single-line JSON (e.g. large tool_call results), easily exceeding the
	// default 64 KB limit. 4 MB covers all practical cases.
	scannerMaxSize = 4 * 1024 * 1024 // 4 MB

	// DefaultGracePeriod is the default time a CLI process has to shut down
	// gracefully after receiving SIGTERM before being force-killed.
	DefaultGracePeriod = 20 * time.Second
)

// newStreamScanner creates a bufio.Scanner with a buffer large enough for
// LLM streaming output. All LLM drivers should use this instead of plain
// bufio.NewScanner to avoid "token too long" errors on large responses.
func newStreamScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), scannerMaxSize)
	return s
}

// configureCommandGrace installs a SIGTERM→grace→SIGKILL shutdown policy on
// an exec.Cmd. When the associated context is cancelled (e.g. caller timeout
// or explicit Kill), the process first receives SIGTERM. If it does not exit
// within graceSec seconds, Go's exec machinery force-kills it. Passing
// graceSec<=0 uses DefaultGracePeriod.
//
// Must be called BEFORE cmd.Start().
func configureCommandGrace(cmd *exec.Cmd, graceSec int) {
	if cmd == nil {
		return
	}
	grace := time.Duration(graceSec) * time.Second
	if grace <= 0 {
		grace = DefaultGracePeriod
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = grace
}

// Message represents a single message in a conversation.
// JSON tags are compatible with context.Message for VFS bridge interop.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// Skill carries an individual skill's content to the driver layer.
// Bundle-capable drivers (SkillsBundleCapable) materialize it into an external
// directory tree and inform the CLI via flags like --add-dir. Non-bundle
// drivers see skill bodies auto-merged into SystemPrompt by the VFS layer.
type Skill struct {
	Name string `json:"name"`
	Body string `json:"body"`           // raw SKILL.md body (no frontmatter)
	Dir  string `json:"dir,omitempty"`  // absolute path to the source skill directory
}

// LLMRequest represents a request to an LLM driver.
type LLMRequest struct {
	Intent       string    `json:"intent"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	Model        string    `json:"model,omitempty"`
	MaxTurns     int       `json:"max_turns,omitempty"`
	TimeoutMs    int64     `json:"timeout_ms,omitempty"`
	Messages     []Message `json:"messages,omitempty"`
	Temperature  *float64  `json:"temperature,omitempty"`
	MaxTokens    int       `json:"max_tokens,omitempty"`
	Tools        []ToolDef `json:"tools,omitempty"`
	Skills       []Skill   `json:"skills,omitempty"`      // R5: carried to bundle-capable drivers
	ProjectDir   string    `json:"project_dir,omitempty"` // R5: project root for bundle placement
}

// LLMResponse represents a response from an LLM driver.
type LLMResponse struct {
	Content           string     `json:"content"`
	Reasoning         string     `json:"reasoning,omitempty"`
	TokensUsed        int        `json:"tokens_used"`
	InputTokens       int        `json:"input_tokens,omitempty"`
	OutputTokens      int        `json:"output_tokens,omitempty"`
	CachedInputTokens int        `json:"cached_input_tokens,omitempty"`
	CostUSD           float64    `json:"cost_usd,omitempty"`
	StopReason        string     `json:"stop_reason,omitempty"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
}

// StreamEvent represents a single event in a streaming LLM response.
type StreamEvent struct {
	Type              string         `json:"type"` // "content", "reasoning", "done", "error", "tool_call", "thinking", "system", "user"
	Content           string         `json:"content,omitempty"`
	TokensUsed        int            `json:"tokens_used,omitempty"`
	InputTokens       int            `json:"input_tokens,omitempty"`
	OutputTokens      int            `json:"output_tokens,omitempty"`
	CachedInputTokens int            `json:"cached_input_tokens,omitempty"`
	CostUSD           float64        `json:"cost_usd,omitempty"`
	StopReason        string         `json:"stop_reason,omitempty"`
	ToolCalls         []ToolCall     `json:"tool_calls,omitempty"`
	Data              map[string]any `json:"data,omitempty"` // extra metadata (e.g., tool_call details)
	Err               error          `json:"-"`
}

// DriverInfo holds metadata about an LLM driver.
type DriverInfo struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	DefaultModel string `json:"default_model"`
	DriverType   string `json:"driver_type"`
}

// LLMDriver is the interface that all LLM drivers must implement.
type LLMDriver interface {
	Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
	Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
	Info() DriverInfo
}

// HealthChecker is an optional interface for drivers that support health checks.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}
