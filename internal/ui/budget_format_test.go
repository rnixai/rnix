package ui

import "testing"

func TestFormatTokensBudget(t *testing.T) {
	tests := []struct {
		used      int
		maxTokens int64
		want      string
	}{
		{12345, 0, "12,345"},
		{12345, 50000, "12,345/50,000"},
		{0, 10000, "0/10,000"},
		{50000, 50000, "50,000/50,000"},
	}
	for _, tt := range tests {
		got := FormatTokensBudget(tt.used, tt.maxTokens)
		if got != tt.want {
			t.Errorf("FormatTokensBudget(%d, %d) = %q, want %q", tt.used, tt.maxTokens, got, tt.want)
		}
	}
}

func TestFormatCostBudget(t *testing.T) {
	tests := []struct {
		usedCost float64
		maxCost  float64
		want     string
	}{
		{0, 0, "-"},
		{0.15, 1.0, "$0.15/$1.00"},
		{1.0, 1.0, "$1.00/$1.00"},
		{0.5, 0, "$0.50"},
		{0, 1.0, "$0.00/$1.00"},
	}
	for _, tt := range tests {
		got := FormatCostBudget(tt.usedCost, tt.maxCost)
		if got != tt.want {
			t.Errorf("FormatCostBudget(%f, %f) = %q, want %q", tt.usedCost, tt.maxCost, got, tt.want)
		}
	}
}
