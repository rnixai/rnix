package context

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
)

func TestManager_CtxAlloc(t *testing.T) {
	m := NewManager()

	t.Run("returns incrementing CtxIDs starting from 1", func(t *testing.T) {
		id1, err := m.CtxAlloc(100)
		if err != nil {
			t.Fatalf("CtxAlloc failed: %v", err)
		}
		if id1 != 1 {
			t.Fatalf("expected CtxID 1, got %d", id1)
		}

		id2, err := m.CtxAlloc(100)
		if err != nil {
			t.Fatalf("CtxAlloc failed: %v", err)
		}
		if id2 != 2 {
			t.Fatalf("expected CtxID 2, got %d", id2)
		}

		id3, err := m.CtxAlloc(100)
		if err != nil {
			t.Fatalf("CtxAlloc failed: %v", err)
		}
		if id3 != 3 {
			t.Fatalf("expected CtxID 3, got %d", id3)
		}
	})

	t.Run("stores MaxSize correctly", func(t *testing.T) {
		m := NewManager()
		cid, err := m.CtxAlloc(42)
		if err != nil {
			t.Fatalf("CtxAlloc failed: %v", err)
		}
		ctx, err := m.getContext("test", cid)
		if err != nil {
			t.Fatalf("getContext failed: %v", err)
		}
		if ctx.MaxSize != 42 {
			t.Fatalf("expected MaxSize 42, got %d", ctx.MaxSize)
		}
	})

	t.Run("rejects invalid size", func(t *testing.T) {
		m := NewManager()
		_, err := m.CtxAlloc(0)
		if err == nil {
			t.Fatal("expected error for size 0")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrInternal {
			t.Fatalf("expected ErrInternal, got %s", ctxErr.Code)
		}

		_, err = m.CtxAlloc(-1)
		if err == nil {
			t.Fatal("expected error for negative size")
		}
	})
}

func TestManager_CtxAllocMultiple(t *testing.T) {
	m := NewManager()
	ids := make(map[types.CtxID]bool)

	for i := 0; i < 100; i++ {
		id, err := m.CtxAlloc(10)
		if err != nil {
			t.Fatalf("CtxAlloc iteration %d failed: %v", i, err)
		}
		if ids[id] {
			t.Fatalf("duplicate CtxID: %d at iteration %d", id, i)
		}
		ids[id] = true
	}

	if len(ids) != 100 {
		t.Fatalf("expected 100 unique IDs, got %d", len(ids))
	}
}

func TestManager_CtxFree(t *testing.T) {
	t.Run("free releases context", func(t *testing.T) {
		m := NewManager()
		cid, err := m.CtxAlloc(100)
		if err != nil {
			t.Fatalf("CtxAlloc failed: %v", err)
		}

		if err := m.CtxFree(cid); err != nil {
			t.Fatalf("CtxFree failed: %v", err)
		}

		// Subsequent Read should fail
		_, err = m.CtxRead(cid, 0, 0)
		if err == nil {
			t.Fatal("expected error after CtxFree for CtxRead")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", ctxErr.Code)
		}
	})

	t.Run("free then Write returns error", func(t *testing.T) {
		m := NewManager()
		cid, err := m.CtxAlloc(100)
		if err != nil {
			t.Fatalf("CtxAlloc failed: %v", err)
		}
		if err := m.CtxFree(cid); err != nil {
			t.Fatalf("CtxFree failed: %v", err)
		}

		data, _ := json.Marshal(Message{Role: RoleUser, Content: "test"})
		err = m.CtxWrite(cid, 0, data)
		if err == nil {
			t.Fatal("expected error after CtxFree for CtxWrite")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", ctxErr.Code)
		}
	})

	t.Run("free then BuildPrompt returns error", func(t *testing.T) {
		m := NewManager()
		cid, err := m.CtxAlloc(100)
		if err != nil {
			t.Fatalf("CtxAlloc failed: %v", err)
		}
		if err := m.CtxFree(cid); err != nil {
			t.Fatalf("CtxFree failed: %v", err)
		}

		_, err = m.BuildPrompt(cid)
		if err == nil {
			t.Fatal("expected error after CtxFree for BuildPrompt")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", ctxErr.Code)
		}
	})
}

func TestManager_CtxFreeNotFound(t *testing.T) {
	m := NewManager()
	err := m.CtxFree(999)
	if err == nil {
		t.Fatal("expected error for non-existent CtxID")
	}
	var ctxErr *ContextError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextError, got %T", err)
	}
	if ctxErr.Code != types.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %s", ctxErr.Code)
	}
}

