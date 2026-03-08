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
	"github.com/rnixai/rnix/ipc"
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

		case "diff":
			if len(parts) < 2 {
				if jsonMode {
					resp := JSONResponse{OK: false, Error: map[string]string{"message": "usage: diff <seq1> <seq2> or diff <seq>"}}
					data, _ := json.Marshal(resp)
					fmt.Fprintln(w, string(data))
				} else {
					fmt.Fprintln(w, "[replay] usage: diff <seq1> <seq2> or diff <seq>")
				}
			} else if len(parts) == 2 {
				// diff <seq> — diff from cursor
				seq, parseErr := strconv.ParseUint(parts[1], 10, 64)
				if parseErr != nil {
					if jsonMode {
						resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("invalid seq_num: %s", parts[1])}}
						data, _ := json.Marshal(resp)
						fmt.Fprintln(w, string(data))
					} else {
						fmt.Fprintf(w, "[replay] invalid seq_num: %s\n", parts[1])
					}
				} else {
					d, diffErr := session.DiffFromCursor(seq)
					if diffErr != nil {
						if jsonMode {
							resp := JSONResponse{OK: false, Error: map[string]string{"message": diffErr.Error()}}
							data, _ := json.Marshal(resp)
							fmt.Fprintln(w, string(data))
						} else {
							fmt.Fprintf(w, "[replay] %v\n", diffErr)
						}
					} else {
						printDiffResult(w, d, jsonMode)
					}
				}
			} else {
				// diff <seq1> <seq2>
				seq1, parseErr1 := strconv.ParseUint(parts[1], 10, 64)
				seq2, parseErr2 := strconv.ParseUint(parts[2], 10, 64)
				if parseErr1 != nil || parseErr2 != nil {
					if jsonMode {
						resp := JSONResponse{OK: false, Error: map[string]string{"message": "usage: diff <seq1> <seq2>"}}
						data, _ := json.Marshal(resp)
						fmt.Fprintln(w, string(data))
					} else {
						fmt.Fprintln(w, "[replay] usage: diff <seq1> <seq2>")
					}
				} else {
					d, diffErr := session.Diff(seq1, seq2)
					if diffErr != nil {
						if jsonMode {
							resp := JSONResponse{OK: false, Error: map[string]string{"message": diffErr.Error()}}
							data, _ := json.Marshal(resp)
							fmt.Fprintln(w, string(data))
						} else {
							fmt.Fprintf(w, "[replay] %v\n", diffErr)
						}
					} else {
						printDiffResult(w, d, jsonMode)
					}
				}
			}

		case "fork":
			forkCtx, forkErr := session.Fork()
			if forkErr != nil {
				if jsonMode {
					resp := JSONResponse{OK: false, Error: map[string]string{"message": forkErr.Error()}}
					data, _ := json.Marshal(resp)
					fmt.Fprintln(w, string(data))
				} else {
					fmt.Fprintf(w, "[fork] error: %v\n", forkErr)
				}
			} else {
				if !jsonMode {
					fmt.Fprintf(w, "[fork] Creating fork from event #%03d...\n", forkCtx.SeqNum)
					fmt.Fprintf(w, "[fork] Original PID: %d\n", forkCtx.OriginalPID)
					fmt.Fprintf(w, "[fork] Intent: %q\n", forkCtx.Intent)
					fmt.Fprintf(w, "[fork] Messages: %d\n", len(forkCtx.Messages))
					if forkCtx.SystemPrompt != "" {
						fmt.Fprintf(w, "[fork] System Prompt: len:%d\n", len(forkCtx.SystemPrompt))
					}
					fmt.Fprintln(w, "[fork]")
					printForkHelp(w)
				}
				runForkSubMode(w, scanner, forkCtx, jsonMode)
			}

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
	fmt.Fprintln(w, "  diff <seq1> <seq2>  - Compare context at two time points")
	fmt.Fprintln(w, "  diff <seq>          - Compare context at cursor vs time point")
	fmt.Fprintln(w, "  fork                - Fork from current position (use continue/go to execute)")
	fmt.Fprintln(w, "  info / i            - Show recording summary")
	fmt.Fprintln(w, "  help / h            - Show this help")
	fmt.Fprintln(w, "  quit / q            - Exit replay")
}

func printDiffResult(w interface{ Write([]byte) (int, error) }, d *debug.ContextDiff, jsonMode bool) {
	if jsonMode {
		data, err := debug.FormatContextDiffJSON(d)
		if err != nil {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			errData, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(errData))
			return
		}
		fmt.Fprintln(w, string(data))
		return
	}
	fmt.Fprint(w, debug.FormatContextDiff(d))
}

func findRecordBaseDir() string {
	cwd, _ := os.Getwd()
	return cwd + "/.rnix/records"
}

func printForkHelp(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(w, "[fork] Available commands:")
	fmt.Fprintln(w, "[fork]   set prompt <text>  - Change system prompt")
	fmt.Fprintln(w, "[fork]   append <role> <msg> - Add a message")
	fmt.Fprintln(w, "[fork]   remove <n>         - Remove last n messages")
	fmt.Fprintln(w, "[fork]   replace <text>     - Replace last message")
	fmt.Fprintln(w, "[fork]   show               - Show fork context summary")
	fmt.Fprintln(w, "[fork]   continue / go      - Execute with LLM (requires daemon)")
	fmt.Fprintln(w, "[fork]   cancel             - Cancel and return to replay")
}

