//go:build linux

package mcp

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// ATDD 48.2 AC6 (Linux 分支) — applyProcessGroupIsolation sets Setpgid AND Pdeathsig
//
// Story §易错点 #1: Pdeathsig is Linux+FreeBSD only. The non-Linux Unix branch
// lives in transport_pgrp_other_unix_test.go and asserts ONLY Setpgid.
//
// Compile note: this file directly accesses SysProcAttr.Pdeathsig, which only
// exists on Linux/FreeBSD. With `//go:build linux` the field is always
// available; on darwin / OpenBSD this file is excluded from the build.
// =============================================================================

func TestATDD_48_2_006a_LinuxPdeathsigConfigured(t *testing.T) {
	transport := NewStdioTransport(TransportConfig{
		Command:       "bash",
		Args:          []string{"-c", mockMCPServer},
		TimeoutMillis: 3000,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer transport.Close()

	if transport.cmd == nil {
		t.Fatal("transport.cmd nil after Connect")
	}
	attr := transport.cmd.SysProcAttr
	if attr == nil {
		t.Fatal("cmd.SysProcAttr nil — applyProcessGroupIsolation not invoked before cmd.Start")
	}
	if !attr.Setpgid {
		t.Errorf("SysProcAttr.Setpgid = false, want true (process group leader required)")
	}
	if attr.Pdeathsig != syscall.SIGKILL {
		t.Errorf("SysProcAttr.Pdeathsig = %v, want SIGKILL (daemon-异常退出兜底, AC6 Linux invariant)",
			attr.Pdeathsig)
	}
}