func TestManager_CtxWriteRead(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc failed: %v", err)
	}

	t.Run("write and read back message", func(t *testing.T) {
		msg := Message{Role: RoleUser, Content: "hello"}
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		if err := m.CtxWrite(cid, 0, data); err != nil {
			t.Fatalf("CtxWrite failed: %v", err)
		}

		readData, err := m.CtxRead(cid, 0, 0)
		if err != nil {
			t.Fatalf("CtxRead failed: %v", err)
		}

		var result struct {
			SystemPrompt string    `json:"system_prompt"`
			Messages     []Message `json:"messages"`
		}
		if err := json.Unmarshal(readData, &result); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if len(result.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result.Messages))
		}
		if result.Messages[0].Role != RoleUser {
			t.Fatalf("expected role %s, got %s", RoleUser, result.Messages[0].Role)
		}
		if result.Messages[0].Content != "hello" {
			t.Fatalf("expected content 'hello', got '%s'", result.Messages[0].Content)
		}
	})

	t.Run("write multiple and read back", func(t *testing.T) {
		msg2 := Message{Role: RoleAssistant, Content: "world"}
		data2, _ := json.Marshal(msg2)
		if err := m.CtxWrite(cid, 0, data2); err != nil {
			t.Fatalf("CtxWrite failed: %v", err)
		}

		readData, err := m.CtxRead(cid, 0, 0)
		if err != nil {
			t.Fatalf("CtxRead failed: %v", err)
		}

		var result struct {
			Messages []Message `json:"messages"`
		}
		if err := json.Unmarshal(readData, &result); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if len(result.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result.Messages))
		}
		if result.Messages[1].Role != RoleAssistant {
			t.Fatalf("expected role %s, got %s", RoleAssistant, result.Messages[1].Role)
		}
	})

	t.Run("invalid JSON data returns error", func(t *testing.T) {
		err := m.CtxWrite(cid, 0, []byte("not json"))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrInternal {
			t.Fatalf("expected ErrInternal, got %s", ctxErr.Code)
		}
	})

	t.Run("context full returns error", func(t *testing.T) {
		m := NewManager()
		cid, _ := m.CtxAlloc(1)
		msg := Message{Role: RoleUser, Content: "fill"}
		data, _ := json.Marshal(msg)
		if err := m.CtxWrite(cid, 0, data); err != nil {
			t.Fatalf("first CtxWrite failed: %v", err)
		}

		err := m.CtxWrite(cid, 0, data)
		if err == nil {
			t.Fatal("expected error for full context")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrInternal {
			t.Fatalf("expected ErrInternal, got %s", ctxErr.Code)
		}
	})
}

