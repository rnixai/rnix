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
make all    # lint + vet + test + build
```

## Development Workflow

### Build & Test

```bash
make build          # Build binary → ./rnix
make test           # Run all tests with race detection
make lint           # golangci-lint
make vet            # go vet
make all            # lint + vet + test + build (run this before every PR)
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
