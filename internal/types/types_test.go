package types

import "testing"

func TestSignal_Valid(t *testing.T) {
	tests := []struct {
		sig  Signal
		want bool
	}{
		{Signal(0), false},
		{SIGTERM, true},
		{SIGKILL, true},
		{SIGINT, true},
		{SIGPAUSE, true},
		{SIGRESUME, true},
		{Signal(6), false},
		{Signal(-1), false},
		{Signal(100), false},
	}
	for _, tt := range tests {
		if got := tt.sig.Valid(); got != tt.want {
			t.Errorf("Signal(%d).Valid() = %v, want %v", tt.sig, got, tt.want)
		}
	}
}

func TestSignal_String(t *testing.T) {
	tests := []struct {
		sig  Signal
		want string
	}{
		{SIGTERM, "SIGTERM"},
		{SIGKILL, "SIGKILL"},
		{SIGINT, "SIGINT"},
		{SIGPAUSE, "SIGPAUSE"},
		{SIGRESUME, "SIGRESUME"},
		{Signal(0), "Signal(0)"},
		{Signal(99), "Signal(99)"},
	}
	for _, tt := range tests {
		if got := tt.sig.String(); got != tt.want {
			t.Errorf("Signal(%d).String() = %q, want %q", tt.sig, got, tt.want)
		}
	}
}

func TestSignal_IsTermination(t *testing.T) {
	tests := []struct {
		sig  Signal
		want bool
	}{
		{SIGTERM, true},
		{SIGKILL, true},
		{SIGINT, true},
		{SIGPAUSE, false},
		{SIGRESUME, false},
		{Signal(0), false},
	}
	for _, tt := range tests {
		if got := tt.sig.IsTermination(); got != tt.want {
			t.Errorf("Signal(%d).IsTermination() = %v, want %v", tt.sig, got, tt.want)
		}
	}
}

func TestSignal_Blockable(t *testing.T) {
	tests := []struct {
		sig  Signal
		want bool
	}{
		{SIGTERM, true},
		{SIGKILL, false},
		{SIGINT, true},
		{SIGPAUSE, true},
		{SIGRESUME, true},
		{Signal(0), true},   // invalid but blockable by definition
		{Signal(99), true},  // invalid but blockable by definition
	}
	for _, tt := range tests {
		if got := tt.sig.Blockable(); got != tt.want {
			t.Errorf("Signal(%d).Blockable() = %v, want %v", tt.sig, got, tt.want)
		}
	}
}

func TestProcessState_String(t *testing.T) {
	tests := []struct {
		state ProcessState
		want  string
	}{
		{StateCreated, "created"},
		{StateRunning, "running"},
		{StateZombie, "zombie"},
		{StateDead, "dead"},
		{ProcessState(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("ProcessState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
