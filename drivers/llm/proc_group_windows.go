//go:build windows

package llm

import "os/exec"

// setProcGroupAttr is a no-op on Windows.
//
// Windows uses Job Objects for child-process containment, a different mechanism
// than Unix process groups. Story 66.5 does not implement Job Object cleanup
// (mirrors the drivers/mcp Windows counterpart); group SIGTERM/SIGKILL degrade
// to leader-only kill via cmd.Process. Job Objects are explicitly out of scope.
func setProcGroupAttr(cmd *exec.Cmd) {
	// No-op.
	_ = cmd
}