func TestManager_CtxWriteOverwrite(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc failed: %v", err)
	}

	// Seed with two messages
	msg1 := Message{Role: RoleUser, Content: "first"}
	msg2 := Message{Role: RoleAssistant, Content: "second"}
	data1, _ := json.Marshal(msg1)
	data2, _ := json.Marshal(msg2)
	if err := m.CtxWrite(cid, 0, data1); err != nil {
		t.Fatalf("CtxWrite append 1 failed: %v", err)
	}
	if err := m.CtxWrite(cid, 0, data2); err != nil {
		t.Fatalf("CtxWrite append 2 failed: %v", err)
	}

	t.Run("overwrite first message with offset=1", func(t *testing.T) {
		replacement := Message{Role: RoleUser, Content: "replaced-first"}
		data, _ := json.Marshal(replacement)
		if err := m.CtxWrite(cid, 1, data); err != nil {
			t.Fatalf("CtxWrite overwrite failed: %v", err)
		}

		result, err := m.BuildPrompt(cid)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}
		if result.Messages[0].Content != "replaced-first" {
			t.Fatalf("expected overwritten content 'replaced-first', got '%s'", result.Messages[0].Content)
		}
		if result.Messages[1].Content != "second" {
			t.Fatalf("second message should be unchanged, got '%s'", result.Messages[1].Content)
		}
	})

	t.Run("overwrite second message with offset=2", func(t *testing.T) {
		replacement := Message{Role: RoleAssistant, Content: "replaced-second"}
		data, _ := json.Marshal(replacement)
		if err := m.CtxWrite(cid, 2, data); err != nil {
			t.Fatalf("CtxWrite overwrite failed: %v", err)
		}

		result, err := m.BuildPrompt(cid)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}
		if result.Messages[1].Content != "replaced-second" {
			t.Fatalf("expected overwritten content 'replaced-second', got '%s'", result.Messages[1].Content)
		}
	})

	t.Run("offset out of range returns error", func(t *testing.T) {
		msg := Message{Role: RoleUser, Content: "oob"}
		data, _ := json.Marshal(msg)

		// offset=3 with only 2 messages
		err := m.CtxWrite(cid, 3, data)
		if err == nil {
			t.Fatal("expected error for out-of-range offset")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrInternal {
			t.Fatalf("expected ErrInternal, got %s", ctxErr.Code)
		}
	})

	t.Run("negative offset returns error", func(t *testing.T) {
		msg := Message{Role: RoleUser, Content: "neg"}
		data, _ := json.Marshal(msg)

		err := m.CtxWrite(cid, -1, data)
		if err == nil {
			t.Fatal("expected error for negative offset")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
	})
}

func TestManager_SetSystemPrompt(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc failed: %v", err)
	}

	t.Run("set system prompt", func(t *testing.T) {
		if err := m.SetSystemPrompt(cid, "You are a helpful assistant."); err != nil {
			t.Fatalf("SetSystemPrompt failed: %v", err)
		}

		result, err := m.BuildPrompt(cid)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}
		if result.SystemPrompt != "You are a helpful assistant." {
			t.Fatalf("expected system prompt 'You are a helpful assistant.', got '%s'", result.SystemPrompt)
		}
	})

	t.Run("update system prompt overwrites", func(t *testing.T) {
		if err := m.SetSystemPrompt(cid, "Updated prompt."); err != nil {
			t.Fatalf("SetSystemPrompt failed: %v", err)
		}

		result, err := m.BuildPrompt(cid)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}
		if result.SystemPrompt != "Updated prompt." {
			t.Fatalf("expected updated system prompt, got '%s'", result.SystemPrompt)
		}
	})

	t.Run("set system prompt on freed context returns error", func(t *testing.T) {
		m := NewManager()
		cid, _ := m.CtxAlloc(10)
		m.CtxFree(cid)

		err := m.SetSystemPrompt(cid, "test")
		if err == nil {
			t.Fatal("expected error for freed context")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", ctxErr.Code)
		}
	})
}

func TestManager_AppendMessage(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc failed: %v", err)
	}

	t.Run("append messages with different roles", func(t *testing.T) {
		if err := m.AppendMessage(cid, RoleUser, "What is Go?"); err != nil {
			t.Fatalf("AppendMessage failed: %v", err)
		}
		if err := m.AppendMessage(cid, RoleAssistant, "Go is a programming language."); err != nil {
			t.Fatalf("AppendMessage failed: %v", err)
		}
		if err := m.AppendMessage(cid, RoleUser, "Tell me more."); err != nil {
			t.Fatalf("AppendMessage failed: %v", err)
		}

		result, err := m.BuildPrompt(cid)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}

		if len(result.Messages) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(result.Messages))
		}
		if result.Messages[0].Role != RoleUser || result.Messages[0].Content != "What is Go?" {
			t.Fatalf("message 0 mismatch: %+v", result.Messages[0])
		}
		if result.Messages[1].Role != RoleAssistant || result.Messages[1].Content != "Go is a programming language." {
			t.Fatalf("message 1 mismatch: %+v", result.Messages[1])
		}
		if result.Messages[2].Role != RoleUser || result.Messages[2].Content != "Tell me more." {
			t.Fatalf("message 2 mismatch: %+v", result.Messages[2])
		}
	})

	t.Run("append to full context returns error", func(t *testing.T) {
		m := NewManager()
		cid, _ := m.CtxAlloc(2)
		m.AppendMessage(cid, RoleUser, "1")
		m.AppendMessage(cid, RoleAssistant, "2")

		err := m.AppendMessage(cid, RoleUser, "3")
		if err == nil {
			t.Fatal("expected error for full context")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrInternal {
			t.Fatalf("expected ErrInternal, got %s", ctxErr.Code)
		}
	})
}

