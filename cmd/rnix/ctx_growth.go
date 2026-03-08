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

var ctxGrowthCmd = &cobra.Command{
	Use:   "ctx-growth <pid>",
	Short: "Predict context growth and budget alerts",
	Long: `Predict token growth trend for a running agent process.

Shows historical growth rate, predicts budget exhaustion timing, and displays
alert status when the remaining budget drops below 20%.

Requires a running daemon (the token history lives in the daemon's memory).`,
	Example: `  rnix ctx-growth 1              Growth prediction for PID 1
  rnix ctx-growth 1 --json       JSON output`,
	Args: cobra.ExactArgs(1),
	RunE: runCtxGrowth,
}

func init() {
	rootCmd.AddCommand(ctxGrowthCmd)
}

func runCtxGrowth(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("invalid PID: %s", args[0])}}
			data, marshalErr := json.Marshal(resp)
			if marshalErr != nil {
				fmt.Fprintf(w, "[ctx-growth] error: %v\n", marshalErr)
				exitCode = 1
				return nil
			}
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[ctx-growth] error: invalid PID: %s\n", args[0])
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
				fmt.Fprintf(w, "[ctx-growth] error: %v\n", marshalErr)
				exitCode = 1
				return nil
			}
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[ctx-growth] error: daemon not available (is the daemon running?)\n")
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	result, err := client.CtxGrowth(pid)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, marshalErr := json.Marshal(resp)
			if marshalErr != nil {
				fmt.Fprintf(w, "[ctx-growth] error: %v\n", marshalErr)
				exitCode = 1
				return nil
			}
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[ctx-growth] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, err := json.Marshal(resp)
		if err != nil {
			fmt.Fprintf(w, "[ctx-growth] error: %v\n", err)
			exitCode = 1
			return nil
		}
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprint(w, debug.FormatGrowthPrediction(result))
	return nil
}
