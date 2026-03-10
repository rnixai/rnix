package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

var lineageCmd = &cobra.Command{
	Use:   "lineage <pid>",
	Short: "Show differentiation lineage for a stem agent",
	Long: `Show the complete differentiation path from stem agent to current specialized form.
Displays each skill loading step with timestamp and trigger reason.`,
	Example: `  rnix lineage 42                 Show lineage for PID 42
  rnix lineage 42 --json         JSON output`,
	Args: cobra.ExactArgs(1),
	RunE: runLineage,
}

func init() {
	rootCmd.AddCommand(lineageCmd)
}

func runLineage(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("invalid PID: %s", args[0])}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[lineage] error: invalid PID: %s\n", args[0])
		}
		exitCode = 1
		return nil
	}
	pid := types.PID(pidNum)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": "daemon not available"}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[lineage] error: daemon not available (is the daemon running?)\n")
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	result, err := client.Lineage(pid)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[lineage] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if len(result.Events) == 0 {
		if flagJSON {
			resp := JSONResponse{OK: true, Data: result}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "Process %d has no differentiation lineage\n", pid)
		}
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprint(w, formatLineage(result))
	return nil
}

func formatLineage(resp *ipc.LineageResponse) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Lineage for PID %d\n\n", resp.PID))

	for i, event := range resp.Events {
		stepNum := ui.BoldStyle.Render(fmt.Sprintf("[%d]", i+1))

		var phaseLabel string
		switch event.Phase {
		case "initial":
			phaseLabel = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render("initial differentiation")
		case "progressive":
			phaseLabel = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Render("progressive specialization")
		default:
			phaseLabel = event.Phase
		}

		timestamp := time.UnixMilli(event.TimestampMs).Format("2006-01-02 15:04:05")

		sb.WriteString(fmt.Sprintf("%s %s  %s\n", stepNum, timestamp, phaseLabel))
		sb.WriteString(fmt.Sprintf("    Skills: %s\n", formatSkillNames(event.Skills)))
		sb.WriteString(fmt.Sprintf("    Trigger: %q\n", event.Trigger))

		var source string
		switch {
		case event.Phase == "progressive":
			source = "ooda-specialize"
		case event.FromMemory:
			source = "memory-reuse"
		default:
			source = "keyword-match"
		}
		sb.WriteString(fmt.Sprintf("    Source: %s\n", source))

		if i < len(resp.Events)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func formatSkillNames(skills []string) string {
	highlighted := make([]string, len(skills))
	for i, s := range skills {
		highlighted[i] = ui.AgentStyle.Render(s)
	}
	return strings.Join(highlighted, ", ")
}
