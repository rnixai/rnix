package debug

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

func makeSnapshots(pairs ...int) []types.TokenSnapshot {
	if len(pairs)%2 != 0 {
		panic("makeSnapshots requires step,tokens pairs")
	}
	out := make([]types.TokenSnapshot, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out[i/2] = types.TokenSnapshot{
			Step:    pairs[i],
			Tokens:  pairs[i+1],
			DeltaMs: int64(pairs[i] * 1000),
		}
	}
	return out
}

func TestPredictGrowth_NoBudget(t *testing.T) {
	history := makeSnapshots(1, 250, 2, 500, 3, 750)
	p := PredictGrowth(1, 750, 0, 3, 50, history)

	if p.AlertLevel != AlertNone {
		t.Errorf("AlertLevel = %q, want %q", p.AlertLevel, AlertNone)
	}
	if p.EstRemaining != 0 {
		t.Errorf("EstRemaining = %d, want 0 (no budget)", p.EstRemaining)
	}
	if p.PredictExhaust {
		t.Error("PredictExhaust should be false with no budget")
	}
	if p.UsagePct != 0 {
		t.Errorf("UsagePct = %f, want 0", p.UsagePct)
	}
}

func TestPredictGrowth_WithHistory(t *testing.T) {
	history := makeSnapshots(1, 200, 2, 420, 3, 660, 4, 880, 5, 1100)
	p := PredictGrowth(1, 1100, 8000, 5, 50, history)

	if p.AvgTokensPerStep != 220 {
		t.Errorf("AvgTokensPerStep = %f, want 220", p.AvgTokensPerStep)
	}
	if p.RecentRate == 0 {
		t.Fatal("RecentRate should not be 0 with 5 steps")
	}
	if p.RemainingBudget != 6900 {
		t.Errorf("RemainingBudget = %d, want 6900", p.RemainingBudget)
	}
	if p.EstRemaining <= 0 {
		t.Errorf("EstRemaining = %d, want >0", p.EstRemaining)
	}
	if p.AlertLevel != AlertNone {
		t.Errorf("AlertLevel = %q, want %q", p.AlertLevel, AlertNone)
	}
}

func TestPredictGrowth_AlertWarning(t *testing.T) {
	history := makeSnapshots(1, 2000, 2, 4000, 3, 6000, 4, 6800)
	p := PredictGrowth(1, 6800, 8000, 4, 50, history)

	if p.UsagePct != 85 {
		t.Errorf("UsagePct = %f, want 85", p.UsagePct)
	}
	if p.AlertLevel != AlertWarning {
		t.Errorf("AlertLevel = %q, want %q", p.AlertLevel, AlertWarning)
	}
}

func TestPredictGrowth_AlertCritical(t *testing.T) {
	history := makeSnapshots(1, 3000, 2, 5000, 3, 7400)
	p := PredictGrowth(1, 7400, 8000, 3, 50, history)

	remainPct := 100 - p.UsagePct
	if remainPct >= 10 {
		t.Errorf("remaining = %.1f%%, want < 10%%", remainPct)
	}
	if p.AlertLevel != AlertCritical {
		t.Errorf("AlertLevel = %q, want %q", p.AlertLevel, AlertCritical)
	}
}

func TestPredictGrowth_EmptyHistory(t *testing.T) {
	p := PredictGrowth(1, 500, 8000, 0, 50, nil)

	if p.EstRemaining != 0 {
		t.Errorf("EstRemaining = %d, want 0 (empty history)", p.EstRemaining)
	}
	if p.AvgTokensPerStep != 0 {
		t.Errorf("AvgTokensPerStep = %f, want 0 (no steps)", p.AvgTokensPerStep)
	}
}

func TestPredictGrowth_SingleStep(t *testing.T) {
	history := makeSnapshots(1, 300)
	p := PredictGrowth(1, 300, 8000, 1, 50, history)

	if p.AvgTokensPerStep != 300 {
		t.Errorf("AvgTokensPerStep = %f, want 300", p.AvgTokensPerStep)
	}
	// With single step, RecentRate falls back to AvgTokensPerStep
	if p.RecentRate != p.AvgTokensPerStep {
		t.Errorf("RecentRate = %f, want %f (should equal AvgRate for single step)", p.RecentRate, p.AvgTokensPerStep)
	}
}

