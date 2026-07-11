// Package llm implements the LLM driver layer for Rnix.
package llm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

// configureCommandGrace installs a process-group SIGTERM→grace→SIGKILL shutdown
// policy on an exec.Cmd. The child is started in its own process group
// (setProcGroupAttr) so the whole CLI-agent subtree — the leader plus any
// subagents it forks (e.g. claude's sa-step-runner) — can be signalled as a
// group. When the associated context is cancelled (caller timeout / explicit
// Kill / step retry / suspend), the entire group first receives SIGTERM; if it
// does not exit within graceSec seconds, Go's exec machinery force-kills the
// leader (WaitDelay), and the caller's post-Wait reapCommandGroup delivers a
// group SIGKILL backstop for any surviving subagents. Passing graceSec<=0 uses
// DefaultGracePeriod.
//
// Story 66.5: this upgrades the shutdown scope from leader-only to group-wide.
// The signal TYPE is unchanged (still SIGTERM then SIGKILL) — only the delivery
// range widens — so existing SIGTERM-graceful CLI behavior is preserved.
//
// Must be called BEFORE cmd.Start().
func configureCommandGrace(cmd *exec.Cmd, graceSec int) {
	if cmd == nil {
		return
	}
	setProcGroupAttr(cmd)
	grace := time.Duration(graceSec) * time.Second
	if grace <= 0 {
		grace = DefaultGracePeriod
	}
	cmd.Cancel = func() error {
		return groupCancelSIGTERM(cmd)
	}
	cmd.WaitDelay = grace
}

// configureCommandDir locks the child process working directory to projectDir,
// preventing inheritance of the long-running daemon's (stale) cwd. The daemon
// never calls os.Chdir, so its cwd is frozen at the project that first cold-
// started it; without this, CLI agents spawned for a different project would
// inherit that stale cwd and resolve relative paths / cwd-derived state against
// the wrong project (silent cross-project corruption).
//
// Only an absolute path is honored — a relative projectDir would resolve
// against the daemon cwd, reintroducing the very bug this guards against, so it
// is rejected (cwd left to inherit the parent, matching prior behavior).
//
// Must be called BEFORE cmd.Start().
func configureCommandDir(cmd *exec.Cmd, projectDir string) {
	if cmd == nil {
		return
	}
	if projectDir != "" && filepath.IsAbs(projectDir) {
		cmd.Dir = projectDir
	}
}

// configureCommandRnixParentEnv lets CLI-agent-native shell tools preserve the
// rnix process tree when they invoke `rnix -i ...` themselves, and marks the
// child (and any subagents that inherit its env) with RNIX_PROC_UUID so the
// daemon os-reconcile loop can attribute orphaned OS processes (Story 66.5).
func configureCommandRnixParentEnv(cmd *exec.Cmd, req LLMRequest) {
	if cmd == nil || (req.CallerPID == 0 && req.CallerDepth == 0 && req.CallerUUID == "") {
		return
	}
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	if req.CallerPID > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("RNIX_PARENT_PID=%d", req.CallerPID))
	}
	if req.CallerDepth > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("RNIX_SPAWN_DEPTH=%d", req.CallerDepth))
	}
	if req.CallerUUID != "" {
		// Strip any INHERITED RNIX_PROC_UUID before injecting our own. If this
		// daemon was itself launched from a marked CLI-agent (e.g. a `rnix -i`
		// shell tool that triggered EnsureDaemon), cmd.Env=os.Environ() already
		// carries the ancestor's marker; appending a second one would leave two
		// RNIX_PROC_UUID= entries in the child's environ, and the os-reconcile
		// scanner (readProcUUID) returns the FIRST match — the ancestor UUID,
		// which is absent from this daemon's table → every child mis-classified
		// as an orphan and reaped (Story 66.5 code-review F5). Keep exactly one.
		cmd.Env = slices.DeleteFunc(cmd.Env, func(kv string) bool {
			return strings.HasPrefix(kv, "RNIX_PROC_UUID=")
		})
		cmd.Env = append(cmd.Env, "RNIX_PROC_UUID="+req.CallerUUID)
	}
}

// ReasoningBlock represents a single thinking-mode content block.
// Type selects the provider shape:
//   - "thinking" (Anthropic): Signature + Thinking text
//   - "redacted_thinking" (Anthropic): Data (opaque)
//   - "thought" (Gemini 2.5+): Thinking text + ThoughtSignature ([]byte);
//     the signature MUST be echoed on subsequent turns when function calling
//     is involved or the API can lose reasoning context.
//
// JSON tags for the Anthropic-style fields match the Anthropic SDK's
// ContentBlockUnion shape; ThoughtSignature is base64-encoded by encoding/json
// for []byte and round-trips cleanly through context persistence.
type ReasoningBlock struct {
	Type             string `json:"type"`                        // "thinking" | "redacted_thinking" | "thought"
	Thinking         string `json:"thinking,omitempty"`          // raw thinking text (Type="thinking" | "thought")
	Signature        string `json:"signature,omitempty"`         // Anthropic tamper-evident signature (Type="thinking")
	Data             string `json:"data,omitempty"`              // Anthropic opaque payload (Type="redacted_thinking")
	ThoughtSignature []byte `json:"thought_signature,omitempty"` // Gemini opaque thought signature (Type="thought")
}

