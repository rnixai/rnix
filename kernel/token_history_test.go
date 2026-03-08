package kernel

import (
	"sync"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

func TestProcess_TokenHistory_Empty(t *testing.T) {
	p := NewProcess(0, "test", nil)
	history := p.GetTokenHistory()
	if history != nil {
		t.Errorf("empty history should return nil, got %v", history)
	}
}

func TestProcess_TokenHistory_AppendAndRetrieve(t *testing.T) {
	p := NewProcess(0, "test", nil)

	p.AppendTokenSnapshot(1, 250)
	p.AppendTokenSnapshot(2, 520)
	p.AppendTokenSnapshot(3, 780)

	history := p.GetTokenHistory()
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3", len(history))
	}
	if history[0].Step != 1 || history[0].Tokens != 250 {
		t.Errorf("history[0] = {Step:%d, Tokens:%d}, want {1, 250}", history[0].Step, history[0].Tokens)
	}
	if history[1].Step != 2 || history[1].Tokens != 520 {
		t.Errorf("history[1] = {Step:%d, Tokens:%d}, want {2, 520}", history[1].Step, history[1].Tokens)
	}
	if history[2].Step != 3 || history[2].Tokens != 780 {
		t.Errorf("history[2] = {Step:%d, Tokens:%d}, want {3, 780}", history[2].Step, history[2].Tokens)
	}

	for _, h := range history {
		if h.DeltaMs < 0 {
			t.Errorf("DeltaMs = %d, should be >= 0", h.DeltaMs)
		}
	}
}

func TestProcess_TokenHistory_RingBufferOverflow(t *testing.T) {
	p := NewProcess(0, "test", nil)

	total := tokenHistoryCap + 10
	for i := 1; i <= total; i++ {
		p.AppendTokenSnapshot(i, i*100)
	}

	history := p.GetTokenHistory()
	if len(history) != tokenHistoryCap {
		t.Fatalf("history len = %d, want %d", len(history), tokenHistoryCap)
	}

	// Oldest should be step 11 (first 10 were overwritten)
	firstStep := total - tokenHistoryCap + 1
	if history[0].Step != firstStep {
		t.Errorf("oldest step = %d, want %d", history[0].Step, firstStep)
	}
	if history[0].Tokens != firstStep*100 {
		t.Errorf("oldest tokens = %d, want %d", history[0].Tokens, firstStep*100)
	}

	// Most recent
	if history[len(history)-1].Step != total {
		t.Errorf("newest step = %d, want %d", history[len(history)-1].Step, total)
	}

	// Verify ordering
	for i := 1; i < len(history); i++ {
		if history[i].Step <= history[i-1].Step {
			t.Errorf("out of order at index %d: step %d <= %d", i, history[i].Step, history[i-1].Step)
		}
	}
}

func TestProcess_TokenHistory_ConcurrentSafety(t *testing.T) {
	p := NewProcess(0, "test", nil)
	_ = p.Start()

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				step := base*20 + i + 1
				p.AppendTokenSnapshot(step, step*10)
			}
		}(g)
	}

	wg.Wait()
	history := p.GetTokenHistory()

	// 200 total appends, but ring buffer holds max 50
	if len(history) != tokenHistoryCap {
		t.Errorf("history len = %d, want %d", len(history), tokenHistoryCap)
	}
}

func TestProcess_TokenHistory_ReturnsCopy(t *testing.T) {
	p := NewProcess(0, "test", nil)
	p.AppendTokenSnapshot(1, 100)
	p.AppendTokenSnapshot(2, 200)

	history := p.GetTokenHistory()
	history[0] = types.TokenSnapshot{Step: 999, Tokens: 999}

	history2 := p.GetTokenHistory()
	if history2[0].Step == 999 {
		t.Error("GetTokenHistory should return a copy, not a reference")
	}
}
