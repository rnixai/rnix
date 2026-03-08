package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

var gdbCmd = &cobra.Command{
	Use:   "gdb <pid>",
	Short: "Attach to a running agent for interactive debugging",
	Long:  "Attach to a running agent process and enter an interactive debugging session.\n\nReceives both syscall events and reasoning logs in real-time.\nType 'detach', 'quit', or 'q' to end the session.\nPress Ctrl+C to gracefully detach without affecting the target process.",
	Example: `  rnix gdb 1              Attach to PID 1
  rnix gdb 1 --verbose    Show full event details
  rnix gdb 1 --json       Output as JSON stream`,
	Args: cobra.ExactArgs(1),
	RunE: runGdb,
}

func runGdb(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.Atoi(args[0])
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer,
			fmt.Sprintf("PID %s", args[0]),
			"invalid PID (expected number)",
			fmt.Sprintf("PID %s: not a valid process ID", args[0]),
			"rnix ps  查看活跃进程")
		exitCode = 1
		return nil
	}
	pid := types.PID(pidNum)

	w := cmd.OutOrStdout()
	mode := resolveOutputMode()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		renderer := ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			"no active daemon (process not found)", "",
			"rnix ps  查看活跃进程")
		exitCode = 1
		return nil
	}
	defer client.Close()

	gdbCtx, gdbCancel := context.WithCancel(context.Background())
	defer gdbCancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		_ = client.SendDetach(pid)
		gdbCancel()
	}()

	var renderer *ui.Renderer
	if !flagJSON {
		renderer = ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
		fmt.Fprintf(w, "[gdb] attaching to PID %d...\n", pid)
	}

	// AttachGdb blocks until stream ends or returns for interactive sessions
	info, err := client.AttachGdb(pid, func(ev ipc.GdbEvent) {
		select {
		case <-gdbCtx.Done():
			return
		default:
		}

		switch ev.Type {
		case ipc.StreamGdbSyscall:
			if flagJSON {
				fmt.Fprintln(w, string(ev.Payload))
			} else {
				var sew ipc.SyscallEventWire
				if err := json.Unmarshal(ev.Payload, &sew); err == nil {
					event := wireToSyscallEvent(sew)
					if flagVerbose {
						fmt.Fprintln(w, debug.FormatEvent(event, debug.Options{Verbose: true}))
					} else if renderer != nil {
						fmt.Fprintln(w, ui.FormatTraceLine(renderer, event, false))
					} else {
						fmt.Fprintln(w, debug.FormatEvent(event, debug.DefaultOptions()))
					}
				}
			}
		case ipc.StreamGdbLog:
			if flagJSON {
				fmt.Fprintln(w, string(ev.Payload))
			} else {
				var lew ipc.LogEntryWire
				if err := json.Unmarshal(ev.Payload, &lew); err == nil {
					fmt.Fprintln(w, FormatLogEntry(renderer, lew))
				}
			}
		case ipc.StreamGdbStateChange:
			if flagJSON {
				fmt.Fprintln(w, string(ev.Payload))
			} else {
				fmt.Fprintf(w, "[gdb] process state changed\n")
			}
		case ipc.StreamGdbPrompt:
			if flagJSON {
				fmt.Fprintln(w, string(ev.Payload))
			} else {
				var prompt map[string]any
				if err := json.Unmarshal(ev.Payload, &prompt); err == nil {
					reason, _ := prompt["reason"].(string)
					switch reason {
					case "step_syscall":
						fmt.Fprintf(w, "\n[gdb] step syscall")
						if name, ok := prompt["syscall_name"]; ok {
							fmt.Fprintf(w, ": %v", name)
						}
						fmt.Fprintln(w)
						if args, ok := prompt["syscall_args"]; ok {
							fmt.Fprintf(w, "[gdb] args: %v\n", args)
						}
						if stepNum, ok := prompt["step_number"]; ok {
							fmt.Fprintf(w, "[gdb] step #%v\n", stepNum)
						}
					case "step_reasoning":
						fmt.Fprintf(w, "\n[gdb] step reasoning")
						if stepNum, ok := prompt["step_number"]; ok {
							fmt.Fprintf(w, " (step #%v)", stepNum)
						}
						fmt.Fprintln(w)
						if summary, ok := prompt["last_result_summary"]; ok && summary != "" {
							fmt.Fprintf(w, "[gdb] last result: %v\n", summary)
						}
					default:
						fmt.Fprintf(w, "\n[gdb] breakpoint hit")
						if reason != "" {
							fmt.Fprintf(w, ": %s", reason)
						}
						fmt.Fprintln(w)
						if bpID, ok := prompt["bp_id"]; ok {
							fmt.Fprintf(w, "[gdb] breakpoint ID: %v\n", bpID)
						}
					}
				}
				fmt.Fprint(w, "gdb> ")
			}
		}
	})
	if err != nil {
		if isNotFoundError(err) {
			if renderer != nil {
				ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
					"process not found or not running",
					fmt.Sprintf("PID %d: 不存在或已退出", pid),
					"rnix ps  查看活跃进程")
			}
		} else {
			if renderer != nil {
				ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
					err.Error(), "", "rnix ps  查看活跃进程")
			}
		}
		exitCode = 1
		return nil
	}

	if !flagJSON && info != nil {
		fmt.Fprintf(w, "[gdb] attached to PID %d (state=%s, intent=%q)\n",
			info.PID, info.State, info.Intent)
		if len(info.Skills) > 0 {
			fmt.Fprintf(w, "[gdb] skills: %s\n", strings.Join(info.Skills, ", "))
		}
		fmt.Fprintf(w, "[gdb] tokens used: %d\n", info.TokensUsed)
		fmt.Fprintf(w, "[gdb] type 'help' for commands, 'detach' to disconnect\n\n")
	}

	// Interactive command loop (read stdin for commands)
	// Blocks until detach/quit or Ctrl+C
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprint(w, "gdb> ")
	for scanner.Scan() {
		select {
		case <-gdbCtx.Done():
			goto done
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		parts := strings.Fields(line)
		if len(parts) == 0 {
			fmt.Fprint(w, "gdb> ")
			continue
		}

		switch parts[0] {
		case "detach", "quit", "q":
			_ = client.SendDetach(pid)
			goto done
		case "help", "h":
			printGdbHelp(w)
		case "info", "i":
			if len(parts) >= 2 && (parts[1] == "breakpoints" || parts[1] == "bp") {
				gdbInfoBreakpoints(w, client, pid)
			} else {
				fmt.Fprintf(w, "  PID:    %d\n", info.PID)
				fmt.Fprintf(w, "  State:  %s\n", info.State)
				fmt.Fprintf(w, "  Intent: %s\n", info.Intent)
				if len(info.Skills) > 0 {
					fmt.Fprintf(w, "  Skills: %s\n", strings.Join(info.Skills, ", "))
				}
				fmt.Fprintf(w, "  Tokens: %d\n", info.TokensUsed)
			}
		case "break", "b":
			gdbBreak(w, client, pid, parts[1:])
		case "delete", "d":
			gdbDelete(w, client, pid, parts[1:])
		case "continue", "c":
			gdbContinue(w, client, pid)
		case "step", "s":
			gdbStep(w, client, pid, parts[1:])
		case "inspect":
			gdbInspect(w, client, pid, parts[1:])
		case "set":
			gdbSet(w, client, pid, parts[1:])
		case "record":
			gdbRecord(w, client, pid, parts[1:])
		default:
			fmt.Fprintf(w, "[gdb] unknown command: %s (type 'help' for commands)\n", line)
		}
		fmt.Fprint(w, "gdb> ")
	}

done:
	if !flagJSON {
		fmt.Fprintf(w, "\n[gdb] detached from PID %d\n", pid)
	}

	return nil
}

