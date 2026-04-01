package debug

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

func TestPredictGrowth_MaxStepsZero_PredictExhaustFalse(t *testing.T) {
	history := []types.TokenSnapshot{
		{Step: 1, Tokens: 100},
		{Step: 2, Tokens: 200},
		{Step: 3, Tokens: 300},
	}
	result := PredictGrowth(1, 300, 1000, 3, 0, history)

	if result.PredictExhaust {
		t.Error("expected PredictExhaust=false when maxSteps=0 (infinite)")
	}
	if result.MaxSteps != 0 {
		t.Errorf("expected MaxSteps=0, got %d", result.MaxSteps)
	}
}

func TestPredictGrowth_MaxStepsPositive_PredictExhaustWorks(t *testing.T) {
	history := []types.TokenSnapshot{
		{Step: 1, Tokens: 100},
		{Step: 2, Tokens: 200},
		{Step: 3, Tokens: 300},
	}
	// Budget=500, rate=100/step, remaining=200 tok => estRemaining=2
	// currentStep(3) + estRemaining(2) = 5 <= maxSteps(10) → true (budget exhausts within maxSteps)
	result := PredictGrowth(1, 300, 500, 3, 10, history)
	if !result.PredictExhaust {
		t.Error("expected PredictExhaust=true when budget exhausts within maxSteps")
	}

	// currentStep(3) + estRemaining(2) = 5 <= maxSteps(4) is 5<=4 → false
	result2 := PredictGrowth(1, 300, 500, 3, 4, history)
	if result2.PredictExhaust {
		t.Error("expected PredictExhaust=false when budget exhausts after maxSteps")
	}
}
