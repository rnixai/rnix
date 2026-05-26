//go:build windows

package shell

import "os/exec"

// applyProcessGroupIsolation is a no-op on Windows. The pipe-leak deadlock
// fixed for Unix (golang/go#23019) manifests through inherited stdout/stderr
// fd inheritance; on Windows the equivalent containment is Job Objects, which
// rnix does not currently model. WaitDelay still provides a partial backstop.
func applyProcessGroupIsolation(_ *exec.Cmd) {}
