package types

import (
	"encoding/json"
	"time"
)

// StepRecord captures the complete data of a single reasonStep execution.
// STUB: Created for ATDD red phase — fields defined per AC-1, not yet populated by kernel.
//
// Messages is stored as json.RawMessage to avoid import cycles with the context package.
// The kernel layer marshals []context.Message into this field before writing.
//
// 设计契约 (Story step-inspector-data-fidelity)：一行 = 一个 reasonStep 完整快照。
//   - 同一 reasonStep 内 LLM 发起的 parallel tool calls 全部进 ToolCalls 数组。
//   - 旧字段 Action/ToolPath/ToolInput/ToolResult/ToolError/ToolDuration 仍保留：
//     文本/完成动作沿用旧字段;tool_call 动作下旧字段从 ToolCalls[len-1] 回填，
//     让旧的 dashboard/replay 客户端读新文件不崩。
//   - 旧 steps.jsonl 中无 ToolCalls 数组的行,读取时 ToolCalls=nil,UI 侧需回退到旧字段。
type StepRecord struct {
	Step              int             `json:"step"`
	Timestamp         time.Duration   `json:"timestamp"`
	Messages          json.RawMessage `json:"messages"`
	MessageCount      int             `json:"message_count"`
	TokenCount        int             `json:"token_count"`
	RawResponse       string          `json:"raw_response"`
	Action            string          `json:"action"`
	Summary           string          `json:"summary"`
	ToolPath          string          `json:"tool_path,omitempty"`
	ToolInput         string          `json:"tool_input,omitempty"`
	ToolResult        string          `json:"tool_result,omitempty"`
	ToolError         string          `json:"tool_error,omitempty"`
	ToolDuration      time.Duration   `json:"tool_duration,omitempty"`
	RequestTokens     int             `json:"request_tokens"`
	ResponseTokens    int             `json:"response_tokens"`
	InputTokens       int             `json:"input_tokens,omitempty"`
	OutputTokens      int             `json:"output_tokens,omitempty"`
	CachedInputTokens int             `json:"cached_input_tokens,omitempty"`
	// CacheCreationInputTokens counts prompt-cache write tokens for the step
	// (Story 74.1). omitempty → old rows/files decode as 0 (NFR5).
	CacheCreationInputTokens int              `json:"cache_creation_input_tokens,omitempty"`
	ToolCalls                []ToolCallRecord `json:"tool_calls,omitempty"`
}

// ToolCallRecord 承载一次工具调用的完整 I/O 记录。
//
// 一个 reasonStep 中 LLM 若发起 N 个 parallel calls,该 step 的 StepRecord.ToolCalls
// 将含 N 个元素,而非 N 行 steps.jsonl 记录。
type ToolCallRecord struct {
	ID         string  `json:"id,omitempty"`         // LLM-issued tool_use_id
	Name       string  `json:"name"`                 // 工具名 (tc.Name)
	Path       string  `json:"path,omitempty"`       // VFS 路径或映射后的工具路径
	Input      string  `json:"input,omitempty"`      // 原始 JSON input
	Result     string  `json:"result,omitempty"`     // 工具执行结果
	Error      string  `json:"error,omitempty"`      // 错误消息 (非空表示失败)
	ErrorCode  string  `json:"error_code,omitempty"` // 错误码 (源自 DriverError.Code, 用于指纹去重)
	DurationMs float64 `json:"duration_ms,omitempty"`
}