// printGdbHelp prints the gdb command help.
func printGdbHelp(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(w, "  break syscall <name>          - Break on syscall (e.g., Read, Write, Open)")
	fmt.Fprintln(w, "  break reasoning               - Break before each reasoning step")
	fmt.Fprintln(w, "  break quality --pattern <pat>  - Break when LLM output matches pattern")
	fmt.Fprintln(w, "  break quality --eval <expr>    - Break when LLM output lacks expression")
	fmt.Fprintln(w, "  break budget <tokens>          - Break when token usage reaches threshold")
	fmt.Fprintln(w, "  delete <bp_id>                 - Delete breakpoint by ID")
	fmt.Fprintln(w, "  info breakpoints / info bp     - List all breakpoints")
	fmt.Fprintln(w, "  continue / c                   - Resume execution after breakpoint hit")
	fmt.Fprintln(w, "  step [syscall|reasoning] / s   - Execute next syscall or reasoning step")
	fmt.Fprintln(w, "  inspect context / inspect ctx  - Show context info with token estimates")
	fmt.Fprintln(w, "  set model <name>               - Override LLM model for subsequent steps")
	fmt.Fprintln(w, "  set context append <text>       - Append text to context")
	fmt.Fprintln(w, "  set skills add <name>           - Add a skill to the agent")
	fmt.Fprintln(w, "  set env KEY=VALUE               - Set an environment variable")
	fmt.Fprintln(w, "  record start                    - Start recording execution events")
	fmt.Fprintln(w, "  record stop                     - Stop recording execution events")
	fmt.Fprintln(w, "  info / i                       - Show process information")
	fmt.Fprintln(w, "  detach / quit / q              - Disconnect from debug session")
	fmt.Fprintln(w, "  help / h                       - Show this help")
}