// Message represents a single message in a conversation.
// JSON tags are compatible with context.Message for VFS bridge interop.
type Message struct {
	Role            string           `json:"role"`
	Content         string           `json:"content"`
	ToolCallID      string           `json:"tool_call_id,omitempty"`
	ToolCalls       []ToolCall       `json:"tool_calls,omitempty"`
	ReasoningBlocks []ReasoningBlock `json:"reasoning_blocks,omitempty"`
	Reasoning       string           `json:"reasoning,omitempty"`
}

// Skill carries an individual skill's content to the driver layer.
// Bundle-capable drivers (SkillsBundleCapable) materialize it into an external
// directory tree and inform the CLI via flags like --add-dir. Non-bundle
// drivers see skill bodies auto-merged into SystemPrompt by the VFS layer.
type Skill struct {
	Name string `json:"name"`
	Body string `json:"body"`          // raw SKILL.md body (no frontmatter)
	Dir  string `json:"dir,omitempty"` // absolute path to the source skill directory
}

// LLMRequest represents a request to an LLM driver.
type LLMRequest struct {
	Intent       string `json:"intent"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Model        string `json:"model,omitempty"`
	// ReasoningEffort overrides the driver instance's configured reasoning
	// effort/level for this request. Empty = fall back to the instance default
	// (the value from ProviderConfig.ReasoningEffort). Passed through verbatim:
	// no validation, mapping, or case normalization. See CLAUDE.md "Reasoning
	// Effort". Mirrors Model's two-tier (instance default + per-request override).
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	MaxTurns        int       `json:"max_turns,omitempty"`
	TimeoutMs       int64     `json:"timeout_ms,omitempty"`
	Messages        []Message `json:"messages,omitempty"`
	Temperature     *float64  `json:"temperature,omitempty"`
	MaxTokens       int       `json:"max_tokens,omitempty"`
	Tools           []ToolDef `json:"tools,omitempty"`
	Skills          []Skill   `json:"skills,omitempty"`      // R5: carried to bundle-capable drivers
	ProjectDir      string    `json:"project_dir,omitempty"` // R5: project root for bundle placement
	CallerPID       uint64    `json:"caller_pid,omitempty"`  // rnix parent PID for CLI-native shell tools
	CallerDepth     int       `json:"caller_depth,omitempty"`
	// CallerUUID is the spawning rnix process's UUIDv7 — globally unique across
	// daemon restarts (unlike PID, which is renumbered). Injected into the CLI
	// child's env as RNIX_PROC_UUID so the daemon os-reconcile loop (Story 66.5)
	// can attribute orphaned OS processes back to their rnix process. JSON tag
	// MUST match kernel/action.go llmRequest (caller_uuid) or the field is
	// silently dropped across the VFS boundary.
	CallerUUID string `json:"caller_uuid,omitempty"`
}

// LLMResponse represents a response from an LLM driver.
type LLMResponse struct {
	Content           string           `json:"content"`
	Reasoning         string           `json:"reasoning,omitempty"`
	ReasoningBlocks   []ReasoningBlock `json:"reasoning_blocks,omitempty"`
	TokensUsed        int              `json:"tokens_used"`
	InputTokens       int              `json:"input_tokens,omitempty"`
	OutputTokens      int              `json:"output_tokens,omitempty"`
	CachedInputTokens int              `json:"cached_input_tokens,omitempty"`
	CostUSD           float64          `json:"cost_usd,omitempty"`
	StopReason        string           `json:"stop_reason,omitempty"`
	ToolCalls         []ToolCall       `json:"tool_calls,omitempty"`
}

// StreamEvent represents a single event in a streaming LLM response.
type StreamEvent struct {
	Type              string           `json:"type"` // "content", "reasoning", "done", "error", "tool_call", "thinking", "system", "user"
	Content           string           `json:"content,omitempty"`
	TokensUsed        int              `json:"tokens_used,omitempty"`
	InputTokens       int              `json:"input_tokens,omitempty"`
	OutputTokens      int              `json:"output_tokens,omitempty"`
	CachedInputTokens int              `json:"cached_input_tokens,omitempty"`
	CostUSD           float64          `json:"cost_usd,omitempty"`
	StopReason        string           `json:"stop_reason,omitempty"`
	ToolCalls         []ToolCall       `json:"tool_calls,omitempty"`
	ReasoningBlocks   []ReasoningBlock `json:"reasoning_blocks,omitempty"`
	Data              map[string]any   `json:"data,omitempty"` // extra metadata (e.g., tool_call details)
	Err               error            `json:"-"`
}

// DriverInfo holds metadata about an LLM driver.
type DriverInfo struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	DefaultModel string `json:"default_model"`
	DriverType   string `json:"driver_type"`
	// ReasoningEffort is the driver's configured reasoning-effort/level string
	// (Story 55.2), surfaced verbatim from the provider config (55.1 passthrough)
	// so the kernel can snapshot it onto the process for display. Empty for
	// drivers without an effort concept (cursor-cli/qwen-cli) or when unset.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
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

// DriverMetaProvider is an optional interface that LLM drivers may implement
// to expose runtime metadata (resolved binary path, capabilities, permission
// mode) for observability. Kernel spawn path uses type assertion to detect.
type DriverMetaProvider interface {
	DriverMeta() map[string]string
}
