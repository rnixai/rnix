package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rnixai/rnix/internal/config"
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
	Example: `  rnix apply "I want a complete blog system"
  rnix apply "build a REST API for user management" --yes
  rnix apply "create a CLI tool" --model claude-opus --json
  rnix apply "add a comment feature" --update intent-1`,
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
	renderer := ui.NewRenderer(os.Stdout, mode)

	client, err := ipc.EnsureDaemon()
	if err != nil {
		return fmt.Errorf("apply: cannot connect to daemon: %w", err)
	}
	defer client.Close()

	if flagUpdateIntent != "" {
		return runApplyIncremental(client, renderer, intentStr, mode)
	}

	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)
	start := time.Now()

	intent := intentStr
	if flagAutoStart {
		intent = "[AUTO_CONFIRM] " + intent
	}

	cwd, _ := os.Getwd()
	projectDir, _ := config.ProjectDir(cwd)

	req := ipc.SpawnRequest{
		Intent:     intent,
		Agent:      "orchestrator",
		Model:      flagModel,
		Provider:   flagProvider,
		ProjectDir: projectDir,
		RnixEnv:    os.Getenv("RNIX_ENV"),
	}

	var spawnProvider, spawnModel string

	pid, final, spawnErr := client.SpawnAndWatch(req, func(ev ipc.StreamEvent) {
		switch ev.Type {
		case ipc.StreamAskUser:
			handleAskUserEvent(ev.Payload, ipc.SocketPath())
			return
		case ipc.StreamProgress:
			// fall through
		default:
			return
		}

		var pp ipc.ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			return
		}
		switch pp.Event {
		case "spawn":
			spawnProvider = pp.Provider
			spawnModel = pp.Model
			uuidShort := pp.UUID
			if len(uuidShort) > 12 {
				uuidShort = uuidShort[:12] + "..."
			}
			if pp.Provider != "" && pp.Model != "" {
				progress.KernelMessage("spawning orchestrator PID %d (uuid: %s, %s/%s)...", pp.PID, uuidShort, pp.Provider, pp.Model)
			} else if pp.Provider != "" {
				progress.KernelMessage("spawning orchestrator PID %d (uuid: %s, %s)...", pp.PID, uuidShort, pp.Provider)
			} else {
				progress.KernelMessage("spawning orchestrator PID %d (uuid: %s)...", pp.PID, uuidShort)
			}
		case "step":
			progress.AgentStep(pp.PID, pp.Step, pp.Total)
		case "step_complete":
			if mode == ui.ModeJSON {
				jsonLine, _ := json.Marshal(pp)
				fmt.Fprintf(os.Stdout, "%s\n", jsonLine)
			} else {
				progress.AgentStepComplete(pp.PID, pp.Step, pp.Action, pp.Summary)
			}
		}
	})

	if spawnErr != nil {
		outputError(renderer, mode, "apply", spawnErr.Error(), "orchestrator agent failed to start", "check that LLM CLI is installed")
		exitCode = 1
		return nil
	}

	elapsed := time.Since(start)

	if final != nil && final.ExitCode == 0 {
		outputSuccess(renderer, mode, pid, final.Result, final.TokensUsed, elapsed, spawnProvider, spawnModel)
	} else {
		reason := "unknown error"
		if final != nil {
			reason = final.ExitReason
			if final.ErrorMessage != "" {
				reason = final.ErrorMessage
			}
		}
		outputError(renderer, mode, "apply", reason, "intent execution failed", "check intent description or retry")
		tokensUsed := 0
		if final != nil {
			tokensUsed = final.TokensUsed
		}
		ui.RenderSummary(renderer, pid, 1, tokensUsed, elapsed, spawnProvider, spawnModel)
		exitCode = 1
	}

	return nil
}

func runApplyIncremental(client *ipc.Client, r *ui.Renderer, intentStr string, mode ui.OutputMode) error {
	req := ipc.ApplyIncrementalIntentRequest{
		IntentID: flagUpdateIntent,
		Intent:   intentStr,
		Model:    flagModel,
		Provider: flagProvider,
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
