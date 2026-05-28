//go:build unix && !linux

package mcp

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// ATDD 48.2 AC6 (非 Linux Unix 分支) — Setpgid 必须设置；不引用 Pdeathsig 字段
//
// Story §易错点 #1: SysProcAttr.Pdeathsig exists only on Linux/FreeBSD. To
// keep this test buildable on darwin / openbsd / netbsd, we MUST NOT reference
// the Pdeathsig field. The Linux-specific Pdeathsig assertion lives in
// transport_pgrp_linux_test.go (`//go:build linux`).
//
// On FreeBSD the production code may also set Pdeathsig, but the build-tag
// design in this Story groups FreeBSD with the non-Linux Unix branch
// (transport_pgrp_other_unix.go) for simplicity — see Dev Notes §平台覆盖矩阵.
// =============================================================================

func TestATDD_48_2_006b_OtherUnixSetpgidOnly(t *testing.T) {
	t.Skip("RED: 待 Task 1.2 (drivers/mcp/transport_pgrp_other_unix.go) + Task 4.1 落地")

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
	// We intentionally DO NOT reference attr.Pdeathsig — the field doesn't
	// exist on darwin/openbsd/netbsd. Linux-specific assertion lives in
	// transport_pgrp_linux_test.go (different build tag).
}
