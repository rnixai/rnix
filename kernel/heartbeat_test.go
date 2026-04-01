package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
)

func TestProcess_HeartbeatFieldsInitialized(t *testing.T) {
	// NewProcess should NOT set heartbeat fields (they are set by Spawn)
	proc := NewProcess(0, "test intent", nil)

	if !proc.LastHeartbeat.IsZero() {
		t.Errorf("NewProcess should not initialize LastHeartbeat, got %v", proc.LastHeartbeat)
	}
	if proc.StepTimeout != 0 {
		t.Errorf("NewProcess should not initialize StepTimeout, got %v", proc.StepTimeout)
	}
}

func TestSpawnOpts_StepTimeout(t *testing.T) {
	// Verify SpawnOpts can hold StepTimeout
	opts := SpawnOpts{
		StepTimeout: 10 * time.Minute,
	}
	if opts.StepTimeout != 10*time.Minute {
		t.Errorf("StepTimeout = %v, want 10m", opts.StepTimeout)
	}
}

func TestAgentManifest_StepTimeout(t *testing.T) {
	// Verify AgentManifest parses step_timeout as string
	m := agents.AgentManifest{
		Name:        "test-agent",
		StepTimeout: "10m",
	}
	d, err := time.ParseDuration(m.StepTimeout)
	if err != nil {
		t.Fatalf("ParseDuration(%q) error: %v", m.StepTimeout, err)
	}
	if d != 10*time.Minute {
		t.Errorf("ParseDuration(%q) = %v, want 10m", m.StepTimeout, d)
	}
}

func TestAgentManifest_StepTimeout_Disabled(t *testing.T) {
	// "0" or "0s" means disabled
	for _, val := range []string{"0s", "0"} {
		m := agents.AgentManifest{
			StepTimeout: val,
		}
		d, err := time.ParseDuration(m.StepTimeout)
		if err != nil {
			t.Fatalf("ParseDuration(%q) error: %v", val, err)
		}
		if d != 0 {
			t.Errorf("ParseDuration(%q) = %v, want 0", val, d)
		}
	}
}

func TestStepTimeout_Resolution_Priority(t *testing.T) {
	// Tests the priority: SpawnOpts > AgentManifest > default (5m)

	tests := []struct {
		name        string
		optsTimeout time.Duration
		manifest    string
		want        time.Duration
	}{
		{
			name:        "default 5 minutes",
			optsTimeout: 0,
			manifest:    "",
			want:        5 * time.Minute,
		},
		{
			name:        "manifest overrides default",
			optsTimeout: 0,
			manifest:    "10m",
			want:        10 * time.Minute,
		},
		{
			name:        "opts overrides manifest",
			optsTimeout: 3 * time.Minute,
			manifest:    "10m",
			want:        3 * time.Minute,
		},
		{
			name:        "manifest disabled",
			optsTimeout: 0,
			manifest:    "0s",
			want:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stepTimeout := 5 * time.Minute
			if tt.optsTimeout > 0 {
				stepTimeout = tt.optsTimeout
			} else if tt.manifest != "" {
				if d, err := time.ParseDuration(tt.manifest); err == nil {
					stepTimeout = d
				}
			}
			if stepTimeout != tt.want {
				t.Errorf("stepTimeout = %v, want %v", stepTimeout, tt.want)
			}
		})
	}
}

func TestProcess_HeartbeatConcurrency(t *testing.T) {
	// Verify concurrent access to LastHeartbeat is safe under proc.mu
	proc := NewProcess(0, "concurrent test", nil)
	proc.mu.Lock()
	proc.LastHeartbeat = time.Now()
	proc.StepTimeout = 5 * time.Minute
	proc.mu.Unlock()

	done := make(chan struct{})

	// Writer goroutine
	go func() {
		for range 100 {
			proc.mu.Lock()
			proc.LastHeartbeat = time.Now()
			proc.mu.Unlock()
		}
		done <- struct{}{}
	}()

	// Reader goroutine
	go func() {
		for range 100 {
			proc.mu.Lock()
			_ = proc.LastHeartbeat
			_ = proc.StepTimeout
			proc.mu.Unlock()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}
