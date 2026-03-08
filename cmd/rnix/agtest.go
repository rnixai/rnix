package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rnixai/rnix/agtest"
	"github.com/spf13/cobra"
)

var flagAgtestDryRun bool

var agtestCmd = &cobra.Command{
	Use:   "agtest [file-or-dir]",
	Short: "Run agent behavior regression tests",
	Long: `Run declarative agent behavior tests defined in YAML files.

Use --dry-run to parse and validate test files without executing them.
Accepts a single YAML file or a directory containing *.yaml files.`,
	Example: `  rnix agtest test.yaml                Parse and run a single test file
  rnix agtest tests/                   Run all tests in a directory
  rnix agtest test.yaml --dry-run      Validate without executing
  rnix agtest tests/ --dry-run --json  Validate with JSON output`,
	Args: cobra.ExactArgs(1),
	RunE: runAgtest,
}

func init() {
	agtestCmd.Flags().BoolVar(&flagAgtestDryRun, "dry-run", false, "Parse and validate only, do not execute tests")
	rootCmd.AddCommand(agtestCmd)
}

func runAgtest(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	path := args[0]

	info, err := os.Stat(path)
	if err != nil {
		return agtestError(w, fmt.Sprintf("cannot access %s: %v", path, err))
	}

	var suite *agtest.TestSuiteSpec
	if info.IsDir() {
		suite, err = agtest.ParseDir(path)
	} else {
		suite, err = agtest.ParseFile(path)
	}

	if err != nil {
		return agtestError(w, err.Error())
	}

	if flagAgtestDryRun {
		return agtestDryRunOutput(w, suite)
	}

	// Full execution is Story 16-3
	return agtestDryRunOutput(w, suite)
}

func agtestDryRunOutput(w interface{ Write([]byte) (int, error) }, suite *agtest.TestSuiteSpec) error {
	if flagJSON {
		type testSummary struct {
			Name   string `json:"name,omitempty"`
			Intent string `json:"intent"`
			Agent  string `json:"agent"`
		}
		summaries := make([]testSummary, len(suite.Tests))
		for i, tc := range suite.Tests {
			summaries[i] = testSummary{Name: tc.Name, Intent: tc.Intent, Agent: tc.Agent.Name}
		}
		resp := JSONResponse{
			OK: true,
			Data: map[string]any{
				"test_count": len(suite.Tests),
				"tests":      summaries,
			},
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprintf(w, "[agtest] parsed %d test case(s)\n", len(suite.Tests))
	for i, tc := range suite.Tests {
		name := tc.Name
		if name == "" {
			name = fmt.Sprintf("test-%d", i+1)
		}
		fmt.Fprintf(w, "  %d. %s (agent: %s, intent: %q)\n", i+1, name, tc.Agent.Name, tc.Intent)
	}
	return nil
}

func agtestError(w interface{ Write([]byte) (int, error) }, msg string) error {
	if flagJSON {
		resp := JSONResponse{OK: false, Error: map[string]string{"message": msg}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
	} else {
		fmt.Fprintf(w, "[agtest] error: %s\n", msg)
	}
	exitCode = 1
	return nil
}
