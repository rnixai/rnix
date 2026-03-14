package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	rnix "github.com/rnixai/rnix"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Rnix configuration environment",
	Long:  "Initialize global configuration (~/.config/rnix/) and project configuration (.rnix/) directories.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	// Global initialization
	globalDir, err := config.GlobalDir()
	if err != nil {
		return fmt.Errorf("resolve global config directory: %w", err)
	}

	if _, err := os.Stat(globalDir); os.IsNotExist(err) {
		if err := initGlobal(globalDir); err != nil {
			return fmt.Errorf("global init failed: %w", err)
		}
		fmt.Fprintf(w, "initialized global config: %s\n", globalDir)
	} else {
		fmt.Fprintln(w, "global config already exists, skipping")
	}

	// Project initialization
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	rnixDir := filepath.Join(cwd, ".rnix")
	if _, err := os.Stat(rnixDir); os.IsNotExist(err) {
		if err := initProject(cwd); err != nil {
			return fmt.Errorf("project init failed: %w", err)
		}
		fmt.Fprintf(w, "initialized project config: %s\n", rnixDir)
	} else {
		fmt.Fprintln(w, "project already initialized, skipping")
	}

	return nil
}

func initGlobal(globalDir string) error {
	// Create directory structure
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		return err
	}
	agentsDir := filepath.Join(globalDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}
	skillsDir := filepath.Join(globalDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return err
	}

	// Extract embedded agents and skills
	if err := config.ExtractEmbedded(rnix.EmbeddedAgents, "lib/agents", agentsDir); err != nil {
		return fmt.Errorf("extract agents: %w", err)
	}
	if err := config.ExtractEmbedded(rnix.EmbeddedSkills, "lib/skills", skillsDir); err != nil {
		return fmt.Errorf("extract skills: %w", err)
	}

	// Generate default providers.yaml
	if err := writeDefaultProviders(globalDir); err != nil {
		return fmt.Errorf("write providers.yaml: %w", err)
	}

	// Generate default config.yaml
	if err := writeDefaultConfig(globalDir); err != nil {
		return fmt.Errorf("write config.yaml: %w", err)
	}

	return nil
}

func initProject(projectDir string) error {
	// Create directory structure
	for _, sub := range []string{".rnix/agents", ".rnix/skills", ".rnix/data"} {
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			return err
		}
	}

	// Generate project config.yaml (only comments)
	configPath := filepath.Join(projectDir, ".rnix", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		content := "# Rnix project configuration\n# See documentation for available options\n"
		if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func writeDefaultProviders(dir string) error {
	providersPath := filepath.Join(dir, "providers.yaml")
	if _, err := os.Stat(providersPath); err == nil {
		return nil // Skip existing
	}

	cfg := llm.DefaultProvidersConfig()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(providersPath, data, 0o644)
}

func writeDefaultConfig(dir string) error {
	configPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return nil // Skip existing
	}

	content := "# Rnix global configuration\n# See documentation for available options\n"
	return os.WriteFile(configPath, []byte(content), 0o644)
}
