package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

var intentCmd = &cobra.Command{
	Use:   "intent",
	Short: "Manage declarative intents",
	Long:  "Commands for managing declarative intent trees: status, list.",
}

var intentStatusCmd = &cobra.Command{
	Use:   "status [intent-id]",
	Short: "Show intent tree status",
	Long:  "Display the current state of an intent tree: overall progress, per-node completion, and active agents.",
	Example: `  rnix intent status                # Show all active intents
  rnix intent status intent-1       # Show specific intent
  rnix intent status --json         # JSON output`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIntentStatus,
}

var intentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all intents",
	Long:  "Display a table of all intents (active + completed).",
	Example: `  rnix intent list
  rnix intent list --json`,
	RunE: runIntentList,
}

func init() {
	intentCmd.AddCommand(intentStatusCmd)
	intentCmd.AddCommand(intentListCmd)
}

func runIntentStatus(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	r := ui.NewRenderer(os.Stdout, mode)

	client, err := ipc.EnsureDaemon()
	if err != nil {
		return fmt.Errorf("intent status: cannot connect to daemon: %w", err)
	}
	defer client.Close()

	intentID := ""
	if len(args) > 0 {
		intentID = args[0]
	}

	resp, err := client.IntentStatus(intentID)
	if err != nil {
		return fmt.Errorf("intent status: %w", err)
	}

	if mode == ui.ModeJSON {
		data, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	if len(resp.Intents) == 0 {
		fmt.Fprintln(os.Stdout, "No active intents.")
		return nil
	}

	for _, tree := range resp.Intents {
		if intentID != "" {
			// Show detailed view for specific intent
			ui.RenderIntentStatusDetail(r, tree, mode)
		} else {
			ui.RenderIntentTree(r, tree, mode)
		}
		if len(tree.Drifts) > 0 {
			ui.RenderDriftList(r, tree.Drifts, mode)
		}
		fmt.Fprintln(os.Stdout)
	}

	return nil
}

func runIntentList(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	r := ui.NewRenderer(os.Stdout, mode)

	client, err := ipc.EnsureDaemon()
	if err != nil {
		return fmt.Errorf("intent list: cannot connect to daemon: %w", err)
	}
	defer client.Close()

	resp, err := client.IntentList()
	if err != nil {
		return fmt.Errorf("intent list: %w", err)
	}

	if mode == ui.ModeJSON {
		data, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	ui.RenderIntentList(r, resp.Intents, mode)

	return nil
}
