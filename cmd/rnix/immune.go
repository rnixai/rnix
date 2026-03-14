package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
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

var immuneResumeCmd = &cobra.Command{
	Use:   "resume <pid>",
	Short: "Resume a suspended process",
	Args:  cobra.ExactArgs(1),
	RunE:  runImmuneResume,
}

var immuneSimilarityCmd = &cobra.Command{
	Use:   "similarity [agent-name]",
	Short: "Show capability similarity for an agent",
	Example: `  rnix immune similarity code-analyst    Show similar agents
  rnix immune similarity --json code-analyst  JSON output`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImmuneSimilarity,
}

func init() {
	immuneCmd.AddCommand(immuneStatusCmd)
	immuneCmd.AddCommand(immuneResumeCmd)
	immuneCmd.AddCommand(immuneSimilarityCmd)
	rootCmd.AddCommand(immuneCmd)
}

func runImmuneResume(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": "invalid PID"}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[immune] error: invalid PID %q\n", args[0])
		}
		exitCode = 1
		return nil
	}

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

	result, err := client.ImmuneResume(pidNum)
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

	fmt.Fprintln(w, result.Message)
	return nil
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
		statusStr = fmt.Sprintf("running (uptime: %s)", formatUptime(result.UptimeMs))
	}
	fmt.Fprintf(w, "Immune Daemon: %s\n", statusStr)

	// Security status summary line
	fmt.Fprintln(w, securitySummary(len(result.Alerts), len(result.SuspendedPIDs)))

	fmt.Fprintf(w, "Profiles: %d\n", result.ProfileCount)
	fmt.Fprintf(w, "Active Monitors: %d\n", len(result.ActivePIDs))
	fmt.Fprintf(w, "Threat Memory: %d signatures\n", result.ThreatCount)

	if len(result.Profiles) > 0 {
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
	}

	// Show alerts section
	if len(result.Alerts) > 0 {
		fmt.Fprintf(w, "\nALERTS (%d):\n", len(result.Alerts))
		for _, a := range result.Alerts {
			ts := time.UnixMilli(a.TimestampMs).Format("2006-01-02 15:04:05")
			fmt.Fprintf(w, "  PID %d: %s - %s (%s)\n", a.PID, a.Type, a.Detail, ts)
			fmt.Fprintf(w, "    Actions: rnix immune resume %d | rnix kill %d\n", a.PID, a.PID)
		}
	}

	// Show suspended processes section
	if len(result.SuspendedPIDs) > 0 {
		fmt.Fprintf(w, "\nSUSPENDED PROCESSES (%d):\n", len(result.SuspendedPIDs))
		for _, pid := range result.SuspendedPIDs {
			// Find alert detail for this PID
			detail := "anomaly"
			for _, a := range result.Alerts {
				if a.PID == pid {
					detail = fmt.Sprintf("%s anomaly", a.Type)
					break
				}
			}
			fmt.Fprintf(w, "  PID %d — %s (use: rnix immune resume %d | rnix kill %d)\n", pid, detail, pid, pid)
		}
	}

	if len(result.Profiles) == 0 && len(result.Alerts) == 0 {
		fmt.Fprintln(w, "\nNo behavior profiles established.")
	}

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "~"
}

func formatUptime(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	totalSec := int(d.Seconds())
	if totalSec < 60 {
		return fmt.Sprintf("%ds", totalSec)
	}
	totalMin := int(d.Minutes())
	sec := totalSec % 60
	if totalMin < 60 {
		return fmt.Sprintf("%dm%ds", totalMin, sec)
	}
	hours := totalMin / 60
	min := totalMin % 60
	return fmt.Sprintf("%dh%dm", hours, min)
}

func securitySummary(alertCount, suspendedCount int) string {
	if alertCount == 0 && suspendedCount == 0 {
		return "Security: OK"
	}
	var parts []string
	if alertCount > 0 {
		parts = append(parts, fmt.Sprintf("%d alerts", alertCount))
	}
	if suspendedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d suspended", suspendedCount))
	}
	return "Security: " + strings.Join(parts, ", ")
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

func runImmuneSimilarity(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	agentName := ""
	if len(args) > 0 {
		agentName = args[0]
	}

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

	result, err := client.SimilarityQuery(agentName, 0.0)
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

	formatSimilarityText(w, result)
	return nil
}

func formatSimilarityText(w io.Writer, response *ipc.SimilarityQueryResponse) {
	// Sort similarities by score descending
	sims := make([]struct {
		agent string
		score float64
		skill float64
	}, 0, len(response.Similarities))
	for _, s := range response.Similarities {
		other := s.AgentB
		if other == response.Agent {
			other = s.AgentA
		}
		sims = append(sims, struct {
			agent string
			score float64
			skill float64
		}{agent: other, score: s.Score, skill: s.SkillScore})
	}
	sort.Slice(sims, func(i, j int) bool {
		return sims[i].score > sims[j].score
	})

	fmt.Fprintf(w, "Capability Similarity for %q:\n\n", response.Agent)

	if len(sims) == 0 {
		fmt.Fprintln(w, "No similar agents found.")
		return
	}

	fmt.Fprintf(w, "%-20s %10s  %10s\n", "AGENT", "SIMILARITY", "SKILL")
	for _, s := range sims {
		fmt.Fprintf(w, "%-20s %10.2f  %10.2f\n", truncate(s.agent, 20), s.score, s.skill)
	}
}
