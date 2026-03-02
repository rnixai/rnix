package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/ipc"
	"github.com/spf13/cobra"
)

var flagFilter string

var logCmd = &cobra.Command{
	Use:   "log <pid>",
	Short: "View categorized reasoning logs of an agent process",
	Long:  "Stream reasoning logs from a running agent, categorized as [think], [tool], and [output].\n\nPress Ctrl+C to detach without affecting the traced process.",
	Example: `  crux log 5                   Stream all log categories
  crux log 5 --filter tool     Show only tool call logs
  crux log 5 --filter think    Show only reasoning logs
  crux log 5 --json            Output as NDJSON stream`,
	Args: cobra.ExactArgs(1),
	RunE: runLog,
}

func init() {
	logCmd.Flags().StringVar(&flagFilter, "filter", "", "Filter by log category (think, tool, output)")
}

// validLogCategories is the set of accepted --filter values.
var validLogCategories = map[string]bool{
	"think":  true,
	"tool":   true,
	"output": true,
}

func runLog(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("✗ crux log %s: invalid PID (expected number)", args[0])
	}
	pid := types.PID(pidNum)

	if flagFilter != "" && !validLogCategories[flagFilter] {
		return fmt.Errorf("✗ crux log: invalid --filter value %q (valid: think, tool, output)", flagFilter)
	}

	w := cmd.OutOrStdout()
	mode := resolveOutputMode()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		renderer := ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			"no active daemon (process not found)", "",
			"crux ps  查看活跃进程")
		return nil
	}
	defer client.Close()

	if !flagJSON {
		fmt.Fprintf(w, "[crux log] attached to PID %d\n\n", pid)
	}

	parentCtx := cmd.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	logCtx, logCancel := context.WithCancel(parentCtx)
	defer logCancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		logCancel()
	}()

	var renderer *ui.Renderer
	if !flagJSON {
		renderer = ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.AttachLog(pid, func(lew ipc.LogEntryWire) {
			select {
			case <-logCtx.Done():
				return
			default:
			}

			if flagFilter != "" && lew.Category != flagFilter {
				return
			}

			if flagJSON {
				data, _ := json.Marshal(lew)
				fmt.Fprintln(w, string(data))
			} else {
				fmt.Fprintln(w, FormatLogEntry(renderer, lew))
			}
		})
	}()

	select {
	case err := <-errCh:
		if err != nil && isNotFoundError(err) {
			if renderer != nil {
				ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
					"process not found",
					fmt.Sprintf("PID %d: 不存在或已退出", pid),
					"crux ps  查看活跃进程")
			}
			exitCode = 1
		} else if !flagJSON {
			if err == nil {
				fmt.Fprintf(w, "\n[crux log] detached from PID %d (process exited)\n", pid)
			} else {
				fmt.Fprintf(w, "\n[crux log] detached from PID %d (error: %v)\n", pid, err)
			}
		}
	case <-logCtx.Done():
		if !flagJSON {
			fmt.Fprintf(w, "\n[crux log] detached from PID %d (interrupted)\n", pid)
		}
	}

	return nil
}

// FormatLogEntry renders a single log entry for human-readable terminal output.
// Format: [HH:MM:SS.sss] [category] content
func FormatLogEntry(r *ui.Renderer, lew ipc.LogEntryWire) string {
	ts := time.Duration(lew.TimestampMs) * time.Millisecond
	timeStr := formatLogTimestamp(ts)

	category := lew.Category
	var styledCategory string
	if r != nil && r.Profile.ColorLevel > 0 {
		switch types.LogCategory(category) {
		case types.LogThink:
			styledCategory = ui.MutedStyle.Render("[think] ")
		case types.LogTool:
			styledCategory = ui.AgentStyle.Render("[tool]  ")
		case types.LogOutput:
			styledCategory = ui.SuccessStyle.Render("[output]")
		default:
			styledCategory = fmt.Sprintf("[%-6s]", category)
		}
	} else {
		switch types.LogCategory(category) {
		case types.LogThink:
			styledCategory = "[think] "
		case types.LogTool:
			styledCategory = "[tool]  "
		case types.LogOutput:
			styledCategory = "[output]"
		default:
			styledCategory = fmt.Sprintf("[%-6s]", category)
		}
	}

	content := lew.Content
	if types.LogCategory(category) == types.LogTool && lew.ToolPath != "" {
		content = lew.ToolPath + " → " + content
	}

	return fmt.Sprintf("[%s] %s %s", timeStr, styledCategory, content)
}

// formatLogTimestamp formats a duration as relative seconds with millisecond precision.
func formatLogTimestamp(d time.Duration) string {
	secs := d.Seconds()
	return fmt.Sprintf("%7.3f", secs)
}

// isNotFoundError checks if an IPC error indicates "process not found".
func isNotFoundError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "not found") || strings.Contains(msg, "NOT_FOUND")
}
