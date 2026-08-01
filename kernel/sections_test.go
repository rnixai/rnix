package kernel

import (
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

func TestScoreSkillMatch_ExactNameMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "code-review", Description: "Review code changes", SearchHint: "lint review"}
	score := scoreSkillMatch(ds, "code-review")
	if score < 12 {
		t.Errorf("expected exact name match score >= 12, got %d", score)
	}
}

func TestScoreSkillMatch_PartialNameMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "code-review", Description: "Review code changes", SearchHint: "lint review"}
	score := scoreSkillMatch(ds, "code")
	if score < 6 {
		t.Errorf("expected partial name match score >= 6, got %d", score)
	}
}

func TestScoreSkillMatch_DescriptionMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "formatter", Description: "Format source files", SearchHint: ""}
	score := scoreSkillMatch(ds, "source")
	if score < 2 {
		t.Errorf("expected description match score >= 2, got %d", score)
	}
}

func TestScoreSkillMatch_SearchHintMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "deploy", Description: "Deploy to cloud", SearchHint: "kubernetes docker containers"}
	score := scoreSkillMatch(ds, "kubernetes")
	if score < 4 {
		t.Errorf("expected search_hint match score >= 4, got %d", score)
	}
}

func TestScoreSkillMatch_NoMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "formatter", Description: "Format code", SearchHint: "prettify"}
	score := scoreSkillMatch(ds, "database")
	if score != 0 {
		t.Errorf("expected 0 score for no match, got %d", score)
	}
}

func TestScoreSkillMatch_CombinedScore(t *testing.T) {
	// "code-review" exact name (12) + hint contains "code-review" (4) + desc contains "code-review" (2) = 18
	ds := DeferredSkillMeta{Name: "code-review", Description: "Run code-review checks", SearchHint: "code-review lint"}
	score := scoreSkillMatch(ds, "code-review")
	if score < 12 {
		t.Errorf("expected combined score >= 12, got %d", score)
	}
}

func TestScoreSkillMatch_EmptyQuery(t *testing.T) {
	ds := DeferredSkillMeta{Name: "test", Description: "test", SearchHint: ""}
	score := scoreSkillMatch(ds, "")
	if score != 0 {
		t.Errorf("expected 0 score for empty query, got %d", score)
	}
}

func setupBackpressureKernel(t *testing.T, maxSize int) (*KernelImpl, *rnixctx.Manager, *Process) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(maxSize)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}

	proc := NewProcess(0, "backpressure test", nil)
	proc.CtxID = cid
	return k, ctxMgr, proc
}

// setBackpressurePct drives the process to a given context TOKEN usage
// percentage (Story 71.1 AC2 moved the backpressure axis off slots). The
// denominator is proc.effectiveContextTokenLimit(), which needs a real
// ContextWindow behind ContextBudget — without one, ContextBudget is a per-step
// leash and must NOT act as a capacity scale.
func setBackpressurePct(proc *Process, pct float64) {
	const window = 100_000
	proc.ContextWindow = window
	proc.ContextBudget = window * 9 / 10
	proc.LastInputTokens = int(float64(proc.ContextBudget) * pct / 100)
}

func TestBackpressureSection_AboveThreshold(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 10)
	setBackpressurePct(proc, 80) // > 70 threshold, <= 85 critical boundary

	sections := registerSections(proc, k, "")
	result := sections.Build()

	if !strings.Contains(result, "Context Resource Warning") {
		t.Error("expected backpressure section when context usage > 70%, got none")
	}
	// Story 69.1: the body is a per-tier constant, so assert on the tier's
	// qualitative wording instead of a percentage.
	if !strings.Contains(result, "Context message slots are running low.") {
		t.Errorf("expected elevated-tier wording in backpressure text, got:\n%s", result)
	}
	if strings.Contains(result, "80%") {
		t.Error("backpressure text must not carry the usage percentage (breaks the prompt cache prefix)")
	}
}

func TestBackpressureSection_BelowThreshold(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 10)
	setBackpressurePct(proc, 50)

	sections := registerSections(proc, k, "")
	result := sections.Build()

	if strings.Contains(result, "Context Resource Warning") {
		t.Error("backpressure section should NOT appear when context usage <= 70%")
	}
}

func TestBackpressureSection_AtExactThreshold(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 10)
	setBackpressurePct(proc, 70)

	sections := registerSections(proc, k, "")
	result := sections.Build()

	if strings.Contains(result, "Context Resource Warning") {
		t.Error("backpressure should NOT trigger at exactly 70% (must be >)")
	}
}

func TestBackpressureSection_CustomThreshold(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 10)
	proc.BackpressureThreshold = 50.0
	setBackpressurePct(proc, 60)

	sections := registerSections(proc, k, "")
	result := sections.Build()

	if !strings.Contains(result, "Context Resource Warning") {
		t.Error("expected backpressure at 60% with custom threshold 50%")
	}
}
