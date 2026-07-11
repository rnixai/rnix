# Copilot Instructions for Rnix

## Project Overview

Rnix is an operating system for AI agents, inspired by Unix design philosophy. Written in Go 1.26 (`github.com/rnixai/rnix`). It provides process management, a virtual filesystem (VFS), IPC via Unix domain sockets, multi-agent orchestration, intent decomposition, and debugging tools.

## 语言

- 回复语言使用简体中文
- 代码注释使用英文

## Build & Test Commands

```bash
make build          # Build binary → ./rnix
make test           # Run all tests with race detection
make lint           # golangci-lint
make vet            # go vet
make all            # lint + vet + modernize-check + test + build

# Single test
go test -race -run TestFunctionName ./package/...

# Package tests
go test -race ./kernel/...
go test -race ./ipc/...
```

Run `make all` after finishing any coding work to catch errors.

## Architecture

### Daemon Model

Rnix runs as a background daemon holding the kernel and process table. CLI commands communicate over a Unix domain socket (`$XDG_RUNTIME_DIR/rnix/rnix.sock` or `/tmp/rnix-$UID/rnix.sock`). The CLI auto-starts the daemon via `EnsureDaemon()`.

### Core Data Flow

```
CLI (cmd/rnix) → IPC Client → Unix Socket → IPC Server → Kernel
                                                            ↓
                                              Process ReasonStep Loop
                                                  ↓           ↑
                                             VFS Devices    Context
                                            (LLM, FS, Shell, MCP)
```

### Package Hierarchy

```
cmd/rnix           ← Entry point, Cobra CLI
├── ipc            ← Client/Server, NDJSON protocol over Unix socket
├── kernel         ← Microkernel: process table, spawn, kill, wait, reaper
│   ├── vfs        ← VFS file abstraction, device registry, FD table
│   ├── context    ← Per-process conversation history (CtxAlloc/Write/BuildPrompt)
│   └── debug      ← Strace, recording, distributed tracing, GDB
├── drivers/       ← VFS device implementations
│   ├── llm        ← /dev/llm/* (Claude CLI, Cursor CLI, HTTP API providers)
│   ├── fs         ← /dev/fs - sandboxed host filesystem
│   ├── shell      ← /dev/shell - subprocess execution
│   └── mcp        ← /dev/mcp/* - MCP server stdio transport
├── intent         ← Declarative intent decomposition
├── compose        ← DAG-based multi-agent orchestration from YAML
├── shell          ← AgentShell scripting language
├── agents         ← Agent loader (lib/agents/{name}/agent.yaml + instructions.md)
├── skills         ← Skill loader (lib/skills/{name}/SKILL.md with YAML frontmatter)
├── skillpkg       ← Skill package management (install/search/update from registry)
└── internal/      ← Shared utilities
    ├── types      ← PID, FD, CtxID, Signal, ProcessState, ErrCode
    ├── xsync      ← Thread-safe SyncMap, Registry, Future
    └── ui         ← Terminal rendering, progress reporting
```

### Key Abstractions

- **Process** (`kernel/process.go`): State machine: Created → Running → Zombie → Dead. Each process runs a `reasonStep` goroutine looping LLM calls through VFS devices.
- **VFS** (`vfs/`): All resources (LLM, filesystem, shell, MCP) accessed as files via Open/Read/Write/Close. Devices register path prefixes.
- **Context** (`context/`): Per-process message history. `CtxAlloc` → `CtxWrite` → `BuildPrompt` cycle. Circular buffer.
- **Kernel** (`kernel/kernel.go`): Composed of sub-interfaces — ProcessManager, MountManager, IPCManager, SignalManager.
- **Intent** (`intent/`): LLM-based decomposition of high-level intent into a DAG of sub-tasks. States: pending → decomposing → await_confirm → executing → completed/failed.
- **IPC**: NDJSON over Unix socket. Request: `{"method": "spawn|kill|list_procs|...", "payload": {...}}`.

## Conventions

### Import Aliases

Use aliases to avoid conflicts with stdlib packages:

```go
import (
    rnixctx "github.com/rnixai/rnix/context"          // not "context"
    drivershell "github.com/rnixai/rnix/drivers/shell"  // not "shell"
    agentshell "github.com/rnixai/rnix/shell"           // not "shell"
)
```

### Custom Types

Always use typed identifiers from `internal/types` — never raw integers:

```go
types.PID       // process ID
types.FD        // file descriptor
types.CtxID     // context ID
types.TraceID   // distributed trace ID
types.SpanID    // trace span ID
types.ErrCode   // TIMEOUT, NOT_FOUND, PERMISSION, INTERNAL, DRIVER, etc.
types.Signal    // SIGTERM, SIGKILL, SIGINT, SIGPAUSE, SIGRESUME
```

### Thread Safety

Use `internal/xsync.SyncMap` for concurrent maps — not `sync.Map`:

```go
procTable := xsync.NewSyncMap[types.PID, *Process]()
```

### VFS Device Registration

Devices must declare full `ToolDef` metadata:
- Set `IsReadOnly`, `IsConcurrencySafe`, `IsDestructive` explicitly (fail-closed defaults: all false)
- Set `MaxResultTokens` to prevent context overflow
- Use `ShouldDefer` + `SearchHint` for non-core devices
- Device descriptions use Go `embed` templates (not hardcoded strings)

### Configuration

Two-tier model: global (`~/.config/rnix/`) + project (`.rnix/`). Run `rnix init` to bootstrap.

- `providers.yaml` — LLM provider definitions
- `init.yaml` — Bootstrap services and supervisor trees
- `compose.yaml` — Multi-agent workflow DAGs
- `agents/*/agent.yaml` + `instructions.md` — Agent manifests and system prompts
- `skills/*/SKILL.md` — Skill definitions (YAML frontmatter + markdown body)

### Environment Variables

- `RNIX_ASCII=1` — forces ASCII mode (disables Unicode glyphs)
- `RNIX_ENV` — selects .env file set (default: `development`)
- Project `.env` files loaded per-spawn: `.env` → `.env.local` → `.env.{RNIX_ENV}` → `.env.{RNIX_ENV}.local`
- API keys resolved via env snapshot, not `os.Getenv`

### Commit Messages

Use conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`

### Branch Naming

`feat/short-description`, `fix/short-description`, `docs/short-description`, `refactor/short-description`

## Known Test Issues

- `TestRunTop_NoDaemon` fails in environments without `/dev/tty` (CI/containers)
- `TestClaudeCliDriver_Call_DefaultArgs` may fail if default model constant changes
