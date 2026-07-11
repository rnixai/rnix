//go:build unix

package llm

import (
	"os/exec"
	"testing"
)

// Story 66.5 — QA-generated edge test (bmad-qa-generate-e2e-tests).
//
// configureCommandGrace(cmd, graceSec) falls back to DefaultGracePeriod when
// graceSec<=0 (driver.go). The dev-story ATDD only exercises the positive path
// (grace=1 ⇒ WaitDelay=1s); this covers the fallback branch that governs the
// real production default whenever providers.yaml omits grace_sec, ensuring
// group isolation + the Cancel hook are still installed under the default grace.
func TestATDD_66_5_ConfigureCommandGrace_DefaultsOnNonPositive(t *testing.T) {
	for _, graceSec := range []int{0, -5} {
		cmd := exec.Command("true")
		configureCommandGrace(cmd, graceSec)

		if cmd.WaitDelay != DefaultGracePeriod {
			t.Fatalf("grace=%d: WaitDelay=%s, want DefaultGracePeriod=%s", graceSec, cmd.WaitDelay, DefaultGracePeriod)
		}
		// Group isolation + Cancel hook must still be installed regardless of grace.
		if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
			t.Fatalf("grace=%d: Setpgid must still be set under default grace", graceSec)
		}
		if cmd.Cancel == nil {
			t.Fatalf("grace=%d: Cancel hook must still be installed under default grace", graceSec)
		}
	}
}
