//go:build windows

package mcp

import "os/exec"

// sendGroupSIGTERM is a no-op on Windows.
//
// Windows lacks a direct equivalent of `kill -SIGTERM -pgid`. A full
// equivalent would require Job Objects or the (poorly-documented)
// GenerateConsoleCtrlEvent. Story 48.2 explicitly defers Windows support;
// this means the SIGTERM "graceful window" simply skips on Windows and the
// transport jumps straight to the SIGKILL fallback below.
func sendGroupSIGTERM(cmd *exec.Cmd) error {
	_ = cmd
	return nil
}

// sendGroupSIGKILL terminates the child process (and any of its descendants
// that share a handle with it through the default cmd.Process setup).
// cmd.Process.Kill on Windows posts an Exit event to the underlying handle,
// which mirrors SIGKILL semantics but does NOT cascade to grandchildren. A
// future story may layer Job Objects on top for true tree cleanup.
func sendGroupSIGKILL(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
