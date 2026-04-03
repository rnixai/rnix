package context

import (
	"strings"
	"testing"
)

func TestEstimateTokens_Empty(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("EstimateTokens(\"\") = %d, want 0", got)
	}
}

func TestEstimateTokens_PureEnglish(t *testing.T) {
	// "Hello, World!" is 13 ASCII chars → 13/3.5 ≈ 3.71 → ceil = 4
	got := EstimateTokens("Hello, World!")
	if got != 4 {
		t.Errorf("EstimateTokens(\"Hello, World!\") = %d, want 4", got)
	}
}

func TestEstimateTokens_PureChinese(t *testing.T) {
	// "你好世界" is 4 CJK runes → 4/1.5 ≈ 2.67 → ceil = 3
	got := EstimateTokens("你好世界")
	if got != 3 {
		t.Errorf("EstimateTokens(\"你好世界\") = %d, want 3", got)
	}
}

func TestEstimateTokens_MixedText(t *testing.T) {
	// "Hello 你好" = 6 ASCII chars + 2 CJK runes
	// 6/3.5 + 2/1.5 = 1.714 + 1.333 = 3.047 → ceil = 4
	got := EstimateTokens("Hello 你好")
	if got != 4 {
		t.Errorf("EstimateTokens(\"Hello 你好\") = %d, want 4", got)
	}
}

func TestEstimateTokens_CodeSnippet(t *testing.T) {
	code := `func main() {
	fmt.Println("Hello, World!")
	for i := range 10 {
		fmt.Println(i)
	}
}`
	got := EstimateTokens(code)
	// ~85 ASCII bytes → ~25 tokens
	if got < 15 || got > 35 {
		t.Errorf("EstimateTokens(code snippet) = %d, expected 15-35", got)
	}
}

func TestEstimateTokens_LongText(t *testing.T) {
	// 3500 ASCII chars should give ~1000 tokens
	text := strings.Repeat("a", 3500)
	got := EstimateTokens(text)
	if got != 1000 {
		t.Errorf("EstimateTokens(3500 'a') = %d, want 1000", got)
	}
}

func TestTokenUsage(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}

	err = mgr.SetSystemPrompt(cid, "You are an assistant.")
	if err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}

	err = mgr.AppendMessage(cid, RoleUser, "Hello, World!")
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	stats, err := mgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}

	if stats.Used <= 0 {
		t.Errorf("TokenUsage.Used = %d, want > 0", stats.Used)
	}
	if stats.Limit != DefaultTokenLimit {
		t.Errorf("TokenUsage.Limit = %d, want %d", stats.Limit, DefaultTokenLimit)
	}
	if stats.Percentage <= 0 {
		t.Errorf("TokenUsage.Percentage = %f, want > 0", stats.Percentage)
	}
}

func TestTokenUsage_NotFound(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.TokenUsage(999)
	if err == nil {
		t.Fatal("expected error for non-existent context")
	}
}
