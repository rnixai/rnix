package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

var flagAutoStart bool
var flagUpdateIntent string

var applyCmd = &cobra.Command{
	Use:   "apply <intent>",
	Short: "Declare intent and auto-decompose into sub-tasks",
	Long:  "Declare a high-level intent. The system decomposes it into a sub-intent tree (Intent Tree), each sub-intent maps to one or more agent processes.",
	Example: `  rnix apply "我要一个完整的博客系统"
  rnix apply "build a REST API for user management" --yes
  rnix apply "create a CLI tool" --model claude-opus --json
  rnix apply "加上评论功能" --update intent-1`,
	Args: cobra.ExactArgs(1),
	RunE: runApply,
}

func init() {
	applyCmd.Flags().BoolVarP(&flagAutoStart, "yes", "y", false, "Skip confirmation and start execution immediately")
	applyCmd.Flags().StringVarP(&flagUpdateIntent, "update", "u", "", "Incremental update an existing intent")
}

func runApply(cmd *cobra.Command, args []string) error {
	intentStr := args[0]
	mode := resolveOutputMode()
	r := ui.NewRenderer(os.Stdout, mode)

	client, err := ipc.EnsureDaemon()
	if err != nil {
		return fmt.Errorf("apply: cannot connect to daemon: %w", err)
	}
	defer client.Close()

	// Handle incremental update
	if flagUpdateIntent != "" {
		return runApplyIncremental(client, r, intentStr, mode)
	}

	req := ipc.ApplyIntentRequest{
		Intent:    intentStr,
		Model:     flagModel,
		AutoStart: flagAutoStart,
	}

	var intentID string

	applyResp, err := client.ApplyIntentAndWatch(req, func(ev ipc.StreamEvent) {
		onEvent(r, ev, client, &intentID, mode)
	})

	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}

	intentID = applyResp.IntentID

	if mode != ui.ModeJSON {
		fmt.Fprintf(os.Stdout, "\n[apply] intent %s created\n", applyResp.IntentID)
	}

	return nil
}

func runApplyIncremental(client *ipc.Client, r *ui.Renderer, intentStr string, mode ui.OutputMode) error {
	req := ipc.ApplyIncrementalIntentRequest{
		IntentID: flagUpdateIntent,
		Intent:   intentStr,
		Model:    flagModel,
	}

	resp, err := client.ApplyIncrementalIntent(req)
	if err != nil {
		return fmt.Errorf("apply incremental: %w", err)
	}

	ui.RenderIntentMergeResult(r, resp.AddedNodes, resp.ModifiedNodes, mode)

	if mode != ui.ModeJSON {
		fmt.Fprintf(os.Stdout, "\n[apply] intent %s updated\n", resp.IntentID)
	}

	return nil
}

func onEvent(r *ui.Renderer, ev ipc.StreamEvent, client *ipc.Client, intentID *string, mode ui.OutputMode) {
	switch ev.Type {
	case ipc.StreamIntentDecomposed:
		var tree ipc.IntentTreeWire
		if err := json.Unmarshal(ev.Payload, &tree); err == nil {
			*intentID = tree.ID
			ui.RenderIntentTree(r, &tree, mode)
		}

	case ipc.StreamIntentConfirmReq:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			confirmID := payload.NodeID
			if confirmID == "" {
				confirmID = *intentID
			}
			if !flagAutoStart && confirmID != "" {
				fmt.Fprint(os.Stderr, "\n确认执行此计划? [y/N] ")
				var answer string
				_, _ = fmt.Scanln(&answer)
				confirm := answer == "y" || answer == "Y" || answer == "yes"
				_ = client.ConfirmIntent(confirmID, confirm)
			}
		}

	case ipc.StreamIntentNodeStart:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentNodeEvent(r, "start", payload.NodeID, payload.Intent, payload.PID, mode)
		}

	case ipc.StreamIntentNodeDone:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentNodeEvent(r, "done", payload.NodeID, payload.Result, 0, mode)
		}

	case ipc.StreamIntentNodeFailed:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentNodeEvent(r, "failed", payload.NodeID, payload.Error, 0, mode)
		}

	case ipc.StreamIntentProgress:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentProgress(r, payload.Completed, payload.Total, mode)
		}

	case ipc.StreamIntentNodeRetry:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentNodeRetry(r, payload.NodeID, payload.RetryAttempt, payload.MaxRetries, mode)
		}

	case ipc.StreamIntentNodeTimeout:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentNodeTimeout(r, payload.NodeID, mode)
		}

	case ipc.StreamIntentDriftDetected:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentNodeEvent(r, "drift", payload.NodeID, payload.DriftType+": "+payload.Error, 0, mode)
		}

	case ipc.StreamIntentDriftResolved:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			ui.RenderIntentNodeEvent(r, "drift_resolved", payload.NodeID, "drift resolved", 0, mode)
		}

	case ipc.StreamIntentIncrementalMerged:
		var resp ipc.ApplyIncrementalIntentResponse
		if err := json.Unmarshal(ev.Payload, &resp); err == nil {
			ui.RenderIntentMergeResult(r, resp.AddedNodes, resp.ModifiedNodes, mode)
		}

	case ipc.StreamIntentComplete:
		var payload ipc.IntentNodeEventPayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			if payload.Error != "" {
				ui.RenderIntentNodeEvent(r, "error", "", payload.Error, 0, mode)
			} else {
				ui.RenderIntentNodeEvent(r, "complete", "", "all tasks finished", 0, mode)
			}
		}
	}
}
