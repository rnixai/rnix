package context

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCompact_Basic(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatal(err)
	}

	// Populate context with messages
	_ = mgr.AppendMessage(cid, RoleUser, "Hello, please help me with a task")
	_ = mgr.AppendMessage(cid, RoleAssistant, "Sure, I can help you with that.")
	_ = mgr.AppendMessage(cid, RoleUser, "Please read file foo.go")
	_ = mgr.AppendMessage(cid, RoleAssistant, "Here is the content of foo.go...")

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		if sysPrompt != compactSystemPrompt {
			t.Errorf("unexpected system prompt: %q", sysPrompt)
		}
		return "<analysis>thinking...</analysis><summary>This is the summary of the conversation.</summary>", nil
	}

	result, err := mgr.Compact(cid, CompactOpts{
		LLMCall: mockLLMCall,
		Trigger: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.PreTokens == 0 {
		t.Error("PreTokens should be non-zero")
	}
	if result.PostTokens == 0 {
		t.Error("PostTokens should be non-zero")
	}

	// Verify messages were replaced
	ctx, _ := mgr.GetContext(cid)
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	if len(ctx.Messages) < 2 {
		t.Fatalf("expected at least 2 messages after compact, got %d", len(ctx.Messages))
	}

	// First message should be boundary
	if !strings.Contains(ctx.Messages[0].Content, "[compact_boundary:") {
		t.Errorf("first message should be boundary marker, got: %s", ctx.Messages[0].Content)
	}
	if !strings.Contains(ctx.Messages[0].Content, "trigger=auto") {
		t.Errorf("boundary should contain trigger=auto, got: %s", ctx.Messages[0].Content)
	}

	// Second message should be the summary
	if ctx.Messages[1].Content != "This is the summary of the conversation." {
		t.Errorf("second message should be summary, got: %s", ctx.Messages[1].Content)
	}
}

func TestStripAnalysis_WithBothTags(t *testing.T) {
	raw := "<analysis>thinking about stuff</analysis><summary>The actual summary</summary>"
	got := stripAnalysis(raw)
	if got != "The actual summary" {
		t.Errorf("stripAnalysis = %q, want %q", got, "The actual summary")
	}
}

func TestStripAnalysis_SummaryOnly(t *testing.T) {
	raw := "<summary>Just the summary</summary>"
	got := stripAnalysis(raw)
	if got != "Just the summary" {
		t.Errorf("stripAnalysis = %q, want %q", got, "Just the summary")
	}
}

func TestStripAnalysis_NoTags(t *testing.T) {
	raw := "Plain text without any tags"
	got := stripAnalysis(raw)
	if got != raw {
		t.Errorf("stripAnalysis = %q, want %q", got, raw)
	}
}

func TestStripAnalysis_AnalysisWithoutSummary(t *testing.T) {
	raw := "before <analysis>draft</analysis> after"
	got := stripAnalysis(raw)
	if got != "before  after" {
		t.Errorf("stripAnalysis = %q, want %q", got, "before  after")
	}
}

func TestStripAnalysis_SummaryNoClosingTag(t *testing.T) {
	raw := "<analysis>draft</analysis><summary>open ended summary"
	got := stripAnalysis(raw)
	if got != "open ended summary" {
		t.Errorf("stripAnalysis = %q, want %q", got, "open ended summary")
	}
}

func TestCompact_PTLRetry_Success(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "msg1")
	_ = mgr.AppendMessage(cid, RoleAssistant, "resp1")
	_ = mgr.AppendMessage(cid, RoleUser, "msg2")
	_ = mgr.AppendMessage(cid, RoleAssistant, "resp2")

	callCount := 0
	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		callCount++
		if callCount == 1 {
			return "", fmt.Errorf("prompt too long")
		}
		return "<summary>Compact summary</summary>", nil
	}

	result, err := mgr.Compact(cid, CompactOpts{
		LLMCall: mockLLMCall,
		Trigger: "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", callCount)
	}
	if result.PostTokens == 0 {
		t.Error("PostTokens should be non-zero")
	}
}

