package inspector

import (
	"testing"

	"github.com/rnixai/rnix/ipc"
)

// makeSteps 生成连续的 ipc.StepSummaryWire 序列（Step = 1..n）。
func makeSteps(n int) []ipc.StepSummaryWire {
	out := make([]ipc.StepSummaryWire, n)
	for i := range n {
		out[i].Step = i + 1
	}
	return out
}

func stepsList(steps []ipc.StepSummaryWire) []int {
	out := make([]int, len(steps))
	for i, s := range steps {
		out[i] = s.Step
	}
	return out
}

func TestTrimThumbnailToWidth_NoTrimNeeded(t *testing.T) {
	t.Parallel()
	steps := makeSteps(5)
	got := TrimThumbnailToWidth(steps, 3, 10)
	if len(got) != 5 {
		t.Fatalf("expected unchanged length 5, got %d", len(got))
	}
}

func TestTrimThumbnailToWidth_CurInMiddle(t *testing.T) {
	t.Parallel()
	steps := makeSteps(20) // 1..20
	got := TrimThumbnailToWidth(steps, 10, 6)
	// maxSlots=6, side=3, around cur=10: window covers 7..12 (6 entries)
	// Sentinels inserted on both sides because start>0 AND end<len(steps)
	if got[0].Step != -1 {
		t.Fatalf("expected leading sentinel, got %d", got[0].Step)
	}
	if got[len(got)-1].Step != -1 {
		t.Fatalf("expected trailing sentinel, got %d", got[len(got)-1].Step)
	}
	if !contains(got, 10) {
		t.Fatalf("current step 10 should be in window: %v", stepsList(got))
	}
}

func TestTrimThumbnailToWidth_CurAtStart(t *testing.T) {
	t.Parallel()
	steps := makeSteps(20)
	got := TrimThumbnailToWidth(steps, 1, 5)
	// start=0, no leading sentinel
	if got[0].Step == -1 {
		t.Fatalf("no leading sentinel when start==0, got %v", stepsList(got))
	}
	// trailing sentinel because end < len(steps)
	if got[len(got)-1].Step != -1 {
		t.Fatalf("expected trailing sentinel, got %v", stepsList(got))
	}
}

func TestTrimThumbnailToWidth_CurAtEnd(t *testing.T) {
	t.Parallel()
	steps := makeSteps(20)
	got := TrimThumbnailToWidth(steps, 20, 5)
	// end=20, start=15, leading sentinel because start>0
	if got[0].Step != -1 {
		t.Fatalf("expected leading sentinel, got %v", stepsList(got))
	}
	if got[len(got)-1].Step == -1 {
		t.Fatalf("no trailing sentinel when end==len, got %v", stepsList(got))
	}
}

func TestTrimThumbnailToWidth_CurNotPresent(t *testing.T) {
	t.Parallel()
	steps := makeSteps(20)
	got := TrimThumbnailToWidth(steps, 999, 5) // cur missing → centered fallback
	if len(got) == 0 {
		t.Fatal("got zero-length window for missing cur")
	}
}

func TestTrimThumbnailToWidth_MaxSlotsOne(t *testing.T) {
	t.Parallel()
	steps := makeSteps(10) // step values 1..10, indices 0..9
	got := TrimThumbnailToWidth(steps, 5, 1)
	// curIdx=4 (step 5). side=max(0, 1)=1. start=max(4-1, 0)=3.
	// end=min(3+1, 10)=4. start=max(4-1, 0)=3. window = steps[3:4] = [step 4].
	// start>0 → leading sentinel; end<10 → trailing sentinel. out = [-1, 4, -1].
	// Story 38-3 review P10: with maxSlots=1 the window picks the slot just
	// before cur due to start re-anchoring; this is acceptable because the
	// real thumbnail bar uses maxSlots ≥ 5 in practice.
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (sentinel + 1 step + sentinel), got %d", len(got))
	}
	if got[0].Step != -1 || got[2].Step != -1 {
		t.Fatalf("expected sentinels at both ends, got %v", stepsList(got))
	}
	if got[1].Step < 1 || got[1].Step > 10 {
		t.Fatalf("middle step out of range, got %d", got[1].Step)
	}
}

func TestCompressThumbnailWindow_CurNotPresentShortList(t *testing.T) {
	t.Parallel()
	steps := makeSteps(5)
	got := CompressThumbnailWindow(steps, 999, 10) // window 21, len 5 → return all
	if len(got) != 5 {
		t.Fatalf("expected unchanged length 5, got %d", len(got))
	}
	if got[0].Step == -1 {
		t.Fatalf("no sentinel when len ≤ window", )
	}
}

func TestCompressThumbnailWindow_CurNotPresentLongList(t *testing.T) {
	t.Parallel()
	steps := makeSteps(100)
	got := CompressThumbnailWindow(steps, 999, 10)
	// windowLen = 21; out = sentinel + last 21 (steps 80..100)
	if got[0].Step != -1 {
		t.Fatalf("expected leading sentinel for missing cur in long list, got %d", got[0].Step)
	}
	if len(got) != 22 { // 1 sentinel + 21 entries
		t.Fatalf("expected 22 entries, got %d", len(got))
	}
	// Should be tail, last entry should be step 100
	if got[len(got)-1].Step != 100 {
		t.Fatalf("expected last step 100, got %d", got[len(got)-1].Step)
	}
}

func TestCompressThumbnailWindow_CurInMiddle(t *testing.T) {
	t.Parallel()
	steps := makeSteps(100)
	got := CompressThumbnailWindow(steps, 50, 5)
	// curIdx=49, start=44, end=55, window steps 45..55 (11 entries)
	// start>0 → leading sentinel; end<100 → trailing sentinel
	if got[0].Step != -1 {
		t.Fatalf("expected leading sentinel, got %d", got[0].Step)
	}
	if got[len(got)-1].Step != -1 {
		t.Fatalf("expected trailing sentinel, got %d", got[len(got)-1].Step)
	}
	if !contains(got, 50) {
		t.Fatalf("current step 50 should be in window: %v", stepsList(got))
	}
}

func TestCompressThumbnailWindow_CurAtStart(t *testing.T) {
	t.Parallel()
	steps := makeSteps(100)
	got := CompressThumbnailWindow(steps, 1, 5)
	// curIdx=0, start=0, end=6 → no leading sentinel, trailing sentinel
	if got[0].Step == -1 {
		t.Fatalf("no leading sentinel when start==0, got %v", stepsList(got))
	}
	if got[len(got)-1].Step != -1 {
		t.Fatalf("expected trailing sentinel, got %v", stepsList(got))
	}
}

func TestCompressThumbnailWindow_CurAtEnd(t *testing.T) {
	t.Parallel()
	steps := makeSteps(100)
	got := CompressThumbnailWindow(steps, 100, 5)
	// curIdx=99, start=94, end=100 → leading sentinel, no trailing sentinel
	if got[0].Step != -1 {
		t.Fatalf("expected leading sentinel, got %v", stepsList(got))
	}
	if got[len(got)-1].Step == -1 {
		t.Fatalf("no trailing sentinel when end==len, got %v", stepsList(got))
	}
}

func TestCompressThumbnailWindow_SmallList(t *testing.T) {
	t.Parallel()
	steps := makeSteps(3)
	got := CompressThumbnailWindow(steps, 2, 5)
	// curIdx=1, start=0, end=3 → no sentinels; same as input
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (no sentinels), got %d", len(got))
	}
}

func contains(steps []ipc.StepSummaryWire, step int) bool {
	for _, s := range steps {
		if s.Step == step {
			return true
		}
	}
	return false
}
