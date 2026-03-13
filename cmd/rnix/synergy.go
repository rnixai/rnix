package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/spf13/cobra"
)

var synergyCmd = &cobra.Command{
	Use:   "synergy",
	Short: "Skill synergy combination management",
}

var synergyListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show skill synergy combination matrix",
	Long: `Show historical performance data for skill combinations.
Displays success rates, token usage, and recommendations.`,
	Example: `  rnix synergy list          # Show combination matrix
  rnix synergy list --json   # JSON output`,
	RunE: runSynergyList,
}

func init() {
	synergyCmd.AddCommand(synergyListCmd)
	rootCmd.AddCommand(synergyCmd)
}

func runSynergyList(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": "daemon not available"}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintln(w, "No synergy combination data available.")
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	result, err := client.SynergyList()
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[synergy] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if len(result.Combos) == 0 {
		if flagJSON {
			resp := JSONResponse{OK: true, Data: result}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintln(w, "No synergy combination data available.")
		}
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprint(w, formatSynergyTable(result.Combos))
	return nil
}

func formatSynergyTable(combos []kernel.ComboSummary) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "%-25s %7s  %10s  %10s  %8s  %10s  %s\n",
		"SKILLS", "SUCCESS", "AVG TOKENS", "EXECUTIONS", "VS SOLO", "TOKEN GAIN", "STATUS")

	for _, c := range combos {
		skillsStr := strings.Join(c.Skills, ",")
		vsSolo := "-"
		if c.AvgSoloRate > 0 {
			diff := (c.SuccessRate - c.AvgSoloRate) * 100
			vsSolo = fmt.Sprintf("%+.1f%%", diff)
		}
		tokenGain := "-"
		if c.TokenImprovement != 0 {
			tokenGain = fmt.Sprintf("%+.1f%%", c.TokenImprovement)
		}
		status := "-"
		if c.Recommended {
			status = "recommended"
		}

		fmt.Fprintf(&sb, "%-25s %5.1f%%  %10s  %10d  %8s  %10s  %s\n",
			skillsStr,
			c.SuccessRate*100,
			formatTokens(c.AvgTokens),
			c.TotalExecutions,
			vsSolo,
			tokenGain,
			status,
		)
	}

	return sb.String()
}
