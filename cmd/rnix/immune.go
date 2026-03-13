package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/ipc"
)

var immuneCmd = &cobra.Command{
	Use:   "immune",
	Short: "Adaptive immune security management",
}

var immuneStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show immune daemon status and behavior profiles",
	Example: `  rnix immune status          Show immune daemon status
  rnix immune status --json   JSON output`,
	Args: cobra.NoArgs,
	RunE: runImmuneStatus,
}

func init() {
	immuneCmd.AddCommand(immuneStatusCmd)
	rootCmd.AddCommand(immuneCmd)
}

func runImmuneStatus(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": "daemon not available"}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintln(w, "[immune] error: daemon not available (is the daemon running?)")
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	result, err := client.ImmuneStatus()
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[immune] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	// Text output
	statusStr := "stopped"
	if result.Running {
		statusStr = "running"
	}
	fmt.Fprintf(w, "Immune Daemon: %s\n", statusStr)
	fmt.Fprintf(w, "Profiles: %d\n", result.ProfileCount)
	fmt.Fprintf(w, "Active Monitors: %d\n", len(result.ActivePIDs))

	if len(result.Profiles) == 0 {
		fmt.Fprintln(w, "\nNo behavior profiles established.")
		return nil
	}

	fmt.Fprintln(w)

	// Sort profiles by name for stable output
	names := make([]string, 0, len(result.Profiles))
	for name := range result.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintf(w, "%-20s %7s  %16s  %14s  %s\n",
		"AGENT TEMPLATE", "SAMPLES", "TOKEN RATE (avg)", "DURATION (avg)", "LAST UPDATED")

	for _, name := range names {
		p := result.Profiles[name]
		updated := p.LastUpdated.Format("2006-01-02")
		fmt.Fprintf(w, "%-20s %7d  %13.1f tok/s  %12s  %s\n",
			truncate(name, 20),
			p.SampleCount,
			p.TokenRateMean,
			formatDurationMs(p.DurationMeanMs),
			updated,
		)
	}

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "~"
}

func formatDurationMs(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%.1fs", sec)
	}
	d := time.Duration(ms) * time.Millisecond
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1fm", d.Minutes()), "0"), ".")
}
