package debug

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

func makeCtxMessage(role, content string) CtxMessage {
	return CtxMessage{Role: role, Content: content}
}

func makeToolMessage(content, toolCallID string) CtxMessage {
	return CtxMessage{Role: "tool", Content: content, ToolCallID: toolCallID}
}

func TestAnalyzeContext_Empty(t *testing.T) {
	data := &ContextData{}
	result := AnalyzeContext(data, 1, 1, 0, 0)

	if result.TotalTokens != 0 {
		t.Fatalf("expected TotalTokens=0, got %d", result.TotalTokens)
	}
	if result.Classification.Active.Messages != 0 {
		t.Fatalf("expected Active.Messages=0, got %d", result.Classification.Active.Messages)
	}
	if len(result.TopConsumers) != 0 {
		t.Fatalf("expected no consumers, got %d", len(result.TopConsumers))
	}
	if len(result.Suggestions) != 0 {
		t.Fatalf("expected no suggestions, got %d", len(result.Suggestions))
	}
}

func TestAnalyzeContext_Nil(t *testing.T) {
	result := AnalyzeContext(nil, 1, 1, 0, 0)
	if result.TotalTokens != 0 {
		t.Fatalf("expected TotalTokens=0, got %d", result.TotalTokens)
	}
}

func TestAnalyzeContext_SystemPromptOnly(t *testing.T) {
	data := &ContextData{
		SystemPrompt: strings.Repeat("x", 400), // 100 tokens
	}
	result := AnalyzeContext(data, 1, 1, 100, 0)

	if result.TotalTokens != 100 {
		t.Fatalf("expected TotalTokens=100, got %d", result.TotalTokens)
	}
	if result.Classification.Active.Tokens != 100 {
		t.Fatalf("expected Active.Tokens=100, got %d", result.Classification.Active.Tokens)
	}
	if result.Classification.Active.Messages != 1 {
		t.Fatalf("expected Active.Messages=1 (system prompt), got %d", result.Classification.Active.Messages)
	}
	if result.Classification.Cold.Tokens != 0 {
		t.Fatalf("expected Cold.Tokens=0, got %d", result.Classification.Cold.Tokens)
	}
}

func TestAnalyzeContext_Classification_10Messages(t *testing.T) {
	data := &ContextData{
		SystemPrompt: strings.Repeat("s", 40), // 10 tokens
	}
	for range 10 {
		data.Messages = append(data.Messages, makeCtxMessage("user", strings.Repeat("m", 40))) // 10 tok each
	}

	result := AnalyzeContext(data, 1, 1, 0, 0)

	// 10 messages: Active=last 4 (idx 6-9), Warm=next 6 (idx 0-5), Cold=none
	if result.Classification.Active.Messages != 5 { // 4 msgs + 1 system prompt
		t.Fatalf("expected Active.Messages=5, got %d", result.Classification.Active.Messages)
	}
	if result.Classification.Active.Tokens != 50 { // 4*10 + 10 system
		t.Fatalf("expected Active.Tokens=50, got %d", result.Classification.Active.Tokens)
	}
	if result.Classification.Warm.Messages != 6 {
		t.Fatalf("expected Warm.Messages=6, got %d", result.Classification.Warm.Messages)
	}
	if result.Classification.Warm.Tokens != 60 {
		t.Fatalf("expected Warm.Tokens=60, got %d", result.Classification.Warm.Tokens)
	}
	if result.Classification.Cold.Messages != 0 {
		t.Fatalf("expected Cold.Messages=0, got %d", result.Classification.Cold.Messages)
	}
}

func TestAnalyzeContext_Classification_20Messages(t *testing.T) {
	data := &ContextData{
		SystemPrompt: strings.Repeat("s", 40), // 10 tokens
	}
	for range 20 {
		data.Messages = append(data.Messages, makeCtxMessage("user", strings.Repeat("m", 40)))
	}

	result := AnalyzeContext(data, 1, 1, 0, 0)

	// Active=last 4 (idx 16-19), Warm=6 (idx 10-15), Cold=10 (idx 0-9)
	if result.Classification.Active.Messages != 5 { // 4 + system
		t.Fatalf("expected Active.Messages=5, got %d", result.Classification.Active.Messages)
	}
	if result.Classification.Warm.Messages != 6 {
		t.Fatalf("expected Warm.Messages=6, got %d", result.Classification.Warm.Messages)
	}
	if result.Classification.Cold.Messages != 10 {
		t.Fatalf("expected Cold.Messages=10, got %d", result.Classification.Cold.Messages)
	}
}