func TestCompact_PTLRetry_Exhausted(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "msg1")
	_ = mgr.AppendMessage(cid, RoleAssistant, "resp1")
	_ = mgr.AppendMessage(cid, RoleUser, "msg2")
	_ = mgr.AppendMessage(cid, RoleAssistant, "resp2")
	_ = mgr.AppendMessage(cid, RoleUser, "msg3")
	_ = mgr.AppendMessage(cid, RoleAssistant, "resp3")

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "", fmt.Errorf("prompt too long")
	}

	_, err := mgr.Compact(cid, CompactOpts{
		LLMCall: mockLLMCall,
	})
	if err == nil {
		t.Fatal("expected error after PTL retries exhausted")
	}
	if !strings.Contains(err.Error(), "prompt too long") {
		t.Errorf("error should mention 'prompt too long', got: %v", err)
	}
}

func TestCompact_FileRestore(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "read files")
	_ = mgr.AppendMessage(cid, RoleAssistant, "ok")

	now := time.Now()
	readFiles := map[string]ReadFileEntry{
		"a.go": {Content: "package a", Timestamp: now.Add(-2 * time.Second)},
		"b.go": {Content: "package b", Timestamp: now.Add(-1 * time.Second)},
		"c.go": {Content: "package c", Timestamp: now},
	}

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "<summary>Summary</summary>", nil
	}

	result, err := mgr.Compact(cid, CompactOpts{
		LLMCall:       mockLLMCall,
		ReadFileState: readFiles,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should restore 3 files
	fileCount := 0
	for _, item := range result.ItemsRestored {
		if item == "file" {
			fileCount++
		}
	}
	if fileCount != 3 {
		t.Errorf("expected 3 file restores, got %d", fileCount)
	}

	// Check messages contain file restore entries
	ctx, _ := mgr.GetContext(cid)
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	// Messages: boundary, summary, 3 files (ordered by timestamp desc: c.go, b.go, a.go)
	if len(ctx.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(ctx.Messages))
	}
	if !strings.Contains(ctx.Messages[2].Content, "c.go") {
		t.Errorf("third message should be c.go (most recent), got: %s", ctx.Messages[2].Content)
	}
}

func TestCompact_SkillRestore(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "use skills")
	_ = mgr.AppendMessage(cid, RoleAssistant, "ok")

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "<summary>Summary</summary>", nil
	}

	result, err := mgr.Compact(cid, CompactOpts{
		LLMCall: mockLLMCall,
		ActiveSkills: []SkillEntry{
			{Name: "code-analyst", Content: "Analyze code quality"},
			{Name: "test-writer", Content: "Write tests"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	skillCount := 0
	for _, item := range result.ItemsRestored {
		if item == "skill" {
			skillCount++
		}
	}
	if skillCount != 2 {
		t.Errorf("expected 2 skill restores, got %d", skillCount)
	}
}

func TestCompact_PlanRestore(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "make a plan")
	_ = mgr.AppendMessage(cid, RoleAssistant, "ok")

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "<summary>Summary</summary>", nil
	}

	result, err := mgr.Compact(cid, CompactOpts{
		LLMCall:    mockLLMCall,
		ActivePlan: "[Plan]\n1. Step one\n2. Step two",
	})
	if err != nil {
		t.Fatal(err)
	}

	planCount := 0
	for _, item := range result.ItemsRestored {
		if item == "plan" {
			planCount++
		}
	}
	if planCount != 1 {
		t.Errorf("expected 1 plan restore, got %d", planCount)
	}

	ctx, _ := mgr.GetContext(cid)
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	lastMsg := ctx.Messages[len(ctx.Messages)-1]
	if !strings.Contains(lastMsg.Content, "[Post-compact restore: plan]") {
		t.Errorf("last message should contain plan restore, got: %s", lastMsg.Content)
	}
}

func TestCompact_EmptyMessages(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "<summary>Summary</summary>", nil
	}

	_, err := mgr.Compact(cid, CompactOpts{LLMCall: mockLLMCall})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
	if !strings.Contains(err.Error(), "not enough messages") {
		t.Errorf("error should mention 'not enough messages', got: %v", err)
	}
}

