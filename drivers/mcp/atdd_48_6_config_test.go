//go:build atdd_48_6_red

package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// ATDD 48.6 AC2/AC3/AC4 — per-server config plumbing
//   mount_timeout / request_timeout / max_output_bytes through the full chain:
//   mcp.yaml → MCPServerConfig → ToMCPConfig → vfs.MCPConfig
//                              → TransportConfig → StdioTransport defaults
//
// Story: _bmad-output/implementation-artifacts/48-6-mount-concurrency-per-server-timeout.md
// Phase: 🔴 RED — gated `//go:build atdd_48_6_red`. Run:
//            go test -tags=atdd_48_6_red -race -run TestATDD_48_6 ./drivers/mcp/
//
// Pre-impl RED signal: COMPILE failure — none of the three new fields exist yet
// (MCPServerConfig.{MountTimeout,RequestTimeout,MaxOutputBytes}, vfs.MCPConfig
// runtime durations, TransportConfig.{MountTimeout,RequestTimeout,MaxOutputBytes}).
// That compile error under the tag IS the red signal (48.5 convention).
//
// Injection points (dev-story Task 1.1–1.5):
//   MCPServerConfig.MountTimeout   string  `yaml:"mount_timeout,omitempty"`
//   MCPServerConfig.RequestTimeout string  `yaml:"request_timeout,omitempty"`
//   MCPServerConfig.MaxOutputBytes int64   `yaml:"max_output_bytes,omitempty"`
//   (c MCPServerConfig) ToMCPConfig parses durations → defaults on empty/invalid
//   vfs.MCPConfig.{MountTimeout,RequestTimeout time.Duration; MaxOutputBytes int64}
//   TransportConfig.{MountTimeout,RequestTimeout time.Duration; MaxOutputBytes int64}
//   NewStdioTransport applies defaults to zero-value config fields
//
// Default contract (Story §易错点 5): mount 5s / request 60s / output 4MB (4194304).
// =============================================================================

const (
	wantDefaultMount   = 5 * time.Second
	wantDefaultRequest = 60 * time.Second
	wantDefaultOutput  = int64(4194304) // 4 MB
)

// -----------------------------------------------------------------------------
// _010: ToMCPConfig with no per-server overrides → defaults (5s / 60s / 4MB). (AC2/3/4)
// -----------------------------------------------------------------------------
func TestATDD_48_6_010_ToMCPConfig_Defaults(t *testing.T) {
	cfg := MCPServerConfig{Command: "npx"}.ToMCPConfig("srv")

	if cfg.MountTimeout != wantDefaultMount {
		t.Errorf("MountTimeout default = %v, want %v", cfg.MountTimeout, wantDefaultMount)
	}
	if cfg.RequestTimeout != wantDefaultRequest {
		t.Errorf("RequestTimeout default = %v, want %v", cfg.RequestTimeout, wantDefaultRequest)
	}
	if cfg.MaxOutputBytes != wantDefaultOutput {
		t.Errorf("MaxOutputBytes default = %d, want %d (4MB)", cfg.MaxOutputBytes, wantDefaultOutput)
	}
}

// -----------------------------------------------------------------------------
// _011: ToMCPConfig parses custom duration strings + byte count. (AC2/3/4)
// -----------------------------------------------------------------------------
func TestATDD_48_6_011_ToMCPConfig_CustomValues(t *testing.T) {
	src := MCPServerConfig{
		Command:        "npx",
		MountTimeout:   "15s",
		RequestTimeout: "90s",
		MaxOutputBytes: 8388608, // 8 MB
	}
	cfg := src.ToMCPConfig("playwright")

	if cfg.MountTimeout != 15*time.Second {
		t.Errorf("MountTimeout = %v, want 15s", cfg.MountTimeout)
	}
	if cfg.RequestTimeout != 90*time.Second {
		t.Errorf("RequestTimeout = %v, want 90s", cfg.RequestTimeout)
	}
	if cfg.MaxOutputBytes != 8388608 {
		t.Errorf("MaxOutputBytes = %d, want 8388608 (8MB)", cfg.MaxOutputBytes)
	}
}

// -----------------------------------------------------------------------------
// _012: LoadMCPConfig wires the three new yaml keys into MCPServerConfig. (AC2/3/4)
// -----------------------------------------------------------------------------
func TestATDD_48_6_012_LoadMCPConfig_ParsesNewFields(t *testing.T) {
	dir := t.TempDir()
	content := []byte("servers:\n" +
		"  playwright:\n" +
		"    command: \"npx\"\n" +
		"    mount_timeout: \"15s\"\n" +
		"    request_timeout: \"90s\"\n" +
		"    max_output_bytes: 8388608\n")
	path := filepath.Join(dir, "mcp.yaml")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	g, err := LoadMCPConfig(path)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	pw, ok := g.Servers["playwright"]
	if !ok {
		t.Fatal("playwright server missing from parsed config")
	}
	if pw.MountTimeout != "15s" {
		t.Errorf("yaml mount_timeout = %q, want \"15s\"", pw.MountTimeout)
	}
	if pw.RequestTimeout != "90s" {
		t.Errorf("yaml request_timeout = %q, want \"90s\"", pw.RequestTimeout)
	}
	if pw.MaxOutputBytes != 8388608 {
		t.Errorf("yaml max_output_bytes = %d, want 8388608", pw.MaxOutputBytes)
	}
}

// -----------------------------------------------------------------------------
// _013: NewStdioTransport backfills defaults for zero-value config fields, so a
//        transport built outside the mcp.yaml path (probe / resume) never gets a
//        "0 = instant timeout" surprise. (Story §易错点 5, Task 1.5)
// -----------------------------------------------------------------------------
func TestATDD_48_6_013_NewStdioTransport_DefaultsZeroValues(t *testing.T) {
	tr := NewStdioTransport(TransportConfig{Command: "echo"})

	if tr.config.MountTimeout != wantDefaultMount {
		t.Errorf("transport MountTimeout default = %v, want %v", tr.config.MountTimeout, wantDefaultMount)
	}
	if tr.config.RequestTimeout != wantDefaultRequest {
		t.Errorf("transport RequestTimeout default = %v, want %v", tr.config.RequestTimeout, wantDefaultRequest)
	}
	if tr.config.MaxOutputBytes != wantDefaultOutput {
		t.Errorf("transport MaxOutputBytes default = %d, want %d (4MB)", tr.config.MaxOutputBytes, wantDefaultOutput)
	}
}

// -----------------------------------------------------------------------------
// _014: NewStdioTransport preserves explicitly-set config values (no clobber). (AC2/3/4)
// -----------------------------------------------------------------------------
func TestATDD_48_6_014_NewStdioTransport_PreservesCustomValues(t *testing.T) {
	tr := NewStdioTransport(TransportConfig{
		Command:        "echo",
		MountTimeout:   15 * time.Second,
		RequestTimeout: 90 * time.Second,
		MaxOutputBytes: 8388608,
	})

	if tr.config.MountTimeout != 15*time.Second {
		t.Errorf("MountTimeout = %v, want 15s", tr.config.MountTimeout)
	}
	if tr.config.RequestTimeout != 90*time.Second {
		t.Errorf("RequestTimeout = %v, want 90s", tr.config.RequestTimeout)
	}
	if tr.config.MaxOutputBytes != 8388608 {
		t.Errorf("MaxOutputBytes = %d, want 8388608", tr.config.MaxOutputBytes)
	}
}