// gdbBreak sends a break command via IPC.
func gdbBreak(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID, args []string) {
	resp, err := client.SendGdbCommand(pid, "break", args)
	if err != nil {
		fmt.Fprintf(w, "[gdb] error: %v\n", err)
		return
	}
	if resp.OK {
		fmt.Fprintf(w, "[gdb] %s\n", resp.Message)
	} else {
		fmt.Fprintf(w, "[gdb] failed: %s\n", resp.Message)
	}
}

// gdbDelete sends a delete command via IPC.
func gdbDelete(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID, args []string) {
	resp, err := client.SendGdbCommand(pid, "delete", args)
	if err != nil {
		fmt.Fprintf(w, "[gdb] error: %v\n", err)
		return
	}
	if resp.OK {
		fmt.Fprintf(w, "[gdb] %s\n", resp.Message)
	} else {
		fmt.Fprintf(w, "[gdb] failed: %s\n", resp.Message)
	}
}

// gdbContinue sends a continue command via IPC.
func gdbContinue(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID) {
	resp, err := client.SendGdbCommand(pid, "continue", nil)
	if err != nil {
		fmt.Fprintf(w, "[gdb] error: %v\n", err)
		return
	}
	if resp.OK {
		fmt.Fprintf(w, "[gdb] %s\n", resp.Message)
	} else {
		fmt.Fprintf(w, "[gdb] failed: %s\n", resp.Message)
	}
}

// gdbStep sends a step command via IPC.
func gdbStep(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID, args []string) {
	parsed, err := parseStepCommand(args)
	if err != nil {
		fmt.Fprintf(w, "[gdb] %v\n", err)
		return
	}
	resp, respErr := client.SendGdbCommand(pid, "step", []string{parsed.Mode})
	if respErr != nil {
		fmt.Fprintf(w, "[gdb] error: %v\n", respErr)
		return
	}
	if resp.OK {
		fmt.Fprintf(w, "[gdb] %s\n", resp.Message)
	} else {
		fmt.Fprintf(w, "[gdb] failed: %s\n", resp.Message)
	}
}

// gdbInspect sends an inspect command via IPC.
func gdbInspect(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID, args []string) {
	parsed, err := parseInspectCommand(args)
	if err != nil {
		fmt.Fprintf(w, "[gdb] %v\n", err)
		return
	}
	resp, respErr := client.SendGdbCommand(pid, "inspect", []string{parsed.SubCommand})
	if respErr != nil {
		fmt.Fprintf(w, "[gdb] error: %v\n", respErr)
		return
	}
	if !resp.OK {
		fmt.Fprintf(w, "[gdb] failed: %s\n", resp.Message)
		return
	}
	// Format context info for display
	if data, ok := resp.Data.(map[string]any); ok {
		formatContextInfo(w, data)
	}
}

// formatContextInfo renders structured context info in a readable format.
func formatContextInfo(w interface{ Write([]byte) (int, error) }, data map[string]any) {
	pid := data["pid"]
	ctxID := data["ctx_id"]
	fmt.Fprintf(w, "[gdb] Context for PID %v (CtxID: %v):\n", pid, ctxID)

	promptChars := data["system_prompt_chars"]
	promptTokens := data["system_prompt_tokens"]
	fmt.Fprintf(w, "  System Prompt: %v chars (~%v tokens)\n", promptChars, promptTokens)

	totalMsgs := data["total_messages"]
	fmt.Fprintf(w, "  Messages: %v total\n", totalMsgs)

	systemCount := data["system_count"]
	systemTokens := data["system_tokens"]
	fmt.Fprintf(w, "    system:    %v  (~%v tokens)\n", systemCount, systemTokens)

	userCount := data["user_count"]
	userTokens := data["user_tokens"]
	fmt.Fprintf(w, "    user:      %v  (~%v tokens)\n", userCount, userTokens)

	assistantCount := data["assistant_count"]
	assistantTokens := data["assistant_tokens"]
	fmt.Fprintf(w, "    assistant: %v  (~%v tokens)\n", assistantCount, assistantTokens)

	toolCount := data["tool_count"]
	toolTokens := data["tool_tokens"]
	if toolCount != nil && toolCount != float64(0) {
		fmt.Fprintf(w, "    tool:      %v  (~%v tokens)\n", toolCount, toolTokens)
	}

	totalTokens := data["total_tokens"]
	fmt.Fprintf(w, "  Total estimated tokens: ~%v\n", totalTokens)

	if lastRole, ok := data["last_message_role"]; ok {
		lastPreview := data["last_message_preview"]
		fmt.Fprintf(w, "  Last Message: [%v] %v\n", lastRole, lastPreview)
	}
}