func TestManager_AppendToolResult(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc failed: %v", err)
	}

	t.Run("append tool result with toolCallID", func(t *testing.T) {
		if err := m.AppendToolResult(cid, "call-123", `{"output": "file contents"}`); err != nil {
			t.Fatalf("AppendToolResult failed: %v", err)
		}

		result, err := m.BuildPrompt(cid)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}

		if len(result.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result.Messages))
		}
		msg := result.Messages[0]
		if msg.Role != RoleTool {
			t.Fatalf("expected role %s, got %s", RoleTool, msg.Role)
		}
		if msg.ToolCallID != "call-123" {
			t.Fatalf("expected ToolCallID 'call-123', got '%s'", msg.ToolCallID)
		}
		if msg.Content != `{"output": "file contents"}` {
			t.Fatalf("expected content mismatch, got '%s'", msg.Content)
		}
	})

	t.Run("append tool result on freed context returns error", func(t *testing.T) {
		m := NewManager()
		cid, _ := m.CtxAlloc(10)
		m.CtxFree(cid)

		err := m.AppendToolResult(cid, "call-456", "result")
		if err == nil {
			t.Fatal("expected error for freed context")
		}
		var ctxErr *ContextError
		if !errors.As(err, &ctxErr) {
			t.Fatalf("expected *ContextError, got %T", err)
		}
		if ctxErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", ctxErr.Code)
		}
	})
}

func TestManager_BuildPrompt(t *testing.T) {
	t.Run("assembly order is correct", func(t *testing.T) {
		m := NewManager()
		cid, _ := m.CtxAlloc(100)

		m.SetSystemPrompt(cid, "You are a code analyst.")
		m.AppendMessage(cid, RoleUser, "Analyze this code")
		m.AppendMessage(cid, RoleAssistant, "I'll read the file first.")
		m.AppendToolResult(cid, "tool-read-1", "file contents here")
		m.AppendMessage(cid, RoleAssistant, "The code does X.")
		m.AppendMessage(cid, RoleUser, "Can you fix bug Y?")

		result, err := m.BuildPrompt(cid)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}

		if result.SystemPrompt != "You are a code analyst." {
			t.Fatalf("system prompt mismatch: %s", result.SystemPrompt)
		}

		expected := []struct {
			role       Role
			content    string
			toolCallID string
		}{
			{RoleUser, "Analyze this code", ""},
			{RoleAssistant, "I'll read the file first.", ""},
			{RoleTool, "file contents here", "tool-read-1"},
			{RoleAssistant, "The code does X.", ""},
			{RoleUser, "Can you fix bug Y?", ""},
		}

		if len(result.Messages) != len(expected) {
			t.Fatalf("expected %d messages, got %d", len(expected), len(result.Messages))
		}

		for i, exp := range expected {
			msg := result.Messages[i]
			if msg.Role != exp.role {
				t.Errorf("message %d: expected role %s, got %s", i, exp.role, msg.Role)
			}
			if msg.Content != exp.content {
				t.Errorf("message %d: expected content '%s', got '%s'", i, exp.content, msg.Content)
			}
			if msg.ToolCallID != exp.toolCallID {
				t.Errorf("message %d: expected toolCallID '%s', got '%s'", i, exp.toolCallID, msg.ToolCallID)
			}
		}
	})
}

