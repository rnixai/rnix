package debug

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/rnixai/rnix/internal/types"
)

// AlertLevel classifies the urgency of a budget alert.
type AlertLevel string

const (
	AlertNone     AlertLevel = "none"
	AlertWarning  AlertLevel = "warning"  // remaining < 20%
	AlertCritical AlertLevel = "critical" // remaining < 10%
)

const recentWindowSize = 5

// GrowthSnapshot holds token usage data at a point in time for growth display.
type GrowthSnapshot struct {
	Step    int   `json:"step"`
	Tokens  int   `json:"tokens"`
	DeltaMs int64 `json:"delta_ms"`
}

// GrowthPrediction holds the result of a context growth analysis.
type GrowthPrediction struct {
	PID              types.PID        `json:"-"`
	TokensUsed       int              `json:"-"`
	ContextBudget    int              `json:"-"`
	UsagePct         float64          `json:"-"`
	CurrentStep      int              `json:"-"`
	MaxSteps         int              `json:"-"`
	AvgTokensPerStep float64          `json:"-"`
	RecentRate       float64          `json:"-"`
	RemainingBudget  int              `json:"-"`
	EstRemaining     int              `json:"-"`
	PredictExhaust   bool             `json:"-"`
	AlertLevel       AlertLevel       `json:"-"`
	History          []GrowthSnapshot `json:"-"`
}

// PredictGrowth analyzes token consumption history and predicts budget exhaustion.
func PredictGrowth(pid types.PID, tokensUsed, contextBudget, currentStep, maxSteps int, history []types.TokenSnapshot) *GrowthPrediction {
	p := &GrowthPrediction{
		PID:           pid,
		TokensUsed:    tokensUsed,
		ContextBudget: contextBudget,
		CurrentStep:   currentStep,
		MaxSteps:      maxSteps,
		AlertLevel:    AlertNone,
	}

	snapshots := make([]GrowthSnapshot, len(history))
	for i, h := range history {
		snapshots[i] = GrowthSnapshot{Step: h.Step, Tokens: h.Tokens, DeltaMs: h.DeltaMs}
	}
	p.History = snapshots

	if contextBudget > 0 {
		p.UsagePct = roundPct(float64(tokensUsed) / float64(contextBudget) * 100)
		p.RemainingBudget = max(contextBudget-tokensUsed, 0)
	}

	if currentStep > 0 {
		p.AvgTokensPerStep = roundPct(float64(tokensUsed) / float64(currentStep))
	}

	p.RecentRate = calcRecentRate(history)
	if p.RecentRate == 0 && p.AvgTokensPerStep > 0 {
		p.RecentRate = p.AvgTokensPerStep
	}

	if contextBudget > 0 && p.RecentRate > 0 {
		p.EstRemaining = max(int(math.Floor(float64(p.RemainingBudget)/p.RecentRate)), 0)
		if maxSteps > 0 {
			p.PredictExhaust = currentStep+p.EstRemaining <= maxSteps
		}
		// maxSteps == 0: infinite steps, PredictExhaust stays false
	}

	if contextBudget > 0 {
		remainPct := 100.0 - p.UsagePct
		if remainPct < 10 {
			p.AlertLevel = AlertCritical
		} else if remainPct < 20 {
			p.AlertLevel = AlertWarning
		}
	}

	return p
}

