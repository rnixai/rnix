package kernel

import (
	"slices"
	"strings"

	"github.com/rnixai/rnix/vfs"
)

// orchestrationOnlyDevices lists the "orchestration-only" VFS devices: devices
// that exist solely to decompose/orchestrate intent and provide no concrete
// execution capability (filesystem, shell, memory, …).
//
// Single source of truth (Story 37.5). An ActionSpawn-derived child would
// otherwise inherit its parent's AllowedDevices verbatim; these devices are
// stripped so the child ends up with the same executable device set as an
// intent-decompose child (cmd/rnix/main.go SpawnFunc). This eliminates the
// deadlock where a child left with only /dev/intent lacks execution devices,
// hallucinates /dev/shell, spawns recursively, and trips the depth breaker.
//
// Extension point: append new orchestration-only devices here as they appear.
var orchestrationOnlyDevices = []string{"/dev/intent"}

// stripOrchestrationDevices returns a new slice with every orchestration-only
// device removed, preserving the order of the remaining (real) devices.
//
// It never mutates its input. When the result is empty it returns nil so that
// spawn.go treats opts.AllowedDevices as unset and falls through to the
// fail-open branch — matching the SpawnFunc semantics for intent children.
func stripOrchestrationDevices(devs []string) []string {
	var out []string
	for _, d := range devs {
		if !slices.Contains(orchestrationOnlyDevices, d) {
			out = append(out, d)
		}
	}
	return out
}

// unionDevices returns the de-duplicated union of a and b in a new slice,
// preserving first-seen order (all of a, then any of b not already present).
// Both inputs are de-duplicated, so the result is free of duplicates even if a
// itself contains repeats. Used to build an ActionSpawn child's DeniedDevices:
// the parent's existing blacklist is preserved AND the orchestration-only
// devices are added. This carries the same /dev/intent deny as cmd/rnix/main.go
// SpawnFunc but is strictly stronger — SpawnFunc hard-sets []string{"/dev/intent"}
// (intent children have no parent denylist to keep), whereas ActionSpawn also
// retains the parent's denies, blocking recursive orchestration without dropping
// inherited restrictions.
func unionDevices(a, b []string) []string {
	var out []string
	for _, d := range a {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	for _, d := range b {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// expandDevicesToTools expands a set of base device paths into the full list of
// tool names those devices expose (Story 54.1 — shared infrastructure for AC2 and
// AC4). It powers two backward-compatibility paths: expanding a legacy
// `allowed-tools: /dev/fs` declaration into [Read Write Edit Glob Grep], and
// rebuilding a persisted process's AllowedTools from its AllowedDevices on
// resume / load_suspended. MCP mount paths have dynamic, per-server tool names
// and yield NO base tool names here (callers keep the path in AllowedDevices).
//
// It mirrors collectFromDriver in toolgen.go: for each non-MCP, non-LLM device
// path it looks up the registered driver, asserts vfs.ToolDescriptor, and
// collects every ToolDef.Name. Names are de-duplicated (first-seen order) so
// overlapping devices do not inflate the list. Unknown paths, MCP mounts, LLM
// devices, and drivers without a ToolDescriptor are silently skipped. Returns
// nil for empty/nil input so callers can treat "no tools" and "unconstrained"
// identically.
func expandDevicesToTools(reg *vfs.DeviceRegistry, devices []string) []string {
	if reg == nil || len(devices) == 0 {
		return nil
	}
	var tools []string
	seen := make(map[string]struct{})
	for _, devPath := range devices {
		// MCP mounts expose dynamic tool names — keep them path-gated, not here.
		if strings.HasPrefix(devPath, mcpPathPrefix) {
			continue
		}
		// LLM devices are not user-invocable tools (mirror collectFromDriver).
		if strings.HasPrefix(devPath, "/dev/llm") {
			continue
		}
		driver, ok := reg.GetDriver(devPath)
		if !ok {
			continue // silently skip unknown paths
		}
		td, ok := driver.(vfs.ToolDescriptor)
		if !ok {
			continue
		}
		for _, def := range td.ToolDefs() {
			if _, dup := seen[def.Name]; dup {
				continue
			}
			seen[def.Name] = struct{}{}
			tools = append(tools, def.Name)
		}
	}
	return tools
}

// restoreAllowedTools restores a process's tool-name whitelist from a persisted
// snapshot (Story 54.1, NFR87 — zero-break migration). When the snapshot carries
// an explicit AllowedTools it is used verbatim. Legacy snapshots predate the
// field and hold only AllowedDevices; for them the tool set is rebuilt by
// expanding the device whitelist so resume / daemon-restart revival keeps
// tool-level enforcement behaving exactly as the device-level enforcement did
// before this story. proc.AllowedDevices must already be restored when called.
func (k *KernelImpl) restoreAllowedTools(proc *Process, persisted []string) {
	if len(persisted) > 0 {
		proc.AllowedTools = append([]string(nil), persisted...)
		return
	}
	if len(proc.AllowedDevices) > 0 {
		proc.AllowedTools = expandDevicesToTools(k.vfs.DeviceRegistry(), proc.AllowedDevices)
	}
}