func TestManager_BuildPromptEmpty(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(100)

	result, err := m.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}

	if result.SystemPrompt != "" {
		t.Fatalf("expected empty system prompt, got '%s'", result.SystemPrompt)
	}
	if len(result.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(result.Messages))
	}
}

func TestManager_BuildPromptPerformance(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(10000)

	m.SetSystemPrompt(cid, "You are a helpful assistant with a very long system prompt that contains many instructions.")

	for i := 0; i < 5000; i++ {
		m.AppendMessage(cid, RoleUser, "This is a user message with some content to simulate real conversation.")
		m.AppendMessage(cid, RoleAssistant, "This is an assistant response with some content to simulate real conversation.")
	}

	// BuildPrompt must complete in under 1 second (NFR5)
	start := time.Now()
	result, err := m.BuildPrompt(cid)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	if len(result.Messages) != 10000 {
		t.Fatalf("expected 10000 messages, got %d", len(result.Messages))
	}
	if elapsed > time.Second {
		t.Fatalf("BuildPrompt took %v, exceeds NFR5 limit of 1s", elapsed)
	}
	t.Logf("BuildPrompt with 10000 messages completed in %v", elapsed)
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager()
	const goroutines = 50
	const opsPerGoroutine = 20

	var wg sync.WaitGroup

	// Concurrent Alloc
	ids := make(chan types.CtxID, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cid, err := m.CtxAlloc(100)
			if err != nil {
				t.Errorf("CtxAlloc failed: %v", err)
				return
			}
			ids <- cid
		}()
	}
	wg.Wait()
	close(ids)

	allocatedIDs := make([]types.CtxID, 0, goroutines)
	seen := make(map[types.CtxID]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate CtxID from concurrent alloc: %d", id)
		}
		seen[id] = true
		allocatedIDs = append(allocatedIDs, id)
	}

	if len(allocatedIDs) != goroutines {
		t.Fatalf("expected %d allocated IDs, got %d", goroutines, len(allocatedIDs))
	}

	// Concurrent Write/Read on the same context
	// MaxSize=100, goroutines*opsPerGoroutine=1000 appends total.
	// Early appends succeed, later ones return "context full" — both outcomes are valid.
	cid := allocatedIDs[0]
	var wg2 sync.WaitGroup
	var appendOK, appendFull, readOK, readErr, buildOK, buildErr int64
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		wg2.Add(1)
		go func(idx int) {
			defer wg2.Done()
			var localAppendOK, localAppendFull, localReadOK, localReadErr, localBuildOK, localBuildErr int64
			for j := 0; j < opsPerGoroutine; j++ {
				if err := m.AppendMessage(cid, RoleUser, "concurrent write"); err != nil {
					localAppendFull++
				} else {
					localAppendOK++
				}
				if _, err := m.CtxRead(cid, 0, 0); err != nil {
					localReadErr++
				} else {
					localReadOK++
				}
				if _, err := m.BuildPrompt(cid); err != nil {
					localBuildErr++
				} else {
					localBuildOK++
				}
			}
			mu.Lock()
			appendOK += localAppendOK
			appendFull += localAppendFull
			readOK += localReadOK
			readErr += localReadErr
			buildOK += localBuildOK
			buildErr += localBuildErr
			mu.Unlock()
		}(i)
	}
	wg2.Wait()

	// Verify: exactly 100 appends succeeded (MaxSize), rest failed
	if appendOK != 100 {
		t.Errorf("expected exactly 100 successful appends (MaxSize), got %d", appendOK)
	}
	if appendFull != int64(goroutines*opsPerGoroutine)-100 {
		t.Errorf("expected %d context-full errors, got %d", goroutines*opsPerGoroutine-100, appendFull)
	}
	// All reads and builds should succeed (context still exists)
	if readErr != 0 {
		t.Errorf("expected 0 read errors, got %d", readErr)
	}
	if buildErr != 0 {
		t.Errorf("expected 0 build errors, got %d", buildErr)
	}
	t.Logf("Concurrent results: append OK=%d full=%d, read OK=%d, build OK=%d", appendOK, appendFull, readOK, buildOK)

	// Concurrent Free
	var wg3 sync.WaitGroup
	for _, id := range allocatedIDs[1:] {
		wg3.Add(1)
		go func(cid types.CtxID) {
			defer wg3.Done()
			m.CtxFree(cid)
		}(id)
	}
	wg3.Wait()

	// Verify freed contexts return error
	for _, id := range allocatedIDs[1:] {
		_, err := m.CtxRead(id, 0, 0)
		if err == nil {
			t.Errorf("expected error for freed CtxID %d", id)
		}
	}
}

