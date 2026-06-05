package kernel

import "slices"

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
// keeping a's order first and appending only the elements of b not already
// present. Used to build an ActionSpawn child's DeniedDevices: the parent's
// existing blacklist is preserved while the orchestration-only devices are
// added (symmetric with cmd/rnix/main.go SpawnFunc's denied /dev/intent,
// preventing recursive orchestration).
func unionDevices(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, d := range b {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}