// runForkSubMode runs the fork sub-mode interactive loop.
// The user can modify the fork context and then either continue (execute with daemon)
// or cancel to return to replay mode.
func runForkSubMode(w interface{ Write([]byte) (int, error) }, scanner *bufio.Scanner, forkCtx *debug.ForkContext, jsonMode bool) {
	if !jsonMode {
		fmt.Fprint(w, "fork> ")
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Fields(line)
		if len(parts) == 0 {
			if !jsonMode {
				fmt.Fprint(w, "fork> ")
			}
			continue
		}

		switch parts[0] {
		case "set":
			if len(parts) >= 3 && parts[1] == "prompt" {
				// Join remaining parts as the prompt text
				promptText := strings.Join(parts[2:], " ")
				forkCtx.SetSystemPrompt(promptText)
				if !jsonMode {
					fmt.Fprintf(w, "[fork] System prompt updated (len:%d)\n", len(promptText))
				}
			} else {
				if !jsonMode {
					fmt.Fprintln(w, "[fork] usage: set prompt <text>")
				}
			}

		case "append":
			if len(parts) >= 3 {
				role := parts[1]
				content := strings.Join(parts[2:], " ")
				forkCtx.AppendMessage(role, content)
				if !jsonMode {
					fmt.Fprintf(w, "[fork] Message appended: [%s] %s\n", role, content)
				}
			} else {
				if !jsonMode {
					fmt.Fprintln(w, "[fork] usage: append <role> <content>")
				}
			}

		case "remove":
			if len(parts) >= 2 {
				n, parseErr := strconv.Atoi(parts[1])
				if parseErr != nil || n < 1 {
					if !jsonMode {
						fmt.Fprintln(w, "[fork] usage: remove <n> (n must be a positive integer)")
					}
				} else {
					forkCtx.RemoveLastMessages(n)
					if !jsonMode {
						fmt.Fprintf(w, "[fork] Removed last %d message(s). Messages remaining: %d\n", n, len(forkCtx.Messages))
					}
				}
			} else {
				if !jsonMode {
					fmt.Fprintln(w, "[fork] usage: remove <n>")
				}
			}

		case "replace":
			if len(parts) >= 2 {
				content := strings.Join(parts[1:], " ")
				forkCtx.ReplaceLastMessage(content)
				if !jsonMode {
					fmt.Fprintf(w, "[fork] Last message replaced with: %s\n", content)
				}
			} else {
				if !jsonMode {
					fmt.Fprintln(w, "[fork] usage: replace <content>")
				}
			}

		case "show":
			if !jsonMode {
				fmt.Fprint(w, forkCtx.Summary())
			} else {
				data, _ := json.Marshal(forkCtx)
				fmt.Fprintln(w, string(data))
			}

		case "continue", "go":
			if !jsonMode {
				fmt.Fprintln(w, "[fork] Connecting to daemon...")
			}
			executeForkContinue(w, forkCtx, jsonMode)
			// Return to replay mode after continue
			return

		case "cancel":
			if !jsonMode {
				fmt.Fprintln(w, "[fork] Fork cancelled. Returning to replay mode.")
			}
			return

		case "help", "h":
			printForkHelp(w)

		default:
			if !jsonMode {
				fmt.Fprintf(w, "[fork] unknown command: %s (type 'help' for commands)\n", parts[0])
			}
		}

		if !jsonMode {
			fmt.Fprint(w, "fork> ")
		}
	}
}

// executeForkContinue connects to the daemon and sends the fork-continue request.
func executeForkContinue(w interface{ Write([]byte) (int, error) }, forkCtx *debug.ForkContext, jsonMode bool) {
	socketPath := ipc.SocketPath()
	client, err := ipc.Dial(socketPath)
	if err != nil {
		if jsonMode {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("daemon not running: %v", err)}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[fork] error: daemon not running (%v)\n", err)
			fmt.Fprintln(w, "[fork] The 'continue' command requires a running daemon.")
			fmt.Fprintln(w, "[fork] Start the daemon with 'rnix daemon' in another terminal.")
		}
		return
	}
	defer client.Close()

	// Convert ForkContext messages to IPC format
	ipcMessages := make([]ipc.ForkContinueMessage, len(forkCtx.Messages))
	for i, msg := range forkCtx.Messages {
		ipcMessages[i] = ipc.ForkContinueMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		}
	}

	req := ipc.ForkContinueRequest{
		Intent:       forkCtx.Intent,
		SystemPrompt: forkCtx.SystemPrompt,
		Messages:     ipcMessages,
		OriginalPID:  uint64(forkCtx.OriginalPID),
	}

	if !jsonMode {
		fmt.Fprintln(w, "[fork] Creating forked process...")
	}

	fcResp, err := client.ForkContinue(req, nil)
	if err != nil {
		if jsonMode {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[fork] error: %v\n", err)
		}
		return
	}

	if jsonMode {
		data, _ := json.Marshal(fcResp)
		fmt.Fprintln(w, string(data))
	} else {
		ppidStr := fmt.Sprintf("%d", fcResp.PPID)
		if !fcResp.PPIDValid {
			ppidStr = fmt.Sprintf("%d (original process no longer exists)", forkCtx.OriginalPID)
		}
		fmt.Fprintf(w, "[fork] New process spawned: PID %d (PPID: %s)\n", fcResp.PID, ppidStr)
		fmt.Fprintln(w, "[fork] Process is now running with real LLM calls.")
		fmt.Fprintf(w, "[fork] Use 'rnix ps' to check status, 'rnix strace %d' to trace.\n", fcResp.PID)
		fmt.Fprintln(w, "[fork] Returning to replay mode.")
	}
}