func TestPredictGrowth_PredictExhaust(t *testing.T) {
	// Moderate rate: ~500 tok/step, budget 5000, at step 6 with 3000 tokens used
	// Remaining = 2000, rate ~500, est ~4 steps => currentStep+4=10 <= maxSteps=50
	history := makeSnapshots(1, 500, 2, 1000, 3, 1500, 4, 2000, 5, 2500, 6, 3000)
	p := PredictGrowth(1, 3000, 5000, 6, 50, history)

	if !p.PredictExhaust {
		t.Error("PredictExhaust should be true (budget will run out within maxSteps)")
	}
	if p.EstRemaining <= 0 {
		t.Errorf("EstRemaining = %d, want >0", p.EstRemaining)
	}
}

func TestFormatGrowthPrediction_Normal(t *testing.T) {
	p := &GrowthPrediction{
		PID:              1,
		TokensUsed:       1200,
		ContextBudget:    8000,
		UsagePct:         15.0,
		CurrentStep:      5,
		MaxSteps:         50,
		AvgTokensPerStep: 240.0,
		RecentRate:       237.0,
		RemainingBudget:  6800,
		EstRemaining:     28,
		AlertLevel:       AlertNone,
		History: []GrowthSnapshot{
			{Step: 1, Tokens: 250, DeltaMs: 1000},
			{Step: 2, Tokens: 520, DeltaMs: 2000},
			{Step: 3, Tokens: 780, DeltaMs: 3000},
		},
	}

	out := FormatGrowthPrediction(p)

	for _, want := range []string{
		"Context Growth: PID 1",
		"1200/8000 tok",
		"Growth Trend",
		"Step  1:",
		"Prediction",
		"240.0 tok/step",
		"6800 tok",
		"~28 steps remaining",
		"none ✓",
		"Budget",
		"15.0%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestFormatGrowthPrediction_NoBudget(t *testing.T) {
	p := &GrowthPrediction{
		PID:           1,
		TokensUsed:    500,
		ContextBudget: 0,
		AlertLevel:    AlertNone,
	}

	out := FormatGrowthPrediction(p)

	if !strings.Contains(out, "No budget set") {
		t.Error("should show 'No budget set'")
	}
	if strings.Contains(out, "Prediction") {
		t.Error("should not show Prediction section without budget")
	}
	if strings.Contains(out, "Budget") && strings.Contains(out, "██") {
		t.Error("should not show budget bar without budget")
	}
}

func TestFormatGrowthPrediction_AlertWarning(t *testing.T) {
	p := &GrowthPrediction{
		PID:             1,
		TokensUsed:      6800,
		ContextBudget:   8000,
		UsagePct:        85.0,
		RemainingBudget: 1200,
		EstRemaining:    5,
		AlertLevel:      AlertWarning,
	}

	out := FormatGrowthPrediction(p)

	if !strings.Contains(out, "⚠ WARNING") {
		t.Error("should show ⚠ WARNING for warning alert")
	}
}

func TestGrowthPrediction_MarshalJSON(t *testing.T) {
	p := &GrowthPrediction{
		PID:              types.PID(1),
		TokensUsed:       1200,
		ContextBudget:    8000,
		UsagePct:         15.0,
		CurrentStep:      5,
		MaxSteps:         50,
		AvgTokensPerStep: 240.0,
		RecentRate:       237.5,
		RemainingBudget:  6800,
		EstRemaining:     28,
		PredictExhaust:   false,
		AlertLevel:       AlertNone,
		History: []GrowthSnapshot{
			{Step: 1, Tokens: 250, DeltaMs: 1000},
		},
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	for _, key := range []string{
		"pid", "tokens_used", "context_budget", "usage_pct",
		"current_step", "max_steps", "avg_tokens_per_step", "recent_rate",
		"remaining_budget", "est_remaining", "predict_exhaust", "alert_level", "history",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON", key)
		}
	}

	if m["alert_level"] != "none" {
		t.Errorf("alert_level = %v, want 'none'", m["alert_level"])
	}
	if m["predict_exhaust"] != false {
		t.Errorf("predict_exhaust = %v, want false", m["predict_exhaust"])
	}

	histArr, ok := m["history"].([]any)
	if !ok || len(histArr) != 1 {
		t.Errorf("history should be array with 1 element, got %v", m["history"])
	}

	// Empty history should be [] not null
	p2 := &GrowthPrediction{AlertLevel: AlertNone}
	data2, _ := json.Marshal(p2)
	var m2 map[string]any
	json.Unmarshal(data2, &m2)
	histArr2, ok := m2["history"].([]any)
	if !ok {
		t.Error("empty history should be [] not null")
	}
	if len(histArr2) != 0 {
		t.Errorf("empty history should have 0 elements, got %d", len(histArr2))
	}
}