func TestContextError_Format(t *testing.T) {
	err := &ContextError{
		Op:   "CtxAlloc",
		CID:  42,
		Err:  errors.New("test error"),
		Code: types.ErrInternal,
	}

	expected := "[INTERNAL] CtxID 42 CtxAlloc: test error"
	if err.Error() != expected {
		t.Fatalf("expected error string '%s', got '%s'", expected, err.Error())
	}

	unwrapped := err.Unwrap()
	if unwrapped == nil || unwrapped.Error() != "test error" {
		t.Fatalf("Unwrap returned unexpected: %v", unwrapped)
	}
}

// ========== Story 4.3: GetContextSummary Tests ==========

func TestManager_GetContextSummary_Basic(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(64)
	_ = m.SetSystemPrompt(cid, "You are a test agent.")
	_ = m.AppendMessage(cid, RoleUser, "请分析这段代码")
	_ = m.AppendMessage(cid, RoleAssistant, "分析完成，发现 3 个问题需要修复")

	summary, err := m.GetContextSummary(cid)
	if err != nil {
		t.Fatalf("GetContextSummary failed: %v", err)
	}

	if !containsStr(summary, "Messages: 2") {
		t.Errorf("expected 'Messages: 2' in summary, got: %s", summary)
	}
	if !containsStr(summary, "user: 1") {
		t.Errorf("expected 'user: 1' in summary, got: %s", summary)
	}
	if !containsStr(summary, "assistant: 1") {
		t.Errorf("expected 'assistant: 1' in summary, got: %s", summary)
	}
	if !containsStr(summary, "System Prompt:") {
		t.Errorf("expected 'System Prompt:' in summary, got: %s", summary)
	}
	if !containsStr(summary, "Last Message: [assistant]") {
		t.Errorf("expected 'Last Message: [assistant]' in summary, got: %s", summary)
	}
}

func TestManager_GetContextSummary_Empty(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(64)

	summary, err := m.GetContextSummary(cid)
	if err != nil {
		t.Fatalf("GetContextSummary failed: %v", err)
	}

	if !containsStr(summary, "Messages: 0") {
		t.Errorf("expected 'Messages: 0' in summary, got: %s", summary)
	}
	if !containsStr(summary, "System Prompt: 0 chars") {
		t.Errorf("expected 'System Prompt: 0 chars' in summary, got: %s", summary)
	}
}

func TestManager_GetContextSummary_NotFound(t *testing.T) {
	m := NewManager()

	_, err := m.GetContextSummary(types.CtxID(999))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var ctxErr *ContextError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextError, got %T", err)
	}
	if ctxErr.Code != types.ErrNotFound {
		t.Errorf("code: got %q, want %q", ctxErr.Code, types.ErrNotFound)
	}
}

func TestManager_GetContextSummary_LongMessage(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(64)

	longContent := strings.Repeat("x", 200)
	_ = m.AppendMessage(cid, RoleUser, longContent)

	summary, err := m.GetContextSummary(cid)
	if err != nil {
		t.Fatalf("GetContextSummary failed: %v", err)
	}

	// Preview should be truncated to 80 chars + "..."
	if !containsStr(summary, "...") {
		t.Errorf("expected truncated preview with '...', got: %s", summary)
	}
}