// gdbInfoBreakpoints sends an info breakpoints command via IPC.
func gdbInfoBreakpoints(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID) {
	resp, err := client.SendGdbCommand(pid, "info", []string{"breakpoints"})
	if err != nil {
		fmt.Fprintf(w, "[gdb] error: %v\n", err)
		return
	}
	if !resp.OK {
		fmt.Fprintf(w, "[gdb] failed: %s\n", resp.Message)
		return
	}
	bpList, ok := resp.Data.([]any)
	if !ok || len(bpList) == 0 {
		fmt.Fprintln(w, "[gdb] no breakpoints set")
		return
	}
	fmt.Fprintf(w, "  %-4s %-12s %-8s %-6s %s\n", "ID", "Type", "Enabled", "Hits", "Condition")
	for _, item := range bpList {
		bp, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := fmt.Sprintf("%v", bp["id"])
		bpType := fmt.Sprintf("%v", bp["type"])
		enabled := "yes"
		if e, ok := bp["enabled"].(bool); ok && !e {
			enabled = "no"
		}
		hits := fmt.Sprintf("%v", bp["hit_count"])
		cond := fmt.Sprintf("%v", bp["condition"])
		fmt.Fprintf(w, "  %-4s %-12s %-8s %-6s %s\n", id, bpType, enabled, hits, cond)
	}
}

// BreakCommandResult captures the parsed components of a "break" command.
type BreakCommandResult struct {
	SubType      string // "syscall", "reasoning", "quality", "budget"
	SyscallName  string // for "break syscall <name>"
	QualityMode  string // "pattern" or "eval"
	Pattern      string // for "break quality --pattern <pat>"
	EvalExpr     string // for "break quality --eval <expr>"
	BudgetTokens int    // for "break budget <tokens>"
}

// parseBreakCommand parses a "break" command's arguments into a BreakCommandResult.
func parseBreakCommand(args []string) (*BreakCommandResult, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: break <syscall|reasoning|quality|budget> [args...]")
	}
	result := &BreakCommandResult{SubType: args[0]}

	switch args[0] {
	case "syscall":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: break syscall <name>")
		}
		result.SyscallName = args[1]
	case "reasoning":
		// no extra args needed
	case "quality":
		if len(args) < 3 {
			return nil, fmt.Errorf("usage: break quality --pattern <pattern> | --eval <criteria>")
		}
		switch args[1] {
		case "--pattern":
			result.QualityMode = "pattern"
			result.Pattern = args[2]
		case "--eval":
			result.QualityMode = "eval"
			result.EvalExpr = args[2]
		default:
			return nil, fmt.Errorf("unknown quality flag: %s (valid: --pattern, --eval)", args[1])
		}
	case "budget":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: break budget <tokens>")
		}
		tokens, err := strconv.Atoi(args[1])
		if err != nil {
			return nil, fmt.Errorf("invalid budget value: %s", args[1])
		}
		result.BudgetTokens = tokens
	default:
		return nil, fmt.Errorf("unknown break type: %s (valid: syscall, reasoning, quality, budget)", args[0])
	}
	return result, nil
}

// parseDeleteCommand parses a "delete" command's arguments to extract the breakpoint ID.
func parseDeleteCommand(args []string) (int, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("usage: delete <bp_id>")
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("invalid breakpoint ID: %s", args[0])
	}
	return id, nil
}

// StepCommandResult captures the parsed components of a "step" command.
type StepCommandResult struct {
	Mode string // "syscall" or "reasoning"
}

// parseStepCommand parses a "step" command's arguments into a StepCommandResult.
func parseStepCommand(args []string) (*StepCommandResult, error) {
	mode := "syscall" // default
	if len(args) > 0 {
		mode = args[0]
	}

	switch mode {
	case "syscall", "reasoning":
		return &StepCommandResult{Mode: mode}, nil
	default:
		return nil, fmt.Errorf("unknown step mode: %s (valid: syscall, reasoning)", mode)
	}
}

