package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

var ctxProfileCmd = &cobra.Command{
	Use:   "ctx-profile <pid>",
	Short: "Analyze context usage for an agent process",
	Long: `Analyze the context of a running or zombie agent process.

Shows context classification (active/warm/cold/leaked), identifies top token
consumers, and provides optimization suggestions.

Requires a running daemon (the context data lives in the daemon's memory).`,
	Example: `  rnix ctx-profile 1              Analyze context for PID 1
  rnix ctx-profile 1 --json       JSON output`,
	Args: cobra.ExactArgs(1),
	RunE: runCtxProfile,
}

func init() {
	rootCmd.AddCommand(ctxProfileCmd)
}

func runCtxProfile(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("invalid PID: %s", args[0])}}
			data, marshalErr := json.Marshal(resp)
			if marshalErr != nil {
				fmt.Fprintf(w, "[ctx-profile] error: %v\n", marshalErr)
				exitCode = 1
				return nil
			}
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[ctx-profile] error: invalid PID: %s\n", args[0])
		}
		exitCode = 1
		return nil
	}
	pid := types.PID(pidNum)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": "daemon not available"}}
			data, marshalErr := json.Marshal(resp)
			if marshalErr != nil {
				fmt.Fprintf(w, "[ctx-profile] error: %v\n", marshalErr)
				exitCode = 1
				return nil
			}
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[ctx-profile] error: daemon not available (is the daemon running?)\n")
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	result, err := client.CtxProfile(pid)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, marshalErr := json.Marshal(resp)
			if marshalErr != nil {
				fmt.Fprintf(w, "[ctx-profile] error: %v\n", marshalErr)
				exitCode = 1
				return nil
			}
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[ctx-profile] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, err := json.Marshal(resp)
		if err != nil {
			fmt.Fprintf(w, "[ctx-profile] error: %v\n", err)
			exitCode = 1
			return nil
		}
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprint(w, debug.FormatCtxProfile(result))
	return nil
}