func TestAnalyzeContext_LeakedToolResults(t *testing.T) {
	data := &ContextData{
		SystemPrompt: strings.Repeat("s", 40),
	}
	// 20 messages: first 10 cold zone, next 6 warm, last 4 active
	for i := range 20 {
		if i < 5 {
			// Big tool results in cold zone → leaked
			data.Messages = append(data.Messages, makeToolMessage(strings.Repeat("t", 2000), "toolu_big"))
		} else {
			data.Messages = append(data.Messages, makeCtxMessage("user", strings.Repeat("m", 40)))
		}
	}

	result := AnalyzeContext(data, 1, 1, 0, 0)

	if result.Classification.Leaked.Messages != 5 {
		t.Fatalf("expected Leaked.Messages=5, got %d", result.Classification.Leaked.Messages)
	}
	if result.Classification.Leaked.Tokens != 2500 { // 5 * 2000/4
		t.Fatalf("expected Leaked.Tokens=2500, got %d", result.Classification.Leaked.Tokens)
	}
}

func TestAnalyzeContext_LeakedNotInWarmOrActive(t *testing.T) {
	data := &ContextData{}
	// 4 messages total — all active, no cold zone
	for range 4 {
		data.Messages = append(data.Messages, makeToolMessage(strings.Repeat("t", 2000), "toolu_big"))
	}

	result := AnalyzeContext(data, 1, 1, 0, 0)

	if result.Classification.Leaked.Messages != 0 {
		t.Fatalf("expected Leaked.Messages=0 (all in active zone), got %d", result.Classification.Leaked.Messages)
	}
}

func TestFindTopConsumers_Ranking(t *testing.T) {
	data := &ContextData{
		SystemPrompt: strings.Repeat("s", 400), // 100 tok
		Messages: []CtxMessage{
			makeCtxMessage("user", strings.Repeat("u", 200)),         // 50 tok
			makeCtxMessage("assistant", strings.Repeat("a", 800)),    // 200 tok
			makeToolMessage(strings.Repeat("t", 1200), "toolu_read"), // 300 tok, contains "read_file"
		},
	}
	data.Messages[2].Content = "read_file result: " + strings.Repeat("x", 1182)

	total := 100 + 50 + 200 + 300
	consumers := findTopConsumers(data, total, 5)

	if len(consumers) != 4 {
		t.Fatalf("expected 4 consumers, got %d", len(consumers))
	}
	if consumers[0].Kind != "tool:read_file" {
		t.Fatalf("expected #1 to be tool:read_file, got %s", consumers[0].Kind)
	}
	if consumers[0].Rank != 1 {
		t.Fatalf("expected Rank=1, got %d", consumers[0].Rank)
	}
	if consumers[1].Kind != "assistant" {
		t.Fatalf("expected #2 to be assistant, got %s", consumers[1].Kind)
	}
	if consumers[2].Kind != "system_prompt" {
		t.Fatalf("expected #3 to be system_prompt, got %s", consumers[2].Kind)
	}
}

func TestFindTopConsumers_ToolNameExtraction(t *testing.T) {
	tests := []struct {
		name     string
		msg      CtxMessage
		wantKind string
	}{
		{
			name:     "known tool in content",
			msg:      CtxMessage{Role: "tool", Content: "list_dir output: foo bar"},
			wantKind: "tool:list_dir",
		},
		{
			name:     "fallback to tool call ID",
			msg:      CtxMessage{Role: "tool", Content: "some result", ToolCallID: "toolu_01ABC123XYZ456"},
			wantKind: "tool:toolu_01ABC1",
		},
		{
			name:     "unknown tool",
			msg:      CtxMessage{Role: "tool", Content: "some result"},
			wantKind: "tool:unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &ContextData{Messages: []CtxMessage{tt.msg}}
			consumers := findTopConsumers(data, 100, 5)
			if len(consumers) == 0 {
				t.Fatal("expected at least 1 consumer")
			}
			if consumers[0].Kind != tt.wantKind {
				t.Fatalf("expected kind=%q, got %q", tt.wantKind, consumers[0].Kind)
			}
		})
	}
}

