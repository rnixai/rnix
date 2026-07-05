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

	configureCommandRnixParentEnv(cmd, LLMRequest{CallerPID: 42, CallerDepth: 3})

	if !slices.Contains(cmd.Env, "RNIX_PARENT_PID=42") {
		t.Fatalf("RNIX_PARENT_PID missing from env: %v", cmd.Env)
	}
	if !slices.Contains(cmd.Env, "RNIX_SPAWN_DEPTH=3") {
		t.Fatalf("RNIX_SPAWN_DEPTH missing from env: %v", cmd.Env)
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
