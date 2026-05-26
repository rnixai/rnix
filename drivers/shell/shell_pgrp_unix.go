//go:build unix

package shell

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// applyProcessGroupIsolation configures cmd to run in its own process group
// and installs a Cancel hook that SIGKILLs the whole group. Without this,
// commands that fork background daemons inheriting stdout/stderr pipes can
// deadlock io.Copy → Cmd.Wait (golang/go#23019). Unix-only — Windows uses a
// different mechanism (Job Objects, not modeled here).
func applyProcessGroupIsolation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			// Process group already gone (raced with natural exit) — treat as
			// success. Per os/exec.Cmd docs, Cancel must wrap os.ErrProcessDone
			// in this case so Wait does not surface ESRCH as the run error.
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
}
