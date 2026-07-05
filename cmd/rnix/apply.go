package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
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
	if depthStr := os.Getenv("RNIX_SPAWN_DEPTH"); depthStr != "" {
		if depth, err := strconv.Atoi(depthStr); err == nil && depth > 0 {
			return fmt.Errorf("recursive rnix apply blocked: already at spawn depth %d", depth)
		}
	}

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

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("apply: cannot determine working directory: %w", err)
	}
	projectDir, err := config.ProjectDir(cwd)
	if err != nil {
		return fmt.Errorf("apply: cannot resolve project directory: %w", err)
	}

	req := ipc.SpawnRequest{
		Intent:           intent,
		Agent:            "orchestrator",
		Model:            flagModel,
		Provider:         flagProvider,
		FallbackModel:    flagFallbackModel,
		FallbackProvider: flagFallbackProvider,
		ReasoningEffort:  flagReasoningEffort,
		ProjectDir:       projectDir,
		RnixEnv:          os.Getenv("RNIX_ENV"),
	}

	var spawnProvider, spawnModel, spawnEffort string
	var spawnUUID string

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
			spawnEffort = pp.ReasoningEffort
			spawnUUID = pp.UUID
			uuidShort := ui.ShortUUID(pp.UUID)
			effortSuffix := ""
			if pp.ReasoningEffort != "" {
				effortSuffix = ", effort: " + pp.ReasoningEffort
			}
			if pp.Provider != "" && pp.Model != "" {
				progress.KernelMessage("spawning orchestrator PID %d (uuid: %s, %s/%s%s)...", pp.PID, uuidShort, pp.Provider, pp.Model, effortSuffix)
			} else if pp.Provider != "" {
				progress.KernelMessage("spawning orchestrator PID %d (uuid: %s, %s%s)...", pp.PID, uuidShort, pp.Provider, effortSuffix)
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
		outputError(renderer, mode, "apply", spawnErr.Error(), "orchestrator agent failed to start", spawnHint(spawnErr.Error()))
		exitCode = 1
		return nil
	}

	elapsed := time.Since(start)

	if final == nil {
		final = tryReconnectAndPoll(progress, spawnUUID, projectDir, req.RnixEnv)
	}

	if final == nil {
		outputError(renderer, mode, "apply", "connection to daemon lost", "orchestrator process may still be running", "check rnix ps for process status")
		exitCode = 1
		return nil
	}

	if final.ExitCode == 0 {
		outputSuccess(renderer, mode, pid, final.Result, final.TokensUsed, elapsed, spawnProvider, spawnModel, spawnEffort)
	} else {
		reason := final.ExitReason
		if final.ErrorMessage != "" {
			reason = final.ErrorMessage
		}
		if reason == "" {
			reason = "unknown error"
		}
		outputError(renderer, mode, "apply", reason, "intent execution failed", "check intent description or retry")
		ui.RenderSummary(renderer, pid, 1, final.TokensUsed, elapsed, spawnProvider, spawnModel, spawnEffort)
		exitCode = 1
	}

	return nil
}

// tryReconnectAndPoll attempts to reconnect to a restarted daemon and poll
// the orchestrator process to completion. Returns nil if reconnection fails
// or the process cannot be found.
func tryReconnectAndPoll(progress *ui.ProgressReporter, uuid, projectDir, rnixEnv string) *ipc.ProgressPayload {
	if uuid == "" {
		return nil
	}

	progress.KernelMessage("daemon connection lost, waiting for new daemon...")

	const reconnectTimeout = 30 * time.Second
	const reconnectInterval = 2 * time.Second
	const pollInterval = 500 * time.Millisecond
	const pollTimeout = 10 * time.Minute

	var newClient *ipc.Client
	deadline := time.Now().Add(reconnectTimeout)
	for time.Now().Before(deadline) {
		c, cerr := ipc.EnsureDaemon()
		if cerr == nil {
			newClient = c
			break
		}
		time.Sleep(reconnectInterval)
	}
	if newClient == nil {
		return nil
	}
	defer newClient.Close()

	progress.KernelMessage("reconnected to daemon, polling process %s...", uuid[:min(12, len(uuid))])

	// If the process is suspended from daemon_shutdown, resume it.
	if detail, derr := newClient.GetProcDetail(0, uuid); derr == nil && detail.State == "suspended" {
		progress.KernelMessage("resuming suspended orchestrator...")
		if _, rerr := newClient.ResumeWithOptsV3(uuid, false, 0, projectDir, rnixEnv); rerr != nil {
			progress.KernelMessage("resume failed: %v", rerr)
			return nil
		}
	}

	deadline = time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		procs, perr := newClient.ListAllProcs()
		if perr != nil {
			continue
		}

		for _, p := range procs {
			if p.UUID != uuid {
				continue
			}
			if p.State == types.StateDead || p.State == types.StateZombie {
				return &ipc.ProgressPayload{
					Event:      "complete",
					PID:        p.PID,
					Result:     p.Result,
					ExitCode:   p.ExitCode,
					ExitReason: p.ExitReason,
					TokensUsed: p.TokensUsed,
				}
			}
			// Still running or suspended — keep polling.
			break
		}
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
