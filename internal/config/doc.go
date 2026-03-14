// Package config provides unified configuration infrastructure for Rnix.
//
// It implements four core capabilities:
//
//   - Dual-layer path resolution: global ($XDG_CONFIG_HOME/rnix/) and
//     project-local (.rnix/) configuration directories.
//   - YAML deep merge: recursive map merging with override semantics.
//   - Shadow lookup: project-level agents/skills shadow global-level
//     counterparts via ShadowResolve and ListMerged.
//   - Embedded resource extraction: extract embed.FS contents to disk
//     with skip-existing or force-overwrite modes.
//
// This package depends only on the Go standard library.
package config
