package debug

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rnixai/rnix/internal/types"
)

const (
	minActiveWindow = 4
	minWarmWindow   = 6
	leakedThreshold = 1000
	topConsumersN   = 5
)

func activeWindowSize(n int) int {
	adaptive := n / 5 // 20% of messages
	if adaptive > minActiveWindow {
		return adaptive
	}
	return minActiveWindow
}

func warmWindowSize(n int) int {
	adaptive := n * 3 / 10 // 30% of messages
	if adaptive > minWarmWindow {
		return adaptive
	}
	return minWarmWindow
}

// CtxProfileResult holds the analysis results for a process context.
type CtxProfileResult struct {
	PID            types.PID            `json:"pid"`
	CtxID          types.CtxID          `json:"ctx_id"`
	TokensUsed     int                  `json:"tokens_used"`
	ContextBudget  int                  `json:"context_budget"`
	TotalTokens    int                  `json:"total_tokens"`
	Classification ClassificationResult `json:"classification"`
	TopConsumers   []ConsumerEntry      `json:"top_consumers"`
	Suggestions    []string             `json:"suggestions"`
}

// ClassificationResult breaks context into four temperature categories.
type ClassificationResult struct {
	Active ClassBucket `json:"active"`
	Warm   ClassBucket `json:"warm"`
	Cold   ClassBucket `json:"cold"`
	Leaked ClassBucket `json:"leaked"`
}

// ClassBucket holds token/message stats for one classification category.
type ClassBucket struct {
	Tokens   int     `json:"tokens"`
	Messages int     `json:"messages"`
	Pct      float64 `json:"pct"`
}

// ConsumerEntry represents one token consumer with ranking and optional suggestion.
type ConsumerEntry struct {
	Kind       string  `json:"kind"`
	Tokens     int     `json:"tokens"`
	Pct        float64 `json:"pct"`
	Rank       int     `json:"rank"`
	Suggestion string  `json:"suggestion,omitempty"`
}

// ContextData is the input structure for the analysis engine, parsed from CtxRead output.
type ContextData struct {
	SystemPrompt string       `json:"system_prompt"`
	Messages     []CtxMessage `json:"messages"`
}

// CtxMessage represents a single context message (mirrors context.Message without importing it).
type CtxMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// AnalyzeContext runs the context profiling analysis.
// pid, ctxID, tokensUsed, contextBudget are passed in from ProcInfo to avoid
// the debug package depending on vfs.
func AnalyzeContext(data *ContextData, pid types.PID, ctxID types.CtxID, tokensUsed, contextBudget int) *CtxProfileResult {
	result := &CtxProfileResult{
		PID:           pid,
		CtxID:         ctxID,
		TokensUsed:    tokensUsed,
		ContextBudget: contextBudget,
	}

	if data == nil {
		return result
	}

	sysTokens := estimateTokens(data.SystemPrompt)
	totalTokens := sysTokens
	for _, msg := range data.Messages {
		totalTokens += estimateTokens(msg.Content)
	}
	result.TotalTokens = totalTokens

	result.Classification = classifyMessages(data, sysTokens, totalTokens)
	result.TopConsumers = findTopConsumers(data, totalTokens, topConsumersN)
	result.Suggestions = generateSuggestions(result)

	return result
}

func estimateTokens(s string) int {
	return len(s) / 4
}

func roundPct(value float64) float64 {
	return math.Round(value*10) / 10
}

func classifyMessages(data *ContextData, sysTokens, totalTokens int) ClassificationResult {
	n := len(data.Messages)
	var result ClassificationResult

	activeStart := max(0, n-activeWindowSize(n))
	warmStart := max(0, activeStart-warmWindowSize(n))

	// Active: system prompt + last activeWindowSize messages
	activeTokens := sysTokens
	activeMsgs := 0
	for i := activeStart; i < n; i++ {
		activeTokens += estimateTokens(data.Messages[i].Content)
		activeMsgs++
	}
	// System prompt always counted in active messages
	if data.SystemPrompt != "" {
		activeMsgs++
	}
	result.Active = ClassBucket{Tokens: activeTokens, Messages: activeMsgs}

	// Warm: messages in [warmStart, activeStart)
	warmTokens := 0
	warmMsgs := 0
	for i := warmStart; i < activeStart; i++ {
		warmTokens += estimateTokens(data.Messages[i].Content)
		warmMsgs++
	}
	result.Warm = ClassBucket{Tokens: warmTokens, Messages: warmMsgs}

	// Cold: messages in [0, warmStart), excluding leaked
	// Leaked: tool results with len(content) > leakedThreshold in cold zone
	coldTokens := 0
	coldMsgs := 0
	leakedTokens := 0
	leakedMsgs := 0
	for i := 0; i < warmStart; i++ {
		msg := data.Messages[i]
		tok := estimateTokens(msg.Content)
		if msg.Role == "tool" && len(msg.Content) > leakedThreshold {
			leakedTokens += tok
			leakedMsgs++
		} else {
			coldTokens += tok
			coldMsgs++
		}
	}
	result.Cold = ClassBucket{Tokens: coldTokens, Messages: coldMsgs}
	result.Leaked = ClassBucket{Tokens: leakedTokens, Messages: leakedMsgs}

	// Calculate percentages
	if totalTokens > 0 {
		result.Active.Pct = roundPct(float64(result.Active.Tokens) / float64(totalTokens) * 100)
		result.Warm.Pct = roundPct(float64(result.Warm.Tokens) / float64(totalTokens) * 100)
		result.Cold.Pct = roundPct(float64(result.Cold.Tokens) / float64(totalTokens) * 100)
		result.Leaked.Pct = roundPct(float64(result.Leaked.Tokens) / float64(totalTokens) * 100)
	}

	return result
}

