//go:build windows

package mcp

import "os/exec"

// applyProcessGroupIsolation is a no-op on Windows.
//
// Windows uses Job Objects for child-process containment, which is a different
// mechanism than Unix process groups. Story 48.2 does not implement Job Object
// cleanup; transport.Close on Windows falls back to cmd.Process.Kill (whose
// semantics match the pre-48.2 behaviour). Implementing Job Objects is tracked
// for a future story but explicitly out of scope here.
func applyProcessGroupIsolation(cmd *exec.Cmd) {
	// No-op.
	_ = cmd
}