// InspectCommandResult captures the parsed components of an "inspect" command.
type InspectCommandResult struct {
	SubCommand string // "context"
}

// parseInspectCommand parses an "inspect" command's arguments into an InspectCommandResult.
func parseInspectCommand(args []string) (*InspectCommandResult, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: inspect <context|ctx>")
	}

	subCmd := args[0]
	switch subCmd {
	case "context":
		return &InspectCommandResult{SubCommand: "context"}, nil
	case "ctx":
		return &InspectCommandResult{SubCommand: "context"}, nil // alias
	default:
		return nil, fmt.Errorf("unknown inspect target: %s (valid: context, ctx)", subCmd)
	}
}

// gdbSet sends a set command via IPC.
func gdbSet(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID, args []string) {
	parsed, err := parseSetCommand(args)
	if err != nil {
		fmt.Fprintf(w, "[gdb] %v\n", err)
		return
	}
	var ipcArgs []string
	switch parsed.SubCommand {
	case "model":
		ipcArgs = []string{"model", parsed.Value}
	case "context":
		ipcArgs = []string{"context", parsed.Action, parsed.Value}
	case "skills":
		ipcArgs = []string{"skills", parsed.Action, parsed.Value}
	case "env":
		ipcArgs = []string{"env", parsed.Value}
	}
	resp, respErr := client.SendGdbCommand(pid, "set", ipcArgs)
	if respErr != nil {
		fmt.Fprintf(w, "[gdb] error: %v\n", respErr)
		return
	}
	if resp.OK {
		fmt.Fprintf(w, "[gdb] %s\n", resp.Message)
	} else {
		fmt.Fprintf(w, "[gdb] failed: %s\n", resp.Message)
	}
}

// SetCommandResult captures the parsed components of a "set" command.
type SetCommandResult struct {
	SubCommand string // "model", "context", "skills", "env"
	Action     string // "append" for context, "add" for skills
	Value      string // model name, text content, skill name, or KEY=VALUE
}

// parseSetCommand parses a "set" command's arguments into a SetCommandResult.
func parseSetCommand(args []string) (*SetCommandResult, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: set <model|context|skills|env> <args...>")
	}

	subCmd := args[0]
	switch subCmd {
	case "model":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: set model <name>")
		}
		return &SetCommandResult{SubCommand: "model", Value: args[1]}, nil
	case "context":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: set context append <text>")
		}
		if args[1] != "append" {
			return nil, fmt.Errorf("usage: set context append <text>")
		}
		if len(args) < 3 {
			return nil, fmt.Errorf("usage: set context append <text>")
		}
		value := strings.Join(args[2:], " ")
		return &SetCommandResult{SubCommand: "context", Action: "append", Value: value}, nil
	case "skills":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: set skills add <name>")
		}
		if args[1] != "add" {
			return nil, fmt.Errorf("usage: set skills add <name>")
		}
		if len(args) < 3 {
			return nil, fmt.Errorf("usage: set skills add <name>")
		}
		return &SetCommandResult{SubCommand: "skills", Action: "add", Value: args[2]}, nil
	case "env":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: set env KEY=VALUE")
		}
		if !strings.Contains(args[1], "=") {
			return nil, fmt.Errorf("invalid format: expected KEY=VALUE (missing '=')")
		}
		return &SetCommandResult{SubCommand: "env", Value: args[1]}, nil
	default:
		return nil, fmt.Errorf("unknown set target: %s (valid: model, context, skills, env)", subCmd)
	}
}

// gdbRecord handles the record command within a gdb session.
func gdbRecord(w interface{ Write([]byte) (int, error) }, client *ipc.Client, pid types.PID, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(w, "[gdb] usage: record <start|stop>")
		return
	}

	switch args[0] {
	case "start":
		recordID, err := client.RecordStart(pid)
		if err != nil {
			fmt.Fprintf(w, "[gdb] record start failed: %v\n", err)
			return
		}
		fmt.Fprintf(w, "[gdb] recording started (record-id: %s)\n", recordID)
	case "stop":
		eventCount, err := client.RecordStop(pid)
		if err != nil {
			fmt.Fprintf(w, "[gdb] record stop failed: %v\n", err)
			return
		}
		fmt.Fprintf(w, "[gdb] recording stopped (%d events captured)\n", eventCount)
	default:
		fmt.Fprintf(w, "[gdb] unknown record subcommand: %s (valid: start, stop)\n", args[0])
	}
}
