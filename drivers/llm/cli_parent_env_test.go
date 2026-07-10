package llm

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestConfigureCommandRnixParentEnv_AppendsParentAndDepth(t *testing.T) {
	cmd := exec.Command("echo")
	cmd.Env = []string{"PATH=/bin"}

	configureCommandRnixParentEnv(cmd, LLMRequest{CallerPID: 42, CallerDepth: 3, CallerUUID: "01890000-0000-7000-8000-000000000abc"})

	if !slices.Contains(cmd.Env, "RNIX_PARENT_PID=42") {
		t.Fatalf("RNIX_PARENT_PID missing from env: %v", cmd.Env)
	}
	if !slices.Contains(cmd.Env, "RNIX_SPAWN_DEPTH=3") {
		t.Fatalf("RNIX_SPAWN_DEPTH missing from env: %v", cmd.Env)
	}
	if !slices.Contains(cmd.Env, "RNIX_PROC_UUID=01890000-0000-7000-8000-000000000abc") {
		t.Fatalf("RNIX_PROC_UUID missing from env: %v", cmd.Env)
	}
}

// TestConfigureCommandRnixParentEnv_UUIDOnlyTopLevel guards the Story 66.5
// requirement that a top-level process (Depth==0, and in a future where PID
// semantics change also CallerPID==0) is still marked with RNIX_PROC_UUID.
// The first-line guard must not early-return when only CallerUUID is set.
func TestConfigureCommandRnixParentEnv_UUIDOnlyTopLevel(t *testing.T) {
	cmd := exec.Command("echo")

	configureCommandRnixParentEnv(cmd, LLMRequest{CallerUUID: "01890000-0000-7000-8000-000000000abc"})

	if !slices.Contains(cmd.Env, "RNIX_PROC_UUID=01890000-0000-7000-8000-000000000abc") {
		t.Fatalf("RNIX_PROC_UUID missing when only CallerUUID set: %v", cmd.Env)
	}
	// PID/Depth zero ⇒ their env vars must NOT be injected.
	if slices.ContainsFunc(cmd.Env, func(s string) bool { return strings.HasPrefix(s, "RNIX_PARENT_PID=") }) {
		t.Fatalf("RNIX_PARENT_PID should be absent when CallerPID==0: %v", cmd.Env)
	}
}

func TestConfigureCommandRnixParentEnv_ZeroDoesNotMutateEnv(t *testing.T) {
	cmd := exec.Command("echo")

	configureCommandRnixParentEnv(cmd, LLMRequest{})

	if cmd.Env != nil {
		t.Fatalf("zero caller process info should not set env, got %v", cmd.Env)
	}
}

func TestConfigureCommandRnixParentEnv_AppendedValueOverridesExisting(t *testing.T) {
	cmd := exec.Command("echo")
	cmd.Env = []string{"RNIX_PARENT_PID=old", "RNIX_SPAWN_DEPTH=old"}

	configureCommandRnixParentEnv(cmd, LLMRequest{CallerPID: 99, CallerDepth: 4})

	if got := lastEnvValue(cmd.Env, "RNIX_PARENT_PID="); got != "RNIX_PARENT_PID=99" {
		t.Fatalf("last RNIX_PARENT_PID = %q, want RNIX_PARENT_PID=99 (env=%v)", got, cmd.Env)
	}
	if got := lastEnvValue(cmd.Env, "RNIX_SPAWN_DEPTH="); got != "RNIX_SPAWN_DEPTH=4" {
		t.Fatalf("last RNIX_SPAWN_DEPTH = %q, want RNIX_SPAWN_DEPTH=4 (env=%v)", got, cmd.Env)
	}
}

func lastEnvValue(env []string, prefix string) string {
	var out string
	for _, v := range env {
		if strings.HasPrefix(v, prefix) {
			out = v
		}
	}
	return out
}
