package ui

import (
	"testing"
	"time"
)

func TestFormatRelativeTime(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero time", time.Time{}, "-"},
		{"just now", time.Now(), "0s ago"},
		{"30 seconds ago", time.Now().Add(-30 * time.Second), "30s ago"},
		{"2 minutes ago", time.Now().Add(-2 * time.Minute), "2m ago"},
		{"1 hour ago", time.Now().Add(-1 * time.Hour), "1h ago"},
		{"3 hours ago", time.Now().Add(-3 * time.Hour), "3h ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRelativeTime(tt.t)
			if got != tt.want {
				t.Errorf("FormatRelativeTime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsStale(t *testing.T) {
	tests := []struct {
		name      string
		heartbeat time.Time
		timeout   time.Duration
		want      bool
	}{
		{
			name:      "disabled timeout (0)",
			heartbeat: time.Now().Add(-10 * time.Minute),
			timeout:   0,
			want:      false,
		},
		{
			name:      "zero heartbeat",
			heartbeat: time.Time{},
			timeout:   5 * time.Minute,
			want:      false,
		},
		{
			name:      "not stale",
			heartbeat: time.Now().Add(-1 * time.Minute),
			timeout:   5 * time.Minute,
			want:      false,
		},
		{
			name:      "stale",
			heartbeat: time.Now().Add(-10 * time.Minute),
			timeout:   5 * time.Minute,
			want:      true,
		},
		{
			name:      "exact boundary",
			heartbeat: time.Now().Add(-5*time.Minute - 1*time.Second),
			timeout:   5 * time.Minute,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStale(tt.heartbeat, tt.timeout)
			if got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}
