package main

import (
	"fmt"
	"io"
	"os"
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

	warnFeatureProfileEnvIgnored(ds.FeatureProfile)
	fmt.Fprintf(w, "Feature Profile: %s\n", ds.FeatureProfile)
	printFlags(w, ds.FeatureFlags)
	return nil
}

// warnFeatureProfileEnvIgnored prints a stderr warning when RNIX_FEATURE_PROFILE
// is set in the CLI environment but the running daemon resolved a different
// profile. ResolveFeatures reads the variable inside the daemon process at
// startup, so a per-CLI override never crosses the IPC boundary — surface that
// instead of letting it be silently ignored (it only appears to work when the
// CLI happens to auto-start the daemon, which inherits the CLI environment).
func warnFeatureProfileEnvIgnored(daemonProfile string) {
	envProfile := os.Getenv("RNIX_FEATURE_PROFILE")
	if envProfile == "" || envProfile == daemonProfile {
		return
	}
	fmt.Fprintf(os.Stderr, "[config] warn: RNIX_FEATURE_PROFILE=%q has no effect on the running daemon (profile %q) — the variable is read once at daemon startup; run `rnix daemon stop` and retry to apply\n", envProfile, daemonProfile)
}

// checkFeatureProfileEnv fetches the daemon profile and delegates to
// warnFeatureProfileEnvIgnored. The extra DaemonStatus round-trip is only paid
// when the env var is actually set.
func checkFeatureProfileEnv(client *ipc.Client) {
	if os.Getenv("RNIX_FEATURE_PROFILE") == "" {
		return
	}
	if ds, err := client.DaemonStatus(); err == nil {
		warnFeatureProfileEnvIgnored(ds.FeatureProfile)
	}
}

func configShowFallback(w io.Writer) error {
	globalDir, err := config.GlobalDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	configPath := filepath.Join(globalDir, "config.yaml")

	flags, profile, warnings := config.ResolveFeatures(configPath)
	for _, warn := range warnings {
		fmt.Fprintf(os.Stderr, "[config] warn: %s\n", warn)
	}
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