func TestGenerateSuggestions_SystemPromptHigh(t *testing.T) {
	result := &CtxProfileResult{
		TotalTokens: 100,
		TopConsumers: []ConsumerEntry{
			{Kind: "system_prompt", Tokens: 30, Pct: 30},
		},
		Classification: ClassificationResult{},
	}
	suggestions := generateSuggestions(result)
	found := false
	for _, s := range suggestions {
		if strings.Contains(s, "System prompt") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected system prompt suggestion")
	}
}

func TestGenerateSuggestions_ToolDominant(t *testing.T) {
	result := &CtxProfileResult{
		TotalTokens: 100,
		TopConsumers: []ConsumerEntry{
			{Kind: "tool:read_file", Tokens: 35, Pct: 35},
			{Kind: "tool:list_dir", Tokens: 20, Pct: 20},
		},
		Classification: ClassificationResult{},
	}
	suggestions := generateSuggestions(result)
	found := false
	for _, s := range suggestions {
		if strings.Contains(s, "Tool results dominate") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected tool dominance suggestion")
	}
}

func TestGenerateSuggestions_Leaked(t *testing.T) {
	result := &CtxProfileResult{
		TotalTokens: 100,
		Classification: ClassificationResult{
			Leaked: ClassBucket{Messages: 2, Tokens: 50},
		},
	}
	suggestions := generateSuggestions(result)
	found := false
	for _, s := range suggestions {
		if strings.Contains(s, "leaked tool result") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected leaked suggestion")
	}
}

func TestGenerateSuggestions_ColdHigh(t *testing.T) {
	result := &CtxProfileResult{
		TotalTokens: 100,
		Classification: ClassificationResult{
			Cold: ClassBucket{Tokens: 45, Pct: 45},
		},
	}
	suggestions := generateSuggestions(result)
	found := false
	for _, s := range suggestions {
		if strings.Contains(s, "cold") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected cold context suggestion")
	}
}

func TestGenerateSuggestions_NearBudget(t *testing.T) {
	result := &CtxProfileResult{
		TotalTokens:    85,
		ContextBudget:  100,
		Classification: ClassificationResult{},
	}
	suggestions := generateSuggestions(result)
	found := false
	for _, s := range suggestions {
		if strings.Contains(s, "budget limit") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected near budget suggestion")
	}
}

func TestGenerateSuggestions_NoSuggestions(t *testing.T) {
	result := &CtxProfileResult{
		TotalTokens: 100,
		TopConsumers: []ConsumerEntry{
			{Kind: "user", Tokens: 40, Pct: 40},
			{Kind: "assistant", Tokens: 60, Pct: 60},
		},
		Classification: ClassificationResult{
			Active: ClassBucket{Tokens: 70, Pct: 70},
			Warm:   ClassBucket{Tokens: 30, Pct: 30},
		},
	}
	suggestions := generateSuggestions(result)
	if len(suggestions) != 0 {
		t.Fatalf("expected no suggestions, got %d: %v", len(suggestions), suggestions)
	}
}