func TestManager_GetContextSummary_WithToolMessages(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(64)
	_ = m.AppendMessage(cid, RoleUser, "read file")
	_ = m.AppendToolResult(cid, "/dev/fs", "file contents here")
	_ = m.AppendMessage(cid, RoleAssistant, "got it")

	summary, err := m.GetContextSummary(cid)
	if err != nil {
		t.Fatalf("GetContextSummary failed: %v", err)
	}

	if !containsStr(summary, "tool: 1") {
		t.Errorf("expected 'tool: 1' in summary, got: %s", summary)
	}
	if !containsStr(summary, "Messages: 3") {
		t.Errorf("expected 'Messages: 3' in summary, got: %s", summary)
	}
}

// ========== Story 4.5: CtxFree Specialized Tests ==========

func TestManager_CtxFreeConcurrent(t *testing.T) {
	// 100 goroutines concurrently Alloc+Free — verify no race conditions under -race
	m := NewManager()
	const goroutines = 100

	var wg sync.WaitGroup
	var successCount atomic.Int64
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cid, err := m.CtxAlloc(10)
			if err != nil {
				errs <- err
				return
			}
			// Write some data before freeing
			if err := m.AppendMessage(cid, RoleUser, "concurrent msg"); err != nil {
				errs <- err
				return
			}
			if err := m.CtxFree(cid); err != nil {
				errs <- err
				return
			}
			// Verify freed: CtxRead should fail
			if _, err := m.CtxRead(cid, 0, 0); err == nil {
				errs <- errors.New("expected error after CtxFree in concurrent goroutine")
				return
			}
			successCount.Add(1)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent CtxFree error: %v", err)
	}

	if got := successCount.Load(); got != goroutines {
		t.Errorf("expected %d successful Alloc+Free cycles, got %d", goroutines, got)
	}
}

func TestManager_CtxFreeDoubleFree(t *testing.T) {
	// Double free: second call returns ErrNotFound (idempotent safety)
	m := NewManager()
	cid, err := m.CtxAlloc(10)
	if err != nil {
		t.Fatalf("CtxAlloc failed: %v", err)
	}

	// First free succeeds
	if err := m.CtxFree(cid); err != nil {
		t.Fatalf("first CtxFree failed: %v", err)
	}

	// Second free returns ErrNotFound
	err = m.CtxFree(cid)
	if err == nil {
		t.Fatal("expected error on double CtxFree")
	}
	var ctxErr *ContextError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextError, got %T", err)
	}
	if ctxErr.Code != types.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %s", ctxErr.Code)
	}
}

func TestManager_CtxFreeAllOperationsFail(t *testing.T) {
	// After free, ALL context operations return ErrNotFound
	m := NewManager()
	cid, err := m.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc failed: %v", err)
	}

	// Populate context before freeing
	_ = m.SetSystemPrompt(cid, "test prompt")
	_ = m.AppendMessage(cid, RoleUser, "test message")

	if err := m.CtxFree(cid); err != nil {
		t.Fatalf("CtxFree failed: %v", err)
	}

	ops := []struct {
		name string
		fn   func() error
	}{
		{"CtxRead", func() error { _, err := m.CtxRead(cid, 0, 0); return err }},
		{"CtxWrite", func() error {
			data, _ := json.Marshal(Message{Role: RoleUser, Content: "x"})
			return m.CtxWrite(cid, 0, data)
		}},
		{"SetSystemPrompt", func() error { return m.SetSystemPrompt(cid, "x") }},
		{"AppendMessage", func() error { return m.AppendMessage(cid, RoleUser, "x") }},
		{"AppendToolResult", func() error { return m.AppendToolResult(cid, "call-1", "x") }},
		{"BuildPrompt", func() error { _, err := m.BuildPrompt(cid); return err }},
		{"GetContextSummary", func() error { _, err := m.GetContextSummary(cid); return err }},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			err := op.fn()
			if err == nil {
				t.Fatalf("%s should fail after CtxFree", op.name)
			}
			var ctxErr *ContextError
			if !errors.As(err, &ctxErr) {
				t.Fatalf("%s: expected *ContextError, got %T", op.name, err)
			}
			if ctxErr.Code != types.ErrNotFound {
				t.Fatalf("%s: expected ErrNotFound, got %s", op.name, ctxErr.Code)
			}
		})
	}
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}