func TestCompact_NilLLMCall(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "msg")
	_ = mgr.AppendMessage(cid, RoleAssistant, "resp")

	_, err := mgr.Compact(cid, CompactOpts{})
	if err == nil {
		t.Fatal("expected error for nil LLMCall")
	}
	if !strings.Contains(err.Error(), "LLMCall callback is required") {
		t.Errorf("error should mention LLMCall, got: %v", err)
	}
}

func TestGroupMessagesByAPIRound(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a1"},
		{Role: RoleTool, Content: "t1"},
		{Role: RoleUser, Content: "u2"},
		{Role: RoleAssistant, Content: "a2"},
	}

	groups := groupMessagesByAPIRound(messages)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// Group 0: [u1]
	if len(groups[0]) != 1 || groups[0][0].Content != "u1" {
		t.Errorf("group 0 should be [u1], got %v", groups[0])
	}
	// Group 1: [a1, t1, u2]
	if len(groups[1]) != 3 {
		t.Errorf("group 1 should have 3 messages, got %d", len(groups[1]))
	}
	// Group 2: [a2]
	if len(groups[2]) != 1 || groups[2][0].Content != "a2" {
		t.Errorf("group 2 should be [a2], got %v", groups[2])
	}
}

func TestCompact_FileRestore_TokenBudget(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "read files")
	_ = mgr.AppendMessage(cid, RoleAssistant, "ok")

	// Create a file with content exceeding per-file token limit
	largeContent := strings.Repeat("x", 20000) // ~5714 tokens at 3.5 chars/token
	readFiles := map[string]ReadFileEntry{
		"large.go": {Content: largeContent, Timestamp: time.Now()},
	}

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "<summary>Summary</summary>", nil
	}

	_, err := mgr.Compact(cid, CompactOpts{
		LLMCall:       mockLLMCall,
		ReadFileState: readFiles,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := mgr.GetContext(cid)
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	// The file should be restored but truncated
	if len(ctx.Messages) < 3 {
		t.Fatalf("expected at least 3 messages (boundary, summary, file), got %d", len(ctx.Messages))
	}
	// Truncated content should be shorter than original
	fileMsg := ctx.Messages[2].Content
	if len(fileMsg) >= len(largeContent) {
		t.Error("file content should be truncated")
	}
}

func TestCompact_SkillRestore_Truncation(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "use skill")
	_ = mgr.AppendMessage(cid, RoleAssistant, "ok")

	// Large skill content that exceeds per-skill limit
	largeSkill := strings.Repeat("y", 20000)

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "<summary>Summary</summary>", nil
	}

	_, err := mgr.Compact(cid, CompactOpts{
		LLMCall: mockLLMCall,
		ActiveSkills: []SkillEntry{
			{Name: "big-skill", Content: largeSkill},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := mgr.GetContext(cid)
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	if len(ctx.Messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(ctx.Messages))
	}
	skillMsg := ctx.Messages[2].Content
	if !strings.Contains(skillMsg, "truncated for compaction") {
		t.Error("truncated skill should contain truncation marker")
	}
}

func TestCompact_MaxRestoreFiles(t *testing.T) {
	mgr := NewManager()
	cid, _ := mgr.CtxAlloc(100)
	_ = mgr.AppendMessage(cid, RoleUser, "read files")
	_ = mgr.AppendMessage(cid, RoleAssistant, "ok")

	now := time.Now()
	readFiles := make(map[string]ReadFileEntry)
	for i := range 8 {
		readFiles[fmt.Sprintf("file%d.go", i)] = ReadFileEntry{
			Content:   fmt.Sprintf("package file%d", i),
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}
	}

	mockLLMCall := func(sysPrompt string, messages []Message) (string, error) {
		return "<summary>Summary</summary>", nil
	}

	result, err := mgr.Compact(cid, CompactOpts{
		LLMCall:       mockLLMCall,
		ReadFileState: readFiles,
	})
	if err != nil {
		t.Fatal(err)
	}

	fileCount := 0
	for _, item := range result.ItemsRestored {
		if item == "file" {
			fileCount++
		}
	}
	if fileCount > maxRestoreFiles {
		t.Errorf("should restore at most %d files, got %d", maxRestoreFiles, fileCount)
	}
}