func findTopConsumers(data *ContextData, totalTokens, topN int) []ConsumerEntry {
	type consumer struct {
		kind   string
		tokens int
	}

	var consumers []consumer

	if data.SystemPrompt != "" {
		consumers = append(consumers, consumer{kind: "system_prompt", tokens: estimateTokens(data.SystemPrompt)})
	}

	userTokens := 0
	assistantTokens := 0
	toolMap := make(map[string]int)

	for _, msg := range data.Messages {
		tok := estimateTokens(msg.Content)
		switch msg.Role {
		case "user":
			userTokens += tok
		case "assistant":
			assistantTokens += tok
		case "tool":
			name := extractToolName(msg)
			toolMap[name] += tok
		}
	}

	if userTokens > 0 {
		consumers = append(consumers, consumer{kind: "user", tokens: userTokens})
	}
	if assistantTokens > 0 {
		consumers = append(consumers, consumer{kind: "assistant", tokens: assistantTokens})
	}
	for name, tok := range toolMap {
		consumers = append(consumers, consumer{kind: "tool:" + name, tokens: tok})
	}

	sort.Slice(consumers, func(i, j int) bool {
		return consumers[i].tokens > consumers[j].tokens
	})

	if len(consumers) > topN {
		consumers = consumers[:topN]
	}

	entries := make([]ConsumerEntry, len(consumers))
	for i, c := range consumers {
		pct := float64(0)
		if totalTokens > 0 {
			pct = roundPct(float64(c.tokens) / float64(totalTokens) * 100)
		}
		entries[i] = ConsumerEntry{
			Kind:   c.kind,
			Tokens: c.tokens,
			Pct:    pct,
			Rank:   i + 1,
		}
	}
	return entries
}

func extractToolName(msg CtxMessage) string {
	knownTools := []string{
		"read_file", "write_file", "list_files", "list_dir",
		"search_repo", "grep", "edit_file", "create_file",
		"run_command", "execute",
	}
	lower := strings.ToLower(msg.Content)
	for _, tool := range knownTools {
		if strings.Contains(lower, tool) {
			return tool
		}
	}

	if msg.ToolCallID != "" {
		id := msg.ToolCallID
		if len(id) > 12 {
			id = id[:12]
		}
		return id
	}

	return "unknown"
}

func generateSuggestions(result *CtxProfileResult) []string {
	var suggestions []string

	if result.TotalTokens == 0 {
		return suggestions
	}

	// System prompt > 25%
	for _, c := range result.TopConsumers {
		if c.Kind == "system_prompt" && c.Pct > 25 {
			suggestions = append(suggestions, "System prompt uses >25% of context; consider trimming")
			break
		}
	}

	// Tool results > 50%
	toolPct := float64(0)
	for _, c := range result.TopConsumers {
		if strings.HasPrefix(c.Kind, "tool:") {
			toolPct += c.Pct
		}
	}
	if toolPct > 50 {
		suggestions = append(suggestions, "Tool results dominate context; consider more concise tool outputs")
	}

	// Leaked tokens
	if result.Classification.Leaked.Messages > 0 {
		suggestions = append(suggestions, fmt.Sprintf(
			"Found %d leaked tool result(s) using ~%d tokens; consider pruning unused tool outputs",
			result.Classification.Leaked.Messages, result.Classification.Leaked.Tokens))
	}

	// Cold > 40%
	if result.Classification.Cold.Pct > 40 {
		suggestions = append(suggestions, fmt.Sprintf(
			"%.0f%% of context is cold; consider context compaction", result.Classification.Cold.Pct))
	}

	// Near budget
	if result.ContextBudget > 0 {
		usagePct := float64(result.TotalTokens) / float64(result.ContextBudget) * 100
		if usagePct > 80 {
			suggestions = append(suggestions, "Context is near budget limit; optimization recommended")
		}
	}

	return suggestions
}

