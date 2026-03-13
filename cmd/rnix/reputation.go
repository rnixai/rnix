package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

var reputationCmd = &cobra.Command{
	Use:   "reputation [agent]",
	Short: "Show agent reputation scores",
	Long: `Show reputation scores based on historical SLA evaluation results.
Without arguments, lists all agents. With an agent name, shows detailed info.`,
	Example: `  rnix reputation                List all agents
  rnix reputation code-reviewer  Show details for code-reviewer
  rnix reputation --json         JSON output`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReputation,
}

func init() {
	rootCmd.AddCommand(reputationCmd)
}

func runReputation(cmd *cobra.Command, args []string) error {
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
			fmt.Fprintln(w, "[reputation] error: daemon not available (is the daemon running?)")
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	result, err := client.ReputationStatus(agentName)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[reputation] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if len(result.Summaries) == 0 {
		if flagJSON {
			resp := JSONResponse{OK: true, Data: result}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintln(w, "No reputation data available.")
		}
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	// Detail mode for single agent
	if agentName != "" && len(result.Summaries) == 1 {
		fmt.Fprint(w, formatReputationDetail(result.Summaries[0]))
		return nil
	}

	// Table mode for listing all agents
	fmt.Fprint(w, formatReputationTable(result.Summaries))
	return nil
}

func formatReputationTable(summaries []kernel.ReputationSummary) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "%-20s %5s  %7s  %10s  %12s  %7s  %s\n",
		"AGENT", "SCORE", "SUCCESS", "AVG TOKENS", "AVG DURATION", "RECORDS", "TREND")

	for _, s := range summaries {
		fmt.Fprintf(&sb, "%-20s %5.2f  %5.1f%%  %10s  %10dms  %7d  %s\n",
			s.AgentName,
			s.Score,
			s.SuccessRate*100,
			formatTokens(s.AvgTokens),
			s.AvgDurationMs,
			s.TotalRecords,
			s.RecentTrend,
		)
	}

	return sb.String()
}

func formatReputationDetail(s kernel.ReputationSummary) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Agent: %s\n", s.AgentName)
	fmt.Fprintf(&sb, "Score: %.2f\n", s.Score)
	fmt.Fprintf(&sb, "Success Rate: %.1f%%\n", s.SuccessRate*100)
	fmt.Fprintf(&sb, "Avg Token Usage: %s\n", formatTokens(s.AvgTokens))
	fmt.Fprintf(&sb, "Avg Duration: %dms\n", s.AvgDurationMs)
	fmt.Fprintf(&sb, "Total Records: %d\n", s.TotalRecords)
	fmt.Fprintf(&sb, "Trend: %s\n", s.RecentTrend)

	return sb.String()
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%d,%03d", n/1000, n%1000)
}
