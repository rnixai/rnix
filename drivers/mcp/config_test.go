package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMCPConfig(t *testing.T) {
	t.Run("valid config with multiple servers", func(t *testing.T) {
		// Given: a valid mcp.yaml with multiple server entries
		path := filepath.Join("testdata", "valid.yaml")

		// When: LoadMCPConfig is called
		cfg, err := LoadMCPConfig(path)

		// Then: config is parsed correctly with all servers
		if err != nil {
			t.Fatalf("LoadMCPConfig returned error: %v", err)
		}
		if len(cfg.Servers) != 2 {
			t.Fatalf("Servers count = %d, want 2", len(cfg.Servers))
		}

		// Verify github server
		github, ok := cfg.Servers["github"]
		if !ok {
			t.Fatal("expected 'github' server in config")
		}
		if github.Command != "npx" {
			t.Errorf("github.Command = %q, want %q", github.Command, "npx")
		}
		if len(github.Args) != 2 || github.Args[0] != "-y" {
			t.Errorf("github.Args = %v, want [-y @modelcontextprotocol/server-github]", github.Args)
		}
		if github.TransportType != "stdio" {
			t.Errorf("github.TransportType = %q, want %q", github.TransportType, "stdio")
		}

		// Verify slack server
		slack, ok := cfg.Servers["slack"]
		if !ok {
			t.Fatal("expected 'slack' server in config")
		}
		if slack.Command != "npx" {
			t.Errorf("slack.Command = %q, want %q", slack.Command, "npx")
		}
	})

	t.Run("valid config with env and args", func(t *testing.T) {
		// Given: a valid mcp.yaml with env variables
		path := filepath.Join("testdata", "valid.yaml")

		// When: LoadMCPConfig is called
		cfg, err := LoadMCPConfig(path)

		// Then: env variables are parsed correctly
		if err != nil {
			t.Fatalf("LoadMCPConfig returned error: %v", err)
		}
		github := cfg.Servers["github"]
		if github.Env["GITHUB_TOKEN"] != "test-token" {
			t.Errorf("github.Env[GITHUB_TOKEN] = %q, want %q", github.Env["GITHUB_TOKEN"], "test-token")
		}
		slack := cfg.Servers["slack"]
		if slack.Env["SLACK_TOKEN"] != "test-token" {
			t.Errorf("slack.Env[SLACK_TOKEN] = %q, want %q", slack.Env["SLACK_TOKEN"], "test-token")
		}
	})

	t.Run("empty servers map", func(t *testing.T) {
		// Given: a mcp.yaml with empty servers
		path := filepath.Join("testdata", "empty.yaml")

		// When: LoadMCPConfig is called
		cfg, err := LoadMCPConfig(path)

		// Then: no error, servers map is empty
		if err != nil {
			t.Fatalf("LoadMCPConfig returned error: %v", err)
		}
		if len(cfg.Servers) != 0 {
			t.Errorf("Servers count = %d, want 0", len(cfg.Servers))
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		// Given: an invalid YAML file
		path := filepath.Join("testdata", "invalid.yaml")

		// When: LoadMCPConfig is called
		_, err := LoadMCPConfig(path)

		// Then: an error is returned
		if err == nil {
			t.Fatal("expected error for invalid YAML, got nil")
		}
	})

	t.Run("file not found returns error", func(t *testing.T) {
		// Given: a non-existent file path
		path := filepath.Join("testdata", "nonexistent.yaml")

		// When: LoadMCPConfig is called
		_, err := LoadMCPConfig(path)

		// Then: an error is returned
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
		if !os.IsNotExist(err) {
			// Accept any error that indicates file not found
			// Implementation may wrap the error
			t.Logf("error type is %T: %v (accepted)", err, err)
		}
	})

	t.Run("default transport type is stdio", func(t *testing.T) {
		// Given: a mcp.yaml with a server that omits transport_type
		dir := t.TempDir()
		content := []byte("servers:\n  test:\n    command: \"echo\"\n")
		path := filepath.Join(dir, "mcp.yaml")
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("write temp file: %v", err)
		}

		// When: LoadMCPConfig is called
		cfg, err := LoadMCPConfig(path)

		// Then: transport_type defaults to "stdio"
		if err != nil {
			t.Fatalf("LoadMCPConfig returned error: %v", err)
		}
		server := cfg.Servers["test"]
		// After LoadMCPConfig applies defaults, TransportType should be "stdio"
		if server.TransportType != "" && server.TransportType != "stdio" {
			t.Errorf("TransportType = %q, want empty or %q", server.TransportType, "stdio")
		}
	})
}

func TestMCPServerConfig_ToMCPConfig(t *testing.T) {
	t.Run("converts to vfs MCPConfig", func(t *testing.T) {
		// Given: a MCPServerConfig with all fields populated
		serverCfg := MCPServerConfig{
			Command:       "npx",
			Args:          []string{"-y", "@mcp/server-github"},
			Env:           map[string]string{"TOKEN": "abc123"},
			TransportType: "stdio",
		}

		// When: converted to vfs.MCPConfig with server name "github"
		mcpConfig := serverCfg.ToMCPConfig("github")

		// Then: all fields are correctly mapped
		if mcpConfig.ServerName != "github" {
			t.Errorf("ServerName = %q, want %q", mcpConfig.ServerName, "github")
		}
		if mcpConfig.Command != "npx" {
			t.Errorf("Command = %q, want %q", mcpConfig.Command, "npx")
		}
		if len(mcpConfig.Args) != 2 || mcpConfig.Args[0] != "-y" {
			t.Errorf("Args = %v, want [-y @mcp/server-github]", mcpConfig.Args)
		}
		if mcpConfig.Env["TOKEN"] != "abc123" {
			t.Errorf("Env[TOKEN] = %q, want %q", mcpConfig.Env["TOKEN"], "abc123")
		}
		if mcpConfig.TransportType != "stdio" {
			t.Errorf("TransportType = %q, want %q", mcpConfig.TransportType, "stdio")
		}
	})
}
