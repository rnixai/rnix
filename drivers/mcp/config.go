package mcp

import (
	"fmt"
	"maps"
	"os"
	"strings"
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
	// minMaxOutputBytes is the floor for a positive max_output_bytes (Story 48.6
	// [Review][Patch] P4). A value of 0 means "use default"; any positive value
	// below this would truncate every result down to an unusable fragment and is
	// almost certainly a misconfiguration (e.g. forgetting the unit is raw bytes,
	// not KiB/MiB). 1 KiB comfortably holds a minimal JSON envelope.
	minMaxOutputBytes = int64(1024)
)

// MCPServerConfig describes a single MCP server's connection parameters.
type MCPServerConfig struct {
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	TransportType string            `yaml:"transport_type"`         // "stdio" (default) | "http"
	Type          string            `yaml:"type,omitempty"`         // alias for transport_type (Story 59.1); transport_type wins when both set
	Instructions  string            `yaml:"instructions,omitempty"` // usage instructions injected into system prompt

	// Streamable HTTP transport (Story 59.1 / Epic 59). Required when the
	// resolved transport type is "http". URL is the single MCP endpoint; Headers
	// are sent verbatim on every request (Bearer / API key / custom) and support
	// ${ENV} interpolation resolved at transport build time (decision D3).
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`

	// Per-server timeout / output knobs (Story 48.6 FR-48-S8). Durations are
	// stored as strings and parsed with time.ParseDuration (aligning with
	// agent.Manifest.StepTimeout, kernel/spawn.go) rather than a custom yaml
	// duration type. Empty / invalid → defaults applied in ToMCPConfig.
	MountTimeout   string `yaml:"mount_timeout,omitempty"`   // e.g. "15s" for Playwright cold start
	RequestTimeout string `yaml:"request_timeout,omitempty"` // e.g. "90s"
	MaxOutputBytes int64  `yaml:"max_output_bytes,omitempty"`
}

// ResolvedTransportType returns the effective transport type, honoring the
// `type` alias (Story 59.1 / decision D2) and normalizing to lower case so
// `http`/`HTTP`/`Http` all match. `transport_type` wins when both are set; an
// empty result means "stdio" (the default) to downstream callers.
func (c MCPServerConfig) ResolvedTransportType() string {
	v := c.TransportType
	if v == "" {
		v = c.Type
	}
	return strings.ToLower(strings.TrimSpace(v))
}

// Validate checks that the duration fields parse. Called from the LoadMCPConfig
// validation phase (kernel/init.go) so a typo in mount_timeout / request_timeout
// fails fast at config-load time alongside the existing command / transport_type
// checks, rather than being silently swallowed into a default by ToMCPConfig
// (Story 48.6 Task 1.3).
func (c MCPServerConfig) Validate() error {
	// Story 59.1 — transport-conditional connection requirements. Centralized
	// here (runs for every LoadMCPConfig caller: daemon init, rnix check,
	// compose preflight) so http/stdio configs fail fast and consistently.
	// "" defaults to stdio.
	switch tt := c.ResolvedTransportType(); tt {
	case "", "stdio":
		if c.Command == "" {
			return fmt.Errorf("command is required for stdio transport")
		}
	case "http":
		if c.URL == "" {
			return fmt.Errorf("url is required for http transport")
		}
	default:
		return fmt.Errorf("transport_type=%q unsupported (only stdio, http)", tt)
	}

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
	// Story 48.6 [Review][Patch] P4 — reject a positive-but-tiny max_output_bytes
	// before it truncates every result into garbage. 0 = use default.
	if c.MaxOutputBytes != 0 && c.MaxOutputBytes < minMaxOutputBytes {
		return fmt.Errorf("max_output_bytes %d too small (minimum %d bytes; use 0 for default %d)",
			c.MaxOutputBytes, minMaxOutputBytes, defaultMaxOutputBytes)
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
		TransportType:  c.ResolvedTransportType(),
		URL:            c.URL,
		Headers:        c.Headers,
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

// MergeGlobalConfig returns a NEW MCPGlobalConfig whose Servers are base's
// servers overlaid by override's (override wins on name collision, mirroring the
// project-overrides-global semantics used for providers.yaml in
// ipc/server_spawn.go:resolveProjectContext). Neither input is mutated.
//
//   - both nil           → nil
//   - one nil            → a shallow copy of the other (fresh map, same entries)
//   - both non-nil       → base entries + override entries, override replacing
//     any same-named base entry
//
// Used by the daemon's project-context spawn path so an agent's declared `mcp:`
// servers resolve against global ~/.config/rnix/mcp.yaml even when the project
// has no .rnix/mcp.yaml of its own (the EchoMatrix case).
func MergeGlobalConfig(base, override *MCPGlobalConfig) *MCPGlobalConfig {
	if base == nil && override == nil {
		return nil
	}
	merged := &MCPGlobalConfig{Servers: make(map[string]MCPServerConfig)}
	if base != nil {
		maps.Copy(merged.Servers, base.Servers)
	}
	if override != nil {
		maps.Copy(merged.Servers, override.Servers)
	}
	return merged
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

	// Story 48.6 [Review][Patch] P3 — validate every server here so ALL callers
	// (rnix check / compose / preflight, not just daemon init) fail fast on a
	// malformed mount_timeout / request_timeout / max_output_bytes instead of
	// silently defaulting it in ToMCPConfig.
	for name, server := range cfg.Servers {
		if err := server.Validate(); err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", name, err)
		}
	}

	return &cfg, nil
}
