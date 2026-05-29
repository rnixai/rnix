package mcp

import (
	"fmt"
	"os"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/vfs"
)

// Per-server config defaults (Story 48.6 §易错点 5). These are applied in TWO
// layers — here in ToMCPConfig (the mcp.yaml → registry path) and again in
// NewStdioTransport (the probe / resume path that may bypass mcp.yaml) — so a
// zero value never degrades into "0 = instant timeout / no output".
//
//   - defaultMountTimeout supersedes the old hardcoded 500ms (Story 9.x NFR25),
//     which Playwright Chromium's ~15s cold start invalidated (Story 48.6 §本质).
//   - defaultRequestTimeout bounds a single tools/call read.
//   - defaultMaxOutputBytes caps a tools/call result before it floods context.
const (
	defaultMountTimeout   = 5 * time.Second
	defaultRequestTimeout = 60 * time.Second
	defaultMaxOutputBytes = int64(4 << 20) // 4 MiB = 4194304
)

// MCPServerConfig describes a single MCP server's connection parameters.
type MCPServerConfig struct {
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	TransportType string            `yaml:"transport_type"`         // "stdio" (default)
	Instructions  string            `yaml:"instructions,omitempty"` // usage instructions injected into system prompt

	// Per-server timeout / output knobs (Story 48.6 FR-48-S8). Durations are
	// stored as strings and parsed with time.ParseDuration (aligning with
	// agent.Manifest.StepTimeout, kernel/spawn.go) rather than a custom yaml
	// duration type. Empty / invalid → defaults applied in ToMCPConfig.
	MountTimeout   string `yaml:"mount_timeout,omitempty"`   // e.g. "15s" for Playwright cold start
	RequestTimeout string `yaml:"request_timeout,omitempty"` // e.g. "90s"
	MaxOutputBytes int64  `yaml:"max_output_bytes,omitempty"`
}

// Validate checks that the duration fields parse. Called from the LoadMCPConfig
// validation phase (kernel/init.go) so a typo in mount_timeout / request_timeout
// fails fast at config-load time alongside the existing command / transport_type
// checks, rather than being silently swallowed into a default by ToMCPConfig
// (Story 48.6 Task 1.3).
func (c MCPServerConfig) Validate() error {
	if c.MountTimeout != "" {
		if _, err := time.ParseDuration(c.MountTimeout); err != nil {
			return fmt.Errorf("invalid mount_timeout %q: %w", c.MountTimeout, err)
		}
	}
	if c.RequestTimeout != "" {
		if _, err := time.ParseDuration(c.RequestTimeout); err != nil {
			return fmt.Errorf("invalid request_timeout %q: %w", c.RequestTimeout, err)
		}
	}
	return nil
}

// ToMCPConfig converts an MCPServerConfig to a vfs.MCPConfig with the given
// server name. The duration strings are parsed into time.Duration and the
// per-server knobs fall back to package defaults when empty / invalid / non-
// positive (Story 48.6 Task 1.3).
func (c MCPServerConfig) ToMCPConfig(name string) vfs.MCPConfig {
	return vfs.MCPConfig{
		ServerName:     name,
		Command:        c.Command,
		Args:           c.Args,
		Env:            c.Env,
		TransportType:  c.TransportType,
		Instructions:   c.Instructions,
		MountTimeout:   parseDurationOr(c.MountTimeout, defaultMountTimeout),
		RequestTimeout: parseDurationOr(c.RequestTimeout, defaultRequestTimeout),
		MaxOutputBytes: int64OrDefault(c.MaxOutputBytes, defaultMaxOutputBytes),
	}
}

// parseDurationOr parses a time.ParseDuration string, falling back to def on an
// empty string, a parse error, or a non-positive value.
func parseDurationOr(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// int64OrDefault returns v when positive, else def.
func int64OrDefault(v, def int64) int64 {
	if v <= 0 {
		return def
	}
	return v
}

// MCPGlobalConfig holds the global MCP configuration loaded from mcp.yaml.
type MCPGlobalConfig struct {
	Servers map[string]MCPServerConfig `yaml:"servers"`
}

// LoadMCPConfig reads and parses a global MCP configuration file.
func LoadMCPConfig(path string) (*MCPGlobalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg MCPGlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Servers == nil {
		cfg.Servers = make(map[string]MCPServerConfig)
	}

	return &cfg, nil
}