func TestFormatCtxProfile_WithSuggestions(t *testing.T) {
	result := &CtxProfileResult{
		PID:           1,
		CtxID:         1,
		TotalTokens:   1200,
		ContextBudget: 8000,
		Classification: ClassificationResult{
			Active: ClassBucket{Tokens: 400, Messages: 4, Pct: 33.3},
			Warm:   ClassBucket{Tokens: 200, Messages: 6, Pct: 16.7},
			Cold:   ClassBucket{Tokens: 400, Messages: 12, Pct: 33.3},
			Leaked: ClassBucket{Tokens: 200, Messages: 2, Pct: 16.7},
		},
		TopConsumers: []ConsumerEntry{
			{Kind: "system_prompt", Tokens: 350, Pct: 29.2, Rank: 1},
			{Kind: "tool:read_file", Tokens: 280, Pct: 23.3, Rank: 2},
		},
		Suggestions: []string{"System prompt uses >25% of context; consider trimming"},
	}

	output := FormatCtxProfile(result)

	for _, want := range []string{
		"Ctx Profile: PID 1",
		"Classification",
		"Active (活跃)",
		"Warm (温)",
		"Cold (冷)",
		"Leaked (泄漏)",
		"Top Consumers",
		"#1",
		"#2",
		"system_prompt",
		"tool:read_file",
		"Suggestions",
		"System prompt",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestFormatCtxProfile_NoSuggestions(t *testing.T) {
	result := &CtxProfileResult{
		PID:         1,
		CtxID:       1,
		TotalTokens: 100,
		Classification: ClassificationResult{
			Active: ClassBucket{Tokens: 100, Messages: 2, Pct: 100},
		},
	}

	output := FormatCtxProfile(result)
	if strings.Contains(output, "Suggestions") {
		t.Error("output should not contain Suggestions section when empty")
	}
}

func TestFormatCtxProfile_NoBudget(t *testing.T) {
	result := &CtxProfileResult{
		PID:         1,
		CtxID:       1,
		TotalTokens: 100,
	}
	output := FormatCtxProfile(result)
	if !strings.Contains(output, "no limit") {
		t.Error("expected 'no limit' when ContextBudget=0")
	}
}

func TestCtxProfileResult_MarshalJSON(t *testing.T) {
	result := &CtxProfileResult{
		PID:           1,
		CtxID:         2,
		TokensUsed:    500,
		ContextBudget: 8000,
		TotalTokens:   600,
		Classification: ClassificationResult{
			Active: ClassBucket{Tokens: 200, Messages: 3, Pct: 33.3},
			Warm:   ClassBucket{Tokens: 100, Messages: 2, Pct: 16.7},
			Cold:   ClassBucket{Tokens: 200, Messages: 5, Pct: 33.3},
			Leaked: ClassBucket{Tokens: 100, Messages: 1, Pct: 16.7},
		},
		TopConsumers: []ConsumerEntry{
			{Kind: "system_prompt", Tokens: 200, Pct: 33.3, Rank: 1},
		},
		Suggestions: []string{"test suggestion"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// snake_case verification
	for _, key := range []string{"pid", "ctx_id", "tokens_used", "context_budget", "total_tokens", "classification", "top_consumers", "suggestions"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON", key)
		}
	}

	// Check classification sub-fields
	cls, ok := m["classification"].(map[string]any)
	if !ok {
		t.Fatal("classification is not an object")
	}
	for _, key := range []string{"active", "warm", "cold", "leaked"} {
		bucket, ok := cls[key].(map[string]any)
		if !ok {
			t.Errorf("classification.%s missing or not object", key)
			continue
		}
		for _, bkey := range []string{"tokens", "messages", "pct"} {
			if _, ok := bucket[bkey]; !ok {
				t.Errorf("classification.%s.%s missing", key, bkey)
			}
		}
	}

	// Verify pct is one decimal
	active := cls["active"].(map[string]any)
	pct := active["pct"].(float64)
	if pct != 33.3 {
		t.Errorf("expected pct=33.3, got %v", pct)
	}

	// Verify suggestions is array
	sug, ok := m["suggestions"].([]any)
	if !ok || len(sug) != 1 {
		t.Errorf("expected suggestions array with 1 element, got %v", m["suggestions"])
	}
}

func TestCtxProfileResult_MarshalJSON_EmptyArrays(t *testing.T) {
	result := &CtxProfileResult{PID: 1, CtxID: 1}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Verify empty arrays are [] not null
	consumers, ok := m["top_consumers"].([]any)
	if !ok {
		t.Fatal("top_consumers should be array, not null")
	}
	if len(consumers) != 0 {
		t.Errorf("expected empty consumers array, got %d elements", len(consumers))
	}

	suggestions, ok := m["suggestions"].([]any)
	if !ok {
		t.Fatal("suggestions should be array, not null")
	}
	if len(suggestions) != 0 {
		t.Errorf("expected empty suggestions array, got %d elements", len(suggestions))
	}
}

func TestAnalyzeContext_PIDAndCtxID(t *testing.T) {
	data := &ContextData{SystemPrompt: "hello"}
	result := AnalyzeContext(data, types.PID(42), types.CtxID(7), 500, 8000)
	if result.PID != 42 {
		t.Fatalf("expected PID=42, got %d", result.PID)
	}
	if result.CtxID != 7 {
		t.Fatalf("expected CtxID=7, got %d", result.CtxID)
	}
	if result.TokensUsed != 500 {
		t.Fatalf("expected TokensUsed=500, got %d", result.TokensUsed)
	}
	if result.ContextBudget != 8000 {
		t.Fatalf("expected ContextBudget=8000, got %d", result.ContextBudget)
	}
}
