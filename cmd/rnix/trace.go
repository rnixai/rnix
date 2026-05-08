package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
)

var traceCmd = &cobra.Command{
	Use:   "trace [trace-id]",
	Short: "View distributed trace data",
	Long: `View distributed trace data from completed process executions.

All top-level processes automatically generate trace data. Compose
orchestrations share a single trace across all agents in the DAG.

Without arguments, lists all available traces.
With a trace-id, shows the full span tree with timing and token usage.

Trace data is read from local .rnix/traces/ directory (no daemon required).`,
	Example: `  rnix trace                                List all traces
  rnix trace abcdef1234567890               Show span tree for trace
  rnix trace abcdef1234567890 --json        JSON output
  rnix trace abcdef1234567890 --verbose     Show extra span details`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTrace,
}

var blameCmd = &cobra.Command{
	Use:   "blame <trace-id>",
	Short: "Analyze trace to find bottlenecks and root causes",
	Long: `Analyze a distributed trace to identify performance bottlenecks and error root causes.

Shows critical path analysis, duration/token hotspots, and error propagation chains.`,
	Example: `  rnix trace blame abcdef1234567890          Analyze trace
  rnix trace blame abcdef1234567890 --json   JSON output`,
	Args: cobra.ExactArgs(1),
	RunE: runBlame,
}

func init() {
	traceCmd.AddCommand(blameCmd)
	rootCmd.AddCommand(traceCmd)
}

func runTrace(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	reader := debug.NewSpanReader(findTraceBaseDir())

	if len(args) == 0 {
		return runTraceList(w, reader)
	}
	return runTraceView(w, reader, types.TraceID(args[0]))
}

func runTraceList(w interface{ Write([]byte) (int, error) }, reader *debug.SpanReader) error {
	summaries, err := reader.ListTraces()
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[trace] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: summaries}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprint(w, debug.FormatTraceList(summaries))
	return nil
}

func runTraceView(w interface{ Write([]byte) (int, error) }, reader *debug.SpanReader, traceID types.TraceID) error {
	spans, err := reader.ReadSpans(traceID)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[trace] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if len(spans) == 0 {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("trace %q not found", traceID)}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[trace] error: trace %q not found\n", traceID)
		}
		exitCode = 1
		return nil
	}

	tree := debug.BuildSpanTree(spans)

	if flagJSON {
		resp := JSONResponse{OK: true, Data: tree}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprint(w, debug.FormatTraceTree(tree, flagVerbose))
	return nil
}

func runBlame(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	reader := debug.NewSpanReader(findTraceBaseDir())
	traceID := types.TraceID(args[0])

	spans, err := reader.ReadSpans(traceID)
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[trace] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if len(spans) == 0 {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": fmt.Sprintf("trace %q not found", traceID)}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[trace] error: trace %q not found\n", traceID)
		}
		exitCode = 1
		return nil
	}

	tree := debug.BuildSpanTree(spans)
	result := debug.AnalyzeTrace(tree)

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprint(w, debug.FormatBlameResult(result))
	return nil
}

func findTraceBaseDir() string {
	cwd, _ := os.Getwd()
	return cwd + "/.rnix/traces"
}
