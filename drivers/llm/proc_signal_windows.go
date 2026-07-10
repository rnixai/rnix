//go:build windows

package llm

import (
	"os"
	"os/exec"
	"syscall"
)

// groupCancelSIGTERM degrades to a leader-only signal on Windows. Windows lacks
// a direct `kill -SIGTERM -pgid` equivalent (a full version would need Job
// Objects or GenerateConsoleCtrlEvent), which Story 66.5 explicitly defers.
// cmd.Process.Signal(SIGTERM) on Windows does not cascade to grandchildren; the
// os-reconcile loop is Linux-only, so Windows residual cleanup is out of scope.
func groupCancelSIGTERM(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}

// reapCommandGroup terminates the leader process on Windows (cmd.Process.Kill
// posts an Exit event to the underlying handle, mirroring SIGKILL semantics but
// NOT cascading to grandchildren). A future story may layer Job Objects for
// true tree cleanup.
func reapCommandGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
