package mcp

import (
	"os"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/vfs"
)

// MCPServerConfig describes a single MCP server's connection parameters.
type MCPServerConfig struct {
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	TransportType string            `yaml:"transport_type"` // "stdio" (default)
}

// ToMCPConfig converts an MCPServerConfig to a vfs.MCPConfig with the given server name.
func (c MCPServerConfig) ToMCPConfig(name string) vfs.MCPConfig {
	return vfs.MCPConfig{
		ServerName:    name,
		Command:       c.Command,
		Args:          c.Args,
		Env:           c.Env,
		TransportType: c.TransportType,
	}
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