func calcRecentRate(history []types.TokenSnapshot) float64 {
	n := len(history)
	if n < 2 {
		return 0
	}

	window := min(recentWindowSize, n)

	recent := history[n-window:]
	totalDelta := 0
	count := 0
	for i := 1; i < len(recent); i++ {
		delta := recent[i].Tokens - recent[i-1].Tokens
		if delta > 0 {
			totalDelta += delta
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return roundPct(float64(totalDelta) / float64(count))
}

// FormatGrowthPrediction renders a GrowthPrediction as human-readable text.
func FormatGrowthPrediction(p *GrowthPrediction) string {
	var sb strings.Builder

	if p.ContextBudget > 0 {
		fmt.Fprintf(&sb, "Context Growth: PID %d  |  %d/%d tok  |  %.1f%% used\n",
			p.PID, p.TokensUsed, p.ContextBudget, p.UsagePct)
	} else {
		fmt.Fprintf(&sb, "Context Growth: PID %d  |  %d tok  |  No budget set\n",
			p.PID, p.TokensUsed)
	}

	if len(p.History) > 0 {
		sb.WriteString("\n── Growth Trend ─────────────────────────────────────\n")
		prev := 0
		for _, h := range p.History {
			delta := h.Tokens - prev
			fmt.Fprintf(&sb, "Step %2d:  %6d tok  (+%d)\n", h.Step, h.Tokens, delta)
			prev = h.Tokens
		}
	}

	if p.ContextBudget > 0 {
		sb.WriteString("\n── Prediction ──────────────────────────────────────\n")
		fmt.Fprintf(&sb, "Avg Rate:     %.1f tok/step\n", p.AvgTokensPerStep)
		if p.RecentRate > 0 && p.RecentRate != p.AvgTokensPerStep {
			fmt.Fprintf(&sb, "Recent Rate:  %.1f tok/step  (last %d steps)\n",
				p.RecentRate, min(recentWindowSize, len(p.History)))
		}
		fmt.Fprintf(&sb, "Remaining:    %d tok\n", p.RemainingBudget)
		if p.EstRemaining > 0 {
			fmt.Fprintf(&sb, "Est. Steps:   ~%d steps remaining\n", p.EstRemaining)
		}

		switch p.AlertLevel {
		case AlertNone:
			sb.WriteString("Alert:        none ✓\n")
		case AlertWarning:
			sb.WriteString("Alert:        ⚠ WARNING  (remaining < 20%)\n")
		case AlertCritical:
			sb.WriteString("Alert:        ⚠ CRITICAL  (remaining < 10%)\n")
		}

		sb.WriteString("\n── Budget ─────────────────────────────────────────\n")
		barWidth := 30
		filled := max(min(int(math.Round(p.UsagePct/100*float64(barWidth))), barWidth), 0)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		fmt.Fprintf(&sb, "[%s] %.1f%%\n", bar, p.UsagePct)
	}

	return sb.String()
}

type growthJSON struct {
	PID              types.PID        `json:"pid"`
	TokensUsed       int              `json:"tokens_used"`
	ContextBudget    int              `json:"context_budget"`
	UsagePct         float64          `json:"usage_pct"`
	CurrentStep      int              `json:"current_step"`
	MaxSteps         int              `json:"max_steps"`
	AvgTokensPerStep float64          `json:"avg_tokens_per_step"`
	RecentRate       float64          `json:"recent_rate"`
	RemainingBudget  int              `json:"remaining_budget"`
	EstRemaining     int              `json:"est_remaining"`
	PredictExhaust   bool             `json:"predict_exhaust"`
	AlertLevel       AlertLevel       `json:"alert_level"`
	History          []GrowthSnapshot `json:"history"`
}

// UnmarshalJSON deserializes from snake_case JSON.
func (p *GrowthPrediction) UnmarshalJSON(data []byte) error {
	var g growthJSON
	if err := json.Unmarshal(data, &g); err != nil {
		return err
	}
	p.PID = g.PID
	p.TokensUsed = g.TokensUsed
	p.ContextBudget = g.ContextBudget
	p.UsagePct = g.UsagePct
	p.CurrentStep = g.CurrentStep
	p.MaxSteps = g.MaxSteps
	p.AvgTokensPerStep = g.AvgTokensPerStep
	p.RecentRate = g.RecentRate
	p.RemainingBudget = g.RemainingBudget
	p.EstRemaining = g.EstRemaining
	p.PredictExhaust = g.PredictExhaust
	p.AlertLevel = g.AlertLevel
	p.History = g.History
	return nil
}

// MarshalJSON implements custom JSON serialization with snake_case fields.
func (p *GrowthPrediction) MarshalJSON() ([]byte, error) {
	history := p.History
	if history == nil {
		history = []GrowthSnapshot{}
	}
	return json.Marshal(growthJSON{
		PID:              p.PID,
		TokensUsed:       p.TokensUsed,
		ContextBudget:    p.ContextBudget,
		UsagePct:         p.UsagePct,
		CurrentStep:      p.CurrentStep,
		MaxSteps:         p.MaxSteps,
		AvgTokensPerStep: p.AvgTokensPerStep,
		RecentRate:       p.RecentRate,
		RemainingBudget:  p.RemainingBudget,
		EstRemaining:     p.EstRemaining,
		PredictExhaust:   p.PredictExhaust,
		AlertLevel:       p.AlertLevel,
		History:          history,
	})
}
