# Contributing to Rnix

Thanks for your interest in contributing to Rnix! This document covers the guidelines and workflow for contributing.

## Prerequisites

- **Go 1.26+**
- **golangci-lint** — `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
- **At least one LLM provider** for integration testing (see [Configuration Guide](https://rnix.ai/guide/configuration))

## Quick Start

```bash
git clone https://github.com/rnixai/rnix.git
cd rnix
make all    # lint + vet + modernize-check + test + build
```

## Development Workflow

### Build & Test

```bash
make build          # Build binary → ./rnix
make test           # Run all tests with race detection
make lint           # golangci-lint
make vet            # go vet
make all            # lint + vet + modernize-check + test + build (run this before every PR)
```

Run a single test:

```bash
go test -race -run TestFunctionName ./package/...
```

### Branch Naming

- `feat/short-description` — new features
- `fix/short-description` — bug fixes
- `docs/short-description` — documentation changes
- `refactor/short-description` — code refactoring

### Commit Messages

Use conventional commit style:

```
feat: add ctx-growth prediction command
fix: handle nil provider in spawn callback
docs: update CLI reference with new commands
refactor: extract provider resolution to helper
```

## Release Workflow

Releases are driven by Makefile targets and documented in [CHANGELOG.md](CHANGELOG.md). The flow is two steps — a local, reversible `release` followed by a `publish` that pushes the tag. Pushing the tag triggers the **Release** GitHub Actions workflow, which runs [GoReleaser](https://goreleaser.com) to build cross-platform archives and create the GitHub release. GoReleaser is the single source of release artifacts — never upload binaries by hand.

### 1. Document the release

Add a new version section to `CHANGELOG.md` under `## [Unreleased]`, following the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format:

```markdown
## [0.10.1] - 2026-06-25

Theme: **One-line summary of the release**

### Added
- ...

### Fixed
- ...
```

Also add the comparison link at the bottom of the file:

```markdown
[0.10.1]: https://github.com/rnixai/rnix/compare/v0.10.0...v0.10.1
```

Document only user-facing feature and behavior changes. Keep fixes concise and avoid leaking internal implementation details. The matching section becomes the GitHub release notes verbatim, so write it for readers of the release page.

### 2. Tag and build (local, reversible)

```bash
make release VERSION=0.10.1
```

This validates the version is semver, checks the working tree is clean, verifies `CHANGELOG.md` has a matching section, runs `lint + vet + modernize-check + test`, creates the annotated tag `v0.10.1`, and builds a local binary as a smoke test. Nothing leaves your machine.

### 3. Publish (push the tag)

```bash
make publish VERSION=0.10.1
```

This pushes the tag. That is the only outward action it takes. The pushed `v*` tag triggers the **Release** workflow, which runs the test suite, then GoReleaser builds the cross-platform archives + checksums and creates the GitHub release. Release notes are pulled from the matching `CHANGELOG.md` section; if no section exists, GoReleaser falls back to generating notes from commits and the workflow logs a warning.

Watch the run to completion:

```bash
make release-watch
```

> Do **not** run `gh release create` or upload assets manually — GoReleaser owns every release artifact. A hand-uploaded binary will sit alongside the proper archives without a checksum and only covers one platform.

### Supporting targets

```bash
make changelog-check VERSION=0.10.1   # verify CHANGELOG has the version section
make release-notes VERSION=0.10.1     # print the CHANGELOG body for that version
```

Version numbers follow [Semantic Versioning](https://semver.org/): patch for fixes and small polish, minor for new features, major for breaking changes.

## Code Conventions

### Import Aliases

Use aliases to avoid conflicts with stdlib packages:

```go
import (
    rnixctx "github.com/rnixai/rnix/context"       // not "context"
    drivershell "github.com/rnixai/rnix/drivers/shell" // not "shell"
    agentshell "github.com/rnixai/rnix/shell"          // not "shell"
)
```

### Custom Types

Always use typed identifiers from `internal/types` — not raw integers:

```go
types.PID       // process ID
types.FD        // file descriptor
types.CtxID     // context ID
types.TraceID   // distributed trace ID
types.SpanID    // trace span ID
```

### Thread Safety

Use `internal/xsync.SyncMap` for concurrent maps — not `sync.Map`:

```go
procTable := xsync.NewSyncMap[types.PID, *Process]()
```

### Error Handling

Use `types.ErrCode` constants for structured errors:

```go
types.ErrNotFound    // resource not found
types.ErrTimeout     // operation timed out
types.ErrPermission  // permission denied
```

### Signals

```go
types.SIGTERM    // graceful termination
types.SIGKILL    // forced termination
types.SIGINT     // interrupt
types.SIGPAUSE   // pause process
types.SIGRESUME  // resume paused process
```

## Project Structure

```
cmd/rnix/          ← CLI entry point (Cobra commands)
kernel/            ← Microkernel: process table, spawn, kill, wait
vfs/               ← Virtual file system abstraction
context/           ← Per-process conversation history
drivers/           ← VFS device implementations (llm, fs, shell, mcp)
ipc/               ← Client/Server over Unix domain socket
internal/          ← Shared utilities (types, xsync, ui)
compose/           ← Multi-agent DAG orchestration
intent/            ← Declarative intent decomposition
shell/             ← AgentShell scripting language
agents/            ← Agent loader
skills/            ← Skill loader
skillpkg/          ← Skill package management
debug/             ← Strace, recording, tracing
```

## Configuration System

Rnix uses a two-tier configuration model:

- **Global** — `~/.config/rnix/` (providers, agents, skills)
- **Project** — `.rnix/` (project-specific overrides)

Config files use bare names (no `rnix-` prefix): `providers.yaml`, `init.yaml`, `compose.yaml`.

Run `rnix init` to bootstrap both directories.

## Pull Request Checklist

Before submitting a PR, ensure:

- [ ] `make all` passes (lint + vet + test + build)
- [ ] New code follows existing conventions (see above)
- [ ] Tests added for new functionality
- [ ] No hardcoded paths — use `internal/config` package for config paths
- [ ] Thread-safe data structures use `xsync.SyncMap`
- [ ] Typed identifiers used (not raw `int` or `uint64`)

## Reporting Issues

Open an issue at [github.com/rnixai/rnix/issues](https://github.com/rnixai/rnix/issues) with:

- Rnix version (`rnix version`)
- OS and Go version
- Steps to reproduce
- Expected vs actual behavior

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
