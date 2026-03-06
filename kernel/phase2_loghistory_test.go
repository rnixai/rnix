package kernel

import (
	"fmt"
	"testing"
	"time"

	"github.com/usecrux/crux/internal/types"
)

// --- BUG-007/008: Log History Ring Buffer Tests ---

func TestAppendLogHistory_BasicInsert(t *testing.T) {
	p := NewProcess(0, "test", nil)

	entry := types.LogEntry{
		Timestamp: 1 * time.Second,
		PID:       p.PID,
		Step:      1,
		Category:  types.LogThink,
		Content:   "thinking...",
	}

	p.mu.Lock()
	p.AppendLogHistory(entry)
	history := p.GetLogHistory()
	p.mu.Unlock()

	if len(history) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(history))
	}
	if history[0].Content != "thinking..." {
		t.Errorf("content = %q, want %q", history[0].Content, "thinking...")
	}
	if history[0].Category != types.LogThink {
		t.Errorf("category = %q, want %q", history[0].Category, types.LogThink)
	}
}

func TestGetLogHistory_Empty(t *testing.T) {
	p := NewProcess(0, "test", nil)

	p.mu.Lock()
	history := p.GetLogHistory()
	p.mu.Unlock()

	if history != nil {
		t.Errorf("expected nil for empty history, got %d entries", len(history))
	}
}

func TestAppendLogHistory_OrderPreserved(t *testing.T) {
	p := NewProcess(0, "test", nil)

	p.mu.Lock()
	for i := 0; i < 5; i++ {
		p.AppendLogHistory(types.LogEntry{
			Timestamp: time.Duration(i) * time.Second,
			Step:      i,
			Content:   fmt.Sprintf("entry-%d", i),
		})
	}
	history := p.GetLogHistory()
	p.mu.Unlock()

	if len(history) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(history))
	}
	for i, entry := range history {
		expected := fmt.Sprintf("entry-%d", i)
		if entry.Content != expected {
			t.Errorf("history[%d].Content = %q, want %q", i, entry.Content, expected)
		}
	}
}

func TestAppendLogHistory_RingBufferOverflow(t *testing.T) {
	p := NewProcess(0, "test", nil)

	total := logHistoryCap + 10

	p.mu.Lock()
	for i := 0; i < total; i++ {
		p.AppendLogHistory(types.LogEntry{
			Step:    i,
			Content: fmt.Sprintf("entry-%d", i),
		})
	}
	history := p.GetLogHistory()
	p.mu.Unlock()

	// Should only have logHistoryCap entries
	if len(history) != logHistoryCap {
		t.Fatalf("expected %d entries, got %d", logHistoryCap, len(history))
	}

	// Oldest entry should be entry-10 (first 10 were overwritten)
	if history[0].Content != "entry-10" {
		t.Errorf("oldest entry = %q, want %q", history[0].Content, "entry-10")
	}

	// Newest entry should be entry-(total-1)
	lastExpected := fmt.Sprintf("entry-%d", total-1)
	if history[len(history)-1].Content != lastExpected {
		t.Errorf("newest entry = %q, want %q", history[len(history)-1].Content, lastExpected)
	}

	// Verify all entries are in sequential order
	for i := 1; i < len(history); i++ {
		if history[i].Step <= history[i-1].Step {
			t.Errorf("entries not in order at [%d]: step %d <= step %d",
				i, history[i].Step, history[i-1].Step)
		}
	}
}

func TestAppendLogHistory_ExactlyAtCapacity(t *testing.T) {
	p := NewProcess(0, "test", nil)

	p.mu.Lock()
	for i := 0; i < logHistoryCap; i++ {
		p.AppendLogHistory(types.LogEntry{
			Step:    i,
			Content: fmt.Sprintf("entry-%d", i),
		})
	}
	history := p.GetLogHistory()
	p.mu.Unlock()

	if len(history) != logHistoryCap {
		t.Fatalf("expected %d entries, got %d", logHistoryCap, len(history))
	}
	if history[0].Content != "entry-0" {
		t.Errorf("first entry = %q, want %q", history[0].Content, "entry-0")
	}
	last := fmt.Sprintf("entry-%d", logHistoryCap-1)
	if history[logHistoryCap-1].Content != last {
		t.Errorf("last entry = %q, want %q", history[logHistoryCap-1].Content, last)
	}
}

func TestGetLogHistory_ReturnsCopy(t *testing.T) {
	p := NewProcess(0, "test", nil)

	p.mu.Lock()
	p.AppendLogHistory(types.LogEntry{Content: "original"})
	h1 := p.GetLogHistory()
	p.mu.Unlock()

	// Modify the returned slice
	h1[0].Content = "modified"

	// Original should be unchanged
	p.mu.Lock()
	h2 := p.GetLogHistory()
	p.mu.Unlock()

	if h2[0].Content != "original" {
		t.Errorf("GetLogHistory did not return a copy: got %q, want %q", h2[0].Content, "original")
	}
}
