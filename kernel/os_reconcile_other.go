//go:build !linux

package kernel

import "context"

// osReconcileSupported is false on non-Linux platforms — /proc/<pid>/environ
// (the RNIX_PROC_UUID self-marker source) has no portable equivalent, so
// StartOSReconcileDaemon logs once and returns without scanning.
const osReconcileSupported = false

// defaultOSProcScanner is unused on non-Linux (osReconcileSupported gates it)
// but must exist so os_reconcile.go compiles across platforms.
func defaultOSProcScanner() []osCliProc { return nil }

// defaultOSProcKiller is a no-op on non-Linux.
func defaultOSProcKiller(_ context.Context, _ int) {}
