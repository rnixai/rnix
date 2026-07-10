//go:build linux

package llm

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestATDD_66_5_ConfigureCommandGrace_LinuxPdeathsig asserts the Linux-only
// property that the child is asked to die with the daemon (Pdeathsig=SIGKILL),
// the缺口② backstop for daemon crash / kill -9 (Dev Notes 拍板 4). Split into a
// linux build-tagged file because darwin/BSD syscall.SysProcAttr has no
// Pdeathsig field.
func TestATDD_66_5_ConfigureCommandGrace_LinuxPdeathsig(t *testing.T) {
	cmd := exec.Command("true")
	configureCommandGrace(cmd, 1)

	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr set")
	}
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("expected Pdeathsig=SIGKILL, got %v", cmd.SysProcAttr.Pdeathsig)
	}
}
