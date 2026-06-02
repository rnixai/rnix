package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show active daemon configuration",
	RunE:  runConfigShow,
}

func init() {
	configCmd.AddCommand(configShowCmd)
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()

	sockPath := ipc.SocketPath()
	client, err := ipc.Dial(sockPath)
	if err != nil {
		return configShowFallback(w)
	}
	defer client.Close()

	ds, err := client.DaemonStatus()
	if err != nil {
		return configShowFallback(w)
	}

	fmt.Fprintf(w, "Feature Profile: %s\n", ds.FeatureProfile)
	printFlags(w, ds.FeatureFlags)
	return nil
}

func configShowFallback(w io.Writer) error {
	globalDir, err := config.GlobalDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	configPath := filepath.Join(globalDir, "config.yaml")

	flags, profile, _ := config.ResolveFeatures(configPath)
	fmt.Fprintf(w, "Feature Profile: %s (from config, daemon not running)\n", profile)
	printFlags(w, map[string]bool{
		"planning":       flags.Planning,
		"replan":         flags.Replan,
		"specialize":     flags.Specialize,
		"discover_skill": flags.DiscoverSkill,
		"spawn":          flags.Spawn,
		"diff_memory":    flags.DiffMemory,
		"stem_matcher":   flags.StemMatcher,
		"immune":         flags.Immune,
		"compaction":     flags.Compaction,
	})
	return nil
}

func printFlags(w io.Writer, flags map[string]bool) {
	keys := make([]string, 0, len(flags))
	for k := range flags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %-15s %v\n", k+":", flags[k])
	}
}
