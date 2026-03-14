package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

var topologyCmd = &cobra.Command{
	Use:   "topology",
	Short: "Show agent collaboration topology and reinforced paths",
	Example: `  rnix topology          Show collaboration topology
  rnix topology --json   JSON output`,
	Args: cobra.NoArgs,
	RunE: runTopology,
}

func init() {
	rootCmd.AddCommand(topologyCmd)
}

func runTopology(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": "daemon not available"}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintln(w, "[topology] error: daemon not available (is the daemon running?)")
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	result, err := client.TopologyQuery()
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintf(w, "[topology] error: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: result}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	formatTopologyText(w, result)
	return nil
}

func formatTopologyText(w io.Writer, response *ipc.TopologyQueryResponse) {
	nodeCount := len(response.Nodes)
	edgeCount := len(response.Edges)

	fmt.Fprintf(w, "Collaboration Topology (%d agents, %d edges)\n", nodeCount, edgeCount)

	if nodeCount == 0 {
		fmt.Fprintln(w, "\nNo collaboration data available.")
		return
	}

	// NODES section
	fmt.Fprintln(w)
	fmt.Fprintln(w, "NODES:")

	// Sort nodes by name for stable output
	nodes := make([]kernel.TopologyNode, len(response.Nodes))
	copy(nodes, response.Nodes)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Agent < nodes[j].Agent
	})

	fmt.Fprintf(w, "%-20s %10s  %11s\n", "AGENT", "REPUTATION", "CONNECTIONS")
	for _, n := range nodes {
		fmt.Fprintf(w, "%-20s %10.2f  %11d\n",
			truncate(n.Agent, 20),
			n.ReputationScore,
			n.Connections,
		)
	}

	// EDGES section
	fmt.Fprintln(w)
	fmt.Fprintln(w, "EDGES:")

	// Sort edges by Total descending
	edges := make([]kernel.CooperationEdge, len(response.Edges))
	copy(edges, response.Edges)
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].Total > edges[j].Total
	})

	fmt.Fprintf(w, "%-20s %-20s %5s  %3s  %5s  %s\n",
		"FROM", "TO", "SPAWN", "MSG", "TOTAL", "REINFORCED")
	for _, e := range edges {
		marker := ""
		if e.Reinforced {
			marker = "*"
		}
		fmt.Fprintf(w, "%-20s %-20s %5d  %3d  %5d  %s\n",
			truncate(e.From, 20),
			truncate(e.To, 20),
			e.SpawnCount,
			e.MsgCount,
			e.Total,
			marker,
		)
	}

	// REINFORCED PATHS section
	if len(response.ReinforcedPaths) > 0 {
		fmt.Fprintf(w, "\nREINFORCED PATHS (%d):\n", len(response.ReinforcedPaths))
		for _, rp := range response.ReinforcedPaths {
			fmt.Fprintf(w, "  %s -> %s (%d interactions)\n", rp.From, rp.To, rp.Total)
		}
	}
}
