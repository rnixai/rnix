package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/debug"
)

var replayCmd = &cobra.Command{
	Use:   "replay <record-id>",
	Short: "Replay a recorded execution trace",
	Long: `Load a recorded execution trace and enter an interactive replay session.

Navigate through recorded events with next/prev/goto commands.
The replay reads local recording files and does not require a running daemon.`,
	Example: `  rnix replay 42-1709856000              Replay recording
  rnix replay 42-1709856000 --json       JSON output mode`,
	Args: cobra.ExactArgs(1),
	RunE: runReplay,
}

func init() {
	replayCmd.Flags().Bool("json", false, "Output events in JSON format")
	rootCmd.AddCommand(replayCmd)
}

func runReplay(cmd *cobra.Command, args []string) error {
	recordID := args[0]
	w := cmd.OutOrStdout()
	jsonMode, _ := cmd.Flags().GetBool("json")

	// Find the recording directory locally
	// Replay is a local file operation and does not require a running daemon
	mgr := debug.NewRecordManager(findRecordBaseDir())
	recordDir, findErr := mgr.FindRecord(recordID)
	if findErr != nil {
		if jsonMode {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("recording %q not found", recordID)}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[replay] error: recording %q not found\n", recordID)
		}
		exitCode = 1
		return nil
	}

	// Load the recording
	reader, err := debug.NewRecordReader(recordDir)
	if err != nil {
		if jsonMode {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[replay] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	meta := reader.Metadata()
	session := debug.NewReplaySession(reader)

	if !jsonMode {
		fmt.Fprintf(w, "[replay] Loading record %s...\n", recordID)
		fmt.Fprintf(w, "[replay] PID: %d | Intent: %q | Events: %d | Status: %s\n",
			meta.PID, meta.Intent, reader.EventCount(), meta.Status)
		if !meta.EndTime.IsZero() && !meta.StartTime.IsZero() {
			duration := meta.EndTime.Sub(meta.StartTime)
			fmt.Fprintf(w, "[replay] Duration: %.0fs\n", duration.Seconds())
		}
		fmt.Fprintf(w, "[replay] Type 'help' for commands, 'quit' to exit\n\n")
	}

	// Interactive command loop
	scanner := bufio.NewScanner(os.Stdin)
	if !jsonMode {
		fmt.Fprint(w, "replay> ")
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Fields(line)
		if len(parts) == 0 {
			if !jsonMode {
				fmt.Fprint(w, "replay> ")
			}
			continue
		}

		switch parts[0] {
		case "next", "n":
			ev, navErr := session.Next()
			if navErr != nil {
				if jsonMode {
					resp := JSONResponse{OK: false, Error: map[string]string{"message": navErr.Error()}}
					data, _ := json.Marshal(resp)
					fmt.Fprintln(w, string(data))
				} else {
					fmt.Fprintf(w, "[replay] %v\n", navErr)
				}
			} else {
				printReplayEvent(w, ev, session, jsonMode)
			}

		case "prev", "p":
			ev, navErr := session.Prev()
			if navErr != nil {
				if jsonMode {
					resp := JSONResponse{OK: false, Error: map[string]string{"message": navErr.Error()}}
					data, _ := json.Marshal(resp)
					fmt.Fprintln(w, string(data))
				} else {
					fmt.Fprintf(w, "[replay] %v\n", navErr)
				}
			} else {
				printReplayEvent(w, ev, session, jsonMode)
			}

		case "goto":
			if len(parts) < 2 {
				if !jsonMode {
					fmt.Fprintln(w, "[replay] usage: goto <seq_num>")
				}
			} else {
				seqNum, parseErr := strconv.ParseUint(parts[1], 10, 64)
				if parseErr != nil {
					if !jsonMode {
						fmt.Fprintf(w, "[replay] invalid seq_num: %s\n", parts[1])
					}
				} else {
					ev, navErr := session.Goto(seqNum)
					if navErr != nil {
						if jsonMode {
							resp := JSONResponse{OK: false, Error: map[string]string{"message": navErr.Error()}}
							data, _ := json.Marshal(resp)
							fmt.Fprintln(w, string(data))
						} else {
							fmt.Fprintf(w, "[replay] %v\n", navErr)
						}
					} else {
						printReplayEvent(w, ev, session, jsonMode)
					}
				}
			}

		case "list", "l":
			items := session.List(5)
			if jsonMode {
				data, _ := json.Marshal(items)
				fmt.Fprintln(w, string(data))
			} else {
				if len(items) == 0 {
					fmt.Fprintln(w, "[replay] no events to display (use 'next' to start)")
				} else {
					fmt.Fprintln(w, debug.FormatReplayList(items))
				}
			}

		case "info", "i":
			if jsonMode {
				data, _ := json.Marshal(meta)
				fmt.Fprintln(w, string(data))
			} else {
				current, total := session.Position()
				summary := debug.FormatReplaySummary(meta, reader.EventCount())
				if current >= 0 {
					summary += fmt.Sprintf("\nPosition:   %d/%d", current+1, total)
				}
				fmt.Fprintln(w, summary)
			}

		case "help", "h":
			printReplayHelp(w)

		case "quit", "q":
			if !jsonMode {
				fmt.Fprintln(w, "[replay] session ended.")
			}
			return nil

		default:
			if !jsonMode {
				fmt.Fprintf(w, "[replay] unknown command: %s (type 'help' for commands)\n", line)
			}
		}

		if !jsonMode {
			fmt.Fprint(w, "replay> ")
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(w, "[replay] input error: %v\n", err)
	}

	if !jsonMode {
		fmt.Fprintln(w, "\n[replay] session ended.")
	}

	return nil
}

func printReplayEvent(w interface{ Write([]byte) (int, error) }, ev *debug.RecordEvent, session *debug.ReplaySession, jsonMode bool) {
	if jsonMode {
		data, _ := json.Marshal(ev)
		fmt.Fprintln(w, string(data))
		return
	}
	fmt.Fprintln(w, debug.FormatReplayEvent(ev, flagVerbose))
	current, total := session.Position()
	fmt.Fprintf(w, "[%d/%d]\n", current+1, total)
}

func printReplayHelp(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(w, "  next / n            - Forward one event")
	fmt.Fprintln(w, "  prev / p            - Backward one event")
	fmt.Fprintln(w, "  goto <seq_num>      - Jump to event by sequence number")
	fmt.Fprintln(w, "  list / l            - Show events around current position")
	fmt.Fprintln(w, "  info / i            - Show recording summary")
	fmt.Fprintln(w, "  help / h            - Show this help")
	fmt.Fprintln(w, "  quit / q            - Exit replay")
}

func findRecordBaseDir() string {
	cwd, _ := os.Getwd()
	return cwd + "/.rnix/records"
}