// FormatCtxProfile formats a CtxProfileResult as human-readable text.
func FormatCtxProfile(result *CtxProfileResult) string {
	var sb strings.Builder

	budgetStr := "no limit"
	if result.ContextBudget > 0 {
		budgetStr = fmt.Sprintf("%d budget", result.ContextBudget)
	}
	fmt.Fprintf(&sb, "Ctx Profile: PID %d  |  CtxID %d  |  ~%d tok / %s\n",
		result.PID, result.CtxID, result.TotalTokens, budgetStr)

	// Classification
	sb.WriteString("\n── Classification ─────────────────────────────────────\n")
	formatBucket(&sb, "Active (活跃)", result.Classification.Active, "当前推理引用")
	formatBucket(&sb, "Warm (温)", result.Classification.Warm, "近期使用")
	formatBucket(&sb, "Cold (冷)", result.Classification.Cold, "未引用")
	formatBucket(&sb, "Leaked (泄漏)", result.Classification.Leaked, "已无用未释放")

	// Top Consumers
	if len(result.TopConsumers) > 0 {
		sb.WriteString("\n── Top Consumers ──────────────────────────────────────\n")
		for _, c := range result.TopConsumers {
			line := fmt.Sprintf("#%-2d %-20s %5d tok  %5.1f%%", c.Rank, c.Kind, c.Tokens, c.Pct)
			if c.Suggestion != "" {
				line += "   ← " + c.Suggestion
			}
			sb.WriteString(line + "\n")
		}
	}

	// Suggestions
	if len(result.Suggestions) > 0 {
		sb.WriteString("\n── Suggestions ────────────────────────────────────────\n")
		for _, s := range result.Suggestions {
			fmt.Fprintf(&sb, "• %s\n", s)
		}
	}

	return sb.String()
}

func formatBucket(sb *strings.Builder, label string, b ClassBucket, desc string) {
	fmt.Fprintf(sb, "%-18s %5d tok  %5.1f%%  %2d msgs   ← %s\n",
		label, b.Tokens, b.Pct, b.Messages, desc)
}

// MarshalJSON implements custom JSON serialization with snake_case fields
// and one-decimal percentages.
func (r *CtxProfileResult) MarshalJSON() ([]byte, error) {
	type bucketJSON struct {
		Tokens   int     `json:"tokens"`
		Messages int     `json:"messages"`
		Pct      float64 `json:"pct"`
	}
	type classJSON struct {
		Active bucketJSON `json:"active"`
		Warm   bucketJSON `json:"warm"`
		Cold   bucketJSON `json:"cold"`
		Leaked bucketJSON `json:"leaked"`
	}
	type consumerJSON struct {
		Kind       string  `json:"kind"`
		Tokens     int     `json:"tokens"`
		Pct        float64 `json:"pct"`
		Rank       int     `json:"rank"`
		Suggestion string  `json:"suggestion,omitempty"`
	}
	type resultJSON struct {
		PID            types.PID      `json:"pid"`
		CtxID          types.CtxID    `json:"ctx_id"`
		TokensUsed     int            `json:"tokens_used"`
		ContextBudget  int            `json:"context_budget"`
		TotalTokens    int            `json:"total_tokens"`
		Classification classJSON      `json:"classification"`
		TopConsumers   []consumerJSON `json:"top_consumers"`
		Suggestions    []string       `json:"suggestions"`
	}

	toBucket := func(b ClassBucket) bucketJSON {
		return bucketJSON(b)
	}

	consumers := make([]consumerJSON, len(r.TopConsumers))
	for i, c := range r.TopConsumers {
		consumers[i] = consumerJSON(c)
	}

	suggestions := r.Suggestions
	if suggestions == nil {
		suggestions = []string{}
	}
	out := resultJSON{
		PID:           r.PID,
		CtxID:         r.CtxID,
		TokensUsed:    r.TokensUsed,
		ContextBudget: r.ContextBudget,
		TotalTokens:   r.TotalTokens,
		Classification: classJSON{
			Active: toBucket(r.Classification.Active),
			Warm:   toBucket(r.Classification.Warm),
			Cold:   toBucket(r.Classification.Cold),
			Leaked: toBucket(r.Classification.Leaked),
		},
		TopConsumers: consumers,
		Suggestions:  suggestions,
	}
	return json.Marshal(out)
}
